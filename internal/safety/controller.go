package safety

import (
	"fmt"
	"math"

	"damascus/internal/watcher"
)

// SafetyController evaluates real-time metric snapshots against configured SLA safety boundaries.
type SafetyController struct {
	policy SafetyPolicy
}

// NewSafetyController instantiates a new safety evaluation controller with the given policy.
func NewSafetyController(policy SafetyPolicy) *SafetyController {
	return &SafetyController{
		policy: policy,
	}
}

// Evaluate analyzes a real-time MetricSnapshot and decides whether the experiment must be halted immediately.
func (c *SafetyController) Evaluate(snapshot watcher.MetricSnapshot) SafetyDecision {
	// 1. Check P95 Latency SLA Threshold
	if c.policy.MaxP95LatencyMs > 0 {
		if math.IsNaN(snapshot.P95LatencyMs) || math.IsInf(snapshot.P95LatencyMs, 0) {
			return SafetyDecision{
				ShouldStop: true,
				Reason:     "P95 latency metric is non-finite (NaN or Inf)",
			}
		}
		if snapshot.P95LatencyMs > c.policy.MaxP95LatencyMs {
			return SafetyDecision{
				ShouldStop: true,
				Reason:     fmt.Sprintf("P95 latency breached SLA: %.2f ms > %.2f ms", snapshot.P95LatencyMs, c.policy.MaxP95LatencyMs),
			}
		}
	}

	// 2. Check Error Rate SLA Threshold
	if c.policy.MaxErrorRate > 0 {
		if math.IsNaN(snapshot.ErrorRate) || math.IsInf(snapshot.ErrorRate, 0) {
			return SafetyDecision{
				ShouldStop: true,
				Reason:     "Error rate metric is non-finite (NaN or Inf)",
			}
		}
		if snapshot.ErrorRate > c.policy.MaxErrorRate {
			return SafetyDecision{
				ShouldStop: true,
				Reason:     fmt.Sprintf("Error rate breached SLA: %.2f%% > %.2f%%", snapshot.ErrorRate*100, c.policy.MaxErrorRate*100),
			}
		}
	}

	// 3. Check Minimum Availability SLA Threshold
	if c.policy.MinAvailability > 0 {
		if math.IsNaN(snapshot.Availability) || math.IsInf(snapshot.Availability, 0) {
			return SafetyDecision{
				ShouldStop: true,
				Reason:     "Availability metric is non-finite (NaN or Inf)",
			}
		}
		if snapshot.Availability < c.policy.MinAvailability {
			return SafetyDecision{
				ShouldStop: true,
				Reason:     fmt.Sprintf("Availability breached SLA: %.2f%% < %.2f%%", snapshot.Availability*100, c.policy.MinAvailability*100),
			}
		}
	}

	// All metrics within acceptable SLA limits
	return SafetyDecision{
		ShouldStop: false,
	}
}
