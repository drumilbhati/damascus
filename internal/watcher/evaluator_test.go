package watcher_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"damascus/internal/watcher"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newHealthyMockAPI returns a mock that always returns a successful vector.
func newHealthyMockAPI() *mockPromAPI {
	return &mockPromAPI{
		queryFn: func(_ context.Context, _ string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
			return model.Vector{&model.Sample{Value: 0.01}}, nil, nil
		},
	}
}

// newFaultyMockAPI returns a mock that always returns an error.
func newFaultyMockAPI(err error) *mockPromAPI {
	return &mockPromAPI{
		queryFn: func(_ context.Context, _ string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
			return nil, nil, err
		},
	}
}

// captureHandler is a thread-safe SnapshotHandler that records all snapshots.
type captureHandler struct {
	mu        sync.Mutex
	snapshots []watcher.MetricSnapshot
}

func (h *captureHandler) Handle(s watcher.MetricSnapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snapshots = append(h.snapshots, s)
}

func (h *captureHandler) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.snapshots)
}

func (h *captureHandler) Last() (watcher.MetricSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.snapshots) == 0 {
		return watcher.MetricSnapshot{}, false
	}
	return h.snapshots[len(h.snapshots)-1], true
}

// ---------------------------------------------------------------------------
// WatcherEngine unit tests
// ---------------------------------------------------------------------------

func TestNewWatcherEngine_Defaults(t *testing.T) {
	client := watcher.NewPrometheusClientWithAPI(newHealthyMockAPI())
	h := &captureHandler{}
	engine := watcher.NewWatcherEngine(client, h.Handle)

	if engine == nil {
		t.Fatal("expected non-nil WatcherEngine")
	}
	if engine.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 consecutive failures on init, got %d", engine.ConsecutiveFailures())
	}
}

func TestNewWatcherEngine_WithOptions(t *testing.T) {
	client := watcher.NewPrometheusClientWithAPI(newHealthyMockAPI())
	h := &captureHandler{}
	engine := watcher.NewWatcherEngine(client, h.Handle,
		watcher.WithPollInterval(50*time.Millisecond),
		watcher.WithConsecutiveFailureThreshold(5),
	)

	if engine == nil {
		t.Fatal("expected non-nil WatcherEngine")
	}
}

// TestWatcherEngine_StartStop verifies the engine can be started and stopped
// cleanly without panics and that real snapshots are forwarded when Prometheus
// responds successfully.
func TestWatcherEngine_StartStop(t *testing.T) {
	h := &captureHandler{}
	client := watcher.NewPrometheusClientWithAPI(newHealthyMockAPI())

	engine := watcher.NewWatcherEngine(client, h.Handle,
		watcher.WithPollInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := engine.Start(ctx, "exp-start-stop", "svc-a"); err != nil {
		t.Fatalf("Start returned unexpected error: %v", err)
	}

	// Wait for at least one poll cycle.
	time.Sleep(80 * time.Millisecond)
	engine.Stop()

	if h.Len() == 0 {
		t.Error("expected at least one snapshot to be forwarded to the handler")
	}
}

// TestWatcherEngine_StartIdempotent verifies that calling Start twice does not
// spawn a second goroutine or return an error.
func TestWatcherEngine_StartIdempotent(t *testing.T) {
	client := watcher.NewPrometheusClientWithAPI(newHealthyMockAPI())
	h := &captureHandler{}

	engine := watcher.NewWatcherEngine(client, h.Handle,
		watcher.WithPollInterval(50*time.Millisecond),
	)

	ctx := context.Background()
	if err := engine.Start(ctx, "exp-idem", "svc-a"); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	// Second call should be a no-op and should NOT return an error.
	if err := engine.Start(ctx, "exp-idem", "svc-a"); err != nil {
		t.Fatalf("second Start failed unexpectedly: %v", err)
	}
	engine.Stop()
}

// TestWatcherEngine_StopIdempotent verifies that calling Stop on an idle engine
// does not panic.
func TestWatcherEngine_StopIdempotent(t *testing.T) {
	client := watcher.NewPrometheusClientWithAPI(newHealthyMockAPI())
	engine := watcher.NewWatcherEngine(client, nil)
	// Stop before Start — must not panic.
	engine.Stop()
	engine.Stop()
}

// TestWatcherEngine_ConsecutiveFailureCounter verifies the failure counter
// increments on each Prometheus error and resets on recovery.
func TestWatcherEngine_ConsecutiveFailureCounter(t *testing.T) {
	errProm := errors.New("connection refused")
	faultyAPI := newFaultyMockAPI(errProm)
	client := watcher.NewPrometheusClientWithAPI(faultyAPI)
	h := &captureHandler{}

	// Use a very large threshold so we can inspect the counter without a
	// synthetic snapshot being emitted mid-test.
	engine := watcher.NewWatcherEngine(client, h.Handle,
		watcher.WithPollInterval(15*time.Millisecond),
		watcher.WithConsecutiveFailureThreshold(100),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := engine.Start(ctx, "exp-counter", "svc-b"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Allow several poll cycles.
	time.Sleep(120*time.Millisecond)
	engine.Stop()

	failures := engine.ConsecutiveFailures()
	if failures < 3 {
		t.Errorf("expected at least 3 consecutive failures, got %d", failures)
	}
}

// TestWatcherEngine_SyntheticBreachEmitted verifies that after
// DefaultConsecutiveFailureThreshold (3) errors the engine forwards a synthetic
// breach snapshot to the handler.
func TestWatcherEngine_SyntheticBreachEmitted(t *testing.T) {
	errProm := errors.New("i/o timeout")
	// Count calls so we can observe breach + possible further polls.
	callCount := 0
	mockAPI := &mockPromAPI{
		queryFn: func(_ context.Context, _ string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
			callCount++
			return nil, nil, errProm
		},
	}
	client := watcher.NewPrometheusClientWithAPI(mockAPI)
	h := &captureHandler{}

	engine := watcher.NewWatcherEngine(client, h.Handle,
		watcher.WithPollInterval(10*time.Millisecond),
		watcher.WithConsecutiveFailureThreshold(watcher.DefaultConsecutiveFailureThreshold),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if err := engine.Start(ctx, "exp-breach", "svc-c"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait long enough to guarantee ≥3 poll ticks (threshold is 3).
	time.Sleep(200*time.Millisecond)
	engine.Stop()

	// The handler must have received at least one synthetic breach snapshot.
	if h.Len() == 0 {
		t.Fatal("expected at least one synthetic breach snapshot to be emitted")
	}

	// Every snapshot forwarded during a total-outage scenario should carry
	// the sentinel breach values.
	last, _ := h.Last()
	if last.ErrorRate != watcher.SyntheticBreachErrorRate {
		t.Errorf("synthetic breach ErrorRate: want %.1f, got %.1f",
			watcher.SyntheticBreachErrorRate, last.ErrorRate)
	}
	if last.Availability != watcher.SyntheticBreachAvailability {
		t.Errorf("synthetic breach Availability: want %.1f, got %.1f",
			watcher.SyntheticBreachAvailability, last.Availability)
	}
}

// TestWatcherEngine_RecoveryResetsCounter verifies that a successful query after
// failures resets the consecutive-failure counter to zero.
func TestWatcherEngine_RecoveryResetsCounter(t *testing.T) {
	promErr := errors.New("network unreachable")

	// First two queries fail; subsequent queries succeed.
	callCount := 0
	var mu sync.Mutex
	mockAPI := &mockPromAPI{
		queryFn: func(_ context.Context, _ string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
			mu.Lock()
			callCount++
			n := callCount
			mu.Unlock()

			// First query per poll cycle increments call count once.
			// QuerySnapshot calls QueryValue 3 times (rps, p95, err rate),
			// so failures on the *first* internal call propagate as one
			// "poll error".  We model this simply: fail for the first few
			// top-level calls, then succeed.
			if n <= 3 {
				return nil, nil, promErr
			}
			return model.Vector{&model.Sample{Value: 0.0}}, nil, nil
		},
	}

	client := watcher.NewPrometheusClientWithAPI(mockAPI)
	h := &captureHandler{}

	// Threshold = 10 so no synthetic breach during this test.
	engine := watcher.NewWatcherEngine(client, h.Handle,
		watcher.WithPollInterval(15*time.Millisecond),
		watcher.WithConsecutiveFailureThreshold(10),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	if err := engine.Start(ctx, "exp-recovery", "svc-d"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Allow time for failures and then recovery.
	time.Sleep(300*time.Millisecond)
	engine.Stop()

	// After recovery the counter should be 0.
	if engine.ConsecutiveFailures() != 0 {
		t.Errorf("expected failure counter to be 0 after recovery, got %d",
			engine.ConsecutiveFailures())
	}
}

// TestWatcherEngine_NilHandlerDoesNotPanic verifies that a nil handler does
// not cause a panic when snapshots are received.
func TestWatcherEngine_NilHandlerDoesNotPanic(t *testing.T) {
	client := watcher.NewPrometheusClientWithAPI(newHealthyMockAPI())
	engine := watcher.NewWatcherEngine(client, nil,
		watcher.WithPollInterval(20*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := engine.Start(ctx, "exp-nil-handler", "svc-e"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(80*time.Millisecond)
	engine.Stop() // must not panic
}

// ---------------------------------------------------------------------------
// BuildSyntheticBreachSnapshot unit tests
// ---------------------------------------------------------------------------

func TestBuildSyntheticBreachSnapshot_Fields(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	snap := watcher.BuildSyntheticBreachSnapshot("exp-syn", "svc-f", 5, cause)

	if snap.ExperimentID != "exp-syn" {
		t.Errorf("ExperimentID: want exp-syn, got %s", snap.ExperimentID)
	}
	if snap.TargetService != "svc-f" {
		t.Errorf("TargetService: want svc-f, got %s", snap.TargetService)
	}
	if snap.ErrorRate != watcher.SyntheticBreachErrorRate {
		t.Errorf("ErrorRate: want %.1f, got %.1f", watcher.SyntheticBreachErrorRate, snap.ErrorRate)
	}
	if snap.Availability != watcher.SyntheticBreachAvailability {
		t.Errorf("Availability: want %.1f, got %.1f", watcher.SyntheticBreachAvailability, snap.Availability)
	}
	if snap.Timestamp.IsZero() {
		t.Error("Timestamp must not be zero")
	}
}

func TestBuildSyntheticBreachSnapshot_NilError(t *testing.T) {
	// Should not panic with a nil cause error.
	snap := watcher.BuildSyntheticBreachSnapshot("exp-nil-err", "svc-g", 3, nil)
	if snap.ErrorRate != watcher.SyntheticBreachErrorRate {
		t.Errorf("expected ErrorRate=%.1f, got %.1f", watcher.SyntheticBreachErrorRate, snap.ErrorRate)
	}
}
