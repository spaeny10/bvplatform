'use client';

// Extracted from ActiveAlarmView.tsx (P1-B-11). Renders the alarm's
// video tile in one of three modes (snapshot / clip / live), overlaying
// any VCA zone polygons / lines associated with the camera. The parent
// owns the mode toggle UI and the bounding-box overlay; this child
// only loads the media for its current mode.

import { useEffect, useState } from 'react';

interface Props {
  cameraId: string;
  snapshotUrl?: string;
  clipUrl?: string;
  alarmType?: string;
  mode: 'snapshot' | 'clip' | 'live';
  boundingBoxes?: { x: number; y: number; w: number; h: number; label?: string }[];
  yoloDetections?: {
    class: string;
    confidence: number;
    bbox_normalized: { x1: number; y1: number; x2: number; y2: number };
  }[];
}

interface VCARule {
  id: string;
  rule_type: string;
  name: string;
  enabled: boolean;
  region: { x: number; y: number }[];
}

export default function AlarmVideoFeed({
  cameraId,
  snapshotUrl: eventSnapshotUrl,
  clipUrl,
  alarmType,
  mode,
}: Props) {
  const [liveFrameUrl, setLiveFrameUrl] = useState<string | null>(null);
  const [liveError, setLiveError] = useState(false);
  const [eventFrameUrl, setEventFrameUrl] = useState<string | null>(null);
  const [snapshotState, setSnapshotState] = useState<'loading' | 'ok' | 'unavailable' | 'error'>('unavailable');
  const [vcaRules, setVcaRules] = useState<VCARule[]>([]);

  // Load VCA zones for overlay
  useEffect(() => {
    fetch(`/api/cameras/${cameraId}/vca/rules`, {
      credentials: 'include',
    })
      .then(r => r.ok ? r.json() : [])
      .then(data => setVcaRules(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, [cameraId]);

  // Fetch the event snapshot once (for snapshot mode).
  // P1-A-03: eventSnapshotUrl may be a legacy /snapshots/<cam>/<file>
  // URL stored by the sense webhook / alarm pipeline before the
  // signed-URL flow existed. resolveMediaURL handles both shapes —
  // already-signed pass-through, legacy → mint.
  useEffect(() => {
    if (!eventSnapshotUrl) {
      setSnapshotState('unavailable');
      return;
    }
    setSnapshotState('loading');
    let cancelled = false;
    (async () => {
      try {
        const { resolveMediaURL } = await import('@/lib/media');
        const url = await resolveMediaURL(eventSnapshotUrl);
        if (cancelled || !url) {
          setSnapshotState('unavailable');
          return;
        }
        // The signed /media/v1/<token> URL carries its own auth — no
        // Authorization header needed (the handler doesn't read one).
        const res = await fetch(url);
        if (!res.ok) {
          setSnapshotState('error');
          return;
        }
        const blob = await res.blob();
        if (cancelled) return;
        if (blob && blob.size > 0) {
          setEventFrameUrl(URL.createObjectURL(blob));
          setSnapshotState('ok');
        } else {
          setSnapshotState('unavailable');
        }
      } catch {
        if (!cancelled) setSnapshotState('error');
      }
    })();
    return () => { cancelled = true; };
  }, [eventSnapshotUrl]);

  // Refreshing snapshot for live mode (unused currently — LIVE opens NVR in new tab,
  // but kept so mode='live' shows something if called directly)
  useEffect(() => {
    if (mode !== 'live') return;
    let cancelled = false;
    const fetchFrame = async () => {
      try {
        const res = await fetch(`/api/cameras/${cameraId}/vca/snapshot`, {
          credentials: 'include',
        });
        if (!res.ok || cancelled) return;
        const blob = await res.blob();
        if (cancelled || blob.size === 0) return;
        const url = URL.createObjectURL(blob);
        setLiveFrameUrl(prev => { if (prev) URL.revokeObjectURL(prev); return url; });
        setLiveError(false);
      } catch {
        if (!cancelled) setLiveError(true);
      }
    };
    fetchFrame();
    const timer = setInterval(fetchFrame, 2000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [cameraId, mode]);

  // Zone overlay styles per rule type
  const zoneColors: Record<string, string> = {
    intrusion: 'rgba(239,68,68,0.15)',
    linecross: 'rgba(59,130,246,0.15)',
    regionentrance: 'rgba(34,197,94,0.15)',
    loitering: 'rgba(234,179,8,0.15)',
  };
  const zoneStrokes: Record<string, string> = {
    intrusion: '#EF4444',
    linecross: '#3B82F6',
    regionentrance: '#22C55E',
    loitering: '#EAB308',
  };

  // Highlight the rule that matches the current alarm type
  const matchingType = alarmType?.toLowerCase();

  return (
    <div style={{ position: 'absolute', inset: 0, background: '#0a0c10' }}>

      {/* ── SNAPSHOT MODE: static event frame with zone overlay ── */}
      {mode === 'snapshot' && snapshotState === 'ok' && eventFrameUrl && (
        <img
          src={eventFrameUrl}
          alt="Event snapshot"
          style={{ width: '100%', height: '100%', objectFit: 'fill', display: 'block' }}
        />
      )}
      {mode === 'snapshot' && snapshotState === 'loading' && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 8,
        }}>
          <div style={{ fontSize: 28, opacity: 0.3 }}>◉</div>
          <div style={{ fontSize: 16, color: '#EAB308' }}>Loading event snapshot...</div>
        </div>
      )}
      {mode === 'snapshot' && (snapshotState === 'unavailable' || snapshotState === 'error') && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 10,
        }}>
          <div style={{ fontSize: 36, opacity: 0.2 }}>📷</div>
          <div style={{ fontSize: 15, color: '#4A5268', fontWeight: 600 }}>No snapshot available</div>
          <div style={{ fontSize: 16, color: '#4A5268', textAlign: 'center', maxWidth: 220, lineHeight: 1.5 }}>
            {snapshotState === 'error'
              ? 'Snapshot could not be loaded.'
              : 'No event frame was captured for this alarm.'}
            {' '}Use the Clip or Live NVR buttons above to review footage.
          </div>
        </div>
      )}

      {/* ── CLIP MODE: play recording segment ── */}
      {mode === 'clip' && clipUrl && (
        <video
          key={clipUrl}
          src={clipUrl}
          controls
          autoPlay
          playsInline
          style={{ width: '100%', height: '100%', objectFit: 'fill', display: 'block' }}
        />
      )}
      {mode === 'clip' && !clipUrl && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 8,
        }}>
          <div style={{ fontSize: 28, opacity: 0.3 }}>⏳</div>
          <div style={{ fontSize: 16, color: '#E89B2A' }}>No clip available for this event</div>
          <div style={{ fontSize: 16, color: '#4A5268' }}>Use LIVE NVR to view the camera</div>
        </div>
      )}

      {/* ── LIVE MODE: fallback refreshing snapshot (NVR opens in new tab) ── */}
      {mode === 'live' && liveFrameUrl && (
        <img
          src={liveFrameUrl}
          alt="Live feed"
          style={{ width: '100%', height: '100%', objectFit: 'fill', display: 'block' }}
        />
      )}
      {mode === 'live' && !liveFrameUrl && !liveError && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex',
          alignItems: 'center', justifyContent: 'center', color: '#4A5268', fontSize: 16,
        }}>
          Connecting to camera...
        </div>
      )}
      {mode === 'live' && liveError && (
        <div style={{
          position: 'absolute', inset: 0, display: 'flex',
          alignItems: 'center', justifyContent: 'center', color: '#E89B2A', fontSize: 16,
        }}>
          Camera unavailable
        </div>
      )}

      {/* ── VCA Zone Overlay ── */}
      {vcaRules.length > 0 && (
        <svg style={{
          position: 'absolute', inset: 0, width: '100%', height: '100%',
          pointerEvents: 'none', zIndex: 10,
        }}>
          {vcaRules
            .filter(r => r.enabled && Array.isArray(r.region) && r.region.length >= 2)
            .map(rule => {
              const isActive = rule.rule_type === matchingType;
              const fill = isActive
                ? (zoneColors[rule.rule_type] || 'rgba(255,255,255,0.1)').replace('0.15', '0.35')
                : (zoneColors[rule.rule_type] || 'rgba(255,255,255,0.05)');
              const stroke = zoneStrokes[rule.rule_type] || '#8891A5';
              const strokeWidth = isActive ? '2' : '1.5';
              const opacity = isActive ? '1' : '0.5';

              if (rule.rule_type === 'linecross' && rule.region.length === 2) {
                return (
                  <g key={rule.id} opacity={opacity}>
                    <line
                      x1={`${rule.region[0].x * 100}%`} y1={`${rule.region[0].y * 100}%`}
                      x2={`${rule.region[1].x * 100}%`} y2={`${rule.region[1].y * 100}%`}
                      stroke={stroke} strokeWidth={strokeWidth} strokeDasharray="6 3"
                    />
                    {isActive && (
                      <text
                        x={`${((rule.region[0].x + rule.region[1].x) / 2) * 100}%`}
                        y={`${((rule.region[0].y + rule.region[1].y) / 2) * 100}%`}
                        fill={stroke} fontSize="9" fontWeight="700" textAnchor="middle"
                        dy="-6" style={{ fontFamily: "'JetBrains Mono', monospace" }}
                      >
                        {rule.name}
                      </text>
                    )}
                  </g>
                );
              }

              const pts = rule.region.map(p =>
                `${(p.x * 100).toFixed(1)}%,${(p.y * 100).toFixed(1)}%`
              ).join(' ');
              const cx = (rule.region.reduce((s, p) => s + p.x, 0) / rule.region.length) * 100;
              const cy = (rule.region.reduce((s, p) => s + p.y, 0) / rule.region.length) * 100;

              return (
                <g key={rule.id} opacity={opacity}>
                  <polygon
                    points={pts}
                    fill={fill}
                    stroke={stroke}
                    strokeWidth={strokeWidth}
                    strokeDasharray={isActive ? 'none' : '5 3'}
                  />
                  <text
                    x={`${cx}%`} y={`${cy}%`}
                    fill={stroke} fontSize={isActive ? '11' : '9'}
                    fontWeight={isActive ? '700' : '500'}
                    textAnchor="middle" dominantBaseline="middle"
                    style={{ fontFamily: "'JetBrains Mono', monospace" }}
                  >
                    {rule.name}
                  </text>
                  {isActive && (
                    <text
                      x={`${cx}%`} y={`${cy}%`}
                      fill={stroke} fontSize="8" fontWeight="500"
                      textAnchor="middle" dominantBaseline="middle"
                      dy="14" style={{ fontFamily: "'JetBrains Mono', monospace", opacity: 0.7 }}
                    >
                      ⚠ TRIGGERED
                    </text>
                  )}
                </g>
              );
            })}
        </svg>
      )}

      {/* Bounding boxes removed from child — rendered by parent with toggle */}
    </div>
  );
}
