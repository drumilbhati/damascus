package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// Default configuration constants for Kafka Event Consumer.
const (
	// DefaultTopic is the canonical Kafka topic for experiment lifecycle events.
	DefaultTopic = "experiment-events"

	// DefaultGroupID is the consumer group ID for the audit logger.
	DefaultGroupID = "damascus-event-logger-group"

	// DefaultMinBytes is the minimum batch bytes to fetch from Kafka (10 KB).
	DefaultMinBytes = 10e3

	// DefaultMaxBytes is the maximum batch bytes to fetch from Kafka (10 MB).
	DefaultMaxBytes = 10e6

	// DefaultMaxWait is the maximum time to wait for a batch.
	DefaultMaxWait = 1 * time.Second
)

// KafkaReader defines the minimal interface required for consuming Kafka messages.
// This interface is satisfied directly by *kafka.Reader and facilitates unit testing.
type KafkaReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// ConsumerConfig specifies the operational settings for the event consumer.
type ConsumerConfig struct {
	// Brokers is a list of Kafka bootstrap brokers (e.g., []string{"localhost:9092"}).
	Brokers []string

	// Topic is the Kafka topic name (defaults to "experiment-events").
	Topic string

	// GroupID is the Kafka consumer group name (defaults to "damascus-event-logger-group").
	GroupID string

	// MinBytes is the minimum batch size in bytes.
	MinBytes int

	// MaxBytes is the maximum batch size in bytes.
	MaxBytes int

	// MaxWait is the maximum batch wait duration.
	MaxWait time.Duration

	// Logger is a structured slog.Logger instance. If nil, slog.Default() is used.
	Logger *slog.Logger

	// Output is where formatted console audit logs are written. If nil, os.Stdout is used.
	Output io.Writer
}

// EventConsumer is a background consumer group worker that reads DomainEvents
// from the experiment-events topic and outputs formatted audit logs.
type EventConsumer struct {
	reader KafkaReader
	cfg    ConsumerConfig
	logger *slog.Logger
	output io.Writer

	mu      sync.Mutex
	running bool
}

// NewEventConsumer initializes an EventConsumer with a real Kafka reader.
func NewEventConsumer(cfg ConsumerConfig) (*EventConsumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka bootstrap brokers cannot be empty")
	}

	normalizeConfig(&cfg)

	readerCfg := kafka.ReaderConfig{
		Brokers:  cfg.Brokers,
		GroupID:  cfg.GroupID,
		Topic:    cfg.Topic,
		MinBytes: cfg.MinBytes,
		MaxBytes: cfg.MaxBytes,
		MaxWait:  cfg.MaxWait,
	}

	if err := readerCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid kafka reader configuration: %w", err)
	}

	reader := kafka.NewReader(readerCfg)

	return NewEventConsumerWithReader(reader, cfg), nil
}

// NewEventConsumerWithReader constructs an EventConsumer using an existing or mock KafkaReader.
func NewEventConsumerWithReader(reader KafkaReader, cfg ConsumerConfig) *EventConsumer {
	normalizeConfig(&cfg)

	return &EventConsumer{
		reader: reader,
		cfg:    cfg,
		logger: cfg.Logger,
		output: cfg.Output,
	}
}

func normalizeConfig(cfg *ConsumerConfig) {
	if cfg.Topic == "" {
		cfg.Topic = DefaultTopic
	}
	if cfg.GroupID == "" {
		cfg.GroupID = DefaultGroupID
	}
	if cfg.MinBytes <= 0 {
		cfg.MinBytes = int(DefaultMinBytes)
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = int(DefaultMaxBytes)
	}
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = DefaultMaxWait
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}
}

// Start runs the background event consumption loop.
// It blocks until ctx is cancelled or an unrecoverable reader error occurs.
func (ec *EventConsumer) Start(ctx context.Context) error {
	ec.mu.Lock()
	if ec.running {
		ec.mu.Unlock()
		return errors.New("consumer is already running")
	}
	ec.running = true
	ec.mu.Unlock()

	defer func() {
		ec.mu.Lock()
		ec.running = false
		ec.mu.Unlock()
	}()

	ec.logger.Info("starting kafka event logger consumer",
		slog.String("topic", ec.cfg.Topic),
		slog.String("group_id", ec.cfg.GroupID),
	)

	for {
		select {
		case <-ctx.Done():
			ec.logger.Info("stopping kafka event logger consumer: context cancelled")
			return ctx.Err()
		default:
		}

		msg, err := ec.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) || strings.Contains(strings.ToLower(err.Error()), "closed") {
				ec.logger.Info("stopping kafka event logger consumer: reader closed")
				return err
			}
			ec.logger.Error("failed to fetch kafka message", slog.Any("error", err))
			// Brief backoff on transient errors to avoid tight error loops
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		var event DomainEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			ec.logger.Warn("skipping unparseable event message",
				slog.Int("partition", msg.Partition),
				slog.Int64("offset", msg.Offset),
				slog.Int("payload_bytes", len(msg.Value)),
				slog.Any("error", err),
			)
			if commitErr := ec.reader.CommitMessages(ctx, msg); commitErr != nil {
				if errors.Is(commitErr, context.Canceled) || ctx.Err() != nil {
					return ctx.Err()
				}
				ec.logger.Error("failed to commit unparseable message", slog.Any("error", commitErr))
			}
			continue
		}

		if event.EventType == "" || event.ExperimentID == "" {
			ec.logger.Warn("skipping invalid event message: missing required fields",
				slog.Int("partition", msg.Partition),
				slog.Int64("offset", msg.Offset),
				slog.String("event_type", string(event.EventType)),
				slog.String("experiment_id", event.ExperimentID),
			)
			if commitErr := ec.reader.CommitMessages(ctx, msg); commitErr != nil {
				if errors.Is(commitErr, context.Canceled) || ctx.Err() != nil {
					return ctx.Err()
				}
				ec.logger.Error("failed to commit invalid message", slog.Any("error", commitErr))
			}
			continue
		}

		// Format and output the audit log entry
		logLine := FormatAuditLog(event, msg.Partition, msg.Offset)
		if _, writeErr := fmt.Fprintln(ec.output, logLine); writeErr != nil {
			ec.logger.Error("failed to write audit log output", slog.Any("error", writeErr))
			return writeErr
		}

		// Commit the message only after the audit write succeeds
		if err := ec.reader.CommitMessages(ctx, msg); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return ctx.Err()
			}
			ec.logger.Error("failed to commit kafka message", slog.Any("error", err))
		}

		// Emit structured log at appropriate severity
		ec.logEvent(event, msg.Partition, msg.Offset)
	}
}

// Close gracefully closes the underlying Kafka reader.
func (ec *EventConsumer) Close() error {
	if ec.reader != nil {
		return ec.reader.Close()
	}
	return nil
}

// logEvent routes the parsed event to slog at an appropriate level.
func (ec *EventConsumer) logEvent(event DomainEvent, partition int, offset int64) {
	attrs := []any{
		slog.String("event_id", event.EventID),
		slog.String("experiment_id", event.ExperimentID),
		slog.String("event_type", string(event.EventType)),
		slog.String("service", event.Service),
		slog.Time("timestamp", event.Timestamp),
		slog.Int("partition", partition),
		slog.Int64("offset", offset),
	}

	switch event.EventType {
	case EventSafetyStopTriggered, EventExperimentAborted:
		ec.logger.Warn(fmt.Sprintf("ALERT: %s", event.EventType), attrs...)
	default:
		ec.logger.Info(fmt.Sprintf("EVENT: %s", event.EventType), attrs...)
	}
}

// sanitizeLogField replaces control characters (newlines, carriage returns, etc.) with
// safe escaped sequences to prevent audit log injection and multi-line formatting issues.
func sanitizeLogField(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 32 || r == 127 {
			switch r {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				fmt.Fprintf(&b, `\x%02x`, r)
			}
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatAuditLog formats a DomainEvent into a concise, human-readable console audit log line.
func FormatAuditLog(event DomainEvent, partition int, offset int64) string {
	ts := event.Timestamp.UTC().Format(time.RFC3339)
	if event.Timestamp.IsZero() {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	icon := "ℹ️ "
	switch event.EventType {
	case EventSafetyStopTriggered:
		icon = "🚨"
	case EventExperimentAborted:
		icon = "❌"
	case EventExperimentCompleted:
		icon = "✅"
	case EventExperimentStarted:
		icon = "🚀"
	case EventLoadStepChanged:
		icon = "📈"
	}

	payloadJSON := "{}"
	if len(event.Payload) > 0 {
		if bytes, err := json.Marshal(event.Payload); err == nil {
			payloadJSON = string(bytes)
		}
	}

	service := event.Service
	if strings.TrimSpace(service) == "" {
		service = "-"
	}

	cleanService := sanitizeLogField(service)
	cleanEventType := sanitizeLogField(strings.TrimSpace(string(event.EventType)))
	cleanExpID := sanitizeLogField(strings.TrimSpace(event.ExperimentID))
	cleanPayload := sanitizeLogField(payloadJSON)

	return fmt.Sprintf("[AUDIT] %s %s | TYPE=%-22s | EXP=%-12s | SVC=%-14s | [P:%d O:%d] | PAYLOAD=%s",
		icon,
		ts,
		cleanEventType,
		cleanExpID,
		cleanService,
		partition,
		offset,
		cleanPayload,
	)
}
