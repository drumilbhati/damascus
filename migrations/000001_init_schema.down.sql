-- PostgreSQL DDL for DAMASCUS Control Plane
-- Migration Version: 000001 (Down)
-- Teardown tables in reverse dependency order

DROP TABLE IF EXISTS reports;

-- ============================================================================
-- TODO (User Practice): Task 3 - Teardown `safety_events` and `observations`
-- ----------------------------------------------------------------------------
-- Objective:
--   Add DROP TABLE IF EXISTS statements for `safety_events` and `observations`
--   before dropping the tables they depend on (`experiments`).
-- ============================================================================
-- >>> YOUR CODE FOR TASK 3 STARTS HERE <<<
DROP TABLE IF EXISTS safety_events;
DROP TABLE IF EXISTS observations;

-- >>> YOUR CODE FOR TASK 3 ENDS HERE <<<

DROP TABLE IF EXISTS service_scores;
DROP TABLE IF EXISTS experiments;
DROP TABLE IF EXISTS dependencies;
DROP TABLE IF EXISTS services;
