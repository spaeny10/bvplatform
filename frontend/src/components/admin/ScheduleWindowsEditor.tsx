'use client';

import type { MonitoringWindow } from '@/types/ironsight';

// Shared multi-window schedule editor. Used by both the Monitoring Schedule
// (when the SOC watches this site) and the Recording Schedule (when cameras
// write to disk). The two features share this UX so operators only learn
// the editor once; whatever data shape they store is their concern.

const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export interface Preset {
    label: string;
    windows: Omit<MonitoringWindow, 'id'>[];
}

interface Props {
    windows: MonitoringWindow[];
    onChange: (next: MonitoringWindow[]) => void;
    presets?: Preset[];
    // Accent is used for the day-picker highlight + window border tint so
    // each feature can carry its own visual identity without duplicating
    // the markup. Defaults to the purple the Monitoring Schedule uses.
    accent?: string;
    addButtonLabel?: string;
    emptyHint?: string;
}

function genId() {
    return 'w-' + Math.random().toString(36).slice(2, 8);
}

export default function ScheduleWindowsEditor({
    windows,
    onChange,
    presets,
    accent = '#a855f7',
    addButtonLabel = '+ Add Custom Window',
    emptyHint = 'No windows configured. Use a preset or add one below.',
}: Props) {
    const addWindow = () => {
        onChange([
            ...windows,
            { id: genId(), label: '', days: [1, 2, 3, 4, 5], start_time: '18:00', end_time: '06:00', enabled: true },
        ]);
    };

    const removeWindow = (id: string) => {
        onChange(windows.filter(w => w.id !== id));
    };

    const updateWindow = (id: string, patch: Partial<MonitoringWindow>) => {
        onChange(windows.map(w => (w.id === id ? { ...w, ...patch } : w)));
    };

    const toggleDay = (id: string, day: number) => {
        onChange(windows.map(w => {
            if (w.id !== id) return w;
            const days = w.days.includes(day)
                ? w.days.filter(d => d !== day)
                : [...w.days, day].sort();
            return { ...w, days };
        }));
    };

    const applyPreset = (preset: Preset) => {
        onChange(preset.windows.map(w => ({ ...w, id: genId() })));
    };

    const accentBg04 = accent + '0a';        // roughly 4% alpha hex
    const accentBg06 = accent + '0f';
    const accentBorder20 = accent + '33';
    const accentBorder30 = accent + '4d';

    return (
        <div>
            {presets && presets.length > 0 && (
                <div style={{ marginBottom: 10 }}>
                    <div style={{ fontSize: 10, color: '#8891A5', marginBottom: 6, letterSpacing: 0.5 }}>
                        QUICK PRESETS
                    </div>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                        {presets.map(p => (
                            <button
                                key={p.label}
                                type="button"
                                onClick={() => applyPreset(p)}
                                style={{
                                    padding: '5px 10px', borderRadius: 4, fontSize: 11, cursor: 'pointer',
                                    border: `1px solid ${accentBorder30}`,
                                    background: accentBg06,
                                    color: accent, fontFamily: 'inherit', fontWeight: 500,
                                }}
                            >
                                {p.label}
                            </button>
                        ))}
                    </div>
                </div>
            )}

            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {windows.length === 0 && (
                    <div style={{
                        padding: 16, textAlign: 'center', fontSize: 12, color: '#8891A5',
                        border: '1px dashed rgba(255,255,255,0.08)', borderRadius: 6,
                    }}>
                        {emptyHint}
                    </div>
                )}

                {windows.map(w => (
                    <div key={w.id} style={{
                        padding: '12px 14px', borderRadius: 6,
                        background: w.enabled ? accentBg04 : 'rgba(255,255,255,0.02)',
                        border: `1px solid ${w.enabled ? accentBorder20 : 'rgba(255,255,255,0.06)'}`,
                        opacity: w.enabled ? 1 : 0.6,
                    }}>
                        {/* Label + enable toggle + delete */}
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                            <input
                                type="text"
                                value={w.label}
                                onChange={e => updateWindow(w.id, { label: e.target.value })}
                                placeholder="Window label"
                                style={{
                                    flex: 1, fontSize: 12, padding: '4px 8px', borderRadius: 4,
                                    background: 'rgba(0,0,0,0.2)', border: '1px solid rgba(255,255,255,0.08)',
                                    color: '#E4E8F0', fontFamily: 'inherit',
                                }}
                            />
                            <button
                                type="button"
                                onClick={() => updateWindow(w.id, { enabled: !w.enabled })}
                                style={{
                                    fontSize: 9, fontWeight: 700, padding: '3px 8px', borderRadius: 4,
                                    cursor: 'pointer', fontFamily: 'inherit',
                                    background: w.enabled ? 'rgba(34,197,94,0.1)' : 'rgba(255,255,255,0.04)',
                                    border: `1px solid ${w.enabled ? 'rgba(34,197,94,0.3)' : 'rgba(255,255,255,0.08)'}`,
                                    color: w.enabled ? '#22C55E' : '#8891A5',
                                }}
                            >
                                {w.enabled ? 'ON' : 'OFF'}
                            </button>
                            <button
                                type="button"
                                onClick={() => removeWindow(w.id)}
                                title="Remove window"
                                style={{
                                    fontSize: 12, background: 'none', border: 'none', cursor: 'pointer',
                                    color: '#EF4444', opacity: 0.6, padding: 2,
                                }}
                            >
                                🗑
                            </button>
                        </div>

                        {/* Day picker */}
                        <div style={{ display: 'flex', gap: 4, marginBottom: 8 }}>
                            {DAY_LABELS.map((label, idx) => {
                                const on = w.days.includes(idx);
                                return (
                                    <button
                                        key={idx}
                                        type="button"
                                        onClick={() => toggleDay(w.id, idx)}
                                        style={{
                                            width: 34, height: 26, borderRadius: 4, fontSize: 10, fontWeight: 600,
                                            cursor: 'pointer', fontFamily: 'inherit',
                                            background: on ? accentBg06 : 'rgba(255,255,255,0.03)',
                                            border: `1px solid ${on ? accentBorder30 : 'rgba(255,255,255,0.06)'}`,
                                            color: on ? accent : '#4A5268',
                                        }}
                                    >
                                        {label}
                                    </button>
                                );
                            })}
                        </div>

                        {/* Time range */}
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 11 }}>
                            <span style={{ color: '#8891A5', fontSize: 10, width: 40 }}>From</span>
                            <input
                                type="time"
                                value={w.start_time}
                                onChange={e => updateWindow(w.id, { start_time: e.target.value })}
                                style={timeInputStyle}
                            />
                            <span style={{ color: '#8891A5', fontSize: 10 }}>to</span>
                            <input
                                type="time"
                                value={w.end_time}
                                onChange={e => updateWindow(w.id, { end_time: e.target.value })}
                                style={timeInputStyle}
                            />
                            {w.start_time > w.end_time && (
                                <span style={{ fontSize: 10, color: '#8891A5', fontStyle: 'italic' }}>
                                    (overnight)
                                </span>
                            )}
                        </div>
                    </div>
                ))}
            </div>

            <button
                type="button"
                onClick={addWindow}
                style={{
                    width: '100%', padding: '8px 0', borderRadius: 6, fontSize: 11, fontWeight: 600,
                    cursor: 'pointer', fontFamily: 'inherit', marginTop: 10,
                    background: 'rgba(255,255,255,0.03)', border: '1px dashed rgba(255,255,255,0.12)',
                    color: '#8891A5',
                }}
            >
                {addButtonLabel}
            </button>
        </div>
    );
}

const timeInputStyle: React.CSSProperties = {
    padding: '3px 6px', borderRadius: 4, fontSize: 11,
    background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
    color: 'inherit', fontFamily: "'JetBrains Mono', monospace",
};
