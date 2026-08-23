package observability_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"damascus/internal/config"
	"damascus/internal/observability"
)

func TestCheckObservabilityStack_LiveIntegration(t *testing.T) {
	checker := observability.NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      "http://localhost:8080",
		JaegerTraceBaseURL: "http://localhost:8080/jaeger/ui",
		PrometheusBaseURL:  "http://localhost:9090",
	}

	status, err := checker.CheckObservabilityStack(ctx, cfg)
	if err != nil {
		t.Fatalf("CheckObservabilityStack returned unexpected error: %v", err)
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
