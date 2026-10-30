package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"damascus/internal/api"
	"damascus/internal/capacity"
	"damascus/internal/config"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/observability"
	"damascus/internal/report"
	"damascus/internal/safety"
	"damascus/internal/storage"
	"damascus/internal/stress"
	"damascus/internal/watcher"
)

// ============================================================================
// TODO (User Practice): Phase 13.1 - Task 1: Environment Configuration
// ----------------------------------------------------------------------------
// Objective:
//   Define the configuration structure and environment variable loader for the
//   DAMASCUS control plane runtime.
//
// All configuration values MUST be supplied strictly via environment variables
// (e.g., from an .env file or container environment); no hardcoded defaults are permitted.
//
// Required Environment Variables:
//   - PORT: HTTP server listen port (e.g., "8080")
//   - POSTGRES_DSN: Connection URI for PostgreSQL database
//   - PROMETHEUS_URL: Base URL for Prometheus query API
//   - JAEGER_URL: Base URL for Jaeger trace ingestion API
//   - KAFKA_BROKERS: Comma-separated Kafka broker addresses
//   - TARGET_BASE_URL: Base URL for target microservice mesh under test
// ============================================================================

// Config holds runtime configuration options for the DAMASCUS control plane.
type Config struct {
	Port          string
	PostgresDSN   string
	PrometheusURL string
	JaegerURL     string
	KafkaBrokers  string
	TargetBaseURL string
}

// LoadConfig reads configuration settings directly from system environment variables.
// If any required environment variable is missing or empty, an error is returned.
func LoadConfig() (Config, error) {
	cfg := Config{
		Port:          strings.TrimSpace(os.Getenv("PORT")),
		PostgresDSN:   strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		PrometheusURL: strings.TrimSpace(os.Getenv("PROMETHEUS_URL")),
		JaegerURL:     strings.TrimSpace(os.Getenv("JAEGER_URL")),
		KafkaBrokers:  strings.TrimSpace(os.Getenv("KAFKA_BROKERS")),
		TargetBaseURL: strings.TrimSpace(os.Getenv("TARGET_BASE_URL")),
	}

	var missing []string
	if cfg.Port == "" {
		missing = append(missing, "PORT")
	}
	if cfg.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if cfg.PrometheusURL == "" {
		missing = append(missing, "PROMETHEUS_URL")
	}
	if cfg.JaegerURL == "" {
		missing = append(missing, "JAEGER_URL")
	}
	if cfg.KafkaBrokers == "" {
		missing = append(missing, "KAFKA_BROKERS")
	}
	if cfg.TargetBaseURL == "" {
		missing = append(missing, "TARGET_BASE_URL")
	}

	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required environment variables: %s (ensure .env is loaded)", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// ============================================================================
// TODO (User Practice): Phase 13.1 - Task 2: Subsystem Wiring & Main Entrypoint
// ----------------------------------------------------------------------------
// Objective:
//   Wire together the persistence, observability, analysis, stress, and API
//   subsystems into a unified control plane server with graceful shutdown.
//
// Key Steps:
//   1. Load configuration via LoadConfig().
//   2. Open PostgreSQL connection using storage.OpenDB(cfg.PostgresDSN).
//   3. Initialize repository: storage.NewPostgresExperimentRepository(db).
//   4. Instantiate subsystem engines:
//      - graph.NewAnalyzer for Jaeger dependency graph reconstruction.
//      - stress.NewEngine for HTTP rate-controlled traffic generation.
//      - watcher.NewPrometheusClient + watcher.NewWatcherEngine for RED metric polling.
//      - safety.NewController for sub-second fast-path context cancellation.
//      - capacity.NewAnalyzer for knee detection and recovery calculation.
//      - report.NewReportEngine for multi-format JSON/HTML report generation.
//      - observability.NewChecker for health monitoring probes.
//   5. Construct experiment.NewManager wiring all subsystems together.
//   6. Build HTTP handler and router using api.NewHandler and api.NewRouter.
//   7. Launch http.Server with graceful shutdown listening for SIGINT and SIGTERM.
// ============================================================================

// healthCheckerAdapter adapts observability.Checker to satisfy api.ObservabilityChecker.
type healthCheckerAdapter struct {
	checker *observability.Checker
	cfg     config.EnvironmentConfig
}

func (a *healthCheckerAdapter) CheckHealth(ctx context.Context) (map[string]string, error) {
	status, err := a.checker.CheckObservabilityStack(ctx, a.cfg)
	if err != nil {
		return map[string]string{
			"status": "DOWN",
			"error":  err.Error(),
		}, err
	}
	return map[string]string{
		"status":     status.Status,
		"target":     status.Target,
		"jaeger":     status.Jaeger,
		"prometheus": status.Prometheus,
	}, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("failed to load configuration from environment", "error", err)
		os.Exit(1)
	}

	logger.Info("starting damascus control plane",
		"port", cfg.Port,
		"prometheus_url", cfg.PrometheusURL,
		"jaeger_url", cfg.JaegerURL,
	)

	// >>> YOUR CODE FOR TASK 2 STARTS HERE <<<

	// 1. Initialize PostgreSQL database connection pool
	db, err := storage.OpenDB(cfg.PostgresDSN)
	if err != nil {
		logger.Warn("database connection unavailable at startup; proceeding with degraded mode", "error", err)
	} else {
		defer db.Close()
		logger.Info("connected to postgres database with connection pooling",
			"max_open_conns", storage.DefaultMaxOpenConns,
			"max_idle_conns", storage.DefaultMaxIdleConns,
		)
	}

	var repo *storage.PostgresExperimentRepository
	if db != nil {
		repo = storage.NewPostgresExperimentRepository(db)
	}

	// 2. Initialize HTTP client with connection reuse
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// 3. Instantiate domain engines
	graphAnalyzer := graph.NewAnalyzer(cfg.JaegerURL, httpClient)
	stressEngine := stress.NewEngine(httpClient)

	var watcherEngine *watcher.WatcherEngine
	promClient, promErr := watcher.NewPrometheusClient(cfg.PrometheusURL)
	if promErr != nil {
		logger.Warn("failed to initialize prometheus client; metrics polling disabled", "error", promErr)
	} else {
		watcherEngine = watcher.NewWatcherEngine(promClient, nil)
	}

	safetyController := safety.NewController(logger)
	capacityAnalyzer := capacity.NewAnalyzer()
	reportEngine := report.NewReportEngine()
	checker := observability.NewChecker(5 * time.Second)

	// 4. Construct ExperimentManager coordinating the 8-state experiment lifecycle
	manager := experiment.NewManager(
		graphAnalyzer,
		stressEngine,
		watcherEngine,
		safetyController,
		capacityAnalyzer,
		reportEngine,
		repo,
	)
	defer manager.Shutdown()

	// 5. Build REST API handler and routes
	healthChecker := &healthCheckerAdapter{
		checker: checker,
		cfg: config.EnvironmentConfig{
			TargetBaseURL:      cfg.TargetBaseURL,
			JaegerTraceBaseURL: cfg.JaegerURL,
			PrometheusBaseURL:  cfg.PrometheusURL,
		},
	}

	handler := api.NewHandler(
		manager,
		repo,
		graphAnalyzer,
		api.WithReportEngine(reportEngine),
		api.WithObservabilityChecker(healthChecker),
	)
	router := api.NewRouter(handler)

	// 6. Launch HTTP server with graceful shutdown
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	// 7. Wait for shutdown signal (SIGINT, SIGTERM)
	shutdownSignal := make(chan os.Signal, 1)
	signal.Notify(shutdownSignal, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrCh:
		logger.Error("server failed to start", "error", err)
		os.Exit(1)
	case sig := <-shutdownSignal:
		logger.Info("shutdown signal received; commencing graceful shutdown", "signal", sig.String())
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server graceful shutdown failed", "error", err)
	} else {
		logger.Info("server exited cleanly")
	}

}
