package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"damascus/internal/config"
	"damascus/internal/observability"
)

func TestCheckObservabilityStack_AllHealthy(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Target OK"))
	}))
	defer targetServer.Close()

	jaegerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer jaegerServer.Close()

	prometheusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prometheusServer.Close()

	checker := observability.NewChecker(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      targetServer.URL,
		JaegerTraceBaseURL: jaegerServer.URL,
		PrometheusBaseURL:  prometheusServer.URL,
	}

	status, err := checker.CheckObservabilityStack(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.Status != "UP" {
		t.Errorf("expected status UP, got: %s", status.Status)
	}
	if status.Target != "CONNECTED" || status.Jaeger != "CONNECTED" || status.Prometheus != "CONNECTED" {
		t.Errorf("expected all components CONNECTED, got: %+v", status)
	}
	if len(status.Errors) != 0 {
		t.Errorf("expected 0 errors, got: %+v", status.Errors)
	}
}

func TestCheckObservabilityStack_TargetServerDown(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer targetServer.Close()

	jaegerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer jaegerServer.Close()

	prometheusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer prometheusServer.Close()

	checker := observability.NewChecker(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      targetServer.URL,
		JaegerTraceBaseURL: jaegerServer.URL,
		PrometheusBaseURL:  prometheusServer.URL,
	}

	status, _ := checker.CheckObservabilityStack(ctx, cfg)
	if status.Status != "DOWN" {
		t.Errorf("expected status DOWN, got: %s", status.Status)
	}
	if status.Target != "UNREACHABLE" {
		t.Errorf("expected target UNREACHABLE, got: %s", status.Target)
	}
	if status.Errors["target"] == "" {
		t.Errorf("expected error recorded for target, got empty")
	}
}

func TestCheckObservabilityStack_JaegerDown(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	jaegerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer jaegerServer.Close()

	prometheusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer prometheusServer.Close()

	checker := observability.NewChecker(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      targetServer.URL,
		JaegerTraceBaseURL: jaegerServer.URL,
		PrometheusBaseURL:  prometheusServer.URL,
	}

	status, _ := checker.CheckObservabilityStack(ctx, cfg)
	if status.Status != "DOWN" {
		t.Errorf("expected status DOWN, got: %s", status.Status)
	}
	if status.Jaeger != "UNREACHABLE" {
		t.Errorf("expected jaeger UNREACHABLE, got: %s", status.Jaeger)
	}
	if status.Errors["jaeger"] == "" {
		t.Errorf("expected error recorded for jaeger, got empty")
	}
}

func TestCheckObservabilityStack_PrometheusQueryError(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	jaegerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer jaegerServer.Close()

	prometheusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"execution","error":"query timed out"}`))
	}))
	defer prometheusServer.Close()

	checker := observability.NewChecker(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      targetServer.URL,
		JaegerTraceBaseURL: jaegerServer.URL,
		PrometheusBaseURL:  prometheusServer.URL,
	}

	status, _ := checker.CheckObservabilityStack(ctx, cfg)
	if status.Status != "DOWN" {
		t.Errorf("expected status DOWN, got: %s", status.Status)
	}
	if status.Prometheus != "UNREACHABLE" {
		t.Errorf("expected prometheus UNREACHABLE, got: %s", status.Prometheus)
	}
	if status.Errors["prometheus"] == "" {
		t.Errorf("expected error recorded for prometheus, got empty")
	}
}

func TestCheckObservabilityStack_ContextCancelled(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	checker := observability.NewChecker(2 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cfg := config.EnvironmentConfig{
		TargetBaseURL:      targetServer.URL,
		JaegerTraceBaseURL: targetServer.URL,
		PrometheusBaseURL:  targetServer.URL,
	}

	status, _ := checker.CheckObservabilityStack(ctx, cfg)
	if status.Status != "DOWN" {
		t.Errorf("expected status DOWN on cancelled context, got: %s", status.Status)
	}
}
