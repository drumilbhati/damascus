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

	mu             sync.RWMutex
	stateMu        sync.Mutex
	activeRuns     map[string]context.CancelFunc
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

func NewManager(graphAnalyzer GraphAnalyzer, stressEngine StressEngine, watcher Watcher, safetyController SafetyController, capacityAnalyzer CapacityAnalyzer, reportEngine ReportEngine, repo ExperimentRepository) *Manager {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	return &Manager{
		graphAnalyzer:    graphAnalyzer,
		stressEngine:     stressEngine,
		watcher:          watcher,
		safetyController: safetyController,
		capacityAnalyzer: capacityAnalyzer,
		reportEngine:     reportEngine,
		repo:             repo,
		activeRuns:       make(map[string]context.CancelFunc),
		shutdownCtx:      shutdownCtx,
		shutdownCancel:   shutdownCancel,
	}
}

// Shutdown stops all active experiments and interrupts recovery waits.
func (m *Manager) Shutdown() {
	m.shutdownCancel()
	m.mu.RLock()
	cancels := make([]context.CancelFunc, 0, len(m.activeRuns))
	for _, cancel := range m.activeRuns {
		cancels = append(cancels, cancel)
	}
	m.mu.RUnlock()
	for _, cancel := range cancels {
		cancel()
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
	if err := m.transitionState(ctx, id, StatePlanned, ""); err != nil {
		return fmt.Errorf("failed to transition experiment %s to StatePlanned: %w", id, err)
	}

	var scores []graph.ServiceScore
	if m.graphAnalyzer != nil {
		dependencyGraph, err := m.graphAnalyzer.BuildGraph(ctx, 3600)
		if err != nil {
			_ = m.transitionState(ctx, id, StateAborted, err.Error())
			return fmt.Errorf("failed to build dependency graph: %w", err)
		}
		scores = m.graphAnalyzer.ScoreCriticality(dependencyGraph)
	}
	if m.stressEngine == nil {
		_ = m.transitionState(ctx, id, StateAborted, "stress engine is nil")
		return errors.New("stress engine is nil")
	}
	if m.watcher == nil {
		_ = m.transitionState(ctx, id, StateAborted, "watcher is nil")
		return errors.New("watcher is nil")
	}

	expCtx, cancelExp := context.WithCancel(ctx)
	stressCtx, cancelStress := context.WithCancel(expCtx)

	m.mu.Lock()
	if _, exists := m.activeRuns[id]; exists {
		m.mu.Unlock()
		cancelStress()
		cancelExp()
		return fmt.Errorf("experiment %s is already running", id)
	}
	m.activeRuns[id] = cancelStress
	m.mu.Unlock()

	if err := m.transitionState(ctx, id, StateRunning, ""); err != nil {
		cancelStress()
		cancelExp()
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
			cancelStress()
			cancelExp()
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
		if err := m.repo.SaveObservation(expCtx, id, observation); err != nil {
			signalFailure(fmt.Errorf("failed to save observation: %w", err))
			return
		}
		if stressCtx.Err() != nil {
			// Stress traffic already stopped; no need to evaluate safety stop during recovery
			return
		}
		if m.safetyController == nil {
			signalFailure(errors.New("safety controller is nil"))
			return
		}
		decision := m.safetyController.Evaluate(expCtx, snapshot, policy)
		if decision.ShouldStop {
			stopOnce.Do(cancelStress)
		}
	}
	m.watcher.SetSnapshotHandler(handler)
	go func() {
		if err := m.watcher.Start(expCtx, id, exp.TargetService); err != nil {
			signalFailure(fmt.Errorf("watcher failed: %w", err))
		}
	}()

	stressDone := make(chan struct{})
	go func() {
		defer close(stressDone)
		if err := m.stressEngine.Start(stressCtx, plan); err != nil {
			if !errors.Is(err, context.Canceled) {
				signalFailure(fmt.Errorf("stress engine failed: %w", err))
			}
			return
		}
		cancelStress()
	}()

	go m.finishExperiment(expCtx, cancelExp, stressDone, startErrs, *exp, scores)
	return nil
}

func (m *Manager) StopExperiment(ctx context.Context, id, reason string) error {
	m.mu.Lock()
	cancel, ok := m.activeRuns[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("experiment %s is not actively running", id)
	}
	cancel()
	if err := m.transitionState(ctx, id, StateStopping, reason); err != nil {
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

// transitionState safely updates the experiment state under mutex synchronization,
// ensuring that transitions strictly adhere to the state machine matrix.
func (m *Manager) transitionState(ctx context.Context, id string, target ExperimentState, reason string) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	exp, err := m.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to retrieve experiment %s: %w", id, err)
	}
	if exp == nil {
		return fmt.Errorf("experiment %s was not found", id)
	}

	if err := ValidateTransition(exp.State, target); err != nil {
		return err
	}

	if err := m.repo.UpdateState(ctx, id, target, reason); err != nil {
		return fmt.Errorf("failed to update state for experiment %s: %w", id, err)
	}

	return nil
}

func (m *Manager) finishExperiment(
	expCtx context.Context,
	cancelExp context.CancelFunc,
	stressDone <-chan struct{},
	startErrs <-chan error,
	exp Experiment,
	scores []graph.ServiceScore,
) {
	defer cancelExp()
	defer m.removeActiveRun(exp.ID)

	var stopReason string
	select {
	case err := <-startErrs:
		stopReason = err.Error()
	default:
		select {
		case err := <-startErrs:
			stopReason = err.Error()
		case <-stressDone:
			// Traffic generation stopped normally or via cancellation.
		case <-expCtx.Done():
			// Context was cancelled.
		}
	}

	workCtx := context.Background()

	// If an engine start failure occurred, abort immediately.
	if stopReason != "" {
		if m.watcher != nil {
			m.watcher.Stop()
		}
		if m.stressEngine != nil {
			m.stressEngine.Stop()
		}
		m.abortExperiment(workCtx, exp.ID, stopReason)
		return
	}

	if m.stressEngine != nil {
		m.stressEngine.Stop()
	}

	curr, err := m.repo.GetByID(workCtx, exp.ID)
	if err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}
	if curr == nil || curr.State != StateStopping {
		if err := m.transitionState(workCtx, exp.ID, StateStopping, ""); err != nil {
			m.abortExperiment(workCtx, exp.ID, err.Error())
			return
		}
	}

	if exp.Config.RecoveryWindowSec > 0 {
		timer := time.NewTimer(time.Duration(exp.Config.RecoveryWindowSec) * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			// Recovery observation window elapsed.
		case <-m.shutdownCtx.Done():
			m.abortExperiment(workCtx, exp.ID, "manager shutdown")
			if m.watcher != nil {
				m.watcher.Stop()
			}
			return
		}
	}

	if m.watcher != nil {
		m.watcher.Stop()
	}
	if err := m.transitionState(workCtx, exp.ID, StateAnalyzing, ""); err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
		return
	}

	// Remaining pipeline: Analysis, Reporting, Completion
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
	if err := m.transitionState(workCtx, exp.ID, StateReporting, ""); err != nil {
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
	if err := m.transitionState(workCtx, exp.ID, StateCompleted, ""); err != nil {
		m.abortExperiment(workCtx, exp.ID, err.Error())
	}
}

func (m *Manager) abortExperiment(ctx context.Context, id, reason string) {
	_ = m.transitionState(ctx, id, StateAborted, reason)
}
