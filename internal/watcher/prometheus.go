package watcher

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PrometheusClient wraps the Prometheus HTTP API v1 client to query RED/USE metrics.
type PrometheusClient struct {
	api promv1.API
}

// NewPrometheusClient initializes a new Prometheus client pointing to the given address.
func NewPrometheusClient(address string) (*PrometheusClient, error) {
	client, err := api.NewClient(api.Config{
		Address: address,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus client: %w", err)
	}

	return &PrometheusClient{
		api: promv1.NewAPI(client),
	}, nil
}

// NewPrometheusClientWithAPI initializes a PrometheusClient using an existing promv1.API implementation (useful for tests).
func NewPrometheusClientWithAPI(v1api promv1.API) *PrometheusClient {
	return &PrometheusClient{api: v1api}
}

// BuildP95LatencyQuery constructs the PromQL query to calculate P95 latency.
// If service is specified:
//   histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service="<service>"}[30s])) by (le))
// If service is empty:
//   histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[30s])) by (le))
func BuildP95LatencyQuery(service string) string {
	if service != "" {
		return fmt.Sprintf(`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service=%q}[30s])) by (le))`, service)
	}
	return `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[30s])) by (le))`
}

// BuildErrorRateQuery constructs the PromQL query to calculate error rate (5xx / total).
// Formula: sum(rate(http_requests_total{status=~"5.."}[30s])) / sum(rate(http_requests_total[30s]))
// If service is provided, filter by service in both total and 5xx metric series:
//   sum(rate(http_requests_total{status=~"5..",service="<service>"}[30s])) / sum(rate(http_requests_total{service="<service>"}[30s]))
func BuildErrorRateQuery(service string) string {
	if service != "" {
		return fmt.Sprintf(`sum(rate(http_requests_total{status=~"5..",service=%q}[30s])) / sum(rate(http_requests_total{service=%q}[30s]))`, service, service)
	}
	return `sum(rate(http_requests_total{status=~"5.."}[30s])) / sum(rate(http_requests_total[30s]))`
}

// BuildRequestRateQuery constructs the PromQL query to calculate total request rate (RPS).
// Formula: sum(rate(http_requests_total[30s]))
func BuildRequestRateQuery(service string) string {
	if service != "" {
		return fmt.Sprintf(`sum(rate(http_requests_total{service=%q}[30s]))`, service)
	}
	return `sum(rate(http_requests_total[30s]))`
}

// ExtractFloatValue extracts a float64 scalar value from a Prometheus model.Value.
// In Prometheus client_golang, query results are represented as model.Value interface:
// - model.Vector: a slice of *model.Sample. If empty, return (0.0, nil). Otherwise, float64(sample.Value).
// - *model.Scalar: a single scalar. Return float64(scalar.Value).
func ExtractFloatValue(val model.Value) (float64, error) {
	if val == nil {
		return 0.0, nil
	}

	switch v := val.(type) {
	case model.Vector:
		if len(v) == 0 || math.IsNaN(float64(v[0].Value)) {
			return 0.0, nil
		}
		return float64(v[0].Value), nil
	case *model.Scalar:
		if math.IsNaN(float64(v.Value)) {
			return 0.0, nil
		}
		return float64(v.Value), nil
	default:
		return 0.0, fmt.Errorf("unsupported prometheus model.Value type: %T", val)
	}
}

// QueryValue executes a PromQL query at the current time and extracts a single scalar float64.
func (c *PrometheusClient) QueryValue(ctx context.Context, query string) (float64, error) {
	result, _, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0.0, fmt.Errorf("promql query failed: %w", err)
	}

	return ExtractFloatValue(result)
}

// QuerySnapshot executes the RED PromQL queries for a target service and compiles
// a comprehensive MetricSnapshot.
// - P95 Latency: PromQL returns seconds; snapshot.P95LatencyMs MUST be in milliseconds (seconds * 1000.0).
// - ErrorRate: Ratio between 0.0 and 1.0.
// - Availability: Calculated as (1.0 - ErrorRate). If ErrorRate >= 1.0, Availability = 0.0.
// - RequestRate: Requests per second.
func (c *PrometheusClient) QuerySnapshot(ctx context.Context, experimentID, targetService string) (MetricSnapshot, error) {
	snapshot := MetricSnapshot{
		ExperimentID:  experimentID,
		TargetService: targetService,
		Timestamp:     time.Now().UTC(),
	}

	// 1. Query Request Rate
	reqRateQuery := BuildRequestRateQuery(targetService)
	reqRate, err := c.QueryValue(ctx, reqRateQuery)
	if err != nil {
		return snapshot, fmt.Errorf("failed to query request rate: %w", err)
	}
	snapshot.RequestRate = reqRate

	// 2. Query P95 Latency (Convert seconds to milliseconds)
	p95LatencyQuery := BuildP95LatencyQuery(targetService)
	p95Seconds, err := c.QueryValue(ctx, p95LatencyQuery)
	if err != nil {
		return snapshot, fmt.Errorf("failed to query p95 latency: %w", err)
	}
	snapshot.P95LatencyMs = p95Seconds * 1000.0

	// 3. Query Error Rate & Compute Availability
	errRateQuery := BuildErrorRateQuery(targetService)
	errRate, err := c.QueryValue(ctx, errRateQuery)
	if err != nil {
		return snapshot, fmt.Errorf("failed to query error rate: %w", err)
	}
	snapshot.ErrorRate = errRate

	avail := 1.0 - errRate
	if avail < 0 {
		avail = 0.0
	}
	snapshot.Availability = avail

	return snapshot, nil
}
