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
// Token usage: all colors/surfaces come from the portal warm-light
// CSS custom properties (--bg, --bg-card, --border, --text-primary,
// --text-secondary, --text-dim, --accent, --green, --shadow-sm, etc.)
// defined in portal.css on .portal-shell. No operator-dark tokens
// (--sg-*) are used here — those belong to the operator console.

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
    // Outer shell uses portal warm-light palette. Must be inside
    // .portal-shell to pick up the CSS custom properties from portal.css.
    <div style={{
      minHeight: '100vh',
      background: 'var(--bg, #f5f0e8)',
      color: 'var(--text-primary, #2a2520)',
      padding: 24,
      fontFamily: "var(--font-family, 'Inter', sans-serif)",
    }}>
      <div style={{ maxWidth: 720, margin: '0 auto' }}>
        <div style={{ marginBottom: 8, fontSize: 12 }}>
          <Link href="/portal" style={{ color: 'var(--accent, #c84b2f)', textDecoration: 'none' }}>
            ← Back to portal
          </Link>
        </div>
        <h1 style={{ fontSize: 22, fontWeight: 700, margin: '0 0 4px', color: 'var(--text-primary, #2a2520)' }}>
          Notification preferences
        </h1>
        <p style={{ fontSize: 13, color: 'var(--text-secondary, #6b6560)', margin: '0 0 24px', lineHeight: 1.5 }}>
          Choose how {BRAND.name} reaches you when something happens at your sites.
          Changes save automatically.
        </p>

        {err && (
          <div style={{
            padding: '10px 14px', marginBottom: 16,
            background: 'rgba(200,75,47,0.06)', border: '1px solid rgba(200,75,47,0.2)',
            borderRadius: 6, color: 'var(--accent, #c84b2f)', fontSize: 12,
          }}>
            {err}
          </div>
        )}

        {savedAt > 0 && Date.now() - savedAt < 3000 && (
          <div style={{
            padding: '8px 14px', marginBottom: 16,
            background: 'rgba(26,122,74,0.06)', border: '1px solid rgba(26,122,74,0.2)',
            borderRadius: 6, color: 'var(--green, #1a7a4a)', fontSize: 12,
          }}>
            Saved
          </div>
        )}

        {subs === null ? (
          <div style={{ padding: 32, textAlign: 'center', color: 'var(--text-dim, #a09990)' }}>
            Loading...
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
                    background: 'var(--bg-card, #ffffff)',
                    border: '1px solid var(--border, #e0dbd0)',
                    borderRadius: 8,
                    boxShadow: 'var(--shadow-sm, 0 1px 3px rgba(0,0,0,0.05))',
                    opacity: saving ? 0.65 : 1,
                    transition: 'opacity 0.15s',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'flex-start', gap: 16, marginBottom: row.hasSeverity ? 12 : 0 }}>
                    <div style={{ flex: 1 }}>
                      <div style={{
                        fontSize: 14, fontWeight: 600, marginBottom: 4,
                        color: 'var(--text-primary, #2a2520)',
                      }}>
                        {row.title}
                      </div>
                      <div style={{
                        fontSize: 12, color: 'var(--text-secondary, #6b6560)', lineHeight: 1.5,
                      }}>
                        {row.subtitle}
                      </div>
                    </div>
                    {/* Toggle button meets ≥44×44px touch target requirement */}
                    <button
                      type="button"
                      onClick={() => saveSub({ ...sub, enabled: !sub.enabled })}
                      disabled={saving}
                      style={{
                        position: 'relative',
                        /* Invisible hit area expanded to 44×44 while track stays 44×24 */
                        minWidth: 44, minHeight: 44,
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                        flexShrink: 0,
                        border: 'none',
                        background: 'transparent',
                        padding: 0,
                        cursor: saving ? 'wait' : 'pointer',
                      }}
                      aria-pressed={sub.enabled}
                      aria-label={`${sub.enabled ? 'Disable' : 'Enable'} ${row.title.toLowerCase()}`}
                    >
                      {/* Visible track */}
                      <span style={{
                        display: 'block',
                        width: 44, height: 24,
                        borderRadius: 12,
                        background: sub.enabled ? 'var(--green, #1a7a4a)' : 'var(--border, #e0dbd0)',
                        transition: 'background 0.15s',
                        position: 'relative',
                      }}>
                        <span style={{
                          position: 'absolute', top: 2,
                          left: sub.enabled ? 22 : 2,
                          width: 20, height: 20, borderRadius: 10,
                          background: '#fff',
                          boxShadow: '0 1px 3px rgba(0,0,0,0.15)',
                          transition: 'left 0.15s',
                        }} />
                      </span>
                    </button>
                  </div>

                  {row.hasSeverity && sub.enabled && (
                    <div style={{ marginLeft: 0 }}>
                      <label style={{
                        display: 'block', fontSize: 11, fontWeight: 600,
                        color: 'var(--text-dim, #a09990)',
                        marginBottom: 6, letterSpacing: 0.4,
                        textTransform: 'uppercase',
                      }}>
                        Minimum severity
                      </label>
                      <select
                        value={sub.severity_min}
                        onChange={(e) => saveSub({ ...sub, severity_min: e.target.value as Subscription['severity_min'] })}
                        disabled={saving}
                        style={{
                          width: '100%', maxWidth: 320,
                          /* Touch target height ≥44px for mobile */
                          padding: '10px 10px', fontSize: 13, minHeight: 44,
                          background: 'var(--bg-warm, #faf9f5)',
                          border: '1px solid var(--border, #e0dbd0)',
                          borderRadius: 6,
                          color: 'var(--text-primary, #2a2520)',
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
          borderTop: '1px solid var(--border, #e0dbd0)',
          fontSize: 11, color: 'var(--text-dim, #a09990)', lineHeight: 1.5,
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
