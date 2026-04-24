'use client';

import { useEffect, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { useSites } from '@/hooks/useSites';
import { useAlerts, useAlertStream } from '@/hooks/useAlerts';
import { useOperatorStore } from '@/stores/operator-store';
import { getCurrentOperator, getSiteLocks, getPendingHandoffs } from '@/lib/ironsight-api';
import { useAlertAudio } from '@/hooks/useAlertAudio';
import FleetStatusBar from '@/components/operator/FleetStatusBar';
import AlertFeed from '@/components/operator/AlertFeed';
import ShiftHandoffModal from '@/components/operator/ShiftHandoffModal';
import ShortcutOverlay from '@/components/operator/ShortcutOverlay';
import OperatorRoster from '@/components/operator/OperatorRoster';
import WrapUpOverlay from '@/components/operator/WrapUpOverlay';
import EvidenceExportModal from '@/components/operator/EvidenceExportModal';
import ErrorBoundary from '@/components/shared/ErrorBoundary';
import UserChip from '@/components/shared/UserChip';
import Logo from '@/components/shared/Logo';
import Link from 'next/link';

function OperatorConsoleInner() {
  const router = useRouter();

  // ── Data ──
  const { data: sites = [] } = useSites();
  const { data: restAlerts = [] } = useAlerts();
  const { wsStatus } = useAlertStream();

  // ── Store ──
  const alertFeed = useOperatorStore(s => s.alertFeed);
  const dismissedIds = useOperatorStore(s => s.dismissedIds);
  const currentOperator = useOperatorStore(s => s.currentOperator);
  const engageAlarm = useOperatorStore(s => s.engageAlarm);
  const operatorStatus = useOperatorStore(s => s.operatorStatus);
  const activeAlarm = useOperatorStore(s => s.activeAlarm);
  const queueDepth = useOperatorStore(s => s.queueDepth);
  const lastDisposition = useOperatorStore(s => s.lastDisposition);
  const shiftStats = useOperatorStore(s => s.shiftStats);
  const exitWrapUp = useOperatorStore(s => s.exitWrapUp);
  const audioMuted = useOperatorStore(s => s.audioMuted);
  const setAudioMuted = useOperatorStore(s => s.setAudioMuted);
  const setOperatorStatus = useOperatorStore(s => s.setOperatorStatus);

  // ── Init ──
  useEffect(() => {
    getCurrentOperator().then(op => useOperatorStore.getState().setCurrentOperator(op));
    getSiteLocks().then(locks => useOperatorStore.getState().initLocks(locks));
  }, []);

  // ── Stale status guard ──
  // If the operator is back on the main page with no active alarm, 'engaged' is stale.
  // 'wrap_up' without a lastDisposition is also stale (e.g. after a server restart).
  useEffect(() => {
    if (operatorStatus === 'engaged' && !activeAlarm) {
      setOperatorStatus('available');
    }
    if (operatorStatus === 'wrap_up' && !lastDisposition) {
      setOperatorStatus('available');
    }
  }, [operatorStatus, activeAlarm, lastDisposition, setOperatorStatus]);

  // Merge REST polling + WebSocket push — REST is ground truth for existing alarms;
  // WebSocket adds new ones in real-time. Dedup by ID, drop acknowledged (archived), sort newest first.
  const displayAlerts = useMemo(() => {
    const dismissed = new Set(dismissedIds);
    const byId = new Map<string, (typeof restAlerts)[0]>();
    // REST first (acknowledged state, clip_url, etc. are authoritative)
    restAlerts.forEach(a => byId.set(a.id, a));
    // WebSocket-pushed alerts fill in anything REST hasn't returned yet
    alertFeed.forEach(a => { if (!byId.has(a.id)) byId.set(a.id, a); });
    return Array.from(byId.values())
      .filter(a => !a.acknowledged && !dismissed.has(a.id))
      .sort((a, b) => b.ts - a.ts);
  }, [restAlerts, alertFeed, dismissedIds]);

  // ── Audio alerts ──
  useAlertAudio(displayAlerts, { muted: audioMuted });

  // ── Fleet stats ──
  const totalOnline = useMemo(() => sites.reduce((s, site) => s + (site.cameras_online ?? 0), 0), [sites]);
  const totalDegraded = useMemo(() => sites.reduce((s, site) => s + ((site.cameras_total ?? 0) - (site.cameras_online ?? 0)), 0), [sites]);
  const unackedAlarms = useMemo(() => displayAlerts.filter(a => !a.acknowledged).length, [displayAlerts]);

  // ── UI state ──
  const [showHandoff, setShowHandoff] = useState(false);
  const [showShortcuts, setShowShortcuts] = useState(false);
  const [showEvidence, setShowEvidence] = useState(false);
  const [showStatusPicker, setShowStatusPicker] = useState(false);
  const [pendingHandoffCount, setPendingHandoffCount] = useState(0);

  useEffect(() => {
    if (currentOperator) getPendingHandoffs(currentOperator.id).then(h => setPendingHandoffCount(h.length));
  }, [currentOperator]);

  // ── Keyboard shortcuts ──
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLSelectElement) return;
      if (e.key === '?' || (e.key === '/' && e.shiftKey)) { setShowShortcuts(v => !v); e.preventDefault(); }
      if (e.key === 'h' || e.key === 'H') { setShowHandoff(v => !v); e.preventDefault(); }
      if (e.key === 'm' || e.key === 'M') { setAudioMuted(!audioMuted); e.preventDefault(); }
      if (e.key === 'Escape') { setShowHandoff(false); setShowShortcuts(false); setShowEvidence(false); setShowStatusPicker(false); }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [audioMuted, setAudioMuted]);

  // ── Status colors ──
  const statusColor = operatorStatus === 'available' ? '#22C55E'
    : operatorStatus === 'engaged' ? '#EF4444'
    : operatorStatus === 'wrap_up' ? '#E89B2A'
    : operatorStatus === 'break' ? '#E89B2A'
    : '#4A5268';
  const statusLabel = operatorStatus === 'available' ? 'AVAILABLE'
    : operatorStatus === 'engaged' ? 'ENGAGED'
    : operatorStatus === 'wrap_up' ? 'WRAP-UP'
    : operatorStatus === 'break' ? 'ON BREAK'
    : 'AWAY';

  // ── Time ──
  const [time, setTime] = useState('');
  useEffect(() => {
    const update = () => setTime(new Date().toLocaleTimeString('en-US', { hour12: false }));
    update();
    const t = setInterval(update, 1000);
    return () => clearInterval(t);
  }, []);

  return (
    <div className="op-shell">
      {/* ══════ TOP BAR ══════ */}
      <div className="op-topbar">
        <div className="op-logo">
          <Logo height={20} />
          {/* Status picker */}
          <div style={{ position: 'relative', marginLeft: 8 }}>
            <button
              onClick={() => {
                // engaged + wrap_up are system-controlled — block manual changes
                if (operatorStatus !== 'engaged' && operatorStatus !== 'wrap_up') {
                  setShowStatusPicker(v => !v);
                }
              }}
              title={operatorStatus === 'engaged' || operatorStatus === 'wrap_up' ? 'Status locked during active alarm' : 'Set availability status'}
              style={{
                fontSize: 8, fontWeight: 700, letterSpacing: 1,
                padding: '2px 8px', borderRadius: 8,
                background: `${statusColor}18`, color: statusColor,
                border: `1px solid ${statusColor}35`,
                cursor: operatorStatus === 'engaged' || operatorStatus === 'wrap_up' ? 'default' : 'pointer',
                fontFamily: 'inherit',
              }}
            >
              ● {statusLabel} {operatorStatus !== 'engaged' && operatorStatus !== 'wrap_up' && <span style={{ opacity: 0.5, fontSize: 7 }}>▾</span>}
            </button>
            {showStatusPicker && (
              <div style={{
                position: 'absolute', top: 'calc(100% + 6px)', left: 0, zIndex: 200,
                background: '#0E1117', border: '1px solid rgba(255,255,255,0.12)',
                borderRadius: 8, padding: 6, minWidth: 160,
                boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
              }}>
                <div style={{ fontSize: 8, color: '#4A5268', letterSpacing: 1, fontWeight: 600, padding: '3px 8px 6px', textTransform: 'uppercase' }}>
                  Set Status
                </div>
                {([
                  { status: 'available', label: 'Available', color: '#22C55E', icon: '●', hint: 'Ready for alarms' },
                  { status: 'break', label: 'On Break', color: '#E89B2A', icon: '☕', hint: 'SLA paused — no new alarms' },
                  { status: 'away', label: 'Away', color: '#4A5268', icon: '⏸', hint: 'Removed from queue' },
                ] as const).map(opt => (
                  <button
                    key={opt.status}
                    onClick={() => { setOperatorStatus(opt.status); setShowStatusPicker(false); }}
                    style={{
                      display: 'flex', alignItems: 'center', gap: 8,
                      width: '100%', padding: '7px 8px', borderRadius: 5,
                      background: operatorStatus === opt.status ? `${opt.color}14` : 'transparent',
                      border: `1px solid ${operatorStatus === opt.status ? `${opt.color}35` : 'transparent'}`,
                      cursor: 'pointer', fontFamily: 'inherit', textAlign: 'left',
                    }}
                  >
                    <span style={{ fontSize: 10, color: opt.color, width: 14, textAlign: 'center', flexShrink: 0 }}>{opt.icon}</span>
                    <div>
                      <div style={{ fontSize: 11, fontWeight: 600, color: operatorStatus === opt.status ? opt.color : '#E4E8F0' }}>{opt.label}</div>
                      <div style={{ fontSize: 8, color: '#4A5268', marginTop: 1 }}>{opt.hint}</div>
                    </div>
                    {operatorStatus === opt.status && (
                      <span style={{ marginLeft: 'auto', fontSize: 9, color: opt.color }}>✓</span>
                    )}
                  </button>
                ))}
              </div>
            )}
          </div>
          <span style={{
            marginLeft: 6, fontSize: 8, fontWeight: 600, letterSpacing: 1,
            padding: '2px 6px', borderRadius: 8,
            background: wsStatus === 'connected' ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)',
            color: wsStatus === 'connected' ? '#22C55E' : '#EF4444',
            border: `1px solid ${wsStatus === 'connected' ? 'rgba(34,197,94,0.2)' : 'rgba(239,68,68,0.2)'}`,
          }}>
            {wsStatus === 'connected' ? 'LIVE' : 'OFFLINE'}
          </span>
        </div>

        <div className="op-topbar-nav">
          <button className="op-nav-item active">SOC Monitor</button>
          <Link href="/portal" style={{ textDecoration: 'none' }}><button className="op-nav-item">Portal</button></Link>
          <Link href="/analytics" style={{ textDecoration: 'none' }}><button className="op-nav-item">Analytics</button></Link>
          <Link href="/" style={{ textDecoration: 'none' }}><button className="op-nav-item">NVR</button></Link>
        </div>

        <div className="op-topbar-right">
          <FleetStatusBar
            onlineCameras={totalOnline}
            degradedCameras={totalDegraded}
            criticalAlerts={unackedAlarms}
            queueDepth={queueDepth}
          />

          {/* Audio mute toggle */}
          <button
            className="op-nav-item"
            onClick={() => setAudioMuted(!audioMuted)}
            title={audioMuted ? 'Unmute alerts (M)' : 'Mute alerts (M)'}
            style={{
              fontSize: 14, padding: '0 8px',
              color: audioMuted ? '#EF4444' : '#4A5268',
              opacity: audioMuted ? 1 : 0.7,
            }}
          >
            {audioMuted ? '🔇' : '🔔'}
          </button>

          <button className="op-nav-item" onClick={() => setShowHandoff(true)} style={{ position: 'relative' }}>
            📋 Handoff
            {pendingHandoffCount > 0 && (
              <span style={{
                position: 'absolute', top: -2, right: -4,
                width: 14, height: 14, borderRadius: '50%',
                background: '#EF4444', color: '#fff', fontSize: 8, fontWeight: 700,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}>{pendingHandoffCount}</span>
            )}
          </button>
          <button className="op-nav-item" onClick={() => setShowShortcuts(v => !v)}>? Help</button>
          <UserChip />
        </div>
      </div>

      {/* ══════ MAIN: Dispatch Ready Screen ══════ */}
      <div style={{
        flex: 1, display: 'grid',
        gridTemplateColumns: '1fr 520px',
        overflow: 'hidden',
      }}>
        {/* ── CENTER: Dispatch Ready ── */}
        <div style={{
          display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center',
          gap: 20, padding: 40,
          position: 'relative',
        }}>
          {/* Ambient pulse when alarms are pending */}
          {unackedAlarms > 0 && (
            <div style={{
              position: 'absolute', inset: 0, pointerEvents: 'none',
              background: `radial-gradient(ellipse at center, rgba(239,68,68,0.03) 0%, transparent 70%)`,
              animation: 'ambientDrift 4s ease-in-out infinite alternate',
            }} />
          )}

          {/* Operator identity */}
          <div style={{
            width: 72, height: 72, borderRadius: '50%',
            background: `${statusColor}15`,
            border: `2px solid ${statusColor}40`,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 24, fontWeight: 700, color: statusColor,
            fontFamily: "'JetBrains Mono', monospace",
            boxShadow: `0 0 30px ${statusColor}20`,
          }}>
            {currentOperator?.callsign?.slice(-2) || '??'}
          </div>

          <div style={{ textAlign: 'center' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-primary)', letterSpacing: -0.3 }}>
              {currentOperator?.name || 'Operator'}
            </div>
            <div style={{
              fontSize: 11, fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-muted)', marginTop: 4,
            }}>
              {currentOperator?.callsign || 'OP-?'} · SOC Operator
            </div>
          </div>

          {/* Status + time */}
          <div style={{
            display: 'flex', alignItems: 'center', gap: 16,
            padding: '10px 24px', borderRadius: 8,
            background: 'var(--bg-secondary, #0E1117)',
            border: '1px solid var(--border, rgba(255,255,255,0.07))',
          }}>
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: 'var(--text-muted)', marginBottom: 3 }}>Status</div>
              <div style={{ fontSize: 12, fontWeight: 700, color: statusColor, display: 'flex', alignItems: 'center', gap: 6 }}>
                <span style={{ width: 6, height: 6, borderRadius: '50%', background: statusColor, boxShadow: `0 0 8px ${statusColor}` }} />
                {statusLabel}
              </div>
            </div>
            <div style={{ width: 1, height: 28, background: 'var(--border)' }} />
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: 'var(--text-muted)', marginBottom: 3 }}>Time</div>
              <div style={{ fontSize: 14, fontWeight: 700, fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>{time}</div>
            </div>
            <div style={{ width: 1, height: 28, background: 'var(--border)' }} />
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: 'var(--text-muted)', marginBottom: 3 }}>Fleet</div>
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--accent-green, #22C55E)' }}>
                {totalOnline} <span style={{ fontWeight: 400, color: 'var(--text-muted)', fontSize: 10 }}>online</span>
              </div>
            </div>
            <div style={{ width: 1, height: 28, background: 'var(--border)' }} />
            <div style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: 'var(--text-muted)', marginBottom: 3 }}>Handled</div>
              <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-primary)' }}>
                {shiftStats.alarmsHandled}
              </div>
            </div>
          </div>

          {/* Queue / alarm status */}
          {unackedAlarms > 0 ? (
            <div style={{
              padding: '14px 28px', borderRadius: 8,
              background: 'rgba(239,68,68,0.06)',
              border: '1px solid rgba(239,68,68,0.15)',
              textAlign: 'center',
              animation: 'op-alert-blink 2s ease-in-out infinite',
            }}>
              <div style={{ fontSize: 28, fontWeight: 700, color: '#EF4444', fontFamily: "'JetBrains Mono', monospace" }}>
                {unackedAlarms}
              </div>
              <div style={{ fontSize: 11, fontWeight: 600, color: '#EF4444', letterSpacing: 1, marginTop: 2 }}>
                ALARM{unackedAlarms > 1 ? 'S' : ''} PENDING
              </div>
              <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 6 }}>
                Click an alarm in the feed to begin investigation →
              </div>
            </div>
          ) : (
            <div style={{
              padding: '14px 28px', borderRadius: 8,
              background: 'var(--bg-secondary, #0E1117)',
              border: '1px solid var(--border, rgba(255,255,255,0.07))',
              textAlign: 'center',
            }}>
              <div style={{ fontSize: 11, color: 'var(--text-secondary)', letterSpacing: 0.5 }}>
                No active alarms — monitoring {sites.length} sites
              </div>
              <div style={{ fontSize: 9, color: 'var(--text-muted)', marginTop: 4 }}>
                Alarms will appear in the feed when triggered during monitoring hours
              </div>
            </div>
          )}

          {/* Operators on shift */}
          <div style={{
            padding: '12px 20px', borderRadius: 8,
            background: 'var(--bg-secondary, #0E1117)',
            border: '1px solid var(--border, rgba(255,255,255,0.07))',
            width: '100%', maxWidth: 400,
          }}>
            <OperatorRoster />
          </div>
        </div>

        {/* ── RIGHT: Alarm Feed ── */}
        <div style={{ borderLeft: '1px solid var(--border, rgba(255,255,255,0.07))', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
          <ErrorBoundary>
            <AlertFeed
              alerts={displayAlerts}
              onAlertClick={alert => {
                engageAlarm(alert);
                router.push(`/operator/alarm/${alert.id}`);
              }}
              onIncidentClick={incident => {
                router.push(`/operator/alarm/${incident.id}`);
              }}
            />
          </ErrorBoundary>
        </div>
      </div>

      {/* ── Modals ── */}
      {showHandoff && <ShiftHandoffModal onClose={() => setShowHandoff(false)} />}
      {showShortcuts && <ShortcutOverlay onClose={() => setShowShortcuts(false)} />}

      {/* ── Wrap-up overlay (shown after resolving an alarm) ── */}
      {operatorStatus === 'wrap_up' && lastDisposition && (
        <WrapUpOverlay
          lastDisposition={lastDisposition}
          shiftStats={shiftStats}
          onReady={exitWrapUp}
          onExportEvidence={() => setShowEvidence(true)}
        />
      )}

      {/* ── Evidence export modal ── */}
      {showEvidence && lastDisposition && (
        <EvidenceExportModal
          lastDisposition={lastDisposition}
          operatorName={currentOperator?.name || 'Operator'}
          operatorCallsign={currentOperator?.callsign || 'OP-?'}
          onClose={() => setShowEvidence(false)}
        />
      )}
    </div>
  );
}

export default function OperatorConsolePage() {
  return <OperatorConsoleInner />;
}
