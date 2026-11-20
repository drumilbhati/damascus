package graph

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// trace builds: gateway [0,100] -> orders [10,90] -> db [20,80]; orders -> cache [5,15] (parallel, early).
func sampleTrace(id string, dbMs int64) jaegerTrace {
	us := func(ms int64) int64 { return ms * 1000 }
	dbEnd := 20 + dbMs
	ordersEnd := dbEnd + 10
	gatewayEnd := ordersEnd + 10
	return jaegerTrace{
		TraceID: id,
		Processes: map[string]jaegerProcess{
			"p1": {ServiceName: "gateway"}, "p2": {ServiceName: "orders"},
			"p3": {ServiceName: "db"}, "p4": {ServiceName: "cache"},
		},
		Spans: []jaegerSpan{
			{SpanID: "a", ProcessID: "p1", StartTime: 0, Duration: us(gatewayEnd)},
			{SpanID: "b", ProcessID: "p2", StartTime: us(10), Duration: us(ordersEnd - 10), References: []jaegerReference{{RefType: "CHILD_OF", SpanID: "a"}}},
			{SpanID: "c", ProcessID: "p3", StartTime: us(20), Duration: us(dbMs), References: []jaegerReference{{RefType: "CHILD_OF", SpanID: "b"}},
				Tags: []jaegerTag{{Key: "otel.status_code", Value: "ERROR"}}},
			{SpanID: "d", ProcessID: "p4", StartTime: us(12), Duration: us(5), References: []jaegerReference{{RefType: "CHILD_OF", SpanID: "b"}}},
		},
	}
}

func TestProfileTrace_CriticalPathAndSelfTime(t *testing.T) {
	p := ProfileTrace(sampleTrace("t1", 60))
	if p.EntryService != "gateway" || p.DurationUs != 100_000 {
		t.Fatalf("entry/duration wrong: %+v", p)
	}
	// Walking back from 100ms: gateway [90,100], orders [80,90], db [20,80], orders gap [17,20],
	// cache [12,17], orders [10,12], gateway [0,10].
	want := map[string]int64{"gateway": 20_000, "orders": 15_000, "db": 60_000, "cache": 5_000}
	for svc, us := range want {
		if p.CriticalUs[svc] != us {
			t.Errorf("critical[%s] = %d, want %d (all=%v)", svc, p.CriticalUs[svc], us, p.CriticalUs)
		}
	}
	var total int64
	for _, us := range p.CriticalUs {
		total += us
	}
	if total != p.DurationUs {
		t.Errorf("critical path must cover the trace exactly: %d vs %d", total, p.DurationUs)
	}
	if p.SelfUs["orders"] != 80_000-60_000-5_000 || p.ErrorSpans["db"] != 1 {
		t.Errorf("self/error attribution wrong: self=%v errors=%v", p.SelfUs, p.ErrorSpans)
	}
	if len(p.Edges) != 3 {
		t.Errorf("expected 3 cross-service edges, got %v", p.Edges)
	}
}

func TestAnalyzeBottlenecks_RanksServiceThatInflatesUnderLoad(t *testing.T) {
	var samples []TraceSample
	for i := range 10 {
		samples = append(samples, TraceSample{Phase: "baseline", Request: "checkout", Trace: ProfileTrace(sampleTrace("b"+string(rune('a'+i)), 10))})
		samples = append(samples, TraceSample{Phase: "load", Request: "checkout", Trace: ProfileTrace(sampleTrace("l"+string(rune('a'+i)), 200))})
	}
	samples = append(samples, TraceSample{Phase: "load", Request: "empty", Trace: nil})
	report := AnalyzeBottlenecks(samples, []ServiceScore{{ServiceName: "gateway", Score: 1}})
	if report.Bottleneck != "db" {
		t.Fatalf("bottleneck = %q, want db; rows=%+v", report.Bottleneck, report.Services)
	}
	top := report.Services[0]
	if top.Inflation < 10 || top.ErrorRate != 1 || top.CriticalShare < 0.8 {
		t.Errorf("unexpected db metrics: %+v", top)
	}
	if report.EntryServices["checkout"] != "gateway" || len(report.Reachability["checkout"]) != 4 {
		t.Errorf("reachability wrong: %+v %+v", report.EntryServices, report.Reachability)
	}
	if report.BaselineTraces != 10 || report.LoadTraces != 10 || len(report.Graph.Edges) != 3 {
		t.Errorf("counts wrong: %d %d %d", report.BaselineTraces, report.LoadTraces, len(report.Graph.Edges))
	}
}

func TestFetchTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/traces/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/api/traces/t1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(jaegerResponse{Data: []jaegerTrace{sampleTrace("t1", 30)}})
	}))
	defer server.Close()
	a := NewAnalyzer(server.URL, server.Client())
	p, err := a.FetchTrace(context.Background(), "t1")
	if err != nil || p.CriticalUs["db"] != 30_000 {
		t.Fatalf("fetch: %+v %v", p, err)
	}
	if _, err := a.FetchTrace(context.Background(), "missing"); !errors.Is(err, ErrTraceNotFound) {
		t.Fatalf("expected ErrTraceNotFound, got %v", err)
	}
}
