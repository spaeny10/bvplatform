'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { Camera } from '@/lib/api';

interface Props {
    cameras: Camera[];
    onCameraClick?: (cameraId: string) => void;
}

interface MapCamera {
    id: string;
    name: string;
    status: string;
    x: number; // 0-100 percentage
    y: number; // 0-100 percentage
}

export default function MapView({ cameras, onCameraClick }: Props) {
    const [mapImage, setMapImage] = useState<string | null>(null);
    const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>({});
    const [dragging, setDragging] = useState<string | null>(null);
    const [editMode, setEditMode] = useState(false);
    const mapRef = useRef<HTMLDivElement>(null);

    // Load saved positions from localStorage
    useEffect(() => {
        const saved = localStorage.getItem('map-positions');
        if (saved) {
            try { setPositions(JSON.parse(saved)); } catch { /* ignore */ }
        }
        const savedMap = localStorage.getItem('map-image');
        if (savedMap) setMapImage(savedMap);
    }, []);

    // Save positions when they change
    useEffect(() => {
        if (Object.keys(positions).length > 0) {
            localStorage.setItem('map-positions', JSON.stringify(positions));
        }
    }, [positions]);

    const handleMapUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        if (!file) return;
        const reader = new FileReader();
        reader.onload = (ev) => {
            const dataUrl = ev.target?.result as string;
            setMapImage(dataUrl);
            localStorage.setItem('map-image', dataUrl);
        };
        reader.readAsDataURL(file);
    };

    const handleMouseDown = (cameraId: string, e: React.MouseEvent) => {
        if (!editMode) return;
        e.preventDefault();
        setDragging(cameraId);
    };

    const handleMouseMove = useCallback((e: MouseEvent) => {
        if (!dragging || !mapRef.current) return;
        const rect = mapRef.current.getBoundingClientRect();
        const x = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100));
        const y = Math.max(0, Math.min(100, ((e.clientY - rect.top) / rect.height) * 100));
        setPositions(prev => ({ ...prev, [dragging]: { x, y } }));
    }, [dragging]);

    const handleMouseUp = useCallback(() => {
        setDragging(null);
    }, []);

    useEffect(() => {
        if (dragging) {
            window.addEventListener('mousemove', handleMouseMove);
            window.addEventListener('mouseup', handleMouseUp);
            return () => {
                window.removeEventListener('mousemove', handleMouseMove);
                window.removeEventListener('mouseup', handleMouseUp);
            };
        }
    }, [dragging, handleMouseMove, handleMouseUp]);

    const STATUS_COLORS: Record<string, string> = {
        online: '#22c55e',
        offline: '#ef4444',
        degraded: '#f59e0b',
        unknown: '#6b7280',
    };

    return (
        <div style={{ padding: 0 }}>
            {/* Toolbar */}
            <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center' }}>
                <label className="btn" style={{ cursor: 'pointer', background: 'var(--accent-orange)', color: '#fff', borderColor: 'var(--accent-orange)' }}>
                    📁 Upload Floorplan
                    <input type="file" accept="image/*" onChange={handleMapUpload} style={{ display: 'none' }} />
                </label>
                <button
                    className={`btn ${editMode ? '' : ''}`}
                    onClick={() => setEditMode(!editMode)}
                    style={editMode ? {
                        background: 'rgba(232,115,42,0.15)',
                        borderColor: 'rgba(232,115,42,0.4)',
                        color: 'var(--accent-orange)',
                        boxShadow: '0 0 12px rgba(232,115,42,0.15)',
                    } : {}}
                >
                    {editMode ? '✓ Done Editing' : '✏️ Edit Positions'}
                </button>
                {editMode && (
                    <span style={{ fontSize: 12, opacity: 0.6, marginLeft: 8 }}>
                        Drag camera pins to position them on the map
                    </span>
                )}
            </div>

            {/* Map area */}
            <div
                ref={mapRef}
                style={{
                    position: 'relative',
                    width: '100%',
                    minHeight: 500,
                    background: mapImage ? `url(${mapImage}) center/contain no-repeat` : 'rgba(255,255,255,0.03)',
                    borderRadius: 12,
                    border: '1px solid rgba(255,255,255,0.08)',
                    overflow: 'hidden',
                    cursor: editMode ? 'crosshair' : 'default',
                }}
            >
                {!mapImage && (
                    <div style={{
                        position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
                        flexDirection: 'column', gap: 12, opacity: 0.4,
                    }}>
                        <div style={{ fontSize: 48 }}>🗺️</div>
                        <div>Upload a floorplan or site map to get started</div>
                    </div>
                )}

                {/* Camera pins */}
                {cameras.map((cam, idx) => {
                    const pos = positions[cam.id] || { x: 50, y: 50 };
                    const color = STATUS_COLORS[cam.status] || STATUS_COLORS.unknown;
                    const isPlaced = !!positions[cam.id];

                    if (!isPlaced && !editMode) return null;

                    return (
                        <div
                            key={cam.id}
                            className="map-pin-drop"
                            onMouseDown={(e) => handleMouseDown(cam.id, e)}
                            onClick={() => !editMode && onCameraClick?.(cam.id)}
                            style={{
                                position: 'absolute',
                                left: `${pos.x}%`,
                                top: `${pos.y}%`,
                                transform: 'translate(-50%, -100%)',
                                cursor: editMode ? 'grab' : 'pointer',
                                zIndex: dragging === cam.id ? 100 : 10,
                                transition: dragging === cam.id ? 'none' : 'left 0.1s, top 0.1s',
                                animationDelay: `${idx * 0.06}s`,
                            }}
                        >
                            {/* Radar pulse for online cameras */}
                            {cam.status === 'online' && !editMode && (
                                <div className="map-pin-radar" style={{ color, top: '60%' }} />
                            )}

                            {/* Pin icon */}
                            <div style={{
                                width: 28, height: 28, borderRadius: '50% 50% 50% 0',
                                background: color, transform: 'rotate(-45deg)',
                                display: 'flex', alignItems: 'center', justifyContent: 'center',
                                boxShadow: `0 2px 12px ${color}60`,
                                border: '2px solid rgba(255,255,255,0.3)',
                                transition: 'box-shadow 0.2s ease',
                            }}>
                                <span style={{ transform: 'rotate(45deg)', fontSize: 12, color: '#fff' }}>📷</span>
                            </div>
                            {/* Label */}
                            <div style={{
                                position: 'absolute', top: -20, left: '50%', transform: 'translateX(-50%)',
                                background: 'rgba(0,0,0,0.8)', padding: '2px 8px', borderRadius: 4,
                                fontSize: 10, whiteSpace: 'nowrap', color: '#fff', fontWeight: 600,
                                backdropFilter: 'blur(4px)',
                            }}>
                                {cam.name}
                            </div>
                        </div>
                    );
                })}
            </div>

            {/* Legend */}
            <div style={{ display: 'flex', gap: 16, marginTop: 12, fontSize: 12, opacity: 0.6 }}>
                {Object.entries(STATUS_COLORS).filter(([k]) => k !== 'unknown').map(([status, color]) => (
                    <div key={status} style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                        <span style={{ width: 8, height: 8, borderRadius: '50%', background: color, display: 'inline-block', boxShadow: `0 0 4px ${color}60` }} />
                        {status.charAt(0).toUpperCase() + status.slice(1)}
                    </div>
                ))}
                <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginLeft: 'auto' }}>
                    <span style={{ fontSize: 10, opacity: 0.5 }}>
                        {Object.keys(positions).length} / {cameras.length} cameras placed
                    </span>
                </div>
            </div>
        </div>
    );
}
