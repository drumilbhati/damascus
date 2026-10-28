import React from 'react';
import { Link } from 'react-router-dom';
import { useExperiments, useHealth, useDependencies } from '../api/hooks';
import {
  FlaskConical,
  Network,
  ArrowRight,
  Play,
  CheckCircle2,
  AlertCircle,
} from 'lucide-react';

// Reusable stat card component
const StatCard: React.FC<{
  label: string;
  value: string | number;
  sub?: string;
  color?: string;
}> = ({ label, value, sub, color = '#e6edf3' }) => (
  <div
    className="rounded p-4"
    style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}
  >
    <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
      {label}
    </div>
    <div className="mt-2 font-mono text-2xl font-bold" style={{ color }}>
      {value}
    </div>
    {sub && (
      <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
        {sub}
      </div>
    )}
  </div>
);

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

export const DashboardPage: React.FC = () => {
  const { data: experiments, isLoading: loadingExps } = useExperiments();
  const { data: health } = useHealth();
  const { data: deps } = useDependencies();

  const total = experiments?.length || 0;
  const running = experiments?.filter((e) => e.state === 'RUNNING').length || 0;
  const completed = experiments?.filter((e) => e.state === 'COMPLETED').length || 0;
  const aborted = experiments?.filter((e) => e.state === 'ABORTED').length || 0;
  const serviceCount = Object.keys(deps?.graph?.nodes || {}).length;

  return (
    <div className="space-y-5">
      {/* Page header - compact, no gradient */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-base font-semibold" style={{ color: '#e6edf3' }}>
            Overview
          </h1>
          <p className="mt-0.5 text-xs" style={{ color: '#8b949e' }}>
            Autonomous chaos &amp; capacity intelligence for microservice meshes
          </p>
        </div>
        <div className="flex gap-2">
          <Link
            to="/experiments"
            className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold"
            style={{ backgroundColor: '#f0883e', color: '#0d1117' }}
          >
            <Play className="h-3 w-3 fill-current" />
            New Experiment
          </Link>
          <Link
            to="/dependencies"
            className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold"
            style={{ border: '1px solid #30363d', color: '#8b949e' }}
          >
            <Network className="h-3 w-3" />
            Topology
          </Link>
        </div>
      </div>

      {/* Stat row */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatCard label="Running" value={running} sub="active experiments" color="#3fb950" />
        <StatCard label="Total" value={total} sub={`${completed} completed`} />
        <StatCard label="Aborted" value={aborted} sub="safety triggered" color={aborted > 0 ? '#f85149' : '#e6edf3'} />
        <StatCard label="Services" value={serviceCount} sub="in mesh topology" color="#58a6ff" />
      </div>

      {/* Main content grid */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
        {/* Experiments list */}
        <div className="lg:col-span-2">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs font-semibold" style={{ color: '#e6edf3' }}>
              Recent Experiments
            </span>
            <Link
              to="/experiments"
              className="flex items-center gap-1 text-[11px]"
              style={{ color: '#58a6ff' }}
            >
              View all <ArrowRight className="h-3 w-3" />
            </Link>
          </div>
          <div
            className="rounded overflow-hidden"
            style={{ border: '1px solid #30363d', backgroundColor: '#161b22' }}
          >
            {loadingExps ? (
              <div className="flex h-40 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
                Loading...
              </div>
            ) : !experiments || experiments.length === 0 ? (
              <div className="flex h-40 flex-col items-center justify-center gap-2 text-xs" style={{ color: '#6e7681' }}>
                <FlaskConical className="h-6 w-6" />
                <span>No experiments yet. Create one to get started.</span>
              </div>
            ) : (
              <table className="w-full text-xs">
                <thead>
                  <tr style={{ borderBottom: '1px solid #30363d', backgroundColor: '#21262d' }}>
                    <th className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>Service</th>
                    <th className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>Type</th>
                    <th className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>State</th>
                    <th className="px-4 py-2 text-right font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}></th>
                  </tr>
                </thead>
                <tbody>
                  {experiments.slice(0, 8).map((exp, idx) => (
                    <tr
                      key={exp.id}
                      style={{
                        borderBottom: idx < Math.min(experiments.length - 1, 7) ? '1px solid #21262d' : 'none',
                      }}
                      className="hover:bg-[#1c2128] transition-colors"
                    >
                      <td className="px-4 py-2.5">
                        <div className="font-medium" style={{ color: '#e6edf3' }}>{exp.target_service}</div>
                        <div className="font-mono text-[10px]" style={{ color: '#6e7681' }}>{exp.target_url}</div>
                      </td>
                      <td className="px-4 py-2.5">
                        <span
                          className="rounded px-1.5 py-0.5 font-mono text-[10px]"
                          style={{ backgroundColor: '#21262d', color: '#8b949e' }}
                        >
                          {exp.type}
                        </span>
                      </td>
                      <td className="px-4 py-2.5">
                        <StateBadge state={exp.state} />
                      </td>
                      <td className="px-4 py-2.5 text-right">
                        <Link
                          to={`/experiments/${exp.id}`}
                          className="text-[11px]"
                          style={{ color: '#58a6ff' }}
                        >
                          Detail →
                        </Link>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>

        {/* System connectivity */}
        <div>
          <div className="mb-2">
            <span className="text-xs font-semibold" style={{ color: '#e6edf3' }}>System Connectivity</span>
          </div>
          <div
            className="rounded overflow-hidden"
            style={{ border: '1px solid #30363d', backgroundColor: '#161b22' }}
          >
            {[
              { label: 'Control Plane API', value: health?.status || 'UP', ok: health?.status === 'UP' },
              { label: 'Prometheus', value: health?.prometheus || 'POLLING', ok: health?.prometheus === 'CONNECTED' },
              { label: 'Jaeger Tracing', value: health?.jaeger || 'CONNECTED', ok: health?.jaeger === 'CONNECTED' },
            ].map((item, idx, arr) => (
              <div
                key={item.label}
                className="flex items-center justify-between px-4 py-3"
                style={{ borderBottom: idx < arr.length - 1 ? '1px solid #21262d' : 'none' }}
              >
                <div className="flex items-center gap-2">
                  <span
                    className="h-1.5 w-1.5 rounded-full shrink-0"
                    style={{ backgroundColor: item.ok ? '#3fb950' : '#8b949e' }}
                  />
                  <span className="text-xs" style={{ color: '#8b949e' }}>{item.label}</span>
                </div>
                <div className="flex items-center gap-1">
                  {item.ok ? (
                    <CheckCircle2 className="h-3 w-3" style={{ color: '#3fb950' }} />
                  ) : (
                    <AlertCircle className="h-3 w-3" style={{ color: '#8b949e' }} />
                  )}
                  <span className="font-mono text-[11px] font-semibold" style={{ color: item.ok ? '#3fb950' : '#8b949e' }}>
                    {item.value}
                  </span>
                </div>
              </div>
            ))}
          </div>

          {/* SLA engine info */}
          <div
            className="mt-3 rounded p-3"
            style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}
          >
            <div className="text-[10px] font-semibold uppercase tracking-widest mb-1" style={{ color: '#6e7681' }}>
              SLA Safety Engine
            </div>
            <p className="text-[11px]" style={{ color: '#8b949e' }}>
              Fast-path cancellation stops stress workers in &lt;100ms when P95 latency, error-rate, or availability SLAs breach.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
};
