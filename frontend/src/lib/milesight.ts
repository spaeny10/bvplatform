// Typed client for the Milesight vendor-config pass-through endpoints.
//
// The backend exposes GET  /api/cameras/{id}/milesight/config/{panel}
//                and PUT  /api/cameras/{id}/milesight/config/{panel}
// forwarding to the camera's operator.cgi / admin.cgi CGI actions.
// The response shapes mirror the vendor JSON verbatim — we intentionally
// do not re-shape server-side, so this file is the one place the frontend
// commits to a typed contract per panel.

import { authFetch } from './api';

const API_BASE = '/api';

// ── Generic helpers ──────────────────────────────────────────────

export async function milesightGet<T>(cameraID: string, panel: string): Promise<T> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/milesight/config/${panel}`);
    if (!res.ok) throw new Error(`milesight ${panel}: HTTP ${res.status}`);
    return res.json();
}

export async function milesightSet<T extends object>(cameraID: string, panel: string, body: T): Promise<unknown> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/milesight/config/${panel}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
    });
    if (!res.ok) {
        const msg = await res.text().catch(() => res.statusText);
        throw new Error(`milesight ${panel} PUT failed: ${msg}`);
    }
    return res.json().catch(() => ({}));
}

// ── Action endpoints ─────────────────────────────────────────────

export async function milesightReboot(cameraID: string): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/milesight/reboot`, { method: 'POST' });
    if (!res.ok) throw new Error(`reboot failed: HTTP ${res.status}`);
}

export async function milesightPTZGoto(cameraID: string, preset: number): Promise<void> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/milesight/ptz/preset/goto`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ preset }),
    });
    if (!res.ok) throw new Error(`preset goto failed: HTTP ${res.status}`);
}

// ── Per-panel typed shapes ───────────────────────────────────────
//
// Each interface matches the camera's JSON response exactly. When the
// device doesn't support a field it's still present in the response
// (usually zero'd), so we keep every field optional-free for simplicity.

// Per-stream video settings. Reported under streamList.{mainStream|subStream|
// thirdStream|fourthStream|fifthStream} from get.video.general. The set
// action accepts the same envelope; streams without hardware support
// report as {"enable": 0} and are otherwise opaque.
export interface VideoStream {
    enable?: 0 | 1;
    width: number;
    height: number;
    url: string;              // path suffix in the RTSP URL
    framerate: number;
    bitrate: number;          // kbps
    profileGop: number;       // I-frame interval
    rateMode: number;         // 0=CBR, 1=VBR
    profile: number;          // H.264/H.265 profile
    profileCodec: number;     // 0=H.264, 1=H.265
    smartStreamEnable: 0 | 1;
    smartStreamLevel: number;
    rateQuality: number;
    vbrQuality: number;
}
export interface StreamsPanel {
    streamList: {
        mainStream: VideoStream;
        subStream: VideoStream;
        thirdStream?: VideoStream;
        fourthStream?: Partial<VideoStream>;
        fifthStream?: Partial<VideoStream>;
    };
    rtspPort: number;
    eventStreamEnable: 0 | 1;
    eventStreamFramerate: number;
    eventStreamBitrate: number;
    eventStreamIframe: number;
    deviceModel?: string;
    deviceSensor?: string;
}

export interface OSDStream {
    streamIndex: number;
    osdEnable: 0 | 1;
    osdString: string;
    osdDateTimeEnable: 0 | 1;
    osdFontSize: number;
    osdFontColor: string;           // "R:G:B"
    osdBackgroundEnable: 0 | 1;
    osdBackgroundColor: string;     // "R:G:B"
    osdTextPosition: number;
    osdDateTimePosition: number;
    osdDateTimeFormat: number;
    cropRoiEnable?: number;
}
export interface OSDPanel {
    osdInfoList: OSDStream[];
}

export interface ImagePanel {
    powerlineFreq: number;
    brightness: number;
    colorSaturation: number;
    sharpness: number;
    contrast: number;
    whiteBalanceMode: number;
    whiteBalanceRedGain: number;
    whiteBalanceBlueGain: number;
    exposureMode: number;
    exposureTime: number;
    exposureGain: number;
    defogMode: number;
    antiFogIntensity: number;
    nfLevel: number;
    dnr2Level: number;
    focusMode: number;
    irCutFilter: number;
    mirrorCorridor: number;
    imageRotation: number;
    dayToNight: number;
    nightToDay: number;
    smartIrMode: number;
    nearViewIrLevel: number;
    middleViewIrLevel: number;
    farViewIrLevel: number;
    whiteLedLevel: number;
}

export interface AudioPanel {
    enable: 0 | 1;
    codec: number;
    source: number;
    mode: number;
    denoise: 0 | 1;
    inputGain: number;
    alarmLevel: number;
    outputAgc: 0 | 1;
    outputVolume: number;
    sampleRate: number;
    bitRate: number;
    speakerVolume: number;
    twoWayOutputVolume: number;
    twoWaySpeakerVolume: number;
}

export interface DateTimePanel {
    year: number;
    month: number;
    day: number;
    hour: number;
    minute: number;
    second: number;
    timeZoneTz: string;
    zoneNameTz: string;
    dayLight: number;
    timeType: number;
    ntpServer: string;
    ntpSyncEnable: 0 | 1;
    ntpInterval: number;
    timeFormat: number;
}

export interface NetworkPanel {
    model: string;
    mac: string;
    firmwareVersion: string;
    systemBootTime: string;
    dhcpEnable: 0 | 1;
    ipaddress: string;
    netmask: string;
    gateway: string;
    dns0: string;
    dns1: string;
    ddnsEnable: 0 | 1;
    ddnsHostName: string;
    deviceName: string;
    deviceLocation: string;
    hardwareVersion: string;
    kernelVersion: string;
}

export interface PrivacyMask {
    index: number;
    maskX: number;
    maskY: number;
    maskWidth: number;
    maskHeight: number;
    maskType: number;  // 0=white, 1=black, 2=blue, 3=yellow, 4=green, 5=brown, 6=red, 7=pink
    maskShow: 0 | 1;
    maskRatio: number;
    isexist: 0 | 1;
    maskName: string;
}
export interface PrivacyMaskPanel {
    calibrationEnable: 0 | 1;
    maskEnable: 0 | 1;
    maskNum: number;
    maskList: PrivacyMask[];
}

export interface AutoRebootPanel {
    rebootEnable: 0 | 1;
    rebootWeekday: number; // 0-6 = Sun-Sat, 7 = daily
    rebootHour: number;
    rebootMin: number;
    rebootSec: number;
}

export interface PTZPreset {
    presetIndex: number;
    presetName: string;
    pan?: number;
    tilt?: number;
    zoom?: number;
}
export interface PTZPresetPanel {
    presetInfoList: PTZPreset[];
}

export interface AlarmInputSchedule {
    scheduleIndex: number;
    schedule: string; // "HH:MM:HH:MM;..." windows
}
export interface AlarmInput {
    index: number;
    enable: 0 | 1;
    normal: 0 | 1;     // 0=NO, 1=NC
    status: number;
    scheduleList: AlarmInputSchedule[];
}
export interface AlarmInputPanel {
    supportInputNum: number;
    inputList: AlarmInput[];
}

export interface AlarmOutputPanel {
    // When the camera doesn't have a wired alarm output it returns
    // {"setState":"failed"} — the UI degrades gracefully.
    supportOutputNum?: number;
    outputList?: Array<{
        index: number;
        enable: 0 | 1;
        normal: 0 | 1;
        delayTime: number;
    }>;
    setState?: string;
}

export interface ImageCaps {
    dayToNight: { range: string; min: number; max: number };
    nightToDay: { range: string; min: number; max: number };
    nearViewIrLevel: { range: string; min: number; max: number };
    farViewIrLevel: { range: string; min: number; max: number };
    maskList?: {
        maskTotalNum: { mask: number; mosaic: number };
        maskType: Record<string, number>;
    };
}

// ── VCA bidirectional pull ───────────────────────────────────────

export interface VCAPullResult {
    camera_id: string;
    rules: VCARuleFromCamera[];
    db_only: VCARuleFromCamera[];
    camera_only: VCARuleFromCamera[];
    modified: { before: VCARuleFromCamera; after: VCARuleFromCamera }[];
    applied: boolean;
}

export interface VCARuleFromCamera {
    id?: string;
    camera_id: string;
    rule_type: 'intrusion' | 'linecross' | 'regionentrance' | 'loitering';
    name: string;
    enabled: boolean;
    sensitivity: number;
    region: { x: number; y: number }[];
    direction: string;
    threshold_sec: number;
    schedule: string;
    actions: string[];
    synced: boolean;
}

export async function vcaPullPreview(cameraID: string): Promise<VCAPullResult> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/vca/pull`);
    if (!res.ok) throw new Error(`vca pull: HTTP ${res.status}`);
    return res.json();
}

export async function vcaPullApply(cameraID: string): Promise<VCAPullResult> {
    const res = await authFetch(`${API_BASE}/cameras/${cameraID}/vca/pull?apply=1`, {
        method: 'POST',
    });
    if (!res.ok) {
        const msg = await res.text().catch(() => res.statusText);
        throw new Error(`vca pull apply: ${msg}`);
    }
    return res.json();
}
