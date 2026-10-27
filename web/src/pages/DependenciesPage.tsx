import React, { useState } from 'react';
import { useDependencies } from '../api/hooks';
import { RefreshCw } from 'lucide-react';

export const DependenciesPage: React.FC = () => {
  const [lookback, setLookback] = useState(3600);
  const { data: deps, error, isLoading, mutate } = useDependencies(lookback);

  const edges = deps?.graph?.edges || [];
  const scores = deps?.scores || [];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-900 dark:text-white">
            Trace Dependency Graph & Criticality
          </h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">
            Real-time microservice topology mapped from Jaeger distributed traces with algorithmic blast-radius scoring.
          </p>
        </div>

        <div className="flex items-center gap-3">
          <select
            value={lookback}
            onChange={(e) => setLookback(parseInt(e.target.value))}
            className="rounded-xl border border-slate-200 bg-white px-3 py-2 text-xs font-semibold text-slate-700 shadow-sm dark:border-slate-800 dark:bg-slate-900 dark:text-slate-200"
          >
            <option value={3600}>Last 1 Hour</option>
            <option value={21600}>Last 6 Hours</option>
            <option value={86400}>Last 24 Hours</option>
          </select>
          <button
            onClick={() => mutate()}
            className="flex h-9 w-9 items-center justify-center rounded-xl border border-slate-200 bg-white text-slate-600 shadow-sm hover:bg-slate-50 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400"
            title="Refresh dependencies"
          >
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>
      </div>

      {isLoading ? (
        <div className="flex h-64 items-center justify-center text-sm text-slate-400">
          Reconstructing service mesh call graph from Jaeger spans...
        </div>
      ) : error ? (
        <div className="rounded-2xl border border-rose-200 bg-rose-50 p-6 text-center text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/40 dark:text-rose-400">
          <p className="font-bold">Failed to load trace dependency graph</p>
          <p className="mt-1 text-xs">{error.message}</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
          {/* Service Criticality Ranking */}
          <div className="space-y-4 lg:col-span-1">
            <h2 className="text-base font-bold text-slate-900 dark:text-white">
              Criticality Score Leaderboard
            </h2>
            <div className="space-y-3">
              {scores.length === 0 ? (
                <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center text-xs text-slate-400 dark:border-slate-800 dark:bg-slate-900">
                  No service scores calculated yet
                </div>
              ) : (
                scores.map((s, idx) => (
                  <div
                    key={s.service_name}
                    className="rounded-2xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="flex h-6 w-6 items-center justify-center rounded-full bg-slate-100 text-xs font-bold text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                          #{idx + 1}
                        </span>
                        <span className="font-bold text-slate-900 dark:text-white">
                          {s.service_name}
                        </span>
                      </div>
                      <span className="rounded-full bg-purple-50 px-2.5 py-0.5 text-xs font-black text-purple-700 dark:bg-purple-950/60 dark:text-purple-300">
                        {(s.score * 100).toFixed(0)}%
                      </span>
                    </div>

                    {s.reasons && s.reasons.length > 0 && (
                      <ul className="mt-3 space-y-1 border-t border-slate-100 pt-2 text-[11px] text-slate-500 dark:border-slate-800 dark:text-slate-400">
                        {s.reasons.map((r, i) => (
                          <li key={i} className="flex items-start gap-1.5">
                            <span className="text-purple-500">•</span>
                            <span>{r}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                ))
              )}
            </div>
          </div>

          {/* Directed Dependencies Table */}
          <div className="space-y-4 lg:col-span-2">
            <h2 className="text-base font-bold text-slate-900 dark:text-white">
              Topology Edges ({edges.length} directed links)
            </h2>
            <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
              {edges.length === 0 ? (
                <div className="flex h-64 items-center justify-center text-xs text-slate-400">
                  No trace spans detected in lookback window
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-sm text-slate-600 dark:text-slate-300">
                    <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:border-slate-800 dark:bg-slate-800/50 dark:text-slate-400">
                      <tr>
                        <th className="px-6 py-3.5 font-semibold">Caller (From)</th>
                        <th className="px-6 py-3.5 font-semibold">Callee (To)</th>
                        <th className="px-6 py-3.5 font-semibold">Call Count</th>
                        <th className="px-6 py-3.5 font-semibold text-right">Cadence</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                      {edges.map((e, idx) => (
                        <tr
                          key={`${e.from}-${e.to}-${idx}`}
                          className="transition hover:bg-slate-50/70 dark:hover:bg-slate-800/30"
                        >
                          <td className="px-6 py-3.5 font-semibold text-slate-900 dark:text-white">
                            {e.from}
                          </td>
                          <td className="px-6 py-3.5 font-semibold text-indigo-600 dark:text-indigo-400">
                            {e.to}
                          </td>
                          <td className="px-6 py-3.5 font-mono text-xs">
                            {e.call_count.toLocaleString()} calls
                          </td>
                          <td className="px-6 py-3.5 text-right font-mono text-xs text-slate-500">
                            {e.frequency > 0 ? `${e.frequency.toFixed(1)}/s` : '—'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
