package graph

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// CriticalityWeights defines the weight configuration for the criticality score formula:
// S(v) = w1*InDegree + w2*OutDegree + w3*CallFreq + w4*Depth + w5*SPOF
type CriticalityWeights struct {
	InDegree  float64
	OutDegree float64
	CallFreq  float64
	Depth     float64
	SPOF      float64
}

// DefaultCriticalityWeights provides the canonical weights specified in LLD Section 6.1.
// w1 = 0.35, w2 = 0.15, w3 = 0.25, w4 = 0.10, w5 = 0.15 (sum = 1.00).
var DefaultCriticalityWeights = CriticalityWeights{
	InDegree:  0.35,
	OutDegree: 0.15,
	CallFreq:  0.25,
	Depth:     0.10,
	SPOF:      0.15,
}

// NodeMetrics captures raw graph metrics for a single service node used during scoring.
type NodeMetrics struct {
	ServiceName   string
	InDegree      int
	OutDegree     int
	Callers       []string
	Callees       []string
	CallFrequency float64
	Depth         int
	IsSPOF        bool
	IsolatedNodes []string
}

// ScoreCriticality calculates weighted criticality scores and explainable rationale
// for all services in the graph using DefaultCriticalityWeights.
// Returns a slice of ServiceScore sorted by Score in descending order.
func ScoreCriticality(g *DependencyGraph) []ServiceScore {
	return ScoreCriticalityWithWeights(g, DefaultCriticalityWeights)
}

// ScoreCriticalityWithWeights calculates criticality scores using custom weights.
func ScoreCriticalityWithWeights(g *DependencyGraph, weights CriticalityWeights) []ServiceScore {
	if g == nil || len(g.Nodes) == 0 {
		return []ServiceScore{}
	}

	metrics := collectNodeMetrics(g)
	spofMap, isolatedMap := identifySPOFs(g)
	depthMap := computeDepths(g)

	var maxIn, maxOut, maxDepth int
	var maxFreq, meshTotalFreq float64

	for name, m := range metrics {
		m.IsSPOF = spofMap[name]
		m.IsolatedNodes = isolatedMap[name]
		m.Depth = depthMap[name]
		metrics[name] = m

		if m.InDegree > maxIn {
			maxIn = m.InDegree
		}
		if m.OutDegree > maxOut {
			maxOut = m.OutDegree
		}
		if m.Depth > maxDepth {
			maxDepth = m.Depth
		}
		if m.CallFrequency > maxFreq {
			maxFreq = m.CallFrequency
		}
		meshTotalFreq += m.CallFrequency
	}

	scores := make([]ServiceScore, 0, len(metrics))

	for _, m := range metrics {
		var normIn, normOut, normFreq, normDepth, normSPOF float64

		if maxIn > 0 {
			normIn = float64(m.InDegree) / float64(maxIn)
		}
		if maxOut > 0 {
			normOut = float64(m.OutDegree) / float64(maxOut)
		}
		if maxFreq > 0 {
			normFreq = m.CallFrequency / maxFreq
		}
		if maxDepth > 0 {
			normDepth = float64(m.Depth) / float64(maxDepth)
		}
		if m.IsSPOF {
			normSPOF = 1.0
		}

		rawScore := weights.InDegree*normIn +
			weights.OutDegree*normOut +
			weights.CallFreq*normFreq +
			weights.Depth*normDepth +
			weights.SPOF*normSPOF

		// Bound to [0.0, 1.0] and round to 2 decimal places
		normalizedScore := math.Min(1.0, math.Max(0.0, rawScore))
		roundedScore := math.Round(normalizedScore*100) / 100

		reasons := GenerateReasons(m, meshTotalFreq)

		scores = append(scores, ServiceScore{
			ServiceName: m.ServiceName,
			Score:       roundedScore,
			Reasons:     reasons,
		})
	}

	// Sort descending by score; if tied, sort alphabetically by service name
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Score != scores[j].Score {
			return scores[i].Score > scores[j].Score
		}
		return scores[i].ServiceName < scores[j].ServiceName
	})

	return scores
}

// GenerateReasons produces human-readable diagnostic explanation strings detailing
// why a service received its criticality score.
func GenerateReasons(m NodeMetrics, meshTotalFreq float64) []string {
	reasons := make([]string, 0)

	// 1. Single Point of Failure (SPOF)
	if m.IsSPOF {
		if len(m.IsolatedNodes) > 0 {
			sort.Strings(m.IsolatedNodes)
			reasons = append(reasons, fmt.Sprintf(
				"Single Point of Failure: %s bottleneck isolates %d downstream services (%s)",
				m.ServiceName, len(m.IsolatedNodes), strings.Join(m.IsolatedNodes, ", "),
			))
		} else {
			reasons = append(reasons, fmt.Sprintf(
				"Single Point of Failure: %s route bottleneck with no redundant path",
				m.ServiceName,
			))
		}
	}

	// 2. In-Degree / Upstream blast radius
	if m.InDegree >= 2 {
		reasons = append(reasons, fmt.Sprintf(
			"High In-Degree: %d upstream dependent services (%s)",
			m.InDegree, strings.Join(m.Callers, ", "),
		))
	} else if m.InDegree == 1 {
		reasons = append(reasons, fmt.Sprintf(
			"Upstream Dependency: called by %s",
			m.Callers[0],
		))
	} else {
		reasons = append(reasons, "Ingress Entrypoint: directly receives external client traffic")
	}

	// 3. Traffic Volume / Throughput Concentration
	if meshTotalFreq > 0 && m.CallFrequency > 0 {
		pct := (m.CallFrequency / meshTotalFreq) * 100
		if pct >= 20.0 {
			reasons = append(reasons, fmt.Sprintf(
				"High Traffic Volume: handles %.1f calls/sec (%.1f%% of mesh traffic)",
				m.CallFrequency, pct,
			))
		} else {
			reasons = append(reasons, fmt.Sprintf(
				"Traffic Throughput: handles %.1f calls/sec across incoming routes",
				m.CallFrequency,
			))
		}
	}

	// 4. Out-Degree / Fan-Out Complexity
	if m.OutDegree >= 2 {
		reasons = append(reasons, fmt.Sprintf(
			"High Out-Degree: fans out to %d downstream dependencies (%s)",
			m.OutDegree, strings.Join(m.Callees, ", "),
		))
	} else if m.OutDegree == 0 {
		reasons = append(reasons, "Leaf Service: terminal node with no downstream dependencies")
	}

	// 5. Execution Depth
	if m.Depth >= 2 {
		reasons = append(reasons, fmt.Sprintf(
			"Deep Call Chain: depth %d from ingress",
			m.Depth,
		))
	}

	return reasons
}

// collectNodeMetrics aggregates degree counts, caller/callee names, and frequencies per node.
func collectNodeMetrics(g *DependencyGraph) map[string]NodeMetrics {
	metrics := make(map[string]NodeMetrics, len(g.Nodes))
	for name := range g.Nodes {
		metrics[name] = NodeMetrics{
			ServiceName: name,
			Callers:     make([]string, 0),
			Callees:     make([]string, 0),
		}
	}

	callersSet := make(map[string]map[string]bool)
	calleesSet := make(map[string]map[string]bool)
	freqMap := make(map[string]float64)

	for name := range g.Nodes {
		callersSet[name] = make(map[string]bool)
		calleesSet[name] = make(map[string]bool)
	}

	for _, edge := range g.Edges {
		if _, ok := callersSet[edge.To]; ok {
			callersSet[edge.To][edge.From] = true
			effFreq := edge.Frequency
			if effFreq == 0 && edge.CallCount > 0 {
				effFreq = float64(edge.CallCount)
			}
			freqMap[edge.To] += effFreq
		}
		if _, ok := calleesSet[edge.From]; ok {
			calleesSet[edge.From][edge.To] = true
		}
	}

	for name, m := range metrics {
		for caller := range callersSet[name] {
			m.Callers = append(m.Callers, caller)
		}
		sort.Strings(m.Callers)
		m.InDegree = len(m.Callers)

		for callee := range calleesSet[name] {
			m.Callees = append(m.Callees, callee)
		}
		sort.Strings(m.Callees)
		m.OutDegree = len(m.Callees)

		m.CallFrequency = freqMap[name]
		metrics[name] = m
	}

	return metrics
}

// identifySPOFs detects services whose removal isolates one or more downstream services
// from the ingress root nodes. Returns a map of node -> isSPOF and node -> isolatedNodeList.
func identifySPOFs(g *DependencyGraph) (map[string]bool, map[string][]string) {
	isSPOF := make(map[string]bool, len(g.Nodes))
	isolatedMap := make(map[string][]string, len(g.Nodes))

	for name := range g.Nodes {
		isSPOF[name] = false
		isolatedMap[name] = make([]string, 0)
	}

	if len(g.Nodes) <= 2 {
		return isSPOF, isolatedMap
	}

	// Ingress roots have in-degree 0
	roots := make([]string, 0)
	for name := range g.Nodes {
		if g.InDegree(name) == 0 {
			roots = append(roots, name)
		}
	}
	// Fallback: if graph is entirely cyclic with no in-degree 0 nodes, use all nodes
	if len(roots) == 0 {
		for name := range g.Nodes {
			roots = append(roots, name)
		}
	}

	// Find reachability from roots before removing any node
	reachableBefore := getReachableNodes(g, roots, "")

	// Test each candidate node by simulating its failure (removal)
	for candidate := range g.Nodes {
		// Effective roots excluding the candidate node
		activeRoots := make([]string, 0)
		for _, r := range roots {
			if r != candidate {
				activeRoots = append(activeRoots, r)
			}
		}

		if len(activeRoots) == 0 {
			// If candidate was the sole root, removing it isolates all nodes it was reaching
			continue
		}

		reachableAfter := getReachableNodes(g, activeRoots, candidate)

		isolated := make([]string, 0)
		for node := range reachableBefore {
			if node != candidate && !reachableAfter[node] {
				isolated = append(isolated, node)
			}
		}

		if len(isolated) > 0 {
			isSPOF[candidate] = true
			isolatedMap[candidate] = isolated
		}
	}

	return isSPOF, isolatedMap
}

// getReachableNodes performs BFS traversal from roots, skipping the excluded node.
func getReachableNodes(g *DependencyGraph, roots []string, exclude string) map[string]bool {
	visited := make(map[string]bool)
	queue := make([]string, 0, len(roots))

	// Build adjacency list for forward traversal
	adj := make(map[string][]string)
	for _, edge := range g.Edges {
		adj[edge.From] = append(adj[edge.From], edge.To)
	}

	for _, r := range roots {
		if r != exclude && !visited[r] {
			visited[r] = true
			queue = append(queue, r)
		}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, next := range adj[curr] {
			if next != exclude && !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}

	return visited
}

// computeDepths calculates shortest distance from ingress roots using BFS.
func computeDepths(g *DependencyGraph) map[string]int {
	depths := make(map[string]int, len(g.Nodes))
	for name := range g.Nodes {
		depths[name] = 0
	}

	roots := make([]string, 0)
	for name := range g.Nodes {
		if g.InDegree(name) == 0 {
			roots = append(roots, name)
		}
	}

	if len(roots) == 0 {
		return depths
	}

	adj := make(map[string][]string)
	for _, edge := range g.Edges {
		adj[edge.From] = append(adj[edge.From], edge.To)
	}

	visited := make(map[string]bool)
	queue := make([]string, 0, len(roots))

	for _, r := range roots {
		depths[r] = 0
		visited[r] = true
		queue = append(queue, r)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for _, next := range adj[curr] {
			if !visited[next] {
				visited[next] = true
				depths[next] = depths[curr] + 1
				queue = append(queue, next)
			}
		}
	}

	return depths
}
