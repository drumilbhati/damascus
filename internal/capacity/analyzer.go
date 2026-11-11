package capacity

import (
	"math"

	"damascus/internal/safety"
)

// CapacityAnalyzer defines the interface for computing capacity thresholds from experiment telemetry.
type CapacityAnalyzer interface {
	Analyze(observations []Observation, policy safety.SafetyPolicy) CapacityResult
}

// Analyzer implements CapacityAnalyzer with deterministic algorithms.
type Analyzer struct{}

// NewAnalyzer creates a new Analyzer instance.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Analyze reports service measurements separately from offered load. Legacy
// integer fields remain for storage compatibility; consumers use Measurements
// for nullable, fractional values and explicit evidence states.
func (a *Analyzer) Analyze(observations []Observation, policy safety.SafetyPolicy) CapacityResult {
	m := measure(observations, policy)
	r := CapacityResult{Measurements: m, ObservedBreach: m.BreachStatus == "observed", ObservedRecovery: m.RecoveryStatus == "recovered"}
	if m.PeakOfferedRate != nil {
		r.MaximumTestedRate = int(math.Round(*m.PeakOfferedRate))
	}
	if m.PeakMeasuredRate != nil {
		r.PeakMeasuredRate = int(math.Round(*m.PeakMeasuredRate))
	}
	if m.SustainableMeasuredRate != nil {
		r.MaximumSustainableRate = int(math.Round(*m.SustainableMeasuredRate))
	}
	if m.FirstBreachMeasuredRate != nil {
		r.SafetyBoundaryRate = int(math.Round(*m.FirstBreachMeasuredRate))
	}
	if m.RecoverySeconds != nil {
		r.RecoveryTimeSec = int(math.Round(*m.RecoverySeconds))
	}
	return r
}

// isSlaBreached checks if an individual observation breached SLA thresholds.
func isSlaBreached(obs Observation, policy safety.SafetyPolicy) bool {
	if policy.MaxP95LatencyMs > 0 && obs.P95LatencyMs > policy.MaxP95LatencyMs {
		return true
	}
	if policy.MaxErrorRate >= 0 && obs.ErrorRate > policy.MaxErrorRate {
		return true
	}
	if policy.MinAvailability > 0 && obs.Availability < policy.MinAvailability {
		return true
	}
	return false
}
