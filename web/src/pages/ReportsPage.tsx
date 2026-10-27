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
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-slate-900 dark:text-white">
          Capacity Analysis Reports
        </h1>
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Deterministic evaluations of sustainable throughput limits, degradation knees, and recovery speeds.
        </p>
      </div>

      {completedExperiments.length === 0 ? (
        <div className="flex h-64 flex-col items-center justify-center space-y-3 rounded-2xl border border-slate-200 bg-white p-6 text-center text-slate-500 shadow-sm dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400">
          <FileSpreadsheet className="h-10 w-10 stroke-1 text-slate-400" />
          <p className="font-medium">No completed experiments available yet</p>
          <p className="text-xs">
            Run an experiment to completion to generate a full capacity and recovery report.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-4">
          {/* Select Experiment List */}
          <div className="space-y-3 lg:col-span-1">
            <h2 className="text-xs font-bold uppercase tracking-wider text-slate-400">
              Completed Runs
            </h2>
            <div className="space-y-2">
              {completedExperiments.map((exp) => (
                <button
                  key={exp.id}
                  onClick={() => setSelectedExpId(exp.id)}
                  className={`w-full text-left rounded-xl p-3 text-xs transition border ${
                    selectedExpId === exp.id
                      ? 'border-indigo-500 bg-indigo-50/70 text-indigo-900 dark:border-indigo-500 dark:bg-indigo-950/60 dark:text-indigo-200 shadow-sm'
                      : 'border-slate-200 bg-white text-slate-700 hover:bg-slate-50 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300'
                  }`}
                >
                  <div className="font-bold">{exp.target_service}</div>
                  <div className="mt-1 font-mono text-[10px] text-slate-400 truncate">
                    {exp.id}
                  </div>
                </button>
              ))}
            </div>
          </div>

          {/* Report Content */}
          <div className="space-y-6 lg:col-span-3">
            {isLoading ? (
              <div className="flex h-64 items-center justify-center text-sm text-slate-400">
                Loading capacity report...
              </div>
            ) : error || !report ? (
              <div className="rounded-2xl border border-slate-200 bg-white p-8 text-center text-slate-500 dark:border-slate-800 dark:bg-slate-900">
                Select an experiment to view its analysis metrics.
              </div>
            ) : (
              <div className="space-y-6">
                {/* Executive Summary Cards */}
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
                  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
                    <span className="text-xs font-semibold text-slate-500">
                      Sustainable Ceiling
                    </span>
                    <div className="mt-2 text-2xl font-black text-emerald-600 dark:text-emerald-400">
                      {report.maximum_sustainable_rate} RPS
                    </div>
                    <span className="text-[11px] text-slate-400">Zero SLA violations</span>
                  </div>

                  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
                    <span className="text-xs font-semibold text-slate-500">
                      Knee Degradation
                    </span>
                    <div className="mt-2 text-2xl font-black text-amber-600 dark:text-amber-400">
                      {report.degradation_rate} RPS
                    </div>
                    <span className="text-[11px] text-slate-400">Latency slope &gt;50% increase</span>
                  </div>

                  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
                    <span className="text-xs font-semibold text-slate-500">
                      Safety Breach Rate
                    </span>
                    <div className="mt-2 text-2xl font-black text-rose-600 dark:text-rose-400">
                      {report.safety_boundary_rate} RPS
                    </div>
                    <span className="text-[11px] text-slate-400">Hard boundary breach</span>
                  </div>

                  <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
                    <span className="text-xs font-semibold text-slate-500">
                      Recovery Time
                    </span>
                    <div className="mt-2 text-2xl font-black text-indigo-600 dark:text-indigo-400">
                      {report.recovery_time_sec}s
                    </div>
                    <span className="text-[11px] text-slate-400">Return to baseline SLA</span>
                  </div>
                </div>

                {/* Recommendations */}
                <div className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900 space-y-3">
                  <h3 className="font-bold text-slate-900 dark:text-white">
                    Automated Engineering Recommendations
                  </h3>
                  {report.recommendations && report.recommendations.length > 0 ? (
                    <ul className="space-y-2 text-sm text-slate-600 dark:text-slate-300">
                      {report.recommendations.map((rec, i) => (
                        <li key={i} className="flex items-start gap-2">
                          <CheckCircle2 className="h-4 w-4 text-emerald-500 shrink-0 mt-0.5" />
                          <span>{rec}</span>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    <p className="text-xs text-slate-500">
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
