'use client';

import { useEffect, useState, useCallback } from 'react';
import {
    milesightGet, milesightSet, milesightReboot, milesightPTZGoto,
    OSDPanel, ImagePanel, AudioPanel, DateTimePanel, NetworkPanel,
    PrivacyMaskPanel, PrivacyMask, AutoRebootPanel, PTZPresetPanel,
    AlarmInputPanel, AlarmOutputPanel, StreamsPanel, VideoStream,
} from '@/lib/milesight';

type TabKey = 'streams' | 'osd' | 'image' | 'audio' | 'network' | 'privacy' | 'system' | 'ptz' | 'alarm';

const TABS: { key: TabKey; label: string }[] = [
    { key: 'streams', label: 'Streams' },
    { key: 'osd', label: 'OSD' },
    { key: 'image', label: 'Image' },
    { key: 'audio', label: 'Audio' },
    { key: 'network', label: 'Network' },
    { key: 'privacy', label: 'Privacy' },
    { key: 'system', label: 'System' },
    { key: 'ptz', label: 'PTZ' },
    { key: 'alarm', label: 'Alarm I/O' },
];

/**
 * MilesightAdvanced is the "all vendor options" tab inside the camera
 * settings modal. Only rendered when the camera manufacturer resolves to
 * Milesight. Each sub-tab talks to one CGI action pair via milesightGet /
 * milesightSet. Saves are explicit (no autosave) because a bad write
 * can silently brick a production camera — we want the operator to see
 * the form validate before the PUT fires.
 */
export default function MilesightAdvanced({ cameraId }: { cameraId: string }) {
    const [tab, setTab] = useState<TabKey>('osd');

    return (
        <div style={{ padding: '4px 0' }}>
            <div style={{
                display: 'flex', gap: 2, marginBottom: 14, flexWrap: 'wrap',
                borderBottom: '1px solid rgba(255,255,255,0.06)',
            }}>
                {TABS.map(t => (
                    <button
                        key={t.key}
                        onClick={() => setTab(t.key)}
                        style={{
                            padding: '6px 12px', fontSize: 11, fontWeight: 600,
                            textTransform: 'uppercase', letterSpacing: 0.8,
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: tab === t.key ? '#60a5fa' : 'rgba(255,255,255,0.45)',
                            borderBottom: tab === t.key ? '2px solid #60a5fa' : '2px solid transparent',
                        }}
                    >
                        {t.label}
                    </button>
                ))}
            </div>

            {tab === 'streams' && <StreamsTab cameraId={cameraId} />}
            {tab === 'osd' && <OSDTab cameraId={cameraId} />}
            {tab === 'image' && <ImageTab cameraId={cameraId} />}
            {tab === 'audio' && <AudioTab cameraId={cameraId} />}
            {tab === 'network' && <NetworkTab cameraId={cameraId} />}
            {tab === 'privacy' && <PrivacyTab cameraId={cameraId} />}
            {tab === 'system' && <SystemTab cameraId={cameraId} />}
            {tab === 'ptz' && <PTZTab cameraId={cameraId} />}
            {tab === 'alarm' && <AlarmTab cameraId={cameraId} />}
        </div>
    );
}

// ── Shared helpers ──────────────────────────────────────────────

function usePanel<T>(cameraId: string, panel: string) {
    const [data, setData] = useState<T | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);

    const reload = useCallback(async () => {
        setLoading(true);
        setError(null);
        try {
            const d = await milesightGet<T>(cameraId, panel);
            setData(d);
        } catch (e) {
            setError(String(e));
        } finally {
            setLoading(false);
        }
    }, [cameraId, panel]);

    useEffect(() => { reload(); }, [reload]);

    return { data, setData, error, loading, reload };
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div style={{ display: 'grid', gridTemplateColumns: '160px 1fr', gap: 12, marginBottom: 10, alignItems: 'center' }}>
            <label style={{ fontSize: 12, color: 'rgba(255,255,255,0.6)' }}>{label}</label>
            <div>{children}</div>
        </div>
    );
}

const inputStyle: React.CSSProperties = {
    width: '100%', background: 'rgba(0,0,0,0.3)', border: '1px solid rgba(255,255,255,0.1)',
    color: 'white', padding: '6px 10px', fontSize: 13, borderRadius: 4,
};

const btnPrimary: React.CSSProperties = {
    background: '#3B82F6', border: 'none', color: 'white', padding: '8px 18px',
    fontSize: 12, fontWeight: 600, cursor: 'pointer', borderRadius: 4,
};
const btnDanger: React.CSSProperties = { ...btnPrimary, background: '#dc2626' };
const btnSecondary: React.CSSProperties = { ...btnPrimary, background: 'rgba(255,255,255,0.08)' };

function SaveBar({ onSave, onReload, saving, statusMsg }: {
    onSave: () => void; onReload: () => void; saving: boolean; statusMsg: string | null;
}) {
    return (
        <div style={{ marginTop: 14, display: 'flex', gap: 8, alignItems: 'center' }}>
            <button style={btnPrimary} onClick={onSave} disabled={saving}>
                {saving ? 'Saving…' : 'Save to camera'}
            </button>
            <button style={btnSecondary} onClick={onReload} disabled={saving}>Reload</button>
            {statusMsg && <span style={{ fontSize: 12, color: statusMsg.startsWith('Error') ? '#ef4444' : '#22c55e' }}>{statusMsg}</span>}
        </div>
    );
}

function useSaver<T extends object>(cameraId: string, panel: string) {
    const [saving, setSaving] = useState(false);
    const [statusMsg, setStatusMsg] = useState<string | null>(null);
    const save = useCallback(async (body: T) => {
        setSaving(true);
        setStatusMsg(null);
        try {
            await milesightSet(cameraId, panel, body);
            setStatusMsg('Saved');
            setTimeout(() => setStatusMsg(null), 3000);
        } catch (e) {
            setStatusMsg('Error: ' + String(e));
        } finally {
            setSaving(false);
        }
    }, [cameraId, panel]);
    return { save, saving, statusMsg };
}

// ── Streams (resolution / framerate / bitrate / codec per stream) ──

// Common resolutions the Milesight lineup supports. Presets keep users
// from typing arbitrary dimensions that the sensor can't produce — the
// camera accepts them but silently clamps, which looks like "Save didn't
// work" on the next reload.
const RESOLUTION_PRESETS: { label: string; w: number; h: number }[] = [
    { label: '3840×2160 (4K)', w: 3840, h: 2160 },
    { label: '2592×1944 (5MP 4:3)', w: 2592, h: 1944 },
    { label: '2560×1440 (QHD)', w: 2560, h: 1440 },
    { label: '1920×1080 (1080p)', w: 1920, h: 1080 },
    { label: '1280×960 (4:3)', w: 1280, h: 960 },
    { label: '1280×720 (720p)', w: 1280, h: 720 },
    { label: '704×576 (D1)', w: 704, h: 576 },
    { label: '640×480 (VGA)', w: 640, h: 480 },
    { label: '352×288 (CIF)', w: 352, h: 288 },
];

function resolutionKey(s: VideoStream): string {
    const match = RESOLUTION_PRESETS.find(p => p.w === s.width && p.h === s.height);
    return match ? `${s.width}×${s.height}` : `custom:${s.width}×${s.height}`;
}

function StreamEditor({ label, stream, onChange, canDisable }: {
    label: string;
    stream: VideoStream;
    onChange: (next: VideoStream) => void;
    canDisable: boolean;
}) {
    const enabled = stream.enable !== 0;
    return (
        <div style={{
            padding: 12, marginBottom: 10,
            background: 'rgba(255,255,255,0.03)',
            border: '1px solid rgba(255,255,255,0.06)',
            borderRadius: 4,
            opacity: canDisable && !enabled ? 0.55 : 1,
        }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                <strong style={{ fontSize: 12, color: 'rgba(255,255,255,0.85)' }}>{label}</strong>
                {canDisable && (
                    <label style={{ fontSize: 11, color: 'rgba(255,255,255,0.5)' }}>
                        <input type="checkbox" checked={enabled}
                            onChange={e => onChange({ ...stream, enable: e.target.checked ? 1 : 0 })}
                            style={{ marginRight: 4 }} />
                        Enabled
                    </label>
                )}
            </div>

            <Row label="Resolution">
                <select style={inputStyle} value={resolutionKey(stream)}
                    onChange={e => {
                        const v = e.target.value;
                        if (v.startsWith('custom:')) return;
                        const preset = RESOLUTION_PRESETS.find(p => `${p.w}×${p.h}` === v);
                        if (preset) onChange({ ...stream, width: preset.w, height: preset.h });
                    }}>
                    {RESOLUTION_PRESETS.map(p => (
                        <option key={`${p.w}x${p.h}`} value={`${p.w}×${p.h}`}>{p.label}</option>
                    ))}
                    {!RESOLUTION_PRESETS.find(p => p.w === stream.width && p.h === stream.height) && (
                        <option value={`custom:${stream.width}×${stream.height}`}>
                            {stream.width}×{stream.height} (current)
                        </option>
                    )}
                </select>
            </Row>

            <Row label="Frame rate (fps)">
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <input type="range" min={1} max={30} value={stream.framerate}
                        onChange={e => onChange({ ...stream, framerate: Number(e.target.value) })}
                        style={{ flex: 1 }} />
                    <span style={{ fontSize: 12, minWidth: 30, textAlign: 'right' }}>{stream.framerate}</span>
                </div>
            </Row>

            <Row label="Bitrate (kbps)">
                <input style={inputStyle} type="number" min={64} max={16384} step={64}
                    value={stream.bitrate}
                    onChange={e => onChange({ ...stream, bitrate: Number(e.target.value) })} />
            </Row>

            <Row label="Rate mode">
                <select style={inputStyle} value={stream.rateMode}
                    onChange={e => onChange({ ...stream, rateMode: Number(e.target.value) })}>
                    <option value={0}>CBR (constant)</option>
                    <option value={1}>VBR (variable)</option>
                </select>
            </Row>

            <Row label="Codec">
                <select style={inputStyle} value={stream.profileCodec}
                    onChange={e => onChange({ ...stream, profileCodec: Number(e.target.value) })}>
                    <option value={0}>H.264</option>
                    <option value={1}>H.265</option>
                    <option value={2}>MJPEG</option>
                </select>
            </Row>

            <Row label="I-frame interval (GOP)">
                <input style={inputStyle} type="number" min={1} max={120}
                    value={stream.profileGop}
                    onChange={e => onChange({ ...stream, profileGop: Number(e.target.value) })} />
            </Row>

            <Row label="Smart stream">
                <input type="checkbox" checked={stream.smartStreamEnable === 1}
                    onChange={e => onChange({ ...stream, smartStreamEnable: e.target.checked ? 1 : 0 })} />
            </Row>

            <div style={{ fontSize: 10, color: 'rgba(255,255,255,0.35)', marginTop: 4 }}>
                RTSP path: /{stream.url}
            </div>
        </div>
    );
}

function StreamsTab({ cameraId }: { cameraId: string }) {
    const { data, setData, error, loading, reload } = usePanel<StreamsPanel>(cameraId, 'streams');
    const { save, saving, statusMsg } = useSaver<StreamsPanel>(cameraId, 'streams');
    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    const patchStream = (key: 'mainStream' | 'subStream' | 'thirdStream', next: VideoStream) => {
        setData({
            ...data,
            streamList: { ...data.streamList, [key]: next },
        });
    };

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>Streams</h4>
            <div style={{ fontSize: 11, color: 'rgba(255,255,255,0.4)', marginBottom: 10 }}>
                Editing stream settings will briefly drop existing RTSP / HLS viewers while the camera restarts its encoder.
            </div>

            <StreamEditor label="Main Stream"
                stream={data.streamList.mainStream}
                onChange={s => patchStream('mainStream', s)}
                canDisable={false} />

            <StreamEditor label="Sub Stream"
                stream={data.streamList.subStream}
                onChange={s => patchStream('subStream', s)}
                canDisable={true} />

            {data.streamList.thirdStream && (
                <StreamEditor label="Third Stream"
                    stream={data.streamList.thirdStream}
                    onChange={s => patchStream('thirdStream', s)}
                    canDisable={true} />
            )}

            <div style={{ marginTop: 14, fontSize: 12, color: 'rgba(255,255,255,0.55)' }}>
                RTSP port: {data.rtspPort}
                {data.deviceModel && <> · Sensor: {data.deviceSensor}</>}
            </div>

            <SaveBar onSave={() => save(data)} onReload={reload} saving={saving} statusMsg={statusMsg} />
        </div>
    );
}

// ── OSD ─────────────────────────────────────────────────────────

function OSDTab({ cameraId }: { cameraId: string }) {
    const { data, setData, error, loading, reload } = usePanel<OSDPanel>(cameraId, 'osd');
    const { save, saving, statusMsg } = useSaver<OSDPanel>(cameraId, 'osd');
    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    const main = data.osdInfoList[0];
    if (!main) return <div style={{ color: '#9ca3af' }}>No OSD streams reported.</div>;

    const patch = (p: Partial<typeof main>) => {
        const next = { ...data, osdInfoList: data.osdInfoList.map((s, i) => i === 0 ? { ...s, ...p } : s) };
        setData(next);
    };

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>On-Screen Display (main stream)</h4>
            <Row label="Enable overlay">
                <input type="checkbox" checked={main.osdEnable === 1}
                    onChange={e => patch({ osdEnable: e.target.checked ? 1 : 0 })} />
            </Row>
            <Row label="Text">
                <input style={inputStyle} value={main.osdString}
                    onChange={e => patch({ osdString: e.target.value })} />
            </Row>
            <Row label="Show date/time">
                <input type="checkbox" checked={main.osdDateTimeEnable === 1}
                    onChange={e => patch({ osdDateTimeEnable: e.target.checked ? 1 : 0 })} />
            </Row>
            <Row label="Font size">
                <select style={inputStyle} value={main.osdFontSize}
                    onChange={e => patch({ osdFontSize: Number(e.target.value) })}>
                    <option value={0}>Small</option>
                    <option value={1}>Medium</option>
                    <option value={2}>Large</option>
                    <option value={3}>XL</option>
                </select>
            </Row>
            <Row label="Font color (R:G:B)">
                <input style={inputStyle} value={main.osdFontColor}
                    onChange={e => patch({ osdFontColor: e.target.value })} />
            </Row>
            <Row label="Background">
                <input type="checkbox" checked={main.osdBackgroundEnable === 1}
                    onChange={e => patch({ osdBackgroundEnable: e.target.checked ? 1 : 0 })} />
            </Row>
            <Row label="Date/time format">
                <select style={inputStyle} value={main.osdDateTimeFormat}
                    onChange={e => patch({ osdDateTimeFormat: Number(e.target.value) })}>
                    <option value={0}>YYYY-MM-DD 24h</option>
                    <option value={1}>MM-DD-YYYY 24h</option>
                    <option value={2}>DD-MM-YYYY 24h</option>
                </select>
            </Row>
            <SaveBar onSave={() => save(data)} onReload={reload} saving={saving} statusMsg={statusMsg} />
        </div>
    );
}

// ── Image ───────────────────────────────────────────────────────

function ImageTab({ cameraId }: { cameraId: string }) {
    const { data, setData, error, loading, reload } = usePanel<ImagePanel>(cameraId, 'image');
    const { save, saving, statusMsg } = useSaver<ImagePanel>(cameraId, 'image');
    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    const slider = (key: keyof ImagePanel, min = 0, max = 100) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <input type="range" min={min} max={max} value={Number(data[key])}
                onChange={e => setData({ ...data, [key]: Number(e.target.value) })}
                style={{ flex: 1 }} />
            <span style={{ fontSize: 12, minWidth: 30, textAlign: 'right' }}>{String(data[key])}</span>
        </div>
    );

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>Image Settings</h4>
            <Row label="Brightness">{slider('brightness')}</Row>
            <Row label="Contrast">{slider('contrast')}</Row>
            <Row label="Saturation">{slider('colorSaturation')}</Row>
            <Row label="Sharpness">{slider('sharpness')}</Row>
            <Row label="Exposure gain">{slider('exposureGain')}</Row>
            <Row label="Noise reduction">{slider('dnr2Level')}</Row>
            <Row label="Power line freq">
                <select style={inputStyle} value={data.powerlineFreq}
                    onChange={e => setData({ ...data, powerlineFreq: Number(e.target.value) })}>
                    <option value={0}>50 Hz</option>
                    <option value={1}>60 Hz</option>
                </select>
            </Row>
            <Row label="Image rotation">
                <select style={inputStyle} value={data.imageRotation}
                    onChange={e => setData({ ...data, imageRotation: Number(e.target.value) })}>
                    <option value={0}>Normal</option>
                    <option value={1}>Flip H</option>
                    <option value={2}>Flip V</option>
                    <option value={3}>180°</option>
                </select>
            </Row>
            <Row label="Defog">
                <select style={inputStyle} value={data.defogMode}
                    onChange={e => setData({ ...data, defogMode: Number(e.target.value) })}>
                    <option value={0}>Off</option>
                    <option value={1}>On</option>
                    <option value={2}>Auto</option>
                </select>
            </Row>
            <Row label="Corridor mode">
                <input type="checkbox" checked={data.mirrorCorridor === 1}
                    onChange={e => setData({ ...data, mirrorCorridor: e.target.checked ? 1 : 0 })} />
            </Row>
            <SaveBar onSave={() => save(data)} onReload={reload} saving={saving} statusMsg={statusMsg} />
        </div>
    );
}

// ── Audio ───────────────────────────────────────────────────────

function AudioTab({ cameraId }: { cameraId: string }) {
    const { data, setData, error, loading, reload } = usePanel<AudioPanel>(cameraId, 'audio');
    const { save, saving, statusMsg } = useSaver<AudioPanel>(cameraId, 'audio');
    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>Audio</h4>
            <Row label="Enable audio">
                <input type="checkbox" checked={data.enable === 1}
                    onChange={e => setData({ ...data, enable: e.target.checked ? 1 : 0 })} />
            </Row>
            <Row label="Codec">
                <select style={inputStyle} value={data.codec}
                    onChange={e => setData({ ...data, codec: Number(e.target.value) })}>
                    <option value={0}>G.711A</option>
                    <option value={1}>G.711U</option>
                    <option value={2}>AAC</option>
                    <option value={3}>G.722</option>
                </select>
            </Row>
            <Row label="Input gain">
                <input type="range" min={0} max={100} value={data.inputGain}
                    onChange={e => setData({ ...data, inputGain: Number(e.target.value) })}
                    style={{ width: '100%' }} />
            </Row>
            <Row label="Output volume">
                <input type="range" min={0} max={100} value={data.outputVolume}
                    onChange={e => setData({ ...data, outputVolume: Number(e.target.value) })}
                    style={{ width: '100%' }} />
            </Row>
            <Row label="Alarm audio level">
                <input type="range" min={0} max={100} value={data.alarmLevel}
                    onChange={e => setData({ ...data, alarmLevel: Number(e.target.value) })}
                    style={{ width: '100%' }} />
            </Row>
            <Row label="Denoise">
                <input type="checkbox" checked={data.denoise === 1}
                    onChange={e => setData({ ...data, denoise: e.target.checked ? 1 : 0 })} />
            </Row>
            <SaveBar onSave={() => save(data)} onReload={reload} saving={saving} statusMsg={statusMsg} />
        </div>
    );
}

// ── Network ─────────────────────────────────────────────────────

function NetworkTab({ cameraId }: { cameraId: string }) {
    const { data, setData, error, loading, reload } = usePanel<NetworkPanel>(cameraId, 'network');
    const { save, saving, statusMsg } = useSaver<NetworkPanel>(cameraId, 'network');
    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>Network</h4>
            <Row label="DHCP">
                <input type="checkbox" checked={data.dhcpEnable === 1}
                    onChange={e => setData({ ...data, dhcpEnable: e.target.checked ? 1 : 0 })} />
            </Row>
            <Row label="IP address">
                <input style={inputStyle} value={data.ipaddress} disabled={data.dhcpEnable === 1}
                    onChange={e => setData({ ...data, ipaddress: e.target.value })} />
            </Row>
            <Row label="Netmask">
                <input style={inputStyle} value={data.netmask} disabled={data.dhcpEnable === 1}
                    onChange={e => setData({ ...data, netmask: e.target.value })} />
            </Row>
            <Row label="Gateway">
                <input style={inputStyle} value={data.gateway} disabled={data.dhcpEnable === 1}
                    onChange={e => setData({ ...data, gateway: e.target.value })} />
            </Row>
            <Row label="Primary DNS">
                <input style={inputStyle} value={data.dns0}
                    onChange={e => setData({ ...data, dns0: e.target.value })} />
            </Row>
            <Row label="Secondary DNS">
                <input style={inputStyle} value={data.dns1}
                    onChange={e => setData({ ...data, dns1: e.target.value })} />
            </Row>
            <Row label="Device name">
                <input style={inputStyle} value={data.deviceName}
                    onChange={e => setData({ ...data, deviceName: e.target.value })} />
            </Row>
            <Row label="Location">
                <input style={inputStyle} value={data.deviceLocation}
                    onChange={e => setData({ ...data, deviceLocation: e.target.value })} />
            </Row>
            <div style={{ marginTop: 8, fontSize: 11, color: 'rgba(255,255,255,0.4)' }}>
                Read-only: MAC {data.mac} · HW {data.hardwareVersion} · Kernel {data.kernelVersion}
            </div>
            <div style={{ marginTop: 12, padding: 10, background: 'rgba(220,38,38,0.08)', border: '1px solid rgba(220,38,38,0.3)', fontSize: 11, color: '#fca5a5' }}>
                ⚠ Changing the IP address will disconnect this camera from our system. You will need to update the camera's address in general settings after it reconnects.
            </div>
            <SaveBar onSave={() => save(data)} onReload={reload} saving={saving} statusMsg={statusMsg} />
        </div>
    );
}

// ── Privacy Masks ───────────────────────────────────────────────

function PrivacyTab({ cameraId }: { cameraId: string }) {
    const { data, setData, error, loading, reload } = usePanel<PrivacyMaskPanel>(cameraId, 'privacyMask');
    const { save, saving, statusMsg } = useSaver<PrivacyMaskPanel>(cameraId, 'privacyMask');
    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    const colorNames = ['White', 'Black', 'Blue', 'Yellow', 'Green', 'Brown', 'Red', 'Pink'];

    const patchMask = (idx: number, p: Partial<PrivacyMask>) => {
        setData({
            ...data,
            maskList: data.maskList.map((m, i) => i === idx ? { ...m, ...p } : m),
        });
    };

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>Privacy Masks</h4>
            <Row label="Masks globally on">
                <input type="checkbox" checked={data.maskEnable === 1}
                    onChange={e => setData({ ...data, maskEnable: e.target.checked ? 1 : 0 })} />
            </Row>
            <div style={{ marginTop: 12, maxHeight: 320, overflowY: 'auto' }}>
                {data.maskList.map((m, i) => (
                    <div key={i} style={{
                        padding: 10, marginBottom: 8, background: 'rgba(255,255,255,0.03)',
                        border: '1px solid rgba(255,255,255,0.06)', borderRadius: 4,
                    }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6 }}>
                            <strong style={{ fontSize: 12, color: 'rgba(255,255,255,0.85)' }}>Mask #{m.index + 1}</strong>
                            <label style={{ fontSize: 11, color: 'rgba(255,255,255,0.5)' }}>
                                <input type="checkbox" checked={m.isexist === 1}
                                    onChange={e => patchMask(i, { isexist: e.target.checked ? 1 : 0 })}
                                    style={{ marginRight: 4 }} />
                                Enabled
                            </label>
                            <label style={{ fontSize: 11, color: 'rgba(255,255,255,0.5)' }}>
                                <input type="checkbox" checked={m.maskShow === 1}
                                    onChange={e => patchMask(i, { maskShow: e.target.checked ? 1 : 0 })}
                                    style={{ marginRight: 4 }} />
                                Visible
                            </label>
                        </div>
                        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 6 }}>
                            <input style={{ ...inputStyle, fontSize: 11 }} placeholder="X" type="number" value={m.maskX}
                                onChange={e => patchMask(i, { maskX: Number(e.target.value) })} />
                            <input style={{ ...inputStyle, fontSize: 11 }} placeholder="Y" type="number" value={m.maskY}
                                onChange={e => patchMask(i, { maskY: Number(e.target.value) })} />
                            <input style={{ ...inputStyle, fontSize: 11 }} placeholder="W" type="number" value={m.maskWidth}
                                onChange={e => patchMask(i, { maskWidth: Number(e.target.value) })} />
                            <input style={{ ...inputStyle, fontSize: 11 }} placeholder="H" type="number" value={m.maskHeight}
                                onChange={e => patchMask(i, { maskHeight: Number(e.target.value) })} />
                        </div>
                        <div style={{ marginTop: 6 }}>
                            <label style={{ fontSize: 11, color: 'rgba(255,255,255,0.5)' }}>Color:</label>
                            <select style={{ ...inputStyle, fontSize: 11, marginLeft: 6, width: 'auto', display: 'inline-block' }}
                                value={m.maskType}
                                onChange={e => patchMask(i, { maskType: Number(e.target.value) })}>
                                {colorNames.map((n, idx) => <option key={idx} value={idx}>{n}</option>)}
                            </select>
                            <input style={{ ...inputStyle, fontSize: 11, marginLeft: 10, width: 'auto', display: 'inline-block' }}
                                placeholder="label" value={m.maskName}
                                onChange={e => patchMask(i, { maskName: e.target.value })} />
                        </div>
                    </div>
                ))}
            </div>
            <div style={{ fontSize: 11, color: 'rgba(255,255,255,0.4)', marginTop: 6 }}>
                Coordinates are in sensor pixels. Use the camera's web UI for visual editing.
            </div>
            <SaveBar onSave={() => save(data)} onReload={reload} saving={saving} statusMsg={statusMsg} />
        </div>
    );
}

// ── System (firmware + datetime + auto-reboot + reboot now) ─────

function SystemTab({ cameraId }: { cameraId: string }) {
    const { data: sys } = usePanel<NetworkPanel>(cameraId, 'system');
    const dtPanel = usePanel<DateTimePanel>(cameraId, 'datetime');
    const dtSaver = useSaver<DateTimePanel>(cameraId, 'datetime');
    const arPanel = usePanel<AutoRebootPanel>(cameraId, 'autoReboot');
    const arSaver = useSaver<AutoRebootPanel>(cameraId, 'autoReboot');
    const [rebooting, setRebooting] = useState(false);
    const [rebootMsg, setRebootMsg] = useState<string | null>(null);

    const reboot = async () => {
        if (!window.confirm('Reboot this camera now? It will be offline ~90 seconds.')) return;
        setRebooting(true);
        setRebootMsg(null);
        try {
            await milesightReboot(cameraId);
            setRebootMsg('Reboot command sent — camera will be offline briefly.');
        } catch (e) {
            setRebootMsg('Error: ' + String(e));
        } finally {
            setRebooting(false);
        }
    };

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>System</h4>
            {sys && (
                <div style={{ fontSize: 12, color: 'rgba(255,255,255,0.65)', marginBottom: 16 }}>
                    <div>Model: <span style={{ color: 'white' }}>{sys.model}</span></div>
                    <div>Firmware: <span style={{ color: 'white' }}>{sys.firmwareVersion}</span></div>
                    <div>HW version: <span style={{ color: 'white' }}>{sys.hardwareVersion}</span></div>
                    <div>Kernel: <span style={{ color: 'white' }}>{sys.kernelVersion}</span></div>
                    <div>Boot time: <span style={{ color: 'white' }}>{sys.systemBootTime}</span></div>
                    <div>MAC: <span style={{ color: 'white' }}>{sys.mac}</span></div>
                </div>
            )}

            <h5 style={{ color: 'rgba(255,255,255,0.7)', marginTop: 18 }}>Date / Time</h5>
            {dtPanel.data && (
                <>
                    <Row label="NTP sync">
                        <input type="checkbox" checked={dtPanel.data.ntpSyncEnable === 1}
                            onChange={e => dtPanel.setData({ ...dtPanel.data!, ntpSyncEnable: e.target.checked ? 1 : 0 })} />
                    </Row>
                    <Row label="NTP server">
                        <input style={inputStyle} value={dtPanel.data.ntpServer}
                            onChange={e => dtPanel.setData({ ...dtPanel.data!, ntpServer: e.target.value })} />
                    </Row>
                    <Row label="Sync interval (min)">
                        <input style={inputStyle} type="number" value={dtPanel.data.ntpInterval}
                            onChange={e => dtPanel.setData({ ...dtPanel.data!, ntpInterval: Number(e.target.value) })} />
                    </Row>
                    <Row label="Time zone">
                        <input style={inputStyle} value={dtPanel.data.timeZoneTz}
                            onChange={e => dtPanel.setData({ ...dtPanel.data!, timeZoneTz: e.target.value })} />
                    </Row>
                    <SaveBar
                        onSave={() => dtPanel.data && dtSaver.save(dtPanel.data)}
                        onReload={dtPanel.reload}
                        saving={dtSaver.saving}
                        statusMsg={dtSaver.statusMsg}
                    />
                </>
            )}

            <h5 style={{ color: 'rgba(255,255,255,0.7)', marginTop: 24 }}>Auto Reboot</h5>
            {arPanel.data && (
                <>
                    <Row label="Scheduled reboot">
                        <input type="checkbox" checked={arPanel.data.rebootEnable === 1}
                            onChange={e => arPanel.setData({ ...arPanel.data!, rebootEnable: e.target.checked ? 1 : 0 })} />
                    </Row>
                    <Row label="Day">
                        <select style={inputStyle} value={arPanel.data.rebootWeekday}
                            onChange={e => arPanel.setData({ ...arPanel.data!, rebootWeekday: Number(e.target.value) })}>
                            <option value={0}>Sunday</option>
                            <option value={1}>Monday</option>
                            <option value={2}>Tuesday</option>
                            <option value={3}>Wednesday</option>
                            <option value={4}>Thursday</option>
                            <option value={5}>Friday</option>
                            <option value={6}>Saturday</option>
                            <option value={7}>Every day</option>
                        </select>
                    </Row>
                    <Row label="Time (HH:MM)">
                        <div style={{ display: 'flex', gap: 6 }}>
                            <input style={{ ...inputStyle, width: 60 }} type="number" min={0} max={23}
                                value={arPanel.data.rebootHour}
                                onChange={e => arPanel.setData({ ...arPanel.data!, rebootHour: Number(e.target.value) })} />
                            <input style={{ ...inputStyle, width: 60 }} type="number" min={0} max={59}
                                value={arPanel.data.rebootMin}
                                onChange={e => arPanel.setData({ ...arPanel.data!, rebootMin: Number(e.target.value) })} />
                        </div>
                    </Row>
                    <SaveBar
                        onSave={() => arPanel.data && arSaver.save(arPanel.data)}
                        onReload={arPanel.reload}
                        saving={arSaver.saving}
                        statusMsg={arSaver.statusMsg}
                    />
                </>
            )}

            <h5 style={{ color: 'rgba(255,255,255,0.7)', marginTop: 24 }}>Reboot Now</h5>
            <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                <button style={btnDanger} onClick={reboot} disabled={rebooting}>
                    {rebooting ? 'Sending…' : '⟲ Reboot camera'}
                </button>
                {rebootMsg && <span style={{ fontSize: 12, color: rebootMsg.startsWith('Error') ? '#ef4444' : '#22c55e' }}>{rebootMsg}</span>}
            </div>
        </div>
    );
}

// ── PTZ Presets ─────────────────────────────────────────────────

function PTZTab({ cameraId }: { cameraId: string }) {
    const { data, error, loading, reload } = usePanel<PTZPresetPanel>(cameraId, 'ptzPresets');
    const [gotoMsg, setGotoMsg] = useState<string | null>(null);

    const handleGoto = async (preset: number) => {
        setGotoMsg(null);
        try {
            await milesightPTZGoto(cameraId, preset);
            setGotoMsg(`Moving to preset ${preset}`);
            setTimeout(() => setGotoMsg(null), 3000);
        } catch (e) {
            setGotoMsg('Error: ' + String(e));
        }
    };

    if (loading) return <div style={{ color: '#9ca3af' }}>Loading…</div>;
    if (error || !data) return <div style={{ color: '#ef4444' }}>Failed: {error ?? 'no data'}</div>;

    const presets = data.presetInfoList ?? [];

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>PTZ Presets</h4>
            {presets.length === 0 ? (
                <div style={{ color: 'rgba(255,255,255,0.5)', fontSize: 13 }}>
                    No presets stored on this camera. Create presets via the camera's native web UI
                    (the vendor's preset-save workflow requires a live video pane).
                </div>
            ) : (
                <table style={{ width: '100%', fontSize: 13, borderCollapse: 'collapse' }}>
                    <thead>
                        <tr style={{ color: '#6b7280', textAlign: 'left', fontSize: 11 }}>
                            <th style={{ padding: '6px 4px' }}>#</th>
                            <th style={{ padding: '6px 4px' }}>Name</th>
                            <th style={{ padding: '6px 4px' }}>Action</th>
                        </tr>
                    </thead>
                    <tbody>
                        {presets.map(p => (
                            <tr key={p.presetIndex} style={{ borderTop: '1px solid rgba(255,255,255,0.06)' }}>
                                <td style={{ padding: '6px 4px' }}>{p.presetIndex}</td>
                                <td style={{ padding: '6px 4px' }}>{p.presetName || <em style={{ color: '#6b7280' }}>(unnamed)</em>}</td>
                                <td style={{ padding: '6px 4px' }}>
                                    <button style={{ ...btnSecondary, padding: '3px 10px', fontSize: 11 }}
                                        onClick={() => handleGoto(p.presetIndex)}>
                                        Go
                                    </button>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            )}
            <div style={{ marginTop: 12, display: 'flex', gap: 10, alignItems: 'center' }}>
                <button style={btnSecondary} onClick={reload}>Reload</button>
                {gotoMsg && <span style={{ fontSize: 12, color: gotoMsg.startsWith('Error') ? '#ef4444' : '#22c55e' }}>{gotoMsg}</span>}
            </div>
        </div>
    );
}

// ── Alarm I/O ───────────────────────────────────────────────────

function AlarmTab({ cameraId }: { cameraId: string }) {
    const inPanel = usePanel<AlarmInputPanel>(cameraId, 'alarmInput');
    const inSaver = useSaver<AlarmInputPanel>(cameraId, 'alarmInput');
    const outPanel = usePanel<AlarmOutputPanel>(cameraId, 'alarmOutput');
    const outSaver = useSaver<AlarmOutputPanel>(cameraId, 'alarmOutput');

    return (
        <div>
            <h4 style={{ marginTop: 0, color: 'rgba(255,255,255,0.85)' }}>Alarm Input</h4>
            {inPanel.loading && <div style={{ color: '#9ca3af' }}>Loading…</div>}
            {inPanel.error && <div style={{ color: '#ef4444' }}>Failed: {inPanel.error}</div>}
            {inPanel.data && (
                <>
                    <div style={{ fontSize: 12, color: 'rgba(255,255,255,0.5)', marginBottom: 8 }}>
                        Camera supports {inPanel.data.supportInputNum} input channel(s).
                    </div>
                    {inPanel.data.inputList.map((inp, idx) => (
                        <div key={inp.index} style={{
                            padding: 10, marginBottom: 8, background: 'rgba(255,255,255,0.03)',
                            border: '1px solid rgba(255,255,255,0.06)', borderRadius: 4,
                        }}>
                            <strong style={{ fontSize: 12, color: 'rgba(255,255,255,0.85)' }}>Input #{inp.index + 1}</strong>
                            <Row label="Enabled">
                                <input type="checkbox" checked={inp.enable === 1}
                                    onChange={e => {
                                        if (!inPanel.data) return;
                                        inPanel.setData({
                                            ...inPanel.data,
                                            inputList: inPanel.data.inputList.map((x, i) =>
                                                i === idx ? { ...x, enable: e.target.checked ? 1 : 0 } : x),
                                        });
                                    }} />
                            </Row>
                            <Row label="Normal state">
                                <select style={inputStyle} value={inp.normal}
                                    onChange={e => {
                                        if (!inPanel.data) return;
                                        inPanel.setData({
                                            ...inPanel.data,
                                            inputList: inPanel.data.inputList.map((x, i) =>
                                                i === idx ? { ...x, normal: Number(e.target.value) as 0 | 1 } : x),
                                        });
                                    }}>
                                    <option value={0}>Normally Open (NO)</option>
                                    <option value={1}>Normally Closed (NC)</option>
                                </select>
                            </Row>
                            <div style={{ fontSize: 11, color: '#6b7280' }}>
                                Current status: {inp.status === 1 ? 'triggered' : 'idle'}
                            </div>
                        </div>
                    ))}
                    <SaveBar
                        onSave={() => inPanel.data && inSaver.save(inPanel.data)}
                        onReload={inPanel.reload}
                        saving={inSaver.saving}
                        statusMsg={inSaver.statusMsg}
                    />
                </>
            )}

            <h4 style={{ marginTop: 24, color: 'rgba(255,255,255,0.85)' }}>Alarm Output (relay)</h4>
            {outPanel.loading && <div style={{ color: '#9ca3af' }}>Loading…</div>}
            {outPanel.data?.setState === 'failed' && (
                <div style={{ color: 'rgba(255,255,255,0.5)', fontSize: 13 }}>
                    This camera does not expose a configurable alarm output.
                    Use the Deterrence button on live view to fire strobe / siren relays.
                </div>
            )}
            {outPanel.data?.outputList && (
                <>
                    {outPanel.data.outputList.map((out, idx) => (
                        <div key={out.index} style={{
                            padding: 10, marginBottom: 8, background: 'rgba(255,255,255,0.03)',
                            border: '1px solid rgba(255,255,255,0.06)', borderRadius: 4,
                        }}>
                            <strong style={{ fontSize: 12, color: 'rgba(255,255,255,0.85)' }}>Output #{out.index + 1}</strong>
                            <Row label="Enabled">
                                <input type="checkbox" checked={out.enable === 1}
                                    onChange={e => {
                                        if (!outPanel.data?.outputList) return;
                                        outPanel.setData({
                                            ...outPanel.data,
                                            outputList: outPanel.data.outputList.map((x, i) =>
                                                i === idx ? { ...x, enable: e.target.checked ? 1 : 0 } : x),
                                        });
                                    }} />
                            </Row>
                            <Row label="Delay (sec)">
                                <input style={inputStyle} type="number" value={out.delayTime}
                                    onChange={e => {
                                        if (!outPanel.data?.outputList) return;
                                        outPanel.setData({
                                            ...outPanel.data,
                                            outputList: outPanel.data.outputList.map((x, i) =>
                                                i === idx ? { ...x, delayTime: Number(e.target.value) } : x),
                                        });
                                    }} />
                            </Row>
                        </div>
                    ))}
                    <SaveBar
                        onSave={() => outPanel.data && outSaver.save(outPanel.data)}
                        onReload={outPanel.reload}
                        saving={outSaver.saving}
                        statusMsg={outSaver.statusMsg}
                    />
                </>
            )}
        </div>
    );
}
