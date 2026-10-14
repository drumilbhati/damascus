package graph_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"damascus/internal/graph"
)

func TestLiveJaeger_Teamsynch(t *testing.T) {
	analyzer := graph.NewAnalyzer("https://jaeger.teamsynch.tech", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	g, err := analyzer.BuildGraph(ctx, 3600)
	if err != nil {
		t.Fatalf("Failed to build graph from live Jaeger: %v", err)
	}

	fmt.Printf("\n=============================================================\n")
	fmt.Printf(" LIVE DEPENDENCY GRAPH from https://jaeger.teamsynch.tech\n")
	fmt.Printf("=============================================================\n")
	fmt.Printf("Total Services Discovered: %d\n", len(g.Nodes))
	for name := range g.Nodes {
		fmt.Printf("  • %-24s -> InDegree: %d | OutDegree: %d\n",
			name, g.InDegree(name), g.OutDegree(name))
	}

	scores := analyzer.ScoreCriticality(g)
	fmt.Printf("\n=============================================================\n")
	fmt.Printf(" TOPOLOGICAL CRITICALITY RANKINGS\n")
	fmt.Printf("=============================================================\n")
	for i, s := range scores {
		fmt.Printf("%2d. %-24s Score: %.2f\n", i+1, s.ServiceName, s.Score)
		for _, r := range s.Reasons {
			fmt.Printf("     └─ %s\n", r)
		}
	}
	fmt.Println("=============================================================")
}
