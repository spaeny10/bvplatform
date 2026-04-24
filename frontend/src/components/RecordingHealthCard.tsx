'use client';

import { useEffect, useState } from 'react';
import { getRecordingHealth, getSDStatus, RecordingHealth, SDStatus } from '@/lib/api';

// SD traffic-light palette. "ok" = card works and ONVIF recording job
// is running; "no_data" = card present but empty; the rest = failure modes.
const SD_COLORS: Record<SDStatus['status'], string> = {
    ok: '#22c55e',
    no_data: '#f59e0b',
    no_card: '#6b7280',
    unreachable: '#ef4444',
};
const SD_LABELS: Record<SDStatus['status'], string> = {
    ok: 'OK',
    no_data: 'Empty',
    no_card: 'No card',
    unreachable: 'Offline',
};

// Traffic-light palette. Matches the rest of the operator console.
const STATUS_COLORS: Record<RecordingHealth['status'], string> = {
    healthy: '#22c55e',
    degraded: '#f59e0b',
    stale: '#ef4444',
    off: '#6b7280',
    unknown: '#6b7280',
};
const STATUS_LABELS: Record<RecordingHealth['status'], string> = {
    healthy: 'Healthy',
    degraded: 'Degraded',
    stale: 'Stale',
    off: 'Off',
    unknown: 'Unknown',
};

// humanBytes trims a byte count to the biggest appropriate unit.
function humanBytes(n: number): string {
    if (!n || n < 1024) return `${n} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let v = n / 1024;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(1)} ${units[i]}`;
}

// humanDuration prints a seconds value as a compact "2h 14m" / "37s".
function humanDuration(sec: number): string {
    if (!sec || sec < 1) return '—';
    if (sec < 60) return `${Math.round(sec)}s`;
    if (sec < 3600) return `${Math.round(sec / 60)}m`;
    const h = Math.floor(sec / 3600);
    const m = Math.round((sec % 3600) / 60);
    return m === 0 ? `${h}h` : `${h}h ${m}m`;
}

/**
 * RecordingHealthCard shows per-camera recording status for the caller's
 * authorized cameras. Surfaces silent-failure modes (stalled recordings,
 * long gaps, wrong recorder engine) that would otherwise only appear in
 * server logs. Polls every 15s; auto-refreshes on mount.
 */
export default function RecordingHealthCard() {
    const [rows, setRows] = useState<RecordingHealth[] | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [sdMap, setSdMap] = useState<Record<string, SDStatus>>({});

    useEffect(() => {
        let cancelled = false;
        const load = async () => {
            try {
                const data = await getRecordingHealth();
                if (!cancelled) {
                    setRows(data);
                    setError(null);
                }
            } catch (e) {
                if (!cancelled) setError(String(e));
            }
        };
        load();
        const t = setInterval(load, 15000);
        return () => { cancelled = true; clearInterval(t); };
    }, []);

    // SD probes are ONVIF round-trips — we fan them out in parallel but
    // only once per camera per card mount. Refresh on the 60s tick, not
    // the 15s DB-stats tick, since the card state barely changes.
    useEffect(() => {
        if (!rows || rows.length === 0) return;
        let cancelled = false;
        const probe = async () => {
            const results = await Promise.all(
                rows.map(r => getSDStatus(r.camera_id).then(s => [r.camera_id, s] as const))
            );
            if (cancelled) return;
            const next: Record<string, SDStatus> = {};
            for (const [id, s] of results) {
                if (s) next[id] = s;
            }
            setSdMap(next);
        };
        probe();
        const t = setInterval(probe, 60000);
        return () => { cancelled = true; clearInterval(t); };
    }, [rows]);

    const summary = (() => {
        if (!rows) return null;
        const counts: Record<string, number> = { healthy: 0, degraded: 0, stale: 0, off: 0 };
        for (const r of rows) counts[r.status] = (counts[r.status] ?? 0) + 1;
        return counts;
    })();

    return (
        <div style={{
            background: '#111827',
            border: '1px solid #1f2937',
            borderRadius: 8,
            padding: 16,
        }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                <div>
                    <div style={{ fontSize: 11, color: '#9ca3af', letterSpacing: 1 }}>RECORDING HEALTH</div>
                    <div style={{ fontSize: 18, color: '#e5e7eb', fontWeight: 600 }}>Last 24h</div>
                </div>
                {summary && (
                    <div style={{ display: 'flex', gap: 12, fontSize: 12 }}>
                        {(['healthy', 'degraded', 'stale', 'off'] as const).map(s => (
                            <div key={s} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                                <span style={{
                                    width: 8, height: 8, borderRadius: 4,
                                    background: STATUS_COLORS[s], display: 'inline-block',
                                }} />
                                <span style={{ color: '#9ca3af' }}>{summary[s]} {STATUS_LABELS[s]}</span>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {error && (
                <div style={{ color: '#ef4444', fontSize: 13 }}>Failed to load: {error}</div>
            )}
            {!error && rows === null && (
                <div style={{ color: '#6b7280', fontSize: 13 }}>Loading…</div>
            )}
            {!error && rows && rows.length === 0 && (
                <div style={{ color: '#6b7280', fontSize: 13 }}>No cameras in your view.</div>
            )}

            {rows && rows.length > 0 && (
                <table style={{ width: '100%', fontSize: 13, borderCollapse: 'collapse' }}>
                    <thead>
                        <tr style={{ color: '#6b7280', textAlign: 'left' }}>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Camera</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Engine</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Segments</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Data</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Last seen</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Longest gap</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>SD</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Status</th>
                        </tr>
                    </thead>
                    <tbody>
                        {rows.map(r => (
                            <tr key={r.camera_id} style={{ borderTop: '1px solid #1f2937', color: '#d1d5db' }}>
                                <td style={{ padding: '6px 4px' }}>{r.camera_name}</td>
                                <td style={{ padding: '6px 4px', color: r.recorder_type === 'gort' ? '#60a5fa' : '#9ca3af' }}>
                                    {r.recorder_type}
                                </td>
                                <td style={{ padding: '6px 4px' }}>{r.segments_24h.toLocaleString()}</td>
                                <td style={{ padding: '6px 4px' }}>{humanBytes(r.bytes_24h)}</td>
                                <td style={{ padding: '6px 4px' }}>
                                    {r.last_segment_at ? humanDuration(r.last_gap_seconds) + ' ago' : '—'}
                                </td>
                                <td style={{ padding: '6px 4px' }}>
                                    {humanDuration(r.longest_gap_seconds_24h)}
                                </td>
                                <td style={{ padding: '6px 4px' }}>
                                    {(() => {
                                        const sd = sdMap[r.camera_id];
                                        if (!sd) return <span style={{ color: '#4b5563' }}>…</span>;
                                        const cap = sd.total_bytes && sd.used_bytes !== undefined
                                            ? ` ${humanBytes(sd.used_bytes)}/${humanBytes(sd.total_bytes)}`
                                            : '';
                                        return (
                                            <span
                                                title={sd.error || (sd.source ? `via ${sd.source}` : '')}
                                                style={{ color: SD_COLORS[sd.status], fontSize: 11 }}
                                            >
                                                ● {SD_LABELS[sd.status]}{cap}
                                            </span>
                                        );
                                    })()}
                                </td>
                                <td style={{ padding: '6px 4px' }}>
                                    <span style={{
                                        display: 'inline-block',
                                        padding: '2px 8px',
                                        borderRadius: 4,
                                        background: STATUS_COLORS[r.status] + '22',
                                        color: STATUS_COLORS[r.status],
                                        fontSize: 11,
                                        fontWeight: 600,
                                    }}>
                                        {STATUS_LABELS[r.status].toUpperCase()}
                                    </span>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
        </div>
    );
}
