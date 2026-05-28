// Wall-clock formatting (P1-B-11 session 6). Extracted from VideoPlayer
// where it formats per-frame timestamps for the multi-camera-sync
// overlay (LOCAL-11 follow-up — see
// ironsight/feature-requests/multi-camera-sync-indicator.md). Centiseconds
// rather than milliseconds because the latter jitters visibly each
// timeupdate tick.
//
// Timezone v0 uses the browser's local zone; the spec calls for a future
// site-timezone-aware variant, but a single helper makes that easy to swap.

/** Format absolute time = (segStartMs + offsetSec) as HH:MM:SS.cc in the browser's local zone. */
export function formatWallClock(segStartMs: number, offsetSec: number): string {
    const d = new Date(segStartMs + offsetSec * 1000);
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    const ss = String(d.getSeconds()).padStart(2, '0');
    const cs = String(Math.floor(d.getMilliseconds() / 10)).padStart(2, '0');
    return `${hh}:${mm}:${ss}.${cs}`;
}
