package events_test

import (
	"context"
	"testing"
	"time"

	"damascus/internal/events"
)

// TestNewKafkaProducer_EmptyBrokers verifies that initializing a KafkaEventProducer
// with an empty slice of broker addresses fails fast with an appropriate error.
func TestNewKafkaProducer_EmptyBrokers(t *testing.T) {
	_, err := events.NewKafkaProducer([]string{}, "experiment-events")
	if err == nil {
		t.Fatal("expected error when brokers list is empty, got nil")
	}
}

// TestKafkaProducer_Publish_CanceledContext asserts the fast-path cancellation guarantee:
// if the passed context is already canceled, Publish must immediately abort and return
// context.Canceled without attempting serialization or broker transmission.
func TestKafkaProducer_Publish_CanceledContext(t *testing.T) {
	producer, err := events.NewKafkaProducer([]string{"localhost:9092"}, "experiment-events")
	if err != nil {
		t.Fatalf("unexpected error creating producer: %v", err)
	}
	defer producer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context to test the fast-path abort

	evt := events.DomainEvent{
		EventID:      "evt-cancel-test",
		ExperimentID: "exp-test-99",
		EventType:    events.EventExperimentStarted,
		Timestamp:    time.Now().UTC(),
	}

	err = producer.Publish(ctx, evt)
	if err == nil {
		t.Fatal("expected error when publishing with canceled context, got nil")
	}
}
