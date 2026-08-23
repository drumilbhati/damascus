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

	if err := c.probeEndpoint(ctx, cfg.TargetBaseURL, true); err != nil {
		status.Status = "DOWN"
		status.Target = "UNREACHABLE"
		status.Errors["target"] = err.Error()
	}

	jaegerURL := strings.TrimRight(cfg.JaegerTraceBaseURL, "/") + "/api/traces"
	if err := c.probeEndpoint(ctx, jaegerURL, false); err != nil {
		status.Status = "DOWN"
		status.Jaeger = "UNREACHABLE"
		status.Errors["jaeger"] = err.Error()
	}

	prometheusURL := strings.TrimRight(cfg.PrometheusBaseURL, "/") + "/api/v1/query?query=up"
	if err := c.probePrometheus(ctx, prometheusURL); err != nil {
		status.Status = "DOWN"
		status.Prometheus = "UNREACHABLE"
		status.Errors["prometheus"] = err.Error()
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
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // Discard the response body

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
	defer resp.Body.Close()
	
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
