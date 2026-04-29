'use client';

import { useEffect, useState } from 'react';
import { getPortalSummary, type PortalSummary } from '@/lib/ironsight-api';

const RANGES: { label: string; days: number }[] = [
  { label: 'Last 7 days', days: 7 },
  { label: 'Last 30 days', days: 30 },
  { label: 'Last 90 days', days: 90 },
];

function fmtSec(sec: number): string {
  if (!sec || sec < 1) return '—';
  if (sec < 60) return `${Math.round(sec)}s`;
  const m = Math.floor(sec / 60);
  const s = Math.round(sec - m * 60);
  return s === 0 ? `${m}m` : `${m}m ${s}s`;
}

export default function HandledForYouPanel() {
  const [days, setDays] = useState(7);
  const [data, setData] = useState<PortalSummary | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getPortalSummary(days)
      .then((d) => { if (!cancelled) setData(d); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [days]);

  const slaTotal = (data?.within_sla ?? 0) + (data?.over_sla ?? 0);
  const slaPct = slaTotal > 0 ? Math.round(((data?.within_sla ?? 0) / slaTotal) * 100) : null;

  return (
    <div style={{
      background: 'var(--surface-1, #fafaf7)',
      border: '1px solid var(--border, rgba(0,0,0,0.08))',
      borderRadius: 8,
      padding: 20,
      marginBottom: 16,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-primary, #1a1a1a)' }}>
            What the SOC handled for you
          </div>
          <div style={{ fontSize: 11, color: 'var(--text-dim, #666)', marginTop: 2 }}>
            The events the SOC reviewed and resolved on your behalf. You weren't paged.
          </div>
        </div>
        <div style={{ display: 'flex', gap: 4 }}>
          {RANGES.map((r) => (
            <button
              key={r.days}
              onClick={() => setDays(r.days)}
              style={{
                padding: '4px 10px', fontSize: 11, fontWeight: 600,
                borderRadius: 4, cursor: 'pointer',
                border: '1px solid var(--border, rgba(0,0,0,0.10))',
                background: days === r.days ? 'var(--brand-primary, #E8732A)' : 'transparent',
                color: days === r.days ? '#fff' : 'var(--text-dim, #666)',
                fontFamily: 'inherit',
              }}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {loading && !data && (
        <div style={{ padding: 24, textAlign: 'center', fontSize: 12, color: 'var(--text-dim, #888)' }}>
          Loading…
        </div>
      )}

      {data && (
        <>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12 }}>
            <Stat
              value={data.events_handled}
              label="Events handled"
              hint="reviewed by an operator"
              accent="#E8732A"
            />
            <Stat
              value={data.false_positives}
              label="False positives we filtered"
              hint={data.events_handled > 0
                ? `${Math.round((data.false_positives / data.events_handled) * 100)}% of events — never reached you`
                : 'never reached you'}
              accent="#7a8a99"
            />
            <Stat
              value={data.verified_threats}
              label="Verified threats"
              hint={data.verified_threats === 0 ? 'a quiet period' : 'escalated and resolved'}
              accent="#c0311a"
            />
            <Stat
              value={fmtSec(data.avg_response_sec)}
              label="Average response"
              hint={slaPct !== null ? `${slaPct}% within SLA` : 'no eligible alarms'}
              accent="#1a6f4f"
            />
          </div>

          {data.events_handled === 0 && (
            <div style={{
              marginTop: 14, padding: 10, fontSize: 12,
              color: 'var(--text-dim, #666)', textAlign: 'center',
              background: 'rgba(0,0,0,0.02)', borderRadius: 4,
            }}>
              Nothing happened at your sites in this window. The cameras were watching.
            </div>
          )}
        </>
      )}
    </div>
  );
}

function Stat({ value, label, hint, accent }: {
  value: number | string;
  label: string;
  hint: string;
  accent: string;
}) {
  return (
    <div>
      <div style={{
        fontSize: 28, fontWeight: 700, lineHeight: 1.1,
        color: accent, fontFamily: "'JetBrains Mono', monospace",
      }}>
        {value}
      </div>
      <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-primary, #1a1a1a)', marginTop: 4 }}>
        {label}
      </div>
      <div style={{ fontSize: 10, color: 'var(--text-dim, #888)', marginTop: 1 }}>
        {hint}
      </div>
    </div>
  );
}
