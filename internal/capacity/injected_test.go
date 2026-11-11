package capacity_test

import (
	"damascus/internal/capacity"
	"damascus/internal/safety"
	"testing"
	"time"
)

func TestInjectedLoadSeparatesBackgroundAndRecovery(t *testing.T) {
	rate, zero := 5.0, 0.0
	now := time.Now()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 500, InjectedRate: &rate, P95LatencyMs: 10, Availability: 1},
		{Timestamp: now.Add(time.Second), LoadRate: 400, InjectedRate: &zero, P95LatencyMs: 200, Availability: 1},
		{Timestamp: now.Add(3 * time.Second), LoadRate: 400, InjectedRate: &zero, P95LatencyMs: 10, Availability: 1},
	}
	result := capacity.NewAnalyzer().Analyze(observations, safety.SafetyPolicy{MaxP95LatencyMs: 100, MaxErrorRate: 0})
	if result.MaximumTestedRate != 5 || result.MaximumSustainableRate != 5 || result.RecoveryTimeSec != 2 {
		t.Fatalf("incorrect capacity/recovery: %+v", result)
	}
	observations[0].ErrorRate = .01
	result = capacity.NewAnalyzer().Analyze(observations, safety.SafetyPolicy{MaxP95LatencyMs: 100, MaxErrorRate: 0})
	if result.MaximumSustainableRate != 0 {
		t.Fatalf("zero error tolerance not enforced: %+v", result)
	}
}

func TestOfferedRateDoesNotCountAsCapacityWithoutMeasuredTraffic(t *testing.T) {
	rate := 1500.0
	observations := []capacity.Observation{{Timestamp: time.Now(), LoadRate: 2, InjectedRate: &rate, P95LatencyMs: 2, Availability: 1}}
	result := capacity.NewAnalyzer().Analyze(observations, safety.SafetyPolicy{MaxP95LatencyMs: 100, MaxErrorRate: 0.1})
	if result.MaximumTestedRate != 1500 || result.MaximumSustainableRate != 0 || result.DegradationRate != 0 || result.SafetyBoundaryRate != 0 {
		t.Fatalf("unobserved traffic must not establish capacity: %+v", result)
	}
}
