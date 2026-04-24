'use client';

import { useState, useEffect } from 'react';
import type { AlertEvent, SiteSOP } from '@/types/ironsight';
import { formatRelativeTime } from '@/lib/format';
import SeverityPill from '@/components/shared/SeverityPill';
import SLATimer from '@/components/operator/SLATimer';
import CreateIncidentModal from '@/components/shared/CreateIncidentModal';
import { getSiteSOPs } from '@/lib/ironsight-api';
import { useOperatorStore } from '@/stores/operator-store';
import { claimAlert as apiClaimAlert, releaseAlert as apiReleaseAlert } from '@/lib/ironsight-api';

interface Props {
  alert: AlertEvent;
  onClose: () => void;
}

// Map alert types to SOP categories for matching
const TYPE_TO_CATEGORY: Record<string, string> = {
  no_harness: 'safety', no_hard_hat: 'safety', no_hi_vis: 'safety',
  zone_breach: 'access', vehicle_hazard: 'equipment', info: 'general',
};

const ESCALATION_LABELS: Record<number, { label: string; color: string }> = {
  0: { label: 'Not Escalated', color: '#4A5268' },
  1: { label: 'Level 1 — SOC Supervisor', color: '#E89B2A' },
  2: { label: 'Level 2 — Site Manager', color: '#EF4444' },
  3: { label: 'Level 3 — Emergency Services', color: '#EF4444' },
};

export default function AlertDetailSlideout({ alert, onClose }: Props) {
  const [sops, setSOPs] = useState<SiteSOP[]>([]);
  const [loadingSOPs, setLoadingSOPs] = useState(true);
  const [expandedSOP, setExpandedSOP] = useState<string | null>(null);
  const [showCreateIncident, setShowCreateIncident] = useState(false);

  const currentOperator = useOperatorStore(s => s.currentOperator);
  const claimAlertStore = useOperatorStore(s => s.claimAlert);
  const releaseAlertStore = useOperatorStore(s => s.releaseAlert);
  const acknowledgeAlert = useOperatorStore(s => s.acknowledgeAlert);

  const isMyClaim = alert.assigned_operator_id === currentOperator?.id;
  const isOtherClaim = !!alert.assigned_operator_id && !isMyClaim;

  // Load SOPs for this alert's site
  useEffect(() => {
    getSiteSOPs(alert.site_id).then(data => {
      // Filter to relevant SOPs based on alert type
      const category = TYPE_TO_CATEGORY[alert.type];
      const relevant = category
        ? data.filter(s => s.category === category || s.priority === 'critical')
        : data;
      setSOPs(relevant);
      setLoadingSOPs(false);
    });
  }, [alert.site_id, alert.type]);

  const handleClaim = () => {
    if (!currentOperator) return;
    claimAlertStore(alert.id);
    apiClaimAlert(alert.id, currentOperator.id, currentOperator.callsign);
  };

  const handleRelease = () => {
    releaseAlertStore(alert.id);
    apiReleaseAlert(alert.id);
  };

  const handleAcknowledge = () => {
    acknowledgeAlert(alert.id);
  };

  const escInfo = ESCALATION_LABELS[alert.escalation_level || 0] || ESCALATION_LABELS[0];

  return (
    <div style={{
      position: 'fixed', top: 0, right: 0, bottom: 0, width: 420, zIndex: 9000,
      background: '#0E1117', borderLeft: '1px solid rgba(255,255,255,0.08)',
      boxShadow: '-20px 0 60px rgba(0,0,0,0.6)',
      display: 'flex', flexDirection: 'column',
      animation: 'slideout-enter 0.25s ease-out',
      fontFamily: "'Inter', sans-serif",
    }}>
      {/* ── Header ── */}
      <div style={{
        padding: '14px 16px', borderBottom: '1px solid rgba(255,255,255,0.06)',
        display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0,
      }}>
        <SeverityPill severity={alert.severity} size="sm" />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 13, fontWeight: 700, color: '#E4E8F0' }}>{alert.site_name}</div>
          <div style={{ fontSize: 10, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
            {alert.id} · {formatRelativeTime(alert.ts)}
          </div>
        </div>
        {alert.sla_deadline_ms && !alert.acknowledged && (
          <SLATimer deadlineMs={alert.sla_deadline_ms} acknowledged={alert.acknowledged} />
        )}
        <button onClick={onClose} style={{
          background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
          borderRadius: 4, padding: '4px 8px', color: '#4A5268', cursor: 'pointer',
          fontSize: 11,
        }}>✕</button>
      </div>

      {/* ── Scrollable Content ── */}
      <div style={{ flex: 1, overflowY: 'auto', scrollbarWidth: 'thin' as const }}>
        {/* Snapshot placeholder */}
        <div style={{
          height: 180, background: 'linear-gradient(135deg, #141a0c, #1a200e 50%, #0a0f08)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          position: 'relative', overflow: 'hidden',
        }}>
          <div style={{ color: '#2a3848', fontSize: 36 }}>📷</div>
          <div style={{
            position: 'absolute', top: 8, left: 8,
            padding: '2px 8px', borderRadius: 2, fontSize: 9, fontWeight: 700,
            background: 'rgba(255,51,85,0.9)', color: '#fff',
            letterSpacing: 1,
          }}>
            ● REC
          </div>
          <div style={{
            position: 'absolute', bottom: 8, left: 8, right: 8,
            display: 'flex', justifyContent: 'space-between',
            fontSize: 9, fontFamily: "'JetBrains Mono', monospace", color: 'rgba(255,255,255,0.4)',
          }}>
            <span>{alert.camera_name}</span>
            <span>{new Date(alert.ts).toLocaleTimeString('en-US', { hour12: false })}</span>
          </div>
        </div>

        {/* Description */}
        <div style={{ padding: '14px 16px' }}>
          <div style={{ fontSize: 13, color: '#E4E8F0', lineHeight: 1.5, marginBottom: 12 }}>
            {alert.description}
          </div>

          {/* Meta chips */}
          <div style={{ display: 'flex', flexWrap: 'wrap' as const, gap: 6, marginBottom: 14 }}>
            {[
              { label: alert.camera_name, icon: '📷' },
              { label: alert.type.replace(/_/g, ' '), icon: '🏷' },
              { label: new Date(alert.ts).toLocaleTimeString('en-US', { hour12: false }), icon: '🕐' },
            ].map((chip, i) => (
              <span key={i} style={{
                fontSize: 10, padding: '3px 8px', borderRadius: 3,
                background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.06)',
                color: '#8891A5', display: 'flex', alignItems: 'center', gap: 4,
              }}>
                {chip.icon} {chip.label}
              </span>
            ))}
          </div>

          {/* Escalation Status */}
          <div style={{
            padding: '10px 12px', borderRadius: 4, marginBottom: 14,
            background: `${escInfo.color}08`, border: `1px solid ${escInfo.color}20`,
          }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: '#4A5268', marginBottom: 4 }}>
              Escalation Status
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div style={{
                width: 8, height: 8, borderRadius: '50%', background: escInfo.color,
                boxShadow: `0 0 8px ${escInfo.color}60`,
              }} />
              <span style={{ fontSize: 11, color: escInfo.color, fontWeight: 600 }}>{escInfo.label}</span>
            </div>
            {alert.escalated_at && (
              <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginTop: 3 }}>
                Escalated {formatRelativeTime(alert.escalated_at)}
              </div>
            )}
          </div>

          {/* Ownership */}
          <div style={{
            padding: '10px 12px', borderRadius: 4, marginBottom: 14,
            background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)',
          }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1, textTransform: 'uppercase' as const, color: '#4A5268', marginBottom: 6 }}>
              Ownership
            </div>
            {alert.assigned_operator_id ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <div style={{
                  width: 24, height: 24, borderRadius: '50%', fontSize: 10, fontWeight: 700,
                  background: isMyClaim ? 'rgba(0,229,160,0.15)' : 'rgba(168,85,247,0.15)',
                  color: isMyClaim ? '#22C55E' : '#a855f7',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  border: `1px solid ${isMyClaim ? 'rgba(0,229,160,0.3)' : 'rgba(168,85,247,0.3)'}`,
                }}>{alert.assigned_operator_callsign?.slice(-1)}</div>
                <div>
                  <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>{alert.assigned_operator_callsign}</div>
                  <div style={{ fontSize: 9, color: '#4A5268' }}>{isMyClaim ? 'Claimed by you' : 'Claimed by another operator'}</div>
                </div>
              </div>
            ) : (
              <div style={{ fontSize: 11, color: '#4A5268' }}>Unclaimed — available for ownership</div>
            )}
          </div>

          {/* ── SOPs Section ── */}
          <div style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268', marginBottom: 8 }}>
              📖 Relevant SOPs ({sops.length})
            </div>

            {loadingSOPs && <div style={{ fontSize: 11, color: '#4A5268' }}>Loading SOPs…</div>}

            {sops.map(sop => {
              const expanded = expandedSOP === sop.id;
              const prioColors: Record<string, string> = { critical: '#EF4444', high: '#EF4444', medium: '#E89B2A' };
              return (
                <div key={sop.id} style={{
                  border: `1px solid ${expanded ? 'rgba(168,85,247,0.2)' : 'rgba(255,255,255,0.06)'}`,
                  borderRadius: 4, marginBottom: 6, overflow: 'hidden',
                  background: expanded ? 'rgba(168,85,247,0.03)' : 'transparent',
                }}>
                  <div
                    onClick={() => setExpandedSOP(expanded ? null : sop.id)}
                    style={{
                      padding: '8px 12px', cursor: 'pointer',
                      display: 'flex', alignItems: 'center', gap: 8,
                    }}
                  >
                    <span style={{ fontSize: 10, transform: expanded ? 'rotate(90deg)' : 'rotate(0)', transition: 'transform 0.15s' }}>▶</span>
                    <div style={{ flex: 1 }}>
                      <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>{sop.title}</div>
                    </div>
                    <span style={{
                      fontSize: 8, padding: '1px 5px', borderRadius: 2, fontWeight: 700,
                      textTransform: 'uppercase' as const, letterSpacing: 0.5,
                      color: prioColors[sop.priority] || '#4A5268',
                      border: `1px solid ${(prioColors[sop.priority] || '#4A5268')}30`,
                    }}>{sop.priority}</span>
                  </div>

                  {expanded && (
                    <div style={{ padding: '0 12px 10px' }}>
                      <ol style={{ margin: 0, paddingLeft: 18, fontSize: 11, color: '#8891A5', lineHeight: 1.8 }}>
                        {sop.steps.map((step, i) => (
                          <li key={i} style={{ color: '#E4E8F0' }}>
                            <span style={{ color: '#8891A5' }}>{step}</span>
                          </li>
                        ))}
                      </ol>
                      {sop.contacts && sop.contacts.length > 0 && (
                        <div style={{ marginTop: 8, paddingTop: 8, borderTop: '1px solid rgba(255,255,255,0.04)' }}>
                          <div style={{ fontSize: 9, color: '#4A5268', fontWeight: 600, letterSpacing: 1, marginBottom: 4 }}>CONTACTS</div>
                          {sop.contacts.map((c, i) => (
                            <div key={i} style={{ fontSize: 10, color: '#8891A5', marginBottom: 2 }}>
                              <span style={{ fontWeight: 600, color: '#E4E8F0' }}>{c.name}</span>
                              <span style={{ color: '#4A5268' }}> · {c.role}</span>
                              {c.phone && <span style={{ color: '#E8732A', fontFamily: "'JetBrains Mono', monospace" }}> · {c.phone}</span>}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}

            {!loadingSOPs && sops.length === 0 && (
              <div style={{ fontSize: 11, color: '#4A5268', fontStyle: 'italic' }}>No SOPs configured for this alert type</div>
            )}
          </div>
        </div>
      </div>

      {/* ── Action Bar ── */}
      <div style={{
        padding: '12px 16px', borderTop: '1px solid rgba(255,255,255,0.06)',
        display: 'flex', gap: 8, flexShrink: 0,
      }}>
        {!alert.acknowledged && (
          <button onClick={handleAcknowledge} style={{
            flex: 1, padding: '8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
            border: '1px solid rgba(0,229,160,0.3)', background: 'rgba(0,229,160,0.1)',
            color: '#22C55E', cursor: 'pointer', fontFamily: 'inherit',
          }}>
            ✓ Acknowledge
          </button>
        )}

        {!alert.assigned_operator_id ? (
          <button onClick={handleClaim} style={{
            flex: 1, padding: '8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
            border: '1px solid rgba(0,212,255,0.3)', background: 'rgba(0,212,255,0.1)',
            color: '#E8732A', cursor: 'pointer', fontFamily: 'inherit',
          }}>
            ⚡ Claim
          </button>
        ) : isMyClaim ? (
          <button onClick={handleRelease} style={{
            flex: 1, padding: '8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
            border: '1px solid rgba(255,255,255,0.08)', background: 'rgba(255,255,255,0.03)',
            color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
          }}>
            ↩ Release
          </button>
        ) : null}

        <button onClick={() => setShowCreateIncident(true)} style={{
          flex: 1, padding: '8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
          border: '1px solid rgba(168,85,247,0.3)', background: 'rgba(168,85,247,0.08)',
          color: '#a855f7', cursor: 'pointer', fontFamily: 'inherit',
        }}>
          📌 Incident
        </button>

        <button onClick={onClose} style={{
          padding: '8px 14px', borderRadius: 4, fontSize: 11, fontWeight: 600,
          border: '1px solid rgba(255,255,255,0.06)', background: 'rgba(255,255,255,0.02)',
          color: '#4A5268', cursor: 'pointer', fontFamily: 'inherit',
        }}>
          Close
        </button>
      </div>

      {/* Create Incident Modal */}
      {showCreateIncident && (
        <CreateIncidentModal
          onClose={() => setShowCreateIncident(false)}
          prefill={{
            site_id: alert.site_id,
            site_name: alert.site_name,
            camera_id: alert.camera_id,
            camera_name: alert.camera_name,
            severity: alert.severity,
            type: alert.type,
            description: alert.description,
            alert_id: alert.id,
          }}
        />
      )}
    </div>
  );
}
