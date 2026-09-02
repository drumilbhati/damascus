package graph_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"damascus/internal/graph"
)

func TestAnalyzer_BuildGraph_Success(t *testing.T) {
	mockServicesJSON := `{"data":["frontend","checkoutservice"]}`
	mockFrontendTracesJSON := `{
		"data": [
			{
				"traceID": "trace-001",
				"spans": [
					{
						"spanID": "span-1",
						"processID": "p1",
						"references": []
					},
					{
						"spanID": "span-2",
						"processID": "p2",
						"references": [
							{
								"refType": "CHILD_OF",
								"spanID": "span-1"
							}
						]
					}
				],
				"processes": {
					"p1": {"serviceName": "frontend"},
					"p2": {"serviceName": "checkoutservice"}
				}
			}
		]
	}`

	mockCheckoutTracesJSON := `{
		"data": [
			{
				"traceID": "trace-002",
				"spans": [
					{
						"spanID": "span-3",
						"processID": "p2",
						"references": []
					},
					{
						"spanID": "span-4",
						"processID": "p3",
						"references": [
							{
								"refType": "CHILD_OF",
								"spanID": "span-3"
							}
						]
					}
				],
				"processes": {
					"p2": {"serviceName": "checkoutservice"},
					"p3": {"serviceName": "paymentservice"}
				}
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/services":
			_, _ = w.Write([]byte(mockServicesJSON))
		case "/api/traces":
			service := r.URL.Query().Get("service")
			if service == "frontend" {
				_, _ = w.Write([]byte(mockFrontendTracesJSON))
			} else if service == "checkoutservice" {
				_, _ = w.Write([]byte(mockCheckoutTracesJSON))
			} else {
				_, _ = w.Write([]byte(`{"data":[]}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	analyzer := graph.NewAnalyzer(server.URL, server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	g, err := analyzer.BuildGraph(ctx, 3600)
	if err != nil {
		t.Fatalf("BuildGraph failed unexpectedly: %v", err)
	}

	if g == nil {
		t.Fatalf("expected non-nil DependencyGraph")
	}

	// Should have nodes: frontend, checkoutservice, paymentservice
	if len(g.Nodes) < 3 {
		t.Errorf("expected at least 3 nodes, got %d", len(g.Nodes))
	}
	if _, ok := g.Nodes["frontend"]; !ok {
		t.Errorf("expected node 'frontend' in graph")
	}
	if _, ok := g.Nodes["checkoutservice"]; !ok {
		t.Errorf("expected node 'checkoutservice' in graph")
	}
	if _, ok := g.Nodes["paymentservice"]; !ok {
		t.Errorf("expected node 'paymentservice' in graph")
	}

	// Should have 2 edges: frontend -> checkoutservice and checkoutservice -> paymentservice
	if len(g.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(g.Edges))
	}

	foundFrontendToCheckout := false
	foundCheckoutToPayment := false
	for _, edge := range g.Edges {
		if edge.From == "frontend" && edge.To == "checkoutservice" {
			foundFrontendToCheckout = true
		}
		if edge.From == "checkoutservice" && edge.To == "paymentservice" {
			foundCheckoutToPayment = true
		}
	}

	if !foundFrontendToCheckout {
		t.Errorf("missing edge frontend -> checkoutservice")
	}
	if !foundCheckoutToPayment {
		t.Errorf("missing edge checkoutservice -> paymentservice")
	}
}

func TestAnalyzer_BuildGraph_ServicesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	analyzer := graph.NewAnalyzer(server.URL, server.Client())
	g, err := analyzer.BuildGraph(context.Background(), 3600)
	if err == nil {
		t.Errorf("expected error when Jaeger returns 500, got nil")
	}
	if g != nil {
		t.Errorf("expected nil graph on error")
	}
}

func TestAnalyzer_BuildGraph_TracesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/services" {
			_, _ = w.Write([]byte(`{"data":["frontend"]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	analyzer := graph.NewAnalyzer(server.URL, server.Client())
	g, err := analyzer.BuildGraph(context.Background(), 3600)
	if err == nil {
		t.Errorf("expected error when /api/traces returns 500, got nil")
	}
	if g != nil {
		t.Errorf("expected nil graph on error")
	}
}
