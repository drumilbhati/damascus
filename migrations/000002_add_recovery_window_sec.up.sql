-- PostgreSQL Schema Migration: 000002 (Up)
-- Add recovery_window_sec to experiments table

ALTER TABLE experiments ADD COLUMN IF NOT EXISTS recovery_window_sec INT NOT NULL DEFAULT 0;
