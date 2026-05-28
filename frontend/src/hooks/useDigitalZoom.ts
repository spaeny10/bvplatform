'use client';

// Digital-zoom + pan hook extracted from VideoPlayer (P1-B-11 session 18).
// Owns the 4 useState slots (scale, pan, isDragging, dragStart) and the
// 4 mouse/wheel handlers. Resets when `resetKey` changes (the caller
// typically passes the camera id so a camera switch zeros the zoom).
//
// Returned shape is the bag of mouse-event handlers + the current
// transform values the parent uses to compute the CSS transform on the
// video element. Caller decides whether to bind the handlers (e.g.
// VideoPlayer only attaches them when `allowZoom` is true).

import { useEffect, useState } from 'react';

export interface DigitalZoom {
    scale: number;
    pan: { x: number; y: number };
    isDragging: boolean;
    handleWheel: (e: React.WheelEvent) => void;
    handleMouseDown: (e: React.MouseEvent) => void;
    handleMouseMove: (e: React.MouseEvent) => void;
    handleMouseUp: () => void;
}

export function useDigitalZoom(allowZoom: boolean, resetKey: unknown): DigitalZoom {
    const [scale, setScale] = useState(1);
    const [pan, setPan] = useState({ x: 0, y: 0 });
    const [isDragging, setIsDragging] = useState(false);
    const [dragStart, setDragStart] = useState({ x: 0, y: 0 });

    useEffect(() => {
        setScale(1);
        setPan({ x: 0, y: 0 });
    }, [resetKey]);

    const handleWheel = (e: React.WheelEvent) => {
        if (!allowZoom) return;
        const zoomDelta = e.deltaY * -0.002;
        setScale((prev) => {
            const newScale = Math.min(Math.max(1, prev + zoomDelta), 10);
            if (newScale === 1) {
                setPan({ x: 0, y: 0 });
            }
            return newScale;
        });
    };

    const handleMouseDown = (e: React.MouseEvent) => {
        if (!allowZoom) return;
        if (scale > 1) {
            setIsDragging(true);
            setDragStart({ x: e.clientX - pan.x, y: e.clientY - pan.y });
        }
    };

    const handleMouseMove = (e: React.MouseEvent) => {
        if (!allowZoom || !isDragging) return;
        if (scale > 1) {
            setPan({
                x: e.clientX - dragStart.x,
                y: e.clientY - dragStart.y,
            });
        }
    };

    const handleMouseUp = () => {
        if (!allowZoom) return;
        setIsDragging(false);
    };

    return { scale, pan, isDragging, handleWheel, handleMouseDown, handleMouseMove, handleMouseUp };
}
