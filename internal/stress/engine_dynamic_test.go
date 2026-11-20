package stress_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestEngine_WeightedRequestMixAndTemplates(t *testing.T) {
	var mu sync.Mutex
	paths := map[string]int{}
	bodies := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths[r.Method+" "+r.URL.Path]++
		if len(body) > 0 {
			bodies[string(body)] = true
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("POST without JSON content type")
			}
			if r.Header.Get("X-Api-Key") != "k" {
				t.Errorf("custom header missing")
			}
		}
		mu.Unlock()
	}))
	defer server.Close()

	plan := stress.LoadPlan{
		InitialRate: 200, MaxRate: 200, StepDurationSeconds: 1,
		Requests: []stress.RequestSpec{
			{Name: "read", Method: "GET", URL: server.URL + "/items", Weight: 3},
			{Name: "write", Method: "POST", URL: server.URL + "/orders", Weight: 1,
				Headers: map[string]string{"X-Api-Key": "k"}, Body: `{"id":"{{uuid}}","n":{{seq}}}`},
		},
	}
	if err := stress.NewEngine(server.Client()).Start(context.Background(), plan); err != nil {
		t.Fatalf("start: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	reads, writes := paths["GET /items"], paths["POST /orders"]
	if writes == 0 || reads < 2*writes {
		t.Fatalf("weights not respected: reads=%d writes=%d", reads, writes)
	}
	if len(bodies) != writes {
		t.Fatalf("templated bodies should be unique: %d unique of %d", len(bodies), writes)
	}
}

func TestEngine_TraceparentTagging(t *testing.T) {
	var tagged atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tp := r.Header.Get("traceparent"); tp != "" {
			parts := strings.Split(tp, "-")
			if len(parts) != 4 || len(parts[1]) != 32 || parts[3] != "01" {
				t.Errorf("malformed traceparent %q", tp)
			}
			tagged.Add(1)
		}
	}))
	defer server.Close()

	var mu sync.Mutex
	var records []stress.TraceRecord
	plan := stress.LoadPlan{
		TargetURL: server.URL, Method: "GET", InitialRate: 100, MaxRate: 100, StepDurationSeconds: 1,
		TraceEvery: 10,
		OnTrace: func(r stress.TraceRecord) {
			mu.Lock()
			records = append(records, r)
			mu.Unlock()
		},
	}
	if err := stress.NewEngine(server.Client()).Start(context.Background(), plan); err != nil {
		t.Fatalf("start: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(records) == 0 || int64(len(records)) != tagged.Load() {
		t.Fatalf("records=%d tagged=%d", len(records), tagged.Load())
	}
	if records[0].Request != "default" || records[0].Rate != 100 || records[0].StatusCode != 200 {
		t.Fatalf("unexpected record %+v", records[0])
	}
}

func TestEngine_WorkerPoolGrowsForSlowTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client(), stress.WithMaxInFlight(200))
	plan := stress.LoadPlan{TargetURL: server.URL, Method: "GET", InitialRate: 200, MaxRate: 200, StepDurationSeconds: 2}
	if err := engine.Start(context.Background(), plan); err != nil {
		t.Fatalf("start: %v", err)
	}
	stats := engine.Stats()
	// Little's law: 200 rps × 0.1 s ≈ 20 concurrent requests; the pool must exceed its floor of 4.
	if stats.PeakWorkers < 15 {
		t.Fatalf("pool did not scale up: %+v", stats)
	}
	if stats.Workers != 0 {
		t.Fatalf("workers should exit after the run: %+v", stats)
	}
}

func TestEngine_SaturationReturnsInFlightLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client(), stress.WithMaxInFlight(5))
	plan := stress.LoadPlan{TargetURL: server.URL, Method: "GET", InitialRate: 200, MaxRate: 200, StepDurationSeconds: 3}
	err := engine.Start(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), stress.ErrInFlightLimit.Error()) {
		t.Fatalf("expected in-flight limit error, got %v", err)
	}
}

func TestRequestSpec_Validate(t *testing.T) {
	bad := []stress.RequestSpec{
		{Name: "m", Method: "DELETE", URL: "http://x/"},
		{Name: "u", Method: "GET", URL: "/relative"},
		{Name: "b", Method: "GET", URL: "http://x/", Body: "{}"},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("%s: expected validation error", s.Name)
		}
	}
	if err := (stress.RequestSpec{Name: "ok", Method: "post", URL: "http://x/{{uuid}}"}).Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
}
