package capacity

import (
	"errors"
	"math"
	"sort"
	"time"
)

// DefaultRTOTolerancePercentage is the standard allowable latency tolerance percentage (10%)
// above the pre-test baseline according to the Recovery Time Objective specification.
const DefaultRTOTolerancePercentage = 10.0

// Sentinel errors for RTO baseline calculation validation.
var (
	ErrEmptyObservations = errors.New("observations slice cannot be empty")
	ErrNoStressPhase     = errors.New("no active stress phase found (load rate > 0)")
	ErrNoLoadTermination = errors.New("no load termination observed after active stress phase")
	ErrNoPreTestBaseline = errors.New("no pre-test baseline observations found prior to stress")
	ErrInvalidBaseline   = errors.New("baseline P95 latency must be strictly greater than zero")
)

// RTOResult encapsulates the outcome of a Recovery Time Objective calculation.
type RTOResult struct {
	PreTestBaselineP95Ms float64       `json:"pre_test_baseline_p95_ms"`
	ThresholdP95Ms       float64       `json:"threshold_p95_ms"`
	TolerancePercent     float64       `json:"tolerance_percent"`
	TerminationTime      time.Time     `json:"termination_time"`
	RecoveryTime         time.Time     `json:"recovery_time,omitempty"`
	RecoveryDuration     time.Duration `json:"recovery_duration"`
	RecoveryDurationSec  int           `json:"recovery_duration_sec"`
	Recovered            bool          `json:"recovered"`
}

// RTOCalculator defines the contract for computing microservice recovery duration.
type RTOCalculator interface {
	CalculateRTO(observations []Observation) (*RTOResult, error)
	CalculateRTOWithBaseline(observations []Observation, baselineP95 float64) (*RTOResult, error)
}

// BaselineCalculator implements RTOCalculator with deterministic baseline threshold calculations.
type BaselineCalculator struct {
	TolerancePercentage float64
}

// NewBaselineCalculator initializes a BaselineCalculator with the specified or default tolerance.
func NewBaselineCalculator(tolerancePercentage ...float64) *BaselineCalculator {
	tolerance := DefaultRTOTolerancePercentage
	if len(tolerancePercentage) > 0 && tolerancePercentage[0] > 0 {
		tolerance = tolerancePercentage[0]
	}
	return &BaselineCalculator{
		TolerancePercentage: tolerance,
	}
}

// ExtractPreTestBaseline computes the baseline P95 latency from pre-stress observations (LoadRate == 0).
func ExtractPreTestBaseline(preStressObs []Observation) (float64, error) {
	if len(preStressObs) == 0 {
		return 0, ErrNoPreTestBaseline
	}

	sumLatencies := 0.0
	for _, obs := range preStressObs {
		sumLatencies += obs.P95LatencyMs
	}

	avg := sumLatencies / float64(len(preStressObs))
	if avg <= 0 {
		return 0, ErrInvalidBaseline
	}

	return avg, nil
}

// CalculateThreshold computes the allowable P95 latency threshold given a baseline and tolerance percentage.
func CalculateThreshold(baseline float64, tolerancePercent float64) float64 {
	raw := baseline * (1.0 + tolerancePercent/100.0)
	return math.Round(raw*1e9) / 1e9
}

// CalculateRTOWithBaseline computes RTO metrics for observations given an explicit pre-test baseline P95 latency.
func (c *BaselineCalculator) CalculateRTOWithBaseline(observations []Observation, baselineP95 float64) (*RTOResult, error) {
	if len(observations) == 0 {
		return nil, ErrEmptyObservations
	}
	if baselineP95 <= 0 {
		return nil, ErrInvalidBaseline
	}

	tolerance := c.TolerancePercentage
	if tolerance <= 0 {
		tolerance = DefaultRTOTolerancePercentage
	}

	threshold := CalculateThreshold(baselineP95, tolerance)

	// Sort a copy of observations chronologically to preserve caller's slice order
	obsCopy := make([]Observation, len(observations))
	copy(obsCopy, observations)
	sort.Slice(obsCopy, func(i, j int) bool {
		return obsCopy[i].Timestamp.Before(obsCopy[j].Timestamp)
	})

	// Partition observations into active stress and post-stress recovery
	foundStress := false
	var postStressObs []Observation

	for _, obs := range obsCopy {
		if obs.LoadRate > 0 {
			foundStress = true
		} else if foundStress && obs.LoadRate == 0 {
			postStressObs = append(postStressObs, obs)
		}
	}

	if !foundStress {
		return nil, ErrNoStressPhase
	}
	if len(postStressObs) == 0 {
		return nil, ErrNoLoadTermination
	}

	terminationTime := postStressObs[0].Timestamp

	var recovered bool
	var recoveryTime time.Time
	var recoveryDuration time.Duration

	for _, obs := range postStressObs {
		if obs.P95LatencyMs <= threshold {
			recovered = true
			recoveryTime = obs.Timestamp
			diff := obs.Timestamp.Sub(terminationTime)
			if diff > 0 {
				recoveryDuration = diff
			}
			break
		}
	}

	if !recovered {
		lastObs := postStressObs[len(postStressObs)-1]
		diff := lastObs.Timestamp.Sub(terminationTime)
		if diff > 0 {
			recoveryDuration = diff
		}
	}

	recoveryDurationSec := int(math.Round(recoveryDuration.Seconds()))

	return &RTOResult{
		PreTestBaselineP95Ms: baselineP95,
		ThresholdP95Ms:       threshold,
		TolerancePercent:     tolerance,
		TerminationTime:      terminationTime,
		RecoveryTime:         recoveryTime,
		RecoveryDuration:     recoveryDuration,
		RecoveryDurationSec:  recoveryDurationSec,
		Recovered:            recovered,
	}, nil
}

// CalculateRTO computes RTO metrics by automatically extracting the baseline from pre-stress observations.
func (c *BaselineCalculator) CalculateRTO(observations []Observation) (*RTOResult, error) {
	if len(observations) == 0 {
		return nil, ErrEmptyObservations
	}

	// Sort a copy chronologically to locate pre-stress observations
	obsCopy := make([]Observation, len(observations))
	copy(obsCopy, observations)
	sort.Slice(obsCopy, func(i, j int) bool {
		return obsCopy[i].Timestamp.Before(obsCopy[j].Timestamp)
	})

	// Collect observations before stress load begins
	var preStressObs []Observation
	for _, obs := range obsCopy {
		if obs.LoadRate > 0 {
			break
		}
		if obs.LoadRate == 0 {
			preStressObs = append(preStressObs, obs)
		}
	}

	baseline, err := ExtractPreTestBaseline(preStressObs)
	if err != nil {
		return nil, err
	}

	return c.CalculateRTOWithBaseline(observations, baseline)
}

// Package-level convenience helpers

// CalculateRTO calculates RTO using the default BaselineCalculator.
func CalculateRTO(observations []Observation) (*RTOResult, error) {
	return NewBaselineCalculator().CalculateRTO(observations)
}

// CalculateRTOWithBaseline calculates RTO with an explicit baseline using the default BaselineCalculator.
func CalculateRTOWithBaseline(observations []Observation, baselineP95 float64) (*RTOResult, error) {
	return NewBaselineCalculator().CalculateRTOWithBaseline(observations, baselineP95)
}
