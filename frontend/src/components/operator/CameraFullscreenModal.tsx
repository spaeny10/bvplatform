'use client';

import { useState, useRef, useEffect, useCallback } from 'react';
import type { SiteCamera, Detection } from '@/types/ironsight';
import PTZControls from '@/components/operator/PTZControls';

interface Props {
  camera: SiteCamera;
  detections: Array<{
    type: 'person-ok' | 'person-violation' | 'vehicle' | 'equipment';
    label: string;
    conf: number;
    style: React.CSSProperties; // percentages
  }>;
  onClose: () => void;
}

const DET_COLORS: Record<string, string> = {
  'person-ok': '#22C55E',
  'person-violation': '#EF4444',
  'vehicle': '#E89B2A',
  'equipment': '#a855f7',
};

export default function CameraFullscreenModal({ camera, detections, onClose }: Props) {
  const [showPTZ, setShowPTZ] = useState(false);
  const [recording, setRecording] = useState(false);
  const [selectedDet, setSelectedDet] = useState<number | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Escape to close
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [onClose]);

  const hasViolation = detections.some(d => d.type === 'person-violation');

  return (
    <div
      style={{
        position: 'fixed', inset: 0, zIndex: 8000,
        background: 'rgba(0,0,0,0.92)',
        display: 'flex', flexDirection: 'column',
        animation: 'cam-fullscreen-enter 0.2s ease-out',
      }}
      onClick={onClose}
    >
      {/* Top toolbar */}
      <div
        style={{
          height: 44, display: 'flex', alignItems: 'center', padding: '0 16px',
          background: 'rgba(0,0,0,0.6)', borderBottom: '1px solid rgba(255,255,255,0.06)',
          flexShrink: 0,
        }}
        onClick={e => e.stopPropagation()}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 12, fontWeight: 700, color: '#E4E8F0', fontFamily: "'JetBrains Mono', monospace" }}>
            {camera.id.toUpperCase()}
          </span>
          <span style={{ fontSize: 11, color: '#8891A5' }}>{camera.name}</span>
          <span style={{
            fontSize: 8, fontWeight: 700, padding: '2px 6px', borderRadius: 2,
            background: hasViolation ? 'rgba(255,51,85,0.15)' : 'rgba(0,229,160,0.1)',
            color: hasViolation ? '#EF4444' : '#22C55E',
            border: `1px solid ${hasViolation ? 'rgba(255,51,85,0.3)' : 'rgba(0,229,160,0.2)'}`,
            letterSpacing: 1,
          }}>
            {hasViolation ? '⚠ VIOLATION' : '✓ CLEAR'}
          </span>
        </div>

        <div style={{ marginLeft: 'auto', display: 'flex', gap: 6 }}>
          <span style={{
            fontSize: 8, padding: '2px 6px', borderRadius: 2,
            background: 'rgba(255,51,85,0.9)', color: '#fff', fontWeight: 700,
            animation: recording ? 'sla-breach-pulse 1s infinite alternate' : 'none',
            letterSpacing: 1,
          }}>
            ● LIVE
          </span>
          <span style={{ fontSize: 8, padding: '2px 6px', borderRadius: 2, background: 'rgba(0,212,255,0.15)', color: '#E8732A', fontWeight: 600, letterSpacing: 1 }}>
            ⊕ AI ACTIVE
          </span>
          <button
            onClick={() => setRecording(!recording)}
            style={{
              padding: '3px 10px', borderRadius: 3, fontSize: 10, fontWeight: 600,
              background: recording ? 'rgba(255,51,85,0.2)' : 'rgba(255,255,255,0.05)',
              border: `1px solid ${recording ? 'rgba(255,51,85,0.4)' : 'rgba(255,255,255,0.08)'}`,
              color: recording ? '#EF4444' : '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            {recording ? '⏹ Stop Rec' : '⏺ Record'}
          </button>
          <button
            onClick={() => setShowPTZ(!showPTZ)}
            style={{
              padding: '3px 10px', borderRadius: 3, fontSize: 10, fontWeight: 600,
              background: showPTZ ? 'rgba(0,212,255,0.1)' : 'rgba(255,255,255,0.05)',
              border: `1px solid ${showPTZ ? 'rgba(0,212,255,0.3)' : 'rgba(255,255,255,0.08)'}`,
              color: showPTZ ? '#E8732A' : '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            🎯 PTZ
          </button>
          <button
            onClick={onClose}
            style={{
              padding: '3px 10px', borderRadius: 3, fontSize: 10,
              background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.08)',
              color: '#4A5268', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ✕ Close
          </button>
        </div>
      </div>

      {/* Video area */}
      <div
        ref={containerRef}
        style={{ flex: 1, position: 'relative', overflow: 'hidden' }}
        onClick={e => e.stopPropagation()}
      >
        {/* Simulated scene */}
        <div style={{
          position: 'absolute', inset: 0,
          background: 'linear-gradient(180deg, #0a1510 0%, #141a0c 30%, #0e1206 60%, #0a0d06 100%)',
        }} />
        <div style={{
          position: 'absolute', bottom: 0, left: 0, right: 0, height: '30%',
          background: 'linear-gradient(180deg, transparent, rgba(30,25,10,0.4))',
        }} />

        {/* Grid overlay */}
        <div style={{
          position: 'absolute', inset: 0,
          backgroundImage: `
            linear-gradient(rgba(255,255,255,0.015) 1px, transparent 1px),
            linear-gradient(90deg, rgba(255,255,255,0.015) 1px, transparent 1px)
          `,
          backgroundSize: '60px 60px',
          pointerEvents: 'none',
        }} />

        {/* Detection bounding boxes (SVG for crisp rendering) */}
        <svg style={{
          position: 'absolute', inset: 0, width: '100%', height: '100%',
          pointerEvents: 'none', zIndex: 5,
        }}>
          {detections.map((det, i) => {
            const color = DET_COLORS[det.type] || '#22C55E';
            const x = parseFloat(det.style.left as string) || 0;
            const y = parseFloat(det.style.top as string) || 0;
            const w = parseFloat(det.style.width as string) || 0;
            const h = parseFloat(det.style.height as string) || 0;
            const isSelected = selectedDet === i;
            const isViolation = det.type === 'person-violation';

            return (
              <g key={i} style={{ cursor: 'pointer', pointerEvents: 'all' }} onClick={() => setSelectedDet(isSelected ? null : i)}>
                {/* Glow for violations */}
                {isViolation && (
                  <rect
                    x={`${x - 0.5}%`} y={`${y - 0.5}%`}
                    width={`${w + 1}%`} height={`${h + 1}%`}
                    fill="none" stroke={color} strokeWidth="4" rx="3"
                    opacity={0.15} style={{ filter: `blur(8px)` }}
                  />
                )}
                {/* Main box */}
                <rect
                  x={`${x}%`} y={`${y}%`}
                  width={`${w}%`} height={`${h}%`}
                  fill="none" stroke={color}
                  strokeWidth={isSelected ? 2.5 : 1.5}
                  rx="2"
                  strokeDasharray={det.type === 'equipment' ? '8 3' : 'none'}
                />
                {/* Label background */}
                <rect
                  x={`${x}%`} y={`${y - 2.5}%`}
                  width={`${Math.max(w, 8)}%`} height="2.2%"
                  rx="1" fill="rgba(0,0,0,0.85)"
                />
                {/* Label text */}
                <text
                  x={`${x + 0.5}%`} y={`${y - 0.7}%`}
                  fill={color} fontSize="11" fontWeight="600"
                  fontFamily="'JetBrains Mono', monospace"
                >
                  {det.label}
                </text>
                {/* Confidence bar */}
                <rect
                  x={`${x}%`} y={`${y + h + 0.3}%`}
                  width={`${w * det.conf}%`} height="0.4%"
                  rx="1" fill={color} opacity="0.6"
                />
                {/* Corner brackets for selected */}
                {isSelected && (
                  <>
                    <line x1={`${x}%`} y1={`${y}%`} x2={`${x + 2}%`} y2={`${y}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x}%`} y1={`${y}%`} x2={`${x}%`} y2={`${y + 2}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x + w}%`} y1={`${y}%`} x2={`${x + w - 2}%`} y2={`${y}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x + w}%`} y1={`${y}%`} x2={`${x + w}%`} y2={`${y + 2}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x}%`} y1={`${y + h}%`} x2={`${x + 2}%`} y2={`${y + h}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x}%`} y1={`${y + h}%`} x2={`${x}%`} y2={`${y + h - 2}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x + w}%`} y1={`${y + h}%`} x2={`${x + w - 2}%`} y2={`${y + h}%`} stroke="#fff" strokeWidth="2" />
                    <line x1={`${x + w}%`} y1={`${y + h}%`} x2={`${x + w}%`} y2={`${y + h - 2}%`} stroke="#fff" strokeWidth="2" />
                  </>
                )}
              </g>
            );
          })}
        </svg>

        {/* Violation banner */}
        {hasViolation && (
          <div style={{
            position: 'absolute', bottom: 60, left: '50%', transform: 'translateX(-50%)',
            padding: '8px 24px', borderRadius: 4,
            background: 'rgba(255,51,85,0.15)', border: '1px solid rgba(255,51,85,0.4)',
            color: '#EF4444', fontSize: 12, fontWeight: 700, letterSpacing: 1,
            boxShadow: '0 0 30px rgba(255,51,85,0.2)',
            animation: 'sla-breach-pulse 1.5s infinite alternate',
            zIndex: 10,
          }}>
            ⚠ ACTIVE VIOLATION DETECTED
          </div>
        )}

        {/* Selected detection info card */}
        {selectedDet !== null && detections[selectedDet] && (
          <div style={{
            position: 'absolute', top: 12, right: 12, width: 240,
            background: 'rgba(12,17,24,0.95)', border: '1px solid rgba(255,255,255,0.08)',
            borderRadius: 6, padding: 12, zIndex: 15,
            boxShadow: '0 8px 32px rgba(0,0,0,0.6)',
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }}>
              <div style={{
                width: 8, height: 8, borderRadius: '50%',
                background: DET_COLORS[detections[selectedDet].type] || '#22C55E',
              }} />
              <span style={{ fontSize: 11, fontWeight: 700, color: '#E4E8F0', textTransform: 'uppercase' }}>
                {detections[selectedDet].label}
              </span>
            </div>
            <div style={{ fontSize: 10, color: '#8891A5', lineHeight: 1.6 }}>
              <div>Type: <span style={{ color: '#E4E8F0' }}>{detections[selectedDet].type.replace('person-', '')}</span></div>
              <div>Confidence: <span style={{ color: DET_COLORS[detections[selectedDet].type], fontFamily: "'JetBrains Mono', monospace" }}>{Math.round(detections[selectedDet].conf * 100)}%</span></div>
              <div>Track ID: <span style={{ color: '#E4E8F0', fontFamily: "'JetBrains Mono', monospace" }}>#{1000 + selectedDet}</span></div>
              <div>Camera: <span style={{ color: '#E4E8F0' }}>{camera.name}</span></div>
            </div>
          </div>
        )}

        {/* PTZ Controls */}
        {showPTZ && (
          <div style={{ position: 'absolute', bottom: 12, left: 12, zIndex: 15 }}>
            <PTZControls cameraId={camera.id} />
          </div>
        )}

        {/* Camera info overlay */}
        <div style={{
          position: 'absolute', bottom: 8, right: 12,
          fontSize: 9, color: 'rgba(255,255,255,0.25)',
          fontFamily: "'JetBrains Mono', monospace",
          textAlign: 'right', lineHeight: 1.5, zIndex: 10,
        }}>
          <div>{camera.id.toUpperCase()} · {camera.name}</div>
          <div>{camera.location} · 30 FPS · 1080p</div>
          <div>{new Date().toLocaleTimeString('en-US', { hour12: false })}</div>
        </div>

        {/* Crosshair center */}
        <div style={{
          position: 'absolute', top: '50%', left: '50%',
          transform: 'translate(-50%, -50%)',
          width: 30, height: 30, pointerEvents: 'none', zIndex: 10,
        }}>
          <div style={{ position: 'absolute', top: '50%', left: 0, right: 0, height: 1, background: 'rgba(255,255,255,0.06)' }} />
          <div style={{ position: 'absolute', left: '50%', top: 0, bottom: 0, width: 1, background: 'rgba(255,255,255,0.06)' }} />
        </div>
      </div>
    </div>
  );
}
