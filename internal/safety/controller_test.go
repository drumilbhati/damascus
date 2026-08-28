package safety_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"damascus/internal/safety"
	"damascus/internal/watcher"
)

func TestSafetyController_WithinBounds(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 250.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.95,
	}

	controller := safety.NewSafetyController(policy)

	snapshot := watcher.MetricSnapshot{
		ExperimentID:  "exp-001",
		TargetService: "checkout",
		Timestamp:     time.Now(),
		P95LatencyMs:  120.0,
		ErrorRate:     0.01,
		Availability:  0.99,
	}

	decision := controller.Evaluate(snapshot)
	if decision.ShouldStop {
		t.Errorf("expected ShouldStop to be false, got true with reason: %s", decision.Reason)
	}
}

func TestSafetyController_P95LatencyBreached(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 200.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.95,
	}

	controller := safety.NewSafetyController(policy)

	snapshot := watcher.MetricSnapshot{
		Timestamp:    time.Now(),
		P95LatencyMs: 250.5, // Breaches 200ms
		ErrorRate:    0.01,
		Availability: 0.99,
	}

	decision := controller.Evaluate(snapshot)
	if !decision.ShouldStop {
		t.Error("expected ShouldStop to be true for latency breach, got false")
	}
	if decision.Reason == "" {
		t.Error("expected non-empty reason for stop decision")
	}
}

func TestSafetyController_ErrorRateBreached(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500.0,
		MaxErrorRate:    0.05, // 5% max error rate
		MinAvailability: 0.95,
	}

	controller := safety.NewSafetyController(policy)

	snapshot := watcher.MetricSnapshot{
		Timestamp:    time.Now(),
		P95LatencyMs: 150.0,
		ErrorRate:    0.08, // 8% error rate (breach)
		Availability: 0.99, // 99% availability (well above 95% threshold)
	}

	decision := controller.Evaluate(snapshot)
	if !decision.ShouldStop {
		t.Error("expected ShouldStop to be true for error rate breach, got false")
	}
	if !strings.Contains(decision.Reason, "Error rate breached") {
		t.Errorf("expected reason to mention Error rate breach, got: %s", decision.Reason)
	}
}

func TestSafetyController_AvailabilityBreached(t *testing.T) {
	policy := safety.SafetyPolicy{
		MinAvailability: 0.99, // 99% min availability
	}

	controller := safety.NewSafetyController(policy)

	snapshot := watcher.MetricSnapshot{
		Timestamp:    time.Now(),
		Availability: 0.96, // 96% availability
	}

	decision := controller.Evaluate(snapshot)
	if !decision.ShouldStop {
		t.Error("expected ShouldStop to be true for availability breach, got false")
	}
}

func TestSafetyController_NonFiniteMetrics(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 200.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.95,
	}

	controller := safety.NewSafetyController(policy)

	// Test NaN in Latency
	nanLatencySnap := watcher.MetricSnapshot{
		Timestamp:    time.Now(),
		P95LatencyMs: math.NaN(),
		ErrorRate:    0.01,
		Availability: 0.99,
	}
	decision := controller.Evaluate(nanLatencySnap)
	if !decision.ShouldStop {
		t.Error("expected ShouldStop to be true for NaN latency, got false")
	}

	// Test Inf in ErrorRate
	infErrorSnap := watcher.MetricSnapshot{
		Timestamp:    time.Now(),
		P95LatencyMs: 100.0,
		ErrorRate:    math.Inf(1),
		Availability: 0.99,
	}
	decision = controller.Evaluate(infErrorSnap)
	if !decision.ShouldStop {
		t.Error("expected ShouldStop to be true for Inf error rate, got false")
	}

	// Test NaN in Availability
	nanAvailSnap := watcher.MetricSnapshot{
		Timestamp:    time.Now(),
		P95LatencyMs: 100.0,
		ErrorRate:    0.01,
		Availability: math.NaN(),
	}
	decision = controller.Evaluate(nanAvailSnap)
	if !decision.ShouldStop {
		t.Error("expected ShouldStop to be true for NaN availability, got false")
	}
}
