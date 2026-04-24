'use client';

import { useRef, useMemo, useCallback, useState, useEffect } from 'react';
import { Camera, Event, TimelineBucket, SegmentCoverage, fetchCoverage } from '@/lib/api';

interface TimelineProps {
    buckets: TimelineBucket[];
    isLive: boolean;
    currentTime: Date;
    filters: Record<string, boolean>;
    onSeek: (time: Date) => void;
    onGoLive: () => void;
    onToggleFilter: (type: string) => void;
    cameras: Camera[];
    isolatedCamera: string | null;
    onIsolateCamera: (cameraId: string | null) => void;
    events: Event[];
    onScrubStart?: () => void;
    onScrubEnd?: () => void;
    /** Whether playback is globally paused */
    globalPaused?: boolean;
    /** Toggle global pause/play */
    onTogglePause?: () => void;
}

const EVENT_TYPES = [
    { key: 'motion', label: 'Motion', color: 'motion' },
    { key: 'lpr', label: 'LPR', color: 'lpr' },
    { key: 'object', label: 'Object', color: 'object' },
    { key: 'face', label: 'Face', color: 'face' },
];



// Zoom levels in milliseconds (label shown to user)
const ZOOM_LEVELS = [
    { ms: 15 * 1000, label: '15s' },
    { ms: 30 * 1000, label: '30s' },
    { ms: 60 * 1000, label: '1m' },
    { ms: 2 * 60 * 1000, label: '2m' },
    { ms: 5 * 60 * 1000, label: '5m' },
    { ms: 15 * 60 * 1000, label: '15m' },
    { ms: 30 * 60 * 1000, label: '30m' },
    { ms: 60 * 60 * 1000, label: '1h' },
    { ms: 2 * 60 * 60 * 1000, label: '2h' },
    { ms: 4 * 60 * 60 * 1000, label: '4h' },
    { ms: 8 * 60 * 60 * 1000, label: '8h' },
    { ms: 24 * 60 * 60 * 1000, label: '24h' },
];
const DEFAULT_ZOOM_INDEX = 7; // 1h

// Snap granularity: 0.25s when zoomed to ≤ 2 minutes, otherwise 1s
const SNAP_MS = (windowMs: number) => windowMs <= 2 * 60 * 1000 ? 250 : 1000;

export default function Timeline({
    buckets,
    isLive,
    currentTime,
    filters,
    onSeek,
    onGoLive,
    onToggleFilter,
    cameras,
    isolatedCamera,
    onIsolateCamera,
    events,
    onScrubStart,
    onScrubEnd,
    globalPaused = false,
    onTogglePause,
}: TimelineProps) {
    const trackRef = useRef<HTMLDivElement>(null);
    const isDragging = useRef(false);
    const [zoomIndex, setZoomIndex] = useState(DEFAULT_ZOOM_INDEX);
    const [coverageSpans, setCoverageSpans] = useState<SegmentCoverage[]>([]);
    const [hoverX, setHoverX] = useState<number | null>(null); // percent 0-100
    const [hoverTime, setHoverTime] = useState<Date | null>(null);
    const [isScrubbing, setIsScrubbing] = useState(false);
    const [cameraMenuOpen, setCameraMenuOpen] = useState(false);
    const cameraMenuRef = useRef<HTMLDivElement>(null);

    // Close camera menu on click outside
    useEffect(() => {
        if (!cameraMenuOpen) return;
        const handler = (e: MouseEvent) => {
            if (cameraMenuRef.current && !cameraMenuRef.current.contains(e.target as Node)) {
                setCameraMenuOpen(false);
            }
        };
        document.addEventListener('mousedown', handler);
        return () => document.removeEventListener('mousedown', handler);
    }, [cameraMenuOpen]);


    // Timeline window: centered on current time, width based on zoom
    const windowMs = ZOOM_LEVELS[zoomIndex].ms;
    const zoomLabel = ZOOM_LEVELS[zoomIndex].label;
    const startTime = useMemo(() => new Date(currentTime.getTime() - windowMs / 2), [currentTime, windowMs]);
    const endTime = useMemo(() => new Date(currentTime.getTime() + windowMs / 2), [currentTime, windowMs]);

    // Fetch coverage whenever the window or cameras change
    // Debounce by rounding to 30-second boundaries so we don't fetch every second
    const coverageStartKey = Math.floor(startTime.getTime() / 30000);
    const coverageEndKey = Math.floor(endTime.getTime() / 30000);
    useEffect(() => {
        const cameraIds = cameras.map(c => c.id);
        if (cameraIds.length === 0) return;
        let cancelled = false;
        fetchCoverage(cameraIds, startTime, endTime)
            .then(spans => { if (!cancelled) setCoverageSpans(spans ?? []); })
            .catch(() => { });
        return () => { cancelled = true; };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [coverageStartKey, coverageEndKey, cameras.map(c => c.id).join(',')]);

    // Pre-compute coverage bar segments clipped to visible window
    const coverageBars = useMemo(() => {
        const winStart = startTime.getTime();
        const winEnd = endTime.getTime();
        return coverageSpans
            .map(span => {
                const sMs = Math.max(new Date(span.start_time).getTime(), winStart);
                const eMs = Math.min(new Date(span.end_time).getTime(), winEnd);
                if (eMs <= sMs) return null;
                return {
                    left: ((sMs - winStart) / windowMs) * 100,
                    width: ((eMs - sMs) / windowMs) * 100,
                    hasAudio: span.has_audio,
                };
            })
            .filter(Boolean) as { left: number; width: number; hasAudio: boolean }[];
    }, [coverageSpans, startTime, endTime, windowMs]);

    // ------------------------------------------------------------------
    // Playhead is always at center (50%)
    const playheadPos = 50;

    // Zoom in/out handlers
    const zoomIn = useCallback(() => {
        setZoomIndex((prev) => Math.max(0, prev - 1));
    }, []);
    const zoomOut = useCallback(() => {
        setZoomIndex((prev) => Math.min(ZOOM_LEVELS.length - 1, prev + 1));
    }, []);

    // Mouse wheel zoom on timeline track
    const handleWheel = useCallback(
        (e: React.WheelEvent<HTMLDivElement>) => {
            e.preventDefault();
            if (e.deltaY < 0) {
                zoomIn(); // scroll up = zoom in
            } else {
                zoomOut(); // scroll down = zoom out
            }
        },
        [zoomIn, zoomOut]
    );

    // Build markers from buckets
    const markers = useMemo(() => {
        if (!buckets || buckets.length === 0) return [];

        const maxTotal = Math.max(...buckets.map((b) => b.total), 1);

        return buckets
            .filter((b) => {
                // Filter by active event types
                return Object.entries(b.counts).some(([type]) => filters[type]);
            })
            .map((bucket) => {
                const bucketTime = new Date(bucket.bucket_time).getTime();
                const pos = ((bucketTime - startTime.getTime()) / windowMs) * 100;
                const height = Math.max(4, (bucket.total / maxTotal) * 18);

                // Determine primary type for color
                const primaryType = Object.entries(bucket.counts).sort((a, b) => b[1] - a[1])[0]?.[0] || 'other';

                return {
                    left: `${pos}%`,
                    height: `${height}px`,
                    type: primaryType,
                };
            })
            .filter((m) => {
                const left = parseFloat(m.left);
                return left >= 0 && left <= 100;
            });
    }, [buckets, filters, startTime]);

    // Handle click on timeline track
    const handleTrackClick = useCallback(
        (e: React.MouseEvent<HTMLDivElement>) => {
            if (isDragging.current) return; // skip click after drag
            const track = trackRef.current;
            if (!track) return;

            const rect = track.getBoundingClientRect();
            const x = e.clientX - rect.left;
            const ratio = x / rect.width;
            const seekTime = new Date(startTime.getTime() + ratio * windowMs);

            onSeek(seekTime);
        },
        [startTime, windowMs, onSeek]
    );

    // Hover tracking for tooltip
    const handleTrackMouseMove = useCallback(
        (e: React.MouseEvent<HTMLDivElement>) => {
            const track = trackRef.current;
            if (!track) return;
            const rect = track.getBoundingClientRect();
            const x = e.clientX - rect.left;
            const ratio = Math.max(0, Math.min(1, x / rect.width));
            setHoverX(ratio * 100);
            setHoverTime(new Date(startTime.getTime() + ratio * windowMs));
        },
        [startTime, windowMs]
    );

    const handleTrackMouseLeave = useCallback(() => {
        if (!isDragging.current) {
            setHoverX(null);
            setHoverTime(null);
        }
    }, []);

    // --- Jog Shuttle Drag (reversed: drag right = earlier, drag left = later) ---
    const lastXRef = useRef(0);
    const currentTimeRef = useRef(currentTime);
    currentTimeRef.current = currentTime;

    const panFromDelta = useCallback(
        (deltaX: number) => {
            const track = trackRef.current;
            if (!track) return;
            const rect = track.getBoundingClientRect();
            // Convert pixel delta to time delta — REVERSED direction
            const timeDelta = (deltaX / rect.width) * windowMs;
            const rawMs = currentTimeRef.current.getTime() + timeDelta;
            const snap = SNAP_MS(windowMs);
            const snappedMs = Math.round(rawMs / snap) * snap;
            onSeek(new Date(snappedMs));
        },
        [windowMs, onSeek]
    );

    const handleMouseDown = useCallback(
        (e: React.MouseEvent<HTMLDivElement>) => {
            isDragging.current = true;
            setIsScrubbing(true);
            onScrubStart?.();
            lastXRef.current = e.clientX;

            let accumulatedDelta = 0;
            let rafId: number | null = null;

            const flushDelta = () => {
                if (accumulatedDelta !== 0) {
                    panFromDelta(accumulatedDelta);
                    accumulatedDelta = 0;
                }
            };

            const handleMouseMove = (ev: MouseEvent) => {
                if (!isDragging.current) return;
                const delta = lastXRef.current - ev.clientX;
                lastXRef.current = ev.clientX;
                accumulatedDelta += delta;

                // Use requestAnimationFrame for frame-locked, jank-free updates
                if (rafId === null) {
                    rafId = requestAnimationFrame(() => {
                        flushDelta();
                        rafId = null;
                    });
                }
            };
            const handleMouseUp = () => {
                isDragging.current = false;
                setIsScrubbing(false);
                setHoverX(null);
                setHoverTime(null);
                if (rafId !== null) { cancelAnimationFrame(rafId); rafId = null; }
                flushDelta(); // flush any remaining delta as authoritative final seek
                onScrubEnd?.();
                window.removeEventListener('mousemove', handleMouseMove);
                window.removeEventListener('mouseup', handleMouseUp);
            };
            window.addEventListener('mousemove', handleMouseMove);
            window.addEventListener('mouseup', handleMouseUp);
        },
        [panFromDelta, onScrubStart, onScrubEnd]
    );

    const handleTouchStart = useCallback(
        (e: React.TouchEvent<HTMLDivElement>) => {
            isDragging.current = true;
            setIsScrubbing(true);
            onScrubStart?.();
            lastXRef.current = e.touches[0].clientX;

            let accumulatedDelta = 0;
            let rafId: number | null = null;

            const flushDelta = () => {
                if (accumulatedDelta !== 0) {
                    panFromDelta(accumulatedDelta);
                    accumulatedDelta = 0;
                }
            };

            const handleTouchMove = (ev: TouchEvent) => {
                if (!isDragging.current) return;
                const delta = lastXRef.current - ev.touches[0].clientX;
                lastXRef.current = ev.touches[0].clientX;
                accumulatedDelta += delta;

                if (rafId === null) {
                    rafId = requestAnimationFrame(() => {
                        flushDelta();
                        rafId = null;
                    });
                }
            };
            const handleTouchEnd = () => {
                isDragging.current = false;
                setIsScrubbing(false);
                if (rafId !== null) { cancelAnimationFrame(rafId); rafId = null; }
                flushDelta();
                onScrubEnd?.();
                window.removeEventListener('touchmove', handleTouchMove);
                window.removeEventListener('touchend', handleTouchEnd);
            };
            window.addEventListener('touchmove', handleTouchMove);
            window.addEventListener('touchend', handleTouchEnd);
        },
        [panFromDelta, onScrubStart, onScrubEnd]
    );

    // Jump buttons
    const jumpBack = useCallback(
        (minutes: number) => {
            const newTime = new Date(currentTime.getTime() - minutes * 60 * 1000);
            onSeek(newTime);
        },
        [currentTime, onSeek]
    );

    const jumpForward = useCallback(
        (minutes: number) => {
            const newTime = new Date(currentTime.getTime() + minutes * 60 * 1000);
            if (newTime >= new Date()) {
                onGoLive();
            } else {
                onSeek(newTime);
            }
        },
        [currentTime, onSeek, onGoLive]
    );

    const skipToPrevEvent = useCallback(() => {
        // events are sorted newest first. find first event whose time is older than currentTime
        const now = currentTime.getTime();
        const prev = events.find(e => new Date(e.event_time).getTime() < now - 1000); // 1s buffer
        if (prev) {
            onSeek(new Date(prev.event_time));
        } else {
            // jump back an hour if no events
            jumpBack(60);
        }
    }, [currentTime, events, onSeek, jumpBack]);

    const skipToNextEvent = useCallback(() => {
        // events are sorted newest first. we need the oldest event that is newer than currentTime
        const now = currentTime.getTime();
        // findIndex of the first event (newest) that is STILL older than or equal to current time
        // then take the one right before it (which would be newer)
        // or just iterate backwards from the end of the array (oldest events first)
        let nextEvent: Event | null = null;
        for (let i = events.length - 1; i >= 0; i--) {
            if (new Date(events[i].event_time).getTime() > now + 1000) {
                nextEvent = events[i];
                break;
            }
        }

        if (nextEvent) {
            onSeek(new Date(nextEvent.event_time));
        } else {
            // if no next event, try jumping forward or go live
            jumpForward(60);
        }
    }, [currentTime, events, onSeek, jumpForward]);

    const [isMounted, setIsMounted] = useState(false);

    useEffect(() => {
        setIsMounted(true);
    }, []);

    // "Go to date/time" — inline editable timestamp
    const [editingTime, setEditingTime] = useState(false);
    const [goToValue, setGoToValue] = useState('');
    const timeInputRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        if (!editingTime && isMounted) {
            const d = currentTime;
            const pad = (n: number) => String(n).padStart(2, '0');
            const local = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
            setGoToValue(local);
        }
    }, [currentTime, isMounted, editingTime]);

    const handleGoTo = useCallback(() => {
        const t = new Date(goToValue);
        if (!isNaN(t.getTime())) onSeek(t);
        setEditingTime(false);
    }, [goToValue, onSeek]);

    const startEditingTime = () => {
        setEditingTime(true);
        setTimeout(() => timeInputRef.current?.focus(), 0);
    };

    // Format time — shows seconds when zoomed ≤ 5m, tenths when ≤ 2m
    const formatTime = (date: Date, forceShort = false) => {
        if (!isMounted) return '';
        if (!forceShort && windowMs <= 2 * 60 * 1000) {
            // HH:MM:SS.s
            const s = date.getSeconds().toString().padStart(2, '0');
            const ds = Math.floor(date.getMilliseconds() / 100); // tenths
            return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ':' + s + '.' + ds;
        }
        if (!forceShort && windowMs <= 5 * 60 * 1000) {
            // HH:MM:SS
            const s = date.getSeconds().toString().padStart(2, '0');
            return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ':' + s;
        }
        return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    };

    return (
        <div className="timeline-container">
            {/* Controls Row */}
            <div className="timeline-controls">
                {/* LEFT: Go Live button */}
                <div className="timeline-left">
                    {!isLive ? (
                        <button className="btn btn-live" onClick={onGoLive}>
                            ● GO LIVE
                        </button>
                    ) : (
                        <span className="btn" style={{ cursor: 'default', color: 'var(--accent-green)' }}>
                            ● LIVE
                        </span>
                    )}
                </div>

                {/* CENTER: Transport + Timestamp + Zoom */}
                <div className="timeline-center">
                    {/* Transport controls */}
                    <div className="timeline-transport">
                        <button className="transport-btn" onClick={skipToPrevEvent} title="Previous Event (Shift+←)">
                            ⏮
                        </button>
                        <button className="transport-btn" onClick={() => jumpBack(60)} title="-1 hour">
                            ⏪
                        </button>
                        <button className="transport-btn" onClick={() => jumpBack(5)} title="-5 min (←)">
                            ◀
                        </button>
                        {/* Global play/pause — only in playback mode */}
                        {!isLive && onTogglePause && (
                            <button
                                className={`transport-btn transport-btn-main ${globalPaused ? 'paused' : 'playing'}`}
                                onClick={onTogglePause}
                                title={globalPaused ? 'Play (Space)' : 'Pause (Space)'}
                            >
                                {globalPaused ? '▶' : '⏸'}
                            </button>
                        )}
                        <button className="transport-btn" onClick={() => jumpForward(5)} title="+5 min (→)">
                            ▶
                        </button>
                        <button className="transport-btn" onClick={() => jumpForward(60)} title="+1 hour">
                            ⏩
                        </button>
                        <button className="transport-btn" onClick={skipToNextEvent} title="Next Event (Shift+→)">
                            ⏭
                        </button>
                    </div>

                    {/* Divider */}
                    <span style={{ width: 1, height: 20, background: 'var(--border-color)', flexShrink: 0 }} />

                    {/* Clickable playback timestamp — click to edit, shows current playback time */}
                    {editingTime ? (
                        <form
                            onSubmit={e => { e.preventDefault(); handleGoTo(); }}
                            style={{ display: 'flex', alignItems: 'center', gap: 4 }}
                        >
                            <input
                                ref={timeInputRef}
                                type="datetime-local"
                                value={goToValue}
                                onChange={e => setGoToValue(e.target.value)}
                                onBlur={() => { handleGoTo(); }}
                                onKeyDown={e => { if (e.key === 'Escape') setEditingTime(false); }}
                                style={{
                                    background: 'var(--bg-secondary)',
                                    border: '1px solid var(--accent-orange)',
                                    color: 'var(--text-primary)',
                                    borderRadius: 5,
                                    padding: '3px 8px',
                                    fontSize: 12,
                                    fontFamily: 'var(--font-mono)',
                                    colorScheme: 'dark',
                                    outline: 'none',
                                    boxShadow: '0 0 8px rgba(232,115,42,0.2)',
                                }}
                            />
                        </form>
                    ) : (
                        <button
                            className={`timeline-time-display ${isScrubbing ? 'scrubbing' : ''}`}
                            onClick={startEditingTime}
                            title="Click to jump to a specific time"
                        >
                            {isMounted ? currentTime.toLocaleString([], {
                                month: 'short',
                                day: 'numeric',
                                hour: '2-digit',
                                minute: '2-digit',
                                second: '2-digit',
                            }) : ''}
                        </button>
                    )}

                    {/* Divider */}
                    <span style={{ width: 1, height: 20, background: 'var(--border-color)', flexShrink: 0 }} />

                    {/* Zoom controls */}
                    <div className="zoom-controls">
                        <button className="btn btn-sm btn-icon" onClick={zoomIn} title="Zoom in (shorter window)">
                            +
                        </button>
                        <span className="zoom-label" title="Timeline window">{zoomLabel}</span>
                        <button className="btn btn-sm btn-icon" onClick={zoomOut} title="Zoom out (longer window)">
                            −
                        </button>
                    </div>
                </div>

                <div className="timeline-right">
                    {/* Camera selector dropdown */}
                    <div className="cam-selector-wrap" ref={cameraMenuRef}>
                        <button
                            className={`cam-selector-trigger ${isolatedCamera ? 'has-selection' : ''}`}
                            onClick={() => setCameraMenuOpen(v => !v)}
                            title={isolatedCamera ? cameras.find(c => c.id === isolatedCamera)?.name || 'Camera' : 'All Cameras'}
                        >
                            <span className="cam-selector-icon">⎚</span>
                            <span className="cam-selector-label">
                                {isolatedCamera
                                    ? (cameras.find(c => c.id === isolatedCamera)?.name || 'Camera')
                                    : `All (${cameras.length})`
                                }
                            </span>
                            <span className={`cam-selector-arrow ${cameraMenuOpen ? 'open' : ''}`}>▾</span>
                        </button>

                        {cameraMenuOpen && (
                            <div className="cam-selector-menu">
                                <div className="cam-menu-header">CAMERA FILTER</div>
                                <button
                                    className={`cam-menu-item ${!isolatedCamera ? 'active' : ''}`}
                                    onClick={() => { onIsolateCamera(null); setCameraMenuOpen(false); }}
                                >
                                    <span className="cam-menu-dot all" />
                                    <span className="cam-menu-name">All Cameras</span>
                                    <span className="cam-menu-count">{cameras.length}</span>
                                </button>
                                <div className="cam-menu-divider" />
                                {cameras.map((cam) => (
                                    <button
                                        key={cam.id}
                                        className={`cam-menu-item ${isolatedCamera === cam.id ? 'active' : ''}`}
                                        onClick={() => {
                                            onIsolateCamera(isolatedCamera === cam.id ? null : cam.id);
                                            setCameraMenuOpen(false);
                                        }}
                                    >
                                        <span className={`cam-menu-dot ${cam.status === 'online' ? 'online' : 'offline'}`} />
                                        <span className="cam-menu-name">{cam.name}</span>
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* Metadata event filters */}
                    <div className="event-filter-group">
                        {EVENT_TYPES.map((et) => (
                            <button
                                key={et.key}
                                className={`event-filter-btn ${filters[et.key] ? 'active' : ''}`}
                                onClick={() => onToggleFilter(et.key)}
                                title={et.label}
                            >
                                <span className={`filter-dot ${et.color}`} />
                                {et.label}
                            </button>
                        ))}
                    </div>
                </div>
            </div>

            {/* Timeline Track - Jog Shuttle */}
            <div
                className={`timeline-track jog-shuttle ${isScrubbing ? 'scrub-active' : ''}`}
                ref={trackRef}
                onClick={handleTrackClick}
                onMouseDown={handleMouseDown}
                onTouchStart={handleTouchStart}
                onWheel={handleWheel}
                onMouseMove={handleTrackMouseMove}
                onMouseLeave={handleTrackMouseLeave}
            >
                {/* Center guide line */}
                <div className="timeline-center-guide" />

                {/* Progress fill */}
                <div className="timeline-progress" style={{ width: `${playheadPos}%` }} />

                {/* Playhead */}
                <div className="timeline-playhead" style={{ left: `${playheadPos}%` }}>
                    {isScrubbing && (
                        <div className="playhead-time-badge">
                            {isMounted ? currentTime.toLocaleTimeString([], {
                                hour: '2-digit',
                                minute: '2-digit',
                                second: '2-digit',
                            }) : ''}
                        </div>
                    )}
                </div>

                {/* Hover ghost playhead and tooltip */}
                {hoverX !== null && !isScrubbing && (
                    <>
                        <div className="timeline-hover-line" style={{ left: `${hoverX}%` }} />
                        {hoverTime && (
                            <div className="timeline-hover-tooltip" style={{ left: `${hoverX}%` }}>
                                {isMounted ? hoverTime.toLocaleString([], {
                                    month: 'short',
                                    day: 'numeric',
                                    hour: '2-digit',
                                    minute: '2-digit',
                                    second: '2-digit',
                                }) : ''}
                            </div>
                        )}
                    </>
                )}

                {/* Event markers */}
                <div className="timeline-markers">
                    {markers.map((marker, i) => (
                        <div
                            key={i}
                            className={`timeline-marker marker-${marker.type}`}
                            style={{
                                left: marker.left,
                                height: marker.height,
                            }}
                        />
                    ))}
                </div>

                {/* Coverage bars — rendered at the very bottom of the track */}
                <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: 6, pointerEvents: 'none' }}>
                    {/* Video coverage — green */}
                    {coverageBars.map((bar, i) => (
                        <div
                            key={`v-${i}`}
                            style={{
                                position: 'absolute',
                                bottom: 0,
                                left: `${bar.left}%`,
                                width: `${bar.width}%`,
                                height: 3,
                                background: '#22c55e',
                                opacity: 0.85,
                                borderRadius: 1,
                            }}
                        />
                    ))}
                    {/* Audio coverage — charcoal/black, just above the green line */}
                    {coverageBars.filter(b => b.hasAudio).map((bar, i) => (
                        <div
                            key={`a-${i}`}
                            style={{
                                position: 'absolute',
                                bottom: 3,
                                left: `${bar.left}%`,
                                width: `${bar.width}%`,
                                height: 3,
                                background: '#1a1a1a',
                                opacity: 0.9,
                                borderRadius: 1,
                            }}
                        />
                    ))}
                </div>
            </div>

            {/* Timeline Labels */}
            <div className="timeline-times">
                <span>{formatTime(startTime)}</span>
                <span>{formatTime(new Date(startTime.getTime() + windowMs * 0.25))}</span>
                <span>{formatTime(new Date(startTime.getTime() + windowMs * 0.5))}</span>
                <span>{formatTime(new Date(startTime.getTime() + windowMs * 0.75))}</span>
                <span>{formatTime(endTime)}</span>
            </div>
        </div>
    );
}
