package events_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"damascus/internal/events"

	"github.com/segmentio/kafka-go"
)

// mockKafkaReader implements events.KafkaReader for unit testing without external Kafka.
type mockKafkaReader struct {
	mu          sync.Mutex
	messages    []kafka.Message
	committed   []kafka.Message
	index       int
	closed      bool
	err         error
	fetchSignal chan struct{}
}

func newMockKafkaReader(messages []kafka.Message) *mockKafkaReader {
	return &mockKafkaReader{
		messages:    messages,
		fetchSignal: make(chan struct{}, 1),
	}
}

func (m *mockKafkaReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if m.fetchSignal != nil {
		select {
		case m.fetchSignal <- struct{}{}:
		default:
		}
	}

	m.mu.Lock()
	if m.err != nil {
		defer m.mu.Unlock()
		return kafka.Message{}, m.err
	}

	if m.closed {
		defer m.mu.Unlock()
		return kafka.Message{}, io.EOF
	}

	if m.index >= len(m.messages) {
		m.mu.Unlock()
		<-ctx.Done()
		m.mu.Lock()
		defer m.mu.Unlock()
		return kafka.Message{}, ctx.Err()
	}

	msg := m.messages[m.index]
	m.index++
	m.mu.Unlock()
	return msg, nil
}

func (m *mockKafkaReader) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("reader is closed")
	}
	m.committed = append(m.committed, msgs...)
	return nil
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

func TestNewEventConsumer_InvalidReaderConfig(t *testing.T) {
	_, err := events.NewEventConsumer(events.ConsumerConfig{
		Brokers:  []string{"localhost:9092"},
		MinBytes: 50000,
		MaxBytes: 100, // MinBytes > MaxBytes
	})
	if err == nil {
		t.Fatal("expected validation error when MinBytes > MaxBytes, got nil")
	}
	if !strings.Contains(err.Error(), "minimum batch size") && !strings.Contains(err.Error(), "MinBytes") {
		t.Errorf("expected error to mention minimum batch size or MinBytes, got: %v", err)
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

	// Verify that messages were committed (both valid and malformed)
	mockReader.mu.Lock()
	committedCount := len(mockReader.committed)
	mockReader.mu.Unlock()
	if committedCount != 3 {
		t.Errorf("expected 3 committed messages, got %d", committedCount)
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

	// Synchronize on first Start reaching FetchMessage instead of time.Sleep
	select {
	case <-mockReader.fetchSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first Start call to reach FetchMessage")
	}

	// Second start should immediately return error
	err := consumer.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected already running error, got: %v", err)
	}

	cancel()
	wg.Wait()
}

type errWriter struct {
	err error
}

func (w *errWriter) Write(p []byte) (n int, err error) {
	return 0, w.err
}

func TestEventConsumer_OutputWriteFailure_DoesNotCommit(t *testing.T) {
	event := events.DomainEvent{
		EventID:      "evt-001",
		ExperimentID: "exp-123",
		EventType:    events.EventExperimentStarted,
		Timestamp:    time.Now().UTC(),
		Service:      "frontend",
	}
	bytesVal, _ := json.Marshal(event)

	messages := []kafka.Message{
		{
			Topic:     events.DefaultTopic,
			Partition: 0,
			Offset:    101,
			Key:       []byte("exp-123"),
			Value:     bytesVal,
		},
	}

	mockReader := newMockKafkaReader(messages)
	writeErr := errors.New("disk full / write error")
	failingWriter := &errWriter{err: writeErr}

	consumer := events.NewEventConsumerWithReader(mockReader, events.ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		Output:  failingWriter,
	})

	err := consumer.Start(context.Background())
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected writeErr %v, got %v", writeErr, err)
	}

	mockReader.mu.Lock()
	committedCount := len(mockReader.committed)
	mockReader.mu.Unlock()

	if committedCount != 0 {
		t.Errorf("expected 0 committed messages on write error, got %d", committedCount)
	}
}

func TestEventConsumer_RejectMissingRequiredFields(t *testing.T) {
	messages := []kafka.Message{
		{
			// Empty JSON
			Topic:     events.DefaultTopic,
			Partition: 0,
			Offset:    1,
			Value:     []byte("{}"),
		},
		{
			// Missing experiment_id
			Topic:     events.DefaultTopic,
			Partition: 0,
			Offset:    2,
			Value:     []byte(`{"event_type":"EXPERIMENT_STARTED"}`),
		},
		{
			// Missing event_type
			Topic:     events.DefaultTopic,
			Partition: 0,
			Offset:    3,
			Value:     []byte(`{"experiment_id":"exp-123"}`),
		},
	}

	mockReader := newMockKafkaReader(messages)
	var outputBuffer bytes.Buffer

	consumer := events.NewEventConsumerWithReader(mockReader, events.ConsumerConfig{
		Brokers: []string{"localhost:9092"},
		Output:  &outputBuffer,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_ = consumer.Start(ctx)

	// None of the invalid events should produce an audit entry
	if outputBuffer.Len() > 0 {
		t.Errorf("expected empty output for invalid events, got:\n%s", outputBuffer.String())
	}

	// All 3 invalid messages should have been committed to advance the partition
	mockReader.mu.Lock()
	committedCount := len(mockReader.committed)
	mockReader.mu.Unlock()
	if committedCount != 3 {
		t.Errorf("expected 3 committed invalid messages, got %d", committedCount)
	}
}

func TestEventConsumer_TerminalClosedReader(t *testing.T) {
	mockReader := newMockKafkaReader(nil)
	_ = mockReader.Close()

	consumer := events.NewEventConsumerWithReader(mockReader, events.ConsumerConfig{
		Brokers: []string{"localhost:9092"},
	})

	err := consumer.Start(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF on closed reader, got %v", err)
	}
}

func TestFormatAuditLog_ControlCharacterSanitization(t *testing.T) {
	evt := events.DomainEvent{
		EventID:      "evt-inject\r\n",
		ExperimentID: "exp-123\nINJECT",
		EventType:    events.EventSafetyStopTriggered,
		Timestamp:    time.Date(2026, 8, 20, 7, 30, 0, 0, time.UTC),
		Service:      "checkout\nservice\r\t\x07",
		Payload: map[string]interface{}{
			"message": "hello\nworld",
		},
	}

	formatted := events.FormatAuditLog(evt, 0, 10)

	// Must not contain raw literal newlines
	if strings.Contains(formatted, "\n") {
		t.Errorf("expected no raw newlines in formatted log line, got:\n%s", formatted)
	}

	// Must escape newlines in Service and ExperimentID
	if !strings.Contains(formatted, `checkout\nservice\r\t\x07`) {
		t.Errorf("expected escaped control characters in Service, got: %s", formatted)
	}
	if !strings.Contains(formatted, `exp-123\nINJECT`) {
		t.Errorf("expected escaped newline in ExperimentID, got: %s", formatted)
	}
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
