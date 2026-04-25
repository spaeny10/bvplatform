'use client';

import { useEffect, useState } from 'react';
import { BRAND } from '@/lib/branding';

// Public service-status page. Unauthenticated — anyone with the URL
// can see whether the SOC is up and operating normally. This is the
// page customers reach for when they're worried; making it
// log-in-required would defeat the whole point.
//
// Data comes from GET /status (no auth header sent), refreshed every
// 30s while the tab is in the foreground. The headline indicator is
// one of three states (operational / degraded / critical), derived
// server-side so a future business decision about thresholds doesn't
// need a frontend deploy.

interface StatusResponse {
  status: 'operational' | 'degraded' | 'critical';
  soc_active: boolean;
  cameras_total: number;
  cameras_online: number;
  alarms_last_hour: number;
  last_disposition: string | null;
  as_of: string;
}

const STATUS_DISPLAY: Record<StatusResponse['status'], { label: string; color: string; bg: string }> = {
  operational: { label: 'All systems operational', color: '#16a34a', bg: 'rgba(22,163,74,0.10)' },
  degraded:    { label: 'Service degraded',         color: '#ca8a04', bg: 'rgba(202,138,4,0.10)' },
  critical:    { label: 'Major service impact',     color: '#dc2626', bg: 'rgba(220,38,38,0.10)' },
};

function fmtRelative(iso: string | null): string {
  if (!iso) return 'never';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return 'unknown';
  const diff = Date.now() - d.getTime();
  if (diff < 60_000) return 'just now';
  const min = Math.floor(diff / 60_000);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

export default function StatusPage() {
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [err, setErr] = useState('');
  const [lastFetched, setLastFetched] = useState<number>(0);

  const load = async () => {
    try {
      const res = await fetch('/api/status');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = (await res.json()) as StatusResponse;
      setStatus(data);
      setErr('');
      setLastFetched(Date.now());
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, []);

  const display = status ? STATUS_DISPLAY[status.status] : null;
  const onlinePct = status && status.cameras_total > 0
    ? Math.round((status.cameras_online / status.cameras_total) * 100)
    : 100;

  return (
    <div style={{
      minHeight: '100vh',
      background: 'var(--sg-bg-base, #0c1015)',
      color: 'var(--sg-text-primary, #E4E8F0)',
      padding: '40px 20px',
      fontFamily: "var(--font-family, 'Inter', sans-serif)",
    }}>
      <div style={{ maxWidth: 720, margin: '0 auto' }}>
        <div style={{ marginBottom: 32, textAlign: 'center' }}>
          <div style={{ fontSize: 22, fontWeight: 700, marginBottom: 4, letterSpacing: 0.3 }}>
            {BRAND.name} Service Status
          </div>
          <div style={{ fontSize: 12, color: 'var(--sg-text-dim, #9CA3AF)' }}>
            {lastFetched > 0 ? `Updated ${fmtRelative(new Date(lastFetched).toISOString())}` : 'Loading…'}
          </div>
        </div>

        {err && (
          <div style={{
            padding: '14px 18px', marginBottom: 24,
            background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.25)',
            borderRadius: 6, color: '#EF9B8B',
          }}>
            ⚠ Status feed unreachable. Last known status may be stale.
          </div>
        )}

        {status && display && (
          <>
            <div style={{
              padding: '24px 28px',
              borderRadius: 10,
              background: display.bg,
              border: `1px solid ${display.color}40`,
              marginBottom: 24,
              display: 'flex', alignItems: 'center', gap: 16,
            }}>
              <div style={{
                width: 14, height: 14, borderRadius: 7,
                background: display.color,
                boxShadow: `0 0 12px ${display.color}80`,
                flexShrink: 0,
              }} />
              <div style={{ flex: 1 }}>
                <div style={{ fontSize: 18, fontWeight: 700, color: display.color, marginBottom: 2 }}>
                  {display.label}
                </div>
                <div style={{ fontSize: 12, color: 'var(--sg-text-dim, #9CA3AF)' }}>
                  As of {new Date(status.as_of).toLocaleString()}
                </div>
              </div>
            </div>

            <div style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
              gap: 12,
            }}>
              <StatCard
                label="SOC monitoring"
                value={status.soc_active ? 'Active' : 'Offline'}
                hint={status.soc_active ? 'Operators on duty' : 'No operator activity in 30 minutes'}
                ok={status.soc_active}
              />
              <StatCard
                label="Camera fleet"
                value={`${status.cameras_online} / ${status.cameras_total} online`}
                hint={`${onlinePct}% of cameras reporting`}
                ok={onlinePct >= 90}
              />
              <StatCard
                label="Alarms (last hour)"
                value={String(status.alarms_last_hour)}
                hint={status.alarms_last_hour === 0 ? 'No incidents' : 'Active monitoring'}
                ok={true}
              />
              <StatCard
                label="Last disposition"
                value={fmtRelative(status.last_disposition)}
                hint="Most recent operator action"
                ok={true}
              />
            </div>

            <div style={{
              marginTop: 32, padding: '16px 20px',
              background: 'var(--sg-surface-1, rgba(255,255,255,0.02))',
              border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
              borderRadius: 8,
              fontSize: 12, color: 'var(--sg-text-dim, #9CA3AF)', lineHeight: 1.6,
            }}>
              This page reports the {BRAND.name} platform's overall health.
              Per-site health (your specific cameras, recordings, and incidents)
              is visible inside your customer portal after sign-in. If you're
              experiencing an outage that this page doesn't reflect, please
              contact your account manager.
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function StatCard({ label, value, hint, ok }: { label: string; value: string; hint: string; ok: boolean }) {
  return (
    <div style={{
      padding: 16,
      background: 'var(--sg-surface-1, rgba(255,255,255,0.02))',
      border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
      borderRadius: 8,
    }}>
      <div style={{ fontSize: 10, fontWeight: 600, letterSpacing: 0.5,
                    color: 'var(--sg-text-dim, #9CA3AF)', textTransform: 'uppercase', marginBottom: 6 }}>
        {label}
      </div>
      <div style={{ fontSize: 18, fontWeight: 700, color: ok ? 'var(--sg-text-primary, #E4E8F0)' : '#ca8a04', marginBottom: 4 }}>
        {value}
      </div>
      <div style={{ fontSize: 11, color: 'var(--sg-text-dim, #6B7280)' }}>
        {hint}
      </div>
    </div>
  );
}
