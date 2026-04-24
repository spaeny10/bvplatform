'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import {
  VCARule, VCARuleCreate, VCAPoint,
  listVCARules, createVCARule, updateVCARule, deleteVCARule,
  syncVCARules, getVCASnapshotURL,
} from '@/lib/api';
import { vcaPullPreview, vcaPullApply, VCAPullResult } from '@/lib/milesight';

// ── Rule type config ──
const RULE_TYPES = [
  { key: 'intrusion',      icon: '🚧', label: 'Intrusion Zone',    color: '#EF4444', fill: 'rgba(239,68,68,0.18)',   minPoints: 3 },
  { key: 'linecross',      icon: '➡️', label: 'Line Crossing',     color: '#3B82F6', fill: 'rgba(59,130,246,0.18)',   minPoints: 2 },
  { key: 'regionentrance',  icon: '🚪', label: 'Region Entrance',  color: '#22C55E', fill: 'rgba(34,197,94,0.18)',    minPoints: 3 },
  { key: 'loitering',      icon: '⏱️', label: 'Loitering Zone',    color: '#EAB308', fill: 'rgba(234,179,8,0.18)',    minPoints: 3 },
] as const;

type RuleTypeKey = typeof RULE_TYPES[number]['key'];
type DrawMode = 'idle' | 'drawing';

const ruleConfig = (type: string) => RULE_TYPES.find(t => t.key === type) || RULE_TYPES[0];

interface Props {
  cameraId: string;
  cameraIp?: string; // for direct link to camera web UI
}

export default function VCAZoneEditor({ cameraId, cameraIp }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const imgRef = useRef<HTMLImageElement | null>(null);

  const [rules, setRules] = useState<VCARule[]>([]);
  const [selectedRuleId, setSelectedRuleId] = useState<string | null>(null);
  const [drawMode, setDrawMode] = useState<DrawMode>('idle');
  const [drawType, setDrawType] = useState<RuleTypeKey>('intrusion');
  const [drawPoints, setDrawPoints] = useState<VCAPoint[]>([]);
  const [drawName, setDrawName] = useState('');
  const [snapshotLoaded, setSnapshotLoaded] = useState(false);
  const [snapshotError, setSnapshotError] = useState<string | null>(null);
  const [syncing, setSyncing] = useState(false);
  const [syncResult, setSyncResult] = useState<string | null>(null);
  const [pullPreview, setPullPreview] = useState<VCAPullResult | null>(null);
  const [pulling, setPulling] = useState(false);
  const [hoveredVertex, setHoveredVertex] = useState<{ ruleIdx: number; ptIdx: number } | null>(null);
  const [draggingVertex, setDraggingVertex] = useState<{ ruleId: string; ptIdx: number } | null>(null);

  // ── Load rules ──
  const loadRules = useCallback(async () => {
    const data = await listVCARules(cameraId);
    setRules(data);
  }, [cameraId]);

  useEffect(() => { loadRules(); }, [loadRules]);

  // ── Load snapshot: try ONVIF endpoint first, then HLS frame grab ──
  const loadSnapshot = useCallback(async () => {
    setSnapshotLoaded(false);
    setSnapshotError(null);
    const token = typeof window !== 'undefined' ? localStorage.getItem('ironsight_token') : '';
    const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {};

    // Attempt 1: ONVIF snapshot endpoint
    try {
      const res = await fetch(getVCASnapshotURL(cameraId), { headers });
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

    // Attempt 2: HLS live stream frame (grab first frame of the sub-stream m3u8)
    try {
      const hlsUrl = `/hls/${cameraId}/sub_live.m3u8`;
      const res = await fetch(hlsUrl, { headers });
      if (res.ok) {
        // If HLS exists, use a video element to grab a frame
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

    // Background
    ctx.clearRect(0, 0, w, h);
    if (imgRef.current) {
      ctx.drawImage(imgRef.current, 0, 0, w, h);
    } else {
      // Dark background with visible grid so zones can still be drawn
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

    // Draw existing rules
    for (const rule of rules) {
      const cfg = ruleConfig(rule.rule_type);
      const pts = rule.region.map(p => ({ x: p.x * w, y: p.y * h }));
      if (pts.length < 2) continue;

      const isSelected = rule.id === selectedRuleId;
      const alpha = rule.enabled ? 1 : 0.35;

      ctx.save();
      ctx.globalAlpha = alpha;

      if (rule.rule_type === 'linecross' && pts.length === 2) {
        // Draw line
        ctx.beginPath();
        ctx.moveTo(pts[0].x, pts[0].y);
        ctx.lineTo(pts[1].x, pts[1].y);
        ctx.strokeStyle = cfg.color;
        ctx.lineWidth = isSelected ? 3 : 2;
        ctx.stroke();

        // Direction arrow at midpoint
        const mx = (pts[0].x + pts[1].x) / 2;
        const my = (pts[0].y + pts[1].y) / 2;
        const angle = Math.atan2(pts[1].y - pts[0].y, pts[1].x - pts[0].x);
        ctx.save();
        ctx.translate(mx, my);
        ctx.rotate(angle);
        ctx.beginPath();
        ctx.moveTo(8, 0);
        ctx.lineTo(-4, -5);
        ctx.lineTo(-4, 5);
        ctx.closePath();
        ctx.fillStyle = cfg.color;
        ctx.fill();
        ctx.restore();
      } else {
        // Draw polygon
        ctx.beginPath();
        ctx.moveTo(pts[0].x, pts[0].y);
        for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i].x, pts[i].y);
        ctx.closePath();
        ctx.fillStyle = cfg.fill;
        ctx.fill();
        ctx.strokeStyle = cfg.color;
        ctx.lineWidth = isSelected ? 2.5 : 1.5;
        if (!rule.enabled) ctx.setLineDash([4, 4]);
        ctx.stroke();
        ctx.setLineDash([]);
      }

      // Vertex handles (when selected)
      if (isSelected) {
        for (let i = 0; i < pts.length; i++) {
          const isHover = hoveredVertex?.ruleIdx === rules.indexOf(rule) && hoveredVertex?.ptIdx === i;
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
      const label = rule.name || cfg.label;
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

    // Draw in-progress shape
    if (drawMode === 'drawing' && drawPoints.length > 0) {
      const cfg = ruleConfig(drawType);
      const pts = drawPoints.map(p => ({ x: p.x * w, y: p.y * h }));

      ctx.beginPath();
      ctx.moveTo(pts[0].x, pts[0].y);
      for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i].x, pts[i].y);

      if (drawType !== 'linecross' && pts.length >= 3) {
        ctx.closePath();
        ctx.fillStyle = cfg.fill;
        ctx.fill();
      }

      ctx.strokeStyle = cfg.color;
      ctx.lineWidth = 2;
      ctx.setLineDash([5, 5]);
      ctx.stroke();
      ctx.setLineDash([]);

      // Vertex dots
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
  }, [rules, selectedRuleId, drawMode, drawType, drawPoints, hoveredVertex]);

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
      if (drawPoints.length >= 3 && drawType !== 'linecross') {
        const first = drawPoints[0];
        const dist = Math.sqrt((pt.x - first.x) ** 2 + (pt.y - first.y) ** 2);
        if (dist < 0.03) {
          finishDrawing();
          return;
        }
      }

      const newPoints = [...drawPoints, pt];

      // Auto-finish line crossing after 2 points
      if (drawType === 'linecross' && newPoints.length === 2) {
        setDrawPoints(newPoints);
        setTimeout(() => finishDrawing(newPoints), 50);
        return;
      }

      setDrawPoints(newPoints);
    } else {
      // Select/deselect rules by clicking
      const pt = getCanvasPoint(e);
      const canvas = canvasRef.current!;
      const w = canvas.width;
      const h = canvas.height;

      for (const rule of [...rules].reverse()) {
        const pts = rule.region.map(p => ({ x: p.x * w, y: p.y * h }));
        if (pointInPolygon(pt.x * w, pt.y * h, pts)) {
          setSelectedRuleId(rule.id);
          return;
        }
      }
      setSelectedRuleId(null);
    }
  };

  const handleCanvasMouseDown = (e: React.MouseEvent) => {
    if (drawMode === 'drawing' || !selectedRuleId) return;
    const pt = getCanvasPoint(e);
    const canvas = canvasRef.current!;
    const rule = rules.find(r => r.id === selectedRuleId);
    if (!rule) return;

    // Check if clicking near a vertex
    for (let i = 0; i < rule.region.length; i++) {
      const vx = rule.region[i].x;
      const vy = rule.region[i].y;
      const dist = Math.sqrt((pt.x - vx) ** 2 + (pt.y - vy) ** 2);
      if (dist < 0.025) {
        setDraggingVertex({ ruleId: rule.id, ptIdx: i });
        return;
      }
    }
  };

  const handleCanvasMouseMove = (e: React.MouseEvent) => {
    if (draggingVertex) {
      const pt = getCanvasPoint(e);
      setRules(prev => prev.map(r => {
        if (r.id !== draggingVertex.ruleId) return r;
        const newRegion = [...r.region];
        newRegion[draggingVertex.ptIdx] = pt;
        return { ...r, region: newRegion };
      }));
      render();
    }
  };

  const handleCanvasMouseUp = async () => {
    if (draggingVertex) {
      const rule = rules.find(r => r.id === draggingVertex.ruleId);
      if (rule) {
        try {
          await updateVCARule(cameraId, rule.id, ruleToCreate(rule));
          autoSync();
        } catch { /* best-effort save */ }
      }
      setDraggingVertex(null);
    }
  };

  const handleDoubleClick = () => {
    if (drawMode === 'drawing' && drawPoints.length >= 3 && drawType !== 'linecross') {
      finishDrawing();
    }
  };

  // ── Finish drawing + save ──
  const finishDrawing = async (pts?: VCAPoint[]) => {
    const region = pts || drawPoints;
    const cfg = ruleConfig(drawType);
    if (region.length < cfg.minPoints) return;

    try {
      await createVCARule(cameraId, {
        rule_type: drawType,
        name: drawName || cfg.label + ' ' + (rules.filter(r => r.rule_type === drawType).length + 1),
        enabled: true,
        sensitivity: 50,
        region,
        direction: 'both',
        threshold_sec: drawType === 'loitering' ? 10 : 0,
        schedule: 'always',
        actions: ['record', 'notify'],
      });
      await loadRules();
      autoSync();
    } catch (err: any) {
      setSyncResult(`Failed to save rule: ${err?.message || err}`);
    }

    setDrawMode('idle');
    setDrawPoints([]);
    setDrawName('');
  };

  // ── Actions ──
  const handleDelete = async (ruleId: string) => {
    await deleteVCARule(cameraId, ruleId);
    setSelectedRuleId(null);
    await loadRules();
    autoSync();
  };

  const handleToggle = async (rule: VCARule) => {
    await updateVCARule(cameraId, rule.id, { ...ruleToCreate(rule), enabled: !rule.enabled });
    await loadRules();
    autoSync();
  };

  const handleSync = async () => {
    setSyncing(true);
    setSyncResult(null);
    try {
      const res = await syncVCARules(cameraId);
      setSyncResult(`✓ Pushed ${res.synced} rule${res.synced !== 1 ? 's' : ''} to camera${res.errors > 0 ? ` (${res.errors} error${res.errors !== 1 ? 's' : ''})` : ''}`);
      loadRules();
    } catch (err: any) {
      setSyncResult(`Sync failed: ${err.message}`);
    } finally {
      setSyncing(false);
    }
  };

  // Pull shows a preview of what's on the camera vs. what we have in DB.
  // Applying overwrites our DB copy — the camera wins. Guarded behind a
  // confirm step so an operator doesn't wipe platform-side edits by accident.
  const handlePullPreview = async () => {
    setPulling(true);
    setSyncResult(null);
    try {
      const preview = await vcaPullPreview(cameraId);
      setPullPreview(preview);
    } catch (err: any) {
      setSyncResult(`Pull failed: ${err.message}`);
    } finally {
      setPulling(false);
    }
  };

  const handlePullApply = async () => {
    if (!pullPreview) return;
    const changes =
      pullPreview.camera_only.length + pullPreview.db_only.length + pullPreview.modified.length;
    if (!window.confirm(`Replace the platform copy of this camera's VCA rules with the camera's current state? (${changes} change${changes === 1 ? '' : 's'})`)) {
      return;
    }
    setPulling(true);
    try {
      await vcaPullApply(cameraId);
      setSyncResult('✓ Pulled camera state into the platform.');
      setPullPreview(null);
      await loadRules();
    } catch (err: any) {
      setSyncResult(`Apply failed: ${err.message}`);
    } finally {
      setPulling(false);
    }
  };

  // Auto-save confirmation — camera VCA must be configured through its web UI
  const autoSync = async () => {
    setSyncResult('✓ Zone saved. Configure matching zones on the camera via its web UI for on-device detection.');
  };

  const ruleToCreate = (r: VCARule): VCARuleCreate => ({
    rule_type: r.rule_type,
    name: r.name,
    enabled: r.enabled,
    sensitivity: r.sensitivity,
    region: r.region,
    direction: r.direction,
    threshold_sec: r.threshold_sec,
    schedule: r.schedule,
    actions: r.actions,
  });

  const selectedRule = rules.find(r => r.id === selectedRuleId);

  // Prevent all clicks inside the editor from bubbling up to the modal overlay
  const stopBubble = (e: React.MouseEvent) => e.stopPropagation();

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }} onClick={stopBubble} onMouseDown={stopBubble}>

      {/* ── Sync toolbar: Push DB→camera / Pull camera→DB ── */}
      <div style={{
        display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap',
        padding: '8px 10px', borderRadius: 6,
        background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)',
      }}>
        <button
          type="button"
          onClick={handleSync}
          disabled={syncing || pulling}
          style={{
            padding: '6px 12px', fontSize: 11, fontWeight: 600, borderRadius: 4,
            background: 'rgba(59,130,246,0.12)', border: '1px solid rgba(59,130,246,0.35)',
            color: '#60a5fa', cursor: syncing ? 'wait' : 'pointer', fontFamily: 'inherit',
          }}
        >
          {syncing ? 'Pushing…' : '↑ Push to camera'}
        </button>
        <button
          type="button"
          onClick={handlePullPreview}
          disabled={syncing || pulling}
          style={{
            padding: '6px 12px', fontSize: 11, fontWeight: 600, borderRadius: 4,
            background: 'rgba(168,85,247,0.12)', border: '1px solid rgba(168,85,247,0.35)',
            color: '#c084fc', cursor: pulling ? 'wait' : 'pointer', fontFamily: 'inherit',
          }}
        >
          {pulling ? 'Pulling…' : '↓ Pull from camera'}
        </button>
        {syncResult && (
          <span style={{
            fontSize: 11, marginLeft: 4,
            color: syncResult.startsWith('✓') ? '#22c55e' : '#ef4444',
          }}>
            {syncResult}
          </span>
        )}
      </div>

      {/* Pull preview — diff the camera state against our DB before the
          operator chooses to overwrite. */}
      {pullPreview && (
        <div style={{
          padding: 12, borderRadius: 6,
          background: 'rgba(168,85,247,0.05)', border: '1px solid rgba(168,85,247,0.25)',
          fontSize: 12,
        }}>
          <div style={{ fontWeight: 600, color: '#c084fc', marginBottom: 6 }}>
            Camera reports {pullPreview.rules.length} rule{pullPreview.rules.length === 1 ? '' : 's'}
          </div>
          <div style={{ color: 'rgba(255,255,255,0.75)', lineHeight: 1.6 }}>
            • New (on camera, not in platform): <strong>{pullPreview.camera_only.length}</strong><br />
            • Will be dropped (in platform, missing from camera): <strong>{pullPreview.db_only.length}</strong><br />
            • Modified (differ between platform and camera): <strong>{pullPreview.modified.length}</strong>
          </div>
          <div style={{ marginTop: 10, display: 'flex', gap: 8 }}>
            <button
              type="button"
              onClick={handlePullApply}
              disabled={pulling}
              style={{
                padding: '6px 14px', fontSize: 11, fontWeight: 700, borderRadius: 4,
                background: '#a855f7', border: 'none', color: 'white',
                cursor: pulling ? 'wait' : 'pointer', fontFamily: 'inherit',
              }}
            >
              Apply — replace platform rules
            </button>
            <button
              type="button"
              onClick={() => setPullPreview(null)}
              style={{
                padding: '6px 12px', fontSize: 11, fontWeight: 600, borderRadius: 4,
                background: 'none', border: '1px solid rgba(255,255,255,0.15)',
                color: 'rgba(255,255,255,0.7)', cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* ── STEP 1: Pick a rule type ── */}
      {drawMode === 'idle' && (
        <div style={{ padding: '10px 12px', borderRadius: 6, background: 'rgba(255,255,255,0.02)', border: '1px solid rgba(255,255,255,0.06)' }}>
          <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268', marginBottom: 8 }}>
            Step 1 — Choose a VCA rule type, then draw on the camera image
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            {RULE_TYPES.map(rt => (
              <button
                key={rt.key}
                type="button"
                onClick={() => { setDrawType(rt.key); setDrawMode('drawing'); setDrawPoints([]); setSelectedRuleId(null); }}
                style={{
                  padding: '8px 14px', borderRadius: 6, fontSize: 11, fontWeight: 600,
                  cursor: 'pointer', fontFamily: 'inherit',
                  background: `${rt.color}10`, border: `1px solid ${rt.color}40`,
                  color: rt.color, display: 'flex', alignItems: 'center', gap: 6,
                }}
              >
                <span style={{ fontSize: 14 }}>{rt.icon}</span>
                <div style={{ textAlign: 'left' }}>
                  <div>{rt.label}</div>
                  <div style={{ fontSize: 8, opacity: 0.7, fontWeight: 400 }}>
                    {rt.key === 'linecross' ? 'Draw a tripwire line (2 points)' :
                     rt.key === 'loitering' ? 'Draw a dwell-time zone' :
                     rt.key === 'regionentrance' ? 'Draw an entry boundary' :
                     'Draw a perimeter polygon'}
                  </div>
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* ── DRAWING MODE: instructions + save/cancel ── */}
      {drawMode === 'drawing' && (() => {
        const cfg = ruleConfig(drawType);
        const canSave = drawType === 'linecross' ? drawPoints.length >= 2 : drawPoints.length >= 3;
        return (
        <div style={{
          padding: '10px 14px', borderRadius: 6,
          background: `${cfg.color}08`,
          border: `1px solid ${cfg.color}30`,
          display: 'flex', flexDirection: 'column', gap: 8,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span style={{ fontSize: 16 }}>{cfg.icon}</span>
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: cfg.color }}>
                Drawing {cfg.label}
              </div>
              <div style={{ fontSize: 10, color: '#8891A5', marginTop: 2 }}>
                {drawType === 'linecross'
                  ? `Click two points on the image below. (${drawPoints.length}/2 placed)`
                  : `Click on the image to place points. (${drawPoints.length} placed, need at least 3)`
                }
              </div>
            </div>
            <input
              value={drawName}
              onChange={e => setDrawName(e.target.value)}
              onClick={e => e.stopPropagation()}
              placeholder="Name (optional)"
              style={{
                padding: '4px 8px', borderRadius: 4, fontSize: 10, width: 120,
                background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
                color: '#E4E8F0', fontFamily: 'inherit',
              }}
            />
          </div>
          {/* Action buttons */}
          <div style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
            <button
              type="button"
              onClick={() => { setDrawMode('idle'); setDrawPoints([]); setDrawName(''); }}
              style={{
                padding: '6px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                background: 'none', border: '1px solid rgba(255,255,255,0.15)',
                color: '#8891A5', cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              Cancel
            </button>
            {drawPoints.length > 0 && (
              <button
                type="button"
                onClick={() => { setDrawPoints(prev => prev.slice(0, -1)); }}
                style={{
                  padding: '6px 16px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                  background: 'none', border: '1px solid rgba(255,255,255,0.15)',
                  color: '#E89B2A', cursor: 'pointer', fontFamily: 'inherit',
                }}
              >
                Undo Last Point
              </button>
            )}
            <button
              type="button"
              disabled={!canSave}
              onClick={() => finishDrawing()}
              style={{
                padding: '6px 20px', borderRadius: 4, fontSize: 11, fontWeight: 700,
                background: canSave ? `${cfg.color}20` : 'rgba(255,255,255,0.02)',
                border: `1px solid ${canSave ? `${cfg.color}60` : 'rgba(255,255,255,0.08)'}`,
                color: canSave ? cfg.color : '#4A5268',
                cursor: canSave ? 'pointer' : 'not-allowed', fontFamily: 'inherit',
              }}
            >
              ✓ Save Zone
            </button>
          </div>
        </div>
        );
      })()}

      {syncResult && (
        <div style={{
          fontSize: 10, padding: '5px 10px', borderRadius: 4,
          background: syncResult.includes('failed') ? 'rgba(239,68,68,0.08)' : 'rgba(34,197,94,0.08)',
          color: syncResult.includes('failed') ? '#EF4444' : '#22C55E',
          border: `1px solid ${syncResult.includes('failed') ? 'rgba(239,68,68,0.2)' : 'rgba(34,197,94,0.2)'}`,
        }}>
          {syncResult}
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
            <button
              type="button"
              onClick={e => { e.stopPropagation(); loadSnapshot(); }}
              style={{
                pointerEvents: 'auto', padding: '4px 12px', borderRadius: 4, fontSize: 10, fontWeight: 600,
                background: 'rgba(232,155,42,0.1)', border: '1px solid rgba(232,155,42,0.3)',
                color: '#E89B2A', cursor: 'pointer', fontFamily: 'inherit',
              }}
            >
              Retry Snapshot
            </button>
          </div>
        )}
      </div>

      {/* Rule list */}
      {rules.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <div style={{ fontSize: 9, fontWeight: 600, letterSpacing: 1.5, textTransform: 'uppercase', color: '#4A5268' }}>
              Rules ({rules.length})
            </div>
            {cameraIp && (
              <a
                href={`http://${cameraIp}`}
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  padding: '3px 10px', borderRadius: 4, fontSize: 9, fontWeight: 700,
                  textDecoration: 'none',
                  background: 'rgba(59,130,246,0.12)', border: '1px solid rgba(59,130,246,0.3)',
                  color: '#3B82F6',
                }}
              >
                🔗 Open Camera VCA Settings
              </a>
            )}
          </div>
          {rules.map(rule => {
            const cfg = ruleConfig(rule.rule_type);
            const isSelected = rule.id === selectedRuleId;
            return (
              <div
                key={rule.id}
                onClick={() => setSelectedRuleId(isSelected ? null : rule.id)}
                style={{
                  display: 'flex', alignItems: 'center', gap: 8,
                  padding: '6px 10px', borderRadius: 4, cursor: 'pointer',
                  background: isSelected ? `${cfg.color}12` : 'rgba(255,255,255,0.02)',
                  border: `1px solid ${isSelected ? `${cfg.color}40` : 'rgba(255,255,255,0.04)'}`,
                  opacity: rule.enabled ? 1 : 0.5,
                }}
              >
                <span style={{ fontSize: 12 }}>{cfg.icon}</span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: 11, fontWeight: 600, color: '#E4E8F0' }}>{rule.name || cfg.label}</div>
                  <div style={{ fontSize: 9, color: '#4A5268' }}>
                    {cfg.label} · {rule.region.length} points · Sensitivity {rule.sensitivity}%
                    {rule.synced ? ' · ✓ Synced' : rule.sync_error ? ` · ⚠ ${rule.sync_error.slice(0, 30)}` : ' · Not synced'}
                  </div>
                </div>
                <button
                  type="button"
                  onClick={e => { e.stopPropagation(); handleToggle(rule); }}
                  style={{
                    fontSize: 8, fontWeight: 700, padding: '2px 6px', borderRadius: 3,
                    background: rule.enabled ? 'rgba(34,197,94,0.1)' : 'rgba(255,255,255,0.04)',
                    border: `1px solid ${rule.enabled ? 'rgba(34,197,94,0.3)' : 'rgba(255,255,255,0.08)'}`,
                    color: rule.enabled ? '#22C55E' : '#8891A5',
                    cursor: 'pointer', fontFamily: 'inherit',
                  }}
                >
                  {rule.enabled ? 'ON' : 'OFF'}
                </button>
                <button
                  type="button"
                  onClick={e => { e.stopPropagation(); handleDelete(rule.id); }}
                  style={{ background: 'none', border: 'none', cursor: 'pointer', color: '#EF4444', opacity: 0.5, fontSize: 11 }}
                >
                  🗑
                </button>
              </div>
            );
          })}
        </div>
      )}

      {rules.length === 0 && drawMode === 'idle' && (
        <div style={{ textAlign: 'center', padding: 16, color: '#4A5268', fontSize: 11 }}>
          No VCA rules configured. Select a rule type above, then draw on the camera snapshot.
        </div>
      )}
    </div>
  );
}

// ── Hit testing helper ──
function pointInPolygon(x: number, y: number, polygon: { x: number; y: number }[]): boolean {
  let inside = false;
  for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
    const xi = polygon[i].x, yi = polygon[i].y;
    const xj = polygon[j].x, yj = polygon[j].y;
    const intersect = yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi;
    if (intersect) inside = !inside;
  }
  return inside;
}
