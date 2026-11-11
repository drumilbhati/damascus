import React, { useMemo, useState } from 'react';
import { TelemetryCharts } from './MetricsChart';
import { apiClient } from '../api/client';
import type { ExperimentReport } from '../types';
const value = (v: number | null | undefined, unit = '') => v == null ? 'Unavailable' : `${v.toFixed(2)}${unit}`;
function Card({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="rounded border border-slate-700 bg-[#161b22] p-4"><h3 className="text-xs text-slate-400">{label}</h3><div className="mt-2 text-xl font-mono text-slate-100">{children}</div></div>;
}
export const ReportView: React.FC<{ report: ExperimentReport; className?: string }> = ({ report, className = '' }) => {
  const [error, setError] = useState('');
  const m = report.measurements;
  const obs = useMemo(() => [...(report.observations ?? [])].sort((a,b) => Date.parse(a.timestamp)-Date.parse(b.timestamp)), [report]);
  const history = useMemo(() => obs.filter(o => !o.telemetry_missing && o.sla_status !== 'unavailable').map(o => ({ ...o, experiment_id: report.experiment_id, target_service: report.target_service, request_rate: o.load_rate, offered_rate: o.injected_rate ?? undefined, p50_latency_ms: 0, p99_latency_ms: 0, cpu_utilization: o.cpu_utilization ?? 0, memory_utilization: o.memory_utilization ?? 0 })), [obs, report]);
  async function download(format: 'json' | 'html') {
    try {
      setError('');
      const data = format === 'json' ? JSON.stringify(report, null, 2) : (await apiClient.get(`/experiments/${report.experiment_id}/report?format=html`, { responseType: 'text' })).data;
      const url = URL.createObjectURL(new Blob([data], { type: format === 'json' ? 'application/json' : 'text/html' }));
      const a = document.createElement('a'); a.href = url; a.download = `damascus-${report.experiment_id}.${format}`; a.click(); URL.revokeObjectURL(url);
    } catch (e) { setError(e instanceof Error ? e.message : 'Export failed'); }
  }
  if (!m) return <p>Measured report results are unavailable. Refresh after updating the control plane.</p>;
  return <div className={`space-y-5 ${className}`}>
    <header className="rounded border border-slate-700 bg-[#161b22] p-4 space-y-2">
      <h2 className="text-base font-semibold">Measured experiment results · {report.target_service}</h2>
      <p className="text-xs text-slate-400 break-all">{report.experiment_id} · {new Date(report.generated_at).toLocaleString()}</p>
      <div className="flex gap-3 text-xs text-blue-400"><button onClick={() => download('json')}>Export JSON</button><button onClick={() => download('html')}>Export HTML</button></div>
      {error && <p role="alert" className="text-red-400">{error}</p>}
    </header>
    <p className="text-xs text-slate-400">{m.summary_scope === 'injection' ? 'Peaks and minimum availability below cover valid observations during injection.' : 'Injection phase is unknown for these older samples; summaries cover recorded service traffic.'} {m.sample_count} samples · {m.invalid_sample_count} invalid samples excluded.</p>
    <div className="grid grid-cols-2 xl:grid-cols-3 gap-3">
      <Card label="Peak measured service traffic">{value(m.peak_measured_rate, ' RPS')}</Card>
      <Card label="Peak offered load target">{value(m.peak_offered_rate, ' RPS')}</Card>
      <Card label="Peak P95 latency">{value(m.peak_p95_latency_ms, ' ms')}</Card>
      <Card label="Peak error rate">{value(m.peak_error_rate == null ? null : m.peak_error_rate * 100, '%')}</Card>
      <Card label="Minimum availability">{value(m.min_availability == null ? null : m.min_availability * 100, '%')}</Card>
    </div>
    <section className="rounded border border-slate-700 p-4 space-y-3 text-sm">
      <h3 className="font-semibold">Sustainable capacity estimate: {m.capacity_status === 'established' ? value(m.sustainable_measured_rate, ' measured RPS') : 'Not established'}</h3>
      <p className="text-slate-400">{m.capacity_reason}</p>
      {m.capacity_status === 'established' && <p>Offered target during qualifying window: {value(m.sustainable_offered_rate, ' RPS')}</p>}
      <p>SLA breach: {m.breach_status === 'observed' ? `${value(m.first_breach_measured_rate, ' measured RPS')}; offered target ${value(m.first_breach_offered_rate, ' RPS')}` : m.breach_status === 'unavailable' ? 'Unavailable — no valid telemetry' : 'Not observed'}</p>
      <p>Recovery: {m.recovery_status === 'recovered' ? `${value(m.recovery_seconds, 's')} to observed SLA compliance from the first zero-offered sample` : m.recovery_status === 'not_needed' ? 'Not needed — no SLA breach observed' : m.recovery_status === 'not_recovered' ? `Not observed within ${value(m.recovery_window_seconds, 's')} of monitoring` : 'Not observed'}</p>
      <p className="text-xs text-slate-400">A breach is an observed SLA event, not proof of a hard capacity boundary. Recovery timing has the resolution of saved samples.</p>
      {report.stop_reason && <p className="text-amber-400">Stop reason: {report.stop_reason}</p>}
    </section>
    <section className="rounded border border-slate-700 bg-[#161b22] p-4 space-y-3"><h3 className="text-sm font-semibold">Measured and offered traffic over time</h3><TelemetryCharts history={history} /></section>
    <section className="overflow-x-auto rounded border border-slate-700 p-4"><h3 className="mb-3 text-sm font-semibold">Saved observations</h3>
      <table className="w-full text-xs text-left whitespace-nowrap"><thead><tr>{['Time', 'Phase', 'Measured RPS', 'Offered target RPS', 'P95 ms', 'Errors %', 'Availability %', 'SLA'].map(h => <th className="p-2" key={h}>{h}</th>)}</tr></thead>
      <tbody>{obs.map((o,i) => { const invalid = o.telemetry_missing || o.sla_status === 'unavailable'; return <tr className="border-t border-slate-700" key={`${o.timestamp}-${i}`}>
        <td className="p-2">{new Date(o.timestamp).toLocaleTimeString()}</td><td className="p-2">{o.phase ?? 'unknown'}</td><td className="p-2">{value(invalid ? null : o.load_rate)}</td><td className="p-2">{value(o.injected_rate)}</td><td className="p-2">{value(invalid ? null : o.p95_latency_ms)}</td><td className="p-2">{value(invalid ? null : o.error_rate * 100)}</td><td className="p-2">{value(invalid ? null : o.availability * 100)}</td><td className="p-2">{o.sla_status?.replaceAll('_', ' ') ?? 'Unavailable'}</td>
      </tr>; })}</tbody></table>
    </section>
    <section className="rounded border border-slate-700 p-4 space-y-3 text-sm"><h3 className="font-semibold">Observations and recommendations</h3><p>Service criticality score: {report.criticality_score.toFixed(2)}</p><ul className="list-disc pl-5 space-y-2 text-slate-400">{report.recommendations.map((r,i) => <li key={i}>{r}</li>)}</ul></section>
  </div>;
};
