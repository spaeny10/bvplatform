'use client';

import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { Camera, Event, queryEvents } from '@/lib/api';

interface EventListPanelProps {
    cameras: Camera[];
    open: boolean;
    onClose: () => void;
    onEventClick: (event: Event) => void;
    // Live events pushed from WebSocket by the parent
    liveEvents: Event[];
    // Camera UUIDs in the ACTIVE grid layout (PR #70's CameraGrid bridge).
    // When the "This layout" scope is on, the feed shows only these cameras
    // and groups rows by them. Empty = no layout active.
    activeLayoutCameraIds?: string[];
}

const EVENT_ICONS: Record<string, string> = {
    motion: '🏃',
    lpr: '🚗',
    object: '📦',
    face: '👤',
    intrusion: '🚨',
    linecross: '⛔',
    loitering: '⏱️',
    human: '🚶',
    vehicle: '🚗',
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
    loitering: '#eab308',
    human: '#ef4444',
    vehicle: '#3b82f6',
    tamper: '#f97316',
    videoloss: '#6b7280',
    other: '#6b7280',
};

type SortMode = 'newest' | 'oldest' | 'type' | 'camera' | 'confidence';

const SORT_OPTIONS: { value: SortMode; label: string }[] = [
    { value: 'newest', label: 'Newest' },
    { value: 'oldest', label: 'Oldest' },
    { value: 'type', label: 'By type' },
    { value: 'camera', label: 'By camera' },
    { value: 'confidence', label: 'By confidence' },
];

function isoLocal(date: Date): string {
    // Returns datetime-local string in local time
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// ── Detection-detail extraction ──────────────────────────────────────────
// The events.details JSON carries the camera-side VCA payload, but its shape
// varies by source:
//   • Milesight WebSocket VCA → obj_type, score, rule_name, bounding_boxes
//   • Milesight Sense webhook → obj_type / object / score
//   • ONVIF PullPoint rule engine (the dominant test-DB shape) → topic
//     ("tns1:RuleEngine/HumanDetector/Human"), rule ("MyHumanDetectorRule"),
//     plus per-type flags (ishuman, isleft, isremove). No score/bbox here.
// These helpers normalize across all of them so a feed row always shows the
// best available object class / rule / confidence.

const TYPE_TO_CLASS: Record<string, string> = {
    human: 'Person',
    face: 'Face',
    vehicle: 'Vehicle',
    lpr: 'License Plate',
    intrusion: 'Intrusion',
    linecross: 'Line Cross',
    loitering: 'Loitering',
    object: 'Object',
    motion: 'Motion',
    tamper: 'Tamper',
};

function titleCase(s: string): string {
    return s.replace(/[-_]/g, ' ').replace(/\b\w/g, c => c.toUpperCase()).trim();
}

// classifyFromTopic pulls a human-readable object/event class out of an ONVIF
// topic like "tns1:RuleEngine/HumanDetector/Human" → "Human".
function classifyFromTopic(topic: string): string | null {
    if (!topic) return null;
    const tail = topic.split('/').pop() || '';
    if (!tail || tail.toLowerCase() === 'state') return null;
    return titleCase(tail);
}

function getObjectClass(event: Event): string | null {
    const d = event.details || {};
    const raw =
        d.obj_type || d.object || d.objecttype || d.objectType || d.class || d.label;
    if (typeof raw === 'string' && raw.trim()) return titleCase(raw);
    if (typeof d.topic === 'string') {
        const fromTopic = classifyFromTopic(d.topic);
        if (fromTopic) return fromTopic;
    }
    return TYPE_TO_CLASS[event.event_type] ?? null;
}

function getRuleName(event: Event): string | null {
    const d = event.details || {};
    const raw = d.rule_name || d.rule || d.ruleName;
    if (typeof raw === 'string' && raw.trim()) {
        // ONVIF rules are camelCase tokens like "MyHumanDetectorRule" — strip
        // the leading "My" and trailing "Rule" boilerplate, then spread.
        const cleaned = raw.replace(/^My/, '').replace(/Rule$/, '').replace(/([a-z])([A-Z])/g, '$1 $2').trim();
        return cleaned || raw;
    }
    return null;
}

// getConfidencePct returns a 0-100 integer, or null when no score is present
// (true for the ONVIF rule-engine events in the test set).
function getConfidencePct(event: Event): number | null {
    const d = event.details || {};
    const raw = d.score ?? d.confidence ?? d.ai_score;
    const n = typeof raw === 'number' ? raw : typeof raw === 'string' ? parseFloat(raw) : NaN;
    if (!isFinite(n)) return null;
    return Math.round(n <= 1 ? n * 100 : n);
}

// ── Bounding-box normalization ────────────────────────────────────────────
// Reuses ActiveAlarmView's CSS-percentage overlay approach. ONVIF VCA boxes
// are already normalized 0-1; Milesight panoramic boxes are pixel-space and
// must be divided by the frame W/H (carried in details when present, else the
// thumbnail's natural size). Output is always a 0-1 fractional rect.
interface NormBox { x: number; y: number; w: number; h: number; label?: string }

function getNormalizedBoxes(event: Event, frameW: number, frameH: number): NormBox[] {
    const d = event.details || {};
    const boxes = d.bounding_boxes ?? d.boundingBoxes ?? d.bbox ?? d.boxes;
    if (!Array.isArray(boxes) || boxes.length === 0) return [];

    // Frame dims for pixel→fraction conversion: prefer explicit details, else
    // the rendered thumbnail's natural size.
    const fw = Number(d.frame_w ?? d.frameWidth ?? d.width) || frameW || 0;
    const fh = Number(d.frame_h ?? d.frameHeight ?? d.height) || frameH || 0;

    const out: NormBox[] = [];
    for (const b of boxes) {
        if (!b || typeof b !== 'object') continue;
        let x = Number(b.x), y = Number(b.y), w = Number(b.w), h = Number(b.h);
        // Some payloads use x1/y1/x2/y2 corner form (YOLO bbox_normalized).
        if (!isFinite(x) && isFinite(Number(b.x1))) {
            x = Number(b.x1); y = Number(b.y1);
            w = Number(b.x2) - Number(b.x1); h = Number(b.y2) - Number(b.y1);
        }
        if (![x, y, w, h].every(isFinite)) continue;
        // Heuristic: any coordinate > 1 means pixel-space → normalize by frame.
        const isPixel = x > 1 || y > 1 || w > 1 || h > 1;
        if (isPixel) {
            if (fw <= 0 || fh <= 0) continue; // can't normalize without frame dims
            x /= fw; w /= fw; y /= fh; h /= fh;
        }
        const label = typeof b.label === 'string' ? b.label : undefined;
        out.push({ x, y, w, h, label });
    }
    return out;
}

export default function EventListPanel({ cameras, open, onClose, onEventClick, liveEvents, activeLayoutCameraIds = [] }: EventListPanelProps) {
    const [isMounted, setIsMounted] = useState(false);
    useEffect(() => { setIsMounted(true); }, []);

    // --- Filter State ---
    const [activeTypes, setActiveTypes] = useState<Set<string>>(new Set(EVENT_TYPES));
    // Multi-select camera filter (was single-select). Empty = all cameras.
    const [selectedCameras, setSelectedCameras] = useState<Set<string>>(new Set());
    const [search, setSearch] = useState('');
    const [dateFrom, setDateFrom] = useState(() => isoLocal(new Date(Date.now() - 24 * 3600 * 1000)));
    const [dateTo, setDateTo] = useState(() => isoLocal(new Date()));
    const [useCustomRange, setUseCustomRange] = useState(false);
    // Scope the feed to the active grid layout's cameras + group by camera.
    const [scopeToLayout, setScopeToLayout] = useState(false);
    const [sortMode, setSortMode] = useState<SortMode>('newest');
    const [groupByCamera, setGroupByCamera] = useState(false);

    // --- Remote events from API ---
    const [remoteEvents, setRemoteEvents] = useState<Event[]>([]);
    const [loading, setLoading] = useState(false);
    const [expandedId, setExpandedId] = useState<number | null>(null);
    // Per-thumbnail natural size, keyed by event id — lets pixel-space bbox
    // coords normalize against the actual image when frame dims aren't in details.
    const [imgDims, setImgDims] = useState<Record<number, { w: number; h: number }>>({});

    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    // The effective camera scope: explicit multi-select wins; otherwise, if
    // "This layout" is on, fall back to the active layout's cameras.
    const effectiveCameraIds = useMemo<string[]>(() => {
        if (selectedCameras.size > 0) return Array.from(selectedCameras);
        if (scopeToLayout && activeLayoutCameraIds.length > 0) return activeLayoutCameraIds;
        return [];
    }, [selectedCameras, scopeToLayout, activeLayoutCameraIds]);

    const fetchEvents = useCallback(async () => {
        setLoading(true);
        try {
            const end = useCustomRange ? new Date(dateTo) : new Date();
            const start = useCustomRange
                ? new Date(dateFrom)
                : new Date(Date.now() - 24 * 3600 * 1000);

            const types = Array.from(activeTypes).join(',');
            // The list endpoint takes a single camera_id; with a multi-select
            // or layout scope we query unscoped (or by the single id when
            // exactly one is chosen) and filter client-side below. The list
            // payload already includes details + thumbnail + source, so no
            // per-event fetch is needed.
            const singleCam = effectiveCameraIds.length === 1 ? effectiveCameraIds[0] : undefined;
            const data = await queryEvents({
                start: start.toISOString(),
                end: end.toISOString(),
                camera_id: singleCam,
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
    }, [activeTypes, effectiveCameraIds, search, dateFrom, dateTo, useCustomRange]);

    // Debounced re-fetch on filter changes
    useEffect(() => {
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(fetchEvents, 300);
        return () => { if (debounceRef.current) clearTimeout(debounceRef.current); };
    }, [fetchEvents]);

    // Client-side event filter predicate (applied to both live and remote)
    const matchesFilters = useCallback((e: Event) => {
        if (!activeTypes.has(e.event_type)) return false;
        if (effectiveCameraIds.length > 0 && !effectiveCameraIds.includes(e.camera_id)) return false;
        if (search && !JSON.stringify(e).toLowerCase().includes(search.toLowerCase())) return false;
        return true;
    }, [activeTypes, effectiveCameraIds, search]);

    const displayedEvents = useMemo(() => {
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

        // Sort
        const ts = (e: Event) => new Date(e.event_time).getTime();
        const sorted = [...deduped];
        switch (sortMode) {
            case 'oldest': sorted.sort((a, b) => ts(a) - ts(b)); break;
            case 'type': sorted.sort((a, b) => a.event_type.localeCompare(b.event_type) || ts(b) - ts(a)); break;
            case 'camera': sorted.sort((a, b) => getCameraName(a.camera_id).localeCompare(getCameraName(b.camera_id)) || ts(b) - ts(a)); break;
            case 'confidence': sorted.sort((a, b) => (getConfidencePct(b) ?? -1) - (getConfidencePct(a) ?? -1) || ts(b) - ts(a)); break;
            default: sorted.sort((a, b) => ts(b) - ts(a)); break;
        }
        return sorted.slice(0, 200);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [remoteEvents, liveEvents, matchesFilters, useCustomRange, sortMode, cameras]);

    // Group rows by camera when requested (or when scoping to a layout).
    const grouped = useMemo(() => {
        const doGroup = groupByCamera || (scopeToLayout && effectiveCameraIds.length > 0);
        if (!doGroup) return null;
        const map = new Map<string, Event[]>();
        for (const e of displayedEvents) {
            const arr = map.get(e.camera_id) ?? [];
            arr.push(e);
            map.set(e.camera_id, arr);
        }
        return Array.from(map.entries());
    }, [displayedEvents, groupByCamera, scopeToLayout, effectiveCameraIds]);

    const toggleType = (type: string) => {
        setActiveTypes(prev => {
            const next = new Set(prev);
            next.has(type) ? next.delete(type) : next.add(type);
            return next;
        });
    };

    const toggleCamera = (id: string) => {
        setSelectedCameras(prev => {
            const next = new Set(prev);
            next.has(id) ? next.delete(id) : next.add(id);
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

    function getCameraName(id: string) {
        return cameras.find(c => c.id === id)?.name ?? id.slice(0, 8);
    }

    const color = (type: string) => EVENT_COLORS[type] ?? EVENT_COLORS.other;

    // ── Single enriched row (reused by flat + grouped renders) ──
    const renderRow = (event: Event, i: number) => {
        const isExpanded = expandedId === event.id;
        const hasThumbnail = !!event.thumbnail;
        const thumbSrc = hasThumbnail
            ? (event.thumbnail.startsWith('data:') ? event.thumbnail : `data:image/jpeg;base64,${event.thumbnail}`)
            : '';

        const objClass = getObjectClass(event);
        const ruleName = getRuleName(event);
        const confidence = getConfidencePct(event);
        // Trust only the backend-normalized Event.source ("camera"/"server").
        // We deliberately do NOT fall back to raw details.source — that key is
        // overloaded by ONVIF (e.g. "VideoSourceToken") and would mislabel rows.
        const src = event.source;
        const isCamera = src === 'camera';
        const isServer = src === 'server';

        const dims = event.id ? imgDims[event.id] : undefined;
        const boxes = getNormalizedBoxes(event, dims?.w ?? 0, dims?.h ?? 0);
        const showBoxes = boxes.length > 0;

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
                {/* Thumbnail + bounding-box overlay */}
                {hasThumbnail && (
                    <div style={{ position: 'relative', width: '100%', borderRadius: 4, overflow: 'hidden', marginBottom: 4, background: '#080a06' }}>
                        <img
                            src={thumbSrc}
                            alt="event snapshot"
                            style={{ width: '100%', maxHeight: 120, objectFit: 'contain', display: 'block' }}
                            loading="lazy"
                            onLoad={e => {
                                const el = e.currentTarget;
                                if (event.id && el.naturalWidth > 0) {
                                    setImgDims(prev => prev[event.id]
                                        ? prev
                                        : { ...prev, [event.id]: { w: el.naturalWidth, h: el.naturalHeight } });
                                }
                            }}
                        />
                        {/* Bounding boxes — CSS-percentage overlay matching the image (reuses ActiveAlarmView logic) */}
                        {showBoxes && boxes.map((b, bi) => (
                            <div key={bi} style={{
                                position: 'absolute',
                                left: `${b.x * 100}%`,
                                top: `${b.y * 100}%`,
                                width: `${b.w * 100}%`,
                                height: `${b.h * 100}%`,
                                border: '2px solid #3B82F6',
                                background: 'rgba(59,130,246,0.08)',
                                pointerEvents: 'none',
                                boxSizing: 'border-box',
                            }}>
                                <div style={{
                                    position: 'absolute', top: -16, left: -2,
                                    background: 'rgba(59,130,246,0.9)', color: '#fff',
                                    fontSize: 9, fontWeight: 700, padding: '1px 5px',
                                    borderRadius: '3px 3px 0 0', whiteSpace: 'nowrap',
                                    fontFamily: "'JetBrains Mono', monospace",
                                }}>
                                    {(b.label || objClass || event.event_type).toUpperCase()}{confidence != null ? ` ${confidence}%` : ''}
                                </div>
                            </div>
                        ))}
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
                                {objClass || event.event_type}
                            </span>
                            <span className="event-info-time">
                                {formatTime(event.event_time)}
                            </span>
                        </div>

                        {/* Detection chips: object class · rule · confidence · source badge */}
                        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, margin: '3px 0' }}>
                            {objClass && (
                                <span style={{
                                    fontSize: 10, fontWeight: 700, padding: '1px 6px', borderRadius: 3,
                                    background: `${color(event.event_type)}22`, color: color(event.event_type),
                                    border: `1px solid ${color(event.event_type)}44`,
                                }}>
                                    {objClass}
                                </span>
                            )}
                            {ruleName && (
                                <span style={{
                                    fontSize: 10, fontWeight: 600, padding: '1px 6px', borderRadius: 3,
                                    background: 'rgba(168,85,247,0.12)', color: '#a855f7',
                                    border: '1px solid rgba(168,85,247,0.3)',
                                    maxWidth: 150, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                                }} title={ruleName}>
                                    {ruleName}
                                </span>
                            )}
                            {confidence != null && (
                                <span style={{
                                    fontSize: 10, fontWeight: 700, padding: '1px 6px', borderRadius: 3,
                                    background: confidence >= 80 ? 'rgba(239,68,68,0.15)' : confidence >= 60 ? 'rgba(234,179,8,0.15)' : 'rgba(107,114,128,0.15)',
                                    color: confidence >= 80 ? '#ef4444' : confidence >= 60 ? '#eab308' : '#9ca3af',
                                    border: '1px solid currentColor',
                                    fontFamily: "'JetBrains Mono', monospace",
                                }}>
                                    {confidence}%
                                </span>
                            )}
                            {(isCamera || isServer) && (
                                <span style={{
                                    fontSize: 10, fontWeight: 700, padding: '1px 6px', borderRadius: 3, letterSpacing: 0.3,
                                    background: isCamera ? 'rgba(34,197,94,0.12)' : 'rgba(0,212,255,0.12)',
                                    color: isCamera ? '#22c55e' : '#00d4ff',
                                    border: `1px solid ${isCamera ? 'rgba(34,197,94,0.35)' : 'rgba(0,212,255,0.35)'}`,
                                }} title={isCamera ? 'Detected by the camera’s onboard VCA' : 'Detected by the server-side AI pipeline'}>
                                    {isCamera ? '📷 Camera VCA' : '🧠 Server AI'}
                                </span>
                            )}
                        </div>

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
    };

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

                {/* Scope + sort row */}
                <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap' }}>
                    <button
                        onClick={() => setScopeToLayout(v => !v)}
                        disabled={activeLayoutCameraIds.length === 0}
                        title={activeLayoutCameraIds.length === 0 ? 'No active grid layout' : 'Scope feed to the cameras in the active grid layout'}
                        style={{
                            background: scopeToLayout ? 'var(--accent-blue, #3b82f6)' : 'transparent',
                            border: `1px solid ${scopeToLayout ? 'var(--accent-blue, #3b82f6)' : 'var(--border)'}`,
                            color: scopeToLayout ? '#fff' : 'var(--text-secondary)',
                            borderRadius: 4, padding: '3px 8px', fontSize: 11, fontWeight: 600,
                            cursor: activeLayoutCameraIds.length === 0 ? 'not-allowed' : 'pointer',
                            opacity: activeLayoutCameraIds.length === 0 ? 0.5 : 1,
                        }}
                    >
                        ▦ This layout{activeLayoutCameraIds.length > 0 ? ` (${activeLayoutCameraIds.length})` : ''}
                    </button>
                    <button
                        onClick={() => setGroupByCamera(v => !v)}
                        title="Group rows by camera"
                        style={{
                            background: groupByCamera ? 'var(--accent-blue, #3b82f6)' : 'transparent',
                            border: `1px solid ${groupByCamera ? 'var(--accent-blue, #3b82f6)' : 'var(--border)'}`,
                            color: groupByCamera ? '#fff' : 'var(--text-secondary)',
                            borderRadius: 4, padding: '3px 8px', fontSize: 11, fontWeight: 600, cursor: 'pointer',
                        }}
                    >
                        ☰ Group
                    </button>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 4, marginLeft: 'auto', fontSize: 11, color: 'var(--text-secondary)' }}>
                        Sort
                        <select
                            value={sortMode}
                            onChange={e => setSortMode(e.target.value as SortMode)}
                            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 4, padding: '2px 5px', fontSize: 11 }}
                        >
                            {SORT_OPTIONS.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                        </select>
                    </label>
                </div>

                {/* Multi-select camera filter (chips) + search */}
                <div style={{ display: 'flex', gap: 6, alignItems: 'flex-start' }}>
                    <div style={{ flex: 1, display: 'flex', flexWrap: 'wrap', gap: 3, maxHeight: 64, overflowY: 'auto' }}>
                        <button
                            onClick={() => setSelectedCameras(new Set())}
                            title="Show all cameras"
                            style={{
                                background: selectedCameras.size === 0 ? 'var(--accent-blue, #3b82f6)' : 'transparent',
                                border: `1px solid ${selectedCameras.size === 0 ? 'var(--accent-blue, #3b82f6)' : 'var(--border)'}`,
                                color: selectedCameras.size === 0 ? '#fff' : 'var(--text-secondary)',
                                borderRadius: 4, padding: '2px 6px', fontSize: 10, fontWeight: 600, cursor: 'pointer',
                            }}
                        >
                            📷 All
                        </button>
                        {cameras.map(c => {
                            const on = selectedCameras.has(c.id);
                            return (
                                <button
                                    key={c.id}
                                    onClick={() => toggleCamera(c.id)}
                                    title={c.name}
                                    style={{
                                        background: on ? 'var(--accent-blue, #3b82f6)' : 'transparent',
                                        border: `1px solid ${on ? 'var(--accent-blue, #3b82f6)' : 'var(--border)'}`,
                                        color: on ? '#fff' : 'var(--text-secondary)',
                                        borderRadius: 4, padding: '2px 6px', fontSize: 10, fontWeight: 500, cursor: 'pointer',
                                        maxWidth: 110, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                                    }}
                                >
                                    {c.name}
                                </button>
                            );
                        })}
                    </div>
                    <input
                        type="search"
                        placeholder="Search…"
                        value={search}
                        onChange={e => setSearch(e.target.value)}
                        style={{ width: 110, flexShrink: 0, background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-primary)', borderRadius: 4, padding: '3px 7px', fontSize: 12 }}
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
                ) : grouped ? (
                    grouped.map(([camId, evs]) => (
                        <div key={camId}>
                            <div style={{
                                position: 'sticky', top: 0, zIndex: 1,
                                padding: '4px 10px', fontSize: 11, fontWeight: 700, letterSpacing: 0.5,
                                background: 'var(--bg-secondary, #14181f)', color: 'var(--text-secondary)',
                                borderBottom: '1px solid var(--border)',
                            }}>
                                📷 {getCameraName(camId)} <span style={{ color: 'var(--text-muted)', fontWeight: 500 }}>({evs.length})</span>
                            </div>
                            {evs.map((event, i) => renderRow(event, i))}
                        </div>
                    ))
                ) : (
                    displayedEvents.map((event, i) => renderRow(event, i))
                )}
            </div>
        </div>
    );
}
