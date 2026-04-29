'use client';

import { useState } from 'react';
import { Camera, createCamera, updateCamera, deleteCamera, rebootCamera, discoverCameras, getDevicePreview, DiscoveredDevice } from '@/lib/api';
import VCAZoneEditor from '@/components/VCAZoneEditor';
import MilesightAdvanced from '@/components/MilesightAdvanced';

interface CameraManagerProps {
    cameras: Camera[];
    onRefresh: () => void;
}

export default function CameraManager({ cameras, onRefresh }: CameraManagerProps) {
    const [showAddModal, setShowAddModal] = useState(false);
    const [showDiscovery, setShowDiscovery] = useState(false);
    const [discovering, setDiscovering] = useState(false);
    const [discoveredDevices, setDiscoveredDevices] = useState<DiscoveredDevice[]>([]);
    const [selectedDevices, setSelectedDevices] = useState<Set<string>>(new Set());
    const [discoveryAuth, setDiscoveryAuth] = useState({ username: 'admin', password: '' });
    const [addingBulk, setAddingBulk] = useState(false);
    const [cameraPreviews, setCameraPreviews] = useState<Record<string, string>>({});
    const [loadingPreviews, setLoadingPreviews] = useState<Set<string>>(new Set());
    const [editingCamera, setEditingCamera] = useState<Camera | null>(null);
    const [settingsTab, setSettingsTab] = useState<'general' | 'recording' | 'vca' | 'milesight'>('general');
    const [rebooting, setRebooting] = useState(false);

    // Add camera form state
    const [addForm, setAddForm] = useState({
        name: '',
        onvif_address: '',
        username: 'admin',
        password: '',
        device_class: 'continuous' as 'continuous' | 'sense_pushed',
    });

    const [addError, setAddError] = useState<string | null>(null);
    const [adding, setAdding] = useState(false);
    // Post-create webhook details for sense cameras. Shown in a follow-up
    // panel so the operator can copy the URL into the camera's Alarm
    // Server config — there's no other way to retrieve the token after
    // dismissing this view (it's stored, but not echoed in list views).
    const [senseSetup, setSenseSetup] = useState<{ url: string; cameraName: string } | null>(null);

    const handleAdd = async () => {
        setAddError(null);
        setAdding(true);
        try {
            const created = await createCamera(addForm);
            setShowAddModal(false);
            setAddForm({ name: '', onvif_address: '', username: 'admin', password: '', device_class: 'continuous' });
            // For sense_pushed cameras the create response carries the
            // freshly-minted webhook token. Build the absolute URL once
            // and present it; for continuous cameras nothing extra to show.
            if (created?.device_class === 'sense_pushed' && created?.sense_webhook_token) {
                const origin = typeof window !== 'undefined' ? window.location.origin : '';
                setSenseSetup({
                    url: `${origin}/api/integrations/milesight/sense/${created.sense_webhook_token}`,
                    cameraName: created.name,
                });
            }
            onRefresh();
        } catch (err: any) {
            setAddError(err?.message || 'Failed to add camera — check the address, credentials, and that ONVIF is enabled.');
        } finally {
            setAdding(false);
        }
    };

    const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [confirmBulkDelete, setConfirmBulkDelete] = useState(false);
    const [bulkDeleting, setBulkDeleting] = useState(false);

    const toggleSelect = (id: string) => {
        setSelected(prev => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id); else next.add(id);
            return next;
        });
    };

    const toggleSelectAll = () => {
        if (selected.size === cameras.length) {
            setSelected(new Set());
        } else {
            setSelected(new Set(cameras.map(c => c.id)));
        }
    };

    const handleBulkDelete = async () => {
        setBulkDeleting(true);
        const ids = Array.from(selected);
        for (let i = 0; i < ids.length; i++) {
            try { await deleteCamera(ids[i]); } catch { /* continue */ }
        }
        setSelected(new Set());
        setConfirmBulkDelete(false);
        setBulkDeleting(false);
        onRefresh();
    };

    const handleDelete = async (id: string) => {
        try {
            await deleteCamera(id);
            setConfirmDeleteId(null);
            onRefresh();
        } catch (err) {
            console.error('Failed to delete camera:', err);
        }
    };

    const handleUpdate = async (id: string, data: Partial<Pick<Camera,
        'name' | 'onvif_address' | 'rtsp_uri' | 'sub_stream_uri' | 'username' |
        'retention_days' | 'recording' | 'recording_mode' |
        'pre_buffer_sec' | 'post_buffer_sec' | 'recording_triggers' |
        'events_enabled' | 'audio_enabled' | 'camera_group' | 'schedule' | 'privacy_mask'>>) => {
        try {
            await updateCamera(id, data);
            // Don't close the modal — just refresh the list in the background
            onRefresh();
        } catch (err) {
            console.error('Failed to update camera:', err);
        }
    };

    const handleDiscover = async () => {
        setDiscovering(true);
        setShowDiscovery(true);
        setSelectedDevices(new Set());
        try {
            const devices = await discoverCameras();
            setDiscoveredDevices(devices);
        } catch (err) {
            console.error('Discovery failed:', err);
        }
        setDiscovering(false);
    };

    const toggleDeviceSelection = (address: string) => {
        const newSelected = new Set(selectedDevices);
        if (newSelected.has(address)) {
            newSelected.delete(address);
        } else {
            newSelected.add(address);
        }
        setSelectedDevices(newSelected);
    };

    const toggleAllDevices = () => {
        if (selectedDevices.size === discoveredDevices.length) {
            setSelectedDevices(new Set());
        } else {
            setSelectedDevices(new Set(discoveredDevices.map(d => d.address)));
        }
    };

    const handleLoadPreview = async (address: string) => {
        setLoadingPreviews(prev => new Set(prev).add(address));
        try {
            const url = await getDevicePreview(address, discoveryAuth);
            setCameraPreviews(prev => ({ ...prev, [address]: url }));
        } catch (err) {
            console.error('Failed to load preview:', err);
            // Optionally, we could set an error state here, but for now just fail silently
        }
        setLoadingPreviews(prev => {
            const next = new Set(prev);
            next.delete(address);
            return next;
        });
    };

    const handleBulkAdd = async () => {
        if (selectedDevices.size === 0) return;
        setAddingBulk(true);
        try {
            const promises = Array.from(selectedDevices).map(address => {
                const device = discoveredDevices.find(d => d.address === address);
                if (!device) return Promise.resolve();
                return createCamera({
                    name: device.name || device.address,
                    onvif_address: device.address,
                    username: discoveryAuth.username,
                    password: discoveryAuth.password,
                });
            });
            await Promise.allSettled(promises);
            setShowDiscovery(false);
            setDiscoveryAuth({ username: 'admin', password: '' });
            onRefresh();
        } catch (err) {
            console.error('Failed to bulk add cameras:', err);
        }
        setAddingBulk(false);
    };

    const addDiscoveredDevice = (device: DiscoveredDevice) => {
        setAddForm({
            name: device.name || device.address,
            onvif_address: device.address,
            username: 'admin',
            password: '',
            device_class: 'continuous',
        });
        setShowDiscovery(false);
        setShowAddModal(true);
    };

    return (
        <div className="page-container">
            <div className="page-header">
                <h1 className="page-title">Camera Management</h1>
                <div className="flex gap-2">
                    <button className="btn" onClick={handleDiscover}>
                        🔍 Discover
                    </button>
                    <button className="btn btn-primary" onClick={() => setShowAddModal(true)}>
                        ➕ Add Camera
                    </button>
                </div>
            </div>

            {/* Camera List */}
            {cameras.length === 0 ? (
                <div className="empty-state">
                    <div className="empty-state-icon">📷</div>
                    <div className="empty-state-title">No Cameras</div>
                    <div className="empty-state-desc">
                        Add an ONVIF camera to get started. Use the Discover button to scan your network, or add one manually.
                    </div>
                    <button className="btn btn-primary" onClick={handleDiscover}>
                        🔍 Discover Cameras
                    </button>
                </div>
            ) : (
                <div className="cam-list">
                    {/* Bulk actions bar */}
                    {selected.size > 0 && (
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 12px', background: 'rgba(239,68,68,0.06)', border: '1px solid rgba(239,68,68,0.15)', borderRadius: 6, marginBottom: 8 }}>
                            <span style={{ fontSize: 12, fontWeight: 600, color: '#E4E8F0' }}>
                                {selected.size} camera{selected.size !== 1 ? 's' : ''} selected
                            </span>
                            {confirmBulkDelete ? (
                                <>
                                    <span style={{ fontSize: 11, color: '#EF4444', fontWeight: 600 }}>Delete all selected?</span>
                                    <button
                                        className="btn"
                                        onClick={handleBulkDelete}
                                        disabled={bulkDeleting}
                                        style={{ fontSize: 11, padding: '4px 12px', background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.4)', color: '#EF4444', fontWeight: 700 }}
                                    >
                                        {bulkDeleting ? `Deleting ${selected.size}...` : 'Yes, Delete'}
                                    </button>
                                    <button
                                        className="btn"
                                        onClick={() => setConfirmBulkDelete(false)}
                                        style={{ fontSize: 11, padding: '4px 12px' }}
                                    >
                                        Cancel
                                    </button>
                                </>
                            ) : (
                                <>
                                    <button
                                        className="btn"
                                        onClick={() => setConfirmBulkDelete(true)}
                                        style={{ fontSize: 11, padding: '4px 12px', background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', color: '#EF4444' }}
                                    >
                                        🗑️ Delete Selected
                                    </button>
                                    <button
                                        className="btn"
                                        onClick={() => setSelected(new Set())}
                                        style={{ fontSize: 11, padding: '4px 12px' }}
                                    >
                                        Clear
                                    </button>
                                </>
                            )}
                        </div>
                    )}

                    {/* Header Row */}
                    <div className="cam-list-header">
                        <div className="cam-list-col" style={{ width: 32, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                            <input
                                type="checkbox"
                                checked={cameras.length > 0 && selected.size === cameras.length}
                                onChange={toggleSelectAll}
                                style={{ cursor: 'pointer', accentColor: '#3B82F6' }}
                                title="Select all"
                            />
                        </div>
                        <div className="cam-list-col cam-col-status">Status</div>
                        <div className="cam-list-col cam-col-name">Name</div>
                        <div className="cam-list-col cam-col-address">Address</div>
                        <div className="cam-list-col cam-col-mfg">Driver</div>
                        <div className="cam-list-col cam-col-model">Model</div>
                        <div className="cam-list-col cam-col-retention">Retention</div>
                        <div className="cam-list-col cam-col-rec">Recording</div>
                        <div className="cam-list-col cam-col-mode">Mode</div>
                        <div className="cam-list-col cam-col-actions">Actions</div>
                    </div>

                    {/* Data Rows */}
                    {cameras.map((camera) => (
                        <div key={camera.id} className={`cam-list-row ${camera.status}`}>
                            {/* Checkbox */}
                            <div className="cam-list-col" style={{ width: 32, flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                                <input
                                    type="checkbox"
                                    checked={selected.has(camera.id)}
                                    onChange={() => toggleSelect(camera.id)}
                                    style={{ cursor: 'pointer', accentColor: '#3B82F6' }}
                                />
                            </div>
                            {/* Status indicator */}
                            <div className="cam-list-col cam-col-status">
                                <span className={`cam-status-dot ${camera.status}`} />
                                <span className={`cam-status-label ${camera.status}`}>{camera.status}</span>
                            </div>

                            {/* Name */}
                            <div className="cam-list-col cam-col-name">
                                <span className="cam-name">{camera.name}</span>
                            </div>

                            {/* Address */}
                            <div className="cam-list-col cam-col-address">
                                <span className="cam-address font-mono">{camera.onvif_address}</span>
                            </div>

                            {/* Driver */}
                            <div className="cam-list-col cam-col-mfg truncate">
                                {(() => {
                                    const mfg = (camera.manufacturer || '').toLowerCase();
                                    const model = (camera.model || '').toLowerCase();
                                    const isMilesight = mfg.includes('milesight') || model.startsWith('ms-c') || model.startsWith('ms-n');
                                    return isMilesight ? (
                                        <span style={{ fontSize: 9, fontWeight: 700, padding: '2px 7px', borderRadius: 3, background: 'rgba(59,130,246,0.12)', color: '#3B82F6', border: '1px solid rgba(59,130,246,0.3)', letterSpacing: 0.3 }}>
                                            MILESIGHT
                                        </span>
                                    ) : (
                                        <span style={{ fontSize: 9, fontWeight: 600, padding: '2px 7px', borderRadius: 3, background: 'rgba(255,255,255,0.04)', color: '#8891A5', border: '1px solid rgba(255,255,255,0.08)', letterSpacing: 0.3 }}>
                                            ONVIF
                                        </span>
                                    );
                                })()}
                            </div>

                            {/* Model */}
                            <div className="cam-list-col cam-col-model truncate">
                                {camera.model || '—'}
                            </div>

                            {/* Retention */}
                            <div className="cam-list-col cam-col-retention">
                                <span className="font-mono">{camera.retention_days}</span>
                                <span className="cam-unit">days</span>
                            </div>

                            {/* Recording */}
                            <div className="cam-list-col cam-col-rec">
                                <span className={`cam-rec-badge ${camera.recording ? 'active' : 'stopped'}`}>
                                    <span className="cam-rec-dot" />
                                    {camera.recording ? 'REC' : 'OFF'}
                                </span>
                            </div>

                            {/* Mode */}
                            <div className="cam-list-col cam-col-mode">
                                <span className="cam-mode-tag">
                                    {camera.recording_mode === 'event' ? 'EVENT' : 'CONT'}
                                </span>
                            </div>

                            {/* Actions */}
                            <div className="cam-list-col cam-col-actions">
                                <button
                                    className="cam-action-btn"
                                    title={camera.recording ? 'Stop recording' : 'Start recording'}
                                    onClick={() => handleUpdate(camera.id, { recording: !camera.recording })}
                                >
                                    {camera.recording ? '⏹' : '⏺'}
                                </button>
                                <button
                                    className="cam-action-btn"
                                    title="Camera settings"
                                    onClick={() => setEditingCamera(camera)}
                                >
                                    ⚙️
                                </button>
                                {confirmDeleteId === camera.id ? (
                                    <>
                                        <button
                                            className="cam-action-btn cam-action-danger"
                                            title="Confirm delete"
                                            onClick={() => handleDelete(camera.id)}
                                            style={{ fontSize: 10, fontWeight: 700, padding: '2px 6px' }}
                                        >
                                            Yes
                                        </button>
                                        <button
                                            className="cam-action-btn"
                                            title="Cancel"
                                            onClick={() => setConfirmDeleteId(null)}
                                            style={{ fontSize: 10, padding: '2px 6px' }}
                                        >
                                            No
                                        </button>
                                    </>
                                ) : (
                                    <button
                                        className="cam-action-btn cam-action-danger"
                                        title="Delete camera"
                                        onClick={() => setConfirmDeleteId(camera.id)}
                                    >
                                        🗑️
                                    </button>
                                )}
                            </div>
                        </div>
                    ))}
                </div>
            )}

            {/* Add Camera Modal */}
            {showAddModal && (
                <div className="modal-overlay" onClick={() => setShowAddModal(false)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()}>
                        <div className="modal-title">Add Camera</div>

                        <div className="form-group">
                            <label className="form-label">Camera Type</label>
                            <select
                                className="form-input"
                                value={addForm.device_class}
                                onChange={(e) => setAddForm({ ...addForm, device_class: e.target.value as 'continuous' | 'sense_pushed' })}
                            >
                                <option value="continuous">Continuous (RTSP + ONVIF events)</option>
                                <option value="sense_pushed">Sense / push-only (Milesight SC4xx, PIR/solar)</option>
                            </select>
                            <div style={{ fontSize: 10, color: 'var(--text-muted)', marginTop: 4, lineHeight: 1.4 }}>
                                {addForm.device_class === 'sense_pushed'
                                    ? 'Skips RTSP and ONVIF event subscription. Camera will POST to a webhook URL we issue after creation — paste it into the camera’s Alarm Server config.'
                                    : 'Default for normal IP cameras. Pulls RTSP + subscribes to ONVIF events.'}
                            </div>
                        </div>

                        <div className="form-group">
                            <label className="form-label">Camera Name</label>
                            <input
                                className="form-input"
                                placeholder="e.g., Front Door"
                                value={addForm.name}
                                onChange={(e) => setAddForm({ ...addForm, name: e.target.value })}
                            />
                        </div>

                        <div className="form-group">
                            <label className="form-label">ONVIF Address</label>
                            <input
                                className="form-input"
                                placeholder="e.g., 192.168.1.100:80"
                                value={addForm.onvif_address}
                                onChange={(e) => setAddForm({ ...addForm, onvif_address: e.target.value })}
                            />
                        </div>

                        <div className="form-group">
                            <label className="form-label">Username</label>
                            <input
                                className="form-input"
                                placeholder="admin"
                                value={addForm.username}
                                onChange={(e) => setAddForm({ ...addForm, username: e.target.value })}
                            />
                        </div>

                        <div className="form-group">
                            <label className="form-label">Password</label>
                            <input
                                className="form-input"
                                type="password"
                                placeholder="••••••••"
                                value={addForm.password}
                                onChange={(e) => setAddForm({ ...addForm, password: e.target.value })}
                            />
                        </div>

                        {addError && (
                            <div style={{ padding: '8px 12px', borderRadius: 6, fontSize: 11, lineHeight: 1.5, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', color: '#EF4444', marginBottom: 8, wordBreak: 'break-word' }}>
                                {addError}
                            </div>
                        )}

                        <div className="modal-actions">
                            <button className="btn" onClick={() => { setShowAddModal(false); setAddError(null); }}>Cancel</button>
                            <button className="btn btn-primary" onClick={handleAdd} disabled={adding || !addForm.onvif_address.trim()}>
                                {adding ? 'Connecting...' : 'Add Camera'}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Sense webhook setup — shown once after creating a sense_pushed camera. */}
            {senseSetup && (
                <div className="modal-overlay" onClick={() => setSenseSetup(null)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: 720 }}>
                        <div className="modal-title">Sense camera webhook</div>
                        <div style={{ padding: 12, fontSize: 13, color: 'var(--text-primary)', lineHeight: 1.6 }}>
                            <p style={{ marginTop: 0 }}>
                                <strong>{senseSetup.cameraName}</strong> is set up as a push-only device.
                                In the camera UI go to <em>Event → Alarm Settings → Alarm Server → Add</em>
                                and copy each value into its matching field:
                            </p>
                            <SenseWebhookFields url={senseSetup.url} />
                            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 12, lineHeight: 1.5 }}>
                                Leave User Name / Password blank — the long path token is the auth.
                                You can review these values later from the camera's Settings → General tab.
                            </div>
                        </div>
                        <div className="modal-actions">
                            <button className="btn btn-primary" onClick={() => setSenseSetup(null)}>Done</button>
                        </div>
                    </div>
                </div>
            )}

            {/* Discovery Modal */}
            {showDiscovery && (
                <div className="modal-overlay" onClick={() => setShowDiscovery(false)}>
                    <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: '800px', width: '90vw' }}>
                        <div className="modal-title">Discovered Cameras</div>

                        {discovering ? (
                            <div className="empty-state" style={{ padding: 30 }}>
                                <div className="empty-state-icon">🔍</div>
                                <div className="empty-state-title">Scanning Network...</div>
                                <div className="empty-state-desc">Looking for ONVIF cameras on your network.</div>
                            </div>
                        ) : discoveredDevices.length === 0 ? (
                            <div className="empty-state" style={{ padding: 30 }}>
                                <div className="empty-state-icon">📡</div>
                                <div className="empty-state-title">No Cameras Found</div>
                                <div className="empty-state-desc">
                                    Make sure cameras are on the same network and ONVIF is enabled.
                                </div>
                            </div>
                        ) : (
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                                <div style={{ maxHeight: '500px', overflowY: 'auto', paddingRight: '8px' }}>
                                    <div className="flex items-center gap-2 mb-2 p-2" style={{ borderBottom: '1px solid var(--border)' }}>
                                        <input
                                            type="checkbox"
                                            checked={selectedDevices.size === discoveredDevices.length && discoveredDevices.length > 0}
                                            onChange={toggleAllDevices}
                                            style={{ width: '16px', height: '16px', cursor: 'pointer' }}
                                        />
                                        <span className="text-sm font-semibold">Select All ({discoveredDevices.length} found)</span>
                                    </div>
                                    {discoveredDevices.map((device, i) => (
                                        <div key={i} className="event-item" style={{ cursor: 'pointer', display: 'flex', alignItems: 'center' }} onClick={() => toggleDeviceSelection(device.address)}>
                                            <input
                                                type="checkbox"
                                                checked={selectedDevices.has(device.address)}
                                                onChange={() => { }} // Handled by parent div click
                                                style={{ marginRight: '12px', width: '16px', height: '16px', pointerEvents: 'none' }}
                                            />

                                            {/* Preview Image or Button */}
                                            <div style={{ width: '120px', height: '90px', borderRadius: '4px', overflow: 'hidden', background: '#000', marginRight: '16px', flexShrink: 0, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                                                {cameraPreviews[device.address] ? (
                                                    <img src={cameraPreviews[device.address]} alt="Preview" style={{ width: '100%', height: '100%', objectFit: 'cover' }} />
                                                ) : loadingPreviews.has(device.address) ? (
                                                    <span style={{ color: '#fff', fontSize: '12px' }}>Loading...</span>
                                                ) : (
                                                    <button
                                                        className="btn btn-sm"
                                                        style={{ padding: '6px 12px', fontSize: '12px' }}
                                                        onClick={(e) => {
                                                            e.stopPropagation();
                                                            handleLoadPreview(device.address);
                                                        }}
                                                    >
                                                        Preview
                                                    </button>
                                                )}
                                            </div>

                                            <div className="event-info">
                                                <div className="event-info-header">
                                                    <span className="event-info-type">{device.name || device.address}</span>
                                                </div>
                                                <div className="event-info-detail">
                                                    {device.manufacturer} {device.model} • {device.address}
                                                </div>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                                {selectedDevices.size > 0 && (
                                    <div style={{ background: 'var(--bg-secondary)', padding: '12px', borderRadius: '8px', border: '1px solid var(--border)' }}>
                                        <div style={{ marginBottom: '8px', fontSize: '0.9rem', fontWeight: 600 }}>Credentials for selected cameras</div>
                                        <div className="flex gap-2">
                                            <input
                                                className="form-input flex-1"
                                                placeholder="Username"
                                                value={discoveryAuth.username}
                                                onChange={(e) => setDiscoveryAuth({ ...discoveryAuth, username: e.target.value })}
                                            />
                                            <input
                                                className="form-input flex-1"
                                                type="password"
                                                placeholder="Password"
                                                value={discoveryAuth.password}
                                                onChange={(e) => setDiscoveryAuth({ ...discoveryAuth, password: e.target.value })}
                                            />
                                        </div>
                                    </div>
                                )}
                            </div>
                        )}

                        <div className="modal-actions" style={{ justifyContent: 'space-between' }}>
                            <div>
                                <button className="btn" onClick={handleDiscover} disabled={discovering || addingBulk}>Scan Again</button>
                            </div>
                            <div className="flex gap-2">
                                <button className="btn" onClick={() => setShowDiscovery(false)} disabled={addingBulk}>Cancel</button>
                                <button
                                    className="btn btn-primary"
                                    disabled={selectedDevices.size === 0 || addingBulk}
                                    onClick={handleBulkAdd}
                                >
                                    {addingBulk ? 'Adding...' : `Add Selected (${selectedDevices.size})`}
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            )}

            {/* Edit Camera Modal */}
            {editingCamera && (() => {
                const mfg = (editingCamera.manufacturer || '').toLowerCase();
                const mdl = (editingCamera.model || '').toLowerCase();
                const isMilesightCam = mfg.includes('milesight') || mdl.startsWith('ms-c') || mdl.startsWith('ms-n');
                const driverName = isMilesightCam ? 'Milesight' : 'ONVIF';
                const driverColor = isMilesightCam ? '#3B82F6' : '#8891A5';
                const driverBg = isMilesightCam ? 'rgba(59,130,246,0.12)' : 'rgba(255,255,255,0.04)';
                const driverBorder = isMilesightCam ? 'rgba(59,130,246,0.3)' : 'rgba(255,255,255,0.08)';
                return (
                <div className="modal-overlay">
                    <div className="modal" onClick={(e) => e.stopPropagation()} style={{ maxWidth: settingsTab === 'vca' ? 720 : settingsTab === 'milesight' ? 640 : 520, maxHeight: '85vh', display: 'flex', flexDirection: 'column', transition: 'max-width 0.2s' }}>
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
                                    onClick={() => setSettingsTab('vca')}
                                    style={{
                                        flex: 1, padding: '10px 16px', fontSize: 12, fontWeight: 600,
                                        textTransform: 'uppercase', letterSpacing: 1.2,
                                        background: 'none', border: 'none', cursor: 'pointer',
                                        color: settingsTab === 'vca' ? '#3B82F6' : 'rgba(255,255,255,0.4)',
                                        borderBottom: settingsTab === 'vca' ? '2px solid #3B82F6' : '2px solid transparent',
                                    }}
                                >
                                    VCA Zones
                                </button>
                            )}
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

                            {/* ════════════ VCA TAB ════════════ */}
                            {settingsTab === 'vca' && isMilesightCam && (
                                <div style={{ padding: '4px 0' }}>
                                    <VCAZoneEditor cameraId={editingCamera.id} cameraIp={editingCamera.onvif_address?.replace(/^https?:\/\//, '').replace(/\/.*/, '') || undefined} />
                                </div>
                            )}

                            {/* ════════════ MILESIGHT TAB ════════════ */}
                            {settingsTab === 'milesight' && isMilesightCam && (
                                <MilesightAdvanced cameraId={editingCamera.id} />
                            )}

                            {/* ════════════ GENERAL TAB ════════════ */}
                            {settingsTab === 'general' && (
                                <>
                                    {/* Sense / push-only camera webhook info — shown for the
                                        lifetime of the camera so the operator can re-paste the
                                        URL fields if the camera firmware loses them. The token
                                        is a credential; it's only rendered inside this admin
                                        modal. */}
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
                                                {[
                                                    { icon: '🚶', label: 'Human Detection', desc: 'On-camera AI person detection', active: true },
                                                    { icon: '🚙', label: 'Vehicle Detection', desc: 'Car/truck/bike classification', active: true },
                                                    { icon: '🪪', label: 'Face Detection', desc: 'Face capture with metadata', active: true },
                                                    { icon: '🔢', label: 'People Counting', desc: 'In/out counting with zones', active: true },
                                                    { icon: '🚗', label: 'License Plate (LPR)', desc: 'Plate number + color extraction', active: true },
                                                    { icon: '🚧', label: 'Intrusion Detection', desc: 'Region-based VCA alerts', active: true },
                                                    { icon: '➡️', label: 'Line Crossing', desc: 'Directional tripwire with tracking', active: true },
                                                    { icon: '⏱️', label: 'Loitering Detection', desc: 'Dwell time threshold alerts', active: true },
                                                ].map(cap => (
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
            })()}
        </div>
    );
}

// SenseWebhookFields decomposes a webhook URL into the four fields the
// camera's Alarm Server dialog expects (Protocol / Host / Port / Path)
// and renders each with its own copy button. The camera's UI splits the
// URL across separate inputs, so showing the full URL alone forces the
// operator to parse it by hand. Reused by the post-create overlay and
// the camera-settings General tab.
function SenseWebhookFields({ url }: { url: string }) {
    const parts = (() => {
        try {
            const u = new URL(url);
            return {
                protocol: u.protocol === 'https:' ? 'HTTPS' : 'HTTP',
                host: u.hostname,
                port: u.port || (u.protocol === 'https:' ? '443' : '80'),
                path: u.pathname + (u.search || ''),
            };
        } catch {
            return { protocol: '', host: '', port: '', path: url };
        }
    })();
    const rows: { label: string; value: string }[] = [
        { label: 'Protocol Type', value: parts.protocol },
        { label: 'Destination IP/Host Name', value: parts.host },
        { label: 'Port', value: parts.port },
        { label: 'Path', value: parts.path },
    ];
    return (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12, marginBottom: 8 }}>
            <tbody>
                {rows.map(row => (
                    <tr key={row.label} style={{ borderTop: '1px solid rgba(255,255,255,0.06)' }}>
                        <td style={{ padding: '8px 4px', color: 'var(--text-muted)', width: '38%' }}>
                            {row.label}
                        </td>
                        <td style={{
                            padding: '8px 4px',
                            fontFamily: "'JetBrains Mono', monospace",
                            color: '#22c55e',
                            wordBreak: 'break-all',
                        }}>
                            {row.value || <span style={{ color: 'var(--text-muted)' }}>—</span>}
                        </td>
                        <td style={{ padding: '8px 4px', width: 70, textAlign: 'right' }}>
                            <button
                                className="btn"
                                style={{ padding: '3px 10px', fontSize: 10 }}
                                onClick={() => navigator.clipboard?.writeText(row.value).catch(() => {})}
                                disabled={!row.value}
                            >
                                Copy
                            </button>
                        </td>
                    </tr>
                ))}
            </tbody>
        </table>
    );
}
