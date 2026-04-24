'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import { Camera, Event, queryEvents } from '@/lib/api';

interface EventListPanelProps {
    cameras: Camera[];
    open: boolean;
    onClose: () => void;
    onEventClick: (event: Event) => void;
    // Live events pushed from WebSocket by the parent
    liveEvents: Event[];
}

const EVENT_ICONS: Record<string, string> = {
    motion: '🏃',
    lpr: '🚗',
    object: '📦',
    face: '👤',
    intrusion: '🚨',
    linecross: '⛔',
    tamper: '⚠️',
    videoloss: '📵',
    other: '📋',
};

const EVENT_TYPES = ['motion', 'lpr', 'object', 'face', 'intrusion', 'linecross', 'tamper', 'videoloss'];

const EVENT_COLORS: Record<string, string> = {
    motion: '#f59e0b',
    lpr: '#3b82f6',
    object: '#8b5cf6',
    face: '#ec4899',
    intrusion: '#ef4444',
    linecross: '#ef4444',
    tamper: '#f97316',
    videoloss: '#6b7280',
    other: '#6b7280',
};

function isoLocal(date: Date): string {
    // Returns datetime-local string in local time
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export default function EventListPanel({ cameras, open, onClose, onEventClick, liveEvents }: EventListPanelProps) {
    const [isMounted, setIsMounted] = useState(false);
    useEffect(() => { setIsMounted(true); }, []);

    // --- Filter State ---
    const [activeTypes, setActiveTypes] = useState<Set<string>>(new Set(EVENT_TYPES));
    const [selectedCamera, setSelectedCamera] = useState<string>('');
    const [search, setSearch] = useState('');
    const [dateFrom, setDateFrom] = useState(() => isoLocal(new Date(Date.now() - 24 * 3600 * 1000)));
    const [dateTo, setDateTo] = useState(() => isoLocal(new Date()));
    const [useCustomRange, setUseCustomRange] = useState(false);

    // --- Remote events from API ---
    const [remoteEvents, setRemoteEvents] = useState<Event[]>([]);
    const [loading, setLoading] = useState(false);
    const [expandedId, setExpandedId] = useState<number | null>(null);

    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const fetchEvents = useCallback(async () => {
        setLoading(true);
        try {
            const end = useCustomRange ? new Date(dateTo) : new Date();
            const start = useCustomRange
                ? new Date(dateFrom)
                : new Date(Date.now() - 24 * 3600 * 1000);

            const types = Array.from(activeTypes).join(',');
            const data = await queryEvents({
                start: start.toISOString(),
                end: end.toISOString(),
                camera_id: selectedCamera || undefined,
                types: types || undefined,
                search: search || undefined,
                limit: 200,
            });
            setRemoteEvents(data ?? []);
        } catch {
            setRemoteEvents([]);
        } finally {
            setLoading(false);
        }
    }, [activeTypes, selectedCamera, search, dateFrom, dateTo, useCustomRange]);

    // Debounced re-fetch on filter changes
    useEffect(() => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(fetchEvents, 300);
        return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
    }, [fetchEvents]);

    // Client-side event filter predicate (applied to both live and remote)
    const matchesFilters = (e: Event) =>
        activeTypes.has(e.event_type) &&
        (!selectedCamera || e.camera_id === selectedCamera) &&
        (!search || JSON.stringify(e).toLowerCase().includes(search.toLowerCase()));

    const displayedEvents = (() => {
        const filtered = useCustomRange
            ? remoteEvents.filter(matchesFilters)
            : [
                ...liveEvents.filter(matchesFilters),
                ...remoteEvents.filter(matchesFilters),
            ];
        // Deduplicate by id — live events come first and take priority
        const seen = new Set<number>();
        const deduped: Event[] = [];
        for (const e of filtered) {
            if (e.id && seen.has(e.id)) continue;
            if (e.id) seen.add(e.id);
            deduped.push(e);
        }
        return deduped.slice(0, 200);
    })();

    const toggleType = (type: string) => {
        setActiveTypes(prev => {
            const next = new Set(prev);
            next.has(type) ? next.delete(type) : next.add(type);
            return next;
        });
    };

    const formatTime = (t: string) => {
        if (!isMounted) return '';
        const d = new Date(t);
        return d.toLocaleString([], {
            month: 'short', day: 'numeric',
            hour: '2-digit', minute: '2-digit', second: '2-digit',
        });
    };

    const getCameraName = (id: string) => cameras.find(c => c.id === id)?.name ?? id.slice(0, 8);

    const getEventDetail = (event: Event): string => {
        const d = event.details || {};
        switch (event.event_type) {
            case 'lpr': return d.plate_number ? `Plate: ${d.plate_number}` : 'License plate detected';
            case 'motion': return d.ismotion === 'true' ? 'Motion started' : 'Motion detected';
            case 'object': return d.objecttype ? `${d.objecttype} detected` : 'Object detected';
            case 'face': return 'Face detected';
            case 'intrusion': return 'Intrusion detected';
            case 'linecross': return 'Line crossed';
            case 'tamper': return 'Camera tamper detected';
            default: return event.event_type;
        }
    };

    const color = (type: string) => EVENT_COLORS[type] ?? EVENT_COLORS.other;

    return (
        <div className={`event-list-panel ${open ? 'open' : ''}`} style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>

            {/* ── Header ── */}
            <div className="event-list-header">
                <h3>Events {loading ? '…' : `(${displayedEvents.length})`}</h3>
                <button className="btn btn-sm" onClick={onClose}>✕</button>
            </div>

            {/* ── Filter Controls ── */}
            <div style={{ padding: '8px 10px', borderBottom: '1px solid var(--border)', display: 'flex', flexDirection: 'column', gap: 6 }}>

                {/* Event type chips */}
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                    {EVENT_TYPES.map(type => (
                        <button
                            key={type}
                            onClick={() => toggleType(type)}
                            title={type}
                            style={{
                                background: activeTypes.has(type) ? color(type) : 'transparent',
                                border: `1px solid ${color(type)}`,
                                color: activeTypes.has(type) ? '#fff' : color(type),
                                borderRadius: 4,
                                padding: '2px 7px',
                                fontSize: 11,
                                cursor: 'pointer',
                                fontWeight: 600,
                                transition: 'all 0.15s',
                            }}
                        >
                            {EVENT_ICONS[type]} {type}
                        </button>
                    ))}
                </div>

                {/* Camera selector + Search */}
                <div style={{ display: 'flex', gap: 6 }}>
                    <select
                        value={selectedCamera}
                        onChange={e => setSelectedCamera(e.target.value)}
                        style={{ flex: 1, background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 4, padding: '3px 6px', fontSize: 12 }}
                    >
                        <option value="">📷 All cameras</option>
                        {cameras.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
                    </select>
                    <input
                        type="search"
                        placeholder="Search…"
                        value={search}
                        onChange={e => setSearch(e.target.value)}
                        style={{ flex: 1, background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 4, padding: '3px 7px', fontSize: 12 }}
                    />
                </div>

                {/* Date range toggle */}
                <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 11, color: 'var(--text-secondary)', cursor: 'pointer' }}>
                        <input
                            type="checkbox"
                            checked={useCustomRange}
                            onChange={e => setUseCustomRange(e.target.checked)}
                            style={{ accentColor: 'var(--accent-blue)' }}
                        />
                        Custom range
                    </label>
                    {useCustomRange && (
                        <div style={{ display: 'flex', gap: 4, flex: 1, flexDirection: 'column' }}>
                            <input type="datetime-local" value={dateFrom} onChange={e => setDateFrom(e.target.value)}
                                style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 4, padding: '2px 6px', fontSize: 11, width: '100%' }} />
                            <input type="datetime-local" value={dateTo} onChange={e => setDateTo(e.target.value)}
                                style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 4, padding: '2px 6px', fontSize: 11, width: '100%' }} />
                        </div>
                    )}
                    {!useCustomRange && (
                        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>Last 24 hours + live</span>
                    )}
                </div>
            </div>

            {/* ── Event List ── */}
            <div className="event-list" style={{ flex: 1, overflowY: 'auto' }}>
                {displayedEvents.length === 0 ? (
                    <div className="empty-state" style={{ padding: 40 }}>
                        <div className="empty-state-icon">📋</div>
                        <div className="empty-state-title">No Events</div>
                        <div className="empty-state-desc">
                            {loading ? 'Loading…' : 'No events match the current filters.'}
                        </div>
                    </div>
                ) : (
                    displayedEvents.map((event, i) => {
                        const isExpanded = expandedId === event.id;
                        const hasThumbnail = !!event.thumbnail;

                        return (
                            <div
                                key={event.id ?? i}
                                className="event-item"
                                style={{ cursor: 'pointer', borderLeft: `3px solid ${color(event.event_type)}` }}
                                onClick={() => {
                                    setExpandedId(isExpanded ? null : event.id);
                                    onEventClick(event);
                                }}
                            >
                                {/* Thumbnail row */}
                                {hasThumbnail && (
                                    <div style={{ width: '100%', borderRadius: 4, overflow: 'hidden', marginBottom: 4 }}>
                                        <img
                                            src={event.thumbnail.startsWith('data:') ? event.thumbnail : `data:image/jpeg;base64,${event.thumbnail}`}
                                            alt="event snapshot"
                                            style={{ width: '100%', maxHeight: 90, objectFit: 'cover', display: 'block' }}
                                            loading="lazy"
                                        />
                                    </div>
                                )}

                                {/* Icon + details row */}
                                <div style={{ display: 'flex', gap: 8, alignItems: 'flex-start' }}>
                                    <div className={`event-type-icon ${event.event_type}`} style={{ color: color(event.event_type), flexShrink: 0 }}>
                                        {EVENT_ICONS[event.event_type] || EVENT_ICONS.other}
                                    </div>
                                    <div className="event-info" style={{ flex: 1, minWidth: 0 }}>
                                        <div className="event-info-header">
                                            <span className="event-info-type" style={{ color: color(event.event_type) }}>
                                                {event.event_type}
                                            </span>
                                            <span className="event-info-time">
                                                {formatTime(event.event_time)}
                                            </span>
                                        </div>
                                        <div className="event-info-detail">{getEventDetail(event)}</div>
                                        <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 2 }}>
                                            📷 {getCameraName(event.camera_id)}
                                        </div>
                                    </div>

                                    {/* Seek button */}
                                    <button
                                        title="Seek to this moment"
                                        onClick={e => { e.stopPropagation(); onEventClick(event); }}
                                        style={{
                                            background: 'transparent',
                                            border: 'none',
                                            color: 'var(--text-muted)',
                                            cursor: 'pointer',
                                            fontSize: 16,
                                            padding: '0 2px',
                                            flexShrink: 0,
                                            lineHeight: 1,
                                        }}
                                    >
                                        ⏩
                                    </button>
                                </div>
                            </div>
                        );
                    })
                )}
            </div>
        </div>
    );
}
