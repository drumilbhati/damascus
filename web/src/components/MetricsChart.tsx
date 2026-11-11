import React, { useEffect, useState } from 'react';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ReferenceLine } from 'recharts';
import type { MetricSnapshot, ExperimentConfig } from '../types';

export function validSnapshot(s: MetricSnapshot) {
  return Number.isFinite(Date.parse(s.timestamp)) && [s.request_rate, s.p95_latency_ms, s.error_rate, s.availability].every(Number.isFinite);
}
export const TrafficTooltip = ({ active, payload }: any) => {
  const p = payload?.[0]?.payload;
  if (!active || !p) return null;
  return <div className="rounded border border-slate-600 bg-slate-900 p-3 text-xs text-slate-100">
    <p>{new Date(p.timestamp).toLocaleString()}</p>
    <p>Measured service traffic: {p.request_rate?.toFixed(2) ?? 'Unavailable'} req/s</p>
    <p>Offered load target: {p.offered_rate == null ? 'Unavailable' : `${p.offered_rate.toFixed(2)} req/s`}</p>
    <p>P95: {p.p95_latency_ms?.toFixed(2)} ms · Errors: {p.errorPercent?.toFixed(2)}%</p>
    <p>Availability: {p.availabilityPercent?.toFixed(2)}%</p>
  </div>;
};
export function TelemetryCharts({ history, config }: { history: MetricSnapshot[]; config?: ExperimentConfig }) {
  const data = history.filter(validSnapshot).map(s => ({ ...s, time: Date.parse(s.timestamp), errorPercent: s.error_rate * 100, availabilityPercent: s.availability * 100 }));
  if (!data.length) return <p className="p-8 text-sm text-slate-400">No valid telemetry observations available.</p>;
  const axis = <XAxis dataKey="time" type="number" domain={['dataMin', 'dataMax']} tickFormatter={v => new Date(v).toLocaleTimeString()} tick={{ fontSize: 10 }} />;
  return <div className="space-y-4">
    <p className="text-xs text-slate-400">Measured traffic is the selected service’s total, including background traffic, over a trailing 30-second window. Offered load is a target, not a count of requests sent.</p>
    <div className="h-64"><ResponsiveContainer width="100%" height="100%"><LineChart data={data} margin={{ right: 20 }}>
      <CartesianGrid stroke="#30363d" strokeDasharray="3 3" />{axis}<YAxis unit=" RPS" tick={{ fontSize: 10 }} />
      <Tooltip content={<TrafficTooltip />} /><Legend wrapperStyle={{ fontSize: 11 }} />
      <Line dataKey="request_rate" name="Measured service traffic (RPS)" stroke="#3fb950" dot={{ r: 2 }} isAnimationActive={false} />
      <Line dataKey="offered_rate" type="stepAfter" name="Offered load target (RPS)" stroke="#58a6ff" strokeDasharray="4 4" dot={{ r: 2 }} isAnimationActive={false} connectNulls={false} />
    </LineChart></ResponsiveContainer></div>
    <div className="h-64"><ResponsiveContainer width="100%" height="100%"><LineChart data={data} margin={{ right: 20 }}>
      <CartesianGrid stroke="#30363d" strokeDasharray="3 3" />{axis}<YAxis yAxisId="latency" unit=" ms" tick={{ fontSize: 10 }} /><YAxis yAxisId="percent" orientation="right" unit="%" domain={[0, 100]} tick={{ fontSize: 10 }} />
      <Tooltip content={<TrafficTooltip />} /><Legend wrapperStyle={{ fontSize: 11 }} />
      <Line yAxisId="latency" dataKey="p95_latency_ms" name="P95 latency (ms)" stroke="#bc8cff" dot={{ r: 2 }} isAnimationActive={false} />
      <Line yAxisId="percent" dataKey="errorPercent" name="Error rate (%)" stroke="#f85149" dot={{ r: 2 }} isAnimationActive={false} />
      <Line yAxisId="percent" dataKey="availabilityPercent" name="Availability (%)" stroke="#d29922" dot={false} isAnimationActive={false} />
      {config && <ReferenceLine yAxisId="latency" y={config.max_p95_latency_ms} stroke="#bc8cff" strokeDasharray="4 4" />}
      {config && <ReferenceLine yAxisId="percent" y={config.max_error_rate_percent} stroke="#f85149" strokeDasharray="4 4" />}
    </LineChart></ResponsiveContainer></div>
  </div>;
}
export const MetricsChart: React.FC<{ snapshot?: MetricSnapshot | null; history?: MetricSnapshot[]; isRunning?: boolean; telemetryUnavailable?: boolean; config?: ExperimentConfig; className?: string }> = ({ snapshot, history, isRunning, telemetryUnavailable, config, className = '' }) => {
  const [live, setLive] = useState<MetricSnapshot[]>([]);
  useEffect(() => { if (snapshot && validSnapshot(snapshot)) setLive(prev => [...prev.filter(s => s.timestamp !== snapshot.timestamp), snapshot].sort((a,b) => Date.parse(a.timestamp)-Date.parse(b.timestamp))); }, [snapshot]);
  const data = history?.length ? history : live;
  return <section className={`rounded border border-slate-700 bg-[#161b22] p-4 space-y-4 ${className}`}>
    <h2 className="text-sm font-semibold">{isRunning ? 'Live experiment telemetry' : 'Recorded experiment telemetry'}</h2>
    {telemetryUnavailable && <p className="text-xs text-amber-400">Latest telemetry unavailable. Any saved observations are shown below.</p>}
    <TelemetryCharts history={data} config={config} />
  </section>;
};
export default MetricsChart;
