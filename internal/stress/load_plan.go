package stress

import "time"

// LoadPlan defines the traffic generation recipe executed by the stress engine.
type LoadPlan struct {
	TargetURL    string        `json:"target_url"`
	Method       string        `json:"method"` // GET, POST, etc.
	Payload      []byte        `json:"payload,omitempty"`
	InitialRate  int           `json:"initial_rate"`
	StepRate     int           `json:"step_rate"`
	MaxRate      int           `json:"max_rate"`
	StepDuration time.Duration `json:"step_duration"`
}
