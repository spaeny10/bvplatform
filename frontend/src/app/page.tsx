'use client';

import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import {
    Camera, Event, TimelineBucket, Speaker, AudioMessage,
    listCameras, queryEvents, getTimeline, updateCamera,
    getSpeakerInfo, playSpeakerMessage, stopSpeakerPlayback,
} from '@/lib/api';
import VideoPlayer from '@/components/VideoPlayer';
import CameraGrid from '@/components/CameraGrid';
import Timeline from '@/components/Timeline';
import EventListPanel from '@/components/EventListPanel';

import ExportDialog from '@/components/ExportDialog';
import AnalyticsDashboard from '@/components/AnalyticsDashboard';
import MapView from '@/components/MapView';
import { ToastProvider, useToast } from '@/components/ToastProvider';
import { useKeyboardShortcuts } from '@/hooks/useKeyboardShortcuts';
import { useAuth } from '@/contexts/AuthContext';
import { useRouter, useSearchParams } from 'next/navigation';
import Logo from '@/components/shared/Logo';
import UserChip from '@/components/shared/UserChip';
import { getSiteCameras } from '@/lib/ironsight-api';
import { useSite } from '@/hooks/useSites';

type Page = 'live' | 'export' | 'analytics' | 'map';

// Outer shell — just provides the toast context so inner component can useToast
export default function Home() {
    return (
        <ToastProvider>
            <HomeInner />
        </ToastProvider>
    );
}

function HomeInner() {
    const { user } = useAuth();
    const router = useRouter();
    const searchParams = useSearchParams();
    const siteIdParam = searchParams.get('site_id');
    const { data: siteDetail } = useSite(siteIdParam);
    const [page, setPage] = useState<Page>('live');
    const [pageKey, setPageKey] = useState(0); // for re-triggering page transition animation
    const [cameras, setCameras] = useState<Camera[]>([]);
    const [selectedCamera, setSelectedCamera] = useState<string | null>(null);
    const [isLive, setIsLive] = useState(true);
    const [playbackTime, setPlaybackTime] = useState<Date>(new Date());
    const [sysTime, setSysTime] = useState('');
    const prevEventCountRef = useRef(0);
    const [badgeBounce, setBadgeBounce] = useState(false);

    // Page change handler with animation key
    const switchPage = useCallback((p: Page) => {
        setPage(p);
        setPageKey(k => k + 1);
    }, []);

    // Redirect soc_operator to their home; AuthContext + RouteGuard handle
    // unauthenticated redirects (a stale localStorage token check here would
    // mis-fire under header-trust SSO where no token is ever stored).
    useEffect(() => {
        if (user?.role === 'soc_operator') {
            router.replace('/operator');
        }
    }, [router, user]);

    // Live system clock — defer to client only to avoid hydration mismatch
    useEffect(() => {
        setSysTime(new Date().toLocaleTimeString('en-US', { hour12: false }));
        const t = setInterval(() => setSysTime(new Date().toLocaleTimeString('en-US', { hour12: false })), 1000);
        return () => clearInterval(t);
    }, []);
    const [events, setEvents] = useState<Event[]>([]);
    const [timelineBuckets, setTimelineBuckets] = useState<TimelineBucket[]>([]);
    const [eventPanelOpen, setEventPanelOpen] = useState(false);
    const [exportOpen, setExportOpen] = useState(false);
    const [isolatedCamera, setIsolatedCamera] = useState<string | null>(null);
    const [filters, setFilters] = useState<Record<string, boolean>>({
        motion: true,
        lpr: true,
        object: true,
        face: true,
    });
    const [connected, setConnected] = useState(false);
    const [peekCameraId, setPeekCameraId] = useState<string | null>(null);
    const [peekSpeakers, setPeekSpeakers] = useState<Speaker[]>([]);
    const [peekMessages, setPeekMessages] = useState<AudioMessage[]>([]);
    const [peekSelectedSpeaker, setPeekSelectedSpeaker] = useState<string>('');
    const [peekPlaying, setPeekPlaying] = useState(false);
    const [storageConfigured, setStorageConfigured] = useState(true); // assume true until checked
    const [syncPlayback, setSyncPlayback] = useState(true); // synchronized multi-camera playback
    const wsRef = useRef<WebSocket | null>(null);
    const _toast = useToast(); // keep provider mounted

    // Check if storage is configured
    useEffect(() => {
        fetch('/api/storage/status', {
            headers: { 'Authorization': `Bearer ${localStorage.getItem('ironsight_token')}` },
        })
            .then(r => r.ok ? r.json() : null)
            .then(data => { if (data) setStorageConfigured(data.configured); })
            .catch(() => { });
    }, [page]); // re-check when switching pages (in case user just configured storage)

    // Screenshot capture helper — draws the peek-view <video> to a canvas and downloads it
    const screenshotPeek = useCallback(() => {
        const video = document.querySelector<HTMLVideoElement>('.peek-content video');
        if (!video) return;
        const canvas = document.createElement('canvas');
        canvas.width = video.videoWidth || video.clientWidth;
        canvas.height = video.videoHeight || video.clientHeight;
        canvas.getContext('2d')?.drawImage(video, 0, 0);
        const cam = cameras.find(c => c.id === peekCameraId);
        const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
        const a = document.createElement('a');
        a.href = canvas.toDataURL('image/png');
        a.download = `${cam?.name ?? 'camera'}_${ts}.png`;
        a.click();
    }, [cameras, peekCameraId]);

    // Keyboard shortcuts — defined after handleGoLive/handleTimelineSeek below

    // Load cameras — use site-specific cameras when ?site_id= is present
    const loadCameras = useCallback(async () => {
        try {
            if (siteIdParam) {
                const siteCams = await getSiteCameras(siteIdParam);
                // Map SiteCamera → NVR Camera shape for the grid
                const mapped: Camera[] = (siteCams as any[]).map((c: any, idx: number) => ({
                    id: String(c.id),
                    name: c.name ?? `Camera ${idx + 1}`,
                    onvif_address: '',
                    username: '',
                    rtsp_uri: '',
                    sub_stream_uri: '',
                    retention_days: 30,
                    recording: true,
                    recording_mode: 'continuous',
                    pre_buffer_sec: 5,
                    post_buffer_sec: 10,
                    recording_triggers: 'motion',
                    events_enabled: true,
                    audio_enabled: false,
                    camera_group: siteIdParam,
                    schedule: '24/7',
                    privacy_mask: false,
                    status: c.status === 'online' ? 'connected' : 'disconnected',
                    profile_token: '',
                    has_ptz: false,
                    manufacturer: 'IRONSight',
                    model: 'Virtual',
                    firmware: '1.0',
                    created_at: new Date().toISOString(),
                    updated_at: new Date().toISOString(),
                }));
                setCameras(mapped);
                if (mapped.length > 0) setSelectedCamera(prev => prev || mapped[0].id);
            } else {
                const data = await listCameras();
                setCameras(data);
                if (data.length > 0) setSelectedCamera(prev => prev || data[0].id);
            }
        } catch (err) {
            console.error('Failed to load cameras:', err);
        }
    }, [siteIdParam]);

    useEffect(() => {
        loadCameras();
        const interval = setInterval(loadCameras, 10000);
        return () => clearInterval(interval);
    }, [loadCameras]);

    // Escape key closes peek view
    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape' && peekCameraId) {
                setPeekCameraId(null);
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => window.removeEventListener('keydown', handleKeyDown);
    }, [peekCameraId]);

    // Auto-isolate timeline to the peeked camera so events + coverage match
    const previewIsolatedRef = useRef<string | null>(null);
    useEffect(() => {
        if (peekCameraId) {
            // Save current isolation and switch to the peeked camera
            previewIsolatedRef.current = isolatedCamera;
            setIsolatedCamera(peekCameraId);
        } else if (previewIsolatedRef.current !== undefined) {
            // Restore previous isolation when peek closes
            setIsolatedCamera(previewIsolatedRef.current);
            previewIsolatedRef.current = null;
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [peekCameraId]);

    // Load speaker info when peek view opens
    useEffect(() => {
        if (peekCameraId) {
            getSpeakerInfo().then(info => {
                setPeekSpeakers(info.speakers);
                setPeekMessages(info.messages);
                if (info.speakers.length > 0 && !peekSelectedSpeaker) {
                    setPeekSelectedSpeaker(info.speakers[0].id);
                }
            }).catch(() => { });
        }
    }, [peekCameraId]); // eslint-disable-line react-hooks/exhaustive-deps

    // WebSocket for live events (with auto-reconnect)
    useEffect(() => {
        let unmounted = false;
        let reconnectDelay = 3000; // Start at 3s, exponential backoff up to 30s
        let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

        function connect() {
            if (unmounted) return;
            // Same-origin WebSocket — proto follows page scheme (wss when HTTPS),
            // host follows current location so reverse-proxy deployments work
            // without a hardcoded port. Behind NPM/Caddy at /ws.
            const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const ws = new WebSocket(`${proto}//${window.location.host}/ws`);
            wsRef.current = ws;

            ws.onopen = () => {
                setConnected(true);
                reconnectDelay = 3000; // Reset backoff on successful connect
                console.log('[WS] Connected');
            };

            ws.onmessage = (msg) => {
                try {
                    const data = JSON.parse(msg.data);
                    if (data.type === 'event') {
                        const normalized = {
                            ...data,
                            event_type: data.event_type ?? data.event,
                            event_time: data.event_time ?? data.time,
                        };
                        setEvents((prev) => [normalized, ...prev].slice(0, 200));
                    } else if (data.type === 'event_thumbnail') {
                        setEvents((prev) =>
                            prev.map((e) =>
                                e.id === data.event_id
                                    ? { ...e, thumbnail: data.thumbnail }
                                    : e
                            )
                        );
                    }
                } catch (err) {
                    // Ignore parse errors
                }
            };

            ws.onclose = () => {
                setConnected(false);
                wsRef.current = null;
                if (!unmounted) {
                    console.log(`[WS] Disconnected — reconnecting in ${reconnectDelay / 1000}s`);
                    reconnectTimer = setTimeout(() => {
                        reconnectDelay = Math.min(reconnectDelay * 2, 30000); // Exponential backoff, max 30s
                        connect();
                    }, reconnectDelay);
                }
            };

            ws.onerror = () => {
                // Will trigger onclose, which handles reconnect
            };
        }

        connect();

        return () => {
            unmounted = true;
            if (reconnectTimer) clearTimeout(reconnectTimer);
            if (wsRef.current) wsRef.current.close();
        };
    }, []);

    // Compute visible camera IDs for timeline filtering (memoized to prevent re-render loops)
    const visibleCameraIds = useMemo(() => cameras.map((c) => c.id), [cameras]);

    // Load timeline data when time range changes
    const loadTimeline = useCallback(async () => {
        try {
            const end = isLive ? new Date() : playbackTime;
            const start = new Date(end.getTime() - 60 * 60 * 1000); // 1 hour window

            const activeFilters = Object.entries(filters)
                .filter(([_, active]) => active)
                .map(([type]) => type);

            // Use isolated camera if set, otherwise filter to visible cameras
            const timelineCameraIds = isolatedCamera
                ? [isolatedCamera]
                : visibleCameraIds;

            const [buckets, eventData] = await Promise.all([
                getTimeline({
                    start: start.toISOString(),
                    end: end.toISOString(),
                    camera_ids: timelineCameraIds.length > 0 ? timelineCameraIds : undefined,
                    interval: 1,
                }),
                queryEvents({
                    start: start.toISOString(),
                    end: end.toISOString(),
                    camera_id: isolatedCamera || selectedCamera || undefined,
                    types: activeFilters.join(','),
                    limit: 100,
                }),
            ]);

            setTimelineBuckets(buckets);
            setEvents((prev) => {
                // Merge live events with queried events
                const combined = [...prev, ...eventData];
                const unique = Array.from(new Map(combined.map((e) => [e.id || e.event_time, e])).values());
                return unique.sort((a, b) => new Date(b.event_time).getTime() - new Date(a.event_time).getTime()).slice(0, 200);
            });
        } catch (err) {
            console.error('Failed to load timeline:', err);
        }
    }, [isLive, playbackTime, selectedCamera, isolatedCamera, visibleCameraIds, filters]);

    useEffect(() => {
        loadTimeline();
        const interval = setInterval(loadTimeline, isLive ? 30000 : 60000);
        return () => clearInterval(interval);
    }, [loadTimeline, isLive]);

    // Handle timeline scrub
    const [scrubbing, setScrubbing] = useState(false);
    const handleTimelineSeek = useCallback((time: Date) => {
        setIsLive(false);
        setPlaybackTime(time);
    }, []);
    const handleScrubStart = useCallback(() => setScrubbing(true), []);
    const handleScrubEnd = useCallback(() => setScrubbing(false), []);

    // Handle GO LIVE
    const handleGoLive = useCallback(() => {
        setIsLive(true);
        setPlaybackTime(new Date());
    }, []);

    // Handle camera rename (admin only)
    const handleRenameCamera = useCallback(async (cameraId: string, newName: string) => {
        try {
            await updateCamera(cameraId, { name: newName });
            // Update local state immediately for responsiveness
            setCameras(prev => prev.map(c => c.id === cameraId ? { ...c, name: newName } : c));
        } catch (err) {
            console.error('[Rename] Failed:', err);
        }
    }, []);

    // Global playback pause state — controlled from Timeline transport bar
    const [globalPaused, setGlobalPaused] = useState(false);
    const handleTogglePause = useCallback(() => {
        setGlobalPaused(prev => !prev);
    }, []);

    // Keyboard shortcuts — placed here so handleGoLive / handleTimelineSeek are in scope
    const { helpOpen, closeHelp } = useKeyboardShortcuts({
        onGoLive: handleGoLive,
        onSeek: handleTimelineSeek,
        onClosePeek: () => setPeekCameraId(null),
        isLive,
        currentTime: isLive ? new Date() : playbackTime,
        peekOpen: !!peekCameraId,
    });

    // Toggle filter
    const toggleFilter = useCallback((type: string) => {
        setFilters((prev) => ({ ...prev, [type]: !prev[type] }));
    }, []);

    const onlineCameras = cameras.filter((c) => c.status === 'online').length;

    return (
        <div className="app-layout">
            {/* Keyboard shortcut help overlay */}
            {helpOpen && (
                <div
                    onClick={closeHelp}
                    style={{
                        position: 'fixed', inset: 0, zIndex: 9000,
                        background: 'rgba(0,0,0,0.65)', backdropFilter: 'blur(4px)',
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                    }}
                >
                    <div
                        onClick={e => e.stopPropagation()}
                        style={{
                            background: 'rgba(18,20,26,0.98)', border: '1px solid rgba(255,255,255,0.1)',
                            borderRadius: 16, padding: '28px 36px', minWidth: 320,
                            boxShadow: '0 24px 64px rgba(0,0,0,0.6)',
                        }}
                    >
                        <h3 style={{ color: '#f0f4ff', marginBottom: 20, fontSize: 18 }}>⌨️ Keyboard Shortcuts</h3>
                        {[
                            ['Space', 'Toggle live / pause'],
                            ['L', 'Go live'],
                            ['←', 'Seek back 30s (playback)'],
                            ['→', 'Seek forward 30s (playback)'],
                            ['Esc', 'Close modal / help'],
                            ['?', 'Toggle this help panel'],
                        ].map(([key, desc]) => (
                            <div key={key} style={{ display: 'flex', gap: 16, marginBottom: 10, alignItems: 'center' }}>
                                <kbd style={{
                                    background: 'rgba(255,255,255,0.08)', border: '1px solid rgba(255,255,255,0.15)',
                                    borderRadius: 6, padding: '3px 10px', fontSize: 12, fontFamily: 'monospace',
                                    color: '#c0c8de', minWidth: 40, textAlign: 'center', flexShrink: 0,
                                }}>{key}</kbd>
                                <span style={{ color: '#9ca3bb', fontSize: 14 }}>{desc}</span>
                            </div>
                        ))}
                        <button
                            onClick={closeHelp}
                            style={{ marginTop: 16, background: 'rgba(255,255,255,0.08)', border: '1px solid rgba(255,255,255,0.15)', color: '#f0f4ff', borderRadius: 8, padding: '6px 20px', cursor: 'pointer', fontSize: 13 }}
                        >Close</button>
                    </div>
                </div>
            )}
            {/* Navigation Bar */}
            <nav className="navbar">
                <div className="navbar-brand" style={{ gap: 12 }}>
                    <Logo height={22} />
                    {siteIdParam && siteDetail && (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginLeft: 4 }}>
                            <span style={{ width: 1, height: 18, background: 'var(--border-color)' }} />
                            <span style={{
                                fontSize: 11, fontWeight: 600, color: '#3B82F6',
                                padding: '2px 8px', borderRadius: 4,
                                background: 'rgba(59,130,246,0.1)',
                                border: '1px solid rgba(59,130,246,0.25)',
                                display: 'flex', alignItems: 'center', gap: 5,
                            }}>
                                📍 {siteDetail.name}
                                <button
                                    onClick={() => router.push('/')}
                                    title="Clear site filter"
                                    style={{
                                        background: 'none', border: 'none', cursor: 'pointer',
                                        color: 'rgba(59,130,246,0.5)', fontSize: 12, padding: '0 0 0 2px',
                                        fontFamily: 'inherit', lineHeight: 1,
                                    }}
                                >✕</button>
                            </span>
                        </div>
                    )}
                </div>

                <div className="navbar-nav">
                    <button
                        className={`nav-link ${page === 'live' ? 'active' : ''}`}
                        onClick={() => switchPage('live')}
                    >
                        Monitor
                    </button>

                    <button
                        className={`nav-link`}
                        onClick={() => setExportOpen(true)}
                    >
                        Export
                    </button>
                    <button
                        className={`nav-link`}
                        onClick={() => setEventPanelOpen(!eventPanelOpen)}
                    >
                        Alert Feed {events.length > 0 && <span className={badgeBounce ? 'alert-badge-bounce' : ''} onAnimationEnd={() => setBadgeBounce(false)}>({events.length})</span>}
                    </button>
                    <button
                        className={`nav-link ${page === 'analytics' ? 'active' : ''}`}
                        onClick={() => switchPage('analytics')}
                    >
                        Analytics
                    </button>
                    <button
                        className={`nav-link ${page === 'map' ? 'active' : ''}`}
                        onClick={() => switchPage('map')}
                    >
                        Map
                    </button>
                    {/* Ironsight Interfaces */}
                    <span style={{ width: 1, height: 20, background: 'var(--border-color)', margin: '0 4px' }} />
                    <a href="/operator" className="nav-link" style={{ textDecoration: 'none' }}>
                        SOC Monitor
                    </a>
                    <a href="/portal" className="nav-link" style={{ textDecoration: 'none' }}>
                        📊 Portal
                    </a>
                    <a href="/search" className="nav-link" style={{ textDecoration: 'none' }}>
                        🔍 Search
                    </a>
                </div>

                <div className="navbar-status" style={{ gap: 8 }}>
                    {/* Online/Degraded camera status pills */}
                    <span className="status-pill status-pill-online">{onlineCameras} Online</span>
                    {cameras.length - onlineCameras > 0 && (
                        <span className="status-pill status-pill-degraded">{cameras.length - onlineCameras} Degraded</span>
                    )}
                    {events.filter(e => e.event_type === 'violation').length > 0 && (
                        <span className="status-pill status-pill-critical">{events.filter(e => e.event_type === 'violation').length} Critical</span>
                    )}
                    {/* Sync playback toggle */}
                    <button
                        className={`btn-sync ${syncPlayback ? 'active' : ''}`}
                        onClick={() => setSyncPlayback(v => !v)}
                        title={syncPlayback ? 'Sync playback ON — all cameras follow timeline' : 'Sync playback OFF — cameras play independently'}
                    >
                        {syncPlayback ? '🔗 Sync ON' : '🔗 Sync OFF'}
                    </button>
                    {/* Live sys-clock */}
                    <span className="sys-clock" style={{ borderLeft: '1px solid var(--border-color)', paddingLeft: 12, marginLeft: 4 }}>{sysTime}</span>
                    {/* Keyboard shortcut hint */}
                    <span style={{ color: 'var(--text-muted)', fontSize: 11, cursor: 'pointer' }}
                        title="Keyboard shortcuts" onClick={() => closeHelp()}>
                        <kbd style={{ background: 'rgba(57,211,83,0.08)', border: '1px solid var(--border-color)', borderRadius: 2, padding: '1px 5px', fontSize: 10, fontFamily: 'var(--font-mono)' }}>?</kbd>
                    </span>
                    <UserChip />
                </div>
            </nav>

            {/* Storage Warning Banner */}
            {!storageConfigured && (
                <div style={{
                    background: 'linear-gradient(90deg, rgba(245,158,11,0.15), rgba(245,158,11,0.08))',
                    border: '1px solid rgba(245,158,11,0.4)',
                    borderRadius: 8,
                    padding: '10px 20px',
                    margin: '8px 12px 0 12px',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 12,
                    fontSize: 13,
                    color: '#fbbf24',
                }}>
                    <span style={{ fontSize: 18 }}>⚠️</span>
                    <span style={{ flex: 1 }}>
                        <strong>No storage location configured</strong> — Recordings are disabled.
                        Add a storage location in Settings to enable recording.
                    </span>
                    <a
                        href="/admin"
                        style={{
                            background: 'rgba(245,158,11,0.25)',
                            border: '1px solid rgba(245,158,11,0.5)',
                            color: '#fbbf24',
                            borderRadius: 6,
                            padding: '5px 14px',
                            cursor: 'pointer',
                            fontSize: 12,
                            fontWeight: 600,
                            whiteSpace: 'nowrap',
                            textDecoration: 'none',
                        }}
                    >
                        ⚙ Configure Storage
                    </a>
                </div>
            )}

            {/* Main Content */}
            <div className="main-content">
                {page === 'live' && (
                    <>
                        {/* Video Grid */}
                        <div className="video-grid-container">
                            <CameraGrid
                                cameras={cameras}
                                selectedCamera={selectedCamera}
                                isLive={isLive}
                                playbackTime={playbackTime}
                                onSelectCamera={setSelectedCamera}
                                onPeekCamera={setPeekCameraId}
                                syncPlayback={syncPlayback}
                                scrubbing={scrubbing}
                                isAdmin={user?.role === 'admin'}
                                onRenameCamera={handleRenameCamera}
                                globalPaused={globalPaused}
                            />
                        </div>

                        {/* Timeline */}
                        <Timeline
                            buckets={timelineBuckets}
                            isLive={isLive}
                            currentTime={isLive ? new Date() : playbackTime}
                            filters={filters}
                            onSeek={handleTimelineSeek}
                            onGoLive={handleGoLive}
                            onToggleFilter={toggleFilter}
                            cameras={cameras.filter((c) => visibleCameraIds.includes(c.id))}
                            isolatedCamera={isolatedCamera}
                            onIsolateCamera={setIsolatedCamera}
                            events={events}
                            onScrubStart={handleScrubStart}
                            onScrubEnd={handleScrubEnd}
                            globalPaused={globalPaused}
                            onTogglePause={handleTogglePause}
                        />
                    </>
                )}



                {page === 'analytics' && (
                    <div key={pageKey} className="page-transition-enter" style={{ padding: '16px 20px', maxWidth: 1200, margin: '0 auto' }}>
                        <h2 style={{ fontSize: 18, fontWeight: 700, marginBottom: 16 }}>📈 Video Analytics</h2>
                        <AnalyticsDashboard />
                    </div>
                )}

                {page === 'map' && (
                    <div key={pageKey} className="page-transition-enter" style={{ padding: '16px 20px', maxWidth: 1200, margin: '0 auto' }}>
                        <h2 style={{ fontSize: 18, fontWeight: 700, marginBottom: 16 }}>🗺️ Site Map</h2>
                        <MapView cameras={cameras} onCameraClick={(id) => { setPeekCameraId(id); switchPage('live'); }} />
                    </div>
                )}
            </div>

            {/* Event List Panel (Slide-out) */}
            <EventListPanel
                cameras={cameras}
                liveEvents={events}
                open={eventPanelOpen}
                onClose={() => setEventPanelOpen(false)}
                onEventClick={(event) => {
                    handleTimelineSeek(new Date(event.event_time));
                    if (event.camera_id) {
                        setSelectedCamera(event.camera_id);
                    }
                }}
            />

            {/* Export Dialog */}
            {exportOpen && (
                <ExportDialog
                    cameras={cameras}
                    onClose={() => setExportOpen(false)}
                />
            )}

            {/* Peek View Modal */}
            {peekCameraId && (() => {
                const peekCamera = cameras.find((c) => c.id === peekCameraId);
                if (!peekCamera) return null;
                return (
                    <div className="peek-overlay" onClick={() => setPeekCameraId(null)}>
                        <div className="peek-content" onClick={(e) => e.stopPropagation()} onDoubleClick={() => setPeekCameraId(null)}>
                            {/* Top-right controls */}
                            <div style={{
                                position: 'absolute', top: 12, right: 12, zIndex: 10,
                                display: 'flex', alignItems: 'center', gap: 6,
                            }}>
                                <button
                                    onClick={screenshotPeek}
                                    title="Save screenshot (PNG)"
                                    style={{
                                        background: 'rgba(0,0,0,0.55)', border: '1px solid rgba(255,255,255,0.2)',
                                        color: '#fff', borderRadius: '6px', padding: '4px 10px',
                                        fontSize: '12px', cursor: 'pointer', backdropFilter: 'blur(4px)',
                                        whiteSpace: 'nowrap',
                                    }}
                                >
                                    📷 Save
                                </button>
                                <button
                                    onClick={() => setPeekCameraId(null)}
                                    title="Close"
                                    style={{
                                        width: 32, height: 32, borderRadius: '50%',
                                        border: '1px solid rgba(255,255,255,0.2)',
                                        background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(8px)',
                                        color: '#fff', cursor: 'pointer', display: 'flex',
                                        alignItems: 'center', justifyContent: 'center', fontSize: 16,
                                    }}
                                >✕</button>
                            </div>
                            <VideoPlayer
                                cameraId={peekCamera.id}
                                cameraName={peekCamera.name}
                                isLive={isLive}
                                playbackTime={playbackTime}
                                hasPTZ={peekCamera.has_ptz}
                                allowZoom={true}
                                streamQuality="high"
                                wsRef={wsRef}
                                globalPaused={globalPaused}
                            />
                            {/* Speaker Talk-Down Bar */}
                            {peekSpeakers.length > 0 && (
                                <div style={{
                                    display: 'flex', alignItems: 'center', gap: 8,
                                    padding: '8px 14px',
                                    background: 'rgba(0,0,0,0.7)',
                                    borderTop: '1px solid rgba(255,255,255,0.1)',
                                    backdropFilter: 'blur(8px)',
                                    fontSize: 12,
                                }}>
                                    <span style={{ fontSize: 16 }}>🔊</span>
                                    <select
                                        value={peekSelectedSpeaker}
                                        onChange={e => setPeekSelectedSpeaker(e.target.value)}
                                        style={{
                                            background: 'rgba(255,255,255,0.1)', border: '1px solid rgba(255,255,255,0.15)',
                                            color: '#fff', borderRadius: 4, padding: '3px 8px', fontSize: 11,
                                        }}
                                    >
                                        {peekSpeakers.map(s => (
                                            <option key={s.id} value={s.id}>{s.name}{s.zone ? ` (${s.zone})` : ''}</option>
                                        ))}
                                    </select>
                                    <div style={{ display: 'flex', gap: 4, flex: 1, flexWrap: 'wrap' }}>
                                        {peekMessages.map(msg => {
                                            const catColors: Record<string, string> = {
                                                warning: '#f59e0b', info: '#3b82f6', emergency: '#ef4444', custom: '#8b5cf6',
                                            };
                                            return (
                                                <button
                                                    key={msg.id}
                                                    onClick={async () => {
                                                        if (peekSelectedSpeaker) {
                                                            setPeekPlaying(true);
                                                            try {
                                                                await playSpeakerMessage(peekSelectedSpeaker, msg.id);
                                                                setTimeout(() => setPeekPlaying(false), (msg.duration || 5) * 1000);
                                                            } catch { setPeekPlaying(false); }
                                                        }
                                                    }}
                                                    disabled={peekPlaying || !peekSelectedSpeaker}
                                                    style={{
                                                        background: 'rgba(255,255,255,0.08)',
                                                        border: `1px solid ${catColors[msg.category] || '#666'}`,
                                                        color: '#fff', borderRadius: 4, padding: '3px 10px',
                                                        fontSize: 11, cursor: 'pointer', whiteSpace: 'nowrap',
                                                        opacity: peekPlaying ? 0.5 : 1,
                                                    }}
                                                    title={`Play: ${msg.name} (${msg.duration?.toFixed(1)}s)`}
                                                >
                                                    {msg.name}
                                                </button>
                                            );
                                        })}
                                    </div>
                                    {peekPlaying && (
                                        <button
                                            onClick={async () => { await stopSpeakerPlayback(); setPeekPlaying(false); }}
                                            style={{
                                                background: '#ef4444', border: 'none', color: '#fff',
                                                borderRadius: 4, padding: '3px 10px', fontSize: 11, cursor: 'pointer',
                                            }}
                                        >■ Stop</button>
                                    )}
                                    {peekPlaying && (
                                        <span style={{ color: '#22c55e', fontWeight: 600, fontSize: 11 }}>🔊 Playing…</span>
                                    )}
                                </div>
                            )}
                            <div className="peek-camera-name">{peekCamera.name}</div>
                            <button
                                onClick={() => {
                                    window.open(`/popout/${peekCameraId}`, `popout_${peekCameraId}`,
                                        'width=960,height=600,menubar=no,toolbar=no');
                                }}
                                style={{
                                    background: 'rgba(255,255,255,0.1)', border: '1px solid rgba(255,255,255,0.2)',
                                    color: '#fff', borderRadius: 4, padding: '4px 10px', fontSize: 11,
                                    cursor: 'pointer', marginLeft: 8,
                                }}
                                title="Open in separate window for multi-monitor setup"
                            >
                                ↗ Pop Out
                            </button>
                        </div>
                    </div>
                );
            })()}
        </div>
    );
}
