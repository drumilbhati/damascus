package capacity_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"damascus/internal/capacity"
)

func TestCalculateRTO_EmptyObservations(t *testing.T) {
	calc := capacity.NewBaselineCalculator()

	_, err := calc.CalculateRTO(nil)
	if !errors.Is(err, capacity.ErrEmptyObservations) {
		t.Fatalf("expected ErrEmptyObservations for nil slice, got %v", err)
	}

	_, err = calc.CalculateRTO([]capacity.Observation{})
	if !errors.Is(err, capacity.ErrEmptyObservations) {
		t.Fatalf("expected ErrEmptyObservations for empty slice, got %v", err)
	}

	_, err = calc.CalculateRTOWithBaseline(nil, 50.0)
	if !errors.Is(err, capacity.ErrEmptyObservations) {
		t.Fatalf("expected ErrEmptyObservations for nil slice with baseline, got %v", err)
	}
}

func TestCalculateRTO_InvalidBaseline(t *testing.T) {
	calc := capacity.NewBaselineCalculator()
	now := time.Now().UTC()
	obs := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 50},
		{Timestamp: now.Add(time.Second), LoadRate: 0, P95LatencyMs: 30},
	}

	_, err := calc.CalculateRTOWithBaseline(obs, 0)
	if !errors.Is(err, capacity.ErrInvalidBaseline) {
		t.Fatalf("expected ErrInvalidBaseline for 0 baseline, got %v", err)
	}

	_, err = calc.CalculateRTOWithBaseline(obs, -10.0)
	if !errors.Is(err, capacity.ErrInvalidBaseline) {
		t.Fatalf("expected ErrInvalidBaseline for negative baseline, got %v", err)
	}
}

func TestCalculateRTO_NoStressPhase(t *testing.T) {
	calc := capacity.NewBaselineCalculator()
	now := time.Now().UTC()
	obs := []capacity.Observation{
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 30},
		{Timestamp: now.Add(time.Second), LoadRate: 0, P95LatencyMs: 32},
	}

	_, err := calc.CalculateRTO(obs)
	if !errors.Is(err, capacity.ErrNoStressPhase) {
		t.Fatalf("expected ErrNoStressPhase when LoadRate is always 0, got %v", err)
	}
}

func TestCalculateRTO_NoLoadTermination(t *testing.T) {
	calc := capacity.NewBaselineCalculator()
	now := time.Now().UTC()
	obs := []capacity.Observation{
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 30},
		{Timestamp: now.Add(time.Second), LoadRate: 100, P95LatencyMs: 150},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 200, P95LatencyMs: 250},
	}

	_, err := calc.CalculateRTO(obs)
	if !errors.Is(err, capacity.ErrNoLoadTermination) {
		t.Fatalf("expected ErrNoLoadTermination when active stress never terminates, got %v", err)
	}
}

func TestCalculateRTO_NoPreTestBaseline_FailsAuto(t *testing.T) {
	calc := capacity.NewBaselineCalculator()
	now := time.Now().UTC()
	// Observations start immediately with active stress
	obs := []capacity.Observation{
		{Timestamp: now, LoadRate: 100, P95LatencyMs: 120},
		{Timestamp: now.Add(time.Second), LoadRate: 0, P95LatencyMs: 40},
	}

	_, err := calc.CalculateRTO(obs)
	if !errors.Is(err, capacity.ErrNoPreTestBaseline) {
		t.Fatalf("expected ErrNoPreTestBaseline when pre-stress observations are absent, got %v", err)
	}
}

func TestExtractPreTestBaseline(t *testing.T) {
	now := time.Now().UTC()
	preStress := []capacity.Observation{
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 40},
		{Timestamp: now.Add(time.Second), LoadRate: 0, P95LatencyMs: 50},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 60},
	}

	baseline, err := capacity.ExtractPreTestBaseline(preStress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := 50.0 // (40 + 50 + 60) / 3
	if baseline != expected {
		t.Errorf("expected baseline %f, got %f", expected, baseline)
	}

	// Empty slice test
	_, err = capacity.ExtractPreTestBaseline(nil)
	if !errors.Is(err, capacity.ErrNoPreTestBaseline) {
		t.Errorf("expected ErrNoPreTestBaseline for nil slice, got %v", err)
	}
}

func TestCalculateThreshold(t *testing.T) {
	threshold := capacity.CalculateThreshold(100.0, 10.0)
	if math.Abs(threshold-110.0) > 1e-6 {
		t.Errorf("expected threshold 110.0 for baseline 100 with 10%% tolerance, got %f", threshold)
	}

	threshold50 := capacity.CalculateThreshold(50.0, 20.0)
	if math.Abs(threshold50-60.0) > 1e-6 {
		t.Errorf("expected threshold 60.0 for baseline 50 with 20%% tolerance, got %f", threshold50)
	}
}

func TestCalculateRTO_ImmediateRecovery(t *testing.T) {
	now := time.Now().UTC()
	obs := []capacity.Observation{
		// Pre-test baseline: 50ms (threshold = 55ms with 10% tolerance)
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 50},
		// Stress phase
		{Timestamp: now.Add(1 * time.Second), LoadRate: 200, P95LatencyMs: 200},
		// Post-stress: first zero-load observation is already within threshold
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 52},
		{Timestamp: now.Add(3 * time.Second), LoadRate: 0, P95LatencyMs: 48},
	}

	result, err := capacity.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Recovered {
		t.Errorf("expected Recovered=true, got false")
	}
	if result.RecoveryDurationSec != 0 {
		t.Errorf("expected RecoveryDurationSec 0 for immediate recovery, got %d", result.RecoveryDurationSec)
	}
	if result.RecoveryDuration != 0 {
		t.Errorf("expected RecoveryDuration 0, got %v", result.RecoveryDuration)
	}
	if math.Abs(result.PreTestBaselineP95Ms-50.0) > 1e-6 {
		t.Errorf("expected PreTestBaselineP95Ms 50.0, got %f", result.PreTestBaselineP95Ms)
	}
	if math.Abs(result.ThresholdP95Ms-55.0) > 1e-6 {
		t.Errorf("expected ThresholdP95Ms 55.0, got %f", result.ThresholdP95Ms)
	}
}

func TestCalculateRTO_DelayedRecovery(t *testing.T) {
	now := time.Now().UTC()
	obs := []capacity.Observation{
		// Pre-test baseline: average = 40ms, threshold = 44ms (10% tolerance)
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 38},
		{Timestamp: now.Add(1 * time.Second), LoadRate: 0, P95LatencyMs: 42},
		// Stress phase
		{Timestamp: now.Add(2 * time.Second), LoadRate: 300, P95LatencyMs: 300},
		{Timestamp: now.Add(3 * time.Second), LoadRate: 500, P95LatencyMs: 500},
		// Load termination starts at t=4s
		{Timestamp: now.Add(4 * time.Second), LoadRate: 0, P95LatencyMs: 150}, // breached
		{Timestamp: now.Add(5 * time.Second), LoadRate: 0, P95LatencyMs: 90},  // breached
		{Timestamp: now.Add(6 * time.Second), LoadRate: 0, P95LatencyMs: 60},  // breached
		// Recovered at t=7s (43ms <= 44ms threshold) -> duration = 3s
		{Timestamp: now.Add(7 * time.Second), LoadRate: 0, P95LatencyMs: 43},
		{Timestamp: now.Add(8 * time.Second), LoadRate: 0, P95LatencyMs: 40},
	}

	result, err := capacity.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Recovered {
		t.Fatalf("expected Recovered=true, got false")
	}
	if result.RecoveryDurationSec != 3 {
		t.Errorf("expected RecoveryDurationSec 3, got %d", result.RecoveryDurationSec)
	}
	if result.RecoveryDuration != 3*time.Second {
		t.Errorf("expected RecoveryDuration 3s, got %v", result.RecoveryDuration)
	}
	if result.PreTestBaselineP95Ms != 40.0 {
		t.Errorf("expected PreTestBaselineP95Ms 40.0, got %f", result.PreTestBaselineP95Ms)
	}
	if result.ThresholdP95Ms != 44.0 {
		t.Errorf("expected ThresholdP95Ms 44.0, got %f", result.ThresholdP95Ms)
	}
	expectedRecoveryTime := now.Add(7 * time.Second)
	if !result.RecoveryTime.Equal(expectedRecoveryTime) {
		t.Errorf("expected RecoveryTime %v, got %v", expectedRecoveryTime, result.RecoveryTime)
	}
}

func TestCalculateRTO_LatencyLowerThanBaseline(t *testing.T) {
	now := time.Now().UTC()
	obs := []capacity.Observation{
		// Baseline: 60ms (threshold: 66ms)
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 60},
		// Stress
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 250},
		// Post stress: latency drops to 45ms (well below baseline)
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 45},
	}

	result, err := capacity.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Recovered {
		t.Errorf("expected Recovered=true when latency drops below baseline")
	}
	if result.RecoveryDurationSec != 0 {
		t.Errorf("expected RecoveryDurationSec 0, got %d", result.RecoveryDurationSec)
	}
}

func TestCalculateRTO_UnrecoveredWindow(t *testing.T) {
	now := time.Now().UTC()
	obs := []capacity.Observation{
		// Baseline: 40ms (threshold = 44ms)
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 40},
		// Stress
		{Timestamp: now.Add(1 * time.Second), LoadRate: 200, P95LatencyMs: 200},
		// Post-stress: t=2s to t=6s (4 seconds total duration)
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 100},
		{Timestamp: now.Add(4 * time.Second), LoadRate: 0, P95LatencyMs: 80},
		{Timestamp: now.Add(6 * time.Second), LoadRate: 0, P95LatencyMs: 65}, // never reaches <= 44ms
	}

	result, err := capacity.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Recovered {
		t.Errorf("expected Recovered=false for unrecovered window")
	}
	if result.RecoveryDurationSec != 4 {
		t.Errorf("expected RecoveryDurationSec 4 (total window duration), got %d", result.RecoveryDurationSec)
	}
	if result.RecoveryDuration != 4*time.Second {
		t.Errorf("expected RecoveryDuration 4s, got %v", result.RecoveryDuration)
	}
	if !result.RecoveryTime.IsZero() {
		t.Errorf("expected zero RecoveryTime when unrecovered, got %v", result.RecoveryTime)
	}
}

func TestCalculateRTO_PreservesSliceOrder(t *testing.T) {
	now := time.Now().UTC()
	// Deliberately unordered slice
	obs := []capacity.Observation{
		{Timestamp: now.Add(4 * time.Second), LoadRate: 0, P95LatencyMs: 42}, // recovered
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 40},                     // baseline
		{Timestamp: now.Add(1 * time.Second), LoadRate: 200, P95LatencyMs: 250},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 120}, // termination start
	}

	firstObsTimestamp := obs[0].Timestamp

	result, err := capacity.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Caller's slice order must be preserved
	if obs[0].Timestamp != firstObsTimestamp {
		t.Errorf("expected original slice order to be preserved")
	}

	if !result.Recovered {
		t.Errorf("expected Recovered=true")
	}
	// Recovery occurred at t=4s, termination began at t=2s -> 2s
	if result.RecoveryDurationSec != 2 {
		t.Errorf("expected RecoveryDurationSec 2, got %d", result.RecoveryDurationSec)
	}
}

func TestCalculateRTO_CustomTolerance(t *testing.T) {
	now := time.Now().UTC()
	// Baseline = 100ms
	// With 5% tolerance -> threshold = 105ms
	// With 20% tolerance -> threshold = 120ms
	obs := []capacity.Observation{
		{Timestamp: now, LoadRate: 0, P95LatencyMs: 100},
		{Timestamp: now.Add(time.Second), LoadRate: 200, P95LatencyMs: 300},
		{Timestamp: now.Add(2 * time.Second), LoadRate: 0, P95LatencyMs: 115},
	}

	calc5 := capacity.NewBaselineCalculator(5.0)
	res5, err := calc5.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res5.ThresholdP95Ms != 105.0 {
		t.Errorf("expected ThresholdP95Ms 105.0, got %f", res5.ThresholdP95Ms)
	}
	if res5.Recovered {
		t.Errorf("expected 115ms to not recover under 5%% tolerance (105ms)")
	}

	calc20 := capacity.NewBaselineCalculator(20.0)
	res20, err := calc20.CalculateRTO(obs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res20.ThresholdP95Ms != 120.0 {
		t.Errorf("expected ThresholdP95Ms 120.0, got %f", res20.ThresholdP95Ms)
	}
	if !res20.Recovered {
		t.Errorf("expected 115ms to recover under 20%% tolerance (120ms)")
	}
}

func TestRTOResult_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	res := capacity.RTOResult{
		PreTestBaselineP95Ms: 50.0,
		ThresholdP95Ms:       55.0,
		TolerancePercent:     10.0,
		TerminationTime:      now,
		RecoveryTime:         now.Add(3 * time.Second),
		RecoveryDuration:     3 * time.Second,
		RecoveryDurationSec:  3,
		Recovered:            true,
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal RTOResult: %v", err)
	}

	var decoded capacity.RTOResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal RTOResult: %v", err)
	}

	if decoded.PreTestBaselineP95Ms != res.PreTestBaselineP95Ms ||
		decoded.ThresholdP95Ms != res.ThresholdP95Ms ||
		decoded.RecoveryDurationSec != res.RecoveryDurationSec ||
		decoded.Recovered != res.Recovered {
		t.Errorf("decoded result %+v does not match original %+v", decoded, res)
	}
}
