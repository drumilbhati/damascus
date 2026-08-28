package stress

import "fmt"

// LoadPlan defines the traffic generation recipe executed by the stress engine.
type LoadPlan struct {
	TargetURL           string `json:"target_url"`
	Method              string `json:"method"` // GET, POST, etc.
	Payload             string `json:"payload,omitempty"`
	InitialRate         int    `json:"initial_rate"`
	StepRate            int    `json:"step_rate"`
	MaxRate             int    `json:"max_rate"`
	StepDurationSeconds int    `json:"step_duration_seconds"`
}

// Validate ensures the LoadPlan has valid execution parameters.
func (p LoadPlan) Validate() error {
	if p.TargetURL == "" {
		return fmt.Errorf("target_url cannot be empty")
	}
	if p.InitialRate <= 0 {
		return fmt.Errorf("initial_rate must be greater than 0")
	}
	if p.MaxRate < p.InitialRate {
		return fmt.Errorf("max_rate (%d) cannot be less than initial_rate (%d)", p.MaxRate, p.InitialRate)
	}
	if p.InitialRate < p.MaxRate && p.StepRate <= 0 {
		return fmt.Errorf("step_rate must be greater than 0 when initial_rate is less than max_rate")
	}
	if p.StepRate < 0 {
		return fmt.Errorf("step_rate cannot be negative")
	}
	if p.StepDurationSeconds <= 0 {
		return fmt.Errorf("step_duration_seconds must be greater than 0")
	}
	return nil
}
