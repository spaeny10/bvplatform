'use client';

// VCA snapshot loader hook extracted from VCAZoneEditor (P1-B-11 session 19).
// Tries the ONVIF snapshot endpoint first; falls back to grabbing the
// first frame of the sub-stream HLS playlist when ONVIF fails (some
// cameras don't expose ONVIF snapshot). Owns 2 useState slots
// (snapshotLoaded, snapshotError) and the imgRef the canvas reads from.
//
// Returns the ref + load flags + a re-trigger function the parent calls
// (e.g. on camera switch). The imgRef is exposed because the canvas
// renderer needs to read .current.naturalWidth/.naturalHeight directly
// during draw — accessing it through state would mean an extra render
// pass per snapshot load.

import { useCallback, useEffect, useRef, useState } from 'react';
import { getVCASnapshotURL } from '@/lib/api';
import { mintMediaToken } from '@/lib/media';

export interface VCASnapshot {
    imgRef: React.MutableRefObject<HTMLImageElement | null>;
    snapshotLoaded: boolean;
    snapshotError: string | null;
    reload: () => void;
}

export function useVCASnapshot(cameraId: string): VCASnapshot {
    const imgRef = useRef<HTMLImageElement | null>(null);
    const [snapshotLoaded, setSnapshotLoaded] = useState(false);
    const [snapshotError, setSnapshotError] = useState<string | null>(null);

    const loadSnapshot = useCallback(async () => {
        setSnapshotLoaded(false);
        setSnapshotError(null);

        // Attempt 1: ONVIF snapshot endpoint
        try {
            const res = await fetch(getVCASnapshotURL(cameraId), { credentials: 'include' });
            if (res.ok) {
                const blob = await res.blob();
                if (blob.size > 0) {
                    const url = URL.createObjectURL(blob);
                    const img = new Image();
                    img.onload = () => { imgRef.current = img; setSnapshotLoaded(true); };
                    img.onerror = () => { setSnapshotError('Snapshot image failed to decode'); setSnapshotLoaded(true); };
                    img.src = url;
                    return;
                }
            }
        } catch { /* try fallback */ }

        // Attempt 2: HLS live stream frame (grab first frame of the sub-stream m3u8).
        // P1-A-03: mint a signed /media/v1/<token> URL for the playlist. The
        // playlist's inner segment URIs are already self-signed by the
        // backend rewriter, so the <video> element follows them with no
        // extra plumbing here.
        try {
            const minted = await mintMediaToken({
                camera_id: cameraId, kind: 'hls', path: 'sub_live.m3u8',
            });
            const hlsUrl = minted.url;
            const res = await fetch(hlsUrl);
            if (res.ok) {
                const video = document.createElement('video');
                video.crossOrigin = 'anonymous';
                video.muted = true;
                video.playsInline = true;
                video.src = hlsUrl;
                video.currentTime = 0.1;
                await new Promise<void>((resolve, reject) => {
                    video.onloadeddata = () => resolve();
                    video.onerror = () => reject();
                    setTimeout(reject, 5000);
                });
                const canvas = document.createElement('canvas');
                canvas.width = video.videoWidth || 640;
                canvas.height = video.videoHeight || 360;
                canvas.getContext('2d')?.drawImage(video, 0, 0);
                const dataUrl = canvas.toDataURL('image/jpeg');
                const img = new Image();
                img.onload = () => { imgRef.current = img; setSnapshotLoaded(true); };
                img.src = dataUrl;
                video.pause();
                video.src = '';
                return;
            }
        } catch { /* proceed without snapshot */ }

        setSnapshotError('Could not load camera snapshot. Zones can still be drawn on the grid.');
        setSnapshotLoaded(true);
    }, [cameraId]);

    useEffect(() => { loadSnapshot(); }, [loadSnapshot]);

    return { imgRef, snapshotLoaded, snapshotError, reload: loadSnapshot };
}
