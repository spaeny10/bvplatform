'use client';

import { Camera } from '@/lib/api';
import VideoPlayer from './VideoPlayer';
import { useState, useEffect, useRef, useCallback } from 'react';

// Saved layout type
interface SavedLayout {
    name: string;
    items: LayoutItem[];
    cols: number;
    version?: number;
    mode?: 'static' | 'freeform';
    staticPreset?: { w: number; h: number };
    staticAssignments?: Record<number, string>; // slot index → camera ID
}

interface LayoutItem {
    i: string;       // camera ID
    x: number;       // gridColStart (0-indexed base)
    y: number;       // gridRowStart (0-indexed base)
    w: number;       // colSpan
    h: number;       // rowSpan
    cameraId?: string;
}

interface CameraGridProps {
    cameras: Camera[];
    selectedCamera: string | null;
    isLive: boolean;
    playbackTime: Date;
    onSelectCamera: (id: string) => void;
    onPeekCamera?: (id: string) => void;
    syncPlayback?: boolean;
    scrubbing?: boolean;
    isAdmin?: boolean;
    onRenameCamera?: (cameraId: string, newName: string) => void;
    globalPaused?: boolean;
}

const STORAGE_KEY = 'onvif-tool-layouts';
const ACTIVE_LAYOUT_KEY = 'onvif-tool-active-layout';

function loadSavedLayouts(): SavedLayout[] {
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

function saveSavedLayouts(layouts: SavedLayout[]) {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layouts));
}

const ROW_HEIGHT_UNIT = 30;
const GRID_COLS = 32;

const STATIC_PRESETS = [
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

function generateDefaultLayout(cameras: Camera[], viewCols: number, hUnits: number): LayoutItem[] {
    return cameras.map((cam, i) => ({
        i: cam.id,
        x: (i % viewCols) * Math.floor(GRID_COLS / viewCols),
        y: Math.floor(i / viewCols) * hUnits,
        w: Math.floor(GRID_COLS / viewCols),
        h: hUnits,
        cameraId: cam.id,
    }));
}

// ── Conversion helpers ──────────────────────────────────────────────
// Convert static assignments → freeform layout items
function staticToFreeform(
    assignments: Record<number, string>,
    preset: { w: number; h: number },
    cameras: Camera[],
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
function freeformToStatic(
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


export default function CameraGrid({
    cameras,
    selectedCamera,
    isLive,
    playbackTime,
    onSelectCamera,
    onPeekCamera,
    syncPlayback = true,
    scrubbing = false,
    isAdmin = false,
    onRenameCamera,
    globalPaused = false,
}: CameraGridProps) {
    const [savedLayouts, setSavedLayouts] = useState<SavedLayout[]>([]);
    const [activeLayoutName, setActiveLayoutName] = useState<string>('');
    const [currentLayout, setCurrentLayout] = useState<LayoutItem[]>([]);
    const [cols, setCols] = useState(2);
    const [isEditing, setIsEditing] = useState(false);
    const [newLayoutName, setNewLayoutName] = useState('');
    const [globalQuality, setGlobalQuality] = useState<'auto' | 'high' | 'low'>('auto');

    // Grid mode for the active layout (or for the creation prompt)
    const [gridMode, setGridMode] = useState<'static' | 'freeform'>('static');
    const [staticPreset, setStaticPreset] = useState<{ w: number; h: number }>({ w: 2, h: 2 });
    const [staticAssignments, setStaticAssignments] = useState<Record<number, string>>({});
    const [pickerOpenSlot, setPickerOpenSlot] = useState<number>(-1);

    // Mode selector for the "Create Layout" prompt (separate from active mode)
    const [createMode, setCreateMode] = useState<'static' | 'freeform'>('static');
    // Freeform "Add Camera" picker
    const [showFreeformPicker, setShowFreeformPicker] = useState(false);

    const gridRef = useRef<HTMLDivElement>(null);
    const containerRef = useRef<HTMLDivElement>(null);

    // --- Interaction State (freeform drag/resize) ---
    const [draggingId, setDraggingId] = useState<string | null>(null);
    const [resizingId, setResizingId] = useState<string | null>(null);
    const [ghostItem, setGhostItem] = useState<LayoutItem | null>(null);
    const [dragStartPos, setDragStartPos] = useState({ clientX: 0, clientY: 0, initialGridX: 0, initialGridY: 0, itemStartX: 0, itemStartY: 0 });
    const [resizeStartPos, setResizeStartPos] = useState({ clientX: 0, clientY: 0, initialW: 0, initialH: 0 });

    // --- Load saved layouts on mount ---
    useEffect(() => {
        const layouts = loadSavedLayouts();
        setSavedLayouts(layouts);

        const activeName = localStorage.getItem(ACTIVE_LAYOUT_KEY) || '';
        const active = layouts.find(l => l.name === activeName);
        if (active) {
            setActiveLayoutName(active.name);
            if (active.mode === 'static' && active.staticPreset && active.staticAssignments) {
                setGridMode('static');
                setStaticPreset(active.staticPreset);
                setStaticAssignments(active.staticAssignments);
            } else {
                setGridMode('freeform');
                setCurrentLayout(active.items);
                setCols(active.cols);
            }
        }
        // No active layout → the "Create Layout" prompt will show
    }, []); // eslint-disable-line react-hooks/exhaustive-deps

    // Validate static assignments when cameras change
    useEffect(() => {
        if (cameras.length === 0) return;
        const totalSlots = staticPreset.w * staticPreset.h;
        const cameraIds = new Set(cameras.map(c => c.id));
        setStaticAssignments(prev => {
            if (Object.keys(prev).length === 0) return prev;
            const cleaned: Record<number, string> = {};
            for (const [slot, camId] of Object.entries(prev)) {
                const slotNum = Number(slot);
                if (slotNum < totalSlots && cameraIds.has(camId)) {
                    cleaned[slotNum] = camId;
                }
            }
            return cleaned;
        });
    }, [cameras, staticPreset]);

    // --- Add camera to freeform layout manually ---
    const handleAddFreeformCamera = useCallback((cameraId: string) => {
        setCurrentLayout(prev => {
            // Don't add duplicates
            if (prev.some(item => item.cameraId === cameraId)) return prev;
            const maxY = prev.length > 0 ? Math.max(...prev.map(l => l.y + l.h)) : 0;
            const colSpan = Math.floor(GRID_COLS / cols);
            const hu = 12;
            const col = prev.length % cols;
            const newItem: LayoutItem = {
                i: cameraId,
                x: col * colSpan,
                y: maxY + (col === 0 ? 0 : -hu), // Stack below or beside
                w: colSpan,
                h: hu,
                cameraId,
            };
            // If placing beside, recalculate y to be on the same row
            if (col > 0 && prev.length > 0) {
                const lastRowY = prev[prev.length - 1].y;
                newItem.y = lastRowY;
            }
            return [...prev, newItem];
        });
        setShowFreeformPicker(false);
    }, [cols]);

    // Remove camera from freeform layout
    const handleRemoveFreeformCamera = useCallback((cameraId: string) => {
        setCurrentLayout(prev => prev.filter(item => item.cameraId !== cameraId));
    }, []);

    // Cameras already in the freeform layout
    const freeformCameraIds = new Set(currentLayout.map(item => item.cameraId).filter((id): id is string => !!id));

    // Close picker when clicking outside
    useEffect(() => {
        if (pickerOpenSlot < 0 && !showFreeformPicker) return;
        const handler = (e: MouseEvent) => {
            const target = e.target as HTMLElement;
            if (!target.closest('.cell-picker-overlay') && !target.closest('.static-grid-cell') && !target.closest('.freeform-add-camera-wrapper')) {
                setPickerOpenSlot(-1);
                setShowFreeformPicker(false);
            }
        };
        const timer = setTimeout(() => document.addEventListener('click', handler), 0);
        return () => { clearTimeout(timer); document.removeEventListener('click', handler); };
    }, [pickerOpenSlot, showFreeformPicker]);

    // --- Static Grid Handlers ---
    const handleAssignCamera = useCallback((slotIndex: number, cameraId: string | null) => {
        setStaticAssignments(prev => {
            const next = { ...prev };
            if (cameraId === null) {
                delete next[slotIndex];
            } else {
                for (const [k, v] of Object.entries(next)) {
                    if (v === cameraId) delete next[Number(k)];
                }
                next[slotIndex] = cameraId;
            }
            return next;
        });
        setPickerOpenSlot(-1);
    }, []);

    const handleStaticPresetChange = useCallback((w: number, h: number) => {
        setStaticPreset({ w, h });
        const totalSlots = w * h;
        setStaticAssignments(prev => {
            const kept: Record<number, string> = {};
            for (const [slot, camId] of Object.entries(prev)) {
                if (Number(slot) < totalSlots) kept[Number(slot)] = camId;
            }
            return kept;
        });
    }, []);

    const assignedCameraIds = new Set(Object.values(staticAssignments));

    // --- Create Layout ---
    const handleCreateLayout = () => {
        if (!newLayoutName.trim()) return;
        const name = newLayoutName.trim();

        const layout: SavedLayout = createMode === 'static'
            ? {
                name,
                items: [],
                cols: staticPreset.w,
                version: 3,
                mode: 'static',
                staticPreset: { ...staticPreset },
                staticAssignments: {},
            }
            : {
                name,
                items: [],  // Start empty — user adds cameras via freeform editing
                cols: 2,
                version: 3,
                mode: 'freeform',
            };

        const existing = savedLayouts.filter(l => l.name !== name);
        const updated = [...existing, layout];
        setSavedLayouts(updated);
        saveSavedLayouts(updated);
        setActiveLayoutName(name);
        setGridMode(createMode);
        localStorage.setItem(ACTIVE_LAYOUT_KEY, name);

        if (createMode === 'static') {
            setStaticAssignments({});
        } else {
            setCurrentLayout([]);
            setCols(2);
            setIsEditing(true); // Open editing immediately for freeform
        }
        setNewLayoutName('');
    };

    // --- Auto-save active layout when assignments/items change ---
    useEffect(() => {
        if (!activeLayoutName) return;
        setSavedLayouts(prev => {
            const updated = prev.map(l => {
                if (l.name !== activeLayoutName) return l;
                if (l.mode === 'static') {
                    return { ...l, staticPreset, staticAssignments };
                } else {
                    return { ...l, items: currentLayout, cols };
                }
            });
            saveSavedLayouts(updated);
            return updated;
        });
    }, [activeLayoutName, staticAssignments, staticPreset, currentLayout, cols]);

    // --- Load Layout ---
    const handleLoadLayout = (layout: SavedLayout) => {
        if (layout.mode === 'static' && layout.staticPreset && layout.staticAssignments) {
            setGridMode('static');
            setStaticPreset(layout.staticPreset);
            setStaticAssignments(layout.staticAssignments);
        } else {
            setGridMode('freeform');
            setCurrentLayout(layout.items || []);
            setCols(layout.cols || 2);
            setIsEditing(false);
        }
        setActiveLayoutName(layout.name);
        localStorage.setItem(ACTIVE_LAYOUT_KEY, layout.name);
    };

    // --- Delete Layout ---
    const handleDeleteLayout = (name: string) => {
        if (!window.confirm(`Delete layout "${name}"?`)) return;
        const updated = savedLayouts.filter(l => l.name !== name);
        setSavedLayouts(updated);
        saveSavedLayouts(updated);
        if (activeLayoutName === name) {
            setActiveLayoutName('');
            localStorage.removeItem(ACTIVE_LAYOUT_KEY);
        }
    };

    // --- Convert Layout Mode ---
    const handleConvertLayout = useCallback(() => {
        if (!activeLayoutName) return;

        if (gridMode === 'static') {
            // Static → Freeform
            const items = staticToFreeform(staticAssignments, staticPreset, cameras);
            setGridMode('freeform');
            setCurrentLayout(items);
            setCols(staticPreset.w);
            setIsEditing(true);
            // Update saved layout
            setSavedLayouts(prev => {
                const updated = prev.map(l =>
                    l.name === activeLayoutName
                        ? { ...l, mode: 'freeform' as const, items, cols: staticPreset.w }
                        : l
                );
                saveSavedLayouts(updated);
                return updated;
            });
        } else {
            // Freeform → Static
            const { preset, assignments } = freeformToStatic(currentLayout, cameras);
            setGridMode('static');
            setStaticPreset(preset);
            setStaticAssignments(assignments);
            setIsEditing(false);
            // Update saved layout
            setSavedLayouts(prev => {
                const updated = prev.map(l =>
                    l.name === activeLayoutName
                        ? { ...l, mode: 'static' as const, staticPreset: preset, staticAssignments: assignments }
                        : l
                );
                saveSavedLayouts(updated);
                return updated;
            });
        }
    }, [activeLayoutName, gridMode, staticAssignments, staticPreset, currentLayout, cameras]);

    // --- Freeform quick layout (resets items) ---
    const handleQuickLayout = (columns: number) => {
        setCols(columns);
        const rows = columns;
        const hu = Math.max(4, Math.floor(18 / rows));
        setCurrentLayout(generateDefaultLayout(cameras, columns, hu));
    };

    // --- Drag and Drop Handlers ---
    const getGridMetrics = () => {
        if (!gridRef.current) return { colWidth: 1, rowHeight: 1, gridRect: null };
        const gridRect = gridRef.current.getBoundingClientRect();
        const totalGapWidth = (GRID_COLS - 1) * 6;
        const colWidth = (gridRect.width - totalGapWidth) / GRID_COLS + 6;
        const rowHeight = ROW_HEIGHT_UNIT + 6;
        return { colWidth, rowHeight, gridRect };
    };

    // Fit all items into the viewport after drag/resize ends
    const fitToViewport = useCallback((items: LayoutItem[]): LayoutItem[] => {
        if (items.length === 0) return items;

        // Find the bounding box of all items
        let maxRight = 0;
        let maxBottom = 0;
        for (const item of items) {
            maxRight = Math.max(maxRight, item.x + item.w);
            maxBottom = Math.max(maxBottom, item.y + item.h);
        }

        if (maxRight === 0 || maxBottom === 0) return items;

        // Scale horizontally if items extend past GRID_COLS
        const scaleX = maxRight > GRID_COLS ? GRID_COLS / maxRight : 1;
        // Scale vertically isn't needed for fitting — we'll just adjust row height
        // But if items overlap or go negative, normalize

        return items.map(item => ({
            ...item,
            x: Math.round(item.x * scaleX),
            y: item.y,
            w: Math.max(2, Math.round(item.w * scaleX)),
            h: Math.max(2, item.h),
        }));
    }, []);

    const onDragStart = (e: React.PointerEvent<HTMLDivElement>, item: LayoutItem) => {
        if (!isEditing || !gridRef.current) return;
        setDraggingId(item.i);
        const elRect = (e.currentTarget.parentElement as HTMLElement).getBoundingClientRect();
        const { gridRect } = getGridMetrics();
        if (!gridRect) return;
        setDragStartPos({
            clientX: e.clientX, clientY: e.clientY,
            initialGridX: item.x, initialGridY: item.y,
            itemStartX: elRect.left - gridRect.left, itemStartY: elRect.top - gridRect.top,
        });
        setGhostItem({ ...item });
        e.currentTarget.setPointerCapture(e.pointerId);
    };

    const onDragMove = (e: React.PointerEvent<HTMLDivElement>) => {
        if (!draggingId || !ghostItem) return;
        const dx = e.clientX - dragStartPos.clientX;
        const dy = e.clientY - dragStartPos.clientY;
        const { colWidth, rowHeight } = getGridMetrics();
        let newX = Math.round(dragStartPos.initialGridX + dx / colWidth);
        let newY = Math.round(dragStartPos.initialGridY + dy / rowHeight);
        // Allow extending past edges during drag — no clamping
        if (newY < 0) newY = 0;
        setGhostItem({ ...ghostItem, x: newX, y: newY });
    };

    const onDragEnd = (e: React.PointerEvent<HTMLDivElement>) => {
        if (!draggingId || !ghostItem) return;
        e.currentTarget.releasePointerCapture(e.pointerId);
        const updated = currentLayout.map(item => item.i === draggingId ? { ...item, x: ghostItem.x, y: ghostItem.y } : item);
        setCurrentLayout(fitToViewport(updated));
        setDraggingId(null);
        setGhostItem(null);
    };

    const onResizeStart = (e: React.PointerEvent<HTMLDivElement>, item: LayoutItem) => {
        if (!isEditing) return;
        e.stopPropagation();
        setResizingId(item.i);
        setResizeStartPos({ clientX: e.clientX, clientY: e.clientY, initialW: item.w, initialH: item.h });
        setGhostItem({ ...item });
        e.currentTarget.setPointerCapture(e.pointerId);
    };

    const onResizeMove = (e: React.PointerEvent<HTMLDivElement>) => {
        if (!resizingId || !ghostItem) return;
        const dx = e.clientX - resizeStartPos.clientX;
        const dy = e.clientY - resizeStartPos.clientY;
        const { colWidth, rowHeight } = getGridMetrics();
        let newW = Math.round(resizeStartPos.initialW + dx / colWidth);
        let newH = Math.round(resizeStartPos.initialH + dy / rowHeight);
        if (newW < 2) newW = 2;
        if (newH < 2) newH = 2;
        // Allow extending past edges during resize — no clamping
        setGhostItem({ ...ghostItem, w: newW, h: newH });
    };

    const onResizeEnd = (e: React.PointerEvent<HTMLDivElement>) => {
        if (!resizingId || !ghostItem) return;
        e.currentTarget.releasePointerCapture(e.pointerId);
        const updated = currentLayout.map(item => item.i === resizingId ? { ...item, w: ghostItem.w, h: ghostItem.h } : item);
        setCurrentLayout(fitToViewport(updated));
        setResizingId(null);
        setGhostItem(null);
    };

    // Build freeform grid items
    const gridItems = currentLayout.filter(item => item.cameraId && cameras.some(c => c.id === item.cameraId));
    const { colWidth, rowHeight } = getGridMetrics();

    // Dynamic row height: fit all items vertically in the container
    const maxBottom = gridItems.length > 0
        ? Math.max(...gridItems.map(it => it.y + it.h))
        : 1;
    const containerHeight = containerRef.current?.querySelector('.camera-grid-area')?.clientHeight;
    const dynamicRowH = containerHeight && maxBottom > 0
        ? Math.max(8, Math.floor((containerHeight - (maxBottom - 1) * 6) / maxBottom))
        : ROW_HEIGHT_UNIT;

    // Get mode of the active layout for display
    const activeLayout = savedLayouts.find(l => l.name === activeLayoutName);

    return (
        <div className="camera-grid-wrapper" ref={containerRef}>
            {/* Layout Toolbar */}
            <div className="layout-toolbar">
                <div className="layout-toolbar-left">
                    {/* Show active mode + convert button when a layout is active */}
                    {activeLayoutName && (
                        <>
                            <span className="layout-mode-badge" title={`Current mode: ${gridMode}`}>
                                {gridMode === 'static' ? '⊞ Static' : '✦ Freeform'}
                            </span>
                            <button
                                className="layout-convert-btn"
                                onClick={handleConvertLayout}
                                title={gridMode === 'static' ? 'Convert to Freeform layout' : 'Convert to Static grid'}
                            >
                                ⇄ Convert to {gridMode === 'static' ? 'Freeform' : 'Static'}
                            </button>
                            <div className="layout-divider" />
                        </>
                    )}

                    {/* Static mode controls */}
                    {activeLayoutName && gridMode === 'static' && (
                        <select
                            className="layout-preset-select"
                            value={`${staticPreset.w}x${staticPreset.h}`}
                            onChange={(e) => {
                                const [w, h] = e.target.value.split('x').map(Number);
                                handleStaticPresetChange(w, h);
                            }}
                            title="Grid layout preset"
                        >
                            {STATIC_PRESETS.map(p => (
                                <option key={p.label} value={`${p.w}x${p.h}`}>{p.label}</option>
                            ))}
                        </select>
                    )}

                    {/* Freeform mode controls */}
                    {activeLayoutName && gridMode === 'freeform' && (
                        <>
                            <div className="freeform-add-camera-wrapper">
                                <button
                                    className={`layout-quick-btn freeform-add-btn ${showFreeformPicker ? 'active' : ''}`}
                                    onClick={() => setShowFreeformPicker(!showFreeformPicker)}
                                    title="Add a camera to this layout"
                                >
                                    ＋ Add Camera
                                </button>
                                {showFreeformPicker && (
                                    <div className="freeform-picker-dropdown">
                                        <CameraPicker
                                            cameras={cameras}
                                            assignedCameraIds={freeformCameraIds}
                                            currentCameraId={null}
                                            onSelect={handleAddFreeformCamera}
                                            onClear={null}
                                        />
                                    </div>
                                )}
                            </div>
                            <div className="layout-divider" />
                            <button
                                className={`layout-edit-btn ${isEditing ? 'active' : ''}`}
                                onClick={() => setIsEditing(!isEditing)}
                                title={isEditing ? 'Lock layout' : 'Edit layout'}
                            >
                                {isEditing ? '🔓 Editing' : '🔒 Locked'}
                            </button>
                        </>
                    )}

                    {activeLayoutName && (
                        <>
                            <div className="layout-divider" />
                            <button
                                className={`stream-quality-btn ${globalQuality === 'high' ? 'hd' : globalQuality === 'low' ? 'sd' : ''}`}
                                onClick={() => setGlobalQuality(q => q === 'auto' ? 'high' : q === 'high' ? 'low' : 'auto')}
                                title={`Stream quality: ${globalQuality === 'auto' ? 'Auto' : globalQuality === 'high' ? 'HD' : 'SD'}`}
                                style={{ fontSize: 11, padding: '4px 9px' }}
                            >
                                {globalQuality === 'high' ? '📺 HD' : globalQuality === 'low' ? '📺 SD' : '📺 Auto'}
                            </button>
                        </>
                    )}
                </div>

                <div className="layout-toolbar-center">
                    {/* All saved layouts — shown regardless of mode */}
                    {savedLayouts.map(layout => (
                        <div key={layout.name} className="saved-layout-chip">
                            <button
                                className={`layout-load-btn ${activeLayoutName === layout.name ? 'active' : ''}`}
                                onClick={() => handleLoadLayout(layout)}
                                title={`${layout.mode === 'static' ? 'Static' : 'Freeform'} layout`}
                            >
                                <span className="layout-chip-mode">{layout.mode === 'static' ? '⊞' : '✦'}</span>
                                {layout.name}
                            </button>
                            <button
                                className="layout-delete-btn"
                                onClick={(e) => { e.stopPropagation(); handleDeleteLayout(layout.name); }}
                                title="Delete layout"
                            >
                                ×
                            </button>
                        </div>
                    ))}
                </div>

                <div className="layout-toolbar-right">
                    {/* New Layout button always visible */}
                    <button
                        className="layout-save-btn"
                        onClick={() => {
                            setActiveLayoutName('');
                            localStorage.removeItem(ACTIVE_LAYOUT_KEY);
                        }}
                        title="Create a new layout"
                    >
                        ＋ New Layout
                    </button>
                </div>
            </div>

            {/* ── Create Layout Prompt ───────────────────────────────────────── */}
            {!activeLayoutName && (
                <div className="camera-grid-area static-grid-create-prompt">
                    <div className="create-layout-card">
                        <div className="create-layout-icon">⊞</div>
                        <h3 className="create-layout-title">Create a Layout</h3>
                        <p className="create-layout-desc">Choose a mode and name your layout to start placing cameras.</p>

                        {/* Mode toggle */}
                        <div className="create-mode-toggle">
                            <button
                                className={`create-mode-btn ${createMode === 'static' ? 'active' : ''}`}
                                onClick={() => setCreateMode('static')}
                            >
                                ⊞ Static Grid
                            </button>
                            <button
                                className={`create-mode-btn ${createMode === 'freeform' ? 'active' : ''}`}
                                onClick={() => setCreateMode('freeform')}
                            >
                                ✦ Freeform
                            </button>
                        </div>

                        {/* Static preset selector (only for static mode) */}
                        {createMode === 'static' && (
                            <div className="create-preset-row">
                                <span className="create-preset-label">Grid Size</span>
                                <select
                                    className="layout-preset-select"
                                    value={`${staticPreset.w}x${staticPreset.h}`}
                                    onChange={(e) => {
                                        const [w, h] = e.target.value.split('x').map(Number);
                                        setStaticPreset({ w, h });
                                    }}
                                >
                                    {STATIC_PRESETS.map(p => (
                                        <option key={p.label} value={`${p.w}x${p.h}`}>{p.label}</option>
                                    ))}
                                </select>
                            </div>
                        )}

                        {createMode === 'freeform' && (
                            <p className="create-layout-hint">Freeform lets you drag and resize camera cells freely on a canvas.</p>
                        )}

                        <div className="create-layout-form">
                            <input
                                type="text"
                                placeholder="Layout name..."
                                value={newLayoutName}
                                onChange={(e) => setNewLayoutName(e.target.value)}
                                onKeyDown={(e) => e.key === 'Enter' && handleCreateLayout()}
                                className="save-input create-layout-input"
                                autoFocus
                            />
                            <button
                                className="save-confirm-btn create-layout-btn"
                                onClick={handleCreateLayout}
                                disabled={!newLayoutName.trim()}
                            >
                                ＋ Create
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* ── Static Grid ─────────────────────────────────────────────────── */}
            {activeLayoutName && gridMode === 'static' && (
                <div
                    className="camera-grid-area static-grid"
                    style={{
                        display: 'grid',
                        gridTemplateColumns: `repeat(${staticPreset.w}, 1fr)`,
                        gridTemplateRows: `repeat(${staticPreset.h}, 1fr)`,
                        gap: '4px',
                        height: '100%',
                        overflow: 'hidden',
                    }}
                >
                    {Array.from({ length: staticPreset.w * staticPreset.h }, (_, slotIndex) => {
                        const assignedCameraId = staticAssignments[slotIndex];
                        const camera = assignedCameraId ? cameras.find(c => c.id === assignedCameraId) : null;
                        const isPickerOpen = pickerOpenSlot === slotIndex;

                        if (camera) {
                            return (
                                <div
                                    key={slotIndex}
                                    className={`custom-grid-item static-grid-cell ${selectedCamera === camera.id ? 'selected' : ''}`}
                                    onClick={() => onSelectCamera(camera.id)}
                                >
                                    {/* Swap/Clear button — top-left gear icon */}
                                    <button
                                        className="cell-swap-btn"
                                        onClick={(e) => {
                                            e.stopPropagation();
                                            setPickerOpenSlot(isPickerOpen ? -1 : slotIndex);
                                        }}
                                        title="Change camera for this cell"
                                    >
                                        ⚙
                                    </button>

                                    <VideoPlayer
                                        cameraId={camera.id}
                                        cameraName={camera.name}
                                        isLive={isLive}
                                        playbackTime={playbackTime}
                                        selected={camera.id === selectedCamera}
                                        hasPTZ={camera.has_ptz}
                                        streamQuality={globalQuality}
                                        syncBadge={syncPlayback && !isLive}
                                        scrubbing={scrubbing}
                                        isAdmin={isAdmin}
                                        onRename={onRenameCamera}
                                        onDoubleClick={() => onPeekCamera?.(camera.id)}
                                        globalPaused={globalPaused}
                                    />

                                    {isPickerOpen && (
                                        <CameraPicker
                                            cameras={cameras}
                                            assignedCameraIds={assignedCameraIds}
                                            currentCameraId={camera.id}
                                            onSelect={(camId) => handleAssignCamera(slotIndex, camId)}
                                            onClear={() => handleAssignCamera(slotIndex, null)}
                                        />
                                    )}
                                </div>
                            );
                        } else {
                            return (
                                <div
                                    key={slotIndex}
                                    className="custom-grid-item static-grid-cell static-grid-cell-empty"
                                    onClick={() => setPickerOpenSlot(isPickerOpen ? -1 : slotIndex)}
                                >
                                    <div className="empty-cell-content">
                                        <span className="empty-cell-icon">+</span>
                                        <span className="empty-cell-label">Assign Camera</span>
                                    </div>
                                    {isPickerOpen && (
                                        <CameraPicker
                                            cameras={cameras}
                                            assignedCameraIds={assignedCameraIds}
                                            currentCameraId={null}
                                            onSelect={(camId) => handleAssignCamera(slotIndex, camId)}
                                            onClear={null}
                                        />
                                    )}
                                </div>
                            );
                        }
                    })}
                </div>
            )}

            {/* ── Freeform Grid ────────────────────────────────────────────────── */}
            {activeLayoutName && gridMode === 'freeform' && (
                <div className="camera-grid-area" style={{ overflow: isEditing ? 'visible' : 'hidden' }}>
                    <div
                        className="custom-grid-canvas"
                        ref={gridRef}
                        style={{
                            '--grid-cols': GRID_COLS,
                            '--grid-row-h': `${dynamicRowH}px`,
                            overflow: isEditing ? 'visible' : 'hidden',
                        } as React.CSSProperties}
                    >
                        {isEditing && ghostItem && (
                            <div
                                className="custom-grid-ghost"
                                style={{
                                    gridColumnStart: ghostItem.x + 1,
                                    gridRowStart: ghostItem.y + 1,
                                    gridColumnEnd: `span ${ghostItem.w}`,
                                    gridRowEnd: `span ${ghostItem.h}`,
                                }}
                            />
                        )}

                        {gridItems.map(item => {
                            const camera = cameras.find(c => c.id === item.cameraId);
                            if (!camera) return null;

                            const isDragging = draggingId === item.i;
                            const isResizing = resizingId === item.i;

                            let inlineStyle: React.CSSProperties = {
                                gridColumnStart: item.x + 1,
                                gridRowStart: item.y + 1,
                                gridColumnEnd: `span ${item.w}`,
                                gridRowEnd: `span ${item.h}`,
                            };

                            if (isDragging) {
                                inlineStyle = {
                                    position: 'absolute',
                                    left: `${dragStartPos.itemStartX}px`,
                                    top: `${dragStartPos.itemStartY}px`,
                                    width: `${item.w * colWidth - 6}px`,
                                    height: `${item.h * rowHeight - 6}px`,
                                    transform: `translate(${ghostItem?.x !== undefined ? (ghostItem.x - dragStartPos.initialGridX) * colWidth : 0}px, ${ghostItem?.y !== undefined ? (ghostItem.y - dragStartPos.initialGridY) * rowHeight : 0}px)`,
                                };
                            } else if (isResizing) {
                                inlineStyle = {
                                    gridColumnStart: item.x + 1,
                                    gridRowStart: item.y + 1,
                                    gridColumnEnd: `span ${ghostItem?.w || item.w}`,
                                    gridRowEnd: `span ${ghostItem?.h || item.h}`,
                                }
                            }

                            return (
                                <div
                                    key={item.i}
                                    className={`custom-grid-item ${selectedCamera === camera.id ? 'selected' : ''} ${isDragging ? 'dragging' : ''} ${isResizing ? 'resizing' : ''}`}
                                    style={inlineStyle}
                                    onClick={() => onSelectCamera(camera.id)}
                                >
                                    {isEditing && (
                                        <div
                                            className="drag-handle"
                                            onPointerDown={(e) => onDragStart(e, item)}
                                            onPointerMove={onDragMove}
                                            onPointerUp={onDragEnd}
                                            onPointerCancel={onDragEnd}
                                        >
                                            ⋮⋮ {camera.name}
                                        </div>
                                    )}

                                    <VideoPlayer
                                        cameraId={camera.id}
                                        cameraName={camera.name}
                                        isLive={isLive}
                                        playbackTime={playbackTime}
                                        selected={camera.id === selectedCamera}
                                        hasPTZ={camera.has_ptz}
                                        streamQuality={globalQuality}
                                        syncBadge={syncPlayback && !isLive}
                                        scrubbing={scrubbing}
                                        isAdmin={isAdmin}
                                        onRename={onRenameCamera}
                                        onDoubleClick={() => onPeekCamera?.(camera.id)}
                                        globalPaused={globalPaused}
                                    />

                                    {isEditing && (
                                        <div
                                            className="custom-resize-handle"
                                            onPointerDown={(e) => onResizeStart(e, item)}
                                            onPointerMove={onResizeMove}
                                            onPointerUp={onResizeEnd}
                                            onPointerCancel={onResizeEnd}
                                        />
                                    )}
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}
        </div>
    );
}


// ── Camera Picker Dropdown ──────────────────────────────────────────────
interface CameraPickerProps {
    cameras: Camera[];
    assignedCameraIds: Set<string>;
    currentCameraId: string | null;
    onSelect: (cameraId: string) => void;
    onClear: (() => void) | null;
}

function CameraPicker({ cameras, assignedCameraIds, currentCameraId, onSelect, onClear }: CameraPickerProps) {
    const [search, setSearch] = useState('');
    const filtered = cameras.filter(cam =>
        cam.name.toLowerCase().includes(search.toLowerCase())
    );

    return (
        <div className="cell-picker-overlay" onClick={(e) => e.stopPropagation()}>
            <div className="cell-picker-header">Select Camera</div>

            {/* Search input */}
            <div className="cell-picker-search">
                <input
                    type="text"
                    placeholder="Search cameras..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="cell-picker-search-input"
                    autoFocus
                    onClick={(e) => e.stopPropagation()}
                />
            </div>

            {onClear && (
                <button className="cell-picker-option cell-picker-clear" onClick={onClear}>
                    <span className="picker-clear-icon">✕</span>
                    <span>Clear Cell</span>
                </button>
            )}

            <div className="cell-picker-list">
                {filtered.length === 0 && (
                    <div className="cell-picker-empty">No cameras match "{search}"</div>
                )}
                {filtered.map(cam => {
                    const isAssigned = assignedCameraIds.has(cam.id) && cam.id !== currentCameraId;
                    const isCurrent = cam.id === currentCameraId;
                    return (
                        <button
                            key={cam.id}
                            className={`cell-picker-option ${isCurrent ? 'active' : ''} ${isAssigned ? 'disabled' : ''}`}
                            onClick={() => { if (!isAssigned) onSelect(cam.id); }}
                            disabled={isAssigned}
                            title={isAssigned ? 'Already assigned to another cell' : cam.name}
                        >
                            <span className={`picker-status-dot ${cam.status === 'online' ? 'online' : 'offline'}`} />
                            <span className="picker-cam-name">{cam.name}</span>
                            {isCurrent && <span className="picker-current-badge">Current</span>}
                            {isAssigned && <span className="picker-assigned-badge">In Use</span>}
                        </button>
                    );
                })}
            </div>
        </div>
    );
}
