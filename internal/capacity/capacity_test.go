package capacity_test

import (
	"encoding/json"
	"testing"
	"time"

	"damascus/internal/capacity"
)

func TestCapacityResult_JSONSerialization(t *testing.T) {
	result := capacity.CapacityResult{
		MaximumTestedRate:      1000,
		MaximumSustainableRate: 750,
		DegradationRate:        600,
		SafetyBoundaryRate:     800,
		RecoveryTimeSec:        4,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal CapacityResult: %v", err)
	}

	var decoded capacity.CapacityResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal CapacityResult: %v", err)
	}

	if decoded.RecoveryTimeSec != result.RecoveryTimeSec {
		t.Errorf("expected RecoveryTimeSec %d, got %d", result.RecoveryTimeSec, decoded.RecoveryTimeSec)
	}
}

func TestExperimentReport_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	report := capacity.ExperimentReport{
		ExperimentID:           "exp-999",
		TargetService:          "cart",
		CriticalityScore:       0.92,
		MaximumTestedRate:      1000,
		MaximumSustainableRate: 750,
		DegradationRate:        600,
		SafetyBoundaryRate:     800,
		RecoveryTimeSec:        4,
		Observations: []capacity.Observation{
			{
				Timestamp:         now,
				LoadRate:          500.0,
				P95LatencyMs:      85.0,
				ErrorRate:         0.0,
				Availability:      1.0,
				CPUUtilization:    45.0,
				MemoryUtilization: 55.0,
			},
		},
		Recommendations: []string{
			"Increase Valkey connection pool size above 50",
			"Enable gRPC connection pooling on frontend -> cart client",
		},
		GeneratedAt: now,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal ExperimentReport: %v", err)
	}

	var decoded capacity.ExperimentReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ExperimentReport: %v", err)
	}

	if decoded.CriticalityScore != report.CriticalityScore {
		t.Errorf("expected CriticalityScore %f, got %f", report.CriticalityScore, decoded.CriticalityScore)
	}
	if len(decoded.Observations) != 1 {
		t.Errorf("expected 1 observation, got %d", len(decoded.Observations))
	}
	if decoded.Observations[0].LoadRate != 500.0 {
		t.Errorf("expected observation LoadRate 500.0, got %f", decoded.Observations[0].LoadRate)
	}
	if decoded.RecoveryTimeSec != report.RecoveryTimeSec {
		t.Errorf("expected RecoveryTimeSec %d, got %d", report.RecoveryTimeSec, decoded.RecoveryTimeSec)
	}
}
