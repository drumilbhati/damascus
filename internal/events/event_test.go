package events_test

import (
	"encoding/json"
	"testing"
	"time"

	"damascus/internal/events"
)

func TestDomainEvent_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	evt := events.DomainEvent{
		EventID:      "evt-001",
		ExperimentID: "exp-123",
		EventType:    events.EventSafetyStopTriggered,
		Timestamp:    now,
		Service:      "checkout",
		Payload: map[string]interface{}{
			"breached_metric": "p95_latency_ms",
			"observed_value":  620.5,
			"threshold_value": 500.0,
			"reason":          "P95 latency breached SLA: 620.50 ms > 500.00 ms",
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal DomainEvent: %v", err)
	}

	var decoded events.DomainEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal DomainEvent: %v", err)
	}

	if decoded.EventID != evt.EventID {
		t.Errorf("expected EventID %s, got %s", evt.EventID, decoded.EventID)
	}
	if decoded.EventType != events.EventSafetyStopTriggered {
		t.Errorf("expected EventType %s, got %s", events.EventSafetyStopTriggered, decoded.EventType)
	}
	if decoded.Payload["breached_metric"] != "p95_latency_ms" {
		t.Errorf("expected payload key breached_metric, got %v", decoded.Payload["breached_metric"])
	}
}
