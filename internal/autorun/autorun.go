// Package autorun drives the unattended pipeline: generate tagged traffic from
// the developer's request specs, pull the resulting traces from Jaeger, find the
// bottleneck service, and run capacity experiments against it automatically.
package autorun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/stress"

	"github.com/google/uuid"
)

// LoadRunner generates traffic for discovery phases.
type LoadRunner interface {
	Start(ctx context.Context, plan stress.LoadPlan) error
}

// TraceSource fetches traces and the structural dependency graph from Jaeger.
type TraceSource interface {
	FetchTrace(ctx context.Context, traceID string) (*graph.TraceProfile, error)
	BuildGraph(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error)
	ScoreCriticality(g *graph.DependencyGraph) []graph.ServiceScore
}

// ExperimentRunner is the subset of experiment.Manager used to run tests.
type ExperimentRunner interface {
	CreateExperiment(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error)
	StartExperiment(ctx context.Context, id string, plan stress.LoadPlan) error
	GetStatus(ctx context.Context, id string) (*experiment.Experiment, error)
}

// Scope selects which requests get a capacity experiment.
type Scope string

const (
	ScopeBottleneck Scope = "bottleneck" // only requests whose traces reach the bottleneck
	ScopeAll        Scope = "all"        // every eligible request
)

// Config controls discovery traffic and the experiments that follow.
type Config struct {
	Requests        []stress.RequestSpec
	BaselineRate    int
	BaselineSeconds int
	ProbeRate       int
	ProbeSeconds    int
	TracesPerPhase  int
	TraceSettle     time.Duration
	Scope           Scope
	Experiment      experiment.ExperimentConfig
	StartDelay      time.Duration
	PollInterval    time.Duration
}

// DefaultConfig returns conservative settings suitable for a disposable test environment.
func DefaultConfig() Config {
	return Config{
		BaselineRate:    2,
		BaselineSeconds: 20,
		ProbeRate:       20,
		ProbeSeconds:    30,
		TracesPerPhase:  150,
		TraceSettle:     5 * time.Second,
		Scope:           ScopeBottleneck,
		PollInterval:    2 * time.Second,
		Experiment: experiment.ExperimentConfig{
			InitialRate:         10,
			StepRate:            10,
			MaxRate:             100,
			StepDurationSeconds: 40,
			MaxP95LatencyMs:     1000,
			MaxErrorRatePercent: 5,
			MinAvailabilityPct:  95,
			RecoveryWindowSec:   30,
		},
	}
}

// ConfigFromEnv overlays AUTO_RUN_* variables on DefaultConfig.
func ConfigFromEnv(getenv func(string) string, requests []stress.RequestSpec) (Config, error) {
	cfg := DefaultConfig()
	cfg.Requests = requests
	var errs []string
	intVar := func(name string, dst *int, minimum int) {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < minimum {
			errs = append(errs, fmt.Sprintf("%s must be an integer >= %d", name, minimum))
			return
		}
		*dst = v
	}
	floatVar := func(name string, dst *float64) {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v < 0 {
			errs = append(errs, fmt.Sprintf("%s must be a non-negative number", name))
			return
		}
		*dst = v
	}
	intVar("AUTO_RUN_BASELINE_RPS", &cfg.BaselineRate, 1)
	intVar("AUTO_RUN_BASELINE_SECONDS", &cfg.BaselineSeconds, 1)
	intVar("AUTO_RUN_PROBE_RPS", &cfg.ProbeRate, 1)
	intVar("AUTO_RUN_PROBE_SECONDS", &cfg.ProbeSeconds, 1)
	intVar("AUTO_RUN_TRACES_PER_PHASE", &cfg.TracesPerPhase, 1)
	intVar("AUTO_RUN_INITIAL_RPS", &cfg.Experiment.InitialRate, 1)
	intVar("AUTO_RUN_STEP_RPS", &cfg.Experiment.StepRate, 0)
	intVar("AUTO_RUN_MAX_RPS", &cfg.Experiment.MaxRate, 1)
	intVar("AUTO_RUN_STEP_SECONDS", &cfg.Experiment.StepDurationSeconds, 1)
	intVar("AUTO_RUN_RECOVERY_SECONDS", &cfg.Experiment.RecoveryWindowSec, 0)
	floatVar("AUTO_RUN_MAX_P95_MS", &cfg.Experiment.MaxP95LatencyMs)
	floatVar("AUTO_RUN_MAX_ERROR_PCT", &cfg.Experiment.MaxErrorRatePercent)
	floatVar("AUTO_RUN_MIN_AVAILABILITY_PCT", &cfg.Experiment.MinAvailabilityPct)
	var delay int
	intVar("AUTO_RUN_START_DELAY_SECONDS", &delay, 0)
	cfg.StartDelay = time.Duration(delay) * time.Second
	switch scope := Scope(strings.ToLower(strings.TrimSpace(getenv("AUTO_RUN_TEST_SCOPE")))); scope {
	case "":
	case ScopeBottleneck, ScopeAll:
		cfg.Scope = scope
	default:
		errs = append(errs, "AUTO_RUN_TEST_SCOPE must be 'bottleneck' or 'all'")
	}
	if err := cfg.Experiment.Validate(); err != nil {
		errs = append(errs, "AUTO_RUN experiment settings: "+err.Error())
	}
	if len(errs) > 0 {
		return cfg, errors.New(strings.Join(errs, "; "))
	}
	return cfg, nil
}

// RunState is the coarse progress of an auto-run.
type RunState string

const (
	RunPending     RunState = "PENDING"
	RunDiscovering RunState = "DISCOVERING"
	RunAnalyzing   RunState = "ANALYZING"
	RunTesting     RunState = "TESTING"
	RunCompleted   RunState = "COMPLETED"
	RunFailed      RunState = "FAILED"
	RunCancelled   RunState = "CANCELLED"
)

// RunExperiment tracks one automatically launched capacity experiment.
type RunExperiment struct {
	Request       string                     `json:"request"`
	Method        string                     `json:"method"`
	URL           string                     `json:"url"`
	TargetService string                     `json:"target_service"`
	ExperimentID  string                     `json:"experiment_id,omitempty"`
	State         experiment.ExperimentState `json:"state,omitempty"`
	StopReason    string                     `json:"stop_reason,omitempty"`
	Error         string                     `json:"error,omitempty"`
}

// Run is the observable status of one pipeline execution.
type Run struct {
	ID          string                  `json:"id"`
	State       RunState                `json:"state"`
	Phase       string                  `json:"phase,omitempty"`
	Error       string                  `json:"error,omitempty"`
	StartedAt   time.Time               `json:"started_at"`
	EndedAt     *time.Time              `json:"ended_at,omitempty"`
	Requests    []stress.RequestSpec    `json:"requests"`
	Skipped     []string                `json:"skipped,omitempty"`
	Report      *graph.BottleneckReport `json:"report,omitempty"`
	Experiments []RunExperiment         `json:"experiments"`
}

// ErrRunInProgress is returned when an auto-run is already executing.
var ErrRunInProgress = errors.New("an auto-run is already in progress")

// ErrNoRequests means no request specs were configured.
var ErrNoRequests = errors.New("no request specs configured; set DAMASCUS_ENDPOINTS or DAMASCUS_ENDPOINTS_FILE")

const maxRunHistory = 10

// Orchestrator executes auto-runs one at a time and keeps recent status in memory.
type Orchestrator struct {
	cfg         Config
	load        LoadRunner
	traces      TraceSource
	experiments ExperimentRunner
	logger      *slog.Logger

	mu      sync.Mutex
	runs    []*Run
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewOrchestrator(cfg Config, load LoadRunner, traces TraceSource, experiments ExperimentRunner, logger *slog.Logger) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	return &Orchestrator{cfg: cfg, load: load, traces: traces, experiments: experiments, logger: logger}
}

// Running reports whether an auto-run is in progress.
func (o *Orchestrator) Running() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

// Requests returns the configured request specs.
func (o *Orchestrator) Requests() []stress.RequestSpec { return o.cfg.Requests }

// Start launches a run in the background and returns its initial status.
func (o *Orchestrator) Start(parent context.Context, delay time.Duration) (Run, error) {
	if len(o.cfg.Requests) == 0 {
		return Run{}, ErrNoRequests
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.running {
		return Run{}, ErrRunInProgress
	}
	run := &Run{ID: uuid.NewString(), State: RunPending, StartedAt: time.Now().UTC(), Requests: o.cfg.Requests, Experiments: []RunExperiment{}}
	o.runs = append(o.runs, run)
	if len(o.runs) > maxRunHistory {
		o.runs = o.runs[len(o.runs)-maxRunHistory:]
	}
	ctx, cancel := context.WithCancel(parent)
	o.running, o.cancel = true, cancel
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer cancel()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
			}
		}
		err := o.execute(ctx, run)
		o.mu.Lock()
		defer o.mu.Unlock()
		now := time.Now().UTC()
		run.EndedAt = &now
		switch {
		case err == nil:
			run.State = RunCompleted
		case ctx.Err() != nil:
			run.State, run.Error = RunCancelled, ctx.Err().Error()
		default:
			run.State, run.Error = RunFailed, err.Error()
		}
		run.Phase = ""
		o.running, o.cancel = false, nil
		o.logger.Info("auto-run finished", "run_id", run.ID, "state", run.State, "error", run.Error)
	}()
	return o.snapshot(run), nil
}

// Shutdown cancels an in-progress run and waits for it to stop.
func (o *Orchestrator) Shutdown() {
	o.mu.Lock()
	cancel := o.cancel
	o.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	o.wg.Wait()
}

// Get returns a copy of the run with the given ID; "latest" selects the newest.
func (o *Orchestrator) Get(id string) (Run, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if id == "latest" && len(o.runs) > 0 {
		return o.snapshotLocked(o.runs[len(o.runs)-1]), true
	}
	for _, r := range o.runs {
		if r.ID == id {
			return o.snapshotLocked(r), true
		}
	}
	return Run{}, false
}

// List returns recent runs, newest first.
func (o *Orchestrator) List() []Run {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Run, 0, len(o.runs))
	for i := len(o.runs) - 1; i >= 0; i-- {
		out = append(out, o.snapshotLocked(o.runs[i]))
	}
	return out
}

func (o *Orchestrator) snapshot(r *Run) Run { return o.snapshotLocked(r) }

func (o *Orchestrator) snapshotLocked(r *Run) Run {
	c := *r
	c.Experiments = append([]RunExperiment(nil), r.Experiments...)
	c.Skipped = append([]string(nil), r.Skipped...)
	c.Requests = RedactedRequests(r.Requests)
	return c
}

// RedactedRequests hides header values (often API keys) before specs leave the process.
func RedactedRequests(specs []stress.RequestSpec) []stress.RequestSpec {
	out := make([]stress.RequestSpec, len(specs))
	for i, s := range specs {
		out[i] = s
		if len(s.Headers) > 0 {
			out[i].Headers = make(map[string]string, len(s.Headers))
			for k := range s.Headers {
				out[i].Headers[k] = "[redacted]"
			}
		}
	}
	return out
}

func (o *Orchestrator) update(run *Run, fn func(*Run)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fn(run)
}

func (o *Orchestrator) execute(ctx context.Context, run *Run) error {
	var eligible []stress.RequestSpec
	var skipped []string
	for _, spec := range o.cfg.Requests {
		if spec.IsMutating() && !spec.AllowMutation {
			skipped = append(skipped, fmt.Sprintf("%s: %s requests need \"allow_mutation\": true to run unattended", spec.Name, spec.Method))
			continue
		}
		eligible = append(eligible, spec)
	}
	o.update(run, func(r *Run) { r.Skipped = skipped; r.State = RunDiscovering })
	if len(eligible) == 0 {
		return errors.New("every configured request is mutating and none set allow_mutation")
	}

	// 1. Generate tagged traffic at a baseline rate and at a probe rate.
	var samples []graph.TraceSample
	for _, phase := range []struct {
		name          string
		rate, seconds int
	}{
		{"baseline", o.cfg.BaselineRate, o.cfg.BaselineSeconds},
		{"load", o.cfg.ProbeRate, o.cfg.ProbeSeconds},
	} {
		o.update(run, func(r *Run) {
			r.Phase = fmt.Sprintf("%s traffic at %d RPS for %ds", phase.name, phase.rate, phase.seconds)
		})
		records, err := o.generate(ctx, eligible, phase.rate, phase.seconds)
		if err != nil {
			return fmt.Errorf("%s traffic: %w", phase.name, err)
		}
		for _, rec := range records {
			samples = append(samples, graph.TraceSample{Phase: phase.name, Rate: rec.Rate, Request: rec.Request, Trace: &graph.TraceProfile{TraceID: rec.TraceID}})
		}
	}

	// 2. Pull those exact traces back from Jaeger.
	o.update(run, func(r *Run) { r.State, r.Phase = RunAnalyzing, "fetching generated traces from Jaeger" })
	select {
	case <-time.After(o.cfg.TraceSettle):
	case <-ctx.Done():
		return ctx.Err()
	}
	samples, err := o.fetchTraces(ctx, samples)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return errors.New("none of the generated traces reached Jaeger; check that the service propagates the W3C traceparent header and exports spans to this Jaeger")
	}
	var structural []graph.ServiceScore
	if g, err := o.traces.BuildGraph(ctx, 900); err == nil {
		structural = o.traces.ScoreCriticality(g)
	}
	report := graph.AnalyzeBottlenecks(samples, structural)
	o.update(run, func(r *Run) { r.Report = &report })
	o.logger.Info("auto-run bottleneck identified", "run_id", run.ID, "bottleneck", report.Bottleneck,
		"baseline_traces", report.BaselineTraces, "load_traces", report.LoadTraces)

	// 3. Run capacity experiments for the requests that exercise the bottleneck.
	o.update(run, func(r *Run) { r.State = RunTesting })
	targets := selectTargets(eligible, report, o.cfg.Scope)
	if len(targets) == 0 {
		return fmt.Errorf("no configured request reaches bottleneck %q", report.Bottleneck)
	}
	var failures []string
	for i, spec := range targets {
		target := report.Bottleneck
		if o.cfg.Scope == ScopeAll && !reaches(report, spec.Name, report.Bottleneck) {
			target = report.EntryServices[spec.Name]
		}
		row := RunExperiment{Request: spec.Name, Method: spec.Method, URL: spec.SampleURL(), TargetService: target}
		o.update(run, func(r *Run) {
			r.Phase = fmt.Sprintf("experiment %d/%d: %s against %s", i+1, len(targets), spec.Name, target)
			r.Experiments = append(r.Experiments, row)
		})
		idx := len(run.Experiments) - 1
		exp, err := o.runExperiment(ctx, spec, target, func(e *experiment.Experiment) {
			o.update(run, func(r *Run) {
				r.Experiments[idx].ExperimentID = e.ID
				r.Experiments[idx].State = e.State
				r.Experiments[idx].StopReason = e.StopReason
			})
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", spec.Name, err))
			o.update(run, func(r *Run) { r.Experiments[idx].Error = err.Error() })
			continue
		}
		if exp.State == experiment.StateAborted {
			failures = append(failures, fmt.Sprintf("%s: aborted: %s", spec.Name, exp.StopReason))
		}
	}
	if len(failures) == len(targets) {
		return fmt.Errorf("all experiments failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func reaches(report graph.BottleneckReport, request, service string) bool {
	for _, s := range report.Reachability[request] {
		if s == service {
			return true
		}
	}
	return false
}

func selectTargets(specs []stress.RequestSpec, report graph.BottleneckReport, scope Scope) []stress.RequestSpec {
	var out []stress.RequestSpec
	for _, spec := range specs {
		if _, seen := report.Reachability[spec.Name]; !seen {
			continue // no trace came back for this request, so nothing can be measured
		}
		if scope == ScopeAll || reaches(report, spec.Name, report.Bottleneck) {
			out = append(out, spec)
		}
	}
	return out
}

func (o *Orchestrator) generate(ctx context.Context, specs []stress.RequestSpec, rate, seconds int) ([]stress.TraceRecord, error) {
	limit := o.cfg.TracesPerPhase
	expected := rate * seconds
	every := max(1, expected/max(limit, 1))
	var mu sync.Mutex
	var records []stress.TraceRecord
	plan := stress.LoadPlan{
		TargetURL:           specs[0].SampleURL(),
		Method:              specs[0].Method,
		Requests:            specs,
		InitialRate:         rate,
		MaxRate:             rate,
		StepDurationSeconds: seconds,
		TraceEvery:          every,
		OnTrace: func(rec stress.TraceRecord) {
			mu.Lock()
			defer mu.Unlock()
			if len(records) < limit {
				records = append(records, rec)
			}
		},
	}
	err := o.load.Start(ctx, plan)
	if errors.Is(err, stress.ErrInFlightLimit) {
		// The generator saturated; the traces already collected are still useful.
		o.logger.Warn("discovery traffic saturated the load generator", "rate", rate, "error", err)
		err = nil
	}
	mu.Lock()
	defer mu.Unlock()
	return records, err
}

func (o *Orchestrator) fetchTraces(ctx context.Context, samples []graph.TraceSample) ([]graph.TraceSample, error) {
	const workers, attempts = 8, 4
	out := make([]graph.TraceSample, len(samples))
	found := make([]bool, len(samples))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				for attempt := range attempts {
					trace, err := o.traces.FetchTrace(ctx, samples[i].Trace.TraceID)
					if err == nil {
						out[i] = samples[i]
						out[i].Trace = trace
						found[i] = true
						break
					}
					if !errors.Is(err, graph.ErrTraceNotFound) || attempt == attempts-1 {
						break
					}
					select {
					case <-time.After(time.Duration(attempt+1) * time.Second):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	for i := range samples {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	kept := out[:0]
	for i, ok := range found {
		if ok {
			kept = append(kept, out[i])
		}
	}
	return kept, nil
}

func (o *Orchestrator) runExperiment(ctx context.Context, spec stress.RequestSpec, target string, onUpdate func(*experiment.Experiment)) (*experiment.Experiment, error) {
	cfg := o.cfg.Experiment
	expType := experiment.ExperimentStep
	if cfg.StepRate == 0 || cfg.MaxRate == cfg.InitialRate {
		expType = experiment.ExperimentFixed
	}
	exp, err := o.experiments.CreateExperiment(ctx, target, spec.SampleURL(), expType, cfg)
	if err != nil {
		return nil, fmt.Errorf("create experiment: %w", err)
	}
	onUpdate(exp)
	plan := stress.LoadPlan{
		TargetURL:           spec.SampleURL(),
		Method:              spec.Method,
		Requests:            []stress.RequestSpec{spec},
		InitialRate:         cfg.InitialRate,
		StepRate:            cfg.StepRate,
		MaxRate:             cfg.MaxRate,
		StepDurationSeconds: cfg.StepDurationSeconds,
	}
	if expType == experiment.ExperimentFixed {
		plan.StepRate, plan.MaxRate = 0, cfg.InitialRate
	}
	if err := o.experiments.StartExperiment(ctx, exp.ID, plan); err != nil {
		if latest, getErr := o.experiments.GetStatus(context.Background(), exp.ID); getErr == nil && latest != nil {
			onUpdate(latest)
		}
		return nil, fmt.Errorf("start experiment: %w", err)
	}
	ticker := time.NewTicker(o.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
		latest, err := o.experiments.GetStatus(ctx, exp.ID)
		if err != nil || latest == nil {
			continue
		}
		onUpdate(latest)
		if latest.IsTerminal() {
			return latest, nil
		}
	}
}
