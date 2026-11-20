package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeJaeger struct{ url string }

func (f *fakeJaeger) SetJaegerURL(u string) { f.url = u }

type fakeProm struct{ url string }

func (f *fakeProm) SetAddress(u string) error { f.url = u; return nil }

func TestStoreUpdateProbesThenApplies(t *testing.T) {
	jaeger := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/services" {
			_, _ = w.Write([]byte(`{"data":["frontend"]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer jaeger.Close()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/query" {
			_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer prom.Close()

	j, p := &fakeJaeger{}, &fakeProm{}
	busy := false
	store := NewStore(Sources{JaegerURL: "http://old:16686", PrometheusURL: "http://old:9090"}, j, p, nil, func() bool { return busy })
	if store.Get().Origin != "env" {
		t.Fatal("initial origin should be env")
	}

	// Pasted UI/API paths are normalised.
	got, err := store.Update(context.Background(), Sources{JaegerURL: jaeger.URL + "/search", PrometheusURL: prom.URL + "/graph/"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.JaegerURL != jaeger.URL || got.PrometheusURL != prom.URL || got.Origin != "ui" || j.url != jaeger.URL || p.url != prom.URL {
		t.Fatalf("not applied: %+v jaeger=%s prom=%s", got, j.url, p.url)
	}

	// Swapped URLs fail the probe on both fields and leave the live config untouched.
	_, err = store.Update(context.Background(), Sources{JaegerURL: prom.URL, PrometheusURL: jaeger.URL})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Fields["jaeger_url"] == "" || fe.Fields["prometheus_url"] == "" {
		t.Fatalf("expected per-field probe errors, got %v", err)
	}
	if j.url != jaeger.URL {
		t.Fatal("failed update must not change the live source")
	}

	busy = true
	if _, err := store.Update(context.Background(), Sources{JaegerURL: jaeger.URL, PrometheusURL: prom.URL}); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

func TestStoreUpdateValidatesURLs(t *testing.T) {
	store := NewStore(Sources{}, nil, nil, func(context.Context, Sources) map[string]string { return nil }, nil)
	_, err := store.Update(context.Background(), Sources{JaegerURL: "", PrometheusURL: "prometheus:9090"})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Fields["jaeger_url"] != "is required" || !strings.Contains(fe.Fields["prometheus_url"], "absolute") {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestProbeDetectsRawMetricsEndpoint(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			_, _ = w.Write([]byte("# HELP http_requests_total Total.\n# TYPE http_requests_total counter\nhttp_requests_total 3\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer target.Close()
	fields := NewHTTPProber(target.Client())(context.Background(), Sources{JaegerURL: target.URL, PrometheusURL: target.URL + "/metrics"})
	if !strings.Contains(fields["prometheus_url"], "raw /metrics endpoint") {
		t.Fatalf("expected raw /metrics hint, got %v", fields)
	}
	if !strings.Contains(fields["jaeger_url"], "Jaeger services list") {
		t.Fatalf("expected jaeger error, got %v", fields)
	}
}
