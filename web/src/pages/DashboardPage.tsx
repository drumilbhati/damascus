import React from 'react';
import { Link } from 'react-router-dom';
import { useExperiments, useHealth, useDependencies } from '../api/hooks';
import {
  FlaskConical,
  Activity,
  Network,
  ArrowRight,
  AlertTriangle,
  Play,
  CheckCircle2,
} from 'lucide-react';

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
    <div className="space-y-6">
      {/* Top Banner / Hero */}
      <div className="relative overflow-hidden rounded-2xl bg-gradient-to-r from-indigo-900 via-indigo-800 to-purple-900 p-6 text-white shadow-xl shadow-indigo-950/10 sm:p-8">
        <div className="relative z-10 max-w-2xl space-y-3">
          <div className="inline-flex items-center gap-2 rounded-full bg-white/10 px-3 py-1 text-xs font-semibold backdrop-blur">
            <span className="h-2 w-2 rounded-full bg-emerald-400"></span>
            Observability Mesh Attached
          </div>
          <h1 className="text-2xl font-black tracking-tight sm:text-3xl">
            Autonomous Chaos & Capacity Intelligence
          </h1>
          <p className="text-sm text-indigo-150 sm:text-base">
            Inject traffic ramps, evaluate live telemetry SLA boundaries, and quantify
            service mesh breaking points with deterministic safety stops.
          </p>
          <div className="flex flex-wrap gap-3 pt-2">
            <Link
              to="/experiments"
              className="inline-flex items-center gap-2 rounded-xl bg-white px-4 py-2 text-sm font-bold text-indigo-900 shadow transition hover:bg-indigo-50"
            >
              <Play className="h-4 w-4 fill-current" />
              Manage Experiments
            </Link>
            <Link
              to="/dependencies"
              className="inline-flex items-center gap-2 rounded-xl border border-white/20 bg-white/10 px-4 py-2 text-sm font-semibold text-white backdrop-blur transition hover:bg-white/20"
            >
              <Network className="h-4 w-4" />
              Trace Topology Graph
            </Link>
          </div>
        </div>
      </div>

      {/* Metrics Summary Grid */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Active Stress Runs
            </span>
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-600 dark:text-emerald-400">
              <Activity className="h-5 w-5 animate-pulse" />
            </div>
          </div>
          <div className="mt-3 flex items-baseline gap-2">
            <span className="text-3xl font-extrabold text-slate-900 dark:text-white">
              {running}
            </span>
            <span className="text-xs text-slate-500">currently executing</span>
          </div>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Total Experiments
            </span>
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-600 dark:text-indigo-400">
              <FlaskConical className="h-5 w-5" />
            </div>
          </div>
          <div className="mt-3 flex items-baseline gap-2">
            <span className="text-3xl font-extrabold text-slate-900 dark:text-white">
              {total}
            </span>
            <span className="text-xs text-slate-500">({completed} completed)</span>
          </div>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Mesh Topology
            </span>
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-purple-500/10 text-purple-600 dark:text-purple-400">
              <Network className="h-5 w-5" />
            </div>
          </div>
          <div className="mt-3 flex items-baseline gap-2">
            <span className="text-3xl font-extrabold text-slate-900 dark:text-white">
              {serviceCount > 0 ? serviceCount : '—'}
            </span>
            <span className="text-xs text-slate-500">services mapped</span>
          </div>
        </div>

        <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center justify-between">
            <span className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Safe Brake Stops
            </span>
            <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-amber-500/10 text-amber-600 dark:text-amber-400">
              <AlertTriangle className="h-5 w-5" />
            </div>
          </div>
          <div className="mt-3 flex items-baseline gap-2">
            <span className="text-3xl font-extrabold text-slate-900 dark:text-white">
              {aborted}
            </span>
            <span className="text-xs text-slate-500">SLA violations caught</span>
          </div>
        </div>
      </div>

      {/* Recent Experiments & Quick Status */}
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-2">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-bold text-slate-900 dark:text-white">
              Recent Experiments
            </h2>
            <Link
              to="/experiments"
              className="inline-flex items-center gap-1 text-xs font-semibold text-indigo-600 hover:text-indigo-500 dark:text-indigo-400"
            >
              View all <ArrowRight className="h-3.5 w-3.5" />
            </Link>
          </div>

          <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
            {loadingExps ? (
              <div className="flex h-48 items-center justify-center text-sm text-slate-400">
                Loading experiments...
              </div>
            ) : !experiments || experiments.length === 0 ? (
              <div className="flex h-48 flex-col items-center justify-center space-y-2 text-center text-slate-500 dark:text-slate-400">
                <FlaskConical className="h-8 w-8 stroke-1 text-slate-400" />
                <p className="text-sm font-medium">No experiments initiated yet</p>
                <Link
                  to="/experiments"
                  className="rounded-lg bg-indigo-50 px-3 py-1.5 text-xs font-semibold text-indigo-600 dark:bg-indigo-950 dark:text-indigo-400"
                >
                  Create your first experiment
                </Link>
              </div>
            ) : (
              <div className="divide-y divide-slate-100 dark:divide-slate-800">
                {experiments.slice(0, 5).map((exp) => (
                  <Link
                    key={exp.id}
                    to={`/experiments/${exp.id}`}
                    className="flex items-center justify-between p-4 transition hover:bg-slate-50 dark:hover:bg-slate-800/40"
                  >
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold text-slate-900 dark:text-white">
                          {exp.target_service}
                        </span>
                        <span className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-mono text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                          {exp.type}
                        </span>
                      </div>
                      <p className="text-xs text-slate-500 dark:text-slate-400 font-mono">
                        {exp.target_url}
                      </p>
                    </div>

                    <div className="flex items-center gap-3">
                      <span
                        className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-bold ${
                          exp.state === 'RUNNING'
                            ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/60 dark:text-emerald-400'
                            : exp.state === 'COMPLETED'
                            ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-950/60 dark:text-indigo-400'
                            : exp.state === 'ABORTED'
                            ? 'bg-rose-50 text-rose-700 dark:bg-rose-950/60 dark:text-rose-400'
                            : 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                        }`}
                      >
                        {exp.state}
                      </span>
                      <ArrowRight className="h-4 w-4 text-slate-400" />
                    </div>
                  </Link>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Observability & Safety Status Card */}
        <div className="space-y-4">
          <h2 className="text-lg font-bold text-slate-900 dark:text-white">
            System Connectivity
          </h2>
          <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900 space-y-4">
            <div className="space-y-3">
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-slate-600 dark:text-slate-400">
                  Control Plane API
                </span>
                <span className="font-bold text-emerald-600 dark:text-emerald-400 flex items-center gap-1">
                  <CheckCircle2 className="h-3 w-3" />
                  {health?.status || 'CONNECTED'}
                </span>
              </div>
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-slate-600 dark:text-slate-400">
                  Prometheus Telemetry
                </span>
                <span
                  className={`font-bold ${
                    health?.prometheus === 'CONNECTED'
                      ? 'text-emerald-600 dark:text-emerald-400'
                      : 'text-slate-500'
                  }`}
                >
                  {health?.prometheus || 'POLLING'}
                </span>
              </div>
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-slate-600 dark:text-slate-400">
                  Jaeger Distributed Tracing
                </span>
                <span
                  className={`font-bold ${
                    health?.jaeger === 'CONNECTED'
                      ? 'text-emerald-600 dark:text-emerald-400'
                      : 'text-slate-500'
                  }`}
                >
                  {health?.jaeger || 'CONNECTED'}
                </span>
              </div>
            </div>

            <div className="rounded-xl bg-slate-50 p-3 text-xs text-slate-600 dark:bg-slate-800/40 dark:text-slate-400">
              <p className="font-semibold text-slate-700 dark:text-slate-300">
                Deterministic SLA Engine
              </p>
              <p className="mt-1">
                Fast-path cancellation cancels stress workers in &lt;100ms when P95 latency,
                availability, or error-rate thresholds breach.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
