'use client';

import { useEffect, useState } from 'react';
import { getAIUsageBySite, SiteUsageRow } from '@/lib/api';

// AIUsageBySiteCard — per-site AI usage breakdown, modeled after a
// rented-GPU billing page. Each row is one site over the selected
// window; the rate inputs are URL params on every refresh so the user
// can model their own cost without persisting anything.

const WINDOW_OPTIONS = [
    { days: 1, label: '24h' },
    { days: 7, label: '7d' },
    { days: 30, label: '30d' },
];

export default function AIUsageBySiteCard() {
    const [days, setDays] = useState(7);
    const [costYolo, setCostYolo] = useState(0.05);
    const [costQwen, setCostQwen] = useState(0.50);
    const [rows, setRows] = useState<SiteUsageRow[]>([]);
    const [totalCost, setTotalCost] = useState(0);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        const load = async () => {
            const data = await getAIUsageBySite(days, costYolo, costQwen);
            if (cancelled) return;
            setRows(data.sites);
            setTotalCost(data.total_cost);
            setLoading(false);
        };
        load();
        const t = setInterval(load, 60000);
        return () => { cancelled = true; clearInterval(t); };
    }, [days, costYolo, costQwen]);

    const maxCalls = Math.max(1, ...rows.map(r => r.yolo_calls + r.qwen_calls));

    return (
        <div style={{
            background: '#111827',
            border: '1px solid #1f2937',
            borderRadius: 8,
            padding: 16,
            marginTop: 24,
        }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 14, gap: 16 }}>
                <div>
                    <div style={{ fontSize: 11, color: '#9ca3af', letterSpacing: 1 }}>USAGE BY SITE</div>
                    <div style={{ fontSize: 18, color: '#e5e7eb', fontWeight: 600 }}>AI inference billing</div>
                </div>
                <div style={{ display: 'flex', gap: 4 }}>
                    {WINDOW_OPTIONS.map(o => (
                        <button
                            key={o.days}
                            onClick={() => setDays(o.days)}
                            style={{
                                padding: '4px 10px', fontSize: 11, fontWeight: 600,
                                borderRadius: 4, cursor: 'pointer', fontFamily: 'inherit',
                                background: days === o.days ? '#1f2937' : 'transparent',
                                color: days === o.days ? '#e5e7eb' : '#6b7280',
                                border: `1px solid ${days === o.days ? '#374151' : '#1f2937'}`,
                            }}
                        >
                            {o.label}
                        </button>
                    ))}
                </div>
            </div>

            <div style={{ display: 'flex', gap: 14, marginBottom: 14, fontSize: 11, color: '#9ca3af', alignItems: 'center' }}>
                <span>Rates (USD per 1,000 inferences):</span>
                <label style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    YOLO
                    <input
                        type="number" min={0} step={0.01} value={costYolo}
                        onChange={e => setCostYolo(Math.max(0, Number(e.target.value)))}
                        style={inputStyle}
                    />
                </label>
                <label style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                    Qwen
                    <input
                        type="number" min={0} step={0.01} value={costQwen}
                        onChange={e => setCostQwen(Math.max(0, Number(e.target.value)))}
                        style={inputStyle}
                    />
                </label>
                <span style={{ marginLeft: 'auto', color: '#e5e7eb', fontWeight: 600 }}>
                    Total: ${totalCost.toFixed(2)}
                </span>
            </div>

            {loading ? (
                <div style={{ color: '#6b7280', fontSize: 13 }}>Loading…</div>
            ) : rows.length === 0 ? (
                <div style={{ color: '#6b7280', fontSize: 13 }}>
                    No per-site AI activity in this window. Counters populate as cameras fire triggered events through the pipeline.
                </div>
            ) : (
                <table style={{ width: '100%', fontSize: 13, borderCollapse: 'collapse' }}>
                    <thead>
                        <tr style={{ color: '#6b7280', textAlign: 'left' }}>
                            <th style={{ padding: '6px 4px', fontWeight: 500 }}>Site</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, minWidth: 120 }}>Volume</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, textAlign: 'right' }}>YOLO</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, textAlign: 'right' }}>Qwen</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, textAlign: 'right' }}>Confirmed</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, textAlign: 'right' }}>Filtered</th>
                            <th style={{ padding: '6px 4px', fontWeight: 500, textAlign: 'right' }}>Cost</th>
                        </tr>
                    </thead>
                    <tbody>
                        {rows.map(r => {
                            const totalCalls = r.yolo_calls + r.qwen_calls;
                            const confirmed = r.yolo_confirmed + r.qwen_confirmed;
                            const filtered = r.yolo_filtered + r.qwen_filtered;
                            const widthPct = (totalCalls / maxCalls) * 100;
                            return (
                                <tr key={r.site_id} style={{ borderTop: '1px solid #1f2937', color: '#d1d5db' }}>
                                    <td style={{ padding: '6px 4px', fontWeight: 600 }}>{r.site_name}</td>
                                    <td style={{ padding: '6px 4px' }}>
                                        <div style={{ height: 6, background: '#1f2937', borderRadius: 3, overflow: 'hidden', display: 'flex' }}>
                                            <div style={{
                                                width: `${(r.yolo_calls / maxCalls) * 100}%`,
                                                background: '#3b82f6',
                                                height: '100%',
                                            }} />
                                            <div style={{
                                                width: `${(r.qwen_calls / maxCalls) * 100}%`,
                                                background: '#a855f7',
                                                height: '100%',
                                            }} />
                                        </div>
                                        <div style={{ fontSize: 10, color: '#6b7280', marginTop: 2 }}>
                                            {Math.round(widthPct)}% of busiest
                                        </div>
                                    </td>
                                    <td style={{ padding: '6px 4px', textAlign: 'right', color: '#3b82f6' }}>
                                        {r.yolo_calls.toLocaleString()}
                                    </td>
                                    <td style={{ padding: '6px 4px', textAlign: 'right', color: '#a855f7' }}>
                                        {r.qwen_calls.toLocaleString()}
                                    </td>
                                    <td style={{ padding: '6px 4px', textAlign: 'right', color: '#22c55e' }}>
                                        {confirmed.toLocaleString()}
                                    </td>
                                    <td style={{ padding: '6px 4px', textAlign: 'right', color: '#f59e0b' }}>
                                        {filtered.toLocaleString()}
                                    </td>
                                    <td style={{ padding: '6px 4px', textAlign: 'right', color: '#e5e7eb', fontWeight: 600 }}>
                                        ${r.estimated_cost.toFixed(2)}
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            )}
        </div>
    );
}

const inputStyle: React.CSSProperties = {
    width: 70,
    padding: '3px 6px',
    background: '#0b1220',
    border: '1px solid #1f2937',
    borderRadius: 4,
    color: '#e5e7eb',
    fontSize: 11,
    fontFamily: 'inherit',
};
