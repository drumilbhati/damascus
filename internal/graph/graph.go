package graph

// ServiceNode represents a microservice within the dependency topology.
type ServiceNode struct {
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DependencyEdge represents a directed call relationship from caller (From) to callee (To).
type DependencyEdge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	CallCount int64   `json:"call_count"`
	Frequency float64 `json:"frequency"` // Calls per second
}

// DependencyGraph represents the full service mesh topology graph.
type DependencyGraph struct {
	Nodes map[string]*ServiceNode `json:"nodes"`
	Edges []DependencyEdge        `json:"edges"`
}

// NewDependencyGraph initializes an empty dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		Nodes: make(map[string]*ServiceNode),
		Edges: make([]DependencyEdge, 0),
	}
}

// AddNode adds a service node to the graph if it does not already exist.
func (g *DependencyGraph) AddNode(name string, metadata map[string]string) *ServiceNode {
	if node, exists := g.Nodes[name]; exists {
		return node
	}
	node := &ServiceNode{
		Name:     name,
		Metadata: metadata,
	}
	g.Nodes[name] = node
	return node
}

// AddEdge records a directed dependency edge and ensures both endpoints exist as nodes.
func (g *DependencyGraph) AddEdge(edge DependencyEdge) {
	g.AddNode(edge.From, nil)
	g.AddNode(edge.To, nil)
	g.Edges = append(g.Edges, edge)
}

// InDegree returns the number of incoming edges to the given service (callers).
func (g *DependencyGraph) InDegree(service string) int {
	count := 0
	for _, edge := range g.Edges {
		if edge.To == service {
			count++
		}
	}
	return count
}

// OutDegree returns the number of outgoing edges from the given service (callees).
func (g *DependencyGraph) OutDegree(service string) int {
	count := 0
	for _, edge := range g.Edges {
		if edge.From == service {
			count++
		}
	}
	return count
}

// ServiceScore holds the calculated criticality score and diagnostic rationale for a service.
type ServiceScore struct {
	ServiceName string   `json:"service_name"`
	Score       float64  `json:"score"` // Normalized 0.0 to 1.0
	Reasons     []string `json:"reasons"`
}
