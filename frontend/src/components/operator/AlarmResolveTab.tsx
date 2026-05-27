'use client';

// Extracted from ActiveAlarmView.tsx (P1-B-11 session 11). The Resolve
// tab: action log timeline + manual note input + disposition card picker
// + TMA-AVS-01 validation factors + Submit button. State stays in the
// parent because most of it (disposition/notes/avsFactors/submitting +
// the handleSubmit closure) is also touched by the keyboard shortcut
// hook and the form-validation logic at the top of the parent. So this
// child is prop-driven.

import type { DispositionCode } from '@/stores/operator-store';
import type { AVSFactors } from '@/lib/ironsight-api';
import { DISPOSITION_SHORT } from './alarm-constants';
import AVSFactorChecklist from './AVSFactorChecklist';

interface Props {
    // Disposition + form state
    disposition: DispositionCode | '';
    setDisposition: (code: DispositionCode | '') => void;
    notes: string;
    setNotes: (v: string) => void;
    avsFactors: AVSFactors;
    toggleAVS: (k: keyof AVSFactors) => void;
    avsPreview: ReturnType<typeof import('@/lib/ironsight-api').previewAVSScore>;
    submitting: boolean;
    handleSubmit: () => void;
    isFalse: boolean;
    keyPrefix: 'f' | 'v' | null;
    // Disposition catalog (filtered once in the parent)
    falseCodes: { code: DispositionCode; label: string; category: string }[];
    verifiedCodes: { code: DispositionCode; label: string; category: string }[];
    // Action log
    actionLog: { ts: number; text: string; auto?: boolean }[];
    actionLogRef: React.RefObject<HTMLDivElement>;
    currentOperator: { callsign?: string } | null;
}

export default function AlarmResolveTab({
    disposition, setDisposition, notes, setNotes,
    avsFactors, toggleAVS, avsPreview, submitting, handleSubmit, isFalse, keyPrefix,
    falseCodes, verifiedCodes,
    actionLog, actionLogRef, currentOperator,
}: Props) {
    return (
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
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                    <div style={{ fontSize: 16, fontWeight: 700, letterSpacing: 1.5, color: '#6B7590', textTransform: 'uppercase' }}>
                        DISPOSITION
                    </div>
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

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 3 }}>
                    {falseCodes.map((fp, idx) => {
                        const vt = verifiedCodes[idx];
                        return [
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

                <AVSFactorChecklist
                    factors={avsFactors}
                    onToggle={toggleAVS}
                    preview={avsPreview}
                />

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
    );
}
