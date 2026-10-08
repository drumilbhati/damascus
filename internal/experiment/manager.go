package experiment

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/graph"
	"damascus/internal/safety"
	"damascus/internal/stress"
	"damascus/internal/watcher"

	"github.com/google/uuid"
)

type GraphAnalyzer interface {
	BuildGraph(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error)
	ScoreCriticality(g *graph.DependencyGraph) []graph.ServiceScore
}

type StressEngine interface {
	Start(ctx context.Context, plan stress.LoadPlan) error
	Stop()
}

type Watcher interface {
	Start(ctx context.Context, experimentID string, targetService string) error
	Stop()
	SetSnapshotHandler(handler watcher.SnapshotHandler)
}

type SafetyController interface {
	Evaluate(ctx context.Context, snapshot watcher.MetricSnapshot, policy safety.SafetyPolicy) safety.SafetyDecision
}

type CapacityAnalyzer interface {
	Analyze(observations []capacity.Observation, policy safety.SafetyPolicy) capacity.CapacityResult
}

type ReportEngine interface {
	Generate(exp Experiment, capResult capacity.CapacityResult, scores []graph.ServiceScore, observations []capacity.Observation) (*capacity.ExperimentReport, error)
}

type ExperimentRepository interface {
	Create(ctx context.Context, exp *Experiment) error
	GetByID(ctx context.Context, id string) (*Experiment, error)
	List(ctx context.Context) ([]Experiment, error)
	UpdateState(ctx context.Context, id string, state ExperimentState, stopReason string) error
	SaveObservation(ctx context.Context, expID string, obs capacity.Observation) error
	GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error)
	SaveReport(ctx context.Context, report *capacity.ExperimentReport) error
	GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error)
}

type Manager struct {
	graphAnalyzer    GraphAnalyzer
	stressEngine     StressEngine
	watcher          Watcher
	safetyController SafetyController
	capacityAnalyzer CapacityAnalyzer
	reportEngine     ReportEngine
	repo             ExperimentRepository

	mu         sync.RWMutex
	activeRuns map[string]context.CancelFunc
}

func NewManager(graphAnalyzer GraphAnalyzer, stressEngine StressEngine, watcher Watcher, safetyController SafetyController, capacityAnalyzer CapacityAnalyzer, reportEngine ReportEngine, repo ExperimentRepository) *Manager {
	return &Manager{
		graphAnalyzer:    graphAnalyzer,
		stressEngine:     stressEngine,
		watcher:          watcher,
		safetyController: safetyController,
		capacityAnalyzer: capacityAnalyzer,
		reportEngine:     reportEngine,
		repo:             repo,
		activeRuns:       make(map[string]context.CancelFunc),
	}
}

func (m *Manager) CreateExperiment(ctx context.Context, targetService, targetURL string, expType ExperimentType, config ExperimentConfig) (*Experiment, error) {
	if targetService == "" || targetURL == "" {
		return nil, fmt.Errorf("targetService and targetURL must be provided")
	}
	if expType != ExperimentRamp && expType != ExperimentStep {
		return nil, fmt.Errorf("unsupported experiment type %q", expType)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid experiment config: %w", err)
	}

	exp := Experiment{
		ID:            uuid.New().String(),
		TargetService: targetService,
		TargetURL:     targetURL,
		Type:          expType,
		State:         StateCreated,
		Config:        config,
		CreatedAt:     time.Now().UTC(),
	}
	if err := m.repo.Create(ctx, &exp); err != nil {
		return nil, fmt.Errorf("failed to create experiment: %w", err)
	}
	return &exp, nil
}

// StartExperiment accepts the traffic plan from the caller. The experiment configuration
// continues to hold safety thresholds and recovery settings.
func (m *Manager) StartExperiment(ctx context.Context, id string, plan stress.LoadPlan) error {
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("invalid load plan: %w", err)
	}
	parsedURL, err := url.ParseRequestURI(plan.TargetURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return fmt.Errorf("invalid load plan target_url %q", plan.TargetURL)
	}
	method := strings.ToUpper(plan.Method)
	if method != "GET" && method != "POST" {
		return fmt.Errorf("invalid load plan method %q: only GET and POST are supported", plan.Method)
	}
	plan.Method = method
	exp, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to retrieve experiment: %w", err)
	}
	if exp == nil {
		return fmt.Errorf("experiment %s was not found", id)
	}
	if exp.State != StateCreated {
		return fmt.Errorf("experiment %s is not in StateCreated; current state: %s", id, exp.State)
	}
	if err := m.repo.UpdateState(ctx, id, StatePlanned, ""); err != nil {
		return fmt.Errorf("failed to transition experiment %s to StatePlanned: %w", id, err)
	}

	var scores []graph.ServiceScore
	if m.graphAnalyzer != nil {
		dependencyGraph, err := m.graphAnalyzer.BuildGraph(ctx, 3600)
		if err != nil {
			_ = m.repo.UpdateState(ctx, id, StateAborted, err.Error())
			return fmt.Errorf("failed to build dependency graph: %w", err)
		}
		scores = m.graphAnalyzer.ScoreCriticality(dependencyGraph)
	}

	runCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	if _, exists := m.activeRuns[id]; exists {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("experiment %s is already running", id)
	}
	m.activeRuns[id] = cancel
	m.mu.Unlock()

	if err := m.repo.UpdateState(ctx, id, StateRunning, ""); err != nil {
		cancel()
		m.removeActiveRun(id)
		return fmt.Errorf("failed to transition experiment %s to StateRunning: %w", id, err)
	}

	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: exp.Config.MaxP95LatencyMs,
		MaxErrorRate:    exp.Config.MaxErrorRatePercent / 100,
		MinAvailability: exp.Config.MinAvailabilityPct / 100,
	}
	startErrs := make(chan error, 1)
	var failureOnce sync.Once
	signalFailure := func(err error) {
		failureOnce.Do(func() {
			startErrs <- err
			cancel()
		})
	}
	var stopOnce sync.Once
	handler := func(snapshot watcher.MetricSnapshot) {
		observation := capacity.Observation{
			Timestamp:    snapshot.Timestamp,
			LoadRate:     snapshot.RequestRate,
			P95LatencyMs: snapshot.P95LatencyMs,
			ErrorRate:    snapshot.ErrorRate,
			Availability: snapshot.Availability,
		}
		if err := m.repo.SaveObservation(runCtx, id, observation); err != nil {
			signalFailure(fmt.Errorf("failed to save observation: %w", err))
			return
		}
		if m.safetyController == nil {
			signalFailure(errors.New("safety controller is nil"))
			return
		}
		decision := m.safetyController.Evaluate(runCtx, snapshot, policy)
		if decision.ShouldStop {
			stopOnce.Do(cancel)
		}
	}
	if m.watcher == nil {
		cancel()
		m.removeActiveRun(id)
		_ = m.repo.UpdateState(ctx, id, StateAborted, "watcher is nil")
		return errors.New("watcher is nil")
	}
	m.watcher.SetSnapshotHandler(handler)
	if m.watcher != nil {
		go func() {
			if err := m.watcher.Start(runCtx, id, exp.TargetService); err != nil {
				signalFailure(fmt.Errorf("watcher failed: %w", err))
			}
		}()
	}
	if m.stressEngine == nil {
		cancel()
		m.removeActiveRun(id)
		_ = m.repo.UpdateState(ctx, id, StateAborted, "stress engine is nil")
		return errors.New("stress engine is nil")
	}
	go func() {
		if err := m.stressEngine.Start(runCtx, plan); err != nil && !errors.Is(err, context.Canceled) {
			signalFailure(fmt.Errorf("stress engine failed: %w", err))
		}
	}()

	go m.finishExperiment(runCtx, cancel, startErrs, *exp, scores)
	return nil
}

func (m *Manager) StopExperiment(ctx context.Context, id, reason string) error {
	m.mu.Lock()
	cancel, ok := m.activeRuns[id]
	if ok {
		delete(m.activeRuns, id)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("experiment %s is not actively running", id)
	}
	cancel()
	if err := m.repo.UpdateState(ctx, id, StateStopping, reason); err != nil {
		return fmt.Errorf("failed to stop experiment %s: %w", id, err)
	}
	return nil
}

func (m *Manager) GetStatus(ctx context.Context, id string) (*Experiment, error) {
	exp, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve experiment %s: %w", id, err)
	}
	if exp == nil {
		return nil, fmt.Errorf("experiment %s was not found", id)
	}
	return exp, nil
}

func (m *Manager) removeActiveRun(id string) {
	m.mu.Lock()
	delete(m.activeRuns, id)
	m.mu.Unlock()
}

func (m *Manager) finishExperiment(runCtx context.Context, cancel context.CancelFunc, startErrs <-chan error, exp Experiment, scores []graph.ServiceScore) {
	var stopReason string
	select {
	case err := <-startErrs:
		stopReason = err.Error()
		cancel()
	default:
		select {
		case err := <-startErrs:
			stopReason = err.Error()
			cancel()
		case <-runCtx.Done():
			// The run was stopped normally, manually, or by the safety controller.
		}
	}

	if m.watcher != nil {
		m.watcher.Stop()
	}
	if m.stressEngine != nil {
		m.stressEngine.Stop()
	}
	m.removeActiveRun(exp.ID)
	workCtx := context.Background()

	if stopReason != "" {
		_ = m.repo.UpdateState(workCtx, exp.ID, StateAborted, stopReason)
		return
	}
	current, err := m.repo.GetByID(workCtx, exp.ID)
	if err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	if current == nil || current.State != StateStopping {
		if err := m.repo.UpdateState(workCtx, exp.ID, StateStopping, ""); err != nil {
			m.abortExperiment(workCtx, exp.ID, err.Error())
			return
		}
	}
	if exp.Config.RecoveryWindowSec > 0 {
		timer := time.NewTimer(time.Duration(exp.Config.RecoveryWindowSec) * time.Second)
		<-timer.C
	}
	if err := m.repo.UpdateState(workCtx, exp.ID, StateAnalyzing, ""); err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	observations, err := m.repo.GetObservations(workCtx, exp.ID)
	if err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	if m.capacityAnalyzer == nil {
		m.abortExperiment(workCtx, exp.ID, "capacity analyzer is nil")
		return
	}
	policy := safety.SafetyPolicy{
		MaxP95LatencyMs: exp.Config.MaxP95LatencyMs,
		MaxErrorRate:    exp.Config.MaxErrorRatePercent / 100,
		MinAvailability: exp.Config.MinAvailabilityPct / 100,
	}
	result := m.capacityAnalyzer.Analyze(observations, policy)
	if m.reportEngine == nil {
		m.abortExperiment(workCtx, exp.ID, "report engine is nil")
		return
	}
	if err := m.repo.UpdateState(workCtx, exp.ID, StateReporting, ""); err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	report, err := m.reportEngine.Generate(exp, result, scores, observations)
	if err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	if err := m.repo.SaveReport(workCtx, report); err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	if err := m.repo.UpdateState(workCtx, exp.ID, StateCompleted, ""); err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
	}
}

func (m *Manager) abortExperiment(ctx context.Context, id, reason string) {
	_ = m.repo.UpdateState(ctx, id, StateAborted, reason)
}
