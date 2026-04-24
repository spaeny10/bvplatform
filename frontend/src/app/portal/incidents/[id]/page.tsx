'use client';

import type { IncidentDetail } from '@/types/ironsight';
import { formatTime, formatRelativeTime, formatConfidence, formatDuration } from '@/lib/format';
import PPEStatusIcon from '@/components/shared/PPEStatusIcon';
import EvidenceExportButton from '@/components/shared/EvidenceExportButton';
import { useIncident } from '@/hooks/useIncidents';
import Link from 'next/link';

/* IRONSight Steel & Fire tokens */
const T = {
  bg: '#0A0C10', bgWhite: '#0E1117', bgPanel: '#12161E',
  bgDark: '#0A0C10', bgDark2: '#0E1117',
  border: 'rgba(255,255,255,0.07)', borderStrong: 'rgba(255,255,255,0.12)',
  text: '#E4E8F0', text2: '#8891A5', text3: '#4A5268', textInv: '#E4E8F0',
  red: '#EF4444', amber: '#E89B2A', green: '#22C55E', blue: '#3B82F6',
};

export default function IncidentDetailPage({ params }: { params: { id: string } }) {
  const { data: incident } = useIncident(params.id);

  if (!incident) return (
    <div style={{ background: T.bg, height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: "'Inter', sans-serif", color: T.text3 }}>
      Loading…
    </div>
  );

  const sevLabel: Record<string, { bg: string; border: string; color: string }> = {
    critical: { bg: 'rgba(192,49,26,0.2)', border: 'rgba(192,49,26,0.35)', color: '#e87060' },
    high: { bg: 'rgba(154,98,0,0.2)', border: 'rgba(154,98,0,0.3)', color: '#d4a040' },
    medium: { bg: 'rgba(154,111,0,0.15)', border: 'rgba(154,111,0,0.25)', color: '#c0a030' },
    low: { bg: 'rgba(20,72,160,0.15)', border: 'rgba(20,72,160,0.25)', color: '#80a8f0' },
  };

  const sev = sevLabel[incident.severity] || sevLabel.medium;

  return (
    <div style={{ background: T.bg, color: T.text, fontFamily: "'Inter', sans-serif", fontSize: 13, height: '100vh', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
      {/* ── CHROME TOPBAR ── */}
      <div style={{ background: T.bgDark, padding: '0 24px', height: 46, display: 'flex', alignItems: 'center', flexShrink: 0, borderBottom: '1px solid rgba(255,255,255,0.05)' }}>
        <div style={{ display: 'flex', alignItems: 'center', fontSize: 11, fontFamily: "'JetBrains Mono', monospace" }}>
          {['Portal', 'Incidents', incident.id].map((seg, i, arr) => (
            <span key={i}>
              {i < arr.length - 1 ? (
                <Link href={i === 0 ? '/portal' : '/portal'} style={{ padding: '0 10px', height: 46, display: 'inline-flex', alignItems: 'center', color: 'rgba(248,247,245,0.35)', textDecoration: 'none', borderRight: '1px solid rgba(255,255,255,0.05)' }}>{seg}</Link>
              ) : (
                <span style={{ padding: '0 10px', height: 46, display: 'inline-flex', alignItems: 'center', color: T.textInv, background: 'rgba(192,49,26,0.12)', borderRight: '1px solid rgba(192,49,26,0.2)' }}>{seg}</span>
              )}
            </span>
          ))}
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
          <EvidenceExportButton incidentId={incident.id} />
          <button style={{ padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 500, cursor: 'pointer', border: '1px solid rgba(20,72,160,0.4)', background: 'rgba(20,72,160,0.25)', color: '#80a8f0', fontFamily: 'inherit', display: 'flex', alignItems: 'center', gap: 6 }}>📄 Export PDF</button>
          <button style={{ padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 500, cursor: 'pointer', border: '1px solid rgba(192,49,26,0.4)', background: 'rgba(192,49,26,0.2)', color: '#e87060', fontFamily: 'inherit', display: 'flex', alignItems: 'center', gap: 6 }}>⚠ Escalate to HSE</button>
        </div>
      </div>

      {/* ── INCIDENT HEADER ── */}
      <div style={{ background: T.bgDark2, padding: '18px 24px 16px', flexShrink: 0, borderBottom: '1px solid rgba(255,255,255,0.05)', position: 'relative', overflow: 'hidden' }}>
        {/* Red left border accent */}
        <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: 4, background: T.red, boxShadow: '0 0 20px rgba(192,49,26,0.5)' }} />

        <div style={{ position: 'relative', zIndex: 1, display: 'grid', gridTemplateColumns: '1fr auto', gap: 24, alignItems: 'start' }}>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6 }}>
              <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 11, color: 'rgba(248,247,245,0.4)', letterSpacing: 1 }}>{incident.id}</span>
              <span style={{ padding: '2px 8px', borderRadius: 2, fontFamily: "'Inter', sans-serif", fontSize: 10, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase' as const, background: sev.bg, color: sev.color, border: `1px solid ${sev.border}` }}>{incident.severity}</span>
              <span style={{ padding: '2px 8px', borderRadius: 2, fontFamily: "'Inter', sans-serif", fontSize: 10, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, background: 'rgba(154,98,0,0.2)', color: '#d4a040', border: '1px solid rgba(154,98,0,0.3)', display: 'flex', alignItems: 'center', gap: 5 }}>
                <span style={{ width: 6, height: 6, borderRadius: '50%', background: '#d4a040', animation: 'pulse 2s infinite' }} />
                {incident.status.replace('_', ' ')}
              </span>
            </div>
            <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 20, fontWeight: 800, color: T.textInv, letterSpacing: -0.3, lineHeight: 1.15, marginBottom: 8 }}>
              {incident.title}
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap' as const, gap: 8 }}>
              {[
                { icon: '📍', label: 'Site', value: incident.site_name },
                { icon: '📷', label: '', value: incident.camera_name },
                { icon: '🕐', label: '', value: formatRelativeTime(incident.ts) },
                { icon: '⏱', label: '', value: formatDuration(incident.duration_ms) },
                { icon: '👷', label: '', value: `${incident.workers_identified} workers` },
              ].map((chip, i) => (
                <span key={i} style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '3px 9px', background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.08)', borderRadius: 4, fontSize: 11, color: 'rgba(248,247,245,0.55)', fontFamily: "'JetBrains Mono', monospace" }}>
                  {chip.icon} <strong style={{ color: 'rgba(248,247,245,0.85)', fontWeight: 500 }}>{chip.value}</strong>
                </span>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, alignItems: 'flex-end' }}>
            <button style={{ padding: '7px 16px', borderRadius: 4, fontSize: 12, fontWeight: 600, cursor: 'pointer', fontFamily: "'Inter', sans-serif", background: T.red, border: `1px solid ${T.red}`, color: '#fff', boxShadow: '0 2px 8px rgba(192,49,26,0.35)', display: 'flex', alignItems: 'center', gap: 7 }}>🚨 Escalate Incident</button>
            <button style={{ padding: '7px 16px', borderRadius: 4, fontSize: 12, fontWeight: 600, cursor: 'pointer', fontFamily: "'Inter', sans-serif", background: 'rgba(26,110,64,0.2)', border: '1px solid rgba(26,110,64,0.35)', color: '#60c890' }}>✓ Mark Resolved</button>
          </div>
        </div>
      </div>

      {/* ── BODY ── */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'grid', gridTemplateColumns: '1fr 360px' }}>
        {/* ── CENTER ── */}
        <div style={{ overflowY: 'auto', scrollbarWidth: 'thin' as const }}>
          {/* Video evidence */}
          <div style={{ background: T.bgDark }}>
            {/* Tab bar */}
            <div style={{ display: 'flex', background: 'rgba(0,0,0,0.4)', borderBottom: '1px solid rgba(255,255,255,0.06)', padding: '0 16px' }}>
              {['Video Evidence', 'Keyframes', 'AI Analysis'].map((tab, i) => (
                <button key={tab} style={{ padding: '9px 14px', fontSize: 11, fontWeight: 500, color: i === 0 ? T.textInv : 'rgba(248,247,245,0.35)', cursor: 'pointer', borderBottom: i === 0 ? `2px solid ${T.red}` : '2px solid transparent', background: 'none', border: 'none', fontFamily: "'Inter', sans-serif", letterSpacing: 0.5, marginBottom: -1 }}>{tab}</button>
              ))}
            </div>

            {/* Video viewport */}
            <div style={{ position: 'relative', background: '#0a0806', aspectRatio: '16/7', overflow: 'hidden' }}>
              <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(180deg, #080c08 0%, #101808 45%, #181c10 100%)' }} />
              <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '32%', background: 'rgba(48,42,22,0.7)' }} />
              <div style={{ position: 'absolute', bottom: '30%', left: '2%', width: '28%', height: '60%', background: 'rgba(38,48,55,0.8)' }} />
              <div style={{ position: 'absolute', bottom: '30%', right: '15%', width: '22%', height: '50%', background: 'rgba(50,48,38,0.5)', border: '1px solid rgba(80,75,50,0.3)' }} />

              {/* Detection boxes */}
              {incident.detections.slice(0, 3).map((det, i) => {
                const color = det.violation ? '#e06050' : '#40b870';
                return (
                  <div key={i} style={{
                    position: 'absolute',
                    left: `${(det.bbox[0] / 1920) * 100}%`,
                    top: `${(det.bbox[1] / 1080) * 100}%`,
                    width: `${((det.bbox[2] - det.bbox[0]) / 1920) * 100}%`,
                    height: `${((det.bbox[3] - det.bbox[1]) / 1080) * 100}%`,
                    border: `2px solid ${color}`,
                    borderRadius: 2,
                    pointerEvents: 'none',
                    boxShadow: det.violation ? `0 0 6px ${color}` : undefined,
                  }}>
                    <span style={{
                      position: 'absolute', bottom: '100%', left: -1, marginBottom: 2,
                      fontFamily: "'JetBrains Mono', monospace", fontSize: 8.5, fontWeight: 500,
                      padding: '2px 6px', borderRadius: 2, whiteSpace: 'nowrap' as const,
                      background: 'rgba(0,0,0,0.88)', color, border: `1px solid ${color}33`,
                    }}>
                      {(det.subclass || det.class).toUpperCase()} {Math.round(det.confidence * 100)}%
                    </span>
                  </div>
                );
              })}

              {/* Evidence badge */}
              <div style={{ position: 'absolute', top: 8, right: 8, display: 'flex', gap: 4, flexDirection: 'column' as const, alignItems: 'flex-end' }}>
                <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 8, padding: '2px 7px', borderRadius: 2, background: 'rgba(20,72,160,0.8)', color: '#90b8f0', border: '1px solid rgba(90,140,220,0.3)' }}>🔒 EVIDENCE LOCKED</span>
              </div>

              {/* Scanlines */}
              <div style={{ position: 'absolute', inset: 0, background: 'repeating-linear-gradient(0deg, transparent, transparent 2px, rgba(0,0,0,0.025) 2px, rgba(0,0,0,0.025) 4px)', pointerEvents: 'none' }} />
            </div>
          </div>

          {/* AI Analysis */}
          <div style={{ padding: '20px 24px', display: 'flex', flexDirection: 'column', gap: 18 }}>
            <div style={{ background: T.bgWhite, border: `1px solid ${T.border}`, borderRadius: 8, overflow: 'hidden', boxShadow: '0 1px 4px rgba(0,0,0,0.07)' }}>
              <div style={{ padding: '12px 16px', background: 'linear-gradient(135deg, #f8f4f0, #f2ecec)', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 10 }}>
                <div style={{ width: 28, height: 28, background: T.bgDark, borderRadius: 6, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 13 }}>🧠</div>
                <div>
                  <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, color: T.text }}>AI Analysis</div>
                  <div style={{ fontSize: 10, color: T.text3, marginTop: 1 }}>Vision Language Model · GPT-4V</div>
                </div>
                <span style={{ marginLeft: 'auto', fontFamily: "'JetBrains Mono', monospace", fontSize: 11, color: T.green, fontWeight: 500, display: 'flex', alignItems: 'center', gap: 5 }}>
                  <span style={{ width: 6, height: 6, borderRadius: '50%', background: T.green }} />
                  {formatConfidence(incident.ai_confidence)} confidence
                </span>
              </div>
              <div style={{ padding: '14px 16px' }}>
                <div style={{ fontSize: 13, lineHeight: 1.65, color: T.text2, fontStyle: 'italic', padding: '10px 14px', background: T.bgPanel, borderRadius: 4, borderLeft: `3px solid ${T.red}`, marginBottom: 12 }}>
                  {incident.ai_caption}
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                  {incident.findings.map(f => (
                    <div key={f.id} style={{ padding: '10px 12px', background: T.bgPanel, borderRadius: 6, border: `1px solid ${T.border}` }}>
                      <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 2, display: 'flex', alignItems: 'center', gap: 6 }}>
                        {f.icon} {f.title}
                      </div>
                      <div style={{ fontSize: 11, color: T.text3, lineHeight: 1.4 }}>{f.description}</div>
                    </div>
                  ))}
                </div>
              </div>
            </div>

            {/* Detection Objects Table */}
            <div style={{ background: T.bgWhite, border: `1px solid ${T.border}`, borderRadius: 8, overflow: 'hidden', boxShadow: '0 1px 4px rgba(0,0,0,0.07)' }}>
              <div style={{ padding: '12px 16px', background: 'linear-gradient(135deg, #f8f4f0, #f2ecec)', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 10 }}>
                <div style={{ width: 28, height: 28, background: T.bgDark, borderRadius: 6, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 13 }}>🎯</div>
                <div>
                  <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, color: T.text }}>Detection Objects</div>
                  <div style={{ fontSize: 10, color: T.text3, marginTop: 1 }}>{incident.detections.length} objects detected</div>
                </div>
              </div>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    {['Class', 'Confidence', 'Bbox', 'Zone', 'Violation'].map(h => (
                      <th key={h} style={{
                        padding: '8px 12px', textAlign: 'left' as const, fontSize: 9,
                        fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const,
                        color: T.text3, borderBottom: `1px solid ${T.border}`,
                        background: T.bgPanel, fontFamily: "'JetBrains Mono', monospace",
                      }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {incident.detections.map((det, i) => (
                    <tr key={i} style={{ borderBottom: `1px solid ${T.border}` }}>
                      <td style={{ padding: '8px 12px', fontSize: 11, fontWeight: 600 }}>
                        {(det.subclass || det.class).replace(/_/g, ' ')}
                      </td>
                      <td style={{ padding: '8px 12px' }}>
                        <span style={{
                          fontFamily: "'JetBrains Mono', monospace", fontSize: 10, fontWeight: 500,
                          padding: '2px 6px', borderRadius: 10,
                          background: det.confidence >= 0.9 ? 'rgba(26,110,64,0.08)' : det.confidence >= 0.7 ? 'rgba(20,72,160,0.08)' : 'rgba(154,98,0,0.08)',
                          color: det.confidence >= 0.9 ? T.green : det.confidence >= 0.7 ? T.blue : T.amber,
                        }}>
                          {Math.round(det.confidence * 100)}%
                        </span>
                      </td>
                      <td style={{ padding: '8px 12px', fontFamily: "'JetBrains Mono', monospace", fontSize: 9, color: T.text3 }}>
                        [{det.bbox.join(', ')}]
                      </td>
                      <td style={{ padding: '8px 12px', fontSize: 11 }}>
                        {det.in_exclusion_zone ? (
                          <span style={{ color: T.red, fontWeight: 600 }}>⚠ In Zone</span>
                        ) : (
                          <span style={{ color: T.text3 }}>Clear</span>
                        )}
                      </td>
                      <td style={{ padding: '8px 12px', fontSize: 11 }}>
                        {det.violation ? (
                          <span style={{ padding: '2px 6px', borderRadius: 2, fontSize: 9, fontWeight: 700, background: 'rgba(192,49,26,0.08)', color: T.red, border: '1px solid rgba(192,49,26,0.16)' }}>VIOLATION</span>
                        ) : (
                          <span style={{ color: T.text3, fontSize: 10 }}>—</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        {/* ── RIGHT SIDEBAR ── */}
        <div style={{ borderLeft: `1px solid ${T.border}`, background: T.bgWhite, overflowY: 'auto', scrollbarWidth: 'thin' as const }}>
          {/* OSHA */}
          <div style={{ padding: '16px 18px', borderBottom: `1px solid ${T.border}` }}>
            <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 8 }}>OSHA Classification</div>
            <div style={{ padding: '8px 12px', background: 'rgba(192,49,26,0.06)', border: '1px solid rgba(192,49,26,0.16)', borderRadius: 4, fontSize: 11, fontFamily: "'JetBrains Mono', monospace", color: T.red, fontWeight: 500 }}>
              {incident.osha_classification}
            </div>
          </div>

          {/* Worker PPE */}
          <div style={{ padding: '16px 18px', borderBottom: `1px solid ${T.border}` }}>
            <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Worker PPE Status</div>
            {incident.workers.map(w => (
              <div key={w.worker_id} style={{ padding: '8px 10px', marginBottom: 6, background: T.bgPanel, borderRadius: 4, border: `1px solid ${T.border}` }}>
                <div style={{ fontSize: 11, fontWeight: 600, marginBottom: 4, color: w.in_zone ? T.red : T.text }}>{w.name} <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 9, color: T.text3 }}>{w.worker_id}</span></div>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' as const }}>
                  <PPEStatusIcon present={w.hard_hat} item="hard_hat" size={16} />
                  <PPEStatusIcon present={w.harness} item="harness" size={16} />
                  <PPEStatusIcon present={w.hi_vis} item="hi_vis" size={16} />
                  <PPEStatusIcon present={w.boots} item="boots" size={16} />
                  <PPEStatusIcon present={w.gloves} item="gloves" size={16} />
                </div>
              </div>
            ))}
          </div>

          {/* Event Timeline */}
          <div style={{ padding: '16px 18px', borderBottom: `1px solid ${T.border}` }}>
            <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Event Timeline</div>
            {incident.timeline.map((evt, i) => {
              const typeColors = { detection: T.blue, alert: T.red, action: T.green, system: T.text3 };
              return (
                <div key={i} style={{ display: 'flex', gap: 10, marginBottom: 10, fontSize: 11 }}>
                  <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 9, color: T.text3, width: 40, flexShrink: 0, textAlign: 'right' as const }}>{formatRelativeTime(evt.ts)}</span>
                  <span style={{ width: 6, height: 6, borderRadius: '50%', background: typeColors[evt.type] || T.text3, marginTop: 4, flexShrink: 0 }} />
                  <span style={{ color: T.text2, lineHeight: 1.4 }}>{evt.label}</span>
                </div>
              );
            })}
          </div>

          {/* Notification Log */}
          <div style={{ padding: '16px 18px', borderBottom: `1px solid ${T.border}` }}>
            <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Notifications</div>
            {incident.notifications.length === 0 && (
              <div style={{ fontSize: 11, color: T.text3, fontStyle: 'italic' }}>No notifications sent</div>
            )}
            {incident.notifications.map((notif, i) => {
              const channelIcons: Record<string, string> = { sms: '💬', email: '📧', push: '🔔' };
              return (
                <div key={i} style={{ display: 'flex', gap: 8, marginBottom: 8, fontSize: 11, alignItems: 'flex-start' }}>
                  <span style={{ fontSize: 13, flexShrink: 0 }}>{channelIcons[notif.channel] || '📨'}</span>
                  <div style={{ flex: 1 }}>
                    <div style={{ color: T.text2 }}>
                      <span style={{ fontWeight: 600, color: T.text }}>{notif.recipient}</span> via {notif.channel}
                    </div>
                    <div style={{ fontSize: 9, color: T.text3, fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
                      {formatRelativeTime(notif.ts)} · <span style={{ color: notif.status === 'sent' ? T.green : T.red }}>{notif.status}</span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>

          {/* Comments */}
          <div style={{ padding: '16px 18px' }}>
            <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Comments</div>
            {incident.comments.map(c => (
              <div key={c.id} style={{ padding: '8px 10px', marginBottom: 6, background: T.bgPanel, borderRadius: 4, border: `1px solid ${T.border}` }}>
                <div style={{ fontSize: 10, fontWeight: 600, marginBottom: 2 }}>{c.author}</div>
                <div style={{ fontSize: 11, color: T.text2 }}>{c.text}</div>
                <div style={{ fontSize: 9, color: T.text3, marginTop: 4, fontFamily: "'JetBrains Mono', monospace" }}>{formatRelativeTime(c.ts)}</div>
              </div>
            ))}
            <textarea placeholder="Add a comment…" style={{ width: '100%', padding: '8px 10px', borderRadius: 4, border: `1px solid ${T.border}`, fontSize: 11, fontFamily: 'inherit', resize: 'vertical' as const, minHeight: 60, marginTop: 6, background: T.bgPanel }} />
          </div>
        </div>
      </div>
    </div>
  );
}
