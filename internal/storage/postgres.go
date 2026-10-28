package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"

	_ "github.com/lib/pq"
)

// ErrNotFound is returned when a requested database entity is not found.
var ErrNotFound = errors.New("storage: entity not found")

// DBTX defines the common SQL operations satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// PostgresExperimentRepository implements experiment.ExperimentRepository using PostgreSQL.
type PostgresExperimentRepository struct {
	db DBTX
}

// Ensure PostgresExperimentRepository satisfies the experiment.ExperimentRepository interface.
var _ experiment.ExperimentRepository = (*PostgresExperimentRepository)(nil)

// NewPostgresExperimentRepository constructs a new PostgresExperimentRepository.
func NewPostgresExperimentRepository(db DBTX) *PostgresExperimentRepository {
	return &PostgresExperimentRepository{db: db}
}

// ============================================================================
// Database Connection Pool Configuration
// ----------------------------------------------------------------------------
// PostgreSQL connection pooling configuration parameters for the Damascus
// control plane. These limits prevent database connection exhaustion under
// heavy experiment telemetry load and eliminate stale connection churn.
// ============================================================================

// Default connection pool configuration parameters for PostgreSQL persistence.
const (
	// DefaultMaxOpenConns caps the total number of simultaneous open connections (active + idle)
	// to prevent exhausting PostgreSQL server process limits (typically 100 by default).
	DefaultMaxOpenConns = 25

	// DefaultMaxIdleConns keeps warm connections open in memory to eliminate TCP/TLS handshake latency
	// on bursty telemetry write spikes.
	DefaultMaxIdleConns = 5

	// DefaultConnMaxLifetime closes and recycles connections after 5 minutes, preventing
	// "connection reset by peer" errors caused by cloud NAT/firewall idle connection drops.
	DefaultConnMaxLifetime = 5 * time.Minute
)

// ConfigureDB applies standard production connection pool limits to a *sql.DB handle.
func ConfigureDB(db *sql.DB) {
	if db == nil {
		return
	}

	db.SetMaxOpenConns(DefaultMaxOpenConns)
	db.SetMaxIdleConns(DefaultMaxIdleConns)
	db.SetConnMaxLifetime(DefaultConnMaxLifetime)
}

// OpenDB opens a connection to PostgreSQL using the provided connection string,
// configures production-ready connection pool limits (25 max open, 5 max idle, 5m max lifetime),
// and verifies connectivity via Ping.
func OpenDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Apply connection pool settings before pinging
	ConfigureDB(db)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return db, nil
}

// ============================================================================
// Database Transaction Execution Helpers
// ----------------------------------------------------------------------------
// Provides atomic transaction lifecycle management for database operations:
// - Transactor interface satisfied by *sql.DB (BeginTx)
// - WithTransaction / WithTransactionOptions managing commit / rollback lifecycle
// - Panic recovery with automatic rollback
// - WithTx / WithRepoTransaction repository execution helpers
// ============================================================================

// Transactor defines the interface required to initiate a database transaction.
// *sql.DB inherently implements this contract.
type Transactor interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// TxFunc is a callback function executed within the scope of a database transaction.
type TxFunc func(tx *sql.Tx) error

// WithTransaction executes the provided function fn within a managed database transaction.
// If fn returns an error or panics, the transaction is safely rolled back.
// If fn succeeds without error, the transaction is committed.
func WithTransaction(ctx context.Context, db Transactor, fn TxFunc) error {
	return WithTransactionOptions(ctx, db, nil, fn)
}

// WithTransactionOptions executes fn inside a managed transaction configured with sql.TxOptions.
// If fn returns an error or panics, the transaction is safely rolled back.
// If fn succeeds, the transaction is committed.
func WithTransactionOptions(ctx context.Context, db Transactor, opts *sql.TxOptions, fn TxFunc) (err error) {
	if db == nil {
		return errors.New("database transactor cannot be nil")
	}
	if fn == nil {
		return errors.New("transaction callback function cannot be nil")
	}

	tx, txErr := db.BeginTx(ctx, opts)
	if txErr != nil {
		return fmt.Errorf("failed to begin transaction: %w", txErr)
	}

	defer func() {
		if p := recover(); p != nil {
			// Roll back transaction on panic, then re-throw panic
			_ = tx.Rollback()
			panic(p)
		} else if err != nil {
			// Roll back transaction on operation error; ignore ErrTxDone if already rolled back
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = fmt.Errorf("transaction rollback failed: %v (original error: %w)", rbErr, err)
			}
		} else {
			// Commit when callback completes without error
			if commitErr := tx.Commit(); commitErr != nil {
				err = fmt.Errorf("failed to commit transaction: %w", commitErr)
			}
		}
	}()

	err = fn(tx)
	return err
}

// WithRepoTransaction executes fn with a PostgresExperimentRepository bound to a newly created transaction.
// If fn returns an error or panics, all operations in the repository are rolled back.
func WithRepoTransaction(ctx context.Context, db Transactor, fn func(repo *PostgresExperimentRepository) error) error {
	if fn == nil {
		return errors.New("repository transaction callback cannot be nil")
	}

	return WithTransaction(ctx, db, func(tx *sql.Tx) error {
		txRepo := NewPostgresExperimentRepository(tx)
		return fn(txRepo)
	})
}

// WithTx executes the provided callback within an atomic transaction.
// The repository's underlying DBTX must implement Transactor (e.g., *sql.DB).
// A new PostgresExperimentRepository instance bound to the transaction is passed to fn.
func (r *PostgresExperimentRepository) WithTx(ctx context.Context, fn func(repo *PostgresExperimentRepository) error) error {
	if fn == nil {
		return errors.New("transaction callback cannot be nil")
	}

	transactor, ok := r.db.(Transactor)
	if !ok {
		return errors.New("underlying database connection does not support transactions")
	}

	return WithRepoTransaction(ctx, transactor, fn)
}

// Create inserts a new experiment record into the database.
func (r *PostgresExperimentRepository) Create(ctx context.Context, exp *experiment.Experiment) error {
	if exp == nil {
		return errors.New("experiment cannot be nil")
	}
	if strings.TrimSpace(exp.ID) == "" {
		return errors.New("experiment id cannot be empty")
	}
	if strings.TrimSpace(exp.TargetService) == "" {
		return errors.New("experiment target_service cannot be empty")
	}

	createdAt := exp.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		exp.CreatedAt = createdAt
	}

	var startedAtVal, endedAtVal, stopReasonVal any
	if exp.StartedAt != nil && !exp.StartedAt.IsZero() {
		startedAtVal = *exp.StartedAt
	}
	if exp.EndedAt != nil && !exp.EndedAt.IsZero() {
		endedAtVal = *exp.EndedAt
	}
	if strings.TrimSpace(exp.StopReason) != "" {
		stopReasonVal = exp.StopReason
	}

	query := `
		INSERT INTO experiments (
			id,
			target_service,
			target_url,
			test_type,
			status,
			initial_rate,
			step_rate,
			max_rate,
			step_duration_seconds,
			max_p95_latency_ms,
			max_error_rate,
			min_availability,
			recovery_window_sec,
			stop_reason,
			started_at,
			ended_at,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)`

	_, err := r.db.ExecContext(ctx, query,
		exp.ID,
		exp.TargetService,
		exp.TargetURL,
		string(exp.Type),
		string(exp.State),
		exp.Config.InitialRate,
		exp.Config.StepRate,
		exp.Config.MaxRate,
		exp.Config.StepDurationSeconds,
		exp.Config.MaxP95LatencyMs,
		exp.Config.MaxErrorRatePercent,
		exp.Config.MinAvailabilityPct,
		exp.Config.RecoveryWindowSec,
		stopReasonVal,
		startedAtVal,
		endedAtVal,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert experiment %s: %w", exp.ID, err)
	}

	return nil
}

// GetByID retrieves an experiment by its primary ID. Returns nil, nil if not found.
func (r *PostgresExperimentRepository) GetByID(ctx context.Context, id string) (*experiment.Experiment, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("experiment id cannot be empty")
	}

	query := `
		SELECT 
			id,
			target_service,
			target_url,
			test_type,
			status,
			initial_rate,
			step_rate,
			max_rate,
			step_duration_seconds,
			max_p95_latency_ms,
			max_error_rate,
			min_availability,
			recovery_window_sec,
			stop_reason,
			started_at,
			ended_at,
			created_at
		FROM experiments
		WHERE id = $1`

	var (
		exp        experiment.Experiment
		testType   string
		status     string
		stopReason sql.NullString
		startedAt  sql.NullTime
		endedAt    sql.NullTime
	)

	row := r.db.QueryRowContext(ctx, query, id)
	err := row.Scan(
		&exp.ID,
		&exp.TargetService,
		&exp.TargetURL,
		&testType,
		&status,
		&exp.Config.InitialRate,
		&exp.Config.StepRate,
		&exp.Config.MaxRate,
		&exp.Config.StepDurationSeconds,
		&exp.Config.MaxP95LatencyMs,
		&exp.Config.MaxErrorRatePercent,
		&exp.Config.MinAvailabilityPct,
		&exp.Config.RecoveryWindowSec,
		&stopReason,
		&startedAt,
		&endedAt,
		&exp.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query experiment %s: %w", id, err)
	}

	exp.Type = experiment.ExperimentType(testType)
	exp.State = experiment.ExperimentState(status)
	if stopReason.Valid {
		exp.StopReason = stopReason.String
	}
	if startedAt.Valid {
		t := startedAt.Time
		exp.StartedAt = &t
	}
	if endedAt.Valid {
		t := endedAt.Time
		exp.EndedAt = &t
	}

	return &exp, nil
}

// List returns all experiments ordered by created_at descending.
func (r *PostgresExperimentRepository) List(ctx context.Context) ([]experiment.Experiment, error) {
	query := `
		SELECT 
			id,
			target_service,
			target_url,
			test_type,
			status,
			initial_rate,
			step_rate,
			max_rate,
			step_duration_seconds,
			max_p95_latency_ms,
			max_error_rate,
			min_availability,
			recovery_window_sec,
			stop_reason,
			started_at,
			ended_at,
			created_at
		FROM experiments
		ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list experiments: %w", err)
	}
	defer rows.Close()

	experiments := make([]experiment.Experiment, 0)
	for rows.Next() {
		var (
			exp        experiment.Experiment
			testType   string
			status     string
			stopReason sql.NullString
			startedAt  sql.NullTime
			endedAt    sql.NullTime
		)

		err := rows.Scan(
			&exp.ID,
			&exp.TargetService,
			&exp.TargetURL,
			&testType,
			&status,
			&exp.Config.InitialRate,
			&exp.Config.StepRate,
			&exp.Config.MaxRate,
			&exp.Config.StepDurationSeconds,
			&exp.Config.MaxP95LatencyMs,
			&exp.Config.MaxErrorRatePercent,
			&exp.Config.MinAvailabilityPct,
			&exp.Config.RecoveryWindowSec,
			&stopReason,
			&startedAt,
			&endedAt,
			&exp.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan experiment row: %w", err)
		}

		exp.Type = experiment.ExperimentType(testType)
		exp.State = experiment.ExperimentState(status)
		if stopReason.Valid {
			exp.StopReason = stopReason.String
		}
		if startedAt.Valid {
			t := startedAt.Time
			exp.StartedAt = &t
		}
		if endedAt.Valid {
			t := endedAt.Time
			exp.EndedAt = &t
		}

		experiments = append(experiments, exp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading experiment rows: %w", err)
	}

	return experiments, nil
}

// UpdateState transitions the lifecycle state and optional stop reason of an experiment.
// Automatically records started_at when entering RUNNING, and ended_at when entering terminal states (COMPLETED, ABORTED).
func (r *PostgresExperimentRepository) UpdateState(ctx context.Context, id string, state experiment.ExperimentState, stopReason string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("experiment id cannot be empty")
	}

	query := `
		UPDATE experiments
		SET 
			status = $1,
			stop_reason = CASE WHEN $2 <> '' THEN $2 ELSE stop_reason END,
			started_at = CASE WHEN $1 = 'RUNNING' AND started_at IS NULL THEN CURRENT_TIMESTAMP ELSE started_at END,
			ended_at = CASE WHEN ($1 = 'COMPLETED' OR $1 = 'ABORTED') AND ended_at IS NULL THEN CURRENT_TIMESTAMP ELSE ended_at END
		WHERE id = $3`

	res, err := r.db.ExecContext(ctx, query, string(state), stopReason, id)
	if err != nil {
		return fmt.Errorf("failed to update state for experiment %s: %w", id, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected for experiment %s: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("experiment %s not found: %w", id, ErrNotFound)
	}

	return nil
}

// SaveObservation appends a granular telemetry metric snapshot to the observations table.
func (r *PostgresExperimentRepository) SaveObservation(ctx context.Context, expID string, obs capacity.Observation) error {
	if strings.TrimSpace(expID) == "" {
		return errors.New("experiment id cannot be empty")
	}

	ts := obs.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	query := `
		INSERT INTO observations (
			experiment_id,
			timestamp,
			load_rate,
			p95_latency,
			error_rate,
			availability,
			cpu_utilization,
			memory_utilization
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)`

	loadRate := int(math.Round(obs.LoadRate))

	_, err := r.db.ExecContext(ctx, query,
		expID,
		ts,
		loadRate,
		obs.P95LatencyMs,
		obs.ErrorRate,
		obs.Availability,
		obs.CPUUtilization,
		obs.MemoryUtilization,
	)
	if err != nil {
		return fmt.Errorf("failed to insert observation for experiment %s: %w", expID, err)
	}

	return nil
}

// GetObservations retrieves all recorded observations for an experiment in chronological order.
func (r *PostgresExperimentRepository) GetObservations(ctx context.Context, expID string) ([]capacity.Observation, error) {
	if strings.TrimSpace(expID) == "" {
		return nil, errors.New("experiment id cannot be empty")
	}

	query := `
		SELECT 
			timestamp,
			load_rate,
			p95_latency,
			error_rate,
			availability,
			cpu_utilization,
			memory_utilization
		FROM observations
		WHERE experiment_id = $1
		ORDER BY timestamp ASC, id ASC`

	rows, err := r.db.QueryContext(ctx, query, expID)
	if err != nil {
		return nil, fmt.Errorf("failed to query observations for experiment %s: %w", expID, err)
	}
	defer rows.Close()

	observations := make([]capacity.Observation, 0)
	for rows.Next() {
		var (
			obs      capacity.Observation
			loadRate int
		)

		err := rows.Scan(
			&obs.Timestamp,
			&loadRate,
			&obs.P95LatencyMs,
			&obs.ErrorRate,
			&obs.Availability,
			&obs.CPUUtilization,
			&obs.MemoryUtilization,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan observation row: %w", err)
		}

		obs.LoadRate = float64(loadRate)
		observations = append(observations, obs)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading observation rows: %w", err)
	}

	return observations, nil
}

// SaveReport stores or updates the compiled capacity analysis report for an experiment.
func (r *PostgresExperimentRepository) SaveReport(ctx context.Context, report *capacity.ExperimentReport) error {
	if report == nil {
		return errors.New("report cannot be nil")
	}
	if strings.TrimSpace(report.ExperimentID) == "" {
		return errors.New("report experiment_id cannot be empty")
	}

	reportData, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report data: %w", err)
	}

	createdAt := report.GeneratedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		report.GeneratedAt = createdAt
	}

	query := `
		INSERT INTO reports (
			experiment_id,
			target_service,
			criticality_score,
			maximum_tested_rate,
			maximum_sustainable_rate,
			degradation_rate,
			safety_boundary_rate,
			recovery_time_seconds,
			report_data,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
		ON CONFLICT (experiment_id) DO UPDATE SET
			target_service = EXCLUDED.target_service,
			criticality_score = EXCLUDED.criticality_score,
			maximum_tested_rate = EXCLUDED.maximum_tested_rate,
			maximum_sustainable_rate = EXCLUDED.maximum_sustainable_rate,
			degradation_rate = EXCLUDED.degradation_rate,
			safety_boundary_rate = EXCLUDED.safety_boundary_rate,
			recovery_time_seconds = EXCLUDED.recovery_time_seconds,
			report_data = EXCLUDED.report_data,
			created_at = EXCLUDED.created_at`

	_, err = r.db.ExecContext(ctx, query,
		report.ExperimentID,
		report.TargetService,
		report.CriticalityScore,
		report.MaximumTestedRate,
		report.MaximumSustainableRate,
		report.DegradationRate,
		report.SafetyBoundaryRate,
		float64(report.RecoveryTimeSec),
		reportData,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save report for experiment %s: %w", report.ExperimentID, err)
	}

	return nil
}

// GetReport retrieves the capacity analysis report for an experiment. Returns nil, nil if not found.
func (r *PostgresExperimentRepository) GetReport(ctx context.Context, expID string) (*capacity.ExperimentReport, error) {
	if strings.TrimSpace(expID) == "" {
		return nil, errors.New("experiment id cannot be empty")
	}

	query := `
		SELECT 
			target_service,
			criticality_score,
			maximum_tested_rate,
			maximum_sustainable_rate,
			degradation_rate,
			safety_boundary_rate,
			recovery_time_seconds,
			report_data,
			created_at
		FROM reports
		WHERE experiment_id = $1`

	var (
		targetService       string
		criticalityScore    float64
		maxTestedRate       int
		maxSustainableRate  int
		degradationRate     int
		safetyBoundaryRate  int
		recoveryTimeSeconds float64
		reportData          []byte
		createdAt           time.Time
	)

	row := r.db.QueryRowContext(ctx, query, expID)
	err := row.Scan(
		&targetService,
		&criticalityScore,
		&maxTestedRate,
		&maxSustainableRate,
		&degradationRate,
		&safetyBoundaryRate,
		&recoveryTimeSeconds,
		&reportData,
		&createdAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query report for experiment %s: %w", expID, err)
	}

	var report capacity.ExperimentReport
	if len(reportData) > 0 {
		if err := json.Unmarshal(reportData, &report); err != nil {
			return nil, fmt.Errorf("failed to unmarshal report_data for experiment %s: %w", expID, err)
		}
	}

	report.ExperimentID = expID
	if report.TargetService == "" {
		report.TargetService = targetService
	}
	if report.CriticalityScore == 0 {
		report.CriticalityScore = criticalityScore
	}
	if report.MaximumTestedRate == 0 {
		report.MaximumTestedRate = maxTestedRate
	}
	if report.MaximumSustainableRate == 0 {
		report.MaximumSustainableRate = maxSustainableRate
	}
	if report.DegradationRate == 0 {
		report.DegradationRate = degradationRate
	}
	if report.SafetyBoundaryRate == 0 {
		report.SafetyBoundaryRate = safetyBoundaryRate
	}
	if report.RecoveryTimeSec == 0 {
		report.RecoveryTimeSec = int(math.Round(recoveryTimeSeconds))
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = createdAt
	}

	return &report, nil
}

// SaveServiceScores persists criticality scores calculated for an experiment into service_scores table.
func (r *PostgresExperimentRepository) SaveServiceScores(ctx context.Context, expID string, scores []graph.ServiceScore) error {
	if strings.TrimSpace(expID) == "" {
		return errors.New("experiment id cannot be empty")
	}

	query := `
		INSERT INTO service_scores (
			experiment_id,
			service_name,
			criticality_score,
			reasons,
			calculated_at
		) VALUES ($1, $2, $3, $4, $5)`

	now := time.Now().UTC()
	for _, score := range scores {
		reasonsJSON, err := json.Marshal(score.Reasons)
		if err != nil {
			return fmt.Errorf("failed to marshal reasons for service %s: %w", score.ServiceName, err)
		}

		_, err = r.db.ExecContext(ctx, query,
			expID,
			score.ServiceName,
			score.Score,
			reasonsJSON,
			now,
		)
		if err != nil {
			return fmt.Errorf("failed to insert service score for %s: %w", score.ServiceName, err)
		}
	}

	return nil
}

// GetServiceScores retrieves all recorded service scores for an experiment.
func (r *PostgresExperimentRepository) GetServiceScores(ctx context.Context, expID string) ([]graph.ServiceScore, error) {
	if strings.TrimSpace(expID) == "" {
		return nil, errors.New("experiment id cannot be empty")
	}

	query := `
		SELECT service_name, criticality_score, reasons
		FROM service_scores
		WHERE experiment_id = $1
		ORDER BY criticality_score DESC`

	rows, err := r.db.QueryContext(ctx, query, expID)
	if err != nil {
		return nil, fmt.Errorf("failed to query service scores for experiment %s: %w", expID, err)
	}
	defer rows.Close()

	scores := make([]graph.ServiceScore, 0)
	for rows.Next() {
		var (
			serviceName string
			scoreVal    float64
			reasonsRaw  []byte
		)

		if err := rows.Scan(&serviceName, &scoreVal, &reasonsRaw); err != nil {
			return nil, fmt.Errorf("failed to scan service score: %w", err)
		}

		var reasons []string
		if len(reasonsRaw) > 0 {
			if err := json.Unmarshal(reasonsRaw, &reasons); err != nil {
				return nil, fmt.Errorf("failed to unmarshal reasons for service %s: %w", serviceName, err)
			}
		}

		scores = append(scores, graph.ServiceScore{
			ServiceName: serviceName,
			Score:       scoreVal,
			Reasons:     reasons,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading service score rows: %w", err)
	}

	return scores, nil
}
