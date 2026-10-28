package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"damascus/internal/capacity"
	"damascus/internal/experiment"
	"damascus/internal/graph"
	"damascus/internal/storage"

	"github.com/DATA-DOG/go-sqlmock"
)

// newMockDB initializes a mock database connection and registers a test cleanup function
// that expects database closure, reports any db.Close() error, and verifies sqlmock expectations.
func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() {
		mock.ExpectClose()
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("db.Close() error: %v", closeErr)
		}
		if metErr := mock.ExpectationsWereMet(); metErr != nil {
			t.Errorf("unfulfilled expectations: %v", metErr)
		}
	})
	return db, mock
}

// TestPostgresExperimentRepository_Create verifies insertion of experiment records, handling of full and minimal configurations, nullable timestamps, and validation failure paths.
func TestPostgresExperimentRepository_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("successful insert with full configuration", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		now := time.Now().UTC()
		started := now.Add(1 * time.Second)
		ended := now.Add(10 * time.Second)

		exp := &experiment.Experiment{
			ID:            "exp-123",
			TargetService: "checkoutservice",
			TargetURL:     "http://checkout:8080/checkout",
			Type:          experiment.ExperimentStep,
			State:         experiment.StateCreated,
			Config: experiment.ExperimentConfig{
				InitialRate:         10,
				StepRate:            10,
				MaxRate:             50,
				StepDurationSeconds: 5,
				MaxP95LatencyMs:     250.0,
				MaxErrorRatePercent: 5.0,
				MinAvailabilityPct:  99.0,
				RecoveryWindowSec:   10,
			},
			StopReason: "manual halt",
			StartedAt:  &started,
			EndedAt:    &ended,
			CreatedAt:  now,
		}

		mock.ExpectExec(regexp.QuoteMeta(`
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
		)`)).WithArgs(
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
			exp.StopReason,
			started,
			ended,
			now,
		).WillReturnResult(sqlmock.NewResult(1, 1))

		if err := repo.Create(ctx, exp); err != nil {
			t.Fatalf("unexpected error creating experiment: %v", err)
		}
	})

	t.Run("successful insert with nil timestamps and empty stop reason", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		exp := &experiment.Experiment{
			ID:            "exp-minimal",
			TargetService: "paymentservice",
			TargetURL:     "http://payment:8080",
			Type:          experiment.ExperimentRamp,
			State:         experiment.StateCreated,
			Config: experiment.ExperimentConfig{
				InitialRate:         20,
				MaxRate:             100,
				StepDurationSeconds: 10,
				MaxP95LatencyMs:     150.0,
				RecoveryWindowSec:   5,
			},
		}

		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO experiments")).
			WithArgs(
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
				nil,
				nil,
				nil,
				sqlmock.AnyArg(), // Generated CreatedAt
			).WillReturnResult(sqlmock.NewResult(1, 1))

		if err := repo.Create(ctx, exp); err != nil {
			t.Fatalf("unexpected error creating experiment: %v", err)
		}

		if exp.CreatedAt.IsZero() {
			t.Errorf("expected CreatedAt to be populated when zero")
		}
	})

	t.Run("validation failure for nil or invalid experiment", func(t *testing.T) {
		db, _ := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		if err := repo.Create(ctx, nil); err == nil {
			t.Error("expected error for nil experiment, got nil")
		}

		if err := repo.Create(ctx, &experiment.Experiment{ID: ""}); err == nil {
			t.Error("expected error for empty ID, got nil")
		}

		if err := repo.Create(ctx, &experiment.Experiment{ID: "valid", TargetService: ""}); err == nil {
			t.Error("expected error for empty TargetService, got nil")
		}
	})

	t.Run("database error returns wrapped error", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO experiments")).
			WillReturnError(errors.New("connection reset by peer"))

		exp := &experiment.Experiment{
			ID:            "exp-err",
			TargetService: "service-a",
			TargetURL:     "http://test",
			Type:          experiment.ExperimentStep,
			State:         experiment.StateCreated,
		}

		err := repo.Create(ctx, exp)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

// TestPostgresExperimentRepository_GetByID verifies retrieving experiments by ID, scanning nullable columns, handling sql.ErrNoRows as (nil, nil), and reporting query errors.
func TestPostgresExperimentRepository_GetByID(t *testing.T) {
	ctx := context.Background()

	columns := []string{
		"id",
		"target_service",
		"target_url",
		"test_type",
		"status",
		"initial_rate",
		"step_rate",
		"max_rate",
		"step_duration_seconds",
		"max_p95_latency_ms",
		"max_error_rate",
		"min_availability",
		"recovery_window_sec",
		"stop_reason",
		"started_at",
		"ended_at",
		"created_at",
	}

	t.Run("successfully retrieves experiment with all fields populated", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)
		now := time.Now().UTC()
		started := now.Add(2 * time.Second)
		ended := now.Add(20 * time.Second)

		rows := sqlmock.NewRows(columns).AddRow(
			"exp-456",
			"cartservice",
			"http://cart:8080",
			"step",
			"COMPLETED",
			10,
			10,
			50,
			5,
			300.0,
			5.0,
			99.0,
			15,
			"test completed normally",
			started,
			ended,
			now,
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT id, target_service, target_url, test_type, status, initial_rate, step_rate, max_rate, step_duration_seconds, max_p95_latency_ms, max_error_rate, min_availability, recovery_window_sec, stop_reason, started_at, ended_at, created_at FROM experiments WHERE id = $1")).
			WithArgs("exp-456").
			WillReturnRows(rows)

		exp, err := repo.GetByID(ctx, "exp-456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exp == nil {
			t.Fatal("expected non-nil experiment")
		}

		if exp.ID != "exp-456" || exp.TargetService != "cartservice" {
			t.Errorf("unexpected experiment values: %+v", exp)
		}
		if exp.Type != experiment.ExperimentStep || exp.State != experiment.StateCompleted {
			t.Errorf("unexpected type or state: type=%s, state=%s", exp.Type, exp.State)
		}
		if exp.Config.RecoveryWindowSec != 15 {
			t.Errorf("expected RecoveryWindowSec=15, got %d", exp.Config.RecoveryWindowSec)
		}
		if exp.StopReason != "test completed normally" {
			t.Errorf("unexpected stop reason: %s", exp.StopReason)
		}
		if exp.StartedAt == nil || exp.EndedAt == nil {
			t.Error("expected non-nil StartedAt and EndedAt")
		}
	})

	t.Run("returns nil, nil when experiment is not found", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT id, target_service, target_url")).
			WithArgs("non-existent").
			WillReturnError(sql.ErrNoRows)

		exp, err := repo.GetByID(ctx, "non-existent")
		if err != nil {
			t.Fatalf("expected nil error on ErrNoRows, got %v", err)
		}
		if exp != nil {
			t.Errorf("expected nil experiment, got %+v", exp)
		}
	})

	t.Run("returns error on empty ID or query failure", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		if _, err := repo.GetByID(ctx, ""); err == nil {
			t.Error("expected error for empty id, got nil")
		}

		mock.ExpectQuery(regexp.QuoteMeta("SELECT id, target_service, target_url")).
			WithArgs("exp-fail").
			WillReturnError(errors.New("db disk failure"))

		if _, err := repo.GetByID(ctx, "exp-fail"); err == nil {
			t.Error("expected error for db failure, got nil")
		}
	})
}

// TestPostgresExperimentRepository_List verifies querying all experiments ordered by creation timestamp descending and handling empty result sets.
func TestPostgresExperimentRepository_List(t *testing.T) {
	ctx := context.Background()

	columns := []string{
		"id",
		"target_service",
		"target_url",
		"test_type",
		"status",
		"initial_rate",
		"step_rate",
		"max_rate",
		"step_duration_seconds",
		"max_p95_latency_ms",
		"max_error_rate",
		"min_availability",
		"recovery_window_sec",
		"stop_reason",
		"started_at",
		"ended_at",
		"created_at",
	}

	t.Run("lists multiple experiments ordered by created_at desc", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)
		now := time.Now().UTC()

		rows := sqlmock.NewRows(columns).
			AddRow("exp-1", "service-1", "http://svc1", "step", "RUNNING", 10, 5, 20, 5, 200.0, 5.0, 99.0, 10, nil, now, nil, now).
			AddRow("exp-2", "service-2", "http://svc2", "ramp", "CREATED", 5, 5, 50, 10, 100.0, 2.0, 99.5, 0, nil, nil, nil, now.Add(-time.Hour))

		mock.ExpectQuery(regexp.QuoteMeta("SELECT id, target_service, target_url, test_type, status, initial_rate, step_rate, max_rate, step_duration_seconds, max_p95_latency_ms, max_error_rate, min_availability, recovery_window_sec, stop_reason, started_at, ended_at, created_at FROM experiments ORDER BY created_at DESC")).
			WillReturnRows(rows)

		list, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("expected 2 experiments, got %d", len(list))
		}
		if list[0].ID != "exp-1" || list[1].ID != "exp-2" {
			t.Errorf("unexpected experiment order: %+v", list)
		}
		if list[0].Config.RecoveryWindowSec != 10 || list[1].Config.RecoveryWindowSec != 0 {
			t.Errorf("unexpected recovery window seconds in list: %+v", list)
		}
	})

	t.Run("returns empty slice when no experiments exist", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		rows := sqlmock.NewRows(columns)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT id, target_service, target_url")).
			WillReturnRows(rows)

		list, err := repo.List(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if list == nil || len(list) != 0 {
			t.Errorf("expected empty non-nil slice, got %+v", list)
		}
	})
}

// TestPostgresExperimentRepository_UpdateState verifies state transitions, conditional stop reason updates, automatic timestamp updates, and ErrNotFound detection when zero rows are affected.
func TestPostgresExperimentRepository_UpdateState(t *testing.T) {
	ctx := context.Background()

	t.Run("successful state transition to STOPPING with reason", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE experiments
		SET 
			status = $1,
			stop_reason = CASE WHEN $2 <> '' THEN $2 ELSE stop_reason END,
			started_at = CASE WHEN $1 = 'RUNNING' AND started_at IS NULL THEN CURRENT_TIMESTAMP ELSE started_at END,
			ended_at = CASE WHEN ($1 = 'COMPLETED' OR $1 = 'ABORTED') AND ended_at IS NULL THEN CURRENT_TIMESTAMP ELSE ended_at END
		WHERE id = $3`)).
			WithArgs(string(experiment.StateStopping), "P95 latency breached", "exp-789").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateState(ctx, "exp-789", experiment.StateStopping, "P95 latency breached")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("returns ErrNotFound when zero rows affected", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		mock.ExpectExec(regexp.QuoteMeta("UPDATE experiments SET")).
			WithArgs(string(experiment.StateRunning), "", "exp-not-found").
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateState(ctx, "exp-not-found", experiment.StateRunning, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("expected ErrNotFound in error chain, got %v", err)
		}
	})

	t.Run("returns error on empty ID", func(t *testing.T) {
		db, _ := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		if err := repo.UpdateState(ctx, "", experiment.StateRunning, ""); err == nil {
			t.Error("expected error for empty id, got nil")
		}
	})
}

// TestPostgresExperimentRepository_Observations verifies inserting telemetry snapshots, rounding load rates, retrieving observations in chronological order, and input validation.
func TestPostgresExperimentRepository_Observations(t *testing.T) {
	ctx := context.Background()

	t.Run("successfully saves and retrieves observations", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)
		now := time.Now().UTC()

		obs := capacity.Observation{
			Timestamp:         now,
			LoadRate:          45.2,
			P95LatencyMs:      120.5,
			ErrorRate:         0.01,
			Availability:      0.99,
			CPUUtilization:    0.45,
			MemoryUtilization: 0.60,
		}

		mock.ExpectExec(regexp.QuoteMeta(`
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
		)`)).WithArgs(
			"exp-obs",
			now,
			45, // rounded load rate
			obs.P95LatencyMs,
			obs.ErrorRate,
			obs.Availability,
			obs.CPUUtilization,
			obs.MemoryUtilization,
		).WillReturnResult(sqlmock.NewResult(1, 1))

		if err := repo.SaveObservation(ctx, "exp-obs", obs); err != nil {
			t.Fatalf("unexpected error saving observation: %v", err)
		}

		columns := []string{
			"timestamp",
			"load_rate",
			"p95_latency",
			"error_rate",
			"availability",
			"cpu_utilization",
			"memory_utilization",
		}

		rows := sqlmock.NewRows(columns).AddRow(
			now,
			45,
			120.5,
			0.01,
			0.99,
			0.45,
			0.60,
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT timestamp, load_rate, p95_latency, error_rate, availability, cpu_utilization, memory_utilization FROM observations WHERE experiment_id = $1 ORDER BY timestamp ASC, id ASC")).
			WithArgs("exp-obs").
			WillReturnRows(rows)

		results, err := repo.GetObservations(ctx, "exp-obs")
		if err != nil {
			t.Fatalf("unexpected error getting observations: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 observation, got %d", len(results))
		}
		if results[0].LoadRate != 45.0 || results[0].P95LatencyMs != 120.5 {
			t.Errorf("unexpected observation values: %+v", results[0])
		}
	})

	t.Run("validation failure on empty experiment id", func(t *testing.T) {
		db, _ := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		if err := repo.SaveObservation(ctx, "", capacity.Observation{}); err == nil {
			t.Error("expected error for empty expID in SaveObservation, got nil")
		}

		if _, err := repo.GetObservations(ctx, ""); err == nil {
			t.Error("expected error for empty expID in GetObservations, got nil")
		}
	})
}

// TestPostgresExperimentRepository_Reports verifies upserting capacity analysis reports with JSON data, retrieving existing reports, and handling missing reports.
func TestPostgresExperimentRepository_Reports(t *testing.T) {
	ctx := context.Background()

	t.Run("successfully saves and retrieves report", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)
		now := time.Now().UTC()

		report := &capacity.ExperimentReport{
			ExperimentID:           "exp-rep-1",
			TargetService:          "frontend",
			CriticalityScore:       0.85,
			MaximumTestedRate:      100,
			MaximumSustainableRate: 80,
			DegradationRate:        85,
			SafetyBoundaryRate:     90,
			RecoveryTimeSec:        15,
			Observations: []capacity.Observation{
				{LoadRate: 80, P95LatencyMs: 150},
			},
			Recommendations: []string{"Scale replicas of frontend to 3"},
			GeneratedAt:     now,
		}

		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO reports")).
			WithArgs(
				report.ExperimentID,
				report.TargetService,
				report.CriticalityScore,
				report.MaximumTestedRate,
				report.MaximumSustainableRate,
				report.DegradationRate,
				report.SafetyBoundaryRate,
				float64(report.RecoveryTimeSec),
				sqlmock.AnyArg(), // JSON report data
				now,
			).WillReturnResult(sqlmock.NewResult(1, 1))

		if err := repo.SaveReport(ctx, report); err != nil {
			t.Fatalf("unexpected error saving report: %v", err)
		}

		columns := []string{
			"target_service",
			"criticality_score",
			"maximum_tested_rate",
			"maximum_sustainable_rate",
			"degradation_rate",
			"safety_boundary_rate",
			"recovery_time_seconds",
			"report_data",
			"created_at",
		}

		rows := sqlmock.NewRows(columns).AddRow(
			report.TargetService,
			report.CriticalityScore,
			report.MaximumTestedRate,
			report.MaximumSustainableRate,
			report.DegradationRate,
			report.SafetyBoundaryRate,
			float64(report.RecoveryTimeSec),
			`{"experiment_id":"exp-rep-1","target_service":"frontend","criticality_score":0.85,"maximum_tested_rate":100,"maximum_sustainable_rate":80,"degradation_rate":85,"safety_boundary_rate":90,"recovery_time_sec":15,"recommendations":["Scale replicas of frontend to 3"]}`,
			now,
		)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT target_service, criticality_score, maximum_tested_rate, maximum_sustainable_rate, degradation_rate, safety_boundary_rate, recovery_time_seconds, report_data, created_at FROM reports WHERE experiment_id = $1")).
			WithArgs("exp-rep-1").
			WillReturnRows(rows)

		retrieved, err := repo.GetReport(ctx, "exp-rep-1")
		if err != nil {
			t.Fatalf("unexpected error retrieving report: %v", err)
		}
		if retrieved == nil {
			t.Fatal("expected non-nil report")
		}

		if retrieved.TargetService != "frontend" || retrieved.MaximumSustainableRate != 80 {
			t.Errorf("unexpected report content: %+v", retrieved)
		}
		if len(retrieved.Recommendations) != 1 || retrieved.Recommendations[0] != "Scale replicas of frontend to 3" {
			t.Errorf("unexpected recommendations: %+v", retrieved.Recommendations)
		}
	})

	t.Run("returns nil, nil when report is not found", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT target_service, criticality_score")).
			WithArgs("exp-none").
			WillReturnError(sql.ErrNoRows)

		rep, err := repo.GetReport(ctx, "exp-none")
		if err != nil {
			t.Fatalf("expected nil error on ErrNoRows, got %v", err)
		}
		if rep != nil {
			t.Errorf("expected nil report, got %+v", rep)
		}
	})

	t.Run("validation failure for nil or invalid report", func(t *testing.T) {
		db, _ := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		if err := repo.SaveReport(ctx, nil); err == nil {
			t.Error("expected error for nil report, got nil")
		}

		if err := repo.SaveReport(ctx, &capacity.ExperimentReport{ExperimentID: ""}); err == nil {
			t.Error("expected error for empty experiment id, got nil")
		}

		if _, err := repo.GetReport(ctx, ""); err == nil {
			t.Error("expected error for empty experiment id in GetReport, got nil")
		}
	})
}

// TestPostgresExperimentRepository_ServiceScores verifies saving topological criticality scores with JSON diagnostic reasons and retrieving them in descending score order.
func TestPostgresExperimentRepository_ServiceScores(t *testing.T) {
	ctx := context.Background()

	t.Run("successfully saves and retrieves service scores", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		scores := []graph.ServiceScore{
			{
				ServiceName: "checkout",
				Score:       0.95,
				Reasons:     []string{"Single Point of Failure", "High In-Degree"},
			},
		}

		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO service_scores")).
			WithArgs("exp-score", "checkout", 0.95, sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		if err := repo.SaveServiceScores(ctx, "exp-score", scores); err != nil {
			t.Fatalf("unexpected error saving service scores: %v", err)
		}

		columns := []string{"service_name", "criticality_score", "reasons"}
		rows := sqlmock.NewRows(columns).AddRow("checkout", 0.95, `["Single Point of Failure", "High In-Degree"]`)

		mock.ExpectQuery(regexp.QuoteMeta("SELECT service_name, criticality_score, reasons FROM service_scores WHERE experiment_id = $1 ORDER BY criticality_score DESC")).
			WithArgs("exp-score").
			WillReturnRows(rows)

		retrieved, err := repo.GetServiceScores(ctx, "exp-score")
		if err != nil {
			t.Fatalf("unexpected error getting service scores: %v", err)
		}
		if len(retrieved) != 1 {
			t.Fatalf("expected 1 score, got %d", len(retrieved))
		}
		if retrieved[0].ServiceName != "checkout" || retrieved[0].Score != 0.95 {
			t.Errorf("unexpected score content: %+v", retrieved[0])
		}
		if len(retrieved[0].Reasons) != 2 {
			t.Errorf("unexpected reasons count: %+v", retrieved[0].Reasons)
		}
	})
}

// TestPostgresExperimentRepository_RecoveryWindowSec_RoundTrip asserts that Config.RecoveryWindowSec survives a Create and GetByID round trip.
func TestPostgresExperimentRepository_RecoveryWindowSec_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db, mock := newMockDB(t)
	repo := storage.NewPostgresExperimentRepository(db)

	now := time.Now().UTC()
	exp := &experiment.Experiment{
		ID:            "exp-recovery-rt",
		TargetService: "emailservice",
		TargetURL:     "http://email:8080/send",
		Type:          experiment.ExperimentStep,
		State:         experiment.StateCreated,
		Config: experiment.ExperimentConfig{
			InitialRate:         5,
			StepRate:            5,
			MaxRate:             25,
			StepDurationSeconds: 10,
			MaxP95LatencyMs:     200.0,
			MaxErrorRatePercent: 2.0,
			MinAvailabilityPct:  99.0,
			RecoveryWindowSec:   45, // Specific recovery window duration to verify
		},
		CreatedAt: now,
	}

	// 1. Expect Create INSERT with recovery_window_sec = 45
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO experiments")).
		WithArgs(
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
			45, // recovery_window_sec
			nil,
			nil,
			nil,
			now,
		).WillReturnResult(sqlmock.NewResult(1, 1))

	if err := repo.Create(ctx, exp); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 2. Expect GetByID query returning recovery_window_sec = 45
	columns := []string{
		"id",
		"target_service",
		"target_url",
		"test_type",
		"status",
		"initial_rate",
		"step_rate",
		"max_rate",
		"step_duration_seconds",
		"max_p95_latency_ms",
		"max_error_rate",
		"min_availability",
		"recovery_window_sec",
		"stop_reason",
		"started_at",
		"ended_at",
		"created_at",
	}

	rows := sqlmock.NewRows(columns).AddRow(
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
		45, // recovery_window_sec
		nil,
		nil,
		nil,
		now,
	)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, target_service, target_url, test_type, status, initial_rate, step_rate, max_rate, step_duration_seconds, max_p95_latency_ms, max_error_rate, min_availability, recovery_window_sec, stop_reason, started_at, ended_at, created_at FROM experiments WHERE id = $1")).
		WithArgs(exp.ID).
		WillReturnRows(rows)

	loaded, err := repo.GetByID(ctx, exp.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil experiment")
	}

	if loaded.Config.RecoveryWindowSec != 45 {
		t.Fatalf("expected RecoveryWindowSec to survive round trip with value 45, got %d", loaded.Config.RecoveryWindowSec)
	}
}

// TestOpenDB verifies opening and pinging a PostgreSQL database connection with invalid connection parameters.
func TestOpenDB(t *testing.T) {
	// Attempt to connect to an unreachable DSN to verify connection error handling
	_, err := storage.OpenDB("postgres://invalid:invalid@127.0.0.1:59999/damascus?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Error("expected error when connecting to unreachable database, got nil")
	}
}

// TestConfigureDB verifies that connection pool limits are properly configured on *sql.DB.
func TestConfigureDB(t *testing.T) {
	ctx := context.Background()

	t.Run("sets connection pool parameters", func(t *testing.T) {
		db, _ := newMockDB(t)
		storage.ConfigureDB(db)

		stats := db.Stats()
		if stats.MaxOpenConnections != storage.DefaultMaxOpenConns {
			t.Errorf("expected MaxOpenConnections=%d, got %d", storage.DefaultMaxOpenConns, stats.MaxOpenConnections)
		}
	})

	t.Run("enforces max idle connections limit", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer func() {
			// One ExpectClose for each of the 5 idle connections remaining in pool when db.Close() is called
			for i := 0; i < storage.DefaultMaxIdleConns; i++ {
				mock.ExpectClose()
			}
			_ = db.Close()
			if metErr := mock.ExpectationsWereMet(); metErr != nil {
				t.Errorf("unfulfilled expectations: %v", metErr)
			}
		}()

		storage.ConfigureDB(db)

		// When the 6th connection is closed back to pool, it exceeds MaxIdleConns (5),
		// so database/sql immediately closes it.
		mock.ExpectClose()

		// Acquire 6 connections (1 more than DefaultMaxIdleConns = 5)
		conns := make([]*sql.Conn, storage.DefaultMaxIdleConns+1)
		for i := range conns {
			var connErr error
			conns[i], connErr = db.Conn(ctx)
			if connErr != nil {
				t.Fatalf("failed to acquire connection %d: %v", i, connErr)
			}
		}

		// Close all connections back to the pool
		for _, c := range conns {
			_ = c.Close()
		}

		stats := db.Stats()
		if stats.Idle > storage.DefaultMaxIdleConns {
			t.Errorf("idle connections (%d) exceeded configured limit (%d)", stats.Idle, storage.DefaultMaxIdleConns)
		}
		if stats.MaxIdleClosed != 1 {
			t.Errorf("expected 1 connection closed due to max idle limit, got %d", stats.MaxIdleClosed)
		}
	})

	t.Run("enforces connection max lifetime behavior", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("failed to create sqlmock: %v", err)
		}
		defer func() {
			// One ExpectClose for the remaining connection closed during db.Close()
			mock.ExpectClose()
			_ = db.Close()
			if metErr := mock.ExpectationsWereMet(); metErr != nil {
				t.Errorf("unfulfilled expectations: %v", metErr)
			}
		}()

		storage.ConfigureDB(db)
		// Override lifetime to a short duration to test expiration behavior
		db.SetConnMaxLifetime(10 * time.Millisecond)

		// Hold an anchor connection open so sqlmock driver remains registered (c.opened > 0)
		anchorConn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("failed to acquire anchor connection: %v", err)
		}
		defer anchorConn.Close()

		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("failed to acquire connection: %v", err)
		}
		_ = conn.Close() // return to idle pool

		time.Sleep(25 * time.Millisecond)

		// When the next connection is requested, database/sql notices the idle connection has expired,
		// closes it (first ExpectClose), and opens a fresh connection.
		mock.ExpectClose()

		conn2, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("failed to acquire connection: %v", err)
		}
		defer conn2.Close()

		stats := db.Stats()
		if stats.MaxLifetimeClosed < 1 {
			t.Errorf("expected at least 1 connection closed due to max lifetime, got %d", stats.MaxLifetimeClosed)
		}
	})

	t.Run("handles nil db safely", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ConfigureDB(nil) panicked: %v", r)
			}
		}()
		storage.ConfigureDB(nil)
	})
}

// TestWithTransaction verifies transaction execution lifecycle, commit, rollback, and error handling.
func TestWithTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("commit on success", func(t *testing.T) {
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("UPDATE experiments SET status = $1 WHERE id = $2")).
			WithArgs("COMPLETED", "exp-1").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		executed := false
		err := storage.WithTransaction(ctx, db, func(tx *sql.Tx) error {
			executed = true
			_, execErr := tx.ExecContext(ctx, "UPDATE experiments SET status = $1 WHERE id = $2", "COMPLETED", "exp-1")
			return execErr
		})

		if err != nil {
			t.Fatalf("expected WithTransaction to succeed, got error: %v", err)
		}
		if !executed {
			t.Errorf("expected transaction callback to be executed")
		}
	})

	t.Run("rollback on error", func(t *testing.T) {
		db, mock := newMockDB(t)
		expectedErr := errors.New("simulated operational error")

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("DELETE FROM experiments WHERE id = $1")).
			WithArgs("exp-1").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectRollback()

		err := storage.WithTransaction(ctx, db, func(tx *sql.Tx) error {
			_, _ = tx.ExecContext(ctx, "DELETE FROM experiments WHERE id = $1", "exp-1")
			return expectedErr
		})

		if err == nil {
			t.Fatal("expected error from WithTransaction, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error wrapping %v, got %v", expectedErr, err)
		}
	})

	t.Run("rollback on panic and re-panics", func(t *testing.T) {
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectRollback()

		panicked := false
		defer func() {
			r := recover()
			if r != nil {
				panicked = true
				if r != "unhandled panic inside transaction" {
					t.Errorf("unexpected panic value: %v", r)
				}
			}
			if !panicked {
				t.Errorf("expected panic to be re-thrown after rollback")
			}
		}()

		_ = storage.WithTransaction(ctx, db, func(tx *sql.Tx) error {
			panic("unhandled panic inside transaction")
		})
	})

	t.Run("begin transaction failure", func(t *testing.T) {
		db, mock := newMockDB(t)
		beginErr := errors.New("database connection refused")
		mock.ExpectBegin().WillReturnError(beginErr)

		err := storage.WithTransaction(ctx, db, func(tx *sql.Tx) error {
			return nil
		})

		if err == nil {
			t.Fatal("expected error when BeginTx fails, got nil")
		}
		if !errors.Is(err, beginErr) {
			t.Fatalf("expected error wrapping %v, got %v", beginErr, err)
		}
	})

	t.Run("commit failure returns error", func(t *testing.T) {
		db, mock := newMockDB(t)
		commitErr := errors.New("commit failed: serialization failure")
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(commitErr)

		err := storage.WithTransaction(ctx, db, func(tx *sql.Tx) error {
			return nil
		})

		if err == nil {
			t.Fatal("expected commit error, got nil")
		}
		if !errors.Is(err, commitErr) {
			t.Fatalf("expected error wrapping %v, got %v", commitErr, err)
		}
	})

	t.Run("rollback failure wraps original error", func(t *testing.T) {
		db, mock := newMockDB(t)
		origErr := errors.New("callback failed")
		rbErr := errors.New("connection broken during rollback")

		mock.ExpectBegin()
		mock.ExpectRollback().WillReturnError(rbErr)

		err := storage.WithTransaction(ctx, db, func(tx *sql.Tx) error {
			return origErr
		})

		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, origErr) {
			t.Fatalf("expected error wrapping %v, got %v", origErr, err)
		}
	})

	t.Run("nil transactor returns error", func(t *testing.T) {
		err := storage.WithTransaction(ctx, nil, func(tx *sql.Tx) error {
			return nil
		})
		if err == nil {
			t.Fatal("expected error with nil transactor, got nil")
		}
	})

	t.Run("nil callback returns error", func(t *testing.T) {
		db, _ := newMockDB(t)
		err := storage.WithTransaction(ctx, db, nil)
		if err == nil {
			t.Fatal("expected error with nil callback function, got nil")
		}
	})
}

// recordingTransactor records TxOptions passed to BeginTx to verify options propagation.
type recordingTransactor struct {
	db           *sql.DB
	recordedOpts *sql.TxOptions
}

func (r *recordingTransactor) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	r.recordedOpts = opts
	return r.db.BeginTx(ctx, opts)
}

// TestWithTransactionOptions verifies custom transaction options (read-only, isolation level) are forwarded to BeginTx.
func TestWithTransactionOptions(t *testing.T) {
	ctx := context.Background()
	db, mock := newMockDB(t)

	rec := &recordingTransactor{db: db}

	opts := &sql.TxOptions{
		ReadOnly:  true,
		Isolation: sql.LevelSerializable,
	}

	mock.ExpectBegin()
	mock.ExpectCommit()

	err := storage.WithTransactionOptions(ctx, rec, opts, func(tx *sql.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected WithTransactionOptions to succeed, got %v", err)
	}

	if rec.recordedOpts == nil {
		t.Fatal("expected recordedOpts to be non-nil")
	}
	if rec.recordedOpts.ReadOnly != opts.ReadOnly || rec.recordedOpts.Isolation != opts.Isolation {
		t.Errorf("expected options %+v forwarded to BeginTx, got %+v", opts, rec.recordedOpts)
	}
}

// TestWithRepoTransaction verifies transaction management using repository helpers.
func TestWithRepoTransaction(t *testing.T) {
	ctx := context.Background()

	t.Run("successful execution", func(t *testing.T) {
		db, mock := newMockDB(t)
		mock.ExpectBegin()
		mock.ExpectCommit()

		called := false
		err := storage.WithRepoTransaction(ctx, db, func(repo *storage.PostgresExperimentRepository) error {
			called = true
			if repo == nil {
				return errors.New("expected non-nil repo")
			}
			return nil
		})

		if err != nil {
			t.Fatalf("expected WithRepoTransaction to succeed, got %v", err)
		}
		if !called {
			t.Error("expected callback to be executed")
		}
	})

	t.Run("nil callback returns error", func(t *testing.T) {
		db, _ := newMockDB(t)
		err := storage.WithRepoTransaction(ctx, db, nil)
		if err == nil {
			t.Fatal("expected error with nil repo callback, got nil")
		}
	})
}

// TestPostgresExperimentRepository_WithTx verifies repository instance WithTx method.
func TestPostgresExperimentRepository_WithTx(t *testing.T) {
	ctx := context.Background()

	t.Run("successful transactional repository execution", func(t *testing.T) {
		db, mock := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		now := time.Now().UTC()
		exp := &experiment.Experiment{
			ID:            "exp-tx-1",
			TargetService: "paymentservice",
			TargetURL:     "http://payment:8080/pay",
			Type:          experiment.ExperimentStep,
			State:         experiment.StateCreated,
			Config: experiment.ExperimentConfig{
				InitialRate:         10,
				StepRate:            5,
				MaxRate:             30,
				StepDurationSeconds: 5,
				MaxP95LatencyMs:     200.0,
				MaxErrorRatePercent: 2.0,
				MinAvailabilityPct:  99.5,
				RecoveryWindowSec:   15,
			},
			CreatedAt: now,
		}

		mock.ExpectBegin()
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO experiments")).
			WithArgs(
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
				nil,
				nil,
				nil,
				now,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		err := repo.WithTx(ctx, func(txRepo *storage.PostgresExperimentRepository) error {
			return txRepo.Create(ctx, exp)
		})

		if err != nil {
			t.Fatalf("expected WithTx to succeed, got %v", err)
		}
	})

	t.Run("unsupported transactor error", func(t *testing.T) {
		// Construct repository with an interface that does NOT satisfy Transactor
		fakeDBTX := &mockNonTransactor{}
		repo := storage.NewPostgresExperimentRepository(fakeDBTX)

		err := repo.WithTx(ctx, func(txRepo *storage.PostgresExperimentRepository) error {
			return nil
		})
		if err == nil {
			t.Fatal("expected error when DB does not support transactions, got nil")
		}
	})

	t.Run("nil callback returns error", func(t *testing.T) {
		db, _ := newMockDB(t)
		repo := storage.NewPostgresExperimentRepository(db)

		err := repo.WithTx(ctx, nil)
		if err == nil {
			t.Fatal("expected error with nil callback, got nil")
		}
	})
}

// mockNonTransactor implements DBTX but does not implement Transactor
type mockNonTransactor struct{}

func (m *mockNonTransactor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, nil
}
func (m *mockNonTransactor) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return nil, nil
}
func (m *mockNonTransactor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return nil, nil
}
func (m *mockNonTransactor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return nil
}
