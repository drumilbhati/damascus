package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	reportpkg "damascus/internal/report"
	"damascus/internal/storage"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

// maxRequestBodyBytes limits request body sizes to prevent memory exhaustion (DoS).
const maxRequestBodyBytes = 1 << 20 // 1 MB

// Common sentinel errors for API error classification.
var (
	ErrNotFound   = errors.New("entity not found")
	ErrConflict   = errors.New("state conflict")
	ErrBadRequest = errors.New("invalid request")
)

// defaultReportEngine is a shared singleton for rendering HTML reports when no custom engine is injected.
var defaultReportEngine = reportpkg.NewGenerator()

// ClientError represents a client-side error with a specific HTTP status code.
type ClientError struct {
	StatusCode int
	Message    string
}

func (e *ClientError) Error() string {
	return e.Message
}

// ExperimentService defines the control operations for experiments.
type ExperimentService interface {
	CreateExperiment(ctx context.Context, targetService, targetURL string, expType experiment.ExperimentType, config experiment.ExperimentConfig) (*experiment.Experiment, error)
	StartExperiment(ctx context.Context, id string, plan stress.LoadPlan) error
	StopExperiment(ctx context.Context, id, reason string) error
	GetStatus(ctx context.Context, id string) (*experiment.Experiment, error)
}

// ExperimentRepository defines persistence operations needed by the API.
type ExperimentRepository interface {
	GetByID(ctx context.Context, id string) (*experiment.Experiment, error)
	List(ctx context.Context) ([]experiment.Experiment, error)
	GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error)
	GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error)
}

// GraphAnalyzer defines the contract for microservice dependency mapping.
type GraphAnalyzer interface {
	BuildGraph(ctx context.Context, lookbackDuration int64) (*graph.DependencyGraph, error)
	ScoreCriticality(g *graph.DependencyGraph) []graph.ServiceScore
}

// SnapshotProvider supplies real-time telemetry snapshots for an active experiment.
type SnapshotProvider interface {
	GetLatestSnapshot(ctx context.Context, expID string) (*watcher.MetricSnapshot, error)
}

// ObservabilityChecker evaluates the connectivity of the observability stack.
type ObservabilityChecker interface {
	CheckHealth(ctx context.Context) (map[string]string, error)
}

// CreateExperimentDTO captures input parameters for defining a new chaos/stress experiment.
type CreateExperimentDTO struct {
	TargetService string                      `json:"target_service"`
	TargetURL     string                      `json:"target_url"`
	Type          experiment.ExperimentType   `json:"type"`
	Config        experiment.ExperimentConfig `json:"config"`
}

// StartExperimentDTO defines an optional payload for specifying or overriding the traffic plan.
type StartExperimentDTO struct {
	Plan *stress.LoadPlan `json:"plan,omitempty"`
}

// StopExperimentDTO defines the payload for manually stopping an active experiment.
type StopExperimentDTO struct {
	Reason string `json:"reason,omitempty"`
}

// StatusResponse is a generic status response container.
type StatusResponse struct {
	Status string `json:"status"`
}

// DependenciesResponse wraps the microservice dependency topology graph and criticality scores.
type DependenciesResponse struct {
	Graph  *graph.DependencyGraph `json:"graph"`
	Scores []graph.ServiceScore   `json:"scores"`
}

// ErrorResponse represents a standardized API error format.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Handler orchestrates REST HTTP endpoints and delegates to domain services.
type Handler struct {
	service          ExperimentService
	repo             ExperimentRepository
	graphAnalyzer    GraphAnalyzer
	snapshotProvider SnapshotProvider
	checker          ObservabilityChecker
	reportEngine     reportpkg.ReportEngine
	logger           *slog.Logger
}

// HandlerOption configures dependencies for Handler.
type HandlerOption func(*Handler)

// WithReportEngine injects a ReportEngine for multi-format report exports.
func WithReportEngine(re reportpkg.ReportEngine) HandlerOption {
	return func(h *Handler) {
		h.reportEngine = re
	}
}

// WithSnapshotProvider injects a SnapshotProvider.
func WithSnapshotProvider(sp SnapshotProvider) HandlerOption {
	return func(h *Handler) {
		h.snapshotProvider = sp
	}
}

// WithObservabilityChecker injects an ObservabilityChecker.
func WithObservabilityChecker(checker ObservabilityChecker) HandlerOption {
	return func(h *Handler) {
		h.checker = checker
	}
}

// WithHandlerLogger configures the logger for Handler.
func WithHandlerLogger(l *slog.Logger) HandlerOption {
	return func(h *Handler) {
		if l != nil {
			h.logger = l
		}
	}
}

// NewHandler initializes a new Handler.
func NewHandler(service ExperimentService, repo ExperimentRepository, graphAnalyzer GraphAnalyzer, opts ...HandlerOption) *Handler {
	h := &Handler{
		service:       service,
		repo:          repo,
		graphAnalyzer: graphAnalyzer,
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// isMaxBytesError checks if the error was caused by http.MaxBytesReader.
func isMaxBytesError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

// handleServiceError categorizes domain errors into appropriate HTTP status codes,
// logging unclassified failures and returning generic 500 responses.
func (h *Handler) handleServiceError(w http.ResponseWriter, err error, action string) {
	if err == nil {
		return
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		h.writeError(w, clientErr.StatusCode, clientErr.Message)
		return
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		h.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, experiment.ErrInvalidTransition) {
		h.writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, ErrBadRequest) {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "was not found") {
		h.writeError(w, http.StatusNotFound, errMsg)
		return
	}
	if strings.Contains(errMsg, "already running") || strings.Contains(errMsg, "not in StateCreated") || strings.Contains(errMsg, "not actively running") {
		h.writeError(w, http.StatusConflict, errMsg)
		return
	}
	if strings.Contains(errMsg, "unsupported") || strings.Contains(errMsg, "must be provided") || strings.Contains(errMsg, "invalid") {
		h.writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	if h.logger != nil {
		h.logger.Error("internal service failure",
			slog.String("action", action),
			slog.Any("error", err),
		)
	}
	h.writeError(w, http.StatusInternalServerError, "internal server error")
}

// CreateExperiment handles POST /api/experiments
func (h *Handler) CreateExperiment(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		h.writeError(w, http.StatusInternalServerError, "experiment service is not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var dto CreateExperimentDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		if isMaxBytesError(err) {
			h.writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds size limit")
			return
		}
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request payload: %v", err))
		return
	}

	dto.TargetService = strings.TrimSpace(dto.TargetService)
	dto.TargetURL = strings.TrimSpace(dto.TargetURL)
	if dto.TargetService == "" || dto.TargetURL == "" {
		h.writeError(w, http.StatusBadRequest, "target_service and target_url must be provided")
		return
	}

	if dto.Type != experiment.ExperimentRamp && dto.Type != experiment.ExperimentStep {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported experiment type %q; must be 'ramp' or 'step'", dto.Type))
		return
	}

	if err := dto.Config.Validate(); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid experiment config: %v", err))
		return
	}

	exp, err := h.service.CreateExperiment(r.Context(), dto.TargetService, dto.TargetURL, dto.Type, dto.Config)
	if err != nil {
		h.handleServiceError(w, err, "create_experiment")
		return
	}

	h.writeJSON(w, http.StatusCreated, exp)
}

// StartExperiment handles POST /api/experiments/{id}/start
func (h *Handler) StartExperiment(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		h.writeError(w, http.StatusInternalServerError, "experiment service is not configured")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "experiment id is required")
		return
	}

	// Fetch experiment metadata first to validate existence or extract defaults
	var exp *experiment.Experiment
	var err error
	if h.repo != nil {
		exp, err = h.repo.GetByID(r.Context(), id)
	} else {
		exp, err = h.service.GetStatus(r.Context(), id)
	}

	if err != nil {
		h.handleServiceError(w, err, "get_experiment_for_start")
		return
	}
	if exp == nil {
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("experiment %s was not found", id))
		return
	}

	if exp.State != experiment.StateCreated {
		h.writeError(w, http.StatusConflict, fmt.Sprintf("experiment %s is in state %s; only CREATED experiments can be started", id, exp.State))
		return
	}

	var plan stress.LoadPlan
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		bodyBytes, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			if isMaxBytesError(readErr) {
				h.writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds size limit")
				return
			}
			h.writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to read request body: %v", readErr))
			return
		}
		if len(strings.TrimSpace(string(bodyBytes))) > 0 {
			var startDTO struct {
				Plan *stress.LoadPlan `json:"plan"`
				stress.LoadPlan
			}
			if unmarshalErr := json.Unmarshal(bodyBytes, &startDTO); unmarshalErr != nil {
				h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request payload: %v", unmarshalErr))
				return
			}
			if startDTO.Plan != nil {
				plan = *startDTO.Plan
			} else {
				plan = startDTO.LoadPlan
			}
		}
	}

	// Fallback to experiment configuration parameters if load plan details were not supplied in request
	if strings.TrimSpace(plan.TargetURL) == "" {
		plan.TargetURL = exp.TargetURL
	}
	if strings.TrimSpace(plan.Method) == "" {
		plan.Method = "GET"
	}
	if plan.InitialRate == 0 {
		plan.InitialRate = exp.Config.InitialRate
	}
	if plan.StepRate == 0 {
		plan.StepRate = exp.Config.StepRate
	}
	if plan.MaxRate == 0 {
		plan.MaxRate = exp.Config.MaxRate
	}
	if plan.StepDurationSeconds == 0 {
		plan.StepDurationSeconds = exp.Config.StepDurationSeconds
	}

	if err := plan.Validate(); err != nil {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid load plan: %v", err))
		return
	}

	if err := h.service.StartExperiment(r.Context(), id, plan); err != nil {
		h.handleServiceError(w, err, "start_experiment")
		return
	}

	h.writeJSON(w, http.StatusOK, StatusResponse{Status: string(experiment.StateRunning)})
}

// StopExperiment handles POST /api/experiments/{id}/stop
func (h *Handler) StopExperiment(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		h.writeError(w, http.StatusInternalServerError, "experiment service is not configured")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "experiment id is required")
		return
	}

	reason := "manual stop via API"
	if r.Body != nil && r.ContentLength != 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		bodyBytes, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			if isMaxBytesError(readErr) {
				h.writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds size limit")
				return
			}
			h.writeError(w, http.StatusBadRequest, fmt.Sprintf("failed to read request body: %v", readErr))
			return
		}
		trimmed := strings.TrimSpace(string(bodyBytes))
		if len(trimmed) > 0 {
			var dto StopExperimentDTO
			if err := json.Unmarshal(bodyBytes, &dto); err != nil {
				h.writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request payload: %v", err))
				return
			}
			if strings.TrimSpace(dto.Reason) != "" {
				reason = strings.TrimSpace(dto.Reason)
			}
		}
	}

	if err := h.service.StopExperiment(r.Context(), id, reason); err != nil {
		h.handleServiceError(w, err, "stop_experiment")
		return
	}

	h.writeJSON(w, http.StatusOK, StatusResponse{Status: string(experiment.StateStopping)})
}

// GetExperiment handles GET /api/experiments/{id}
func (h *Handler) GetExperiment(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "experiment id is required")
		return
	}

	var exp *experiment.Experiment
	var err error
	if h.repo != nil {
		exp, err = h.repo.GetByID(r.Context(), id)
	} else if h.service != nil {
		exp, err = h.service.GetStatus(r.Context(), id)
	} else {
		h.writeError(w, http.StatusInternalServerError, "no experiment repository or service configured")
		return
	}

	if err != nil {
		h.handleServiceError(w, err, "get_experiment")
		return
	}
	if exp == nil {
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("experiment %s was not found", id))
		return
	}

	h.writeJSON(w, http.StatusOK, exp)
}

// ListExperiments handles GET /api/experiments
func (h *Handler) ListExperiments(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		h.writeError(w, http.StatusInternalServerError, "experiment repository is not configured")
		return
	}

	experiments, err := h.repo.List(r.Context())
	if err != nil {
		h.handleServiceError(w, err, "list_experiments")
		return
	}
	if experiments == nil {
		experiments = make([]experiment.Experiment, 0)
	}

	h.writeJSON(w, http.StatusOK, experiments)
}

// GetExperimentStatus handles GET /api/experiments/{id}/status
func (h *Handler) GetExperimentStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "experiment id is required")
		return
	}

	// Verify experiment existence first
	var exp *experiment.Experiment
	var err error
	if h.repo != nil {
		exp, err = h.repo.GetByID(r.Context(), id)
	} else if h.service != nil {
		exp, err = h.service.GetStatus(r.Context(), id)
	} else {
		h.writeError(w, http.StatusInternalServerError, "no experiment repository or service configured")
		return
	}

	if err != nil {
		h.handleServiceError(w, err, "get_experiment_status")
		return
	}
	if exp == nil {
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("experiment %s was not found", id))
		return
	}

	// 1. Try real-time SnapshotProvider if available
	if h.snapshotProvider != nil {
		snapshot, snapErr := h.snapshotProvider.GetLatestSnapshot(r.Context(), id)
		if snapErr == nil && snapshot != nil {
			h.writeJSON(w, http.StatusOK, snapshot)
			return
		}
	}

	// 2. Fall back to latest persisted observation in repository
	if h.repo != nil {
		observations, obsErr := h.repo.GetObservations(r.Context(), id)
		if obsErr == nil && len(observations) > 0 {
			latest := observations[len(observations)-1]
			snapshot := watcher.MetricSnapshot{
				ExperimentID:      id,
				TargetService:     exp.TargetService,
				Timestamp:         latest.Timestamp,
				RequestRate:       latest.LoadRate,
				P95LatencyMs:      latest.P95LatencyMs,
				ErrorRate:         latest.ErrorRate,
				Availability:      latest.Availability,
				CPUUtilization:    latest.CPUUtilization,
				MemoryUtilization: latest.MemoryUtilization,
			}
			h.writeJSON(w, http.StatusOK, snapshot)
			return
		}
	}

	// 3. If experiment has no telemetry recorded yet, return a baseline snapshot
	h.writeJSON(w, http.StatusOK, watcher.MetricSnapshot{
		ExperimentID:  id,
		TargetService: exp.TargetService,
		Timestamp:     exp.CreatedAt,
	})
}

// GetDependencies handles GET /api/dependencies
func (h *Handler) GetDependencies(w http.ResponseWriter, r *http.Request) {
	if h.graphAnalyzer == nil {
		h.writeError(w, http.StatusInternalServerError, "graph analyzer is not configured")
		return
	}

	lookback := int64(3600)
	if q := r.URL.Query().Get("lookback"); q != "" {
		if parsed, err := strconv.ParseInt(q, 10, 64); err == nil && parsed > 0 {
			lookback = parsed
		}
	} else if q := r.URL.Query().Get("lookback_seconds"); q != "" {
		if parsed, err := strconv.ParseInt(q, 10, 64); err == nil && parsed > 0 {
			lookback = parsed
		}
	}

	depGraph, err := h.graphAnalyzer.BuildGraph(r.Context(), lookback)
	if err != nil {
		h.handleServiceError(w, err, "build_graph")
		return
	}

	scores := h.graphAnalyzer.ScoreCriticality(depGraph)
	if scores == nil {
		scores = make([]graph.ServiceScore, 0)
	}

	h.writeJSON(w, http.StatusOK, DependenciesResponse{
		Graph:  depGraph,
		Scores: scores,
	})
}

// GetExperimentReport handles GET /api/experiments/{id}/report
func (h *Handler) GetExperimentReport(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		h.writeError(w, http.StatusInternalServerError, "experiment repository is not configured")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "experiment id is required")
		return
	}

	// Verify experiment exists first
	exp, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, err, "get_experiment_for_report")
		return
	}
	if exp == nil {
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("experiment %s was not found", id))
		return
	}

	report, err := h.repo.GetReport(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, err, "get_report")
		return
	}
	if report == nil {
		h.writeError(w, http.StatusNotFound, fmt.Sprintf("report for experiment %s was not found", id))
		return
	}

	// Always vary cached representations by Accept header
	w.Header().Set("Vary", "Accept")

	if prefersHTML(r) {
		engine := h.reportEngine
		if engine == nil {
			engine = defaultReportEngine
		}
		htmlBytes, err := engine.RenderHTML(report)
		if err != nil {
			h.handleServiceError(w, err, "render_html_report")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(htmlBytes)
		return
	}

	h.writeJSON(w, http.StatusOK, report)
}

// prefersHTML evaluates request format query parameter and HTTP Accept header quality values (RFC 7231).
// Explicit format takes precedence (?format=html -> true, ?format=json -> false).
// Accept: text/html is only selected when quality q > 0 and higher than application/json.
func prefersHTML(r *http.Request) bool {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "html" {
		return true
	}
	if format != "" {
		return false
	}

	accept := r.Header.Get("Accept")
	if strings.TrimSpace(accept) == "" {
		return false
	}

	var qHTML, qJSON float64 = -1.0, -1.0

	for _, clause := range strings.Split(accept, ",") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		parts := strings.Split(clause, ";")
		media := strings.TrimSpace(strings.ToLower(parts[0]))
		q := 1.0
		for _, param := range parts[1:] {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(strings.ToLower(param), "q=") {
				if parsed, err := strconv.ParseFloat(param[2:], 64); err == nil {
					q = parsed
				}
			}
		}

		switch media {
		case "text/html":
			if q > qHTML {
				qHTML = q
			}
		case "application/json":
			if q > qJSON {
				qJSON = q
			}
		}
	}

	// Do not treat q <= 0 as acceptable
	if qHTML <= 0 {
		return false
	}

	// Select HTML if acceptable and either JSON is not requested or HTML has strictly higher quality
	return qJSON < 0 || qHTML > qJSON
}

// GetHealth handles GET /api/health
func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":     "UP",
		"jaeger":     "UNKNOWN",
		"prometheus": "UNKNOWN",
	}

	if h.checker != nil {
		healthStatus, err := h.checker.CheckHealth(r.Context())
		// Merge healthStatus before applying checker error status
		for k, v := range healthStatus {
			resp[k] = strings.TrimSpace(v)
		}
		if err != nil {
			if h.logger != nil {
				h.logger.Error("observability health check probe failed", slog.Any("error", err))
			}
			resp["status"] = "DOWN"
			resp["errors"] = "observability stack health check failed"
		}
		if resp["status"] == "DOWN" {
			h.writeJSON(w, http.StatusServiceUnavailable, resp)
			return
		}
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, ErrorResponse{Error: message})
}
