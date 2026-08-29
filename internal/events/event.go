package events

import "time"

// EventType represents the category of a domain event emitted across Kafka.
type EventType string

const (
	EventExperimentStarted   EventType = "EXPERIMENT_STARTED"
	EventLoadStepChanged     EventType = "LOAD_STEP_CHANGED"
	EventSafetyStopTriggered EventType = "SAFETY_STOP_TRIGGERED"
	EventExperimentCompleted EventType = "EXPERIMENT_COMPLETED"
	EventExperimentAborted   EventType = "EXPERIMENT_ABORTED"
)

// DomainEvent encapsulates an event broadcast over the experiment-events Kafka topic.
type DomainEvent struct {
	EventID      string                 `json:"event_id"`
	ExperimentID string                 `json:"experiment_id"`
	EventType    EventType              `json:"event_type"`
	Timestamp    time.Time              `json:"timestamp"`
	Service      string                 `json:"service,omitempty"`
	Payload      map[string]interface{} `json:"payload,omitempty"`
}
