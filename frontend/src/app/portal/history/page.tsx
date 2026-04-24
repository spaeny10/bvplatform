'use client';

import type { CSSProperties, ReactNode } from 'react';
import { useEffect, useMemo, useState, useCallback } from 'react';
import Link from 'next/link';
import {
    searchEvents,
    exportEvidenceURL,
    listCameras,
    searchSemantic,
    HistoricalEvent,
    Camera,
    SemanticMatch,
} from '@/lib/api';
import Logo from '@/components/shared/Logo';
import UserChip from '@/components/shared/UserChip';

// Event types the Milesight WS driver emits. Kept in sync with the alarmTypes
// map in cmd/server/main.go so customers can filter on the same buckets SOC
// operators use.
const EVENT_TYPES = [
    'intrusion',
    'linecross',
    'human',
    'vehicle',
    'face',
    'loitering',
    'lpr',
    'object',
    'motion',
];

// Severity heuristic mirroring the server-side alarm pipeline — keeps the
// color scheme consistent with the SOC queue. When AI enrichment is added to
// the events table we can replace this with the stored verdict.
function eventSeverity(type: string): { color: string; label: string } {
    switch (type) {
        case 'intrusion':
        case 'human':
        case 'face':
            return { color: '#ef4444', label: 'HIGH' };
        case 'vehicle':
        case 'linecross':
        case 'loitering':
        case 'lpr':
            return { color: '#f59e0b', label: 'MED' };
        default:
            return { color: '#6b7280', label: 'INFO' };
    }
}

// formatLocalTime renders an RFC3339 timestamp as "Apr 22, 9:30:35 PM".
function formatLocalTime(iso: string): string {
    try {
        const d = new Date(iso);
        return d.toLocaleString(undefined, {
            month: 'short',
            day: 'numeric',
            hour: 'numeric',
            minute: '2-digit',
            second: '2-digit',
        });
    } catch {
        return iso;
    }
}

// Defaults to "last 24 hours" to match the recording-health window.
function defaultRange(): { start: string; end: string } {
    const end = new Date();
    const start = new Date(end.getTime() - 24 * 60 * 60 * 1000);
    return { start: start.toISOString(), end: end.toISOString() };
}

/**
 * PortalHistoryPage is the customer-facing NVR history view. Leverages the
 * server-side RBAC + unified search endpoint to show a single list of every
 * event on the caller's assigned cameras, each playable in-place via the
 * resolved playback_url. One-click evidence export per row.
 */
export default function PortalHistoryPage() {
    const [events, setEvents] = useState<HistoricalEvent[]>([]);
    const [cameras, setCameras] = useState<Camera[]>([]);
    const [authorizedCameraIDs, setAuthorizedCameraIDs] = useState<string[] | null>(null);
    const [selectedCamera, setSelectedCamera] = useState<string>('');
    const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
    const [search, setSearch] = useState<string>('');
    const [range, setRange] = useState(defaultRange());
    const [loading, setLoading] = useState(false);
    const [hasMore, setHasMore] = useState(false);
    const [activeEvent, setActiveEvent] = useState<HistoricalEvent | null>(null);

    // Semantic-search state. semanticQuery is the box text; semanticResults
    // replaces the event list when populated. Clearing the box returns to
    // event-triggered mode.
    const [semanticQuery, setSemanticQuery] = useState<string>('');
    const [semanticResults, setSemanticResults] = useState<SemanticMatch[] | null>(null);
    const [semanticLoading, setSemanticLoading] = useState(false);
    const [activeSegment, setActiveSegment] = useState<SemanticMatch | null>(null);

    // Initial camera list is used to pretty-print camera names in the dropdown
    // and in each event row. The server will only return cameras the caller
    // can see via listCameras, so this doubles as an allowlist for the UI.
    useEffect(() => {
        listCameras().then(setCameras);
    }, []);

    const loadEvents = useCallback(async () => {
        setLoading(true);
        try {
            const resp = await searchEvents({
                start: range.start,
                end: range.end,
                camera_id: selectedCamera || undefined,
                types: selectedTypes.length ? selectedTypes : undefined,
                search: search || undefined,
                limit: 100,
            });
            setEvents(resp.events);
            setHasMore(resp.has_more);
            if (resp.authorized_cameras) {
                setAuthorizedCameraIDs(resp.authorized_cameras);
            } else {
                setAuthorizedCameraIDs(null);
            }
        } finally {
            setLoading(false);
        }
    }, [range, selectedCamera, selectedTypes, search]);

    useEffect(() => {
        loadEvents();
    }, [loadEvents]);

    // If the server told us the caller has a restricted camera whitelist,
    // trim the dropdown to just those. Otherwise show every camera they can
    // listCameras() — admins see them all.
    const cameraOptions = useMemo(() => {
        if (authorizedCameraIDs === null) return cameras;
        const set = new Set(authorizedCameraIDs);
        return cameras.filter(c => set.has(c.id));
    }, [cameras, authorizedCameraIDs]);

    const cameraName = useCallback(
        (id: string) => cameras.find(c => c.id === id)?.name ?? id.slice(0, 8),
        [cameras]
    );

    // ── Render ──
    return (
        <div style={{ minHeight: '100vh', background: '#0b0f17', color: '#e5e7eb' }}>
            {/* Header */}
            <div style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                padding: '16px 32px',
                borderBottom: '1px solid #1f2937',
            }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
                    <Logo />
                    <div>
                        <div style={{ fontSize: 18, fontWeight: 600 }}>History</div>
                        <div style={{ fontSize: 12, color: '#6b7280' }}>
                            Every recorded event on your assigned cameras — playable and exportable.
                        </div>
                    </div>
                </div>
                <div style={{ display: 'flex', gap: 16, alignItems: 'center' }}>
                    <Link href="/portal" style={{ color: '#60a5fa', fontSize: 13 }}>← Back to Overview</Link>
                    <UserChip />
                </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: (activeEvent || activeSegment) ? '1fr 640px' : '1fr', gap: 0 }}>
                {/* LEFT: filters + list */}
                <div style={{ padding: 24 }}>
                    {/* Semantic search — AI-powered free-text over VLM descriptions */}
                    <div style={{
                        background: '#111827',
                        padding: 16,
                        marginBottom: 16,
                        borderRadius: 8,
                        border: '1px solid #1f2937',
                    }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                            <span style={{ fontSize: 11, color: '#a78bfa', letterSpacing: 1, fontWeight: 600 }}>✨ AI SEARCH</span>
                            <span style={{ fontSize: 11, color: '#6b7280' }}>
                                Describe what you&apos;re looking for in plain English
                            </span>
                        </div>
                        <form
                            onSubmit={async (e) => {
                                e.preventDefault();
                                const q = semanticQuery.trim();
                                if (!q) {
                                    setSemanticResults(null);
                                    return;
                                }
                                setSemanticLoading(true);
                                try {
                                    const resp = await searchSemantic({
                                        q,
                                        start: range.start,
                                        end: range.end,
                                        camera_id: selectedCamera || undefined,
                                        limit: 50,
                                    });
                                    setSemanticResults(resp.results);
                                    setActiveEvent(null);
                                } finally {
                                    setSemanticLoading(false);
                                }
                            }}
                            style={{ display: 'flex', gap: 8 }}
                        >
                            <input
                                type="text"
                                value={semanticQuery}
                                onChange={(e) => setSemanticQuery(e.target.value)}
                                placeholder='e.g. "person in red jacket carrying a ladder" or "delivery truck at night"'
                                style={{ ...inputStyle, flex: 1, fontSize: 14 }}
                            />
                            <button
                                type="submit"
                                disabled={semanticLoading || !semanticQuery.trim()}
                                style={{
                                    ...buttonStyle('#7c3aed'),
                                    opacity: (semanticLoading || !semanticQuery.trim()) ? 0.5 : 1,
                                    cursor: (semanticLoading || !semanticQuery.trim()) ? 'not-allowed' : 'pointer',
                                    border: 'none',
                                }}
                            >
                                {semanticLoading ? 'Searching…' : 'Search'}
                            </button>
                            {semanticResults && (
                                <button
                                    type="button"
                                    onClick={() => {
                                        setSemanticQuery('');
                                        setSemanticResults(null);
                                        setActiveSegment(null);
                                    }}
                                    style={{ ...buttonStyle('#374151'), border: 'none' }}
                                >
                                    Clear
                                </button>
                            )}
                        </form>
                        {semanticResults && (
                            <div style={{ marginTop: 8, fontSize: 11, color: '#6b7280' }}>
                                {semanticResults.length} match{semanticResults.length === 1 ? '' : 'es'} · date range + camera filter applied · ranked by relevance
                            </div>
                        )}
                    </div>

                    {/* Filters */}
                    <div style={{
                        display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 16,
                        background: '#111827', padding: 12, borderRadius: 8,
                        border: '1px solid #1f2937',
                    }}>
                        <FilterField label="From">
                            <input type="datetime-local" value={toLocalInput(range.start)}
                                onChange={e => setRange(r => ({ ...r, start: fromLocalInput(e.target.value) }))}
                                style={inputStyle} />
                        </FilterField>
                        <FilterField label="To">
                            <input type="datetime-local" value={toLocalInput(range.end)}
                                onChange={e => setRange(r => ({ ...r, end: fromLocalInput(e.target.value) }))}
                                style={inputStyle} />
                        </FilterField>
                        <FilterField label="Camera">
                            <select value={selectedCamera}
                                onChange={e => setSelectedCamera(e.target.value)}
                                style={inputStyle}>
                                <option value="">All cameras</option>
                                {cameraOptions.map(c => (
                                    <option key={c.id} value={c.id}>{c.name}</option>
                                ))}
                            </select>
                        </FilterField>
                        <FilterField label="Event types">
                            <select multiple value={selectedTypes}
                                onChange={e => setSelectedTypes(Array.from(e.target.selectedOptions, o => o.value))}
                                style={{ ...inputStyle, minWidth: 160, height: 72 }}>
                                {EVENT_TYPES.map(t => (
                                    <option key={t} value={t}>{t}</option>
                                ))}
                            </select>
                        </FilterField>
                        <FilterField label="Search (plate, etc.)">
                            <input type="text" value={search} onChange={e => setSearch(e.target.value)}
                                placeholder="Optional"
                                style={inputStyle} />
                        </FilterField>
                    </div>

                    {/* Semantic results list — when populated, replaces the event list */}
                    {semanticResults && (
                        <div style={{ marginBottom: 16 }}>
                            {semanticResults.length === 0 && (
                                <div style={{ padding: 48, textAlign: 'center', color: '#6b7280', fontSize: 14 }}>
                                    No segments matched &quot;{semanticQuery}&quot; in this window. Try a broader query or widen the date range.
                                </div>
                            )}
                            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                                {semanticResults.map(m => {
                                    const active = activeSegment?.segment_id === m.segment_id;
                                    return (
                                        <div key={m.segment_id}
                                            onClick={() => { setActiveSegment(m); setActiveEvent(null); }}
                                            style={{
                                                display: 'grid',
                                                gridTemplateColumns: '60px 1fr auto',
                                                gap: 12,
                                                alignItems: 'center',
                                                padding: 10,
                                                background: active ? '#1f2937' : '#111827',
                                                border: `1px solid ${active ? '#a78bfa' : '#1f2937'}`,
                                                borderRadius: 6,
                                                cursor: 'pointer',
                                            }}>
                                            <div style={{ fontSize: 11, color: '#a78bfa', fontWeight: 600 }}>
                                                {(m.rank * 100).toFixed(0)}%
                                            </div>
                                            <div>
                                                <div style={{ fontSize: 13, color: '#e5e7eb', marginBottom: 2 }}>
                                                    {m.description}
                                                </div>
                                                <div style={{ fontSize: 11, color: '#9ca3af' }}>
                                                    {m.camera_name} · {formatLocalTime(m.start_time)}
                                                    {m.tags && m.tags.length > 0 && (
                                                        <span style={{ marginLeft: 8 }}>
                                                            {m.tags.slice(0, 6).map(t => (
                                                                <span key={t} style={{
                                                                    display: 'inline-block',
                                                                    padding: '1px 6px',
                                                                    marginRight: 4,
                                                                    borderRadius: 3,
                                                                    background: '#1f2937',
                                                                    color: '#9ca3af',
                                                                    fontSize: 10,
                                                                }}>{t}</span>
                                                            ))}
                                                        </span>
                                                    )}
                                                </div>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    )}

                    {/* Event list — shown only when not in semantic-search mode */}
                    {!semanticResults && (<>

                    <div style={{ fontSize: 12, color: '#6b7280', marginBottom: 6 }}>
                        {loading ? 'Loading…' : `${events.length} event${events.length === 1 ? '' : 's'}${hasMore ? ' (more available — narrow range)' : ''}`}
                    </div>

                    {events.length === 0 && !loading && (
                        <div style={{ padding: 48, textAlign: 'center', color: '#6b7280', fontSize: 14 }}>
                            No events in the selected range. Try widening the date filter.
                        </div>
                    )}

                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                        {events.map(ev => {
                            const sev = eventSeverity(ev.event_type);
                            const active = activeEvent?.id === ev.id;
                            return (
                                <div key={ev.id}
                                    onClick={() => setActiveEvent(ev)}
                                    style={{
                                        display: 'grid',
                                        gridTemplateColumns: '80px 1fr auto',
                                        gap: 12,
                                        alignItems: 'center',
                                        padding: 10,
                                        background: active ? '#1f2937' : '#111827',
                                        border: `1px solid ${active ? '#60a5fa' : '#1f2937'}`,
                                        borderRadius: 6,
                                        cursor: 'pointer',
                                    }}>
                                    <div>
                                        <span style={{
                                            display: 'inline-block',
                                            padding: '2px 8px',
                                            borderRadius: 4,
                                            fontSize: 10,
                                            fontWeight: 700,
                                            background: sev.color + '22',
                                            color: sev.color,
                                        }}>{sev.label}</span>
                                    </div>
                                    <div>
                                        <div style={{ fontSize: 13, fontWeight: 500 }}>
                                            {ev.event_type} — {cameraName(ev.camera_id)}
                                        </div>
                                        <div style={{ fontSize: 11, color: '#9ca3af' }}>
                                            {formatLocalTime(ev.event_time)}
                                            {ev.playback_url ? '' : '  · no clip available'}
                                        </div>
                                    </div>
                                    <div style={{ display: 'flex', gap: 8 }} onClick={e => e.stopPropagation()}>
                                        {ev.playback_url && (
                                            <a href={exportEvidenceURL(ev.id)}
                                                style={buttonStyle('#2563eb')}
                                                title="Download .zip evidence package">
                                                ⬇ Export
                                            </a>
                                        )}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                    </>)}
                </div>

                {/* RIGHT: inline player — segment mode (semantic hit) */}
                {activeSegment && !activeEvent && (
                    <div style={{
                        borderLeft: '1px solid #1f2937',
                        padding: 16,
                        background: '#0b0f17',
                        position: 'sticky', top: 0, alignSelf: 'start',
                    }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                            <div>
                                <div style={{ fontSize: 14, fontWeight: 600 }}>
                                    {activeSegment.camera_name}
                                </div>
                                <div style={{ fontSize: 11, color: '#9ca3af' }}>
                                    {formatLocalTime(activeSegment.start_time)} · {activeSegment.activity_level} activity · match {(activeSegment.rank * 100).toFixed(0)}%
                                </div>
                            </div>
                            <button onClick={() => setActiveSegment(null)}
                                style={{ background: 'none', color: '#9ca3af', border: '1px solid #1f2937', padding: '4px 8px', borderRadius: 4, cursor: 'pointer' }}>
                                ✕
                            </button>
                        </div>
                        <video key={activeSegment.segment_id} src={activeSegment.playback_url}
                            controls autoPlay
                            style={{ width: '100%', background: '#000', borderRadius: 4 }} />
                        <div style={{ marginTop: 12, fontSize: 13, color: '#d1d5db', lineHeight: 1.5 }}>
                            {activeSegment.description}
                        </div>
                        {activeSegment.tags && activeSegment.tags.length > 0 && (
                            <div style={{ marginTop: 8, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                                {activeSegment.tags.map(t => (
                                    <span key={t} style={{
                                        padding: '2px 8px',
                                        borderRadius: 3,
                                        background: '#1f2937',
                                        color: '#9ca3af',
                                        fontSize: 11,
                                    }}>{t}</span>
                                ))}
                            </div>
                        )}
                    </div>
                )}

                {/* RIGHT: inline player — event mode (standard historical event) */}
                {activeEvent && (
                    <div style={{
                        borderLeft: '1px solid #1f2937',
                        padding: 16,
                        background: '#0b0f17',
                        position: 'sticky', top: 0, alignSelf: 'start',
                    }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                            <div>
                                <div style={{ fontSize: 14, fontWeight: 600 }}>
                                    {activeEvent.event_type} · {cameraName(activeEvent.camera_id)}
                                </div>
                                <div style={{ fontSize: 11, color: '#9ca3af' }}>
                                    {formatLocalTime(activeEvent.event_time)}
                                </div>
                            </div>
                            <button onClick={() => setActiveEvent(null)}
                                style={{ background: 'none', color: '#9ca3af', border: '1px solid #1f2937', padding: '4px 8px', borderRadius: 4, cursor: 'pointer' }}>
                                ✕
                            </button>
                        </div>
                        {activeEvent.playback_url ? (
                            <video key={activeEvent.id} src={activeEvent.playback_url}
                                controls autoPlay
                                style={{ width: '100%', background: '#000', borderRadius: 4 }} />
                        ) : (
                            <div style={{ padding: 32, textAlign: 'center', color: '#6b7280' }}>
                                No recording covered this moment.
                            </div>
                        )}
                        <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
                            <a href={exportEvidenceURL(activeEvent.id)}
                                style={buttonStyle('#2563eb')}>
                                ⬇ Download evidence package (.zip)
                            </a>
                        </div>
                        {activeEvent.details && Object.keys(activeEvent.details).length > 0 && (
                            <details style={{ marginTop: 12 }}>
                                <summary style={{ cursor: 'pointer', fontSize: 12, color: '#9ca3af' }}>Raw event details</summary>
                                <pre style={{ fontSize: 11, color: '#d1d5db', background: '#0b0f17', padding: 8, borderRadius: 4, overflow: 'auto' }}>
                                    {JSON.stringify(activeEvent.details, null, 2)}
                                </pre>
                            </details>
                        )}
                    </div>
                )}
            </div>
        </div>
    );
}

// ── small layout helpers ──

function FilterField({ label, children }: { label: string; children: ReactNode }) {
    return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <label style={{ fontSize: 10, color: '#6b7280', letterSpacing: 0.5 }}>{label.toUpperCase()}</label>
            {children}
        </div>
    );
}

const inputStyle: CSSProperties = {
    background: '#0b0f17',
    color: '#e5e7eb',
    border: '1px solid #1f2937',
    padding: '6px 8px',
    borderRadius: 4,
    fontSize: 13,
};

function buttonStyle(color: string): CSSProperties {
    return {
        display: 'inline-block',
        padding: '6px 12px',
        background: color,
        color: '#fff',
        textDecoration: 'none',
        borderRadius: 4,
        fontSize: 12,
        fontWeight: 500,
    };
}

// datetime-local inputs need "YYYY-MM-DDTHH:MM" (local, no timezone), while
// we pass ISO strings around. These two helpers bridge the formats.
function toLocalInput(iso: string): string {
    try {
        const d = new Date(iso);
        const pad = (n: number) => String(n).padStart(2, '0');
        return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
    } catch {
        return '';
    }
}

function fromLocalInput(local: string): string {
    try {
        return new Date(local).toISOString();
    } catch {
        return local;
    }
}
