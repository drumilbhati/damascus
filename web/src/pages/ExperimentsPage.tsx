import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { useExperiments, useServices } from '../api/hooks';
import { createExperiment } from '../api/endpoints';
import type { ExperimentType, ExperimentConfig } from '../types';
import { FlaskConical, Plus, ArrowRight, X, Loader2, AlertCircle, RefreshCw } from 'lucide-react';

function StateBadge({ state }: { state: string }) {
  const styles: Record<string, { color: string; bg: string }> = {
    RUNNING: { color: '#3fb950', bg: 'rgba(63,185,80,0.12)' },
    COMPLETED: { color: '#58a6ff', bg: 'rgba(88,166,255,0.12)' },
    ABORTED: { color: '#f85149', bg: 'rgba(248,81,73,0.12)' },
    CREATED: { color: '#d29922', bg: 'rgba(210,153,34,0.12)' },
  };
  const s = styles[state] || { color: '#8b949e', bg: '#21262d' };
  return (
    <span className="rounded px-2 py-0.5 font-mono text-[10px] font-semibold" style={{ color: s.color, backgroundColor: s.bg }}>
      {state}
    </span>
  );
}

const inputCls = "w-full rounded px-3 py-1.5 text-xs font-mono outline-none focus:ring-1 focus:ring-[#f0883e]";
const inputStyle = { backgroundColor: '#0d1117', border: '1px solid #30363d', color: '#e6edf3' };

export const ExperimentsPage: React.FC = () => {
  const { data: experiments, mutate, isLoading } = useExperiments();
  const [filterState, setFilterState] = useState<string>('ALL');
  const [showModal, setShowModal] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // New flow: URL first, then service selected from discovered list
  const [targetURL, setTargetURL] = useState('');
  const [targetService, setTargetService] = useState('');
  const [servicesLoaded, setServicesLoaded] = useState(false);

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

  // Only fetch services when the modal is open
  const { data: servicesData, isLoading: loadingServices, error: servicesError, mutate: refetchServices } = useServices(showModal);

  const availableServices = servicesData?.services ?? [];

  // When services load for the first time, auto-select the first one
  React.useEffect(() => {
    if (availableServices.length > 0 && !servicesLoaded) {
      setTargetService(availableServices[0]);
      setServicesLoaded(true);
    }
  }, [availableServices, servicesLoaded]);

  const handleOpenModal = () => {
    setTargetURL('');
    setTargetService('');
    setServicesLoaded(false);
    setFormError(null);
    setShowModal(true);
  };

  const handleCloseModal = () => {
    setShowModal(false);
    setServicesLoaded(false);
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!targetService) {
      setFormError('Please select a target service');
      return;
    }
    if (!targetURL) {
      setFormError('Please enter the target URL');
      return;
    }
    setFormError(null);
    setIsSubmitting(true);
    try {
      await createExperiment({ target_service: targetService, target_url: targetURL, type: expType, config });
      handleCloseModal();
      mutate();
    } catch (err: any) {
      setFormError(err.message || 'Failed to create experiment');
    } finally {
      setIsSubmitting(false);
    }
  };

  const filtered = experiments?.filter((exp) => filterState === 'ALL' || exp.state === filterState);
  const states = ['ALL', 'RUNNING', 'CREATED', 'COMPLETED', 'ABORTED'];

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-base font-semibold" style={{ color: '#e6edf3' }}>Experiments</h1>
          <p className="text-xs" style={{ color: '#8b949e' }}>
            Load ramp schedules with SLA safety brakes
          </p>
        </div>
        <button
          onClick={handleOpenModal}
          className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold"
          style={{ backgroundColor: '#f0883e', color: '#0d1117' }}
        >
          <Plus className="h-3.5 w-3.5" />
          New Experiment
        </button>
      </div>

      {/* Filter tabs */}
      <div className="flex gap-1 flex-wrap" style={{ borderBottom: '1px solid #30363d', paddingBottom: '8px' }}>
        {states.map((state) => (
          <button
            key={state}
            onClick={() => setFilterState(state)}
            className="rounded px-3 py-1 font-mono text-[11px] font-semibold transition-colors"
            style={
              filterState === state
                ? { backgroundColor: '#21262d', color: '#e6edf3', border: '1px solid #30363d' }
                : { backgroundColor: 'transparent', color: '#6e7681', border: '1px solid transparent' }
            }
          >
            {state}
          </button>
        ))}
      </div>

      {/* Table */}
      <div className="rounded overflow-hidden" style={{ border: '1px solid #30363d', backgroundColor: '#161b22' }}>
        {isLoading ? (
          <div className="flex h-40 items-center justify-center text-xs" style={{ color: '#6e7681' }}>Loading experiments...</div>
        ) : !filtered || filtered.length === 0 ? (
          <div className="flex h-40 flex-col items-center justify-center gap-2 text-xs" style={{ color: '#6e7681' }}>
            <FlaskConical className="h-7 w-7" />
            <span>No experiments match the selected filter</span>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr style={{ borderBottom: '1px solid #30363d', backgroundColor: '#21262d' }}>
                  {['Target Service', 'Strategy', 'Rate (init → max)', 'SLA Thresholds', 'State', ''].map((h) => (
                    <th key={h} className="px-4 py-2 text-left font-semibold uppercase tracking-wider" style={{ color: '#6e7681' }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {filtered.map((exp, idx) => (
                  <tr
                    key={exp.id}
                    style={{ borderBottom: idx < filtered.length - 1 ? '1px solid #21262d' : 'none' }}
                    className="hover:bg-[#1c2128] transition-colors"
                  >
                    <td className="px-4 py-2.5">
                      <div className="font-medium" style={{ color: '#e6edf3' }}>{exp.target_service}</div>
                      <div className="font-mono text-[10px]" style={{ color: '#6e7681' }}>{exp.target_url}</div>
                    </td>
                    <td className="px-4 py-2.5">
                      <span className="rounded px-1.5 py-0.5 font-mono text-[10px]" style={{ backgroundColor: '#21262d', color: '#8b949e' }}>
                        {exp.type}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-[11px]" style={{ color: '#8b949e' }}>
                      {exp.config.initial_rate} → {exp.config.max_rate} RPS
                    </td>
                    <td className="px-4 py-2.5 font-mono text-[11px]" style={{ color: '#8b949e' }}>
                      P95 ≤ {exp.config.max_p95_latency_ms}ms · err ≤ {exp.config.max_error_rate_percent}%
                    </td>
                    <td className="px-4 py-2.5"><StateBadge state={exp.state} /></td>
                    <td className="px-4 py-2.5 text-right">
                      <Link to={`/experiments/${exp.id}`} className="inline-flex items-center gap-1 text-[11px]" style={{ color: '#58a6ff' }}>
                        Detail <ArrowRight className="h-3 w-3" />
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Modal */}
      {showModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4" style={{ backgroundColor: 'rgba(0,0,0,0.7)' }}>
          <div className="w-full max-w-lg rounded-lg overflow-hidden" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            {/* Modal header */}
            <div className="flex items-center justify-between px-5 py-3" style={{ borderBottom: '1px solid #30363d' }}>
              <span className="text-sm font-semibold" style={{ color: '#e6edf3' }}>New Experiment</span>
              <button onClick={handleCloseModal} className="rounded p-1" style={{ color: '#8b949e' }}>
                <X className="h-4 w-4" />
              </button>
            </div>

            <form onSubmit={handleCreate} className="p-5 space-y-4">
              {formError && (
                <div className="rounded p-3 text-xs" style={{ backgroundColor: 'rgba(248,81,73,0.1)', border: '1px solid rgba(248,81,73,0.3)', color: '#f85149' }}>
                  {formError}
                </div>
              )}

              {/* Step 1: Target URL */}
              <div>
                <label className="block text-[11px] font-semibold mb-1" style={{ color: '#8b949e' }}>
                  Target URL
                </label>
                <input
                  className={inputCls}
                  style={inputStyle}
                  placeholder="http://my-service:8080/api/endpoint"
                  value={targetURL}
                  onChange={(e) => setTargetURL(e.target.value)}
                  required
                />
              </div>

              {/* Step 2: Service dropdown (from Jaeger) */}
              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="text-[11px] font-semibold" style={{ color: '#8b949e' }}>
                    Target Service
                    <span className="ml-1.5 font-normal" style={{ color: '#6e7681' }}>
                      — discovered from Jaeger traces
                    </span>
                  </label>
                  <button
                    type="button"
                    onClick={() => refetchServices()}
                    className="flex items-center gap-1 text-[10px]"
                    style={{ color: '#6e7681' }}
                    title="Refresh service list"
                  >
                    <RefreshCw className="h-3 w-3" />
                    Refresh
                  </button>
                </div>

                {loadingServices ? (
                  <div
                    className="flex items-center gap-2 rounded px-3 py-2 text-xs"
                    style={{ backgroundColor: '#0d1117', border: '1px solid #30363d', color: '#6e7681' }}
                  >
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    Querying Jaeger for active services…
                  </div>
                ) : servicesError ? (
                  <div className="space-y-2">
                    <div
                      className="flex items-center gap-2 rounded px-3 py-2 text-xs"
                      style={{ backgroundColor: 'rgba(248,81,73,0.08)', border: '1px solid rgba(248,81,73,0.25)', color: '#f85149' }}
                    >
                      <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                      Could not reach Jaeger — enter service name manually
                    </div>
                    <input
                      className={inputCls}
                      style={inputStyle}
                      placeholder="e.g. checkout"
                      value={targetService}
                      onChange={(e) => setTargetService(e.target.value)}
                      required
                    />
                  </div>
                ) : availableServices.length === 0 ? (
                  <div className="space-y-2">
                    <div
                      className="flex items-center gap-2 rounded px-3 py-2 text-xs"
                      style={{ backgroundColor: '#21262d', border: '1px solid #30363d', color: '#8b949e' }}
                    >
                      <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                      No services found in recent traces — enter name manually
                    </div>
                    <input
                      className={inputCls}
                      style={inputStyle}
                      placeholder="e.g. checkout"
                      value={targetService}
                      onChange={(e) => setTargetService(e.target.value)}
                      required
                    />
                  </div>
                ) : (
                  <select
                    className={inputCls}
                    style={inputStyle}
                    value={targetService}
                    onChange={(e) => setTargetService(e.target.value)}
                    required
                  >
                    <option value="">— select a service —</option>
                    {availableServices.sort().map((svc) => (
                      <option key={svc} value={svc}>{svc}</option>
                    ))}
                  </select>
                )}

                {targetService && !servicesError && availableServices.length > 0 && (
                  <p className="mt-1 font-mono text-[10px]" style={{ color: '#6e7681' }}>
                    selected: <span style={{ color: '#f0883e' }}>{targetService}</span>
                    &nbsp;·&nbsp;{availableServices.length} service{availableServices.length !== 1 ? 's' : ''} in mesh
                  </p>
                )}
              </div>

              {/* Strategy */}
              <div>
                <label className="block text-[11px] font-semibold mb-1" style={{ color: '#8b949e' }}>Strategy</label>
                <select className={inputCls} style={inputStyle} value={expType} onChange={(e) => setExpType(e.target.value as ExperimentType)}>
                  <option value="ramp">ramp — linearly increase load step by step</option>
                  <option value="step">step — jump to fixed load levels</option>
                </select>
              </div>

              <div className="text-[10px] font-semibold uppercase tracking-widest pt-1" style={{ color: '#6e7681' }}>Rate Config (RPS)</div>
              <div className="grid grid-cols-3 gap-3">
                {[
                  { label: 'Initial', key: 'initial_rate' },
                  { label: 'Step', key: 'step_rate' },
                  { label: 'Max', key: 'max_rate' },
                ].map(({ label, key }) => (
                  <div key={key}>
                    <label className="block text-[11px] mb-1" style={{ color: '#8b949e' }}>{label}</label>
                    <input type="number" min="1" className={inputCls} style={inputStyle}
                      value={(config as any)[key]}
                      onChange={(e) => setConfig({ ...config, [key]: parseInt(e.target.value) || 1 })}
                    />
                  </div>
                ))}
              </div>

              <div className="text-[10px] font-semibold uppercase tracking-widest pt-1" style={{ color: '#6e7681' }}>SLA Thresholds</div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-[11px] mb-1" style={{ color: '#8b949e' }}>Max P95 Latency (ms)</label>
                  <input type="number" min="1" className={inputCls} style={inputStyle}
                    value={config.max_p95_latency_ms}
                    onChange={(e) => setConfig({ ...config, max_p95_latency_ms: parseFloat(e.target.value) || 100 })}
                  />
                </div>
                <div>
                  <label className="block text-[11px] mb-1" style={{ color: '#8b949e' }}>Max Error Rate (%)</label>
                  <input type="number" step="0.1" min="0" max="100" className={inputCls} style={inputStyle}
                    value={config.max_error_rate_percent}
                    onChange={(e) => setConfig({ ...config, max_error_rate_percent: parseFloat(e.target.value) || 1.0 })}
                  />
                </div>
              </div>

              <div className="flex justify-end gap-2 pt-2" style={{ borderTop: '1px solid #30363d' }}>
                <button
                  type="button"
                  onClick={handleCloseModal}
                  className="rounded px-4 py-1.5 text-xs font-semibold"
                  style={{ color: '#8b949e', border: '1px solid #30363d' }}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={isSubmitting || !targetService || !targetURL}
                  className="rounded px-4 py-1.5 text-xs font-semibold disabled:opacity-40"
                  style={{ backgroundColor: '#f0883e', color: '#0d1117' }}
                >
                  {isSubmitting ? 'Creating…' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
