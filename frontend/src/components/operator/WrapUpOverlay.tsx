'use client';

import { useState, useEffect, useCallback } from 'react';
import type { LastDisposition, ShiftStats } from '@/stores/operator-store';
import { DISPOSITION_OPTIONS } from '@/stores/operator-store';
import SeverityPill from '@/components/shared/SeverityPill';

const WRAPUP_SECONDS = 60;

interface Props {
  lastDisposition: LastDisposition;
  shiftStats: ShiftStats;
  onReady: () => void;
  onExportEvidence: () => void;
}

function formatDuration(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

function formatShiftDuration(startMs: number): string {
  const ms = Date.now() - startMs;
  const h = Math.floor(ms / 3600000);
  const m = Math.floor((ms % 3600000) / 60000);
  if (h === 0) return `${m}m`;
  return `${h}h ${m}m`;
}

// SVG countdown ring
function CountdownRing({ seconds, total }: { seconds: number; total: number }) {
  const r = 44;
  const circumference = 2 * Math.PI * r;
  const progress = seconds / total;
  const dashOffset = circumference * (1 - progress);
  const color = seconds > total * 0.5 ? '#22C55E' : seconds > total * 0.25 ? '#E89B2A' : '#EF4444';

  return (
    <svg width={112} height={112} style={{ transform: 'rotate(-90deg)' }}>
      {/* Track */}
      <circle cx={56} cy={56} r={r} fill="none" stroke="rgba(255,255,255,0.06)" strokeWidth={6} />
      {/* Progress */}
      <circle
        cx={56} cy={56} r={r}
        fill="none"
        stroke={color}
        strokeWidth={6}
        strokeDasharray={circumference}
        strokeDashoffset={dashOffset}
        strokeLinecap="round"
        style={{ transition: 'stroke-dashoffset 1s linear, stroke 0.5s ease' }}
      />
    </svg>
  );
}

export default function WrapUpOverlay({ lastDisposition, shiftStats, onReady, onExportEvidence }: Props) {
  const [countdown, setCountdown] = useState(WRAPUP_SECONDS);
  const { alarm, dispositionCode, responseMs, resolvedAt } = lastDisposition;

  const dispOption = DISPOSITION_OPTIONS.find(d => d.code === dispositionCode);
  const isVerified = dispositionCode.startsWith('verified');
  const dispColor = isVerified ? '#EF4444' : '#22C55E';

  const slaMs = alarm.sla_deadline_ms;
  const slaStatus = slaMs
    ? resolvedAt <= slaMs ? 'met' : 'missed'
    : 'n/a';

  const alarmsHandled = shiftStats.alarmsHandled;
  const slaMissed = shiftStats.slaMissed;
  const slaCompliance = alarmsHandled > 0
    ? Math.round(((alarmsHandled - slaMissed) / alarmsHandled) * 100)
    : 100;

  useEffect(() => {
    if (countdown <= 0) { onReady(); return; }
    const t = setTimeout(() => setCountdown(c => c - 1), 1000);
    return () => clearTimeout(t);
  }, [countdown, onReady]);

  const DISPOSITION_ICONS: Record<string, string> = {
    false_positive_animal: '🐾', false_positive_weather: '🌩️',
    false_positive_shadow: '💡', false_positive_equipment: '⚙️',
    false_positive_other: '❓', verified_customer_notified: '📞',
    verified_police_dispatched: '🚔', verified_guard_responded: '🛡️',
    verified_no_threat: '✅', verified_other: '⚠️',
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 300,
      background: 'rgba(5,7,10,0.92)',
      backdropFilter: 'blur(20px)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }}>
      <div style={{
        width: 560, background: '#0E1117',
        border: '1px solid rgba(255,255,255,0.1)',
        borderRadius: 16,
        boxShadow: '0 32px 96px rgba(0,0,0,0.8)',
        overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          padding: '14px 20px',
          background: 'rgba(232,115,42,0.05)',
          borderBottom: '1px solid rgba(255,255,255,0.07)',
          display: 'flex', alignItems: 'center', gap: 10,
        }}>
          <span style={{
            fontSize: 9, fontWeight: 700, letterSpacing: 2, padding: '3px 10px',
            borderRadius: 4, background: 'rgba(232,115,42,0.15)',
            border: '1px solid rgba(232,115,42,0.3)', color: '#E8732A',
          }}>
            WRAP-UP MODE
          </span>
          <div style={{ flex: 1, fontSize: 11, color: '#6B7590' }}>
            Preparing for next assignment…
          </div>
        </div>

        {/* Main content */}
        <div style={{ padding: '24px 28px', display: 'flex', gap: 28, alignItems: 'flex-start' }}>
          {/* Countdown ring */}
          <div style={{ flexShrink: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6 }}>
            <div style={{ position: 'relative', width: 112, height: 112 }}>
              <CountdownRing seconds={countdown} total={WRAPUP_SECONDS} />
              <div style={{
                position: 'absolute', inset: 0,
                display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
                transform: 'rotate(0deg)',
              }}>
                <div style={{ fontSize: 26, fontWeight: 700, color: '#E4E8F0', fontFamily: "'JetBrains Mono', monospace", lineHeight: 1 }}>
                  {countdown}
                </div>
                <div style={{ fontSize: 8, color: '#4A5268', letterSpacing: 1, marginTop: 2 }}>READY IN</div>
              </div>
            </div>
          </div>

          {/* Right: resolved alarm + shift stats */}
          <div style={{ flex: 1, minWidth: 0 }}>
            {/* Resolved alarm card */}
            <div style={{
              padding: '12px 14px', borderRadius: 8, marginBottom: 16,
              background: isVerified ? 'rgba(239,68,68,0.05)' : 'rgba(34,197,94,0.05)',
              border: `1px solid ${isVerified ? 'rgba(239,68,68,0.2)' : 'rgba(34,197,94,0.2)'}`,
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                <span style={{ fontSize: 18 }}>{DISPOSITION_ICONS[dispositionCode] || '✓'}</span>
                <div>
                  <div style={{ fontSize: 12, fontWeight: 700, color: dispColor }}>
                    {dispOption?.label || dispositionCode}
                  </div>
                  <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1 }}>
                    {alarm.site_name} · {alarm.camera_name}
                  </div>
                </div>
                <div style={{ marginLeft: 'auto', flexShrink: 0 }}>
                  <SeverityPill severity={alarm.severity} size="sm" />
                </div>
              </div>
              <div style={{ display: 'flex', gap: 16, fontSize: 9, fontFamily: "'JetBrains Mono', monospace" }}>
                <span style={{ color: '#6B7590' }}>
                  Response: <span style={{ color: responseMs < 30000 ? '#22C55E' : responseMs < 90000 ? '#E89B2A' : '#EF4444' }}>
                    {formatDuration(responseMs)}
                  </span>
                </span>
                <span style={{ color: '#6B7590' }}>
                  SLA: <span style={{ color: slaStatus === 'met' ? '#22C55E' : slaStatus === 'missed' ? '#EF4444' : '#6B7590' }}>
                    {slaStatus === 'met' ? '✓ Met' : slaStatus === 'missed' ? '✗ Missed' : '—'}
                  </span>
                </span>
                <span style={{ color: '#6B7590' }}>
                  Actions: <span style={{ color: '#E4E8F0' }}>{lastDisposition.actionLog.filter(e => !e.auto).length} manual</span>
                </span>
              </div>
            </div>

            {/* Shift stats row */}
            <div style={{
              display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 8,
            }}>
              {[
                { label: 'Alarms Handled', value: String(alarmsHandled), color: '#E4E8F0' },
                { label: 'SLA Compliance', value: `${slaCompliance}%`, color: slaCompliance >= 90 ? '#22C55E' : slaCompliance >= 70 ? '#E89B2A' : '#EF4444' },
                { label: 'Shift Duration', value: formatShiftDuration(shiftStats.shiftStartMs), color: '#E4E8F0' },
              ].map(stat => (
                <div key={stat.label} style={{
                  padding: '10px 12px', borderRadius: 6,
                  background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)',
                  textAlign: 'center',
                }}>
                  <div style={{ fontSize: 18, fontWeight: 700, color: stat.color, fontFamily: "'JetBrains Mono', monospace" }}>
                    {stat.value}
                  </div>
                  <div style={{ fontSize: 8, color: '#4A5268', marginTop: 3, letterSpacing: 0.5 }}>
                    {stat.label}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Action buttons */}
        <div style={{
          padding: '16px 28px 20px',
          borderTop: '1px solid rgba(255,255,255,0.06)',
          display: 'flex', gap: 10,
        }}>
          <button
            onClick={onExportEvidence}
            style={{
              flex: 1, padding: '10px', borderRadius: 6, fontSize: 11, fontWeight: 600,
              background: 'rgba(232,115,42,0.08)', border: '1px solid rgba(232,115,42,0.25)',
              color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
            }}
          >
            📋 Export Evidence Package
          </button>
          <button
            onClick={onReady}
            style={{
              flex: 1, padding: '10px', borderRadius: 6, fontSize: 11, fontWeight: 700,
              background: 'rgba(34,197,94,0.12)', border: '1px solid rgba(34,197,94,0.35)',
              color: '#22C55E', cursor: 'pointer', fontFamily: 'inherit',
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
            }}
          >
            ✓ Ready for Next Alarm
          </button>
        </div>
      </div>
    </div>
  );
}
