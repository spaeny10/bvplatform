import { Page, Locator, expect, test } from '@playwright/test';

// Error-overlay headlines rendered by frontend/src/components/VideoPlayer.tsx.
// The HEVC one is the only acceptable overlay on an otherwise-healthy
// stream: the fleet records H.265 and the bundled Chromium has no HEVC
// decoder, so network Tier-1 can pass while decode fails.
export const HEVC_HEADLINE = 'Browser cannot decode this stream (HEVC support missing)';
export const ERROR_HEADLINES = [
    'Camera offline',
    'Not authorized to view this camera',
    'Stream not found',
    'Network error reaching camera',
    HEVC_HEADLINE,
    'Playback error in browser',
    'Stream unavailable',
];


export type LiveStatus = 'streaming' | 'decode-unavailable' | 'failed';

export interface LiveResult {
    cameraId: string;
    cameraName: string;
    status: LiveStatus;
    detail: string;
}

/**
 * Read the state of a .video-cell overlay.
 *  - null            -> no placeholder visible (video element is displayed)
 *  - 'Connecting...' -> still loading
 *  - anything else   -> an error headline
 */
async function overlayHeadline(cell: Locator): Promise<string | null> {
    const placeholder = cell.locator('.video-cell-placeholder');
    if ((await placeholder.count()) === 0) return null;
    if (!(await placeholder.first().isVisible().catch(() => false))) return null;
    // Error overlay renders headline + detail + camera name as separate
    // spans; the headline is the first span after the icon.
    const spans = placeholder.first().locator('span');
    const first = (await spans.first().textContent().catch(() => '')) ?? '';
    return first.trim();
}

/**
 * Live network recorder. Attach BEFORE navigation; HLS re-polls the media
 * playlist every targetDuration so late attachment still converges, but
 * early attachment catches the init segment too.
 */
export interface LiveTraffic {
    manifests: Set<string>;
    segments: Set<string>;
}

export function recordLiveTraffic(page: Page): LiveTraffic {
    const traffic: LiveTraffic = { manifests: new Set(), segments: new Set() };
    page.on('response', res => {
        if (res.status() !== 200) return;
        const url = res.url();
        const m = url.match(/\/api\/live\/([^/]+)\/[^?]*\.m3u8/);
        if (m) traffic.manifests.add(decodeURIComponent(m[1]));
        const s = url.match(/\/api\/live\/([^/]+)\/[^?]*\.(mp4|m4s)/);
        if (s) traffic.segments.add(decodeURIComponent(s[1]));
    });
    return traffic;
}

async function pollUntil(cond: () => Promise<boolean> | boolean, timeoutMs: number, stepMs = 500): Promise<boolean> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        if (await cond()) return true;
        await new Promise(r => setTimeout(r, stepMs));
    }
    return cond();
}

/**
 * Tier-2 decode check (only meaningful when the browser can decode HEVC):
 * video.currentTime must advance >=1s over a 6s window and readyState>=2.
 */
export async function assertDecodeProgress(cell: Locator): Promise<void> {
    const video = cell.locator('video');
    const t0 = await video.evaluate((v: HTMLVideoElement) => v.currentTime);
    await new Promise(r => setTimeout(r, 6000));
    const { t1, readyState } = await video.evaluate((v: HTMLVideoElement) => ({
        t1: v.currentTime,
        readyState: v.readyState,
    }));
    expect(t1 - t0, `video.currentTime should advance >=1s over 6s (was ${t0} -> ${t1})`).toBeGreaterThanOrEqual(1);
    expect(readyState, 'video.readyState should be >=2 (HAVE_CURRENT_DATA)').toBeGreaterThanOrEqual(2);
}

/**
 * Strict single-camera live assertion (popout etc.).
 *
 * Tier 1 (always): a 200 .m3u8 for this camera within 15s AND either a
 * 200 media segment within 20s or the EXACT HEVC-decode overlay (hls.js
 * aborts before segment fetch on manifestIncompatibleCodecs, so segment
 * absence + HEVC overlay is still a healthy server). Any other error
 * headline fails. Returns 'decode-unavailable' (and annotates) when the
 * HEVC overlay is shown.
 *
 * `traffic` MUST be a recorder attached before navigation: when the codec
 * is rejected, hls.js fetches index.m3u8 exactly once at mount, so a
 * waitForResponse attached after page load races (and loses).
 *
 * Tier 2 (IRONSIGHT_EXPECT_DECODE=1 only): playback actually advances.
 */
export async function assertLiveStream(
    page: Page,
    cell: Locator,
    cameraId: string,
    traffic: LiveTraffic,
): Promise<LiveStatus> {
    const gotManifest = await pollUntil(() => traffic.manifests.has(cameraId), 15_000, 250);
    if (!gotManifest) {
        throw new Error(`camera ${cameraId}: no 200 /api/live/.../*.m3u8 observed within 15s`);
    }

    const sawSegment = await pollUntil(() => traffic.segments.has(cameraId), 20_000, 250);

    // Give hls.js a moment to settle into its final state (overlay or video).
    await pollUntil(async () => {
        const h = await overlayHeadline(cell);
        return h === null || (h !== 'Connecting...' && h !== 'Loading...');
    }, 10_000);

    const headline = await overlayHeadline(cell);
    if (headline === null) {
        expect(sawSegment, 'no overlay shown but no media segment was fetched either').toBeTruthy();
        if (process.env.IRONSIGHT_EXPECT_DECODE === '1') {
            await assertDecodeProgress(cell);
        }
        return 'streaming';
    }
    if (headline === HEVC_HEADLINE) {
        if (process.env.IRONSIGHT_EXPECT_DECODE === '1') {
            throw new Error('IRONSIGHT_EXPECT_DECODE=1 but the HEVC-decode overlay is shown');
        }
        test.info().annotations.push({
            type: 'decode-unavailable',
            description: `camera ${cameraId}: Tier-1 network OK, browser lacks HEVC decode`,
        });
        return 'decode-unavailable';
    }
    throw new Error(`camera ${cameraId}: unexpected video overlay "${headline}"`);
}

/**
 * Grid-wide survey for the NVR page. Cellular cameras drop individually,
 * so the page passes when >=1 camera streams; every camera gets a
 * per-camera annotation either way.
 */
export async function surveyLiveGrid(
    page: Page,
    traffic: LiveTraffic,
    cameras: { id: string; name: string }[],
    timeoutMs = 30_000,
): Promise<LiveResult[]> {
    // Wait until at least one camera has manifest+segment, or the clock runs out
    // (decode-unavailable deployments may never fetch segments — handled below).
    await pollUntil(
        () => cameras.some(c => traffic.manifests.has(c.id) && traffic.segments.has(c.id)),
        timeoutMs,
    );
    // Let overlays settle.
    await pollUntil(async () => {
        const headlines = await allOverlayHeadlines(page);
        return headlines.every(h => h !== 'Connecting...' && h !== 'Loading...');
    }, 10_000);

    const overlays = await cellOverlaysByName(page);
    const results: LiveResult[] = [];
    for (const cam of cameras) {
        const manifest = traffic.manifests.has(cam.id);
        const segment = traffic.segments.has(cam.id);
        const headline = overlays.get(cam.name) ?? null;
        let status: LiveStatus;
        let detail: string;
        if (manifest && segment && (headline === null || headline === HEVC_HEADLINE)) {
            status = headline === HEVC_HEADLINE ? 'decode-unavailable' : 'streaming';
            detail = 'manifest 200, segment 200' + (headline ? ', HEVC overlay' : '');
        } else if (manifest && !segment && headline === HEVC_HEADLINE) {
            // hls.js bails on incompatible codecs before fetching segments.
            status = 'decode-unavailable';
            detail = 'manifest 200, no segment (codec rejected pre-fetch), HEVC overlay';
        } else {
            status = 'failed';
            detail = `manifest ${manifest ? '200' : 'missing'}, segment ${segment ? '200' : 'missing'}`
                + (headline ? `, overlay "${headline}"` : ', no overlay');
        }
        results.push({ cameraId: cam.id, cameraName: cam.name, status, detail });
        test.info().annotations.push({ type: `camera-${status}`, description: `${cam.name} (${cam.id}): ${detail}` });
    }
    return results;
}

async function allOverlayHeadlines(page: Page): Promise<string[]> {
    const cells = page.locator('.video-cell');
    const n = await cells.count();
    const out: string[] = [];
    for (let i = 0; i < n; i++) {
        const h = await overlayHeadline(cells.nth(i));
        if (h !== null) out.push(h);
    }
    return out;
}

/** Map of camera display name -> overlay headline (or null when video shows). */
async function cellOverlaysByName(page: Page): Promise<Map<string, string | null>> {
    const cells = page.locator('.video-cell');
    const n = await cells.count();
    const map = new Map<string, string | null>();
    for (let i = 0; i < n; i++) {
        const cell = cells.nth(i);
        const name = ((await cell.locator('.video-cell-name').first().textContent().catch(() => '')) ?? '').trim();
        map.set(name, await overlayHeadline(cell));
    }
    return map;
}
