package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ---------------------------------------------------------------------------
// Target Order Service Mock
// ---------------------------------------------------------------------------

// MockOrderService simulates the target Order Service under stress and chaos.
// It supports dynamic latency and error rate injection at runtime.
type MockOrderService struct {
	server       *httptest.Server
	latencyNs    atomic.Int64
	statusCode   atomic.Int32
	requestCount atomic.Int64
	errorCount   atomic.Int64

	mu        sync.Mutex
	latencies []time.Duration
}

func newMockOrderService() *MockOrderService {
	mos := &MockOrderService{}
	mos.statusCode.Store(http.StatusOK)

	mos.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		mos.requestCount.Add(1)

		// Inject artificial latency if configured
		if lat := mos.latencyNs.Load(); lat > 0 {
			time.Sleep(time.Duration(lat))
		}

		duration := time.Since(start)
		mos.mu.Lock()
		mos.latencies = append(mos.latencies, duration)
		mos.mu.Unlock()

		// Inject error response if configured
		if code := mos.statusCode.Load(); code != http.StatusOK {
			mos.errorCount.Add(1)
			w.WriteHeader(int(code))
			_, _ = w.Write([]byte(`{"error":"order service chaos fault injected"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"order_id":"ord-99214","status":"placed"}`))
	}))

	return mos
}

func (s *MockOrderService) URL() string {
	return s.server.URL
}

func (s *MockOrderService) Close() {
	s.server.Close()
}

func (s *MockOrderService) SetLatency(d time.Duration) {
	s.latencyNs.Store(int64(d))
}

func (s *MockOrderService) SetStatusCode(code int) {
	s.statusCode.Store(int32(code))
}

func (s *MockOrderService) RequestCount() int64 {
	return s.requestCount.Load()
}

func (s *MockOrderService) ErrorCount() int64 {
	return s.errorCount.Load()
}

func (s *MockOrderService) P95LatencySeconds() float64 {
	// If artificial latency is set, reflect it directly for fast, deterministic evaluation
	if lat := s.latencyNs.Load(); lat > 0 {
		return time.Duration(lat).Seconds()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.latencies)
	if n == 0 {
		return 0.005 // default 5ms baseline
	}

	sorted := make([]time.Duration, n)
	copy(sorted, s.latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(float64(n) * 0.95)
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx].Seconds()
}

func (s *MockOrderService) ErrorRate() float64 {
	reqs := s.requestCount.Load()
	errs := s.errorCount.Load()
	if reqs == 0 {
		return 0.0
	}
	return float64(errs) / float64(reqs)
}

func (s *MockOrderService) RequestRate() float64 {
	return float64(s.requestCount.Load())
}

// ---------------------------------------------------------------------------
// Mock Prometheus API connected dynamically to MockOrderService
// ---------------------------------------------------------------------------

type mockOrderPromAPI struct {
	promv1.API
	orderSvc *MockOrderService
}

func newMockOrderPromAPI(svc *MockOrderService) *mockOrderPromAPI {
	return &mockOrderPromAPI{orderSvc: svc}
}

func (m *mockOrderPromAPI) Query(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
	// P95 Latency Query
	if strings.Contains(query, "histogram_quantile(0.95") {
		val := m.orderSvc.P95LatencySeconds()
		return model.Vector{&model.Sample{Value: model.SampleValue(val)}}, nil, nil
	}

	// Error Rate Query (matches status=~"5.." or http_status_code=~"5..")
	if strings.Contains(query, "5..") {
		val := m.orderSvc.ErrorRate()
		return model.Vector{&model.Sample{Value: model.SampleValue(val)}}, nil, nil
	}

	// Request Rate Query
	if strings.Contains(query, "http_requests_total") || strings.Contains(query, "duration_milliseconds_count") {
		val := m.orderSvc.RequestRate()
		return model.Vector{&model.Sample{Value: model.SampleValue(val)}}, nil, nil
	}

	return model.Vector{&model.Sample{Value: 0.0}}, nil, nil
}

// ---------------------------------------------------------------------------
// E2E Chaos Stress Tests
// ---------------------------------------------------------------------------

// TestChaosE2E_OrderService_P95LatencySLABreachStop validates that when an active step load
// experiment targets the Order Service and artificial latency is injected (triggering a P95 SLA breach),
// the SafetyController executes an in-memory fast-path context cancellation that terminates
// all stress traffic in less than 1 second (<1s).
func TestChaosE2E_OrderService_P95LatencySLABreachStop(t *testing.T) {
	orderSvc := newMockOrderService()
	defer orderSvc.Close()

	promAPI := newMockOrderPromAPI(orderSvc)
	promClient := watcher.NewPrometheusClientWithAPI(promAPI)

	// Configure strict SLA Policy: Max P95 Latency 200ms
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 200.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.95,
	}

	expCtx, cancelExp := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelExp()

	stressCtx, cancelStress := context.WithCancel(expCtx)

	var cancelCallCount atomic.Int32

	wrappedCancel := func() {
		cancelCallCount.Add(1)
		cancelStress()
	}

	evaluatingCtrl := safety.NewEvaluatingController(policy, wrappedCancel, nil)

	// Wire WatcherEngine to EvaluatingController
	watcherEngine := watcher.NewWatcherEngine(
		promClient,
		evaluatingCtrl.MakeSnapshotHandler(),
		watcher.WithPollInterval(30*time.Millisecond),
	)

	stressEngine := stress.NewEngine(orderSvc.server.Client())

	plan := stress.LoadPlan{
		TargetURL:           orderSvc.URL() + "/order",
		Method:              "POST",
		Payload:             `{"product_id":"prod-789","quantity":1}`,
		InitialRate:         50,
		StepRate:            10,
		MaxRate:             200,
		StepDurationSeconds: 5,
	}

	// 1. Start Watcher
	if err := watcherEngine.Start(expCtx, "exp-chaos-p95", "order-service"); err != nil {
		t.Fatalf("failed to start watcher engine: %v", err)
	}
	defer watcherEngine.Stop()

	// 2. Start Stress Traffic Generator in background
	stressDoneCh := make(chan error, 1)
	go func() {
		stressDoneCh <- stressEngine.Start(stressCtx, plan)
	}()

	// 3. Healthy Baseline: Wait for baseline requests to land
	time.Sleep(150 * time.Millisecond)
	initialReqs := orderSvc.RequestCount()
	if initialReqs == 0 {
		t.Fatalf("expected initial requests to hit Order Service, got 0")
	}

	// Verify no premature cancellation occurred during healthy period
	if cancelCallCount.Load() != 0 {
		t.Fatalf("unexpected cancellation during healthy baseline: count = %d", cancelCallCount.Load())
	}

	// 4. INJECT CHAOS: Artificial latency spike to 400ms (> 200ms SLA threshold)
	chaosInjectedAt := time.Now()
	orderSvc.SetLatency(400 * time.Millisecond)

	// 5. Await Fast-Path Stop (<1s reaction time)
	select {
	case err := <-stressDoneCh:
		totalElapsed := time.Since(chaosInjectedAt)
		if totalElapsed >= 1*time.Second {
			t.Fatalf("SafetyController took >=1s to stop stress test: took %v", totalElapsed)
		}
		t.Logf("✓ SafetyController stopped stress engine in %v (< 1s threshold)", totalElapsed)

		if err != nil && err != context.Canceled {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}

	case <-time.After(1 * time.Second):
		t.Fatalf("SLA breach stop failed: stress engine did not stop within 1 second of chaos injection")
	}

	// Verify cancel was invoked exactly once
	if count := cancelCallCount.Load(); count != 1 {
		t.Errorf("cancel function called %d times; want 1", count)
	}

	// 6. Verify traffic generation completely ceased
	reqCountAtStop := orderSvc.RequestCount()
	time.Sleep(100 * time.Millisecond)
	reqCountAfterWait := orderSvc.RequestCount()

	if reqCountAfterWait > reqCountAtStop+1 {
		t.Errorf("traffic continued after SafetyController stop: at stop=%d, after wait=%d", reqCountAtStop, reqCountAfterWait)
	}

	// 7. Verify safety controller recorded the breach audit log
	audits := evaluatingCtrl.GetAuditHistory()
	if len(audits) == 0 {
		t.Fatal("expected safety audit entries, got 0")
	}

	lastAudit := audits[len(audits)-1]
	if !lastAudit.Decision.ShouldStop {
		t.Errorf("last audit decision ShouldStop: want true, got false")
	}
	if !strings.Contains(lastAudit.Decision.Reason, "P95 latency breached SLA") {
		t.Errorf("unexpected breach reason: %q", lastAudit.Decision.Reason)
	}
}

// TestChaosE2E_OrderService_ErrorRateSLABreachStop validates that when an active step load
// triggers an error rate breach on the Order Service (HTTP 500 responses), the SafetyController
// halts the stress engine in less than 1 second (<1s).
func TestChaosE2E_OrderService_ErrorRateSLABreachStop(t *testing.T) {
	orderSvc := newMockOrderService()
	defer orderSvc.Close()

	promAPI := newMockOrderPromAPI(orderSvc)
	promClient := watcher.NewPrometheusClientWithAPI(promAPI)

	// Configure SLA Policy: Max 5% Error Rate
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 1000.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.95,
	}

	expCtx, cancelExp := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelExp()

	stressCtx, cancelStress := context.WithCancel(expCtx)

	evaluatingCtrl := safety.NewEvaluatingController(policy, cancelStress, nil)

	watcherEngine := watcher.NewWatcherEngine(
		promClient,
		evaluatingCtrl.MakeSnapshotHandler(),
		watcher.WithPollInterval(25*time.Millisecond),
	)

	stressEngine := stress.NewEngine(orderSvc.server.Client())

	plan := stress.LoadPlan{
		TargetURL:           orderSvc.URL() + "/order",
		Method:              "POST",
		Payload:             `{"product_id":"prod-100","quantity":2}`,
		InitialRate:         40,
		StepRate:            10,
		MaxRate:             100,
		StepDurationSeconds: 5,
	}

	if err := watcherEngine.Start(expCtx, "exp-chaos-err", "order-service"); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}
	defer watcherEngine.Stop()

	stressDoneCh := make(chan error, 1)
	go func() {
		stressDoneCh <- stressEngine.Start(stressCtx, plan)
	}()

	time.Sleep(100 * time.Millisecond)

	// INJECT CHAOS: Force Order Service to return HTTP 500
	chaosInjectedAt := time.Now()
	orderSvc.SetStatusCode(http.StatusInternalServerError)

	select {
	case err := <-stressDoneCh:
		elapsed := time.Since(chaosInjectedAt)
		if elapsed >= 1*time.Second {
			t.Fatalf("Error rate SLA breach stop took >= 1s: %v", elapsed)
		}
		t.Logf("✓ SafetyController halted traffic after error chaos in %v (< 1s)", elapsed)

		if err != nil && err != context.Canceled {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}

	case <-time.After(1 * time.Second):
		t.Fatalf("Error rate SLA breach did not stop within 1 second")
	}

	audits := evaluatingCtrl.GetAuditHistory()
	if len(audits) == 0 {
		t.Fatal("expected safety audit entries, got 0")
	}
	lastAudit := audits[len(audits)-1]
	if !lastAudit.Decision.ShouldStop {
		t.Errorf("last decision ShouldStop: want true, got false")
	}
	if !strings.Contains(lastAudit.Decision.Reason, "Error rate breached SLA") {
		t.Errorf("unexpected breach reason: %q", lastAudit.Decision.Reason)
	}
}

// ---------------------------------------------------------------------------
// In-Memory Repository for ExperimentManager E2E Test
// ---------------------------------------------------------------------------

type e2eMockRepo struct {
	mu           sync.Mutex
	experiments  map[string]*experiment.Experiment
	stateHistory []experiment.ExperimentState
	observations map[string][]capacity.Observation
}

func newE2EMockRepo() *e2eMockRepo {
	return &e2eMockRepo{
		experiments:  make(map[string]*experiment.Experiment),
		observations: make(map[string][]capacity.Observation),
	}
}

func (r *e2eMockRepo) Create(ctx context.Context, exp *experiment.Experiment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *exp
	r.experiments[exp.ID] = &copied
	r.stateHistory = append(r.stateHistory, exp.State)
	return nil
}

func (r *e2eMockRepo) GetByID(ctx context.Context, id string) (*experiment.Experiment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.experiments[id]
	if !ok {
		return nil, nil
	}
	copied := *exp
	return &copied, nil
}

func (r *e2eMockRepo) List(ctx context.Context) ([]experiment.Experiment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]experiment.Experiment, 0, len(r.experiments))
	for _, e := range r.experiments {
		list = append(list, *e)
	}
	return list, nil
}

func (r *e2eMockRepo) UpdateState(ctx context.Context, id string, state experiment.ExperimentState, stopReason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.experiments[id]
	if !ok {
		return fmt.Errorf("experiment not found")
	}
	exp.State = state
	if stopReason != "" {
		exp.StopReason = stopReason
	}
	r.stateHistory = append(r.stateHistory, state)
	return nil
}

func (r *e2eMockRepo) SaveObservation(ctx context.Context, expID string, obs capacity.Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations[expID] = append(r.observations[expID], obs)
	return nil
}

func (r *e2eMockRepo) GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capacity.Observation(nil), r.observations[expID]...), nil
}

func (r *e2eMockRepo) SaveReport(ctx context.Context, report *capacity.ExperimentReport) error {
	return nil
}

func (r *e2eMockRepo) GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error) {
	return nil, nil
}

type e2eMockGraphAnalyzer struct{}

func (g *e2eMockGraphAnalyzer) BuildGraph(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error) {
	return graph.NewDependencyGraph(), nil
}

func (g *e2eMockGraphAnalyzer) ScoreCriticality(depGraph *graph.DependencyGraph) []graph.ServiceScore {
	return []graph.ServiceScore{
		{ServiceName: "order-service", Score: 85.0, Reasons: []string{"High call volume"}},
	}
}

type e2eMockCapacityAnalyzer struct{}

func (c *e2eMockCapacityAnalyzer) Analyze(obs []capacity.Observation, p safety.SafetyPolicy) capacity.CapacityResult {
	return capacity.CapacityResult{MaximumSustainableRate: 40}
}

type e2eMockReportEngine struct{}

func (e *e2eMockReportEngine) Generate(exp experiment.Experiment, capResult capacity.CapacityResult, scores []graph.ServiceScore, observations []capacity.Observation) (*capacity.ExperimentReport, error) {
	return &capacity.ExperimentReport{ExperimentID: exp.ID}, nil
}

type e2eCustomWatcher struct {
	mu         sync.Mutex
	promClient *watcher.PrometheusClient
	handler    watcher.SnapshotHandler
	engine     *watcher.WatcherEngine
}

func (w *e2eCustomWatcher) Start(ctx context.Context, experimentID string, targetService string) error {
	w.mu.Lock()
	w.engine = watcher.NewWatcherEngine(
		w.promClient,
		w.handler,
		watcher.WithPollInterval(30*time.Millisecond),
	)
	w.mu.Unlock()
	return w.engine.Start(ctx, experimentID, targetService)
}

func (w *e2eCustomWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.engine != nil {
		w.engine.Stop()
	}
}

func (w *e2eCustomWatcher) SetSnapshotHandler(h watcher.SnapshotHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handler = h
}

// TestChaosE2E_ExperimentManager_LifecycleWithChaosStop tests the complete closed-loop
// orchestration via ExperimentManager: creating an experiment, starting a Step Load Plan against
// the Order Service, injecting chaos latency, and observing that the ExperimentManager automatically
// stops the stress engine and executes the recovery and analysis pipeline in <1s.
func TestChaosE2E_ExperimentManager_LifecycleWithChaosStop(t *testing.T) {
	orderSvc := newMockOrderService()
	defer orderSvc.Close()

	promAPI := newMockOrderPromAPI(orderSvc)
	promClient := watcher.NewPrometheusClientWithAPI(promAPI)

	repo := newE2EMockRepo()
	stressEng := stress.NewEngine(orderSvc.server.Client())
	customWatcher := &e2eCustomWatcher{promClient: promClient}
	safetyCtrl := safety.NewController(nil)

	manager := experiment.NewManager(
		&e2eMockGraphAnalyzer{},
		stressEng,
		customWatcher,
		safetyCtrl,
		&e2eMockCapacityAnalyzer{},
		&e2eMockReportEngine{},
		repo,
	)
	defer manager.Shutdown()

	config := experiment.ExperimentConfig{
		InitialRate:         50,
		StepRate:            10,
		MaxRate:             200,
		StepDurationSeconds: 5,
		MaxP95LatencyMs:     200.0,
		MaxErrorRatePercent: 5.0,
		MinAvailabilityPct:  95.0,
		RecoveryWindowSec:   1,
	}

	ctx := context.Background()
	exp, err := manager.CreateExperiment(ctx, "order-service", orderSvc.URL()+"/order", experiment.ExperimentStep, config)
	if err != nil {
		t.Fatalf("failed to create experiment: %v", err)
	}

	plan := stress.LoadPlan{
		TargetURL:           orderSvc.URL() + "/order",
		Method:              "POST",
		Payload:             `{"order_id":"123"}`,
		InitialRate:         50,
		StepRate:            10,
		MaxRate:             200,
		StepDurationSeconds: 5,
	}

	if err := manager.StartExperiment(ctx, exp.ID, plan); err != nil {
		t.Fatalf("failed to start experiment: %v", err)
	}

	// Wait for experiment to enter StateRunning and generate baseline traffic
	time.Sleep(100 * time.Millisecond)
	status, err := manager.GetStatus(ctx, exp.ID)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.State != experiment.StateRunning {
		t.Fatalf("expected experiment state %s, got %s", experiment.StateRunning, status.State)
	}

	// INJECT CHAOS: Artificial latency jump to 500ms (> 200ms SLA)
	chaosInjectedAt := time.Now()
	orderSvc.SetLatency(500 * time.Millisecond)

	// Poll until experiment transitions out of StateRunning due to safety cancellation
	var stoppedDuration time.Duration
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	stopped := false
	for !stopped {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for experiment to halt after chaos injection")
		case <-ticker.C:
			current, err := manager.GetStatus(ctx, exp.ID)
			if err == nil && current.State != experiment.StateRunning {
				stoppedDuration = time.Since(chaosInjectedAt)
				stopped = true
				t.Logf("✓ ExperimentManager halted experiment from RUNNING to %s in %v", current.State, stoppedDuration)
			}
		}
	}

	// Sub-second validation: stress traffic halted in <1s
	if stoppedDuration >= 1*time.Second {
		t.Fatalf("chaos stop took >= 1s: took %v", stoppedDuration)
	}

	// Confirm observations were recorded
	observations, err := repo.GetObservations(ctx, exp.ID)
	if err != nil {
		t.Fatalf("failed to fetch observations: %v", err)
	}
	if len(observations) == 0 {
		t.Errorf("expected observations to be recorded, got 0")
	}
}
