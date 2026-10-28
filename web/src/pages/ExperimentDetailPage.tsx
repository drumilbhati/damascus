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

function StateBadge({ state }: { state: string }) {
  const styles: Record<string, { color: string; bg: string }> = {
    RUNNING: { color: '#3fb950', bg: 'rgba(63,185,80,0.12)' },
    COMPLETED: { color: '#58a6ff', bg: 'rgba(88,166,255,0.12)' },
    ABORTED: { color: '#f85149', bg: 'rgba(248,81,73,0.12)' },
    CREATED: { color: '#d29922', bg: 'rgba(210,153,34,0.12)' },
  };
  const s = styles[state] || { color: '#8b949e', bg: '#21262d' };
  return (
    <span
      className="rounded px-2 py-0.5 font-mono text-[10px] font-semibold"
      style={{ color: s.color, backgroundColor: s.bg }}
    >
      {state}
    </span>
  );
}

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
      <div className="flex h-64 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
        Loading experiment details...
      </div>
    );
  }

  if (expError || !experiment) {
    return (
      <div className="space-y-4">
        <Link
          to="/experiments"
          className="inline-flex items-center gap-1 text-xs font-semibold"
          style={{ color: '#58a6ff' }}
        >
          <ArrowLeft className="h-3.5 w-3.5" /> Back to experiments
        </Link>
        <div
          className="rounded p-5 text-center text-xs"
          style={{ backgroundColor: 'rgba(248,81,73,0.08)', border: '1px solid rgba(248,81,73,0.25)', color: '#f85149' }}
        >
          <p className="font-bold">Experiment not found or failed to load</p>
          <p className="mt-1" style={{ color: '#8b949e' }}>{expError?.message || 'Check experiment identifier'}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      {/* Breadcrumb + header */}
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div className="space-y-1">
          <Link
            to="/experiments"
            className="inline-flex items-center gap-1 text-[11px] font-semibold"
            style={{ color: '#58a6ff' }}
          >
            <ArrowLeft className="h-3.5 w-3.5" /> All Experiments
          </Link>
          <div className="flex items-center gap-2.5">
            <h1 className="text-base font-semibold" style={{ color: '#e6edf3' }}>
              {experiment.target_service}
            </h1>
            <StateBadge state={experiment.state} />
          </div>
          <p className="font-mono text-[10px]" style={{ color: '#6e7681' }}>{experiment.id}</p>
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-2">
          {experiment.state === 'CREATED' && (
            <button
              onClick={handleStart}
              disabled={isActing}
              className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold disabled:opacity-50"
              style={{ backgroundColor: '#238636', color: '#e6edf3' }}
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              Start Traffic Injection
            </button>
          )}

          {experiment.state === 'RUNNING' && (
            <button
              onClick={handleStop}
              disabled={isActing}
              className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold disabled:opacity-50"
              style={{ backgroundColor: 'rgba(248,81,73,0.15)', color: '#f85149', border: '1px solid rgba(248,81,73,0.4)' }}
            >
              <Square className="h-3.5 w-3.5 fill-current" />
              Emergency Stop
            </button>
          )}

          {experiment.state === 'COMPLETED' && (
            <Link
              to="/reports"
              className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold"
              style={{ backgroundColor: 'rgba(88,166,255,0.12)', color: '#58a6ff', border: '1px solid rgba(88,166,255,0.3)' }}
            >
              <FileSpreadsheet className="h-3.5 w-3.5" />
              View Analysis Report
            </Link>
          )}
        </div>
      </div>

      {actionError && (
        <div
          className="rounded p-3 text-xs font-semibold"
          style={{ backgroundColor: 'rgba(248,81,73,0.1)', border: '1px solid rgba(248,81,73,0.3)', color: '#f85149' }}
        >
          {actionError}
        </div>
      )}

      {/* Real-Time Telemetry */}
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
            Real-Time Telemetry Stream
          </span>
          {isRunning && (
            <span className="inline-flex items-center gap-1.5 text-[11px] font-semibold" style={{ color: '#3fb950' }}>
              <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full opacity-75" style={{ backgroundColor: '#3fb950' }}></span>
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full" style={{ backgroundColor: '#3fb950' }}></span>
              </span>
              Live · 1s polling
            </span>
          )}
        </div>

        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {/* Throughput */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>Throughput (RPS)</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#e6edf3' }}>
              {snapshot?.request_rate ? `${snapshot.request_rate.toFixed(1)}` : '0.0'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>req/s</div>
          </div>

          {/* P95 Latency */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>P95 Latency</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#bc8cff' }}>
              {snapshot?.p95_latency_ms ? `${snapshot.p95_latency_ms.toFixed(1)}` : '0.0'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
              ms · SLA &lt;{experiment.config.max_p95_latency_ms}ms
            </div>
          </div>

          {/* Error Rate */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>Error Rate</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#f85149' }}>
              {snapshot?.error_rate !== undefined ? `${(snapshot.error_rate * 100).toFixed(2)}` : '0.00'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
              % · max {experiment.config.max_error_rate_percent}%
            </div>
          </div>

          {/* Availability */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>Availability</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#3fb950' }}>
              {snapshot?.availability !== undefined ? `${(snapshot.availability * 100).toFixed(2)}` : '100.00'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
              % · min {experiment.config.min_availability_pct}%
            </div>
          </div>
        </div>
      </div>

      {/* Config detail grids */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Traffic Plan */}
        <div className="rounded p-4 space-y-3" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
            Traffic Plan Specifications
          </div>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
            <div>
              <dt style={{ color: '#8b949e' }}>Target Endpoint</dt>
              <dd className="mt-0.5 font-mono font-semibold truncate" style={{ color: '#e6edf3' }}>
                {experiment.target_url}
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Injection Pattern</dt>
              <dd className="mt-0.5 font-mono font-semibold uppercase" style={{ color: '#e6edf3' }}>
                {experiment.type}
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Initial RPS</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.initial_rate} req/s
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Max Ceiling</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.max_rate} req/s
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Step Rate Increment</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                +{experiment.config.step_rate} req/s
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Step Duration</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.step_duration_seconds}s
              </dd>
            </div>
          </dl>
        </div>

        {/* Safety Brakes */}
        <div className="rounded p-4 space-y-3" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
            Safety Brakes &amp; Recovery Window
          </div>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
            <div>
              <dt style={{ color: '#8b949e' }}>Max P95 Latency SLA</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.max_p95_latency_ms} ms
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Max Error Rate SLA</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.max_error_rate_percent}%
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Minimum Availability</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.min_availability_pct}%
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Recovery Wait</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.recovery_window_sec}s
              </dd>
            </div>
          </dl>

          {experiment.stop_reason && (
            <div
              className="rounded p-3 text-[11px]"
              style={{ backgroundColor: 'rgba(210,153,34,0.1)', border: '1px solid rgba(210,153,34,0.3)', color: '#d29922' }}
            >
              <span className="font-bold">Interruption Reason: </span>
              {experiment.stop_reason}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
