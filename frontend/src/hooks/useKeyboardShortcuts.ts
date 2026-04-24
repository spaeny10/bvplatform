import { useEffect, useCallback, useState } from 'react';

export interface ShortcutConfig {
    onGoLive: () => void;
    onSeek: (time: Date) => void;
    onClosePeek: () => void;
    isLive: boolean;
    currentTime: Date;
    peekOpen: boolean;
}

const SEEK_DELTA_MS = 30_000; // 30 seconds

/** Thin wrapper around document.addEventListener for keyboard shortcuts */
export function useKeyboardShortcuts({
    onGoLive,
    onSeek,
    onClosePeek,
    isLive,
    currentTime,
    peekOpen,
}: ShortcutConfig) {
    const [helpOpen, setHelpOpen] = useState(false);

    const handleKey = useCallback(
        (e: KeyboardEvent) => {
            // Ignore when typing in inputs / selects / textareas
            const tag = (e.target as HTMLElement).tagName;
            if (['INPUT', 'TEXTAREA', 'SELECT'].includes(tag)) return;

            switch (e.key) {
                case '?':
                    e.preventDefault();
                    setHelpOpen(v => !v);
                    break;
                case 'Escape':
                    if (helpOpen) { setHelpOpen(false); break; }
                    if (peekOpen) { onClosePeek(); }
                    break;
                case 'l':
                case 'L':
                    e.preventDefault();
                    onGoLive();
                    break;
                case 'ArrowLeft':
                    e.preventDefault();
                    if (!isLive) onSeek(new Date(currentTime.getTime() - SEEK_DELTA_MS));
                    break;
                case 'ArrowRight':
                    e.preventDefault();
                    if (!isLive) {
                        const next = new Date(currentTime.getTime() + SEEK_DELTA_MS);
                        if (next <= new Date()) onSeek(next); else onGoLive();
                    }
                    break;
                case ' ':
                    e.preventDefault();
                    if (isLive) {
                        // Space during live → pause to now
                        onSeek(new Date());
                    } else {
                        // Space during playback → go live
                        onGoLive();
                    }
                    break;
            }
        },
        [onGoLive, onSeek, onClosePeek, isLive, currentTime, peekOpen, helpOpen],
    );

    useEffect(() => {
        window.addEventListener('keydown', handleKey);
        return () => window.removeEventListener('keydown', handleKey);
    }, [handleKey]);

    return { helpOpen, closeHelp: () => setHelpOpen(false) };
}
