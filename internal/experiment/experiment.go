package experiment

import (
	"fmt"
	"time"
)

// ExperimentState represents the discrete lifecycle state of an experiment.
type ExperimentState string

const (
	StateCreated   ExperimentState = "CREATED"
	StatePlanned   ExperimentState = "PLANNED"
	StateRunning   ExperimentState = "RUNNING"
	StateStopping  ExperimentState = "STOPPING"
	StateAnalyzing ExperimentState = "ANALYZING"
	StateReporting ExperimentState = "REPORTING"
	StateCompleted ExperimentState = "COMPLETED"
	StateAborted   ExperimentState = "ABORTED"
)

// ExperimentType defines the load injection strategy.
type ExperimentType string

const (
	ExperimentRamp ExperimentType = "ramp"
	ExperimentStep ExperimentType = "step"
)

// ExperimentConfig defines the execution parameters and safety thresholds.
type ExperimentConfig struct {
	InitialRate         int     `json:"initial_rate"`
	StepRate            int     `json:"step_rate"`
	MaxRate             int     `json:"max_rate"`
	StepDurationSeconds int     `json:"step_duration_seconds"`
	MaxP95LatencyMs     float64 `json:"max_p95_latency_ms"`
	MaxErrorRatePercent float64 `json:"max_error_rate_percent"`
	MinAvailabilityPct  float64 `json:"min_availability_pct"`
	RecoveryWindowSec   int     `json:"recovery_window_sec"`
}

// Validate checks if the configuration has valid parameters.
func (c ExperimentConfig) Validate() error {
	if c.InitialRate <= 0 {
		return fmt.Errorf("initial_rate must be greater than 0")
	}
	if c.MaxRate < c.InitialRate {
		return fmt.Errorf("max_rate (%d) cannot be less than initial_rate (%d)", c.MaxRate, c.InitialRate)
	}
	if c.StepDurationSeconds <= 0 {
		return fmt.Errorf("step_duration_seconds must be greater than 0")
	}
	if c.MaxP95LatencyMs <= 0 {
		return fmt.Errorf("max_p95_latency_ms must be greater than 0")
	}
	if c.MaxErrorRatePercent < 0 || c.MaxErrorRatePercent > 100 {
		return fmt.Errorf("max_error_rate_percent must be between 0 and 100")
	}
	return nil
}

// Experiment encapsulates a chaos/stress test run and its execution lifecycle.
type Experiment struct {
	ID            string           `json:"id"`
	TargetService string           `json:"target_service"`
	TargetURL     string           `json:"target_url"`
	Type          ExperimentType   `json:"type"`
	State         ExperimentState  `json:"state"`
	Config        ExperimentConfig `json:"config"`
	StopReason    string           `json:"stop_reason,omitempty"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	EndedAt       *time.Time       `json:"ended_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
}

// IsTerminal returns true if the experiment has completed or aborted.
func (e *Experiment) IsTerminal() bool {
	return e.State == StateCompleted || e.State == StateAborted
}
