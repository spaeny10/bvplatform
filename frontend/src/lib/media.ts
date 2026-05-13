// P1-A-03 — media-token mint client.
//
// Before P1-A-03, recordings / live HLS / alarm snapshots were served by
// bare http.FileServer routes (/recordings/*, /hls/*, /snapshots/*) with
// no authentication. Any logged-in user — even a cross-tenant one —
// could fetch the bytes by guessing the camera UUID.
//
// Now every media fetch goes through /media/v1/<signed-token>. This
// module is the thin client: it asks the backend for a short-lived
// token, hands back the absolute URL, and (for long-running surfaces
// like a video player) auto-refreshes before expiry.
//
// Design parameters:
//
//   - Tokens default to a 5-minute TTL. The mint endpoint accepts an
//     optional ttl_seconds parameter capped server-side at 3600s for
//     evidence exports; everything in this module asks for the default.
//   - The refresher re-mints 60 seconds before expiry — leaves a
//     comfortable margin in case the user's connection is briefly slow.
//   - The legacy URL shapes (/snapshots/<cam>/<file>, /recordings/...)
//     are still emitted by some DB-stored fields (alarms.snapshot_url,
//     events.details.snapshot_url, etc.) for back-compat reasons. The
//     resolveMediaURL helper rewrites them transparently — UI code that
//     renders <img src> or <video src> from those fields just calls it
//     once and gets back a /media/v1/<token> URL it can use directly.

import { authFetch } from '@/lib/api';

export type MediaKind = 'segment' | 'hls' | 'snapshot';

export interface MintRequest {
    camera_id: string;
    kind: MediaKind;
    path: string;
    ttl_seconds?: number;
}

export interface MintResponse {
    url: string;
    expires_at: string;
}

/**
 * Ask the backend for a signed /media/v1/<token> URL. Returns the URL
 * + ISO expiry timestamp. Throws on any non-2xx response; callers
 * surface those as inline UI errors.
 */
export async function mintMediaToken(req: MintRequest): Promise<MintResponse> {
    const res = await authFetch('/api/media/mint', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
    });
    if (!res.ok) {
        throw new Error(`mint failed: ${res.status}`);
    }
    return res.json();
}

/**
 * Convenience for `<img>` / `<video>` consumers: given any URL shape
 * the backend may have stored (legacy `/snapshots/<cam>/<file>`,
 * legacy `/recordings/<cam>/<file>[#t=]`, or an already-signed
 * `/media/v1/<token>`), return a fetchable signed URL. Falls back to
 * the input URL untouched for shapes we don't recognise (e.g. full
 * https:// URLs to a CDN).
 *
 * NOTE: this preserves any `#t=` fragment on the legacy /recordings/
 * URLs, because the HTML5 video element honours it as an initial
 * seek hint.
 */
export async function resolveMediaURL(rawURL: string | null | undefined): Promise<string> {
    if (!rawURL) return '';
    if (rawURL.startsWith('/media/v1/')) return rawURL;
    if (rawURL.startsWith('http://') || rawURL.startsWith('https://')) return rawURL;

    // Legacy /snapshots/<cam>/<file>
    const snapMatch = rawURL.match(/^\/snapshots\/([^/]+)\/([^/?#]+)$/);
    if (snapMatch) {
        const [, cam, file] = snapMatch;
        try {
            const { url } = await mintMediaToken({ camera_id: cam, kind: 'snapshot', path: file });
            return url;
        } catch {
            return ''; // signal the UI to drop the <img>
        }
    }

    // Legacy /recordings/<cam>/<file>[#t=...]
    const recMatch = rawURL.match(/^\/recordings\/([^/]+)\/([^/?#]+)(#.*)?$/);
    if (recMatch) {
        const [, cam, file, frag = ''] = recMatch;
        try {
            const { url } = await mintMediaToken({ camera_id: cam, kind: 'segment', path: file });
            return url + frag;
        } catch {
            return '';
        }
    }

    // Legacy /hls/<cam>/<file>
    const hlsMatch = rawURL.match(/^\/hls\/([^/]+)\/([^/?#]+)$/);
    if (hlsMatch) {
        const [, cam, file] = hlsMatch;
        try {
            const { url } = await mintMediaToken({ camera_id: cam, kind: 'hls', path: file });
            return url;
        } catch {
            return '';
        }
    }

    return rawURL; // unknown shape — let the caller deal with it
}

/**
 * Long-running surfaces (live HLS player, multi-camera grid) need
 * fresh tokens on a timer. createMediaRefresher returns a small
 * object that re-mints every (ttl - 60s) and invokes onRefresh
 * with the new URL. Call dispose() to stop the timer.
 *
 * Use it like:
 *   const ref = createMediaRefresher(
 *     { camera_id, kind: 'hls', path: 'main_live.m3u8' },
 *     (url) => { player.attachSource(url); },
 *   );
 *   await ref.start();
 *   // ... later, when the component unmounts
 *   ref.dispose();
 */
export function createMediaRefresher(
    req: MintRequest,
    onRefresh: (url: string) => void,
) {
    let timer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;

    const tick = async () => {
        if (disposed) return;
        try {
            const r = await mintMediaToken(req);
            if (disposed) return;
            onRefresh(r.url);
            // Schedule the next mint 60s before the token expires.
            // Token TTL is 5 min by default → refresh every 4 min.
            const expiresAtMs = Date.parse(r.expires_at);
            const delay = Math.max(30_000, expiresAtMs - Date.now() - 60_000);
            timer = setTimeout(tick, delay);
        } catch {
            // On failure, retry after 10 s. Don't surface to the UI
            // here — the caller's <video> error handler will report
            // the playback failure if it persists.
            if (!disposed) {
                timer = setTimeout(tick, 10_000);
            }
        }
    };

    return {
        start: tick,
        dispose() {
            disposed = true;
            if (timer) {
                clearTimeout(timer);
                timer = null;
            }
        },
    };
}
