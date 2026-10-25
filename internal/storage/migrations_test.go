package storage_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// getMigrationsDir resolves the path to the migrations directory relative to this test file.
func getMigrationsDir(t *testing.T) string {
	t.Helper()
	// Navigate up from internal/storage to repo root
	dir, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("failed to resolve migrations directory path: %v", err)
	}
	return dir
}

// readMigrationFile reads the content of a migration file.
func readMigrationFile(t *testing.T, filename string) string {
	t.Helper()
	path := filepath.Join(getMigrationsDir(t), filename)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read migration file %s: %v", filename, err)
	}
	return string(content)
}

// TestMigrationFilesExist ensures both up and down migration files are present.
func TestMigrationFilesExist(t *testing.T) {
	files := []string{
		"000001_init_schema.up.sql", "000001_init_schema.down.sql",
		"000002_add_recovery_window_sec.up.sql", "000002_add_recovery_window_sec.down.sql",
	}
	for _, f := range files {
		path := filepath.Join(getMigrationsDir(t), f)
		stat, err := os.Stat(path)
		if err != nil {
			t.Errorf("migration file %s does not exist: %v", f, err)
			continue
		}
		if stat.Size() == 0 {
			t.Errorf("migration file %s is empty", f)
		}
	}
}

// TestUpMigration_FoundationTables validates the foundation tables that are pre-implemented.
func TestUpMigration_FoundationTables(t *testing.T) {
	upSQL := readMigrationFile(t, "000001_init_schema.up.sql")

	tests := []struct {
		table   string
		columns []string
	}{
		{
			table: "services",
			columns: []string{
				"id VARCHAR(64) PRIMARY KEY",
				"name VARCHAR(128) NOT NULL UNIQUE",
				"created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP",
			},
		},
		{
			table: "dependencies",
			columns: []string{
				"id SERIAL PRIMARY KEY",
				"source_service VARCHAR(128) NOT NULL REFERENCES services(name)",
				"target_service VARCHAR(128) NOT NULL REFERENCES services(name)",
				"call_frequency DOUBLE PRECISION DEFAULT 0.0",
				"observed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP",
			},
		},
		{
			table: "experiments",
			columns: []string{
				"id VARCHAR(64) PRIMARY KEY",
				"target_service VARCHAR(128) NOT NULL",
				"target_url VARCHAR(512) NOT NULL",
				"test_type VARCHAR(32) NOT NULL",
				"status VARCHAR(32) NOT NULL",
				"initial_rate INT NOT NULL",
				"step_rate INT NOT NULL",
				"max_rate INT NOT NULL",
				"step_duration_seconds INT NOT NULL",
				"max_p95_latency_ms DOUBLE PRECISION NOT NULL",
				"max_error_rate DOUBLE PRECISION NOT NULL",
				"min_availability DOUBLE PRECISION NOT NULL",
			},
		},
		{
			table: "service_scores",
			columns: []string{
				"id SERIAL PRIMARY KEY",
				"experiment_id VARCHAR(64) REFERENCES experiments(id) ON DELETE CASCADE",
				"service_name VARCHAR(128) NOT NULL",
				"criticality_score DOUBLE PRECISION NOT NULL",
				"reasons JSONB",
			},
		},
		{
			table: "reports",
			columns: []string{
				"experiment_id VARCHAR(64) PRIMARY KEY REFERENCES experiments(id) ON DELETE CASCADE",
				"target_service VARCHAR(128) NOT NULL",
				"criticality_score DOUBLE PRECISION NOT NULL",
				"maximum_tested_rate INT NOT NULL",
				"maximum_sustainable_rate INT NOT NULL",
				"degradation_rate INT NOT NULL",
				"safety_boundary_rate INT NOT NULL",
				"recovery_time_seconds DOUBLE PRECISION NOT NULL",
				"report_data JSONB NOT NULL",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			pattern := "(?i)CREATE\\s+TABLE\\s+(IF\\s+NOT\\s+EXISTS\\s+)?" + tt.table
			if matched, _ := regexp.MatchString(pattern, upSQL); !matched {
				t.Fatalf("table %q not defined with CREATE TABLE", tt.table)
			}
			for _, col := range tt.columns {
				// Normalize whitespace for matching
				normalizedCol := regexp.MustCompile(`\s+`).ReplaceAllString(regexp.QuoteMeta(col), `\s+`)
				re := regexp.MustCompile("(?i)" + normalizedCol)
				if !re.MatchString(upSQL) {
					t.Errorf("table %q missing column definition matching: %s", tt.table, col)
				}
			}
		})
	}
}

// TestUpMigration_TODO_Task1_Observations verifies Task 1: user implementation of `observations` table.
func TestUpMigration_TODO_Task1_Observations(t *testing.T) {
	upSQL := readMigrationFile(t, "000001_init_schema.up.sql")

	tablePattern := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?observations\b`)
	if !tablePattern.MatchString(upSQL) {
		t.Fatalf("\n❌ [TODO Task 1 Pending] 'observations' table is not yet created in migrations/000001_init_schema.up.sql.\n" +
			"   Expected: CREATE TABLE IF NOT EXISTS observations (...);\n" +
			"   Refer to the TODO Task 1 block in 000001_init_schema.up.sql or docs/LLD.md.")
	}

	expectedColumns := []string{
		"id SERIAL PRIMARY KEY",
		"experiment_id VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE",
		"timestamp TIMESTAMP WITH TIME ZONE NOT NULL",
		"load_rate INT NOT NULL",
		"p95_latency DOUBLE PRECISION NOT NULL",
		"error_rate DOUBLE PRECISION NOT NULL",
		"availability DOUBLE PRECISION NOT NULL",
		"cpu_utilization DOUBLE PRECISION NOT NULL",
		"memory_utilization DOUBLE PRECISION NOT NULL",
	}

	for _, col := range expectedColumns {
		normalized := regexp.MustCompile(`\s+`).ReplaceAllString(regexp.QuoteMeta(col), `\s+`)
		re := regexp.MustCompile("(?i)" + normalized)
		if !re.MatchString(upSQL) {
			t.Errorf("observations table is missing expected column: %s", col)
		}
	}
}

// TestUpMigration_TODO_Task2_SafetyEvents verifies Task 2: user implementation of `safety_events` table.
func TestUpMigration_TODO_Task2_SafetyEvents(t *testing.T) {
	upSQL := readMigrationFile(t, "000001_init_schema.up.sql")

	tablePattern := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?safety_events\s*\(`)
	if !tablePattern.MatchString(upSQL) {
		t.Fatalf("\n❌ [TODO Task 2 Pending / Syntax Error] 'safety_events' table DDL is missing opening parenthesis '(' after table name.\n" +
			"   Expected: CREATE TABLE IF NOT EXISTS safety_events (\n" +
			"   Refer to the TODO Task 2 block in 000001_init_schema.up.sql or docs/LLD.md.")
	}

	syntaxPattern := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?safety_events\s*\([^;]+\);`)
	if !syntaxPattern.MatchString(upSQL) {
		t.Fatalf("\n❌ [TODO Task 2 Syntax Error] 'safety_events' table DDL must close with ');'\n" +
			"   Expected: CREATE TABLE IF NOT EXISTS safety_events (...);")
	}

	expectedColumns := []string{
		"id SERIAL PRIMARY KEY",
		"experiment_id VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE",
		"event_type VARCHAR(64) NOT NULL",
		"reason TEXT NOT NULL",
		"timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP",
	}

	for _, col := range expectedColumns {
		normalized := regexp.MustCompile(`\s+`).ReplaceAllString(regexp.QuoteMeta(col), `\s+`)
		re := regexp.MustCompile("(?i)" + normalized)
		if !re.MatchString(upSQL) {
			t.Errorf("safety_events table is missing expected column: %s", col)
		}
	}
}

// TestDownMigration_TeardownOrder verifies Task 3 and the teardown sequence.
func TestDownMigration_TeardownOrder(t *testing.T) {
	downSQL := readMigrationFile(t, "000001_init_schema.down.sql")

	expectedTables := []string{
		"reports",
		"safety_events",
		"observations",
		"service_scores",
		"experiments",
		"dependencies",
		"services",
	}

	positions := make(map[string]int)
	for _, tbl := range expectedTables {
		re := regexp.MustCompile(`(?i)DROP\s+TABLE\s+(IF\s+EXISTS\s+)?` + tbl + `\s*;`)
		loc := re.FindStringIndex(downSQL)
		if loc == nil {
			if tbl == "safety_events" || tbl == "observations" {
				t.Errorf("\n❌ [TODO Task 3 Pending] 'DROP TABLE IF EXISTS %s;' not found in migrations/000001_init_schema.down.sql.\n" +
					"   Please complete Task 3 in 000001_init_schema.down.sql.", tbl)
			} else {
				t.Errorf("table %q not dropped in down migration", tbl)
			}
			continue
		}
		positions[tbl] = loc[0]
	}

	// Verify order: child tables must be dropped before parent tables
	// reports, safety_events, observations, service_scores before experiments
	// dependencies before services
	for i := 0; i < len(expectedTables)-1; i++ {
		t1 := expectedTables[i]
		t2 := expectedTables[i+1]
		pos1, ok1 := positions[t1]
		pos2, ok2 := positions[t2]
		if ok1 && ok2 && pos1 > pos2 {
			t.Errorf("incorrect drop order: %s (pos %d) should be dropped before %s (pos %d)", t1, pos1, t2, pos2)
		}
	}
}

// TestCascadeConstraints validates that foreign keys referencing experiments use ON DELETE CASCADE.
func TestCascadeConstraints(t *testing.T) {
	upSQL := readMigrationFile(t, "000001_init_schema.up.sql")

	cascadeTables := []string{"service_scores", "reports"}
	for _, tbl := range cascadeTables {
		pattern := "(?i)CREATE\\s+TABLE\\s+(IF\\s+NOT\\s+EXISTS\\s+)?" + tbl + `\b[^;]+ON\s+DELETE\s+CASCADE`
		re := regexp.MustCompile(pattern)
		if !re.MatchString(upSQL) {
			t.Errorf("table %s should specify ON DELETE CASCADE on experiments(id) foreign key", tbl)
		}
	}
}

// TestMigration_RecoveryWindowSec validates the 000002 migration that adds and reverts recovery_window_sec.
func TestMigration_RecoveryWindowSec(t *testing.T) {
	upSQL := readMigrationFile(t, "000002_add_recovery_window_sec.up.sql")
	pattern := `(?i)ALTER\s+TABLE\s+experiments\s+ADD\s+COLUMN\s+(IF\s+NOT\s+EXISTS\s+)?recovery_window_sec\s+INT`
	if matched, _ := regexp.MatchString(pattern, upSQL); !matched {
		t.Errorf("expected recovery_window_sec column migration in 000002 up migration")
	}

	downSQL := readMigrationFile(t, "000002_add_recovery_window_sec.down.sql")
	downPattern := `(?i)ALTER\s+TABLE\s+experiments\s+DROP\s+COLUMN\s+(IF\s+EXISTS\s+)?recovery_window_sec`
	if matched, _ := regexp.MatchString(downPattern, downSQL); !matched {
		t.Errorf("expected recovery_window_sec column drop in 000002 down migration")
	}
}
