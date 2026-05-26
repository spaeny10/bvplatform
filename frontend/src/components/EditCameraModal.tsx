'use client';

// Extracted from CameraManager.tsx (P1-B-11 session 16). The Edit
// Camera modal — the biggest single block in CameraManager (~400 lines).
// Tabbed UI: General / Recording / VCA Zones / Milesight. VCA + Milesight
// tabs only render for Milesight-driver cameras (detected from manufacturer
// or model prefix). Sense_pushed cameras get the webhook config callout
// at the top of the General tab.
//
// State: settingsTab + rebooting are local to the modal. editingCamera +
// setEditingCamera are passed from the parent because the parent's camera
// list also reads/writes the same row (this is what makes the "edit blurs
// save back to the parent's list" pattern work).

import { useState } from 'react';
import { type Camera, rebootCamera } from '@/lib/api';
import VCAZoneEditor from './VCAZoneEditor';
import MilesightAdvanced from './MilesightAdvanced';
import SenseWebhookFields from './SenseWebhookFields';
import PPEZoneEditor from './PPEZoneEditor';
import ComplianceRulesPanel from './ComplianceRulesPanel';

// P2-C-04: "vca" tab renamed to "security_vca" for clarity; "ppe_zones" and
// "compliance_rules" tabs added. The distinction matters: security_vca zones
// are pushed to the camera via ONVIF; ppe_zones are Ironsight-server-side only.
type SettingsTab = 'general' | 'recording' | 'security_vca' | 'ppe_zones' | 'compliance_rules' | 'milesight';

type CameraUpdate = Partial<Pick<Camera,
    'name' | 'onvif_address' | 'rtsp_uri' | 'sub_stream_uri' | 'username' |
    'retention_days' | 'recording' | 'recording_mode' |
    'pre_buffer_sec' | 'post_buffer_sec' | 'recording_triggers' |
    'events_enabled' | 'audio_enabled' | 'camera_group' | 'schedule' | 'privacy_mask'>>;

interface Props {
    editingCamera: Camera;
    setEditingCamera: (cam: Camera | null) => void;
    handleUpdate: (id: string, data: CameraUpdate) => Promise<void>;
    onRefresh: () => void;
}

const MILESIGHT_AI_CAPABILITIES = [
    { icon: '🚶', label: 'Human Detection', desc: 'On-camera AI person detection' },
    { icon: '🚙', label: 'Vehicle Detection', desc: 'Car/truck/bike classification' },
    { icon: '🪪', label: 'Face Detection', desc: 'Face capture with metadata' },
    { icon: '🔢', label: 'People Counting', desc: 'In/out counting with zones' },
    { icon: '🚗', label: 'License Plate (LPR)', desc: 'Plate number + color extraction' },
    { icon: '🚧', label: 'Intrusion Detection', desc: 'Region-based VCA alerts' },
    { icon: '➡️', label: 'Line Crossing', desc: 'Directional tripwire with tracking' },
    { icon: '⏱️', label: 'Loitering Detection', desc: 'Dwell time threshold alerts' },
];

export default function EditCameraModal({
    editingCamera, setEditingCamera, handleUpdate, onRefresh,
}: Props) {
    const [settingsTab, setSettingsTab] = useState<SettingsTab>('general');
    const [rebooting, setRebooting] = useState(false);

    const mfg = (editingCamera.manufacturer || '').toLowerCase();
    const mdl = (editingCamera.model || '').toLowerCase();
    const isMilesightCam = mfg.includes('milesight') || mdl.startsWith('ms-c') || mdl.startsWith('ms-n');
    const driverName = isMilesightCam ? 'Milesight' : 'ONVIF';
    const driverColor = isMilesightCam ? '#3B82F6' : '#8891A5';
    const driverBg = isMilesightCam ? 'rgba(59,130,246,0.12)' : 'rgba(255,255,255,0.04)';
    const driverBorder = isMilesightCam ? 'rgba(59,130,246,0.3)' : 'rgba(255,255,255,0.08)';

    return (
        <div className="modal-overlay">
            <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: settingsTab === 'security_vca' || settingsTab === 'ppe_zones' ? 720 : settingsTab === 'milesight' ? 640 : 520, maxHeight: '85vh', display: 'flex', flexDirection: 'column', transition: 'max-width 0.2s' }}>
                <div className="modal-title" style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    Camera Settings — {editingCamera.name}
                    <span style={{ fontSize: 9, fontWeight: 700, padding: '2px 7px', borderRadius: 3, background: driverBg, color: driverColor, border: `1px solid ${driverBorder}`, letterSpacing: 0.3 }}>
                        {driverName.toUpperCase()} DRIVER
                    </span>
                </div>

                {/* Tab Navigation */}
                <div style={{ display: 'flex', gap: 0, borderBottom: '1px solid rgba(255,255,255,0.1)', marginBottom: 16, flexShrink: 0 }}>
                    <button
                        onClick={() => setSettingsTab('general')}
                        style={{
                            flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                            textTransform: 'uppercase', letterSpacing: 1.2,
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: settingsTab === 'general' ? 'var(--accent-green)' : 'rgba(255,255,255,0.4)',
                            borderBottom: settingsTab === 'general' ? '2px solid var(--accent-green)' : '2px solid transparent',
                        }}
                    >
                        General
                    </button>
                    <button
                        onClick={() => setSettingsTab('recording')}
                        style={{
                            flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                            textTransform: 'uppercase', letterSpacing: 1.2,
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: settingsTab === 'recording' ? 'var(--accent-green)' : 'rgba(255,255,255,0.4)',
                            borderBottom: settingsTab === 'recording' ? '2px solid var(--accent-green)' : '2px solid transparent',
                        }}
                    >
                        Recording
                    </button>
                    {isMilesightCam && (
                        <button
                            onClick={() => setSettingsTab('security_vca')}
                            style={{
                                flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                                textTransform: 'uppercase', letterSpacing: 1.2,
                                background: 'none', border: 'none', cursor: 'pointer',
                                color: settingsTab === 'security_vca' ? '#3B82F6' : 'rgba(255,255,255,0.4)',
                                borderBottom: settingsTab === 'security_vca' ? '2px solid #3B82F6' : '2px solid transparent',
                            }}
                            title="Camera-pushed VCA rules (ONVIF). Run on the camera — never interact with Ironsight PPE detection."
                        >
                            Security VCA
                        </button>
                    )}
                    <button
                        onClick={() => setSettingsTab('ppe_zones')}
                        style={{
                            flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                            textTransform: 'uppercase', letterSpacing: 1.2,
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: settingsTab === 'ppe_zones' ? '#22C55E' : 'rgba(255,255,255,0.4)',
                            borderBottom: settingsTab === 'ppe_zones' ? '2px solid #22C55E' : '2px solid transparent',
                        }}
                        title="Ironsight server-side zones for PPE detection filtering. Never sent to the camera."
                    >
                        PPE Zones
                    </button>
                    <button
                        onClick={() => setSettingsTab('compliance_rules')}
                        style={{
                            flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                            textTransform: 'uppercase', letterSpacing: 1.2,
                            background: 'none', border: 'none', cursor: 'pointer',
                            color: settingsTab === 'compliance_rules' ? '#F59E0B' : 'rgba(255,255,255,0.4)',
                            borderBottom: settingsTab === 'compliance_rules' ? '2px solid #F59E0B' : '2px solid transparent',
                        }}
                        title="Bind PPE zones to compliance rules (ppe_required or no_go)."
                    >
                        Rules
                    </button>
                    {isMilesightCam && (
                        <button
                            onClick={() => setSettingsTab('milesight')}
                            style={{
                                flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                                textTransform: 'uppercase', letterSpacing: 1.2,
                                background: 'none', border: 'none', cursor: 'pointer',
                                color: settingsTab === 'milesight' ? '#60a5fa' : 'rgba(255,255,255,0.4)',
                                borderBottom: settingsTab === 'milesight' ? '2px solid #60a5fa' : '2px solid transparent',
                            }}
                        >
                            Milesight
                        </button>
                    )}
                </div>

                <div style={{ overflowY: 'auto', flex: 1, paddingRight: 4 }}>

                    {/* ════════════ SECURITY VCA TAB ════════════ */}
                    {settingsTab === 'security_vca' && isMilesightCam && (
                        <div style={{ padding: '4px 0' }}>
                            {/* IMPORTANT DISTINCTION BANNER — do not remove (scope plan R1 mitigation) */}
                            <div style={{
                                padding: '8px 12px', marginBottom: 10, borderRadius: 5,
                                background: 'rgba(59,130,246,0.06)', border: '1px solid rgba(59,130,246,0.25)',
                            }}>
                                <div style={{ fontSize: 10, color: '#60a5fa', lineHeight: 1.5 }}>
                                    These rules are pushed to the camera firmware via ONVIF. They run on-device and do not interact with Ironsight's PPE detection.
                                </div>
                            </div>
                            <VCAZoneEditor cameraId={editingCamera.id} cameraIp={editingCamera.onvif_address?.replace(/^https?:\/\//, '').replace(/\/.*/, '') || undefined} />
                        </div>
                    )}

                    {/* ════════════ PPE ZONES TAB ════════════ */}
                    {settingsTab === 'ppe_zones' && (
                        <div style={{ padding: '4px 0' }}>
                            {/* IMPORTANT DISTINCTION BANNER — do not remove (scope plan R1 mitigation) */}
                            <div style={{
                                padding: '8px 12px', marginBottom: 10, borderRadius: 5,
                                background: 'rgba(34,197,94,0.05)', border: '1px solid rgba(34,197,94,0.2)',
                            }}>
                                <div style={{ fontSize: 10, color: '#4ade80', lineHeight: 1.5 }}>
                                    These zones are used by Ironsight for PPE detection filtering only. They are never sent to the camera.
                                </div>
                            </div>
                            <PPEZoneEditor cameraId={editingCamera.id} />
                        </div>
                    )}

                    {/* ════════════ COMPLIANCE RULES TAB ════════════ */}
                    {settingsTab === 'compliance_rules' && (
                        <div style={{ padding: '4px 0' }}>
                            <ComplianceRulesPanel cameraId={editingCamera.id} />
                        </div>
                    )}

                    {/* ════════════ MILESIGHT TAB ════════════ */}
                    {settingsTab === 'milesight' && isMilesightCam && (
                        <MilesightAdvanced cameraId={editingCamera.id} />
                    )}

                    {/* ════════════ GENERAL TAB ════════════ */}
                    {settingsTab === 'general' && (
                        <>
                            {editingCamera.device_class === 'sense_pushed' && editingCamera.sense_webhook_token && (
                                <div style={{
                                    padding: 12,
                                    marginBottom: 12,
                                    border: '1px solid rgba(34,197,94,0.25)',
                                    background: 'rgba(34,197,94,0.04)',
                                    borderRadius: 6,
                                }}>
                                    <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--accent-green)', letterSpacing: 0.5, marginBottom: 4 }}>
                                        ALARM-SERVER WEBHOOK
                                    </div>
                                    <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 10, lineHeight: 1.5 }}>
                                        Paste these into the camera's Event → Alarm Settings → Alarm Server form.
                                        User Name and Password stay blank.
                                    </div>
                                    <SenseWebhookFields
                                        url={`${typeof window !== 'undefined' ? window.location.origin : ''}/api/integrations/milesight/sense/${editingCamera.sense_webhook_token}`}
                                    />
                                </div>
                            )}

                            <div className="form-group">
                                <label className="form-label">Camera Name</label>
                                <input
                                    className="form-input"
                                    defaultValue={editingCamera.name}
                                    onBlur={(e) => {
                                        if (e.target.value !== editingCamera.name) {
                                            handleUpdate(editingCamera.id, { name: e.target.value });
                                        }
                                    }}
                                />
                            </div>

                            <div className="form-group">
                                <label className="form-label">Camera Group / Zone</label>
                                <input
                                    className="form-input"
                                    placeholder="e.g., Perimeter, Interior, Parking"
                                    defaultValue={editingCamera.camera_group || ''}
                                    onBlur={(e) => {
                                        if (e.target.value !== (editingCamera.camera_group || '')) {
                                            handleUpdate(editingCamera.id, { camera_group: e.target.value });
                                        }
                                    }}
                                />
                            </div>

                            {/* ── Network / Address ──────────────────────── */}
                            <div style={{ borderTop: '1px solid rgba(255,255,255,0.08)', margin: '16px 0 12px', paddingTop: 12 }}>
                                <div style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: 1.5, color: 'rgba(255,255,255,0.35)', marginBottom: 8 }}>Network</div>
                            </div>

                            <div className="form-group">
                                <label className="form-label">Device Address</label>
                                <input
                                    className="form-input"
                                    placeholder="e.g. 192.168.1.100"
                                    defaultValue={editingCamera.onvif_address || ''}
                                    onBlur={(e) => {
                                        if (e.target.value !== (editingCamera.onvif_address || '')) {
                                            handleUpdate(editingCamera.id, { onvif_address: e.target.value });
                                            setEditingCamera({ ...editingCamera, onvif_address: e.target.value });
                                        }
                                    }}
                                />
                            </div>

                            <div className="form-group">
                                <label className="form-label">RTSP URI (main stream)</label>
                                <input
                                    className="form-input"
                                    placeholder="rtsp://user:pass@192.168.1.100:554/stream1"
                                    defaultValue={editingCamera.rtsp_uri || ''}
                                    onBlur={(e) => {
                                        if (e.target.value !== (editingCamera.rtsp_uri || '')) {
                                            handleUpdate(editingCamera.id, { rtsp_uri: e.target.value });
                                            setEditingCamera({ ...editingCamera, rtsp_uri: e.target.value });
                                        }
                                    }}
                                />
                                <div style={{ fontSize: 10, color: 'rgba(255,255,255,0.3)', marginTop: 4 }}>Used for recording and high-quality live view</div>
                            </div>

                            <div className="form-group">
                                <label className="form-label">RTSP URI (sub stream)</label>
                                <input
                                    className="form-input"
                                    placeholder="rtsp://user:pass@192.168.1.100:554/stream2"
                                    defaultValue={editingCamera.sub_stream_uri || ''}
                                    onBlur={(e) => {
                                        if (e.target.value !== (editingCamera.sub_stream_uri || '')) {
                                            handleUpdate(editingCamera.id, { sub_stream_uri: e.target.value });
                                            setEditingCamera({ ...editingCamera, sub_stream_uri: e.target.value });
                                        }
                                    }}
                                />
                                <div style={{ fontSize: 10, color: 'rgba(255,255,255,0.3)', marginTop: 4 }}>Lower resolution stream used for grid thumbnails</div>
                            </div>

                            <div className="form-group">
                                <label className="form-label">Username</label>
                                <input
                                    className="form-input"
                                    placeholder="admin"
                                    defaultValue={editingCamera.username || ''}
                                    onBlur={(e) => {
                                        if (e.target.value !== (editingCamera.username || '')) {
                                            handleUpdate(editingCamera.id, { username: e.target.value });
                                            setEditingCamera({ ...editingCamera, username: e.target.value });
                                        }
                                    }}
                                />
                            </div>

                            {/* ── Device Info ──────────────────────────── */}
                            <div style={{ borderTop: '1px solid rgba(255,255,255,0.08)', margin: '16px 0 12px', paddingTop: 12 }}>
                                <div style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: 1.5, color: 'rgba(255,255,255,0.35)', marginBottom: 8 }}>Device Info</div>
                            </div>

                            <div className="camera-card-detail">
                                <span>Driver</span>
                                <span style={{ fontWeight: 600, color: driverColor }}>{driverName}</span>
                            </div>
                            <div className="camera-card-detail">
                                <span>Manufacturer</span>
                                <span>{editingCamera.manufacturer || '—'}</span>
                            </div>
                            <div className="camera-card-detail">
                                <span>Model</span>
                                <span>{editingCamera.model || '—'}</span>
                            </div>
                            <div className="camera-card-detail">
                                <span>Firmware</span>
                                <span>{editingCamera.firmware || '—'}</span>
                            </div>
                            <div className="camera-card-detail">
                                <span>PTZ Support</span>
                                <span>{editingCamera.has_ptz ? '✅ Yes' : '❌ No'}</span>
                            </div>
                        </>
                    )}

                    {/* ════════════ RECORDING TAB ════════════ */}
                    {/*
                      * Recording policy (retention / mode / buffers / schedule / event-mode
                      * triggers) moved to the site level in the 2026-04 migration. The
                      * camera-side tab now only exposes per-camera operational flags:
                      * audio, ONVIF event subscription, privacy-mask flag, and the
                      * Milesight AI capability readout. The recorder reads its policy
                      * directly from the camera's site at start/restart time.
                      */}
                    {settingsTab === 'recording' && (
                        <>
                            <div style={{
                                padding: '10px 12px', marginBottom: 14, borderRadius: 6,
                                background: 'rgba(59,130,246,0.06)', border: '1px solid rgba(59,130,246,0.25)',
                                fontSize: 11, color: '#93c5fd', lineHeight: 1.5,
                            }}>
                                Recording mode, buffers, schedule, triggers, and retention are now
                                set per <strong>site</strong>, not per camera. Open <em>Admin → Sites &amp; Customers →
                                [site] → Recording &amp; Retention</em> to change them. This tab only
                                controls per-camera operational flags.
                            </div>

                            <div className="form-group">
                                <label className="form-label" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                                    <span>🔊 Audio Recording</span>
                                    <button
                                        className={`btn btn-sm ${editingCamera.audio_enabled !== false ? 'btn-primary' : ''}`}
                                        style={{ minWidth: 50 }}
                                        onClick={() => {
                                            const val = !(editingCamera.audio_enabled !== false);
                                            handleUpdate(editingCamera.id, { audio_enabled: val });
                                            setEditingCamera({ ...editingCamera, audio_enabled: val });
                                        }}
                                    >
                                        {editingCamera.audio_enabled !== false ? 'ON' : 'OFF'}
                                    </button>
                                </label>
                                <span style={{ fontSize: 11, opacity: 0.5 }}>Per-camera: whether audio tracks are encoded into segments.</span>
                            </div>

                            {/* ── Events ────────────────────────── */}
                            <div style={{ borderTop: '1px solid rgba(255,255,255,0.08)', margin: '16px 0 12px', paddingTop: 12 }}>
                                <div style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: 1.5, color: 'rgba(255,255,255,0.35)', marginBottom: 8 }}>
                                    {driverName} Events
                                </div>
                            </div>

                            <div className="form-group">
                                <label className="form-label" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                                    <span>📡 Event Subscription</span>
                                    <button
                                        className={`btn btn-sm ${editingCamera.events_enabled !== false ? 'btn-primary' : ''}`}
                                        style={{ minWidth: 50 }}
                                        onClick={() => {
                                            const val = !(editingCamera.events_enabled !== false);
                                            handleUpdate(editingCamera.id, { events_enabled: val });
                                            setEditingCamera({ ...editingCamera, events_enabled: val });
                                        }}
                                    >
                                        {editingCamera.events_enabled !== false ? 'ON' : 'OFF'}
                                    </button>
                                </label>
                                <span style={{ fontSize: 11, opacity: 0.5 }}>Subscribe to this camera's ONVIF event stream. Events index historical footage regardless of recording mode.</span>
                            </div>

                            {/* ── Milesight AI Features ── */}
                            {isMilesightCam && (
                                <>
                                    <div style={{ borderTop: '1px solid rgba(59,130,246,0.15)', margin: '16px 0 12px', paddingTop: 12 }}>
                                        <div style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: 1.5, color: '#3B82F6', marginBottom: 8, display: 'flex', alignItems: 'center', gap: 6 }}>
                                            Milesight AI Capabilities
                                            <span style={{ fontSize: 8, padding: '1px 5px', borderRadius: 3, background: 'rgba(59,130,246,0.12)', border: '1px solid rgba(59,130,246,0.25)', fontWeight: 700, letterSpacing: 0.5 }}>DRIVER</span>
                                        </div>
                                    </div>

                                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
                                        {MILESIGHT_AI_CAPABILITIES.map(cap => (
                                            <div key={cap.label} style={{
                                                padding: '8px 10px', borderRadius: 5,
                                                background: 'rgba(59,130,246,0.04)',
                                                border: '1px solid rgba(59,130,246,0.12)',
                                                display: 'flex', alignItems: 'flex-start', gap: 8,
                                            }}>
                                                <span style={{ fontSize: 14, flexShrink: 0, marginTop: 1 }}>{cap.icon}</span>
                                                <div>
                                                    <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>{cap.label}</div>
                                                    <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1 }}>{cap.desc}</div>
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                    <div style={{ fontSize: 10, color: '#4A5268', marginTop: 8, lineHeight: 1.5 }}>
                                        These capabilities are processed on-camera by Milesight AI firmware. Event metadata (confidence, bounding boxes, plate numbers, counts) is extracted by the Milesight driver and stored with each event.
                                    </div>
                                </>
                            )}

                            {/* ── Privacy ─────────────────────────────── */}
                            <div style={{ borderTop: '1px solid rgba(255,255,255,0.08)', margin: '16px 0 12px', paddingTop: 12 }}>
                                <div style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: 1.5, color: 'rgba(255,255,255,0.35)', marginBottom: 8 }}>Privacy & Compliance</div>
                            </div>

                            <div className="form-group">
                                <label className="form-label" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                                    <span>🔒 Privacy Mask Enabled</span>
                                    <button
                                        className={`btn btn-sm ${editingCamera.privacy_mask ? 'btn-primary' : ''}`}
                                        style={{ minWidth: 50 }}
                                        onClick={() => {
                                            const val = !editingCamera.privacy_mask;
                                            handleUpdate(editingCamera.id, { privacy_mask: val });
                                            setEditingCamera({ ...editingCamera, privacy_mask: val });
                                        }}
                                    >
                                        {editingCamera.privacy_mask ? 'ON' : 'OFF'}
                                    </button>
                                </label>
                                <span style={{ fontSize: 11, opacity: 0.5 }}>Mark this camera as having privacy zone requirements</span>
                            </div>
                        </>
                    )}

                </div>

                <div className="modal-actions" style={{ flexShrink: 0, borderTop: '1px solid rgba(255,255,255,0.08)', paddingTop: 12, marginTop: 12, display: 'flex', justifyContent: 'space-between' }}>
                    <button
                        className="btn"
                        style={{ borderColor: 'rgba(239,68,68,0.4)', color: '#ef4444' }}
                        disabled={rebooting}
                        onClick={async () => {
                            if (!confirm(`Reboot ${editingCamera.name}? The camera will be unreachable for 30–90 seconds and recording will pause.`)) return;
                            setRebooting(true);
                            try {
                                const r = await rebootCamera(editingCamera.id);
                                alert(`Reboot requested: ${r.message}`);
                                onRefresh();
                            } catch (e: any) {
                                alert(`Reboot failed: ${e?.message ?? String(e)}`);
                            } finally {
                                setRebooting(false);
                            }
                        }}
                    >
                        {rebooting ? 'Rebooting…' : 'Reboot device'}
                    </button>
                    <button className="btn" onClick={() => setEditingCamera(null)}>Close</button>
                </div>
            </div>
        </div>
    );
}
