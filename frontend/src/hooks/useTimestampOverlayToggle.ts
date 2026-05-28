'use client';

// Global tile-timestamp-overlay toggle extracted from VideoPlayer
// (P1-B-11 session 18). The "click any tile to flip the overlay on every
// tile at once" UX is implemented as a localStorage-backed bool + a
// CustomEvent broadcast — cheaper than a React Context for one bool.
// Every VideoPlayer subscribes via this hook; whichever tile gets the
// click calls toggleOverlay(), the storage value updates, and the
// CustomEvent fans out to every other tile's listener.

import { useCallback, useEffect, useState } from 'react';

const STORAGE_KEY = 'ironsight-tile-timestamp-overlay';
const EVENT_NAME = 'ironsight:timestamp-overlay-toggle';

export interface TimestampOverlayToggle {
    showOverlay: boolean;
    toggleOverlay: () => void;
}

export function useTimestampOverlayToggle(): TimestampOverlayToggle {
    const [showOverlay, setShowOverlay] = useState<boolean>(true);

    useEffect(() => {
        const saved = typeof window !== 'undefined'
            ? localStorage.getItem(STORAGE_KEY)
            : null;
        if (saved !== null) setShowOverlay(saved === 'on');
        const onToggle = (e: Event) => {
            setShowOverlay((e as CustomEvent<boolean>).detail);
        };
        window.addEventListener(EVENT_NAME, onToggle);
        return () => window.removeEventListener(EVENT_NAME, onToggle);
    }, []);

    const toggleOverlay = useCallback(() => {
        setShowOverlay(prev => {
            const next = !prev;
            localStorage.setItem(STORAGE_KEY, next ? 'on' : 'off');
            window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail: next }));
            return next;
        });
    }, []);

    return { showOverlay, toggleOverlay };
}
