package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

// Mocks

type mockExperimentService struct {
	createFn    func(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error)
	startFn     func(ctx context.Context, id string, plan stress.LoadPlan) error
	stopFn      func(ctx context.Context, id, reason string) error
	getStatusFn func(ctx context.Context, id string) (*experiment.Experiment, error)
}

func (m *mockExperimentService) CreateExperiment(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error) {
	if m.createFn != nil {
		return m.createFn(ctx, targetService, targetURL, expType, config)
	}
	return nil, nil
}

func (m *mockExperimentService) StartExperiment(ctx context.Context, id string, plan stress.LoadPlan) error {
	if m.startFn != nil {
		return m.startFn(ctx, id, plan)
	}
	return nil
}

func (m *mockExperimentService) StopExperiment(ctx context.Context, id, reason string) error {
	if m.stopFn != nil {
		return m.stopFn(ctx, id, reason)
	}
	return nil
}

func (m *mockExperimentService) GetStatus(ctx context.Context, id string) (*experiment.Experiment, error) {
	if m.getStatusFn != nil {
		return m.getStatusFn(ctx, id)
	}
	return nil, nil
}

type mockExperimentRepository struct {
	getByIDFn         func(ctx context.Context, id string) (*experiment.Experiment, error)
	listFn            func(ctx context.Context) ([]experiment.Experiment, error)
	getReportFn       func(ctx context.Context, expID string) (*capacity.ExperimentReport, error)
	getObservationsFn func(ctx context.Context, expID string) ([]capacity.Observation, error)
}

func (m *mockExperimentRepository) GetByID(ctx context.Context, id string) (*experiment.Experiment, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockExperimentRepository) List(ctx context.Context) ([]experiment.Experiment, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockExperimentRepository) GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error) {
	if m.getReportFn != nil {
		return m.getReportFn(ctx, expID)
	}
	return nil, nil
}

func (m *mockExperimentRepository) GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error) {
	if m.getObservationsFn != nil {
		return m.getObservationsFn(ctx, expID)
	}
	return nil, nil
}

type mockGraphAnalyzer struct {
	buildGraphFn       func(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error)
	scoreCriticalityFn func(g *graph.DependencyGraph) []graph.ServiceScore
}

func (m *mockGraphAnalyzer) BuildGraph(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error) {
	if m.buildGraphFn != nil {
		return m.buildGraphFn(ctx, lookbackDuration)
	}
	return graph.NewDependencyGraph(), nil
}

func (m *mockGraphAnalyzer) ScoreCriticality(g *graph.DependencyGraph) []graph.ServiceScore {
	if m.scoreCriticalityFn != nil {
		return m.scoreCriticalityFn(g)
	}
	return nil
}

type mockSnapshotProvider struct {
	getLatestSnapshotFn func(ctx context.Context, expID string) (*watcher.MetricSnapshot, error)
}

func (m *mockSnapshotProvider) GetLatestSnapshot(ctx context.Context, expID string) (*watcher.MetricSnapshot, error) {
	if m.getLatestSnapshotFn != nil {
		return m.getLatestSnapshotFn(ctx, expID)
	}
	return nil, nil
}

type mockObservabilityChecker struct {
	checkHealthFn func(ctx context.Context) (map[string]string, error)
}

func (m *mockObservabilityChecker) CheckHealth(ctx context.Context) (map[string]string, error) {
	if m.checkHealthFn != nil {
		return m.checkHealthFn(ctx)
	}
	return map[string]string{"target": "CONNECTED", "jaeger": "CONNECTED", "prometheus": "CONNECTED"}, nil
}

// Tests

func TestHandler_CreateExperiment(t *testing.T) {
	validConfig := experiment.ExperimentConfig{
		InitialRate:         10,
		StepRate:            10,
		MaxRate:             50,
		StepDurationSeconds: 5,
		MaxP95LatencyMs:     100,
		MaxErrorRatePercent: 1.0,
		MinAvailabilityPct:  99.0,
		RecoveryWindowSec:   5,
	}

	tests := []struct {
		name       string
		dto        any
		serviceFn  func(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error)
		wantStatus int
	}{
		{
			name: "successful creation ramp",
			dto: CreateExperimentDTO{
				TargetService: "checkout",
				TargetURL:     "http://checkout:8080/checkout",
				Type:          experiment.ExperimentRamp,
				Config:        validConfig,
			},
			serviceFn: func(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error) {
				return &experiment.Experiment{
					ID:            "exp-123",
					TargetService: targetService,
					TargetURL:     targetURL,
					Type:          expType,
					State:         experiment.StateCreated,
					Config:        config,
					CreatedAt:     time.Now(),
				}, nil
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing target service",
			dto: CreateExperimentDTO{
				TargetService: "",
				TargetURL:     "http://checkout:8080",
				Type:          experiment.ExperimentRamp,
				Config:        validConfig,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unsupported experiment type",
			dto: CreateExperimentDTO{
				TargetService: "checkout",
				TargetURL:     "http://checkout:8080",
				Type:          "invalid-type",
				Config:        validConfig,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid config validation",
			dto: CreateExperimentDTO{
				TargetService: "checkout",
				TargetURL:     "http://checkout:8080",
				Type:          experiment.ExperimentStep,
				Config: experiment.ExperimentConfig{
					InitialRate: 0, // Invalid
				},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json payload",
			dto:        "invalid json string",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockExperimentService{createFn: tt.serviceFn}
			h := NewHandler(svc, nil, nil)

			var body []byte
			switch v := tt.dto.(type) {
			case string:
				body = []byte(v)
			default:
				body, _ = json.Marshal(v)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/experiments", bytes.NewReader(body))
			rec := httptest.NewRecorder()

			h.CreateExperiment(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantStatus == http.StatusCreated {
				var created experiment.Experiment
				if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
					t.Fatalf("failed to decode created experiment: %v", err)
				}
				if created.ID != "exp-123" {
					t.Errorf("expected id exp-123, got %s", created.ID)
				}
			}
		})
	}

	t.Run("body exceeding max limit returns 413", func(t *testing.T) {
		svc := &mockExperimentService{}
		h := NewHandler(svc, nil, nil)

		// Create > 1 MB payload with valid JSON syntax so decoder reaches limit
		largeJSON := `{"target_service":"a","target_url":"http://a","type":"ramp","padding":"` + strings.Repeat("x", 2<<20) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/experiments", strings.NewReader(largeJSON))
		rec := httptest.NewRecorder()

		h.CreateExperiment(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected status 413, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestHandler_StartExperiment(t *testing.T) {
	exp := &experiment.Experiment{
		ID:            "exp-123",
		TargetService: "checkout",
		TargetURL:     "http://checkout:8080/checkout",
		Type:          experiment.ExperimentRamp,
		State:         experiment.StateCreated,
		Config: experiment.ExperimentConfig{
			InitialRate:         10,
			StepRate:            10,
			MaxRate:             50,
			StepDurationSeconds: 5,
			MaxP95LatencyMs:     100,
			MaxErrorRatePercent: 1.0,
			MinAvailabilityPct:  99.0,
			RecoveryWindowSec:   5,
		},
	}

	tests := []struct {
		name       string
		expID      string
		body       any
		repoGet    func(ctx context.Context, id string) (*experiment.Experiment, error)
		serviceFn  func(ctx context.Context, id string, plan stress.LoadPlan) error
		wantStatus int
	}{
		{
			name:  "start with empty body uses config defaults",
			expID: "exp-123",
			body:  nil,
			repoGet: func(ctx context.Context, id string) (*experiment.Experiment, error) {
				return exp, nil
			},
			serviceFn: func(ctx context.Context, id string, plan stress.LoadPlan) error {
				if plan.InitialRate != 10 || plan.MaxRate != 50 {
					return errors.New("unexpected load plan")
				}
				return nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "start with custom plan",
			expID: "exp-123",
			body: map[string]any{
				"plan": map[string]any{
					"target_url":            "http://custom:8080",
					"method":                "POST",
					"initial_rate":          20,
					"step_rate":             10,
					"max_rate":              100,
					"step_duration_seconds": 10,
				},
			},
			repoGet: func(ctx context.Context, id string) (*experiment.Experiment, error) {
				return exp, nil
			},
			serviceFn: func(ctx context.Context, id string, plan stress.LoadPlan) error {
				if plan.TargetURL != "http://custom:8080" || plan.Method != "POST" || plan.InitialRate != 20 {
					return errors.New("custom plan not applied")
				}
				return nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "experiment not found",
			expID: "non-existent",
			body:  nil,
			repoGet: func(ctx context.Context, id string) (*experiment.Experiment, error) {
				return nil, nil
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "experiment not in StateCreated",
			expID: "exp-running",
			body:  nil,
			repoGet: func(ctx context.Context, id string) (*experiment.Experiment, error) {
				running := *exp
				running.State = experiment.StateRunning
				return &running, nil
			},
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockExperimentService{startFn: tt.serviceFn}
			repo := &mockExperimentRepository{getByIDFn: tt.repoGet}
			h := NewHandler(svc, repo, nil)

			var body []byte
			if tt.body != nil {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/experiments/"+tt.expID+"/start", bytes.NewReader(body))
			req.SetPathValue("id", tt.expID)
			rec := httptest.NewRecorder()

			h.StartExperiment(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var resp StatusResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.Status != "RUNNING" {
					t.Errorf("expected status RUNNING, got %s", resp.Status)
				}
			}
		})
	}
}

func TestHandler_StopExperiment(t *testing.T) {
	tests := []struct {
		name       string
		expID      string
		body       any
		stopFn     func(ctx context.Context, id, reason string) error
		wantStatus int
	}{
		{
			name:  "successful stop",
			expID: "exp-123",
			body:  StopExperimentDTO{Reason: "user cancelled"},
			stopFn: func(ctx context.Context, id, reason string) error {
				if reason != "user cancelled" {
					return errors.New("expected reason 'user cancelled'")
				}
				return nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "stop experiment not actively running",
			expID: "exp-stopped",
			body:  nil,
			stopFn: func(ctx context.Context, id, reason string) error {
				return errors.New("experiment exp-stopped is not actively running")
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "malformed json returns 400",
			expID:      "exp-123",
			body:       "{invalid-json-body",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockExperimentService{stopFn: tt.stopFn}
			h := NewHandler(svc, nil, nil)

			var body []byte
			switch v := tt.body.(type) {
			case string:
				body = []byte(v)
			default:
				if tt.body != nil {
					body, _ = json.Marshal(tt.body)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/api/experiments/"+tt.expID+"/stop", bytes.NewReader(body))
			req.SetPathValue("id", tt.expID)
			rec := httptest.NewRecorder()

			h.StopExperiment(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var resp StatusResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.Status != "STOPPING" {
					t.Errorf("expected status STOPPING, got %s", resp.Status)
				}
			}
		})
	}
}

func TestHandler_GetExperiment(t *testing.T) {
	exp := &experiment.Experiment{
		ID:            "exp-123",
		TargetService: "frontend",
		TargetURL:     "http://frontend:8080",
		Type:          experiment.ExperimentRamp,
		State:         experiment.StateRunning,
	}

	repo := &mockExperimentRepository{
		getByIDFn: func(ctx context.Context, id string) (*experiment.Experiment, error) {
			if id == "exp-123" {
				return exp, nil
			}
			return nil, nil
		},
	}
	h := NewHandler(nil, repo, nil)

	// Existing
	req := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123", nil)
	req.SetPathValue("id", "exp-123")
	rec := httptest.NewRecorder()
	h.GetExperiment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Missing
	req = httptest.NewRequest(http.MethodGet, "/api/experiments/unknown", nil)
	req.SetPathValue("id", "unknown")
	rec = httptest.NewRecorder()
	h.GetExperiment(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_ListExperiments(t *testing.T) {
	experiments := []experiment.Experiment{
		{ID: "exp-1", TargetService: "frontend"},
		{ID: "exp-2", TargetService: "cartservice"},
	}

	repo := &mockExperimentRepository{
		listFn: func(ctx context.Context) ([]experiment.Experiment, error) {
			return experiments, nil
		},
	}
	h := NewHandler(nil, repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/experiments", nil)
	rec := httptest.NewRecorder()
	h.ListExperiments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var res []experiment.Experiment
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode list: %v", err)
	}
	if len(res) != 2 {
		t.Errorf("expected 2 experiments, got %d", len(res))
	}
}

func TestHandler_GetExperimentStatus(t *testing.T) {
	exp := &experiment.Experiment{
		ID:            "exp-123",
		TargetService: "checkout",
		CreatedAt:     time.Now(),
	}

	t.Run("from SnapshotProvider", func(t *testing.T) {
		provider := &mockSnapshotProvider{
			getLatestSnapshotFn: func(ctx context.Context, expID string) (*watcher.MetricSnapshot, error) {
				return &watcher.MetricSnapshot{
					ExperimentID: expID,
					RequestRate:  100,
					P95LatencyMs: 45.5,
				}, nil
			},
		}
		repo := &mockExperimentRepository{
			getByIDFn: func(ctx context.Context, id string) (*experiment.Experiment, error) {
				return exp, nil
			},
		}
		h := NewHandler(nil, repo, nil, WithSnapshotProvider(provider))

		req := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/status", nil)
		req.SetPathValue("id", "exp-123")
		rec := httptest.NewRecorder()
		h.GetExperimentStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var snapshot watcher.MetricSnapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatalf("failed to decode snapshot: %v", err)
		}
		if snapshot.RequestRate != 100 || snapshot.P95LatencyMs != 45.5 {
			t.Errorf("unexpected snapshot data: %+v", snapshot)
		}
	})

	t.Run("fallback to repository observations", func(t *testing.T) {
		repo := &mockExperimentRepository{
			getByIDFn: func(ctx context.Context, id string) (*experiment.Experiment, error) {
				return exp, nil
			},
			getObservationsFn: func(ctx context.Context, expID string) ([]capacity.Observation, error) {
				return []capacity.Observation{
					{LoadRate: 50, P95LatencyMs: 30},
					{LoadRate: 75, P95LatencyMs: 40},
				}, nil
			},
		}
		h := NewHandler(nil, repo, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/status", nil)
		req.SetPathValue("id", "exp-123")
		rec := httptest.NewRecorder()
		h.GetExperimentStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var snapshot watcher.MetricSnapshot
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatalf("failed to decode snapshot: %v", err)
		}
		if snapshot.RequestRate != 75 || snapshot.P95LatencyMs != 40 {
			t.Errorf("expected latest observation rate 75, got %v", snapshot.RequestRate)
		}
	})

	t.Run("returns 500 when neither repo nor service is configured", func(t *testing.T) {
		h := NewHandler(nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/status", nil)
		req.SetPathValue("id", "exp-123")
		rec := httptest.NewRecorder()
		h.GetExperimentStatus(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})
}

func TestHandler_GetDependencies(t *testing.T) {
	depGraph := graph.NewDependencyGraph()
	depGraph.AddEdge(graph.DependencyEdge{
		From:      "frontend",
		To:        "cartservice",
		CallCount: 100,
	})
	scores := []graph.ServiceScore{
		{ServiceName: "cartservice", Score: 0.8},
		{ServiceName: "frontend", Score: 0.5},
	}

	analyzer := &mockGraphAnalyzer{
		buildGraphFn: func(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error) {
			if lookbackDuration != 7200 {
				t.Errorf("expected lookback 7200, got %d", lookbackDuration)
			}
			return depGraph, nil
		},
		scoreCriticalityFn: func(g *graph.DependencyGraph) []graph.ServiceScore {
			return scores
		},
	}

	h := NewHandler(nil, nil, analyzer)

	req := httptest.NewRequest(http.MethodGet, "/api/dependencies?lookback=7200", nil)
	rec := httptest.NewRecorder()
	h.GetDependencies(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp DependenciesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode dependencies: %v", err)
	}
	if len(resp.Scores) != 2 || resp.Graph == nil {
		t.Errorf("unexpected dependencies response: %+v", resp)
	}
}

func TestHandler_GetExperimentReport(t *testing.T) {
	exp := &experiment.Experiment{
		ID:            "exp-123",
		TargetService: "payment",
	}
	report := &capacity.ExperimentReport{
		ExperimentID:           "exp-123",
		TargetService:          "payment",
		MaximumSustainableRate: 200,
		GeneratedAt:            time.Now(),
	}

	repo := &mockExperimentRepository{
		getByIDFn: func(ctx context.Context, id string) (*experiment.Experiment, error) {
			if id == "exp-123" {
				return exp, nil
			}
			return nil, nil
		},
		getReportFn: func(ctx context.Context, expID string) (*capacity.ExperimentReport, error) {
			if expID == "exp-123" {
				return report, nil
			}
			return nil, nil
		},
	}

	h := NewHandler(nil, repo, nil)

	// Existing report
	req := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/report", nil)
	req.SetPathValue("id", "exp-123")
	rec := httptest.NewRecorder()
	h.GetExperimentReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if vary := rec.Header().Get("Vary"); vary != "Accept" {
		t.Errorf("expected Vary: Accept header on JSON response, got %q", vary)
	}

	// HTML format request via query parameter
	reqHTML := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/report?format=html", nil)
	reqHTML.SetPathValue("id", "exp-123")
	recHTML := httptest.NewRecorder()
	h.GetExperimentReport(recHTML, reqHTML)

	if recHTML.Code != http.StatusOK {
		t.Fatalf("expected 200 for HTML report, got %d", recHTML.Code)
	}
	if vary := recHTML.Header().Get("Vary"); vary != "Accept" {
		t.Errorf("expected Vary: Accept header on HTML response, got %q", vary)
	}
	if ct := recHTML.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}
	if !strings.Contains(recHTML.Body.String(), "<!DOCTYPE html>") {
		t.Errorf("expected HTML content in response body, got: %s", recHTML.Body.String())
	}

	// Accept negotiation: text/html with q=0 should NOT select HTML (should return JSON)
	reqQ0 := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/report", nil)
	reqQ0.Header.Set("Accept", "text/html;q=0, application/json")
	reqQ0.SetPathValue("id", "exp-123")
	recQ0 := httptest.NewRecorder()
	h.GetExperimentReport(recQ0, reqQ0)

	if recQ0.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recQ0.Code)
	}
	if ct := recQ0.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json for text/html;q=0, got %q", ct)
	}

	// Accept negotiation: text/html with higher quality than application/json selects HTML
	reqQHigh := httptest.NewRequest(http.MethodGet, "/api/experiments/exp-123/report", nil)
	reqQHigh.Header.Set("Accept", "text/html;q=0.9, application/json;q=0.8")
	reqQHigh.SetPathValue("id", "exp-123")
	recQHigh := httptest.NewRecorder()
	h.GetExperimentReport(recQHigh, reqQHigh)

	if recQHigh.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recQHigh.Code)
	}
	if ct := recQHigh.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html when qHTML > qJSON, got %q", ct)
	}

	// Missing report
	req = httptest.NewRequest(http.MethodGet, "/api/experiments/exp-404/report", nil)
	req.SetPathValue("id", "exp-404")
	rec = httptest.NewRecorder()
	h.GetExperimentReport(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_GetHealth(t *testing.T) {
	t.Run("default health without checker reports UNKNOWN for dependencies", func(t *testing.T) {
		h := NewHandler(nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rec := httptest.NewRecorder()
		h.GetHealth(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["jaeger"] != "UNKNOWN" || resp["prometheus"] != "UNKNOWN" {
			t.Errorf("expected UNKNOWN for unconfigured dependencies, got: %+v", resp)
		}
	})

	t.Run("checker health DOWN with error", func(t *testing.T) {
		checker := &mockObservabilityChecker{
			checkHealthFn: func(ctx context.Context) (map[string]string, error) {
				return map[string]string{"status": "UP", "prometheus": "UNREACHABLE"}, errors.New("connection failed: sensitive-internal-ip:9090")
			},
		}
		h := NewHandler(nil, nil, nil, WithObservabilityChecker(checker))
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		rec := httptest.NewRecorder()
		h.GetHealth(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["status"] != "DOWN" {
			t.Errorf("expected DOWN status, got %v", resp["status"])
		}
		// Verify raw error string is sanitized
		if strings.Contains(rec.Body.String(), "sensitive-internal-ip") {
			t.Errorf("sensitive error details leaked in response: %s", rec.Body.String())
		}
	})
}

func TestHandler_GetServices(t *testing.T) {
	t.Run("success returns discovered service list", func(t *testing.T) {
		g := graph.NewDependencyGraph()
		g.AddNode("cart-service", nil)
		g.AddNode("order-service", nil)
		analyzer := &mockGraphAnalyzer{
			buildGraphFn: func(ctx context.Context, lookback int64) (*graph.DependencyGraph, error) {
				return g, nil
			},
		}

		h := NewHandler(nil, nil, analyzer)
		req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
		rec := httptest.NewRecorder()
		h.GetServices(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp ServicesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Services) != 2 {
			t.Fatalf("expected 2 services, got %d: %v", len(resp.Services), resp.Services)
		}
	})

	t.Run("nil analyzer returns internal error", func(t *testing.T) {
		h := NewHandler(nil, nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
		rec := httptest.NewRecorder()
		h.GetServices(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
	})
}
