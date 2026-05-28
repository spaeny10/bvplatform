'use client';

// Extracted from ActiveAlarmView.tsx (P1-B-11 session 13). The Assess
// tab: AI assessment summary (description + YOLO + PPE violations +
// recommended action + feedback buttons) + incident timeline (when
// multi-alarm) + recent-at-this-site history + "Next: Respond" navigation.
//
// Prop-driven — `displayAlarm` and `recentEvents` already come from the
// parent's data-load effect; `childAlarms` is a parent prop; the timeline
// row updates the parent's selectedChildAlarm + viewMode via callbacks.

import type { AlertEvent } from '@/types/ironsight';
import type { SecurityEventRecord } from '@/lib/ironsight-api';
import AIFeedbackButtons from './AIFeedbackButtons';

interface Props {
    displayAlarm: AlertEvent;
    childAlarms: AlertEvent[] | undefined;
    selectedChildAlarm: AlertEvent | null;
    setSelectedChildAlarm: (a: AlertEvent | null) => void;
    setViewMode: (mode: 'snapshot' | 'clip' | 'live') => void;
    addLogEntry: (text: string, auto: boolean) => void;
    recentEvents: SecurityEventRecord[];
    setWorkflowTab: (tab: 'assess' | 'respond' | 'resolve') => void;
}

export default function AlarmAssessTab({
    displayAlarm, childAlarms,
    selectedChildAlarm, setSelectedChildAlarm,
    setViewMode, addLogEntry,
    recentEvents, setWorkflowTab,
}: Props) {
    return (
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

                    {displayAlarm.ai_detections && displayAlarm.ai_detections.length > 0 && (
                        <div style={{ fontSize: 15, color: '#8891A5', lineHeight: 1.5, marginBottom: 6, fontFamily: "'JetBrains Mono', monospace" }}>
                            YOLO: {displayAlarm.ai_detections.map(d => `${d.class} ${Math.round(d.confidence * 100)}%`).join(', ')}
                        </div>
                    )}

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

                    {displayAlarm.ai_recommended_action && (
                        <div style={{ fontSize: 15, color: '#E89B2A', lineHeight: 1.5, marginTop: 4 }}>
                            {displayAlarm.ai_recommended_action}
                        </div>
                    )}

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
    );
}
