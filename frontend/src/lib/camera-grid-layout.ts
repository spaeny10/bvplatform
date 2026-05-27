// Camera-grid layout types + persistence + conversion helpers.
// Extracted from components/CameraGrid.tsx (P1-B-11 session 5). Pure
// data utilities live in lib/ where they can be reused (e.g. by future
// layout-import/export tooling or by tests).

import type { Camera } from '@/lib/api';

// ── Types ──

export interface LayoutItem {
    i: string;       // camera ID
    x: number;       // gridColStart (0-indexed base)
    y: number;       // gridRowStart (0-indexed base)
    w: number;       // colSpan
    h: number;       // rowSpan
    cameraId?: string;
}

export interface SavedLayout {
    name: string;
    items: LayoutItem[];
    cols: number;
    version?: number;
    mode?: 'static' | 'freeform';
    staticPreset?: { w: number; h: number };
    staticAssignments?: Record<number, string>; // slot index → camera ID
}

// ── Constants ──

export const ROW_HEIGHT_UNIT = 30;
export const GRID_COLS = 32;

export const STATIC_PRESETS = [
    { w: 1, h: 1, label: '1×1' },
    { w: 2, h: 1, label: '2×1' },
    { w: 2, h: 2, label: '2×2' },
    { w: 3, h: 2, label: '3×2' },
    { w: 3, h: 3, label: '3×3' },
    { w: 4, h: 3, label: '4×3' },
    { w: 4, h: 4, label: '4×4' },
    { w: 5, h: 3, label: '5×3' },
    { w: 6, h: 4, label: '6×4' },
];

// LOCAL-06: localStorage key namespace migration. P1-B-07 renamed the
// Go module from `onvif-tool` → `ironsight`; the frontend's localStorage
// keys still carried the old prefix, so any operator with saved camera
// layouts had them under `onvif-tool-*`. Renaming the keys outright
// would orphan every existing user's saved layouts. Instead: on first
// run after deploy, copy old → new, then delete the old key. After
// every user's session has rotated once, the migration is a no-op and
// the shim could in principle be removed.
export const STORAGE_KEY = 'ironsight-layouts';
export const ACTIVE_LAYOUT_KEY = 'ironsight-active-layout';
const LEGACY_STORAGE_KEY = 'onvif-tool-layouts';
const LEGACY_ACTIVE_LAYOUT_KEY = 'onvif-tool-active-layout';

// migrateLegacyLayoutKeys runs once at module load (idempotent — second
// call is a no-op because the legacy keys are already gone). Wrapped in
// try/catch because some browser contexts (Safari private mode, embedded
// webview without storage permission) throw on every localStorage access
// and a thrown error here would prevent the rest of the file from
// initialising.
export function migrateLegacyLayoutKeys() {
    if (typeof window === 'undefined') return; // Next.js SSR pass — no localStorage
    try {
        const oldLayouts = localStorage.getItem(LEGACY_STORAGE_KEY);
        if (oldLayouts !== null && localStorage.getItem(STORAGE_KEY) === null) {
            localStorage.setItem(STORAGE_KEY, oldLayouts);
            localStorage.removeItem(LEGACY_STORAGE_KEY);
        }
        const oldActive = localStorage.getItem(LEGACY_ACTIVE_LAYOUT_KEY);
        if (oldActive !== null && localStorage.getItem(ACTIVE_LAYOUT_KEY) === null) {
            localStorage.setItem(ACTIVE_LAYOUT_KEY, oldActive);
            localStorage.removeItem(LEGACY_ACTIVE_LAYOUT_KEY);
        }
    } catch {
        // best-effort — if storage is unavailable the user gets a clean
        // empty layout list, which is recoverable.
    }
}

export function loadSavedLayouts(): SavedLayout[] {
    try {
        const raw = localStorage.getItem(STORAGE_KEY);
        if (!raw) return [];
        const layouts = JSON.parse(raw) as any[];
        return layouts.map(l => {
            if (!l.version || l.version < 3) {
                l.items = l.items.map((item: LayoutItem) => ({ ...item }));
                l.version = 3;
            }
            if (!l.mode) l.mode = 'freeform';
            return l;
        });
    } catch {
        return [];
    }
}

export function saveSavedLayouts(layouts: SavedLayout[]) {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layouts));
}

// ── Layout generation + conversions ──

export function generateDefaultLayout(cameras: Camera[], viewCols: number, hUnits: number): LayoutItem[] {
    return cameras.map((cam, i) => ({
        i: cam.id,
        x: (i % viewCols) * Math.floor(GRID_COLS / viewCols),
        y: Math.floor(i / viewCols) * hUnits,
        w: Math.floor(GRID_COLS / viewCols),
        h: hUnits,
        cameraId: cam.id,
    }));
}

// Convert static assignments → freeform layout items
export function staticToFreeform(
    assignments: Record<number, string>,
    preset: { w: number; h: number },
    _cameras: Camera[],
): LayoutItem[] {
    const colSpan = Math.floor(GRID_COLS / preset.w);
    const rowSpan = Math.max(4, Math.floor(18 / preset.h));
    const items: LayoutItem[] = [];
    for (const [slotStr, camId] of Object.entries(assignments)) {
        const slot = Number(slotStr);
        const col = slot % preset.w;
        const row = Math.floor(slot / preset.w);
        items.push({
            i: camId,
            x: col * colSpan,
            y: row * rowSpan,
            w: colSpan,
            h: rowSpan,
            cameraId: camId,
        });
    }
    return items;
}

// Convert freeform layout items → static assignments (best-fit preset)
export function freeformToStatic(
    items: LayoutItem[],
    cameras: Camera[],
): { preset: { w: number; h: number }; assignments: Record<number, string> } {
    const cameraItems = items.filter(it => it.cameraId && cameras.some(c => c.id === it.cameraId));
    const count = cameraItems.length;
    // Find the smallest preset that fits all cameras
    const preset = STATIC_PRESETS.find(p => p.w * p.h >= count)
        || STATIC_PRESETS[STATIC_PRESETS.length - 1];
    // Sort items by position (top-left first) and assign to slots in order
    const sorted = [...cameraItems].sort((a, b) => a.y !== b.y ? a.y - b.y : a.x - b.x);
    const assignments: Record<number, string> = {};
    sorted.forEach((item, i) => {
        if (i < preset.w * preset.h && item.cameraId) {
            assignments[i] = item.cameraId;
        }
    });
    return { preset, assignments };
}
