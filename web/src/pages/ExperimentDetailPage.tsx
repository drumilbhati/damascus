import React, { useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useExperiment, useExperimentStatus } from '../api/hooks';
import { startExperiment, stopExperiment } from '../api/endpoints';
import {
  Play,
  Square,
  ArrowLeft,
  FileSpreadsheet,
} from 'lucide-react';

export const ExperimentDetailPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const { data: experiment, error: expError, mutate: mutateExp, isLoading } = useExperiment(id);
  const isRunning = experiment?.state === 'RUNNING';
  const { data: snapshot } = useExperimentStatus(id, isRunning);

  const [actionError, setActionError] = useState<string | null>(null);
  const [isActing, setIsActing] = useState(false);

  const handleStart = async () => {
    if (!id) return;
    setActionError(null);
    setIsActing(true);
    try {
      await startExperiment(id);
      mutateExp();
    } catch (err: any) {
      setActionError(err.message || 'Failed to start experiment');
    } finally {
      setIsActing(false);
    }
  };

  const handleStop = async () => {
    if (!id) return;
    setActionError(null);
    setIsActing(true);
    try {
      await stopExperiment(id, { reason: 'manual emergency stop via web UI' });
      mutateExp();
    } catch (err: any) {
      setActionError(err.message || 'Failed to stop experiment');
    } finally {
      setIsActing(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center text-sm text-slate-400">
        Loading experiment details...
      </div>
    );
  }

  if (expError || !experiment) {
    return (
      <div className="space-y-4">
        <Link
          to="/experiments"
          className="inline-flex items-center gap-1 text-xs font-semibold text-indigo-600 dark:text-indigo-400"
        >
          <ArrowLeft className="h-4 w-4" /> Back to experiments
        </Link>
        <div className="rounded-2xl border border-rose-200 bg-rose-50 p-6 text-center text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-400">
          <p className="font-bold">Experiment not found or failed to load</p>
          <p className="mt-1 text-xs">{expError?.message || 'Check experiment identifier'}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Top Navigation & Status */}
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
        <div className="space-y-1">
          <Link
            to="/experiments"
            className="inline-flex items-center gap-1 text-xs font-semibold text-indigo-600 hover:text-indigo-500 dark:text-indigo-400"
          >
            <ArrowLeft className="h-4 w-4" /> All Experiments
          </Link>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold tracking-tight text-slate-900 dark:text-white">
              {experiment.target_service}
            </h1>
            <span
              className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-bold ${
                experiment.state === 'RUNNING'
                  ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-400'
                  : experiment.state === 'COMPLETED'
                  ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-950/60 dark:text-indigo-400'
                  : experiment.state === 'ABORTED'
                  ? 'bg-rose-50 text-rose-700 dark:bg-rose-950/60 dark:text-rose-400'
                  : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
              }`}
            >
              {experiment.state}
            </span>
          </div>
          <p className="font-mono text-xs text-slate-400">{experiment.id}</p>
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-3">
          {experiment.state === 'CREATED' && (
            <button
              onClick={handleStart}
              disabled={isActing}
              className="inline-flex items-center gap-2 rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-emerald-500 disabled:opacity-50"
            >
              <Play className="h-4 w-4 fill-current" />
              Start Traffic Injection
            </button>
          )}

          {experiment.state === 'RUNNING' && (
            <button
              onClick={handleStop}
              disabled={isActing}
              className="inline-flex items-center gap-2 rounded-xl bg-rose-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-rose-500 disabled:opacity-50"
            >
              <Square className="h-4 w-4 fill-current" />
              Emergency Stop
            </button>
          )}

          {experiment.state === 'COMPLETED' && (
            <Link
              to="/reports"
              className="inline-flex items-center gap-2 rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500"
            >
              <FileSpreadsheet className="h-4 w-4" />
              View Analysis Report
            </Link>
          )}
        </div>
      </div>

      {actionError && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs font-semibold text-rose-600 dark:border-rose-900/50 dark:bg-rose-950/50 dark:text-rose-400">
          {actionError}
        </div>
      )}

      {/* Real-Time Live Telemetry Metrics */}
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Real-Time Telemetry Stream
          </h2>
          {isRunning && (
            <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-emerald-600 dark:text-emerald-400">
              <span className="relative flex h-2 w-2">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500"></span>
              </span>
              Live Prometheus Polling (1s)
            </span>
          )}
        </div>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <span className="text-xs font-semibold text-slate-500">Throughput (RPS)</span>
            <div className="mt-2 text-2xl font-black text-slate-900 dark:text-white">
              {snapshot?.request_rate ? `${snapshot.request_rate.toFixed(1)} req/s` : '0.0 req/s'}
            </div>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <span className="text-xs font-semibold text-slate-500">P95 Latency</span>
            <div className="mt-2 text-2xl font-black text-slate-900 dark:text-white">
              {snapshot?.p95_latency_ms ? `${snapshot.p95_latency_ms.toFixed(1)} ms` : '0.0 ms'}
            </div>
            <span className="text-[11px] text-slate-400">
              SLA Threshold: &lt;{experiment.config.max_p95_latency_ms} ms
            </span>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <span className="text-xs font-semibold text-slate-500">Error Rate</span>
            <div className="mt-2 text-2xl font-black text-slate-900 dark:text-white">
              {snapshot?.error_rate !== undefined ? `${(snapshot.error_rate * 100).toFixed(2)}%` : '0.0%'}
            </div>
            <span className="text-[11px] text-slate-400">
              Max Limit: {experiment.config.max_error_rate_percent}%
            </span>
          </div>

          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
            <span className="text-xs font-semibold text-slate-500">Availability</span>
            <div className="mt-2 text-2xl font-black text-slate-900 dark:text-white">
              {snapshot?.availability !== undefined ? `${(snapshot.availability * 100).toFixed(2)}%` : '100%'}
            </div>
            <span className="text-[11px] text-slate-400">
              Min Threshold: {experiment.config.min_availability_pct}%
            </span>
          </div>
        </div>
      </div>

      {/* Configuration & Parameter Matrix */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900 space-y-4">
          <h3 className="font-bold text-slate-900 dark:text-white">
            Traffic Plan Specifications
          </h3>
          <dl className="grid grid-cols-2 gap-4 text-xs">
            <div>
              <dt className="text-slate-400">Target Endpoint</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.target_url}
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Injection Pattern</dt>
              <dd className="mt-1 font-semibold uppercase text-slate-800 dark:text-slate-200">
                {experiment.type}
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Initial RPS</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.initial_rate} req/s
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Max Sustainable Ceiling</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.max_rate} req/s
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Step Rate Increment</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                +{experiment.config.step_rate} req/s
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Step Duration</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.step_duration_seconds} seconds
              </dd>
            </div>
          </dl>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900 space-y-4">
          <h3 className="font-bold text-slate-900 dark:text-white">
            Safety Brakes & Recovery Window
          </h3>
          <dl className="grid grid-cols-2 gap-4 text-xs">
            <div>
              <dt className="text-slate-400">Max P95 Latency SLA</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.max_p95_latency_ms} ms
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Max Error Rate SLA</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.max_error_rate_percent}%
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Minimum Availability</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.min_availability_pct}%
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">Post-Stress Recovery Wait</dt>
              <dd className="mt-1 font-mono font-semibold text-slate-800 dark:text-slate-200">
                {experiment.config.recovery_window_sec} seconds
              </dd>
            </div>
          </dl>

          {experiment.stop_reason && (
            <div className="rounded-xl bg-amber-50 p-3 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
              <span className="font-bold">Interruption Reason: </span>
              {experiment.stop_reason}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
