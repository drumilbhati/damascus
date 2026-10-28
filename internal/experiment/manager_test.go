package experiment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/report"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

type mockRepo struct {
	mu           sync.Mutex
	experiments  map[string]*experiment.Experiment
	stateHistory []experiment.ExperimentState
	observations map[string][]capacity.Observation
	report       *capacity.ExperimentReport
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		experiments:  make(map[string]*experiment.Experiment),
		observations: make(map[string][]capacity.Observation),
	}
}

func (r *mockRepo) Create(ctx context.Context, exp *experiment.Experiment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyExp := *exp
	r.experiments[exp.ID] = &copyExp
	r.stateHistory = append(r.stateHistory, exp.State)
	return nil
}

func (r *mockRepo) GetByID(ctx context.Context, id string) (*experiment.Experiment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.experiments[id]
	if !ok {
		return nil, nil
	}
	copyExp := *exp
	return &copyExp, nil
}

func (r *mockRepo) List(ctx context.Context) ([]experiment.Experiment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]experiment.Experiment, 0, len(r.experiments))
	for _, exp := range r.experiments {
		list = append(list, *exp)
	}
	return list, nil
}

func (r *mockRepo) UpdateState(ctx context.Context, id string, state experiment.ExperimentState, stopReason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	exp, ok := r.experiments[id]
	if !ok {
		return errors.New("not found")
	}
	exp.State = state
	r.stateHistory = append(r.stateHistory, state)
	return nil
}

func (r *mockRepo) SaveObservation(ctx context.Context, expID string, obs capacity.Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations[expID] = append(r.observations[expID], obs)
	return nil
}

func (r *mockRepo) GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capacity.Observation(nil), r.observations[expID]...), nil
}

func (r *mockRepo) SaveReport(ctx context.Context, report *capacity.ExperimentReport) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report = report
	return nil
}

func (r *mockRepo) GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.report, nil
}

type mockStressEngine struct {
	mu           sync.Mutex
	startCalled  bool
	stopCalled   bool
	startBlockCh chan struct{}
}

func newMockStressEngine() *mockStressEngine {
	return &mockStressEngine{
		startBlockCh: make(chan struct{}),
	}
}

func (m *mockStressEngine) Start(ctx context.Context, plan stress.LoadPlan) error {
	m.mu.Lock()
	m.startCalled = true
	m.mu.Unlock()

	select {
	case <-m.startBlockCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockStressEngine) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
}

func (m *mockStressEngine) Unblock() {
	close(m.startBlockCh)
}

type mockWatcher struct {
	mu         sync.Mutex
	handler    watcher.SnapshotHandler
	stopCalled bool
	running    bool
	stoppedCh  chan struct{}
}

func newMockWatcher() *mockWatcher {
	return &mockWatcher{
		stoppedCh: make(chan struct{}),
	}
}

func (w *mockWatcher) Start(ctx context.Context, experimentID string, targetService string) error {
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (w *mockWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running = false
	w.stopCalled = true
}

func (w *mockWatcher) SetSnapshotHandler(h watcher.SnapshotHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handler = h
}

func (w *mockWatcher) EmitSnapshot(s watcher.MetricSnapshot) {
	w.mu.Lock()
	h := w.handler
	w.mu.Unlock()
	if h != nil {
		h(s)
	}
}

type mockSafetyController struct{}

func (s *mockSafetyController) Evaluate(ctx context.Context, snapshot watcher.MetricSnapshot, policy safety.SafetyPolicy) safety.SafetyDecision {
	return safety.SafetyDecision{ShouldStop: false}
}

type mockCapacityAnalyzer struct{}

func (c *mockCapacityAnalyzer) Analyze(obs []capacity.Observation, p safety.SafetyPolicy) capacity.CapacityResult {
	return capacity.CapacityResult{MaximumSustainableRate: 100}
}

type mockReportEngine struct{}

func (r *mockReportEngine) Generate(exp experiment.Experiment, capResult capacity.CapacityResult, scores []graph.ServiceScore, obs []capacity.Observation) (*capacity.ExperimentReport, error) {
	return &capacity.ExperimentReport{ExperimentID: exp.ID}, nil
}

func TestManager_TransitionState_ThreadSafe(t *testing.T) {
	repo := newMockRepo()
	mgr := experiment.NewManager(nil, newMockStressEngine(), newMockWatcher(), &mockSafetyController{}, &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)

	exp, err := mgr.CreateExperiment(context.Background(), "test-svc", "http://test.local", experiment.ExperimentStep, experiment.ExperimentConfig{
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
		MaxP95LatencyMs:     200,
		MaxErrorRatePercent: 5,
		MinAvailabilityPct:  99,
		RecoveryWindowSec:   1,
	})
	if err != nil {
		t.Fatalf("failed to create experiment: %v", err)
	}

	// Concurrent attempts to mutate or stop
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.StopExperiment(context.Background(), exp.ID, "concurrent stop")
		}()
	}
	wg.Wait()
}

func TestManager_TransitionState_ValidationAndPersistence(t *testing.T) {
	repo := newMockRepo()
	mgr := experiment.NewManager(nil, newMockStressEngine(), newMockWatcher(), &mockSafetyController{}, &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)

	exp, err := mgr.CreateExperiment(context.Background(), "test-svc", "http://test.local", experiment.ExperimentStep, experiment.ExperimentConfig{
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
		MaxP95LatencyMs:     200,
		MaxErrorRatePercent: 5,
		MinAvailabilityPct:  99,
		RecoveryWindowSec:   1,
	})
	if err != nil {
		t.Fatalf("failed to create experiment: %v", err)
	}

	// Attempting to stop an experiment that is in CREATED should fail transition validation (CREATED -> STOPPING is illegal)
	err = mgr.StopExperiment(context.Background(), exp.ID, "stop created")
	if err == nil {
		t.Errorf("expected StopExperiment on CREATED experiment to fail validation, got nil")
	}
}

func TestManager_RecoveryWindow_ObservesMetricsAndTransitions(t *testing.T) {
	repo := newMockRepo()
	stressEng := newMockStressEngine()
	mockW := newMockWatcher()
	mgr := experiment.NewManager(nil, stressEng, mockW, &mockSafetyController{}, &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)

	exp, err := mgr.CreateExperiment(context.Background(), "order-service", "http://order:8080/order", experiment.ExperimentStep, experiment.ExperimentConfig{
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
		MaxP95LatencyMs:     200,
		MaxErrorRatePercent: 5,
		MinAvailabilityPct:  99,
		RecoveryWindowSec:   1, // 1 second recovery window
	})
	if err != nil {
		t.Fatalf("failed to create experiment: %v", err)
	}

	plan := stress.LoadPlan{
		TargetURL:           "http://order:8080/order",
		Method:              "POST",
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
	}

	if err := mgr.StartExperiment(context.Background(), exp.ID, plan); err != nil {
		t.Fatalf("failed to start experiment: %v", err)
	}

	// Emit metric snapshot while stress is active
	mockW.EmitSnapshot(watcher.MetricSnapshot{Timestamp: time.Now(), RequestRate: 10, P95LatencyMs: 50})

	// Unblock stress engine so traffic finishes
	stressEng.Unblock()

	// Wait briefly for stress engine to terminate and enter STOPPING
	time.Sleep(100 * time.Millisecond)

	// Verify StressEngine was stopped
	stressEng.mu.Lock()
	if !stressEng.stopCalled {
		t.Errorf("expected StressEngine.Stop() to be called after stress finishes")
	}
	stressEng.mu.Unlock()

	// During recovery window (RecoveryWindowSec=1s), Watcher should NOT be stopped yet!
	mockW.mu.Lock()
	if mockW.stopCalled {
		t.Errorf("watcher should NOT be stopped during recovery window")
	}
	mockW.mu.Unlock()

	// Emit observation DURING recovery window
	mockW.EmitSnapshot(watcher.MetricSnapshot{Timestamp: time.Now(), RequestRate: 0, P95LatencyMs: 20})

	// Now wait for recovery window (1s) + margin to complete
	time.Sleep(1200 * time.Millisecond)

	// After recovery window expires: Watcher should now be stopped
	mockW.mu.Lock()
	if !mockW.stopCalled {
		t.Errorf("expected Watcher.Stop() to be called after recovery window expired")
	}
	mockW.mu.Unlock()

	// Check that observations including post-stress were saved
	obs, _ := repo.GetObservations(context.Background(), exp.ID)
	if len(obs) < 2 {
		t.Errorf("expected at least 2 observations (stress + recovery), got %d", len(obs))
	}

	// Verify terminal state COMPLETED reached
	status, err := mgr.GetStatus(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.State != experiment.StateCompleted {
		t.Errorf("expected final state %s, got %s", experiment.StateCompleted, status.State)
	}
}

func TestManager_RecoveryWindow_ShutdownAborts(t *testing.T) {
	repo := newMockRepo()
	stressEng := newMockStressEngine()
	mockW := newMockWatcher()
	mgr := experiment.NewManager(nil, stressEng, mockW, &mockSafetyController{}, &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)

	exp, err := mgr.CreateExperiment(context.Background(), "payment-service", "http://payment:8080/pay", experiment.ExperimentStep, experiment.ExperimentConfig{
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
		MaxP95LatencyMs:     200,
		MaxErrorRatePercent: 5,
		MinAvailabilityPct:  99,
		RecoveryWindowSec:   5, // 5 seconds recovery window
	})
	if err != nil {
		t.Fatalf("failed to create experiment: %v", err)
	}

	plan := stress.LoadPlan{
		TargetURL:           "http://payment:8080/pay",
		Method:              "POST",
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
	}

	if err := mgr.StartExperiment(context.Background(), exp.ID, plan); err != nil {
		t.Fatalf("failed to start experiment: %v", err)
	}

	// Unblock stress engine so it finishes
	stressEng.Unblock()
	time.Sleep(100 * time.Millisecond)

	// Trigger manager shutdown while in recovery window
	mgr.Shutdown()
	time.Sleep(200 * time.Millisecond)

	status, err := mgr.GetStatus(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	if status.State != experiment.StateAborted {
		t.Errorf("expected state %s on shutdown during recovery, got %s", experiment.StateAborted, status.State)
	}
}

func TestManager_Pipeline_WithReportGenerator(t *testing.T) {
	repo := newMockRepo()
	stressEng := newMockStressEngine()
	mockW := newMockWatcher()
	reportGen := report.NewGenerator()

	mgr := experiment.NewManager(
		nil,
		stressEng,
		mockW,
		&mockSafetyController{},
		&mockCapacityAnalyzer{},
		reportGen,
		repo,
	)

	exp, err := mgr.CreateExperiment(context.Background(), "payment-service", "http://payment:8080/pay", experiment.ExperimentStep, experiment.ExperimentConfig{
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 1,
		MaxP95LatencyMs:     200,
		MaxErrorRatePercent: 5,
		MinAvailabilityPct:  99,
		RecoveryWindowSec:   0,
	})
	if err != nil {
		t.Fatalf("failed to create experiment: %v", err)
	}

	plan := stress.LoadPlan{
		TargetURL:           "http://payment:8080/pay",
		Method:              "POST",
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 1,
	}

	if err := mgr.StartExperiment(context.Background(), exp.ID, plan); err != nil {
		t.Fatalf("failed to start experiment: %v", err)
	}

	// Unblock stress engine so it completes the experiment pipeline
	stressEng.Unblock()

	// Bounded poll until experiment reaches StateCompleted or deadline expires
	var lastStatus *experiment.Experiment
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := mgr.GetStatus(context.Background(), exp.ID)
		if err != nil {
			t.Fatalf("failed to get status during polling: %v", err)
		}
		lastStatus = status
		if status.State == experiment.StateCompleted {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if lastStatus == nil || lastStatus.State != experiment.StateCompleted {
		t.Fatalf("expected state %s within deadline, got: %v", experiment.StateCompleted, lastStatus)
	}

	rep, err := repo.GetReport(context.Background(), exp.ID)
	if err != nil {
		t.Fatalf("failed to get generated report: %v", err)
	}
	if rep == nil {
		t.Fatal("expected report to be saved in repository, got nil")
	}
	if rep.ExperimentID != exp.ID {
		t.Errorf("expected report experiment ID %s, got %s", exp.ID, rep.ExperimentID)
	}
	if rep.TargetService != "payment-service" {
		t.Errorf("expected target service 'payment-service', got %s", rep.TargetService)
	}
	if len(rep.Recommendations) == 0 {
		t.Error("expected recommendations to be populated by report generator")
	}

	// Verify the report can be rendered to HTML and JSON
	htmlBytes, err := reportGen.RenderHTML(rep)
	if err != nil {
		t.Fatalf("failed to render HTML for manager report: %v", err)
	}
	if len(htmlBytes) == 0 {
		t.Error("expected non-empty HTML bytes")
	}

	jsonBytes, err := reportGen.RenderJSON(rep)
	if err != nil {
		t.Fatalf("failed to render JSON for manager report: %v", err)
	}
	if len(jsonBytes) == 0 {
		t.Error("expected non-empty JSON bytes")
	}
}
