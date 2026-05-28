'use client';

// Extracted from SettingsPage.tsx (P1-B-11 session 3). The Event Log
// tab: real-time camera event feed with time-window/camera/type filters
// and a 10 s auto-refresh tick.

import { useCallback, useEffect, useState } from 'react';
import { queryEvents, type Camera, type Event as CameraEvent } from '@/lib/api';

const EVENT_COLORS: Record<string, string> = {
    intrusion: '#EF4444', human: '#EF4444', face: '#EF4444',
    vehicle: '#F97316', linecross: '#F97316', loitering: '#F97316', lpr: '#F97316',
    motion: '#3B82F6', object: '#3B82F6', peoplecount: '#3B82F6',
    tamper: '#E89B2A', videoloss: '#E89B2A',
};

export default function EventLogTab({ cameras }: { cameras: Camera[] }) {
    const [events, setEvents] = useState<CameraEvent[]>([]);
    const [loading, setLoading] = useState(false);
    const [cameraFilter, setCameraFilter] = useState('all');
    const [typeFilter, setTypeFilter] = useState('all');
    const [hoursBack, setHoursBack] = useState(1);
    const [autoRefresh, setAutoRefresh] = useState(true);

    const loadEvents = useCallback(async () => {
        setLoading(true);
        const end = new Date();
        const start = new Date(end.getTime() - hoursBack * 3600000);
        const params: any = {
            start: start.toISOString(),
            end: end.toISOString(),
            limit: 200,
        };
        if (cameraFilter !== 'all') params.camera_id = cameraFilter;
        if (typeFilter !== 'all') params.types = typeFilter;
        const data = await queryEvents(params);
        setEvents(data);
        setLoading(false);
    }, [cameraFilter, typeFilter, hoursBack]);

    useEffect(() => { loadEvents(); }, [loadEvents]);

    // Auto-refresh every 10s
    useEffect(() => {
        if (!autoRefresh) return;
        const timer = setInterval(loadEvents, 10000);
        return () => clearInterval(timer);
    }, [autoRefresh, loadEvents]);

    // Collect unique event types from results
    const eventTypes = Array.from(new Set(events.map(e => e.event_type))).sort();

    // Camera name lookup
    const camNames: Record<string, string> = {};
    for (const c of cameras) camNames[c.id] = c.name;

    return (
        <div className="settings-section" role="tabpanel">
            <div className="settings-section-title">Camera Event Log</div>
            <p className="settings-section-desc" style={{ marginBottom: 12 }}>
                Real-time feed of ONVIF and VCA events from all cameras.
            </p>

            {/* Filters */}
            <div style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center' }}>
                <select
                    className="settings-input"
                    value={cameraFilter}
                    onChange={e => setCameraFilter(e.target.value)}
                    style={{ padding: '6px 10px', fontSize: 11, minWidth: 160 }}
                >
                    <option value="all">All Cameras</option>
                    {cameras.map(c => (
                        <option key={c.id} value={c.id}>{c.name}</option>
                    ))}
                </select>
                <select
                    className="settings-input"
                    value={typeFilter}
                    onChange={e => setTypeFilter(e.target.value)}
                    style={{ padding: '6px 10px', fontSize: 11, minWidth: 130 }}
                >
                    <option value="all">All Types</option>
                    {eventTypes.map(t => (
                        <option key={t} value={t}>{t}</option>
                    ))}
                </select>
                <select
                    className="settings-input"
                    value={String(hoursBack)}
                    onChange={e => setHoursBack(parseInt(e.target.value))}
                    style={{ padding: '6px 10px', fontSize: 11, minWidth: 110 }}
                >
                    <option value="1">Last 1 hour</option>
                    <option value="4">Last 4 hours</option>
                    <option value="12">Last 12 hours</option>
                    <option value="24">Last 24 hours</option>
                    <option value="72">Last 3 days</option>
                </select>
                <button
                    className={`btn btn-sm ${autoRefresh ? 'btn-primary' : ''}`}
                    onClick={() => setAutoRefresh(v => !v)}
                    style={{ fontSize: 10, padding: '5px 10px' }}
                >
                    {autoRefresh ? 'Auto-refresh ON' : 'Auto-refresh OFF'}
                </button>
                <button className="btn btn-sm" onClick={loadEvents} disabled={loading} style={{ fontSize: 10, padding: '5px 10px' }}>
                    {loading ? 'Loading...' : '↻ Refresh'}
                </button>
                <span style={{ marginLeft: 'auto', fontSize: 10, color: '#4A5268' }}>
                    {events.length} event{events.length !== 1 ? 's' : ''}
                </span>
            </div>

            {/* Event table */}
            <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)', borderRadius: 8, overflow: 'hidden' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                        <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
                            {['Time', 'Type', 'Camera', 'Details'].map(h => (
                                <th key={h} style={{
                                    padding: '9px 14px', textAlign: 'left', fontSize: 9, fontWeight: 600,
                                    letterSpacing: 1.2, textTransform: 'uppercase', color: '#4A5268',
                                    background: 'rgba(255,255,255,0.02)',
                                }}>{h}</th>
                            ))}
                        </tr>
                    </thead>
                    <tbody>
                        {events.map(evt => {
                            const color = EVENT_COLORS[evt.event_type] || '#8891A5';
                            const time = new Date(evt.event_time);
                            const topic = (evt.details?.topic as string) || '';
                            const driver = (evt.details?.driver as string) || '';
                            const confidence = evt.details?.confidence as string;
                            const plate = evt.details?.plate_number as string;
                            const direction = evt.details?.direction as string;
                            const targetType = evt.details?.target_type as string;
                            return (
                                <tr key={evt.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.03)' }}>
                                    <td style={{ padding: '8px 14px', fontSize: 11, color: '#8891A5', fontFamily: "'JetBrains Mono', monospace", whiteSpace: 'nowrap' }}>
                                        {time.toLocaleTimeString('en-US', { hour12: false })}<br />
                                        <span style={{ fontSize: 9, color: '#4A5268' }}>{time.toLocaleDateString()}</span>
                                    </td>
                                    <td style={{ padding: '8px 14px' }}>
                                        <span style={{
                                            display: 'inline-block', fontSize: 9, fontWeight: 700, padding: '2px 7px',
                                            borderRadius: 3, letterSpacing: 0.3, textTransform: 'uppercase',
                                            background: `${color}15`, color, border: `1px solid ${color}30`,
                                        }}>
                                            {evt.event_type}
                                        </span>
                                        {driver && (
                                            <span style={{ fontSize: 8, color: '#3B82F6', marginLeft: 4 }}>{driver}</span>
                                        )}
                                    </td>
                                    <td style={{ padding: '8px 14px', fontSize: 11, color: '#E4E8F0' }}>
                                        {camNames[evt.camera_id] || evt.camera_id.slice(0, 8)}
                                    </td>
                                    <td style={{ padding: '8px 14px', fontSize: 10, color: '#4A5268' }}>
                                        {confidence && <span style={{ marginRight: 8 }}>Confidence: {confidence}%</span>}
                                        {plate && <span style={{ marginRight: 8, color: '#E89B2A', fontWeight: 600 }}>Plate: {plate}</span>}
                                        {direction && <span style={{ marginRight: 8 }}>Direction: {direction}</span>}
                                        {targetType && <span style={{ marginRight: 8 }}>Target: {targetType}</span>}
                                        {topic && (
                                            <span style={{ fontSize: 9, color: '#2a3848', fontFamily: "'JetBrains Mono', monospace" }}>
                                                {topic.length > 60 ? '...' + topic.slice(-60) : topic}
                                            </span>
                                        )}
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
                {events.length === 0 && !loading && (
                    <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
                        No events in the selected time range
                    </div>
                )}
                {loading && events.length === 0 && (
                    <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
                        Loading events...
                    </div>
                )}
            </div>
        </div>
    );
}
