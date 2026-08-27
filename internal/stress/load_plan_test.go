package stress_test

import (
	"encoding/json"
	"testing"

	"damascus/internal/stress"
)

func TestLoadPlan_JSONSerialization(t *testing.T) {
	plan := stress.LoadPlan{
		TargetURL:           "http://localhost:8080/cart",
		Method:              "POST",
		Payload:             `{"item_id":"123","quantity":1}`,
		InitialRate:         100,
		StepRate:            50,
		MaxRate:             1000,
		StepDurationSeconds: 10,
	}

	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("failed to marshal LoadPlan: %v", err)
	}

	var decoded stress.LoadPlan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal LoadPlan: %v", err)
	}

	if decoded.TargetURL != plan.TargetURL {
		t.Errorf("expected TargetURL %s, got %s", plan.TargetURL, decoded.TargetURL)
	}
	if decoded.Payload != plan.Payload {
		t.Errorf("expected Payload %s, got %s", plan.Payload, decoded.Payload)
	}
	if decoded.StepDurationSeconds != plan.StepDurationSeconds {
		t.Errorf("expected StepDurationSeconds %d, got %d", plan.StepDurationSeconds, decoded.StepDurationSeconds)
	}
}
