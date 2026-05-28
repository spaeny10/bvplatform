'use client';

// PPEZoneEditor — draws Ironsight server-side PPE/safety zones on a camera
// snapshot. These zones are used by Ironsight to spatially filter YOLO
// violation detections; they are NEVER pushed to camera firmware.
//
// Canvas logic mirrors VCAZoneEditor.tsx. No "Push to camera" / "Pull from
// camera" toolbar — its absence is a deliberate UX signal that these zones
// live only in Ironsight.
//
// P2-C-04. useVCACanvas hook extraction (P1-B-11 / commit 8a358a7) is on a
// sibling branch; canvas logic is inlined here rather than duplicated via a
// missing import to keep this branch self-contained and buildable.

import { useState, useEffect, useRef, useCallback } from 'react';
import {
    PPEZone, PPEZoneCreate,
    listPPEZones, createPPEZone, updatePPEZone, deletePPEZone,
    getVCASnapshotURL, VCAPoint,
} from '@/lib/api';
import { PPE_ZONE_TYPES, PPEZoneTypeKey, ppeZoneConfig, pointInPolygon } from '@/lib/vca-zones';
import { mintMediaToken } from '@/lib/media';

type DrawMode = 'idle' | 'drawing';

interface Props {
    cameraId: string;
}

export default function PPEZoneEditor({ cameraId }: Props) {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const imgRef = useRef<HTMLImageElement | null>(null);

    const [zones, setZones] = useState<PPEZone[]>([]);
    const [selectedZoneId, setSelectedZoneId] = useState<string | null>(null);
    const [drawMode, setDrawMode] = useState<DrawMode>('idle');
    const [drawType, setDrawType] = useState<PPEZoneTypeKey>('ppe_required');
    const [drawPoints, setDrawPoints] = useState<VCAPoint[]>([]);
    const [drawName, setDrawName] = useState('');
    const [snapshotLoaded, setSnapshotLoaded] = useState(false);
    const [snapshotError, setSnapshotError] = useState<string | null>(null);
    const [saveResult, setSaveResult] = useState<string | null>(null);
    const [hoveredVertex, setHoveredVertex] = useState<{ zoneIdx: number; ptIdx: number } | null>(null);
    const [draggingVertex, setDraggingVertex] = useState<{ zoneId: string; ptIdx: number } | null>(null);
    const [deleting, setDeleting] = useState<string | null>(null);

    // ── Load zones ──
    const loadZones = useCallback(async () => {
        const data = await listPPEZones(cameraId);
        setZones(data);
    }, [cameraId]);

    useEffect(() => { loadZones(); }, [loadZones]);

    // ── Load snapshot ──
    const loadSnapshot = useCallback(async () => {
        setSnapshotLoaded(false);
        setSnapshotError(null);

        try {
            const res = await fetch(getVCASnapshotURL(cameraId), { credentials: 'include' });
            if (res.ok) {
                const blob = await res.blob();
                if (blob.size > 0) {
                    const url = URL.createObjectURL(blob);
                    const img = new Image();
                    img.onload = () => { imgRef.current = img; setSnapshotLoaded(true); };
                    img.onerror = () => { setSnapshotError('Snapshot image failed to decode'); setSnapshotLoaded(true); };
                    img.src = url;
                    return;
                }
            }
        } catch { /* try fallback */ }

        try {
            const minted = await mintMediaToken({ camera_id: cameraId, kind: 'hls', path: 'sub_live.m3u8' });
            const hlsUrl = minted.url;
            const res = await fetch(hlsUrl);
            if (res.ok) {
                const video = document.createElement('video');
                video.crossOrigin = 'anonymous';
                video.muted = true;
                video.playsInline = true;
                video.src = hlsUrl;
                video.currentTime = 0.1;
                await new Promise<void>((resolve, reject) => {
                    video.onloadeddata = () => resolve();
                    video.onerror = () => reject();
                    setTimeout(reject, 5000);
                });
                const canvas = document.createElement('canvas');
                canvas.width = video.videoWidth || 640;
                canvas.height = video.videoHeight || 360;
                canvas.getContext('2d')?.drawImage(video, 0, 0);
                const dataUrl = canvas.toDataURL('image/jpeg');
                const img = new Image();
                img.onload = () => { imgRef.current = img; setSnapshotLoaded(true); };
                img.src = dataUrl;
                video.pause();
                video.src = '';
                return;
            }
        } catch { /* proceed without snapshot */ }

        setSnapshotError('Could not load camera snapshot. Zones can still be drawn on the grid.');
        setSnapshotLoaded(true);
    }, [cameraId]);

    useEffect(() => { loadSnapshot(); }, [loadSnapshot]);

    // ── Canvas rendering ──
    const render = useCallback(() => {
        const canvas = canvasRef.current;
        if (!canvas) return;
        const ctx = canvas.getContext('2d');
        if (!ctx) return;

        const w = canvas.width;
        const h = canvas.height;

        ctx.clearRect(0, 0, w, h);
        if (imgRef.current) {
            ctx.drawImage(imgRef.current, 0, 0, w, h);
        } else {
            ctx.fillStyle = '#0c0f14';
            ctx.fillRect(0, 0, w, h);
            ctx.strokeStyle = 'rgba(255,255,255,0.06)';
            ctx.lineWidth = 0.5;
            for (let gx = 0; gx <= w; gx += w / 8) {
                ctx.beginPath(); ctx.moveTo(gx, 0); ctx.lineTo(gx, h); ctx.stroke();
            }
            for (let gy = 0; gy <= h; gy += h / 6) {
                ctx.beginPath(); ctx.moveTo(0, gy); ctx.lineTo(w, gy); ctx.stroke();
            }
        }

        // Draw existing zones
        for (const zone of zones) {
            const cfg = ppeZoneConfig(zone.zone_type);
            const pts = zone.region.map(p => ({ x: p.x * w, y: p.y * h }));
            if (pts.length < 3) continue;

            const isSelected = zone.id === selectedZoneId;
            const alpha = zone.enabled ? 1 : 0.35;

            ctx.save();
            ctx.globalAlpha = alpha;

            ctx.beginPath();
            ctx.moveTo(pts[0].x, pts[0].y);
            for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i].x, pts[i].y);
            ctx.closePath();
            ctx.fillStyle = cfg.fill;
            ctx.fill();
            ctx.strokeStyle = cfg.color;
            ctx.lineWidth = isSelected ? 2.5 : 1.5;
            if (!zone.enabled) ctx.setLineDash([4, 4]);
            ctx.stroke();
            ctx.setLineDash([]);

            // Vertex handles when selected
            if (isSelected) {
                for (let i = 0; i < pts.length; i++) {
                    const isHover = hoveredVertex?.zoneIdx === zones.indexOf(zone) && hoveredVertex?.ptIdx === i;
                    ctx.beginPath();
                    ctx.arc(pts[i].x, pts[i].y, isHover ? 7 : 5, 0, Math.PI * 2);
                    ctx.fillStyle = '#fff';
                    ctx.fill();
                    ctx.strokeStyle = cfg.color;
                    ctx.lineWidth = 2;
                    ctx.stroke();
                }
            }

            // Label at centroid
            const cx = pts.reduce((s, p) => s + p.x, 0) / pts.length;
            const cy = pts.reduce((s, p) => s + p.y, 0) / pts.length;
            const label = zone.name || cfg.label;
            ctx.font = '600 10px Inter, sans-serif';
            const tw = ctx.measureText(label).width;
            ctx.fillStyle = 'rgba(0,0,0,0.75)';
            ctx.roundRect(cx - tw / 2 - 5, cy - 7, tw + 10, 14, 3);
            ctx.fill();
            ctx.fillStyle = cfg.color;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillText(label, cx, cy);

            ctx.restore();
        }

        // Draw in-progress polygon
        if (drawMode === 'drawing' && drawPoints.length > 0) {
            const cfg = ppeZoneConfig(drawType);
            const pts = drawPoints.map(p => ({ x: p.x * w, y: p.y * h }));

            ctx.beginPath();
            ctx.moveTo(pts[0].x, pts[0].y);
            for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i].x, pts[i].y);

            if (pts.length >= 3) {
                ctx.closePath();
                ctx.fillStyle = cfg.fill;
                ctx.fill();
            }

            ctx.strokeStyle = cfg.color;
            ctx.lineWidth = 2;
            ctx.setLineDash([5, 5]);
            ctx.stroke();
            ctx.setLineDash([]);

            for (const pt of pts) {
                ctx.beginPath();
                ctx.arc(pt.x, pt.y, 5, 0, Math.PI * 2);
                ctx.fillStyle = '#fff';
                ctx.fill();
                ctx.strokeStyle = cfg.color;
                ctx.lineWidth = 2;
                ctx.stroke();
            }
        }
    }, [zones, selectedZoneId, drawMode, drawType, drawPoints, hoveredVertex]);

    useEffect(() => { render(); }, [render, snapshotLoaded]);

    // ── Mouse handlers ──
    const getCanvasPoint = (e: React.MouseEvent): VCAPoint => {
        const canvas = canvasRef.current!;
        const rect = canvas.getBoundingClientRect();
        return {
            x: Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)),
            y: Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height)),
        };
    };

    const handleCanvasClick = (e: React.MouseEvent) => {
        if (draggingVertex) return;

        if (drawMode === 'drawing') {
            const pt = getCanvasPoint(e);

            // Close polygon: click near first point
            if (drawPoints.length >= 3) {
                const first = drawPoints[0];
                const dist = Math.sqrt((pt.x - first.x) ** 2 + (pt.y - first.y) ** 2);
                if (dist < 0.03) {
                    finishDrawing();
                    return;
                }
            }

            setDrawPoints(prev => [...prev, pt]);
        } else {
            // Select/deselect zones
            const pt = getCanvasPoint(e);
            const canvas = canvasRef.current!;
            const w = canvas.width;
            const h = canvas.height;

            for (const zone of [...zones].reverse()) {
                const pts = zone.region.map(p => ({ x: p.x * w, y: p.y * h }));
                if (pointInPolygon(pt.x * w, pt.y * h, pts)) {
                    setSelectedZoneId(zone.id);
                    return;
                }
            }
            setSelectedZoneId(null);
        }
    };

    const handleCanvasMouseDown = (e: React.MouseEvent) => {
        if (drawMode === 'drawing' || !selectedZoneId) return;
        const pt = getCanvasPoint(e);
        const zone = zones.find(z => z.id === selectedZoneId);
        if (!zone) return;

        for (let i = 0; i < zone.region.length; i++) {
            const vx = zone.region[i].x;
            const vy = zone.region[i].y;
            const dist = Math.sqrt((pt.x - vx) ** 2 + (pt.y - vy) ** 2);
            if (dist < 0.025) {
                setDraggingVertex({ zoneId: zone.id, ptIdx: i });
                return;
            }
        }
    };

    const handleCanvasMouseMove = (e: React.MouseEvent) => {
        if (draggingVertex) {
            const pt = getCanvasPoint(e);
            setZones(prev => prev.map(z => {
                if (z.id !== draggingVertex.zoneId) return z;
                const newRegion = [...z.region];
                newRegion[draggingVertex.ptIdx] = pt;
                return { ...z, region: newRegion };
            }));
            render();
        }
    };

    const handleCanvasMouseUp = async () => {
        if (draggingVertex) {
            const zone = zones.find(z => z.id === draggingVertex.zoneId);
            if (zone) {
                try {
                    await updatePPEZone(cameraId, zone.id, zoneToCreate(zone));
                    setSaveResult('Zone vertex updated.');
                } catch { /* best-effort */ }
            }
            setDraggingVertex(null);
        }
    };

    const handleDoubleClick = () => {
        if (drawMode === 'drawing' && drawPoints.length >= 3) {
            finishDrawing();
        }
    };

    const finishDrawing = async (pts?: VCAPoint[]) => {
        const region = pts || drawPoints;
        const cfg = ppeZoneConfig(drawType);
        if (region.length < cfg.minPoints) return;

        try {
            await createPPEZone(cameraId, {
                zone_type: drawType,
                name: drawName || cfg.label + ' ' + (zones.filter(z => z.zone_type === drawType).length + 1),
                region,
                enabled: true,
            });
            await loadZones();
            setSaveResult('Zone saved.');
        } catch (err: any) {
            setSaveResult('Failed to save zone: ' + (err?.message || err));
        }

        setDrawMode('idle');
        setDrawPoints([]);
        setDrawName('');
    };

    const handleToggle = async (zone: PPEZone) => {
        try {
            await updatePPEZone(cameraId, zone.id, { ...zoneToCreate(zone), enabled: !zone.enabled });
            await loadZones();
        } catch { /* best-effort */ }
    };

    const handleDelete = async (zone: PPEZone) => {
        setDeleting(zone.id);
        try {
            await deletePPEZone(cameraId, zone.id);
            setSelectedZoneId(null);
            await loadZones();
            setSaveResult('Zone deleted.');
        } catch (err: any) {
            const msg = err?.message || String(err);
            setSaveResult(msg.includes('compliance rule') ? msg : 'Delete failed: ' + msg);
        } finally {
            setDeleting(null);
        }
    };

    const zoneToCreate = (z: PPEZone): PPEZoneCreate => ({
        zone_type: z.zone_type,
        name: z.name,
        region: z.region,
        enabled: z.enabled,
        notes: z.notes,
    });

    const selectedZone = zones.find(z => z.id === selectedZoneId);
    const stopBubble = (e: React.MouseEvent) => e.stopPropagation();

    return (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }} onClick={stopBubble} onMouseDown={stopBubble}>

            {/* Zone type picker (idle mode) */}
            {drawMode === 'idle' && (
                <div style={{ padding: '10px 12px', borderRadius: 6, background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)' }}>
                    <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 8 }}>
                        Step 1 — Choose a zone type, then draw on the camera image
                    </div>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                        {PPE_ZONE_TYPES.map(zt => (
                            <button
                                key={zt.key}
                                type="button"
                                onClick={() => { setDrawType(zt.key); setDrawMode('drawing'); setDrawPoints([]); setSelectedZoneId(null); }}
                                style={{
                                    padding: '8px 14px', borderRadius: 6, fontSize: 11, fontWeight: 600,
                                    cursor: 'pointer', fontFamily: 'inherit',
                                    background: `${zt.color}10`, border: `1px solid ${zt.color}40`,
                                    color: zt.color,
                                }}
                            >
                                {zt.label}
                            </button>
                        ))}
                    </div>
                </div>
            )}

            {/* Drawing mode controls */}
            {drawMode === 'drawing' && (() => {
                const cfg = ppeZoneConfig(drawType);
                const canSave = drawPoints.length >= cfg.minPoints;
                return (
                    <div style={{
                        padding: '10px 14px', borderRadius: 6,
                        background: `${cfg.color}08`, border: `1px solid ${cfg.color}30`,
                        display: 'flex', flexDirection: 'column', gap: 8,
                    }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                            <div style={{ flex: 1 }}>
                                <div style={{ fontSize: 12, fontWeight: 600, color: cfg.color }}>
                                    Drawing {cfg.label}
                                </div>
                                <div style={{ fontSize: 10, color: '#8891A5', marginTop: 2 }}>
                                    Click to place points ({drawPoints.length} placed, need at least {cfg.minPoints})
                                </div>
                            </div>
                            <input
                                value={drawName}
                                onChange={e => setDrawName(e.target.value)}
                                onClick={e => e.stopPropagation()}
                                placeholder="Zone name (optional)"
                                style={{
                                    padding: '4px 8px', borderRadius: 4, fontSize: 10, width: 140,
                                    background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
                                    color: '#E4E8F0', fontFamily: 'inherit',
                                }}
                            />
                        </div>
                        <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
                            <button type="button" onClick={() => { setDrawMode('idle'); setDrawPoints([]); setDrawName(''); }}
                                style={{ padding: '6px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: 'none', border: '1px solid rgba(255,255,255,0.15)', color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit' }}>
                                Cancel
                            </button>
                            {drawPoints.length > 0 && (
                                <button type="button" onClick={() => setDrawPoints(prev => prev.slice(0, -1))}
                                    style={{ padding: '6px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600, background: 'none', border: '1px solid rgba(255,255,255,0.15)', color: '#E89B2A', cursor: 'pointer', fontFamily: 'inherit' }}>
                                    Undo Point
                                </button>
                            )}
                            <button type="button" disabled={!canSave} onClick={() => finishDrawing()}
                                style={{
                                    padding: '6px 20px', borderRadius: 4, fontSize: 11, fontWeight: 700,
                                    background: canSave ? `${cfg.color}20` : 'rgba(255,255,255,0.02)',
                                    border: `1px solid ${canSave ? `${cfg.color}60` : 'rgba(255,255,255,0.08)'}`,
                                    color: canSave ? cfg.color : '#4A5268',
                                    cursor: canSave ? 'pointer' : 'not-allowed', fontFamily: 'inherit',
                                }}>
                                Save Zone
                            </button>
                        </div>
                    </div>
                );
            })()}

            {saveResult && (
                <div style={{
                    fontSize: 10, padding: '5px 10px', borderRadius: 4,
                    background: saveResult.includes('failed') || saveResult.includes('Failed') ? 'rgba(239,68,68,0.08)' : 'rgba(34,197,94,0.08)',
                    color: saveResult.includes('failed') || saveResult.includes('Failed') ? '#EF4444' : '#22C55E',
                    border: `1px solid ${saveResult.includes('failed') || saveResult.includes('Failed') ? 'rgba(239,68,68,0.2)' : 'rgba(34,197,94,0.2)'}`,
                }}>
                    {saveResult}
                </div>
            )}

            {/* Canvas */}
            <div style={{ position: 'relative', borderRadius: 6, overflow: 'hidden', border: '1px solid rgba(255,255,255,0.08)' }}>
                <canvas
                    ref={canvasRef}
                    width={640}
                    height={360}
                    onClick={handleCanvasClick}
                    onDoubleClick={handleDoubleClick}
                    onMouseDown={handleCanvasMouseDown}
                    onMouseMove={handleCanvasMouseMove}
                    onMouseUp={handleCanvasMouseUp}
                    style={{
                        width: '100%', display: 'block',
                        cursor: drawMode === 'drawing' ? 'crosshair' : draggingVertex ? 'grabbing' : 'default',
                    }}
                />
                {!snapshotLoaded && (
                    <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#4A5268', fontSize: 12, background: 'rgba(10,12,16,0.8)' }}>
                        Loading camera snapshot...
                    </div>
                )}
                {snapshotLoaded && snapshotError && (
                    <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 8, pointerEvents: 'none' }}>
                        <div style={{ fontSize: 11, color: '#E89B2A', fontWeight: 500, pointerEvents: 'auto', textAlign: 'center', lineHeight: 1.5 }}>
                            {snapshotError}
                        </div>
                        <button type="button" onClick={e => { e.stopPropagation(); loadSnapshot(); }}
                            style={{ pointerEvents: 'auto', padding: '4px 12px', borderRadius: 4, fontSize: 10, fontWeight: 600, background: 'rgba(232,155,42,0.1)', border: '1px solid rgba(232,155,42,0.3)', color: '#E89B2A', cursor: 'pointer', fontFamily: 'inherit' }}>
                            Retry Snapshot
                        </button>
                    </div>
                )}
            </div>

            {/* Zone list */}
            {zones.length > 0 && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268' }}>
                        Zones ({zones.length})
                    </div>
                    {zones.map(zone => {
                        const cfg = ppeZoneConfig(zone.zone_type);
                        const isSelected = zone.id === selectedZoneId;
                        return (
                            <div key={zone.id} onClick={() => setSelectedZoneId(isSelected ? null : zone.id)}
                                style={{
                                    display: 'flex', alignItems: 'center', gap: 8,
                                    padding: '6px 10px', borderRadius: 4, cursor: 'pointer',
                                    background: isSelected ? `${cfg.color}12` : 'rgba(255,255,255,0.02)',
                                    border: `1px solid ${isSelected ? `${cfg.color}40` : 'rgba(255,255,255,0.04)'}`,
                                    opacity: zone.enabled ? 1 : 0.5,
                                }}
                            >
                                <div style={{ width: 10, height: 10, borderRadius: 2, background: cfg.color, flexShrink: 0 }} />
                                <div style={{ flex: 1, minWidth: 0 }}>
                                    <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>{zone.name || cfg.label}</div>
                                    <div style={{ fontSize: 9, color: '#4A5268' }}>{cfg.label} · {zone.region.length} points</div>
                                </div>
                                <button type="button" onClick={e => { e.stopPropagation(); handleToggle(zone); }}
                                    style={{ fontSize: 8, fontWeight: 700, padding: '2px 6px', borderRadius: 3, background: zone.enabled ? 'rgba(34,197,94,0.1)' : 'rgba(255,255,255,0.04)', border: `1px solid ${zone.enabled ? 'rgba(34,197,94,0.3)' : 'rgba(255,255,255,0.08)'}`, color: zone.enabled ? '#22C55E' : '#4A5268', cursor: 'pointer', fontFamily: 'inherit' }}>
                                    {zone.enabled ? 'ON' : 'OFF'}
                                </button>
                                <button type="button" onClick={e => { e.stopPropagation(); handleDelete(zone); }} disabled={deleting === zone.id}
                                    style={{ fontSize: 8, fontWeight: 700, padding: '2px 6px', borderRadius: 3, background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.2)', color: '#EF4444', cursor: deleting === zone.id ? 'wait' : 'pointer', fontFamily: 'inherit' }}>
                                    {deleting === zone.id ? '...' : 'Del'}
                                </button>
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
