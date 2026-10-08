package watcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Default tuning constants for Watcher.
const (
	// DefaultWatcherPollInterval is the default cadence at which Watcher polls Prometheus.
	DefaultWatcherPollInterval = 1 * time.Second

	// DefaultWatcherBufferSize is the default capacity of the metric snapshot channel.
	DefaultWatcherBufferSize = 100
)

// SnapshotQuerier defines the interface for querying RED metric snapshots.
// Both *PrometheusClient and test mocks satisfy this interface.
type SnapshotQuerier interface {
	QuerySnapshot(ctx context.Context, experimentID, targetService string) (MetricSnapshot, error)
}

// WatcherOption is a functional option for configuring a Watcher.
type WatcherOption func(*Watcher)

// WithWatcherPollInterval sets the interval between Prometheus query polls.
func WithWatcherPollInterval(d time.Duration) WatcherOption {
	return func(w *Watcher) {
		if d > 0 {
			w.pollInterval = d
		}
	}
}

// WithWatcherBufferSize sets the capacity of the returned snapshot channel.
func WithWatcherBufferSize(size int) WatcherOption {
	return func(w *Watcher) {
		if size > 0 {
			w.bufferSize = size
		}
	}
}

// WithBufferSize is an alias for WithWatcherBufferSize.
func WithBufferSize(size int) WatcherOption {
	return WithWatcherBufferSize(size)
}

// WithWatcherFailureThreshold configures the number of consecutive Prometheus
// query errors before a synthetic breach snapshot is generated.
func WithWatcherFailureThreshold(threshold int) WatcherOption {
	return func(w *Watcher) {
		if threshold > 0 {
			w.failureThreshold = threshold
		}
	}
}

// WithWatcherLogger attaches a structured logger to the Watcher.
func WithWatcherLogger(l *slog.Logger) WatcherOption {
	return func(w *Watcher) {
		if l != nil {
			w.logger = l
		}
	}
}

// WithWatcherDropOnFull configures whether snapshots are dropped when the channel buffer is full.
func WithWatcherDropOnFull(drop bool) WatcherOption {
	return func(w *Watcher) {
		w.dropOnFull = drop
	}
}

// WithWatcherImmediatePoll configures whether to execute an initial poll immediately upon Start.
func WithWatcherImmediatePoll(immediate bool) WatcherOption {
	return func(w *Watcher) {
		w.immediatePoll = immediate
	}
}

// Watcher periodically polls Prometheus for RED metrics and streams
// MetricSnapshot instances across a read-only Go channel.
type Watcher struct {
	client              SnapshotQuerier
	pollInterval        time.Duration
	bufferSize          int
	failureThreshold    int
	logger              *slog.Logger
	dropOnFull          bool
	immediatePoll       bool
	consecutiveFailures atomic.Int64

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// NewWatcher initializes a Watcher instance with the given Prometheus client/querier and options.
func NewWatcher(client SnapshotQuerier, opts ...WatcherOption) *Watcher {
	w := &Watcher{
		client:           client,
		pollInterval:     DefaultWatcherPollInterval,
		bufferSize:       DefaultWatcherBufferSize,
		failureThreshold: DefaultConsecutiveFailureThreshold,
		logger:           slog.Default(),
		dropOnFull:       true,
		immediatePoll:    false,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// New is an alias for NewWatcher.
func New(client SnapshotQuerier, opts ...WatcherOption) *Watcher {
	return NewWatcher(client, opts...)
}

// Start launches the background polling goroutine and returns a read-only channel
// of MetricSnapshot structures. It polls Prometheus periodically (default: 1 second)
// and closes the returned channel when the context is cancelled or Stop is called.
func (w *Watcher) Start(ctx context.Context, expID string, targetService string) (<-chan MetricSnapshot, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if expID == "" {
		return nil, errors.New("experiment ID cannot be empty")
	}
	if targetService == "" {
		return nil, errors.New("target service cannot be empty")
	}
	if w.client == nil {
		return nil, errors.New("prometheus client is required")
	}

	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil, errors.New("watcher is already running")
	}
	w.running = true
	loopCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	done := make(chan struct{})
	w.done = done
	w.consecutiveFailures.Store(0)

	out := make(chan MetricSnapshot, w.bufferSize)
	w.mu.Unlock()

	go w.pollLoop(loopCtx, expID, targetService, out, done)

	return out, nil
}

// Stop terminates the polling loop and blocks until the background goroutine exits
// and the snapshot channel is closed. Calling Stop on an inactive watcher is a safe no-op.
func (w *Watcher) Stop() {
	w.mu.Lock()
	if !w.running {
		done := w.done
		w.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}

	cancel := w.cancel
	done := w.done
	w.running = false
	w.cancel = nil
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Running reports whether the watcher is currently running an active polling loop.
func (w *Watcher) Running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// ConsecutiveFailures returns the current number of back-to-back Prometheus query errors.
func (w *Watcher) ConsecutiveFailures() int {
	return int(w.consecutiveFailures.Load())
}

// pollLoop drives the periodic polling ticker and gracefully tears down when finished.
func (w *Watcher) pollLoop(ctx context.Context, expID, targetService string, out chan MetricSnapshot, done chan struct{}) {
	defer func() {
		close(out)
		w.mu.Lock()
		w.running = false
		w.cancel = nil
		w.mu.Unlock()
		close(done)
		w.logger.Info("watcher poller stopped",
			slog.String("experiment_id", expID),
			slog.String("target_service", targetService),
		)
	}()

	if w.immediatePoll {
		w.poll(ctx, expID, targetService, out)
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx, expID, targetService, out)
		}
	}
}

// poll queries Prometheus for RED metrics and sends the snapshot to the output channel.
// If consecutive errors reach failureThreshold, it emits a synthetic breach snapshot.
func (w *Watcher) poll(ctx context.Context, expID, targetService string, out chan MetricSnapshot) {
	snapshot, err := w.client.QuerySnapshot(ctx, expID, targetService)
	if err != nil {
		failures := w.consecutiveFailures.Add(1)
		w.logger.Warn("prometheus query failed",
			slog.String("experiment_id", expID),
			slog.String("target_service", targetService),
			slog.Int64("consecutive_failures", failures),
			slog.Int("threshold", w.failureThreshold),
			slog.String("error", err.Error()),
		)

		if w.failureThreshold > 0 && int(failures) >= w.failureThreshold {
			synthetic := BuildSyntheticBreachSnapshot(expID, targetService, int(failures), err)
			w.sendSnapshot(ctx, expID, targetService, out, synthetic)
		}
		return
	}

	prev := w.consecutiveFailures.Swap(0)
	if prev > 0 {
		w.logger.Info("prometheus connectivity restored",
			slog.String("experiment_id", expID),
			slog.String("target_service", targetService),
			slog.Int64("previous_consecutive_failures", prev),
		)
	}

	w.sendSnapshot(ctx, expID, targetService, out, snapshot)
}

// sendSnapshot dispatches the snapshot to the channel with non-blocking buffer management.
func (w *Watcher) sendSnapshot(ctx context.Context, expID, targetService string, out chan MetricSnapshot, snapshot MetricSnapshot) {
	select {
	case <-ctx.Done():
		return
	case out <- snapshot:
		return
	default:
		if w.dropOnFull {
			w.logger.Warn("metric snapshot channel buffer full; dropping incoming snapshot",
				slog.String("experiment_id", expID),
				slog.String("target_service", targetService),
			)
			return
		}
		select {
		case <-ctx.Done():
			return
		case out <- snapshot:
			return
		}
	}
}
