-- PostgreSQL DDL for DAMASCUS Control Plane
-- Migration Version: 000001 (Up)

CREATE TABLE IF NOT EXISTS services (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS dependencies (
    id SERIAL PRIMARY KEY,
    source_service VARCHAR(128) NOT NULL REFERENCES services(name),
    target_service VARCHAR(128) NOT NULL REFERENCES services(name),
    call_frequency DOUBLE PRECISION DEFAULT 0.0,
    observed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS experiments (
    id VARCHAR(64) PRIMARY KEY,
    target_service VARCHAR(128) NOT NULL,
    target_url VARCHAR(512) NOT NULL,
    test_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    initial_rate INT NOT NULL,
    step_rate INT NOT NULL,
    max_rate INT NOT NULL,
    step_duration_seconds INT NOT NULL,
    max_p95_latency_ms DOUBLE PRECISION NOT NULL,
    max_error_rate DOUBLE PRECISION NOT NULL,
    min_availability DOUBLE PRECISION NOT NULL,
    stop_reason TEXT,
    started_at TIMESTAMP WITH TIME ZONE,
    ended_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS service_scores (
    id SERIAL PRIMARY KEY,
    experiment_id VARCHAR(64) REFERENCES experiments(id) ON DELETE CASCADE,
    service_name VARCHAR(128) NOT NULL,
    criticality_score DOUBLE PRECISION NOT NULL,
    reasons JSONB,
    calculated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================================
-- TODO (User Practice): Task 1 - Define the `observations` table
-- ----------------------------------------------------------------------------
-- Objective:
--   Persist granular time-series observations sampled during experiment execution.
--
-- Specifications from docs/LLD.md:
--   - Table name: observations
--   - id: SERIAL PRIMARY KEY
--   - experiment_id: VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE
--   - timestamp: TIMESTAMP WITH TIME ZONE NOT NULL
--   - load_rate: INT NOT NULL
--   - p95_latency: DOUBLE PRECISION NOT NULL
--   - error_rate: DOUBLE PRECISION NOT NULL
--   - availability: DOUBLE PRECISION NOT NULL
--   - cpu_utilization: DOUBLE PRECISION NOT NULL
--   - memory_utilization: DOUBLE PRECISION NOT NULL
-- ============================================================================
-- >>> YOUR CODE FOR TASK 1 STARTS HERE <<<
CREATE TABLE IF NOT EXISTS observations (
    id SERIAL PRIMARY KEY,
    experiment_id VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    load_rate INT NOT NULL,
    p95_latency DOUBLE PRECISION NOT NULL,
    error_rate DOUBLE PRECISION NOT NULL,
    availability DOUBLE PRECISION NOT NULL,
    cpu_utilization DOUBLE PRECISION NOT NULL,
    memory_utilization DOUBLE PRECISION NOT NULL
);

-- >>> YOUR CODE FOR TASK 1 ENDS HERE <<<


-- ============================================================================
-- TODO (User Practice): Task 2 - Define the `safety_events` table
-- ----------------------------------------------------------------------------
-- Objective:
--   Store safety intervention / SLA breach audit events triggered during tests.
--
-- Specifications from docs/LLD.md:
--   - Table name: safety_events
--   - id: SERIAL PRIMARY KEY
--   - experiment_id: VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE
--   - event_type: VARCHAR(64) NOT NULL
--   - reason: TEXT NOT NULL
--   - timestamp: TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
-- ============================================================================
-- >>> YOUR CODE FOR TASK 2 STARTS HERE <<<
CREATE TABLE IF NOT EXISTS safety_events(
    id SERIAL PRIMARY KEY,
    experiment_id VARCHAR(64) NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    event_type VARCHAR(64) NOT NULL,
    reason TEXT NOT NULL,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- >>> YOUR CODE FOR TASK 2 ENDS HERE <<<


CREATE TABLE IF NOT EXISTS reports (
    experiment_id VARCHAR(64) PRIMARY KEY REFERENCES experiments(id) ON DELETE CASCADE,
    target_service VARCHAR(128) NOT NULL,
    criticality_score DOUBLE PRECISION NOT NULL,
    maximum_tested_rate INT NOT NULL,
    maximum_sustainable_rate INT NOT NULL,
    degradation_rate INT NOT NULL,
    safety_boundary_rate INT NOT NULL,
    recovery_time_seconds DOUBLE PRECISION NOT NULL,
    report_data JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
