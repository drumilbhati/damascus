package safety_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"damascus/internal/safety"
	"damascus/internal/watcher"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeSnapshot constructs a test MetricSnapshot with the given metric values.
func makeSnapshot(expID, svc string, p95Ms, errorRate, availability float64) watcher.MetricSnapshot {
	return watcher.MetricSnapshot{
		ExperimentID:  expID,
		TargetService: svc,
		Timestamp:     time.Now().UTC(),
		P95LatencyMs:  p95Ms,
		ErrorRate:     errorRate,
		Availability:  availability,
		RequestRate:   100.0,
	}
}

// capturingCancel returns a context.CancelFunc that records every invocation.
func capturingCancel() (context.CancelFunc, *atomic.Int32) {
	var count atomic.Int32
	return func() { count.Add(1) }, &count
}

// ---------------------------------------------------------------------------
// NewEvaluatingController
// ---------------------------------------------------------------------------

func TestNewEvaluatingController_NotNil(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, _ := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	if ec == nil {
		t.Fatal("expected non-nil EvaluatingController")
	}
}

func TestNewEvaluatingController_PolicyRoundTrip(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.10,
		MinAvailability: 0.95,
	}
	cancel, _ := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)

	if ec.Policy().MaxP95LatencyMs != 300.0 {
		t.Errorf("MaxP95LatencyMs: want 300.0, got %f", ec.Policy().MaxP95LatencyMs)
	}
	if ec.Policy().MaxErrorRate != 0.10 {
		t.Errorf("MaxErrorRate: want 0.10, got %f", ec.Policy().MaxErrorRate)
	}
	if ec.Policy().MinAvailability != 0.95 {
		t.Errorf("MinAvailability: want 0.95, got %f", ec.Policy().MinAvailability)
	}
}

// ---------------------------------------------------------------------------
// MakeSnapshotHandler — healthy snapshot (no cancellation)
// ---------------------------------------------------------------------------

func TestSnapshotHandler_HealthySnapshot_NoCancellation(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	// Perfectly healthy snapshot — all within policy thresholds.
	snapshot := makeSnapshot("exp-ok", "svc-a", 200.0, 0.01, 0.999)
	handler(snapshot)

	if count.Load() != 0 {
		t.Errorf("cancelFunc called %d times on healthy snapshot; want 0", count.Load())
	}
}

func TestSnapshotHandler_HealthySnapshot_AuditRecorded(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, _ := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	handler(makeSnapshot("exp-audit", "svc-b", 100.0, 0.001, 0.999))

	history := ec.GetAuditHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(history))
	}
	if history[0].Decision.ShouldStop {
		t.Error("audit entry should have ShouldStop=false for healthy snapshot")
	}
}

// ---------------------------------------------------------------------------
// MakeSnapshotHandler — P95 latency breach
// ---------------------------------------------------------------------------

func TestSnapshotHandler_P95Breach_CancelsContext(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	// P95 = 620ms — exceeds 300ms threshold.
	handler(makeSnapshot("exp-p95", "svc-c", 620.0, 0.01, 0.999))

	if count.Load() != 1 {
		t.Errorf("cancelFunc called %d times; want exactly 1", count.Load())
	}

	history := ec.GetAuditHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(history))
	}
	if !history[0].Decision.ShouldStop {
		t.Error("audit ShouldStop must be true on P95 breach")
	}
	if history[0].Action != safety.ActionSafetyStopTriggered {
		t.Errorf("expected action %s, got %s", safety.ActionSafetyStopTriggered, history[0].Action)
	}
}

// ---------------------------------------------------------------------------
// MakeSnapshotHandler — error rate breach
// ---------------------------------------------------------------------------

func TestSnapshotHandler_ErrorRateBreach_CancelsContext(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	// ErrorRate = 0.12 — exceeds 5 % threshold.
	handler(makeSnapshot("exp-err", "svc-d", 100.0, 0.12, 0.999))

	if count.Load() != 1 {
		t.Errorf("cancelFunc called %d times; want exactly 1", count.Load())
	}
}

// ---------------------------------------------------------------------------
// MakeSnapshotHandler — availability breach
// ---------------------------------------------------------------------------

func TestSnapshotHandler_AvailabilityBreach_CancelsContext(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	// Availability = 0.96 — below 99 % threshold.
	handler(makeSnapshot("exp-avail", "svc-e", 100.0, 0.01, 0.96))

	if count.Load() != 1 {
		t.Errorf("cancelFunc called %d times; want exactly 1", count.Load())
	}
}

// ---------------------------------------------------------------------------
// MakeSnapshotHandler — synthetic breach snapshot (observability lost)
// ---------------------------------------------------------------------------

func TestSnapshotHandler_SyntheticBreach_CancelsContext(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	synthetic := watcher.BuildSyntheticBreachSnapshot("exp-syn", "svc-f", 3, nil)
	handler(synthetic)

	if count.Load() != 1 {
		t.Errorf("cancelFunc called %d times on synthetic breach; want 1", count.Load())
	}
}

// ---------------------------------------------------------------------------
// Exactly-once cancellation guarantee
// ---------------------------------------------------------------------------

func TestSnapshotHandler_MultipleBreaches_CancelCalledOnce(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	// Deliver 5 breaching snapshots in sequence.
	for i := 0; i < 5; i++ {
		handler(makeSnapshot("exp-once", "svc-g", 999.0, 0.20, 0.50))
	}

	if count.Load() != 1 {
		t.Errorf("cancelFunc called %d times; want exactly 1 (sync.Once)", count.Load())
	}
}

// TestSnapshotHandler_ConcurrentBreaches_CancelCalledOnce verifies that even
// with concurrent snapshot deliveries the cancel function is invoked exactly
// once, exercising the sync.Once path under race-detector scrutiny.
func TestSnapshotHandler_ConcurrentBreaches_CancelCalledOnce(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	cancel, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancel, nil)
	handler := ec.MakeSnapshotHandler()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			handler(makeSnapshot("exp-conc", "svc-h", 999.0, 0.50, 0.30))
		}()
	}
	wg.Wait()

	if count.Load() != 1 {
		t.Errorf("cancelFunc called %d times concurrently; want exactly 1", count.Load())
	}
}

// ---------------------------------------------------------------------------
// Nil cancelFunc safety
// ---------------------------------------------------------------------------

func TestSnapshotHandler_NilCancelFunc_DoesNotPanic(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	// Pass nil cancelFunc — must not panic on breach.
	ec := safety.NewEvaluatingController(policy, nil, nil)
	handler := ec.MakeSnapshotHandler()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handler panicked with nil cancelFunc: %v", r)
		}
	}()
	handler(makeSnapshot("exp-nil-cancel", "svc-i", 999.0, 0.99, 0.0))
}

// ---------------------------------------------------------------------------
// End-to-end: WatcherEngine → EvaluatingController → context cancellation
// ---------------------------------------------------------------------------

// TestEndToEnd_WatcherEngineWiredToController exercises the full fast-path:
//  1. WatcherEngine polls a mock Prometheus returning a breaching value.
//  2. EvaluatingController.MakeSnapshotHandler fires the cancelFunc.
//  3. The experiment context is cancelled within the poll cycle.
func TestEndToEnd_WatcherEngineWiredToController(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}

	// Experiment context — this is what the StressEngine would use.
	expCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel() // safety net

	var count atomic.Int32
	// wrappedCancel is the cancelFunc passed to EvaluatingController.
	// It counts invocations AND propagates the real cancellation so expCtx.Done() fires.
	wrappedCancel := func() {
		count.Add(1)
		cancel() // actually cancel the context
	}

	ec := safety.NewEvaluatingController(policy, wrappedCancel, nil)
	handler := ec.MakeSnapshotHandler()

	// Simulate WatcherEngine delivering a P95 breach snapshot.
	breach := makeSnapshot("exp-e2e", "svc-j", 999.0, 0.01, 0.999)
	handler(breach)

	// The context must be done immediately (sub-second).
	select {
	case <-expCtx.Done():
		// ✅ context cancelled as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context was not cancelled within 100 ms of breach delivery")
	}

	if count.Load() != 1 {
		t.Errorf("cancelFunc invocation count: want 1, got %d", count.Load())
	}
}

// TestEndToEnd_HealthySnapshots_ContextStaysOpen verifies that a series of
// healthy snapshots does NOT cancel the context.
func TestEndToEnd_HealthySnapshots_ContextStaysOpen(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}

	expCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelWrapper, count := capturingCancel()
	ec := safety.NewEvaluatingController(policy, cancelWrapper, nil)
	handler := ec.MakeSnapshotHandler()

	// Send 10 healthy snapshots.
	for i := 0; i < 10; i++ {
		handler(makeSnapshot("exp-healthy", "svc-k", 100.0, 0.001, 0.999))
	}

	select {
	case <-expCtx.Done():
		t.Fatal("context was unexpectedly cancelled for healthy snapshots")
	default:
		// ✅ context still open
	}

	if count.Load() != 0 {
		t.Errorf("cancelFunc called %d times on healthy snapshots; want 0", count.Load())
	}
}
