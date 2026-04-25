'use client';

import { useState, useEffect, useRef } from 'react';
import type { AlertEvent, SOCIncident, SiteSOP, SiteDetail, SiteCamera } from '@/types/ironsight';
import { useOperatorStore, DISPOSITION_OPTIONS, type DispositionCode } from '@/stores/operator-store';
import { getSiteSOPs, listSecurityEvents, createSecurityEvent, previewAVSScore, type SecurityEventRecord, type AVSFactors } from '@/lib/ironsight-api';
import { fireDeterrence } from '@/lib/api';
import { useSite, useSiteCameras } from '@/hooks/useSites';
import SeverityPill from '@/components/shared/SeverityPill';
import SiteMapModal from '@/components/operator/SiteMapModal';
import AVSFactorChecklist from '@/components/operator/AVSFactorChecklist';

interface Props {
  alarm: AlertEvent;
  incident?: SOCIncident;
  childAlarms?: AlertEvent[];
  onResolved: () => void;
}

const BG_SCENES = [
  'linear-gradient(180deg, #0a0f08 0%, #141a0c 40%, #1a200e 100%)',
  'linear-gradient(160deg, #0a1520, #151f10 40%, #0d1818)',
  'linear-gradient(200deg, #0f1510, #1a1208 60%, #0c1015)',
  'linear-gradient(140deg, #08121a, #12100a 50%, #0a1510)',
];

const DISPOSITION_ICONS: Record<DispositionCode, string> = {
  false_positive_animal: '🐾',
  false_positive_weather: '🌩️',
  false_positive_shadow: '💡',
  false_positive_equipment: '⚙️',
  false_positive_other: '❓',
  verified_customer_notified: '📞',
  verified_police_dispatched: '🚔',
  verified_guard_responded: '🛡️',
  verified_no_threat: '✅',
  verified_other: '⚠️',
};

const DISPOSITION_SHORT: Record<DispositionCode, string> = {
  false_positive_animal: 'Animal',
  false_positive_weather: 'Weather',
  false_positive_shadow: 'Shadow / Light',
  false_positive_equipment: 'Equipment',
  false_positive_other: 'Other',
  verified_customer_notified: 'Customer Notified',
  verified_police_dispatched: 'Police Dispatched',
  verified_guard_responded: 'Guard Responded',
  verified_no_threat: 'No Active Threat',
  verified_other: 'Other',
};

export default function ActiveAlarmView({ alarm, incident, childAlarms, onResolved }: Props) {
  const actionLog = useOperatorStore(s => s.actionLog);
  const addLogEntry = useOperatorStore(s => s.addActionLogEntry);
  const resolveAlarm = useOperatorStore(s => s.resolveAlarm);
  const abandonAlarm = useOperatorStore(s => s.abandonAlarm);
  const escalateActiveAlarm = useOperatorStore(s => s.escalateActiveAlarm);
  const currentOperator = useOperatorStore(s => s.currentOperator);
  const acknowledgeIncident = useOperatorStore(s => s.acknowledgeIncident);

  // ── Incident-aware: selected child alarm for camera switching ──
  const [selectedChildAlarm, setSelectedChildAlarm] = useState<AlertEvent | null>(null);
  const displayAlarm = selectedChildAlarm || alarm;

  // ── State ──
  const [viewMode, setViewMode] = useState<'snapshot' | 'clip' | 'live'>('snapshot');
  const [showBoxes, setShowBoxes] = useState(true);
  const [workflowTab, setWorkflowTab] = useState<'assess' | 'respond' | 'resolve'>('assess');
  const [sops, setSOPs] = useState<SiteSOP[]>([]);
  const [sopChecked, setSopChecked] = useState<Record<string, boolean[]>>({});
  const [disposition, setDisposition] = useState<DispositionCode | ''>('');
  const [notes, setNotes] = useState('');

  // TMA-AVS-01 validation factors. Captured during disposition so the
  // backend can compute the alarm validation score for any downstream
  // PSAP / central-station forwarding. Initialized to "video_verified
  // = true" because the SOC's contractual baseline is video-verified
  // alarms only — operators uncheck it on the rare cases where they
  // dispositioned without seeing video (e.g., heard audio but blocked
  // camera view). All other factors stay false until explicitly
  // marked, so the score floor is "VERIFIED" (2) by default.
  const [avsFactors, setAvsFactors] = useState<AVSFactors>({
    video_verified: true,
    person_detected: false,
    suspicious_behavior: false,
    weapon_observed: false,
    active_crime: false,
    multi_camera_evidence: false,
    multi_sensor_evidence: false,
    audio_verified: false,
    talkdown_ignored: false,
    auth_failure: false,
    ai_corroborated: false,
  });
  const avsPreview = previewAVSScore(avsFactors);
  const toggleAVS = (k: keyof AVSFactors) => setAvsFactors(prev => ({ ...prev, [k]: !prev[k] }));
  const [submitting, setSubmitting] = useState(false);
  const [selectedAdjacentCam, setSelectedAdjacentCam] = useState<string | null>(null);
  const [showSiteMap, setShowSiteMap] = useState(false);
  const [keyPrefix, setKeyPrefix] = useState<'f' | 'v' | null>(null);
  const [recentEvents, setRecentEvents] = useState<SecurityEventRecord[]>([]);
  const [showEscalateConfirm, setShowEscalateConfirm] = useState(false);

  // Active deterrence: operator can fire the camera's strobe / siren via
  // the DeviceIO relay outputs. showDeterrence reveals the action menu;
  // deterrenceBusy disables re-clicks during the ONVIF round-trip. Last
  // outcome feeds a brief status toast so the operator knows it worked.
  const [showDeterrence, setShowDeterrence] = useState(false);
  const [deterrenceBusy, setDeterrenceBusy] = useState(false);
  const [deterrenceStatus, setDeterrenceStatus] = useState<{ ok: boolean; msg: string } | null>(null);

  const actionLogRef = useRef<HTMLDivElement>(null);
  const keyPrefixTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // ── Load site detail and cameras from real API ──
  const { data: siteData } = useSite(alarm.site_id);
  const { data: rawCameras = [] } = useSiteCameras(alarm.site_id);
  const siteCameras: SiteCamera[] = (rawCameras as any[]).map(c => ({
    id: String(c.id),
    name: c.name,
    location: c.camera_group || c.location || '',
    status: (c.status as 'online' | 'offline' | 'degraded') ?? 'offline',
    has_alert: String(c.id) === alarm.camera_id,
    stream_url: `/hls/${c.id}/main_live.m3u8`,
  }));
  const siteNotes: string[] = (siteData as any)?.site_notes ?? (siteData as any)?.risk_notes ?? [];
  const triggeredCamera = siteCameras.find(c => c.id === alarm.camera_id);
  const adjacentCameras = siteCameras.filter(c => c.id !== alarm.camera_id).slice(0, 4);

  useEffect(() => {
    getSiteSOPs(alarm.site_id).then(data => {
      setSOPs(data);
      const checks: Record<string, boolean[]> = {};
      data.forEach(sop => { checks[sop.id] = sop.steps.map(() => false); });
      setSopChecked(checks);
    });
    listSecurityEvents(alarm.site_id).then(events => {
      // Exclude the current alarm; show the 5 most recent
      setRecentEvents(events.filter(e => e.alarm_id !== alarm.id).slice(0, 5));
    });
  }, [alarm.site_id, alarm.id]);

  // ── SLA tracking ──
  const [elapsedMs, setElapsedMs] = useState(Date.now() - alarm.ts);
  useEffect(() => {
    const timer = setInterval(() => setElapsedMs(Date.now() - alarm.ts), 1000);
    return () => clearInterval(timer);
  }, [alarm.ts]);

  const elapsedMin = Math.floor(elapsedMs / 60000);
  const elapsedSec = Math.floor((elapsedMs % 60000) / 1000);
  const slaColor = elapsedMs < 30000 ? '#22C55E' : elapsedMs < 90000 ? '#E89B2A' : '#EF4444';

  // ── Auto-scroll action log to bottom on new entries ──
  useEffect(() => {
    if (actionLogRef.current) {
      actionLogRef.current.scrollTop = actionLogRef.current.scrollHeight;
    }
  }, [actionLog.length]);

  // ── Keyboard shortcuts: F+1–5 = false positive, V+1–5 = verified ──
  useEffect(() => {
    const falseCodes = DISPOSITION_OPTIONS.filter(d => d.category === 'false');
    const verifiedCodes = DISPOSITION_OPTIONS.filter(d => d.category === 'verified');

    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as Element).tagName;
      if (tag === 'TEXTAREA' || tag === 'INPUT' || tag === 'SELECT') return;

      const key = e.key.toLowerCase();

      if (key === 'f' || key === 'v') {
        if (keyPrefixTimer.current) clearTimeout(keyPrefixTimer.current);
        setKeyPrefix(key as 'f' | 'v');
        keyPrefixTimer.current = setTimeout(() => setKeyPrefix(null), 1500);
        return;
      }

      const digit = parseInt(e.key);
      if (keyPrefix && digit >= 1 && digit <= 5) {
        e.preventDefault();
        const opts = keyPrefix === 'f' ? falseCodes : verifiedCodes;
        if (opts[digit - 1]) setDisposition(opts[digit - 1].code);
        setKeyPrefix(null);
        if (keyPrefixTimer.current) clearTimeout(keyPrefixTimer.current);
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [keyPrefix]);

  // ── Quick-log buttons ──
  const quickLog = (text: string) => addLogEntry(text, true);

  // ── SOP checklist toggle ──
  const toggleSopStep = (sopId: string, stepIdx: number) => {
    setSopChecked(prev => {
      const updated = { ...prev, [sopId]: [...(prev[sopId] || [])] };
      updated[sopId][stepIdx] = !updated[sopId][stepIdx];
      return updated;
    });
  };

  // ── Phone number copy ──
  const copyPhone = (phone: string, contactName: string) => {
    navigator.clipboard.writeText(phone).catch(() => {});
    addLogEntry(`Copied phone: ${contactName} (${phone})`, true);
  };

  // ── Submit disposition ──
  const handleSubmit = async () => {
    if (!disposition) return;
    setSubmitting(true);
    const dispLabel = DISPOSITION_OPTIONS.find(d => d.code === disposition)?.label || disposition;
    addLogEntry(`Disposition: ${dispLabel}`, false);
    if (notes.trim()) addLogEntry(`Notes: ${notes.trim()}`, false);
    // Persist to backend before clearing local state
    await createSecurityEvent({
      alarm_id: alarm.id,
      site_id: alarm.site_id,
      camera_id: alarm.camera_id,
      disposition_code: disposition,
      disposition_label: dispLabel,
      operator_notes: notes,
      action_log: actionLog.map(e => ({ ts: e.ts, text: e.text, auto: e.auto })),
      escalation_depth: alarm.escalation_level ?? 0,
      severity: alarm.severity,
      type: alarm.type,
      description: alarm.description,
      avs_factors: avsFactors,
    });
    // If this is part of an incident, acknowledge the whole incident
    if (incident) {
      try {
        const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : null;
        await fetch(`/api/v1/incidents/${incident.id}/acknowledge`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(token ? { Authorization: `Bearer ${token}` } : {}),
          },
        });
      } catch { /* best-effort */ }
      acknowledgeIncident(incident.id);
    }
    resolveAlarm(disposition);
    setSubmitting(false);
    onResolved();
  };

  // ── Abandon (return alarm to queue) ──
  const handleAbandon = () => {
    abandonAlarm();
    onResolved();
  };

  // ── Disposition card groups ──
  const falseCodes = DISPOSITION_OPTIONS.filter(d => d.category === 'false');
  const verifiedCodes = DISPOSITION_OPTIONS.filter(d => d.category === 'verified');

  const isFalse = disposition.startsWith('false');

  return (
    <>
    <div style={{
      display: 'grid',
      gridTemplateColumns: '1fr 380px',
      gridTemplateRows: '1fr',
      height: '100vh',
      overflow: 'hidden',
      background: 'var(--sg-bg-base)',
    }}>
      {/* ═══ PANE 1: VIDEO INVESTIGATION (Left) ═══ */}
      <div style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', borderRight: '1px solid var(--sg-border)' }}>
        {/* Alarm header bar */}
        <div style={{
          padding: '8px 14px',
          background: 'var(--sg-bg-panel)',
          borderBottom: '1px solid var(--sg-border)',
          display: 'flex', alignItems: 'center', gap: 10,
          flexShrink: 0,
        }}>
          <div style={{
            width: 8, height: 8, borderRadius: '50%',
            background: '#EF4444',
            boxShadow: '0 0 8px rgba(255,51,85,0.6)',
            animation: 'sla-breach-pulse 1s infinite alternate',
          }} />
          <SeverityPill severity={alarm.severity} size="sm" />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 16, fontWeight: 700, color: '#E4E8F0', display: 'flex', alignItems: 'center', gap: 8 }}>
              {incident ? 'ACTIVE INCIDENT' : 'ACTIVE ALARM'} — {alarm.site_name}
              {incident && incident.alarm_count > 1 && (
                <span style={{
                  fontSize: 15, padding: '1px 6px', borderRadius: 3, fontWeight: 700,
                  background: 'rgba(232,155,42,0.12)',
                  border: '1px solid rgba(232,155,42,0.25)',
                  color: '#E89B2A',
                  fontFamily: "'JetBrains Mono', monospace",
                }}>
                  ×{incident.alarm_count}
                </span>
              )}
              {incident && incident.camera_ids.length > 1 && (
                <span style={{
                  fontSize: 15, padding: '1px 6px', borderRadius: 3, fontWeight: 700,
                  background: 'rgba(0,212,255,0.08)',
                  border: '1px solid rgba(0,212,255,0.2)',
                  color: '#00d4ff',
                  fontFamily: "'JetBrains Mono', monospace",
                }}>
                  {incident.camera_ids.length} cams
                </span>
              )}
            </div>
            <div style={{ fontSize: 16, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
              {incident ? incident.id : alarm.id} · {displayAlarm.camera_name} · {displayAlarm.type.replace(/_/g, ' ')}
            </div>
          </div>
          {/* SLA timer */}
          <div style={{
            padding: '4px 10px', borderRadius: 4,
            background: `${slaColor}12`, border: `1px solid ${slaColor}30`,
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: 16, fontWeight: 700, color: slaColor,
            letterSpacing: 1,
          }}>
            {elapsedMin}:{String(elapsedSec).padStart(2, '0')}
          </div>
          {/* Escalation level + button */}
          <div style={{ position: 'relative', flexShrink: 0 }}>
            {alarm.escalation_level > 0 && (
              <div style={{
                fontSize: 16, fontWeight: 700, padding: '2px 6px', borderRadius: 3, marginBottom: 4,
                background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.35)',
                color: '#EF4444', fontFamily: "'JetBrains Mono', monospace", textAlign: 'center',
              }}>
                ESC LEVEL {alarm.escalation_level}
              </div>
            )}
            {/* Active deterrence: fire strobe / siren on the camera with
                this alarm. Operator-only (backend guards customer/viewer).
                Uses a tiny popover rather than a modal so it's one-click
                away during a live incident. Each press writes an audit row. */}
            <div style={{ position: 'relative' }}>
              <button
                onClick={() => { setShowDeterrence(v => !v); setDeterrenceStatus(null); }}
                disabled={deterrenceBusy}
                style={{
                  padding: '5px 10px', borderRadius: 4, fontSize: 16, fontWeight: 600,
                  background: showDeterrence ? 'rgba(239,68,68,0.18)' : 'rgba(239,68,68,0.08)',
                  border: `1px solid ${showDeterrence ? 'rgba(239,68,68,0.45)' : 'rgba(239,68,68,0.25)'}`,
                  color: '#EF4444',
                  cursor: deterrenceBusy ? 'wait' : 'pointer', fontFamily: 'inherit',
                  opacity: deterrenceBusy ? 0.6 : 1,
                }}
              >
                🚨 Deterrence
              </button>
              {showDeterrence && (
                <div style={{
                  position: 'absolute', top: '100%', right: 0, marginTop: 4, zIndex: 50,
                  background: '#1A1F2A', border: '1px solid rgba(239,68,68,0.3)',
                  borderRadius: 6, padding: 10, width: 240, boxShadow: '0 8px 32px rgba(0,0,0,0.5)',
                }}>
                  <div style={{ fontSize: 14, fontWeight: 600, color: '#EF4444', marginBottom: 8 }}>
                    Fire on {alarm.camera_name}
                  </div>
                  <div style={{ fontSize: 12, color: '#6B7590', marginBottom: 8, lineHeight: 1.4 }}>
                    Each action is audited with your username and time.
                  </div>
                  {(['strobe', 'siren', 'both'] as const).map(act => (
                    <button
                      key={act}
                      onClick={async () => {
                        setDeterrenceBusy(true);
                        setDeterrenceStatus(null);
                        try {
                          const resp = await fireDeterrence(alarm.camera_id, act, {
                            durationSec: 15,
                            alarmID: alarm.id,
                            reason: `Operator fired from alarm ${alarm.id}`,
                          });
                          setDeterrenceStatus({ ok: true, msg: resp.message ?? `${act} fired` });
                        } catch (e) {
                          setDeterrenceStatus({ ok: false, msg: String(e) });
                        } finally {
                          setDeterrenceBusy(false);
                          setTimeout(() => setShowDeterrence(false), 1600);
                        }
                      }}
                      disabled={deterrenceBusy}
                      style={{
                        width: '100%', marginBottom: 4, padding: '8px 0',
                        borderRadius: 4, fontSize: 13, fontWeight: 600,
                        background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                        color: '#EF4444', cursor: deterrenceBusy ? 'wait' : 'pointer',
                        fontFamily: 'inherit', opacity: deterrenceBusy ? 0.5 : 1,
                      }}
                    >
                      {act === 'strobe' ? '💡 Strobe Light' : act === 'siren' ? '🔊 Siren' : '⚡ Strobe + Siren'}
                    </button>
                  ))}
                  {deterrenceStatus && (
                    <div style={{
                      marginTop: 6, padding: '6px 8px', borderRadius: 4, fontSize: 11,
                      background: deterrenceStatus.ok ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)',
                      color: deterrenceStatus.ok ? '#22C55E' : '#EF4444',
                      border: `1px solid ${deterrenceStatus.ok ? 'rgba(34,197,94,0.3)' : 'rgba(239,68,68,0.3)'}`,
                    }}>
                      {deterrenceStatus.ok ? '✓ ' : '✕ '}{deterrenceStatus.msg}
                    </div>
                  )}
                </div>
              )}
            </div>
            <button
              onClick={() => setShowEscalateConfirm(v => !v)}
              style={{
                padding: '5px 10px', borderRadius: 4, fontSize: 16, fontWeight: 600,
                background: showEscalateConfirm ? 'rgba(239,68,68,0.12)' : 'rgba(255,153,0,0.06)',
                border: `1px solid ${showEscalateConfirm ? 'rgba(239,68,68,0.35)' : 'rgba(255,153,0,0.2)'}`,
                color: showEscalateConfirm ? '#EF4444' : '#E89B2A',
                cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              ↑ Escalate
            </button>
            {showEscalateConfirm && (
              <div style={{
                position: 'absolute', top: '100%', right: 0, marginTop: 4, zIndex: 50,
                background: '#1A1F2A', border: '1px solid rgba(239,68,68,0.3)',
                borderRadius: 6, padding: 12, width: 200, boxShadow: '0 8px 32px rgba(0,0,0,0.5)',
              }}>
                <div style={{ fontSize: 16, fontWeight: 600, color: '#EF4444', marginBottom: 6 }}>
                  Escalate to Level {(alarm.escalation_level || 0) + 1}?
                </div>
                <div style={{ fontSize: 15, color: '#6B7590', marginBottom: 10, lineHeight: 1.4 }}>
                  Notifies next contact in call tree and extends SLA window.
                </div>
                <div style={{ display: 'flex', gap: 6 }}>
                  <button
                    onClick={() => {
                      escalateActiveAlarm();
                      setShowEscalateConfirm(false);
                    }}
                    style={{
                      flex: 1, padding: '6px 0', borderRadius: 4, fontSize: 16, fontWeight: 700,
                      background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.4)',
                      color: '#EF4444', cursor: 'pointer', fontFamily: 'inherit',
                    }}
                  >
                    Escalate
                  </button>
                  <button
                    onClick={() => setShowEscalateConfirm(false)}
                    style={{
                      padding: '6px 10px', borderRadius: 4, fontSize: 16, fontWeight: 600,
                      background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                      color: '#4A5268', cursor: 'pointer', fontFamily: 'inherit',
                    }}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </div>

          <button
            onClick={handleAbandon}
            style={{
              padding: '5px 10px', borderRadius: 4, fontSize: 16, fontWeight: 600,
              background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
              color: '#4A5268', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ↩ Release
          </button>
        </div>

        {/* Video viewer + mode toggle */}
        <div style={{ flex: 1, position: 'relative', overflow: 'hidden', background: '#080a06' }}>
          {/* 3-button mode toggle */}
          <div style={{
            position: 'absolute', top: 10, left: 10, zIndex: 20,
            display: 'flex', gap: 2, background: 'rgba(0,0,0,0.75)',
            borderRadius: 4, padding: 2, backdropFilter: 'blur(8px)',
          }}>
            {/* SNAPSHOT */}
            <button
              onClick={() => { setViewMode('snapshot'); addLogEntry('Viewed event snapshot', true); }}
              style={{
                padding: '5px 12px', borderRadius: 3, fontSize: 16, fontWeight: 700,
                background: viewMode === 'snapshot' ? 'rgba(234,179,8,0.2)' : 'transparent',
                border: viewMode === 'snapshot' ? '1px solid rgba(234,179,8,0.5)' : '1px solid transparent',
                color: viewMode === 'snapshot' ? '#EAB308' : '#4A5268',
                cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
                letterSpacing: 1,
              }}
            >
              ◉ SNAPSHOT
            </button>
            {/* CLIP */}
            <button
              onClick={() => { setViewMode('clip'); addLogEntry('Reviewing event clip', true); }}
              style={{
                padding: '5px 12px', borderRadius: 3, fontSize: 16, fontWeight: 700,
                background: viewMode === 'clip' ? 'rgba(0,212,255,0.15)' : 'transparent',
                border: viewMode === 'clip' ? '1px solid rgba(0,212,255,0.35)' : '1px solid transparent',
                color: viewMode === 'clip' ? '#00d4ff' : '#4A5268',
                cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
                letterSpacing: 1,
              }}
            >
              ▶ CLIP
            </button>
            {/* LIVE — opens NVR in new tab */}
            <button
              onClick={() => {
                addLogEntry('Opened NVR for situational awareness', true);
                window.open(`/?site_id=${alarm.site_id}`, '_blank');
              }}
              style={{
                padding: '5px 12px', borderRadius: 3, fontSize: 16, fontWeight: 700,
                background: 'transparent',
                border: '1px solid transparent',
                color: '#4A5268',
                cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
                letterSpacing: 1,
              }}
            >
              ↗ LIVE NVR
            </button>

            {/* Bounding box toggle */}
            <button
              onClick={() => setShowBoxes(v => !v)}
              style={{
                padding: '5px 12px', borderRadius: 3, fontSize: 16, fontWeight: 700,
                background: showBoxes ? 'rgba(59,130,246,0.2)' : 'transparent',
                border: showBoxes ? '1px solid rgba(59,130,246,0.5)' : '1px solid transparent',
                color: showBoxes ? '#3B82F6' : '#4A5268',
                cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
                letterSpacing: 1,
              }}
            >
              {showBoxes ? '▣' : '▢'} BBOX
            </button>
          </div>

          {/* Status badge */}
          <div style={{
            position: 'absolute', top: 10, right: 10, zIndex: 20,
            display: 'flex', gap: 4, flexDirection: 'column', alignItems: 'flex-end',
          }}>
            <span style={{
              fontSize: 16, padding: '2px 6px', borderRadius: 2, fontWeight: 700,
              background: viewMode === 'snapshot' ? 'rgba(234,179,8,0.2)' : 'rgba(0,212,255,0.15)',
              color: viewMode === 'snapshot' ? '#EAB308' : '#00d4ff',
              letterSpacing: 1, fontFamily: "'JetBrains Mono', monospace",
            }}>
              {viewMode === 'snapshot' ? '◉ EVENT' : '▶ CLIP'}
            </span>
            <span style={{
              fontSize: 16, padding: '2px 6px', borderRadius: 2, fontWeight: 600,
              background: 'rgba(0,212,255,0.12)', color: '#E8732A',
              letterSpacing: 1, fontFamily: "'JetBrains Mono', monospace",
              border: '1px solid rgba(0,212,255,0.2)',
            }}>
              ⊕ AI VERIFIED
            </span>
            {displayAlarm.ai_score != null && displayAlarm.ai_score > 0 && (
              <span style={{
                fontSize: 16, padding: '2px 6px', borderRadius: 2, fontWeight: 700,
                background: displayAlarm.ai_score >= 0.8 ? 'rgba(239,68,68,0.15)' : displayAlarm.ai_score >= 0.6 ? 'rgba(232,155,42,0.15)' : 'rgba(74,82,104,0.15)',
                color: displayAlarm.ai_score >= 0.8 ? '#EF4444' : displayAlarm.ai_score >= 0.6 ? '#E89B2A' : '#4A5268',
                border: `1px solid ${displayAlarm.ai_score >= 0.8 ? 'rgba(239,68,68,0.3)' : displayAlarm.ai_score >= 0.6 ? 'rgba(232,155,42,0.3)' : 'rgba(74,82,104,0.3)'}`,
                letterSpacing: 0.5, fontFamily: "'JetBrains Mono', monospace",
              }}>
                {Math.round(displayAlarm.ai_score * 100)}% {displayAlarm.obj_type || 'CONF'}
              </span>
            )}
            {displayAlarm.rule_name && (
              <span style={{
                fontSize: 16, padding: '2px 6px', borderRadius: 2, fontWeight: 600,
                background: 'rgba(168,85,247,0.1)',
                color: '#A855F7',
                border: '1px solid rgba(168,85,247,0.2)',
                letterSpacing: 0.5, fontFamily: "'JetBrains Mono', monospace",
              }}>
                {displayAlarm.rule_name}
              </span>
            )}
          </div>

          {/* Video content — key forces remount when switching between child alarms */}
          <AlarmVideoFeed
            key={displayAlarm.id}
            cameraId={displayAlarm.camera_id}
            snapshotUrl={displayAlarm.snapshot_url || undefined}
            clipUrl={displayAlarm.clip_url || undefined}
            alarmType={displayAlarm.type}
            mode={viewMode}
            boundingBoxes={displayAlarm.bounding_boxes}
            yoloDetections={displayAlarm.ai_detections}
          />

          {/* YOLO bounding boxes — positioned with CSS percentages to match image */}
          {showBoxes && displayAlarm.ai_detections && displayAlarm.ai_detections.length > 0 &&
            displayAlarm.ai_detections.map((det, idx) => {
              const b = det.bbox_normalized;
              if (!b) return null;
              const conf = Math.round(det.confidence * 100);
              return (
                <div key={idx} style={{
                  position: 'absolute',
                  left: `${b.x1 * 100}%`,
                  top: `${b.y1 * 100}%`,
                  width: `${(b.x2 - b.x1) * 100}%`,
                  height: `${(b.y2 - b.y1) * 100}%`,
                  border: '2px solid #3B82F6',
                  background: 'rgba(59,130,246,0.08)',
                  pointerEvents: 'none',
                  zIndex: 20,
                }}>
                  <div style={{
                    position: 'absolute', top: -20, left: -2,
                    background: 'rgba(59,130,246,0.9)',
                    color: '#fff', fontSize: 16, fontWeight: 700,
                    padding: '2px 8px', borderRadius: '3px 3px 0 0',
                    fontFamily: "'JetBrains Mono', monospace",
                    whiteSpace: 'nowrap',
                  }}>
                    {det.class.toUpperCase()} {conf}%
                  </div>
                </div>
              );
            })
          }

          {/* Camera info overlay */}
          <div style={{
            position: 'absolute', bottom: 0, left: 0, right: 0,
            padding: '30px 14px 10px',
            background: 'linear-gradient(to top, rgba(0,0,0,0.8), transparent)',
            display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end',
            pointerEvents: 'none',
          }}>
            <div>
              <div style={{ fontSize: 15, fontWeight: 600, color: 'rgba(255,255,255,0.85)' }}>{displayAlarm.camera_name}</div>
              <div style={{ fontSize: 16, color: 'rgba(255,255,255,0.4)', fontFamily: "'JetBrains Mono', monospace" }}>
                {displayAlarm.camera_id.toUpperCase()} · {triggeredCamera?.location || ''} · 30 FPS
              </div>
            </div>
            <div style={{ fontSize: 15, color: 'rgba(255,255,255,0.3)', fontFamily: "'JetBrains Mono', monospace", textAlign: 'right' }}>
              {new Date(displayAlarm.ts).toLocaleTimeString('en-US', { hour12: false })}
            </div>
          </div>
        </div>

        {/* Adjacent cameras strip */}
        <div style={{
          height: 110, flexShrink: 0,
          background: 'var(--sg-bg-panel)',
          borderTop: '1px solid var(--sg-border)',
          display: 'flex',
          overflow: 'hidden',
        }}>
          <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
            {childAlarms && childAlarms.length > 0 ? (
              /* ── Incident mode: show camera tabs from child alarms ── */
              (() => {
                // Dedupe cameras: pick the latest alarm per unique camera_id
                const camMap = new Map<string, AlertEvent>();
                childAlarms.forEach(a => {
                  const existing = camMap.get(a.camera_id);
                  if (!existing || a.ts > existing.ts) camMap.set(a.camera_id, a);
                });
                const uniqueCamAlarms = Array.from(camMap.values());
                return uniqueCamAlarms.map((camAlarm, idx) => {
                  const isActive = displayAlarm.camera_id === camAlarm.camera_id;
                  return (
                    <div
                      key={camAlarm.camera_id}
                      onClick={() => {
                        setSelectedChildAlarm(camAlarm.id === selectedChildAlarm?.id ? null : camAlarm);
                        setViewMode('snapshot');
                        addLogEntry(`Switched to incident camera: ${camAlarm.camera_name}`, true);
                      }}
                      style={{
                        flex: 1, position: 'relative', cursor: 'pointer',
                        borderRight: '1px solid var(--sg-border)',
                        outline: isActive ? '2px solid #E8732A' : undefined,
                        outlineOffset: -2,
                      }}
                    >
                      {camAlarm.snapshot_url ? (
                        <img
                          src={camAlarm.snapshot_url}
                          alt={camAlarm.camera_name}
                          style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' }}
                          onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
                        />
                      ) : (
                        <div style={{ position: 'absolute', inset: 0, background: BG_SCENES[(idx + 1) % BG_SCENES.length] }} />
                      )}
                      <div style={{
                        position: 'absolute', bottom: 0, left: 0, right: 0, padding: '3px 6px',
                        background: 'rgba(0,0,0,0.75)',
                        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                      }}>
                        <span style={{
                          fontSize: 16, fontFamily: "'JetBrains Mono', monospace",
                          color: isActive ? '#E8732A' : 'rgba(255,255,255,0.6)',
                          fontWeight: isActive ? 700 : 400,
                        }}>
                          {camAlarm.camera_name}
                        </span>
                        {isActive && (
                          <span style={{
                            fontSize: 16, padding: '1px 4px', borderRadius: 2,
                            background: 'rgba(232,115,42,0.2)', color: '#E8732A',
                            fontWeight: 700, letterSpacing: 0.5,
                          }}>
                            VIEWING
                          </span>
                        )}
                      </div>
                    </div>
                  );
                });
              })()
            ) : (
              /* ── Standard mode: adjacent site cameras ── */
              adjacentCameras.map((cam, idx) => (
                <div
                  key={cam.id}
                  onClick={() => {
                    setSelectedAdjacentCam(cam.id === selectedAdjacentCam ? null : cam.id);
                    addLogEntry(`Viewed adjacent camera: ${cam.name}`, true);
                  }}
                  style={{
                    flex: 1, position: 'relative', cursor: 'pointer',
                    borderRight: '1px solid var(--sg-border)',
                    outline: selectedAdjacentCam === cam.id ? '2px solid #E8732A' : undefined,
                    outlineOffset: -2,
                  }}
                >
                  <div style={{ position: 'absolute', inset: 0, background: BG_SCENES[(idx + 1) % BG_SCENES.length] }} />
                  <div style={{
                    position: 'absolute', bottom: 0, left: 0, right: 0, padding: '3px 6px',
                    background: 'rgba(0,0,0,0.75)',
                    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                  }}>
                    <span style={{ fontSize: 16, color: 'rgba(255,255,255,0.6)', fontFamily: "'JetBrains Mono', monospace" }}>
                      {cam.name}
                    </span>
                    <span style={{
                      width: 5, height: 5, borderRadius: '50%',
                      background: cam.status === 'online' ? '#22C55E' : '#EF4444',
                    }} />
                  </div>
                </div>
              ))
            )}
          </div>

          {/* Site map toggle */}
          <div
            onClick={() => setShowSiteMap(v => !v)}
            style={{
              width: 130, flexShrink: 0,
              background: showSiteMap ? 'rgba(0,212,255,0.08)' : 'var(--sg-bg-card)',
              borderLeft: '1px solid var(--sg-border)',
              display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
              cursor: 'pointer', gap: 4,
              transition: 'background 0.15s',
              outline: showSiteMap ? '1px solid rgba(0,212,255,0.3)' : undefined,
              outlineOffset: -1,
            }}
          >
            <span style={{ fontSize: 20 }}>🗺️</span>
            <span style={{ fontSize: 16, color: showSiteMap ? '#E8732A' : '#8891A5', fontFamily: "'JetBrains Mono', monospace", letterSpacing: 1, transition: 'color 0.15s' }}>
              SITE MAP
            </span>
          </div>
        </div>
      </div>

      {/* ═══ RIGHT PANE: 3-TAB WORKFLOW (Assess / Respond / Resolve) ═══ */}
      <div style={{
        display: 'flex', flexDirection: 'column',
        overflow: 'hidden',
        background: 'var(--sg-bg-panel)',
      }}>
        {/* ── Tab bar ── */}
        <div style={{
          display: 'flex', flexShrink: 0,
          borderBottom: '1px solid rgba(255,255,255,0.08)',
          background: 'rgba(0,0,0,0.15)',
        }}>
          {([
            { key: 'assess' as const, label: '1 ASSESS' },
            { key: 'respond' as const, label: '2 RESPOND' },
            { key: 'resolve' as const, label: '3 RESOLVE' },
          ]).map(tab => {
            const active = workflowTab === tab.key;
            return (
              <button
                key={tab.key}
                onClick={() => setWorkflowTab(tab.key)}
                style={{
                  flex: 1, padding: '8px 0', cursor: 'pointer',
                  background: 'transparent', border: 'none', outline: 'none',
                  borderBottom: `2px solid ${active ? '#8891A5' : 'transparent'}`,
                  color: active ? '#E4E8F0' : '#4A5268',
                  fontSize: 15, fontWeight: 700, letterSpacing: 1.2, textTransform: 'uppercase',
                  fontFamily: 'inherit',
                  transition: 'all 0.15s',
                }}
              >
                {tab.label}
              </button>
            );
          })}
        </div>

        {/* ── Tab content (scrollable) ── */}
        <div style={{ flex: 1, overflowY: 'auto', scrollbarWidth: 'thin' }}>

          {/* ════════ TAB 1: ASSESS ════════ */}
          {workflowTab === 'assess' && (
            <>
              {/* ── AI Assessment (collapsed summary) ── */}
              {(displayAlarm.ai_description || displayAlarm.ai_score || displayAlarm.rule_name) && (
                <div style={{
                  padding: '12px 14px',
                  borderBottom: '1px solid var(--sg-border)',
                }}>
                  <div style={{
                    fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                    color: '#6B7590', marginBottom: 10,
                  }}>
                    AI ASSESSMENT
                  </div>

                  {/* Combined description */}
                  <div style={{ fontSize: 15, color: '#E4E8F0', lineHeight: 1.6, marginBottom: 6 }}>
                    {displayAlarm.ai_description
                      ? displayAlarm.ai_description
                      : (
                        <>
                          {displayAlarm.rule_name || displayAlarm.type.replace(/_/g, ' ')} triggered
                          {displayAlarm.obj_type && ` — ${displayAlarm.obj_type} detected`}
                          {displayAlarm.ai_score != null && displayAlarm.ai_score > 0 && ` (${Math.round(displayAlarm.ai_score * 100)}% confidence)`}
                          .
                        </>
                      )}
                  </div>

                  {/* YOLO detections as plain text line */}
                  {displayAlarm.ai_detections && displayAlarm.ai_detections.length > 0 && (
                    <div style={{ fontSize: 15, color: '#8891A5', lineHeight: 1.5, marginBottom: 6, fontFamily: "'JetBrains Mono', monospace" }}>
                      YOLO: {displayAlarm.ai_detections.map(d => `${d.class} ${Math.round(d.confidence * 100)}%`).join(', ')}
                    </div>
                  )}

                  {/* PPE Violations callout — amber/red for safety compliance */}
                  {displayAlarm.ai_ppe_violations && displayAlarm.ai_ppe_violations.length > 0 && (
                    <div style={{
                      marginTop: 8, marginBottom: 6,
                      padding: '8px 12px',
                      background: 'rgba(239,68,68,0.06)',
                      border: '1px solid rgba(239,68,68,0.25)',
                      borderRadius: 4,
                    }}>
                      <div style={{
                        fontSize: 12, fontWeight: 700, letterSpacing: 1, color: '#EF4444',
                        marginBottom: 4, display: 'flex', alignItems: 'center', gap: 6,
                      }}>
                        <span style={{ fontSize: 14 }}>⚠</span> PPE VIOLATION
                      </div>
                      <div style={{ fontSize: 14, color: '#E4E8F0', lineHeight: 1.4 }}>
                        {displayAlarm.ai_ppe_violations.map(v => {
                          const label = (v as any).missing || v.class.replace(/^no-?/i, '').replace(/[-_]/g, ' ').replace(/\b\w/g, c => c.toUpperCase()).trim();
                          return `Missing ${label} (${Math.round(v.confidence * 100)}%)`;
                        }).join(' · ')}
                      </div>
                    </div>
                  )}

                  {/* Recommended action — subtle amber text */}
                  {displayAlarm.ai_recommended_action && (
                    <div style={{ fontSize: 15, color: '#E89B2A', lineHeight: 1.5, marginTop: 4 }}>
                      {displayAlarm.ai_recommended_action}
                    </div>
                  )}

                  {/* Operator feedback on AI assessment */}
                  <AIFeedbackButtons alarmId={displayAlarm.id} />
                </div>
              )}

              {/* Incident timeline (only shown for multi-alarm incidents) */}
              {childAlarms && childAlarms.length > 0 && (
                <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--sg-border)' }}>
                  <div style={{
                    fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                    color: '#6B7590', marginBottom: 8,
                  }}>
                    INCIDENT TIMELINE ({childAlarms.length} events)
                  </div>
                  {childAlarms.map((ca) => {
                    const isViewing = displayAlarm.id === ca.id;
                    return (
                      <div
                        key={ca.id}
                        onClick={() => {
                          setSelectedChildAlarm(ca.id === selectedChildAlarm?.id ? null : ca);
                          setViewMode('snapshot');
                          addLogEntry(`Timeline: switched to ${ca.camera_name} (${ca.type})`, true);
                        }}
                        style={{
                          display: 'flex', gap: 8, alignItems: 'flex-start',
                          padding: '5px 6px', marginBottom: 2, borderRadius: 4,
                          cursor: 'pointer',
                          background: 'transparent',
                          borderLeft: isViewing ? '2px solid #8891A5' : '2px solid transparent',
                          transition: 'all 0.15s',
                        }}
                      >
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
                            <span style={{
                              fontSize: 15, color: '#8891A5', fontFamily: "'JetBrains Mono', monospace",
                            }}>
                              {new Date(ca.ts).toLocaleTimeString('en-US', { hour12: false })}
                            </span>
                            <span style={{ fontSize: 15, color: isViewing ? '#E4E8F0' : '#6B7590' }}>
                              {ca.camera_name} · {ca.type.replace(/_/g, ' ')}
                            </span>
                            {isViewing && (
                              <span style={{
                                fontSize: 15, color: '#8891A5', fontWeight: 600, letterSpacing: 0.5,
                              }}>
                                VIEWING
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* Recent incidents at this site */}
              <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--sg-border)' }}>
                <div style={{
                  fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                  color: '#6B7590', marginBottom: 8,
                }}>
                  RECENT AT THIS SITE
                </div>
                {recentEvents.length === 0 ? (
                  <div style={{ fontSize: 16, color: '#4A5268', fontStyle: 'italic' }}>No previous incidents on record</div>
                ) : (
                  recentEvents.map((ev, i) => {
                    const evIsFalse = ev.disposition_code?.startsWith('false');
                    return (
                      <div key={ev.id} style={{
                        padding: '5px 0',
                        borderBottom: i < recentEvents.length - 1 ? '1px solid rgba(255,255,255,0.03)' : undefined,
                      }}>
                        <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
                          <span style={{ fontSize: 15, color: '#8891A5', fontFamily: "'JetBrains Mono', monospace" }}>
                            {new Date(ev.resolved_at || ev.ts).toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })}
                          </span>
                          {ev.disposition_code && (
                            <span style={{
                              fontSize: 15,
                              color: evIsFalse ? '#6B7590' : '#8891A5',
                            }}>
                              {ev.disposition_code.replace(/_/g, ' ')}
                            </span>
                          )}
                        </div>
                        <div style={{ fontSize: 15, color: '#4A5268', marginTop: 1 }}>
                          {ev.type?.replace(/_/g, ' ')} · {ev.operator_callsign || 'unknown op'}
                        </div>
                      </div>
                    );
                  })
                )}
              </div>

              {/* Next: Respond button */}
              <div style={{ padding: '12px 14px' }}>
                <button
                  onClick={() => setWorkflowTab('respond')}
                  style={{
                    width: '100%', padding: '8px 16px', borderRadius: 4,
                    fontSize: 15, fontWeight: 700, letterSpacing: 0.5,
                    fontFamily: 'inherit', cursor: 'pointer',
                    background: 'rgba(232,155,42,0.08)',
                    border: '1px solid rgba(232,155,42,0.25)',
                    color: '#E89B2A',
                    transition: 'all 0.15s',
                  }}
                >
                  Next: Respond →
                </button>
              </div>
            </>
          )}

          {/* ════════ TAB 2: RESPOND ════════ */}
          {workflowTab === 'respond' && (
            <>
              {/* Site notes */}
              <div style={{
                padding: '10px 14px',
                borderBottom: '1px solid var(--sg-border)',
              }}>
                <div style={{
                  fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                  color: '#6B7590', marginBottom: 6,
                }}>
                  SITE NOTES
                </div>
                {siteNotes.length > 0 ? (
                  siteNotes.map((note, i) => (
                    <div key={i} style={{
                      fontSize: 15, color: '#E4E8F0', lineHeight: 1.5, marginBottom: 4,
                      padding: '4px 8px', borderRadius: 3,
                      borderLeft: '2px solid rgba(255,255,255,0.15)',
                    }}>
                      {note}
                    </div>
                  ))
                ) : (
                  <div style={{ fontSize: 16, color: '#4A5268', fontStyle: 'italic' }}>No site notes</div>
                )}
              </div>

              {/* SOPs as interactive checklists */}
              <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--sg-border)' }}>
                <div style={{
                  fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                  color: '#6B7590', marginBottom: 8,
                }}>
                  RESPONSE PROCEDURES ({sops.length})
                </div>
                {sops.map(sop => {
                  const checks = sopChecked[sop.id] || [];
                  const completedCount = checks.filter(Boolean).length;
                  const pct = sop.steps.length > 0 ? completedCount / sop.steps.length : 0;
                  const allDone = completedCount === sop.steps.length && sop.steps.length > 0;
                  return (
                    <div key={sop.id} style={{
                      marginBottom: 10, borderRadius: 4,
                      border: '1px solid rgba(255,255,255,0.06)',
                      overflow: 'hidden',
                      transition: 'border-color 0.3s',
                    }}>
                      <div style={{
                        padding: '6px 10px',
                        background: 'rgba(255,255,255,0.02)',
                        borderBottom: '1px solid rgba(255,255,255,0.04)',
                      }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <span style={{ fontSize: 15, fontWeight: 600, color: '#E4E8F0', flex: 1 }}>{sop.title}</span>
                          <span style={{
                            fontSize: 16, fontFamily: "'JetBrains Mono', monospace",
                            color: allDone ? '#8891A5' : '#4A5268',
                          }}>
                            {completedCount}/{sop.steps.length}
                          </span>
                        </div>
                        {/* Progress bar */}
                        <div style={{
                          height: 3, background: '#4A5268', borderRadius: 2,
                          marginTop: 6, overflow: 'hidden',
                        }}>
                          <div style={{
                            height: '100%', borderRadius: 2,
                            width: `${pct * 100}%`,
                            background: '#8891A5',
                            transition: 'width 0.3s ease, background 0.3s ease',
                          }} />
                        </div>
                      </div>
                      <div style={{ padding: '6px 10px' }}>
                        {sop.steps.map((step, si) => (
                          <label
                            key={si}
                            style={{
                              display: 'flex', gap: 8, padding: '4px 0', cursor: 'pointer',
                              fontSize: 16, color: checks[si] ? '#4A5268' : '#8891A5',
                              textDecoration: checks[si] ? 'line-through' : 'none',
                              lineHeight: 1.4,
                            }}
                          >
                            <input
                              type="checkbox"
                              checked={checks[si] || false}
                              onChange={() => {
                                toggleSopStep(sop.id, si);
                                if (!checks[si]) addLogEntry(`SOP step completed: "${step.slice(0, 60)}"`, true);
                              }}
                              style={{ marginTop: 1, flexShrink: 0 }}
                            />
                            {step}
                          </label>
                        ))}
                      </div>
                    </div>
                  );
                })}
              </div>

              {/* Call tree */}
              <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--sg-border)' }}>
                <div style={{
                  fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                  color: '#6B7590', marginBottom: 8,
                }}>
                  CALL TREE
                </div>
                {sops.flatMap(sop => sop.contacts).filter((c, i, arr) => arr.findIndex(x => x.name === c.name) === i).map((contact, i) => (
                  <div key={i} style={{
                    display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px',
                    marginBottom: 4, borderRadius: 4,
                    background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)',
                  }}>
                    <div style={{
                      width: 20, height: 20, borderRadius: '50%', flexShrink: 0,
                      background: '#4A5268',
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      fontSize: 15, fontWeight: 700, color: '#E4E8F0',
                    }}>
                      {i + 1}
                    </div>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 15, fontWeight: 600, color: '#E4E8F0' }}>{contact.name}</div>
                      <div style={{ fontSize: 15, color: '#4A5268' }}>{contact.role}</div>
                    </div>
                    {contact.phone && (
                      <div style={{ display: 'flex', gap: 3, flexShrink: 0 }}>
                        <span style={{ fontSize: 16, fontFamily: "'JetBrains Mono', monospace", color: '#8891A5' }}>
                          {contact.phone}
                        </span>
                        <button
                          onClick={() => copyPhone(contact.phone!, contact.name)}
                          title="Copy phone number"
                          style={{
                            padding: '2px 6px', borderRadius: 3, fontSize: 15,
                            background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                            color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
                          }}
                        >
                          Copy
                        </button>
                      </div>
                    )}
                  </div>
                ))}

                {/* Quick-log buttons */}
                <div style={{
                  display: 'flex', gap: 4, flexWrap: 'wrap', marginTop: 8,
                  padding: '8px 0', borderTop: '1px solid rgba(255,255,255,0.04)',
                }}>
                  <span style={{ fontSize: 16, color: '#4A5268', width: '100%', marginBottom: 2, letterSpacing: 1, fontWeight: 600 }}>
                    QUICK LOG:
                  </span>
                  {[
                    { label: 'Spoke to Contact', text: 'Spoke to contact on call tree' },
                    { label: 'Left Voicemail', text: 'Left voicemail on call tree contact' },
                    { label: 'No Answer', text: 'No answer from call tree contact' },
                    { label: 'Police Dispatched', text: 'Local police dispatched to site' },
                    { label: 'Guard Notified', text: 'On-site guard notified and responding' },
                  ].map(btn => (
                    <button
                      key={btn.label}
                      onClick={() => quickLog(btn.text)}
                      style={{
                        padding: '4px 10px', borderRadius: 3, fontSize: 15, fontWeight: 600,
                        background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                        color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
                        transition: 'all 0.1s',
                      }}
                    >
                      {btn.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Next: Resolve button */}
              <div style={{ padding: '12px 14px' }}>
                <button
                  onClick={() => setWorkflowTab('resolve')}
                  style={{
                    width: '100%', padding: '8px 16px', borderRadius: 4,
                    fontSize: 15, fontWeight: 700, letterSpacing: 0.5,
                    fontFamily: 'inherit', cursor: 'pointer',
                    background: 'rgba(34,197,94,0.08)',
                    border: '1px solid rgba(34,197,94,0.25)',
                    color: '#22C55E',
                    transition: 'all 0.15s',
                  }}
                >
                  Next: Resolve →
                </button>
              </div>
            </>
          )}

          {/* ════════ TAB 3: RESOLVE ════════ */}
          {workflowTab === 'resolve' && (
            <>
              {/* ── Action log as timeline ── */}
              <div style={{ padding: '8px 14px 0' }}>
                <div style={{
                  fontSize: 16, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                  color: '#6B7590', marginBottom: 4,
                }}>
                  ACTION LOG ({actionLog.length})
                  {currentOperator && (
                    <span style={{ fontWeight: 400, marginLeft: 6, color: '#8891A5' }}>
                      {currentOperator.callsign}
                    </span>
                  )}
                </div>
              </div>
              <div
                ref={actionLogRef}
                style={{
                  maxHeight: 200, overflowY: 'auto', scrollbarWidth: 'thin',
                  padding: '4px 14px 8px',
                }}
              >
                {actionLog.length === 0 && (
                  <div style={{ fontSize: 16, color: '#4A5268', fontStyle: 'italic', paddingTop: 4 }}>
                    No actions yet
                  </div>
                )}
                {actionLog.map((entry, i) => (
                  <div
                    key={i}
                    style={{
                      display: 'flex', gap: 8, alignItems: 'flex-start',
                      paddingBottom: 5, marginBottom: 5,
                      borderBottom: i < actionLog.length - 1 ? '1px solid rgba(255,255,255,0.03)' : undefined,
                    }}
                  >
                    {/* Timeline indicator */}
                    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', paddingTop: 3, flexShrink: 0 }}>
                      <div style={{
                        width: 6, height: 6, borderRadius: '50%',
                        background: entry.auto ? '#4A5268' : '#8891A5',
                        flexShrink: 0,
                      }} />
                    </div>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 16, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", lineHeight: 1 }}>
                        {new Date(entry.ts).toLocaleTimeString('en-US', { hour12: false })}
                      </div>
                      <div style={{
                        fontSize: 16, lineHeight: 1.4, marginTop: 1,
                        color: entry.auto ? '#4A5268' : '#B8BDD0',
                        fontStyle: entry.auto ? 'italic' : 'normal',
                      }}>
                        {entry.text}
                      </div>
                    </div>
                  </div>
                ))}
              </div>

              {/* Manual note input */}
              <div style={{ padding: '0 14px 8px' }}>
                <textarea
                  value={notes}
                  onChange={e => setNotes(e.target.value)}
                  placeholder="Add manual notes..."
                  rows={2}
                  style={{
                    width: '100%', padding: '6px 8px', borderRadius: 4, fontSize: 16,
                    background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.08)',
                    color: '#E4E8F0', fontFamily: 'inherit', resize: 'none',
                    boxSizing: 'border-box',
                  }}
                />
              </div>

              {/* ── Disposition card picker ── */}
              <div style={{
                padding: '10px 14px',
                borderTop: '1px solid rgba(255,255,255,0.06)',
                background: 'var(--sg-bg-panel)',
              }}>
                {/* Header row */}
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                  <div style={{ fontSize: 16, fontWeight: 700, letterSpacing: 1.5, color: '#6B7590', textTransform: 'uppercase' }}>
                    DISPOSITION
                  </div>
                  {/* Keyboard hint + mode indicator */}
                  <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                    {keyPrefix && (
                      <span style={{
                        fontSize: 16, padding: '2px 6px', borderRadius: 3,
                        background: keyPrefix === 'f' ? 'rgba(34,197,94,0.15)' : 'rgba(239,68,68,0.15)',
                        border: `1px solid ${keyPrefix === 'f' ? 'rgba(34,197,94,0.3)' : 'rgba(239,68,68,0.3)'}`,
                        color: keyPrefix === 'f' ? '#22C55E' : '#EF4444',
                        fontFamily: "'JetBrains Mono', monospace", fontWeight: 700,
                        animation: 'sla-breach-pulse 0.5s infinite alternate',
                      }}>
                        {keyPrefix.toUpperCase()} + 1–5
                      </span>
                    )}
                    <span style={{ fontSize: 16, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
                      F+1–5 / V+1–5
                    </span>
                  </div>
                </div>

                {/* Column headers */}
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 4, marginBottom: 4 }}>
                  <div style={{
                    fontSize: 16, fontWeight: 700, color: '#8891A5', letterSpacing: 1,
                    paddingBottom: 4, borderBottom: '1px solid rgba(255,255,255,0.08)',
                    textAlign: 'center',
                  }}>
                    FALSE POSITIVE
                  </div>
                  <div style={{
                    fontSize: 16, fontWeight: 700, color: '#8891A5', letterSpacing: 1,
                    paddingBottom: 4, borderBottom: '1px solid rgba(255,255,255,0.08)',
                    textAlign: 'center',
                  }}>
                    VERIFIED THREAT
                  </div>
                </div>

                {/* 2-column card grid (interleaved: false[0], verified[0], false[1], ...) */}
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 3 }}>
                  {falseCodes.map((fp, idx) => {
                    const vt = verifiedCodes[idx];
                    return [
                      /* False positive card */
                      <button
                        key={fp.code}
                        onClick={() => setDisposition(disposition === fp.code ? '' : fp.code)}
                        style={{
                          display: 'flex', alignItems: 'center', gap: 5,
                          padding: '5px 7px', borderRadius: 4, cursor: 'pointer',
                          fontFamily: 'inherit', textAlign: 'left',
                          background: 'transparent',
                          border: `1px solid ${disposition === fp.code ? 'rgba(255,255,255,0.25)' : 'rgba(255,255,255,0.08)'}`,
                          color: disposition === fp.code ? '#E4E8F0' : '#6B7590',
                          transition: 'all 0.1s',
                          outline: 'none',
                        }}
                      >
                        <span style={{ fontSize: 15, fontWeight: disposition === fp.code ? 700 : 500, flex: 1, lineHeight: 1.2 }}>
                          {DISPOSITION_SHORT[fp.code]}
                        </span>
                        <span style={{
                          fontSize: 15, fontFamily: "'JetBrains Mono', monospace",
                          color: '#4A5268',
                          flexShrink: 0,
                        }}>
                          F{idx + 1}
                        </span>
                      </button>,

                      /* Verified threat card */
                      vt ? (
                        <button
                          key={vt.code}
                          onClick={() => setDisposition(disposition === vt.code ? '' : vt.code)}
                          style={{
                            display: 'flex', alignItems: 'center', gap: 5,
                            padding: '5px 7px', borderRadius: 4, cursor: 'pointer',
                            fontFamily: 'inherit', textAlign: 'left',
                            background: 'transparent',
                            border: `1px solid ${disposition === vt.code ? 'rgba(239,68,68,0.3)' : 'rgba(255,255,255,0.08)'}`,
                            color: disposition === vt.code ? '#EF4444' : '#6B7590',
                            transition: 'all 0.1s',
                            outline: 'none',
                          }}
                        >
                          <span style={{ fontSize: 15, fontWeight: disposition === vt.code ? 700 : 500, flex: 1, lineHeight: 1.2 }}>
                            {DISPOSITION_SHORT[vt.code]}
                          </span>
                          <span style={{
                            fontSize: 15, fontFamily: "'JetBrains Mono', monospace",
                            color: '#4A5268',
                            flexShrink: 0,
                          }}>
                            V{idx + 1}
                          </span>
                        </button>
                      ) : <div key={`empty-${idx}`} />,
                    ];
                  })}
                </div>

                {/* TMA-AVS-01 validation factors */}
                <AVSFactorChecklist
                  factors={avsFactors}
                  onToggle={toggleAVS}
                  preview={avsPreview}
                />

                {/* Submit button */}
                <button
                  onClick={handleSubmit}
                  disabled={!disposition || submitting}
                  style={{
                    marginTop: 10, width: '100%',
                    padding: '10px 20px', borderRadius: 5, fontSize: 16, fontWeight: 700,
                    fontFamily: 'inherit', cursor: disposition ? 'pointer' : 'not-allowed',
                    opacity: submitting ? 0.6 : 1,
                    transition: 'all 0.15s',
                    background: !disposition
                      ? 'rgba(255,255,255,0.02)'
                      : isFalse
                        ? 'rgba(255,255,255,0.06)'
                        : 'rgba(239,68,68,0.08)',
                    border: `1px solid ${!disposition
                      ? 'rgba(255,255,255,0.06)'
                      : isFalse
                        ? 'rgba(255,255,255,0.15)'
                        : 'rgba(239,68,68,0.25)'}`,
                    color: !disposition ? '#4A5268' : isFalse ? '#E4E8F0' : '#EF4444',
                    letterSpacing: 0.5,
                  }}
                >
                  {submitting
                    ? 'Closing...'
                    : !disposition
                      ? 'Select a disposition to close alarm'
                      : `Close — ${DISPOSITION_SHORT[disposition as DispositionCode]}`}
                </button>
              </div>
            </>
          )}

        </div>
      </div>
    </div>

    {/* Site map modal */}
    {showSiteMap && (
      <SiteMapModal
        siteDetail={{
          id: alarm.site_id,
          name: siteData?.name ?? alarm.site_id,
          address: (siteData as any)?.address ?? '',
          status: (siteData as any)?.status ?? 'active',
          compliance_score: (siteData as any)?.compliance_score ?? 0,
          cameras_online: (siteData as any)?.cameras_online ?? 0,
          cameras_total: (siteData as any)?.cameras_total ?? 0,
          open_incidents: (siteData as any)?.open_incidents ?? 0,
          workers_on_site: (siteData as any)?.workers_on_site ?? 0,
          last_activity: (siteData as any)?.last_activity ?? '',
          trend: (siteData as any)?.trend ?? 'flat',
          latitude: (siteData as any)?.latitude ?? 0,
          longitude: (siteData as any)?.longitude ?? 0,
          cameras: siteCameras,
          compliance_history: [],
          ppe_breakdown: { hard_hat: 0, harness: 0, hi_vis: 0, boots: 0, gloves: 0 },
          risk_notes: siteNotes,
        }}
        alarm={alarm}
        onClose={() => setShowSiteMap(false)}
      />
    )}
    </>
  );
}

// ── AI Feedback Buttons ──
function AIFeedbackButtons({ alarmId }: { alarmId: string }) {
  const [feedback, setFeedback] = useState<'agreed' | 'disagreed' | null>(null);
  const addLogEntry = useOperatorStore(s => s.addActionLogEntry);

  const handleFeedback = async (agreed: boolean) => {
    setFeedback(agreed ? 'agreed' : 'disagreed');
    addLogEntry(`AI assessment: ${agreed ? 'agreed' : 'disagreed'}`, true);
    const { submitAIFeedback } = await import('@/lib/ironsight-api');
    submitAIFeedback(alarmId, agreed);
  };

  return (
    <div style={{ marginTop: 8, fontSize: 12, color: '#6B7590' }}>
      Was this accurate?{' '}
      <button
        onClick={() => handleFeedback(true)}
        disabled={feedback !== null}
        style={{
          color: feedback === 'agreed' ? '#8891A5' : '#6B7590',
          background: 'none', border: 'none', cursor: feedback !== null ? 'default' : 'pointer',
          textDecoration: 'underline', fontFamily: 'inherit', fontSize: 12,
          opacity: feedback === 'disagreed' ? 0.4 : 1,
        }}
      >
        Yes
      </button>
      {' \u00b7 '}
      <button
        onClick={() => handleFeedback(false)}
        disabled={feedback !== null}
        style={{
          color: feedback === 'disagreed' ? '#8891A5' : '#6B7590',
          background: 'none', border: 'none', cursor: feedback !== null ? 'default' : 'pointer',
          textDecoration: 'underline', fontFamily: 'inherit', fontSize: 12,
          opacity: feedback === 'agreed' ? 0.4 : 1,
        }}
      >
        No
      </button>
      {feedback && <span style={{ color: '#4A5268', marginLeft: 8 }}>Recorded</span>}
    </div>
  );
}

// ── Alarm Video Feed ──
// mode is controlled by the parent; no internal toggle buttons here.
function AlarmVideoFeed({ cameraId, snapshotUrl: eventSnapshotUrl, clipUrl, alarmType, mode, boundingBoxes, yoloDetections }: {
  cameraId: string;
  snapshotUrl?: string;
  clipUrl?: string;
  alarmType?: string;
  mode: 'snapshot' | 'clip' | 'live';
  boundingBoxes?: { x: number; y: number; w: number; h: number; label?: string }[];
  yoloDetections?: { class: string; confidence: number; bbox_normalized: { x1: number; y1: number; x2: number; y2: number } }[];
}) {
  const [liveFrameUrl, setLiveFrameUrl] = useState<string | null>(null);
  const [liveError, setLiveError] = useState(false);
  const [eventFrameUrl, setEventFrameUrl] = useState<string | null>(null);
  const [snapshotState, setSnapshotState] = useState<'loading' | 'ok' | 'unavailable' | 'error'>('unavailable');
  const [vcaRules, setVcaRules] = useState<any[]>([]);

  // Load VCA zones for overlay
  useEffect(() => {
    const token = typeof window !== 'undefined'
      ? (localStorage.getItem('ironsight_token') || localStorage.getItem('onvif_token'))
      : '';
    fetch(`/api/cameras/${cameraId}/vca/rules`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
      .then(r => r.ok ? r.json() : [])
      .then(data => setVcaRules(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, [cameraId]);

  // Fetch the event snapshot once (for snapshot mode)
  useEffect(() => {
    if (!eventSnapshotUrl) {
      setSnapshotState('unavailable');
      return;
    }
    setSnapshotState('loading');
    const token = typeof window !== 'undefined'
      ? (localStorage.getItem('ironsight_token') || localStorage.getItem('onvif_token'))
      : '';
    fetch(eventSnapshotUrl, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
      .then(r => r.ok ? r.blob() : Promise.reject(r.status))
      .then(blob => {
        if (blob && blob.size > 0) {
          setEventFrameUrl(URL.createObjectURL(blob));
          setSnapshotState('ok');
        } else {
          setSnapshotState('unavailable');
        }
      })
      .catch(() => setSnapshotState('error'));
  }, [eventSnapshotUrl]);

  // Refreshing snapshot for live mode (unused currently — LIVE opens NVR in new tab,
  // but kept so mode='live' shows something if called directly)
  useEffect(() => {
    if (mode !== 'live') return;
    let cancelled = false;
    const fetchFrame = async () => {
      try {
        const token = typeof window !== 'undefined'
          ? (localStorage.getItem('ironsight_token') || localStorage.getItem('onvif_token'))
          : '';
        const res = await fetch(`/api/cameras/${cameraId}/vca/snapshot`, {
          headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!res.ok || cancelled) return;
        const blob = await res.blob();
        if (cancelled || blob.size === 0) return;
        const url = URL.createObjectURL(blob);
        setLiveFrameUrl(prev => { if (prev) URL.revokeObjectURL(prev); return url; });
        setLiveError(false);
      } catch {
        if (!cancelled) setLiveError(true);
      }
    };
    fetchFrame();
    const timer = setInterval(fetchFrame, 2000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [cameraId, mode]);

  // Zone overlay styles per rule type
  const zoneColors: Record<string, string> = {
    intrusion: 'rgba(239,68,68,0.15)',
    linecross: 'rgba(59,130,246,0.15)',
    regionentrance: 'rgba(34,197,94,0.15)',
    loitering: 'rgba(234,179,8,0.15)',
  };
  const zoneStrokes: Record<string, string> = {
    intrusion: '#EF4444',
    linecross: '#3B82F6',
    regionentrance: '#22C55E',
    loitering: '#EAB308',
  };

  // Highlight the rule that matches the current alarm type
  const matchingType = alarmType?.toLowerCase();

  return (
    <div style={{ position: 'absolute', inset: 0, background: '#0a0c10' }}>

      {/* ── SNAPSHOT MODE: static event frame with zone overlay ── */}
      {mode === 'snapshot' && snapshotState === 'ok' && eventFrameUrl && (
        <img
          src={eventFrameUrl}
          alt="Event snapshot"
          style={{ width: '100%', height: '100%', objectFit: 'fill', display: 'block' }}
        />
      )}
      {mode === 'snapshot' && snapshotState === 'loading' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 8,
        }}>
          <div style={{ fontSize: 28, opacity: 0.3 }}>◉</div>
          <div style={{ fontSize: 16, color: '#EAB308' }}>Loading event snapshot...</div>
        </div>
      )}
      {mode === 'snapshot' && (snapshotState === 'unavailable' || snapshotState === 'error') && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 10,
        }}>
          <div style={{ fontSize: 36, opacity: 0.2 }}>📷</div>
          <div style={{ fontSize: 15, color: '#4A5268', fontWeight: 600 }}>No snapshot available</div>
          <div style={{ fontSize: 16, color: '#4A5268', textAlign: 'center', maxWidth: 220, lineHeight: 1.5 }}>
            {snapshotState === 'error'
              ? 'Snapshot could not be loaded.'
              : 'No event frame was captured for this alarm.'}
            {' '}Use the Clip or Live NVR buttons above to review footage.
          </div>
        </div>
      )}

      {/* ── CLIP MODE: play recording segment ── */}
      {mode === 'clip' && clipUrl && (
        <video
          key={clipUrl}
          src={clipUrl}
          controls
          autoPlay
          playsInline
          style={{ width: '100%', height: '100%', objectFit: 'fill', display: 'block' }}
        />
      )}
      {mode === 'clip' && !clipUrl && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 8,
        }}>
          <div style={{ fontSize: 28, opacity: 0.3 }}>⏳</div>
          <div style={{ fontSize: 16, color: '#E89B2A' }}>No clip available for this event</div>
          <div style={{ fontSize: 16, color: '#4A5268' }}>Use LIVE NVR to view the camera</div>
        </div>
      )}

      {/* ── LIVE MODE: fallback refreshing snapshot (NVR opens in new tab) ── */}
      {mode === 'live' && liveFrameUrl && (
        <img
          src={liveFrameUrl}
          alt="Live feed"
          style={{ width: '100%', height: '100%', objectFit: 'fill', display: 'block' }}
        />
      )}
      {mode === 'live' && !liveFrameUrl && !liveError && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex',
          alignItems: 'center', justifyContent: 'center', color: '#4A5268', fontSize: 16,
        }}>
          Connecting to camera...
        </div>
      )}
      {mode === 'live' && liveError && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex',
          alignItems: 'center', justifyContent: 'center', color: '#E89B2A', fontSize: 16,
        }}>
          Camera unavailable
        </div>
      )}

      {/* ── VCA Zone Overlay ── */}
      {vcaRules.length > 0 && (
        <svg style={{
          position: 'absolute', inset: 0, width: '100%', height: '100%',
          pointerEvents: 'none', zIndex: 10,
        }}>
          {vcaRules
            .filter((r: any) => r.enabled && Array.isArray(r.region) && r.region.length >= 2)
            .map((rule: any) => {
              const isActive = rule.rule_type === matchingType;
              const fill = isActive
                ? (zoneColors[rule.rule_type] || 'rgba(255,255,255,0.1)').replace('0.15', '0.35')
                : (zoneColors[rule.rule_type] || 'rgba(255,255,255,0.05)');
              const stroke = zoneStrokes[rule.rule_type] || '#8891A5';
              const strokeWidth = isActive ? '2' : '1.5';
              const opacity = isActive ? '1' : '0.5';

              if (rule.rule_type === 'linecross' && rule.region.length === 2) {
                return (
                  <g key={rule.id} opacity={opacity}>
                    <line
                      x1={`${rule.region[0].x * 100}%`} y1={`${rule.region[0].y * 100}%`}
                      x2={`${rule.region[1].x * 100}%`} y2={`${rule.region[1].y * 100}%`}
                      stroke={stroke} strokeWidth={strokeWidth} strokeDasharray="6 3"
                    />
                    {isActive && (
                      <text
                        x={`${((rule.region[0].x + rule.region[1].x) / 2) * 100}%`}
                        y={`${((rule.region[0].y + rule.region[1].y) / 2) * 100}%`}
                        fill={stroke} fontSize="9" fontWeight="700" textAnchor="middle"
                        dy="-6" style={{ fontFamily: "'JetBrains Mono', monospace" }}
                      >
                        {rule.name}
                      </text>
                    )}
                  </g>
                );
              }

              const pts = (rule.region as any[]).map((p: any) =>
                `${(p.x * 100).toFixed(1)}%,${(p.y * 100).toFixed(1)}%`
              ).join(' ');
              const cx = (rule.region.reduce((s: number, p: any) => s + p.x, 0) / rule.region.length) * 100;
              const cy = (rule.region.reduce((s: number, p: any) => s + p.y, 0) / rule.region.length) * 100;

              return (
                <g key={rule.id} opacity={opacity}>
                  <polygon
                    points={pts}
                    fill={fill}
                    stroke={stroke}
                    strokeWidth={strokeWidth}
                    strokeDasharray={isActive ? 'none' : '5 3'}
                  />
                  <text
                    x={`${cx}%`} y={`${cy}%`}
                    fill={stroke} fontSize={isActive ? '11' : '9'}
                    fontWeight={isActive ? '700' : '500'}
                    textAnchor="middle" dominantBaseline="middle"
                    style={{ fontFamily: "'JetBrains Mono', monospace" }}
                  >
                    {rule.name}
                  </text>
                  {isActive && (
                    <text
                      x={`${cx}%`} y={`${cy}%`}
                      fill={stroke} fontSize="8" fontWeight="500"
                      textAnchor="middle" dominantBaseline="middle"
                      dy="14" style={{ fontFamily: "'JetBrains Mono', monospace", opacity: 0.7 }}
                    >
                      ⚠ TRIGGERED
                    </text>
                  )}
                </g>
              );
            })}
        </svg>
      )}

      {/* Bounding boxes removed from child — rendered by parent with toggle */}
    </div>
  );
}
