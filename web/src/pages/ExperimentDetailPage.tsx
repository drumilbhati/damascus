import React, { useState, useEffect, useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useExperiment, useExperimentStatus, useExperimentReport } from '../api/hooks';
import { startExperiment, stopExperiment } from '../api/endpoints';
import type { StartExperimentPayload } from '../types';
import { MetricsChart } from '../components/MetricsChart';
import {
  Play,
  Square,
  ArrowLeft,
  FileSpreadsheet,
} from 'lucide-react';

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

function defaultRequestForTarget(targetURL: string): string | null {
  const path = targetURL.toLowerCase();
  if (path.includes('/cartservice/additem')) {
    return JSON.stringify({ userId: 'damascus-load-test', item: { productId: 'OLJCESPC7Z', quantity: 1 } }, null, 2);
  }
  if (path.includes('/cartservice/emptycart')) {
    return JSON.stringify({ userId: 'damascus-load-test' }, null, 2);
  }
  if (path.includes('/shippingservice/shiporder')) {
    return JSON.stringify({ address: { streetAddress: '1600 Amphitheatre Parkway', city: 'Mountain View', state: 'CA', country: 'US', zipCode: '94043' }, items: [{ productId: 'OLJCESPC7Z', quantity: 1 }] }, null, 2);
  }
  if (path.includes('/paymentservice/charge')) {
    return JSON.stringify({ amount: { currencyCode: 'USD', units: 1 }, creditCard: { creditCardNumber: '4111111111111111', creditCardCvv: 123, creditCardExpirationYear: 2030, creditCardExpirationMonth: 12 } }, null, 2);
  }
  if (path.includes('/checkoutservice/placeorder')) {
    return JSON.stringify({ userId: 'damascus-load-test', userCurrency: 'USD', address: { streetAddress: '1600 Amphitheatre Parkway', city: 'Mountain View', state: 'CA', country: 'US', zipCode: '94043' }, email: 'load-test@example.com', creditCard: { creditCardNumber: '4111111111111111', creditCardCvv: 123, creditCardExpirationYear: 2030, creditCardExpirationMonth: 12 } }, null, 2);
  }
  if (path.includes('/emailservice/send_order_confirmation')) {
    return JSON.stringify({ email: 'load-test@example.com', order: { order_id: 'damascus-load-test', shipping_tracking_id: 'test-tracking', shipping_cost: { units: 1, nanos: 0, currency_code: 'USD' }, shipping_address: { street_address_1: '1600 Amphitheatre Parkway', street_address_2: '', city: 'Mountain View', country: 'US', zip_code: '94043' }, items: [{ item: { product_id: 'OLJCESPC7Z', quantity: 1 }, cost: { units: 1, nanos: 0, currency_code: 'USD' } }] } }, null, 2);
  }
  return null;
}

export const ExperimentDetailPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const { data: experiment, error: expError, mutate: mutateExp, isLoading } = useExperiment(id);
  const hasReport = experiment?.state === 'COMPLETED' || experiment?.state === 'ABORTED';
  const { data: report } = useExperimentReport(hasReport ? id : null);
  const history = useMemo(() => report?.observations.filter(o => !o.telemetry_missing).map(o => ({ ...o, experiment_id: report.experiment_id, target_service: report.target_service, request_rate: o.load_rate, offered_rate: o.injected_rate ?? undefined, p50_latency_ms: 0, p99_latency_ms: 0, cpu_utilization: o.cpu_utilization ?? 0, memory_utilization: o.memory_utilization ?? 0 })), [report]);
  const isRunning = experiment?.state === 'RUNNING';
  const { data: latestSnapshot, error: telemetryError, mutate: refreshTelemetry } = useExperimentStatus(
    experiment && experiment.state !== 'CREATED' ? id : null,
    Boolean(experiment && ['PLANNED', 'RUNNING', 'STOPPING', 'ANALYZING', 'REPORTING'].includes(experiment.state))
  );
  useEffect(() => {
    if (experiment?.state === 'COMPLETED' || experiment?.state === 'ABORTED') void refreshTelemetry();
  }, [experiment?.state, refreshTelemetry]);
  const snapshot = telemetryError ? undefined : latestSnapshot;

  const [actionError, setActionError] = useState<string | null>(null);
  const [isActing, setIsActing] = useState(false);
  const [requestMethod, setRequestMethod] = useState<'GET' | 'POST'>('GET');
  const [requestPayload, setRequestPayload] = useState('');

  useEffect(() => {
    const defaults = defaultRequestForTarget(experiment?.target_url ?? '');
    if (defaults) {
      setRequestMethod('POST');
      setRequestPayload((current) => current || defaults);
    }
  }, [experiment?.target_url]);

  const handleStart = async () => {
    if (!id || !experiment) return;
    if (requestMethod === 'POST' && requestPayload.trim() && !isValidJSON(requestPayload)) {
      setActionError('Request payload must be valid JSON');
      return;
    }
    if (requestMethod === 'POST' && !window.confirm('This same POST request and payload will be replayed at every scheduled request rate. It may create orders, alter carts, or trigger other demo side effects. Continue only against a test environment.')) {
      return;
    }
    setActionError(null);
    setIsActing(true);
    try {
      const plan: NonNullable<StartExperimentPayload['plan']> = {
        target_url: experiment.target_url,
        method: requestMethod,
      };
      if (requestMethod === 'POST' && requestPayload.trim()) plan.payload = requestPayload;
      await startExperiment(id, { plan });
      mutateExp();
    } catch (err: any) {
      setActionError(err.message || 'Failed to start experiment');
    } finally {
      setIsActing(false);
    }
  };

  const isValidJSON = (value: string) => {
    try {
      JSON.parse(value);
      return true;
    } catch {
      return false;
    }
  };

  const handleStop = async () => {
    if (!id) return;
    setActionError(null);
    setIsActing(true);
    try {
      await stopExperiment(id, { reason: 'manual emergency stop via web UI' });
      mutateExp();
    } catch (err: any) {
      setActionError(err.message || 'Failed to stop experiment');
    } finally {
      setIsActing(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-64 items-center justify-center text-xs" style={{ color: '#6e7681' }}>
        Loading experiment details...
      </div>
    );
  }

  if (expError || !experiment) {
    return (
      <div className="space-y-4">
        <Link
          to="/experiments"
          className="inline-flex items-center gap-1 text-xs font-semibold"
          style={{ color: '#58a6ff' }}
        >
          <ArrowLeft className="h-3.5 w-3.5" /> Back to experiments
        </Link>
        <div
          className="rounded p-5 text-center text-xs"
          style={{ backgroundColor: 'rgba(248,81,73,0.08)', border: '1px solid rgba(248,81,73,0.25)', color: '#f85149' }}
        >
          <p className="font-bold">Experiment not found or failed to load</p>
          <p className="mt-1" style={{ color: '#8b949e' }}>{expError?.message || 'Check experiment identifier'}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      {/* Breadcrumb + header */}
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div className="space-y-1">
          <Link
            to="/experiments"
            className="inline-flex items-center gap-1 text-[11px] font-semibold"
            style={{ color: '#58a6ff' }}
          >
            <ArrowLeft className="h-3.5 w-3.5" /> All Experiments
          </Link>
          <div className="flex items-center gap-2.5">
            <h1 className="text-base font-semibold" style={{ color: '#e6edf3' }}>
              {experiment.target_service}
            </h1>
            <StateBadge state={experiment.state} />
          </div>
          <p className="font-mono text-[10px]" style={{ color: '#6e7681' }}>{experiment.id}</p>
        </div>

        {/* Action Controls */}
        <div className="flex items-center gap-2">
          {experiment.state === 'CREATED' && (
            <button
              onClick={handleStart}
              disabled={isActing}
              className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold disabled:opacity-50"
              style={{ backgroundColor: '#238636', color: '#e6edf3' }}
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              Start Traffic Injection
            </button>
          )}

          {experiment.state === 'RUNNING' && (
            <button
              onClick={handleStop}
              disabled={isActing}
              className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold disabled:opacity-50"
              style={{ backgroundColor: 'rgba(248,81,73,0.15)', color: '#f85149', border: '1px solid rgba(248,81,73,0.4)' }}
            >
              <Square className="h-3.5 w-3.5 fill-current" />
              Emergency Stop
            </button>
          )}

          {(experiment.state === 'COMPLETED' || experiment.state === 'ABORTED') && (
            <Link
              to={`/reports?experiment=${id}`}
              className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold"
              style={{ backgroundColor: 'rgba(88,166,255,0.12)', color: '#58a6ff', border: '1px solid rgba(88,166,255,0.3)' }}
            >
              <FileSpreadsheet className="h-3.5 w-3.5" />
              View Analysis Report
            </Link>
          )}
        </div>
      </div>

      {actionError && (
        <div
          className="rounded p-3 text-xs font-semibold"
          style={{ backgroundColor: 'rgba(248,81,73,0.1)', border: '1px solid rgba(248,81,73,0.3)', color: '#f85149' }}
        >
          {actionError}
        </div>
      )}

      {/* Real-Time Telemetry */}
      {/\:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/i.test(experiment.target_url) && (
        <p role="alert" className="rounded border border-amber-700 p-3 text-xs text-amber-300">This experiment targets localhost. When the control plane runs in Docker, that address points to its own container. Create a new experiment with a reachable URL, such as http://frontend:8080/ for the demo shop.</p>
      )}
      {experiment.state === 'CREATED' ? (
        <div className="rounded border border-slate-700 p-5 text-sm text-slate-300 space-y-3">
          <p className="font-semibold">Ready to start</p>
          <p>No traffic has been sent yet. Choose the HTTP method and request body for the configured target, then start injection.</p>
          <div className="grid gap-3 sm:grid-cols-[140px_1fr]">
            <label className="text-xs" style={{ color: '#8b949e' }}>
              Request method
              <select value={requestMethod} onChange={(event) => setRequestMethod(event.target.value as 'GET' | 'POST')} className="mt-1 w-full rounded border border-slate-700 bg-slate-950 px-2 py-2 text-xs text-slate-200">
                <option value="GET">GET</option>
                <option value="POST">POST</option>
              </select>
            </label>
            {requestMethod === 'POST' && (
              <label className="text-xs" style={{ color: '#8b949e' }}>
                JSON request body (same body replayed for every request)
                <textarea value={requestPayload} onChange={(event) => setRequestPayload(event.target.value)} placeholder={'{\n  "from": {"currencyCode": "USD", "units": 1},\n  "toCode": "EUR"\n}'} rows={5} className="mt-1 w-full rounded border border-slate-700 bg-slate-950 px-2 py-2 font-mono text-xs text-slate-200" />
              </label>
            )}
          </div>
          {requestMethod === 'POST' && <p role="alert" className="rounded border border-amber-800 bg-amber-950/30 p-2 text-xs text-amber-300">POST bodies are replayed at the full configured RPS. Mutating calls can add cart items, charge simulated cards, send emails, or place orders. Use test-only data; every POST start requires confirmation.</p>}
          <p className="text-xs">GET operations may take a JSON request in the gateway URL’s <code>request</code> query parameter. POST is required for state-changing adapter operations.</p>
        </div>
      ) : (
      <>
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
            Latest recorded service sample
          </span>
          {isRunning && snapshot && (
            <span className="inline-flex items-center gap-1.5 text-[11px] font-semibold" style={{ color: '#3fb950' }}>
              <span className="relative flex h-1.5 w-1.5">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full opacity-75" style={{ backgroundColor: '#3fb950' }}></span>
                <span className="relative inline-flex h-1.5 w-1.5 rounded-full" style={{ backgroundColor: '#3fb950' }}></span>
              </span>
              Live · 5s metric polling
            </span>
          )}
        </div>

        {telemetryError && <p role="alert" className="rounded border border-red-800 p-3 text-xs text-red-300">Telemetry unavailable: {telemetryError.message}</p>}
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {/* Throughput */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>Measured service traffic (RPS)</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#e6edf3' }}>
              {snapshot ? snapshot.request_rate.toFixed(1) : '—'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>req/s · Offered target: {snapshot?.offered_rate == null ? 'Unavailable' : `${snapshot.offered_rate} req/s`}</div>
          </div>

          {/* P95 Latency */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>P95 Latency</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#bc8cff' }}>
              {snapshot ? snapshot.p95_latency_ms.toFixed(1) : '—'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
              ms · SLA &lt;{experiment.config.max_p95_latency_ms}ms
            </div>
          </div>

          {/* Error Rate */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>Error Rate</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#f85149' }}>
              {snapshot?.error_rate !== undefined ? `${(snapshot.error_rate * 100).toFixed(2)}` : '—'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
              % · max {experiment.config.max_error_rate_percent}%
            </div>
          </div>

          {/* Availability */}
          <div className="rounded p-4" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
            <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>Availability</div>
            <div className="mt-2 font-mono text-2xl font-bold" style={{ color: '#3fb950' }}>
              {snapshot?.availability !== undefined ? `${(snapshot.availability * 100).toFixed(2)}` : '—'}
            </div>
            <div className="mt-0.5 font-mono text-[11px]" style={{ color: '#6e7681' }}>
              % · min {experiment.config.min_availability_pct}%
            </div>
          </div>
        </div>
      </div>

      {/* Live Telemetry Streaming Chart */}
      <MetricsChart
        key={id}
        history={history}
        snapshot={snapshot}
        isRunning={isRunning}
        telemetryUnavailable={Boolean(telemetryError) || (isRunning && !snapshot)}
        config={experiment.config}
      />
      </>
      )}

      {/* Config detail grids */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Traffic Plan */}
        <div className="rounded p-4 space-y-3" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
            Traffic Plan Specifications
          </div>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
            <div>
              <dt style={{ color: '#8b949e' }}>Target Endpoint</dt>
              <dd className="mt-0.5 font-mono font-semibold truncate" style={{ color: '#e6edf3' }}>
                {experiment.target_url}
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Injection Pattern</dt>
              <dd className="mt-0.5 font-mono font-semibold uppercase" style={{ color: '#e6edf3' }}>
                {experiment.type}
              </dd>
            </div>
            {experiment.type === 'fixed' ? (
              <div>
                <dt style={{ color: '#8b949e' }}>Fixed Rate</dt>
                <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                  {experiment.config.initial_rate} req/s
                </dd>
              </div>
            ) : (
              <>
                <div>
                  <dt style={{ color: '#8b949e' }}>Initial RPS</dt>
                  <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                    {experiment.config.initial_rate} req/s
                  </dd>
                </div>
                <div>
                  <dt style={{ color: '#8b949e' }}>Max Ceiling</dt>
                  <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                    {experiment.config.max_rate} req/s
                  </dd>
                </div>
                <div>
                  <dt style={{ color: '#8b949e' }}>Step Rate Increment</dt>
                  <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                    +{experiment.config.step_rate} req/s
                  </dd>
                </div>
              </>
            )}
            <div>
              <dt style={{ color: '#8b949e' }}>{experiment.type === 'fixed' ? 'Run Duration' : 'Step Duration'}</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.step_duration_seconds}s
              </dd>
            </div>
          </dl>
        </div>

        {/* Safety Brakes */}
        <div className="rounded p-4 space-y-3" style={{ backgroundColor: '#161b22', border: '1px solid #30363d' }}>
          <div className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
            Safety Brakes &amp; Recovery Window
          </div>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
            <div>
              <dt style={{ color: '#8b949e' }}>Max P95 Latency SLA</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.max_p95_latency_ms} ms
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Max Error Rate SLA</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.max_error_rate_percent}%
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Minimum Availability</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.min_availability_pct}%
              </dd>
            </div>
            <div>
              <dt style={{ color: '#8b949e' }}>Recovery Wait</dt>
              <dd className="mt-0.5 font-mono font-semibold" style={{ color: '#e6edf3' }}>
                {experiment.config.recovery_window_sec}s
              </dd>
            </div>
          </dl>

          {experiment.stop_reason && (
            <div
              className="rounded p-3 text-[11px]"
              style={{ backgroundColor: 'rgba(210,153,34,0.1)', border: '1px solid rgba(210,153,34,0.3)', color: '#d29922' }}
            >
              <span className="font-bold">Interruption Reason: </span>
              {experiment.stop_reason}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
