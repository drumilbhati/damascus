package safety

type SafetyPolicy struct {
	MaxP95LatencyMs float64 `json:"max_p95_latency_ms"`
	MaxErrorRate    float64 `json:"max_error_rate"`   // e.g. 0.05 for 5%
	MinAvailability float64 `json:"min_availability"` // e.g. 0.99 for 99%
}

type SafetyDecision struct {
	ShouldStop bool   `json:"should_stop"`
	Reason     string `json:"reason"`
}
