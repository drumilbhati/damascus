package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// DefaultFlushTimeout defines the bounded duration for flushing in-flight records on shutdown.
const DefaultFlushTimeout = 5 * time.Second

// EventProducer defines the contract for streaming domain events to an event bus.
type EventProducer interface {
	Publish(ctx context.Context, event DomainEvent) error
	Close() error
}

// KafkaEventProducer publishes DomainEvent envelopes asynchronously to an Apache Kafka cluster
// using the pure-Go franz-go client library (kgo).
type KafkaEventProducer struct {
	client     *kgo.Client
	topic      string
	produceCtx context.Context
	cancel     context.CancelFunc
	logger     *slog.Logger
	mu         sync.RWMutex
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

	produceCtx, cancel := context.WithCancel(context.Background())

	return &KafkaEventProducer{
		client:     client,
		topic:      topic,
		produceCtx: produceCtx,
		cancel:     cancel,
		logger:     slog.Default(),
	}, nil
}

// SetLogger overrides the default structured logger for reporting delivery errors.
func (p *KafkaEventProducer) SetLogger(l *slog.Logger) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l != nil {
		p.logger = l
	}
}

// Publish serializes and streams a single DomainEvent to Kafka asynchronously.
//
// Architectural guarantees:
//  1. Fast-Path Context Check: Returns ctx.Err() immediately if caller's context is already cancelled.
//  2. Producer-Owned Delivery Lifecycle: Once accepted past the fast-path check, queued records
//     use a producer-owned context so they remain eligible for delivery even if caller's ctx cancels.
//  3. Non-Blocking TryProduce: Uses client.TryProduce() so dispatch never blocks the stress-testing
//     loop.
//  4. Partition Ordering: Sets the Kafka Record Key to event.ExperimentID. This guarantees
//     that all lifecycle events for a specific experiment land on the exact same partition,
//     ensuring strict chronological in-order delivery to downstream consumers.
//  5. Observable Delivery Failures: Asynchronous delivery failures are logged via structured slog
//     with complete event metadata (event_id, experiment_id, event_type). Callers are not required
//     to retry lifecycle events since the producer manages client-side retry policies automatically.
func (p *KafkaEventProducer) Publish(ctx context.Context, event DomainEvent) error {
	// Fast-path: honor caller's cancellation before accepting the event.
	if err := ctx.Err(); err != nil {
		return err
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

	// Capture metadata for the asynchronous callback closure.
	eventID := event.EventID
	experimentID := event.ExperimentID
	eventType := string(event.EventType)

	p.client.TryProduce(p.produceCtx, record, func(r *kgo.Record, err error) {
		if err != nil {
			p.mu.RLock()
			logger := p.logger
			p.mu.RUnlock()

			if errors.Is(err, kgo.ErrMaxBuffered) {
				logger.Warn("kafka producer buffer full; record dropped or rejected",
					slog.String("event_id", eventID),
					slog.String("experiment_id", experimentID),
					slog.String("event_type", eventType),
					slog.Any("error", err),
				)
				return
			}
			logger.Error("asynchronous kafka event delivery failed",
				slog.String("event_id", eventID),
				slog.String("experiment_id", experimentID),
				slog.String("event_type", eventType),
				slog.Any("error", err),
			)
		}
	})

	return nil
}

// Close flushes any buffered in-flight messages using a bounded context and terminates broker connections.
// Returns any flush error encountered prior to client closure.
func (p *KafkaEventProducer) Close() error {
	if p.client == nil {
		return nil
	}

	// Flush buffered records with bounded timeout before teardown.
	flushCtx, flushCancel := context.WithTimeout(context.Background(), DefaultFlushTimeout)
	defer flushCancel()

	flushErr := p.client.Flush(flushCtx)

	p.cancel()
	p.client.Close()

	if flushErr != nil {
		return fmt.Errorf("failed to flush buffered kafka records during close: %w", flushErr)
	}

	return nil
}
