package watcher_test

import (
	"encoding/json"
	"testing"
	"time"

	"damascus/internal/watcher"
)

func TestMetricSnapshot_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := watcher.MetricSnapshot{
		ExperimentID:      "exp-001",
		TargetService:     "payment",
		Timestamp:         now,
		RequestRate:       250.5,
		P50LatencyMs:      12.4,
		P95LatencyMs:      45.8,
		P99LatencyMs:      120.0,
		ErrorRate:         0.01,
		Availability:      0.99,
		CPUUtilization:    42.5,
		MemoryUtilization: 68.2,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("failed to marshal MetricSnapshot: %v", err)
	}

	var decoded watcher.MetricSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal MetricSnapshot: %v", err)
	}

	if decoded.ExperimentID != snapshot.ExperimentID {
		t.Errorf("expected ExperimentID %s, got %s", snapshot.ExperimentID, decoded.ExperimentID)
	}
	if decoded.P95LatencyMs != snapshot.P95LatencyMs {
		t.Errorf("expected P95LatencyMs %f, got %f", snapshot.P95LatencyMs, decoded.P95LatencyMs)
	}
}
