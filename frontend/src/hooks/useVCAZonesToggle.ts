'use client';

// Global live VCA-zone-overlay toggle, modeled on useTimestampOverlayToggle.
// Renders a camera's VCA detection zones/polygons over the LIVE video feed
// across every tile at once. Implemented as a localStorage-backed bool + a
// CustomEvent broadcast (cheaper than a React Context for one bool): every
// VideoPlayer subscribes via this hook; whichever tile gets the click calls
// toggleZones(), the storage value updates, and the CustomEvent fans out to
// every other tile's listener so the whole grid stays in sync.
//
// Default ON: this is a security view and seeing the configured detection
// zones over live footage is the operator's expected baseline.

import { useCallback, useEffect, useState } from 'react';

const STORAGE_KEY = 'ironsight-vca-zones-visible';
const EVENT_NAME = 'ironsight:vca-zones-toggle';

export interface VCAZonesToggle {
    showZones: boolean;
    toggleZones: () => void;
}

export function useVCAZonesToggle(): VCAZonesToggle {
    const [showZones, setShowZones] = useState<boolean>(true);

    useEffect(() => {
        const saved = typeof window !== 'undefined'
            ? localStorage.getItem(STORAGE_KEY)
            : null;
        if (saved !== null) setShowZones(saved === 'on');
        const onToggle = (e: Event) => {
            setShowZones((e as CustomEvent<boolean>).detail);
        };
        window.addEventListener(EVENT_NAME, onToggle);
        return () => window.removeEventListener(EVENT_NAME, onToggle);
    }, []);

    const toggleZones = useCallback(() => {
        setShowZones(prev => {
            const next = !prev;
            localStorage.setItem(STORAGE_KEY, next ? 'on' : 'off');
            window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail: next }));
            return next;
        });
    }, []);

    return { showZones, toggleZones };
}
