'use client';

import { useState, useEffect } from 'react';
import type { SiteSnooze } from '@/types/ironsight';
import { getComplianceLevel, formatRelativeTime } from '@/lib/format';
import { SeverityPillLight } from '@/components/shared/SeverityPill';
import { useSite, useUpdateSite } from '@/hooks/useSites';
import { useIncidents } from '@/hooks/useIncidents';
import { useAuth } from '@/contexts/AuthContext';
import { listSecurityEvents } from '@/lib/ironsight-api';
import type { SecurityEventRecord } from '@/lib/ironsight-api';
import { mintMediaToken } from '@/lib/media';
import Link from 'next/link';

/* IRONSight Steel & Fire tokens */
const T = {
  bg: '#0A0C10', bgWhite: '#0E1117', bgWarm: '#12161E', bgInk: '#0A0C10',
  border: 'rgba(255,255,255,0.07)', borderStrong: 'rgba(255,255,255,0.12)',
  textPrimary: '#E4E8F0', textSecondary: '#8891A5', textDim: '#4A5268', textWhite: '#E4E8F0',
  red: '#EF4444', amber: '#E89B2A', green: '#22C55E', blue: '#3B82F6', purple: '#A855F7',
};

const SNOOZE_DURATIONS = [
  { label: '30 min', ms: 30 * 60_000 },
  { label: '1 hour', ms: 60 * 60_000 },
  { label: '4 hours', ms: 4 * 60 * 60_000 },
  { label: '8 hours', ms: 8 * 60 * 60_000 },
  { label: '24 hours', ms: 24 * 60 * 60_000 },
];

export default function SiteDrilldownPage({ params }: { params: { id: string } }) {
  const { data: site } = useSite(params.id);
  const { data: incidents = [] } = useIncidents({ site_id: params.id, limit: 8 });
  const updateSite = useUpdateSite();
  const { user } = useAuth();
  const [showSnoozeMenu, setShowSnoozeMenu] = useState(false);
  const [snoozeReason, setSnoozeReason] = useState('');
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null);
  const [fullEvents, setFullEvents] = useState<SecurityEventRecord[]>([]);
  const [snapshotUrl, setSnapshotUrl] = useState<string | null>(null);

  /* ── Fetch full security events on mount ── */
  useEffect(() => {
    listSecurityEvents(params.id).then(evts => setFullEvents(evts));
  }, [params.id]);

  /* ── Fetch snapshot when event selected ── */
  const selectedFullEvent = selectedEventId
    ? fullEvents.find(e => e.id === selectedEventId) ?? null
    : null;

  useEffect(() => {
    if (!selectedFullEvent || !selectedFullEvent.alarm_id) {
      setSnapshotUrl(null);
      return;
    }

    let cancelled = false;
    // P1-A-03: mint a signed /media/v1/<token> URL for the snapshot
    // file. The naming convention <alarm_id>.jpg under the camera's
    // snapshot dir is preserved on disk; only the URL shape changed.
    mintMediaToken({
      camera_id: selectedFullEvent.camera_id,
      kind: 'snapshot',
      path: `${selectedFullEvent.alarm_id}.jpg`,
    })
      .then(({ url }) => fetch(url))
      .then(r => r.ok ? r.blob() : Promise.reject(r.status))
      .then(blob => {
        if (!cancelled && blob && blob.size > 0) {
          setSnapshotUrl(URL.createObjectURL(blob));
        }
      })
      .catch(() => {
        if (!cancelled) setSnapshotUrl(null);
      });
    return () => {
      cancelled = true;
      setSnapshotUrl(prev => { if (prev) URL.revokeObjectURL(prev); return null; });
    };
  }, [selectedFullEvent?.id, selectedFullEvent?.alarm_id, selectedFullEvent?.camera_id]);

  const isSnoozed = site?.snooze?.active && new Date(site.snooze.expires_at) > new Date();

  const handleSnooze = async (durationMs: number) => {
    if (!site) return;
    const now = new Date();
    const snooze: SiteSnooze = {
      active: true,
      reason: snoozeReason || 'Customer-initiated snooze',
      snoozed_by: user?.display_name || user?.email || 'Customer',
      snoozed_at: now.toISOString(),
      expires_at: new Date(now.getTime() + durationMs).toISOString(),
    };
    try {
      await updateSite.mutateAsync({ id: params.id, data: { snooze } as any });
      setShowSnoozeMenu(false);
      setSnoozeReason('');
    } catch { /* displayed via UI */ }
  };

  const handleRearm = async () => {
    try {
      await updateSite.mutateAsync({ id: params.id, data: { snooze: null } as any });
      setShowSnoozeMenu(false);
    } catch { /* displayed via UI */ }
  };

  if (!site) return (
    <div style={{ background: T.bg, height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', fontFamily: "'Inter', sans-serif", color: T.textDim }}>
      Loading…
    </div>
  );

  const cameras = site.cameras ?? [];
  const isSecurityOnly = site.feature_mode === 'security_only';
  const openIncidents = site.open_incidents ?? 0;
  const complianceScore = site.compliance_score ?? 0;
  const workersOnSite = site.workers_on_site ?? 0;

  /* ── KPI cards ── */
  const onlineCams = cameras.filter(c => c.status === 'online').length;
  const camOnlinePct = cameras.length > 0 ? Math.round(onlineCams / cameras.length * 100) : 100;

  const kpis = isSecurityOnly
    ? [
        { label: 'Cameras', value: `${onlineCams}/${cameras.length}`, color: T.blue },
        { label: 'Incidents', value: String(openIncidents), color: openIncidents > 0 ? T.red : T.green },
        { label: 'Alerts', value: String(cameras.filter(c => c.has_alert).length), color: T.amber },
        { label: 'Online', value: `${camOnlinePct}%`, color: T.green },
      ]
    : [
        { label: 'PPE Score',  value: `${Math.round(complianceScore)}%`, color: complianceScore >= 90 ? '#40c080' : complianceScore >= 75 ? '#d4a030' : '#e05040' },
        { label: 'Incidents',  value: String(openIncidents), color: openIncidents > 0 ? '#e07060' : '#40c080' },
        { label: 'Violations', value: '7', color: '#d4a030' },
        { label: 'Workers',    value: String(workersOnSite), color: T.textWhite },
      ];

  const ppe = site.ppe_breakdown;
  const ppeItems = ppe ? [
    { key: 'hard_hat', label: 'Hard Hat', value: ppe.hard_hat },
    { key: 'harness',  label: 'Harness',  value: ppe.harness },
    { key: 'hi_vis',   label: 'Hi-Vis',   value: ppe.hi_vis },
    { key: 'boots',    label: 'Boots',    value: ppe.boots },
    { key: 'gloves',   label: 'Gloves',   value: ppe.gloves },
  ] : [];

  /* ── Find the selected incident row for matching ── */
  const selectedIncident = selectedEventId
    ? incidents.find(inc => inc.id === selectedEventId) ?? null
    : null;

  return (
    <div style={{ background: T.bg, color: T.textPrimary, fontFamily: "'Inter', sans-serif", fontSize: 13, height: '100vh', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>

      {/* ── TOPBAR ── */}
      <div style={{ background: T.bgInk, padding: '0 24px', height: 44, display: 'flex', alignItems: 'center', gap: 16, flexShrink: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12 }}>
          <Link href="/portal" style={{ color: 'rgba(247,244,239,0.4)', textDecoration: 'none' }}>Portal</Link>
          <span style={{ color: 'rgba(247,244,239,0.2)', fontSize: 10 }}>›</span>
          <Link href="/portal" style={{ color: 'rgba(247,244,239,0.4)', textDecoration: 'none' }}>Sites</Link>
          <span style={{ color: 'rgba(247,244,239,0.2)', fontSize: 10 }}>›</span>
          <span style={{ color: T.textWhite, fontWeight: 500 }}>{site.name}</span>
        </div>
        <div style={{ marginLeft: 'auto', display: 'flex', gap: 8, alignItems: 'center' }}>
          {/* Self-service contacts editor — site_managers reach this from
              the breadcrumb-line action so they don't have to dig
              through site settings to update who answers when an
              alarm fires. */}
          <Link
            href={`/portal/sites/${encodeURIComponent(params.id)}/contacts`}
            style={{
              padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 600,
              border: '1px solid rgba(247,244,239,0.15)',
              background: 'rgba(247,244,239,0.04)',
              color: T.textWhite,
              textDecoration: 'none', fontFamily: 'inherit',
              display: 'flex', alignItems: 'center', gap: 5,
            }}
          >
            👥 Contacts
          </Link>
          {/* Snooze / Disarm */}
          <div style={{ position: 'relative' }}>
            {isSnoozed ? (
              <button
                onClick={handleRearm}
                style={{
                  padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 600, cursor: 'pointer',
                  border: '1px solid rgba(232,155,42,0.5)', background: 'rgba(232,155,42,0.12)',
                  color: '#E89B2A', fontFamily: 'inherit', display: 'flex', alignItems: 'center', gap: 5,
                }}
              >
                ⏸ Snoozed — Re-arm
              </button>
            ) : (
              <button
                onClick={() => setShowSnoozeMenu(v => !v)}
                style={{
                  padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 500, cursor: 'pointer',
                  border: '1px solid rgba(255,255,255,0.12)', background: 'rgba(255,255,255,0.04)',
                  color: T.textSecondary, fontFamily: 'inherit', display: 'flex', alignItems: 'center', gap: 5,
                }}
              >
                ⏸ Snooze Site
              </button>
            )}

            {/* Snooze dropdown */}
            {showSnoozeMenu && !isSnoozed && (
              <div style={{
                position: 'absolute', top: 'calc(100% + 6px)', right: 0, zIndex: 200,
                background: '#0E1117', border: '1px solid rgba(255,255,255,0.12)',
                borderRadius: 8, padding: 10, minWidth: 220,
                boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
              }}>
                <div style={{ fontSize: 9, color: T.textDim, letterSpacing: 1, fontWeight: 600, textTransform: 'uppercase' as const, marginBottom: 8 }}>
                  Snooze monitoring
                </div>
                <input
                  value={snoozeReason}
                  onChange={e => setSnoozeReason(e.target.value)}
                  placeholder="Reason (optional)"
                  style={{
                    width: '100%', padding: '5px 8px', borderRadius: 4, fontSize: 11, marginBottom: 8,
                    background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
                    color: T.textPrimary, fontFamily: 'inherit', boxSizing: 'border-box',
                  }}
                />
                <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                  {SNOOZE_DURATIONS.map(d => (
                    <button
                      key={d.label}
                      onClick={() => handleSnooze(d.ms)}
                      style={{
                        padding: '6px 10px', borderRadius: 4, fontSize: 11, fontWeight: 500,
                        cursor: 'pointer', fontFamily: 'inherit', textAlign: 'left',
                        background: 'transparent', border: '1px solid rgba(255,255,255,0.06)',
                        color: T.textSecondary,
                      }}
                    >
                      ⏸ {d.label}
                    </button>
                  ))}
                </div>
                <button
                  onClick={() => setShowSnoozeMenu(false)}
                  style={{
                    width: '100%', marginTop: 6, padding: '5px 0', borderRadius: 4, fontSize: 10,
                    background: 'none', border: '1px solid rgba(255,255,255,0.06)',
                    color: T.textDim, cursor: 'pointer', fontFamily: 'inherit',
                  }}
                >
                  Cancel
                </button>
              </div>
            )}
          </div>

          <Link href={`/portal/incidents/${incidents[0]?.id || 'INC-2026-0847'}`} style={{ textDecoration: 'none' }}>
            <button style={{ padding: '5px 12px', borderRadius: 4, fontSize: 11, fontWeight: 500, cursor: 'pointer', border: '1px solid rgba(184,50,32,0.4)', background: 'rgba(184,50,32,0.1)', color: '#e07060', fontFamily: 'inherit', display: 'flex', alignItems: 'center', gap: 5 }}>
              ⚠ {isSecurityOnly ? 'Security Events' : 'View Incidents'}
            </button>
          </Link>
        </div>
      </div>

      {/* ── HERO HEADER ── */}
      <div style={{ background: T.bgInk, borderBottom: '1px solid rgba(255,255,255,0.06)', padding: '20px 24px 18px', flexShrink: 0 }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 24, alignItems: 'start' }}>
          <div>
            <div style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 10, letterSpacing: 2, textTransform: 'uppercase' as const, color: 'rgba(247,244,239,0.35)', marginBottom: 6 }}>
              SITE {site.id}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
              <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 26, fontWeight: 700, color: T.textWhite, letterSpacing: -0.5, lineHeight: 1.1 }}>
                {site.name}
              </div>
              {/* Tier badge */}
              <span style={{
                padding: '3px 8px', borderRadius: 4, fontSize: 9, fontWeight: 700, letterSpacing: 1,
                textTransform: 'uppercase' as const, marginTop: 2,
                background: isSecurityOnly ? 'rgba(59,130,246,0.12)' : 'rgba(168,85,247,0.12)',
                border: `1px solid ${isSecurityOnly ? 'rgba(59,130,246,0.35)' : 'rgba(168,85,247,0.35)'}`,
                color: isSecurityOnly ? T.blue : T.purple,
              }}>
                {isSecurityOnly ? '🔒 Security' : '🛡 Security + Safety'}
              </span>
            </div>
            <div style={{ display: 'flex', gap: 20, alignItems: 'center', marginTop: 8 }}>
              <span style={{ fontSize: 11, color: 'rgba(247,244,239,0.45)' }}>📍 <strong style={{ color: 'rgba(247,244,239,0.75)' }}>{site.address || 'No address'}</strong></span>
              <span style={{ fontSize: 11, color: 'rgba(247,244,239,0.45)' }}>📷 <strong style={{ color: 'rgba(247,244,239,0.75)' }}>{cameras.length} cameras</strong></span>
            </div>
          </div>
          <div style={{ display: 'flex', gap: 2 }}>
            {kpis.map((kpi, i) => (
              <div key={i} style={{ padding: '10px 18px', textAlign: 'center' as const, borderLeft: i > 0 ? '1px solid rgba(255,255,255,0.08)' : undefined, minWidth: 80 }}>
                <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 22, fontWeight: 700, lineHeight: 1, marginBottom: 4, color: kpi.color }}>{kpi.value}</div>
                <div style={{ fontSize: 9, letterSpacing: 1, textTransform: 'uppercase' as const, color: 'rgba(247,244,239,0.35)', fontFamily: "'JetBrains Mono', monospace" }}>{kpi.label}</div>
              </div>
            ))}
          </div>
        </div>
        {isSnoozed && site.snooze && (
          <div style={{ marginTop: 12, padding: '8px 14px', background: 'rgba(232,155,42,0.12)', border: '1px solid rgba(232,155,42,0.25)', borderRadius: 4, display: 'flex', alignItems: 'center', gap: 10, fontSize: 11, color: '#E89B2A', fontWeight: 500 }}>
            ⏸ Monitoring snoozed{site.snooze.reason ? ` — ${site.snooze.reason}` : ''} · Expires {new Date(site.snooze.expires_at).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}
            <button onClick={handleRearm} style={{ marginLeft: 'auto', fontSize: 10, fontWeight: 700, color: '#E89B2A', background: 'rgba(232,155,42,0.15)', border: '1px solid rgba(232,155,42,0.3)', borderRadius: 4, padding: '2px 8px', cursor: 'pointer', fontFamily: 'inherit' }}>
              Re-arm Now
            </button>
          </div>
        )}
        {(site.risk_notes?.length ?? 0) > 0 && (
          <div style={{ marginTop: 12, padding: '8px 14px', background: 'rgba(184,50,32,0.12)', border: '1px solid rgba(184,50,32,0.25)', borderRadius: 4, display: 'flex', alignItems: 'center', gap: 10, fontSize: 11, color: '#e07060', fontWeight: 500 }}>
            ⚠ {site.risk_notes![0]}
          </div>
        )}
      </div>

      {/* ── BODY ── */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'grid', gridTemplateColumns: '1fr 380px' }}>

        {/* ── LEFT PANE: Security Events Table ── */}
        <div style={{ overflowY: 'auto', scrollbarWidth: 'thin' as const, display: 'flex', flexDirection: 'column' }}>
          <div style={{ background: T.bgWhite, border: `1px solid ${T.border}`, borderRadius: 0, overflow: 'hidden', flex: 1, display: 'flex', flexDirection: 'column' }}>
            <div style={{ padding: '13px 18px', borderBottom: `1px solid ${T.border}`, background: T.bgWarm, display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
              <span style={{ fontFamily: "'Inter', sans-serif", fontSize: 13, fontWeight: 700 }}>{isSecurityOnly ? 'Security Events' : 'Incidents'}</span>
              <span style={{ fontSize: 10, color: T.textDim }}>{incidents.length} total</span>
            </div>
            <div style={{ flex: 1, overflowY: 'auto', scrollbarWidth: 'thin' as const }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    {['Time', 'Severity', 'Type', 'Disposition', 'Camera', 'Operator'].map(h => (
                      <th key={h} style={{ padding: '9px 16px', textAlign: 'left' as const, fontSize: 9, fontWeight: 600, letterSpacing: 1.2, textTransform: 'uppercase' as const, color: T.textDim, borderBottom: `1px solid ${T.border}`, background: T.bgWarm, fontFamily: "'JetBrains Mono', monospace", position: 'sticky' as const, top: 0, zIndex: 2 }}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {incidents.map(inc => {
                    const isFalse = inc.disposition_code?.startsWith('false');
                    const isVerified = inc.disposition_code?.startsWith('verified');
                    const dispColor = isFalse ? T.green : isVerified ? T.red : T.textDim;
                    const isSelected = selectedEventId === inc.id;
                    return (
                      <tr
                        key={inc.id}
                        onClick={() => setSelectedEventId(isSelected ? null : inc.id)}
                        style={{
                          borderBottom: `1px solid ${T.border}`,
                          cursor: 'pointer',
                          background: isSelected ? 'rgba(59,130,246,0.10)' : undefined,
                          borderLeft: isSelected ? `3px solid ${T.blue}` : '3px solid transparent',
                          transition: 'background 0.15s, border-color 0.15s',
                        }}
                      >
                        <td style={{ padding: '10px 16px', fontFamily: "'JetBrains Mono', monospace", fontSize: 11, color: isSelected ? T.textPrimary : T.textDim }}>{formatRelativeTime(inc.ts)}</td>
                        <td style={{ padding: '10px 16px' }}>
                          <SeverityPillLight severity={inc.severity} size="sm" />
                        </td>
                        <td style={{ padding: '10px 16px', fontSize: 12, fontWeight: 600, color: T.textPrimary }}>{inc.type.replace(/_/g, ' ')}</td>
                        <td style={{ padding: '10px 16px' }}>
                          {inc.disposition_code ? (
                            <span style={{
                              display: 'inline-block', padding: '2px 7px', borderRadius: 10,
                              fontSize: 10, fontWeight: 600,
                              background: isFalse ? 'rgba(34,197,94,0.08)' : 'rgba(239,68,68,0.08)',
                              color: dispColor,
                              border: `1px solid ${isFalse ? 'rgba(34,197,94,0.2)' : 'rgba(239,68,68,0.2)'}`,
                            }}>
                              {inc.disposition_label || inc.disposition_code.replace(/_/g, ' ')}
                            </span>
                          ) : (
                            <span style={{ fontSize: 10, color: T.textDim }}>—</span>
                          )}
                        </td>
                        <td style={{ padding: '10px 16px', fontSize: 12, color: T.textSecondary }}>{inc.camera_name || inc.camera_id?.slice(0, 8)}</td>
                        <td style={{ padding: '10px 16px', fontSize: 11, color: T.textDim, fontFamily: "'JetBrains Mono', monospace" }}>{inc.operator_callsign || '—'}</td>
                      </tr>
                    );
                  })}
                  {incidents.length === 0 && (
                    <tr>
                      <td colSpan={6} style={{ padding: 32, textAlign: 'center', color: T.textDim, fontSize: 12, fontStyle: 'italic' }}>
                        No security events recorded
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        {/* ── RIGHT PANE: Event Detail or Camera Health ── */}
        <div style={{ borderLeft: `1px solid ${T.border}`, background: T.bgWhite, overflowY: 'auto', scrollbarWidth: 'thin' as const }}>

          {selectedEventId && selectedIncident ? (
            /* ── EVENT DETAIL PANEL ── */
            <div style={{ display: 'flex', flexDirection: 'column' }}>
              {/* Close button */}
              <div style={{ padding: '10px 16px', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', justifyContent: 'space-between', background: T.bgWarm, flexShrink: 0 }}>
                <span style={{ fontFamily: "'Inter', sans-serif", fontSize: 13, fontWeight: 700 }}>Event Detail</span>
                <button
                  onClick={() => setSelectedEventId(null)}
                  style={{
                    padding: '3px 10px', borderRadius: 4, fontSize: 10, fontWeight: 600,
                    cursor: 'pointer', fontFamily: "'JetBrains Mono', monospace",
                    background: 'rgba(255,255,255,0.04)', border: `1px solid ${T.border}`,
                    color: T.textSecondary, letterSpacing: 0.5,
                  }}
                >
                  CLOSE
                </button>
              </div>

              {/* Snapshot or clip video fallback */}
              <div style={{ position: 'relative', width: '100%', aspectRatio: '16/10', background: '#080a06', overflow: 'hidden', borderBottom: `1px solid ${T.border}` }}>
                {snapshotUrl ? (
                  <img
                    src={snapshotUrl}
                    alt="Event snapshot"
                    style={{ width: '100%', height: '100%', objectFit: 'cover' }}
                    onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
                  />
                ) : selectedFullEvent?.clip_url ? (
                  <video
                    key={selectedFullEvent.clip_url}
                    src={selectedFullEvent.clip_url}
                    controls
                    playsInline
                    style={{ width: '100%', height: '100%', objectFit: 'contain' }}
                  />
                ) : (
                  <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', color: T.textDim, fontSize: 11, fontStyle: 'italic' }}>
                    No snapshot or clip available
                  </div>
                )}
                {/* Severity badge overlay */}
                <div style={{ position: 'absolute', top: 8, right: 8 }}>
                  <SeverityPillLight severity={selectedIncident.severity} size="sm" />
                </div>
              </div>

              {/* Event metadata */}
              <div style={{ padding: '14px 16px', borderBottom: `1px solid ${T.border}` }}>
                <div style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 10, color: T.textDim, letterSpacing: 1, marginBottom: 6 }}>
                  {selectedIncident.id}
                </div>
                <div style={{ fontSize: 14, fontWeight: 700, color: T.textPrimary, marginBottom: 4 }}>
                  {selectedIncident.type.replace(/_/g, ' ')}
                </div>
                <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap' as const, fontSize: 11, color: T.textSecondary }}>
                  <span style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: 10, color: T.textDim }}>
                    {new Date(selectedIncident.ts).toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}
                  </span>
                  <span>
                    {selectedIncident.camera_name || selectedIncident.camera_id?.slice(0, 8)}
                  </span>
                </div>
              </div>

              {/* Disposition card */}
              <div style={{ padding: '12px 16px', borderBottom: `1px solid ${T.border}` }}>
                <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: T.textDim, marginBottom: 8, fontFamily: "'JetBrains Mono', monospace" }}>
                  Disposition
                </div>
                {(() => {
                  const code = selectedIncident.disposition_code || selectedFullEvent?.disposition_code;
                  const label = selectedIncident.disposition_label || selectedFullEvent?.disposition_label;
                  const isFP = code?.startsWith('false');
                  const isVer = code?.startsWith('verified');
                  const dColor = isFP ? T.green : isVer ? T.red : T.amber;
                  const dBg = isFP ? 'rgba(34,197,94,0.08)' : isVer ? 'rgba(239,68,68,0.08)' : 'rgba(232,155,42,0.08)';
                  const dBorder = isFP ? 'rgba(34,197,94,0.25)' : isVer ? 'rgba(239,68,68,0.25)' : 'rgba(232,155,42,0.25)';
                  const icon = isFP ? '✓' : isVer ? '✕' : '●';
                  return code ? (
                    <div style={{
                      padding: '10px 14px', borderRadius: 6, background: dBg,
                      border: `1px solid ${dBorder}`, display: 'flex', alignItems: 'center', gap: 10,
                    }}>
                      <span style={{ fontSize: 16, color: dColor, fontWeight: 700 }}>{icon}</span>
                      <div>
                        <div style={{ fontSize: 12, fontWeight: 700, color: dColor }}>
                          {label || code.replace(/_/g, ' ')}
                        </div>
                        <div style={{ fontSize: 10, color: T.textDim, marginTop: 2, fontFamily: "'JetBrains Mono', monospace" }}>
                          {code}
                        </div>
                      </div>
                    </div>
                  ) : (
                    <div style={{ fontSize: 11, color: T.textDim, fontStyle: 'italic' }}>Pending disposition</div>
                  );
                })()}
              </div>

              {/* Operator */}
              <div style={{ padding: '12px 16px', borderBottom: `1px solid ${T.border}` }}>
                <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: T.textDim, marginBottom: 8, fontFamily: "'JetBrains Mono', monospace" }}>
                  Operator
                </div>
                <div style={{ fontSize: 12, fontWeight: 600, color: T.textPrimary, marginBottom: 4 }}>
                  {selectedIncident.operator_callsign || selectedFullEvent?.operator_callsign || '—'}
                </div>
                {selectedFullEvent?.operator_notes && (
                  <div style={{
                    fontSize: 11, color: T.textSecondary, lineHeight: 1.5,
                    padding: '8px 10px', background: 'rgba(255,255,255,0.02)',
                    border: `1px solid ${T.border}`, borderRadius: 4, marginTop: 4,
                  }}>
                    {selectedFullEvent.operator_notes}
                  </div>
                )}
              </div>

              {/* Action Log Timeline */}
              {selectedFullEvent && selectedFullEvent.action_log && selectedFullEvent.action_log.length > 0 && (
                <div style={{ padding: '12px 16px', borderBottom: `1px solid ${T.border}` }}>
                  <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: T.textDim, marginBottom: 10, fontFamily: "'JetBrains Mono', monospace" }}>
                    Action Log
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
                    {selectedFullEvent.action_log
                      .slice()
                      .sort((a, b) => a.ts - b.ts)
                      .map((entry, idx) => {
                        const isManual = !entry.auto;
                        return (
                          <div key={idx} style={{ display: 'flex', gap: 10, alignItems: 'flex-start', position: 'relative', paddingLeft: 16, paddingBottom: 8, paddingTop: 2 }}>
                            {/* Timeline line */}
                            {idx < selectedFullEvent.action_log.length - 1 && (
                              <div style={{ position: 'absolute', left: 5, top: 10, bottom: 0, width: 1, background: 'rgba(255,255,255,0.06)' }} />
                            )}
                            {/* Dot */}
                            <div style={{
                              position: 'absolute', left: 2, top: 6,
                              width: 7, height: 7, borderRadius: '50%',
                              background: isManual ? T.blue : 'rgba(255,255,255,0.15)',
                              border: isManual ? `1px solid ${T.blue}` : '1px solid rgba(255,255,255,0.1)',
                              flexShrink: 0,
                            }} />
                            <div style={{ flex: 1 }}>
                              <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
                                <span style={{
                                  fontFamily: "'JetBrains Mono', monospace", fontSize: 9, color: T.textDim, flexShrink: 0,
                                }}>
                                  {new Date(entry.ts).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}
                                </span>
                                <span style={{
                                  fontSize: 11,
                                  color: isManual ? T.textPrimary : T.textSecondary,
                                  fontWeight: isManual ? 600 : 400,
                                }}>
                                  {entry.text}
                                </span>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                  </div>
                </div>
              )}

              {/* Clip link */}
              {(selectedFullEvent?.clip_url || selectedIncident?.camera_id) && selectedFullEvent?.clip_url && (
                <div style={{ padding: '12px 16px', borderBottom: `1px solid ${T.border}` }}>
                  <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: T.textDim, marginBottom: 8, fontFamily: "'JetBrains Mono', monospace" }}>
                    Recording
                  </div>
                  <a
                    href={selectedFullEvent.clip_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{
                      display: 'inline-flex', alignItems: 'center', gap: 8,
                      padding: '8px 14px', borderRadius: 5, fontSize: 11, fontWeight: 600,
                      background: 'rgba(59,130,246,0.08)', border: `1px solid rgba(59,130,246,0.25)`,
                      color: T.blue, textDecoration: 'none', cursor: 'pointer',
                    }}
                  >
                    ▶ View Recording
                  </a>
                </div>
              )}
            </div>
          ) : (
            /* ── DEFAULT: Camera Health + Safety panels ── */
            <>
              {/* ── SECURITY ONLY: Camera Health ── */}
              {isSecurityOnly && (
                <div style={{ padding: '16px 18px' }}>
                  <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Camera Health</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {cameras.map(cam => (
                      <div key={cam.id} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ width: 7, height: 7, borderRadius: '50%', background: cam.status === 'online' ? T.green : T.red, flexShrink: 0 }} />
                        <span style={{ fontSize: 11, color: T.textSecondary, flex: 1 }}>{cam.name}</span>
                        {cam.has_alert && (
                          <span style={{ fontSize: 9, color: T.amber, padding: '1px 5px', borderRadius: 3, background: 'rgba(232,155,42,0.1)', border: '1px solid rgba(232,155,42,0.25)' }}>ALERT</span>
                        )}
                      </div>
                    ))}
                    {cameras.length === 0 && (
                      <div style={{ fontSize: 10, color: T.textDim, fontStyle: 'italic', padding: 8 }}>No cameras assigned</div>
                    )}
                  </div>
                </div>
              )}

              {/* ── SAFETY TIER: PPE Breakdown + Workers ── */}
              {!isSecurityOnly && (
                <>
                  <div style={{ padding: '16px 18px', borderBottom: `1px solid ${T.border}` }}>
                    <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>PPE Compliance</div>
                    {ppeItems.map(({ key, label, value }) => {
                      const level = getComplianceLevel(value);
                      const colors = { green: T.green, amber: T.amber, red: T.red };
                      return (
                        <div key={key} style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
                          <span style={{ fontSize: 11, color: T.textSecondary, width: 70, flexShrink: 0 }}>{label}</span>
                          <div style={{ flex: 1, height: 6, background: T.bgWarm, borderRadius: 3, overflow: 'hidden', border: `1px solid ${T.border}` }}>
                            <div style={{ height: '100%', width: `${value}%`, borderRadius: 3, background: colors[level], transition: 'width 0.6s' }} />
                          </div>
                          <span style={{ fontSize: 10, fontWeight: 600, color: colors[level], width: 32, textAlign: 'right' as const, fontFamily: "'JetBrains Mono', monospace" }}>{Math.round(value)}%</span>
                        </div>
                      );
                    })}
                  </div>

                  <div style={{ padding: '16px 18px' }}>
                    <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Workers On Site</div>
                    <div style={{ fontSize: 24, fontWeight: 700, fontFamily: "'Inter', sans-serif", marginBottom: 4 }}>{workersOnSite}</div>
                    <div style={{ fontSize: 11, color: T.textDim }}>Active workers across all zones</div>
                  </div>

                  <div style={{ padding: '16px 18px', borderTop: `1px solid ${T.border}` }}>
                    <div style={{ fontFamily: "'Inter', sans-serif", fontSize: 12, fontWeight: 700, marginBottom: 12 }}>Camera Health</div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      {cameras.map(cam => (
                        <div key={cam.id} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span style={{ width: 7, height: 7, borderRadius: '50%', background: cam.status === 'online' ? T.green : T.red, flexShrink: 0 }} />
                          <span style={{ fontSize: 11, color: T.textSecondary, flex: 1 }}>{cam.name}</span>
                          {cam.has_alert && (
                            <span style={{ fontSize: 9, color: T.amber, padding: '1px 5px', borderRadius: 3, background: 'rgba(232,155,42,0.1)', border: '1px solid rgba(232,155,42,0.25)' }}>ALERT</span>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                </>
              )}

              {/* Empty state hint */}
              <div style={{ padding: '20px 18px', borderTop: `1px solid ${T.border}` }}>
                <div style={{ fontSize: 11, color: T.textDim, fontStyle: 'italic', textAlign: 'center' }}>
                  Select an event from the table to view details
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
