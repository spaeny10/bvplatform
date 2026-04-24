'use client';

import type { SiteCamera } from '@/types/ironsight';
import { DraggableCameraCell } from '@/hooks/useCameraLayout';
import HLSVideoPlayer from '@/components/shared/HLSVideoPlayer';

interface OperatorCameraGridProps {
  cameras: (SiteCamera & { pinned?: boolean })[];
  siteName?: string;
  onCameraClick?: (cameraId: string) => void;
  columns?: number;
  editMode?: boolean;
  dragItem?: number | null;
  dragOverItem?: number | null;
  onDragStart?: (index: number) => void;
  onDragOver?: (index: number) => void;
  onDragEnd?: () => void;
  onTogglePin?: (cameraId: string) => void;
}

const BG_CLASSES = ['op-cam-bg-1', 'op-cam-bg-2', 'op-cam-bg-3', 'op-cam-bg-4', 'op-cam-bg-5'];

// Simulated detection boxes for mock display
const MOCK_DETECTIONS: Record<number, Array<{
  type: 'person-ok' | 'person-violation' | 'vehicle' | 'equipment';
  label: string;
  conf: number;
  style: React.CSSProperties;
}>> = {
  0: [
    { type: 'person-violation', label: 'NO HARNESS 94%', conf: 0.94, style: { left: '35%', top: '30%', width: '10%', height: '35%' } },
    { type: 'person-ok', label: 'WORKER 91%', conf: 0.91, style: { left: '60%', top: '32%', width: '8%', height: '32%' } },
    { type: 'equipment', label: 'SCAFFOLD', conf: 0.96, style: { left: '15%', top: '20%', width: '70%', height: '55%' } },
  ],
  1: [
    { type: 'person-ok', label: 'WORKER 89%', conf: 0.89, style: { left: '25%', top: '40%', width: '8%', height: '28%' } },
    { type: 'vehicle', label: 'EXCAVATOR 95%', conf: 0.95, style: { left: '50%', top: '35%', width: '25%', height: '30%' } },
  ],
  2: [
    { type: 'person-ok', label: 'WORKER 87%', conf: 0.87, style: { left: '40%', top: '38%', width: '7%', height: '30%' } },
  ],
  3: [
    { type: 'person-ok', label: 'WORKER 92%', conf: 0.92, style: { left: '30%', top: '42%', width: '8%', height: '26%' } },
    { type: 'person-ok', label: 'WORKER 88%', conf: 0.88, style: { left: '55%', top: '40%', width: '7%', height: '28%' } },
  ],
  4: [
    { type: 'person-violation', label: 'NO HAT 93%', conf: 0.93, style: { left: '45%', top: '35%', width: '9%', height: '32%' } },
  ],
};

export default function OperatorCameraGrid({
  cameras, siteName, onCameraClick,
  columns = 3, editMode = false,
  dragItem, dragOverItem,
  onDragStart, onDragOver, onDragEnd, onTogglePin,
}: OperatorCameraGridProps) {
  // Use up to columns*2 cameras for the grid
  const maxCams = columns * 2;
  const displayCameras = cameras.slice(0, Math.max(maxCams, 5));

  const gridStyle: React.CSSProperties = {
    display: 'grid',
    gridTemplateColumns: `repeat(${columns}, 1fr)`,
    gap: 2,
    flex: 1,
    overflow: 'hidden',
  };

  return (
    <>
      {/* Center top toolbar */}
      <div className="op-center-topbar">
        <span className="op-site-title">{siteName || 'All Cameras'}</span>
        <span className="op-site-subtitle">{cameras.length} CAMERAS · LIVE</span>
        <div className="op-toolbar-actions">
          <button className="op-tool-btn">◫ {columns}×{Math.ceil(displayCameras.length / columns)}</button>
          <button className="op-tool-btn">↔ Cycle</button>
          <button className="op-tool-btn primary">⏺ Rec All</button>
        </div>
      </div>

      {/* Search bar — navigates to /search on click */}
      <div className="op-search-row" onClick={() => window.location.href = '/search'} style={{ cursor: 'pointer' }}>
        <div className="op-search-input-wrap">
          <span style={{ fontSize: 12, color: 'var(--sg-text-dim)' }}>🔍</span>
          <input
            className="op-search-input"
            placeholder="Search video: 'worker without hard hat near crane'..."
            readOnly
            aria-label="Search video feeds — click to open search"
            style={{ cursor: 'pointer' }}
          />
          <span className="op-search-kbd">⌘K</span>
        </div>
      </div>

      {/* Video grid */}
      <div className="op-video-grid" style={editMode ? gridStyle : undefined}>
        {displayCameras.map((cam, idx) => {
          const dets = MOCK_DETECTIONS[idx] || [];
          const hasViolation = dets.some(d => d.type === 'person-violation');
          const cellClass = `op-video-cell ${idx === 0 && !editMode ? 'featured' : ''} ${hasViolation ? 'alert-critical' : ''}`;
          const pinned = 'pinned' in cam ? !!cam.pinned : false;

          const cellContent = (
            <div
              key={cam.id}
              className={cellClass}
              onClick={() => onCameraClick?.(cam.id)}
              role="button"
              aria-label={`Camera ${cam.name} — ${cam.location}${hasViolation ? ' — Violation detected' : ''}`}
            >
              {/* HLS video stream or background scene fallback */}
              {cam.stream_url ? (
                <HLSVideoPlayer
                  src={cam.stream_url}
                  style={{ position: 'absolute', inset: 0 }}
                />
              ) : (
                <>
                  <div className={`op-cam-bg ${BG_CLASSES[idx % BG_CLASSES.length]}`} />
                  <div className="op-scene-ground" />
                </>
              )}

              {/* Detection boxes */}
              {dets.map((det, di) => (
                <div
                  key={di}
                  className={`op-detection-box ${det.type}`}
                  style={det.style}
                >
                  <span className="op-detection-label">{det.label}</span>
                  <span style={{
                    position: 'absolute',
                    bottom: -5, left: 0,
                    height: 2,
                    width: `${det.conf * 100}%`,
                    background: 'currentColor',
                    borderRadius: 1,
                    opacity: 0.8,
                  }} />
                </div>
              ))}

              {/* Violation banner */}
              {hasViolation && (
                <div className="op-violation-banner">⚠ VIOLATION DETECTED</div>
              )}

              {/* Scanlines effect */}
              <div className="op-scanlines" />

              {/* Camera overlay UI */}
              <div className="op-cam-overlay">
                <div className="op-cam-header">
                  <span className="op-cam-id">{cam.id.toUpperCase()}</span>
                  <div className="op-cam-badges">
                    <span className="op-cam-badge live">● LIVE</span>
                    <span className="op-cam-badge ai">⊕ AI</span>
                  </div>
                </div>
                <div className="op-cam-footer">
                  <span className="op-cam-location">{cam.name}</span>
                  <span className="op-cam-stats">
                    {cam.location}<br />30 FPS · 1080p
                  </span>
                </div>
              </div>
            </div>
          );

          // Wrap in draggable wrapper when in edit mode
          if (editMode && onDragStart && onDragOver && onDragEnd && onTogglePin) {
            return (
              <DraggableCameraCell
                key={cam.id}
                index={idx}
                isDragging={dragItem === idx}
                isDragOver={dragOverItem === idx}
                editMode={editMode}
                pinned={pinned}
                onDragStart={onDragStart}
                onDragOver={onDragOver}
                onDragEnd={onDragEnd}
                onTogglePin={() => onTogglePin(cam.id)}
              >
                {cellContent}
              </DraggableCameraCell>
            );
          }

          return cellContent;
        })}
      </div>

      {/* Bottom metric strip */}
      <div className="op-center-bottombar">
        <div className="op-bottom-stat">
          Inference Latency <strong>24ms</strong>
        </div>
        <div className="op-bottom-stat">
          Ingest Rate <strong>2.4 Gbps</strong>
        </div>
        <div className="op-bottom-stat">
          Buffer Usage <strong>42%</strong>
        </div>
        <div className="op-bottom-stat">
          GPU Utilization <strong>68%</strong>
        </div>
        <div className="op-bottom-stat" style={{ marginLeft: 'auto' }}>
          Model <strong>SG-PPE v2.1</strong>
        </div>
      </div>
    </>
  );
}
