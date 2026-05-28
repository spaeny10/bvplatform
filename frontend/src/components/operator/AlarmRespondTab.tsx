'use client';

// Extracted from ActiveAlarmView.tsx (P1-B-11 session 12). The Respond
// tab: site notes + SOP checklists + call tree + quick-log buttons +
// "Next: Resolve" navigation. Prop-driven; state (sops/sopChecked) lives
// in the parent because it's also touched by the data-load effect.

import type { SiteSOP } from '@/types/ironsight';

interface Props {
    siteNotes: string[];
    sops: SiteSOP[];
    sopChecked: Record<string, boolean[]>;
    toggleSopStep: (sopId: string, stepIdx: number) => void;
    addLogEntry: (text: string, auto: boolean) => void;
    quickLog: (text: string) => void;
    copyPhone: (phone: string, contactName: string) => void;
    setWorkflowTab: (tab: 'assess' | 'respond' | 'resolve') => void;
}

const QUICK_LOG_BUTTONS = [
    { label: 'Spoke to Contact', text: 'Spoke to contact on call tree' },
    { label: 'Left Voicemail', text: 'Left voicemail on call tree contact' },
    { label: 'No Answer', text: 'No answer from call tree contact' },
    { label: 'Police Dispatched', text: 'Local police dispatched to site' },
    { label: 'Guard Notified', text: 'On-site guard notified and responding' },
];

export default function AlarmRespondTab({
    siteNotes, sops, sopChecked, toggleSopStep, addLogEntry, quickLog, copyPhone, setWorkflowTab,
}: Props) {
    return (
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
                    {QUICK_LOG_BUTTONS.map(btn => (
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
    );
}
