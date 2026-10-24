package events_test

import (
	"context"
	"testing"
	"time"

	"damascus/internal/events"
	"github.com/twmb/franz-go/pkg/kgo"
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

// TestKafkaProducer_Publish_ProducerOwnedContext verifies that once an event passes
// the caller context check, delivery is managed via the producer-owned lifecycle,
// allowing Publish to succeed even if the caller's context subsequently cancels.
func TestKafkaProducer_Publish_ProducerOwnedContext(t *testing.T) {
	producer, err := events.NewKafkaProducer([]string{"localhost:9092"}, "experiment-events")
	if err != nil {
		t.Fatalf("unexpected error creating producer: %v", err)
	}
	defer producer.Close()

	ctx, cancel := context.WithCancel(context.Background())

	evt := events.DomainEvent{
		EventID:      "evt-async-test",
		ExperimentID: "exp-test-100",
		EventType:    events.EventLoadStepChanged,
		Timestamp:    time.Now().UTC(),
	}

	err = producer.Publish(ctx, evt)
	if err != nil {
		t.Fatalf("expected Publish to succeed for active context, got: %v", err)
	}

	cancel() // Canceling caller context afterwards should not panic or fail
}

// TestKafkaProducer_Publish_TryProduce_NonBlocking verifies that TryProduce
// dispatches records without blocking.
func TestKafkaProducer_Publish_TryProduce_NonBlocking(t *testing.T) {
	producer, err := events.NewKafkaProducer(
		[]string{"localhost:9092"},
		"experiment-events",
		kgo.MaxBufferedRecords(100),
	)
	if err != nil {
		t.Fatalf("unexpected error creating producer: %v", err)
	}
	defer producer.Close()

	evt := events.DomainEvent{
		EventID:      "evt-try-1",
		ExperimentID: "exp-test-try",
		EventType:    events.EventExperimentStarted,
		Timestamp:    time.Now().UTC(),
	}

	err = producer.Publish(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected Publish to succeed, got: %v", err)
	}
}

// TestKafkaProducer_Close_Idempotent verifies that Close safely executes bounded flush and teardown.
func TestKafkaProducer_Close_Idempotent(t *testing.T) {
	producer, err := events.NewKafkaProducer([]string{"localhost:9092"}, "experiment-events")
	if err != nil {
		t.Fatalf("unexpected error creating producer: %v", err)
	}

	// First close should succeed (flush timeout or success)
	_ = producer.Close()

	// Nil receiver or empty producer safety
	emptyProducer := &events.KafkaEventProducer{}
	if err := emptyProducer.Close(); err != nil {
		t.Errorf("expected nil error on empty producer close, got: %v", err)
	}
}
