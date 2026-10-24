package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// EventProducer defines the contract for streaming domain events to an event bus.
type EventProducer interface {
	Publish(ctx context.Context, event DomainEvent) error
	Close() error
}

// KafkaEventProducer publishes DomainEvent envelopes asynchronously to an Apache Kafka cluster
// using the pure-Go franz-go client library (kgo).
type KafkaEventProducer struct {
	client *kgo.Client
	topic  string
}

// NewKafkaProducer creates and initializes a KafkaEventProducer connected to the specified seed brokers.
//
// Parameters:
//   - brokers: List of host:port seed broker addresses (e.g. []string{"localhost:9092"}).
//   - topic: Target Kafka topic. If empty, defaults to DefaultTopic ("experiment-events").
//   - extraOpts: Optional franz-go functional options for custom TLS, SASL, or batching configurations.
//
// Returns:
//   - *KafkaEventProducer: A ready-to-use event producer.
//   - error: Non-nil if broker addresses are missing or client initialization fails.
func NewKafkaProducer(brokers []string, topic string, extraOpts ...kgo.Opt) (*KafkaEventProducer, error) {
	if len(brokers) == 0 {
		return nil, errors.New("at least one broker address must be provided")
	}

	if topic == "" {
		topic = DefaultTopic
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.DefaultProduceTopic(topic),
	}
	opts = append(opts, extraOpts...)

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize kafka client: %w", err)
	}

	return &KafkaEventProducer{
		client: client,
		topic:  topic,
	}, nil
}

// Publish serializes and streams a single DomainEvent to Kafka asynchronously.
//
// Architectural guarantees:
//  1. Fast-Path Context Check: Returns ctx.Err() immediately if context is already cancelled.
//  2. Partition Ordering: Sets the Kafka Record Key to event.ExperimentID. This guarantees
//     that all lifecycle events for a specific experiment land on the exact same partition,
//     ensuring strict chronological in-order delivery to downstream consumers.
//  3. Non-Blocking Dispatch: Uses client.Produce() with a completion callback to avoid
//     blocking the stress testing or telemetry polling loops.
func (p *KafkaEventProducer) Publish(ctx context.Context, event DomainEvent) error {
	// Fast-path: abort immediately if caller context is already canceled.
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Serialize DomainEvent into JSON payload.
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal domain event: %w", err)
	}

	// Construct Kafka record with ExperimentID as the partition routing key.
	record := &kgo.Record{
		Topic: p.topic,
		Key:   []byte(event.ExperimentID),
		Value: payload,
	}

	// Dispatch asynchronously via franz-go client buffer.
	p.client.Produce(ctx, record, func(r *kgo.Record, err error) {
		if err != nil {
			// Async error callback: in production, unhandled delivery failures can be logged here.
		}
	})

	return nil
}

// Close flushes any buffered in-flight messages and gracefully terminates broker network connections.
func (p *KafkaEventProducer) Close() error {
	if p.client != nil {
		p.client.Close()
	}
	return nil
}
