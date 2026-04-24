'use client';

import type { AlertEvent, SOCIncident, Severity } from '@/types/ironsight';
import { formatRelativeTime } from '@/lib/format';
import SeverityPill from '@/components/shared/SeverityPill';
import SLATimer from '@/components/operator/SLATimer';
import { useOperatorStore } from '@/stores/operator-store';
import { useState, useRef, useEffect } from 'react';
import { claimAlert as apiClaimAlert, releaseAlert as apiReleaseAlert } from '@/lib/ironsight-api';

interface AlertFeedProps {
  alerts: AlertEvent[];
  onAlertClick?: (alert: AlertEvent) => void;
  onIncidentClick?: (incident: SOCIncident) => void;
}

const ALERT_TYPE_ICONS: Record<string, string> = {
  motion_detection: '👁️',
  intrusion: '🚨',
  loitering: '⏱️',
  perimeter_breach: '🔴',
  crowd_detection: '👥',
  vehicle_detection: '🚗',
  fire_smoke: '🔥',
  tailgating: '🚶',
  weapon_detection: '⚠️',
  face_recognition: '🪪',
  linecross: '🚧',
  human: '🚶',
  vehicle: '🚗',
  face: '🪪',
  lpr: '🔢',
  object: '📦',
};

const ESCALATION_COLORS = ['#4A5268', '#E89B2A', '#EF4444', '#FF0055'];

function getEscalationLabel(level: number): string {
  if (level <= 0) return 'ESC 0';
  if (level === 1) return 'ESC 1';
  if (level === 2) return 'ESC 2';
  return `ESC ${level}`;
}

export default function AlertFeed({ alerts, onAlertClick, onIncidentClick }: AlertFeedProps) {
  const [filter, setFilter] = useState<'all' | Severity>('all');
  const scrollRef = useRef<HTMLDivElement>(null);
  const incidents = useOperatorStore((s) => s.incidents);

  const currentOperator = useOperatorStore((s) => s.currentOperator);
  const claimAlertStore = useOperatorStore((s) => s.claimAlert);
  const releaseAlertStore = useOperatorStore((s) => s.releaseAlert);

  const prevLenRef = useRef(incidents.length + alerts.length);
  useEffect(() => {
    const total = incidents.length + alerts.length;
    if (total > prevLenRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = 0;
    }
    prevLenRef.current = total;
  }, [incidents.length, alerts.length]);

  // Filter incidents by severity
  const filteredIncidents = filter === 'all'
    ? incidents
    : incidents.filter(i => i.severity === filter);

  // Alerts not yet attached to any incident (backward compat / legacy)
  const orphanAlerts = alerts.filter(a =>
    !a.incident_id && !a.acknowledged &&
    (filter === 'all' || a.severity === filter)
  );

  const unackCount = incidents.filter(i => i.status === 'active').length
    + alerts.filter(a => !a.acknowledged && !a.incident_id).length;

  const handleClaim = (e: React.MouseEvent, alertId: string) => {
    e.stopPropagation();
    if (!currentOperator) return;
    claimAlertStore(alertId);
    apiClaimAlert(alertId, currentOperator.id, currentOperator.callsign);
  };

  const handleRelease = (e: React.MouseEvent, alertId: string) => {
    e.stopPropagation();
    releaseAlertStore(alertId);
    apiReleaseAlert(alertId);
  };

  const getOwnershipClass = (alert: AlertEvent): string => {
    if (!alert.assigned_operator_id) return 'unowned';
    if (alert.assigned_operator_id === currentOperator?.id) return 'owned-mine';
    return 'owned-other';
  };

  // When clicking an incident, find the latest alarm in it and pass that to onAlertClick
  const handleIncidentClick = (incident: SOCIncident) => {
    if (onIncidentClick) {
      onIncidentClick(incident);
      return;
    }
    // Fallback: find latest alarm for this incident and open it
    if (onAlertClick) {
      const alarm = alerts
        .filter(a => a.incident_id === incident.id)
        .sort((a, b) => b.ts - a.ts)[0];
      if (alarm) onAlertClick(alarm);
    }
  };

  return (
    <div className="op-alerts-panel">
      <div className="op-alerts-header">
        <span className="op-alerts-title">Alerts</span>
        {unackCount > 0 && (
          <span className="op-alert-count-badge">{unackCount}</span>
        )}
      </div>

      <div className="op-filter-row">
        {(['all', 'critical', 'high', 'medium', 'low'] as const).map(f => (
          <button
            key={f}
            className={`op-filter-chip ${filter === f ? 'active' : ''}`}
            onClick={() => setFilter(f)}
          >
            {f}
          </button>
        ))}
      </div>

      <div className="op-alerts-scroll" ref={scrollRef}>
        {/* ── Incidents (grouped alarms) ── */}
        {filteredIncidents.map(incident => {
          const typeIcon = ALERT_TYPE_ICONS[incident.latest_type] || '⚡';
          const cameraCount = incident.camera_ids?.length ?? 0;

          return (
            <div
              key={incident.id}
              className="op-alert-item unread"
              data-severity={incident.severity}
              onClick={() => handleIncidentClick(incident)}
              style={{ cursor: 'pointer' }}
            >
              {/* Header row */}
              <div className="op-alert-header">
                <span style={{ fontSize: 13, lineHeight: 1, flexShrink: 0 }}>
                  {typeIcon}
                </span>

                <SeverityPill severity={incident.severity as Severity} size="sm" />
                <span className="op-alert-site" style={{ flex: 1, minWidth: 0 }}>{incident.site_name}</span>

                {/* Multi-alarm badge */}
                {incident.alarm_count > 1 && (
                  <span style={{
                    fontSize: 8, padding: '1px 5px', borderRadius: 3, fontWeight: 700,
                    background: 'rgba(232,155,42,0.12)',
                    border: '1px solid rgba(232,155,42,0.25)',
                    color: '#E89B2A',
                    fontFamily: "'JetBrains Mono', monospace",
                    flexShrink: 0,
                  }}>
                    ×{incident.alarm_count}
                  </span>
                )}

                {/* SLA Timer */}
                {incident.sla_deadline_ms && incident.status === 'active' && (
                  <SLATimer deadlineMs={incident.sla_deadline_ms} acknowledged={false} />
                )}

                <span className="op-alert-time">{formatRelativeTime(incident.first_alarm_ts)}</span>
              </div>

              {/* Content row: snapshot left, details right */}
              <div style={{ display: 'flex', gap: 10 }}>
                {/* Snapshot thumbnail */}
                {incident.snapshot_url && (
                  <div style={{
                    width: 120, flexShrink: 0,
                    borderRadius: 4, overflow: 'hidden',
                    aspectRatio: '16/9', background: '#080a06',
                    position: 'relative',
                  }}>
                    <img
                      src={incident.snapshot_url}
                      alt="Event snapshot"
                      style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                      onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
                    />
                    <div style={{
                      position: 'absolute', bottom: 2, right: 3,
                      fontSize: 6, fontFamily: "'JetBrains Mono', monospace",
                      background: 'rgba(0,0,0,0.75)', color: 'rgba(255,255,255,0.5)',
                      padding: '1px 3px', borderRadius: 2, letterSpacing: 0.5,
                    }}>
                      AI CAPTURE
                    </div>
                  </div>
                )}

                {/* Description + camera */}
                <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4, justifyContent: 'center' }}>
                  <div className="op-alert-desc">{incident.description}</div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                    {(incident.camera_names ?? []).map((name, idx) => (
                      <span key={incident.camera_ids[idx]} className="op-alert-camera" style={{
                        minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                      }}>
                        📷 {name}
                      </span>
                    ))}
                    {cameraCount === 0 && (
                      <span className="op-alert-camera">📷 Unknown</span>
                    )}
                  </div>
                </div>
              </div>
            </div>
          );
        })}

        {/* ── Orphan alerts (not part of any incident — backward compat) ── */}
        {orphanAlerts.map(alert => {
          const ownerClass = getOwnershipClass(alert);
          const typeIcon = ALERT_TYPE_ICONS[alert.type] || '⚡';
          const escalationLevel = alert.escalation_level ?? 0;
          const escalationColor = ESCALATION_COLORS[Math.min(escalationLevel, ESCALATION_COLORS.length - 1)];

          return (
            <div
              key={alert.id}
              className={`op-alert-item ${!alert.acknowledged ? 'unread' : ''} ${ownerClass}`}
              data-severity={alert.severity}
              onClick={() => onAlertClick?.(alert)}
              style={{ cursor: onAlertClick ? 'pointer' : undefined }}
            >
              {/* Header row */}
              <div className="op-alert-header">
                <span style={{
                  fontSize: 13, lineHeight: 1, flexShrink: 0,
                  filter: alert.acknowledged ? 'grayscale(0.6) opacity(0.6)' : undefined,
                }}>
                  {typeIcon}
                </span>

                <SeverityPill severity={alert.severity} size="sm" />
                <span className="op-alert-site" style={{ flex: 1, minWidth: 0 }}>{alert.site_name}</span>

                {/* Escalation badge */}
                {escalationLevel > 0 && (
                  <span style={{
                    fontSize: 8, padding: '1px 5px', borderRadius: 3, fontWeight: 700,
                    background: `${escalationColor}18`,
                    border: `1px solid ${escalationColor}40`,
                    color: escalationColor,
                    fontFamily: "'JetBrains Mono', monospace",
                    flexShrink: 0,
                  }}>
                    {getEscalationLabel(escalationLevel)}
                  </span>
                )}

                {/* Ownership badge */}
                {alert.assigned_operator_id && (
                  <span className={`op-alert-owner ${alert.assigned_operator_id === currentOperator?.id ? 'mine' : 'other'}`}>
                    {alert.assigned_operator_callsign}
                  </span>
                )}

                {/* SLA Timer */}
                {alert.sla_deadline_ms && !alert.acknowledged && (
                  <SLATimer deadlineMs={alert.sla_deadline_ms} acknowledged={alert.acknowledged} />
                )}

                <span className="op-alert-time">{formatRelativeTime(alert.ts)}</span>
              </div>

              {/* Content row: snapshot left, details right */}
              <div style={{ display: 'flex', gap: 10 }}>
                {/* Snapshot thumbnail */}
                {alert.snapshot_url && (
                  <div style={{
                    width: 120, flexShrink: 0,
                    borderRadius: 4, overflow: 'hidden',
                    aspectRatio: '16/9', background: '#080a06',
                    position: 'relative',
                  }}>
                    <img
                      src={alert.snapshot_url}
                      alt="Event snapshot"
                      style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                      onError={e => { (e.target as HTMLImageElement).style.display = 'none'; }}
                    />
                    <div style={{
                      position: 'absolute', bottom: 2, right: 3,
                      fontSize: 6, fontFamily: "'JetBrains Mono', monospace",
                      background: 'rgba(0,0,0,0.75)', color: 'rgba(255,255,255,0.5)',
                      padding: '1px 3px', borderRadius: 2, letterSpacing: 0.5,
                    }}>
                      AI CAPTURE
                    </div>
                  </div>
                )}

                {/* Description + camera + claim */}
                <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4, justifyContent: 'center' }}>
                  <div className="op-alert-desc">{alert.description}</div>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 6 }}>
                    <div className="op-alert-camera" style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      📷 {alert.camera_name}
                    </div>
                    {!alert.assigned_operator_id ? (
                      <button
                        className="op-alert-claim-btn claim"
                        onClick={(e) => handleClaim(e, alert.id)}
                      >
                        ⚡ Claim
                      </button>
                    ) : alert.assigned_operator_id === currentOperator?.id ? (
                      <button
                        className="op-alert-claim-btn release"
                        onClick={(e) => handleRelease(e, alert.id)}
                      >
                        Release
                      </button>
                    ) : null}
                  </div>
                </div>
              </div>
            </div>
          );
        })}

        {filteredIncidents.length === 0 && orphanAlerts.length === 0 && (
          <div style={{ padding: 24, textAlign: 'center', color: 'var(--sg-text-dim)', fontSize: 12 }}>
            No {filter === 'all' ? '' : filter} alerts
          </div>
        )}
      </div>
    </div>
  );
}
