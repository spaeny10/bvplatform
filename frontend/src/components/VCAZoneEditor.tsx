'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import {
  VCARule, VCARuleCreate, VCAPoint,
  listVCARules, createVCARule, updateVCARule, deleteVCARule,
} from '@/lib/api';
import { RULE_TYPES, ruleConfig, RuleTypeKey, DrawMode } from '@/lib/vca-zones';
import { useVCASnapshot } from '@/hooks/useVCASnapshot';
import { useVCACanvas, CanvasPoint } from '@/hooks/useVCACanvas';

interface Props {
  cameraId: string;
  cameraIp?: string; // for direct link to camera web UI
}

export default function VCAZoneEditor({ cameraId, cameraIp }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  const [rules, setRules] = useState<VCARule[]>([]);
  const [selectedRuleId, setSelectedRuleId] = useState<string | null>(null);
  const [drawMode, setDrawMode] = useState<DrawMode>('idle');
  const [drawType, setDrawType] = useState<RuleTypeKey>('intrusion');
  const [drawPoints, setDrawPoints] = useState<CanvasPoint[]>([]);
  const [drawName, setDrawName] = useState('');
  const [syncResult, setSyncResult] = useState<string | null>(null);

  // ── Snapshot ──
  const { imgRef, snapshotLoaded, snapshotError, reload: loadSnapshot } = useVCASnapshot(cameraId);

  // ── Load rules ──
  const loadRules = useCallback(async () => {
    const data = await listVCARules(cameraId);
    setRules(data);
  }, [cameraId]);

  useEffect(() => { loadRules(); }, [loadRules]);

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

  // Auto-save confirmation — camera VCA must be configured through its web UI
  const autoSync = () => {
    setSyncResult('✓ Zone saved. Configure matching zones on the camera via its web UI for on-device detection.');
  };

  // ── Finish drawing + save ──
  const finishDrawing = useCallback(async (pts: CanvasPoint[]) => {
    const cfg = ruleConfig(drawType);
    if (pts.length < cfg.minPoints) return;

    try {
      await createVCARule(cameraId, {
        rule_type: drawType,
        name: drawName || cfg.label + ' ' + (rules.filter(r => r.rule_type === drawType).length + 1),
        enabled: true,
        sensitivity: 50,
        region: pts as VCAPoint[],
        direction: 'both',
        threshold_sec: drawType === 'loitering' ? 10 : 0,
        schedule: 'always',
        actions: ['record', 'notify'],
      });
      await loadRules();
      autoSync();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setSyncResult(`Failed to save rule: ${msg}`);
    }

    setDrawMode('idle');
    setDrawPoints([]);
    setDrawName('');
  }, [cameraId, drawType, drawName, rules, loadRules]);

  // ── Canvas hook ──
  const { isDragging, onClick, onMouseDown, onMouseMove, onMouseUp, onDoubleClick } = useVCACanvas({
    canvasRef,
    imgRef,
    ruleTypes: RULE_TYPES,
    zones: rules,
    drawMode,
    activeRuleType: drawType,
    draftPoints: drawPoints,
    selectedZoneId: selectedRuleId,
    onDraftPointAdd: (pt) => setDrawPoints(prev => [...prev, pt]),
    onPolygonComplete: (pts) => {
      // finishDrawing resets drawMode + drawPoints itself
      finishDrawing(pts);
    },
    onZoneSelect: (id) => setSelectedRuleId(id),
    onVertexMove: (zoneId, ptIdx, pt) => {
      setRules(prev => prev.map(r => {
        if (r.id !== zoneId) return r;
        const newRegion = [...r.region];
        newRegion[ptIdx] = pt as VCAPoint;
        return { ...r, region: newRegion };
      }));
    },
    onVertexDragEnd: async (zoneId) => {
      const rule = rules.find(r => r.id === zoneId);
      if (rule) {
        try {
          await updateVCARule(cameraId, rule.id, ruleToCreate(rule));
          autoSync();
        } catch { /* best-effort save */ }
      }
    },
  });

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

  // Prevent all clicks inside the editor from bubbling up to the modal overlay
  const stopBubble = (e: React.MouseEvent) => e.stopPropagation();

  // Tab toggle: zones drawn here are PLATFORM-SIDE (used by server-side
  // AI). Camera-side VCA is configured in the camera's own web UI which
  // we embed below. The pull/push sync attempt was unreliable because
  // coordinate-space + slot-mapping mismatches lost fidelity on every
  // round-trip; the iframe approach is the camera's UI is the source of
  // truth for camera-side rules.
  const [vcaTab, setVcaTab] = useState<'platform' | 'camera'>('platform');
  // Camera-side VCA is configured in the camera's OWN web UI. We OPEN IT
  // IN A NEW TAB at the camera's reachable address (onvif_address host:port,
  // e.g. 504.bigview.ai:8080) rather than embedding it in an iframe. The
  // firmware is a JavaScript SPA that navigates to absolute paths ("/");
  // inside a same-origin iframe that "/" resolves to the Ironsight origin,
  // so the iframe ended up loading the Ironsight app itself. At its own
  // origin in a new tab the SPA works correctly, and a top-level navigation
  // has no mixed-content / X-Frame-Options problem (both of which only
  // affect framing). internal/api/camera_web_proxy.go remains in the
  // backend for a possible future separate-origin embed but is no longer
  // used by this component.
  const cameraDirectURL = cameraIp ? `http://${cameraIp}/` : null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }} onClick={stopBubble} onMouseDown={stopBubble}>

      {/* Tabs: platform-side zones vs camera-side VCA (embedded camera UI). */}
      <div style={{ display: 'flex', gap: 4, borderBottom: '1px solid rgba(255,255,255,0.08)' }}>
        <button
          type="button"
          onClick={() => setVcaTab('platform')}
          style={{
            padding: '8px 14px', fontSize: 12, fontWeight: 600,
            background: 'transparent', border: 'none', cursor: 'pointer', fontFamily: 'inherit',
            color: vcaTab === 'platform' ? '#a855f7' : 'rgba(255,255,255,0.6)',
            borderBottom: vcaTab === 'platform' ? '2px solid #a855f7' : '2px solid transparent',
            marginBottom: -1,
          }}
        >
          Platform zones (for AI detection)
        </button>
        <button
          type="button"
          onClick={() => setVcaTab('camera')}
          style={{
            padding: '8px 14px', fontSize: 12, fontWeight: 600,
            background: 'transparent', border: 'none', cursor: 'pointer', fontFamily: 'inherit',
            color: vcaTab === 'camera' ? '#a855f7' : 'rgba(255,255,255,0.6)',
            borderBottom: vcaTab === 'camera' ? '2px solid #a855f7' : '2px solid transparent',
            marginBottom: -1,
          }}
        >
          Camera VCA (on-device)
        </button>
      </div>

      {/* ───────────────────────── Camera tab ───────────────────────── */}
      {vcaTab === 'camera' && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{
            padding: '8px 12px', borderRadius: 4, fontSize: 11, lineHeight: 1.5,
            background: 'rgba(168,85,247,0.08)', border: '1px solid rgba(168,85,247,0.25)',
            color: 'rgba(255,255,255,0.8)',
          }}>
            Camera-side VCA (intrusion, line-cross, loitering, etc.) is configured in the camera's
            <strong> own web UI</strong>. Open it in a new tab below, then go to
            <strong> Settings → Event / VCA</strong>. The camera runs detection in its DSP and emits ONVIF
            events that Ironsight ingests automatically — no separate sync step.
            {!cameraDirectURL && (
              <> <em>This camera has no reachable web address configured (set its ONVIF address in NVR Settings).</em></>
            )}
          </div>
          {cameraDirectURL && (
            <>
              <a
                href={cameraDirectURL}
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  alignSelf: 'flex-start', display: 'inline-flex', alignItems: 'center', gap: 6,
                  padding: '8px 16px', borderRadius: 4, fontSize: 13, fontWeight: 600,
                  background: '#a855f7', color: '#fff', textDecoration: 'none', cursor: 'pointer',
                }}
              >
                Open camera web UI ↗
              </a>
              <div style={{ fontSize: 10, color: 'rgba(255,255,255,0.45)', lineHeight: 1.4 }}>
                Opens <code>{cameraDirectURL}</code> in a new tab. The camera serves its UI over HTTP;
                authenticate with the camera's own credentials. (Embedding it here isn't possible — the
                firmware is a single-page app that escapes a same-origin frame.)
              </div>
            </>
          )}
        </div>
      )}

      {/* Everything below this point is the PLATFORM-side zone editor —
          drawn zones are stored in the api DB and used by server-side
          AI (intrusion/PPE detection). They are NOT pushed to the
          camera. Camera-side VCA lives in the "Camera VCA" tab above. */}
      {vcaTab === 'platform' && <>

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
              onClick={() => finishDrawing(drawPoints)}
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
          onClick={onClick}
          onDoubleClick={onDoubleClick}
          onMouseDown={onMouseDown}
          onMouseMove={onMouseMove}
          onMouseUp={onMouseUp}
          style={{
            width: '100%', display: 'block',
            cursor: drawMode === 'drawing' ? 'crosshair' : isDragging ? 'grabbing' : 'default',
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

      </>}
    </div>
  );
}
