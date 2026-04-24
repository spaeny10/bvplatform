'use client';

import { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { getSiteMap, getCameraAssignments, getExclusionZones, updateSiteMap } from '@/lib/ironsight-api';
import { useSites } from '@/hooks/useSites';
import type { SiteMapData, SiteMapMarker, CameraAssignment, ExclusionZone } from '@/types/ironsight';

interface Props {
  siteId: string;
  onClose: () => void;
  embedded?: boolean;
}

type MapMode = 'view' | 'place' | 'drag';

export default function SiteMapModal({ siteId, onClose, embedded }: Props) {
  const { data: sites = [] } = useSites();
  const site = sites.find(s => s.id === siteId);

  const [mapData, setMapData] = useState<SiteMapData | null>(null);
  const [cameras, setCameras] = useState<CameraAssignment[]>([]);
  const [exclusionZones, setExclusionZones] = useState<ExclusionZone[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedMarkerId, setSelectedMarkerId] = useState<string | null>(null);
  const [hoveredMarkerId, setHoveredMarkerId] = useState<string | null>(null);

  // Interactive state
  const [mode, setMode] = useState<MapMode>('view');
  const [placingCameraId, setPlacingCameraId] = useState<string | null>(null);
  const [placingCameraName, setPlacingCameraName] = useState('');
  const [placingLabel, setPlacingLabel] = useState('');
  const [dragMarkerId, setDragMarkerId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [uploadingImage, setUploadingImage] = useState(false);
  const mapRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Floor plan image state
  const [floorPlanUrl, setFloorPlanUrl] = useState<string | null>(null);

  useEffect(() => {
    Promise.all([
      getSiteMap(siteId),
      getCameraAssignments(siteId),
      getExclusionZones(siteId),
    ]).then(([map, cams, zones]) => {
      setMapData(map);
      if (map?.image_url) setFloorPlanUrl(map.image_url);
      setCameras(cams);
      setExclusionZones(zones);
      setLoading(false);
    });
  }, [siteId]);

  const markers = mapData?.markers || [];
  const selectedMarker = markers.find(m => m.id === selectedMarkerId);

  const unmappedCameras = useMemo(() => {
    const mappedCameraIds = new Set(markers.map(m => m.camera_id));
    return cameras.filter(c => !mappedCameraIds.has(c.camera_id));
  }, [markers, cameras]);

  // ── Save markers to backend ──
  const saveMarkers = useCallback(async (newMarkers: SiteMapMarker[]) => {
    setSaving(true);
    try {
      const updated = await updateSiteMap(siteId, { markers: newMarkers });
      setMapData(updated);
    } catch { /* best-effort */ }
    setSaving(false);
  }, [siteId]);

  // ── Get position from mouse event ──
  const getMapPosition = (e: React.MouseEvent): { x: number; y: number } | null => {
    const el = mapRef.current;
    if (!el) return null;
    const rect = el.getBoundingClientRect();
    return {
      x: Math.round(Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100))),
      y: Math.round(Math.max(0, Math.min(100, ((e.clientY - rect.top) / rect.height) * 100))),
    };
  };

  // ── Place camera on click ──
  const handleMapClick = (e: React.MouseEvent) => {
    if (mode === 'place' && placingCameraId) {
      const pos = getMapPosition(e);
      if (!pos) return;
      const cam = cameras.find(c => c.camera_id === placingCameraId);
      const newMarker: SiteMapMarker = {
        id: `marker-${Date.now()}`,
        camera_id: placingCameraId,
        camera_name: placingCameraName || cam?.camera_name || 'Camera',
        x: pos.x,
        y: pos.y,
        rotation: 0,
        fov_angle: 90,
        label: placingLabel || cam?.location_label || placingCameraName,
      };
      const newMarkers = [...markers, newMarker];
      saveMarkers(newMarkers);
      setMode('view');
      setPlacingCameraId(null);
      setPlacingLabel('');
      setSelectedMarkerId(newMarker.id);
    }
  };

  // ── Drag camera ──
  const handleMapMouseMove = (e: React.MouseEvent) => {
    if (mode === 'drag' && dragMarkerId) {
      const pos = getMapPosition(e);
      if (!pos) return;
      setMapData(prev => {
        if (!prev) return prev;
        return {
          ...prev,
          markers: prev.markers.map(m => m.id === dragMarkerId ? { ...m, x: pos.x, y: pos.y } : m),
        };
      });
    }
  };

  const handleMapMouseUp = () => {
    if (mode === 'drag' && dragMarkerId) {
      saveMarkers(markers);
      setMode('view');
      setDragMarkerId(null);
    }
  };

  const handleMarkerMouseDown = (e: React.MouseEvent, markerId: string) => {
    e.stopPropagation();
    setMode('drag');
    setDragMarkerId(markerId);
    setSelectedMarkerId(markerId);
  };

  // ── Start placing a camera ──
  const startPlacing = (cam: CameraAssignment) => {
    setMode('place');
    setPlacingCameraId(cam.camera_id);
    setPlacingCameraName(cam.camera_name);
    setPlacingLabel(cam.location_label || '');
    setSelectedMarkerId(null);
  };

  // ── Remove marker ──
  const removeMarker = (markerId: string) => {
    const newMarkers = markers.filter(m => m.id !== markerId);
    saveMarkers(newMarkers);
    setSelectedMarkerId(null);
  };

  // ── Update marker properties ──
  const updateMarker = (markerId: string, patch: Partial<SiteMapMarker>) => {
    const newMarkers = markers.map(m => m.id === markerId ? { ...m, ...patch } : m);
    saveMarkers(newMarkers);
  };

  // ── Floor plan upload ──
  const handleFloorPlanUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploadingImage(true);
    const reader = new FileReader();
    reader.onload = () => {
      const dataUrl = reader.result as string;
      setFloorPlanUrl(dataUrl);
      // Save to backend (image_url as data URI for now — production would use object storage)
      updateSiteMap(siteId, { image_url: dataUrl }).then(updated => {
        if (updated) setMapData(updated);
      }).finally(() => setUploadingImage(false));
    };
    reader.readAsDataURL(file);
  };

  const bodyContent = (
    <>
      {loading && (
        <div style={{ padding: 60, textAlign: 'center', color: '#4A5268' }}>Loading site map…</div>
      )}

      {!loading && (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 220px', minHeight: 420 }}>
          {/* ── Map View ── */}
          <div
            ref={mapRef}
            onClick={handleMapClick}
            onMouseMove={handleMapMouseMove}
            onMouseUp={handleMapMouseUp}
            style={{
              position: 'relative', background: '#0a0e14',
              borderRight: '1px solid rgba(255,255,255,0.06)',
              overflow: 'hidden',
              cursor: mode === 'place' ? 'crosshair' : mode === 'drag' ? 'grabbing' : 'default',
            }}
          >
            {/* Floor plan image or grid */}
            {floorPlanUrl ? (
              <img
                src={floorPlanUrl}
                alt="Site floor plan"
                style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'contain', pointerEvents: 'none' }}
                draggable={false}
              />
            ) : (
              <>
                <div style={{
                  position: 'absolute', inset: 0,
                  backgroundImage: `
                    linear-gradient(rgba(255,255,255,0.02) 1px, transparent 1px),
                    linear-gradient(90deg, rgba(255,255,255,0.02) 1px, transparent 1px)
                  `,
                  backgroundSize: '40px 40px',
                }} />
                {/* Placeholder zones */}
                <div style={{ position: 'absolute', inset: 20, pointerEvents: 'none' }}>
                  <div style={{ position: 'absolute', left: '10%', top: '15%', width: '30%', height: '35%', border: '1px solid rgba(0,212,255,0.15)', borderRadius: 4, background: 'rgba(0,212,255,0.03)' }}>
                    <div style={{ position: 'absolute', top: 4, left: 6, fontSize: 8, color: 'rgba(0,212,255,0.4)', letterSpacing: 1, fontWeight: 600 }}>BUILDING A</div>
                  </div>
                  <div style={{ position: 'absolute', left: '50%', top: '20%', width: '25%', height: '40%', border: '1px solid rgba(0,229,160,0.15)', borderRadius: 4, background: 'rgba(0,229,160,0.03)' }}>
                    <div style={{ position: 'absolute', top: 4, left: 6, fontSize: 8, color: 'rgba(0,229,160,0.4)', letterSpacing: 1, fontWeight: 600 }}>BUILDING B</div>
                  </div>
                  <div style={{ position: 'absolute', left: '60%', top: '70%', width: '30%', height: '20%', border: '1px solid rgba(255,255,255,0.06)', borderRadius: 4, background: 'rgba(255,255,255,0.02)' }}>
                    <div style={{ position: 'absolute', top: 4, left: 6, fontSize: 8, color: 'rgba(255,255,255,0.15)', letterSpacing: 1, fontWeight: 600 }}>PARKING</div>
                  </div>
                </div>
              </>
            )}

            {/* Exclusion Zones SVG */}
            <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 5 }}>
              {exclusionZones.filter(z => z.active).map(zone => {
                const sevColors: Record<string, { stroke: string; fill: string }> = {
                  critical: { stroke: 'rgba(255,51,85,0.6)', fill: 'rgba(255,51,85,0.08)' },
                  high: { stroke: 'rgba(255,107,53,0.5)', fill: 'rgba(255,107,53,0.06)' },
                  medium: { stroke: 'rgba(255,204,0,0.4)', fill: 'rgba(255,204,0,0.04)' },
                  low: { stroke: 'rgba(0,212,255,0.3)', fill: 'rgba(0,212,255,0.03)' },
                };
                const c = sevColors[zone.severity] || sevColors.medium;
                const points = zone.polygon.map(([x, y]) => `${x}%,${y}%`).join(' ');
                return (
                  <g key={zone.id}>
                    <polygon points={points} fill={c.fill} stroke={c.stroke} strokeWidth="1.5" strokeDasharray="6 3" />
                    <text x={`${zone.polygon[0][0]}%`} y={`${zone.polygon[0][1] - 1}%`} fill={c.stroke} fontSize="8" fontWeight="600" fontFamily="sans-serif">
                      {zone.name}
                    </text>
                  </g>
                );
              })}
            </svg>

            {/* Camera markers — draggable */}
            {markers.map(marker => {
              const isSelected = selectedMarkerId === marker.id;
              const isHovered = hoveredMarkerId === marker.id;
              const active = isSelected || isHovered;
              return (
                <div
                  key={marker.id}
                  onClick={e => { e.stopPropagation(); setSelectedMarkerId(isSelected ? null : marker.id); }}
                  onMouseDown={e => handleMarkerMouseDown(e, marker.id)}
                  onMouseEnter={() => setHoveredMarkerId(marker.id)}
                  onMouseLeave={() => setHoveredMarkerId(null)}
                  style={{
                    position: 'absolute',
                    left: `${marker.x}%`,
                    top: `${marker.y}%`,
                    transform: 'translate(-50%, -50%)',
                    cursor: mode === 'drag' && dragMarkerId === marker.id ? 'grabbing' : 'grab',
                    zIndex: active ? 20 : 10,
                  }}
                >
                  {/* FOV cone */}
                  {active && marker.fov_angle && (
                    <div style={{
                      position: 'absolute', left: '50%', top: '50%',
                      width: 120, height: 120,
                      transform: `translate(-50%, -50%) rotate(${(marker.rotation || 0) - 90}deg)`,
                      background: `conic-gradient(from -${marker.fov_angle / 2}deg, rgba(0,212,255,0.12) 0deg, rgba(0,212,255,0.12) ${marker.fov_angle}deg, transparent ${marker.fov_angle}deg)`,
                      borderRadius: '50%', pointerEvents: 'none',
                    }} />
                  )}

                  {/* Marker dot */}
                  <div style={{
                    width: active ? 20 : 14, height: active ? 20 : 14,
                    borderRadius: '50%',
                    background: active ? '#E8732A' : 'rgba(0,212,255,0.6)',
                    border: `2px solid ${active ? '#fff' : 'rgba(0,212,255,0.8)'}`,
                    boxShadow: active ? '0 0 16px rgba(232,115,42,0.5)' : '0 0 8px rgba(0,212,255,0.3)',
                    transition: 'all 0.15s',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    fontSize: 8, color: '#fff', fontWeight: 700,
                  }}>
                    {markers.indexOf(marker) + 1}
                  </div>

                  {/* Label */}
                  {active && (
                    <div style={{
                      position: 'absolute', top: '100%', left: '50%', transform: 'translateX(-50%)',
                      marginTop: 4, padding: '3px 7px', borderRadius: 3,
                      background: 'rgba(0,0,0,0.85)', border: '1px solid rgba(0,212,255,0.3)',
                      fontSize: 9, color: '#E8732A', fontWeight: 600, whiteSpace: 'nowrap' as const,
                    }}>
                      {marker.label || marker.camera_name}
                    </div>
                  )}
                </div>
              );
            })}

            {/* Placement mode hint */}
            {mode === 'place' && (
              <div style={{
                position: 'absolute', top: 8, left: '50%', transform: 'translateX(-50%)',
                padding: '6px 14px', borderRadius: 6, fontSize: 11, fontWeight: 600,
                background: 'rgba(232,115,42,0.15)', border: '1px solid rgba(232,115,42,0.4)',
                color: '#E8732A', zIndex: 30, whiteSpace: 'nowrap' as const,
              }}>
                Click on the map to place "{placingCameraName}"
              </div>
            )}

            {/* Save indicator */}
            {saving && (
              <div style={{
                position: 'absolute', bottom: 8, right: 8, padding: '3px 8px',
                borderRadius: 4, fontSize: 9, background: 'rgba(0,0,0,0.7)', color: '#E89B2A', zIndex: 30,
              }}>
                Saving...
              </div>
            )}

            {/* Empty state */}
            {markers.length === 0 && mode !== 'place' && (
              <div style={{
                position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
                flexDirection: 'column', gap: 8, color: '#4A5268', pointerEvents: 'none',
              }}>
                <div style={{ fontSize: 12 }}>
                  {floorPlanUrl ? 'Click a camera in the sidebar to place it' : 'Upload a floor plan or place cameras on the grid'}
                </div>
              </div>
            )}
          </div>

          {/* ── Sidebar ── */}
          <div style={{ overflowY: 'auto', scrollbarWidth: 'thin' as const, display: 'flex', flexDirection: 'column' }}>
            {/* Upload floor plan */}
            <div style={{ padding: '10px 12px', borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
              <input ref={fileInputRef} type="file" accept="image/*" onChange={handleFloorPlanUpload} style={{ display: 'none' }} />
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                disabled={uploadingImage}
                style={{
                  width: '100%', padding: '6px 0', borderRadius: 4, fontSize: 10, fontWeight: 600,
                  background: 'rgba(59,130,246,0.08)', border: '1px solid rgba(59,130,246,0.25)',
                  color: '#3B82F6', cursor: 'pointer', fontFamily: 'inherit',
                }}
              >
                {uploadingImage ? 'Uploading...' : floorPlanUrl ? 'Replace Floor Plan' : 'Upload Floor Plan'}
              </button>
            </div>

            {/* Placed cameras */}
            <div style={{ padding: '10px 12px 4px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#4A5268' }}>
              Placed ({markers.length})
            </div>
            {markers.map(marker => (
              <div
                key={marker.id}
                onClick={() => setSelectedMarkerId(selectedMarkerId === marker.id ? null : marker.id)}
                onMouseEnter={() => setHoveredMarkerId(marker.id)}
                onMouseLeave={() => setHoveredMarkerId(null)}
                style={{
                  padding: '8px 12px', cursor: 'pointer', transition: 'background 0.1s',
                  background: selectedMarkerId === marker.id ? 'rgba(0,212,255,0.06)' : 'transparent',
                  borderBottom: '1px solid rgba(255,255,255,0.04)',
                  borderLeft: selectedMarkerId === marker.id ? '2px solid #E8732A' : '2px solid transparent',
                }}
              >
                <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>
                  {markers.indexOf(marker) + 1}. {marker.camera_name}
                </div>
                <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginTop: 1 }}>
                  {marker.label} · ({marker.x}%, {marker.y}%)
                </div>
              </div>
            ))}

            {/* Unplaced cameras — click to place */}
            {unmappedCameras.length > 0 && (
              <>
                <div style={{ padding: '12px 12px 4px', fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#E8732A' }}>
                  Click to Place ({unmappedCameras.length})
                </div>
                {unmappedCameras.map(cam => (
                  <div
                    key={cam.camera_id}
                    onClick={() => startPlacing(cam)}
                    style={{
                      padding: '8px 12px', borderBottom: '1px solid rgba(255,255,255,0.04)',
                      cursor: 'pointer', transition: 'background 0.1s',
                      background: placingCameraId === cam.camera_id ? 'rgba(232,115,42,0.08)' : 'transparent',
                    }}
                  >
                    <div style={{ fontSize: 11, fontWeight: 600, color: '#8891A5' }}>
                      {cam.camera_name}
                    </div>
                    <div style={{ fontSize: 9, color: '#4A5268', marginTop: 1 }}>
                      {cam.location_label || 'Click to place on map'}
                    </div>
                  </div>
                ))}
              </>
            )}

            {/* Selected marker editor */}
            {selectedMarker && (
              <div style={{ padding: 12, borderTop: '1px solid rgba(0,212,255,0.15)', background: 'rgba(0,212,255,0.03)', marginTop: 'auto' }}>
                <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase' as const, color: '#E8732A', marginBottom: 8 }}>
                  Edit Camera
                </div>
                <div style={{ marginBottom: 6 }}>
                  <div style={{ fontSize: 9, color: '#4A5268', marginBottom: 2 }}>Label</div>
                  <input
                    style={{ width: '100%', boxSizing: 'border-box', padding: '4px 6px', borderRadius: 3, fontSize: 11, background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', color: '#E4E8F0', fontFamily: 'inherit' }}
                    value={selectedMarker.label}
                    onChange={e => {
                      const val = e.target.value;
                      setMapData(prev => prev ? { ...prev, markers: prev.markers.map(m => m.id === selectedMarker.id ? { ...m, label: val } : m) } : prev);
                    }}
                    onBlur={() => saveMarkers(markers)}
                  />
                </div>
                <div style={{ marginBottom: 6 }}>
                  <div style={{ fontSize: 9, color: '#4A5268', marginBottom: 2 }}>Rotation ({selectedMarker.rotation || 0}°)</div>
                  <input
                    type="range" min="0" max="360" step="5"
                    value={selectedMarker.rotation || 0}
                    onChange={e => {
                      const val = parseInt(e.target.value);
                      setMapData(prev => prev ? { ...prev, markers: prev.markers.map(m => m.id === selectedMarker.id ? { ...m, rotation: val } : m) } : prev);
                    }}
                    onMouseUp={() => saveMarkers(markers)}
                    style={{ width: '100%', accentColor: '#E8732A' }}
                  />
                </div>
                <div style={{ marginBottom: 8 }}>
                  <div style={{ fontSize: 9, color: '#4A5268', marginBottom: 2 }}>FOV Angle ({selectedMarker.fov_angle || 90}°)</div>
                  <input
                    type="range" min="10" max="180" step="5"
                    value={selectedMarker.fov_angle || 90}
                    onChange={e => {
                      const val = parseInt(e.target.value);
                      setMapData(prev => prev ? { ...prev, markers: prev.markers.map(m => m.id === selectedMarker.id ? { ...m, fov_angle: val } : m) } : prev);
                    }}
                    onMouseUp={() => saveMarkers(markers)}
                    style={{ width: '100%', accentColor: '#0084ff' }}
                  />
                </div>
                <div style={{ fontSize: 10, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace", marginBottom: 8 }}>
                  Position: {selectedMarker.x}%, {selectedMarker.y}%
                </div>
                <button
                  type="button"
                  onClick={() => removeMarker(selectedMarker.id)}
                  style={{
                    width: '100%', padding: '5px 0', borderRadius: 4, fontSize: 10, fontWeight: 600,
                    background: 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.25)',
                    color: '#EF4444', cursor: 'pointer', fontFamily: 'inherit',
                  }}
                >
                  Remove from Map
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );

  if (embedded) return bodyContent;

  return (
    <div className="admin-modal-overlay" onClick={onClose}>
      <div className="admin-modal" onClick={e => e.stopPropagation()} style={{ width: 860, maxHeight: '90vh' }}>
        <div className="admin-modal-header">
          <div>
            <div className="admin-modal-title">Site Map</div>
            <div style={{ fontSize: 11, color: '#4A5268', marginTop: 2 }}>
              {site?.name || siteId} · {markers.length} camera{markers.length !== 1 ? 's' : ''} placed
            </div>
          </div>
          <button className="admin-modal-close" onClick={onClose}>✕</button>
        </div>
        <div className="admin-modal-body" style={{ padding: 0 }}>{bodyContent}</div>
        <div className="admin-modal-footer">
          <button className="admin-btn admin-btn-ghost" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  );
}
