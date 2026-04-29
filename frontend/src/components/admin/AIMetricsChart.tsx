'use client';

import { useEffect, useState } from 'react';
import { getAIMetricsTimeseries, AIMetricSample } from '@/lib/api';

// AIMetricsChart renders two stacked panels: GPU utilization (area) +
// AI calls (stacked bars: confirmed green, filtered amber). One copy
// per AI service. SVG is hand-rolled — there's no chart library in
// the codebase and each panel only renders ~120 sample points, so
// inline SVG is simpler than a dependency.

const WINDOW_OPTIONS = [
    { minutes: 60, label: '1h' },
    { minutes: 240, label: '4h' },
    { minutes: 1440, label: '24h' },
];

export default function AIMetricsChart() {
    const [windowMin, setWindowMin] = useState(60);
    const [yolo, setYolo] = useState<AIMetricSample[]>([]);
    const [qwen, setQwen] = useState<AIMetricSample[]>([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        const load = async () => {
            const data = await getAIMetricsTimeseries(windowMin);
            if (cancelled) return;
            setYolo(data.series.yolo);
            setQwen(data.series.qwen);
            setLoading(false);
        };
        load();
        // Refresh aligned with sampler cadence (30s) so the latest tick
        // shows up shortly after it's written.
        const t = setInterval(load, 30000);
        return () => { cancelled = true; clearInterval(t); };
    }, [windowMin]);

    return (
        <div style={{
            background: '#111827',
            border: '1px solid #1f2937',
            borderRadius: 8,
            padding: 16,
            marginTop: 24,
        }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 14 }}>
                <div>
                    <div style={{ fontSize: 11, color: '#9ca3af', letterSpacing: 1 }}>AI METRICS · TIMELINE</div>
                    <div style={{ fontSize: 18, color: '#e5e7eb', fontWeight: 600 }}>GPU saturation &amp; inference funnel</div>
                </div>
                <div style={{ display: 'flex', gap: 4 }}>
                    {WINDOW_OPTIONS.map(o => (
                        <button
                            key={o.minutes}
                            onClick={() => setWindowMin(o.minutes)}
                            style={{
                                padding: '4px 10px', fontSize: 11, fontWeight: 600,
                                borderRadius: 4, cursor: 'pointer', fontFamily: 'inherit',
                                background: windowMin === o.minutes ? '#1f2937' : 'transparent',
                                color: windowMin === o.minutes ? '#e5e7eb' : '#6b7280',
                                border: `1px solid ${windowMin === o.minutes ? '#374151' : '#1f2937'}`,
                            }}
                        >
                            {o.label}
                        </button>
                    ))}
                </div>
            </div>

            {loading ? (
                <div style={{ color: '#6b7280', fontSize: 13 }}>Loading…</div>
            ) : yolo.length === 0 && qwen.length === 0 ? (
                <div style={{ color: '#6b7280', fontSize: 13 }}>
                    No samples yet. The sampler writes one row per service every 30s — wait a minute and refresh.
                </div>
            ) : (
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                    <ServicePanel label="YOLO" samples={yolo} />
                    <ServicePanel label="Qwen VLM" samples={qwen} />
                </div>
            )}
        </div>
    );
}

function ServicePanel({ label, samples }: { label: string; samples: AIMetricSample[] }) {
    if (samples.length === 0) {
        return (
            <div style={{ background: '#0b1220', border: '1px solid #1f2937', borderRadius: 6, padding: 12 }}>
                <div style={{ fontSize: 11, color: '#9ca3af', fontWeight: 600, marginBottom: 6 }}>{label}</div>
                <div style={{ fontSize: 11, color: '#6b7280' }}>No samples in window</div>
            </div>
        );
    }
    const totalCalls = samples.reduce((s, x) => s + x.calls_delta, 0);
    const totalConfirmed = samples.reduce((s, x) => s + x.confirmed_delta, 0);
    const totalFiltered = samples.reduce((s, x) => s + x.filtered_delta, 0);
    const lastUtil = samples[samples.length - 1]?.gpu_util_pct;
    const lastTemp = samples[samples.length - 1]?.gpu_temperature_c;

    return (
        <div style={{ background: '#0b1220', border: '1px solid #1f2937', borderRadius: 6, padding: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 8 }}>
                <div style={{ fontSize: 11, color: '#9ca3af', fontWeight: 600 }}>{label}</div>
                <div style={{ fontSize: 10, color: '#6b7280' }}>
                    {typeof lastUtil === 'number' ? `${lastUtil}% util` : ''}
                    {typeof lastTemp === 'number' ? ` · ${lastTemp}°C` : ''}
                </div>
            </div>
            <UtilArea samples={samples} />
            <div style={{ marginTop: 10 }}>
                <div style={{ fontSize: 10, color: '#9ca3af', marginBottom: 4 }}>Inferences over window</div>
                <CallsBars samples={samples} />
            </div>
            <div style={{ display: 'flex', gap: 14, marginTop: 8, fontSize: 10 }}>
                <span style={{ color: '#9ca3af' }}>{totalCalls.toLocaleString()} calls</span>
                <span style={{ color: '#22c55e' }}>{totalConfirmed.toLocaleString()} confirmed</span>
                <span style={{ color: '#f59e0b' }}>{totalFiltered.toLocaleString()} filtered</span>
            </div>
        </div>
    );
}

// UtilArea: filled area chart of gpu_util_pct over time. Y is fixed
// 0–100 so the visual scale is comparable across windows. Missing
// samples (gpu_util_pct undefined) draw as a 0 baseline gap so the
// timeline doesn't wrongly imply data continuity.
function UtilArea({ samples }: { samples: AIMetricSample[] }) {
    const W = 320, H = 60;
    const n = samples.length;
    if (n === 0) return null;

    const xAt = (i: number) => (n === 1 ? W / 2 : (i / (n - 1)) * W);
    const yAt = (v: number) => H - (Math.min(100, Math.max(0, v)) / 100) * H;

    let path = '';
    let fill = '';
    for (let i = 0; i < n; i++) {
        const v = samples[i].gpu_util_pct;
        if (typeof v !== 'number') continue;
        const x = xAt(i);
        const y = yAt(v);
        path += `${path === '' ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)} `;
        fill += `${fill === '' ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)} `;
    }
    if (fill) {
        // Close the area down to the baseline.
        const lastX = xAt(n - 1);
        const firstSample = samples.findIndex(s => typeof s.gpu_util_pct === 'number');
        const firstX = firstSample >= 0 ? xAt(firstSample) : 0;
        fill += `L${lastX.toFixed(1)},${H} L${firstX.toFixed(1)},${H} Z`;
    }

    return (
        <svg width="100%" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ display: 'block' }}>
            {/* gridlines at 25/50/75% */}
            {[25, 50, 75].map(g => (
                <line key={g} x1={0} y1={yAt(g)} x2={W} y2={yAt(g)}
                    stroke="#1f2937" strokeWidth={0.5} strokeDasharray="2 3" />
            ))}
            {fill && <path d={fill} fill="#22c55e22" />}
            {path && <path d={path} fill="none" stroke="#22c55e" strokeWidth={1.5} />}
        </svg>
    );
}

// CallsBars: stacked bars per sample. Confirmed (green) on top of
// filtered (amber). Heights scaled to the max bar in the window so
// the chart never collapses to nothing during a quiet stretch.
function CallsBars({ samples }: { samples: AIMetricSample[] }) {
    const W = 320, H = 40;
    const n = samples.length;
    if (n === 0) return null;
    const maxBar = Math.max(1, ...samples.map(s => s.calls_delta));
    const barW = Math.max(1, (W / n) - 1);

    return (
        <svg width="100%" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ display: 'block' }}>
            {samples.map((s, i) => {
                const total = s.calls_delta;
                if (total <= 0) return null;
                const totalH = (total / maxBar) * H;
                const confirmedH = (s.confirmed_delta / maxBar) * H;
                const filteredH = (s.filtered_delta / maxBar) * H;
                const x = (i / n) * W;
                return (
                    <g key={i}>
                        <rect x={x} y={H - totalH} width={barW} height={filteredH} fill="#f59e0b" />
                        <rect x={x} y={H - totalH + filteredH} width={barW} height={confirmedH} fill="#22c55e" />
                    </g>
                );
            })}
        </svg>
    );
}
