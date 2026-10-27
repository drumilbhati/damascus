-- PostgreSQL DDL for DAMASCUS Control Plane
-- Migration Version: 000001 (Down)
-- Teardown tables in reverse dependency order

DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS safety_events;
DROP TABLE IF EXISTS observations;

DROP TABLE IF EXISTS service_scores;
DROP TABLE IF EXISTS experiments;
DROP TABLE IF EXISTS dependencies;
DROP TABLE IF EXISTS services;
