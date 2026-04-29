'use client';

import { useEffect, useState } from 'react';
import { getServicesHealth, ServiceStatus, AIStats } from '@/lib/api';

const STATUS_COLOR: Record<ServiceStatus['status'], string> = {
    up: '#22c55e',
    down: '#ef4444',
    degraded: '#f59e0b',
    unknown: '#6b7280',
};

const STATUS_LABEL: Record<ServiceStatus['status'], string> = {
    up: 'Up',
    down: 'Down',
    degraded: 'Degraded',
    unknown: 'Unknown',
};

// ServicesHealthCard surfaces the four backing services that don't show
// up in camera-recording health: the two GPU AI containers (YOLO,
// Qwen), the mediamtx control API, and the worker process. Polls every
// 15s on its own — independent of RecordingHealthCard's poll cycle so
// a stuck render of one card doesn't pause the other.
export default function ServicesHealthCard() {
    const [rows, setRows] = useState<ServiceStatus[] | null>(null);
    const [stats, setStats] = useState<AIStats | null>(null);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        let cancelled = false;
        const load = async () => {
            try {
                const data = await getServicesHealth();
                if (!cancelled) {
                    setRows(data.services);
                    setStats(data.ai_stats ?? null);
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

    const summary = (() => {
        if (!rows) return null;
        const counts: Record<ServiceStatus['status'], number> = { up: 0, down: 0, degraded: 0, unknown: 0 };
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
                    <div style={{ fontSize: 11, color: '#9ca3af', letterSpacing: 1 }}>SERVICES</div>
                    <div style={{ fontSize: 18, color: '#e5e7eb', fontWeight: 600 }}>AI &amp; infrastructure</div>
                </div>
                {summary && (
                    <div style={{ display: 'flex', gap: 12, fontSize: 12 }}>
                        {(['up', 'degraded', 'down', 'unknown'] as const).map(s => (
                            <div key={s} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                                <span style={{
                                    width: 8, height: 8, borderRadius: 4,
                                    background: STATUS_COLOR[s], display: 'inline-block',
                                }} />
                                <span style={{ color: '#9ca3af' }}>{summary[s]} {STATUS_LABEL[s]}</span>
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
                <div style={{ color: '#6b7280', fontSize: 13 }}>No service probes configured.</div>
            )}

            {rows && rows.length > 0 && (
                <table style={{ width: '100%', fontSize: 13, borderCollapse: 'collapse' }}>
                    <thead>
                        <tr style={{ color: '#6b7280', textAlign: 'left' }}>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Service</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Status</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Detail</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, minWidth: 150 }}>GPU</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Endpoint</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, textAlign: 'right' }}>Latency</th>
                        </tr>
                    </thead>
                    <tbody>
                        {rows.map(r => (
                            <tr key={r.name} style={{ borderTop: '1px solid #1f2937', color: '#d1d5db' }}>
                                <td style={{ padding: '6px 4px', fontWeight: 600 }}>{r.name}</td>
                                <td style={{ padding: '6px 4px' }}>
                                    <span style={{
                                        display: 'inline-block',
                                        padding: '2px 8px',
                                        borderRadius: 4,
                                        background: STATUS_COLOR[r.status] + '22',
                                        color: STATUS_COLOR[r.status],
                                        fontSize: 11,
                                        fontWeight: 600,
                                    }}>
                                        {STATUS_LABEL[r.status]}
                                    </span>
                                </td>
                                <td style={{ padding: '6px 4px', color: '#9ca3af' }}>{r.detail || '—'}</td>
                                <td style={{ padding: '6px 4px' }}>
                                    <GPUCell row={r} />
                                </td>
                                <td style={{
                                    padding: '6px 4px', color: '#6b7280',
                                    fontFamily: "'JetBrains Mono', monospace", fontSize: 11,
                                }}>
                                    {r.endpoint || '—'}
                                </td>
                                <td style={{
                                    padding: '6px 4px', textAlign: 'right',
                                    color: r.response_ms > 1500 ? '#f59e0b' : '#9ca3af',
                                }}>
                                    {r.response_ms}ms
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}

            {stats && (
                <div style={{ marginTop: 18, paddingTop: 14, borderTop: '1px solid #1f2937' }}>
                    <div style={{ fontSize: 11, color: '#9ca3af', letterSpacing: 1, marginBottom: 10 }}>
                        AI FUNNEL · since process start
                        {stats.yolo_calls === 0 && stats.qwen_calls === 0 && (
                            <span style={{ marginLeft: 10, color: '#6b7280', letterSpacing: 0, textTransform: 'none', fontSize: 11 }}>
                                (waiting for the first triggered event)
                            </span>
                        )}
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                        <FunnelBlock
                            label="YOLO detection"
                            calls={stats.yolo_calls}
                            confirmed={stats.yolo_confirmed}
                            filtered={stats.yolo_filtered}
                            avgMs={stats.yolo_avg_ms}
                        />
                        <FunnelBlock
                            label="Qwen VLM"
                            calls={stats.qwen_calls}
                            confirmed={stats.qwen_confirmed}
                            filtered={stats.qwen_filtered}
                            avgMs={stats.qwen_avg_ms}
                        />
                    </div>
                </div>
            )}
        </div>
    );
}

function GPUCell({ row }: { row: ServiceStatus }) {
    const hasUtil = typeof row.gpu_util_pct === 'number';
    const hasMem = typeof row.gpu_memory_used_mb === 'number' && typeof row.gpu_memory_total_mb === 'number';
    if (!hasUtil && !hasMem) {
        return <span style={{ color: '#4b5563', fontSize: 11 }}>—</span>;
    }
    const util = hasUtil ? (row.gpu_util_pct as number) : 0;
    const utilColor = util >= 90 ? '#ef4444' : util >= 60 ? '#f59e0b' : '#22c55e';
    const memUsed = (row.gpu_memory_used_mb ?? 0) / 1024;
    const memTotal = (row.gpu_memory_total_mb ?? 0) / 1024;
    const memPct = memTotal > 0 ? (memUsed / memTotal) * 100 : 0;
    return (
        <div style={{ minWidth: 140 }}>
            {hasUtil && (
                <>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: '#9ca3af' }}>
                        <span>util</span>
                        <span style={{ color: utilColor, fontWeight: 600 }}>{util}%</span>
                    </div>
                    <div style={{ height: 3, background: '#1f2937', borderRadius: 2, overflow: 'hidden', marginBottom: hasMem ? 4 : 0 }}>
                        <div style={{ width: `${util}%`, height: '100%', background: utilColor }} />
                    </div>
                </>
            )}
            {hasMem && (
                <>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 10, color: '#9ca3af' }}>
                        <span>vram</span>
                        <span>{memUsed.toFixed(1)}/{memTotal.toFixed(1)}GB</span>
                    </div>
                    <div style={{ height: 3, background: '#1f2937', borderRadius: 2, overflow: 'hidden' }}>
                        <div style={{ width: `${memPct}%`, height: '100%', background: '#3b82f6' }} />
                    </div>
                </>
            )}
            {typeof row.gpu_temperature_c === 'number' && (
                <div style={{ fontSize: 10, color: '#6b7280', marginTop: 3 }}>
                    {row.gpu_temperature_c}°C
                </div>
            )}
        </div>
    );
}

function FunnelBlock({ label, calls, confirmed, filtered, avgMs }: {
    label: string; calls: number; confirmed: number; filtered: number; avgMs: number;
}) {
    const confirmedPct = calls > 0 ? Math.round((confirmed / calls) * 100) : 0;
    return (
        <div style={{
            background: '#0b1220',
            border: '1px solid #1f2937',
            borderRadius: 6,
            padding: 12,
        }}>
            <div style={{ fontSize: 11, color: '#9ca3af', marginBottom: 6, fontWeight: 600 }}>{label}</div>
            <div style={{ display: 'flex', gap: 14, alignItems: 'baseline' }}>
                <Metric value={calls.toLocaleString()} label="calls" />
                <Metric value={confirmed.toLocaleString()} label="confirmed" color="#22c55e" />
                <Metric value={filtered.toLocaleString()} label="filtered" color="#f59e0b" />
                <Metric value={avgMs > 0 ? `${avgMs}ms` : '—'} label="avg" />
            </div>
            <div style={{
                marginTop: 8, height: 4, background: '#1f2937',
                borderRadius: 2, overflow: 'hidden', display: 'flex',
            }}>
                <div style={{ width: `${confirmedPct}%`, background: '#22c55e' }} />
                <div style={{ flex: 1, background: '#f59e0b' }} />
            </div>
            <div style={{ fontSize: 10, color: '#6b7280', marginTop: 4 }}>
                {confirmedPct}% pass-through
            </div>
        </div>
    );
}

function Metric({ value, label, color }: { value: string; label: string; color?: string }) {
    return (
        <div>
            <div style={{ fontSize: 18, color: color ?? '#e5e7eb', fontWeight: 600, lineHeight: 1 }}>
                {value}
            </div>
            <div style={{ fontSize: 10, color: '#6b7280', marginTop: 2 }}>{label}</div>
        </div>
    );
}
