package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// Default topic configuration constants.
const (
	// DefaultPartitions is the default partition count for initialized Kafka topics.
	DefaultPartitions = 1

	// DefaultReplicationFactor is the default replication factor for single-broker/dev environments.
	DefaultReplicationFactor = 1

	// DefaultTopicInitTimeout is the default timeout for topic administrative operations.
	DefaultTopicInitTimeout = 10 * time.Second
)

// TopicSpecification describes a Kafka topic to be initialized.
type TopicSpecification struct {
	// Topic is the name of the Kafka topic.
	Topic string

	// NumPartitions is the number of partitions for the topic (defaults to DefaultPartitions if <= 0).
	NumPartitions int

	// ReplicationFactor is the replica count for the topic (defaults to DefaultReplicationFactor if <= 0).
	ReplicationFactor int

	// ConfigEntries defines optional Kafka topic-level configuration key/value pairs (e.g. retention.ms).
	ConfigEntries map[string]string
}

// TopicAdmin defines the minimal administrative contract required for Kafka topic creation.
// Satisfied directly by *kafka.Client, enabling clean dependency injection in unit tests.
type TopicAdmin interface {
	CreateTopics(ctx context.Context, req *kafka.CreateTopicsRequest) (*kafka.CreateTopicsResponse, error)
}

// TopicInitializer coordinates the idempotent creation and initialization of Kafka topics.
type TopicInitializer struct {
	admin  TopicAdmin
	addr   net.Addr
	logger *slog.Logger
}

// parseBrokers splits comma-separated strings and removes empty/whitespace entries.
func parseBrokers(brokers []string) []string {
	var clean []string
	for _, b := range brokers {
		for _, part := range strings.Split(b, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				clean = append(clean, trimmed)
			}
		}
	}
	return clean
}

// NewTopicInitializer initializes a TopicInitializer using standard Kafka bootstrap brokers.
func NewTopicInitializer(brokers []string, logger *slog.Logger) (*TopicInitializer, error) {
	cleanBrokers := parseBrokers(brokers)
	if len(cleanBrokers) == 0 {
		return nil, errors.New("kafka bootstrap brokers cannot be empty")
	}

	if logger == nil {
		logger = slog.Default()
	}

	addr := kafka.TCP(cleanBrokers...)
	client := &kafka.Client{
		Addr:    addr,
		Timeout: DefaultTopicInitTimeout,
	}

	return &TopicInitializer{
		admin:  client,
		addr:   addr,
		logger: logger,
	}, nil
}

// NewTopicInitializerWithAdmin creates a TopicInitializer with an explicit TopicAdmin implementation.
// Useful for mock injection during unit and integration testing.
func NewTopicInitializerWithAdmin(admin TopicAdmin, addr net.Addr, logger *slog.Logger) *TopicInitializer {
	if logger == nil {
		logger = slog.Default()
	}
	return &TopicInitializer{
		admin:  admin,
		addr:   addr,
		logger: logger,
	}
}

// isTopicAlreadyExists checks whether an error signifies that the topic already exists.
func isTopicAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, kafka.TopicAlreadyExists) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "topicwiththisnamealreadyexists")
}

// EnsureTopic idempotently creates the specified Kafka topic if it does not already exist.
func (ti *TopicInitializer) EnsureTopic(ctx context.Context, spec TopicSpecification) error {
	return ti.EnsureTopics(ctx, spec)
}

// EnsureTopics idempotently creates multiple Kafka topics in a single request.
func (ti *TopicInitializer) EnsureTopics(ctx context.Context, specs ...TopicSpecification) error {
	if len(specs) == 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	kafkaTopics := make([]kafka.TopicConfig, 0, len(specs))
	for _, spec := range specs {
		topic := strings.TrimSpace(spec.Topic)
		if topic == "" {
			return errors.New("topic name cannot be empty")
		}

		partitions := spec.NumPartitions
		if partitions <= 0 {
			partitions = DefaultPartitions
		}

		replicationFactor := spec.ReplicationFactor
		if replicationFactor <= 0 {
			replicationFactor = DefaultReplicationFactor
		}

		topicCfg := kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicationFactor,
		}

		if len(spec.ConfigEntries) > 0 {
			entries := make([]kafka.ConfigEntry, 0, len(spec.ConfigEntries))
			for k, v := range spec.ConfigEntries {
				entries = append(entries, kafka.ConfigEntry{
					ConfigName:  k,
					ConfigValue: v,
				})
			}
			topicCfg.ConfigEntries = entries
		}

		kafkaTopics = append(kafkaTopics, topicCfg)
	}

	req := &kafka.CreateTopicsRequest{
		Addr:   ti.addr,
		Topics: kafkaTopics,
	}

	resp, err := ti.admin.CreateTopics(ctx, req)
	if err != nil {
		if isTopicAlreadyExists(err) {
			ti.logger.Info("kafka topics already exist", slog.Int("count", len(specs)))
			return nil
		}
		return fmt.Errorf("failed to create kafka topics: %w", err)
	}

	var creationErrors []string
	for _, spec := range specs {
		topicErr, exists := resp.Errors[spec.Topic]
		if !exists || topicErr == nil {
			ti.logger.Info("successfully initialized kafka topic",
				slog.String("topic", spec.Topic),
				slog.Int("partitions", spec.NumPartitions),
				slog.Int("replication_factor", spec.ReplicationFactor),
			)
			continue
		}

		if isTopicAlreadyExists(topicErr) {
			ti.logger.Info("kafka topic already exists, skipping creation",
				slog.String("topic", spec.Topic),
			)
			continue
		}

		creationErrors = append(creationErrors, fmt.Sprintf("topic %q: %v", spec.Topic, topicErr))
	}

	if len(creationErrors) > 0 {
		return fmt.Errorf("failed to initialize kafka topic(s): %s", strings.Join(creationErrors, "; "))
	}

	return nil
}

// EnsureTopicExists provides a top-level helper to idempotently ensure a topic exists on Kafka.
func EnsureTopicExists(ctx context.Context, brokers []string, topic string, partitions, replicationFactor int) error {
	initializer, err := NewTopicInitializer(brokers, nil)
	if err != nil {
		return err
	}
	return initializer.EnsureTopic(ctx, TopicSpecification{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: replicationFactor,
	})
}

// InitDefaultTopic idempotently initializes the canonical experiment-events topic with default settings.
func InitDefaultTopic(ctx context.Context, brokers []string) error {
	return EnsureTopicExists(ctx, brokers, DefaultTopic, DefaultPartitions, DefaultReplicationFactor)
}
