import React, { useState, useMemo } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useExperiments, useDependencies } from '../api/hooks';
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
  const [searchParams, setSearchParams] = useSearchParams();
  const targetParam = searchParams.get('target');

  const { data: experiments, mutate, isLoading } = useExperiments();
  const [filterState, setFilterState] = useState<string>('ALL');
  const [showModal, setShowModal] = useState(Boolean(targetParam));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  // An explicit user choice overrides the current score recommendation.
  const [targetURL, setTargetURL] = useState('');
  const [targetService, setTargetService] = useState(targetParam || '');

  // Adjust state during render when target query parameter in URL changes
  const [prevTargetParam, setPrevTargetParam] = useState(targetParam);
  if (targetParam !== prevTargetParam) {
    setPrevTargetParam(targetParam);
    if (targetParam) {
      setTargetService(targetParam);
      setShowModal(true);
    }
  }

  const [expType, setExpType] = useState<ExperimentType>('ramp');
  const [config, setConfig] = useState<ExperimentConfig>({
    initial_rate: 1,
    step_rate: 1,
    max_rate: 5,
    step_duration_seconds: 15,
    max_p95_latency_ms: 1000,
    max_error_rate_percent: 5,
    min_availability_pct: 95,
    recovery_window_sec: 10,
  });

  const { data: dependencies, isLoading: loadingServices, isValidating: refreshingServices, error: servicesError, mutate: refetchServices } = useDependencies(3600, showModal);
  const [refreshMessage, setRefreshMessage] = useState('');
  const refreshRecommendation = async () => {
    setRefreshMessage('');
    try {
      await refetchServices();
      setRefreshMessage(`Scores refreshed at ${new Date().toLocaleTimeString()}. Rankings may remain unchanged when the observed topology is unchanged.`);
    } catch (error) {
      setRefreshMessage(`Refresh failed: ${error instanceof Error ? error.message : 'Unable to fetch scores'}`);
    }
  };
  const rankedScores = useMemo(() => (dependencies?.scores ?? []).slice().sort(
    (a, b) => b.score - a.score || a.service_name.localeCompare(b.service_name)
  ), [dependencies?.scores]);
  const availableServices = useMemo(() => Object.keys(dependencies?.graph?.nodes ?? {}).sort(), [dependencies?.graph?.nodes]);
  const recommendation = rankedScores.find((score) => score.score > 0 && availableServices.includes(score.service_name));
  const scoresByService = new Map(rankedScores.map((score) => [score.service_name, score]));

  const handleOpenModal = () => {
    setTargetURL('');
    setTargetService('');
    setFormError(null);
    setRefreshMessage('');
    setShowModal(true);
  };

  const handleCloseModal = () => {
    setShowModal(false);
    if (searchParams.has('target')) {
      const nextParams = new URLSearchParams(searchParams);
      nextParams.delete('target');
      setSearchParams(nextParams, { replace: true });
    }
  };

  const effectiveTargetService = targetService || recommendation?.service_name || '';

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!effectiveTargetService) {
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
      await createExperiment({ target_service: effectiveTargetService, target_url: targetURL, type: expType, config });
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
          <div className="flex max-h-[90dvh] w-full max-w-lg flex-col rounded-lg overflow-hidden" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            {/* Modal header */}
            <div className="flex shrink-0 items-center justify-between px-5 py-3" style={{ borderBottom: '1px solid #30363d' }}>
              <span className="text-sm font-semibold" style={{ color: '#e6edf3' }}>New Experiment</span>
              <button onClick={handleCloseModal} className="rounded p-1" style={{ color: '#8b949e' }}>
                <X className="h-4 w-4" />
              </button>
            </div>

            <form onSubmit={handleCreate} className="overflow-y-auto p-5 space-y-4">
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
                  placeholder="http://frontend:8080/"
                  value={targetURL}
                  onChange={(e) => setTargetURL(e.target.value)}
                  required
                />
                <p className="mt-1 text-xs" style={{ color: '#8b949e' }}>
                  Use a URL reachable by the control plane. For the Docker demo shop, use http://frontend:8080/.
                  For a service running on your Mac, use host.docker.internal instead of localhost.
                </p>
              </div>

              {/* Step 2: Service dropdown (from Jaeger) */}
              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="text-[11px] font-semibold" style={{ color: '#8b949e' }}>
                    Target Service
                    <span className="ml-1.5 font-normal" style={{ color: '#6e7681' }}>
                      — recommended by criticality score
                    </span>
                  </label>
                  <button
                    type="button"
                    onClick={refreshRecommendation}
                    disabled={refreshingServices}
                    className="flex items-center gap-1 text-[10px]"
                    style={{ color: '#6e7681' }}
                    title="Refresh services and criticality scores"
                  >
                    <RefreshCw className={`h-3 w-3 ${refreshingServices ? 'animate-spin' : ''}`} />
                    {refreshingServices ? 'Refreshing…' : 'Refresh'}
                  </button>
                </div>

                {refreshMessage && <p role="status" className="mb-2 text-xs" style={{ color: '#8b949e' }}>{refreshMessage}</p>}
                <p className="mb-2 text-xs" style={{ color: '#8b949e' }}>Ranking uses existing cluster traces from the last hour. The URL specifies where test traffic will be sent; the selected service specifies which telemetry to monitor.</p>
                <p className="mb-2 text-xs" style={{ color: '#8b949e' }}>Traffic rates use the retrieved trace sample, which may cover only part of the traffic.</p>
                {loadingServices ? (
                  <div
                    className="flex items-center gap-2 rounded px-3 py-2 text-xs"
                    style={{ backgroundColor: '#0d1117', border: '1px solid #30363d', color: '#6e7681' }}
                  >
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    Calculating service criticality from Jaeger traces…
                  </div>
                ) : servicesError ? (
                  <div className="space-y-2">
                    <div
                      className="flex items-center gap-2 rounded px-3 py-2 text-xs"
                      style={{ backgroundColor: 'rgba(248,81,73,0.08)', border: '1px solid rgba(248,81,73,0.25)', color: '#f85149' }}
                    >
                      <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                      Could not calculate a recommendation — enter service name manually
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
                    value={effectiveTargetService}
                    onChange={(e) => setTargetService(e.target.value)}
                    required
                  >
                    <option value="">— select a service —</option>
                    {effectiveTargetService && !availableServices.includes(effectiveTargetService) && (
                      <option value={effectiveTargetService}>{effectiveTargetService} (targeted)</option>
                    )}
                    {availableServices.slice().sort((a, b) => (scoresByService.get(b)?.score ?? 0) - (scoresByService.get(a)?.score ?? 0) || a.localeCompare(b)).map((svc) => (
                      <option key={svc} value={svc}>
                        {svc}{scoresByService.has(svc) ? ` — ${((scoresByService.get(svc)?.score ?? 0) * 100).toFixed(1)}%` : ''}{svc === recommendation?.service_name ? ' (recommended)' : ''}
                      </option>
                    ))}
                  </select>
                )}

                {recommendation && !servicesError && (
                  <div className="mt-2 rounded p-3 text-xs" style={{ backgroundColor: '#21262d', color: '#8b949e' }}>
                    <p>Recommended: <strong style={{ color: '#f0883e' }}>{recommendation.service_name}</strong> ({(recommendation.score * 100).toFixed(1)}% criticality), the highest calculated score.</p>
                    <ul className="mt-1 list-disc pl-4">
                      {recommendation.reasons.map((reason, index) => <li key={index}>{reason}</li>)}
                    </ul>
                    {targetService && targetService !== recommendation.service_name ? (
                      <p className="mt-2">Override selected. <button type="button" className="underline" onClick={() => setTargetService('')}>Use recommended service</button></p>
                    ) : <p className="mt-2">Choose another service above to override the recommendation.</p>}
                  </div>
                )}
                {!recommendation && !loadingServices && !servicesError && availableServices.length > 0 && (
                  <p className="mt-2 text-xs" style={{ color: '#d29922' }}>No positive criticality scores are available yet. Generate traffic and refresh, or select a service manually.</p>
                )}

                {effectiveTargetService && !servicesError && availableServices.length > 0 && (
                  <p className="mt-1 font-mono text-[10px]" style={{ color: '#6e7681' }}>
                    selected: <span style={{ color: '#f0883e' }}>{effectiveTargetService}</span>
                    &nbsp;·&nbsp;{availableServices.length} service{availableServices.length !== 1 ? 's' : ''} in mesh
                  </p>
                )}
              </div>

              {/* Strategy */}
              <div>
                <label className="block text-[11px] font-semibold mb-1" style={{ color: '#8b949e' }}>Strategy</label>
                <select className={inputCls} style={inputStyle} value={expType} onChange={(e) => setExpType(e.target.value as ExperimentType)}>
                  <option value="ramp">ramp — linearly increase load step by step</option>
                  <option value="step">step — increase load at each configured interval</option>
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
              <div>
                <label className="block text-[11px] mb-1" style={{ color: '#8b949e' }}>Step Duration (seconds)</label>
                <input type="number" min="1" required className={inputCls} style={inputStyle}
                  value={config.step_duration_seconds}
                  onChange={(e) => setConfig({ ...config, step_duration_seconds: Number(e.target.value) })}
                />
              </div>
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
                    onChange={(e) => setConfig({ ...config, max_error_rate_percent: Number(e.target.value) })}
                  />
                </div>
                <div>
                  <label className="block text-[11px] mb-1" style={{ color: '#8b949e' }}>Minimum Availability (%)</label>
                  <input type="number" min="0" max="100" step="0.1" required className={inputCls} style={inputStyle}
                    value={config.min_availability_pct}
                    onChange={(e) => setConfig({ ...config, min_availability_pct: Number(e.target.value) })}
                  />
                </div>
                <div>
                  <label className="block text-[11px] mb-1" style={{ color: '#8b949e' }}>Recovery Window (seconds)</label>
                  <input type="number" min="1" required className={inputCls} style={inputStyle}
                    value={config.recovery_window_sec}
                    onChange={(e) => setConfig({ ...config, recovery_window_sec: Number(e.target.value) })}
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
                  disabled={isSubmitting || !effectiveTargetService || !targetURL}
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
