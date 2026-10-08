package graph_test

import (
	"strings"
	"testing"

	"damascus/internal/graph"
)

func TestScoreCriticality_EmptyGraph(t *testing.T) {
	g := graph.NewDependencyGraph()
	scores := graph.ScoreCriticality(g)
	if len(scores) != 0 {
		t.Errorf("expected 0 scores for empty graph, got %d", len(scores))
	}

	nilScores := graph.ScoreCriticality(nil)
	if len(nilScores) != 0 {
		t.Errorf("expected 0 scores for nil graph, got %d", len(nilScores))
	}
}

func TestScoreCriticality_SingleNode(t *testing.T) {
	g := graph.NewDependencyGraph()
	g.AddNode("standalone-service", nil)

	scores := graph.ScoreCriticality(g)
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}

	if scores[0].ServiceName != "standalone-service" {
		t.Errorf("expected 'standalone-service', got %s", scores[0].ServiceName)
	}
	if scores[0].Score < 0.0 || scores[0].Score > 1.0 {
		t.Errorf("score out of bounds: %f", scores[0].Score)
	}
	if len(scores[0].Reasons) == 0 {
		t.Errorf("expected reasons for standalone service")
	}
}

func TestScoreCriticality_LinearChainAndSPOF(t *testing.T) {
	// frontend -> checkout -> payment
	g := graph.NewDependencyGraph()
	g.AddEdge(graph.DependencyEdge{
		From:      "frontend",
		To:        "checkout",
		CallCount: 1000,
		Frequency: 50.0,
	})
	g.AddEdge(graph.DependencyEdge{
		From:      "checkout",
		To:        "payment",
		CallCount: 800,
		Frequency: 40.0,
	})

	scores := graph.ScoreCriticality(g)
	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}

	scoreMap := make(map[string]graph.ServiceScore)
	for _, s := range scores {
		scoreMap[s.ServiceName] = s
	}

	// 1. Checkout is an intermediary node whose failure isolates payment
	checkout, ok := scoreMap["checkout"]
	if !ok {
		t.Fatalf("missing checkout in scores")
	}

	hasSPOF := false
	for _, r := range checkout.Reasons {
		if strings.Contains(r, "Single Point of Failure") && strings.Contains(r, "payment") {
			hasSPOF = true
			break
		}
	}
	if !hasSPOF {
		t.Errorf("expected checkout to have SPOF reason isolating payment, reasons: %v", checkout.Reasons)
	}

	// 2. Frontend should be identified as Ingress Entrypoint
	frontend, ok := scoreMap["frontend"]
	if !ok {
		t.Fatalf("missing frontend in scores")
	}
	hasIngress := false
	for _, r := range frontend.Reasons {
		if strings.Contains(r, "Ingress Entrypoint") {
			hasIngress = true
			break
		}
	}
	if !hasIngress {
		t.Errorf("expected frontend to have Ingress Entrypoint reason, reasons: %v", frontend.Reasons)
	}

	// 3. Payment should be identified as Leaf Service and have depth 2
	payment, ok := scoreMap["payment"]
	if !ok {
		t.Fatalf("missing payment in scores")
	}
	hasLeaf := false
	hasDepth := false
	for _, r := range payment.Reasons {
		if strings.Contains(r, "Leaf Service") {
			hasLeaf = true
		}
		if strings.Contains(r, "Deep Call Chain") && strings.Contains(r, "depth 2") {
			hasDepth = true
		}
	}
	if !hasLeaf {
		t.Errorf("expected payment to have Leaf Service reason, reasons: %v", payment.Reasons)
	}
	if !hasDepth {
		t.Errorf("expected payment to have Deep Call Chain reason, reasons: %v", payment.Reasons)
	}
}

func TestScoreCriticality_HighInDegreeAndHighOutDegree(t *testing.T) {
	// auth, orders, cart -> payment_db (In-Degree = 3)
	// gateway -> auth, orders, cart (Out-Degree = 3)
	g := graph.NewDependencyGraph()
	g.AddEdge(graph.DependencyEdge{From: "gateway", To: "auth", Frequency: 10.0})
	g.AddEdge(graph.DependencyEdge{From: "gateway", To: "orders", Frequency: 10.0})
	g.AddEdge(graph.DependencyEdge{From: "gateway", To: "cart", Frequency: 10.0})

	g.AddEdge(graph.DependencyEdge{From: "auth", To: "payment_db", Frequency: 10.0})
	g.AddEdge(graph.DependencyEdge{From: "orders", To: "payment_db", Frequency: 20.0})
	g.AddEdge(graph.DependencyEdge{From: "cart", To: "payment_db", Frequency: 15.0})

	scores := graph.ScoreCriticality(g)
	scoreMap := make(map[string]graph.ServiceScore)
	for _, s := range scores {
		scoreMap[s.ServiceName] = s
	}

	// Check payment_db has High In-Degree reason
	db := scoreMap["payment_db"]
	hasHighInDeg := false
	for _, r := range db.Reasons {
		if strings.Contains(r, "High In-Degree: 3 upstream dependent services") {
			hasHighInDeg = true
			break
		}
	}
	if !hasHighInDeg {
		t.Errorf("expected payment_db to have High In-Degree reason, got: %v", db.Reasons)
	}

	// Check gateway has High Out-Degree reason
	gw := scoreMap["gateway"]
	hasHighOutDeg := false
	for _, r := range gw.Reasons {
		if strings.Contains(r, "High Out-Degree: fans out to 3 downstream dependencies") {
			hasHighOutDeg = true
			break
		}
	}
	if !hasHighOutDeg {
		t.Errorf("expected gateway to have High Out-Degree reason, got: %v", gw.Reasons)
	}
}

func TestScoreCriticality_TrafficVolume(t *testing.T) {
	// Traffic concentration on checkout
	g := graph.NewDependencyGraph()
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "checkout", Frequency: 90.0})
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "catalog", Frequency: 10.0})

	scores := graph.ScoreCriticality(g)
	scoreMap := make(map[string]graph.ServiceScore)
	for _, s := range scores {
		scoreMap[s.ServiceName] = s
	}

	checkout := scoreMap["checkout"]
	hasTraffic := false
	for _, r := range checkout.Reasons {
		if strings.Contains(r, "High Traffic Volume") && strings.Contains(r, "90.0%") {
			hasTraffic = true
			break
		}
	}
	if !hasTraffic {
		t.Errorf("expected checkout to have High Traffic Volume reason (90%%), got: %v", checkout.Reasons)
	}
}

func TestScoreCriticality_RedundantPaths_NoSPOF(t *testing.T) {
	// Diamond graph:
	// frontend -> orders -> inventory
	// frontend -> checkout -> inventory
	// Neither orders nor checkout is a SPOF because inventory can be reached through the other
	g := graph.NewDependencyGraph()
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "orders", Frequency: 10.0})
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "checkout", Frequency: 10.0})
	g.AddEdge(graph.DependencyEdge{From: "orders", To: "inventory", Frequency: 10.0})
	g.AddEdge(graph.DependencyEdge{From: "checkout", To: "inventory", Frequency: 10.0})

	scores := graph.ScoreCriticality(g)
	scoreMap := make(map[string]graph.ServiceScore)
	for _, s := range scores {
		scoreMap[s.ServiceName] = s
	}

	orders := scoreMap["orders"]
	for _, r := range orders.Reasons {
		if strings.Contains(r, "Single Point of Failure") {
			t.Errorf("orders should not be marked as SPOF in diamond topology, got reason: %s", r)
		}
	}

	checkout := scoreMap["checkout"]
	for _, r := range checkout.Reasons {
		if strings.Contains(r, "Single Point of Failure") {
			t.Errorf("checkout should not be marked as SPOF in diamond topology, got reason: %s", r)
		}
	}
}

func TestScoreCriticality_RankingOrder(t *testing.T) {
	g := graph.NewDependencyGraph()
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "checkout", Frequency: 50.0})
	g.AddEdge(graph.DependencyEdge{From: "checkout", To: "payment", Frequency: 40.0})

	scores := graph.ScoreCriticality(g)

	for i := 1; i < len(scores); i++ {
		if scores[i].Score > scores[i-1].Score {
			t.Errorf("scores not sorted descending: %f > %f", scores[i].Score, scores[i-1].Score)
		}
	}
}

func TestAnalyzer_ScoreCriticality(t *testing.T) {
	analyzer := graph.NewAnalyzer("http://localhost:16686", nil)
	g := graph.NewDependencyGraph()
	g.AddEdge(graph.DependencyEdge{From: "frontend", To: "checkout", Frequency: 20.0})

	scores := analyzer.ScoreCriticality(g)
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores from analyzer.ScoreCriticality, got %d", len(scores))
	}

	if len(scores[0].Reasons) == 0 {
		t.Errorf("expected reasons populated for top scored service")
	}
}
