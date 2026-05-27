'use client';

// Extracted from CameraManager.tsx (P1-B-11 session 17). ONVIF discovery
// modal: scans the local network for cameras, lets the operator pick a
// subset, optionally previews each camera's video, and bulk-adds them
// with shared credentials. The previous design owned all this state on
// the parent CameraManager; here it moves into a self-contained modal
// that takes only `onClose` + `onRefresh` from the parent.

import { useCallback, useEffect, useState } from 'react';
import {
    createCamera, discoverCameras, getDevicePreview,
    type DiscoveredDevice,
} from '@/lib/api';

interface Props {
    onClose: () => void;
    onRefresh: () => void;
}

export default function DiscoveryModal({ onClose, onRefresh }: Props) {
    const [discovering, setDiscovering] = useState(false);
    const [discoveredDevices, setDiscoveredDevices] = useState<DiscoveredDevice[]>([]);
    const [selectedDevices, setSelectedDevices] = useState<Set<string>>(new Set());
    const [discoveryAuth, setDiscoveryAuth] = useState({ username: 'admin', password: '' });
    const [addingBulk, setAddingBulk] = useState(false);
    const [cameraPreviews, setCameraPreviews] = useState<Record<string, string>>({});
    const [loadingPreviews, setLoadingPreviews] = useState<Set<string>>(new Set());

    const handleDiscover = useCallback(async () => {
        setDiscovering(true);
        setSelectedDevices(new Set());
        try {
            const devices = await discoverCameras();
            setDiscoveredDevices(devices);
        } catch (err) {
            console.error('Discovery failed:', err);
        }
        setDiscovering(false);
    }, []);

    // Trigger initial scan on mount.
    useEffect(() => { handleDiscover(); }, [handleDiscover]);

    const toggleDeviceSelection = (address: string) => {
        const newSelected = new Set(selectedDevices);
        if (newSelected.has(address)) newSelected.delete(address);
        else newSelected.add(address);
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
            onClose();
            onRefresh();
        } catch (err) {
            console.error('Bulk add failed:', err);
        }
        setAddingBulk(false);
    };

    return (
        <div className="modal-overlay" onClick={onClose}>
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
                        <button className="btn" onClick={onClose} disabled={addingBulk}>Cancel</button>
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
    );
}
