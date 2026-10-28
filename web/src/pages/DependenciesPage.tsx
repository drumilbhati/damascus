import React, { useState } from 'react';
import { useDependencies } from '../api/hooks';
import { RefreshCw } from 'lucide-react';

export const DependenciesPage: React.FC = () => {
  const [lookback, setLookback] = useState(3600);
  const { data: deps, error, isLoading, mutate } = useDependencies(lookback);

  const edges = deps?.graph?.edges || [];
  const scores = deps?.scores || [];

  return (
    <div className="space-y-5">
      {/* Compact header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-base font-semibold" style={{ color: '#e6edf3' }}>
            Dependency Graph
          </h1>
          <p className="text-xs" style={{ color: '#8b949e' }}>
            Service topology mapped from Jaeger traces with blast-radius scoring
          </p>
        </div>

        <div className="flex items-center gap-2">
          <select
            value={lookback}
            onChange={(e) => setLookback(parseInt(e.target.value))}
            className="rounded px-2.5 py-1.5 text-xs font-mono outline-none"
            style={{ backgroundColor: '#21262d', border: '1px solid #30363d', color: '#e6edf3' }}
          >
            <option value={3600}>Last 1 Hour</option>
            <option value={21600}>Last 6 Hours</option>
            <option value={86400}>Last 24 Hours</option>
          </select>
          <button
            onClick={() => mutate()}
            className="flex h-7 w-7 items-center justify-center rounded"
            style={{ backgroundColor: '#21262d', border: '1px solid #30363d', color: '#8b949e' }}
            title="Refresh dependencies"
          >
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {isLoading ? (
        <div className="flex h-64 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
          Reconstructing service mesh call graph from Jaeger spans...
        </div>
      ) : error ? (
        <div
          className="rounded p-5 text-center text-xs"
          style={{ backgroundColor: 'rgba(248,81,73,0.08)', border: '1px solid rgba(248,81,73,0.25)', color: '#f85149' }}
        >
          <p className="font-bold">Failed to load trace dependency graph</p>
          <p className="mt-1" style={{ color: '#8b949e' }}>{error.message}</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-3">
          {/* Criticality Ranking */}
          <div className="lg:col-span-1">
            <div className="mb-2">
              <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                Criticality Score Leaderboard
              </span>
            </div>
            <div
              className="rounded overflow-hidden"
              style={{ border: '1px solid #30363d', backgroundColor: '#161b22' }}
            >
              {scores.length === 0 ? (
                <div className="flex h-32 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
                  No service scores calculated yet
                </div>
              ) : (
                scores.map((s, idx) => (
                  <div
                    key={s.service_name}
                    className="px-4 py-3"
                    style={{ borderBottom: idx < scores.length - 1 ? '1px solid #21262d' : 'none' }}
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span
                          className="flex h-5 w-5 items-center justify-center rounded font-mono text-[10px] font-bold shrink-0"
                          style={{ backgroundColor: '#21262d', color: '#6e7681' }}
                        >
                          {idx + 1}
                        </span>
                        <span className="text-xs font-semibold" style={{ color: '#e6edf3' }}>
                          {s.service_name}
                        </span>
                      </div>
                      <span
                        className="rounded px-1.5 py-0.5 font-mono text-[10px] font-bold"
                        style={{ backgroundColor: 'rgba(188,140,255,0.12)', color: '#bc8cff' }}
                      >
                        {(s.score * 100).toFixed(0)}%
                      </span>
                    </div>

                    {s.reasons && s.reasons.length > 0 && (
                      <ul className="mt-2 space-y-0.5 pl-7 text-[10px]" style={{ color: '#6e7681' }}>
                        {s.reasons.map((r, i) => (
                          <li key={i} className="flex items-start gap-1">
                            <span style={{ color: '#bc8cff' }}>·</span>
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

          {/* Topology Edges Table */}
          <div className="lg:col-span-2">
            <div className="mb-2 flex items-center gap-2">
              <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                Topology Edges
              </span>
              <span
                className="rounded px-1.5 py-0.5 font-mono text-[10px]"
                style={{ backgroundColor: '#21262d', color: '#8b949e', border: '1px solid #30363d' }}
              >
                {edges.length} directed links
              </span>
            </div>
            <div
              className="rounded overflow-hidden"
              style={{ border: '1px solid #30363d', backgroundColor: '#161b22' }}
            >
              {edges.length === 0 ? (
                <div className="flex h-32 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
                  No trace spans detected in lookback window
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-xs">
                    <thead>
                      <tr style={{ borderBottom: '1px solid #30363d', backgroundColor: '#21262d' }}>
                        <th className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>Caller (From)</th>
                        <th className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>Callee (To)</th>
                        <th className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>Call Count</th>
                        <th className="px-4 py-2 text-right font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>Cadence</th>
                      </tr>
                    </thead>
                    <tbody>
                      {edges.map((e, idx) => (
                        <tr
                          key={`${e.from}-${e.to}-${idx}`}
                          style={{ borderBottom: idx < edges.length - 1 ? '1px solid #21262d' : 'none' }}
                          className="hover:bg-[#1c2128] transition-colors"
                        >
                          <td className="px-4 py-2.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                            {e.from}
                          </td>
                          <td className="px-4 py-2.5 font-mono font-semibold" style={{ color: '#58a6ff' }}>
                            {e.to}
                          </td>
                          <td className="px-4 py-2.5 font-mono" style={{ color: '#8b949e' }}>
                            {e.call_count.toLocaleString()} calls
                          </td>
                          <td className="px-4 py-2.5 text-right font-mono" style={{ color: '#6e7681' }}>
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
