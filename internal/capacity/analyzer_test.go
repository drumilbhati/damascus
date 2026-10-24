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

	result := analyzer.Analyze(nil, policy)
	if result.MaximumTestedRate != 0 || result.MaximumSustainableRate != 0 || result.DegradationRate != 0 {
		t.Errorf("expected all zeros for empty observations, got %+v", result)
	}

	result = analyzer.Analyze([]capacity.Observation{}, policy)
	if result.MaximumTestedRate != 0 || result.MaximumSustainableRate != 0 || result.DegradationRate != 0 {
		t.Errorf("expected all zeros for empty slice, got %+v", result)
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
