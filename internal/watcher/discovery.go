package watcher

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/model"
)

// MetricProfile describes which Prometheus series carry RED metrics for one
// service. It is discovered at runtime so Damascus works against services whose
// metric names and labels it has never seen, using only the Prometheus API.
type MetricProfile struct {
	Histogram     string  `json:"histogram"`      // base name; _bucket and _count series must exist
	ServiceLabel  string  `json:"service_label"`  // label whose value equals the telemetry service name
	StatusLabel   string  `json:"status_label"`   // empty when the family has no status dimension
	ErrorMatcher  string  `json:"error_matcher"`  // e.g. =~"5.." or ="STATUS_CODE_ERROR"
	ExtraSelector string  `json:"extra_selector"` // e.g. span_kind="SPAN_KIND_SERVER"
	ToMs          float64 `json:"to_ms"`          // multiplier from the histogram unit to milliseconds
}

func (p MetricProfile) selector(service string, extra ...string) string {
	parts := []string{fmt.Sprintf("%s=%q", p.ServiceLabel, service)}
	if p.ExtraSelector != "" {
		parts = append(parts, p.ExtraSelector)
	}
	parts = append(parts, extra...)
	return strings.Join(parts, ",")
}

// Queries returns the PromQL for request rate, P95 latency (histogram unit) and error count rate.
func (p MetricProfile) Queries(service string) (rate, p95, errorsQ string) {
	sel := p.selector(service)
	rate = fmt.Sprintf(`sum(rate(%s_count{%s}[30s]))`, p.Histogram, sel)
	p95 = fmt.Sprintf(`histogram_quantile(0.95, sum(rate(%s_bucket{%s}[30s])) by (le))`, p.Histogram, sel)
	if p.StatusLabel != "" {
		errorsQ = fmt.Sprintf(`sum(rate(%s_count{%s}[30s]))`, p.Histogram, p.selector(service, p.StatusLabel+p.ErrorMatcher))
	}
	return rate, p95, errorsQ
}

// Label preferences, most specific first.
var (
	serviceLabels = []string{"service_name", "service", "exported_job", "job", "app", "application"}
	statusLabels  = []string{"http_response_status_code", "http_status_code", "status_code", "status", "code", "rpc_grpc_status_code", "grpc_code"}
)

const (
	discoveryLookback  = 15 * time.Minute
	discoveryRetry     = 30 * time.Second
	maxCandidateFamily = 25
)

var errNoProfile = errors.New("no latency histogram in Prometheus is labelled with this service")

type profileCache struct {
	mu      sync.Mutex
	found   map[string]MetricProfile
	missed  map[string]time.Time
	nowFunc func() time.Time
}

func newProfileCache() *profileCache {
	return &profileCache{found: map[string]MetricProfile{}, missed: map[string]time.Time{}, nowFunc: time.Now}
}

// EnableMetricDiscovery lets QuerySnapshot fall back to auto-discovered metric
// families when the built-in metric names have no samples for a service.
func (c *PrometheusClient) EnableMetricDiscovery() *PrometheusClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.profiles = newProfileCache()
	return c
}

func (c *PrometheusClient) profileCache() *profileCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.profiles == nil {
		c.profiles = newProfileCache()
	}
	return c.profiles
}

func (c *PrometheusClient) discoveryEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.profiles != nil
}

// DiscoverProfile finds the best RED metric family for service, caching the result.
func (c *PrometheusClient) DiscoverProfile(ctx context.Context, service string) (MetricProfile, error) {
	cache := c.profileCache()
	cache.mu.Lock()
	if p, ok := cache.found[service]; ok {
		cache.mu.Unlock()
		return p, nil
	}
	if at, ok := cache.missed[service]; ok && cache.nowFunc().Sub(at) < discoveryRetry {
		cache.mu.Unlock()
		return MetricProfile{}, errNoProfile
	}
	cache.mu.Unlock()

	p, err := c.discover(ctx, service)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err != nil {
		cache.missed[service] = cache.nowFunc()
		return MetricProfile{}, err
	}
	cache.found[service] = p
	delete(cache.missed, service)
	return p, nil
}

func familyScore(name string) int {
	score := 0
	for word, points := range map[string]int{"server": 4, "http": 2, "rpc": 1, "request": 1, "client": -10} {
		if strings.Contains(name, word) {
			score += points
		}
	}
	return score
}

func (c *PrometheusClient) discover(ctx context.Context, service string) (MetricProfile, error) {
	end := time.Now()
	start := end.Add(-discoveryLookback)
	names, _, err := c.promAPI().LabelValues(ctx, "__name__", nil, start, end)
	if err != nil {
		return MetricProfile{}, fmt.Errorf("list prometheus metric names: %w", err)
	}
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[string(n)] = true
	}
	var families []string
	for name := range present {
		base, ok := strings.CutSuffix(name, "_bucket")
		if !ok || !present[base+"_count"] {
			continue
		}
		if strings.Contains(base, "duration") || strings.Contains(base, "latency") {
			families = append(families, base)
		}
	}
	sort.Slice(families, func(i, j int) bool {
		si, sj := familyScore(families[i]), familyScore(families[j])
		if si != sj {
			return si > sj
		}
		return families[i] < families[j]
	})
	if len(families) > maxCandidateFamily {
		families = families[:maxCandidateFamily]
	}

	best, bestScore, found := MetricProfile{}, math.MinInt, false
	for _, family := range families {
		series, _, err := c.promAPI().Series(ctx, []string{family + "_count"}, start, end)
		if err != nil {
			continue
		}
		profile, ok := profileFromSeries(family, service, series)
		if !ok {
			continue
		}
		score := familyScore(family)
		if profile.ExtraSelector != "" {
			score += 4 // span metrics filtered to server spans are protocol independent
		}
		if profile.StatusLabel != "" {
			score++
		}
		if score > bestScore {
			best, bestScore, found = profile, score, true
		}
	}
	if !found {
		return MetricProfile{}, errNoProfile
	}
	return best, nil
}

// profileFromSeries picks labels by inspecting the series that belong to service.
func profileFromSeries(family, service string, series []model.LabelSet) (MetricProfile, bool) {
	profile := MetricProfile{Histogram: family, ToMs: 1000}
	if strings.Contains(family, "milliseconds") || strings.HasSuffix(family, "_ms") {
		profile.ToMs = 1
	}
	var matched []model.LabelSet
	for _, label := range serviceLabels {
		for _, s := range series {
			if string(s[model.LabelName(label)]) == service {
				matched = append(matched, s)
			}
		}
		if len(matched) > 0 {
			profile.ServiceLabel = label
			break
		}
	}
	if len(matched) == 0 {
		return profile, false
	}
	values := func(label string) map[string]bool {
		out := map[string]bool{}
		for _, s := range matched {
			if v, ok := s[model.LabelName(label)]; ok {
				out[string(v)] = true
			}
		}
		return out
	}
	if kinds := values("span_kind"); kinds["SPAN_KIND_SERVER"] {
		profile.ExtraSelector = `span_kind="SPAN_KIND_SERVER"`
	}
	for _, label := range statusLabels {
		vals := values(label)
		if len(vals) == 0 {
			continue
		}
		profile.StatusLabel = label
		switch {
		case vals["STATUS_CODE_OK"] || vals["STATUS_CODE_UNSET"] || vals["STATUS_CODE_ERROR"]:
			profile.ErrorMatcher = `="STATUS_CODE_ERROR"`
		case label == "grpc_code" || label == "rpc_grpc_status_code":
			profile.ErrorMatcher = `!~"OK|0"`
		default:
			profile.ErrorMatcher = `=~"5.."`
		}
		break
	}
	return profile, true
}

// queryDiscovered builds a snapshot from an auto-discovered metric profile.
func (c *PrometheusClient) queryDiscovered(ctx context.Context, snapshot MetricSnapshot) (MetricSnapshot, error) {
	profile, err := c.DiscoverProfile(ctx, snapshot.TargetService)
	if err != nil {
		return snapshot, err
	}
	rateQ, p95Q, errQ := profile.Queries(snapshot.TargetService)
	rate, err := c.queryRequired(ctx, rateQ)
	if err != nil {
		return snapshot, fmt.Errorf("discovered request rate (%s): %w", profile.Histogram, err)
	}
	p95, err := c.queryRequired(ctx, p95Q)
	if err != nil {
		return snapshot, fmt.Errorf("discovered p95 latency (%s): %w", profile.Histogram, err)
	}
	errRate := 0.0
	if errQ != "" && rate > 0 {
		// An absent error series means no failures were recorded.
		if errs, qErr := c.QueryValue(ctx, errQ); qErr == nil && !math.IsNaN(errs) {
			errRate = math.Min(1, math.Max(0, errs/rate))
		}
	}
	snapshot.RequestRate = rate
	snapshot.P95LatencyMs = p95 * profile.ToMs
	snapshot.ErrorRate = errRate
	snapshot.Availability = 1 - errRate
	return snapshot, nil
}
