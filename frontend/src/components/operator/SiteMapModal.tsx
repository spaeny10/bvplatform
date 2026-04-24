'use client';

import { useEffect, useRef } from 'react';
import type { AlertEvent, SiteDetail, SiteCamera } from '@/types/ironsight';

interface Props {
  siteDetail: SiteDetail;
  alarm: AlertEvent;
  onClose: () => void;
}

// Fixed camera mounting positions around a generic industrial floor plan (SVG units 560x380)
const MOUNT_POSITIONS: { x: number; y: number; angle: number; label: string }[] = [
  { x: 44, y: 44,  angle: 135, label: 'NW' },
  { x: 280, y: 30, angle: 90,  label: 'N'  },
  { x: 516, y: 44, angle: 45,  label: 'NE' },
  { x: 530, y: 190, angle: 0,  label: 'E'  },
  { x: 516, y: 336, angle: -45, label: 'SE' },
  { x: 280, y: 350, angle: -90, label: 'S'  },
  { x: 44, y: 336, angle: -135, label: 'SW' },
  { x: 30, y: 190,  angle: 180, label: 'W'  },
];

// FOV cone for a camera: returns SVG path string
function fovPath(x: number, y: number, angleDeg: number, length = 60, spread = 40): string {
  const toRad = (d: number) => (d * Math.PI) / 180;
  const a1 = toRad(angleDeg - spread / 2);
  const a2 = toRad(angleDeg + spread / 2);
  const x1 = x + Math.cos(a1) * length;
  const y1 = y - Math.sin(a1) * length;
  const x2 = x + Math.cos(a2) * length;
  const y2 = y - Math.sin(a2) * length;
  return `M ${x} ${y} L ${x1} ${y1} A ${length} ${length} 0 0 1 ${x2} ${y2} Z`;
}

// Camera icon path (simplified camera body)
function CameraIcon({ x, y, color, size = 12 }: { x: number; y: number; color: string; size?: number }) {
  const h = size * 0.7;
  return (
    <g transform={`translate(${x - size / 2}, ${y - h / 2})`}>
      {/* Body */}
      <rect width={size * 0.75} height={h} rx={2} fill={color} opacity={0.9} />
      {/* Lens */}
      <circle cx={size * 0.75} cy={h / 2} r={h * 0.35} fill={color} opacity={0.9} />
      {/* Lens highlight */}
      <circle cx={size * 0.75} cy={h / 2} r={h * 0.18} fill="rgba(0,0,0,0.5)" />
    </g>
  );
}

export default function SiteMapModal({ siteDetail, alarm, onClose }: Props) {
  const backdropRef = useRef<HTMLDivElement>(null);

  // Close on backdrop click
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [onClose]);

  const cameras = siteDetail.cameras.slice(0, 8);

  return (
    <div
      ref={backdropRef}
      onClick={e => { if (e.target === backdropRef.current) onClose(); }}
      style={{
        position: 'fixed', inset: 0, zIndex: 200,
        background: 'rgba(5,7,10,0.88)',
        backdropFilter: 'blur(12px)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
      }}
    >
      <div style={{
        background: '#0E1117',
        border: '1px solid rgba(255,255,255,0.1)',
        borderRadius: 12,
        boxShadow: '0 24px 80px rgba(0,0,0,0.7)',
        overflow: 'hidden',
        width: 640,
        maxWidth: '95vw',
      }}>
        {/* Header */}
        <div style={{
          padding: '12px 16px',
          background: 'rgba(232,115,42,0.05)',
          borderBottom: '1px solid rgba(255,255,255,0.07)',
          display: 'flex', alignItems: 'center', gap: 10,
        }}>
          <span style={{ fontSize: 16 }}>🗺️</span>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 12, fontWeight: 700, color: '#E4E8F0' }}>
              {siteDetail.name} — Site Map
            </div>
            <div style={{ fontSize: 9, color: '#4A5268', fontFamily: "'JetBrains Mono', monospace" }}>
              {cameras.length} cameras · {siteDetail.address || 'No address on file'}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 14, alignItems: 'center', fontSize: 9, color: '#6B7590' }}>
            <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#EF4444', display: 'inline-block' }} />
              Triggered
            </span>
            <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#22C55E', display: 'inline-block' }} />
              Online
            </span>
            <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#4A5268', display: 'inline-block' }} />
              Offline
            </span>
          </div>
          <button
            onClick={onClose}
            style={{
              padding: '4px 10px', borderRadius: 4, fontSize: 11,
              background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
              color: '#6B7590', cursor: 'pointer', fontFamily: 'inherit',
            }}
          >
            ✕
          </button>
        </div>

        {/* Map SVG */}
        <div style={{ padding: '16px', background: '#080a0d' }}>
          <svg
            viewBox="0 0 560 380"
            style={{ width: '100%', height: 'auto', display: 'block' }}
          >
            {/* ── Floor plan background ── */}
            {/* Outer building perimeter */}
            <rect x={20} y={20} width={520} height={340} rx={6}
              fill="rgba(20,26,34,0.8)" stroke="rgba(232,115,42,0.2)" strokeWidth={1.5} />

            {/* Zone dividers */}
            <line x1={186} y1={20} x2={186} y2={360} stroke="rgba(255,255,255,0.05)" strokeWidth={1} strokeDasharray="4,4" />
            <line x1={373} y1={20} x2={373} y2={360} stroke="rgba(255,255,255,0.05)" strokeWidth={1} strokeDasharray="4,4" />
            <line x1={20} y1={127} x2={540} y2={127} stroke="rgba(255,255,255,0.05)" strokeWidth={1} strokeDasharray="4,4" />
            <line x1={20} y1={253} x2={540} y2={253} stroke="rgba(255,255,255,0.05)" strokeWidth={1} strokeDasharray="4,4" />

            {/* Zone labels */}
            {[
              { x: 103, y: 74, label: 'ZONE A' },
              { x: 280, y: 74, label: 'MAIN ENTRANCE' },
              { x: 457, y: 74, label: 'ZONE B' },
              { x: 103, y: 190, label: 'STORAGE' },
              { x: 280, y: 190, label: 'OPERATIONS' },
              { x: 457, y: 190, label: 'OFFICES' },
              { x: 103, y: 307, label: 'ZONE C' },
              { x: 280, y: 307, label: 'LOADING DOCK' },
              { x: 457, y: 307, label: 'ZONE D' },
            ].map(z => (
              <text key={z.label} x={z.x} y={z.y} textAnchor="middle"
                fill="rgba(255,255,255,0.06)" fontSize={9} fontFamily="JetBrains Mono, monospace"
                fontWeight={600} letterSpacing={1}>
                {z.label}
              </text>
            ))}

            {/* Entry/Exit markers */}
            <rect x={240} y={17} width={80} height={7} rx={2} fill="rgba(34,197,94,0.25)" />
            <text x={280} y={23} textAnchor="middle" fill="rgba(34,197,94,0.6)" fontSize={6} fontFamily="monospace">MAIN ENTRY</text>

            {/* ── Cameras ── */}
            {cameras.map((cam, idx) => {
              const pos = MOUNT_POSITIONS[idx % MOUNT_POSITIONS.length];
              const isTriggered = cam.id === alarm.camera_id;
              const isOffline = cam.status !== 'online';
              const camColor = isTriggered ? '#EF4444' : isOffline ? '#4A5268' : '#22C55E';
              const fovColor = isTriggered ? 'rgba(239,68,68,0.15)' : 'rgba(34,197,94,0.06)';
              const fovStroke = isTriggered ? 'rgba(239,68,68,0.4)' : 'rgba(34,197,94,0.15)';

              return (
                <g key={cam.id}>
                  {/* FOV cone */}
                  <path
                    d={fovPath(pos.x, pos.y, pos.angle)}
                    fill={fovColor}
                    stroke={fovStroke}
                    strokeWidth={0.5}
                  />

                  {/* Pulsing ring on triggered camera */}
                  {isTriggered && (
                    <>
                      <circle cx={pos.x} cy={pos.y} r={18}
                        fill="none" stroke="rgba(239,68,68,0.2)" strokeWidth={1}>
                        <animate attributeName="r" values="14;22;14" dur="1.5s" repeatCount="indefinite" />
                        <animate attributeName="opacity" values="0.4;0;0.4" dur="1.5s" repeatCount="indefinite" />
                      </circle>
                    </>
                  )}

                  {/* Camera icon */}
                  <CameraIcon x={pos.x} y={pos.y} color={camColor} size={14} />

                  {/* Camera label */}
                  <text
                    x={pos.x}
                    y={pos.y + (pos.y < 190 ? -16 : 20)}
                    textAnchor="middle"
                    fill={isTriggered ? '#EF4444' : '#6B7590'}
                    fontSize={7}
                    fontFamily="JetBrains Mono, monospace"
                    fontWeight={isTriggered ? 700 : 400}
                  >
                    {cam.name.length > 14 ? cam.name.slice(0, 13) + '…' : cam.name}
                  </text>

                  {/* Alarm badge on triggered camera */}
                  {isTriggered && (
                    <text x={pos.x + 10} y={pos.y - 6} fill="#EF4444" fontSize={10} fontWeight={700}>⚠</text>
                  )}
                </g>
              );
            })}

            {/* Compass rose */}
            <g transform="translate(510, 350)">
              <circle cx={0} cy={0} r={16} fill="rgba(255,255,255,0.03)" stroke="rgba(255,255,255,0.08)" strokeWidth={1} />
              <text x={0} y={-6} textAnchor="middle" fill="rgba(255,255,255,0.3)" fontSize={7} fontFamily="monospace">N</text>
              <line x1={0} y1={-14} x2={0} y2={-4} stroke="rgba(255,255,255,0.3)" strokeWidth={1} />
            </g>
          </svg>
        </div>

        {/* Camera index footer */}
        <div style={{
          padding: '10px 16px',
          borderTop: '1px solid rgba(255,255,255,0.06)',
          display: 'flex', flexWrap: 'wrap', gap: 8,
        }}>
          {cameras.map((cam, idx) => {
            const isTriggered = cam.id === alarm.camera_id;
            return (
              <div key={cam.id} style={{
                display: 'flex', alignItems: 'center', gap: 5,
                padding: '3px 8px', borderRadius: 4,
                background: isTriggered ? 'rgba(239,68,68,0.1)' : 'rgba(255,255,255,0.02)',
                border: `1px solid ${isTriggered ? 'rgba(239,68,68,0.3)' : 'rgba(255,255,255,0.06)'}`,
              }}>
                <span style={{
                  width: 6, height: 6, borderRadius: '50%', flexShrink: 0,
                  background: isTriggered ? '#EF4444' : cam.status === 'online' ? '#22C55E' : '#4A5268',
                }} />
                <span style={{ fontSize: 9, color: isTriggered ? '#EF4444' : '#6B7590', fontFamily: "'JetBrains Mono', monospace" }}>
                  {MOUNT_POSITIONS[idx % MOUNT_POSITIONS.length].label}
                </span>
                <span style={{ fontSize: 9, color: isTriggered ? '#E4E8F0' : '#4A5268' }}>
                  {cam.name}
                </span>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
