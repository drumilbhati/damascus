package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// RouterOption configures the router and its middlewares.
type RouterOption func(*routerConfig)

type routerConfig struct {
	logger *slog.Logger
}

// WithRouterLogger sets the structured logger for the router middleware.
func WithRouterLogger(l *slog.Logger) RouterOption {
	return func(cfg *routerConfig) {
		if l != nil {
			cfg.logger = l
		}
	}
}

// NewRouter constructs an http.Handler with all REST endpoints and middlewares registered.
func NewRouter(h *Handler, opts ...RouterOption) http.Handler {
	cfg := &routerConfig{
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	mux := http.NewServeMux()

	// Register REST API endpoints per LLD specification
	mux.HandleFunc("POST /api/experiments", h.CreateExperiment)
	mux.HandleFunc("POST /api/experiments/{id}/start", h.StartExperiment)
	mux.HandleFunc("POST /api/experiments/{id}/stop", h.StopExperiment)
	mux.HandleFunc("GET /api/experiments", h.ListExperiments)
	mux.HandleFunc("GET /api/experiments/{id}", h.GetExperiment)
	mux.HandleFunc("GET /api/experiments/{id}/status", h.GetExperimentStatus)
	mux.HandleFunc("GET /api/dependencies", h.GetDependencies)
	mux.HandleFunc("GET /api/experiments/{id}/report", h.GetExperimentReport)
	mux.HandleFunc("GET /api/health", h.GetHealth)

	// Middleware execution hierarchy:
	// loggingMiddleware (outermost) wraps recoveryMiddleware, ensuring request duration
	// and HTTP 500 status are accurately logged when recoveryMiddleware recovers from a panic.
	// jsonResponseMiddleware (innermost) ensures router-level 404/405 errors are emitted as JSON.
	var handler http.Handler = mux
	handler = jsonResponseMiddleware(handler)
	handler = recoveryMiddleware(cfg.logger, handler)
	handler = loggingMiddleware(cfg.logger, handler)

	return handler
}

// recoveryMiddleware intercepts unhandled panics and returns a 500 JSON response.
func recoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if logger != nil {
					logger.Error("panic recovered in HTTP handler",
						slog.Any("recover", rec),
						slog.String("path", r.URL.Path),
						slog.String("method", r.Method),
					)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs incoming HTTP requests with latency and response status.
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)

		if logger != nil {
			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.statusCode),
				slog.Duration("duration", duration),
			)
		}
	})
}

// jsonResponseMiddleware intercepts ServeMux-generated 404/405 plain text responses,
// transforming them into JSON ErrorResponse payloads. It avoids setting JSON headers on redirects.
func jsonResponseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jw := &jsonResponseWrapper{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(jw, r)
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *statusResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

type jsonResponseWrapper struct {
	http.ResponseWriter
	statusCode    int
	isIntercepted bool
}

func (jw *jsonResponseWrapper) WriteHeader(code int) {
	jw.statusCode = code

	// Avoid setting application/json header on HTTP redirect responses (3xx)
	if code >= 300 && code < 400 {
		jw.ResponseWriter.WriteHeader(code)
		return
	}

	// Intercept ServeMux default plain-text 404/405 responses
	if (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) && jw.Header().Get("Content-Type") != "application/json" {
		jw.isIntercepted = true
		jw.Header().Set("Content-Type", "application/json")
		jw.ResponseWriter.WriteHeader(code)
		var errMsg string
		if code == http.StatusNotFound {
			errMsg = "route not found"
		} else {
			errMsg = "method not allowed"
		}
		_ = json.NewEncoder(jw.ResponseWriter).Encode(ErrorResponse{Error: errMsg})
		return
	}

	if jw.Header().Get("Content-Type") == "" {
		jw.Header().Set("Content-Type", "application/json")
	}
	jw.ResponseWriter.WriteHeader(code)
}

func (jw *jsonResponseWrapper) Write(b []byte) (int, error) {
	if jw.isIntercepted {
		// Suppress raw plain-text payload from default router handlers
		return len(b), nil
	}
	return jw.ResponseWriter.Write(b)
}
