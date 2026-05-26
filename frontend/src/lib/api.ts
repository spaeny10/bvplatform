// getStoredToken import is kept during the PR1+PR2 migration window for
// any transitive consumers. It is a no-op stub now — delete in PR 3.
import { getStoredToken } from '@/contexts/AuthContext';

const API_BASE = '/api';

/**
 * Read the ironsight_csrf cookie value from document.cookie.
 * Returns an empty string when running server-side (no document) or when
 * the cookie is absent (e.g. pre-login, SSO-only session).
 * Never throws — callers treat an empty string as "no CSRF token available".
 *
 * P1-A-02 part 2 — double-submit CSRF pattern. The backend CSRFMiddleware
 * compares this value against the X-CSRF-Token request header.
 */
function getCSRFToken(): string {
    if (typeof document === 'undefined') return '';
    const pair = document.cookie.split('; ').find(r => r.startsWith('ironsight_csrf='));
    return pair ? pair.split('=')[1] : '';
}

/**
 * Fetch wrapper that:
 *  - attaches credentials:'include' so the ironsight_session HttpOnly cookie
 *    is sent automatically on every request
 *  - injects X-CSRF-Token from the ironsight_csrf cookie on non-idempotent
 *    requests (POST / PUT / PATCH / DELETE)
 *  - redirects to /login on 401
 *
 * The Authorization: Bearer path is intentionally removed post-P1-A-02-part2.
 * The getStoredToken() import above is a dead stub kept for import compat.
 *
 * P1-A-02 part 2 — replaces the localStorage-based token injection.
 */
export async function authFetch(input: RequestInfo, init: RequestInit = {}): Promise<Response> {
    // getStoredToken always returns null now — call kept for import compat only.
    void getStoredToken;
    const headers = new Headers(init.headers ?? {});
    const method = (init.method ?? 'GET').toUpperCase();
    // Inject CSRF header on state-changing requests. GET / HEAD / OPTIONS
    // are exempt (mirrors the backend CSRFMiddleware exemption list).
    if (method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS') {
        const csrf = getCSRFToken();
        if (csrf) headers.set('X-CSRF-Token', csrf);
    }
    const res = await fetch(input, { ...init, headers, credentials: 'include' });
    if (res.status === 401 && typeof window !== 'undefined') {
        window.location.href = '/login';
    }
    return res;
}

export interface Camera {
    id: string;
    name: string;
    onvif_address: string;
    username: string;
    rtsp_uri: string;
    sub_stream_uri: string;
    retention_days: number;
    recording: boolean;
    recording_mode: string;
    pre_buffer_sec: number;
    post_buffer_sec: number;
    recording_triggers: string;
    events_enabled: boolean;
    audio_enabled: boolean;
    camera_group: string;
    schedule: string;
    privacy_mask: boolean;
    status: string;
    profile_token: string;
    has_ptz: boolean;
    manufacturer: string;
    model: string;
    firmware: string;
    // 'continuous' = traditional RTSP+ONVIF (default).
    // 'sense_pushed' = inbound-webhook only (Milesight SC4xx Sense series).
    device_class?: 'continuous' | 'sense_pushed';
    // Per-camera webhook token. Only present in the response from
    // createCamera for sense_pushed cameras — list endpoints omit it.
    sense_webhook_token?: string;
    created_at: string;
    updated_at: string;
}

export interface Event {
    id: number;
    camera_id: string;
    event_time: string;
    event_type: string;
    details: Record<string, any>;
    thumbnail: string;
}

export interface TimelineBucket {
    bucket_time: string;
    counts: Record<string, number>;
    total: number;
}

export interface Segment {
    id: number;
    camera_id: string;
    start_time: string;
    end_time: string;
    file_path: string;
    file_size: number;
    duration_ms: number;
}

export interface ExportJob {
    id: string;
    camera_id: string;
    start_time: string;
    end_time: string;
    status: string;
    file_path: string;
    file_size: number;
    error: string;
    created_at: string;
    completed_at: string | null;
}

export interface DiscoveredDevice {
    address: string;
    name: string;
    manufacturer: string;
    model: string;
    xaddr: string;
}

// fireDeterrence activates one of the camera's relay outputs (strobe, siren,
// both, or generic alarm_out). Requires operator or site-manager role on the
// backend; the button should be hidden from customer/viewer roles in the UI
// to avoid an obviously-dead button.
export interface DeterrenceResponse {
    ok: boolean;
    action: string;
    camera_id: string;
    camera_name: string;
    duration_sec: number;
    fired_at: number;
    relay_tokens: string[];
    message?: string;
}

export async function fireDeterrence(
    cameraID: string,
    action: 'strobe' | 'siren' | 'both' | 'alarm_out',
    opts?: { durationSec?: number; reason?: string; alarmID?: string },
): Promise<DeterrenceResponse> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/deterrence`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            action,
            duration_sec: opts?.durationSec ?? 10,
            reason: opts?.reason ?? '',
            alarm_id: opts?.alarmID ?? '',
        }),
    });
    if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `fire ${action} failed (HTTP ${res.status})`);
    }
    return res.json();
}

// Historical event returned by GET /api/events and /api/search/events.
// playback_url is populated at query time by JOINing segments — ready to
// drop into an <a href> or <video src>.
export interface HistoricalEvent {
    id: number;
    camera_id: string;
    event_time: string;
    event_type: string;
    details: Record<string, unknown>;
    thumbnail?: string;
    segment_id?: number;
    playback_url?: string;
}

export interface SearchEventsResponse {
    events: HistoricalEvent[];
    next_offset: number;
    has_more: boolean;
    authorized_cameras?: string[];
    restricted: boolean;
}

// searchEvents hits the unified history endpoint. Returns events (filtered
// server-side by RBAC) with playback_url already resolved.
export async function searchEvents(params: {
    start?: string;
    end?: string;
    camera_id?: string;
    types?: string[];
    search?: string;
    limit?: number;
    offset?: number;
}): Promise<SearchEventsResponse> {
    const qs = new URLSearchParams();
    if (params.start) qs.set('start', params.start);
    if (params.end) qs.set('end', params.end);
    if (params.camera_id) qs.set('camera_id', params.camera_id);
    if (params.types && params.types.length) qs.set('types', params.types.join(','));
    if (params.search) qs.set('search', params.search);
    if (params.limit != null) qs.set('limit', String(params.limit));
    if (params.offset != null) qs.set('offset', String(params.offset));
    try {
        const res = await authFetch(`${API_BASE}/search/events?${qs.toString()}`);
        if (!res.ok) return { events: [], next_offset: 0, has_more: false, restricted: false };
        return res.json();
    } catch {
        return { events: [], next_offset: 0, has_more: false, restricted: false };
    }
}

// exportEvidenceURL returns the URL for the /events/{id}/export endpoint.
// Call with an <a> download attribute so the browser saves the zip. Query
// params tune the clip window; defaults are 5s pre + 10s post.
export function exportEvidenceURL(eventID: number, preSec?: number, postSec?: number): string {
    const qs = new URLSearchParams();
    if (preSec != null) qs.set('pre', String(preSec));
    if (postSec != null) qs.set('post', String(postSec));
    const qStr = qs.toString();
    return `${API_BASE}/events/${eventID}/export${qStr ? '?' + qStr : ''}`;
}

// One match from GET /api/search/semantic — a segment whose VLM-generated
// description/tags matched the user's query. playback_url lands right on
// the segment so the UI can render an inline <video>.
export interface SemanticMatch {
    segment_id: number;
    camera_id: string;
    camera_name: string;
    start_time: string;
    end_time: string;
    description: string;
    tags: string[];
    activity_level: string;
    playback_url: string;
    rank: number;
}

export interface SemanticSearchResponse {
    query: string;
    results: SemanticMatch[];
    total: number;
    next_offset: number;
    has_more: boolean;
    restricted: boolean;
    authorized_cameras?: string[];
}

// searchSemantic runs a natural-language keyword search over VLM-generated
// segment descriptions (populated during idle hours by the indexer).
export async function searchSemantic(params: {
    q: string;
    start?: string;
    end?: string;
    camera_id?: string;
    activity?: 'low' | 'moderate' | 'high';
    limit?: number;
    offset?: number;
}): Promise<SemanticSearchResponse> {
    const qs = new URLSearchParams();
    qs.set('q', params.q);
    if (params.start) qs.set('start', params.start);
    if (params.end) qs.set('end', params.end);
    if (params.camera_id) qs.set('camera_id', params.camera_id);
    if (params.activity) qs.set('activity', params.activity);
    if (params.limit != null) qs.set('limit', String(params.limit));
    if (params.offset != null) qs.set('offset', String(params.offset));
    try {
        const res = await authFetch(`${API_BASE}/search/semantic?${qs.toString()}`);
        if (!res.ok) return { query: params.q, results: [], total: 0, next_offset: 0, has_more: false, restricted: false };
        return res.json();
    } catch {
        return { query: params.q, results: [], total: 0, next_offset: 0, has_more: false, restricted: false };
    }
}

// Recording-health snapshot returned by GET /api/recording/health.
// One entry per camera the caller is authorized to see.
export interface RecordingHealth {
    camera_id: string;
    camera_name: string;
    site_id?: string;
    recording: boolean;
    recorder_type: 'ffmpeg' | 'gort' | 'off';
    segments_24h: number;
    bytes_24h: number;
    last_segment_at?: string;
    last_gap_seconds: number;
    longest_gap_seconds_24h: number;
    status: 'healthy' | 'degraded' | 'stale' | 'off' | 'unknown';
}

export async function getRecordingHealth(): Promise<RecordingHealth[]> {
    try {
        const res = await authFetch(`${API_BASE}/recording/health`);
        if (!res.ok) return [];
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    } catch { return []; }
}

// Liveness of the four services that don't show up in camera health:
// the two GPU AI services, mediamtx, and the worker process.
export interface ServiceStatus {
    name: string;
    status: 'up' | 'down' | 'degraded' | 'unknown';
    detail: string;
    endpoint?: string;
    response_ms: number;
    last_checked: string;
    // Populated only for AI services and only when the Python /health
    // returned them (pynvml available + CUDA reachable).
    gpu_util_pct?: number;
    gpu_memory_used_mb?: number;
    gpu_memory_total_mb?: number;
    gpu_temperature_c?: number;
}

// AIStats is the cumulative-since-process-start runtime counter from the
// AI pipeline. The funnel is YOLO calls → YOLO confirmed (had detections)
// → Qwen calls → Qwen confirmed (actionable threat). "Filtered" is the
// drop at each stage and is what justifies the AI spend in the customer
// transparency UI.
export interface AIStats {
    yolo_calls: number;
    yolo_confirmed: number;
    yolo_filtered: number;
    yolo_avg_ms: number;
    qwen_calls: number;
    qwen_confirmed: number;
    qwen_filtered: number;
    qwen_avg_ms: number;
}

export interface ServicesHealthPayload {
    services: ServiceStatus[];
    ai_stats?: AIStats;
}

export async function getServicesHealth(): Promise<ServicesHealthPayload> {
    try {
        const res = await authFetch(`${API_BASE}/system/services`);
        if (!res.ok) return { services: [] };
        const data = await res.json();
        return {
            services: Array.isArray(data?.services) ? data.services : [],
            ai_stats: data?.ai_stats,
        };
    } catch { return { services: [] }; }
}

// One row from the ai_runtime_metrics hypertable. Deltas are per-window
// (sample-to-sample); GPU fields are point-in-time absolutes.
export interface AIMetricSample {
    ts: string;
    gpu_util_pct?: number;
    gpu_memory_used_mb?: number;
    gpu_memory_total_mb?: number;
    gpu_temperature_c?: number;
    calls_delta: number;
    confirmed_delta: number;
    filtered_delta: number;
    avg_inference_ms?: number;
}

export interface AIMetricsTimeseries {
    window_minutes: number;
    series: { yolo: AIMetricSample[]; qwen: AIMetricSample[] };
}

// Per-site usage row for the "GPU rental bill" view. yolo_/qwen_ counts
// are summed over the window; estimated_cost is computed server-side
// from the cost-per-1k params the client passed in.
export interface SiteUsageRow {
    site_id: string;
    site_name: string;
    yolo_calls: number;
    yolo_confirmed: number;
    yolo_filtered: number;
    qwen_calls: number;
    qwen_confirmed: number;
    qwen_filtered: number;
    estimated_cost: number;
}

export interface AIUsageBySite {
    window_days: number;
    cost_per_1k_yolo: number;
    cost_per_1k_qwen: number;
    total_cost: number;
    sites: SiteUsageRow[];
}

export async function getAIUsageBySite(days = 7, costPer1kYolo = 0.05, costPer1kQwen = 0.50): Promise<AIUsageBySite> {
    try {
        const url = `${API_BASE}/system/services/usage?days=${days}&cost_per_1k_yolo=${costPer1kYolo}&cost_per_1k_qwen=${costPer1kQwen}`;
        const res = await authFetch(url);
        if (!res.ok) return { window_days: days, cost_per_1k_yolo: costPer1kYolo, cost_per_1k_qwen: costPer1kQwen, total_cost: 0, sites: [] };
        const data = await res.json();
        return {
            window_days: data?.window_days ?? days,
            cost_per_1k_yolo: data?.cost_per_1k_yolo ?? costPer1kYolo,
            cost_per_1k_qwen: data?.cost_per_1k_qwen ?? costPer1kQwen,
            total_cost: data?.total_cost ?? 0,
            sites: Array.isArray(data?.sites) ? data.sites : [],
        };
    } catch {
        return { window_days: days, cost_per_1k_yolo: costPer1kYolo, cost_per_1k_qwen: costPer1kQwen, total_cost: 0, sites: [] };
    }
}

export async function getAIMetricsTimeseries(minutes = 60): Promise<AIMetricsTimeseries> {
    try {
        const res = await authFetch(`${API_BASE}/system/services/timeseries?minutes=${minutes}`);
        if (!res.ok) return { window_minutes: minutes, series: { yolo: [], qwen: [] } };
        const data = await res.json();
        return {
            window_minutes: data?.window_minutes ?? minutes,
            series: {
                yolo: Array.isArray(data?.series?.yolo) ? data.series.yolo : [],
                qwen: Array.isArray(data?.series?.qwen) ? data.series.qwen : [],
            },
        };
    } catch { return { window_minutes: minutes, series: { yolo: [], qwen: [] } }; }
}

export interface SDStatus {
    camera_id: string;
    camera_name: string;
    reachable: boolean;
    error?: string;
    present: boolean;
    storage_type?: string;
    recording_count: number;
    data_from?: string;
    data_until?: string;
    total_bytes?: number;
    used_bytes?: number;
    free_bytes?: number;
    source?: 'onvif' | 'milesight' | 'none';
    status: 'ok' | 'no_data' | 'no_card' | 'unreachable';
}

export async function getSDStatus(cameraID: string): Promise<SDStatus | null> {
    try {
        const res = await authFetch(`${API_BASE}/cameras/${cameraID}/sd/status`);
        if (!res.ok) return null;
        return await res.json();
    } catch { return null; }
}

// Camera API
export async function listCameras(): Promise<Camera[]> {
    try {
        const res = await authFetch(`${API_BASE}/cameras`);
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    } catch { return []; }
}

export async function getCamera(id: string): Promise<Camera> {
    const res = await authFetch(`${API_BASE}/cameras/${id}`);
    return res.json();
}

export async function createCamera(data: { name: string; onvif_address: string; username: string; password: string; device_class?: 'continuous' | 'sense_pushed' }): Promise<Camera> {
    const res = await authFetch(`${API_BASE}/cameras`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) {
        const text = await res.text();
        throw new Error(text || `Failed to add camera (HTTP ${res.status})`);
    }
    return res.json();
}

export async function updateCamera(id: string, data: Partial<Pick<Camera,
    'name' | 'onvif_address' | 'rtsp_uri' | 'sub_stream_uri' | 'username' |
    'retention_days' | 'recording' | 'recording_mode' |
    'pre_buffer_sec' | 'post_buffer_sec' | 'recording_triggers' |
    'events_enabled' | 'audio_enabled' | 'camera_group' | 'schedule' | 'privacy_mask'
>>): Promise<Camera> {
    const res = await authFetch(`${API_BASE}/cameras/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    return res.json();
}

export async function deleteCamera(id: string): Promise<void> {
    await authFetch(`${API_BASE}/cameras/${id}`, { method: 'DELETE' });
}

// rebootCamera issues an ONVIF SystemReboot. Camera will be unreachable
// for ~30–90s while it restarts. Admin / supervisor only on the server.
export async function rebootCamera(id: string): Promise<{ ok: boolean; message: string }> {
    const res = await authFetch(`${API_BASE}/cameras/${id}/reboot`, { method: 'POST' });
    if (!res.ok) {
        const text = await res.text().catch(() => res.statusText);
        throw new Error(text || `HTTP ${res.status}`);
    }
    return res.json();
}

// ── VCA (Video Content Analytics) Rules ──

export interface VCAPoint { x: number; y: number; }

export interface VCARule {
    id: string;
    camera_id: string;
    rule_type: 'intrusion' | 'linecross' | 'regionentrance' | 'loitering';
    name: string;
    enabled: boolean;
    sensitivity: number;
    region: VCAPoint[];
    direction: 'both' | 'left_to_right' | 'right_to_left';
    threshold_sec: number;
    schedule: string;
    actions: string[];
    synced: boolean;
    sync_error: string;
    created_at: string;
    updated_at: string;
}

export type VCARuleCreate = Omit<VCARule, 'id' | 'camera_id' | 'synced' | 'sync_error' | 'created_at' | 'updated_at'>;

export async function listVCARules(cameraId: string): Promise<VCARule[]> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/vca/rules`);
    if (!res.ok) return [];
    return res.json();
}

export async function createVCARule(cameraId: string, data: VCARuleCreate): Promise<VCARule> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/vca/rules`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function updateVCARule(cameraId: string, ruleId: string, data: VCARuleCreate): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/vca/rules/${ruleId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
}

export async function deleteVCARule(cameraId: string, ruleId: string): Promise<void> {
    await authFetch(`${API_BASE}/cameras/${cameraId}/vca/rules/${ruleId}`, { method: 'DELETE' });
}

export async function syncVCARules(cameraId: string): Promise<{ synced: number; errors: number }> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/vca/sync`, { method: 'POST' });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export function getVCASnapshotURL(cameraId: string): string {
    return `${API_BASE}/cameras/${cameraId}/vca/snapshot`;
}

// Camera management
export async function discoverCameras(): Promise<DiscoveredDevice[]> {
    const res = await authFetch(`${API_BASE}/discover`, { method: 'POST' });
    if (!res.ok) throw new Error('Failed to discover cameras');
    const data = await res.json();
    return data || [];
}

export async function getDevicePreview(address: string, auth: { username: string, password: string }): Promise<string> {
    const res = await authFetch(`${API_BASE}/discover/preview`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            address,
            username: auth.username,
            password: auth.password,
        }),
    });

    if (!res.ok) {
        throw new Error('Failed to fetch preview');
    }

    const blob = await res.blob();
    return URL.createObjectURL(blob);
}

// Events & Timeline  
export async function queryEvents(params: {
    start: string;
    end: string;
    camera_id?: string;
    types?: string;
    search?: string;
    limit?: number;
}): Promise<Event[]> {
    const query = new URLSearchParams();
    query.set('start', params.start);
    query.set('end', params.end);
    if (params.camera_id) query.set('camera_id', params.camera_id);
    if (params.types) query.set('types', params.types);
    if (params.search) query.set('search', params.search);
    if (params.limit) query.set('limit', params.limit.toString());
    try {
        const res = await authFetch(`${API_BASE}/events?${query}`);
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    } catch { return []; }
}

export async function getTimeline(params: {
    start: string;
    end: string;
    camera_ids?: string[];
    interval?: number;
}): Promise<TimelineBucket[]> {
    const query = new URLSearchParams();
    query.set('start', params.start);
    query.set('end', params.end);
    if (params.camera_ids && params.camera_ids.length > 0) {
        query.set('camera_ids', params.camera_ids.join(','));
    }
    if (params.interval) query.set('interval', params.interval.toString());
    try {
        const res = await authFetch(`${API_BASE}/timeline?${query}`);
        const data = await res.json();
        return Array.isArray(data) ? data : [];
    } catch { return []; }
}

// Exports
export async function createExport(data: { camera_id: string; start_time: string; end_time: string }): Promise<ExportJob> {
    const res = await authFetch(`${API_BASE}/exports`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    return res.json();
}

export async function listExports(): Promise<ExportJob[]> {
    const res = await authFetch(`${API_BASE}/exports`);
    return res.json();
}

// Health check
export async function healthCheck(): Promise<{ status: string }> {
    const res = await authFetch(`${API_BASE}/health`);
    return res.json();
}

// -----------------------------------------------------------------------------
// PTZ
// -----------------------------------------------------------------------------

export function ptzMove(id: string, pan: number, tilt: number, zoom: number): void {
    authFetch(`${API_BASE}/cameras/${id}/ptz/move`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pan, tilt, zoom }),
        keepalive: true,
    }).catch(() => { }); // fire-and-forget
}

export function ptzStop(id: string): void {
    authFetch(`${API_BASE}/cameras/${id}/ptz/stop`, {
        method: 'POST',
        keepalive: true,
    }).catch(() => { }); // fire-and-forget
}

export function ptzPrewarm(id: string): void {
    authFetch(`${API_BASE}/cameras/${id}/ptz/prewarm`, {
        method: 'POST',
        keepalive: true,
    }).catch(() => { }); // fire-and-forget
}

// -----------------------------------------------------------------------------
// AI Detection (ONVIF Profile M analytics bounding boxes)
// -----------------------------------------------------------------------------

export interface BoundingBox {
    label: string;
    confidence: number;
    x: number; // normalized 0-1 (left edge)
    y: number; // normalized 0-1 (top edge)
    w: number; // normalized 0-1 (width)
    h: number; // normalized 0-1 (height)
}

export interface DetectionResult {
    type: string;
    camera_id: string;
    time: string;
    boxes: BoundingBox[];
}

/** Fetch the latest cached bounding boxes for a camera from the server. */
export async function fetchDetections(cameraId: string): Promise<DetectionResult> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/detect`);
    if (!res.ok) throw new Error('Detection fetch failed');
    return res.json();
}

// -----------------------------------------------------------------------------
// Recording coverage (for timeline green/audio bars)
// -----------------------------------------------------------------------------

export interface SegmentCoverage {
    camera_id: string;
    start_time: string; // ISO8601
    end_time: string;   // ISO8601
    has_audio: boolean;
}

/**
 * Fetch segment coverage spans for a set of cameras within a time window.
 * Used by the timeline to draw green (video) and dark (audio) bars.
 */
export async function fetchCoverage(
    cameraIds: string[],
    start: Date,
    end: Date,
): Promise<SegmentCoverage[]> {
    if (cameraIds.length === 0) return [];
    const params = new URLSearchParams({
        start: start.toISOString(),
        end: end.toISOString(),
        camera_ids: cameraIds.join(','),
    });
    const res = await authFetch(`${API_BASE}/timeline/coverage?${params}`);
    if (!res.ok) return [];
    return res.json();
}

// -----------------------------------------------------------------------------
// System Settings
// -----------------------------------------------------------------------------

export interface SystemSettings {
    recordings_path: string;
    snapshots_path: string;
    exports_path: string;
    hls_path: string;
    default_retention_days: number;
    default_recording_mode: string;
    default_segment_duration: number;
    ffmpeg_path: string;
    discovery_subnet: string;
    discovery_ports: string;
    notification_webhook_url: string;
    notification_email: string;
    notification_triggers: string;
    updated_at: string;
}

export interface SystemHealth {
    uptime_seconds: number;
    cameras_online: number;
    cameras_offline: number;
    cameras_recording: number;
    cameras_total: number;
    active_streams: number;
    memory_mb: number;
    memory_sys_mb: number;
    goroutines: number;
    storage: Array<{
        label: string;
        path: string;
        enabled: boolean;
        total_bytes?: number;
        free_bytes?: number;
        used_bytes?: number;
        dir_size?: number;
    }>;
    go_version: string;
    os: string;
    arch: string;
}

export async function getSystemHealth(): Promise<SystemHealth> {
    const res = await authFetch(`${API_BASE}/system/health`);
    if (!res.ok) throw new Error('Failed to load system health');
    return res.json();
}

export async function getSettings(): Promise<SystemSettings> {
    const res = await authFetch(`${API_BASE}/settings`);
    if (!res.ok) throw new Error('Failed to load settings');
    return res.json();
}

export async function updateSettings(data: Partial<SystemSettings>): Promise<SystemSettings> {
    const res = await authFetch(`${API_BASE}/settings`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

// -----------------------------------------------------------------------------
// User Management
// -----------------------------------------------------------------------------

export interface UserPublic {
    id: string;
    username: string;
    role: string;
    display_name: string;
    email: string;
    phone: string;
    organization_id?: string;
    assigned_site_ids: string[];
    created_at: string;
    updated_at: string;
}

export interface UserCreate {
    username: string;
    password: string;
    role: string;
    display_name?: string;
    email?: string;
    phone?: string;
    organization_id?: string;
    assigned_site_ids?: string[];
}

export interface UserProfileUpdate {
    display_name?: string;
    email?: string;
    phone?: string;
    organization_id?: string;
    assigned_site_ids?: string[];
}

export async function listUsers(): Promise<UserPublic[]> {
    const res = await authFetch(`${API_BASE}/users`);
    if (!res.ok) throw new Error('Failed to load users');
    return res.json();
}

export async function createUser(data: UserCreate): Promise<UserPublic> {
    const res = await authFetch(`${API_BASE}/users`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function deleteUser(id: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/users/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
}

export async function updateUserPassword(id: string, password: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/users/${id}/password`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
    });
    if (!res.ok) throw new Error(await res.text());
}

export async function updateUserRole(id: string, role: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/users/${id}/role`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role }),
    });
    if (!res.ok) throw new Error(await res.text());
}

export async function updateUserProfile(id: string, data: UserProfileUpdate): Promise<UserPublic> {
    const res = await authFetch(`${API_BASE}/users/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

// -----------------------------------------------------------------------------
// Storage Management
// -----------------------------------------------------------------------------

export interface DriveInfo {
    letter: string;
    label: string;
    file_system: string;
    drive_type: string;   // local, network, removable
    total_bytes: number;
    free_bytes: number;
    used_bytes: number;
}

export interface FolderEntry {
    name: string;
    path: string;
    is_dir: boolean;
}

export interface StorageLocation {
    id: string;
    label: string;
    path: string;
    purpose: string;
    retention_days: number;
    max_gb: number;
    priority: number;
    enabled: boolean;
    created_at: string;
    updated_at: string;
}

export interface StorageLocationCreate {
    label: string;
    path: string;
    purpose: string;
    retention_days: number;
    max_gb: number;
    priority: number;
}

export interface DiskUsage {
    total_bytes: number;
    free_bytes: number;
    used_bytes: number;
}

export async function listDrives(): Promise<DriveInfo[]> {
    const res = await authFetch(`${API_BASE}/storage/drives`);
    if (!res.ok) return [];
    return res.json();
}

export async function browsePath(path: string): Promise<FolderEntry[]> {
    const res = await authFetch(`${API_BASE}/storage/browse?path=${encodeURIComponent(path)}`);
    if (!res.ok) return [];
    return res.json();
}

export async function getDiskUsage(path: string): Promise<DiskUsage | null> {
    const res = await authFetch(`${API_BASE}/storage/disk-usage?path=${encodeURIComponent(path)}`);
    if (!res.ok) return null;
    return res.json();
}

export async function listStorageLocations(): Promise<StorageLocation[]> {
    const res = await authFetch(`${API_BASE}/storage/locations`);
    if (!res.ok) return [];
    return res.json();
}

export async function createStorageLocation(data: StorageLocationCreate): Promise<StorageLocation> {
    const res = await authFetch(`${API_BASE}/storage/locations`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function updateStorageLocation(id: string, data: StorageLocationCreate): Promise<void> {
    const res = await authFetch(`${API_BASE}/storage/locations/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
}

export async function deleteStorageLocation(id: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/storage/locations/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
}

// -----------------------------------------------------------------------------
// Speakers
// -----------------------------------------------------------------------------

export interface Speaker {
    id: string;
    name: string;
    onvif_address: string;
    username: string;
    rtsp_uri: string;
    zone: string;
    status: string;
    manufacturer: string;
    model: string;
    created_at: string;
    updated_at: string;
}

export interface AudioMessage {
    id: string;
    name: string;
    category: string; // warning, info, emergency, custom
    file_name: string;
    duration: number;
    file_size: number;
    created_at: string;
}

export async function listSpeakers(): Promise<Speaker[]> {
    const res = await authFetch(`${API_BASE}/speakers`);
    if (!res.ok) throw new Error('Failed to list speakers');
    return res.json();
}

export async function createSpeaker(data: {
    name: string; onvif_address: string; username: string; password: string; zone: string;
}): Promise<Speaker> {
    const res = await authFetch(`${API_BASE}/speakers`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function deleteSpeaker(id: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/speakers/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
}

export async function playSpeakerMessage(speakerId: string, messageId: string): Promise<{ status: string }> {
    const res = await authFetch(`${API_BASE}/speakers/${speakerId}/play/${messageId}`, {
        method: 'POST',
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function stopSpeakerPlayback(): Promise<void> {
    await authFetch(`${API_BASE}/speakers/stop`, { method: 'POST' });
}

export async function getSpeakerStatus(): Promise<{ playing: boolean }> {
    const res = await authFetch(`${API_BASE}/speakers/status`);
    if (!res.ok) throw new Error('Failed to get speaker status');
    return res.json();
}

export async function listAudioMessages(): Promise<AudioMessage[]> {
    const res = await authFetch(`${API_BASE}/audio-messages`);
    if (!res.ok) throw new Error('Failed to list audio messages');
    return res.json();
}

export async function uploadAudioMessage(name: string, category: string, file: File): Promise<AudioMessage> {
    const form = new FormData();
    form.append('name', name);
    form.append('category', category);
    form.append('file', file);
    const res = await authFetch(`${API_BASE}/audio-messages`, {
        method: 'POST',
        body: form,
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function deleteAudioMessage(id: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/audio-messages/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
}

export async function getSpeakerInfo(): Promise<{ speakers: Speaker[]; messages: AudioMessage[] }> {
    const res = await authFetch(`${API_BASE}/speaker-info`);
    if (!res.ok) throw new Error('Failed to load speaker info');
    return res.json();
}

// ──────────────────── Audit Log ────────────────────

export interface AuditEntry {
    id: number;
    user_id: string;
    username: string;
    action: string;
    target_type: string;
    target_id: string;
    details: string;
    ip_address: string;
    created_at: string;
}

export interface AuditLogResponse {
    entries: AuditEntry[];
    total: number;
    limit: number;
    offset: number;
}

export async function queryAuditLog(params: {
    username?: string;
    action?: string;
    target_type?: string;
    limit?: number;
    offset?: number;
} = {}): Promise<AuditLogResponse> {
    const qs = new URLSearchParams();
    if (params.username) qs.set('username', params.username);
    if (params.action) qs.set('action', params.action);
    if (params.target_type) qs.set('target_type', params.target_type);
    if (params.limit) qs.set('limit', String(params.limit));
    if (params.offset) qs.set('offset', String(params.offset));
    const res = await authFetch(`${API_BASE}/audit?${qs.toString()}`);
    if (!res.ok) throw new Error('Failed to query audit log');
    return res.json();
}

// ──────────────────── Bookmarks ────────────────────

export interface Bookmark {
    id: string;
    camera_id: string;
    event_time: string;
    label: string;
    notes: string;
    severity: string;
    created_by: string;
    username: string;
    created_at: string;
}

export async function createBookmark(data: {
    camera_id: string;
    event_time: string;
    label: string;
    notes?: string;
    severity?: string;
}): Promise<Bookmark> {
    const res = await authFetch(`${API_BASE}/bookmarks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
}

export async function listBookmarks(params: {
    start?: string;
    end?: string;
    camera_id?: string;
} = {}): Promise<Bookmark[]> {
    const qs = new URLSearchParams();
    if (params.start) qs.set('start', params.start);
    if (params.end) qs.set('end', params.end);
    if (params.camera_id) qs.set('camera_id', params.camera_id);
    const res = await authFetch(`${API_BASE}/bookmarks?${qs.toString()}`);
    if (!res.ok) throw new Error('Failed to list bookmarks');
    return res.json();
}

export async function deleteBookmark(id: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/bookmarks/${id}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
}

export interface PlaybackSegment {
    url: string;
    start_time: string;
    end_time: string;
    duration_ms: number;
}

export async function fetchPlaybackSegments(cameraId: string, time: string, signal?: AbortSignal): Promise<PlaybackSegment[]> {
    const res = await authFetch(`${API_BASE}/playback/${cameraId}?t=${time}`, { signal });
    if (!res.ok) return [];
    return res.json();
}

// ── PPE Pending Review Queue (P2-C-01) ───────────────────────────────────────

import type { PendingReviewEntry } from '@/types/ironsight';
export type { PendingReviewEntry };

export interface PendingReviewFilter {
    status?: string;
    camera_id?: string;
    limit?: number;
    before?: string;
}

export async function getPendingReview(filter: PendingReviewFilter = {}): Promise<{ entries: PendingReviewEntry[] }> {
    const qs = new URLSearchParams();
    if (filter.status) qs.set('status', filter.status);
    if (filter.camera_id) qs.set('camera_id', filter.camera_id);
    if (filter.limit) qs.set('limit', String(filter.limit));
    if (filter.before) qs.set('before', filter.before);
    const res = await authFetch(`${API_BASE}/v1/portal/pending-review?${qs.toString()}`);
    if (!res.ok) throw new Error(`pending review: ${res.status}`);
    return res.json();
}

export async function submitReview(
    id: string,
    body: { status: string; notes?: string }
): Promise<void> {
    const res = await authFetch(`${API_BASE}/v1/portal/pending-review/${id}/review`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(`submit review: ${res.status}`);
}

// ── PPE Zones + Compliance Rules (P2-C-04) ───────────────────────────────────

import type { PPEZone, PPEZoneCreate, ComplianceRule, ComplianceRuleCreate } from '@/types/ironsight';
export type { PPEZone, PPEZoneCreate, ComplianceRule, ComplianceRuleCreate };

export async function listPPEZones(cameraId: string): Promise<PPEZone[]> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/ppe/zones`);
    if (!res.ok) return [];
    return res.json();
}

export async function createPPEZone(cameraId: string, data: PPEZoneCreate): Promise<PPEZone> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/ppe/zones`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(`create PPE zone: ${res.status}`);
    return res.json();
}

export async function updatePPEZone(cameraId: string, zoneId: string, data: PPEZoneCreate): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/ppe/zones/${zoneId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(`update PPE zone: ${res.status}`);
}

export async function deletePPEZone(cameraId: string, zoneId: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/ppe/zones/${zoneId}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(`delete PPE zone: ${res.status}`);
}

export async function listComplianceRules(cameraId: string): Promise<ComplianceRule[]> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/compliance-rules`);
    if (!res.ok) return [];
    return res.json();
}

export async function createComplianceRule(cameraId: string, data: ComplianceRuleCreate): Promise<ComplianceRule> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/compliance-rules`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(`create compliance rule: ${res.status}`);
    return res.json();
}

export async function updateComplianceRule(cameraId: string, ruleId: string, data: ComplianceRuleCreate): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/compliance-rules/${ruleId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
    });
    if (!res.ok) throw new Error(`update compliance rule: ${res.status}`);
}

export async function deleteComplianceRule(cameraId: string, ruleId: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraId}/compliance-rules/${ruleId}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(`delete compliance rule: ${res.status}`);
}

// ── Compliance Dashboard (P2-C-06) ───────────────────────────────────────────

import type { ComplianceSummary } from '@/types/ironsight';

export interface ComplianceSummaryParams {
    site_id?: string;
    period: 'today' | 'week' | 'month' | '90days' | 'custom';
    start_date?: string;
    end_date?: string;
    org?: string;
}

export async function getComplianceSummary(params: ComplianceSummaryParams): Promise<ComplianceSummary> {
    const qs = new URLSearchParams();
    qs.set('period', params.period);
    if (params.site_id) qs.set('site_id', params.site_id);
    if (params.start_date) qs.set('start_date', params.start_date);
    if (params.end_date) qs.set('end_date', params.end_date);
    if (params.org) qs.set('org', params.org);

    const res = await authFetch(`${API_BASE}/v1/portal/compliance/summary?${qs.toString()}`);
    if (!res.ok) throw new Error(`compliance summary: ${res.status}`);
    return res.json();
}

export async function downloadComplianceReport(
    params: ComplianceSummaryParams & { include_findings?: boolean }
): Promise<void> {
    const qs = new URLSearchParams();
    qs.set('period', params.period);
    if (params.site_id) qs.set('site_id', params.site_id);
    if (params.start_date) qs.set('start_date', params.start_date);
    if (params.end_date) qs.set('end_date', params.end_date);
    if (params.org) qs.set('org', params.org);
    if (params.include_findings === false) qs.set('include_findings', 'false');

    const res = await authFetch(`${API_BASE}/v1/portal/compliance/report.pdf?${qs.toString()}`);
    if (!res.ok) throw new Error(`compliance PDF: ${res.status}`);

    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `ironsight-compliance-${params.period}.pdf`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
}

