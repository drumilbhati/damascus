package stress_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestEngine_StartAndContextCancellation(t *testing.T) {
	var requestCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client())
	ctx, cancel := context.WithCancel(context.Background())

	plan := stress.LoadPlan{
		TargetURL:   server.URL,
		Method:      "GET",
		InitialRate: 100, // 100 RPS
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start(ctx, plan)
	}()

	// Let it run for a brief window
	time.Sleep(100 * time.Millisecond)

	// Cancel context (Emergency stop simulation)
	startTime := time.Now()
	cancel()

	err := <-errCh
	duration := time.Since(startTime)

	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	if duration > 100*time.Millisecond {
		t.Errorf("cancellation took too long: %v (expected < 100ms)", duration)
	}

	if atomic.LoadInt64(&requestCount) == 0 {
		t.Errorf("expected at least some requests sent, got 0")
	}
}

func TestEngine_StopMethod(t *testing.T) {
	var requestCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client())

	plan := stress.LoadPlan{
		TargetURL:   server.URL,
		Method:      "POST",
		Payload:     `{"item_id":"123"}`,
		InitialRate: 50,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start(context.Background(), plan)
	}()

	time.Sleep(100 * time.Millisecond)

	// Call Stop() externally
	engine.Stop()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled on Stop(), got: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("engine failed to stop within 500ms after Stop() called")
	}
}

func TestEngine_PayloadAndHeadersReceived(t *testing.T) {
	receivedCh := make(chan struct {
		method string
		body   string
	}, 10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedCh <- struct {
			method string
			body   string
		}{
			method: r.Method,
			body:   string(body),
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	expectedPayload := `{"test":"data"}`
	plan := stress.LoadPlan{
		TargetURL:   server.URL,
		Method:      "POST",
		Payload:     expectedPayload,
		InitialRate: 50, // 20ms interval
	}

	go func() {
		_ = engine.Start(ctx, plan)
	}()

	select {
	case req := <-receivedCh:
		if req.method != "POST" {
			t.Errorf("expected method POST, got %s", req.method)
		}
		if req.body != expectedPayload {
			t.Errorf("expected body %s, got %s", expectedPayload, req.body)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for request to reach mock server")
	}
}
