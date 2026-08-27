package safety_test

import (
	"encoding/json"
	"testing"

	"damascus/internal/safety"
)

func TestSafetyPolicy_JSONSerialization(t *testing.T) {
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: 300.0,
		MaxErrorRate:    0.05,
		MinAvailability: 0.99,
	}

	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("failed to marshal SafetyPolicy: %v", err)
	}

	var decoded safety.SafetyPolicy
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal SafetyPolicy: %v", err)
	}

	if decoded.MaxP95LatencyMs != policy.MaxP95LatencyMs {
		t.Errorf("expected MaxP95LatencyMs %f, got %f", policy.MaxP95LatencyMs, decoded.MaxP95LatencyMs)
	}
}
