import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { useExperiments } from '../api/hooks';
import { createExperiment } from '../api/endpoints';
import type { ExperimentType, ExperimentConfig } from '../types';
import {
  FlaskConical,
  Plus,
  ArrowRight,
  X,
} from 'lucide-react';

export const ExperimentsPage: React.FC = () => {
  const { data: experiments, mutate, isLoading } = useExperiments();
  const [filterState, setFilterState] = useState<string>('ALL');
  const [showModal, setShowModal] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // Form State
  const [targetService, setTargetService] = useState('checkout');
  const [targetURL, setTargetURL] = useState('http://localhost:8080/checkout');
  const [expType, setExpType] = useState<ExperimentType>('ramp');
  const [config, setConfig] = useState<ExperimentConfig>({
    initial_rate: 10,
    step_rate: 10,
    max_rate: 100,
    step_duration_seconds: 5,
    max_p95_latency_ms: 150,
    max_error_rate_percent: 2.0,
    min_availability_pct: 99.0,
    recovery_window_sec: 5,
  });

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setIsSubmitting(true);
    try {
      await createExperiment({
        target_service: targetService,
        target_url: targetURL,
        type: expType,
        config,
      });
      setShowModal(false);
      mutate();
    } catch (err: any) {
      setFormError(err.message || 'Failed to create experiment');
    } finally {
      setIsSubmitting(false);
    }
  };

  const filtered = experiments?.filter((exp) => {
    if (filterState === 'ALL') return true;
    return exp.state === filterState;
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-slate-900 dark:text-white">
            Chaos & Capacity Experiments
          </h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">
            Define, monitor, and inspect automated load ramp schedules and SLA health limits.
          </p>
        </div>
        <button
          onClick={() => setShowModal(true)}
          className="inline-flex items-center gap-2 rounded-xl bg-indigo-600 px-4 py-2.5 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-600/50"
        >
          <Plus className="h-4 w-4" />
          New Experiment
        </button>
      </div>

      {/* Filter Tabs */}
      <div className="flex flex-wrap gap-2 border-b border-slate-200 pb-3 dark:border-slate-800">
        {['ALL', 'RUNNING', 'CREATED', 'COMPLETED', 'ABORTED'].map((state) => (
          <button
            key={state}
            onClick={() => setFilterState(state)}
            className={`rounded-lg px-3 py-1.5 text-xs font-semibold transition ${
              filterState === state
                ? 'bg-slate-900 text-white dark:bg-white dark:text-slate-900'
                : 'text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800'
            }`}
          >
            {state}
          </button>
        ))}
      </div>

      {/* Table / List */}
      <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
        {isLoading ? (
          <div className="flex h-64 items-center justify-center text-sm text-slate-400">
            Loading experiments...
          </div>
        ) : !filtered || filtered.length === 0 ? (
          <div className="flex h-64 flex-col items-center justify-center space-y-3 text-center text-slate-500 dark:text-slate-400">
            <FlaskConical className="h-10 w-10 stroke-1 text-slate-400" />
            <p className="font-medium">No experiments match the selected filter</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm text-slate-600 dark:text-slate-300">
              <thead className="border-b border-slate-200 bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:border-slate-800 dark:bg-slate-800/50 dark:text-slate-400">
                <tr>
                  <th className="px-6 py-4 font-semibold">Target Service</th>
                  <th className="px-6 py-4 font-semibold">Strategy</th>
                  <th className="px-6 py-4 font-semibold">Rate Parameters</th>
                  <th className="px-6 py-4 font-semibold">SLA Latency</th>
                  <th className="px-6 py-4 font-semibold">Status</th>
                  <th className="px-6 py-4 font-semibold text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {filtered.map((exp) => (
                  <tr
                    key={exp.id}
                    className="transition hover:bg-slate-50/70 dark:hover:bg-slate-800/30"
                  >
                    <td className="px-6 py-4">
                      <div className="font-semibold text-slate-900 dark:text-white">
                        {exp.target_service}
                      </div>
                      <div className="font-mono text-xs text-slate-400 truncate max-w-xs">
                        {exp.target_url}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="rounded bg-slate-100 px-2 py-0.5 text-xs font-mono font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                        {exp.type}
                      </span>
                    </td>
                    <td className="px-6 py-4 font-mono text-xs">
                      {exp.config.initial_rate} → {exp.config.max_rate} RPS (+{exp.config.step_rate}/step)
                    </td>
                    <td className="px-6 py-4 font-mono text-xs">
                      &lt;{exp.config.max_p95_latency_ms}ms
                    </td>
                    <td className="px-6 py-4">
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
                    </td>
                    <td className="px-6 py-4 text-right">
                      <Link
                        to={`/experiments/${exp.id}`}
                        className="inline-flex items-center gap-1 font-semibold text-indigo-600 hover:text-indigo-500 dark:text-indigo-400"
                      >
                        Details <ArrowRight className="h-4 w-4" />
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* New Experiment Modal */}
      {showModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-950/50 backdrop-blur-sm">
          <div className="w-full max-w-xl rounded-2xl border border-slate-200 bg-white p-6 shadow-2xl dark:border-slate-800 dark:bg-slate-900">
            <div className="flex items-center justify-between border-b border-slate-200 pb-4 dark:border-slate-800">
              <h2 className="text-lg font-bold text-slate-900 dark:text-white">
                New Stress & Chaos Experiment
              </h2>
              <button
                onClick={() => setShowModal(false)}
                className="rounded-lg p-1 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <form onSubmit={handleCreate} className="mt-4 space-y-4">
              {formError && (
                <div className="rounded-lg bg-rose-50 p-3 text-xs font-semibold text-rose-600 dark:bg-rose-950/50 dark:text-rose-400">
                  {formError}
                </div>
              )}

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Target Service
                  </label>
                  <input
                    type="text"
                    required
                    value={targetService}
                    onChange={(e) => setTargetService(e.target.value)}
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Strategy Type
                  </label>
                  <select
                    value={expType}
                    onChange={(e) => setExpType(e.target.value as ExperimentType)}
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  >
                    <option value="ramp">Ramp (Progressive Load)</option>
                    <option value="step">Step (Stepped Plateau)</option>
                  </select>
                </div>
              </div>

              <div>
                <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                  Target Endpoint URL
                </label>
                <input
                  type="url"
                  required
                  value={targetURL}
                  onChange={(e) => setTargetURL(e.target.value)}
                  className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm font-mono text-slate-900 focus:border-indigo-500 focus:outline-none dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                />
              </div>

              <div className="grid grid-cols-3 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Initial Rate (RPS)
                  </label>
                  <input
                    type="number"
                    min="1"
                    value={config.initial_rate}
                    onChange={(e) =>
                      setConfig({ ...config, initial_rate: parseInt(e.target.value) || 1 })
                    }
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Step Rate (RPS)
                  </label>
                  <input
                    type="number"
                    min="1"
                    value={config.step_rate}
                    onChange={(e) =>
                      setConfig({ ...config, step_rate: parseInt(e.target.value) || 1 })
                    }
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Max Rate (RPS)
                  </label>
                  <input
                    type="number"
                    min="1"
                    value={config.max_rate}
                    onChange={(e) =>
                      setConfig({ ...config, max_rate: parseInt(e.target.value) || 1 })
                    }
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Max P95 Latency (ms)
                  </label>
                  <input
                    type="number"
                    min="1"
                    value={config.max_p95_latency_ms}
                    onChange={(e) =>
                      setConfig({ ...config, max_p95_latency_ms: parseFloat(e.target.value) || 100 })
                    }
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    Max Error Rate (%)
                  </label>
                  <input
                    type="number"
                    step="0.1"
                    min="0"
                    max="100"
                    value={config.max_error_rate_percent}
                    onChange={(e) =>
                      setConfig({ ...config, max_error_rate_percent: parseFloat(e.target.value) || 1.0 })
                    }
                    className="mt-1 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-white"
                  />
                </div>
              </div>

              <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-800">
                <button
                  type="button"
                  onClick={() => setShowModal(false)}
                  className="rounded-lg px-4 py-2 text-sm font-semibold text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting}
                  className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow hover:bg-indigo-500 disabled:opacity-50"
                >
                  {isSubmitting ? 'Creating...' : 'Create Experiment'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
