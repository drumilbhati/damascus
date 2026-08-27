package stress_test

import (
	"encoding/json"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestLoadPlan_JSONSerialization(t *testing.T) {
	plan := stress.LoadPlan{
		TargetURL:    "http://localhost:8080/cart",
		Method:       "POST",
		Payload:      []byte(`{"item_id":"123","quantity":1}`),
		InitialRate:  100,
		StepRate:     50,
		MaxRate:      1000,
		StepDuration: 10 * time.Second,
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
	if decoded.StepDuration != plan.StepDuration {
		t.Errorf("expected StepDuration %v, got %v", plan.StepDuration, decoded.StepDuration)
	}
}
