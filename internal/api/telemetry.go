package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"damascus/internal/telemetry"
)

// TelemetrySettings exposes the Jaeger and Prometheus APIs Damascus reads from.
type TelemetrySettings interface {
	Get() telemetry.Settings
	Update(ctx context.Context, next telemetry.Sources) (telemetry.Settings, error)
}

// WithTelemetrySettings enables GET/PUT /api/settings/telemetry.
func WithTelemetrySettings(s TelemetrySettings) HandlerOption {
	return func(h *Handler) { h.telemetry = s }
}

// TelemetryErrorResponse carries per-field validation or connectivity errors.
type TelemetryErrorResponse struct {
	Error  string            `json:"error"`
	Fields map[string]string `json:"fields,omitempty"`
}

// GetTelemetrySettings handles GET /api/settings/telemetry.
func (h *Handler) GetTelemetrySettings(w http.ResponseWriter, _ *http.Request) {
	if h.telemetry == nil {
		h.writeError(w, http.StatusNotImplemented, "telemetry settings are not configured")
		return
	}
	h.writeJSON(w, http.StatusOK, h.telemetry.Get())
}

// UpdateTelemetrySettings handles PUT /api/settings/telemetry. Both URLs are
// probed before they replace the live configuration.
func (h *Handler) UpdateTelemetrySettings(w http.ResponseWriter, r *http.Request) {
	if h.telemetry == nil {
		h.writeError(w, http.StatusNotImplemented, "telemetry settings are not configured")
		return
	}
	var next telemetry.Sources
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&next); err != nil {
		h.writeError(w, http.StatusBadRequest, "body must be {\"jaeger_url\": \"...\", \"prometheus_url\": \"...\"}")
		return
	}
	settings, err := h.telemetry.Update(r.Context(), next)
	var fieldErr *telemetry.FieldError
	switch {
	case errors.As(err, &fieldErr):
		h.writeJSON(w, http.StatusUnprocessableEntity, TelemetryErrorResponse{Error: fieldErr.Error(), Fields: fieldErr.Fields})
	case errors.Is(err, telemetry.ErrBusy):
		h.writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		h.writeError(w, http.StatusInternalServerError, err.Error())
	default:
		h.writeJSON(w, http.StatusOK, settings)
	}
}
