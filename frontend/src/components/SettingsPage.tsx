'use client';

import { useState, useEffect, useCallback } from 'react';
import {
    SystemSettings, SystemHealth, Camera,
    DriveInfo, FolderEntry, StorageLocation, StorageLocationCreate, DiskUsage,
    Speaker, AudioMessage, AuditEntry, AuditLogResponse,
    getSettings, updateSettings, getSystemHealth,
    listDrives, browsePath, getDiskUsage, listStorageLocations,
    createStorageLocation, updateStorageLocation, deleteStorageLocation,
    listSpeakers, createSpeaker, deleteSpeaker,
    listAudioMessages, uploadAudioMessage, deleteAudioMessage,
    queryAuditLog, queryEvents,
} from '@/lib/api';
import type { Event as CameraEvent } from '@/lib/api';
import { BRAND } from '@/lib/branding';
import CameraManager from '@/components/CameraManager';
import { useToast } from '@/components/ToastProvider';
import { useSites } from '@/hooks/useSites';
import { useMasterCameras } from '@/hooks/useCameraAssignment';

type SettingsTab = 'cameras' | 'storage' | 'defaults' | 'speakers' | 'system' | 'events' | 'audit';

interface Props {
    currentUserId: string;
    currentUserRole: string;
    cameras: Camera[];
    onRefresh: () => void;
}


export default function SettingsPage({ currentUserId, currentUserRole, cameras, onRefresh }: Props) {
    const isAdmin = currentUserRole === 'admin';
    const [tab, setTab] = useState<SettingsTab>('cameras');
    const { enabled: toastsEnabled, setEnabled: setToastsEnabled } = useToast();

    // ── Audit Log state ─────────────────────────────────────────────────────────────
    const [auditEntries, setAuditEntries] = useState<AuditEntry[]>([]);
    const [auditTotal, setAuditTotal] = useState(0);
    const [auditPage, setAuditPage] = useState(0);
    const [auditFilter, setAuditFilter] = useState('');
    const AUDIT_PAGE_SIZE = 25;

    // ── Settings state ─────────────────────────────────────────────────────────
    const [settings, setSettings] = useState<SystemSettings | null>(null);
    const [settingsDraft, setSettingsDraft] = useState<SystemSettings | null>(null);
    const [settingsSaving, setSettingsSaving] = useState(false);
    const [settingsMsg, setSettingsMsg] = useState<{ ok: boolean; text: string } | null>(null);
    const [systemHealth, setSystemHealth] = useState<SystemHealth | null>(null);

    // ── Speakers state ───────────────────────────────────────────────────────
    const [speakers, setSpeakers] = useState<Speaker[]>([]);
    const [audioMessages, setAudioMessages] = useState<AudioMessage[]>([]);
    const [newSpeaker, setNewSpeaker] = useState({ name: '', onvif_address: '', username: '', password: '', zone: '' });
    const [addingSpeaker, setAddingSpeaker] = useState(false);
    const [newMsgName, setNewMsgName] = useState('');
    const [newMsgCategory, setNewMsgCategory] = useState('warning');
    const [newMsgFile, setNewMsgFile] = useState<File | null>(null);
    const [uploadingMsg, setUploadingMsg] = useState(false);

    // ── Storage Locations state ─────────────────────────────────────────────
    const [storageLocations, setStorageLocations] = useState<StorageLocation[]>([]);
    const [storageLoading, setStorageLoading] = useState(false);
    const [diskUsages, setDiskUsages] = useState<Record<string, DiskUsage>>({});
    const [showFolderPicker, setShowFolderPicker] = useState(false);
    const [editingLocation, setEditingLocation] = useState<StorageLocation | null>(null);
    const [newLocation, setNewLocation] = useState<StorageLocationCreate>({
        label: '', path: '', purpose: 'recordings', retention_days: 30, max_gb: 0, priority: 0,
    });
    const [pickerDrives, setPickerDrives] = useState<DriveInfo[]>([]);
    const [pickerFolders, setPickerFolders] = useState<FolderEntry[]>([]);
    const [pickerPath, setPickerPath] = useState('');
    const [pickerBreadcrumb, setPickerBreadcrumb] = useState<string[]>([]);
    const [addingNew, setAddingNew] = useState(false);

    // ── Load settings ──────────────────────────────────────────────────────────
    useEffect(() => {
        getSettings().then(s => {
            setSettings(s);
            setSettingsDraft(s);
        }).catch(() => { });
    }, []);

    // ── Storage Location callbacks ──────────────────────────────────────────────

    const loadStorageLocations = useCallback(async () => {
        setStorageLoading(true);
        try {
            const locs = await listStorageLocations();
            setStorageLocations(locs);
            const usages: Record<string, DiskUsage> = {};
            await Promise.all(locs.map(async (loc) => {
                const usage = await getDiskUsage(loc.path);
                if (usage) usages[loc.id] = usage;
            }));
            setDiskUsages(usages);
        } catch { /* ignore */ }
        setStorageLoading(false);
    }, []);

    useEffect(() => {
        if (tab === 'storage') loadStorageLocations();
    }, [tab, loadStorageLocations]);

    const openFolderPicker = useCallback(async () => {
        setShowFolderPicker(true);
        const drives = await listDrives();
        setPickerDrives(drives);
        setPickerFolders([]);
        setPickerPath('');
        setPickerBreadcrumb([]);
    }, []);

    const navigatePicker = useCallback(async (path: string) => {
        setPickerPath(path);
        const folders = await browsePath(path);
        setPickerFolders(folders);
        const parts = path.replace(/\\/g, '/').split('/').filter(Boolean);
        setPickerBreadcrumb(parts);
    }, []);

    const selectPickerPath = useCallback(() => {
        if (editingLocation) {
            setEditingLocation({ ...editingLocation, path: pickerPath });
        } else {
            setNewLocation(prev => ({ ...prev, path: pickerPath }));
        }
        setShowFolderPicker(false);
    }, [pickerPath, editingLocation]);

    const handleAddLocation = useCallback(async () => {
        if (!newLocation.label || !newLocation.path) return;
        try {
            await createStorageLocation(newLocation);
            setNewLocation({ label: '', path: '', purpose: 'recordings', retention_days: 30, max_gb: 0, priority: 0 });
            setAddingNew(false);
            loadStorageLocations();
        } catch { /* ignore */ }
    }, [newLocation, loadStorageLocations]);

    const handleUpdateLocation = useCallback(async () => {
        if (!editingLocation) return;
        try {
            await updateStorageLocation(editingLocation.id, {
                label: editingLocation.label,
                path: editingLocation.path,
                purpose: editingLocation.purpose,
                retention_days: editingLocation.retention_days,
                max_gb: editingLocation.max_gb,
                priority: editingLocation.priority,
            });
            setEditingLocation(null);
            loadStorageLocations();
        } catch { /* ignore */ }
    }, [editingLocation, loadStorageLocations]);

    const handleDeleteLocation = useCallback(async (id: string) => {
        if (!confirm('Delete this storage location?')) return;
        try {
            await deleteStorageLocation(id);
            loadStorageLocations();
        } catch { /* ignore */ }
    }, [loadStorageLocations]);

    // ── Settings save ──────────────────────────────────────────────────────────
    const handleSaveSettings = async () => {
        if (!settingsDraft) return;
        setSettingsSaving(true);
        setSettingsMsg(null);
        try {
            const updated = await updateSettings(settingsDraft);
            setSettings(updated);
            setSettingsDraft(updated);
            setSettingsMsg({ ok: true, text: '✓ Settings saved. Some changes require a server restart to take effect.' });
        } catch (e: any) {
            setSettingsMsg({ ok: false, text: '✗ ' + (e.message ?? 'Save failed') });
        } finally {
            setSettingsSaving(false);
        }
    };

    const patchDraft = (key: keyof SystemSettings, val: string | number) =>
        setSettingsDraft(d => d ? { ...d, [key]: val } : d);

    // ── Render helpers ─────────────────────────────────────────────────────────

    const PathField = ({
        label, hint, field,
    }: { label: string; hint: string; field: keyof SystemSettings }) => (
        <div className="settings-form-row">
            <label className="settings-label">
                <span className="settings-label-text">{label}</span>
                <span className="settings-label-hint">{hint}</span>
            </label>
            <input
                className="settings-input"
                type="text"
                value={settingsDraft ? String(settingsDraft[field]) : ''}
                onChange={e => patchDraft(field, e.target.value)}
                disabled={!isAdmin}
                spellCheck={false}
            />
        </div>
    );

    const tabs: { id: SettingsTab; icon: string; label: string }[] = [
        { id: 'cameras', icon: '📷', label: 'Cameras' },
        { id: 'storage', icon: '📁', label: 'Storage' },
        { id: 'defaults', icon: '🎥', label: 'Camera Defaults' },
        { id: 'speakers', icon: '🔊', label: 'Speakers' },
        { id: 'system', icon: '🔧', label: 'System' },
        { id: 'events', icon: '📡', label: 'Event Log' },
        { id: 'audit', icon: '📝', label: 'Audit Log' },
    ];

    return (
        <div className="settings-page">
            {/* Header */}
            <div className="settings-header">
                <div className="settings-header-icon">⚙</div>
                <div>
                    <h2 className="settings-title">Settings</h2>
                    <p className="settings-subtitle">
                        Configure storage, cameras, and system behaviour
                        {isAdmin ? '' : ' — read-only (operator view)'}
                    </p>
                </div>
            </div>

            {/* Tab bar */}
            <div className="settings-tabs" role="tablist">
                {tabs.map(t => (
                    <button
                        key={t.id}
                        role="tab"
                        className={`settings-tab ${tab === t.id ? 'active' : ''}`}
                        onClick={() => setTab(t.id)}
                        aria-selected={tab === t.id}
                    >
                        <span className="settings-tab-icon">{t.icon}</span>
                        {t.label}
                    </button>
                ))}
            </div>

            {/* ── Tab: Cameras ────────────────────────────────────────────────────── */}
            {tab === 'cameras' && (
                <CameraManager cameras={cameras} onRefresh={onRefresh} />
            )}

            {/* ── Tab: Storage ───────────────────────────────────────────────── */}
            {tab === 'storage' && (
                <div className="settings-section" role="tabpanel">
                    <div className="settings-section-title">
                        Storage Locations
                        {isAdmin && !addingNew && (
                            <button className="settings-add-location-btn" onClick={() => setAddingNew(true)}>
                                ＋ Add Location
                            </button>
                        )}
                    </div>
                    <p className="settings-section-desc">
                        Configure storage paths for recordings, snapshots, and exports.
                        Supports local drives (DAS), network shares (NAS/SMB), and iSCSI volumes.
                    </p>

                    {/* Add New Location Form */}
                    {addingNew && (
                        <div className="storage-location-form">
                            <div className="storage-form-row">
                                <label>Label</label>
                                <input
                                    type="text" placeholder="e.g. Primary NAS"
                                    value={newLocation.label}
                                    onChange={e => setNewLocation(prev => ({ ...prev, label: e.target.value }))}
                                />
                            </div>
                            <div className="storage-form-row">
                                <label>Path</label>
                                <div className="storage-path-input">
                                    <input
                                        type="text" placeholder="D:\\NVR\\recordings or \\\\nas01\\video"
                                        value={newLocation.path}
                                        onChange={e => setNewLocation(prev => ({ ...prev, path: e.target.value }))}
                                    />
                                    <button className="storage-browse-btn" onClick={openFolderPicker} type="button">
                                        📁 Browse
                                    </button>
                                </div>
                            </div>
                            <div className="storage-form-row-group">
                                <div className="storage-form-row">
                                    <label>Purpose</label>
                                    <select
                                        value={newLocation.purpose}
                                        onChange={e => setNewLocation(prev => ({ ...prev, purpose: e.target.value }))}
                                    >
                                        <option value="recordings">Recordings</option>
                                        <option value="snapshots">Snapshots</option>
                                        <option value="exports">Exports</option>
                                        <option value="all">All</option>
                                    </select>
                                </div>
                                <div className="storage-form-row">
                                    <label>Retention (days)</label>
                                    <input
                                        type="number" min={1}
                                        value={newLocation.retention_days}
                                        onChange={e => setNewLocation(prev => ({ ...prev, retention_days: parseInt(e.target.value) || 30 }))}
                                    />
                                </div>
                                <div className="storage-form-row">
                                    <label>Max GB (0 = unlimited)</label>
                                    <input
                                        type="number" min={0}
                                        value={newLocation.max_gb}
                                        onChange={e => setNewLocation(prev => ({ ...prev, max_gb: parseInt(e.target.value) || 0 }))}
                                    />
                                </div>
                            </div>
                            <div className="storage-form-actions">
                                <button className="settings-save-btn" onClick={handleAddLocation}>💾 Add Location</button>
                                <button className="save-cancel-btn" onClick={() => setAddingNew(false)}>Cancel</button>
                            </div>
                        </div>
                    )}

                    {/* Edit Location Form */}
                    {editingLocation && (
                        <div className="storage-location-form">
                            <div className="storage-form-row">
                                <label>Label</label>
                                <input
                                    type="text"
                                    value={editingLocation.label}
                                    onChange={e => setEditingLocation({ ...editingLocation, label: e.target.value })}
                                />
                            </div>
                            <div className="storage-form-row">
                                <label>Path</label>
                                <div className="storage-path-input">
                                    <input
                                        type="text"
                                        value={editingLocation.path}
                                        onChange={e => setEditingLocation({ ...editingLocation, path: e.target.value })}
                                    />
                                    <button className="storage-browse-btn" onClick={openFolderPicker} type="button">
                                        📁 Browse
                                    </button>
                                </div>
                            </div>
                            <div className="storage-form-row-group">
                                <div className="storage-form-row">
                                    <label>Purpose</label>
                                    <select
                                        value={editingLocation.purpose}
                                        onChange={e => setEditingLocation({ ...editingLocation, purpose: e.target.value })}
                                    >
                                        <option value="recordings">Recordings</option>
                                        <option value="snapshots">Snapshots</option>
                                        <option value="exports">Exports</option>
                                        <option value="all">All</option>
                                    </select>
                                </div>
                                <div className="storage-form-row">
                                    <label>Retention (days)</label>
                                    <input
                                        type="number" min={1}
                                        value={editingLocation.retention_days}
                                        onChange={e => setEditingLocation({ ...editingLocation, retention_days: parseInt(e.target.value) || 30 })}
                                    />
                                </div>
                                <div className="storage-form-row">
                                    <label>Max GB (0 = unlimited)</label>
                                    <input
                                        type="number" min={0}
                                        value={editingLocation.max_gb}
                                        onChange={e => setEditingLocation({ ...editingLocation, max_gb: parseInt(e.target.value) || 0 })}
                                    />
                                </div>
                            </div>
                            <div className="storage-form-actions">
                                <button className="settings-save-btn" onClick={handleUpdateLocation}>💾 Save Changes</button>
                                <button className="save-cancel-btn" onClick={() => setEditingLocation(null)}>Cancel</button>
                            </div>
                        </div>
                    )}

                    {/* Location Cards */}
                    {storageLoading ? (
                        <div className="settings-loading">Loading storage locations…</div>
                    ) : storageLocations.length === 0 && !addingNew ? (
                        <div className="storage-empty">
                            <p>No storage locations configured.</p>
                            <p>Click <strong>＋ Add Location</strong> to add a recording drive, NAS share, or iSCSI volume.</p>
                        </div>
                    ) : (
                        <div className="storage-cards">
                            {storageLocations.map(loc => {
                                const usage = diskUsages[loc.id];
                                const pct = usage ? Math.round((usage.used_bytes / usage.total_bytes) * 100) : 0;
                                const statusColor = pct > 95 ? 'critical' : pct > 80 ? 'warn' : 'ok';
                                const formatBytes = (b: number) => {
                                    if (b >= 1e12) return (b / 1e12).toFixed(1) + ' TB';
                                    if (b >= 1e9) return (b / 1e9).toFixed(1) + ' GB';
                                    if (b >= 1e6) return (b / 1e6).toFixed(0) + ' MB';
                                    return b + ' B';
                                };
                                const purposeLabels: Record<string, string> = {
                                    recordings: '🎥 Recordings', snapshots: '📷 Snapshots',
                                    exports: '📦 Exports', all: '📂 All',
                                };

                                return (
                                    <div key={loc.id} className={`storage-card ${editingLocation?.id === loc.id ? 'editing' : ''}`}>
                                        <div className="storage-card-header">
                                            <div className="storage-card-title">
                                                <span className={`storage-health-pill ${statusColor}`} />
                                                <strong>{loc.label}</strong>
                                                <span className="storage-purpose-badge">{purposeLabels[loc.purpose] || loc.purpose}</span>
                                            </div>
                                            {isAdmin && !editingLocation && (
                                                <div className="storage-card-actions">
                                                    <button onClick={() => setEditingLocation(loc)} title="Edit">⚙️</button>
                                                    <button onClick={() => handleDeleteLocation(loc.id)} title="Delete">🗑️</button>
                                                </div>
                                            )}
                                        </div>
                                        <div className="storage-card-path">{loc.path}</div>

                                        {/* Disk Usage Bar */}
                                        {usage && (
                                            <div className="storage-usage">
                                                <div className="storage-usage-bar">
                                                    <div
                                                        className={`storage-usage-fill ${statusColor}`}
                                                        style={{ width: `${pct}%` }}
                                                    />
                                                </div>
                                                <div className="storage-usage-stats">
                                                    <span>{formatBytes(usage.used_bytes)} / {formatBytes(usage.total_bytes)}</span>
                                                    <span>{formatBytes(usage.free_bytes)} free ({100 - pct}%)</span>
                                                </div>
                                            </div>
                                        )}

                                        <div className="storage-card-meta">
                                            <span>🕐 {loc.retention_days}d retention</span>
                                            <span>{loc.max_gb > 0 ? `📊 ${loc.max_gb} GB cap` : '📊 Unlimited'}</span>
                                            <span>⚡ Priority {loc.priority}</span>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    )}

                    {/* Legacy paths section - kept for backward compatibility */}
                    {settingsDraft && (
                        <details className="storage-legacy-section">
                            <summary>Advanced: Legacy Path Configuration</summary>
                            <div className="settings-form" style={{ marginTop: 12 }}>
                                <PathField label="Recordings" hint="Video segments from cameras" field="recordings_path" />
                                <PathField label="Snapshots" hint="Thumbnails and event stills" field="snapshots_path" />
                                <PathField label="Exports" hint="Exported video clips" field="exports_path" />
                                <PathField label="HLS Segments" hint="Live stream buffer (temp)" field="hls_path" />
                            </div>
                            {isAdmin && (
                                <div className="settings-actions">
                                    <button className="settings-save-btn" onClick={handleSaveSettings} disabled={settingsSaving}>
                                        {settingsSaving ? '⏳ Saving…' : '💾 Save Paths'}
                                    </button>
                                    {settingsMsg && (
                                        <span className={`settings-msg ${settingsMsg.ok ? 'ok' : 'err'}`}>{settingsMsg.text}</span>
                                    )}
                                </div>
                            )}
                        </details>
                    )}
                </div>
            )}

            {/* ── Folder Picker Modal ────────────────────────────────────────── */}
            {showFolderPicker && (
                <div className="folder-picker-overlay" onClick={() => setShowFolderPicker(false)}>
                    <div className="folder-picker-modal" onClick={e => e.stopPropagation()}>
                        <div className="folder-picker-header">
                            <h3>Select Folder</h3>
                            <button className="folder-picker-close" onClick={() => setShowFolderPicker(false)}>✕</button>
                        </div>

                        <div className="folder-picker-body">
                            {/* Drive List */}
                            <div className="folder-picker-drives">
                                <div className="folder-picker-section-title">Drives</div>
                                {pickerDrives.map(drive => {
                                    const pct = Math.round((drive.used_bytes / drive.total_bytes) * 100);
                                    const fmt = (b: number) => b >= 1e12 ? (b / 1e12).toFixed(1) + ' TB' : (b / 1e9).toFixed(0) + ' GB';
                                    const typeIcon = drive.drive_type === 'network' ? '🌐' : drive.drive_type === 'removable' ? '💾' : '💿';
                                    return (
                                        <button
                                            key={drive.letter}
                                            className={`folder-picker-drive ${pickerPath.startsWith(drive.letter) ? 'active' : ''}`}
                                            onClick={() => navigatePicker(drive.letter)}
                                        >
                                            <span className="drive-icon">{typeIcon}</span>
                                            <div className="drive-info">
                                                <span className="drive-name">{drive.letter} {drive.label && `(${drive.label})`}</span>
                                                <span className="drive-space">{fmt(drive.free_bytes)} free / {fmt(drive.total_bytes)}</span>
                                                <div className="drive-bar">
                                                    <div className="drive-bar-fill" style={{ width: `${pct}%` }} />
                                                </div>
                                            </div>
                                        </button>
                                    );
                                })}
                            </div>

                            {/* Folder Tree */}
                            <div className="folder-picker-tree">
                                {/* Breadcrumb */}
                                {pickerPath && (
                                    <div className="folder-picker-breadcrumb">
                                        {pickerBreadcrumb.map((part, i) => {
                                            const pathTo = pickerBreadcrumb.slice(0, i + 1).join('\\') + '\\';
                                            return (
                                                <span key={i}>
                                                    <button onClick={() => navigatePicker(pathTo)}>{part}</button>
                                                    {i < pickerBreadcrumb.length - 1 && <span className="sep">\</span>}
                                                </span>
                                            );
                                        })}
                                    </div>
                                )}

                                {/* Folder list */}
                                <div className="folder-picker-list">
                                    {pickerPath && (
                                        <button
                                            className="folder-picker-item parent"
                                            onClick={() => {
                                                const parent = pickerPath.replace(/\\[^\\]+\\?$/, '');
                                                if (parent && parent !== pickerPath) navigatePicker(parent + '\\');
                                            }}
                                        >
                                            📁 ..
                                        </button>
                                    )}
                                    {pickerFolders.map(f => (
                                        <button
                                            key={f.path}
                                            className="folder-picker-item"
                                            onClick={() => navigatePicker(f.path)}
                                        >
                                            📁 {f.name}
                                        </button>
                                    ))}
                                    {pickerPath && pickerFolders.length === 0 && (
                                        <div className="folder-picker-empty">No subdirectories</div>
                                    )}
                                </div>
                            </div>
                        </div>

                        {/* Manual path + select */}
                        <div className="folder-picker-footer">
                            <input
                                type="text"
                                className="folder-picker-path-input"
                                placeholder="Type a path or UNC (\\\\server\\share)"
                                value={pickerPath}
                                onChange={e => setPickerPath(e.target.value)}
                                onKeyDown={e => { if (e.key === 'Enter') navigatePicker(pickerPath); }}
                            />
                            <button className="folder-picker-go-btn" onClick={() => navigatePicker(pickerPath)}>Go</button>
                            <button className="folder-picker-select-btn" onClick={selectPickerPath} disabled={!pickerPath}>
                                ✓ Select This Folder
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* ── Tab: Camera Defaults ────────────────────────────────────────── */}
            {tab === 'defaults' && (
                <div className="settings-section" role="tabpanel">
                    <div className="settings-section-title">Camera Defaults</div>
                    <p className="settings-section-desc">
                        These values are applied automatically when a new camera is added.
                        They do not retroactively change existing cameras.
                    </p>

                    {settingsDraft && (
                        <div className="settings-form">
                            <div className="settings-form-row">
                                <label className="settings-label">
                                    <span className="settings-label-text">Default Retention</span>
                                    <span className="settings-label-hint">Days before recordings are deleted</span>
                                </label>
                                <div className="settings-input-with-unit">
                                    <input
                                        className="settings-input"
                                        type="number"
                                        min={1}
                                        max={365}
                                        value={settingsDraft.default_retention_days}
                                        onChange={e => patchDraft('default_retention_days', parseInt(e.target.value) || 30)}
                                        disabled={!isAdmin}
                                    />
                                    <span className="settings-input-unit">days</span>
                                </div>
                            </div>

                            <div className="settings-form-row">
                                <label className="settings-label">
                                    <span className="settings-label-text">Default Recording Mode</span>
                                    <span className="settings-label-hint">How new cameras record by default</span>
                                </label>
                                <select
                                    className="settings-input settings-select"
                                    value={settingsDraft.default_recording_mode}
                                    onChange={e => patchDraft('default_recording_mode', e.target.value)}
                                    disabled={!isAdmin}
                                >
                                    <option value="continuous">Continuous</option>
                                    <option value="event">Event-triggered</option>
                                </select>
                            </div>

                            <div className="settings-form-row">
                                <label className="settings-label">
                                    <span className="settings-label-text">Segment Duration</span>
                                    <span className="settings-label-hint">Length of each recording file</span>
                                </label>
                                <div className="settings-input-with-unit">
                                    <input
                                        className="settings-input"
                                        type="number"
                                        min={10}
                                        max={3600}
                                        value={settingsDraft.default_segment_duration}
                                        onChange={e => patchDraft('default_segment_duration', parseInt(e.target.value) || 60)}
                                        disabled={!isAdmin}
                                    />
                                    <span className="settings-input-unit">seconds</span>
                                </div>
                            </div>
                        </div>
                    )}

                    {isAdmin && (
                        <div className="settings-actions">
                            <button className="settings-save-btn" onClick={handleSaveSettings} disabled={settingsSaving}>
                                {settingsSaving ? '⏳ Saving…' : '💾 Save Defaults'}
                            </button>
                            {settingsMsg && (
                                <span className={`settings-msg ${settingsMsg.ok ? 'ok' : 'err'}`}>{settingsMsg.text}</span>
                            )}
                        </div>
                    )}
                </div>
            )}

            {/* ── Tab: Speakers ────────────────────────────────────────────── */}
            {tab === 'speakers' && (() => {
                // Load speakers and messages on tab open
                if (speakers.length === 0 && audioMessages.length === 0 && !addingSpeaker) {
                    listSpeakers().then(setSpeakers).catch(() => { });
                    listAudioMessages().then(setAudioMessages).catch(() => { });
                }

                const refreshSpeakers = () => { listSpeakers().then(setSpeakers).catch(() => { }); };
                const refreshMessages = () => { listAudioMessages().then(setAudioMessages).catch(() => { }); };

                const handleAddSpeaker = async () => {
                    if (!newSpeaker.name || !newSpeaker.onvif_address) return;
                    setAddingSpeaker(true);
                    try {
                        await createSpeaker(newSpeaker);
                        setNewSpeaker({ name: '', onvif_address: '', username: '', password: '', zone: '' });
                        refreshSpeakers();
                    } catch { /* ignore */ }
                    setAddingSpeaker(false);
                };

                const handleUploadMsg = async () => {
                    if (!newMsgName || !newMsgFile) return;
                    setUploadingMsg(true);
                    try {
                        await uploadAudioMessage(newMsgName, newMsgCategory, newMsgFile);
                        setNewMsgName('');
                        setNewMsgFile(null);
                        refreshMessages();
                    } catch { /* ignore */ }
                    setUploadingMsg(false);
                };

                const cats = ['warning', 'info', 'emergency', 'custom'] as const;
                const catColors: Record<string, string> = {
                    warning: '#f59e0b', info: '#3b82f6', emergency: '#ef4444', custom: '#8b5cf6',
                };

                return (
                    <div className="settings-section" role="tabpanel">
                        <div className="settings-section-title">Speaker Devices</div>
                        <p className="settings-section-desc">ONVIF audio speakers for talk-down. Add speakers by their ONVIF address.</p>

                        {/* Speaker list */}
                        {speakers.length > 0 && (
                            <div style={{ marginBottom: 16 }}>
                                {speakers.map(spk => (
                                    <div key={spk.id} style={{
                                        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                                        padding: '10px 14px', marginBottom: 6, borderRadius: 8,
                                        background: 'rgba(255,255,255,0.03)', border: '1px solid rgba(255,255,255,0.06)',
                                    }}>
                                        <div>
                                            <div style={{ fontWeight: 600, fontSize: 14 }}>
                                                <span style={{
                                                    display: 'inline-block', width: 8, height: 8, borderRadius: '50%',
                                                    background: spk.status === 'online' ? '#22c55e' : '#ef4444',
                                                    marginRight: 8,
                                                }} />
                                                {spk.name}
                                                {spk.zone && <span style={{ opacity: 0.5, marginLeft: 8, fontSize: 12 }}>({spk.zone})</span>}
                                            </div>
                                            <div style={{ fontSize: 11, opacity: 0.5 }}>
                                                {spk.onvif_address} · {spk.manufacturer} {spk.model}
                                            </div>
                                        </div>
                                        {isAdmin && (
                                            <button
                                                style={{ background: 'transparent', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 14 }}
                                                onClick={async () => { await deleteSpeaker(spk.id); refreshSpeakers(); }}
                                            >✕ Remove</button>
                                        )}
                                    </div>
                                ))}
                            </div>
                        )}

                        {/* Add speaker form */}
                        {isAdmin && (
                            <div style={{
                                background: 'rgba(255,255,255,0.02)', border: '1px dashed rgba(255,255,255,0.1)',
                                borderRadius: 8, padding: 14, marginBottom: 20,
                            }}>
                                <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>+ Add Speaker</div>
                                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                                    <input className="settings-input" placeholder="Speaker Name" value={newSpeaker.name}
                                        onChange={e => setNewSpeaker({ ...newSpeaker, name: e.target.value })} />
                                    <input className="settings-input" placeholder="ONVIF Address (e.g. 192.168.1.50)" value={newSpeaker.onvif_address}
                                        onChange={e => setNewSpeaker({ ...newSpeaker, onvif_address: e.target.value })} />
                                    <input className="settings-input" placeholder="Username" value={newSpeaker.username}
                                        onChange={e => setNewSpeaker({ ...newSpeaker, username: e.target.value })} />
                                    <input className="settings-input" placeholder="Password" type="password" value={newSpeaker.password}
                                        onChange={e => setNewSpeaker({ ...newSpeaker, password: e.target.value })} />
                                    <input className="settings-input" placeholder="Zone (e.g. Perimeter)" value={newSpeaker.zone}
                                        onChange={e => setNewSpeaker({ ...newSpeaker, zone: e.target.value })} />
                                    <button className="settings-save-btn" onClick={handleAddSpeaker} disabled={addingSpeaker || !newSpeaker.name || !newSpeaker.onvif_address}>
                                        {addingSpeaker ? '⏳ Probing…' : '🔊 Add Speaker'}
                                    </button>
                                </div>
                            </div>
                        )}

                        {/* Audio Messages section */}
                        <div className="settings-section-title" style={{ marginTop: 24 }}>Pre-recorded Messages</div>
                        <p className="settings-section-desc">Upload WAV or MP3 audio files for talk-down playback.</p>

                        {/* Message list */}
                        {audioMessages.length > 0 && (
                            <div style={{ marginBottom: 16 }}>
                                {cats.map(cat => {
                                    const msgs = audioMessages.filter(m => m.category === cat);
                                    if (msgs.length === 0) return null;
                                    return (
                                        <div key={cat} style={{ marginBottom: 12 }}>
                                            <div style={{
                                                fontSize: 11, fontWeight: 700, textTransform: 'uppercase',
                                                color: catColors[cat], marginBottom: 4, letterSpacing: 1,
                                            }}>{cat}</div>
                                            {msgs.map(msg => (
                                                <div key={msg.id} style={{
                                                    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
                                                    padding: '8px 12px', marginBottom: 4, borderRadius: 6,
                                                    background: 'rgba(255,255,255,0.02)', borderLeft: `3px solid ${catColors[cat]}`,
                                                }}>
                                                    <div>
                                                        <span style={{ fontWeight: 500 }}>{msg.name}</span>
                                                        <span style={{ opacity: 0.4, marginLeft: 8, fontSize: 11 }}>
                                                            {msg.duration > 0 ? `${msg.duration.toFixed(1)}s` : '—'} · {(msg.file_size / 1024).toFixed(0)} KB
                                                        </span>
                                                    </div>
                                                    {isAdmin && (
                                                        <button
                                                            style={{ background: 'transparent', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 12 }}
                                                            onClick={async () => { await deleteAudioMessage(msg.id); refreshMessages(); }}
                                                        >✕</button>
                                                    )}
                                                </div>
                                            ))}
                                        </div>
                                    );
                                })}
                            </div>
                        )}
                        {audioMessages.length === 0 && (
                            <p style={{ opacity: 0.4, fontSize: 13, marginBottom: 16 }}>No messages uploaded yet.</p>
                        )}

                        {/* Upload form */}
                        {isAdmin && (
                            <div style={{
                                background: 'rgba(255,255,255,0.02)', border: '1px dashed rgba(255,255,255,0.1)',
                                borderRadius: 8, padding: 14,
                            }}>
                                <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>+ Upload Message</div>
                                <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 8 }}>
                                    <input className="settings-input" placeholder="Message Name" value={newMsgName}
                                        onChange={e => setNewMsgName(e.target.value)} />
                                    <select className="settings-input" value={newMsgCategory}
                                        onChange={e => setNewMsgCategory(e.target.value)}
                                        style={{ width: 'auto' }}>
                                        <option value="warning">⚠️ Warning</option>
                                        <option value="info">ℹ️ Info</option>
                                        <option value="emergency">🚨 Emergency</option>
                                        <option value="custom">🎵 Custom</option>
                                    </select>
                                </div>
                                <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
                                    <input type="file" accept=".wav,.mp3,.ogg,.m4a"
                                        onChange={e => setNewMsgFile(e.target.files?.[0] || null)}
                                        style={{ flex: 1 }} />
                                    <button className="settings-save-btn" onClick={handleUploadMsg}
                                        disabled={uploadingMsg || !newMsgName || !newMsgFile}>
                                        {uploadingMsg ? '⏳ Uploading…' : '⬆ Upload'}
                                    </button>
                                </div>
                            </div>
                        )}
                    </div>
                );
            })()}

            {/* ── Tab: System ────────────────────────────────────────────────── */}
            {tab === 'system' && <SystemTab
                isAdmin={isAdmin}
                settings={settings}
                settingsDraft={settingsDraft}
                settingsSaving={settingsSaving}
                settingsMsg={settingsMsg}
                systemHealth={systemHealth}
                cameras={cameras}
                toastsEnabled={toastsEnabled}
                setToastsEnabled={setToastsEnabled}
                patchDraft={patchDraft}
                handleSaveSettings={handleSaveSettings}
                onRefreshHealth={() => getSystemHealth().then(setSystemHealth).catch(() => {})}
            />}
            {/* ═════ AUDIT LOG TAB ═════ */}
            {tab === 'events' && <EventLogTab cameras={cameras} />}

            {tab === 'audit' && (
                <div className="settings-section">
                    <div className="settings-section-header">
                        <h3>Audit Log</h3>
                        <p style={{ fontSize: 13, opacity: 0.6, margin: '4px 0 0' }}>All user actions are recorded here</p>
                    </div>

                    <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
                        <input
                            type="text"
                            className="settings-input"
                            placeholder="Filter by username..."
                            value={auditFilter}
                            onChange={e => setAuditFilter(e.target.value)}
                            style={{ flex: 1 }}
                        />
                        <button
                            className="settings-btn settings-btn-primary"
                            onClick={async () => {
                                try {
                                    const data = await queryAuditLog({
                                        username: auditFilter || undefined,
                                        limit: AUDIT_PAGE_SIZE,
                                        offset: auditPage * AUDIT_PAGE_SIZE,
                                    });
                                    setAuditEntries(data.entries);
                                    setAuditTotal(data.total);
                                } catch { /* ignore */ }
                            }}
                        >
                            Search
                        </button>
                    </div>

                    <div style={{ overflowX: 'auto' }}>
                        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                            <thead>
                                <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
                                    <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>Time</th>
                                    <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>User</th>
                                    <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>Action</th>
                                    <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>Target</th>
                                    <th style={{ textAlign: 'left', padding: '8px 6px', fontWeight: 600 }}>IP</th>
                                </tr>
                            </thead>
                            <tbody>
                                {auditEntries.length === 0 ? (
                                    <tr><td colSpan={5} style={{ padding: 20, textAlign: 'center', opacity: 0.5 }}>No entries — click Search to load</td></tr>
                                ) : auditEntries.map(e => (
                                    <tr key={e.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.04)' }}>
                                        <td style={{ padding: '6px', whiteSpace: 'nowrap', opacity: 0.7 }}>{new Date(e.created_at).toLocaleString()}</td>
                                        <td style={{ padding: '6px', fontWeight: 600 }}>{e.username}</td>
                                        <td style={{ padding: '6px' }}>
                                            <span style={{
                                                background: e.action.includes('delete') ? 'rgba(239,68,68,0.15)' :
                                                    e.action.includes('create') ? 'rgba(34,197,94,0.15)' :
                                                        'rgba(255,255,255,0.06)',
                                                padding: '2px 8px', borderRadius: 4, fontSize: 12,
                                                color: e.action.includes('delete') ? '#ef4444' :
                                                    e.action.includes('create') ? '#22c55e' : 'inherit'
                                            }}>
                                                {e.action}
                                            </span>
                                        </td>
                                        <td style={{ padding: '6px', opacity: 0.7 }}>{e.target_type}{e.target_id ? ` / ${e.target_id.substring(0, 8)}...` : ''}</td>
                                        <td style={{ padding: '6px', fontSize: 11, opacity: 0.5, fontFamily: 'monospace' }}>{e.ip_address}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>

                    {auditTotal > AUDIT_PAGE_SIZE && (
                        <div style={{ display: 'flex', justifyContent: 'center', gap: 12, marginTop: 16 }}>
                            <button className="settings-btn" disabled={auditPage === 0} onClick={() => setAuditPage(p => p - 1)}>◀ Prev</button>
                            <span style={{ fontSize: 13, alignSelf: 'center', opacity: 0.6 }}>
                                Page {auditPage + 1} of {Math.ceil(auditTotal / AUDIT_PAGE_SIZE)}
                            </span>
                            <button className="settings-btn" disabled={(auditPage + 1) * AUDIT_PAGE_SIZE >= auditTotal} onClick={() => setAuditPage(p => p + 1)}>Next ▶</button>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

// ═══════════════════════════════════════════════════════════════
// System Tab — site-aware health, server config, driver status
// ═══════════════════════════════════════════════════════════════

function SystemTab({ isAdmin, settings, settingsDraft, settingsSaving, settingsMsg, systemHealth, cameras, toastsEnabled, setToastsEnabled, patchDraft, handleSaveSettings, onRefreshHealth }: {
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
}) {
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

// ═══════════════════════════════════════════════════════════════
// Event Log Tab — real-time camera event feed with filters
// ═══════════════════════════════════════════════════════════════

function EventLogTab({ cameras }: { cameras: Camera[] }) {
    const [events, setEvents] = useState<CameraEvent[]>([]);
    const [loading, setLoading] = useState(false);
    const [cameraFilter, setCameraFilter] = useState('all');
    const [typeFilter, setTypeFilter] = useState('all');
    const [hoursBack, setHoursBack] = useState(1);
    const [autoRefresh, setAutoRefresh] = useState(true);

    const loadEvents = useCallback(async () => {
        setLoading(true);
        const end = new Date();
        const start = new Date(end.getTime() - hoursBack * 3600000);
        const params: any = {
            start: start.toISOString(),
            end: end.toISOString(),
            limit: 200,
        };
        if (cameraFilter !== 'all') params.camera_id = cameraFilter;
        if (typeFilter !== 'all') params.types = typeFilter;
        const data = await queryEvents(params);
        setEvents(data);
        setLoading(false);
    }, [cameraFilter, typeFilter, hoursBack]);

    useEffect(() => { loadEvents(); }, [loadEvents]);

    // Auto-refresh every 10s
    useEffect(() => {
        if (!autoRefresh) return;
        const timer = setInterval(loadEvents, 10000);
        return () => clearInterval(timer);
    }, [autoRefresh, loadEvents]);

    // Collect unique event types from results
    const eventTypes = Array.from(new Set(events.map(e => e.event_type))).sort();

    // Camera name lookup
    const camNames: Record<string, string> = {};
    for (const c of cameras) camNames[c.id] = c.name;

    const EVENT_COLORS: Record<string, string> = {
        intrusion: '#EF4444', human: '#EF4444', face: '#EF4444',
        vehicle: '#F97316', linecross: '#F97316', loitering: '#F97316', lpr: '#F97316',
        motion: '#3B82F6', object: '#3B82F6', peoplecount: '#3B82F6',
        tamper: '#E89B2A', videoloss: '#E89B2A',
    };

    return (
        <div className="settings-section" role="tabpanel">
            <div className="settings-section-title">Camera Event Log</div>
            <p className="settings-section-desc" style={{ marginBottom: 12 }}>
                Real-time feed of ONVIF and VCA events from all cameras.
            </p>

            {/* Filters */}
            <div style={{ display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center' }}>
                <select
                    className="settings-input"
                    value={cameraFilter}
                    onChange={e => setCameraFilter(e.target.value)}
                    style={{ padding: '6px 10px', fontSize: 11, minWidth: 160 }}
                >
                    <option value="all">All Cameras</option>
                    {cameras.map(c => (
                        <option key={c.id} value={c.id}>{c.name}</option>
                    ))}
                </select>
                <select
                    className="settings-input"
                    value={typeFilter}
                    onChange={e => setTypeFilter(e.target.value)}
                    style={{ padding: '6px 10px', fontSize: 11, minWidth: 130 }}
                >
                    <option value="all">All Types</option>
                    {eventTypes.map(t => (
                        <option key={t} value={t}>{t}</option>
                    ))}
                </select>
                <select
                    className="settings-input"
                    value={String(hoursBack)}
                    onChange={e => setHoursBack(parseInt(e.target.value))}
                    style={{ padding: '6px 10px', fontSize: 11, minWidth: 110 }}
                >
                    <option value="1">Last 1 hour</option>
                    <option value="4">Last 4 hours</option>
                    <option value="12">Last 12 hours</option>
                    <option value="24">Last 24 hours</option>
                    <option value="72">Last 3 days</option>
                </select>
                <button
                    className={`btn btn-sm ${autoRefresh ? 'btn-primary' : ''}`}
                    onClick={() => setAutoRefresh(v => !v)}
                    style={{ fontSize: 10, padding: '5px 10px' }}
                >
                    {autoRefresh ? 'Auto-refresh ON' : 'Auto-refresh OFF'}
                </button>
                <button className="btn btn-sm" onClick={loadEvents} disabled={loading} style={{ fontSize: 10, padding: '5px 10px' }}>
                    {loading ? 'Loading...' : '↻ Refresh'}
                </button>
                <span style={{ marginLeft: 'auto', fontSize: 10, color: '#4A5268' }}>
                    {events.length} event{events.length !== 1 ? 's' : ''}
                </span>
            </div>

            {/* Event table */}
            <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)', borderRadius: 8, overflow: 'hidden' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                        <tr style={{ borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
                            {['Time', 'Type', 'Camera', 'Details'].map(h => (
                                <th key={h} style={{
                                    padding: '9px 14px', textAlign: 'left', fontSize: 9, fontWeight: 600,
                                    letterSpacing: 1.2, textTransform: 'uppercase', color: '#4A5268',
                                    background: 'rgba(255,255,255,0.02)',
                                }}>{h}</th>
                            ))}
                        </tr>
                    </thead>
                    <tbody>
                        {events.map(evt => {
                            const color = EVENT_COLORS[evt.event_type] || '#8891A5';
                            const time = new Date(evt.event_time);
                            const topic = (evt.details?.topic as string) || '';
                            const driver = (evt.details?.driver as string) || '';
                            const confidence = evt.details?.confidence as string;
                            const plate = evt.details?.plate_number as string;
                            const direction = evt.details?.direction as string;
                            const targetType = evt.details?.target_type as string;
                            return (
                                <tr key={evt.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.03)' }}>
                                    <td style={{ padding: '8px 14px', fontSize: 11, color: '#8891A5', fontFamily: "'JetBrains Mono', monospace", whiteSpace: 'nowrap' }}>
                                        {time.toLocaleTimeString('en-US', { hour12: false })}<br />
                                        <span style={{ fontSize: 9, color: '#4A5268' }}>{time.toLocaleDateString()}</span>
                                    </td>
                                    <td style={{ padding: '8px 14px' }}>
                                        <span style={{
                                            display: 'inline-block', fontSize: 9, fontWeight: 700, padding: '2px 7px',
                                            borderRadius: 3, letterSpacing: 0.3, textTransform: 'uppercase',
                                            background: `${color}15`, color, border: `1px solid ${color}30`,
                                        }}>
                                            {evt.event_type}
                                        </span>
                                        {driver && (
                                            <span style={{ fontSize: 8, color: '#3B82F6', marginLeft: 4 }}>{driver}</span>
                                        )}
                                    </td>
                                    <td style={{ padding: '8px 14px', fontSize: 11, color: '#E4E8F0' }}>
                                        {camNames[evt.camera_id] || evt.camera_id.slice(0, 8)}
                                    </td>
                                    <td style={{ padding: '8px 14px', fontSize: 10, color: '#4A5268' }}>
                                        {confidence && <span style={{ marginRight: 8 }}>Confidence: {confidence}%</span>}
                                        {plate && <span style={{ marginRight: 8, color: '#E89B2A', fontWeight: 600 }}>Plate: {plate}</span>}
                                        {direction && <span style={{ marginRight: 8 }}>Direction: {direction}</span>}
                                        {targetType && <span style={{ marginRight: 8 }}>Target: {targetType}</span>}
                                        {topic && (
                                            <span style={{ fontSize: 9, color: '#2a3848', fontFamily: "'JetBrains Mono', monospace" }}>
                                                {topic.length > 60 ? '...' + topic.slice(-60) : topic}
                                            </span>
                                        )}
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
                {events.length === 0 && !loading && (
                    <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
                        No events in the selected time range
                    </div>
                )}
                {loading && events.length === 0 && (
                    <div style={{ padding: 30, textAlign: 'center', color: '#4A5268', fontSize: 12 }}>
                        Loading events...
                    </div>
                )}
            </div>
        </div>
    );
}
