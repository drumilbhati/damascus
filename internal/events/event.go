package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// EventType represents the category of a domain or telemetry event emitted across Kafka.
type EventType string

const (
	// Canonical domain event types
	EventExperimentStarted EventType = "EXPERIMENT_STARTED"
	EventLoadChanged       EventType = "LOAD_CHANGED"
	EventMetricSnapshot    EventType = "METRIC_SNAPSHOT"
	EventThresholdBreached EventType = "THRESHOLD_BREACHED"
	EventExperimentStopped EventType = "EXPERIMENT_STOPPED"
	EventCompleted         EventType = "COMPLETED"

	// Legacy event types retained for backward compatibility with earlier telemetry pipelines
	EventLoadStepChanged     EventType = "LOAD_STEP_CHANGED"
	EventSafetyStopTriggered EventType = "SAFETY_STOP_TRIGGERED"
	EventExperimentCompleted EventType = "EXPERIMENT_COMPLETED"
	EventExperimentAborted   EventType = "EXPERIMENT_ABORTED"
)

// ValidEventTypes defines the recognized set of domain event types across Damascus.
var ValidEventTypes = map[EventType]bool{
	EventExperimentStarted:   true,
	EventLoadChanged:         true,
	EventMetricSnapshot:      true,
	EventThresholdBreached:   true,
	EventExperimentStopped:   true,
	EventCompleted:           true,
	EventLoadStepChanged:     true,
	EventSafetyStopTriggered: true,
	EventExperimentCompleted: true,
	EventExperimentAborted:   true,
}

// IsValid reports whether the event type is one of the recognized domain event types.
func (t EventType) IsValid() bool {
	return ValidEventTypes[t]
}

// Event represents the canonical JSON event envelope published and consumed across Kafka topics.
type Event struct {
	ID           string          `json:"id"`
	ExperimentID string          `json:"experiment_id"`
	Type         EventType       `json:"type"`
	Timestamp    time.Time       `json:"timestamp"`
	Service      string          `json:"service,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
}

// Validate verifies that the Event contains all required fields and valid JSON payload.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("event id cannot be empty")
	}
	if strings.TrimSpace(e.ExperimentID) == "" {
		return errors.New("experiment id cannot be empty")
	}
	if !e.Type.IsValid() {
		return fmt.Errorf("invalid or unrecognized event type: %q", e.Type)
	}
	if e.Timestamp.IsZero() {
		return errors.New("event timestamp cannot be zero")
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return errors.New("event payload must be valid JSON")
	}
	return nil
}

// UnmarshalJSON unmarshals JSON data into an Event, supporting both standard
// ("id", "type") and legacy ("event_id", "event_type") envelope field names.
func (e *Event) UnmarshalJSON(data []byte) error {
	type rawEvent struct {
		ID           string          `json:"id"`
		EventID      string          `json:"event_id"`
		ExperimentID string          `json:"experiment_id"`
		Type         EventType       `json:"type"`
		EventType    EventType       `json:"event_type"`
		Timestamp    time.Time       `json:"timestamp"`
		Service      string          `json:"service,omitempty"`
		Payload      json.RawMessage `json:"payload,omitempty"`
	}

	var raw rawEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	id := raw.ID
	if id == "" {
		id = raw.EventID
	}
	evtType := raw.Type
	if evtType == "" {
		evtType = raw.EventType
	}

	e.ID = id
	e.ExperimentID = raw.ExperimentID
	e.Type = evtType
	e.Timestamp = raw.Timestamp
	e.Service = raw.Service
	e.Payload = raw.Payload
	return nil
}

// NewEvent constructs and validates a new Event with a unique UUID and current UTC timestamp.
func NewEvent(experimentID string, eventType EventType, service string, payload any) (Event, error) {
	var rawPayload json.RawMessage
	if payload != nil {
		switch p := payload.(type) {
		case json.RawMessage:
			if len(p) > 0 {
				if !json.Valid(p) {
					return Event{}, errors.New("payload RawMessage is not valid JSON")
				}
				rawPayload = p
			}
		case []byte:
			if len(p) > 0 {
				if !json.Valid(p) {
					return Event{}, errors.New("payload bytes are not valid JSON")
				}
				rawPayload = json.RawMessage(p)
			}
		case string:
			if p != "" {
				b := []byte(p)
				if json.Valid(b) {
					rawPayload = json.RawMessage(b)
				} else {
					marshaled, err := json.Marshal(p)
					if err != nil {
						return Event{}, fmt.Errorf("failed to marshal payload string: %w", err)
					}
					rawPayload = marshaled
				}
			}
		default:
			marshaled, err := json.Marshal(p)
			if err != nil {
				return Event{}, fmt.Errorf("failed to marshal payload: %w", err)
			}
			rawPayload = marshaled
		}
	}

	evt := Event{
		ID:           uuid.New().String(),
		ExperimentID: experimentID,
		Type:         eventType,
		Timestamp:    time.Now().UTC(),
		Service:      service,
		Payload:      rawPayload,
	}

	if err := evt.Validate(); err != nil {
		return Event{}, err
	}
	return evt, nil
}

// DomainEvent encapsulates an event broadcast over the experiment-events Kafka topic.
// Maintained for backward compatibility with existing consumer and producer pipelines.
type DomainEvent struct {
	EventID      string                 `json:"event_id"`
	ExperimentID string                 `json:"experiment_id"`
	EventType    EventType              `json:"event_type"`
	Timestamp    time.Time              `json:"timestamp"`
	Service      string                 `json:"service,omitempty"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
}

// UnmarshalJSON unmarshals JSON into DomainEvent, accepting both legacy and standard envelope names.
func (d *DomainEvent) UnmarshalJSON(data []byte) error {
	type rawDomainEvent struct {
		EventID      string                 `json:"event_id"`
		ID           string                 `json:"id"`
		ExperimentID string                 `json:"experiment_id"`
		EventType    EventType              `json:"event_type"`
		Type         EventType              `json:"type"`
		Timestamp    time.Time              `json:"timestamp"`
		Service      string                 `json:"service,omitempty"`
		Payload      map[string]interface{} `json:"payload,omitempty"`
	}

	var raw rawDomainEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	eventID := raw.EventID
	if eventID == "" {
		eventID = raw.ID
	}
	evtType := raw.EventType
	if evtType == "" {
		evtType = raw.Type
	}

	d.EventID = eventID
	d.ExperimentID = raw.ExperimentID
	d.EventType = evtType
	d.Timestamp = raw.Timestamp
	d.Service = raw.Service
	d.Payload = raw.Payload
	return nil
}

// ToDomainEvent converts an Event envelope to a DomainEvent struct.
func (e Event) ToDomainEvent() (DomainEvent, error) {
	var payloadMap map[string]interface{}
	if len(e.Payload) > 0 && string(e.Payload) != "null" {
		if err := json.Unmarshal(e.Payload, &payloadMap); err != nil {
			return DomainEvent{}, fmt.Errorf("failed to unmarshal Event payload to map: %w", err)
		}
	}
	return DomainEvent{
		EventID:      e.ID,
		ExperimentID: e.ExperimentID,
		EventType:    e.Type,
		Timestamp:    e.Timestamp,
		Service:      e.Service,
		Payload:      payloadMap,
	}, nil
}

// ToEvent converts a DomainEvent struct to an Event envelope.
func (d DomainEvent) ToEvent() (Event, error) {
	var rawPayload json.RawMessage
	if len(d.Payload) > 0 {
		marshaled, err := json.Marshal(d.Payload)
		if err != nil {
			return Event{}, fmt.Errorf("failed to marshal DomainEvent payload: %w", err)
		}
		rawPayload = marshaled
	}
	return Event{
		ID:           d.EventID,
		ExperimentID: d.ExperimentID,
		Type:         d.EventType,
		Timestamp:    d.Timestamp,
		Service:      d.Service,
		Payload:      rawPayload,
	}, nil
}
