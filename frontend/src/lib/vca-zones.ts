// VCA zone pure data + helpers extracted from components/VCAZoneEditor.tsx
// (P1-B-11 session 19). RULE_TYPES is the catalog of VCA rule kinds we
// support drawing — intrusion / linecross / regionentrance / loitering —
// each with its color, fill, icon, label, and the minimum-point count
// the geometry needs to be valid. `pointInPolygon` is the standard
// ray-casting hit test used by the canvas click handler. Pure helpers
// in lib/ so they're reusable + unit-testable in isolation.

// PPE_ZONE_TYPES: catalog of server-side safety zones drawn in PPEZoneEditor.
// Color palette is intentionally distinct from RULE_TYPES to avoid operator
// confusion between camera-pushed VCA zones and Ironsight-side PPE zones.
// These types are NEVER pushed to camera firmware.
export const PPE_ZONE_TYPES = [
    { key: 'work_area',    label: 'Work Area',    color: '#F59E0B', fill: 'rgba(245,158,11,0.15)', minPoints: 3 },
    { key: 'no_go',        label: 'No-Go Area',   color: '#EF4444', fill: 'rgba(239,68,68,0.18)',   minPoints: 3 },
    { key: 'ppe_required', label: 'PPE Required',  color: '#22C55E', fill: 'rgba(34,197,94,0.15)',   minPoints: 3 },
    { key: 'ppe_optional', label: 'PPE Optional',  color: '#6366F1', fill: 'rgba(99,102,241,0.12)',  minPoints: 3 },
] as const;

export type PPEZoneTypeKey = typeof PPE_ZONE_TYPES[number]['key'];

export const ppeZoneConfig = (type: string) =>
    PPE_ZONE_TYPES.find(t => t.key === type) || PPE_ZONE_TYPES[0];

export const RULE_TYPES = [
    { key: 'intrusion',      icon: '🚧', label: 'Intrusion Zone',    color: '#EF4444', fill: 'rgba(239,68,68,0.18)',   minPoints: 3 },
    { key: 'linecross',      icon: '➡️', label: 'Line Crossing',     color: '#3B82F6', fill: 'rgba(59,130,246,0.18)',   minPoints: 2 },
    { key: 'regionentrance', icon: '🚪', label: 'Region Entrance',   color: '#22C55E', fill: 'rgba(34,197,94,0.18)',    minPoints: 3 },
    { key: 'loitering',      icon: '⏱️', label: 'Loitering Zone',    color: '#EAB308', fill: 'rgba(234,179,8,0.18)',    minPoints: 3 },
] as const;

export type RuleTypeKey = typeof RULE_TYPES[number]['key'];
export type DrawMode = 'idle' | 'drawing';

export const ruleConfig = (type: string) =>
    RULE_TYPES.find(t => t.key === type) || RULE_TYPES[0];

/** Ray-casting point-in-polygon test. Returns true when (x, y) lies
 *  inside the polygon described by `polygon` (an array of {x,y} points,
 *  any winding order). Used by the canvas click handler to figure out
 *  which existing zone the user clicked. */
export function pointInPolygon(
    x: number, y: number,
    polygon: { x: number; y: number }[],
): boolean {
    let inside = false;
    for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
        const xi = polygon[i].x, yi = polygon[i].y;
        const xj = polygon[j].x, yj = polygon[j].y;
        const intersect = yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi;
        if (intersect) inside = !inside;
    }
    return inside;
}
