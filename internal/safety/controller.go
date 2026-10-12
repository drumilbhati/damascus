package safety

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"damascus/internal/watcher"
)

// SafetyController evaluates telemetry snapshots against a configured SafetyPolicy.
type SafetyController struct {
	policy SafetyPolicy
}

// NewSafetyController constructs a new SafetyController with the given policy.
func NewSafetyController(policy SafetyPolicy) *SafetyController {
	return &SafetyController{
		policy: policy,
	}
}

// Evaluate evaluates a metric snapshot deterministically against the safety policy.
// It returns a SafetyDecision indicating whether an SLA threshold was breached.
func (sc *SafetyController) Evaluate(snapshot watcher.MetricSnapshot) SafetyDecision {
	// TODO: Check P95 Latency SLA breach:
	// If sc.policy.MaxP95LatencyMs > 0 and snapshot.P95LatencyMs > sc.policy.MaxP95LatencyMs,
	// return SafetyDecision{ShouldStop: true, Reason: fmt.Sprintf("P95 latency breached SLA: %.2f ms > %.2f ms", ...)}
	if sc.policy.MaxP95LatencyMs > 0 && snapshot.P95LatencyMs > sc.policy.MaxP95LatencyMs {
		return SafetyDecision{
			ShouldStop: true,
			Reason:     fmt.Sprintf("P95 latency breached SLA: %.2f ms > %.2f ms", snapshot.P95LatencyMs, sc.policy.MaxP95LatencyMs),
		}
	}

	// TODO: Check Error Rate SLA breach:
	// If sc.policy.MaxErrorRate > 0 and snapshot.ErrorRate > sc.policy.MaxErrorRate,
	// return SafetyDecision{ShouldStop: true, Reason: fmt.Sprintf("Error rate breached SLA: %.2f%% > %.2f%%", ...)}
	if sc.policy.MaxErrorRate > 0 && snapshot.ErrorRate > sc.policy.MaxErrorRate {
		return SafetyDecision{
			ShouldStop: true,
			Reason:     fmt.Sprintf("Error rate breached SLA: %.2f%% > %.2f%%", snapshot.ErrorRate*100, sc.policy.MaxErrorRate*100),
		}
	}

	// TODO: Check Availability SLA breach:
	// If sc.policy.MinAvailability > 0 and snapshot.Availability < sc.policy.MinAvailability,
	// return SafetyDecision{ShouldStop: true, Reason: fmt.Sprintf("Availability breached SLA: %.2f%% < %.2f%%", ...)}
	if sc.policy.MinAvailability > 0 && snapshot.Availability < sc.policy.MinAvailability {
		return SafetyDecision{
			ShouldStop: true,
			Reason:     fmt.Sprintf("Availability breached SLA: %.2f%% < %.2f%%", snapshot.Availability*100, sc.policy.MinAvailability*100),
		}
	}

	// TODO: Return SafetyDecision{ShouldStop: false} when all metrics are within acceptable thresholds
	return SafetyDecision{ShouldStop: false}
}

// EvaluatingController composes the base Controller with a SafetyPolicy and an
// experiment-scoped context.CancelFunc, wiring the WatcherEngine metric stream
// directly to SafetyController evaluation with a sub-second fast-path stop.
//
// # Design
//
// When WatcherEngine forwards a MetricSnapshot (real or synthetic), the
// SnapshotHandler returned by MakeSnapshotHandler:
//  1. Calls Controller.Evaluate synchronously with the configured SafetyPolicy.
//  2. On a breach (ShouldStop == true), invokes the experiment CancelFunc
//     exactly once via sync.Once — cancelling the shared experiment context
//     and propagating <-ctx.Done() to every StressEngine worker goroutine and
//     to the WatcherEngine poll loop itself, all within the same CPU tick.
//
// # Concurrency guarantees
//
//   - cancelOnce ensures the cancelFunc is called at most once even if two
//     concurrent snapshots are evaluated simultaneously (e.g. a real breach
//     arrives while a synthetic loss-of-observability snapshot is in-flight).
//   - The embedded *Controller uses its own sync.RWMutex for audit-log safety.
type EvaluatingController struct {
	*Controller

	policy     SafetyPolicy
	cancelOnce sync.Once
	cancelFunc context.CancelFunc
}

// NewEvaluatingController constructs an EvaluatingController ready to be wired
// into a WatcherEngine via MakeSnapshotHandler.
//
//   - policy     — SLA thresholds evaluated on every MetricSnapshot.
//   - cancelFunc — the context.CancelFunc from context.WithCancel for the
//     running experiment; called at most once on first breach.
//   - logger     — structured slog.Logger (uses slog.Default() when nil).
func NewEvaluatingController(
	policy SafetyPolicy,
	cancelFunc context.CancelFunc,
	logger *slog.Logger,
) *EvaluatingController {
	return &EvaluatingController{
		Controller: NewController(logger),
		policy:     policy,
		cancelFunc: cancelFunc,
	}
}

// MakeSnapshotHandler returns a watcher.SnapshotHandler suitable for passing
// directly to watcher.NewWatcherEngine as the handler argument.
//
// The returned closure captures this EvaluatingController and executes the
// full evaluate-then-cancel pipeline on every metric snapshot it receives.
//
// Example wire-up:
//
//	ctx, cancel := context.WithCancel(parentCtx)
//	ec := safety.NewEvaluatingController(policy, cancel, logger)
//	engine := watcher.NewWatcherEngine(promClient, ec.MakeSnapshotHandler())
//	engine.Start(ctx, experimentID, targetService)
func (ec *EvaluatingController) MakeSnapshotHandler() watcher.SnapshotHandler {
	return func(snapshot watcher.MetricSnapshot) {
		// Evaluate the snapshot against the SLA policy.
		// This also records an audit entry regardless of outcome.
		decision := ec.Controller.Evaluate(context.Background(), snapshot, ec.policy)

		if !decision.ShouldStop {
			return
		}

		// Fast-path: cancel the experiment context exactly once.
		// Using sync.Once means even if WatcherEngine delivers two breach
		// snapshots in rapid succession (e.g. from two goroutines), the
		// cancel function is only invoked a single time.
		ec.cancelOnce.Do(func() {
			ec.Controller.logger.Error(
				"safety controller: fast-path cancellation triggered",
				slog.String("experiment_id", snapshot.ExperimentID),
				slog.String("target_service", snapshot.TargetService),
				slog.String("reason", decision.Reason),
			)
			if ec.cancelFunc != nil {
				ec.cancelFunc()
			}
		})
	}
}

// Policy returns the SafetyPolicy configured on this controller.
// Exposed for introspection and tests.
func (ec *EvaluatingController) Policy() SafetyPolicy {
	return ec.policy
}
