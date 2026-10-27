package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"damascus/internal/experiment"
)

func TestRouter_Integration(t *testing.T) {
	exp := &experiment.Experiment{
		ID:            "exp-route-1",
		TargetService: "checkout",
		TargetURL:     "http://checkout:8080",
		Type:          experiment.ExperimentRamp,
		State:         experiment.StateCreated,
		Config: experiment.ExperimentConfig{
			InitialRate:         10,
			StepRate:            5,
			MaxRate:             30,
			StepDurationSeconds: 2,
			MaxP95LatencyMs:     50,
			MaxErrorRatePercent: 1.0,
			MinAvailabilityPct:  99.0,
		},
		CreatedAt: time.Now(),
	}

	repo := &mockExperimentRepository{
		getByIDFn: func(ctx context.Context, id string) (*experiment.Experiment, error) {
			if id == "exp-route-1" {
				return exp, nil
			}
			return nil, nil
		},
		listFn: func(ctx context.Context) ([]experiment.Experiment, error) {
			return []experiment.Experiment{*exp}, nil
		},
	}

	svc := &mockExperimentService{
		createFn: func(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error) {
			return exp, nil
		},
	}

	h := NewHandler(svc, repo, &mockGraphAnalyzer{})
	router := NewRouter(h)

	ts := httptest.NewServer(router)
	defer ts.Close()

	client := ts.Client()

	t.Run("GET /api/experiments", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/experiments")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
	})

	t.Run("GET /api/experiments/{id}", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/experiments/exp-route-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var fetched experiment.Experiment
		if err := json.NewDecoder(resp.Body).Decode(&fetched); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if fetched.ID != "exp-route-1" {
			t.Errorf("expected exp-route-1, got %s", fetched.ID)
		}
	})

	t.Run("POST /api/experiments", func(t *testing.T) {
		dto := CreateExperimentDTO{
			TargetService: "checkout",
			TargetURL:     "http://checkout:8080",
			Type:          experiment.ExperimentRamp,
			Config:        exp.Config,
		}
		body, _ := json.Marshal(dto)
		resp, err := client.Post(ts.URL+"/api/experiments", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("GET /api/health", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/health")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("404 on unknown endpoint", func(t *testing.T) {
		resp, err := client.Get(ts.URL + "/api/unknown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestRouter_PanicRecovery(t *testing.T) {
	// Custom handler that deliberately panics
	panickyHandler := &Handler{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) {
		panic("simulated critical crash")
	})

	recovered := recoveryMiddleware(nil, mux)
	ts := httptest.NewServer(recovered)
	defer ts.Close()

	_ = panickyHandler
	resp, err := ts.Client().Get(ts.URL + "/panic")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 on panic recovery, got %d", resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error != "internal server error" {
		t.Errorf("unexpected error text: %s", errResp.Error)
	}
}
