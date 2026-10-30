package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"damascus/internal/config"
	"damascus/internal/observability"
)

// TestLoadConfig_MissingVariables verifies that LoadConfig rejects configurations
// when any or all required environment variables are missing or empty.
func TestLoadConfig_MissingVariables(t *testing.T) {
	// Clear all environment variables to ensure it detects missing configuration
	vars := []string{"PORT", "POSTGRES_DSN", "PROMETHEUS_URL", "JAEGER_URL", "KAFKA_BROKERS", "TARGET_BASE_URL"}
	for _, v := range vars {
		t.Setenv(v, "")
	}

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error when required environment variables are missing, got nil")
	}

	// Supply only a subset to verify validation lists missing ones
	t.Setenv("PORT", "8080")
	_, err = LoadConfig()
	if err == nil {
		t.Fatal("expected error when partial environment variables are missing, got nil")
	}
}

// TestLoadConfig_SuccessFromEnvironment verifies that LoadConfig properly reads
// and populates all Config struct fields when valid environment variables are present.
func TestLoadConfig_SuccessFromEnvironment(t *testing.T) {
	t.Setenv("PORT", "9095")
	t.Setenv("POSTGRES_DSN", "postgres://custom:pass@pg.local:5432/db")
	t.Setenv("PROMETHEUS_URL", "http://prom.prod:9090")
	t.Setenv("JAEGER_URL", "http://jaeger.prod:16686")
	t.Setenv("KAFKA_BROKERS", "kafka-1:9092,kafka-2:9092")
	t.Setenv("TARGET_BASE_URL", "http://mesh.prod:8080")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error when all environment variables are present: %v", err)
	}

	if cfg.Port != "9095" {
		t.Errorf("expected Port 9095, got %s", cfg.Port)
	}
	if cfg.PostgresDSN != "postgres://custom:pass@pg.local:5432/db" {
		t.Errorf("expected custom PostgresDSN, got %s", cfg.PostgresDSN)
	}
	if cfg.PrometheusURL != "http://prom.prod:9090" {
		t.Errorf("expected custom PrometheusURL, got %s", cfg.PrometheusURL)
	}
	if cfg.JaegerURL != "http://jaeger.prod:16686" {
		t.Errorf("expected custom JaegerURL, got %s", cfg.JaegerURL)
	}
	if cfg.KafkaBrokers != "kafka-1:9092,kafka-2:9092" {
		t.Errorf("expected custom KafkaBrokers, got %s", cfg.KafkaBrokers)
	}
	if cfg.TargetBaseURL != "http://mesh.prod:8080" {
		t.Errorf("expected custom TargetBaseURL, got %s", cfg.TargetBaseURL)
	}
}

// TestHealthCheckerAdapter verifies the behavior of healthCheckerAdapter across
// healthy stacks, unreachable endpoints, and connection timeout scenarios.
func TestHealthCheckerAdapter(t *testing.T) {
	ctx := context.Background()

	t.Run("healthy stack", func(t *testing.T) {
		// Mock healthy HTTP server handling probe requests
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/api/v1/query") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		checker := observability.NewChecker(1 * time.Second)
		adapter := &healthCheckerAdapter{
			checker: checker,
			cfg: config.EnvironmentConfig{
				TargetBaseURL:      server.URL,
				JaegerTraceBaseURL: server.URL,
				PrometheusBaseURL:  server.URL,
			},
		}

		res, err := adapter.CheckHealth(ctx)
		if err != nil {
			t.Fatalf("expected healthy stack to succeed, got error: %v", err)
		}
		if res["status"] != "UP" {
			t.Errorf("expected status UP, got %s", res["status"])
		}
		if res["target"] != "CONNECTED" || res["jaeger"] != "CONNECTED" || res["prometheus"] != "CONNECTED" {
			t.Errorf("expected all services CONNECTED, got %+v", res)
		}
	})

	t.Run("unreachable endpoints", func(t *testing.T) {
		checker := observability.NewChecker(1 * time.Second)
		adapter := &healthCheckerAdapter{
			checker: checker,
			cfg: config.EnvironmentConfig{
				TargetBaseURL:      "http://invalid.target:8080",
				JaegerTraceBaseURL: "http://invalid.jaeger:16686",
				PrometheusBaseURL:  "http://invalid.prom:9090",
			},
		}

		res, err := adapter.CheckHealth(ctx)
		if err == nil {
			t.Errorf("expected error connecting to invalid endpoints, got nil")
		}
		if res["status"] != "DOWN" {
			t.Errorf("expected status DOWN for unreachable stack, got %s", res["status"])
		}
		if res["target"] != "UNREACHABLE" || res["jaeger"] != "UNREACHABLE" || res["prometheus"] != "UNREACHABLE" {
			t.Errorf("expected per-service UNREACHABLE statuses in error result, got %+v", res)
		}
	})

	t.Run("mock error handling", func(t *testing.T) {
		adapter := &healthCheckerAdapter{
			checker: observability.NewChecker(100 * time.Millisecond),
			cfg: config.EnvironmentConfig{
				TargetBaseURL: "http://localhost:59999",
			},
		}

		res, err := adapter.CheckHealth(ctx)
		if err == nil {
			t.Error("expected error for unreachable target, got nil")
		}
		if res == nil || res["status"] != "DOWN" {
			t.Errorf("expected result status DOWN, got %+v", res)
		}
		if res["target"] != "UNREACHABLE" {
			t.Errorf("expected target UNREACHABLE, got %s", res["target"])
		}
	})
}
