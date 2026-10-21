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

func TestEvent_AllEventTypes_Serialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	testTypes := []events.EventType{
		events.EventExperimentStarted,
		events.EventLoadChanged,
		events.EventMetricSnapshot,
		events.EventThresholdBreached,
		events.EventExperimentStopped,
		events.EventCompleted,
	}

	for _, evtType := range testTypes {
		t.Run(string(evtType), func(t *testing.T) {
			rawPayload := json.RawMessage(`{"step_rate":100,"reason":"test"}`)
			evt := events.Event{
				ID:           "evt-" + string(evtType),
				ExperimentID: "exp-456",
				Type:         evtType,
				Timestamp:    now,
				Service:      "payment-service",
				Payload:      rawPayload,
			}

			data, err := json.Marshal(evt)
			if err != nil {
				t.Fatalf("failed to marshal Event: %v", err)
			}

			// Verify JSON envelope schema keys
			var rawMap map[string]interface{}
			if err := json.Unmarshal(data, &rawMap); err != nil {
				t.Fatalf("failed to unmarshal JSON into map: %v", err)
			}

			if rawMap["id"] != evt.ID {
				t.Errorf("expected json 'id' to be %s, got %v", evt.ID, rawMap["id"])
			}
			if rawMap["experiment_id"] != evt.ExperimentID {
				t.Errorf("expected json 'experiment_id' to be %s, got %v", evt.ExperimentID, rawMap["experiment_id"])
			}
			if rawMap["type"] != string(evtType) {
				t.Errorf("expected json 'type' to be %s, got %v", evtType, rawMap["type"])
			}
			if rawMap["service"] != evt.Service {
				t.Errorf("expected json 'service' to be %s, got %v", evt.Service, rawMap["service"])
			}

			var decoded events.Event
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal Event: %v", err)
			}

			if decoded.ID != evt.ID {
				t.Errorf("expected ID %s, got %s", evt.ID, decoded.ID)
			}
			if decoded.Type != evtType {
				t.Errorf("expected Type %s, got %s", evtType, decoded.Type)
			}
			if string(decoded.Payload) != string(rawPayload) {
				t.Errorf("expected Payload %s, got %s", string(rawPayload), string(decoded.Payload))
			}
		})
	}
}

func TestEvent_UnmarshalJSON_LegacyFieldCompat(t *testing.T) {
	legacyJSON := []byte(`{
		"event_id": "legacy-evt-001",
		"experiment_id": "exp-legacy",
		"event_type": "THRESHOLD_BREACHED",
		"timestamp": "2026-10-21T12:00:00Z",
		"service": "checkout",
		"payload": {"p95": 650.0}
	}`)

	var evt events.Event
	if err := json.Unmarshal(legacyJSON, &evt); err != nil {
		t.Fatalf("failed to unmarshal legacy JSON into Event: %v", err)
	}

	if evt.ID != "legacy-evt-001" {
		t.Errorf("expected ID to fall back to event_id, got %s", evt.ID)
	}
	if evt.Type != events.EventThresholdBreached {
		t.Errorf("expected Type to fall back to event_type, got %s", evt.Type)
	}
	if evt.ExperimentID != "exp-legacy" {
		t.Errorf("expected ExperimentID exp-legacy, got %s", evt.ExperimentID)
	}
	if evt.Service != "checkout" {
		t.Errorf("expected Service checkout, got %s", evt.Service)
	}
}

func TestEvent_Validate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		event   events.Event
		wantErr bool
	}{
		{
			name: "valid event with payload",
			event: events.Event{
				ID:           "evt-valid",
				ExperimentID: "exp-1",
				Type:         events.EventExperimentStarted,
				Timestamp:    now,
				Service:      "frontend",
				Payload:      json.RawMessage(`{"concurrency":10}`),
			},
			wantErr: false,
		},
		{
			name: "valid event without payload",
			event: events.Event{
				ID:           "evt-no-payload",
				ExperimentID: "exp-1",
				Type:         events.EventCompleted,
				Timestamp:    now,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			event: events.Event{
				ExperimentID: "exp-1",
				Type:         events.EventCompleted,
				Timestamp:    now,
			},
			wantErr: true,
		},
		{
			name: "missing ExperimentID",
			event: events.Event{
				ID:        "evt-1",
				Type:      events.EventCompleted,
				Timestamp: now,
			},
			wantErr: true,
		},
		{
			name: "invalid event type",
			event: events.Event{
				ID:           "evt-1",
				ExperimentID: "exp-1",
				Type:         events.EventType("UNKNOWN_EVENT"),
				Timestamp:    now,
			},
			wantErr: true,
		},
		{
			name: "zero timestamp",
			event: events.Event{
				ID:           "evt-1",
				ExperimentID: "exp-1",
				Type:         events.EventCompleted,
			},
			wantErr: true,
		},
		{
			name: "malformed json payload",
			event: events.Event{
				ID:           "evt-1",
				ExperimentID: "exp-1",
				Type:         events.EventCompleted,
				Timestamp:    now,
				Payload:      json.RawMessage(`{not-json}`),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewEvent_Constructor(t *testing.T) {
	t.Run("with map payload", func(t *testing.T) {
		payload := map[string]interface{}{"rate": 500, "status": "ramping"}
		evt, err := events.NewEvent("exp-test", events.EventLoadChanged, "cartservice", payload)
		if err != nil {
			t.Fatalf("unexpected NewEvent error: %v", err)
		}

		if evt.ID == "" {
			t.Error("expected generated UUID, got empty ID")
		}
		if evt.ExperimentID != "exp-test" {
			t.Errorf("expected exp-test, got %s", evt.ExperimentID)
		}
		if evt.Type != events.EventLoadChanged {
			t.Errorf("expected LOAD_CHANGED, got %s", evt.Type)
		}
		if evt.Service != "cartservice" {
			t.Errorf("expected cartservice, got %s", evt.Service)
		}
		if evt.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
		if len(evt.Payload) == 0 {
			t.Error("expected non-empty payload")
		}
	})

	t.Run("with RawMessage payload", func(t *testing.T) {
		raw := json.RawMessage(`{"metric":"cpu","value":82.5}`)
		evt, err := events.NewEvent("exp-test", events.EventMetricSnapshot, "nodesvc", raw)
		if err != nil {
			t.Fatalf("unexpected NewEvent error: %v", err)
		}
		if string(evt.Payload) != string(raw) {
			t.Errorf("expected %s, got %s", string(raw), string(evt.Payload))
		}
	})

	t.Run("with nil payload", func(t *testing.T) {
		evt, err := events.NewEvent("exp-test", events.EventExperimentStopped, "nodesvc", nil)
		if err != nil {
			t.Fatalf("unexpected NewEvent error: %v", err)
		}
		if evt.Payload != nil {
			t.Errorf("expected nil payload, got %s", string(evt.Payload))
		}
	})

	t.Run("with invalid event type", func(t *testing.T) {
		_, err := events.NewEvent("exp-test", events.EventType("INVALID"), "nodesvc", nil)
		if err == nil {
			t.Error("expected error for invalid event type, got nil")
		}
	})
}

func TestEvent_DomainEvent_BidirectionalConversion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	evt := events.Event{
		ID:           "evt-conversion",
		ExperimentID: "exp-conv",
		Type:         events.EventThresholdBreached,
		Timestamp:    now,
		Service:      "checkout",
		Payload:      json.RawMessage(`{"threshold":500,"observed":620}`),
	}

	domainEvt, err := evt.ToDomainEvent()
	if err != nil {
		t.Fatalf("failed ToDomainEvent: %v", err)
	}

	if domainEvt.EventID != evt.ID {
		t.Errorf("expected EventID %s, got %s", evt.ID, domainEvt.EventID)
	}
	if domainEvt.EventType != evt.Type {
		t.Errorf("expected EventType %s, got %s", evt.Type, domainEvt.EventType)
	}
	if domainEvt.Payload["threshold"] != float64(500) {
		t.Errorf("expected threshold 500, got %v", domainEvt.Payload["threshold"])
	}

	convertedBack, err := domainEvt.ToEvent()
	if err != nil {
		t.Fatalf("failed ToEvent: %v", err)
	}

	if convertedBack.ID != evt.ID {
		t.Errorf("expected ID %s, got %s", evt.ID, convertedBack.ID)
	}
	if convertedBack.Type != evt.Type {
		t.Errorf("expected Type %s, got %s", evt.Type, convertedBack.Type)
	}
}
