'use client';

// CameraPicker overlay: dropdown for assigning a camera to a grid cell.
// Extracted from CameraGrid.tsx (P1-B-11 session 5).

import { useState } from 'react';
import type { Camera } from '@/lib/api';

interface CameraPickerProps {
    cameras: Camera[];
    assignedCameraIds: Set<string>;
    currentCameraId: string | null;
    onSelect: (cameraId: string) => void;
    onClear: (() => void) | null;
}

export default function CameraPicker({ cameras, assignedCameraIds, currentCameraId, onSelect, onClear }: CameraPickerProps) {
    const [search, setSearch] = useState('');
    const filtered = cameras.filter(cam =>
        cam.name.toLowerCase().includes(search.toLowerCase())
    );

    return (
        <div className="cell-picker-overlay" onClick={(e) => e.stopPropagation()}>
            <div className="cell-picker-header">Select Camera</div>

            {/* Search input */}
            <div className="cell-picker-search">
                <input
                    type="text"
                    placeholder="Search cameras..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="cell-picker-search-input"
                    autoFocus
                    onClick={(e) => e.stopPropagation()}
                />
            </div>

            {onClear && (
                <button className="cell-picker-option cell-picker-clear" onClick={onClear}>
                    <span className="picker-clear-icon">✕</span>
                    <span>Clear Cell</span>
                </button>
            )}

            <div className="cell-picker-list">
                {filtered.length === 0 && (
                    <div className="cell-picker-empty">No cameras match "{search}"</div>
                )}
                {filtered.map(cam => {
                    const isAssigned = assignedCameraIds.has(cam.id) && cam.id !== currentCameraId;
                    const isCurrent = cam.id === currentCameraId;
                    return (
                        <button
                            key={cam.id}
                            className={`cell-picker-option ${isCurrent ? 'active' : ''} ${isAssigned ? 'disabled' : ''}`}
                            onClick={() => { if (!isAssigned) onSelect(cam.id); }}
                            disabled={isAssigned}
                            title={isAssigned ? 'Already assigned to another cell' : cam.name}
                        >
                            <span className={`picker-status-dot ${cam.status === 'online' ? 'online' : 'offline'}`} />
                            <span className="picker-cam-name">{cam.name}</span>
                            {isCurrent && <span className="picker-current-badge">Current</span>}
                            {isAssigned && <span className="picker-assigned-badge">In Use</span>}
                        </button>
                    );
                })}
            </div>
        </div>
    );
}
