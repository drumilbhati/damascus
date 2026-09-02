package observability_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"damascus/internal/config"
	"damascus/internal/observability"
)

func TestCheckObservabilityStack_LiveIntegration(t *testing.T) {
	// Check if live target is reachable; skip gracefully if offline in CI / non-docker environment
	conn, err := net.DialTimeout("tcp", "localhost:8080", 500*time.Millisecond)
	if err != nil {
		t.Skip("Skipping live integration test: localhost:8080 is not reachable (Docker stack offline)")
	}
	_ = conn.Close()

	checker := observability.NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      "http://localhost:8080",
		JaegerTraceBaseURL: "http://localhost:16686",
		PrometheusBaseURL:  "http://localhost:9090",
	}

	status, err := checker.CheckObservabilityStack(ctx, cfg)
	if err != nil {
		t.Fatalf("CheckObservabilityStack returned unexpected error: %v, status: %+v", err, status)
	}

	statusJSON, _ := json.MarshalIndent(status, "", "  ")
	fmt.Printf("\n=== Live Observability Health Check Result ===\n%s\n\n", string(statusJSON))

	if status.Status != "UP" {
		t.Errorf("Expected overall status UP, got: %s. Errors: %+v", status.Status, status.Errors)
	}
	if status.Target != "CONNECTED" {
		t.Errorf("Expected Target CONNECTED, got: %s", status.Target)
	}
	if status.Jaeger != "CONNECTED" {
		t.Errorf("Expected Jaeger CONNECTED, got: %s", status.Jaeger)
	}
	if status.Prometheus != "CONNECTED" {
		t.Errorf("Expected Prometheus CONNECTED, got: %s", status.Prometheus)
	}
}
