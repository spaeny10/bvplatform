'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { ptzMove, ptzStop, ptzPrewarm, fetchPlaybackSegments, PlaybackSegment } from '@/lib/api';

interface VideoPlayerProps {
    cameraId: string;
    cameraName: string;
    isLive: boolean;
    playbackTime?: Date;
    selected?: boolean;
    hasPTZ?: boolean;
    allowZoom?: boolean;
    /** 'high' = main stream, 'low' = sub stream, 'auto' = sub in grid / main in peek */
    streamQuality?: 'auto' | 'high' | 'low';
    /** Show a '🔗 SYNC' badge when synchronized multi-camera playback is active */
    syncBadge?: boolean;
    /** Whether the timeline is actively being scrubbed (suppresses loading flash) */
    scrubbing?: boolean;
    /** Whether the current user is admin (enables inline rename) */
    isAdmin?: boolean;
    onClick?: () => void;
    onDoubleClick?: () => void;
    /** Callback when admin renames the camera */
    onRename?: (cameraId: string, newName: string) => void;
    // Pass the WebSocket ref from the parent so we reuse the same connection
    wsRef?: React.RefObject<WebSocket | null>;
    /** Global playback pause state from timeline transport */
    globalPaused?: boolean;
}




export default function VideoPlayer({
    cameraId,
    cameraName,
    isLive,
    playbackTime,
    selected,
    hasPTZ = false,
    allowZoom = false,
    streamQuality = 'auto',
    syncBadge = false,
    scrubbing = false,
    isAdmin = false,
    onClick,
    onDoubleClick,
    onRename,
    wsRef,
    globalPaused = false,
}: VideoPlayerProps) {
    const videoRef = useRef<HTMLVideoElement>(null);
    const hlsRef = useRef<any>(null);
    const [error, setError] = useState<string>('');
    const [loading, setLoading] = useState(true);
    const [bitrateBps, setBitrateBps] = useState<number>(0);
    const [resolution, setResolution] = useState<{ w: number, h: number } | null>(null);
    const [paused, setPaused] = useState(false);

    // Per-cell quality override: 'auto' uses prop, 'high'/'low' override it
    const [cellQuality, setCellQuality] = useState<'auto' | 'high' | 'low'>('auto');
    // Bump this to force WebRTC reconnect when quality changes
    const [qualityKey, setQualityKey] = useState(0);

    // Resolve the effective quality: cell override wins, then prop, then default
    const effectiveQuality = cellQuality !== 'auto' ? cellQuality : streamQuality;
    // 'high' = main stream; 'low' or 'auto' = sub stream (more reliable)
    const useMainStream = effectiveQuality === 'high';

    const toggleCellQuality = (e: React.MouseEvent) => {
        e.stopPropagation();
        setCellQuality(prev => {
            const next = prev === 'auto'
                ? (useMainStream ? 'low' : 'high')  // flip from current effective
                : prev === 'high' ? 'low' : 'high';
            return next;
        });
        setQualityKey(k => k + 1);
    };


    // Zoom and Pan state
    const [scale, setScale] = useState(1);
    const [pan, setPan] = useState({ x: 0, y: 0 });
    const [isDragging, setIsDragging] = useState(false);
    const [dragStart, setDragStart] = useState({ x: 0, y: 0 });

    // Reset zoom when camera switches
    useEffect(() => {
        setScale(1);
        setPan({ x: 0, y: 0 });
    }, [cameraId]);

    // --- Inline name editing state ---
    const [editingName, setEditingName] = useState(false);
    const [editValue, setEditValue] = useState(cameraName);
    const nameInputRef = useRef<HTMLInputElement>(null);

    // Sync editValue when cameraName prop changes
    useEffect(() => { setEditValue(cameraName); }, [cameraName]);

    const startEditing = (e: React.MouseEvent) => {
        if (!isAdmin || !onRename) return;
        e.stopPropagation();
        e.preventDefault();
        setEditValue(cameraName);
        setEditingName(true);
        setTimeout(() => nameInputRef.current?.select(), 0);
    };

    const commitRename = () => {
        const trimmed = editValue.trim();
        setEditingName(false);
        if (trimmed && trimmed !== cameraName && onRename) {
            onRename(cameraId, trimmed);
        }
    };

    const cancelRename = () => {
        setEditingName(false);
        setEditValue(cameraName);
    };

    // Playback segment refs
    const segmentsRef = useRef<PlaybackSegment[]>([]);
    const currentSegUrlRef = useRef<string>('');
    const playlistStartRef = useRef<number>(0);
    const playlistEndRef = useRef<number>(0);
    const lastSeekTimeRef = useRef<number>(0);
    const abortRef = useRef<AbortController | null>(null);
    const scrubDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    // Track the overall time range covered by cached segments
    const segWindowStartRef = useRef<number>(0);
    const segWindowEndRef = useRef<number>(0);

    // ---- PRE-WARM PTZ CONNECTION ----
    useEffect(() => {
        if (hasPTZ && isLive) {
            ptzPrewarm(cameraId);
        }
    }, [hasPTZ, isLive, cameraId]);


    // ---- LIVE MODE EFFECT (WebRTC via WHEP) ----
    const pcRef = useRef<RTCPeerConnection | null>(null);

    useEffect(() => {
        if (!isLive) return;
        const video = videoRef.current;
        if (!video) return;

        setLoading(true);
        setError('');

        let cancelled = false;
        // Track whether we already fell back to sub stream (avoid infinite loop)
        let fellBackToSub = false;

        const connectWebRTC = async (forceSubStream = false) => {
            try {
                // Clean up previous connection
                if (pcRef.current) {
                    pcRef.current.close();
                    pcRef.current = null;
                }

                const pc = new RTCPeerConnection({
                    // No external STUN needed — MediaMTX is on localhost
                });
                pcRef.current = pc;

                // Track whether we received a video track within timeout
                let gotTrack = false;

                // Receive video/audio tracks
                pc.ontrack = (evt) => {
                    if (cancelled) return;
                    gotTrack = true;
                    video.srcObject = evt.streams[0];
                    video.play().catch(() => { });
                    setLoading(false);
                };

                pc.onconnectionstatechange = () => {
                    if (cancelled) return;
                    if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected') {
                        // If we were trying the main stream and it failed, fall back to sub
                        if (useMainStream && !forceSubStream && !fellBackToSub) {
                            console.warn('[WebRTC] Main stream (HD) failed — falling back to sub stream (camera likely uses H.265)');
                            fellBackToSub = true;
                            connectWebRTC(true);
                            return;
                        }
                        setError('Stream disconnected');
                        // Auto-retry after 3 seconds
                        setTimeout(() => {
                            if (!cancelled) {
                                setError('');
                                setLoading(true);
                                connectWebRTC(fellBackToSub);
                            }
                        }, 3000);
                    }
                };

                // Add receive-only transceivers
                pc.addTransceiver('video', { direction: 'recvonly' });
                pc.addTransceiver('audio', { direction: 'recvonly' });

                // Create and set local SDP offer
                const offer = await pc.createOffer();
                await pc.setLocalDescription(offer);

                // Wait for ICE gathering — resolve on first candidate or fast timeout
                await new Promise<void>((resolve) => {
                    if (pc.iceGatheringState === 'complete') {
                        resolve();
                        return;
                    }
                    let resolved = false;
                    const done = () => { if (!resolved) { resolved = true; resolve(); } };
                    // Resolve as soon as we have at least one candidate
                    pc.onicecandidate = (e) => { if (e.candidate) done(); };
                    pc.addEventListener('icegatheringstatechange', () => {
                        if (pc.iceGatheringState === 'complete') done();
                    });
                    // Fast timeout for localhost
                    setTimeout(done, 300);
                });

                // Send SDP offer to MediaMTX WHEP endpoint via our proxy
                // Use main stream unless forced to sub (fallback for H.265 cameras)
                const useSub = forceSubStream || !useMainStream;
                const streamSuffix = useSub ? '_sub' : '';
                const whepUrl = `/webrtc/${cameraId}${streamSuffix}/whep`;
                const resp = await fetch(whepUrl, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/sdp' },
                    body: pc.localDescription!.sdp,
                });

                if (!resp.ok) {
                    // If main stream WHEP failed (e.g. H.265 unsupported), fall back to sub
                    if (useMainStream && !forceSubStream && !fellBackToSub) {
                        console.warn(`[WebRTC] Main stream WHEP returned ${resp.status} — falling back to sub stream`);
                        pc.close();
                        pcRef.current = null;
                        fellBackToSub = true;
                        connectWebRTC(true);
                        return;
                    }
                    throw new Error(`WHEP error: ${resp.status}`);
                }

                const answerSdp = await resp.text();
                await pc.setRemoteDescription({
                    type: 'answer',
                    sdp: answerSdp,
                });

                // For main stream attempts: if no track arrives within 8s, the source
                // is likely H.265 (MediaMTX accepts WHEP but can't relay the codec).
                // Fall back to sub stream.
                if (useMainStream && !forceSubStream && !fellBackToSub) {
                    setTimeout(() => {
                        if (!cancelled && !gotTrack && !fellBackToSub) {
                            console.warn('[WebRTC] Main stream timed out (no track) — falling back to sub stream (likely H.265)');
                            pc.close();
                            pcRef.current = null;
                            fellBackToSub = true;
                            connectWebRTC(true);
                        }
                    }, 8000);
                }

            } catch (err: any) {
                if (!cancelled) {
                    // If main stream failed and we haven't fallen back yet, try sub
                    if (useMainStream && !forceSubStream && !fellBackToSub) {
                        console.warn('[WebRTC] Main stream error — falling back to sub stream:', err?.message);
                        fellBackToSub = true;
                        connectWebRTC(true);
                        return;
                    }
                    console.error('[WebRTC]', err);
                    setError('Stream unavailable');
                    setLoading(false);
                    // Auto-retry after 5 seconds
                    setTimeout(() => {
                        if (!cancelled) {
                            setError('');
                            setLoading(true);
                            connectWebRTC(fellBackToSub);
                        }
                    }, 5000);
                }
            }
        };

        connectWebRTC();

        const updateRes = () => {
            if (video.videoWidth && video.videoHeight) {
                setResolution({ w: video.videoWidth, h: video.videoHeight });
            }
        };
        video.addEventListener('resize', updateRes);
        video.addEventListener('loadedmetadata', updateRes);

        return () => {
            cancelled = true;
            video.removeEventListener('resize', updateRes);
            video.removeEventListener('loadedmetadata', updateRes);
            if (pcRef.current) {
                pcRef.current.close();
                pcRef.current = null;
            }
            if (hlsRef.current) {
                hlsRef.current.destroy();
                hlsRef.current = null;
            }
            video.srcObject = null;
        };
    }, [cameraId, isLive, qualityKey]);

    // ---- PLAYBACK MODE: Direct MP4 segment loading (optimized) ----
    const loadSegmentForTime = useCallback(async (targetMs: number, video: HTMLVideoElement, autoPlay: boolean, suppressLoading = false) => {
        // Cancel any previous in-flight segment fetch
        if (abortRef.current) {
            abortRef.current.abort();
        }
        const controller = new AbortController();
        abortRef.current = controller;

        if (!suppressLoading) {
            setLoading(true);
        }
        setError('');

        try {
            // Check if the target is within our cached segments before fetching
            let segments = segmentsRef.current;
            const inCache = segments.length > 0 &&
                targetMs >= segWindowStartRef.current &&
                targetMs <= segWindowEndRef.current;

            if (!inCache) {
                const t = new Date(targetMs).toISOString();
                segments = await fetchPlaybackSegments(cameraId, t, controller.signal);

                if (controller.signal.aborted) return;

                if (!segments || segments.length === 0) {
                    setError('No recordings found');
                    setLoading(false);
                    return;
                }

                // Cache the fetched segments and their time window
                segmentsRef.current = segments;
                segWindowStartRef.current = new Date(segments[0].start_time).getTime();
                segWindowEndRef.current = new Date(segments[segments.length - 1].end_time).getTime();
            }

            // Find the segment that contains the target time
            let bestIdx = 0;
            for (let i = 0; i < segments.length; i++) {
                const segStart = new Date(segments[i].start_time).getTime();
                const segEnd = new Date(segments[i].end_time).getTime();
                if (targetMs >= segStart && targetMs <= segEnd) {
                    bestIdx = i;
                    break;
                }
                if (segStart <= targetMs) bestIdx = i;
            }

            const seg = segments[bestIdx];
            const segStartMs = new Date(seg.start_time).getTime();
            const segEndMs = new Date(seg.end_time).getTime();

            const needsReload = (seg.url !== currentSegUrlRef.current);

            currentSegUrlRef.current = seg.url;
            playlistStartRef.current = segStartMs;
            playlistEndRef.current = segEndMs;
            lastSeekTimeRef.current = targetMs;

            if (needsReload) {
                video.src = seg.url;
                video.load();

                await new Promise<void>((resolve, reject) => {
                    const onLoaded = () => {
                        video.removeEventListener('error', onError);
                        resolve();
                    };
                    const onError = () => {
                        video.removeEventListener('loadedmetadata', onLoaded);
                        reject(new Error('Failed to load recording'));
                    };
                    const onAbort = () => {
                        video.removeEventListener('loadedmetadata', onLoaded);
                        video.removeEventListener('error', onError);
                        reject(new DOMException('Aborted', 'AbortError'));
                    };
                    controller.signal.addEventListener('abort', onAbort, { once: true });
                    video.addEventListener('loadedmetadata', onLoaded, { once: true });
                    video.addEventListener('error', onError, { once: true });
                });
            }

            if (controller.signal.aborted) return;

            // Seek within the segment to the correct position
            const offsetSec = Math.max(0, (targetMs - segStartMs) / 1000);
            if (isFinite(video.duration) && offsetSec < video.duration) {
                video.currentTime = offsetSec;
            }

            setLoading(false);

            if (autoPlay) {
                video.play().catch(() => { });
                setPaused(false);
            } else {
                video.pause();
                setPaused(true);
            }
        } catch (err: any) {
            if (err?.name === 'AbortError') return;
            setError('Failed to load recordings');
            setLoading(false);
        }
    }, [cameraId]);

    // ---- PLAYBACK: Initial load when switching from live to playback ----
    useEffect(() => {
        if (isLive) return;
        const video = videoRef.current;
        if (!video) return;

        // Clean up any live WebRTC
        if (pcRef.current) {
            pcRef.current.close();
            pcRef.current = null;
        }
        video.srcObject = null;

        // Reset segment tracking
        segmentsRef.current = [];
        currentSegUrlRef.current = '';
        playlistStartRef.current = 0;
        playlistEndRef.current = 0;
        segWindowStartRef.current = 0;
        segWindowEndRef.current = 0;

        const targetMs = playbackTime?.getTime() || Date.now();
        loadSegmentForTime(targetMs, video, false);

        // Auto-advance to next segment when current one ends
        const onEnded = () => {
            const curIdx = segmentsRef.current.findIndex((s: any) => s.url === currentSegUrlRef.current);
            if (curIdx >= 0 && curIdx < segmentsRef.current.length - 1) {
                const nextSeg = segmentsRef.current[curIdx + 1];
                const nextStartMs = new Date(nextSeg.start_time).getTime();
                const nextEndMs = new Date(nextSeg.end_time).getTime();
                currentSegUrlRef.current = nextSeg.url;
                playlistStartRef.current = nextStartMs;
                playlistEndRef.current = nextEndMs;
                video.src = nextSeg.url;
                video.load();
                video.play().catch(() => { });
            }
        };
        video.addEventListener('ended', onEnded);

        const updateRes = () => {
            if (video.videoWidth && video.videoHeight) {
                setResolution({ w: video.videoWidth, h: video.videoHeight });
            }
        };
        video.addEventListener('resize', updateRes);
        video.addEventListener('loadedmetadata', updateRes);

        return () => {
            video.removeEventListener('ended', onEnded);
            video.removeEventListener('resize', updateRes);
            video.removeEventListener('loadedmetadata', updateRes);
            video.src = '';
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [cameraId, isLive]);

    // ---- PLAYBACK SCRUBBING: Seek on playbackTime change ----
    useEffect(() => {
        if (isLive) return;
        const video = videoRef.current;
        if (!video || !playbackTime) return;

        const targetMs = playbackTime.getTime();

        // Skip if same time
        if (targetMs === lastSeekTimeRef.current) return;

        const segStart = playlistStartRef.current;
        const segEnd = playlistEndRef.current;

        // If target is within the currently loaded segment, just seek instantly
        if (segStart > 0 && segEnd > 0 && targetMs >= segStart && targetMs <= segEnd) {
            const offsetSec = (targetMs - segStart) / 1000;
            if (isFinite(video.duration) && offsetSec >= 0 && offsetSec <= video.duration) {
                video.currentTime = offsetSec;
                lastSeekTimeRef.current = targetMs;
                if (video.paused && !globalPaused) {
                    video.play().catch(() => { });
                    setPaused(false);
                }
            }
            return;
        }

        // Target is in a different segment — use cached segments if possible
        if (scrubDebounceRef.current) {
            clearTimeout(scrubDebounceRef.current);
        }
        // Shorter debounce when within cached window (no network needed)
        const inCache = targetMs >= segWindowStartRef.current && targetMs <= segWindowEndRef.current;
        const delay = scrubbing ? (inCache ? 50 : 300) : 0;
        scrubDebounceRef.current = setTimeout(() => {
            scrubDebounceRef.current = null;
            loadSegmentForTime(targetMs, video, true, scrubbing);
        }, delay);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [playbackTime, scrubbing]);

    // --- Playback Controls ---
    const togglePause = () => {
        const video = videoRef.current;
        if (!video) return;
        if (video.paused) {
            video.play().catch(() => { });
            setPaused(false);
        } else {
            video.pause();
            setPaused(true);
        }
    };

    // Respond to global pause/play from the timeline transport bar
    useEffect(() => {
        if (isLive) return; // Only affects playback mode
        const video = videoRef.current;
        if (!video) return;
        if (globalPaused && !video.paused) {
            video.pause();
            setPaused(true);
        } else if (!globalPaused && video.paused) {
            video.play().catch(() => { });
            setPaused(false);
        }
    }, [globalPaused, isLive]);

    const stepFrame = (direction: 1 | -1) => {
        const video = videoRef.current;
        if (!video) return;
        video.pause();
        setPaused(true);
        // Assume ~30fps => each frame is ~0.0333s
        const frameTime = 1 / 30;
        video.currentTime = Math.max(0, video.currentTime + direction * frameTime);
    };

    // --- PTZ Control Handlers ---
    const ptzTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const ptzActiveRef = useRef(false);

    const handlePTZStart = (pan: number, tilt: number, zoom: number) => {
        if (!isLive) return;
        // Cancel any pending stop
        if (ptzTimerRef.current) {
            clearTimeout(ptzTimerRef.current);
            ptzTimerRef.current = null;
        }
        ptzActiveRef.current = true;
        ptzMove(cameraId, pan, tilt, zoom);
    };

    const handlePTZStop = () => {
        if (!isLive || !ptzActiveRef.current) return;
        // Small delay to prevent stop-then-immediately-start on direction changes
        if (ptzTimerRef.current) clearTimeout(ptzTimerRef.current);
        ptzTimerRef.current = setTimeout(() => {
            ptzActiveRef.current = false;
            ptzStop(cameraId);
            ptzTimerRef.current = null;
        }, 50);
    };

    // --- Digital Zoom & Pan Handlers ---
    const handleWheel = (e: React.WheelEvent) => {
        if (!allowZoom) return;
        const zoomDelta = e.deltaY * -0.002;
        setScale((prev) => {
            const newScale = Math.min(Math.max(1, prev + zoomDelta), 10);
            if (newScale === 1) {
                setPan({ x: 0, y: 0 });
            }
            return newScale;
        });
    };

    const handleMouseDown = (e: React.MouseEvent) => {
        if (!allowZoom) return;
        if (scale > 1) {
            setIsDragging(true);
            setDragStart({ x: e.clientX - pan.x, y: e.clientY - pan.y });
        }
    };

    const handleMouseMove = (e: React.MouseEvent) => {
        if (!allowZoom || !isDragging) return;
        if (scale > 1) {
            setPan({
                x: e.clientX - dragStart.x,
                y: e.clientY - dragStart.y,
            });
        }
    };

    const handleMouseUp = () => {
        if (!allowZoom) return;
        setIsDragging(false);
    };

    return (
        <div
            className={`video-cell ${selected ? 'selected' : ''}`}
            onClick={onClick}
            onDoubleClick={onDoubleClick}
            onWheel={handleWheel}
            onMouseDown={handleMouseDown}
            onMouseMove={handleMouseMove}
            onMouseUp={handleMouseUp}
            onMouseLeave={handleMouseUp}
        >
            {/* Header overlay */}
            <div className="video-cell-header">
                {editingName ? (
                    <input
                        ref={nameInputRef}
                        className="video-cell-name-input"
                        value={editValue}
                        onChange={e => setEditValue(e.target.value)}
                        onBlur={commitRename}
                        onKeyDown={e => {
                            if (e.key === 'Enter') commitRename();
                            if (e.key === 'Escape') cancelRename();
                            e.stopPropagation();
                        }}
                        onClick={e => e.stopPropagation()}
                        autoFocus
                        style={{
                            background: 'rgba(0,0,0,0.7)',
                            border: '1px solid var(--accent-orange)',
                            borderRadius: 4,
                            color: '#fff',
                            padding: '2px 6px',
                            fontSize: 12,
                            fontWeight: 600,
                            fontFamily: 'inherit',
                            outline: 'none',
                            width: '50%',
                            minWidth: 80,
                            maxWidth: 200,
                            boxShadow: '0 0 8px rgba(232,115,42,0.3)',
                        }}
                    />
                ) : (
                    <span
                        className="video-cell-name"
                        style={{ pointerEvents: isAdmin && onRename ? 'auto' : 'none', cursor: isAdmin && onRename ? 'text' : 'default' }}
                        onDoubleClick={startEditing}
                        title={isAdmin && onRename ? 'Double-click to rename' : undefined}
                    >
                        {cameraName}
                        {allowZoom && scale > 1 && <span style={{ marginLeft: 6, color: 'var(--accent-amber)' }}>({Math.round(scale * 10) / 10}x)</span>}
                    </span>
                )}
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    {isLive && (
                        <button
                            className={`stream-quality-btn ${useMainStream ? 'hd' : 'sd'}`}
                            onClick={toggleCellQuality}
                            title={useMainStream ? 'Switch to sub-stream (SD)' : 'Switch to main stream (HD)'}
                        >
                            {useMainStream ? 'HD' : 'SD'}
                        </button>
                    )}
                    <span className={`video-cell-badge ${isLive ? 'badge-live' : 'badge-playback'}`} style={{ pointerEvents: 'none' }}>
                        {isLive ? '● LIVE' : '▶ PLAYBACK'}
                    </span>
                    {syncBadge && (
                        <span className="video-cell-badge badge-sync" style={{ pointerEvents: 'none' }}>
                            🔗 SYNC
                        </span>
                    )}
                </div>
            </div>

            {/* Video element */}
            <video
                ref={videoRef}
                autoPlay
                muted
                playsInline
                style={{
                    display: error || loading ? 'none' : 'block',
                    transform: `translate(${pan.x}px, ${pan.y}px) scale(${scale})`,
                    transition: isDragging ? 'none' : 'transform 0.1s ease',
                    cursor: allowZoom && scale > 1 ? (isDragging ? 'grabbing' : 'grab') : 'default'
                }}
            />


            {/* Loading state */}
            {loading && !error && (
                <div className="video-cell-placeholder">
                    <div className="icon">⏳</div>
                    <span style={{ fontSize: 12 }}>{isLive ? 'Connecting...' : 'Loading...'}</span>
                </div>
            )}

            {/* Error state */}
            {error && (
                <div className="video-cell-placeholder">
                    <div className="icon">📹</div>
                    <span style={{ fontSize: 12 }}>{error}</span>
                    <span style={{ fontSize: 10, color: 'var(--text-muted)' }}>
                        {cameraName}
                    </span>
                </div>
            )}

            {/* Stats Overlay */}
            {!error && !loading && (
                <div style={{
                    position: 'absolute',
                    bottom: '8px',
                    left: '8px',
                    background: 'rgba(0, 0, 0, 0.65)',
                    color: 'white',
                    padding: '4px 8px',
                    borderRadius: '4px',
                    fontSize: '11px',
                    fontFamily: 'monospace',
                    pointerEvents: 'none',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '2px',
                    zIndex: 10
                }}>
                    {resolution && (
                        <div>{resolution.w}×{resolution.h}</div>
                    )}
                    {bitrateBps > 0 && (
                        <div style={{ color: 'var(--accent-blue)' }}>
                            {bitrateBps > 1000000
                                ? `${(bitrateBps / 1000000).toFixed(2)} Mbps`
                                : `${(bitrateBps / 1000).toFixed(0)} kbps`}
                        </div>
                    )}
                </div>
            )}

            {/* Playback controls removed — now unified in the Timeline transport bar */}

            {/* PTZ Overlay — only shown for cameras with PTZ capability */}
            {hasPTZ && isLive && !error && !loading && (
                <div style={{
                    position: 'absolute',
                    bottom: '8px',
                    right: '8px',
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    gap: '4px',
                    zIndex: 10,
                    background: 'rgba(0,0,0,0.5)',
                    padding: '6px',
                    borderRadius: '8px',
                }}>
                    <div style={{ display: 'flex', gap: '4px', justifyContent: 'center' }}>
                        {/* Pan left */}
                        <button className="ptz-btn"
                            onMouseDown={() => handlePTZStart(-1, 0, 0)} onMouseUp={handlePTZStop}
                            onTouchStart={() => handlePTZStart(-1, 0, 0)} onTouchEnd={handlePTZStop}
                            onMouseLeave={handlePTZStop}>◀</button>

                        <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                            {/* Tilt up */}
                            <button className="ptz-btn"
                                onMouseDown={() => handlePTZStart(0, 1, 0)} onMouseUp={handlePTZStop}
                                onTouchStart={() => handlePTZStart(0, 1, 0)} onTouchEnd={handlePTZStop}
                                onMouseLeave={handlePTZStop}>▲</button>
                            {/* Tilt down */}
                            <button className="ptz-btn"
                                onMouseDown={() => handlePTZStart(0, -1, 0)} onMouseUp={handlePTZStop}
                                onTouchStart={() => handlePTZStart(0, -1, 0)} onTouchEnd={handlePTZStop}
                                onMouseLeave={handlePTZStop}>▼</button>
                        </div>

                        {/* Pan right */}
                        <button className="ptz-btn"
                            onMouseDown={() => handlePTZStart(1, 0, 0)} onMouseUp={handlePTZStop}
                            onTouchStart={() => handlePTZStart(1, 0, 0)} onTouchEnd={handlePTZStop}
                            onMouseLeave={handlePTZStop}>▶</button>
                    </div>

                    <div style={{ display: 'flex', gap: '4px', width: '100%', marginTop: '4px', borderTop: '1px solid rgba(255,255,255,0.2)', paddingTop: '4px' }}>
                        {/* Zoom out */}
                        <button className="ptz-btn" style={{ flex: 1 }}
                            onMouseDown={() => handlePTZStart(0, 0, -1)} onMouseUp={handlePTZStop}
                            onTouchStart={() => handlePTZStart(0, 0, -1)} onTouchEnd={handlePTZStop}
                            onMouseLeave={handlePTZStop}>-</button>
                        {/* Zoom in */}
                        <button className="ptz-btn" style={{ flex: 1 }}
                            onMouseDown={() => handlePTZStart(0, 0, 1)} onMouseUp={handlePTZStop}
                            onTouchStart={() => handlePTZStart(0, 0, 1)} onTouchEnd={handlePTZStop}
                            onMouseLeave={handlePTZStop}>+</button>
                    </div>
                </div>
            )}
        </div>
    );
}
