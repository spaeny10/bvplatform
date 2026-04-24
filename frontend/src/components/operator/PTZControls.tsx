'use client';

import { useState, useEffect } from 'react';
import { getPTZCapability, sendPTZCommand } from '@/lib/ironsight-api';
import type { PTZCapability } from '@/types/ironsight';

interface Props { cameraId: string; }

export default function PTZControls({ cameraId }: Props) {
  const [ptz, setPtz] = useState<PTZCapability | null>(null);

  useEffect(() => {
    getPTZCapability(cameraId).then(setPtz);
  }, [cameraId]);

  if (!ptz || !ptz.supports_ptz) return null;

  const move = (pan: number, tilt: number) => sendPTZCommand(cameraId, { pan, tilt });
  const zoom = (level: number) => sendPTZCommand(cameraId, { zoom: level });
  const goPreset = (presetId: string) => sendPTZCommand(cameraId, { preset_id: presetId });

  const btnStyle = (active = false): React.CSSProperties => ({
    width: 28, height: 28, borderRadius: 3, border: 'none', cursor: 'pointer',
    background: active ? 'rgba(0,212,255,0.15)' : 'rgba(0,0,0,0.5)',
    color: active ? '#E8732A' : '#8891A5', fontSize: 12,
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    backdropFilter: 'blur(4px)',
    transition: 'all 0.1s',
  });

  return (
    <div style={{
      position: 'absolute', bottom: 8, right: 8, zIndex: 20,
      display: 'flex', flexDirection: 'column', gap: 4, alignItems: 'flex-end',
    }}>
      {/* D-pad */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 28px)', gap: 2 }}>
        <div />
        <button style={btnStyle()} onClick={() => move(0, 10)} title="Tilt Up">▲</button>
        <div />
        <button style={btnStyle()} onClick={() => move(-10, 0)} title="Pan Left">◀</button>
        <button style={btnStyle()} onClick={() => move(0, 0)} title="Center">⊙</button>
        <button style={btnStyle()} onClick={() => move(10, 0)} title="Pan Right">▶</button>
        <div />
        <button style={btnStyle()} onClick={() => move(0, -10)} title="Tilt Down">▼</button>
        <div />
      </div>

      {/* Zoom */}
      <div style={{ display: 'flex', gap: 2 }}>
        <button style={btnStyle()} onClick={() => zoom(-1)} title="Zoom Out">−</button>
        <button style={btnStyle()} onClick={() => zoom(1)} title="Zoom In">+</button>
      </div>

      {/* Presets */}
      {ptz.supports_presets && ptz.presets.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 2, maxWidth: 120 }}>
          {ptz.presets.map(p => (
            <button key={p.id} style={{
              ...btnStyle(), width: 'auto', height: 'auto',
              padding: '3px 8px', fontSize: 8, fontWeight: 600, letterSpacing: 0.3,
            }} onClick={() => goPreset(p.id)} title={`Go to ${p.name}`}>
              📍 {p.name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
