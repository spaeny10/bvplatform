'use client';

// Extracted from SettingsPage.tsx (P1-B-11 session 10). The Storage tab —
// the largest single tab body in the file. Owns: 11 useState slots
// (storage locations + disk usages + add/edit form state + folder
// picker state), 8 handlers (load + folder navigation + CRUD), and the
// folder-picker modal that the storage forms drive.
//
// The "Legacy paths" sub-section at the bottom uses the parent's
// settings-draft state machine (shared with SystemTab + CameraDefaultsTab)
// and so it stays prop-driven — settingsDraft + handlers come in as props.
//
// Load-on-mount: the parent previously had `useEffect(() => { if (tab ===
// 'storage') loadStorageLocations(); })`. Now that this component only
// mounts when the tab is active, a plain mount effect handles the same job.

import { useCallback, useEffect, useState } from 'react';
import {
    type SystemSettings, type StorageLocation, type StorageLocationCreate,
    type DriveInfo, type FolderEntry, type DiskUsage,
    listStorageLocations, createStorageLocation, updateStorageLocation, deleteStorageLocation,
    listDrives, browsePath, getDiskUsage,
} from '@/lib/api';

interface Props {
    isAdmin: boolean;
    // Settings-draft state shared across tabs (used only by the legacy paths section)
    settingsDraft: SystemSettings | null;
    settingsSaving: boolean;
    settingsMsg: { ok: boolean; text: string } | null;
    patchDraft: (key: keyof SystemSettings, val: string | number) => void;
    handleSaveSettings: () => void;
}

export default function StorageTab({
    isAdmin, settingsDraft, settingsSaving, settingsMsg, patchDraft, handleSaveSettings,
}: Props) {
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

    useEffect(() => { loadStorageLocations(); }, [loadStorageLocations]);

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

    const PathField = ({ label, hint, field }: { label: string; hint: string; field: keyof SystemSettings }) => (
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

    return (
        <>
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

            {/* Folder picker modal */}
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
        </>
    );
}
