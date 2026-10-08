package watcher_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"damascus/internal/watcher"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type customMockQuerier struct {
	mu            sync.Mutex
	queryFn       func(ctx context.Context, expID, targetService string) (watcher.MetricSnapshot, error)
	callCount     int
	lastServiceID string
}

func (m *customMockQuerier) QuerySnapshot(ctx context.Context, expID, targetService string) (watcher.MetricSnapshot, error) {
	m.mu.Lock()
	m.callCount++
	m.lastServiceID = targetService
	fn := m.queryFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, expID, targetService)
	}
	return watcher.MetricSnapshot{
		ExperimentID:  expID,
		TargetService: targetService,
		Timestamp:     time.Now().UTC(),
		RequestRate:   100.0,
		P95LatencyMs:  50.0,
		ErrorRate:     0.01,
		Availability:  0.99,
	}, nil
}

func (m *customMockQuerier) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestWatcher_StartAndStreamSnapshots(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock,
		watcher.WithWatcherPollInterval(15*time.Millisecond),
		watcher.WithWatcherBufferSize(10),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-123", "orders")
	if err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel from Start")
	}
	if !w.Running() {
		t.Fatal("expected watcher to be running")
	}

	// Read first snapshot from channel
	select {
	case snapshot, ok := <-ch:
		if !ok {
			t.Fatal("channel closed prematurely")
		}
		if snapshot.ExperimentID != "exp-123" {
			t.Errorf("expected experimentID exp-123, got %s", snapshot.ExperimentID)
		}
		if snapshot.TargetService != "orders" {
			t.Errorf("expected targetService orders, got %s", snapshot.TargetService)
		}
		if snapshot.RequestRate != 100.0 {
			t.Errorf("expected request rate 100.0, got %f", snapshot.RequestRate)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for metric snapshot on channel")
	}

	// Read a second snapshot to confirm periodic streaming
	select {
	case snapshot, ok := <-ch:
		if !ok {
			t.Fatal("channel closed prematurely")
		}
		if snapshot.ExperimentID != "exp-123" {
			t.Errorf("expected experimentID exp-123, got %s", snapshot.ExperimentID)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for second metric snapshot")
	}

	w.Stop()

	if w.Running() {
		t.Fatal("expected watcher to stop running")
	}

	// Verify channel is drained and closed
	for range ch {
	}
}

func TestWatcher_WithPrometheusClient(t *testing.T) {
	mockAPI := &mockPromAPI{
		queryFn: func(_ context.Context, query string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
			if strings.Contains(query, "histogram_quantile") {
				return model.Vector{&model.Sample{Value: 0.15}}, nil, nil
			}
			if strings.Contains(query, `status=~"5.."`) {
				return model.Vector{&model.Sample{Value: 0.02}}, nil, nil
			}
			if strings.Contains(query, "http_requests_total") {
				return model.Vector{&model.Sample{Value: 250.0}}, nil, nil
			}
			return model.Vector{&model.Sample{Value: 0.0}}, nil, nil
		},
	}

	client := watcher.NewPrometheusClientWithAPI(mockAPI)
	w := watcher.New(client,
		watcher.WithWatcherPollInterval(15*time.Millisecond),
		watcher.WithBufferSize(5),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-prom", "cartservice")
	if err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}

	select {
	case snapshot, ok := <-ch:
		if !ok {
			t.Fatal("expected snapshot on channel")
		}
		if snapshot.ExperimentID != "exp-prom" {
			t.Errorf("expected exp-prom, got %s", snapshot.ExperimentID)
		}
		if snapshot.TargetService != "cartservice" {
			t.Errorf("expected cartservice, got %s", snapshot.TargetService)
		}
		if snapshot.RequestRate != 250.0 {
			t.Errorf("expected RequestRate 250.0, got %f", snapshot.RequestRate)
		}
		if snapshot.P95LatencyMs != 150.0 {
			t.Errorf("expected P95LatencyMs 150.0, got %f", snapshot.P95LatencyMs)
		}
		if snapshot.ErrorRate != 0.02 {
			t.Errorf("expected ErrorRate 0.02, got %f", snapshot.ErrorRate)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for metric snapshot from PrometheusClient")
	}

	w.Stop()
}

func TestWatcher_ImmediatePoll(t *testing.T) {
	mock := &customMockQuerier{}
	// Large interval, but immediate poll is enabled
	w := watcher.NewWatcher(mock,
		watcher.WithWatcherPollInterval(10*time.Second),
		watcher.WithWatcherImmediatePoll(true),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-imm", "frontend")
	if err != nil {
		t.Fatalf("unexpected Start error: %v", err)
	}

	select {
	case snapshot, ok := <-ch:
		if !ok {
			t.Fatal("expected snapshot immediately")
		}
		if snapshot.TargetService != "frontend" {
			t.Errorf("expected frontend, got %s", snapshot.TargetService)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("immediate poll did not produce snapshot within 50ms")
	}

	w.Stop()
}

func TestWatcher_ValidationErrors(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock)

	// Nil context
	if _, err := w.Start(nil, "exp-1", "service"); err == nil {
		t.Error("expected error for nil context")
	}

	// Cancelled context
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Start(cancCtx, "exp-1", "service"); err == nil {
		t.Error("expected error for cancelled context")
	}

	// Empty expID
	if _, err := w.Start(context.Background(), "", "service"); err == nil {
		t.Error("expected error for empty expID")
	}

	// Empty targetService
	if _, err := w.Start(context.Background(), "exp-1", ""); err == nil {
		t.Error("expected error for empty targetService")
	}

	// Nil client
	nilWatcher := watcher.NewWatcher(nil)
	if _, err := nilWatcher.Start(context.Background(), "exp-1", "service"); err == nil {
		t.Error("expected error for nil client")
	}
}

func TestWatcher_AlreadyRunning(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock, watcher.WithWatcherPollInterval(20*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-1", "service")
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}
	defer w.Stop()

	// Second start while running
	_, err2 := w.Start(ctx, "exp-2", "service-2")
	if err2 == nil {
		t.Fatal("expected error on starting already running watcher")
	}
	if !strings.Contains(err2.Error(), "already running") {
		t.Errorf("expected 'already running' error, got %v", err2)
	}

	// Verify original channel still works
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("original channel closed unexpectedly")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for original snapshot")
	}
}

func TestWatcher_StopIdempotent(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock, watcher.WithWatcherPollInterval(10*time.Millisecond))

	// Stop before start is safe no-op
	w.Stop()

	ch, err := w.Start(context.Background(), "exp-stop", "service")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Stop()
		}()
	}
	wg.Wait()

	if w.Running() {
		t.Error("expected watcher not running after Stop()")
	}

	// Channel should be closed
	for range ch {
	}
}

func TestWatcher_ContextCancellationStopsLoop(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock, watcher.WithWatcherPollInterval(10*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := w.Start(ctx, "exp-ctx", "service")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel external context
	cancel()

	// Channel should close
	timeout := time.After(200 * time.Millisecond)
	channelClosed := false
	for !channelClosed {
		select {
		case _, ok := <-ch:
			if !ok {
				channelClosed = true
			}
		case <-timeout:
			t.Fatal("channel was not closed within timeout after context cancellation")
		}
	}

	// Check running state resets
	time.Sleep(20 * time.Millisecond)
	if w.Running() {
		t.Error("expected watcher to not be running after context cancellation")
	}
}

func TestWatcher_LossOfObservabilitySyntheticBreach(t *testing.T) {
	queryErr := errors.New("prometheus unreachable")
	mock := &customMockQuerier{
		queryFn: func(_ context.Context, _, _ string) (watcher.MetricSnapshot, error) {
			return watcher.MetricSnapshot{}, queryErr
		},
	}

	w := watcher.NewWatcher(mock,
		watcher.WithWatcherPollInterval(10*time.Millisecond),
		watcher.WithWatcherFailureThreshold(3),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-err", "checkout")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// First 2 failures do not emit snapshots.
	// On 3rd failure (threshold=3), a synthetic breach snapshot is emitted!
	select {
	case snapshot, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		if snapshot.ErrorRate != watcher.SyntheticBreachErrorRate {
			t.Errorf("expected ErrorRate %f, got %f", watcher.SyntheticBreachErrorRate, snapshot.ErrorRate)
		}
		if snapshot.Availability != watcher.SyntheticBreachAvailability {
			t.Errorf("expected Availability %f, got %f", watcher.SyntheticBreachAvailability, snapshot.Availability)
		}
		if snapshot.ExperimentID != "exp-err" {
			t.Errorf("expected exp-err, got %s", snapshot.ExperimentID)
		}
		if snapshot.TargetService != "checkout" {
			t.Errorf("expected checkout, got %s", snapshot.TargetService)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for synthetic breach snapshot")
	}

	if w.ConsecutiveFailures() < 3 {
		t.Errorf("expected at least 3 consecutive failures, got %d", w.ConsecutiveFailures())
	}

	w.Stop()
}

func TestWatcher_RecoveryResetsFailureCount(t *testing.T) {
	var mu sync.Mutex
	fail := true

	mock := &customMockQuerier{
		queryFn: func(_ context.Context, expID, svc string) (watcher.MetricSnapshot, error) {
			mu.Lock()
			defer mu.Unlock()
			if fail {
				return watcher.MetricSnapshot{}, errors.New("temporary failure")
			}
			return watcher.MetricSnapshot{
				ExperimentID:  expID,
				TargetService: svc,
				RequestRate:   50.0,
			}, nil
		},
	}

	w := watcher.NewWatcher(mock,
		watcher.WithWatcherPollInterval(10*time.Millisecond),
		watcher.WithWatcherFailureThreshold(10), // high threshold so breach isn't emitted yet
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-rec", "payment")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for failures to accumulate
	time.Sleep(35 * time.Millisecond)
	if w.ConsecutiveFailures() < 2 {
		t.Errorf("expected consecutive failures >= 2, got %d", w.ConsecutiveFailures())
	}

	// Recover Prometheus
	mu.Lock()
	fail = false
	mu.Unlock()

	// Read next healthy snapshot
	select {
	case snapshot, ok := <-ch:
		if !ok {
			t.Fatal("channel closed prematurely")
		}
		if snapshot.RequestRate != 50.0 {
			t.Errorf("expected request rate 50.0, got %f", snapshot.RequestRate)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for recovered snapshot")
	}

	if w.ConsecutiveFailures() != 0 {
		t.Errorf("expected consecutive failures to reset to 0, got %d", w.ConsecutiveFailures())
	}

	w.Stop()
}

func TestWatcher_BufferFullNonBlocking(t *testing.T) {
	mock := &customMockQuerier{}
	// Small buffer of size 2, fast polling
	w := watcher.NewWatcher(mock,
		watcher.WithWatcherPollInterval(5*time.Millisecond),
		watcher.WithWatcherBufferSize(2),
		watcher.WithWatcherDropOnFull(true),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := w.Start(ctx, "exp-buf", "service")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Let multiple ticks happen without draining the channel
	time.Sleep(40 * time.Millisecond)

	// Poller should still be running without deadlock
	if !w.Running() {
		t.Fatal("expected watcher to still be running")
	}

	// Drain the channel
	drained := 0
	for {
		select {
		case _, ok := <-ch:
			if ok {
				drained++
			}
		default:
			goto DRAINED
		}
	}
DRAINED:
	if drained == 0 {
		t.Error("expected to have drained snapshots from the buffer")
	}

	w.Stop()
}

func TestWatcher_CanRestartAfterStop(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock, watcher.WithWatcherPollInterval(10*time.Millisecond))

	// First cycle
	ctx1, cancel1 := context.WithCancel(context.Background())
	ch1, err := w.Start(ctx1, "exp-cycle-1", "service")
	if err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	<-ch1
	w.Stop()
	cancel1()

	if w.Running() {
		t.Fatal("expected watcher not running after first stop")
	}

	// Second cycle
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch2, err := w.Start(ctx2, "exp-cycle-2", "service")
	if err != nil {
		t.Fatalf("second start failed: %v", err)
	}
	<-ch2
	w.Stop()

	if w.Running() {
		t.Fatal("expected watcher not running after second stop")
	}
}

func TestWatcher_ConcurrentStopAndRestart(t *testing.T) {
	mock := &customMockQuerier{}
	w := watcher.NewWatcher(mock, watcher.WithWatcherPollInterval(5*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for cycle := 0; cycle < 20; cycle++ {
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			w.Stop()
		}()

		go func() {
			defer wg.Done()
			_, _ = w.Start(ctx, "exp-concurrent", "service")
		}()

		wg.Wait()
	}

	// Final stop must cleanly terminate and not block indefinitely
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stopped cleanly without blocking
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() blocked indefinitely after concurrent stop and restart")
	}

	if w.Running() {
		t.Error("expected watcher not running after final Stop()")
	}
}
