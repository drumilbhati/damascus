import React, { useState } from 'react';
import { useExperiments, useExperimentReport } from '../api/hooks';
import {
  FileSpreadsheet,
  CheckCircle2,
} from 'lucide-react';

export const ReportsPage: React.FC = () => {
  const { data: experiments } = useExperiments();
  const completedExperiments = experiments?.filter((e) => e.state === 'COMPLETED') || [];
  const [selectedExpId, setSelectedExpId] = useState<string | null>(
    completedExperiments[0]?.id || null
  );

  const { data: report, isLoading, error } = useExperimentReport(selectedExpId);

  return (
    <div className="space-y-5">
      {/* Compact header */}
      <div>
        <h1 className="text-base font-semibold" style={{ color: '#e6edf3' }}>
          Capacity Analysis Reports
        </h1>
        <p className="text-xs" style={{ color: '#8b949e' }}>
          Deterministic evaluations of sustainable throughput limits, degradation knees, and recovery speeds
        </p>
      </div>

      {completedExperiments.length === 0 ? (
        <div
          className="flex h-64 flex-col items-center justify-center gap-3 rounded text-center"
          style={{ border: '1px solid #30363d', backgroundColor: '#161b22' }}
        >
          <FileSpreadsheet className="h-8 w-8" style={{ color: '#30363d' }} />
          <div>
            <p className="text-xs font-semibold" style={{ color: '#8b949e' }}>No completed experiments available yet</p>
            <p className="mt-1 text-[11px]" style={{ color: '#6e7681' }}>
              Run an experiment to completion to generate a capacity and recovery report.
            </p>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-4">
          {/* Experiment selector list */}
          <div className="lg:col-span-1">
            <div className="mb-2">
              <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                Completed Runs
              </span>
            </div>
            <div className="space-y-1">
              {completedExperiments.map((exp) => (
                <button
                  key={exp.id}
                  onClick={() => setSelectedExpId(exp.id)}
                  className="w-full rounded px-3 py-2.5 text-left text-xs transition-colors"
                  style={
                    selectedExpId === exp.id
                      ? {
                          backgroundColor: 'rgba(240,136,62,0.08)',
                          border: '1px solid rgba(240,136,62,0.4)',
                          color: '#f0883e',
                        }
                      : {
                          backgroundColor: '#161b22',
                          border: '1px solid #30363d',
                          color: '#8b949e',
                        }
                  }
                >
                  <div className="font-semibold">{exp.target_service}</div>
                  <div className="mt-0.5 font-mono text-[10px] truncate" style={{ color: '#6e7681' }}>
                    {exp.id}
                  </div>
                </button>
              ))}
            </div>
          </div>

          {/* Report content */}
          <div className="space-y-4 lg:col-span-3">
            {isLoading ? (
              <div className="flex h-64 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
                Loading capacity report...
              </div>
            ) : error || !report ? (
              <div
                className="flex h-40 items-center justify-center rounded text-xs"
                style={{ border: '1px solid #30363d', backgroundColor: '#161b22', color: '#6e7681' }}
              >
                Select an experiment to view its analysis metrics.
              </div>
            ) : (
              <div className="space-y-4">
                {/* Summary stat cards */}
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                  <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
                    <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                      Sustainable Ceiling
                    </div>
                    <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#3fb950' }}>
                      {report.maximum_sustainable_rate}
                    </div>
                    <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>RPS · zero SLA violations</div>
                  </div>

                  <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
                    <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                      Knee Degradation
                    </div>
                    <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#d29922' }}>
                      {report.degradation_rate}
                    </div>
                    <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>RPS · latency slope &gt;50%</div>
                  </div>

                  <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
                    <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                      Safety Breach Rate
                    </div>
                    <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#f85149' }}>
                      {report.safety_boundary_rate}
                    </div>
                    <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>RPS · hard boundary breach</div>
                  </div>

                  <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
                    <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                      Recovery Time
                    </div>
                    <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#58a6ff' }}>
                      {report.recovery_time_sec}s
                    </div>
                    <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>return to baseline SLA</div>
                  </div>
                </div>

                {/* Recommendations */}
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
            )}
          </div>
        </div>
      )}
    </div>
  );
};
