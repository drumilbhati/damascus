package graph_test

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"damascus/internal/graph"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAnalyzer_PreservesServicesWithoutRecentSpans(t *testing.T) {
	client := &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"data":[]}`
		if r.URL.Path == "/api/services" {
			body = `{"data":["frontend","cartservice","checkoutservice","paymentservice"]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	g, err := graph.NewAnalyzer("http://jaeger", client).BuildGraph(context.Background(), 3600)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"frontend", "cartservice", "checkoutservice", "paymentservice"} {
		if _, ok := g.Nodes[name]; !ok {
			t.Errorf("registered service %q missing without recent spans", name)
		}
	}
	if len(g.Edges) != 0 {
		t.Fatal("service registration must not invent dependency edges")
	}
}

func TestAnalyzer_DeduplicatesCallsAndCalculatesWindowRate(t *testing.T) {
	client := &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"data":[{"traceID":"t1","processes":{"p1":{"serviceName":"frontend"},"p2":{"serviceName":"catalog"}},"spans":[{"spanID":"root","processID":"p1"},{"spanID":"child1","processID":"p2","references":[{"refType":"CHILD_OF","traceID":"t1","spanID":"root"}]},{"spanID":"child2","processID":"p2","references":[{"refType":"CHILD_OF","traceID":"t1","spanID":"root"}]}]}]}`
		if r.URL.Path == "/api/services" {
			body = `{"data":["frontend","catalog"]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	g, err := graph.NewAnalyzer("http://jaeger", client).BuildGraph(context.Background(), 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected one aggregated edge, got %d", len(g.Edges))
	}
	edge := g.Edges[0]
	if edge.From != "frontend" || edge.To != "catalog" || edge.CallCount != 2 {
		t.Fatalf("duplicate service-query results inflated calls: %+v", edge)
	}
	if math.Abs(edge.Frequency-2.0/60) > 1e-9 {
		t.Fatalf("expected 2 calls over 60 seconds, got %g calls/sec", edge.Frequency)
	}
}
