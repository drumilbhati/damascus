package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Jaeger API response data structures
type jaegerResponse struct {
	Data []jaegerTrace `json:"data"`
}

type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Spans     []jaegerSpan             `json:"spans"`
	Processes map[string]jaegerProcess `json:"processes"`
}

type jaegerSpan struct {
	SpanID     string            `json:"spanID"`
	ProcessID  string            `json:"processID"`
	References []jaegerReference `json:"references"`
	Duration   int64             `json:"duration"` // Microseconds
}

type jaegerReference struct {
	RefType string `json:"refType"` // "CHILD_OF"
	SpanID  string `json:"spanID"`
}

type jaegerProcess struct {
	ServiceName string `json:"serviceName"`
}

type jaegerServicesResponse struct {
	Data []string `json:"data"`
}

// Analyzer implements the interfaces.GraphAnalyzer contract.
type Analyzer struct {
	jaegerBaseURL string
	client        *http.Client
}

// NewAnalyzer initializes a new GraphAnalyzer instance.
func NewAnalyzer(jaegerBaseURL string, client *http.Client) *Analyzer {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Analyzer{
		jaegerBaseURL: strings.TrimRight(jaegerBaseURL, "/"),
		client:        client,
	}
}

// BuildGraph fetches traces from Jaeger and stitches them into a DependencyGraph.
func (a *Analyzer) BuildGraph(ctx context.Context, lookbackDuration int64) (*DependencyGraph, error) {
	graph := NewDependencyGraph()

	// TODO 1: Fetch list of active services from /api/services
	// e.g. GET a.jaegerBaseURL + "/api/services"
	// Decode into jaegerServicesResponse
	url := a.jaegerBaseURL + "/api/services"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var jaegerServices jaegerServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&jaegerServices); err != nil {
		return nil, err
	}

	// TODO 2: For each discovered service, query /api/traces?service=<service>&limit=50
	// e.g. GET fmt.Sprintf("%s/api/traces?service=%s&limit=50", a.jaegerBaseURL, service)
	// Decode into jaegerResponse
	for _, service := range jaegerServices.Data {
		url := a.jaegerBaseURL + "/api/traces?service=" + service + "&limit=50"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}

		var jaegerResp jaegerResponse
		err = json.NewDecoder(resp.Body).Decode(&jaegerResp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		// TODO 3: Iterate through traces and spans:
		//   a) Build a map of spanID -> serviceName for quick parent lookup:
		//      spanToService := make(map[string]string)
		//   b) For each span:
		//      childService := trace.Processes[span.ProcessID].ServiceName
		//      graph.AddNode(childService, nil)
		//
		//   c) Check span.References for "CHILD_OF" parentSpanID:
		//      parentService := spanToService[parentSpanID]
		//      if parentService != "" && parentService != childService {
		//          // Record edge: parentService -> childService
		//          graph.AddEdge(DependencyEdge{
		//		From: parentService,
		//		To: childService,
		//		CallCount: 1, // aggregate counts
		//          })
		//      }

		for _, trace := range jaegerResp.Data {
			spanToService := make(map[string]string)
			for _, span := range trace.Spans {
				serviceName := trace.Processes[span.ProcessID].ServiceName
				spanToService[span.SpanID] = serviceName
				graph.AddNode(serviceName, nil)
			}

			for _, span := range trace.Spans {
				childService := spanToService[span.SpanID]
				for _, ref := range span.References {
					if ref.RefType == "CHILD_OF" {
						parentService := spanToService[ref.SpanID]
						if parentService != "" && parentService != childService {
							graph.AddEdge(DependencyEdge{
								From:      parentService,
								To:        childService,
								CallCount: 1,
								Frequency: 1.0,
							})
						}
					}
				}
			}
		}
	}

	// TODO 4: Return the populated graph
	return graph, nil
}

// ScoreCriticality will be implemented in Phase 7.2.
func (a *Analyzer) ScoreCriticality(g *DependencyGraph) []ServiceScore {
	return nil
}
