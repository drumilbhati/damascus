package safety

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"damascus/internal/experiment"
	"damascus/internal/watcher"
)

// Metric names monitored during experiment execution.
const (
	MetricP95Latency = "p95_latency_ms"
	MetricErrorRate  = "error_rate"
	MetricAvail      = "availability"
)

// Safety action constants.
const (
	ActionSafetyStopTriggered = "SAFETY_STOP_TRIGGERED"
	ActionMonitoringHealthy   = "MONITORING_HEALTHY"
)

// SafetyAuditRecord captures structured audit log entries for safety evaluations.
type SafetyAuditRecord struct {
	Timestamp      time.Time      `json:"timestamp"`
	ExperimentID   string         `json:"experiment_id"`
	TargetService  string         `json:"target_service"`
	BreachedMetric string         `json:"breached_metric,omitempty"`
	ObservedValue  float64        `json:"observed_value,omitempty"`
	ThresholdValue float64        `json:"threshold_value,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	Decision       SafetyDecision `json:"decision"`
	Action         string         `json:"action"`
}

// Controller evaluates real-time metric snapshots and records safety audit entries.
type Controller struct {
	mu       sync.RWMutex
	auditLog []SafetyAuditRecord
	logger   *slog.Logger
}

// NewController initializes a new SafetyController instance.
func NewController(logger *slog.Logger) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		auditLog: make([]SafetyAuditRecord, 0),
		logger:   logger,
	}
}

// FormatP95Breach creates a human-readable reason string for a P95 latency SLA breach.
// Expected format: "P95 latency breached SLA: %.2f ms > %.2f ms"
// Example: "P95 latency breached SLA: 620.50 ms > 500.00 ms"
func FormatP95Breach(observed, threshold float64) string {
	return fmt.Sprintf("P95 latency breached SLA: %.2f ms > %.2f ms", observed, threshold)
}

// FormatErrorRateBreach creates a human-readable reason string for an error rate SLA breach.
// Expected format: "Error rate breached SLA: %.2f%% > %.2f%%"
// Note: observed and threshold are ratios (e.g. 0.05 is 5.00%). Multiply by 100 for display.
// Example: "Error rate breached SLA: 8.50% > 5.00%"
func FormatErrorRateBreach(observed, threshold float64) string {
	return fmt.Sprintf("Error rate breached SLA: %.2f%% > %.2f%%", observed*100, threshold*100)
}

// FormatAvailabilityBreach creates a human-readable reason string for an availability SLA breach.
// Expected format: "Availability breached SLA: %.2f%% < %.2f%%"
// Note: observed and threshold are ratios (e.g. 0.99 is 99.00%). Multiply by 100 for display.
// Example: "Availability breached SLA: 97.50% < 99.00%"
func FormatAvailabilityBreach(observed, threshold float64) string {
	return fmt.Sprintf("Availability breached SLA: %.2f%% < %.2f%%", observed*100, threshold*100)
}

// Evaluate inspects a real-time MetricSnapshot against the provided SafetyPolicy.
// If any threshold is breached, it constructs a stop decision, formats the reason,
// logs an audit entry, and returns ShouldStop = true.
func (c *Controller) Evaluate(ctx context.Context, snapshot watcher.MetricSnapshot, policy SafetyPolicy) SafetyDecision {
	// 1. Check P95 Latency Breach
	if policy.MaxP95LatencyMs > 0 && snapshot.P95LatencyMs > policy.MaxP95LatencyMs {
		reason := FormatP95Breach(snapshot.P95LatencyMs, policy.MaxP95LatencyMs)
		c.logAndRecord(snapshot, MetricP95Latency, snapshot.P95LatencyMs, policy.MaxP95LatencyMs, reason, true)
		return SafetyDecision{
			ShouldStop: true,
			Reason:     reason,
		}
	}

	// 2. Check Error Rate Breach
	if policy.MaxErrorRate > 0 && snapshot.ErrorRate > policy.MaxErrorRate {
		reason := FormatErrorRateBreach(snapshot.ErrorRate, policy.MaxErrorRate)
		c.logAndRecord(snapshot, MetricErrorRate, snapshot.ErrorRate, policy.MaxErrorRate, reason, true)
		return SafetyDecision{
			ShouldStop: true,
			Reason:     reason,
		}
	}

	// 3. Check Availability Breach
	if policy.MinAvailability > 0 && snapshot.Availability < policy.MinAvailability {
		reason := FormatAvailabilityBreach(snapshot.Availability, policy.MinAvailability)
		c.logAndRecord(snapshot, MetricAvail, snapshot.Availability, policy.MinAvailability, reason, true)
		return SafetyDecision{
			ShouldStop: true,
			Reason:     reason,
		}
	}

	// All metrics within acceptable thresholds
	c.logAndRecord(snapshot, "", 0, 0, "", false)
	return SafetyDecision{ShouldStop: false}
}

// logAndRecord is an internal helper that logs via slog and safely stores an audit entry.
func (c *Controller) logAndRecord(snapshot watcher.MetricSnapshot, metric string, observed, threshold float64, reason string, stopped bool) {
	record := SafetyAuditRecord{
		Timestamp:      time.Now().UTC(),
		ExperimentID:   snapshot.ExperimentID,
		TargetService:  snapshot.TargetService,
		BreachedMetric: metric,
		ObservedValue:  observed,
		ThresholdValue: threshold,
		Reason:         reason,
		Decision:       SafetyDecision{ShouldStop: stopped, Reason: reason},
		Action:         ActionMonitoringHealthy,
	}

	if stopped {
		record.Action = ActionSafetyStopTriggered
		c.logger.Warn("safety threshold breached",
			slog.String("experiment_id", snapshot.ExperimentID),
			slog.String("target_service", snapshot.TargetService),
			slog.String("breached_metric", metric),
			slog.Float64("observed_value", observed),
			slog.Float64("threshold_value", threshold),
			slog.String("reason", reason),
		)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.auditLog = append(c.auditLog, record)
}

// GetAuditHistory returns a copy of all audit records recorded so far.
func (c *Controller) GetAuditHistory() []SafetyAuditRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]SafetyAuditRecord, len(c.auditLog))
	copy(result, c.auditLog)
	return result
}

// ClearAuditHistory resets the in-memory audit log history.
func (c *Controller) ClearAuditHistory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.auditLog = make([]SafetyAuditRecord, 0)
}

// ApplySafetyDecision updates an experiment's StopReason and transitions its state
// if the decision indicates that execution must halt.
// It returns true if a stop was applied, false otherwise.
func ApplySafetyDecision(exp *experiment.Experiment, decision SafetyDecision) bool {
	if decision.ShouldStop {
		exp.StopReason = decision.Reason
		if exp.State == experiment.StateRunning {
			exp.State = experiment.StateStopping
		}
		return true
	}

	return false
}
