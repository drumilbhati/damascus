package experiment_test

import (
	"context"
	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func regressionConfig() experiment.ExperimentConfig {
	return experiment.ExperimentConfig{InitialRate: 10, StepRate: 10, MaxRate: 20, StepDurationSeconds: 1, MaxP95LatencyMs: 100, MaxErrorRatePercent: 0, MinAvailabilityPct: 0, RecoveryWindowSec: 0}
}
func regressionPlan() stress.LoadPlan {
	return stress.LoadPlan{TargetURL: "http://target.invalid", Method: "GET", InitialRate: 10, StepRate: 10, MaxRate: 20, StepDurationSeconds: 1}
}
func waitState(t *testing.T, m *experiment.Manager, id string, want experiment.ExperimentState) *experiment.Experiment {
	t.Helper()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		e, err := m.GetStatus(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if e.State == want {
			return e
		}
		select {
		case <-deadline:
			t.Fatalf("wanted %s, got %s: %s", want, e.State, e.StopReason)
		case <-ticker.C:
		}
	}
}
func createRegression(t *testing.T, m *experiment.Manager) *experiment.Experiment {
	t.Helper()
	e, err := m.CreateExperiment(context.Background(), "catalog", "http://target.invalid", experiment.ExperimentRamp, regressionConfig())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRegressionSingleActiveExperimentAndSafetyReason(t *testing.T) {
	repo := newMockRepo()
	engine := newMockStressEngine()
	w := newMockWatcher()
	m := experiment.NewManager(nil, engine, w, safety.NewController(nil), &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)
	defer m.Shutdown()
	first := createRegression(t, m)
	second := createRegression(t, m)
	if err := m.StartExperiment(context.Background(), first.ID, regressionPlan()); err != nil {
		t.Fatal(err)
	}
	if err := m.StartExperiment(context.Background(), second.ID, regressionPlan()); err == nil {
		t.Fatal("second concurrent experiment accepted")
	}
	other, _ := m.GetStatus(context.Background(), second.ID)
	if other.State != experiment.StateCreated {
		t.Fatalf("rejected experiment mutated: %s", other.State)
	}
	w.EmitSnapshot(watcher.MetricSnapshot{Timestamp: time.Now(), RequestRate: 900, P95LatencyMs: 2, ErrorRate: 0.01, Availability: 0.99})
	done := waitState(t, m, first.ID, experiment.StateCompleted)
	if !strings.Contains(done.StopReason, "Error rate breached") {
		t.Fatalf("zero-tolerance safety reason lost: %q", done.StopReason)
	}
	obs, _ := repo.GetObservations(context.Background(), first.ID)
	if len(obs) != 1 || obs[0].InjectedRate == nil || *obs[0].InjectedRate != 10 || obs[0].LoadRate != 900 {
		t.Fatalf("injected and observed load confused: %+v", obs)
	}
}

type failedTransport func(*http.Request) (*http.Response, error)

func (f failedTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRegressionHTTPFailureAbortsRatherThanCompletes(t *testing.T) {
	repo := newMockRepo()
	w := newMockWatcher()
	engine := stress.NewEngine(&http.Client{Transport: failedTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})})
	m := experiment.NewManager(nil, engine, w, &mockSafetyController{}, &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)
	defer m.Shutdown()
	exp := createRegression(t, m)
	if err := m.StartExperiment(context.Background(), exp.ID, regressionPlan()); err != nil {
		t.Fatal(err)
	}
	done := waitState(t, m, exp.ID, experiment.StateAborted)
	if !strings.Contains(done.StopReason, "503") {
		t.Fatalf("HTTP failure not persisted: %s", done.StopReason)
	}
	if r, _ := repo.GetReport(context.Background(), exp.ID); r != nil {
		t.Fatal("failed injection produced a successful capacity report")
	}
}

func TestRegressionTelemetryFailureDoesNotBecomeMeasurement(t *testing.T) {
	repo := newMockRepo()
	w := newMockWatcher()
	m := experiment.NewManager(nil, newMockStressEngine(), w, &mockSafetyController{}, &mockCapacityAnalyzer{}, &mockReportEngine{}, repo)
	defer m.Shutdown()
	exp := createRegression(t, m)
	if err := m.StartExperiment(context.Background(), exp.ID, regressionPlan()); err != nil {
		t.Fatal(err)
	}
	w.EmitSnapshot(watcher.BuildSyntheticBreachSnapshot(exp.ID, "catalog", 3, errors.New("missing series")))
	done := waitState(t, m, exp.ID, experiment.StateAborted)
	if !strings.Contains(done.StopReason, "loss-of-observability") {
		t.Fatalf("missing cause: %q", done.StopReason)
	}
	if obs, _ := repo.GetObservations(context.Background(), exp.ID); len(obs) != 0 {
		t.Fatal("synthetic failure persisted as real measurement")
	}
}

func TestRegressionRecoveryWithBackgroundTraffic(t *testing.T) {
	repo := newMockRepo()
	engine := newMockStressEngine()
	w := newMockWatcher()
	m := experiment.NewManager(nil, engine, w, &mockSafetyController{}, capacity.NewAnalyzer(), &mockReportEngine{}, repo)
	defer m.Shutdown()
	cfg := regressionConfig()
	cfg.RecoveryWindowSec = 1
	exp, err := m.CreateExperiment(context.Background(), "catalog", "http://target.invalid", experiment.ExperimentRamp, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StartExperiment(context.Background(), exp.ID, regressionPlan()); err != nil {
		t.Fatal(err)
	}
	w.EmitSnapshot(watcher.MetricSnapshot{Timestamp: time.Now(), RequestRate: 100, P95LatencyMs: 50, Availability: 1})
	engine.Unblock()
	waitState(t, m, exp.ID, experiment.StateStopping)
	w.EmitSnapshot(watcher.MetricSnapshot{Timestamp: time.Now(), RequestRate: 90, P95LatencyMs: 10, Availability: 1})
	waitState(t, m, exp.ID, experiment.StateCompleted)
	obs, _ := repo.GetObservations(context.Background(), exp.ID)
	if len(obs) != 2 || *obs[1].InjectedRate != 0 || obs[1].LoadRate != 90 {
		t.Fatalf("recovery mislabeled: %+v", obs)
	}
	result := capacity.NewAnalyzer().Analyze(obs, safety.SafetyPolicy{MaxP95LatencyMs: 100, MaxErrorRate: 0})
	if result.MaximumTestedRate != 10 {
		t.Fatalf("background traffic inflated capacity: %+v", result)
	}
}
