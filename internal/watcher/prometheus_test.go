package watcher_test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"damascus/internal/watcher"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type mockPromAPI struct {
	promv1.API
	queryFn func(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (model.Value, promv1.Warnings, error)
}

func TestQuerySnapshot_MissingTelemetryIsNotHealthy(t *testing.T) {
	client := watcher.NewPrometheusClientWithAPI(&mockPromAPI{
		queryFn: func(context.Context, string, time.Time, ...promv1.Option) (model.Value, promv1.Warnings, error) {
			return model.Vector{}, nil, nil
		},
	})
	_, err := client.QuerySnapshot(context.Background(), "exp", "emailservice")
	if err == nil || !strings.Contains(err.Error(), "no telemetry samples") {
		t.Fatalf("expected missing telemetry error, got %v", err)
	}
}

func TestQuerySnapshot_GRPCSpanMetricsFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		errors float64
	}{{"successful calls", 0}, {"failed calls", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			client := watcher.NewPrometheusClientWithAPI(&mockPromAPI{
				queryFn: func(_ context.Context, query string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
					if !strings.Contains(query, "oteldemo_damascus_") {
						return model.Vector{}, nil, nil
					}
					if !strings.Contains(query, `service_name="productcatalogservice"`) || !strings.Contains(query, `span_kind="SPAN_KIND_SERVER"`) {
						t.Fatalf("missing service/server filter: %s", query)
					}
					value := 10.0
					if strings.Contains(query, "duration_seconds_bucket") {
						value = 0.025
					} else if strings.Contains(query, `status_code="STATUS_CODE_ERROR"`) {
						if tc.errors == 0 {
							return model.Vector{}, nil, nil
						}
						value = tc.errors
					}
					return model.Vector{&model.Sample{Value: model.SampleValue(value)}}, nil, nil
				},
			})
			snapshot, err := client.QuerySnapshot(context.Background(), "exp", "productcatalogservice")
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.RequestRate != 10 || snapshot.P95LatencyMs != 25 || snapshot.ErrorRate != tc.errors/10 || snapshot.Availability != 1-tc.errors/10 {
				t.Fatalf("incorrect span-derived snapshot: %+v", snapshot)
			}
		})
	}
}

func (m *mockPromAPI) Query(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, query, ts, opts...)
	}
	return nil, nil, nil
}

func TestBuildP95LatencyQuery(t *testing.T) {
	unscoped := watcher.BuildP95LatencyQuery("")
	if !strings.Contains(unscoped, "histogram_quantile(0.95") || !strings.Contains(unscoped, "http_request_duration_seconds_bucket[30s]") {
		t.Errorf("unexpected unscoped query: %s", unscoped)
	}

	scoped := watcher.BuildP95LatencyQuery("checkout")
	if !strings.Contains(scoped, `service="checkout"`) {
		t.Errorf("expected service filter in scoped query, got: %s", scoped)
	}
}

func TestBuildErrorRateQuery(t *testing.T) {
	unscoped := watcher.BuildErrorRateQuery("")
	if unscoped == "" {
		t.Fatal("expected non-empty unscoped error rate query")
	}
	if !strings.Contains(unscoped, `status=~"5.."`) {
		t.Errorf("expected status regex filter in query: %s", unscoped)
	}

	scoped := watcher.BuildErrorRateQuery("checkout")
	if !strings.Contains(scoped, `service="checkout"`) {
		t.Errorf("expected service filter in scoped error rate query: %s", scoped)
	}
}

func TestExtractFloatValue_Vector(t *testing.T) {
	vec := model.Vector{
		&model.Sample{
			Value: 42.5,
		},
	}

	val, err := watcher.ExtractFloatValue(vec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42.5 {
		t.Errorf("expected 42.5, got %f", val)
	}
}

func TestExtractFloatValue_Scalar(t *testing.T) {
	scalar := &model.Scalar{
		Value: 99.9,
	}

	val, err := watcher.ExtractFloatValue(scalar)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 99.9 {
		t.Errorf("expected 99.9, got %f", val)
	}
}

func TestExtractFloatValue_EmptyVector(t *testing.T) {
	vec := model.Vector{}
	val, err := watcher.ExtractFloatValue(vec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 0.0 {
		t.Errorf("expected 0.0 for empty vector, got %f", val)
	}
}

func TestExtractFloatValue_NaN(t *testing.T) {
	vec := model.Vector{
		&model.Sample{
			Value: model.SampleValue(math.NaN()),
		},
	}
	val, err := watcher.ExtractFloatValue(vec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 0.0 {
		t.Errorf("expected 0.0 for NaN, got %f", val)
	}
}

func TestPrometheusClient_QuerySnapshot(t *testing.T) {
	mockAPI := &mockPromAPI{
		queryFn: func(ctx context.Context, query string, ts time.Time, opts ...promv1.Option) (model.Value, promv1.Warnings, error) {
			if strings.Contains(query, "histogram_quantile") {
				// P95 latency: 0.35 seconds -> should become 350.0 ms
				return model.Vector{&model.Sample{Value: 0.35}}, nil, nil
			}
			if strings.Contains(query, `status=~"5.."`) {
				// Error rate: 0.05
				return model.Vector{&model.Sample{Value: 0.05}}, nil, nil
			}
			if strings.Contains(query, "http_requests_total") {
				// Request rate: 150.0 RPS
				return model.Vector{&model.Sample{Value: 150.0}}, nil, nil
			}
			return model.Vector{&model.Sample{Value: 0.0}}, nil, nil
		},
	}

	client := watcher.NewPrometheusClientWithAPI(mockAPI)
	snapshot, err := client.QuerySnapshot(context.Background(), "exp-001", "checkout")
	if err != nil {
		t.Fatalf("unexpected QuerySnapshot error: %v", err)
	}

	if snapshot.ExperimentID != "exp-001" {
		t.Errorf("expected ExperimentID exp-001, got %s", snapshot.ExperimentID)
	}
	if snapshot.TargetService != "checkout" {
		t.Errorf("expected TargetService checkout, got %s", snapshot.TargetService)
	}
	if snapshot.RequestRate != 150.0 {
		t.Errorf("expected RequestRate 150.0, got %f", snapshot.RequestRate)
	}
	if snapshot.P95LatencyMs != 350.0 {
		t.Errorf("expected P95LatencyMs 350.0, got %f", snapshot.P95LatencyMs)
	}
	if snapshot.ErrorRate != 0.05 {
		t.Errorf("expected ErrorRate 0.05, got %f", snapshot.ErrorRate)
	}
	if snapshot.Availability != 0.95 {
		t.Errorf("expected Availability 0.95, got %f", snapshot.Availability)
	}
}
