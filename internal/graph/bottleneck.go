package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ErrTraceNotFound means Jaeger has not (yet) indexed the requested trace.
var ErrTraceNotFound = errors.New("trace not found in jaeger")

// SpanProfile is a span reduced to what latency attribution needs.
type SpanProfile struct {
	SpanID    string `json:"span_id"`
	ParentID  string `json:"parent_id,omitempty"`
	Service   string `json:"service"`
	Operation string `json:"operation"`
	StartUs   int64  `json:"start_us"`
	EndUs     int64  `json:"end_us"`
	Error     bool   `json:"error"`
}

// TraceProfile is one distributed trace with per-service latency attribution.
type TraceProfile struct {
	TraceID      string           `json:"trace_id"`
	EntryService string           `json:"entry_service"`
	DurationUs   int64            `json:"duration_us"`
	CriticalUs   map[string]int64 `json:"critical_us"` // time each service spends on the critical path
	SelfUs       map[string]int64 `json:"self_us"`     // exclusive time (not covered by child spans)
	Spans        map[string]int   `json:"spans"`
	ErrorSpans   map[string]int   `json:"error_spans"`
	Edges        [][2]string      `json:"edges"`
}

// FetchTrace retrieves one trace by ID from Jaeger's query API.
func (a *Analyzer) FetchTrace(ctx context.Context, traceID string) (*TraceProfile, error) {
	traceURL := fmt.Sprintf("%s/api/traces/%s", a.JaegerURL(), url.PathEscape(traceID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrTraceNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected status code from jaeger /api/traces/{id}: %d", resp.StatusCode)
	}
	var body jaegerResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Data) == 0 || len(body.Data[0].Spans) == 0 {
		return nil, ErrTraceNotFound
	}
	return ProfileTrace(body.Data[0]), nil
}

func spanHasError(span jaegerSpan) bool {
	for _, tag := range span.Tags {
		switch tag.Key {
		case "error":
			if v, ok := tag.Value.(bool); ok && v {
				return true
			}
			if v, ok := tag.Value.(string); ok && strings.EqualFold(v, "true") {
				return true
			}
		case "otel.status_code":
			if v, ok := tag.Value.(string); ok && strings.EqualFold(v, "ERROR") {
				return true
			}
		}
	}
	return false
}

// ProfileTrace converts a Jaeger trace into a latency-attributed profile.
func ProfileTrace(trace jaegerTrace) *TraceProfile {
	spans := make(map[string]*SpanProfile, len(trace.Spans))
	for _, s := range trace.Spans {
		p := &SpanProfile{
			SpanID:    s.SpanID,
			Service:   trace.Processes[s.ProcessID].ServiceName,
			Operation: s.OperationName,
			StartUs:   s.StartTime,
			EndUs:     s.StartTime + s.Duration,
			Error:     spanHasError(s),
		}
		for _, ref := range s.References {
			if ref.RefType == "CHILD_OF" && (ref.TraceID == "" || ref.TraceID == trace.TraceID) {
				p.ParentID = ref.SpanID
				break
			}
		}
		if p.Service == "" {
			p.Service = "unknown"
		}
		spans[s.SpanID] = p
	}
	return buildProfile(trace.TraceID, spans)
}

// ProfileSpans builds a profile from already-normalised spans (useful for non-Jaeger sources and tests).
func ProfileSpans(traceID string, spans []SpanProfile) *TraceProfile {
	byID := make(map[string]*SpanProfile, len(spans))
	for i := range spans {
		s := spans[i]
		byID[s.SpanID] = &s
	}
	return buildProfile(traceID, byID)
}

func buildProfile(traceID string, spans map[string]*SpanProfile) *TraceProfile {
	children := make(map[string][]*SpanProfile)
	var roots []*SpanProfile
	for _, s := range spans {
		if _, ok := spans[s.ParentID]; s.ParentID != "" && ok {
			children[s.ParentID] = append(children[s.ParentID], s)
		} else {
			roots = append(roots, s)
		}
	}
	profile := &TraceProfile{
		TraceID:    traceID,
		CriticalUs: map[string]int64{},
		SelfUs:     map[string]int64{},
		Spans:      map[string]int{},
		ErrorSpans: map[string]int{},
	}
	if len(roots) == 0 {
		return profile
	}
	// The earliest root is the entry point; orphaned spans are attributed as extra roots.
	sort.Slice(roots, func(i, j int) bool { return roots[i].StartUs < roots[j].StartUs })
	root := roots[0]
	profile.EntryService = root.Service
	profile.DurationUs = root.EndUs - root.StartUs
	seenEdge := map[[2]string]bool{}
	for _, s := range spans {
		profile.Spans[s.Service]++
		if s.Error {
			profile.ErrorSpans[s.Service]++
		}
		profile.SelfUs[s.Service] += selfTime(s, children[s.SpanID])
		if parent, ok := spans[s.ParentID]; ok && parent.Service != s.Service {
			edge := [2]string{parent.Service, s.Service}
			if !seenEdge[edge] {
				seenEdge[edge] = true
				profile.Edges = append(profile.Edges, edge)
			}
		}
	}
	criticalPath(root, root.StartUs, root.EndUs, children, profile.CriticalUs, 0)
	return profile
}

// selfTime is the span duration minus the union of its children's intervals.
func selfTime(span *SpanProfile, kids []*SpanProfile) int64 {
	type interval struct{ start, end int64 }
	ivs := make([]interval, 0, len(kids))
	for _, k := range kids {
		start, end := max(k.StartUs, span.StartUs), min(k.EndUs, span.EndUs)
		if end > start {
			ivs = append(ivs, interval{start, end})
		}
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].start < ivs[j].start })
	covered, curStart, curEnd := int64(0), int64(-1), int64(-1)
	for _, iv := range ivs {
		if iv.start > curEnd {
			if curEnd > curStart {
				covered += curEnd - curStart
			}
			curStart, curEnd = iv.start, iv.end
		} else if iv.end > curEnd {
			curEnd = iv.end
		}
	}
	if curEnd > curStart {
		covered += curEnd - curStart
	}
	return max(0, (span.EndUs-span.StartUs)-covered)
}

// criticalPath walks backwards from the span end, descending into the child
// that finished last before the cursor (Jaeger's critical-path algorithm).
// Time not covered by such a child is charged to the span's own service.
func criticalPath(span *SpanProfile, from, until int64, children map[string][]*SpanProfile, out map[string]int64, depth int) {
	if depth > 512 || until <= from {
		return
	}
	kids := append([]*SpanProfile(nil), children[span.SpanID]...)
	sort.Slice(kids, func(i, j int) bool { return kids[i].EndUs > kids[j].EndUs })
	cursor := until
	for _, kid := range kids {
		if cursor <= from {
			break
		}
		kidEnd := min(kid.EndUs, cursor)
		kidStart := max(kid.StartUs, from)
		if kidEnd <= kidStart || kidStart >= cursor {
			continue
		}
		out[span.Service] += cursor - kidEnd
		criticalPath(kid, kidStart, kidEnd, children, out, depth+1)
		cursor = kidStart
	}
	if cursor > from {
		out[span.Service] += cursor - from
	}
}

// TraceSample is a fetched trace tagged with the load phase that produced it.
type TraceSample struct {
	Phase   string        `json:"phase"` // "baseline" or "load"
	Rate    int           `json:"rate"`
	Request string        `json:"request"`
	Trace   *TraceProfile `json:"trace"`
}

// ServiceBottleneck summarises one service's latency behaviour under load.
type ServiceBottleneck struct {
	Service            string   `json:"service"`
	BaselineCriticalMs float64  `json:"baseline_critical_ms"` // P95 of critical-path time per trace
	LoadCriticalMs     float64  `json:"load_critical_ms"`
	Inflation          float64  `json:"inflation"`      // load / baseline
	CriticalShare      float64  `json:"critical_share"` // mean share of trace duration under load
	SelfLoadMs         float64  `json:"self_load_ms"`   // P95 exclusive time under load
	ErrorRate          float64  `json:"error_rate"`     // error spans / spans under load
	Structural         float64  `json:"structural"`     // graph criticality score, 0..1
	Score              float64  `json:"score"`
	Requests           []string `json:"requests"` // developer requests whose traces reach the service
	Reasons            []string `json:"reasons"`
}

// BottleneckReport ranks services by how much latency they add under load.
type BottleneckReport struct {
	Bottleneck     string              `json:"bottleneck"`
	Services       []ServiceBottleneck `json:"services"`
	BaselineTraces int                 `json:"baseline_traces"`
	LoadTraces     int                 `json:"load_traces"`
	EntryServices  map[string]string   `json:"entry_services"` // request name -> entry service.name
	Reachability   map[string][]string `json:"reachability"`   // request name -> services its traces touch
	Graph          *DependencyGraph    `json:"graph"`
}

// Score weights: critical-path share dominates; growth under load and errors refine it;
// structural criticality only breaks ties between otherwise similar services.
const (
	weightShare      = 0.45
	weightGrowth     = 0.30
	weightErrors     = 0.20
	weightStructural = 0.05
)

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(idx, 0), len(sorted)-1)]
}

// AnalyzeBottlenecks compares baseline and loaded traces to find the service
// whose critical-path time grows the most. structural may be nil.
func AnalyzeBottlenecks(samples []TraceSample, structural []ServiceScore) BottleneckReport {
	report := BottleneckReport{
		EntryServices: map[string]string{},
		Reachability:  map[string][]string{},
		Graph:         NewDependencyGraph(),
	}
	type acc struct {
		baseCritical, loadCritical, loadSelf, loadShare []float64
		spans, errors                                   int
		requests                                        map[string]bool
	}
	accs := map[string]*acc{}
	get := func(svc string) *acc {
		if a, ok := accs[svc]; ok {
			return a
		}
		a := &acc{requests: map[string]bool{}}
		accs[svc] = a
		return a
	}
	reach := map[string]map[string]bool{}
	entryVotes := map[string]map[string]int{}
	edgeIdx := map[[2]string]int{}

	for _, sample := range samples {
		t := sample.Trace
		if t == nil || t.DurationUs <= 0 {
			continue
		}
		if reach[sample.Request] == nil {
			reach[sample.Request] = map[string]bool{}
			entryVotes[sample.Request] = map[string]int{}
		}
		entryVotes[sample.Request][t.EntryService]++
		for _, e := range t.Edges {
			if i, ok := edgeIdx[e]; ok {
				report.Graph.Edges[i].CallCount++
			} else {
				edgeIdx[e] = len(report.Graph.Edges)
				report.Graph.AddEdge(DependencyEdge{From: e[0], To: e[1], CallCount: 1})
			}
		}
		for svc := range t.Spans {
			report.Graph.AddNode(svc, nil)
			reach[sample.Request][svc] = true
			get(svc).requests[sample.Request] = true
		}
		switch sample.Phase {
		case "baseline":
			report.BaselineTraces++
			for svc := range t.Spans {
				get(svc).baseCritical = append(get(svc).baseCritical, float64(t.CriticalUs[svc])/1000)
			}
		default:
			report.LoadTraces++
			for svc, n := range t.Spans {
				a := get(svc)
				a.loadCritical = append(a.loadCritical, float64(t.CriticalUs[svc])/1000)
				a.loadSelf = append(a.loadSelf, float64(t.SelfUs[svc])/1000)
				a.loadShare = append(a.loadShare, float64(t.CriticalUs[svc])/float64(t.DurationUs))
				a.spans += n
				a.errors += t.ErrorSpans[svc]
			}
		}
	}
	for req, svcs := range reach {
		list := make([]string, 0, len(svcs))
		for s := range svcs {
			list = append(list, s)
		}
		sort.Strings(list)
		report.Reachability[req] = list
		best, bestN := "", -1
		for svc, n := range entryVotes[req] {
			if n > bestN || (n == bestN && svc < best) {
				best, bestN = svc, n
			}
		}
		report.EntryServices[req] = best
	}

	structuralByName := map[string]float64{}
	for _, s := range structural {
		structuralByName[s.ServiceName] = s.Score
	}
	maxGrowth := 0.0
	rows := make([]ServiceBottleneck, 0, len(accs))
	for svc, a := range accs {
		row := ServiceBottleneck{
			Service:            svc,
			BaselineCriticalMs: percentile(a.baseCritical, 0.95),
			LoadCriticalMs:     percentile(a.loadCritical, 0.95),
			SelfLoadMs:         percentile(a.loadSelf, 0.95),
			Structural:         structuralByName[svc],
		}
		if len(a.loadCritical) == 0 {
			// Only baseline data: rank by baseline behaviour instead of discarding the service.
			row.LoadCriticalMs = row.BaselineCriticalMs
		}
		for _, s := range a.loadShare {
			row.CriticalShare += s
		}
		if len(a.loadShare) > 0 {
			row.CriticalShare /= float64(len(a.loadShare))
		}
		if a.spans > 0 {
			row.ErrorRate = float64(a.errors) / float64(a.spans)
		}
		if row.BaselineCriticalMs > 0 {
			row.Inflation = row.LoadCriticalMs / row.BaselineCriticalMs
		}
		maxGrowth = math.Max(maxGrowth, row.LoadCriticalMs-row.BaselineCriticalMs)
		for r := range a.requests {
			row.Requests = append(row.Requests, r)
		}
		sort.Strings(row.Requests)
		rows = append(rows, row)
	}
	for i := range rows {
		r := &rows[i]
		growth := 0.0
		if maxGrowth > 0 {
			growth = math.Max(0, r.LoadCriticalMs-r.BaselineCriticalMs) / maxGrowth
		}
		r.Score = weightShare*r.CriticalShare + weightGrowth*growth + weightErrors*r.ErrorRate + weightStructural*r.Structural
		r.Reasons = append(r.Reasons,
			fmt.Sprintf("%.0f%% of end-to-end latency is spent on its critical path under load", r.CriticalShare*100),
			fmt.Sprintf("P95 critical-path time %.1f ms at baseline -> %.1f ms under load", r.BaselineCriticalMs, r.LoadCriticalMs))
		if r.Inflation > 1.5 {
			r.Reasons = append(r.Reasons, fmt.Sprintf("latency contribution inflates %.1fx under load", r.Inflation))
		}
		if r.ErrorRate > 0 {
			r.Reasons = append(r.Reasons, fmt.Sprintf("%.1f%% of its spans report errors under load", r.ErrorRate*100))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Service < rows[j].Service
	})
	report.Services = rows
	if len(rows) > 0 {
		report.Bottleneck = rows[0].Service
	}
	return report
}
