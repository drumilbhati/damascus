package capacity_test

import (
	"testing"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/safety"
)

func TestAnalyzer_EmptyObservations(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 200,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}

	expectedZero := capacity.CapacityResult{}

	result := analyzer.Analyze(nil, policy)
	if result != expectedZero {
		t.Errorf("expected zero-valued result for nil observations, got %+v", result)
	}

	result = analyzer.Analyze([]capacity.Observation{}, policy)
	if result != expectedZero {
		t.Errorf("expected zero-valued result for empty slice, got %+v", result)
	}
}

func TestAnalyzer_DeterministicMetrics(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 150,
		MaxErrorRate:    0.02,
		MinAvailability: 0.99,
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{
			Timestamp:    now,
			LoadRate:     100,
			P95LatencyMs: 30,
			ErrorRate:    0.0,
			Availability: 1.0,
		},
		{
			Timestamp:    now.Add(1 * time.Second),
			LoadRate:     200,
			P95LatencyMs: 40, // slope = (40-30)/100 = 0.1
			ErrorRate:    0.0,
			Availability: 1.0,
		},
		{
			Timestamp:    now.Add(2 * time.Second),
			LoadRate:     300,
			P95LatencyMs: 50, // slope = (50-40)/100 = 0.1
			ErrorRate:    0.005,
			Availability: 0.995,
		},
		{
			Timestamp:    now.Add(3 * time.Second),
			LoadRate:     400,
			P95LatencyMs: 70, // slope = (70-50)/100 = 0.2 (spike from 0.1 -> 0.2 is +100% > 50%) -> degradation rate 300
			ErrorRate:    0.01,
			Availability: 0.99,
		},
		{
			Timestamp:    now.Add(4 * time.Second),
			LoadRate:     500,
			P95LatencyMs: 120, // slope = (120-70)/100 = 0.5
			ErrorRate:    0.015,
			Availability: 0.99,
		},
		{
			Timestamp:    now.Add(5 * time.Second),
			LoadRate:     600,
			P95LatencyMs: 180, // breaches SLA MaxP95LatencyMs (180 > 150)
			ErrorRate:    0.03, // breaches SLA MaxErrorRate (0.03 > 0.02)
			Availability: 0.97,
		},
	}

	result := analyzer.Analyze(observations, policy)

	if result.MaximumTestedRate != 600 {
		t.Errorf("expected MaximumTestedRate 600, got %d", result.MaximumTestedRate)
	}
	if result.MaximumSustainableRate != 500 {
		t.Errorf("expected MaximumSustainableRate 500, got %d", result.MaximumSustainableRate)
	}
	if result.DegradationRate != 300 {
		t.Errorf("expected DegradationRate 300, got %d", result.DegradationRate)
	}
	if result.SafetyBoundaryRate != 600 {
		t.Errorf("expected SafetyBoundaryRate 600, got %d", result.SafetyBoundaryRate)
	}
}

func TestAnalyzer_DegradationFromFlatSlope(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500,
		MaxErrorRate:    0.05,
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 20},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 20}, // slope = 0
		{Timestamp: now.Add(2 * time.Second), LoadRate: 300, P95LatencyMs: 60}, // slope = 40/100 = 0.4 (from 0 -> >0) -> degradation knee at 200
	}

	result := analyzer.Analyze(observations, policy)
	if result.DegradationRate != 200 {
		t.Errorf("expected DegradationRate 200, got %d", result.DegradationRate)
	}
}

func TestAnalyzer_MultipleObservationsPerTier(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 100,
		MaxErrorRate:    0.05,
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 20, ErrorRate: 0.0},
		{Timestamp: now.Add(time.Second), LoadRate: 100, P95LatencyMs: 22, ErrorRate: 0.0},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 200, P95LatencyMs: 30, ErrorRate: 0.0},
		{Timestamp: now.Add(3 * time.Second), LoadRate: 200, P95LatencyMs: 32, ErrorRate: 0.0},
		{Timestamp: now.Add(4 * time.Second), LoadRate: 300, P95LatencyMs: 110, ErrorRate: 0.0}, // breach
	}

	result := analyzer.Analyze(observations, policy)
	if result.MaximumTestedRate != 300 {
		t.Errorf("expected MaximumTestedRate 300, got %d", result.MaximumTestedRate)
	}
	if result.MaximumSustainableRate != 200 {
		t.Errorf("expected MaximumSustainableRate 200, got %d", result.MaximumSustainableRate)
	}
}

func TestAnalyzer_RecoveryTime(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 100,
		MaxErrorRate:    0.02,
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 200, P95LatencyMs: 80, ErrorRate: 0.0},
		{Timestamp: now.Add(1 * time.Second), LoadRate: 500, P95LatencyMs: 250, ErrorRate: 0.05}, // breach
		// Post stress recovery at LoadRate = 0
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 180, ErrorRate: 0.03}, // still breached
		{Timestamp: now.Add(5 * time.Second), LoadRate: 0, P95LatencyMs: 90, ErrorRate: 0.01},  // recovered at +3s from start of zero load
	}

	result := analyzer.Analyze(observations, policy)
	if result.RecoveryTimeSec != 3 {
		t.Errorf("expected RecoveryTimeSec 3, got %d", result.RecoveryTimeSec)
	}
}

func TestAnalyzer_SafetyBoundaryRate_FallbackWhenNoBreach(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300,
		MaxErrorRate:    0.05,
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 20, ErrorRate: 0.0},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 30, ErrorRate: 0.0},
	}

	result := analyzer.Analyze(observations, policy)
	// When no breach occurs, SafetyBoundaryRate falls back to MaximumSustainableRate
	if result.SafetyBoundaryRate != 200 {
		t.Errorf("expected SafetyBoundaryRate 200, got %d", result.SafetyBoundaryRate)
	}
	if result.MaximumSustainableRate != 200 {
		t.Errorf("expected MaximumSustainableRate 200, got %d", result.MaximumSustainableRate)
	}
}

func TestAnalyzer_AvailabilityOnlyBreach(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500,
		MaxErrorRate:    0.10,
		MinAvailability: 0.99, // 99%
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 20, ErrorRate: 0.0, Availability: 0.999},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 25, ErrorRate: 0.0, Availability: 0.95}, // breaches availability only
	}

	result := analyzer.Analyze(observations, policy)
	if result.MaximumTestedRate != 200 {
		t.Errorf("expected MaximumTestedRate 200, got %d", result.MaximumTestedRate)
	}
	if result.MaximumSustainableRate != 100 {
		t.Errorf("expected MaximumSustainableRate 100, got %d", result.MaximumSustainableRate)
	}
	if result.SafetyBoundaryRate != 200 {
		t.Errorf("expected SafetyBoundaryRate 200, got %d", result.SafetyBoundaryRate)
	}
}

func TestAnalyzer_DegradationRate_FewerThanThreeTiers(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 500,
	}

	now := time.Now().UTC()
	observations := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 20},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 200}, // only 2 tiers
	}

	result := analyzer.Analyze(observations, policy)
	if result.DegradationRate != 0 {
		t.Errorf("expected DegradationRate 0 for fewer than 3 tiers, got %d", result.DegradationRate)
	}
}

func TestAnalyzer_RecoveryTime_EdgeCases(t *testing.T) {
	analyzer := capacity.NewAnalyzer()
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 100,
		MaxErrorRate:    0.02,
	}

	now := time.Now().UTC()

	// 1. No zero-load observations post-stress
	obsNoZeroLoad := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 50, ErrorRate: 0.0},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 150, ErrorRate: 0.05},
	}
	res1 := analyzer.Analyze(obsNoZeroLoad, policy)
	if res1.RecoveryTimeSec != 0 {
		t.Errorf("expected RecoveryTimeSec 0 when no zero-load observations exist, got %d", res1.RecoveryTimeSec)
	}

	// 2. Immediate recovery at first zero-load observation
	obsImmediate := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 50, ErrorRate: 0.0},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 150, ErrorRate: 0.05},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 30, ErrorRate: 0.0}, // immediately compliant
	}
	res2 := analyzer.Analyze(obsImmediate, policy)
	if res2.RecoveryTimeSec != 0 {
		t.Errorf("expected RecoveryTimeSec 0 for immediate recovery at first zero-load observation, got %d", res2.RecoveryTimeSec)
	}

	// 3. Unrecovered recovery window (never recovered)
	obsUnrecovered := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 50, ErrorRate: 0.0},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 150, ErrorRate: 0.05},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 140, ErrorRate: 0.05},
		{Timestamp: now.Add(6 * time.Second), LoadRate: 0, P95LatencyMs: 120, ErrorRate: 0.04}, // window duration: 6s - 2s = 4s
	}
	res3 := analyzer.Analyze(obsUnrecovered, policy)
	if res3.RecoveryTimeSec != 4 {
		t.Errorf("expected RecoveryTimeSec 4 for unrecovered window, got %d", res3.RecoveryTimeSec)
	}

	// 4. Preserves caller's slice order and handles unordered observations
	obsUnordered := []capacity.Observation{
		{Timestamp: now.Add(5 * time.Second), LoadRate: 0, P95LatencyMs: 90, ErrorRate: 0.01}, // recovered
		{Timestamp: now, LoadRate: 200, P95LatencyMs: 80, ErrorRate: 0.0},
		{Timestamp: now.Add(1 * time.Second), LoadRate: 500, P95LatencyMs: 250, ErrorRate: 0.05}, // breach
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 180, ErrorRate: 0.03}, // still breached
	}
	firstObsTimestamp := obsUnordered[0].Timestamp
	res4 := analyzer.Analyze(obsUnordered, policy)
	if res4.RecoveryTimeSec != 3 {
		t.Errorf("expected RecoveryTimeSec 3 for unordered observations, got %d", res4.RecoveryTimeSec)
	}
	if obsUnordered[0].Timestamp != firstObsTimestamp {
		t.Errorf("expected caller's slice order to be preserved")
	}
}
