import React, { useState, useMemo } from 'react';
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
import {
  FileSpreadsheet,
  CheckCircle2,
  AlertOctagon,
  Download,
  Gauge,
  TrendingDown,
  Timer,
  ShieldCheck,
  Table as TableIcon,
  LineChart as ChartIcon,
} from 'lucide-react';
import type { ExperimentReport, CapacityObservation } from '../types';

// ---------------------------------------------------------------------------
// Type Definitions
// ---------------------------------------------------------------------------

export interface TransformedObservationPoint {
  index: number;
  timestamp: string;
  timeLabel: string;
  loadRate: number;
  p95LatencyMs: number;
  errorRatePercent: number;
  availabilityPercent: number;
  cpuUtilization?: number;
  memoryUtilization?: number;
  isDegraded?: boolean;
  isBreached?: boolean;
}

export interface ReportViewProps {
  /** Capacity evaluation and recovery report data */
  report: ExperimentReport;
  className?: string;
}

// ---------------------------------------------------------------------------
// Custom Dark Tooltip
// ---------------------------------------------------------------------------

interface CapacityTooltipEntry {
  name: string;
  value: number;
  color: string;
  dataKey: string;
}

interface CapacityTooltipProps {
  active?: boolean;
  payload?: CapacityTooltipEntry[];
  label?: string | number;
}

const CapacityChartTooltip: React.FC<CapacityTooltipProps> = ({ active, payload, label }) => {
  if (!active || !payload || !payload.length) return null;

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
        Load Rate: <span className="font-semibold text-white">{label} req/s</span>
      </div>
      <div className="space-y-1 pt-0.5">
        {payload.map((entry, index) => {
          const isError = entry.dataKey === 'errorRatePercent';
          return (
            <div key={`cap-tooltip-${index}`} className="flex items-center justify-between gap-4 font-mono text-[11px]">
              <span className="flex items-center gap-1.5" style={{ color: entry.color }}>
                <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: entry.color }} />
                <span>{entry.name}:</span>
              </span>
              <span className="font-bold" style={{ color: '#e6edf3' }}>
                {typeof entry.value === 'number' ? entry.value.toFixed(2) : entry.value}
                {isError ? '%' : ' ms'}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
};

// ===========================================================================
// CORE LOGIC 1: Capacity Observations & Degradation Knee Data Mapping
// ===========================================================================

/**
 * Transforms raw capacity observations into chart-ready data points and annotates
 * performance knees and safety boundary breaches.
 */
function transformObservations(
  observations: CapacityObservation[] = [],
  report: ExperimentReport
): TransformedObservationPoint[] {
  // Sort observations by load rate ascending for proper capacity curve progression
  const sorted = [...observations].sort((a, b) => a.load_rate - b.load_rate);

  return sorted.map((obs, idx) => {
    const isDegraded = report.degradation_rate > 0 && obs.load_rate >= report.degradation_rate;
    const isBreached = report.safety_boundary_rate > 0 && obs.load_rate >= report.safety_boundary_rate;

    const rawError = obs.error_rate ?? 0;
    const errorRatePercent = Math.min(100, Math.max(0, rawError > 1 ? rawError : rawError * 100));

    const rawAvailability = obs.availability ?? 1;
    const availabilityPercent = Math.min(100, Math.max(0, rawAvailability > 1 ? rawAvailability : rawAvailability * 100));

    const date = obs.timestamp ? new Date(obs.timestamp) : new Date();
    const timeLabel = isNaN(date.getTime())
      ? `Step ${idx + 1}`
      : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });

    return {
      index: idx + 1,
      timestamp: obs.timestamp,
      timeLabel,
      loadRate: Math.max(0, obs.load_rate || 0),
      p95LatencyMs: Math.max(0, obs.p95_latency_ms || 0),
      errorRatePercent,
      availabilityPercent,
      cpuUtilization: obs.cpu_utilization,
      memoryUtilization: obs.memory_utilization,
      isDegraded,
      isBreached,
    };
  });
}

// ===========================================================================
// CORE LOGIC 2: Capacity Curve & Degradation Knee Chart Rendering
// ===========================================================================

export interface CapacityChartProps {
  data: TransformedObservationPoint[];
  report: ExperimentReport;
}

export const CapacityChart: React.FC<CapacityChartProps> = ({ data, report }) => {
  const maxLatency = useMemo(() => {
    let max = 10;
    data.forEach((d) => {
      if (d.p95LatencyMs > max) max = d.p95LatencyMs;
    });
    return Math.ceil(max * 1.2);
  }, [data]);

  return (
    <div className="w-full h-[320px] min-w-0">
      <ResponsiveContainer width="100%" height="100%" minWidth={0}>
        <LineChart data={data} margin={{ top: 16, right: 24, left: -6, bottom: 4 }}>
          <CartesianGrid stroke="#30363d" strokeDasharray="3 3" opacity={0.6} />
          <XAxis
            dataKey="loadRate"
            stroke="#6e7681"
            tick={{ fontSize: 10, fill: '#8b949e' }}
            tickLine={{ stroke: '#30363d' }}
            unit=" req/s"
          />
          <YAxis
            yAxisId="left"
            stroke="#6e7681"
            tick={{ fontSize: 10, fill: '#8b949e' }}
            tickLine={{ stroke: '#30363d' }}
            domain={[0, maxLatency]}
            unit=" ms"
          />
          <YAxis
            yAxisId="right"
            orientation="right"
            stroke="#6e7681"
            tick={{ fontSize: 10, fill: '#8b949e' }}
            tickLine={{ stroke: '#30363d' }}
            domain={[0, 100]}
            unit="%"
          />
          <Tooltip content={<CapacityChartTooltip />} />
          <Legend wrapperStyle={{ fontSize: '11px', paddingTop: '8px' }} iconType="circle" iconSize={8} />

          {/* Milestone Reference Lines */}
          {report.maximum_sustainable_rate > 0 && (
            <ReferenceLine
              yAxisId="left"
              x={report.maximum_sustainable_rate}
              label={{
                value: `Ceiling (${report.maximum_sustainable_rate} RPS)`,
                fill: '#3fb950',
                fontSize: 10,
                position: 'insideTopLeft',
              }}
              stroke="#3fb950"
              strokeDasharray="4 4"
              strokeWidth={1.5}
            />
          )}
          {report.degradation_rate > 0 && (
            <ReferenceLine
              yAxisId="left"
              x={report.degradation_rate}
              label={{
                value: `Knee (${report.degradation_rate} RPS)`,
                fill: '#d29922',
                fontSize: 10,
                position: 'insideTopLeft',
              }}
              stroke="#d29922"
              strokeDasharray="4 4"
              strokeWidth={1.5}
            />
          )}
          {report.safety_boundary_rate > 0 && (
            <ReferenceLine
              yAxisId="left"
              x={report.safety_boundary_rate}
              label={{
                value: `Safety Boundary (${report.safety_boundary_rate} RPS)`,
                fill: '#f85149',
                fontSize: 10,
                position: 'insideTopLeft',
              }}
              stroke="#f85149"
              strokeDasharray="4 4"
              strokeWidth={1.5}
            />
          )}

          {/* Observation Metric Lines */}
          <Line
            yAxisId="left"
            type="monotone"
            dataKey="p95LatencyMs"
            name="P95 Latency (ms)"
            stroke="#bc8cff"
            strokeWidth={2}
            dot={{ stroke: '#bc8cff', strokeWidth: 2, r: 3 }}
          />
          <Line
            yAxisId="right"
            type="monotone"
            dataKey="errorRatePercent"
            name="Error Rate (%)"
            stroke="#f85149"
            strokeWidth={1.5}
            dot={{ stroke: '#f85149', strokeWidth: 1.5, r: 2.5 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export const ReportView: React.FC<ReportViewProps> = ({ report, className = '' }) => {
  const [activeTab, setActiveTab] = useState<'chart' | 'table'>('chart');

  const transformedPoints = useMemo(
    () => transformObservations(report.observations || [], report),
    [report]
  );

  const handleDownloadJSON = () => {
    const dataStr = 'data:text/json;charset=utf-8,' + encodeURIComponent(JSON.stringify(report, null, 2));
    const downloadAnchor = document.createElement('a');
    downloadAnchor.setAttribute('href', dataStr);
    downloadAnchor.setAttribute('download', `capacity-report-${report.experiment_id || 'run'}.json`);
    document.body.appendChild(downloadAnchor);
    downloadAnchor.click();
    downloadAnchor.remove();
  };

  return (
    <div className={`space-y-5 ${className}`}>
      {/* Header Info Banner */}
      <div
        className="flex flex-col gap-3 rounded p-4 sm:flex-row sm:items-center sm:justify-between"
        style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}
      >
        <div>
          <div className="flex items-center gap-2">
            <FileSpreadsheet className="h-4 w-4" style={{ color: '#58a6ff' }} />
            <h2 className="text-sm font-semibold" style={{ color: '#e6edf3' }}>
              Capacity &amp; Degradation Evaluation
            </h2>
            <span
              className="rounded px-2 py-0.5 font-mono text-[10px] font-semibold"
              style={{ backgroundColor: 'rgba(88,166,255,0.12)', color: '#58a6ff' }}
            >
              Target: {report.target_service}
            </span>
          </div>
          <p className="mt-1 font-mono text-[11px]" style={{ color: '#6e7681' }}>
            Experiment ID: {report.experiment_id} · Generated at {new Date(report.generated_at).toLocaleString()}
          </p>
        </div>

        <button
          onClick={handleDownloadJSON}
          className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold self-start sm:self-auto transition-colors"
          style={{ backgroundColor: '#21262d', border: '1px solid #30363d', color: '#c9d1d9' }}
        >
          <Download className="h-3.5 w-3.5" />
          Export JSON Report
        </button>
      </div>

      {/* Summary KPI Cards */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {/* Sustainable Ceiling */}
        <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="flex items-center justify-between">
            <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
              Sustainable Ceiling
            </span>
            <ShieldCheck className="h-3.5 w-3.5" style={{ color: '#3fb950' }} />
          </div>
          <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#3fb950' }}>
            {report.maximum_sustainable_rate}
          </div>
          <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
            RPS · zero SLA violations
          </div>
        </div>

        {/* Knee Degradation */}
        <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="flex items-center justify-between">
            <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
              Knee Degradation
            </span>
            <TrendingDown className="h-3.5 w-3.5" style={{ color: '#d29922' }} />
          </div>
          <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#d29922' }}>
            {report.degradation_rate}
          </div>
          <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
            RPS · latency slope &gt;50%
          </div>
        </div>

        {/* Safety Boundary */}
        <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="flex items-center justify-between">
            <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
              Safety Boundary
            </span>
            <AlertOctagon className="h-3.5 w-3.5" style={{ color: '#f85149' }} />
          </div>
          <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#f85149' }}>
            {report.safety_boundary_rate}
          </div>
          <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
            RPS · hard boundary breach
          </div>
        </div>

        {/* Recovery Time */}
        <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="flex items-center justify-between">
            <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
              Recovery Time
            </span>
            <Timer className="h-3.5 w-3.5" style={{ color: '#58a6ff' }} />
          </div>
          <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#58a6ff' }}>
            {report.recovery_time_sec}s
          </div>
          <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
            return to baseline SLA
          </div>
        </div>
      </div>

      {/* Observations View: Chart vs Table Toggle */}
      <div
        className="rounded p-4 space-y-4"
        style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Gauge className="h-4 w-4" style={{ color: '#bc8cff' }} />
            <h3 className="text-xs font-semibold uppercase tracking-wider" style={{ color: '#e6edf3' }}>
              Capacity Observations &amp; Degradation Knee Curve
            </h3>
          </div>

          {/* Toggle buttons */}
          <div className="flex rounded p-0.5" style={{ backgroundColor: '#21262d', border: '1px solid #30363d' }}>
            <button
              onClick={() => setActiveTab('chart')}
              className={`flex items-center gap-1 rounded px-2.5 py-1 text-[11px] font-semibold transition-colors ${
                activeTab === 'chart'
                  ? 'bg-[#30363d] text-[#e6edf3]'
                  : 'text-[#8b949e] hover:text-[#c9d1d9]'
              }`}
            >
              <ChartIcon className="h-3 w-3" />
              Curve Chart
            </button>
            <button
              onClick={() => setActiveTab('table')}
              className={`flex items-center gap-1 rounded px-2.5 py-1 text-[11px] font-semibold transition-colors ${
                activeTab === 'table'
                  ? 'bg-[#30363d] text-[#e6edf3]'
                  : 'text-[#8b949e] hover:text-[#c9d1d9]'
              }`}
            >
              <TableIcon className="h-3 w-3" />
              Step Table ({transformedPoints.length})
            </button>
          </div>
        </div>

        {/* Content Tab: Chart or Table */}
        {transformedPoints.length === 0 ? (
          <div
            className="flex h-48 items-center justify-center text-xs"
            style={{ color: '#6e7681' }}
          >
            No observations recorded for this experiment run.
          </div>
        ) : activeTab === 'chart' ? (
          <div className="w-full">
            <CapacityChart data={transformedPoints} report={report} />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead>
                <tr style={{ borderBottom: '1px solid #30363d', color: '#6e7681' }}>
                  <th className="pb-2 font-semibold uppercase tracking-wider">Step</th>
                  <th className="pb-2 font-semibold uppercase tracking-wider">Load Rate</th>
                  <th className="pb-2 font-semibold uppercase tracking-wider">P95 Latency</th>
                  <th className="pb-2 font-semibold uppercase tracking-wider">Error Rate</th>
                  <th className="pb-2 font-semibold uppercase tracking-wider">Availability</th>
                  <th className="pb-2 font-semibold uppercase tracking-wider">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#30363d] font-mono text-[11px]">
                {transformedPoints.map((pt) => (
                  <tr key={pt.index} className="hover:bg-[#21262d]/50">
                    <td className="py-2.5" style={{ color: '#8b949e' }}>#{pt.index}</td>
                    <td className="py-2.5 font-semibold" style={{ color: '#e6edf3' }}>{pt.loadRate} req/s</td>
                    <td className="py-2.5" style={{ color: '#bc8cff' }}>{pt.p95LatencyMs.toFixed(1)} ms</td>
                    <td className="py-2.5" style={{ color: pt.errorRatePercent > 0 ? '#f85149' : '#3fb950' }}>
                      {pt.errorRatePercent.toFixed(2)}%
                    </td>
                    <td className="py-2.5" style={{ color: '#3fb950' }}>{pt.availabilityPercent.toFixed(2)}%</td>
                    <td className="py-2.5">
                      {pt.isBreached ? (
                        <span className="rounded px-1.5 py-0.5 text-[9px] font-bold" style={{ backgroundColor: 'rgba(248,81,73,0.15)', color: '#f85149' }}>
                          BREACH
                        </span>
                      ) : pt.isDegraded ? (
                        <span className="rounded px-1.5 py-0.5 text-[9px] font-bold" style={{ backgroundColor: 'rgba(210,153,34,0.15)', color: '#d29922' }}>
                          KNEE
                        </span>
                      ) : (
                        <span className="rounded px-1.5 py-0.5 text-[9px] font-bold" style={{ backgroundColor: 'rgba(63,185,80,0.12)', color: '#3fb950' }}>
                          NORMAL
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Automated Recommendations Section */}
      <div
        className="rounded p-4 space-y-3"
        style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}
      >
        <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
          Automated Engineering Recommendations
        </div>
        {report.recommendations && report.recommendations.length > 0 ? (
          <ul className="space-y-2">
            {report.recommendations.map((rec, i) => (
              <li key={i} className="flex items-start gap-2 text-xs">
                <CheckCircle2 className="h-3.5 w-3.5 shrink-0 mt-0.5" style={{ color: '#3fb950' }} />
                <span style={{ color: '#8b949e' }}>{rec}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-[11px]" style={{ color: '#6e7681' }}>
            System operated within normal operational thresholds. No auto-remediations required.
          </p>
        )}
      </div>
    </div>
  );
};

export default ReportView;
