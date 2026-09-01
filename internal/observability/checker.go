package observability

import (
	"context"
	"damascus/internal/config"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Checker struct {
	client *http.Client
}

func NewChecker(timeout time.Duration) *Checker {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Checker{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Checker) CheckObservabilityStack(ctx context.Context, cfg config.EnvironmentConfig) (*config.HealthStatus, error) {
	status := &config.HealthStatus{
		Status:     "UP",
		Target:     "CONNECTED",
		Jaeger:     "CONNECTED",
		Prometheus: "CONNECTED",
		Errors:     make(map[string]string),
	}

	// 1. Probe Target Base URL (accepts 2xx/3xx/4xx, rejects >= 500)
	if err := c.probeEndpoint(ctx, cfg.TargetBaseURL, false); err != nil {
		status.Target = "UNREACHABLE"
		status.Errors["target"] = err.Error()
	}

	// 2. Probe Jaeger Services API (strictly requires 200 OK)
	jaegerURL := strings.TrimRight(cfg.JaegerTraceBaseURL, "/") + "/api/services"
	if err := c.probeEndpoint(ctx, jaegerURL, true); err != nil {
		status.Jaeger = "UNREACHABLE"
		status.Errors["jaeger"] = err.Error()
	}

	// 3. Probe Prometheus PromQL API (strictly requires 200 OK and status: "success")
	prometheusURL := strings.TrimRight(cfg.PrometheusBaseURL, "/") + "/api/v1/query?query=up"
	if err := c.probePrometheus(ctx, prometheusURL); err != nil {
		status.Prometheus = "UNREACHABLE"
		status.Errors["prometheus"] = err.Error()
	}

	if len(status.Errors) > 0 {
		status.Status = "DOWN"
		return status, fmt.Errorf("observability stack health check failed: %d errors encountered", len(status.Errors))
	}

	return status, nil
}

func (c *Checker) probeEndpoint(ctx context.Context, url string, requireOK bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach %s: %w", url, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Printf("observability checker: closing response body: %v\n", closeErr)
		}
	}()
	_, _ = io.Copy(io.Discard, resp.Body)

	if requireOK && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, url)
	}
	if !requireOK && resp.StatusCode >= http.StatusInternalServerError {
		return fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, url)
	}
	return nil
}

func (c *Checker) probePrometheus(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach %s: %w", url, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			fmt.Printf("observability checker: closing response body: %v\n", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d from %s", resp.StatusCode, url)
	}

	var payload struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("failed to decode response from %s: %w", url, err)
	}

	if payload.Status != "success" {
		return fmt.Errorf("unexpected status in response from %s: %s", url, payload.Status)
	}

	return nil
}