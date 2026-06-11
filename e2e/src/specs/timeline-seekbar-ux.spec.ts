import { test, expect } from '../fixtures';
import { authFile } from '../helpers/auth';

test.use({ storageState: authFile('admin') });

// ─────────────────────────────────────────────────────────────────────────
// Proof for feat/timeline-seekbar-ux. Verifies the four transport/timeline
// upgrades against the bob test deploy (through the tunnel):
//
//   1. Zoom-aware tick ruler renders multiple labelled ticks AND the labels
//      CHANGE when the window is zoomed.
//   2. An event marker shows a tooltip on hover and clicking it seeks the
//      timeline (drops out of live → playback, timestamp updates).
//   3. The playback speed control sets video.playbackRate (read back: 2× →
//      playbackRate === 2) and is HIDDEN while live.
//   4. The ±10s/±30s skip buttons move the playback time by ~that amount and
//      the frame-step button nudges video.currentTime.
//
// Seeds a single-camera layout for a camera that carries seeded events
// ("504 right ptz" in the test DB — same one timeline-layout-scope.spec uses)
// so event markers are present to hover/click.
// ─────────────────────────────────────────────────────────────────────────

interface Cam { id: string; name: string; }

function staticLayout(name: string, cameraIds: string[]) {
    const presets = [
        { w: 1, h: 1 }, { w: 2, h: 1 }, { w: 2, h: 2 }, { w: 3, h: 2 },
        { w: 3, h: 3 }, { w: 4, h: 3 }, { w: 4, h: 4 },
    ];
    const preset = presets.find(p => p.w * p.h >= cameraIds.length) ?? presets[presets.length - 1];
    const staticAssignments: Record<number, string> = {};
    cameraIds.forEach((id, i) => { staticAssignments[i] = id; });
    return { name, items: [], cols: preset.w, version: 3, mode: 'static', staticPreset: preset, staticAssignments };
}

/** Parse the displayed playback timestamp (e.g. "Jun 11, 04:12:07 PM") to ms,
 *  anchoring on today so only the time-of-day matters for delta checks. */
function parseDisplayedTime(text: string): number | null {
    // The button renders toLocaleString month/day + h:m:s. Extract h:m:s and
    // an am/pm if present; compute seconds-of-day (sufficient for ~10-30s
    // delta assertions that never straddle a day boundary in this window).
    const m = text.match(/(\d{1,2}):(\d{2}):(\d{2})\s*([AP]M)?/i);
    if (!m) return null;
    let h = Number(m[1]);
    const min = Number(m[2]);
    const s = Number(m[3]);
    const ap = m[4]?.toUpperCase();
    if (ap === 'PM' && h < 12) h += 12;
    if (ap === 'AM' && h === 12) h = 0;
    return ((h * 60 + min) * 60 + s) * 1000;
}

test.describe('Timeline + seek-bar UX @core', () => {
    test('ruler adapts on zoom; marker tooltip+seek; speed sets playbackRate (hidden live); fine skip + frame-step', async ({ page }) => {
        test.setTimeout(120_000);

        // 1. Resolve a camera with seeded events from the live inventory.
        const res = await page.request.get('/api/cameras');
        expect(res.ok(), `GET /api/cameras -> ${res.status()}`).toBeTruthy();
        const cameras = (await res.json()) as Cam[];
        const byName = (needle: string) =>
            cameras.find(c => c.name.toLowerCase() === needle.toLowerCase())
            ?? cameras.find(c => c.name.toLowerCase().includes(needle.toLowerCase()));
        const cam = byName('504 right ptz') ?? byName('504') ?? cameras[0];
        expect(cam, `no usable camera in inventory: ${cameras.map(c => c.name).join(', ')}`).toBeTruthy();

        // 2. Seed a 1-cam layout so the timeline scopes to a camera with events.
        const layouts = [staticLayout('e2e-seekbar', [cam.id])];
        await page.addInitScript(([ls, active]) => {
            localStorage.setItem('ironsight-layouts', ls as string);
            localStorage.setItem('ironsight-active-layout', active as string);
        }, [JSON.stringify(layouts), 'e2e-seekbar'] as const);

        await page.goto('/');
        await expect(page.locator('.video-cell').first()).toBeVisible({ timeout: 20_000 });
        await expect(page.locator('.timeline-container')).toBeVisible({ timeout: 15_000 });

        // ── ASSERT 3a: speed control is HIDDEN while live ──────────────────
        // Page boots in live mode (● LIVE chip / no playback yet).
        await expect(page.locator('.speed-control')).toHaveCount(0);
        test.info().annotations.push({ type: 'speed-hidden-live', description: '.speed-control absent in live mode' });

        // ── Enter PLAYBACK by clicking the timeline track (seeks to center) ─
        const track = page.locator('.timeline-track');
        await track.click({ position: { x: 200, y: 24 } });
        // Speed control should now be present (playback mode).
        await expect(page.locator('.speed-control')).toBeVisible({ timeout: 10_000 });

        // ── ASSERT 1: tick ruler renders multiple labels AND adapts on zoom ─
        const labels = page.locator('.timeline-ruler-labels .ruler-label');
        await expect.poll(() => labels.count(), { timeout: 10_000, message: 'no ruler labels rendered' })
            .toBeGreaterThan(2);
        const before = (await labels.allTextContents()).filter(Boolean);
        const beforeCount = before.length;

        // Zoom IN several steps (shorter window → finer ticks). The "+" button
        // lives in .zoom-controls. Read the zoom label so we know it changed.
        const zoomLabel = page.locator('.zoom-label');
        const zoomBefore = (await zoomLabel.textContent())?.trim();
        const zoomInBtn = page.locator('.zoom-controls button').first();
        for (let i = 0; i < 5; i++) await zoomInBtn.click();
        const zoomAfter = (await zoomLabel.textContent())?.trim();
        expect(zoomAfter, `zoom label should change after zooming (was ${zoomBefore})`).not.toBe(zoomBefore);

        // Labels must change after the zoom (different interval → different set).
        await expect.poll(async () => {
            const now = (await labels.allTextContents()).filter(Boolean);
            return JSON.stringify(now) !== JSON.stringify(before) && now.length > 0;
        }, { timeout: 10_000, message: 'ruler labels did not change after zoom' }).toBeTruthy();
        const after = (await labels.allTextContents()).filter(Boolean);
        test.info().annotations.push({
            type: 'ruler-adapts',
            description: `zoom ${zoomBefore}→${zoomAfter}; labels ${beforeCount} [${before.slice(0, 3).join(', ')}…] → ${after.length} [${after.slice(0, 3).join(', ')}…]`,
        });

        // Zoom back out to a window where the seeded events are visible as
        // markers (1h default region) so we can hover/click one.
        const zoomOutBtn = page.locator('.zoom-controls button').last();
        for (let i = 0; i < 5; i++) await zoomOutBtn.click();

        // ── ASSERT 2: event marker tooltip + click-seek ───────────────────
        const markers = page.locator('.timeline-marker');
        await expect.poll(() => markers.count(), { timeout: 15_000, message: 'no event markers rendered (seeded events expected for this camera)' })
            .toBeGreaterThan(0);
        const marker = markers.first();
        await marker.hover();
        await expect(page.locator('.timeline-marker-tooltip')).toBeVisible({ timeout: 5_000 });
        const tipText = (await page.locator('.timeline-marker-tooltip').textContent())?.trim();
        test.info().annotations.push({ type: 'marker-tooltip', description: `tooltip: "${tipText}"` });

        // Click the marker → it should seek the timeline to that event's time.
        const tsDisplay = page.locator('.timeline-time-display');
        const tsBeforeMarker = (await tsDisplay.textContent())?.trim() ?? '';
        await marker.click();
        // Seeking updates the displayed playback timestamp.
        await expect.poll(async () => ((await tsDisplay.textContent())?.trim() ?? ''), {
            timeout: 10_000, message: 'clicking a marker did not change the playback timestamp',
        }).not.toBe(tsBeforeMarker);
        const tsAfterMarker = (await tsDisplay.textContent())?.trim() ?? '';
        test.info().annotations.push({ type: 'marker-seek', description: `marker click moved timestamp ${tsBeforeMarker} → ${tsAfterMarker}` });

        // ── ASSERT 3b: speed control sets video.playbackRate ──────────────
        const video = page.locator('.video-cell video').first();
        // Let any in-flight segment load (kicked off by the marker seek above)
        // settle before changing speed — a video.load() resets playbackRate to
        // 1, and we want to assert the operator's selection STICKS past that.
        await video.evaluate((v: HTMLVideoElement) => new Promise<void>(r => setTimeout(r, 1500)));
        await page.locator('.speed-btn', { hasText: '2×' }).click();
        await expect(page.locator('.speed-btn.active', { hasText: '2×' })).toBeVisible();
        await expect.poll(
            () => video.evaluate((v: HTMLVideoElement) => v.playbackRate),
            { timeout: 10_000, message: 'video.playbackRate did not become 2 after selecting 2×' },
        ).toBe(2);
        // And back to 1× for cleanliness / to prove the control is bidirectional.
        await page.locator('.speed-btn', { hasText: '1×' }).click();
        await expect.poll(() => video.evaluate((v: HTMLVideoElement) => v.playbackRate), { timeout: 5_000 }).toBe(1);
        test.info().annotations.push({ type: 'playbackRate', description: '2× → video.playbackRate===2; 1× → 1' });

        // ── ASSERT 4a: ±30s / ±10s skip moves playback time by ~that much ──
        const tsBeforeSkip = parseDisplayedTime((await tsDisplay.textContent())?.trim() ?? '');
        await page.locator('.transport-btn-sec', { hasText: '-30s' }).click();
        await expect.poll(async () => {
            const after = parseDisplayedTime((await tsDisplay.textContent())?.trim() ?? '');
            if (tsBeforeSkip === null || after === null) return -1;
            return Math.round((tsBeforeSkip - after) / 1000);
        }, { timeout: 10_000, message: '-30s did not move the playback timestamp back ~30s' }).toBeGreaterThanOrEqual(25);
        test.info().annotations.push({ type: 'skip-30s', description: '-30s moved playback time back ~30s' });

        const tsBefore10 = parseDisplayedTime((await tsDisplay.textContent())?.trim() ?? '');
        await page.locator('.transport-btn-sec', { hasText: '+10s' }).click();
        await expect.poll(async () => {
            const after = parseDisplayedTime((await tsDisplay.textContent())?.trim() ?? '');
            if (tsBefore10 === null || after === null) return -999;
            return Math.round((after - tsBefore10) / 1000);
        }, { timeout: 10_000, message: '+10s did not move the playback timestamp forward ~10s' }).toBeGreaterThanOrEqual(8);
        test.info().annotations.push({ type: 'skip-10s', description: '+10s moved playback time forward ~10s' });

        // ── ASSERT 4b: frame-step nudges video.currentTime ────────────────
        // Frame-step only does anything if a segment actually loaded (footage
        // present). Detect that; if no footage is loadable through the tunnel
        // we record it honestly rather than assert visible motion.
        const ct0 = await video.evaluate((v: HTMLVideoElement) => v.currentTime);
        const hasFootage = await video.evaluate((v: HTMLVideoElement) => v.readyState >= 1 && isFinite(v.duration) && v.duration > 0);
        const frameFwd = page.locator('.transport-btn-frame', { hasText: '|▶' });
        await expect(frameFwd).toBeVisible(); // frame-step button is present in playback
        if (hasFootage) {
            await frameFwd.click();
            await expect.poll(
                () => video.evaluate((v: HTMLVideoElement) => v.currentTime),
                { timeout: 5_000, message: 'frame-step did not advance video.currentTime' },
            ).toBeGreaterThan(ct0);
            test.info().annotations.push({ type: 'frame-step', description: `currentTime advanced ${ct0} → (after |▶)` });
        } else {
            test.info().annotations.push({
                type: 'frame-step-unverified',
                description: 'no loadable segment through tunnel (video.duration not finite); frame-step BUTTON present + wired, but currentTime motion not asserted',
            });
        }
    });
});
