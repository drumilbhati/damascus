package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"damascus/internal/autorun"
	"damascus/internal/stress"
)

// AutoRunService runs the unattended discover -> bottleneck -> test pipeline.
type AutoRunService interface {
	Start(ctx context.Context, delay time.Duration) (autorun.Run, error)
	Get(id string) (autorun.Run, bool)
	List() []autorun.Run
	Requests() []stress.RequestSpec
}

// WithAutoRun enables the /api/autoruns and /api/endpoints routes.
func WithAutoRun(svc AutoRunService) HandlerOption {
	return func(h *Handler) { h.autoRun = svc }
}

// EndpointsResponse lists the developer-configured request specs (header values redacted).
type EndpointsResponse struct {
	Requests []stress.RequestSpec `json:"requests"`
}

// AutoRunsResponse lists recent auto-runs, newest first.
type AutoRunsResponse struct {
	Runs []autorun.Run `json:"runs"`
}

func (h *Handler) requireAutoRun(w http.ResponseWriter) bool {
	if h.autoRun == nil {
		h.writeError(w, http.StatusNotImplemented, "auto-run is not configured")
		return false
	}
	return true
}

// GetEndpoints handles GET /api/endpoints.
func (h *Handler) GetEndpoints(w http.ResponseWriter, _ *http.Request) {
	if !h.requireAutoRun(w) {
		return
	}
	requests := autorun.RedactedRequests(h.autoRun.Requests())
	h.writeJSON(w, http.StatusOK, EndpointsResponse{Requests: requests})
}

// StartAutoRun handles POST /api/autoruns.
func (h *Handler) StartAutoRun(w http.ResponseWriter, _ *http.Request) {
	if !h.requireAutoRun(w) {
		return
	}
	// The run outlives this request; the orchestrator owns its cancellation.
	run, err := h.autoRun.Start(context.Background(), 0)
	switch {
	case errors.Is(err, autorun.ErrRunInProgress):
		h.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, autorun.ErrNoRequests):
		h.writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		h.writeError(w, http.StatusInternalServerError, err.Error())
	default:
		h.writeJSON(w, http.StatusAccepted, run)
	}
}

// ListAutoRuns handles GET /api/autoruns.
func (h *Handler) ListAutoRuns(w http.ResponseWriter, _ *http.Request) {
	if !h.requireAutoRun(w) {
		return
	}
	h.writeJSON(w, http.StatusOK, AutoRunsResponse{Runs: h.autoRun.List()})
}

// GetAutoRun handles GET /api/autoruns/{id}; the id "latest" selects the newest run.
func (h *Handler) GetAutoRun(w http.ResponseWriter, r *http.Request) {
	if !h.requireAutoRun(w) {
		return
	}
	run, ok := h.autoRun.Get(strings.TrimSpace(r.PathValue("id")))
	if !ok {
		h.writeError(w, http.StatusNotFound, "auto-run not found")
		return
	}
	h.writeJSON(w, http.StatusOK, run)
}
