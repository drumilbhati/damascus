package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"damascus/internal/events"

	"github.com/segmentio/kafka-go"
)

// mockKafkaReader implements events.KafkaReader for unit testing without external Kafka.
type mockKafkaReader struct {
	mu       sync.Mutex
	messages []kafka.Message
	index    int
	closed   bool
	err      error
}

func newMockKafkaReader(messages []kafka.Message) *mockKafkaReader {
	return &mockKafkaReader{
		messages: messages,
	}
}

func (m *mockKafkaReader) ReadMessage(ctx context.Context) (kafka.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return kafka.Message{}, m.err
	}

	if m.closed {
		return kafka.Message{}, errors.New("reader is closed")
	}

	if m.index >= len(m.messages) {
		// Wait for context cancellation when all messages have been read
		<-ctx.Done()
		return kafka.Message{}, ctx.Err()
	}

	msg := m.messages[m.index]
	m.index++
	return msg, nil
}

func (m *mockKafkaReader) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func TestNewEventConsumer_Validation(t *testing.T) {
	_, err := events.NewEventConsumer(events.ConsumerConfig{
		Brokers: []string{},
	})
	if err == nil {
		t.Fatal("expected error when brokers list is empty, got nil")
	}
}

func TestEventConsumer_ConsumeAndLog(t *testing.T) {
	now := time.Date(2026, 8, 20, 7, 30, 0, 0, time.UTC)

	sampleEvent1 := events.DomainEvent{
		EventID:      "evt-001",
		ExperimentID: "exp-123",
		EventType:    events.EventExperimentStarted,
		Timestamp:    now,
		Service:      "frontend",
		Payload: map[string]interface{}{
			"initial_rate": 50,
			"target_url":   "http://localhost:8080/cart",
		},
	}
	sampleEvent2 := events.DomainEvent{
		EventID:      "evt-002",
		ExperimentID: "exp-123",
		EventType:    events.EventSafetyStopTriggered,
		Timestamp:    now.Add(15 * time.Second),
		Service:      "checkoutservice",
		Payload: map[string]interface{}{
			"breached_metric": "p95_latency_ms",
			"observed_value":  620.5,
			"threshold_value": 500.0,
			"reason":          "P95 latency breached SLA: 620.50 ms > 500.00 ms",
		},
	}

	bytes1, _ := json.Marshal(sampleEvent1)
	bytes2, _ := json.Marshal(sampleEvent2)

	messages := []kafka.Message{
		{
			Topic:     events.DefaultTopic,
			Partition: 0,
			Offset:    101,
			Key:       []byte("exp-123"),
			Value:     bytes1,
		},
		{
			// Invalid message to test parsing resilience
			Topic:     events.DefaultTopic,
			Partition: 0,
			Offset:    102,
			Key:       []byte("exp-123"),
			Value:     []byte("invalid json syntax"),
		},
		{
			Topic:     events.DefaultTopic,
			Partition: 1,
			Offset:    205,
			Key:       []byte("exp-123"),
			Value:     bytes2,
		},
	}

	mockReader := newMockKafkaReader(messages)
	var outputBuffer bytes.Buffer

	cfg := events.ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		Output:  &outputBuffer,
	}

	consumer := events.NewEventConsumerWithReader(mockReader, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := consumer.Start(ctx)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}

	output := outputBuffer.String()

	// Verify that valid events were printed to output
	if !strings.Contains(output, "EXPERIMENT_STARTED") {
		t.Errorf("expected output to contain EXPERIMENT_STARTED, got:\n%s", output)
	}
	if !strings.Contains(output, "SAFETY_STOP_TRIGGERED") {
		t.Errorf("expected output to contain SAFETY_STOP_TRIGGERED, got:\n%s", output)
	}
	if !strings.Contains(output, "frontend") {
		t.Errorf("expected output to contain frontend, got:\n%s", output)
	}
	if !strings.Contains(output, "checkoutservice") {
		t.Errorf("expected output to contain checkoutservice, got:\n%s", output)
	}
	if !strings.Contains(output, "[P:0 O:101]") {
		t.Errorf("expected partition and offset P:0 O:101, got:\n%s", output)
	}
	if !strings.Contains(output, "[P:1 O:205]") {
		t.Errorf("expected partition and offset P:1 O:205, got:\n%s", output)
	}

	// Verify Close
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer.Close() failed: %v", err)
	}
}

func TestEventConsumer_PreventConcurrentStart(t *testing.T) {
	mockReader := newMockKafkaReader(nil)
	consumer := events.NewEventConsumerWithReader(mockReader, events.ConsumerConfig{
		Brokers: []string{"localhost:9092"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		_ = consumer.Start(ctx)
	}()

	time.Sleep(20 * time.Millisecond)

	// Second start should immediately return error
	err := consumer.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected already running error, got: %v", err)
	}

	cancel()
	wg.Wait()
}

func TestFormatAuditLog_EventTypes(t *testing.T) {
	testCases := []struct {
		eventType events.EventType
		icon      string
	}{
		{events.EventExperimentStarted, "🚀"},
		{events.EventLoadStepChanged, "📈"},
		{events.EventSafetyStopTriggered, "🚨"},
		{events.EventExperimentCompleted, "✅"},
		{events.EventExperimentAborted, "❌"},
	}

	for _, tc := range testCases {
		evt := events.DomainEvent{
			EventID:      "evt-test",
			ExperimentID: "exp-test",
			EventType:    tc.eventType,
			Timestamp:    time.Now().UTC(),
			Service:      "cart",
			Payload: map[string]interface{}{
				"key": "value",
			},
		}

		formatted := events.FormatAuditLog(evt, 0, 10)
		if !strings.Contains(formatted, tc.icon) {
			t.Errorf("expected icon %s for %s, got: %s", tc.icon, tc.eventType, formatted)
		}
		if !strings.Contains(formatted, string(tc.eventType)) {
			t.Errorf("expected event type %s, got: %s", tc.eventType, formatted)
		}
	}
}
