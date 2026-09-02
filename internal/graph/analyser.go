package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

	// 1. Fetch list of active services from /api/services
	servicesURL := a.jaegerBaseURL + "/api/services"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, servicesURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected status code from jaeger /api/services: %d", resp.StatusCode)
	}

	var jaegerServices jaegerServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&jaegerServices); err != nil {
		return nil, err
	}

	lookbackStr := "1h"
	if lookbackDuration > 0 {
		lookbackStr = fmt.Sprintf("%ds", lookbackDuration)
	}

	// 2. For each discovered service, query /api/traces
	for _, service := range jaegerServices.Data {
		queryParams := url.Values{}
		queryParams.Set("service", service)
		queryParams.Set("limit", "50")
		queryParams.Set("lookback", lookbackStr)

		traceURL := fmt.Sprintf("%s/api/traces?%s", a.jaegerBaseURL, queryParams.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected status code from jaeger /api/traces: %d", resp.StatusCode)
		}

		var jaegerResp jaegerResponse
		err = json.NewDecoder(resp.Body).Decode(&jaegerResp)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		// 3. Iterate through traces and spans:
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

	// 4. Return the populated graph
	return graph, nil
}

// ScoreCriticality will be implemented in Phase 7.2.
func (a *Analyzer) ScoreCriticality(g *DependencyGraph) []ServiceScore {
	return nil
}
