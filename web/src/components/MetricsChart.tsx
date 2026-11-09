import React, { useState, useEffect, useRef, useMemo } from 'react';
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ReferenceLine,
} from 'recharts';
import { Activity, Clock, ShieldAlert } from 'lucide-react';
import type { MetricSnapshot, ExperimentConfig } from '../types';

// ---------------------------------------------------------------------------
// Type Definitions
// ---------------------------------------------------------------------------

export interface ProcessedMetricPoint {
  timestamp: string;
  timeLabel: string;
  elapsedSec: number;
  requestRate: number;
  p50LatencyMs: number;
  p95LatencyMs: number;
  p99LatencyMs: number;
  errorRatePercent: number;
  availabilityPercent: number;
}

export type MetricFilterMode = 'all' | 'latency' | 'errors';

export interface MetricsChartProps {
  /** Current polled telemetry snapshot from the backend (1s interval during RUNNING) */
  snapshot?: MetricSnapshot | null;
  /** Optional pre-loaded or external historical snapshots */
  history?: MetricSnapshot[];
  /** Whether the experiment is actively running */
  isRunning?: boolean;
  telemetryUnavailable?: boolean;
  /** Experiment configuration containing SLA thresholds and rate limits */
  config?: ExperimentConfig;
  /** Maximum number of data points to retain in the live sliding window buffer (default: 60) */
  maxBufferPoints?: number;
  className?: string;
}

// ---------------------------------------------------------------------------
// Scaffolding: Custom Dark Tooltip
// ---------------------------------------------------------------------------

interface TooltipPayloadEntry {
  name: string;
  value: number;
  color: string;
  dataKey: string;
  unit?: string;
}

interface CustomTooltipProps {
  active?: boolean;
  payload?: TooltipPayloadEntry[];
  label?: string;
}

const CustomChartTooltip: React.FC<CustomTooltipProps> = ({ active, payload, label }) => {
  if (!active || !payload || !payload.length) return null;

  const unitMap: Record<string, string> = {
    requestRate: 'req/s',
    p95LatencyMs: 'ms',
    p50LatencyMs: 'ms',
    p99LatencyMs: 'ms',
    errorRatePercent: '%',
    availabilityPercent: '%',
  };

  return (
    <div
      className="rounded p-3 text-xs shadow-xl space-y-1.5 backdrop-blur-md"
      style={{
        backgroundColor: 'rgba(22, 27, 34, 0.95)',
        border: '1px solid #30363d',
        color: '#e6edf3',
        minWidth: '180px',
      }}
    >
      <div className="font-mono text-[10px] pb-1 border-b border-[#30363d]" style={{ color: '#8b949e' }}>
        Timestamp: <span className="font-semibold text-white">{label}</span>
      </div>
      <div className="space-y-1 pt-0.5">
        {payload.map((entry, index) => {
          const unit = unitMap[entry.dataKey] || '';
          return (
            <div key={`tooltip-${index}`} className="flex items-center justify-between gap-4 font-mono text-[11px]">
              <span className="flex items-center gap-1.5" style={{ color: entry.color }}>
                <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: entry.color }} />
                <span>{entry.name}:</span>
              </span>
              <span className="font-bold" style={{ color: '#e6edf3' }}>
                {typeof entry.value === 'number' ? entry.value.toFixed(2) : entry.value}
                {unit ? ` ${unit}` : ''}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
};

// ===========================================================================
// CORE LOGIC 1: Telemetry Data Buffering & Normalization
// ===========================================================================

/**
 * Transforms incoming raw MetricSnapshots into sanitized, formatted chart points,
 * maintaining a sliding window of historical points.
 */
function processTelemetryStream(
  buffer: ProcessedMetricPoint[],
  snapshot: MetricSnapshot | null | undefined,
  maxBufferPoints = 60
): ProcessedMetricPoint[] {
  if (!snapshot) {
    return buffer;
  }

  // Validate and parse timestamp
  const date = snapshot.timestamp ? new Date(snapshot.timestamp) : new Date();
  const isValidDate = !isNaN(date.getTime());
  const timeLabel = isValidDate
    ? date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : `+${buffer.length}s`;

  // Prevent duplicate consecutive entries with identical timestamps or snapshot state
  const lastEntry = buffer[buffer.length - 1];
  if (
    lastEntry &&
    lastEntry.timestamp === snapshot.timestamp &&
    lastEntry.requestRate === snapshot.request_rate &&
    lastEntry.p95LatencyMs === snapshot.p95_latency_ms
  ) {
    return buffer;
  }

  // Normalize rates and convert error ratio (0.0 - 1.0) to percentage (0 - 100%)
  const rawError = snapshot.error_rate ?? 0;
  const errorRatePercent = Math.min(100, Math.max(0, rawError > 1 ? rawError : rawError * 100));

  const rawAvailability = snapshot.availability ?? 1;
  const availabilityPercent = Math.min(100, Math.max(0, rawAvailability > 1 ? rawAvailability : rawAvailability * 100));

  const newPoint: ProcessedMetricPoint = {
    timestamp: snapshot.timestamp || new Date().toISOString(),
    timeLabel,
    elapsedSec: buffer.length + 1,
    requestRate: Math.max(0, snapshot.request_rate || 0),
    p50LatencyMs: Math.max(0, snapshot.p50_latency_ms || 0),
    p95LatencyMs: Math.max(0, snapshot.p95_latency_ms || 0),
    p99LatencyMs: Math.max(0, snapshot.p99_latency_ms || 0),
    errorRatePercent,
    availabilityPercent,
  };

  const updated = [...buffer, newPoint];
  return updated.length > maxBufferPoints ? updated.slice(updated.length - maxBufferPoints) : updated;
}

// ===========================================================================
// CORE LOGIC 2: Real-Time Streaming Chart Rendering
// ===========================================================================

export interface StreamingChartProps {
  data: ProcessedMetricPoint[];
  config?: ExperimentConfig;
  filterMode?: MetricFilterMode;
}

export const StreamingChart: React.FC<StreamingChartProps> = ({
  data,
  config,
  filterMode = 'all',
}) => {
  const maxP95SLA = config?.max_p95_latency_ms;
  const maxErrorSLA = config?.max_error_rate_percent;

  // Determine dynamic bounds for left Y-axis
  const maxLeftValue = useMemo(() => {
    let maxVal = 10;
    data.forEach((d) => {
      if (filterMode !== 'errors') {
        if (d.requestRate > maxVal) maxVal = d.requestRate;
        if (d.p95LatencyMs > maxVal) maxVal = d.p95LatencyMs;
      }
    });
    if (maxP95SLA && maxP95SLA > maxVal) maxVal = maxP95SLA;
    return Math.ceil(maxVal * 1.15);
  }, [data, maxP95SLA, filterMode]);

  const showThroughput = filterMode === 'all' || filterMode === 'latency';
  const showLatency = filterMode === 'all' || filterMode === 'latency';
  const showError = filterMode === 'all' || filterMode === 'errors';

  return (
    <div className="w-full h-[320px] min-w-0">
      <ResponsiveContainer width="100%" height="100%" minWidth={0}>
        <LineChart data={data} margin={{ top: 12, right: 24, left: -6, bottom: 4 }}>
          <CartesianGrid stroke="#30363d" strokeDasharray="3 3" opacity={0.6} />
          <XAxis
            dataKey="timeLabel"
            stroke="#6e7681"
            tick={{ fontSize: 10, fill: '#8b949e' }}
            tickLine={{ stroke: '#30363d' }}
          />

          {/* Left Y-Axis: Load Rate (RPS) & P95 Latency (ms) */}
          <YAxis
            yAxisId="left"
            stroke="#6e7681"
            tick={{ fontSize: 10, fill: '#8b949e' }}
            tickLine={{ stroke: '#30363d' }}
            domain={[0, maxLeftValue]}
          />

          {/* Right Y-Axis: Error Rate Percentage (0 - 100%) */}
          <YAxis
            yAxisId="right"
            orientation="right"
            stroke="#6e7681"
            tick={{ fontSize: 10, fill: '#8b949e' }}
            tickLine={{ stroke: '#30363d' }}
            domain={[0, 100]}
            unit="%"
          />

          <Tooltip content={<CustomChartTooltip />} />
          <Legend
            wrapperStyle={{ fontSize: '11px', paddingTop: '8px' }}
            iconType="circle"
            iconSize={8}
          />

          {/* SLA Threshold Reference Lines */}
          {showLatency && maxP95SLA !== undefined && maxP95SLA > 0 && (
            <ReferenceLine
              yAxisId="left"
              y={maxP95SLA}
              label={{
                value: `SLA Latency (${maxP95SLA}ms)`,
                fill: '#f85149',
                fontSize: 10,
                position: 'insideTopRight',
              }}
              stroke="#f85149"
              strokeDasharray="4 4"
              strokeWidth={1.5}
            />
          )}

          {showError && maxErrorSLA !== undefined && maxErrorSLA > 0 && (
            <ReferenceLine
              yAxisId="right"
              y={maxErrorSLA}
              label={{
                value: `SLA Error (${maxErrorSLA}%)`,
                fill: '#f85149',
                fontSize: 10,
                position: 'insideBottomRight',
              }}
              stroke="#f85149"
              strokeDasharray="4 4"
              strokeWidth={1.5}
            />
          )}

          {/* Telemetry Metric Lines */}
          {showThroughput && (
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="requestRate"
              name="Load Rate (req/s)"
              stroke="#3fb950"
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          )}

          {showLatency && (
            <Line
              yAxisId="left"
              type="monotone"
              dataKey="p95LatencyMs"
              name="P95 Latency (ms)"
              stroke="#bc8cff"
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          )}

          {showError && (
            <Line
              yAxisId="right"
              type="monotone"
              dataKey="errorRatePercent"
              name="Error Rate (%)"
              stroke="#f85149"
              strokeWidth={1.5}
              dot={false}
              isAnimationActive={false}
            />
          )}
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export const MetricsChart: React.FC<MetricsChartProps> = ({
  snapshot,
  history,
  isRunning = false,
  telemetryUnavailable = false,
  config,
  maxBufferPoints = 60,
  className = '',
}) => {
  const [filterMode, setFilterMode] = useState<MetricFilterMode>('all');
  const [dataPoints, setDataPoints] = useState<ProcessedMetricPoint[]>(() => {
    if (history && history.length > 0) {
      return history.reduce<ProcessedMetricPoint[]>(
        (acc, snap) => processTelemetryStream(acc, snap, maxBufferPoints),
        []
      );
    }
    return [];
  });

  const prevSnapshotRef = useRef<MetricSnapshot | null | undefined>(null);

  // Ingest incoming live snapshot updates
  useEffect(() => {
    if (snapshot && snapshot !== prevSnapshotRef.current) {
      prevSnapshotRef.current = snapshot;
      setDataPoints((prev) => processTelemetryStream(prev, snapshot, maxBufferPoints));
    }
  }, [snapshot, maxBufferPoints]);

  const latest = dataPoints[dataPoints.length - 1];

  return (
    <div
      className={`rounded p-4 space-y-4 ${className}`}
      style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}
    >
      {/* Chart Header & Live Indicator */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2">
          <Activity className="h-4 w-4" style={{ color: '#58a6ff' }} />
          <h2 className="text-xs font-semibold uppercase tracking-wider" style={{ color: '#e6edf3' }}>
            Live Telemetry Metrics (Load vs P95 &amp; Errors)
          </h2>
          {telemetryUnavailable ? (
            <span className="text-xs text-amber-400">Telemetry unavailable</span>
          ) : isRunning ? (
            <span
              className="inline-flex items-center gap-1.5 rounded px-2 py-0.5 font-mono text-[10px] font-semibold"
              style={{ backgroundColor: 'rgba(63,185,80,0.12)', color: '#3fb950' }}
            >
              <span className="relative flex h-1.5 w-1.5">
                <span
                  className="absolute inline-flex h-full w-full animate-ping rounded-full opacity-75"
                  style={{ backgroundColor: '#3fb950' }}
                />
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full" style={{ backgroundColor: '#3fb950' }} />
              </span>
              Streaming
            </span>
          ) : (
            <span
              className="inline-flex items-center gap-1 rounded px-2 py-0.5 font-mono text-[10px] font-semibold"
              style={{ backgroundColor: '#21262d', color: '#8b949e' }}
            >
              <Clock className="h-3 w-3" />
              Idle / Historical
            </span>
          )}
        </div>

        {/* View Filter Toggles & Live Pills */}
        <div className="flex flex-wrap items-center gap-3">
          {latest && (
            <div className="flex items-center gap-2.5 font-mono text-[11px]">
              <span style={{ color: '#8b949e' }}>
                RPS: <strong style={{ color: '#3fb950' }}>{latest.requestRate.toFixed(1)}</strong>
              </span>
              <span style={{ color: '#8b949e' }}>
                P95: <strong style={{ color: '#bc8cff' }}>{latest.p95LatencyMs.toFixed(1)}ms</strong>
              </span>
              <span style={{ color: '#8b949e' }}>
                Err:{' '}
                <strong style={{ color: latest.errorRatePercent > 0 ? '#f85149' : '#3fb950' }}>
                  {latest.errorRatePercent.toFixed(2)}%
                </strong>
              </span>
            </div>
          )}

          <div className="flex rounded p-0.5" style={{ backgroundColor: '#21262d', border: '1px solid #30363d' }}>
            <button
              onClick={() => setFilterMode('all')}
              className={`rounded px-2 py-0.5 text-[10px] font-semibold transition-colors ${
                filterMode === 'all' ? 'bg-[#30363d] text-[#e6edf3]' : 'text-[#8b949e] hover:text-[#c9d1d9]'
              }`}
            >
              All
            </button>
            <button
              onClick={() => setFilterMode('latency')}
              className={`rounded px-2 py-0.5 text-[10px] font-semibold transition-colors ${
                filterMode === 'latency' ? 'bg-[#30363d] text-[#e6edf3]' : 'text-[#8b949e] hover:text-[#c9d1d9]'
              }`}
            >
              Rate / P95
            </button>
            <button
              onClick={() => setFilterMode('errors')}
              className={`rounded px-2 py-0.5 text-[10px] font-semibold transition-colors ${
                filterMode === 'errors' ? 'bg-[#30363d] text-[#e6edf3]' : 'text-[#8b949e] hover:text-[#c9d1d9]'
              }`}
            >
              Errors
            </button>
          </div>
        </div>
      </div>

      {/* Chart Canvas or Empty State */}
      {dataPoints.length === 0 ? (
        <div
          className="flex h-64 flex-col items-center justify-center gap-2 rounded text-center text-xs"
          style={{ backgroundColor: '#0d1117', border: '1px dashed #30363d', color: '#6e7681' }}
        >
          <ShieldAlert className="h-6 w-6" style={{ color: '#30363d' }} />
          <span>No telemetry data points received yet.</span>
          <span className="text-[11px]">{isRunning ? 'Waiting for service metrics. Check trace export and traffic reaching the selected service.' : 'Start the experiment to begin streaming live metrics.'}</span>
        </div>
      ) : (
        <div className="w-full">
          <StreamingChart data={dataPoints} config={config} filterMode={filterMode} />
        </div>
      )}
    </div>
  );
};

export default MetricsChart;
