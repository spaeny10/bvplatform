'use client';

import { useState, useEffect } from 'react';

interface EventStats {
    by_type: Record<string, number>;
    by_hour: { hour: string; count: number }[];
    by_camera: { camera_id: string; camera_name: string; count: number }[];
    total: number;
}

interface Props {
    cameraId?: string; // optional filter
}

export default function AnalyticsDashboard({ cameraId }: Props) {
    const [timeRange, setTimeRange] = useState<'24h' | '7d' | '30d'>('24h');
    const [stats, setStats] = useState<EventStats | null>(null);
    const [loading, setLoading] = useState(false);
    const [animKey, setAnimKey] = useState(0);

    useEffect(() => {
        loadStats();
    }, [timeRange, cameraId]);

    const loadStats = async () => {
        setLoading(true);
        try {
            const now = new Date();
            let start: Date;
            switch (timeRange) {
                case '7d': start = new Date(now.getTime() - 7 * 86400000); break;
                case '30d': start = new Date(now.getTime() - 30 * 86400000); break;
                default: start = new Date(now.getTime() - 86400000);
            }

            const qs = new URLSearchParams({
                start_time: start.toISOString(),
                end_time: now.toISOString(),
            });
            if (cameraId) qs.set('camera_id', cameraId);

            // Use the existing events API to fetch events and compute stats client-side
            const token = localStorage.getItem('ironsight_token');
            const res = await fetch(`/api/events?${qs.toString()}&limit=1000`, {
                headers: token ? { Authorization: `Bearer ${token}` } : {},
            });
            if (!res.ok) throw new Error('Failed to fetch events');
            const events: { event_type: string; event_time: string; camera_id: string }[] = await res.json();

            // Compute stats
            const byType: Record<string, number> = {};
            const byHourMap: Record<string, number> = {};
            const byCameraMap: Record<string, number> = {};

            for (const evt of events) {
                // By type
                const type = evt.event_type || 'unknown';
                byType[type] = (byType[type] || 0) + 1;

                // By hour
                const hour = new Date(evt.event_time).getHours().toString().padStart(2, '0') + ':00';
                byHourMap[hour] = (byHourMap[hour] || 0) + 1;

                // By camera
                byCameraMap[evt.camera_id] = (byCameraMap[evt.camera_id] || 0) + 1;
            }

            // Sort by hour
            const byHour = Object.entries(byHourMap)
                .map(([hour, count]) => ({ hour, count }))
                .sort((a, b) => a.hour.localeCompare(b.hour));

            // Sort by camera count, top 10
            const byCamera = Object.entries(byCameraMap)
                .map(([camera_id, count]) => ({ camera_id, camera_name: camera_id.substring(0, 8), count }))
                .sort((a, b) => b.count - a.count)
                .slice(0, 10);

            setStats({ by_type: byType, by_hour: byHour, by_camera: byCamera, total: events.length });
            setAnimKey(k => k + 1); // re-trigger bar animations
        } catch (err) {
            console.error('[Analytics]', err);
        }
        setLoading(false);
    };

    const maxBarVal = (data: { count: number }[]) => Math.max(1, ...data.map(d => d.count));

    const TYPE_COLORS: Record<string, string> = {
        motion: '#3b82f6',
        humandetect: '#f59e0b',
        vehicledetect: '#8b5cf6',
        tampering: '#ef4444',
        regionentrance: '#22c55e',
        regionexit: '#ec4899',
        loitering: '#f97316',
        peoplecount: '#06b6d4',
        unknown: '#6b7280',
    };

    return (
        <div style={{ padding: 0 }}>
            {/* Time range selector pills */}
            <div style={{ display: 'flex', gap: 8, marginBottom: 20 }}>
                {(['24h', '7d', '30d'] as const).map(range => (
                    <button
                        key={range}
                        className={`time-range-pill ${timeRange === range ? 'active' : ''}`}
                        onClick={() => setTimeRange(range)}
                    >
                        {range === '24h' ? 'Last 24 Hours' : range === '7d' ? 'Last 7 Days' : 'Last 30 Days'}
                    </button>
                ))}
                {loading && <span style={{ fontSize: 12, opacity: 0.5, alignSelf: 'center', marginLeft: 8 }}>Loading...</span>}
            </div>

            {stats && (
                <div key={animKey} style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
                    {/* Events by Type — Bar chart */}
                    <div className="stagger-card dash-card" style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 12, padding: 20, border: '1px solid rgba(255,255,255,0.06)' }}>
                        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 16, opacity: 0.7 }}>Events by Type</div>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                            {Object.entries(stats.by_type).sort((a, b) => b[1] - a[1]).map(([type, count], idx) => {
                                const max = Math.max(1, ...Object.values(stats.by_type));
                                const pct = (count / max) * 100;
                                const color = TYPE_COLORS[type] || TYPE_COLORS.unknown;
                                return (
                                    <div key={type}>
                                        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, marginBottom: 3 }}>
                                            <span>{type}</span>
                                            <span style={{ fontWeight: 600 }}>{count}</span>
                                        </div>
                                        <div style={{ height: 6, background: 'rgba(255,255,255,0.06)', borderRadius: 3, overflow: 'hidden' }}>
                                            <div className="progress-bar-grow" style={{
                                                height: '100%', width: `${pct}%`, background: color, borderRadius: 3,
                                                animationDelay: `${idx * 0.08}s`,
                                            }} />
                                        </div>
                                    </div>
                                );
                            })}
                            {Object.keys(stats.by_type).length === 0 && (
                                <div style={{ opacity: 0.4, fontSize: 12, textAlign: 'center', padding: 20 }}>No events in this period</div>
                            )}
                        </div>
                    </div>

                    {/* Events by Hour — Bar chart */}
                    <div className="stagger-card dash-card" style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 12, padding: 20, border: '1px solid rgba(255,255,255,0.06)' }}>
                        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 16, opacity: 0.7 }}>Activity by Hour</div>
                        <div style={{ display: 'flex', alignItems: 'flex-end', gap: 2, height: 120 }}>
                            {Array.from({ length: 24 }, (_, i) => {
                                const hour = i.toString().padStart(2, '0') + ':00';
                                const entry = stats.by_hour.find(h => h.hour === hour);
                                const count = entry?.count || 0;
                                const max = maxBarVal(stats.by_hour);
                                const heightPct = max > 0 ? (count / max) * 100 : 0;
                                return (
                                    <div key={hour} style={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                                        <div
                                            className="bar-grow"
                                            style={{
                                                width: '100%', borderRadius: '3px 3px 0 0',
                                                background: count > 0 ? 'var(--accent-green, #22c55e)' : 'rgba(255,255,255,0.04)',
                                                height: `${Math.max(2, heightPct)}%`,
                                                minHeight: 2,
                                                animationDelay: `${i * 0.03}s`,
                                            }}
                                            title={`${hour}: ${count} events`}
                                        />
                                        {i % 4 === 0 && (
                                            <div style={{ fontSize: 9, opacity: 0.4, marginTop: 4 }}>{i}h</div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    </div>

                    {/* Summary stats */}
                    <div className="stagger-card dash-card" style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 12, padding: 20, border: '1px solid rgba(255,255,255,0.06)' }}>
                        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 16, opacity: 0.7 }}>Summary</div>
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                            <div className="dash-card" style={{ background: 'rgba(255,255,255,0.04)', borderRadius: 8, padding: 12, textAlign: 'center', border: '1px solid transparent' }}>
                                <div className="stat-number" style={{ fontSize: 24, fontWeight: 700 }}>{stats.total}</div>
                                <div style={{ fontSize: 10, opacity: 0.5 }}>Total Events</div>
                            </div>
                            <div className="dash-card" style={{ background: 'rgba(255,255,255,0.04)', borderRadius: 8, padding: 12, textAlign: 'center', border: '1px solid transparent' }}>
                                <div className="stat-number" style={{ fontSize: 24, fontWeight: 700 }}>{Object.keys(stats.by_type).length}</div>
                                <div style={{ fontSize: 10, opacity: 0.5 }}>Event Types</div>
                            </div>
                            <div className="dash-card" style={{ background: 'rgba(255,255,255,0.04)', borderRadius: 8, padding: 12, textAlign: 'center', border: '1px solid transparent' }}>
                                <div className="stat-number" style={{ fontSize: 24, fontWeight: 700 }}>{stats.by_camera.length}</div>
                                <div style={{ fontSize: 10, opacity: 0.5 }}>Active Cameras</div>
                            </div>
                            <div className="dash-card" style={{ background: 'rgba(255,255,255,0.04)', borderRadius: 8, padding: 12, textAlign: 'center', border: '1px solid transparent' }}>
                                <div className="stat-number" style={{ fontSize: 24, fontWeight: 700 }}>
                                    {stats.by_hour.length > 0 ? Math.max(...stats.by_hour.map(h => h.count)) : 0}
                                </div>
                                <div style={{ fontSize: 10, opacity: 0.5 }}>Peak Hour Events</div>
                            </div>
                        </div>
                    </div>

                    {/* Top cameras */}
                    <div className="stagger-card dash-card" style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 12, padding: 20, border: '1px solid rgba(255,255,255,0.06)' }}>
                        <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 16, opacity: 0.7 }}>Top Cameras by Events</div>
                        {stats.by_camera.length === 0 ? (
                            <div style={{ opacity: 0.4, fontSize: 12, textAlign: 'center', padding: 20 }}>No data</div>
                        ) : stats.by_camera.map((cam, i) => {
                            const max = stats.by_camera[0]?.count || 1;
                            const pct = (cam.count / max) * 100;
                            return (
                                <div key={cam.camera_id} style={{ marginBottom: 8 }}>
                                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, marginBottom: 3 }}>
                                        <span>#{i + 1} {cam.camera_name}...</span>
                                        <span style={{ fontWeight: 600 }}>{cam.count}</span>
                                    </div>
                                    <div style={{ height: 5, background: 'rgba(255,255,255,0.06)', borderRadius: 3, overflow: 'hidden' }}>
                                        <div className="progress-bar-grow" style={{
                                            height: '100%', width: `${pct}%`, background: '#3b82f6', borderRadius: 3,
                                            animationDelay: `${i * 0.08}s`,
                                        }} />
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}

            {!stats && !loading && (
                <div style={{ textAlign: 'center', padding: 40, opacity: 0.4 }}>
                    Select a time range to load analytics
                </div>
            )}
        </div>
    );
}
