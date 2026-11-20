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

type discoveryPromAPI struct {
	promv1.API
	names   []string
	series  map[string][]model.LabelSet
	queryFn func(query string) (model.Value, error)
	queries []string
}

func (d *discoveryPromAPI) LabelValues(context.Context, string, []string, time.Time, time.Time, ...promv1.Option) (model.LabelValues, promv1.Warnings, error) {
	out := make(model.LabelValues, 0, len(d.names))
	for _, n := range d.names {
		out = append(out, model.LabelValue(n))
	}
	return out, nil, nil
}

func (d *discoveryPromAPI) Series(_ context.Context, matches []string, _, _ time.Time, _ ...promv1.Option) ([]model.LabelSet, promv1.Warnings, error) {
	return d.series[matches[0]], nil, nil
}

func (d *discoveryPromAPI) Query(_ context.Context, query string, _ time.Time, _ ...promv1.Option) (model.Value, promv1.Warnings, error) {
	d.queries = append(d.queries, query)
	v, err := d.queryFn(query)
	return v, nil, err
}

func vec(v float64) model.Value { return model.Vector{&model.Sample{Value: model.SampleValue(v)}} }

func TestDiscoverProfile_PrefersServerHistogramWithServiceLabel(t *testing.T) {
	api := &discoveryPromAPI{
		names: []string{
			"http_client_request_duration_seconds_bucket", "http_client_request_duration_seconds_count",
			"http_server_request_duration_seconds_bucket", "http_server_request_duration_seconds_count",
			"queue_size", "orders_latency_ms_bucket", // no _count: not a histogram family
		},
		series: map[string][]model.LabelSet{
			"http_client_request_duration_seconds_count": {{"service_name": "orders", "http_response_status_code": "200"}},
			"http_server_request_duration_seconds_count": {
				{"job": "orders", "http_response_status_code": "200"},
				{"job": "orders", "http_response_status_code": "503"},
				{"job": "billing", "http_response_status_code": "200"},
			},
		},
	}
	client := watcher.NewPrometheusClientWithAPI(api)
	p, err := client.DiscoverProfile(context.Background(), "orders")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	want := watcher.MetricProfile{Histogram: "http_server_request_duration_seconds", ServiceLabel: "job", StatusLabel: "http_response_status_code", ErrorMatcher: `=~"5.."`, ToMs: 1000}
	if p != want {
		t.Fatalf("profile = %+v, want %+v", p, want)
	}
	if _, err := client.DiscoverProfile(context.Background(), "unknown"); err == nil {
		t.Fatal("expected no profile for an unlabelled service")
	}
}

func TestDiscoverProfile_SpanMetrics(t *testing.T) {
	api := &discoveryPromAPI{
		names: []string{"traces_span_metrics_duration_milliseconds_bucket", "traces_span_metrics_duration_milliseconds_count"},
		series: map[string][]model.LabelSet{
			"traces_span_metrics_duration_milliseconds_count": {
				{"service_name": "cart", "span_kind": "SPAN_KIND_SERVER", "status_code": "STATUS_CODE_UNSET"},
				{"service_name": "cart", "span_kind": "SPAN_KIND_CLIENT", "status_code": "STATUS_CODE_ERROR"},
			},
		},
	}
	p, err := watcher.NewPrometheusClientWithAPI(api).DiscoverProfile(context.Background(), "cart")
	if err != nil {
		t.Fatal(err)
	}
	if p.ExtraSelector != `span_kind="SPAN_KIND_SERVER"` || p.ErrorMatcher != `="STATUS_CODE_ERROR"` || p.ToMs != 1 {
		t.Fatalf("unexpected span profile %+v", p)
	}
}

func TestQuerySnapshot_FallsBackToDiscoveredMetrics(t *testing.T) {
	api := &discoveryPromAPI{
		names: []string{"app_request_duration_seconds_bucket", "app_request_duration_seconds_count"},
		series: map[string][]model.LabelSet{
			"app_request_duration_seconds_count": {{"service": "orders", "code": "200"}},
		},
	}
	api.queryFn = func(q string) (model.Value, error) {
		if !strings.Contains(q, "app_request_duration_seconds") {
			return model.Vector{}, nil // built-in metric names are absent
		}
		switch {
		case strings.Contains(q, "histogram_quantile"):
			return vec(0.25), nil
		case strings.Contains(q, `code=~"5.."`):
			return vec(5), nil
		default:
			return vec(50), nil
		}
	}
	client := watcher.NewPrometheusClientWithAPI(api).EnableMetricDiscovery()
	snap, err := client.QuerySnapshot(context.Background(), "exp", "orders")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.RequestRate != 50 || snap.P95LatencyMs != 250 || math.Abs(snap.ErrorRate-0.1) > 1e-9 || math.Abs(snap.Availability-0.9) > 1e-9 {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
}
