package stress

import (
	"fmt"
	"time"
)

// LoadPlan defines the traffic generation recipe executed by the stress engine.
type LoadPlan struct {
	TargetURL           string `json:"target_url"`
	Method              string `json:"method"` // GET, POST, etc.
	Payload             string `json:"payload,omitempty"`
	Type                string `json:"type,omitempty"` // "step", "ramp", "fixed", "aimd"
	InitialRate         int    `json:"initial_rate"`
	StepRate            int    `json:"step_rate"`
	MaxRate             int    `json:"max_rate"`
	StepDurationSeconds int    `json:"step_duration_seconds"`

	// Requests is a weighted mix of developer-supplied operations. When empty,
	// the legacy TargetURL/Method/Payload fields describe a single operation.
	Requests []RequestSpec `json:"requests,omitempty"`

	// TraceEvery injects a sampled W3C traceparent into every Nth request so
	// the generated traces can be fetched from Jaeger by ID. Zero disables it.
	TraceEvery int `json:"trace_every,omitempty"`

	// OnTrace receives each tagged request after the target responded.
	OnTrace func(TraceRecord) `json:"-"`
}

// TraceRecord links a generated trace ID back to the request that produced it.
type TraceRecord struct {
	TraceID    string
	Request    string
	Rate       int
	StatusCode int
	Latency    time.Duration
	SentAt     time.Time
}

// MaxStepDurationSeconds is the maximum duration in seconds that fits within a time.Duration without overflow (int64 nanoseconds).
const MaxStepDurationSeconds = 9223372036

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
	if p.StepDurationSeconds > MaxStepDurationSeconds {
		return fmt.Errorf("step_duration_seconds (%d) cannot exceed %d", p.StepDurationSeconds, MaxStepDurationSeconds)
	}
	if p.TraceEvery < 0 {
		return fmt.Errorf("trace_every cannot be negative")
	}
	for _, spec := range p.Requests {
		if err := spec.Validate(); err != nil {
			return err
		}
	}
	return nil
}
