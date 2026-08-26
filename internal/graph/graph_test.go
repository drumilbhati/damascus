package graph_test

import (
	"encoding/json"
	"testing"

	"damascus/internal/graph"
)

func TestDependencyGraph_ConstructionAndDegree(t *testing.T) {
	g := graph.NewDependencyGraph()

	// Frontend -> Checkout -> Payment
	// Frontend -> Checkout -> Shipping
	g.AddEdge(graph.DependencyEdge{
		From:      "frontend",
		To:        "checkout",
		CallCount: 1500,
		Frequency: 50.0,
	})
	g.AddEdge(graph.DependencyEdge{
		From:      "checkout",
		To:        "payment",
		CallCount: 1200,
		Frequency: 40.0,
	})
	g.AddEdge(graph.DependencyEdge{
		From:      "checkout",
		To:        "shipping",
		CallCount: 1200,
		Frequency: 40.0,
	})

	if len(g.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 3 {
		t.Errorf("expected 3 edges, got %d", len(g.Edges))
	}

	// In-degree checks
	if in := g.InDegree("frontend"); in != 0 {
		t.Errorf("expected frontend in-degree 0, got %d", in)
	}
	if in := g.InDegree("checkout"); in != 1 {
		t.Errorf("expected checkout in-degree 1, got %d", in)
	}
	if in := g.InDegree("payment"); in != 1 {
		t.Errorf("expected payment in-degree 1, got %d", in)
	}

	// Out-degree checks
	if out := g.OutDegree("frontend"); out != 1 {
		t.Errorf("expected frontend out-degree 1, got %d", out)
	}
	if out := g.OutDegree("checkout"); out != 2 {
		t.Errorf("expected checkout out-degree 2, got %d", out)
	}
	if out := g.OutDegree("payment"); out != 0 {
		t.Errorf("expected payment out-degree 0, got %d", out)
	}
}

func TestDependencyGraph_JSONSerialization(t *testing.T) {
	g := graph.NewDependencyGraph()
	g.AddNode("cart", map[string]string{"version": "v1.11.0"})
	g.AddEdge(graph.DependencyEdge{
		From:      "frontend",
		To:        "cart",
		CallCount: 300,
		Frequency: 10.0,
	})

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("failed to marshal DependencyGraph: %v", err)
	}

	var decoded graph.DependencyGraph
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal DependencyGraph: %v", err)
	}

	if len(decoded.Nodes) != 2 {
		t.Errorf("expected 2 nodes after decode, got %d", len(decoded.Nodes))
	}
	if len(decoded.Edges) != 1 {
		t.Errorf("expected 1 edge after decode, got %d", len(decoded.Edges))
	}
}

func TestServiceScore_JSONSerialization(t *testing.T) {
	score := graph.ServiceScore{
		ServiceName: "checkout",
		Score:       0.85,
		Reasons: []string{
			"High in-degree centrality (calls from frontend)",
			"Critical path for revenue transactions",
		},
	}

	data, err := json.Marshal(score)
	if err != nil {
		t.Fatalf("failed to marshal ServiceScore: %v", err)
	}

	var decoded graph.ServiceScore
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ServiceScore: %v", err)
	}

	if decoded.ServiceName != score.ServiceName {
		t.Errorf("expected ServiceName %s, got %s", score.ServiceName, decoded.ServiceName)
	}
	if decoded.Score != score.Score {
		t.Errorf("expected Score %f, got %f", score.Score, decoded.Score)
	}
	if len(decoded.Reasons) != len(score.Reasons) {
		t.Errorf("expected %d reasons, got %d", len(score.Reasons), len(decoded.Reasons))
	}
}
