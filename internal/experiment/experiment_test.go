package experiment_test

import (
	"encoding/json"
	"testing"
	"time"

	"damascus/internal/experiment"
)

func TestExperimentConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     experiment.ExperimentConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: experiment.ExperimentConfig{
				InitialRate:         50,
				StepRate:            50,
				MaxRate:             500,
				StepDurationSeconds: 10,
				MaxP95LatencyMs:     200.0,
				MaxErrorRatePercent: 5.0,
				MinAvailabilityPct:  99.0,
				RecoveryWindowSec:   15,
			},
			wantErr: false,
		},
		{
			name: "invalid initial rate <= 0",
			cfg: experiment.ExperimentConfig{
				InitialRate:         0,
				MaxRate:             500,
				StepDurationSeconds: 10,
				MaxP95LatencyMs:     200.0,
			},
			wantErr: true,
		},
		{
			name: "max rate less than initial rate",
			cfg: experiment.ExperimentConfig{
				InitialRate:         100,
				MaxRate:             50,
				StepDurationSeconds: 10,
				MaxP95LatencyMs:     200.0,
			},
			wantErr: true,
		},
		{
			name: "step duration <= 0",
			cfg: experiment.ExperimentConfig{
				InitialRate:         50,
				MaxRate:             500,
				StepDurationSeconds: 0,
				MaxP95LatencyMs:     200.0,
			},
			wantErr: true,
		},
		{
			name: "max p95 latency <= 0",
			cfg: experiment.ExperimentConfig{
				InitialRate:         50,
				MaxRate:             500,
				StepDurationSeconds: 10,
				MaxP95LatencyMs:     0,
			},
			wantErr: true,
		},
		{
			name: "error rate percent > 100",
			cfg: experiment.ExperimentConfig{
				InitialRate:         50,
				MaxRate:             500,
				StepDurationSeconds: 10,
				MaxP95LatencyMs:     200.0,
				MaxErrorRatePercent: 120.0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExperiment_JSONMarshaling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	exp := experiment.Experiment{
		ID:            "exp-12345",
		TargetService: "checkout",
		TargetURL:     "http://localhost:8080/checkout",
		Type:          experiment.ExperimentStep,
		State:         experiment.StateCreated,
		Config: experiment.ExperimentConfig{
			InitialRate:         50,
			StepRate:            50,
			MaxRate:             500,
			StepDurationSeconds: 10,
			MaxP95LatencyMs:     250.0,
			MaxErrorRatePercent: 2.0,
			MinAvailabilityPct:  99.5,
			RecoveryWindowSec:   10,
		},
		CreatedAt: now,
	}

	data, err := json.Marshal(exp)
	if err != nil {
		t.Fatalf("failed to marshal experiment: %v", err)
	}

	var decoded experiment.Experiment
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal experiment: %v", err)
	}

	if decoded.ID != exp.ID {
		t.Errorf("expected ID %s, got %s", exp.ID, decoded.ID)
	}
	if decoded.StartedAt != nil {
		t.Errorf("expected StartedAt to be nil, got %v", decoded.StartedAt)
	}
	if decoded.IsTerminal() {
		t.Errorf("expected state CREATED to not be terminal")
	}

	// Test terminal state
	decoded.State = experiment.StateCompleted
	if !decoded.IsTerminal() {
		t.Errorf("expected state COMPLETED to be terminal")
	}

	decoded.State = experiment.StateAborted
	if !decoded.IsTerminal() {
		t.Errorf("expected state ABORTED to be terminal")
	}
}
