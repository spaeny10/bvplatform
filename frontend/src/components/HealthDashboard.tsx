'use client';

import { useState, useEffect, useCallback } from 'react';
import { Camera } from '@/lib/api';

interface Props {
    cameras: Camera[];
}

interface CameraHealthInfo {
    id: string;
    name: string;
    status: string;
    recording: boolean;
    recording_mode: string;
    events_enabled: boolean;
    manufacturer: string;
    model: string;
    camera_group: string;
    uptime_pct: number;
    last_event: string;
    stream_fps: number;
    bitrate_kbps: number;
}

const STATUS_COLORS: Record<string, string> = {
    online: '#22c55e',
    offline: '#ef4444',
    degraded: '#f59e0b',
    unknown: '#6b7280',
};

export default function HealthDashboard({ cameras }: Props) {
    const [filter, setFilter] = useState('');
    const [groupFilter, setGroupFilter] = useState('');

    // Derive health info from camera data
    const healthData: CameraHealthInfo[] = cameras.map(cam => ({
        id: cam.id,
        name: cam.name,
        status: cam.status || 'unknown',
        recording: cam.recording,
        recording_mode: cam.recording_mode,
        events_enabled: cam.events_enabled,
        manufacturer: cam.manufacturer,
        model: cam.model,
        camera_group: cam.camera_group || 'Ungrouped',
        uptime_pct: cam.status === 'online' ? 100 : 0,
        last_event: '',
        stream_fps: 0,
        bitrate_kbps: 0,
    }));

    // Get unique groups
    const groups = Array.from(new Set(healthData.map(c => c.camera_group))).sort();

    // Filter
    const filtered = healthData.filter(c => {
        if (filter && !c.name.toLowerCase().includes(filter.toLowerCase())) return false;
        if (groupFilter && c.camera_group !== groupFilter) return false;
        return true;
    });

    // Stats
    const total = cameras.length;
    const online = cameras.filter(c => c.status === 'online').length;
    const recording = cameras.filter(c => c.recording).length;
    const offline = cameras.filter(c => c.status !== 'online').length;

    return (
        <div style={{ padding: 0 }}>
            {/* Summary cards */}
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 20 }}>
                <div className="stagger-card dash-card" style={{ background: 'rgba(255,255,255,0.04)', borderRadius: 10, padding: 16, textAlign: 'center', border: '1px solid rgba(255,255,255,0.06)' }}>
                    <div className="stat-number" style={{ fontSize: 28, fontWeight: 700 }}>{total}</div>
                    <div style={{ fontSize: 11, opacity: 0.5, textTransform: 'uppercase', letterSpacing: 1 }}>Total Cameras</div>
                </div>
                <div className="stagger-card dash-card" style={{ background: 'rgba(34,197,94,0.08)', borderRadius: 10, padding: 16, textAlign: 'center', border: '1px solid rgba(34,197,94,0.15)' }}>
                    <div className="stat-number" style={{ fontSize: 28, fontWeight: 700, color: '#22c55e' }}>{online}</div>
                    <div style={{ fontSize: 11, opacity: 0.5, textTransform: 'uppercase', letterSpacing: 1 }}>Online</div>
                </div>
                <div className="stagger-card dash-card" style={{ background: 'rgba(59,130,246,0.08)', borderRadius: 10, padding: 16, textAlign: 'center', border: '1px solid rgba(59,130,246,0.15)' }}>
                    <div className="stat-number" style={{ fontSize: 28, fontWeight: 700, color: '#3b82f6' }}>{recording}</div>
                    <div style={{ fontSize: 11, opacity: 0.5, textTransform: 'uppercase', letterSpacing: 1 }}>Recording</div>
                </div>
                <div className="stagger-card dash-card" style={{ background: offline > 0 ? 'rgba(239,68,68,0.08)' : 'rgba(255,255,255,0.04)', borderRadius: 10, padding: 16, textAlign: 'center', border: `1px solid ${offline > 0 ? 'rgba(239,68,68,0.15)' : 'rgba(255,255,255,0.06)'}` }}>
                    <div className="stat-number" style={{ fontSize: 28, fontWeight: 700, color: offline > 0 ? '#ef4444' : 'inherit' }}>{offline}</div>
                    <div style={{ fontSize: 11, opacity: 0.5, textTransform: 'uppercase', letterSpacing: 1 }}>Offline</div>
                </div>
            </div>

            {/* Filters */}
            <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
                <input
                    type="text"
                    className="settings-input"
                    placeholder="Search cameras..."
                    value={filter}
                    onChange={e => setFilter(e.target.value)}
                    style={{ flex: 1 }}
                />
                <select
                    className="settings-input"
                    value={groupFilter}
                    onChange={e => setGroupFilter(e.target.value)}
                    style={{ width: 180 }}
                >
                    <option value="">All Groups</option>
                    {groups.map(g => <option key={g} value={g}>{g}</option>)}
                </select>
            </div>

            {/* Camera health cards */}
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 12 }}>
                {filtered.map((cam, idx) => (
                    <div key={cam.id} className="stagger-card dash-card" style={{
                        background: 'rgba(255,255,255,0.03)',
                        borderRadius: 10,
                        padding: 16,
                        border: `1px solid ${cam.status === 'online' ? 'rgba(34,197,94,0.15)' : 'rgba(239,68,68,0.15)'}`,
                        animationDelay: `${0.05 + idx * 0.04}s`,
                    }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                            <div>
                                <div style={{ fontWeight: 600, fontSize: 14 }}>{cam.name}</div>
                                <div style={{ fontSize: 11, opacity: 0.5 }}>{cam.manufacturer} {cam.model}</div>
                            </div>
                            <div style={{
                                display: 'flex', alignItems: 'center', gap: 6,
                                background: `${STATUS_COLORS[cam.status] || STATUS_COLORS.unknown}20`,
                                padding: '3px 10px', borderRadius: 12, fontSize: 11, fontWeight: 600,
                                color: STATUS_COLORS[cam.status] || STATUS_COLORS.unknown,
                            }}>
                                <span style={{
                                    width: 7, height: 7, borderRadius: '50%',
                                    background: STATUS_COLORS[cam.status] || STATUS_COLORS.unknown,
                                    display: 'inline-block',
                                    boxShadow: cam.status === 'online' ? `0 0 6px ${STATUS_COLORS[cam.status]}` : 'none',
                                    animation: cam.status === 'online' ? 'statusPing 2s infinite' : 'none',
                                }} />
                                {cam.status.toUpperCase()}
                            </div>
                        </div>

                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, fontSize: 12 }}>
                            <div style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 6, padding: '8px 10px' }}>
                                <div style={{ opacity: 0.5, fontSize: 10, marginBottom: 2 }}>Recording</div>
                                <div style={{ color: cam.recording ? '#22c55e' : '#ef4444', fontWeight: 600 }}>
                                    {cam.recording ? `● ${cam.recording_mode.toUpperCase()}` : '○ OFF'}
                                </div>
                            </div>
                            <div style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 6, padding: '8px 10px' }}>
                                <div style={{ opacity: 0.5, fontSize: 10, marginBottom: 2 }}>Events</div>
                                <div style={{ color: cam.events_enabled ? '#22c55e' : '#6b7280', fontWeight: 600 }}>
                                    {cam.events_enabled ? '● ENABLED' : '○ DISABLED'}
                                </div>
                            </div>
                            <div style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 6, padding: '8px 10px' }}>
                                <div style={{ opacity: 0.5, fontSize: 10, marginBottom: 2 }}>Group</div>
                                <div style={{ fontWeight: 600 }}>{cam.camera_group}</div>
                            </div>
                            <div style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 6, padding: '8px 10px' }}>
                                <div style={{ opacity: 0.5, fontSize: 10, marginBottom: 2 }}>Mode</div>
                                <div style={{ fontWeight: 600 }}>{cam.recording_mode || 'continuous'}</div>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {filtered.length === 0 && (
                <div style={{ textAlign: 'center', padding: 40, opacity: 0.5 }}>
                    No cameras match the current filter
                </div>
            )}
        </div>
    );
}
