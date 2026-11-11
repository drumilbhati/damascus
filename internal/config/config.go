package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type EnvironmentConfig struct {
	TargetBaseURL      string `json:"target_base_url"`
	JaegerTraceBaseURL string `json:"jaeger_trace_base_url"`
	PrometheusBaseURL  string `json:"prometheus_base_url"`
}

type HealthStatus struct {
	Status     string            `json:"status"`     // "UP" or "DOWN"
	Target     string            `json:"target"`     // "CONNECTED" or "UNREACHABLE"
	Jaeger     string            `json:"jaeger"`     // "CONNECTED" or "UNREACHABLE"
	Prometheus string            `json:"prometheus"` // "CONNECTED" or "UNREACHABLE"
	Errors     map[string]string `json:"errors,omitempty"`
}

// ParseServiceTargetURLs parses a JSON object mapping telemetry service names
// to HTTP endpoints reachable by the Damascus control-plane container.
func ParseServiceTargetURLs(raw string) (map[string]string, error) {
	urls := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return urls, nil
	}
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		return nil, fmt.Errorf("SERVICE_TARGET_URLS must be a JSON object mapping service names to URLs: %w", err)
	}
	if urls == nil {
		return nil, fmt.Errorf("SERVICE_TARGET_URLS must be a JSON object mapping service names to URLs")
	}
	validated := make(map[string]string, len(urls))
	for service, target := range urls {
		service = strings.TrimSpace(service)
		target = strings.TrimSpace(target)
		parsed, err := url.ParseRequestURI(target)
		if service == "" || err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("SERVICE_TARGET_URLS contains an invalid service name or HTTP(S) URL for %q", service)
		}
		validated[service] = target
	}
	return validated, nil
}
