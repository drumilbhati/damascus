package autorun

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/stress"
)

// fakeLoad emits one TraceRecord per expected tagged request without real HTTP.
type fakeLoad struct {
	mu    sync.Mutex
	plans []stress.LoadPlan
}

func (f *fakeLoad) Start(_ context.Context, plan stress.LoadPlan) error {
	f.mu.Lock()
	f.plans = append(f.plans, plan)
	f.mu.Unlock()
	total := plan.InitialRate * plan.StepDurationSeconds
	for i := 1; i <= total; i++ {
		if i%plan.TraceEvery != 0 {
			continue
		}
		spec := plan.Requests[(i-1)%len(plan.Requests)]
		plan.OnTrace(stress.TraceRecord{TraceID: spec.Name + "|" + strconv.Itoa(plan.InitialRate) + "|" + strconv.Itoa(i), Request: spec.Name, Rate: plan.InitialRate})
	}
	return nil
}

// fakeTraces: "orders" requests go gateway -> orders -> db; "catalog" goes gateway -> catalog.
// db inflates heavily at the probe rate.
type fakeTraces struct{}

func span(id, parent, svc string, start, end int64) graph.SpanProfile {
	return graph.SpanProfile{SpanID: id, ParentID: parent, Service: svc, StartUs: start, EndUs: end}
}

func (f *fakeTraces) FetchTrace(_ context.Context, id string) (*graph.TraceProfile, error) {
	parts := strings.Split(id, "|")
	loaded := parts[1] != "2" // baseline rate is 2 RPS in the test config
	var p *graph.TraceProfile
	if parts[0] == "orders" {
		db := int64(5_000)
		if loaded {
			db = 150_000
		}
		p = graph.ProfileSpans(id, []graph.SpanProfile{
			span("a", "", "gateway", 0, db+20_000),
			span("b", "a", "orders", 5_000, db+15_000),
			span("c", "b", "db", 10_000, db+10_000),
		})
	} else {
		p = graph.ProfileSpans(id, []graph.SpanProfile{
			span("a", "", "gateway", 0, 20_000),
			span("b", "a", "catalog", 5_000, 15_000),
		})
	}
	return p, nil
}

func (f *fakeTraces) BuildGraph(context.Context, int64) (*graph.DependencyGraph, error) {
	return nil, errors.New("jaeger graph not needed")
}
func (f *fakeTraces) ScoreCriticality(*graph.DependencyGraph) []graph.ServiceScore { return nil }

type fakeManager struct {
	mu      sync.Mutex
	created []string
	plans   []stress.LoadPlan
	exps    map[string]*experiment.Experiment
}

func (m *fakeManager) CreateExperiment(_ context.Context, target, url string, typ experiment.ExperimentType, cfg experiment.ExperimentConfig) (*experiment.Experiment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "exp-" + target + "-" + strconv.Itoa(len(m.created))
	m.created = append(m.created, target+" "+url)
	e := &experiment.Experiment{ID: id, TargetService: target, TargetURL: url, Type: typ, Config: cfg, State: experiment.StateCreated}
	m.exps[id] = e
	return e, nil
}

func (m *fakeManager) StartExperiment(_ context.Context, id string, plan stress.LoadPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans = append(m.plans, plan)
	m.exps[id].State = experiment.StateCompleted
	return nil
}

func (m *fakeManager) GetStatus(_ context.Context, id string) (*experiment.Experiment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *m.exps[id]
	return &c, nil
}

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.BaselineRate, cfg.BaselineSeconds = 2, 10
	cfg.ProbeRate, cfg.ProbeSeconds = 20, 5
	cfg.TracesPerPhase = 20
	cfg.TraceSettle = 0
	cfg.PollInterval = 5 * time.Millisecond
	cfg.Requests = []stress.RequestSpec{
		{Name: "orders", Method: "GET", URL: "http://svc/orders", Headers: map[string]string{"Authorization": "secret"}},
		{Name: "catalog", Method: "GET", URL: "http://svc/catalog"},
		{Name: "pay", Method: "POST", URL: "http://svc/pay", Body: "{}"},
	}
	return cfg
}

func waitDone(t *testing.T, o *Orchestrator) Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if run, ok := o.Get("latest"); ok && run.EndedAt != nil {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("auto-run did not finish")
	return Run{}
}

func TestOrchestrator_FindsBottleneckAndTestsReachingRequests(t *testing.T) {
	load := &fakeLoad{}
	mgr := &fakeManager{exps: map[string]*experiment.Experiment{}}
	o := NewOrchestrator(testConfig(), load, &fakeTraces{}, mgr, nil)
	if _, err := o.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Start(context.Background(), 0); !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("second start should be rejected, got %v", err)
	}
	run := waitDone(t, o)
	if run.State != RunCompleted {
		t.Fatalf("run failed: %+v", run)
	}
	if run.Report == nil || run.Report.Bottleneck != "db" {
		t.Fatalf("expected db bottleneck, got %+v", run.Report)
	}
	if len(run.Skipped) != 1 || !strings.Contains(run.Skipped[0], "pay") {
		t.Fatalf("POST without allow_mutation must be skipped: %v", run.Skipped)
	}
	for _, p := range load.plans {
		for _, r := range p.Requests {
			if r.Name == "pay" {
				t.Fatal("mutating request was replayed")
			}
		}
	}
	// Only "orders" reaches db, so exactly one experiment targets db with that request.
	if len(mgr.created) != 1 || mgr.created[0] != "db http://svc/orders" {
		t.Fatalf("unexpected experiments: %v", mgr.created)
	}
	if len(mgr.plans[0].Requests) != 1 || mgr.plans[0].Requests[0].Name != "orders" || mgr.plans[0].MaxRate != 100 {
		t.Fatalf("unexpected experiment plan: %+v", mgr.plans[0])
	}
	if len(run.Experiments) != 1 || run.Experiments[0].State != experiment.StateCompleted {
		t.Fatalf("experiment status not tracked: %+v", run.Experiments)
	}
	if run.Requests[0].Headers["Authorization"] != "[redacted]" {
		t.Fatalf("headers must be redacted in status: %+v", run.Requests[0])
	}
}

func TestOrchestrator_TargetsDirectServiceURLWhenConfigured(t *testing.T) {
	cfg := testConfig()
	cfg.ServiceTargetURLs = map[string]string{
		"db": "http://direct-db-gateway:8090/v1/db/status",
	}
	mgr := &fakeManager{exps: map[string]*experiment.Experiment{}}
	o := NewOrchestrator(cfg, &fakeLoad{}, &fakeTraces{}, mgr, nil)
	if _, err := o.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	run := waitDone(t, o)
	if run.State != RunCompleted {
		t.Fatalf("run failed: %+v", run)
	}
	if len(mgr.created) != 1 || mgr.created[0] != "db http://direct-db-gateway:8090/v1/db/status" {
		t.Fatalf("expected experiment targeting direct service URL, got %v", mgr.created)
	}
}

func TestOrchestrator_ScopeAllTestsEveryRequest(t *testing.T) {
	cfg := testConfig()
	cfg.Scope = ScopeAll
	mgr := &fakeManager{exps: map[string]*experiment.Experiment{}}
	o := NewOrchestrator(cfg, &fakeLoad{}, &fakeTraces{}, mgr, nil)
	if _, err := o.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if run := waitDone(t, o); run.State != RunCompleted {
		t.Fatalf("run failed: %+v", run)
	}
	got := strings.Join(mgr.created, ",")
	if !strings.Contains(got, "db http://svc/orders") || !strings.Contains(got, "gateway http://svc/catalog") {
		t.Fatalf("scope=all should test both requests (catalog against its entry service): %v", mgr.created)
	}
}

func TestOrchestrator_FailsWhenNoTracesReachJaeger(t *testing.T) {
	cfg := testConfig()
	o := NewOrchestrator(cfg, &fakeLoad{}, &allMissing{}, &fakeManager{exps: map[string]*experiment.Experiment{}}, nil)
	if _, err := o.Start(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	run := waitDone(t, o)
	if run.State != RunFailed || !strings.Contains(run.Error, "traceparent") {
		t.Fatalf("expected trace propagation failure, got %+v", run)
	}
}

type allMissing struct{ fakeTraces }

func (allMissing) FetchTrace(context.Context, string) (*graph.TraceProfile, error) {
	return nil, errors.New("jaeger unavailable")
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{"AUTO_RUN_MAX_RPS": "50", "AUTO_RUN_TEST_SCOPE": "all", "AUTO_RUN_MAX_P95_MS": "250"}
	cfg, err := ConfigFromEnv(func(k string) string { return env[k] }, nil)
	if err != nil || cfg.Experiment.MaxRate != 50 || cfg.Scope != ScopeAll || cfg.Experiment.MaxP95LatencyMs != 250 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	env = map[string]string{"AUTO_RUN_MAX_RPS": "abc", "AUTO_RUN_TEST_SCOPE": "x"}
	if _, err := ConfigFromEnv(func(k string) string { return env[k] }, nil); err == nil {
		t.Fatal("expected validation errors")
	}
	if _, err := NewOrchestrator(Config{}, nil, nil, nil, nil).Start(context.Background(), 0); !errors.Is(err, ErrNoRequests) {
		t.Fatalf("expected ErrNoRequests, got %v", err)
	}
}
