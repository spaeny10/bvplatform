'use client';

import { useEffect, useState } from 'react';
import type { MonitoringWindow } from '@/types/ironsight';
import ScheduleWindowsEditor, { Preset } from './ScheduleWindowsEditor';

// Recording / retention policy moved out of the per-camera settings modal
// in 2026-04 — now lives at the site level. Every camera assigned to the
// site inherits these values on its next recording restart. There is no
// per-camera override by design: retention is a compliance surface, and
// keeping it uniform across a site avoids the "Camera A had a 7-day
// retention, evidence is gone" incident the team hit last quarter.

interface Props {
    siteId: string;
    siteName: string;
}

interface RecordingPolicy {
    retention_days: number;
    recording_mode: 'continuous' | 'event';
    pre_buffer_sec: number;
    post_buffer_sec: number;
    recording_triggers: string;
    recording_schedule: string;
}

const DEFAULT_POLICY: RecordingPolicy = {
    retention_days: 3,
    recording_mode: 'continuous',
    pre_buffer_sec: 10,
    post_buffer_sec: 30,
    recording_triggers: 'motion,object',
    recording_schedule: '',
};

// Mirrors recording.RetentionTiers in the Go backend — keep in sync.
// 3 days is the included default; the rest are paid upgrade tiers.
const RETENTION_TIERS: { days: number; label: string }[] = [
    { days: 3, label: '3 days (included)' },
    { days: 7, label: '7 days (upgrade)' },
    { days: 14, label: '14 days (upgrade)' },
    { days: 30, label: '30 days (upgrade)' },
    { days: 60, label: '60 days (upgrade)' },
    { days: 90, label: '90 days (upgrade)' },
];

// Recording-specific presets. Different goals than monitoring windows —
// here we care about when cameras write to disk, not when the SOC watches.
const RECORDING_PRESETS: Preset[] = [
    {
        label: 'Always (24/7)',
        windows: [
            { label: '24/7', days: [0, 1, 2, 3, 4, 5, 6], start_time: '00:00', end_time: '23:59', enabled: true },
        ],
    },
    {
        label: 'Business hours (weekdays 7am-7pm)',
        windows: [
            { label: 'Weekdays business', days: [1, 2, 3, 4, 5], start_time: '07:00', end_time: '19:00', enabled: true },
        ],
    },
    {
        label: 'After-hours only (weeknights + weekends 24hr)',
        windows: [
            { label: 'Weeknights', days: [1, 2, 3, 4, 5], start_time: '19:00', end_time: '07:00', enabled: true },
            { label: 'Weekends 24hr', days: [0, 6], start_time: '00:00', end_time: '23:59', enabled: true },
        ],
    },
];

// Parse the stored schedule string into a MonitoringWindow[]. The backend
// accepts two shapes; we normalise to the array on load so the editor only
// ever sees one format. Empty string / unparseable → empty array ≡ "always
// record" (the editor's emptyHint explains the behaviour).
function parseSchedule(raw: string): MonitoringWindow[] {
    const s = (raw ?? '').trim();
    if (!s) return [];
    try {
        const parsed = JSON.parse(s);
        if (Array.isArray(parsed)) {
            return parsed.map((w: any, i: number) => ({
                id: w.id || `w-${i}-${Math.random().toString(36).slice(2, 6)}`,
                label: w.label ?? '',
                days: Array.isArray(w.days) ? w.days : [],
                start_time: w.start_time ?? '00:00',
                end_time: w.end_time ?? '23:59',
                enabled: w.enabled !== false,
            }));
        }
        // Legacy single-object format {days, start, end}. Migrate to a
        // one-window array so the editor can display and mutate it.
        if (parsed && typeof parsed === 'object' && Array.isArray(parsed.days)) {
            return [{
                id: `legacy-${Math.random().toString(36).slice(2, 6)}`,
                label: 'Imported',
                days: parsed.days,
                start_time: parsed.start ?? '00:00',
                end_time: parsed.end ?? '23:59',
                enabled: true,
            }];
        }
    } catch { /* fall through to empty */ }
    return [];
}

function apiFetch(url: string, opts: RequestInit = {}) {
    const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : '';
    return fetch(url, {
        ...opts,
        headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...opts.headers },
    });
}

export default function SiteRecordingPanel({ siteId, siteName }: Props) {
    const [policy, setPolicy] = useState<RecordingPolicy>(DEFAULT_POLICY);
    // Schedule is the only field the API stores as an opaque string. We
    // keep it in MonitoringWindow[] form while the user edits so the
    // shared editor can work with it, then stringify on save.
    const [scheduleWindows, setScheduleWindows] = useState<MonitoringWindow[]>([]);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [status, setStatus] = useState<string | null>(null);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            setLoading(true);
            try {
                const res = await apiFetch(`/api/v1/sites/${siteId}`);
                const site = await res.json();
                if (cancelled) return;
                const scheduleRaw = site.recording_schedule ?? '';
                setPolicy({
                    retention_days: site.retention_days ?? 3,
                    recording_mode: site.recording_mode ?? 'continuous',
                    pre_buffer_sec: site.pre_buffer_sec ?? 10,
                    post_buffer_sec: site.post_buffer_sec ?? 30,
                    recording_triggers: site.recording_triggers ?? 'motion,object',
                    recording_schedule: scheduleRaw,
                });
                setScheduleWindows(parseSchedule(scheduleRaw));
            } catch {
                if (!cancelled) setStatus('Failed to load current policy');
            } finally {
                if (!cancelled) setLoading(false);
            }
        })();
        return () => { cancelled = true; };
    }, [siteId]);

    const save = async () => {
        setSaving(true);
        setStatus(null);
        try {
            // Serialise the window array back to the opaque string the API
            // persists. Empty array → empty string (engine treats that as
            // "always record"), keeping a clean round-trip for sites that
            // never touch the schedule.
            const body = {
                ...policy,
                recording_schedule: scheduleWindows.length === 0 ? '' : JSON.stringify(scheduleWindows),
            };
            const res = await apiFetch(`/api/v1/sites/${siteId}/recording`, {
                method: 'PATCH',
                body: JSON.stringify(body),
            });
            if (!res.ok) {
                const msg = await res.text().catch(() => res.statusText);
                throw new Error(msg);
            }
            setStatus('Saved — cameras pick up the new policy on their next recording restart.');
        } catch (err: any) {
            setStatus('Error: ' + (err?.message ?? String(err)));
        } finally {
            setSaving(false);
        }
    };

    const triggerTokens = policy.recording_triggers.split(',').map(t => t.trim()).filter(Boolean);
    const toggleTrigger = (t: string) => {
        const has = triggerTokens.includes(t);
        const next = has ? triggerTokens.filter(x => x !== t) : [...triggerTokens, t];
        setPolicy({ ...policy, recording_triggers: next.join(',') });
    };

    if (loading) {
        return <div style={{ padding: 24, color: '#8891A5', fontSize: 13 }}>Loading policy for {siteName}…</div>;
    }

    return (
        <div style={{ padding: '20px 24px', color: '#E4E8F0', fontSize: 13 }}>
            <div style={{
                padding: '10px 12px', marginBottom: 20, borderRadius: 6,
                background: 'rgba(59,130,246,0.06)', border: '1px solid rgba(59,130,246,0.25)',
                fontSize: 12, color: '#93c5fd',
            }}>
                Every camera on <strong>{siteName}</strong> inherits this policy. There is no per-camera override — change the site policy to change every recorder.
            </div>

            {/* Retention */}
            <Section title="Retention">
                <Row label="Keep recordings for">
                    <select
                        value={policy.retention_days}
                        onChange={e => setPolicy({ ...policy, retention_days: Number(e.target.value) })}
                        style={inputStyle}
                    >
                        {RETENTION_TIERS.map(t => (
                            <option key={t.days} value={t.days}>{t.label}</option>
                        ))}
                    </select>
                    <div style={hintStyle}>
                        3 days is included with every site. Longer retention is a paid upgrade — capacity planning is reviewed when you commit.
                        A platform-wide 85% disk safety valve will force-prune the oldest segments first if storage runs hot.
                    </div>
                </Row>
            </Section>

            {/* Mode */}
            <Section title="Recording Mode">
                <Row label="Mode">
                    <div style={{ display: 'flex', gap: 8 }}>
                        <ModeButton
                            active={policy.recording_mode === 'continuous'}
                            onClick={() => setPolicy({ ...policy, recording_mode: 'continuous' })}
                            label="Continuous"
                            desc="Always recording — best evidence coverage, highest disk use."
                        />
                        <ModeButton
                            active={policy.recording_mode === 'event'}
                            onClick={() => setPolicy({ ...policy, recording_mode: 'event' })}
                            label="Event"
                            desc="Records only around triggered events using a ring buffer."
                        />
                    </div>
                </Row>
                {policy.recording_mode === 'event' && (
                    <>
                        <Row label="Pre-buffer (sec)">
                            <input
                                type="number" min={0} max={300}
                                value={policy.pre_buffer_sec}
                                onChange={e => setPolicy({ ...policy, pre_buffer_sec: Number(e.target.value) })}
                                style={inputStyle}
                            />
                            <div style={hintStyle}>Seconds of footage captured before each trigger.</div>
                        </Row>
                        <Row label="Post-buffer (sec)">
                            <input
                                type="number" min={0} max={600}
                                value={policy.post_buffer_sec}
                                onChange={e => setPolicy({ ...policy, post_buffer_sec: Number(e.target.value) })}
                                style={inputStyle}
                            />
                            <div style={hintStyle}>Seconds of footage captured after each trigger ends.</div>
                        </Row>
                        <Row label="Triggers">
                            <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                                {(['motion', 'object', 'line_cross', 'intrusion', 'loitering', 'audio'] as const).map(t => (
                                    <button
                                        key={t}
                                        type="button"
                                        onClick={() => toggleTrigger(t)}
                                        style={{
                                            padding: '5px 10px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                                            cursor: 'pointer', fontFamily: 'inherit',
                                            background: triggerTokens.includes(t) ? 'rgba(34,197,94,0.12)' : 'rgba(255,255,255,0.04)',
                                            border: `1px solid ${triggerTokens.includes(t) ? 'rgba(34,197,94,0.35)' : 'rgba(255,255,255,0.08)'}`,
                                            color: triggerTokens.includes(t) ? '#22C55E' : '#8891A5',
                                        }}
                                    >
                                        {t}
                                    </button>
                                ))}
                            </div>
                            <div style={hintStyle}>Events that wake the recorder in event mode.</div>
                        </Row>
                    </>
                )}
            </Section>

            {/* Schedule — same multi-window editor the Monitoring Schedule
                tab uses, just with recording-oriented presets. An empty
                list means "record 24/7"; a window covers the hours when
                the recorder is actively writing. */}
            <Section title="Recording Schedule">
                <ScheduleWindowsEditor
                    windows={scheduleWindows}
                    onChange={setScheduleWindows}
                    presets={RECORDING_PRESETS}
                    accent="#3B82F6"
                    emptyHint="No schedule — cameras record 24/7. Add a window to restrict recording to specific hours."
                    addButtonLabel="+ Add recording window"
                />
            </Section>

            {/* Save bar */}
            <div style={{ marginTop: 28, display: 'flex', gap: 10, alignItems: 'center' }}>
                <button
                    type="button"
                    onClick={save}
                    disabled={saving}
                    style={{
                        padding: '8px 18px', borderRadius: 6, fontSize: 12, fontWeight: 700,
                        background: saving ? 'rgba(59,130,246,0.3)' : '#3B82F6',
                        color: 'white', border: 'none', cursor: saving ? 'wait' : 'pointer',
                        fontFamily: 'inherit',
                    }}
                >
                    {saving ? 'Saving…' : 'Save policy'}
                </button>
                {status && (
                    <span style={{ fontSize: 11, color: status.startsWith('Error') ? '#EF4444' : '#22C55E' }}>
                        {status}
                    </span>
                )}
            </div>
        </div>
    );
}

// ── Layout helpers ──

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <div style={{ marginBottom: 18 }}>
            <div style={{
                fontSize: 10, fontWeight: 700, letterSpacing: 1.5, textTransform: 'uppercase',
                color: '#4A5268', marginBottom: 10,
            }}>
                {title}
            </div>
            {children}
        </div>
    );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div style={{ marginBottom: 10 }}>
            <label style={{ display: 'block', fontSize: 11, color: '#8891A5', marginBottom: 4 }}>{label}</label>
            {children}
        </div>
    );
}

function ModeButton({ active, onClick, label, desc }: {
    active: boolean; onClick: () => void; label: string; desc: string;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            style={{
                flex: 1, padding: '10px 14px', borderRadius: 6, textAlign: 'left',
                background: active ? 'rgba(59,130,246,0.12)' : 'rgba(255,255,255,0.03)',
                border: `1px solid ${active ? 'rgba(59,130,246,0.4)' : 'rgba(255,255,255,0.06)'}`,
                color: active ? '#60A5FA' : '#8891A5',
                cursor: 'pointer', fontFamily: 'inherit',
            }}
        >
            <div style={{ fontSize: 12, fontWeight: 700, marginBottom: 2 }}>{label}</div>
            <div style={{ fontSize: 10, opacity: 0.85, fontWeight: 400 }}>{desc}</div>
        </button>
    );
}

const inputStyle: React.CSSProperties = {
    width: '100%', padding: '8px 10px', fontSize: 12,
    background: 'rgba(0,0,0,0.3)', border: '1px solid rgba(255,255,255,0.1)',
    color: '#E4E8F0', borderRadius: 4, fontFamily: 'inherit',
};
const hintStyle: React.CSSProperties = {
    fontSize: 10, color: '#4A5268', marginTop: 4, lineHeight: 1.4,
};
