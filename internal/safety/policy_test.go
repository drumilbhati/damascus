package safety_test

import (
	"context"
	"testing"
	"time"

	"damascus/internal/experiment"
	"damascus/internal/interfaces"
	"damascus/internal/safety"
	"damascus/internal/watcher"
)

// EvaluatingController must satisfy the full SafetyController interface,
// including the MakeSnapshotHandler method added in this PR.
var _ interfaces.SafetyController = (*safety.EvaluatingController)(nil)

func TestFormatP95Breach(t *testing.T) {
	got := safety.FormatP95Breach(620.5, 500.0)
	want := "P95 latency breached SLA: 620.50 ms > 500.00 ms"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatErrorRateBreach(t *testing.T) {
	got := safety.FormatErrorRateBreach(0.085, 0.05)
	want := "Error rate breached SLA: 8.50% > 5.00%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatAvailabilityBreach(t *testing.T) {
	got := safety.FormatAvailabilityBreach(0.975, 0.99)
	want := "Availability breached SLA: 97.50% < 99.00%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestController_Evaluate_P95Breach(t *testing.T) {
	controller := safety.NewController(nil)
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	snapshot := watcher.MetricSnapshot{
		ExperimentID:  "exp-001",
		TargetService: "checkout",
		Timestamp:     time.Now(),
		P95LatencyMs:  350.0,
		ErrorRate:     0.01,
		Availability:  0.995,
	}

	decision := controller.Evaluate(context.Background(), snapshot, policy)
	if !decision.ShouldStop {
		t.Fatalf("expected ShouldStop to be true")
	}
	expectedReason := "P95 latency breached SLA: 350.00 ms > 300.00 ms"
	if decision.Reason != expectedReason {
		t.Errorf("expected reason %q, got %q", expectedReason, decision.Reason)
	}

	history := controller.GetAuditHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(history))
	}
	if history[0].Action != safety.ActionSafetyStopTriggered {
		t.Errorf("expected action %s, got %s", safety.ActionSafetyStopTriggered, history[0].Action)
	}
}

func TestController_Evaluate_ErrorRateBreach(t *testing.T) {
	controller := safety.NewController(nil)
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	snapshot := watcher.MetricSnapshot{
		ExperimentID:  "exp-002",
		TargetService: "payment",
		Timestamp:     time.Now(),
		P95LatencyMs:  200.0,
		ErrorRate:     0.075,
		Availability:  0.995,
	}

	decision := controller.Evaluate(context.Background(), snapshot, policy)
	if !decision.ShouldStop {
		t.Fatalf("expected ShouldStop to be true")
	}
	expectedReason := "Error rate breached SLA: 7.50% > 5.00%"
	if decision.Reason != expectedReason {
		t.Errorf("expected reason %q, got %q", expectedReason, decision.Reason)
	}
}

func TestController_Evaluate_AvailabilityBreach(t *testing.T) {
	controller := safety.NewController(nil)
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	snapshot := watcher.MetricSnapshot{
		ExperimentID:  "exp-003",
		TargetService: "frontend",
		Timestamp:     time.Now(),
		P95LatencyMs:  150.0,
		ErrorRate:     0.01,
		Availability:  0.965,
	}

	decision := controller.Evaluate(context.Background(), snapshot, policy)
	if !decision.ShouldStop {
		t.Fatalf("expected ShouldStop to be true")
	}
	expectedReason := "Availability breached SLA: 96.50% < 99.00%"
	if decision.Reason != expectedReason {
		t.Errorf("expected reason %q, got %q", expectedReason, decision.Reason)
	}
}

func TestController_Evaluate_Healthy(t *testing.T) {
	controller := safety.NewController(nil)
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}
	snapshot := watcher.MetricSnapshot{
		ExperimentID:  "exp-004",
		TargetService: "frontend",
		Timestamp:     time.Now(),
		P95LatencyMs:  200.0,
		ErrorRate:     0.01,
		Availability:  0.999,
	}

	decision := controller.Evaluate(context.Background(), snapshot, policy)
	if decision.ShouldStop {
		t.Fatalf("expected ShouldStop to be false")
	}
	if decision.Reason != "" {
		t.Errorf("expected empty reason, got %q", decision.Reason)
	}
}

func TestApplySafetyDecision(t *testing.T) {
	exp := &experiment.Experiment{
		ID:    "exp-100",
		State: experiment.StateRunning,
	}

	stopDecision := safety.SafetyDecision{
		ShouldStop: true,
		Reason:     "P95 latency breached SLA: 600.00 ms > 500.00 ms",
	}

	applied := safety.ApplySafetyDecision(exp, stopDecision)
	if !applied {
		t.Fatalf("expected ApplySafetyDecision to return true")
	}
	if exp.StopReason != stopDecision.Reason {
		t.Errorf("expected StopReason %q, got %q", stopDecision.Reason, exp.StopReason)
	}
	if exp.State != experiment.StateStopping {
		t.Errorf("expected State %s, got %s", experiment.StateStopping, exp.State)
	}

	// Healthy decision should not alter experiment
	healthyDecision := safety.SafetyDecision{
		ShouldStop: false,
	}
	applied = safety.ApplySafetyDecision(exp, healthyDecision)
	if applied {
		t.Fatalf("expected ApplySafetyDecision to return false for healthy decision")
	}
}
