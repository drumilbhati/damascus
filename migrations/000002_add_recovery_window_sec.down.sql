-- PostgreSQL Schema Migration: 000002 (Down)
-- Revert recovery_window_sec column from experiments table

ALTER TABLE experiments DROP COLUMN IF EXISTS recovery_window_sec;
