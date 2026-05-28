'use client';

// Shared building blocks for the Milesight Advanced tabs (P1-B-11 session 4).
// Extracted from MilesightAdvanced.tsx so each tab can be its own file.
// Re-exporting these from one module keeps the visual style consistent
// across tabs without each tab carrying its own copy.

import { useCallback, useEffect, useState } from 'react';
import { milesightGet, milesightSet, type VideoStream } from '@/lib/milesight';

// ── Generic data hooks ──

export function usePanel<T>(cameraId: string, panel: string) {
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

export function useSaver<T extends object>(cameraId: string, panel: string) {
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

// ── Layout primitives ──

export function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div style={{ display: 'grid', gridTemplateColumns: '160px 1fr', gap: 12, marginBottom: 10, alignItems: 'center' }}>
            <label style={{ fontSize: 12, color: 'rgba(255,255,255,0.6)' }}>{label}</label>
            <div>{children}</div>
        </div>
    );
}

export const inputStyle: React.CSSProperties = {
    width: '100%', background: 'rgba(0,0,0,0.3)', border: '1px solid rgba(255,255,255,0.1)',
    color: 'white', padding: '6px 10px', fontSize: 13, borderRadius: 4,
};

export const btnPrimary: React.CSSProperties = {
    background: '#3B82F6', border: 'none', color: 'white', padding: '8px 18px',
    fontSize: 12, fontWeight: 600, cursor: 'pointer', borderRadius: 4,
};
export const btnDanger: React.CSSProperties = { ...btnPrimary, background: '#dc2626' };
export const btnSecondary: React.CSSProperties = { ...btnPrimary, background: 'rgba(255,255,255,0.08)' };

export function SaveBar({ onSave, onReload, saving, statusMsg }: {
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

// ── Stream resolution presets + editor ──

// Common resolutions the Milesight lineup supports. Presets keep users
// from typing arbitrary dimensions that the sensor can't produce — the
// camera accepts them but silently clamps, which looks like "Save didn't
// work" on the next reload.
export const RESOLUTION_PRESETS: { label: string; w: number; h: number }[] = [
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

export function resolutionKey(s: VideoStream): string {
    const match = RESOLUTION_PRESETS.find(p => p.w === s.width && p.h === s.height);
    return match ? `${s.width}×${s.height}` : `custom:${s.width}×${s.height}`;
}

export function StreamEditor({ label, stream, onChange, canDisable }: {
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
