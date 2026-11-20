// Package telemetry holds the two telemetry APIs Damascus depends on, Jaeger
// (traces) and Prometheus (metrics), and lets users change them at runtime.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Sources are the two telemetry API base URLs.
type Sources struct {
	JaegerURL     string `json:"jaeger_url"`
	PrometheusURL string `json:"prometheus_url"`
}

// Settings is the current configuration plus where it came from.
type Settings struct {
	Sources
	Origin    string     `json:"origin"` // "env" until changed from the UI/API, then "ui"
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// FieldError reports per-source problems, keyed by "jaeger_url" / "prometheus_url".
type FieldError struct {
	Fields map[string]string `json:"fields"`
}

func (e *FieldError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, k := range []string{"jaeger_url", "prometheus_url"} {
		if msg, ok := e.Fields[k]; ok {
			parts = append(parts, k+": "+msg)
		}
	}
	return strings.Join(parts, "; ")
}

// ErrBusy means a change would pull telemetry out from under a running test.
var ErrBusy = errors.New("telemetry sources cannot change while an experiment or auto-run is in progress")

// JaegerTarget and PrometheusTarget are the components that query each source.
type JaegerTarget interface{ SetJaegerURL(string) }
type PrometheusTarget interface{ SetAddress(string) error }

// Prober checks that each URL serves the expected API, returning per-field errors.
type Prober func(ctx context.Context, s Sources) map[string]string

// Store owns the live telemetry sources.
type Store struct {
	mu       sync.RWMutex
	current  Settings
	jaeger   JaegerTarget
	prom     PrometheusTarget
	probe    Prober
	busy     func() bool
	updateMu sync.Mutex
}

// NewStore starts from the .env values; busy may be nil.
func NewStore(initial Sources, jaeger JaegerTarget, prom PrometheusTarget, probe Prober, busy func() bool) *Store {
	if probe == nil {
		probe = NewHTTPProber(&http.Client{Timeout: 5 * time.Second})
	}
	if busy == nil {
		busy = func() bool { return false }
	}
	return &Store{current: Settings{Sources: initial, Origin: "env"}, jaeger: jaeger, prom: prom, probe: probe, busy: busy}
}

// Get returns the sources currently in use.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update validates, probes and then applies new sources atomically.
func (s *Store) Update(ctx context.Context, next Sources) (Settings, error) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	next = Sources{JaegerURL: NormalizeJaegerURL(next.JaegerURL), PrometheusURL: NormalizePrometheusURL(next.PrometheusURL)}
	fields := map[string]string{}
	for key, raw := range map[string]string{"jaeger_url": next.JaegerURL, "prometheus_url": next.PrometheusURL} {
		if msg := validateURL(raw); msg != "" {
			fields[key] = msg
		}
	}
	if len(fields) > 0 {
		return s.Get(), &FieldError{Fields: fields}
	}
	if s.busy() {
		return s.Get(), ErrBusy
	}
	if fields := s.probe(ctx, next); len(fields) > 0 {
		return s.Get(), &FieldError{Fields: fields}
	}
	if s.prom != nil {
		if err := s.prom.SetAddress(next.PrometheusURL); err != nil {
			return s.Get(), &FieldError{Fields: map[string]string{"prometheus_url": err.Error()}}
		}
	}
	if s.jaeger != nil {
		s.jaeger.SetJaegerURL(next.JaegerURL)
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.current = Settings{Sources: next, Origin: "ui", UpdatedAt: &now}
	s.mu.Unlock()
	return s.Get(), nil
}

func validateURL(raw string) string {
	if raw == "" {
		return "is required"
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "must be an absolute http(s) URL, e.g. http://jaeger:16686"
	}
	return ""
}

// NormalizeJaegerURL strips UI and API paths users commonly paste.
func NormalizeJaegerURL(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/api/services", "/api/traces", "/api", "/search"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// NormalizePrometheusURL strips UI and API paths users commonly paste.
func NormalizePrometheusURL(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/api/v1/query", "/api/v1", "/graph", "/query"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// NewHTTPProber probes Jaeger's /api/services and Prometheus' /api/v1/query.
func NewHTTPProber(client *http.Client) Prober {
	return func(ctx context.Context, s Sources) map[string]string {
		fields := map[string]string{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		set := func(k, v string) {
			mu.Lock()
			fields[k] = v
			mu.Unlock()
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := probeJaeger(ctx, client, s.JaegerURL); err != nil {
				set("jaeger_url", err.Error())
			}
		}()
		go func() {
			defer wg.Done()
			if err := probePrometheus(ctx, client, s.PrometheusURL); err != nil {
				set("prometheus_url", err.Error())
			}
		}()
		wg.Wait()
		return fields
	}
}

func get(ctx context.Context, client *http.Client, target string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("cannot reach %s from the control plane: %w", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
}

func probeJaeger(ctx context.Context, client *http.Client, base string) error {
	status, body, err := get(ctx, client, base+"/api/services")
	if err != nil {
		return err
	}
	var parsed struct {
		Data []string `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &parsed) != nil {
		return fmt.Errorf("%s/api/services did not return a Jaeger services list (HTTP %d); use the Jaeger query URL, e.g. http://jaeger:16686", base, status)
	}
	return nil
}

func probePrometheus(ctx context.Context, client *http.Client, base string) error {
	status, body, err := get(ctx, client, base+"/api/v1/query?query=up")
	if err != nil {
		return err
	}
	var parsed struct {
		Status string `json:"status"`
	}
	if status == http.StatusOK && json.Unmarshal(body, &parsed) == nil && parsed.Status == "success" {
		return nil
	}
	// A raw exposition endpoint (/metrics) has no query API; say so explicitly.
	if s, raw, rawErr := get(ctx, client, base); rawErr == nil && s == http.StatusOK && looksLikeExposition(raw) {
		return fmt.Errorf("%s is a raw /metrics endpoint; enter the URL of the Prometheus server that scrapes it (e.g. http://prometheus:9090)", base)
	}
	return fmt.Errorf("%s/api/v1/query did not answer like Prometheus (HTTP %d); use the Prometheus server URL, e.g. http://prometheus:9090", base, status)
}

func looksLikeExposition(body []byte) bool {
	text := string(body)
	return strings.Contains(text, "# HELP ") || strings.Contains(text, "# TYPE ")
}
