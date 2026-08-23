package config

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
