'use client';

// Extracted from SettingsPage.tsx (P1-B-11 session 3). The System tab:
// site-aware health overview, camera-driver fingerprinting, platform
// health metrics, server config form, UI prefs, build info. All inputs
// are passed by prop so the parent owns the settings-draft state machine.

import type { SystemSettings, SystemHealth, Camera } from '@/lib/api';
import { BRAND } from '@/lib/branding';
import { useSites } from '@/hooks/useSites';
import { useMasterCameras } from '@/hooks/useCameraAssignment';

interface Props {
    isAdmin: boolean;
    settings: SystemSettings | null;
    settingsDraft: SystemSettings | null;
    settingsSaving: boolean;
    settingsMsg: { ok: boolean; text: string } | null;
    systemHealth: SystemHealth | null;
    cameras: Camera[];
    toastsEnabled: boolean;
    setToastsEnabled: (v: boolean) => void;
    patchDraft: (key: keyof SystemSettings, val: string | number) => void;
    handleSaveSettings: () => void;
    onRefreshHealth: () => void;
}

export default function SystemTab({
    isAdmin, settings, settingsDraft, settingsSaving, settingsMsg, systemHealth, cameras,
    toastsEnabled, setToastsEnabled, patchDraft, handleSaveSettings, onRefreshHealth,
}: Props) {
    const { data: sites = [] } = useSites();
    const { data: masterCameras = [] } = useMasterCameras();

    // Group cameras by site
    const camerasBySite: Record<string, typeof masterCameras> = {};
    const unassignedCameras: typeof masterCameras = [];
    for (const cam of masterCameras) {
        if ((cam as any).site_id) {
            const sid = (cam as any).site_id;
            if (!camerasBySite[sid]) camerasBySite[sid] = [];
            camerasBySite[sid].push(cam);
        } else {
            unassignedCameras.push(cam);
        }
    }

    // Detect drivers
    const driverCounts: Record<string, number> = {};
    for (const cam of cameras) {
        const mfg = (cam.manufacturer || '').toLowerCase();
        const mdl = (cam.model || '').toLowerCase();
        const driver = mfg.includes('milesight') || mdl.startsWith('ms-c') || mdl.startsWith('ms-n') ? 'Milesight' : 'ONVIF (Generic)';
        driverCounts[driver] = (driverCounts[driver] || 0) + 1;
    }

    return (
        <div className="settings-section" role="tabpanel">
            {/* ── Site Health Overview ── */}
            <div className="settings-section-title">Site Health Overview</div>
            <p className="settings-section-desc" style={{ marginBottom: 12 }}>
                Camera and recording status per site.
                <button className="btn btn-sm" style={{ marginLeft: 8 }} onClick={onRefreshHealth}>↻ Refresh</button>
            </p>

            {sites.length > 0 ? (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 20 }}>
                    {sites.map(site => {
                        const siteCams = camerasBySite[site.id] || [];
                        const online = siteCams.filter((c: any) => c.status === 'connected' || c.status === 'online').length;
                        const recording = siteCams.filter((c: any) => c.recording).length;
                        const mode = (site as any).feature_mode ?? 'security_and_safety';
                        const isSec = mode === 'security_only';
                        return (
                            <div key={site.id} style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 8, padding: '12px 16px', border: '1px solid rgba(255,255,255,0.06)' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
                                    <div style={{ fontWeight: 600, fontSize: 13, flex: 1 }}>{site.name}</div>
                                    <span style={{
                                        fontSize: 8, fontWeight: 700, padding: '2px 6px', borderRadius: 3, letterSpacing: 0.5,
                                        background: isSec ? 'rgba(59,130,246,0.1)' : 'rgba(168,85,247,0.1)',
                                        color: isSec ? '#3B82F6' : '#a855f7',
                                        border: `1px solid ${isSec ? 'rgba(59,130,246,0.3)' : 'rgba(168,85,247,0.3)'}`,
                                    }}>
                                        {isSec ? 'SECURITY' : 'SECURITY + SAFETY'}
                                    </span>
                                    <span style={{ fontSize: 10, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>{site.id}</span>
                                </div>
                                <div style={{ display: 'flex', gap: 16 }}>
                                    <div style={{ textAlign: 'center' }}>
                                        <div style={{ fontSize: 18, fontWeight: 700, color: siteCams.length > 0 ? '#E4E8F0' : '#4A5268' }}>{siteCams.length}</div>
                                        <div style={{ fontSize: 9, color: '#4A5268', letterSpacing: 1, textTransform: 'uppercase' }}>Cameras</div>
                                    </div>
                                    <div style={{ textAlign: 'center' }}>
                                        <div style={{ fontSize: 18, fontWeight: 700, color: online > 0 ? 'var(--accent-green)' : '#4A5268' }}>{online}</div>
                                        <div style={{ fontSize: 9, color: '#4A5268', letterSpacing: 1, textTransform: 'uppercase' }}>Online</div>
                                    </div>
                                    <div style={{ textAlign: 'center' }}>
                                        <div style={{ fontSize: 18, fontWeight: 700, color: siteCams.length > 0 && online < siteCams.length ? 'var(--accent-red)' : '#4A5268' }}>{siteCams.length - online}</div>
                                        <div style={{ fontSize: 9, color: '#4A5268', letterSpacing: 1, textTransform: 'uppercase' }}>Offline</div>
                                    </div>
                                    <div style={{ textAlign: 'center' }}>
                                        <div style={{ fontSize: 18, fontWeight: 700, color: recording > 0 ? 'var(--accent-green)' : '#4A5268' }}>{recording}</div>
                                        <div style={{ fontSize: 9, color: '#4A5268', letterSpacing: 1, textTransform: 'uppercase' }}>Recording</div>
                                    </div>
                                    {(site as any).monitoring_schedule?.length > 0 && (
                                        <div style={{ textAlign: 'center' }}>
                                            <div style={{ fontSize: 18, fontWeight: 700, color: '#E89B2A' }}>{(site as any).monitoring_schedule.filter((w: any) => w.enabled).length}</div>
                                            <div style={{ fontSize: 9, color: '#4A5268', letterSpacing: 1, textTransform: 'uppercase' }}>Schedules</div>
                                        </div>
                                    )}
                                </div>
                                {siteCams.length === 0 && (
                                    <div style={{ fontSize: 10, color: '#E89B2A', marginTop: 6 }}>⚠ No cameras assigned — configure via Sites & Customers → 📷</div>
                                )}
                            </div>
                        );
                    })}

                    {/* Unassigned cameras */}
                    {unassignedCameras.length > 0 && (
                        <div style={{ background: 'rgba(232,155,42,0.04)', borderRadius: 8, padding: '12px 16px', border: '1px solid rgba(232,155,42,0.15)' }}>
                            <div style={{ fontWeight: 600, fontSize: 13, color: '#E89B2A', marginBottom: 6 }}>
                                ⚠ Unassigned Cameras ({unassignedCameras.length})
                            </div>
                            <div style={{ fontSize: 11, color: '#8891A5' }}>
                                These cameras are in the NVR but not assigned to any site. Assign them via Sites & Customers → 📷.
                            </div>
                            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 8 }}>
                                {unassignedCameras.map((cam: any) => (
                                    <span key={cam.id} style={{ fontSize: 10, padding: '2px 8px', borderRadius: 3, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#8891A5' }}>
                                        {cam.name}
                                    </span>
                                ))}
                            </div>
                        </div>
                    )}
                </div>
            ) : (
                <p style={{ opacity: 0.5, fontSize: 13, marginBottom: 20 }}>No sites configured. Create sites in the Sites & Customers tab.</p>
            )}

            {/* ── Camera Drivers ── */}
            <div className="settings-section-title" style={{ marginTop: 8 }}>Camera Drivers</div>
            <p className="settings-section-desc" style={{ marginBottom: 8 }}>
                Active integrations detected from connected cameras.
            </p>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 20 }}>
                {Object.entries(driverCounts).map(([driver, count]) => {
                    const isMilesight = driver === 'Milesight';
                    return (
                        <div key={driver} style={{
                            padding: '10px 16px', borderRadius: 6, display: 'flex', alignItems: 'center', gap: 10,
                            background: isMilesight ? 'rgba(59,130,246,0.06)' : 'rgba(255,255,255,0.03)',
                            border: `1px solid ${isMilesight ? 'rgba(59,130,246,0.2)' : 'rgba(255,255,255,0.06)'}`,
                        }}>
                            <div style={{ fontSize: 20, fontWeight: 700, color: isMilesight ? '#3B82F6' : '#8891A5' }}>{count}</div>
                            <div>
                                <div style={{ fontSize: 12, fontWeight: 600, color: isMilesight ? '#3B82F6' : '#E4E8F0' }}>{driver}</div>
                                <div style={{ fontSize: 9, color: '#4A5268' }}>
                                    {isMilesight ? 'AI analytics · VCA · LPR · People counting' : 'Standard ONVIF Profile S/T'}
                                </div>
                            </div>
                        </div>
                    );
                })}
                {cameras.length === 0 && (
                    <div style={{ fontSize: 12, color: '#4A5268' }}>No cameras connected. Add cameras in the Cameras tab.</div>
                )}
            </div>

            {/* ── Platform Health ── */}
            {systemHealth && (
                <>
                    <div className="settings-section-title" style={{ marginTop: 8 }}>Platform Health</div>
                    <p className="settings-section-desc" style={{ marginBottom: 8 }}>Server-level metrics.</p>
                    <div className="settings-form">
                        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(130px, 1fr))', gap: 10, marginBottom: 16 }}>
                            {[
                                { label: 'Uptime', value: `${Math.floor(systemHealth.uptime_seconds / 3600)}h ${Math.floor((systemHealth.uptime_seconds % 3600) / 60)}m`, color: '' },
                                { label: 'Memory', value: `${systemHealth.memory_mb} MB`, color: '' },
                                { label: 'Goroutines', value: String(systemHealth.goroutines), color: '' },
                                { label: 'Active Streams', value: String(systemHealth.active_streams), color: '' },
                                { label: 'Total Online', value: String(systemHealth.cameras_online), color: 'var(--accent-green)' },
                                { label: 'Total Offline', value: String(systemHealth.cameras_offline), color: systemHealth.cameras_offline > 0 ? 'var(--accent-red)' : '' },
                            ].map(m => (
                                <div key={m.label} style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 8, padding: 10, textAlign: 'center' }}>
                                    <div style={{ fontSize: 20, fontWeight: 700, color: m.color || '#E4E8F0' }}>{m.value}</div>
                                    <div style={{ fontSize: 9, opacity: 0.5, letterSpacing: 1, textTransform: 'uppercase' }}>{m.label}</div>
                                </div>
                            ))}
                        </div>
                        {systemHealth.storage.length > 0 && (
                            <>
                                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 8, opacity: 0.6 }}>Storage Volumes</div>
                                {systemHealth.storage.map((vol, i) => {
                                    const pct = vol.total_bytes ? Math.round(((vol.used_bytes || 0) / vol.total_bytes) * 100) : 0;
                                    const formatGB = (b?: number) => b ? (b / 1073741824).toFixed(1) + ' GB' : '—';
                                    return (
                                        <div key={i} style={{ background: 'rgba(255,255,255,0.03)', borderRadius: 8, padding: 12, marginBottom: 8 }}>
                                            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
                                                <span style={{ fontWeight: 600 }}>{vol.label || vol.path}</span>
                                                <span style={{ fontSize: 12, opacity: 0.6 }}>{formatGB(vol.used_bytes)} / {formatGB(vol.total_bytes)} ({pct}%)</span>
                                            </div>
                                            <div style={{ height: 6, background: 'rgba(255,255,255,0.08)', borderRadius: 3, overflow: 'hidden' }}>
                                                <div style={{
                                                    height: '100%', width: `${pct}%`, borderRadius: 3,
                                                    background: pct > 90 ? 'var(--accent-red)' : pct > 70 ? 'var(--accent-warn)' : 'var(--accent-green)',
                                                    transition: 'width 0.3s',
                                                }} />
                                            </div>
                                        </div>
                                    );
                                })}
                            </>
                        )}
                    </div>
                </>
            )}

            {/* ── Server Config ── */}
            <div className="settings-section-title" style={{ marginTop: 24 }}>Server Configuration</div>
            <p className="settings-section-desc">
                Low-level settings. FFmpeg path changes require a server restart.
            </p>
            {settingsDraft && (
                <div className="settings-form">
                    <div className="settings-form-row">
                        <label className="settings-label">
                            <span className="settings-label-text">FFmpeg Path</span>
                            <span className="settings-label-hint">Full path to the ffmpeg binary</span>
                        </label>
                        <input className="settings-input" type="text" value={settingsDraft.ffmpeg_path} onChange={e => patchDraft('ffmpeg_path', e.target.value)} disabled={!isAdmin} spellCheck={false} />
                    </div>
                    <div className="settings-form-row">
                        <label className="settings-label">
                            <span className="settings-label-text">API Server</span>
                            <span className="settings-label-hint">Backend listen address</span>
                        </label>
                        <input className="settings-input" type="text" value="localhost:8080" disabled readOnly />
                    </div>
                    <div className="settings-form-row">
                        <label className="settings-label">
                            <span className="settings-label-text">Last Settings Update</span>
                            <span className="settings-label-hint">When settings were last saved</span>
                        </label>
                        <input className="settings-input" type="text" value={settings ? new Date(settings.updated_at).toLocaleString() : '—'} disabled readOnly />
                    </div>
                </div>
            )}
            {isAdmin && (
                <div className="settings-actions">
                    <button className="settings-save-btn" onClick={handleSaveSettings} disabled={settingsSaving}>
                        {settingsSaving ? '⏳ Saving…' : '💾 Save System Config'}
                    </button>
                    {settingsMsg && (
                        <span className={`settings-msg ${settingsMsg.ok ? 'ok' : 'err'}`}>{settingsMsg.text}</span>
                    )}
                </div>
            )}

            {/* ── UI Preferences ── */}
            <div className="settings-section-title" style={{ marginTop: 24 }}>UI Preferences</div>
            <div className="settings-form" style={{ marginTop: 8 }}>
                <div className="settings-form-row" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                    <label className="settings-label">
                        <span className="settings-label-text">Toast Notifications</span>
                        <span className="settings-label-hint">Show event alerts as pop-up toasts</span>
                    </label>
                    <button
                        className={`toast-toggle-btn ${toastsEnabled ? 'on' : 'off'}`}
                        onClick={() => setToastsEnabled(!toastsEnabled)}
                        role="switch"
                        aria-checked={toastsEnabled}
                    >
                        <span className="toast-toggle-knob" />
                        <span className="toast-toggle-label">{toastsEnabled ? 'ON' : 'OFF'}</span>
                    </button>
                </div>
            </div>

            {/* ── Build Info ── */}
            <div className="settings-build-info">
                <div className="settings-build-row">
                    <span className="settings-build-label">Application</span>
                    <span className="settings-build-value">{BRAND.name} Platform</span>
                </div>
                <div className="settings-build-row">
                    <span className="settings-build-label">Stack</span>
                    <span className="settings-build-value">Go · Next.js · PostgreSQL · FFmpeg</span>
                </div>
                <div className="settings-build-row">
                    <span className="settings-build-label">Drivers</span>
                    <span className="settings-build-value">{Object.keys(driverCounts).join(' · ') || 'None loaded'}</span>
                </div>
            </div>
        </div>
    );
}
