package capacity

import (
	"damascus/internal/safety"
	"math"
	"sort"
	"time"
)

// Measurements separates directly observed values from optional estimates.
// Rates are service totals (including background traffic), not generator counts.
type Measurements struct {
	Version                 int      `json:"version"`
	SampleCount             int      `json:"sample_count"`
	InvalidSampleCount      int      `json:"invalid_sample_count"`
	SummaryScope            string   `json:"summary_scope"`
	MetricWindowSec         int      `json:"metric_window_sec"`
	PeakMeasuredRate        *float64 `json:"peak_measured_rate"`
	PeakOfferedRate         *float64 `json:"peak_offered_rate"`
	PeakP95LatencyMs        *float64 `json:"peak_p95_latency_ms"`
	PeakErrorRate           *float64 `json:"peak_error_rate"`
	MinAvailability         *float64 `json:"min_availability"`
	CapacityStatus          string   `json:"capacity_status"`
	CapacityReason          string   `json:"capacity_reason"`
	SustainableMeasuredRate *float64 `json:"sustainable_measured_rate"`
	SustainableOfferedRate  *float64 `json:"sustainable_offered_rate"`
	BreachStatus            string   `json:"breach_status"`
	FirstBreachMeasuredRate *float64 `json:"first_breach_measured_rate"`
	FirstBreachOfferedRate  *float64 `json:"first_breach_offered_rate"`
	RecoveryStatus          string   `json:"recovery_status"`
	RecoverySeconds         *float64 `json:"recovery_seconds"`
	RecoveryWindowSeconds   *float64 `json:"recovery_window_seconds"`
}

func number(v float64) *float64 { return &v }
func finite(v float64) bool     { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// ValidObservation rejects absent timestamps, incomplete JSON telemetry, and
// non-finite/out-of-range values; missing measurements never become SLA passes.
func ValidObservation(o Observation) bool {
	return !o.TelemetryMissing && !o.Timestamp.IsZero() && finite(o.LoadRate) && o.LoadRate >= 0 &&
		finite(o.P95LatencyMs) && o.P95LatencyMs >= 0 && finite(o.ErrorRate) && o.ErrorRate >= 0 && o.ErrorRate <= 1 &&
		finite(o.Availability) && o.Availability >= 0 && o.Availability <= 1 &&
		(o.InjectedRate == nil || (finite(*o.InjectedRate) && *o.InjectedRate >= 0))
}

func Phase(o Observation) string {
	if o.InjectedRate == nil {
		return "unknown"
	}
	if *o.InjectedRate > 0 {
		return "injection"
	}
	return "idle/recovery"
}

func SLAStatus(o Observation, policy safety.SafetyPolicy) string {
	if !ValidObservation(o) {
		return "unavailable"
	}
	if isSlaBreached(o, policy) {
		return "breach"
	}
	return "within_limits"
}

func measure(observations []Observation, policy safety.SafetyPolicy) Measurements {
	m := Measurements{Version: 2, SampleCount: len(observations), MetricWindowSec: 30,
		SummaryScope: "injection", CapacityStatus: "not_established", BreachStatus: "not_observed", RecoveryStatus: "not_observed",
		CapacityReason: "No continuous 30-second load level with valid, SLA-compliant measurements was observed. Use longer steps; the service rate uses a trailing 30-second window."}
	obs := append([]Observation(nil), observations...)
	sort.SliceStable(obs, func(i, j int) bool { return obs[i].Timestamp.Before(obs[j].Timestamp) })
	hasInjection := false
	for _, o := range obs {
		if o.InjectedRate != nil && *o.InjectedRate > 0 {
			hasInjection = true
		}
	}
	if !hasInjection {
		m.SummaryScope = "recorded_samples_phase_unknown"
	}
	var start, last time.Time
	var offered float64
	validCount := 0
	lastActive := -1
	for i, o := range obs {
		if !ValidObservation(o) {
			m.InvalidSampleCount++
			start = time.Time{}
			last = time.Time{}
			continue
		}
		validCount++
		if o.InjectedRate != nil {
			if m.PeakOfferedRate == nil || *o.InjectedRate > *m.PeakOfferedRate {
				m.PeakOfferedRate = number(*o.InjectedRate)
			}
			if *o.InjectedRate > 0 {
				lastActive = i
			}
		}
		if (hasInjection && o.InjectedRate != nil && *o.InjectedRate > 0) || (!hasInjection && o.InjectedRate == nil) {
			if m.PeakMeasuredRate == nil || o.LoadRate > *m.PeakMeasuredRate {
				m.PeakMeasuredRate = number(o.LoadRate)
			}
			if m.PeakP95LatencyMs == nil || o.P95LatencyMs > *m.PeakP95LatencyMs {
				m.PeakP95LatencyMs = number(o.P95LatencyMs)
			}
			if m.PeakErrorRate == nil || o.ErrorRate > *m.PeakErrorRate {
				m.PeakErrorRate = number(o.ErrorRate)
			}
			if m.MinAvailability == nil || o.Availability < *m.MinAvailability {
				m.MinAvailability = number(o.Availability)
			}
		}
		breached := isSlaBreached(o, policy)
		if breached && m.BreachStatus != "observed" {
			m.BreachStatus = "observed"
			m.FirstBreachMeasuredRate = number(o.LoadRate)
			if o.InjectedRate != nil {
				m.FirstBreachOfferedRate = number(*o.InjectedRate)
			}
		}
		// A gap of more than two poll periods breaks the evidence window. Checking
		// at its end aligns the 30-second Prometheus rate with a constant target.
		if o.InjectedRate == nil || *o.InjectedRate <= 0 || breached {
			start, last = time.Time{}, time.Time{}
			continue
		}
		if start.IsZero() || offered != *o.InjectedRate || o.Timestamp.Sub(last) > 10*time.Second || !o.Timestamp.After(last) {
			start = o.Timestamp
			offered = *o.InjectedRate
		}
		last = o.Timestamp
		if o.Timestamp.Sub(start) >= 30*time.Second && o.LoadRate > 0 && (m.SustainableMeasuredRate == nil || o.LoadRate > *m.SustainableMeasuredRate) {
			m.CapacityStatus = "established"
			m.CapacityReason = "Estimated observed service throughput from a complete 30-second window at a constant offered target, with all sampled SLA checks passing. Includes background traffic; this is not an indefinite capacity guarantee."
			m.SustainableMeasuredRate = number(o.LoadRate)
			m.SustainableOfferedRate = number(offered)
		}
	}
	if validCount == 0 {
		m.BreachStatus = "unavailable"
		m.CapacityReason = "No valid telemetry was recorded."
	} else if !hasInjection {
		m.CapacityReason = "Injection metadata is unavailable; measured traffic is shown, but load phases and sustainable capacity cannot be established."
	}
	if m.BreachStatus == "not_observed" {
		m.RecoveryStatus = "not_needed"
	}
	// Recovery starts at the first explicit zero-injection sample after the final
	// active sample. Never substitute the observation-window length for recovery.
	if lastActive >= 0 && m.BreachStatus == "observed" {
		var recoveryStart, recoveryLast time.Time
		breachSeen := false
		for i, o := range obs {
			if !ValidObservation(o) {
				continue
			}
			if isSlaBreached(o, policy) {
				breachSeen = true
			}
			if i <= lastActive || o.InjectedRate == nil || *o.InjectedRate != 0 {
				continue
			}
			if recoveryStart.IsZero() {
				recoveryStart = o.Timestamp
			}
			recoveryLast = o.Timestamp
			if breachSeen && !isSlaBreached(o, policy) {
				m.RecoveryStatus = "recovered"
				m.RecoverySeconds = number(o.Timestamp.Sub(recoveryStart).Seconds())
				break
			}
		}
		if !recoveryStart.IsZero() {
			m.RecoveryWindowSeconds = number(recoveryLast.Sub(recoveryStart).Seconds())
			if m.RecoverySeconds == nil {
				m.RecoveryStatus = "not_recovered"
			}
		}
	}
	return m
}
