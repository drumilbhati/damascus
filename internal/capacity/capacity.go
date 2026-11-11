package capacity

import (
	"encoding/json"
	"time"
)

// Observation captures a single time-series metric data point for capacity modeling.
type Observation struct {
	TelemetryMissing  bool      `json:"telemetry_missing,omitempty"`
	Phase             string    `json:"phase,omitempty"`
	SLAStatus         string    `json:"sla_status,omitempty"`
	Timestamp         time.Time `json:"timestamp"`
	LoadRate          float64   `json:"load_rate"`
	InjectedRate      *float64  `json:"injected_rate,omitempty"`
	P95LatencyMs      float64   `json:"p95_latency_ms"`
	ErrorRate         float64   `json:"error_rate"`
	Availability      float64   `json:"availability"`
	CPUUtilization    float64   `json:"cpu_utilization"`
	MemoryUtilization float64   `json:"memory_utilization"`
}

// CapacityResult represents the computed capacity metrics and thresholds from an experiment.
type CapacityResult struct {
	Measurements           Measurements `json:"measurements"`
	MaximumTestedRate      int          `json:"maximum_tested_rate"`
	PeakMeasuredRate       int          `json:"peak_measured_rate"`
	MaximumSustainableRate int          `json:"maximum_sustainable_rate"`
	DegradationRate        int          `json:"degradation_rate"`
	SafetyBoundaryRate     int          `json:"safety_boundary_rate"`
	RecoveryTimeSec        int          `json:"recovery_time_sec"`
	ObservedBreach         bool         `json:"observed_breach"`
	ObservedRecovery       bool         `json:"observed_recovery"`
}

// ExperimentReport compiles the complete findings, observations, and recommendations of an experiment.
type ExperimentReport struct {
	Measurements           Measurements  `json:"measurements"`
	StopReason             string        `json:"stop_reason,omitempty"`
	ExperimentID           string        `json:"experiment_id"`
	TargetService          string        `json:"target_service"`
	CriticalityScore       float64       `json:"criticality_score"` // Normalized 0.0 to 1.0
	MaximumTestedRate      int           `json:"maximum_tested_rate"`
	PeakMeasuredRate       int           `json:"peak_measured_rate"`
	MaximumSustainableRate int           `json:"maximum_sustainable_rate"`
	DegradationRate        int           `json:"degradation_rate"`
	SafetyBoundaryRate     int           `json:"safety_boundary_rate"`
	RecoveryTimeSec        int           `json:"recovery_time_sec"`
	ObservedBreach         bool          `json:"observed_breach"`
	ObservedRecovery       bool          `json:"observed_recovery"`
	Observations           []Observation `json:"observations"`
	Recommendations        []string      `json:"recommendations"`
	GeneratedAt            time.Time     `json:"generated_at"`
}

// OfferedRate is the configured injection rate for new runs. Older observations
// have no injection metadata and retain their historical interpretation.
func (o Observation) OfferedRate() float64 {
	if o.InjectedRate != nil {
		return *o.InjectedRate
	}
	return o.LoadRate
}

// UnmarshalJSON retains missing-field information for historical report data.
func (o *Observation) UnmarshalJSON(data []byte) error {
	type plain Observation
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*o = Observation(value)
	for _, key := range []string{"timestamp", "load_rate", "p95_latency_ms", "error_rate", "availability"} {
		v, ok := fields[key]
		if !ok || string(v) == "null" {
			o.TelemetryMissing = true
		}
	}
	return nil
}
