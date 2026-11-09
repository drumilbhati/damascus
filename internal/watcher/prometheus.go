package watcher

import (
	"context"
	"errors"
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

var errNoTelemetry = errors.New("no telemetry samples for the selected service; check metric export and traffic reaching that service")

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
//
//	histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service="<service>"}[30s])) by (le))
//
// If service is empty:
//
//	histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[30s])) by (le))
func BuildP95LatencyQuery(service string) string {
	if service != "" {
		return fmt.Sprintf(`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{service=%q}[30s])) by (le))`, service)
	}
	return `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[30s])) by (le))`
}

// BuildErrorRateQuery constructs the PromQL query to calculate error rate (5xx / total).
// Formula: sum(rate(http_requests_total{status=~"5.."}[30s])) / sum(rate(http_requests_total[30s]))
// If service is provided, filter by service in both total and 5xx metric series:
//
//	sum(rate(http_requests_total{status=~"5..",service="<service>"}[30s])) / sum(rate(http_requests_total{service="<service>"}[30s]))
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

// queryRequired distinguishes missing telemetry from a measured zero.
func (c *PrometheusClient) queryRequired(ctx context.Context, queries ...string) (float64, error) {
	for _, query := range queries {
		result, _, err := c.api.Query(ctx, query, time.Now())
		if err != nil {
			return 0, fmt.Errorf("promql query failed: %w", err)
		}
		if result == nil {
			continue
		}
		if vector, ok := result.(model.Vector); ok && (len(vector) == 0 || math.IsNaN(float64(vector[0].Value)) || math.IsInf(float64(vector[0].Value), 0)) {
			continue
		}
		if scalar, ok := result.(*model.Scalar); ok && (math.IsNaN(float64(scalar.Value)) || math.IsInf(float64(scalar.Value), 0)) {
			continue
		}
		return ExtractFloatValue(result)
	}
	return 0, errNoTelemetry
}

// querySpanSnapshot reads the Collector's protocol-independent server-span metrics.
// These cover services whose native instrumentation only exposes gRPC metrics.
func (c *PrometheusClient) querySpanSnapshot(ctx context.Context, snapshot MetricSnapshot) (MetricSnapshot, error) {
	selector := fmt.Sprintf(`service_name=%q,span_kind="SPAN_KIND_SERVER"`, snapshot.TargetService)
	rateQuery := fmt.Sprintf(`sum(rate(oteldemo_damascus_calls_total{%s}[30s]))`, selector)
	rate, err := c.queryRequired(ctx, rateQuery)
	if err != nil {
		return snapshot, fmt.Errorf("span request rate: %w", err)
	}
	p95, err := c.queryRequired(ctx, fmt.Sprintf(`histogram_quantile(0.95, sum(rate(oteldemo_damascus_duration_seconds_bucket{%s}[30s])) by (le))`, selector))
	if err != nil {
		return snapshot, fmt.Errorf("span p95 latency: %w", err)
	}
	// No error series is normal when every observed call succeeds.
	errorRate, err := c.QueryValue(ctx, fmt.Sprintf(`sum(rate(oteldemo_damascus_calls_total{%s,status_code="STATUS_CODE_ERROR"}[30s]))`, selector))
	if err != nil {
		return snapshot, fmt.Errorf("span error rate: %w", err)
	}
	snapshot.RequestRate = rate
	snapshot.P95LatencyMs = p95 * 1000
	if rate > 0 {
		snapshot.ErrorRate = math.Min(1, math.Max(0, errorRate/rate))
	}
	snapshot.Availability = 1 - snapshot.ErrorRate
	return snapshot, nil
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
	reqRate, err := c.queryRequired(ctx, reqRateQuery,
		fmt.Sprintf(`sum(rate(oteldemo_http_server_duration_milliseconds_count{exported_job=%q}[30s]))`, targetService))
	if err != nil {
		if errors.Is(err, errNoTelemetry) {
			return c.querySpanSnapshot(ctx, snapshot)
		}
		return snapshot, fmt.Errorf("failed to query request rate: %w", err)
	}
	snapshot.RequestRate = reqRate

	// 2. Query P95 Latency (Convert seconds to milliseconds)
	p95LatencyQuery := BuildP95LatencyQuery(targetService)
	p95Seconds, err := c.queryRequired(ctx, p95LatencyQuery)
	p95Ms := p95Seconds * 1000.0
	if err != nil && targetService != "" {
		p95Ms, err = c.queryRequired(ctx, fmt.Sprintf(`histogram_quantile(0.95, sum(rate(oteldemo_http_server_duration_milliseconds_bucket{exported_job=%q}[30s])) by (le))`, targetService))
	}
	if err != nil {
		if errors.Is(err, errNoTelemetry) {
			return c.querySpanSnapshot(ctx, snapshot)
		}
		return snapshot, fmt.Errorf("failed to query p95 latency: %w", err)
	}
	snapshot.P95LatencyMs = p95Ms

	// 3. Query Error Rate & Compute Availability
	errRateQuery := BuildErrorRateQuery(targetService)
	errRate, err := c.QueryValue(ctx, errRateQuery)
	if err != nil {
		return snapshot, fmt.Errorf("failed to query error rate: %w", err)
	}
	if errRate == 0 && targetService != "" {
		otelErrQuery := fmt.Sprintf(`sum(rate(oteldemo_http_server_duration_milliseconds_count{http_status_code=~"5..",exported_job=%q}[30s])) / sum(rate(oteldemo_http_server_duration_milliseconds_count{exported_job=%q}[30s]))`, targetService, targetService)
		if er, qErr := c.QueryValue(ctx, otelErrQuery); qErr == nil && !math.IsNaN(er) {
			errRate = er
		}
	}
	snapshot.ErrorRate = errRate

	avail := 1.0 - errRate
	if avail < 0 {
		avail = 0.0
	}
	snapshot.Availability = avail

	return snapshot, nil
}
