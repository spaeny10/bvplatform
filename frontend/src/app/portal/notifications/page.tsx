'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { fetchJSON } from '@/lib/ironsight-api';
import { BRAND } from '@/lib/branding';

// Per-customer notification preferences page. Lives under /portal so
// the customer reaches it from their normal navigation; supervisor
// and admin users can also reach it via the same URL since the route
// guard on /portal already covers all monitoring-side roles.
//
// The 2-channel × 2-event matrix is rendered as four toggle cards
// rather than a table — easier to scan on mobile, and each card has
// room for the "minimum severity" select that's specific to email
// alarm-disposition (the most-common knob a customer actually
// changes).

interface Subscription {
  id?: number;
  user_id?: string;
  channel: 'email' | 'sms';
  event_type: 'alarm_disposition' | 'monthly_summary';
  severity_min: 'low' | 'medium' | 'high' | 'critical';
  enabled: boolean;
  quiet_start?: string;
  quiet_end?: string;
}

const ROWS: Array<{
  channel: 'email' | 'sms';
  event_type: 'alarm_disposition' | 'monthly_summary';
  title: string;
  subtitle: string;
  hasSeverity: boolean;
}> = [
  {
    channel: 'email',
    event_type: 'alarm_disposition',
    title: 'Email me when an alarm is dispositioned',
    subtitle: 'Full incident summary including AI description, operator notes, and a link to the recording.',
    hasSeverity: true,
  },
  {
    channel: 'sms',
    event_type: 'alarm_disposition',
    title: 'SMS me when an alarm is dispositioned',
    subtitle: 'Compact one-liner with severity, site, and the AI-generated scene description. For high/critical events.',
    hasSeverity: true,
  },
  {
    channel: 'email',
    event_type: 'monthly_summary',
    title: 'Email me a monthly summary',
    subtitle: 'On the 1st of every month: total alarms, response times, top events, and recording uptime.',
    hasSeverity: false,
  },
];

const SEVERITY_OPTIONS: Array<{ value: Subscription['severity_min']; label: string }> = [
  { value: 'low',      label: 'All alarms (low and above)' },
  { value: 'medium',   label: 'Medium and above' },
  { value: 'high',     label: 'High and critical only' },
  { value: 'critical', label: 'Critical only' },
];

function findSub(subs: Subscription[], channel: string, eventType: string): Subscription {
  const found = subs.find(s => s.channel === channel && s.event_type === eventType);
  if (found) return found;
  return {
    channel: channel as Subscription['channel'],
    event_type: eventType as Subscription['event_type'],
    severity_min: 'low',
    enabled: false,
  };
}

export default function NotificationPrefsPage() {
  const [subs, setSubs] = useState<Subscription[] | null>(null);
  const [savingKey, setSavingKey] = useState<string>('');
  const [err, setErr] = useState('');
  const [savedAt, setSavedAt] = useState<number>(0);

  const load = async () => {
    try {
      const data = await fetchJSON<Subscription[]>('/api/me/notifications');
      setSubs(data ?? []);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  useEffect(() => { load(); }, []);

  const saveSub = async (next: Subscription) => {
    const key = `${next.channel}-${next.event_type}`;
    setSavingKey(key);
    setErr('');
    try {
      await fetchJSON('/api/me/notifications', {
        method: 'PUT',
        body: JSON.stringify({
          channel: next.channel,
          event_type: next.event_type,
          severity_min: next.severity_min,
          enabled: next.enabled,
          site_ids: null,
          quiet_start: next.quiet_start ?? '',
          quiet_end: next.quiet_end ?? '',
        }),
      });
      setSavedAt(Date.now());
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSavingKey('');
    }
  };

  return (
    <div style={{
      minHeight: '100vh',
      background: 'var(--sg-bg-base, #0c1015)',
      color: 'var(--sg-text-primary, #E4E8F0)',
      padding: 24,
    }}>
      <div style={{ maxWidth: 720, margin: '0 auto' }}>
        <div style={{ marginBottom: 8, fontSize: 12 }}>
          <Link href="/portal" style={{ color: 'var(--sg-text-dim, #9CA3AF)', textDecoration: 'none' }}>
            ← Back to portal
          </Link>
        </div>
        <h1 style={{ fontSize: 22, fontWeight: 700, margin: '0 0 4px' }}>
          Notification preferences
        </h1>
        <p style={{ fontSize: 13, color: 'var(--sg-text-dim, #9CA3AF)', margin: '0 0 24px', lineHeight: 1.5 }}>
          Choose how {BRAND.name} reaches you when something happens at your sites.
          Changes save automatically.
        </p>

        {err && (
          <div style={{
            padding: '10px 14px', marginBottom: 16,
            background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.25)',
            borderRadius: 4, color: '#EF9B8B', fontSize: 12,
          }}>
            ⚠ {err}
          </div>
        )}

        {savedAt > 0 && Date.now() - savedAt < 3000 && (
          <div style={{
            padding: '8px 14px', marginBottom: 16,
            background: 'rgba(132,204,22,0.08)', border: '1px solid rgba(132,204,22,0.25)',
            borderRadius: 4, color: '#A3E635', fontSize: 12,
          }}>
            ✓ Saved
          </div>
        )}

        {subs === null ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--sg-text-dim, #9CA3AF)' }}>
            Loading…
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            {ROWS.map((row) => {
              const sub = findSub(subs, row.channel, row.event_type);
              const key = `${row.channel}-${row.event_type}`;
              const saving = savingKey === key;
              return (
                <div
                  key={key}
                  style={{
                    padding: 16,
                    background: 'var(--sg-surface-1, rgba(255,255,255,0.02))',
                    border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
                    borderRadius: 8,
                    opacity: saving ? 0.65 : 1,
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'flex-start', gap: 16, marginBottom: row.hasSeverity ? 12 : 0 }}>
                    <div style={{ flex: 1 }}>
                      <div style={{ fontSize: 14, fontWeight: 600, marginBottom: 4 }}>
                        {row.title}
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--sg-text-dim, #9CA3AF)', lineHeight: 1.5 }}>
                        {row.subtitle}
                      </div>
                    </div>
                    <button
                      type="button"
                      onClick={() => saveSub({ ...sub, enabled: !sub.enabled })}
                      disabled={saving}
                      style={{
                        position: 'relative',
                        width: 44, height: 24, flexShrink: 0,
                        borderRadius: 12, border: 'none',
                        background: sub.enabled ? '#84CC16' : 'rgba(255,255,255,0.12)',
                        cursor: saving ? 'wait' : 'pointer',
                        transition: 'background 0.15s',
                      }}
                      aria-pressed={sub.enabled}
                      aria-label={`${sub.enabled ? 'Disable' : 'Enable'} ${row.title.toLowerCase()}`}
                    >
                      <span style={{
                        position: 'absolute', top: 2,
                        left: sub.enabled ? 22 : 2,
                        width: 20, height: 20, borderRadius: 10,
                        background: '#fff',
                        transition: 'left 0.15s',
                      }} />
                    </button>
                  </div>

                  {row.hasSeverity && sub.enabled && (
                    <div style={{ marginLeft: 0 }}>
                      <label style={{
                        display: 'block', fontSize: 11, fontWeight: 600,
                        color: 'var(--sg-text-dim, #9CA3AF)',
                        marginBottom: 6, letterSpacing: 0.4,
                      }}>
                        MINIMUM SEVERITY
                      </label>
                      <select
                        value={sub.severity_min}
                        onChange={(e) => saveSub({ ...sub, severity_min: e.target.value as Subscription['severity_min'] })}
                        disabled={saving}
                        style={{
                          width: '100%', maxWidth: 320,
                          padding: '6px 10px', fontSize: 12,
                          background: 'var(--sg-surface-2, rgba(255,255,255,0.04))',
                          border: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.10))',
                          borderRadius: 4,
                          color: 'var(--sg-text-primary, #E4E8F0)',
                          fontFamily: 'inherit',
                        }}
                      >
                        {SEVERITY_OPTIONS.map(opt => (
                          <option key={opt.value} value={opt.value}>{opt.label}</option>
                        ))}
                      </select>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}

        <div style={{
          marginTop: 32, paddingTop: 16,
          borderTop: '1px solid var(--sg-border-subtle, rgba(255,255,255,0.06))',
          fontSize: 11, color: 'var(--sg-text-dim, #6B7280)', lineHeight: 1.5,
        }}>
          {BRAND.name} sends notifications only for events at sites your account
          has access to. SMS notifications use the phone number on your profile.
          To stop all notifications immediately, disable each toggle above —
          your other preferences (severity threshold) stay saved for next time.
        </div>
      </div>
    </div>
  );
}
