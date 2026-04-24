'use client';

import { useState, useCallback, useRef, useEffect } from 'react';
import type { SiteCamera } from '@/types/ironsight';

interface CameraLayoutItem {
  cameraId: string;
  order: number;
  pinned: boolean;
}

interface LayoutConfig {
  siteId: string;
  cameras: CameraLayoutItem[];
  columns: number;
}

const STORAGE_KEY = 'sg-camera-layouts';

function loadLayouts(): Record<string, LayoutConfig> {
  if (typeof window === 'undefined') return {};
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved ? JSON.parse(saved) : {};
  } catch { return {}; }
}

function saveLayouts(layouts: Record<string, LayoutConfig>) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(layouts));
}

export function useCameraLayout(siteId: string, cameras: SiteCamera[]) {
  const [layouts, setLayouts] = useState<Record<string, LayoutConfig>>(loadLayouts);
  const [dragItem, setDragItem] = useState<number | null>(null);
  const [dragOverItem, setDragOverItem] = useState<number | null>(null);

  // Initialize layout for site if none exists
  const layout = layouts[siteId] || {
    siteId,
    cameras: cameras.map((cam, i) => ({ cameraId: cam.id, order: i, pinned: false })),
    columns: 3,
  };

  // Get ordered cameras
  const orderedCameras = [...layout.cameras]
    .sort((a, b) => {
      // Pinned cameras first
      if (a.pinned && !b.pinned) return -1;
      if (!a.pinned && b.pinned) return 1;
      return a.order - b.order;
    })
    .map(item => {
      const cam = cameras.find(c => c.id === item.cameraId);
      return cam ? { ...cam, pinned: item.pinned } : null;
    })
    .filter(Boolean) as (SiteCamera & { pinned: boolean })[];

  const updateLayout = useCallback((newLayout: LayoutConfig) => {
    setLayouts(prev => {
      const updated = { ...prev, [siteId]: newLayout };
      saveLayouts(updated);
      return updated;
    });
  }, [siteId]);

  const onDragStart = useCallback((index: number) => {
    setDragItem(index);
  }, []);

  const onDragOver = useCallback((index: number) => {
    setDragOverItem(index);
  }, []);

  const onDragEnd = useCallback(() => {
    if (dragItem === null || dragOverItem === null || dragItem === dragOverItem) {
      setDragItem(null);
      setDragOverItem(null);
      return;
    }

    const newCameras = [...layout.cameras].sort((a, b) => a.order - b.order);
    const [dragged] = newCameras.splice(dragItem, 1);
    newCameras.splice(dragOverItem, 0, dragged);

    // Re-index
    newCameras.forEach((cam, i) => { cam.order = i; });

    updateLayout({ ...layout, cameras: newCameras });
    setDragItem(null);
    setDragOverItem(null);
  }, [dragItem, dragOverItem, layout, updateLayout]);

  const togglePin = useCallback((cameraId: string) => {
    const newCameras = layout.cameras.map(c =>
      c.cameraId === cameraId ? { ...c, pinned: !c.pinned } : c
    );
    updateLayout({ ...layout, cameras: newCameras });
  }, [layout, updateLayout]);

  const resetLayout = useCallback(() => {
    const defaultLayout: LayoutConfig = {
      siteId,
      cameras: cameras.map((cam, i) => ({ cameraId: cam.id, order: i, pinned: false })),
      columns: layout.columns,
    };
    updateLayout(defaultLayout);
  }, [siteId, cameras, layout.columns, updateLayout]);

  const setColumns = useCallback((cols: number) => {
    updateLayout({ ...layout, columns: cols });
  }, [layout, updateLayout]);

  return {
    orderedCameras,
    columns: layout.columns,
    dragItem,
    dragOverItem,
    onDragStart,
    onDragOver,
    onDragEnd,
    togglePin,
    resetLayout,
    setColumns,
  };
}

// ── Layout Toolbar Component ──
interface ToolbarProps {
  columns: number;
  onColumnsChange: (n: number) => void;
  onReset: () => void;
  editMode: boolean;
  onToggleEditMode: () => void;
}

export function CameraLayoutToolbar({ columns, onColumnsChange, onReset, editMode, onToggleEditMode }: ToolbarProps) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 6,
      padding: '4px 8px',
      background: editMode ? 'rgba(0,212,255,0.04)' : 'transparent',
      border: `1px solid ${editMode ? 'rgba(0,212,255,0.15)' : 'transparent'}`,
      borderRadius: 4,
      transition: 'all 0.2s',
    }}>
      <button
        onClick={onToggleEditMode}
        style={{
          padding: '3px 8px', borderRadius: 3,
          background: editMode ? 'rgba(0,212,255,0.1)' : 'rgba(255,255,255,0.03)',
          border: `1px solid ${editMode ? 'rgba(0,212,255,0.2)' : 'rgba(255,255,255,0.06)'}`,
          color: editMode ? '#E8732A' : '#8891A5',
          fontSize: 9, fontWeight: 600, cursor: 'pointer',
          fontFamily: "'JetBrains Mono', monospace",
          letterSpacing: 0.5,
        }}
      >
        {editMode ? '✓ Done' : '⊞ Layout'}
      </button>

      {editMode && (
        <>
          <div style={{
            width: 1, height: 16,
            background: 'rgba(255,255,255,0.06)',
          }} />
          {[2, 3, 4].map(n => (
            <button
              key={n}
              onClick={() => onColumnsChange(n)}
              style={{
                width: 22, height: 22, borderRadius: 3,
                background: columns === n ? 'rgba(0,212,255,0.1)' : 'rgba(255,255,255,0.02)',
                border: `1px solid ${columns === n ? 'rgba(0,212,255,0.2)' : 'rgba(255,255,255,0.04)'}`,
                color: columns === n ? '#E8732A' : '#4A5268',
                fontSize: 9, fontWeight: 700, cursor: 'pointer',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              {n}
            </button>
          ))}
          <button
            onClick={onReset}
            style={{
              padding: '3px 8px', borderRadius: 3,
              background: 'rgba(255,51,85,0.04)',
              border: '1px solid rgba(255,51,85,0.1)',
              color: '#EF4444',
              fontSize: 9, fontWeight: 600, cursor: 'pointer',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            ↺ Reset
          </button>
        </>
      )}
    </div>
  );
}

// ── Draggable Camera Cell Wrapper ──
interface DragWrapperProps {
  index: number;
  isDragging: boolean;
  isDragOver: boolean;
  editMode: boolean;
  pinned: boolean;
  onDragStart: (i: number) => void;
  onDragOver: (i: number) => void;
  onDragEnd: () => void;
  onTogglePin: () => void;
  children: React.ReactNode;
}

export function DraggableCameraCell({
  index, isDragging, isDragOver, editMode, pinned,
  onDragStart, onDragOver, onDragEnd, onTogglePin,
  children,
}: DragWrapperProps) {
  return (
    <div
      draggable={editMode}
      onDragStart={() => editMode && onDragStart(index)}
      onDragOver={(e) => { e.preventDefault(); editMode && onDragOver(index); }}
      onDrop={() => editMode && onDragEnd()}
      onDragEnd={onDragEnd}
      style={{
        position: 'relative',
        opacity: isDragging ? 0.4 : 1,
        transform: isDragOver ? 'scale(1.02)' : 'scale(1)',
        transition: 'all 0.15s',
        cursor: editMode ? 'grab' : 'default',
        outline: isDragOver ? '2px dashed rgba(0,212,255,0.4)' : 'none',
        outlineOffset: -2,
        borderRadius: isDragOver ? 6 : 0,
      }}
    >
      {children}

      {/* Edit mode overlay indicators */}
      {editMode && (
        <>
          {/* Drag handle */}
          <div style={{
            position: 'absolute', top: 6, left: 6,
            background: 'rgba(0,0,0,0.7)', backdropFilter: 'blur(4px)',
            borderRadius: 4, padding: '3px 6px',
            fontSize: 10, color: '#8891A5', cursor: 'grab',
          }}>
            ⠿
          </div>

          {/* Pin button */}
          <button
            onClick={(e) => { e.stopPropagation(); onTogglePin(); }}
            style={{
              position: 'absolute', top: 6, right: 6,
              width: 24, height: 24, borderRadius: 4,
              background: pinned ? 'rgba(255,204,0,0.15)' : 'rgba(0,0,0,0.7)',
              border: `1px solid ${pinned ? 'rgba(255,204,0,0.3)' : 'rgba(255,255,255,0.08)'}`,
              color: pinned ? '#E89B2A' : '#4A5268',
              cursor: 'pointer', fontSize: 12,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              backdropFilter: 'blur(4px)',
            }}
            title={pinned ? 'Unpin camera' : 'Pin to top'}
          >
            📌
          </button>
        </>
      )}

      {/* Pinned indicator (always shown) */}
      {pinned && !editMode && (
        <div style={{
          position: 'absolute', top: 4, right: 4,
          fontSize: 8, padding: '1px 5px',
          background: 'rgba(255,204,0,0.1)',
          border: '1px solid rgba(255,204,0,0.2)',
          borderRadius: 3,
          color: '#E89B2A',
          fontFamily: "'JetBrains Mono', monospace",
          fontWeight: 700,
        }}>
          📌 PINNED
        </div>
      )}
    </div>
  );
}
