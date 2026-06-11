import { test, expect } from '../fixtures';
import { authFile } from '../helpers/auth';

test.use({ storageState: authFile('admin') });

// ─────────────────────────────────────────────────────────────────────────
// Proof for fix/timeline-cross-camera-leak (active-layout scoping).
//
// Before the fix the playback timeline queried ALL loaded cameras, so a
// layout containing only the 5001 cameras still showed camera 504's events.
// After the fix the timeline scopes to the cameras in the ACTIVE grid layout.
//
// This spec seeds two static layouts in localStorage (CameraGrid reads
// `ironsight-layouts` / `ironsight-active-layout`), captures every
// GET /api/timeline request, and asserts:
//   1. with the 5001-only layout active, camera_ids == exactly the 5001
//      UUIDs and the timeline returns 0 buckets (5001 has no events);
//   2. after switching to the 504 layout, the next timeline request carries
//      the 504 camera UUID and returns >0 buckets (504 has events).
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

/** Parse the camera_ids query param of a /api/timeline URL into a sorted array. */
function timelineCameraIds(url: string): string[] | null {
    const u = new URL(url);
    const raw = u.searchParams.get('camera_ids');
    if (raw === null) return null; // no filter at all (all cameras)
    return raw.split(',').filter(Boolean).sort();
}

test.describe('Timeline scopes to the active grid layout @core', () => {
    test('5001 layout queries only 5001 cameras (no 504 leak); 504 layout queries 504', async ({ page }) => {
        test.setTimeout(120_000);

        // 1. Resolve the real camera UUIDs from the live inventory by name so
        //    the test is not pinned to hard-coded ids.
        const res = await page.request.get('/api/cameras');
        expect(res.ok(), `GET /api/cameras -> ${res.status()}`).toBeTruthy();
        const cameras = (await res.json()) as Cam[];
        const byName = (needle: string) =>
            cameras.find(c => c.name.toLowerCase() === needle.toLowerCase())
            ?? cameras.find(c => c.name.toLowerCase().includes(needle.toLowerCase()));

        const c5001front = byName('5001 front');
        const c5001back = byName('5001 back');
        // 504 layout: include a 504 camera that actually has events so the
        // "504 timeline shows events" half is meaningful. From the test DB,
        // "504 right ptz" carries all the seeded events.
        const c504events = byName('504 right ptz') ?? byName('504');
        expect(c5001front && c5001back && c504events,
            `expected 5001 front/back + a 504 camera in inventory, got: ${cameras.map(c => c.name).join(', ')}`,
        ).toBeTruthy();

        const ids5001 = [c5001front!.id, c5001back!.id].sort();
        const ids504 = [c504events!.id].sort();
        test.info().annotations.push({ type: 'cams-5001', description: ids5001.join(',') });
        test.info().annotations.push({ type: 'cams-504', description: ids504.join(',') });

        // 2. Seed both layouts before any page script runs; 5001 active first.
        const layouts = [
            staticLayout('e2e-5001-only', ids5001),
            staticLayout('e2e-504', ids504),
        ];
        await page.addInitScript(([ls, active]) => {
            localStorage.setItem('ironsight-layouts', ls as string);
            localStorage.setItem('ironsight-active-layout', active as string);
        }, [JSON.stringify(layouts), 'e2e-5001-only'] as const);

        // 3. Record every /api/timeline request URL + its response body.
        const timelineCalls: { ids: string[] | null; bucketCount: number; url: string }[] = [];
        page.on('response', async (resp) => {
            const url = resp.url();
            if (!/\/api\/timeline\?/.test(url)) return;
            let bucketCount = -1;
            try {
                const body = await resp.json();
                // TimelineBucket = { bucket_time, counts, total } — sum the
                // per-bucket totals to get the event count in the window.
                bucketCount = Array.isArray(body)
                    ? body.reduce((sum: number, b: any) => sum + (Number(b.total) || 0), 0)
                    : -1;
            } catch { /* non-JSON / aborted */ }
            timelineCalls.push({ ids: timelineCameraIds(url), bucketCount, url });
        });

        await page.goto('/');
        // The static grid renders one cell per assigned camera.
        await expect(page.locator('.video-cell').first()).toBeVisible({ timeout: 20_000 });
        await expect(page.locator('.timeline-container')).toBeVisible({ timeout: 15_000 });

        // 4. Wait for a timeline request that carries the 5001 scope.
        await expect.poll(
            () => timelineCalls.some(c => c.ids && c.ids.length === 2
                && c.ids[0] === ids5001[0] && c.ids[1] === ids5001[1]),
            { timeout: 30_000, message: `no 5001-scoped /api/timeline seen. Calls: ${JSON.stringify(timelineCalls.map(c => c.ids))}` },
        ).toBeTruthy();

        const call5001 = [...timelineCalls].reverse().find(c => c.ids && c.ids.length === 2);
        expect(call5001, 'a 2-camera (5001) timeline call').toBeTruthy();
        // EXACTLY the two 5001 UUIDs — nothing else leaked in.
        expect(call5001!.ids).toEqual(ids5001);
        // 5001 has no events, so the timeline must come back empty.
        expect(call5001!.bucketCount, `5001 timeline must have 0 events (got ${call5001!.bucketCount})`).toBe(0);

        // CRITICAL anti-leak assertion: NO 5001-active timeline request may
        // carry the 504 camera id.
        const leaked = timelineCalls.filter(c => c.ids && c.ids.includes(c504events!.id));
        expect(leaked.length,
            `504 camera id must NOT appear in a 5001-layout timeline request (leaks: ${JSON.stringify(leaked.map(l => l.ids))})`,
        ).toBe(0);

        // 5. Switch to the 504 layout via its toolbar chip.
        const before = timelineCalls.length;
        await page.getByRole('button', { name: /e2e-504/ }).click();

        await expect.poll(
            () => timelineCalls.slice(before).some(c => c.ids && c.ids.length === 1 && c.ids[0] === ids504[0]),
            { timeout: 30_000, message: `no 504-scoped /api/timeline after switch. Calls: ${JSON.stringify(timelineCalls.slice(before).map(c => c.ids))}` },
        ).toBeTruthy();

        const call504 = [...timelineCalls].reverse().find(c => c.ids && c.ids.length === 1 && c.ids[0] === ids504[0]);
        expect(call504, 'a 504-scoped timeline call after switching layout').toBeTruthy();
        expect(call504!.ids).toEqual(ids504);
        // 504 (right ptz) has events in the test DB.
        expect(call504!.bucketCount, `504 timeline must have >0 events (got ${call504!.bucketCount})`).toBeGreaterThan(0);

        test.info().annotations.push({
            type: 'proof',
            description: `5001 call ids=${JSON.stringify(call5001!.ids)} buckets=${call5001!.bucketCount}; `
                + `504 call ids=${JSON.stringify(call504!.ids)} buckets=${call504!.bucketCount}`,
        });
    });
});
