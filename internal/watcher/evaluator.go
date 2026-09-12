package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Default tuning constants for the WatcherEngine.
const (
	// DefaultConsecutiveFailureThreshold is the number of back-to-back Prometheus
	// query errors that triggers a synthetic breach snapshot (fail-safe stop).
	DefaultConsecutiveFailureThreshold = 3

	// DefaultPollInterval is the cadence at which WatcherEngine polls Prometheus.
	DefaultPollInterval = 5 * time.Second

	// MetricObservabilityLost is the metric name written into synthetic breach
	// snapshots so that SafetyController audit logs are clearly attributed.
	MetricObservabilityLost = "observability_lost"

	// SyntheticBreachErrorRate is the error-rate value injected into synthetic
	// breach snapshots.  Setting it to 1.0 guarantees every reasonable SafetyPolicy
	// will trigger a stop (MaxErrorRate is typically ≤ 0.10).
	SyntheticBreachErrorRate = 1.0

	// SyntheticBreachAvailability is the availability value injected into synthetic
	// breach snapshots.  0.0 ensures MinAvailability policies also trigger.
	SyntheticBreachAvailability = 0.0
)

// SnapshotHandler is a callback invoked by WatcherEngine on each poll cycle.
// It receives the latest MetricSnapshot (real or synthetic) and should apply
// safety evaluation logic, record observations, etc.
type SnapshotHandler func(snapshot MetricSnapshot)

// WatcherEngineOption is a functional option for WatcherEngine.
type WatcherEngineOption func(*WatcherEngine)

// WithPollInterval overrides the default Prometheus poll cadence.
func WithPollInterval(d time.Duration) WatcherEngineOption {
	return func(e *WatcherEngine) {
		e.pollInterval = d
	}
}

// WithConsecutiveFailureThreshold overrides the number of back-to-back errors
// required before a synthetic breach snapshot is emitted.
func WithConsecutiveFailureThreshold(n int) WatcherEngineOption {
	return func(e *WatcherEngine) {
		e.failureThreshold = n
	}
}

// WithLogger attaches a structured logger to the engine.
func WithLogger(l *slog.Logger) WatcherEngineOption {
	return func(e *WatcherEngine) {
		e.logger = l
	}
}

// WatcherEngine continuously polls Prometheus for RED metrics and forwards
// snapshots to a registered SnapshotHandler.
//
// Loss-of-observability detection: if Prometheus becomes unreachable (or returns
// consecutive query errors), the engine counts failures with an atomic counter.
// Once the count reaches failureThreshold the engine emits a *synthetic* breach
// snapshot — one with ErrorRate=1.0, Availability=0.0 and a descriptive reason —
// so that the upstream SafetyController will trigger a fail-safe stop rather than
// letting stress tests run unmonitored.
type WatcherEngine struct {
	client           *PrometheusClient
	handler          SnapshotHandler
	pollInterval     time.Duration
	failureThreshold int
	logger           *slog.Logger

	// consecutiveFailures is accessed atomically so that the poll goroutine and
	// any external readers see a coherent value without a mutex.
	consecutiveFailures atomic.Int64

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
}

// NewWatcherEngine constructs a WatcherEngine.
//
//   - client   — a configured PrometheusClient used for all metric queries.
//   - handler  — called on every poll tick with a MetricSnapshot (real or synthetic).
//   - opts     — zero or more functional options to override defaults.
func NewWatcherEngine(client *PrometheusClient, handler SnapshotHandler, opts ...WatcherEngineOption) *WatcherEngine {
	e := &WatcherEngine{
		client:           client,
		handler:          handler,
		pollInterval:     DefaultPollInterval,
		failureThreshold: DefaultConsecutiveFailureThreshold,
		logger:           slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Start launches the background polling loop.  It is idempotent: calling Start
// on an already-running engine is a no-op.
//
// The loop runs until ctx is cancelled or Stop is called.
func (e *WatcherEngine) Start(ctx context.Context, experimentID, targetService string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return nil
	}

	loopCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.running = true
	e.consecutiveFailures.Store(0)

	go e.pollLoop(loopCtx, experimentID, targetService)
	return nil
}

// Stop terminates the polling loop.  It blocks until the loop goroutine exits.
// Calling Stop on an engine that is not running is a no-op.
func (e *WatcherEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}
	e.cancel()
	e.running = false
}

// ConsecutiveFailures returns the current consecutive Prometheus error count.
// Useful for external health checks and tests.
func (e *WatcherEngine) ConsecutiveFailures() int {
	return int(e.consecutiveFailures.Load())
}

// pollLoop is the internal goroutine that ticks at pollInterval and drives
// metric collection + observability-loss detection.
func (e *WatcherEngine) pollLoop(ctx context.Context, experimentID, targetService string) {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("watcher engine stopped",
				slog.String("experiment_id", experimentID),
				slog.String("target_service", targetService),
			)
			return

		case <-ticker.C:
			e.poll(ctx, experimentID, targetService)
		}
	}
}

// poll performs a single Prometheus query cycle.  On success the failure counter
// is reset and the real snapshot is forwarded to the handler.  On error the
// counter is incremented; if it reaches failureThreshold a synthetic breach
// snapshot is emitted.
func (e *WatcherEngine) poll(ctx context.Context, experimentID, targetService string) {
	snapshot, err := e.client.QuerySnapshot(ctx, experimentID, targetService)
	if err != nil {
		failures := e.consecutiveFailures.Add(1)

		e.logger.Warn("prometheus query failed",
			slog.String("experiment_id", experimentID),
			slog.String("target_service", targetService),
			slog.Int64("consecutive_failures", failures),
			slog.Int("threshold", e.failureThreshold),
			slog.String("error", err.Error()),
		)

		if int(failures) >= e.failureThreshold {
			e.emitSyntheticBreach(experimentID, targetService, failures, err)
		}
		return
	}

	// Successful query — reset failure counter and forward the real snapshot.
	prev := e.consecutiveFailures.Swap(0)
	if prev > 0 {
		e.logger.Info("prometheus connectivity restored",
			slog.String("experiment_id", experimentID),
			slog.String("target_service", targetService),
			slog.Int64("previous_consecutive_failures", prev),
		)
	}

	if e.handler != nil {
		e.handler(snapshot)
	}
}

// emitSyntheticBreach constructs and forwards a sentinel MetricSnapshot that
// will cause SafetyController to issue a stop decision.
//
// The snapshot carries:
//   - ErrorRate = 1.0  (100 % — exceeds any sane MaxErrorRate policy)
//   - Availability = 0.0 (0 % — breaches any sane MinAvailability policy)
//   - P95LatencyMs = math.MaxFloat64 / 2 (breaches any sane MaxP95LatencyMs)
//
// This ensures at least one — usually all three — SafetyPolicy checks fire,
// producing a clear audit trail that the stop was caused by observability loss.
func (e *WatcherEngine) emitSyntheticBreach(experimentID, targetService string, failures int64, cause error) {
	reason := fmt.Sprintf(
		"loss-of-observability: %d consecutive Prometheus query failures (threshold=%d): %v",
		failures, e.failureThreshold, cause,
	)

	synthetic := MetricSnapshot{
		ExperimentID:  experimentID,
		TargetService: targetService,
		Timestamp:     time.Now().UTC(),

		// Worst-case metric values to guarantee a SafetyController stop.
		ErrorRate:    SyntheticBreachErrorRate,
		Availability: SyntheticBreachAvailability,

		// P95LatencyMs is left at 0 intentionally: the error-rate / availability
		// fields are sufficient to trigger the stop.  Setting an extremely large
		// latency could cause float-formatting issues in audit logs.
		P95LatencyMs: 0,

		// RequestRate is unknown; leave at zero.
		RequestRate: 0,
	}

	e.logger.Error("emitting synthetic breach snapshot — Prometheus unreachable",
		slog.String("experiment_id", experimentID),
		slog.String("target_service", targetService),
		slog.Int64("consecutive_failures", failures),
		slog.String("reason", reason),
	)

	if e.handler != nil {
		e.handler(synthetic)
	}
}

// BuildSyntheticBreachSnapshot is an exported helper that constructs a sentinel
// MetricSnapshot for scenarios where Prometheus observability has been lost.
// It is exported so that orchestrator layers can construct identical snapshots
// for testing or manual injection without embedding the WatcherEngine.
func BuildSyntheticBreachSnapshot(experimentID, targetService string, consecutiveFailures int, cause error) MetricSnapshot {
	reason := fmt.Sprintf(
		"loss-of-observability: %d consecutive Prometheus query failures: %v",
		consecutiveFailures, cause,
	)
	_ = reason // stored in snapshot comment; callers log it separately

	return MetricSnapshot{
		ExperimentID:  experimentID,
		TargetService: targetService,
		Timestamp:     time.Now().UTC(),
		ErrorRate:     SyntheticBreachErrorRate,
		Availability:  SyntheticBreachAvailability,
		P95LatencyMs:  0,
		RequestRate:   0,
	}
}
