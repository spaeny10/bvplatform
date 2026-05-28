'use client';

// useVCACanvas — canvas render + polygon interaction hook (P1-B-11 session 20).
//
// Extracted from VCAZoneEditor.tsx to avoid duplicating ~400 lines of canvas
// logic in the upcoming PPEZoneEditor.tsx (P2-C-04). Owns:
//
//   1. The draw loop (useEffect → ctx draw calls). Re-runs whenever zones,
//      draftPoints, drawMode, imgRef.current, or selectedZoneId changes.
//
//   2. The four mouse handlers (onClick / onMouseDown / onMouseMove / onMouseUp)
//      that handle:
//        - click-to-select zone (idle mode, ray-cast hit test)
//        - add vertex (drawing mode)
//        - vertex drag start/move/end (idle mode, selected zone)
//      Vertex drag state (hoveredVertex, draggingVertex) lives HERE — it is
//      purely ephemeral canvas interaction state, not persisted zone data.
//
// Callbacks into the parent:
//   - onPolygonComplete(pts)   — parent saves the new zone (calls the API)
//   - onZoneSelect(id | null)  — parent tracks which zone row is highlighted
//   - onVertexMove(id, idx, pt) — parent applies in-flight drag updates to its
//                                  own zones array so the draw loop stays live
//   - onVertexDragEnd(id)      — parent persists the moved vertex (calls the API)
//
// The hook is intentionally generic:
//   - `zones` is ZoneLike[] — works for VCARule[] AND future PPEZone[]
//   - `ruleTypes` is readonly RuleTypeDescriptor[] — works for RULE_TYPES AND
//     PPE_ZONE_TYPES from vca-zones.ts
//
// Coordinate system note:
//   - Canvas pixel coords: px = normalized_float × canvas.width/height
//   - Normalized coords: float 0-1 on each axis (what we store in the DB)
//   - This hook always converts to normalized before firing callbacks, and
//     converts back to px when drawing.

import { useCallback, useEffect, useRef, useState } from 'react';
import { pointInPolygon } from '@/lib/vca-zones';

// ── Shared generic types ─────────────────────────────────────────────────────

/** Normalized canvas point — matches VCAPoint from api.ts. */
export interface CanvasPoint { x: number; y: number; }

/** Minimal descriptor each rule/zone catalog entry must satisfy.
 *  RULE_TYPES and PPE_ZONE_TYPES both satisfy this shape. */
export interface RuleTypeDescriptor {
    key: string;
    label: string;
    color: string;
    fill: string;
    minPoints: number;
    icon?: string;
}

/** Minimal zone shape the hook needs to draw and hit-test.
 *  VCARule and PPEZone both satisfy this shape. */
export interface ZoneLike {
    id: string;
    rule_type: string;  // maps to key in ruleTypes
    name: string;
    enabled: boolean;
    region: CanvasPoint[];
}

export type DrawMode = 'idle' | 'drawing';

// ── Hook interface ────────────────────────────────────────────────────────────

export interface UseVCACanvasOptions {
    canvasRef: React.RefObject<HTMLCanvasElement | null>;
    imgRef: React.RefObject<HTMLImageElement | null>;
    /** Rule/zone type catalog. Pass RULE_TYPES for VCA, PPE_ZONE_TYPES for PPE. */
    ruleTypes: readonly RuleTypeDescriptor[];
    zones: ZoneLike[];
    drawMode: DrawMode;
    activeRuleType: string;
    draftPoints: CanvasPoint[];
    selectedZoneId: string | null;
    /** Parent appends the incoming point to its draftPoints array. */
    onDraftPointAdd: (pt: CanvasPoint) => void;
    /** Called when the polygon is finished (enough vertices). Parent saves. */
    onPolygonComplete: (pts: CanvasPoint[]) => void;
    /** Called on click-to-select / deselect. */
    onZoneSelect: (id: string | null) => void;
    /** Called every mousemove during a vertex drag — lets parent update its
     *  zones state so the draw loop reflects the live cursor position. */
    onVertexMove: (zoneId: string, ptIdx: number, pt: CanvasPoint) => void;
    /** Called on mouseup after a vertex drag — parent should persist the update. */
    onVertexDragEnd: (zoneId: string) => void;
}

export interface UseVCACanvasResult {
    /** Whether a vertex drag is currently active (drives cursor style). */
    isDragging: boolean;
    onClick: (e: React.MouseEvent<HTMLCanvasElement>) => void;
    onMouseDown: (e: React.MouseEvent<HTMLCanvasElement>) => void;
    onMouseMove: (e: React.MouseEvent<HTMLCanvasElement>) => void;
    onMouseUp: () => void;
    onDoubleClick: () => void;
}

// ── Implementation ────────────────────────────────────────────────────────────

export function useVCACanvas(opts: UseVCACanvasOptions): UseVCACanvasResult {
    const {
        canvasRef, imgRef,
        ruleTypes, zones,
        drawMode, activeRuleType, draftPoints, selectedZoneId,
        onDraftPointAdd, onPolygonComplete, onZoneSelect,
        onVertexMove, onVertexDragEnd,
    } = opts;

    // Ephemeral interaction state — never leaves this hook.
    const [hoveredVertex, setHoveredVertex] = useState<{ ruleIdx: number; ptIdx: number } | null>(null);
    const draggingVertexRef = useRef<{ zoneId: string; ptIdx: number } | null>(null);
    // Expose a boolean state version so the parent can drive cursor style.
    const [isDragging, setIsDragging] = useState(false);

    // ── Helpers ──────────────────────────────────────────────────────────────

    const ruleConfig = useCallback((type: string): RuleTypeDescriptor => {
        return ruleTypes.find(t => t.key === type) ?? ruleTypes[0];
    }, [ruleTypes]);

    /** Convert a React mouse event to a normalized canvas point (0-1, 0-1). */
    const toNormalized = useCallback((e: React.MouseEvent<HTMLCanvasElement>): CanvasPoint => {
        const canvas = canvasRef.current;
        if (!canvas) return { x: 0, y: 0 };
        const rect = canvas.getBoundingClientRect();
        return {
            x: Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)),
            y: Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height)),
        };
    }, [canvasRef]);

    // ── Draw loop ─────────────────────────────────────────────────────────────

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
            // Dark grid background — zones can still be drawn without a snapshot.
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
            const cfg = ruleConfig(zone.rule_type);
            const pts = zone.region.map(p => ({ x: p.x * w, y: p.y * h }));
            if (pts.length < 2) continue;

            const isSelected = zone.id === selectedZoneId;
            const alpha = zone.enabled ? 1 : 0.35;
            const isLineCross = zone.rule_type === 'linecross';

            ctx.save();
            ctx.globalAlpha = alpha;

            if (isLineCross && pts.length === 2) {
                // Draw directional line
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
                // Draw closed polygon
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
            }

            // Vertex handles (only when selected)
            if (isSelected) {
                const zoneIdx = zones.indexOf(zone);
                for (let i = 0; i < pts.length; i++) {
                    const isHover = hoveredVertex?.ruleIdx === zoneIdx && hoveredVertex?.ptIdx === i;
                    ctx.beginPath();
                    ctx.arc(pts[i].x, pts[i].y, isHover ? 7 : 5, 0, Math.PI * 2);
                    ctx.fillStyle = '#fff';
                    ctx.fill();
                    ctx.strokeStyle = cfg.color;
                    ctx.lineWidth = 2;
                    ctx.stroke();
                }
            }

            // Centroid label
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
        if (drawMode === 'drawing' && draftPoints.length > 0) {
            const cfg = ruleConfig(activeRuleType);
            const pts = draftPoints.map(p => ({ x: p.x * w, y: p.y * h }));
            const isLineCross = activeRuleType === 'linecross';

            ctx.beginPath();
            ctx.moveTo(pts[0].x, pts[0].y);
            for (let i = 1; i < pts.length; i++) ctx.lineTo(pts[i].x, pts[i].y);

            if (!isLineCross && pts.length >= 3) {
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
    }, [
        canvasRef, imgRef, ruleConfig, zones, selectedZoneId,
        drawMode, activeRuleType, draftPoints, hoveredVertex,
    ]);

    // Re-render whenever anything the draw loop depends on changes.
    useEffect(() => { render(); }, [render]);

    // ── Mouse handlers ────────────────────────────────────────────────────────

    const onClick = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
        // Don't fire a click if the user just finished dragging a vertex.
        if (draggingVertexRef.current) return;

        const pt = toNormalized(e);

        if (drawMode === 'drawing') {
            const cfg = ruleConfig(activeRuleType);
            const isLineCross = activeRuleType === 'linecross';

            // Close polygon: click near the first point (≥3 pts already placed)
            if (draftPoints.length >= 3 && !isLineCross) {
                const first = draftPoints[0];
                const dist = Math.sqrt((pt.x - first.x) ** 2 + (pt.y - first.y) ** 2);
                if (dist < 0.03) {
                    onPolygonComplete([...draftPoints]);
                    return;
                }
            }

            const newPoints = [...draftPoints, pt];

            // Auto-finish line crossing after 2 points
            if (isLineCross && newPoints.length >= cfg.minPoints) {
                onPolygonComplete(newPoints);
                return;
            }

            onDraftPointAdd(pt);
        } else {
            // Idle mode: hit-test existing zones (iterate in reverse so top-most wins)
            const canvas = canvasRef.current;
            if (!canvas) return;
            const w = canvas.width;
            const h = canvas.height;

            for (const zone of [...zones].reverse()) {
                const pts = zone.region.map(p => ({ x: p.x * w, y: p.y * h }));
                if (pointInPolygon(pt.x * w, pt.y * h, pts)) {
                    onZoneSelect(zone.id);
                    return;
                }
            }
            onZoneSelect(null);
        }
    }, [
        drawMode, activeRuleType, draftPoints,
        ruleConfig, toNormalized, canvasRef, zones,
        onDraftPointAdd, onPolygonComplete, onZoneSelect,
    ]);

    const onMouseDown = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
        // Only handle vertex grabs in idle mode when a zone is selected.
        if (drawMode === 'drawing' || !selectedZoneId) return;

        const pt = toNormalized(e);
        const zone = zones.find(z => z.id === selectedZoneId);
        if (!zone) return;

        for (let i = 0; i < zone.region.length; i++) {
            const vx = zone.region[i].x;
            const vy = zone.region[i].y;
            const dist = Math.sqrt((pt.x - vx) ** 2 + (pt.y - vy) ** 2);
            if (dist < 0.025) {
                draggingVertexRef.current = { zoneId: zone.id, ptIdx: i };
                setIsDragging(true);
                return;
            }
        }
    }, [drawMode, selectedZoneId, zones, toNormalized]);

    const onMouseMove = useCallback((e: React.MouseEvent<HTMLCanvasElement>) => {
        const dv = draggingVertexRef.current;
        if (dv) {
            const pt = toNormalized(e);
            onVertexMove(dv.zoneId, dv.ptIdx, pt);
            // Trigger a redraw via render() — zones state update in parent will
            // cause a re-render, but we also call render() directly to get smooth
            // 60fps drag without waiting for the React reconcile cycle.
            render();
        }
    }, [toNormalized, onVertexMove, render]);

    const onMouseUp = useCallback(() => {
        const dv = draggingVertexRef.current;
        if (dv) {
            onVertexDragEnd(dv.zoneId);
            draggingVertexRef.current = null;
            setIsDragging(false);
        }
    }, [onVertexDragEnd]);

    const onDoubleClick = useCallback(() => {
        const isLineCross = activeRuleType === 'linecross';
        if (drawMode === 'drawing' && draftPoints.length >= 3 && !isLineCross) {
            onPolygonComplete([...draftPoints]);
        }
    }, [drawMode, activeRuleType, draftPoints, onPolygonComplete]);

    return { isDragging, onClick, onMouseDown, onMouseMove, onMouseUp, onDoubleClick };
}
