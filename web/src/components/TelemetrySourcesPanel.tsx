import React, { useEffect, useState } from 'react';
import { CheckCircle2, Plug } from 'lucide-react';
import { useTelemetrySettings } from '../api/hooks';
import { updateTelemetrySettings } from '../api/endpoints';
import type { TelemetryErrorResponse, TelemetrySources } from '../types';

const inputCls = 'w-full rounded px-3 py-1.5 text-xs font-mono outline-none focus:ring-1 focus:ring-[#f0883e]';
const inputStyle = { backgroundColor: '#0d1117', border: '1px solid #30363d', color: '#e6edf3' };

interface Props {
  /** Called after both sources were verified and applied. */
  onApplied?: () => void;
  /** Reports whether the inputs differ from the sources in use. */
  onDirtyChange?: (dirty: boolean) => void;
}

const FIELDS: { key: keyof TelemetrySources; label: string; placeholder: string; help: string }[] = [
  {
    key: 'jaeger_url',
    label: 'Jaeger traces API',
    placeholder: 'http://jaeger:16686',
    help: 'Jaeger query service base URL (serves /api/services and /api/traces)',
  },
  {
    key: 'prometheus_url',
    label: 'Prometheus metrics API',
    placeholder: 'http://prometheus:9090',
    help: 'Prometheus server that scrapes the target service (serves /api/v1/query)',
  },
];

export const TelemetrySourcesPanel: React.FC<Props> = ({ onApplied, onDirtyChange }) => {
  const { data: settings, error: loadError, mutate } = useTelemetrySettings();
  const [values, setValues] = useState<TelemetrySources>({ jaeger_url: '', prometheus_url: '' });
  const [loadedFrom, setLoadedFrom] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<TelemetryErrorResponse['fields']>({});
  const [message, setMessage] = useState<{ kind: 'ok' | 'error'; text: string } | null>(null);
  const [saving, setSaving] = useState(false);

  // Fill the inputs once from the server; never overwrite the user's edits.
  const settingsKey = settings ? `${settings.jaeger_url}|${settings.prometheus_url}` : null;
  if (settings && settingsKey !== loadedFrom) {
    setLoadedFrom(settingsKey);
    setValues({ jaeger_url: settings.jaeger_url, prometheus_url: settings.prometheus_url });
  }

  const dirty = Boolean(
    settings && (values.jaeger_url.trim() !== settings.jaeger_url || values.prometheus_url.trim() !== settings.prometheus_url),
  );
  useEffect(() => onDirtyChange?.(dirty), [dirty, onDirtyChange]);

  const apply = async () => {
    setSaving(true);
    setMessage(null);
    setFieldErrors({});
    try {
      const next = await updateTelemetrySettings({
        jaeger_url: values.jaeger_url.trim(),
        prometheus_url: values.prometheus_url.trim(),
      });
      await mutate(next, { revalidate: false });
      setMessage({ kind: 'ok', text: 'Both APIs answered; Damascus now reads traces and metrics from these sources.' });
      onApplied?.();
    } catch (err) {
      const data = (err as { response?: { data?: TelemetryErrorResponse } })?.response?.data;
      setFieldErrors(data?.fields ?? {});
      setMessage({ kind: 'error', text: data?.fields ? 'Fix the highlighted source and try again.' : data?.error ?? 'Could not update telemetry sources.' });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="rounded p-3 space-y-3" style={{ border: '1px solid #30363d', backgroundColor: '#0d1117' }}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-[11px] font-semibold" style={{ color: '#e6edf3' }}>Telemetry sources</span>
        {settings && (
          <span className="text-[10px]" style={{ color: '#6e7681' }}>
            {settings.origin === 'ui' && settings.updated_at
              ? `set from UI ${new Date(settings.updated_at).toLocaleString()}`
              : 'defaults from .env'}
          </span>
        )}
      </div>

      {loadError && !settings && (
        <p className="text-xs" style={{ color: '#f85149' }}>Telemetry settings are unavailable from the control plane.</p>
      )}

      {FIELDS.map(({ key, label, placeholder, help }) => (
        <div key={key}>
          <label htmlFor={`telemetry-${key}`} className="block text-[11px] font-semibold mb-1" style={{ color: '#8b949e' }}>
            {label}
          </label>
          <input
            id={`telemetry-${key}`}
            className={inputCls}
            style={{ ...inputStyle, border: fieldErrors?.[key] ? '1px solid #f85149' : inputStyle.border }}
            placeholder={placeholder}
            value={values[key]}
            onChange={(e) => setValues((v) => ({ ...v, [key]: e.target.value }))}
            aria-invalid={Boolean(fieldErrors?.[key])}
            aria-describedby={`telemetry-${key}-help`}
          />
          <p id={`telemetry-${key}-help`} className="mt-1 text-[11px]" style={{ color: fieldErrors?.[key] ? '#f85149' : '#6e7681' }}>
            {fieldErrors?.[key] ?? help}
          </p>
        </div>
      ))}

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={apply}
          disabled={saving || !settings || !dirty}
          className="inline-flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-semibold disabled:opacity-50"
          style={{ backgroundColor: '#21262d', border: '1px solid #30363d', color: '#e6edf3' }}
        >
          <Plug className="h-3 w-3" />
          {saving ? 'Testing…' : 'Test & connect'}
        </button>
        {message && (
          <span role="status" className="inline-flex items-center gap-1 text-[11px]" style={{ color: message.kind === 'ok' ? '#3fb950' : '#f85149' }}>
            {message.kind === 'ok' && <CheckCircle2 className="h-3 w-3" />}
            {message.text}
          </span>
        )}
        {!message && dirty && (
          <span className="text-[11px]" style={{ color: '#d29922' }}>Unsaved: test & connect to use these sources.</span>
        )}
      </div>
      <p className="text-[11px]" style={{ color: '#6e7681' }}>
        Use URLs reachable from the control-plane container (Docker DNS names such as http://jaeger:16686, not localhost).
        Changes apply to every experiment and auto-run until the control plane restarts.
      </p>
    </div>
  );
};
