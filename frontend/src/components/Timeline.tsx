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
    /** Current playback speed multiplier (playback mode only) */
    playbackRate?: number;
    /** Set playback speed multiplier */
    onSetPlaybackRate?: (rate: number) => void;
    /** Step a single frame forward (+1) or back (-1) — playback mode only */
    onStepFrame?: (direction: 1 | -1) => void;
}

// Playback speed options (playback mode only — HLS playbackRate is unreliable)
const PLAYBACK_RATES = [0.5, 1, 2, 4];

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
    playbackRate = 1,
    onSetPlaybackRate,
    onStepFrame,
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
    const [isMounted, setIsMounted] = useState(false);
    // Hovered event marker (for tooltip). Holds the marker index.
    const [hoverMarker, setHoverMarker] = useState<number | null>(null);

    useEffect(() => {
        setIsMounted(true);
    }, []);

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

    // Ruler tick label formatter. Granularity follows the chosen MAJOR
    // interval: sub-minute majors show seconds, sub-day show HH:MM, day+
    // shows the hour. Guarded on isMounted so SSR doesn't emit a locale
    // string that mismatches on hydration.
    const formatTick = useCallback((d: Date, majorMs: number): string => {
        if (!isMounted) return '';
        if (majorMs < 60_000) {
            // seconds-resolution window: HH:MM:SS
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
        }
        if (majorMs < 24 * 60 * 60_000) {
            return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
        }
        return d.toLocaleTimeString([], { hour: '2-digit', hour12: false }) + ':00';
    }, [isMounted]);

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
                const bucketMs = new Date(bucket.bucket_time).getTime();
                const pos = ((bucketMs - startTime.getTime()) / windowMs) * 100;
                const height = Math.max(8, (bucket.total / maxTotal) * 18);

                // Count only the active (filtered-in) event types for this bucket.
                const activeCounts = Object.entries(bucket.counts).filter(([type]) => filters[type]);
                const visibleTotal = activeCounts.reduce((sum, [, n]) => sum + n, 0);
                // Determine primary type for color (highest count among active types)
                const primaryType = activeCounts.sort((a, b) => b[1] - a[1])[0]?.[0] || 'other';

                return {
                    pos,
                    bucketMs,
                    height,
                    type: primaryType,
                    count: visibleTotal,
                    counts: Object.fromEntries(activeCounts),
                };
            })
            .filter((m) => m.pos >= 0 && m.pos <= 100 && m.count > 0);
    }, [buckets, filters, startTime, windowMs]);

    // ------------------------------------------------------------------
    // Zoom-aware tick ruler. Pick a "nice" interval from the visible span
    // so labels never crowd, then walk the window placing minor + major
    // ticks aligned to the SAME time→x mapping the markers/playhead use.
    const ticks = useMemo(() => {
        const winStart = startTime.getTime();
        // Candidate intervals in ms, sorted ascending. Each "major" tick is
        // labelled; minor ticks subdivide between majors.
        const STEPS: { major: number; minor: number }[] = [
            { major: 5_000, minor: 1_000 },          // 5s major / 1s minor   (≤30s window)
            { major: 10_000, minor: 5_000 },         // 10s / 5s              (≤1m)
            { major: 30_000, minor: 10_000 },        // 30s / 10s             (≤2-3m)
            { major: 60_000, minor: 15_000 },        // 1m / 15s              (≤5m)
            { major: 5 * 60_000, minor: 60_000 },    // 5m / 1m               (≤30m)
            { major: 10 * 60_000, minor: 2 * 60_000 },// 10m / 2m
            { major: 30 * 60_000, minor: 5 * 60_000 },// 30m / 5m             (≤2h)
            { major: 60 * 60_000, minor: 15 * 60_000 },// 1h / 15m            (≤8h)
            { major: 2 * 60 * 60_000, minor: 30 * 60_000 },// 2h / 30m
            { major: 6 * 60 * 60_000, minor: 60 * 60_000 },// 6h / 1h         (24h)
        ];
        // Target ~6-8 major labels across the track.
        const targetMajors = 7;
        let chosen = STEPS[STEPS.length - 1];
        for (const s of STEPS) {
            if (windowMs / s.major <= targetMajors) { chosen = s; break; }
        }

        const out: { pos: number; major: boolean; label: string }[] = [];
        const firstTick = Math.ceil(winStart / chosen.minor) * chosen.minor;
        const winEnd = winStart + windowMs;
        // Guard against pathological tiny intervals producing huge arrays.
        const maxTicks = 400;
        let count = 0;
        for (let t = firstTick; t <= winEnd && count < maxTicks; t += chosen.minor, count++) {
            const pos = ((t - winStart) / windowMs) * 100;
            if (pos < 0 || pos > 100) continue;
            const isMajor = t % chosen.major === 0;
            out.push({
                pos,
                major: isMajor,
                label: isMajor ? formatTick(new Date(t), chosen.major) : '',
            });
        }
        return out;
    }, [startTime, windowMs, formatTick]);

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

    // Second-granularity skip (the "fast forward / rewind" the operator
    // wants). Uses the same onSeek mechanism as the minute/hour jumps —
    // negative seconds = backward, positive = forward. Forward past "now"
    // snaps to live.
    const skipSeconds = useCallback(
        (seconds: number) => {
            const newTime = new Date(currentTime.getTime() + seconds * 1000);
            if (seconds > 0 && newTime >= new Date()) {
                onGoLive();
            } else {
                onSeek(newTime);
            }
        },
        [currentTime, onSeek, onGoLive]
    );

    // Click an event marker → seek the timeline to that bucket's time.
    const seekToMarker = useCallback(
        (bucketMs: number) => {
            onSeek(new Date(bucketMs));
        },
        [onSeek]
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
                        {/* Second-granularity rewind — finer than the minute jumps */}
                        <button className="transport-btn transport-btn-sec" onClick={() => skipSeconds(-30)} title="-30 sec">
                            -30s
                        </button>
                        <button className="transport-btn transport-btn-sec" onClick={() => skipSeconds(-10)} title="-10 sec">
                            -10s
                        </button>
                        {/* Frame-step back — playback mode only */}
                        {!isLive && onStepFrame && (
                            <button className="transport-btn transport-btn-frame" onClick={() => onStepFrame(-1)} title="Step back 1 frame">
                                ◀|
                            </button>
                        )}
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
                        {/* Frame-step forward — playback mode only */}
                        {!isLive && onStepFrame && (
                            <button className="transport-btn transport-btn-frame" onClick={() => onStepFrame(1)} title="Step forward 1 frame">
                                |▶
                            </button>
                        )}
                        {/* Second-granularity fast-forward */}
                        <button className="transport-btn transport-btn-sec" onClick={() => skipSeconds(10)} title="+10 sec">
                            +10s
                        </button>
                        <button className="transport-btn transport-btn-sec" onClick={() => skipSeconds(30)} title="+30 sec">
                            +30s
                        </button>
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

                    {/* Playback speed control — playback mode only (HLS live
                        playbackRate is unreliable, so hide it when live). */}
                    {!isLive && onSetPlaybackRate && (
                        <>
                            <span style={{ width: 1, height: 20, background: 'var(--border-color)', flexShrink: 0 }} />
                            <div className="speed-control" title="Playback speed">
                                {PLAYBACK_RATES.map((rate) => (
                                    <button
                                        key={rate}
                                        className={`speed-btn ${playbackRate === rate ? 'active' : ''}`}
                                        onClick={() => onSetPlaybackRate(rate)}
                                        title={`Play at ${rate}× speed`}
                                    >
                                        {rate}×
                                    </button>
                                ))}
                            </div>
                        </>
                    )}

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

                {/* Zoom-aware tick ruler (in-track marks). Aligned to the same
                    time→x mapping as the playhead/markers. Major ticks are
                    taller; minor ticks are short. */}
                <div className="timeline-ruler" aria-hidden="true">
                    {ticks.map((t, i) => (
                        <div
                            key={i}
                            className={`timeline-tick ${t.major ? 'tick-major' : 'tick-minor'}`}
                            style={{ left: `${t.pos}%` }}
                        />
                    ))}
                </div>

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

                {/* Event markers — wider, hoverable (tooltip) and clickable
                    (seek to that event's time). */}
                <div className="timeline-markers">
                    {markers.map((marker, i) => {
                        const typeLabel = EVENT_TYPES.find(t => t.key === marker.type)?.label || 'Event';
                        return (
                            <button
                                key={i}
                                type="button"
                                className={`timeline-marker marker-${marker.type} ${hoverMarker === i ? 'marker-hover' : ''}`}
                                style={{
                                    left: `${marker.pos}%`,
                                    height: `${marker.height}px`,
                                }}
                                onMouseEnter={() => setHoverMarker(i)}
                                onMouseLeave={() => setHoverMarker(prev => prev === i ? null : prev)}
                                onClick={(e) => { e.stopPropagation(); seekToMarker(marker.bucketMs); }}
                                onMouseDown={(e) => e.stopPropagation()}
                                title={`${typeLabel} — ${isMounted ? new Date(marker.bucketMs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''}${marker.count > 1 ? ` ×${marker.count}` : ''}`}
                                aria-label={`Seek to ${typeLabel} event`}
                            />
                        );
                    })}
                    {/* Rich hover tooltip for the focused marker */}
                    {hoverMarker !== null && markers[hoverMarker] && (
                        <div
                            className="timeline-marker-tooltip"
                            style={{ left: `${markers[hoverMarker].pos}%` }}
                        >
                            <div className="marker-tip-time">
                                {isMounted ? new Date(markers[hoverMarker].bucketMs).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : ''}
                            </div>
                            <div className="marker-tip-types">
                                {Object.entries(markers[hoverMarker].counts).map(([type, n]) => (
                                    <span key={type} className="marker-tip-type">
                                        <span className={`filter-dot ${EVENT_TYPES.find(t => t.key === type)?.color || 'other'}`} />
                                        {EVENT_TYPES.find(t => t.key === type)?.label || type}
                                        {n > 1 ? ` ×${n}` : ''}
                                    </span>
                                ))}
                            </div>
                        </div>
                    )}
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

            {/* Timeline ruler labels — one label per MAJOR tick, positioned
                at the same x as its in-track tick so the scale reads true. */}
            <div className="timeline-times timeline-ruler-labels">
                {ticks.filter(t => t.major && t.label).map((t, i) => (
                    <span
                        key={i}
                        className="ruler-label"
                        style={{ left: `${t.pos}%` }}
                    >
                        {t.label}
                    </span>
                ))}
            </div>
        </div>
    );
}
