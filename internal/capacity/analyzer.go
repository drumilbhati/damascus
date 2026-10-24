package capacity

import (
	"math"
	"sort"

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

// aggregatedTier stores averaged metrics for a distinct load rate step.
type aggregatedTier struct {
	loadRate     int
	avgP95Ms     float64
	avgErrorRate float64
	minAvail     float64
	hasBreach    bool
}

// Analyze computes deterministic throughput limits:
// - MaximumTestedRate: Highest load rate attempted across observations.
// - MaximumSustainableRate: Highest load rate where P95 <= policy.MaxP95LatencyMs AND error_rate <= policy.MaxErrorRate.
// - DegradationRate: Load rate where the latency slope (delta latency / delta load rate) spikes > 50% over prior slope.
// - SafetyBoundaryRate: Lowest load rate where an SLA breach occurred (or MaximumSustainableRate if no breach).
// - RecoveryTimeSec: Post-stress recovery time in seconds (from zero load rate observations).
func (a *Analyzer) Analyze(observations []Observation, policy safety.SafetyPolicy) CapacityResult {
	if len(observations) == 0 {
		return CapacityResult{}
	}

	// 1. Compute MaximumTestedRate
	maxTestedRate := 0
	for _, obs := range observations {
		rate := int(math.Round(obs.LoadRate))
		if rate > maxTestedRate {
			maxTestedRate = rate
		}
	}

	// 2. Aggregate observations by load rate (grouping by rounded integer load rate)
	// We separate active load rate observations (> 0) from idle/recovery observations (== 0).
	type tierMetrics struct {
		totalP95   float64
		totalError float64
		minAvail   float64
		count      int
		anyBreach  bool
	}
	tiersMap := make(map[int]*tierMetrics)

	for _, obs := range observations {
		rate := int(math.Round(obs.LoadRate))
		if rate <= 0 {
			continue
		}

		breached := isSlaBreached(obs, policy)

		m, exists := tiersMap[rate]
		if !exists {
			m = &tierMetrics{
				minAvail: obs.Availability,
			}
			tiersMap[rate] = m
		}
		m.totalP95 += obs.P95LatencyMs
		m.totalError += obs.ErrorRate
		if obs.Availability < m.minAvail {
			m.minAvail = obs.Availability
		}
		m.count++
		if breached {
			m.anyBreach = true
		}
	}

	// Sort active rate tiers ascending
	rates := make([]int, 0, len(tiersMap))
	for r := range tiersMap {
		rates = append(rates, r)
	}
	sort.Ints(rates)

	var sortedTiers []aggregatedTier
	for _, r := range rates {
		m := tiersMap[r]
		sortedTiers = append(sortedTiers, aggregatedTier{
			loadRate:     r,
			avgP95Ms:     m.totalP95 / float64(m.count),
			avgErrorRate: m.totalError / float64(m.count),
			minAvail:     m.minAvail,
			hasBreach:    m.anyBreach,
		})
	}

	// 3. Compute MaximumSustainableRate & SafetyBoundaryRate
	maxSustainableRate := 0
	firstBreachRate := 0

	for _, tier := range sortedTiers {
		compliant := isTierCompliant(tier, policy)
		if compliant {
			if tier.loadRate > maxSustainableRate {
				maxSustainableRate = tier.loadRate
			}
		} else if firstBreachRate == 0 {
			firstBreachRate = tier.loadRate
		}
	}

	safetyBoundaryRate := maxSustainableRate
	if firstBreachRate > 0 {
		safetyBoundaryRate = firstBreachRate
	}

	// 4. Compute DegradationRate (latency knee point where slope spikes > 50%)
	degradationRate := calculateDegradationRate(sortedTiers)

	// 5. Compute RecoveryTimeSec (optional duration observed post-stress at 0 load rate until P95 drops to SLA)
	recoveryTimeSec := calculateRecoveryTime(observations, policy)

	return CapacityResult{
		MaximumTestedRate:      maxTestedRate,
		MaximumSustainableRate: maxSustainableRate,
		DegradationRate:        degradationRate,
		SafetyBoundaryRate:     safetyBoundaryRate,
		RecoveryTimeSec:        recoveryTimeSec,
	}
}

// isSlaBreached checks if an individual observation breached SLA thresholds.
func isSlaBreached(obs Observation, policy safety.SafetyPolicy) bool {
	if policy.MaxP95LatencyMs > 0 && obs.P95LatencyMs > policy.MaxP95LatencyMs {
		return true
	}
	if policy.MaxErrorRate > 0 && obs.ErrorRate > policy.MaxErrorRate {
		return true
	}
	if policy.MinAvailability > 0 && obs.Availability < policy.MinAvailability {
		return true
	}
	return false
}

// isTierCompliant checks if an aggregated tier satisfies the SLA policy.
func isTierCompliant(tier aggregatedTier, policy safety.SafetyPolicy) bool {
	if tier.hasBreach {
		return false
	}
	if policy.MaxP95LatencyMs > 0 && tier.avgP95Ms > policy.MaxP95LatencyMs {
		return false
	}
	if policy.MaxErrorRate > 0 && tier.avgErrorRate > policy.MaxErrorRate {
		return false
	}
	if policy.MinAvailability > 0 && tier.minAvail < policy.MinAvailability {
		return false
	}
	return true
}

// calculateDegradationRate finds the load rate where the latency slope increases by > 50% relative to the prior slope.
// Slope between tier i-1 and tier i is defined as: (Latency_i - Latency_{i-1}) / (LoadRate_i - LoadRate_{i-1}).
// A knee / degradation point occurs at tier i when: (Slope_i - Slope_{i-1}) / Slope_{i-1} > 0.50 (with Slope_{i-1} > 0).
func calculateDegradationRate(tiers []aggregatedTier) int {
	if len(tiers) < 3 {
		return 0
	}

	var slopes []float64
	for i := 1; i < len(tiers); i++ {
		deltaRate := float64(tiers[i].loadRate - tiers[i-1].loadRate)
		if deltaRate <= 0 {
			slopes = append(slopes, 0)
			continue
		}
		deltaLatency := tiers[i].avgP95Ms - tiers[i-1].avgP95Ms
		slope := deltaLatency / deltaRate
		slopes = append(slopes, slope)
	}

	// slopes[k] corresponds to interval between tiers[k] and tiers[k+1]
	// Knee detection compares slope[k] with slope[k-1] (for k >= 1)
	for k := 1; k < len(slopes); k++ {
		prevSlope := slopes[k-1]
		currSlope := slopes[k]

		if prevSlope > 0 {
			growthRatio := (currSlope - prevSlope) / prevSlope
			if growthRatio > 0.50 {
				// Degradation begins at tiers[k].loadRate (start of steeper slope interval)
				return tiers[k].loadRate
			}
		} else if prevSlope <= 0 && currSlope > 0 {
			// If previous slope was flat or negative and current slope spikes positively
			return tiers[k].loadRate
		}
	}

	return 0
}

// calculateRecoveryTime calculates post-stress recovery duration if zero-load observations exist.
func calculateRecoveryTime(observations []Observation, policy safety.SafetyPolicy) int {
	// Find post-stress phase where load rate drops back to 0
	var zeroLoadObs []Observation
	foundStress := false

	for _, obs := range observations {
		if obs.LoadRate > 0 {
			foundStress = true
		} else if foundStress && obs.LoadRate == 0 {
			zeroLoadObs = append(zeroLoadObs, obs)
		}
	}

	if len(zeroLoadObs) == 0 {
		return 0
	}

	startTime := zeroLoadObs[0].Timestamp
	for _, obs := range zeroLoadObs {
		if !isSlaBreached(obs, policy) {
			diff := obs.Timestamp.Sub(startTime)
			if diff > 0 {
				return int(math.Round(diff.Seconds()))
			}
			return 0
		}
	}

	// If it never fully recovered within observation window, return total window duration
	lastTime := zeroLoadObs[len(zeroLoadObs)-1].Timestamp
	return int(math.Round(lastTime.Sub(startTime).Seconds()))
}
