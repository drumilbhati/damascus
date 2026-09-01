package stress_test

import (
	"context"
	"strings"
	"testing"

	"damascus/internal/stress"
)

func TestWorkerPool_StartRejectsInvalidConcurrency(t *testing.T) {
	wp := stress.NewWorkerPool(0, 10)
	if err := wp.Start(context.Background()); err == nil {
		t.Fatal("expected non-positive concurrency to be rejected")
	}
}

func TestWorkerPool_SubmitBeforeStartRejected(t *testing.T) {
	wp := stress.NewWorkerPool(1, 10)
	if err := wp.Submit(func() {}); err == nil {
		t.Fatal("expected submit before start to fail")
	} else if !strings.Contains(err.Error(), "not been started") {
		t.Fatalf("expected start error, got %v", err)
	}
}

func TestWorkerPool_SubmitAfterStopRejected(t *testing.T) {
	wp := stress.NewWorkerPool(1, 10)
	if err := wp.Start(context.Background()); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	wp.Stop()
	if err := wp.Submit(func() {}); err == nil {
		t.Fatal("expected submit after stop to fail")
	}
}
