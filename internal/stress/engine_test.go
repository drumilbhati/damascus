package stress_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestEngine_StartAndContextCancellation(t *testing.T) {
	var requestCount int64
	var signalOnce sync.Once
	firstReqCh := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		signalOnce.Do(func() {
			close(firstReqCh)
		})
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
		MaxRate:     100, StepDurationSeconds: 5,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start(ctx, plan)
	}()

	// Wait for the first request to hit the server before triggering cancellation
	select {
	case <-firstReqCh:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for first request to hit mock server")
	}

	// Trigger cancellation
	cancel()

	// Wait for engine shutdown on bounded channel
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("engine failed to shut down within 1s after context cancellation")
	}

	if atomic.LoadInt64(&requestCount) == 0 {
		t.Errorf("expected at least 1 request sent, got 0")
	}
}

func TestEngine_StopMethod(t *testing.T) {
	var requestCount int64
	var signalOnce sync.Once
	firstReqCh := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		signalOnce.Do(func() {
			close(firstReqCh)
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client())

	plan := stress.LoadPlan{
		TargetURL:   server.URL,
		Method:      "POST",
		Payload:     `{"item_id":"123"}`,
		InitialRate: 50,
		MaxRate:     50, StepDurationSeconds: 5,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start(context.Background(), plan)
	}()

	// Wait for first request to arrive
	select {
	case <-firstReqCh:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for first request to hit mock server")
	}

	// Call Stop() externally
	engine.Stop()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled on Stop(), got: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("engine failed to stop within 1s after Stop() called")
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
	ctx, cancel := context.WithCancel(context.Background())

	expectedPayload := `{"test":"data"}`
	plan := stress.LoadPlan{
		TargetURL:   server.URL,
		Method:      "POST",
		Payload:     expectedPayload,
		InitialRate: 50,
		MaxRate:     50, StepDurationSeconds: 5,
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
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for request to reach mock server")
	}

	cancel()
}

func TestEngine_HTTPErrorsDoNotAbortExperiment(t *testing.T) {
	// 4xx and 5xx responses are valid load-test observations measured by
	// Prometheus/SafetyController. The engine must keep running rather than
	// treating them as fatal and aborting the experiment mid-ramp.
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		code := code
		t.Run(fmt.Sprintf("HTTP_%d", code), func(t *testing.T) {
			var hitCount int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt64(&hitCount, 1)
				w.WriteHeader(code)
			}))
			defer server.Close()

			engine := stress.NewEngine(server.Client())
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()

			plan := stress.LoadPlan{
				TargetURL:           server.URL,
				Method:              "GET",
				InitialRate:         10,
				MaxRate:             10,
				StepDurationSeconds: 1,
			}
			// Engine must not return a non-context error because of HTTP error codes.
			err := engine.Start(ctx, plan)
			if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
				t.Errorf("HTTP %d response aborted experiment unexpectedly: %v", code, err)
			}
			if atomic.LoadInt64(&hitCount) == 0 {
				t.Errorf("HTTP %d: expected requests to be sent, got 0", code)
			}
		})
	}
}

func TestEngine_RateValidation(t *testing.T) {
	engine := stress.NewEngine(nil)
	ctx := context.Background()

	// InitialRate <= 0
	err := engine.Start(ctx, stress.LoadPlan{InitialRate: 0})
	if err == nil {
		t.Errorf("expected error for InitialRate = 0, got nil")
	}

	err = engine.Start(ctx, stress.LoadPlan{InitialRate: -5})
	if err == nil {
		t.Errorf("expected error for InitialRate = -5, got nil")
	}

	// InitialRate > 1,000,000,000
	err = engine.Start(ctx, stress.LoadPlan{InitialRate: 1_000_000_001})
	if err == nil {
		t.Errorf("expected error for InitialRate > 1B, got nil")
	}
}

func TestEngine_ConcurrentStartRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewEngine(server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	plan := stress.LoadPlan{
		TargetURL:   server.URL,
		Method:      "GET",
		InitialRate: 20,
		MaxRate:     20, StepDurationSeconds: 5,
	}

	go func() {
		_ = engine.Start(ctx, plan)
	}()

	// Give it a brief moment to start running
	time.Sleep(10 * time.Millisecond)

	// Second concurrent Start call must be rejected
	err := engine.Start(ctx, plan)
	if err == nil {
		t.Errorf("expected concurrent Start call to be rejected, got nil")
	}
}
