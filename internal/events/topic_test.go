package events_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"damascus/internal/events"
)

// mockTopicAdmin implements events.TopicAdmin for testing without live Kafka.
type mockTopicAdmin struct {
	mu           sync.Mutex
	createCalls  []*kafka.CreateTopicsRequest
	createErr    error
	topicErrors  map[string]error
	createdTopics map[string]kafka.TopicConfig
}

func newMockTopicAdmin() *mockTopicAdmin {
	return &mockTopicAdmin{
		createdTopics: make(map[string]kafka.TopicConfig),
		topicErrors:   make(map[string]error),
	}
}

func (m *mockTopicAdmin) CreateTopics(ctx context.Context, req *kafka.CreateTopicsRequest) (*kafka.CreateTopicsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.createCalls = append(m.createCalls, req)

	if m.createErr != nil {
		return nil, m.createErr
	}

	resp := &kafka.CreateTopicsResponse{
		Errors: make(map[string]error),
	}

	for _, t := range req.Topics {
		if specificErr, exists := m.topicErrors[t.Topic]; exists {
			resp.Errors[t.Topic] = specificErr
			continue
		}

		if _, alreadyExists := m.createdTopics[t.Topic]; alreadyExists {
			resp.Errors[t.Topic] = kafka.TopicAlreadyExists
		} else {
			m.createdTopics[t.Topic] = t
			resp.Errors[t.Topic] = nil
		}
	}

	return resp, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewTopicInitializer_Validation(t *testing.T) {
	t.Run("empty brokers returns error", func(t *testing.T) {
		_, err := events.NewTopicInitializer(nil, testLogger())
		if err == nil {
			t.Fatal("expected error for nil brokers, got nil")
		}

		_, err = events.NewTopicInitializer([]string{}, testLogger())
		if err == nil {
			t.Fatal("expected error for empty brokers, got nil")
		}

		_, err = events.NewTopicInitializer([]string{"   ", ""}, testLogger())
		if err == nil {
			t.Fatal("expected error for whitespace brokers, got nil")
		}
	})

	t.Run("valid brokers initializes correctly", func(t *testing.T) {
		ti, err := events.NewTopicInitializer([]string{"localhost:9092", "localhost:9093"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ti == nil {
			t.Fatal("expected non-nil TopicInitializer")
		}
	})

	t.Run("comma-separated broker string parses properly", func(t *testing.T) {
		ti, err := events.NewTopicInitializer([]string{"localhost:9092,localhost:9093"}, testLogger())
		if err != nil {
			t.Fatalf("unexpected error parsing comma-separated brokers: %v", err)
		}
		if ti == nil {
			t.Fatal("expected non-nil TopicInitializer")
		}
	})
}

func TestEnsureTopic_Success(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	spec := events.TopicSpecification{
		Topic:             "test-topic-1",
		NumPartitions:     3,
		ReplicationFactor: 1,
		ConfigEntries: map[string]string{
			"retention.ms":   "604800000",
			"cleanup.policy": "delete",
		},
	}

	ctx := context.Background()
	if err := ti.EnsureTopic(ctx, spec); err != nil {
		t.Fatalf("EnsureTopic failed: %v", err)
	}

	mockAdmin.mu.Lock()
	defer mockAdmin.mu.Unlock()

	if len(mockAdmin.createCalls) != 1 {
		t.Fatalf("expected 1 CreateTopics call, got %d", len(mockAdmin.createCalls))
	}

	req := mockAdmin.createCalls[0]
	if len(req.Topics) != 1 {
		t.Fatalf("expected 1 topic in request, got %d", len(req.Topics))
	}

	topic := req.Topics[0]
	if topic.Topic != "test-topic-1" {
		t.Errorf("expected topic name test-topic-1, got %s", topic.Topic)
	}
	if topic.NumPartitions != 3 {
		t.Errorf("expected 3 partitions, got %d", topic.NumPartitions)
	}
	if topic.ReplicationFactor != 1 {
		t.Errorf("expected replication factor 1, got %d", topic.ReplicationFactor)
	}
	if len(topic.ConfigEntries) != 2 {
		t.Errorf("expected 2 config entries, got %d", len(topic.ConfigEntries))
	}
}

func TestEnsureTopic_DefaultsApplied(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	spec := events.TopicSpecification{
		Topic: "defaulted-topic",
		// partitions and replication factor omitted / 0
	}

	ctx := context.Background()
	if err := ti.EnsureTopic(ctx, spec); err != nil {
		t.Fatalf("EnsureTopic failed: %v", err)
	}

	mockAdmin.mu.Lock()
	defer mockAdmin.mu.Unlock()

	req := mockAdmin.createCalls[0]
	topic := req.Topics[0]
	if topic.NumPartitions != events.DefaultPartitions {
		t.Errorf("expected default %d partitions, got %d", events.DefaultPartitions, topic.NumPartitions)
	}
	if topic.ReplicationFactor != events.DefaultReplicationFactor {
		t.Errorf("expected default replication factor %d, got %d", events.DefaultReplicationFactor, topic.ReplicationFactor)
	}
}

func TestEnsureTopic_IdempotentOnAlreadyExists(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	spec := events.TopicSpecification{
		Topic: "idempotent-topic",
	}

	ctx := context.Background()
	// First call creates the topic
	if err := ti.EnsureTopic(ctx, spec); err != nil {
		t.Fatalf("first EnsureTopic failed: %v", err)
	}

	// Second call encounters kafka.TopicAlreadyExists, should be ignored and succeed
	if err := ti.EnsureTopic(ctx, spec); err != nil {
		t.Fatalf("second EnsureTopic failed (expected idempotent success): %v", err)
	}
}

func TestEnsureTopic_TransportLevelTopicAlreadyExists(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	// Simulate transport or request level TopicAlreadyExists error
	mockAdmin.createErr = kafka.TopicAlreadyExists
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	spec := events.TopicSpecification{
		Topic: "existing-topic",
	}

	ctx := context.Background()
	if err := ti.EnsureTopic(ctx, spec); err != nil {
		t.Fatalf("expected nil error on already exists, got: %v", err)
	}
}

func TestEnsureTopic_ValidationAndErrors(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	t.Run("empty topic name", func(t *testing.T) {
		err := ti.EnsureTopic(context.Background(), events.TopicSpecification{Topic: "   "})
		if err == nil {
			t.Fatal("expected error for empty topic name, got nil")
		}
	})

	t.Run("context cancelled before call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := ti.EnsureTopic(ctx, events.TopicSpecification{Topic: "cancelled"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("unrecoverable broker error", func(t *testing.T) {
		adminErr := errors.New("broker connection refused")
		failingAdmin := newMockTopicAdmin()
		failingAdmin.createErr = adminErr

		tiFail := events.NewTopicInitializerWithAdmin(failingAdmin, kafka.TCP("localhost:9092"), testLogger())
		err := tiFail.EnsureTopic(context.Background(), events.TopicSpecification{Topic: "err-topic"})
		if err == nil || !strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("expected connection refused error, got %v", err)
		}
	})

	t.Run("specific topic authorization failure", func(t *testing.T) {
		authAdmin := newMockTopicAdmin()
		authAdmin.topicErrors["unauthorized-topic"] = kafka.TopicAuthorizationFailed

		tiAuth := events.NewTopicInitializerWithAdmin(authAdmin, kafka.TCP("localhost:9092"), testLogger())
		err := tiAuth.EnsureTopic(context.Background(), events.TopicSpecification{Topic: "unauthorized-topic"})
		if err == nil || (!strings.Contains(strings.ToLower(err.Error()), "authorization failed") && !errors.Is(err, kafka.TopicAuthorizationFailed)) {
			t.Fatalf("expected authorization failure, got %v", err)
		}
	})
}

func TestEnsureTopics_BatchSuccess(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	specs := []events.TopicSpecification{
		{Topic: "batch-1", NumPartitions: 2},
		{Topic: "batch-2", NumPartitions: 4},
		{Topic: "batch-3", NumPartitions: 1},
	}

	if err := ti.EnsureTopics(context.Background(), specs...); err != nil {
		t.Fatalf("EnsureTopics batch failed: %v", err)
	}

	mockAdmin.mu.Lock()
	defer mockAdmin.mu.Unlock()

	if len(mockAdmin.createCalls) != 1 {
		t.Fatalf("expected 1 batch call, got %d", len(mockAdmin.createCalls))
	}
	if len(mockAdmin.createCalls[0].Topics) != 3 {
		t.Fatalf("expected 3 topics in request, got %d", len(mockAdmin.createCalls[0].Topics))
	}
}

func TestEnsureTopics_EmptyBatchNoOp(t *testing.T) {
	mockAdmin := newMockTopicAdmin()
	ti := events.NewTopicInitializerWithAdmin(mockAdmin, kafka.TCP("localhost:9092"), testLogger())

	if err := ti.EnsureTopics(context.Background()); err != nil {
		t.Fatalf("expected nil for empty batch, got %v", err)
	}

	if len(mockAdmin.createCalls) != 0 {
		t.Errorf("expected 0 calls for empty batch, got %d", len(mockAdmin.createCalls))
	}
}

func TestEnsureTopicExists_Helper(t *testing.T) {
	t.Run("invalid broker returns error", func(t *testing.T) {
		err := events.EnsureTopicExists(context.Background(), []string{}, "topic", 1, 1)
		if err == nil {
			t.Fatal("expected error on empty broker slice, got nil")
		}
	})

	t.Run("init default topic helper with empty broker returns error", func(t *testing.T) {
		err := events.InitDefaultTopic(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error on nil broker slice, got nil")
		}
	})
}

func TestFormatEventAuditLog(t *testing.T) {
	evt := events.Event{
		ID:           "evt-audit-1",
		ExperimentID: "exp-audit-99",
		Type:         events.EventThresholdBreached,
		Timestamp:    time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		Service:      "orderservice",
		Payload:      []byte(`{"p95":720}`),
	}

	line := events.FormatEventAuditLog(evt, 0, 42)
	if !strings.Contains(line, "🚨") {
		t.Errorf("expected threshold breached icon 🚨 in line, got: %s", line)
	}
	if !strings.Contains(line, "THRESHOLD_BREACHED") {
		t.Errorf("expected THRESHOLD_BREACHED in line, got: %s", line)
	}
	if !strings.Contains(line, "exp-audit-99") {
		t.Errorf("expected exp-audit-99 in line, got: %s", line)
	}
	if !strings.Contains(line, "orderservice") {
		t.Errorf("expected orderservice in line, got: %s", line)
	}
	if !strings.Contains(line, "720") {
		t.Errorf("expected 720 payload in line, got: %s", line)
	}
}
