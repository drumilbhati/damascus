package stress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"damascus/internal/stress"
)

func TestHTTPStressEngine_SteppedLoad(t *testing.T) {
	var requestCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	engine := stress.NewHTTPStressEngine(server.Client(), 20)

	plan := stress.LoadPlan{
		TargetURL:           server.URL,
		Method:              "GET",
		InitialRate:         50,
		StepRate:            50,
		MaxRate:             100,
		StepDurationSeconds: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	err := engine.Start(ctx, plan)
	if err != nil {
		t.Fatalf("unexpected error starting stress engine: %v", err)
	}

	stats := engine.GetStats()
	if stats.TotalRequests <= 0 {
		t.Errorf("expected TotalRequests > 0, got %d", stats.TotalRequests)
	}
	if stats.SuccessCount <= 0 {
		t.Errorf("expected SuccessCount > 0, got %d", stats.SuccessCount)
	}
	if stats.ErrorCount != 0 {
		t.Errorf("expected ErrorCount 0, got %d", stats.ErrorCount)
	}
}

func TestHTTPStressEngine_FastPathContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewHTTPStressEngine(server.Client(), 10)

	plan := stress.LoadPlan{
		TargetURL:           server.URL,
		InitialRate:         100,
		StepRate:            50,
		MaxRate:             500,
		StepDurationSeconds: 10,
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start(ctx, plan)
	}()

	// Allow engine to start briefly, then trigger emergency cancel
	time.Sleep(100 * time.Millisecond)
	startTime := time.Now()
	cancel() // Fast-path in-memory cancellation

	select {
	case err := <-errCh:
		elapsed := time.Since(startTime)
		if err != nil {
			t.Errorf("expected nil error on clean context cancellation, got: %v", err)
		}
		if elapsed > 300*time.Millisecond {
			t.Errorf("cancellation took too long (%v), expected sub-second abort", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("engine failed to abort within 2 seconds after context cancellation")
	}
}

func TestHTTPStressEngine_StopMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewHTTPStressEngine(server.Client(), 10)

	plan := stress.LoadPlan{
		TargetURL:           server.URL,
		InitialRate:         100,
		StepRate:            50,
		MaxRate:             500,
		StepDurationSeconds: 10,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start(context.Background(), plan)
	}()

	time.Sleep(100 * time.Millisecond)
	engine.Stop()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("expected nil on Stop(), got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("engine failed to stop after engine.Stop() called")
	}
}

func TestHTTPStressEngine_InvalidLoadPlan(t *testing.T) {
	engine := stress.NewHTTPStressEngine(nil, 10)

	invalidPlan := stress.LoadPlan{
		TargetURL:   "", // Missing target URL
		InitialRate: 0,
	}

	err := engine.Start(context.Background(), invalidPlan)
	if err == nil {
		t.Error("expected error for invalid load plan, got nil")
	}
}

func TestHTTPStressEngine_AlreadyRunningRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := stress.NewHTTPStressEngine(server.Client(), 10)

	plan := stress.LoadPlan{
		TargetURL:           server.URL,
		InitialRate:         50,
		StepRate:            10,
		MaxRate:             100,
		StepDurationSeconds: 5,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = engine.Start(ctx, plan)
	}()

	time.Sleep(50 * time.Millisecond)

	// Attempt concurrent execution on the same engine instance
	err := engine.Start(ctx, plan)
	if err == nil {
		t.Error("expected error when attempting to start already-running engine, got nil")
	}
}

func TestHTTPStressEngine_HTTPErrorTracking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("500 Internal Error"))
	}))
	defer server.Close()

	engine := stress.NewHTTPStressEngine(server.Client(), 10)

	plan := stress.LoadPlan{
		TargetURL:           server.URL,
		Method:              "POST",
		Payload:             `{"test":"payload"}`,
		InitialRate:         50,
		StepRate:            10,
		MaxRate:             100,
		StepDurationSeconds: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_ = engine.Start(ctx, plan)

	stats := engine.GetStats()
	if stats.ErrorCount <= 0 {
		t.Errorf("expected ErrorCount > 0 for 500 responses, got %d", stats.ErrorCount)
	}
	if stats.SuccessCount != 0 {
		t.Errorf("expected SuccessCount 0 for 500 responses, got %d", stats.SuccessCount)
	}
}
