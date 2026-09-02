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

// Validate checks if the load plan has valid parameters.
func (p LoadPlan) Validate() error {
	if p.InitialRate <= 0 {
		return fmt.Errorf("initial_rate must be greater than 0")
	}
	if p.MaxRate < p.InitialRate {
		return fmt.Errorf("max_rate (%d) cannot be less than initial_rate (%d)", p.MaxRate, p.InitialRate)
	}
	if p.StepRate < 0 {
		return fmt.Errorf("step_rate cannot be negative")
	}
	if p.StepRate == 0 && p.MaxRate > p.InitialRate {
		return fmt.Errorf("step_rate must be greater than 0 when max_rate exceeds initial_rate")
	}
	if p.StepDurationSeconds <= 0 {
		return fmt.Errorf("step_duration_seconds must be greater than 0")
	}
	return nil
}

