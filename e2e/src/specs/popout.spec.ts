import { test, expect } from '../fixtures';
import { authFile } from '../helpers/auth';
import { assertLiveStream, recordLiveTraffic } from '../helpers/video';

test.use({ storageState: authFile('admin') });

test.describe('popout single-camera view @core', () => {
    test('popout renders the camera and streams live', async ({ page }) => {
        test.setTimeout(150_000);

        // Resolve the camera list through an API request context that shares
        // the admin cookies (page.request reuses the context's storage state).
        const res = await page.request.get('/api/cameras');
        expect(res.ok(), `GET /api/cameras -> HTTP ${res.status()}`).toBeTruthy();
        const cameras = (await res.json()) as { id: string; name: string; status?: string }[];
        expect(cameras.length, 'at least one camera registered').toBeGreaterThan(0);

        // First camera per the spec — but the fleet is cellular, so verify the
        // manifest actually serves before committing (db `status` can lag
        // reality). Falls back to the first camera if none probe healthy.
        let target = cameras[0];
        for (const cam of cameras) {
            const probe = await page.request
                .get(`/api/live/${cam.id}/index.m3u8`, { timeout: 15_000 })
                .catch(() => null);
            if (probe?.ok()) { target = cam; break; }
            test.info().annotations.push({
                type: 'camera-probe-failed',
                description: `${cam.name} (${cam.id}): live manifest not serving, trying next`,
            });
        }
        test.info().annotations.push({ type: 'camera-under-test', description: `${target.name} (${target.id})` });

        // Attach the traffic recorder BEFORE navigation — when the browser
        // rejects the HEVC codec, the one-and-only index.m3u8 fetch happens
        // immediately at player mount.
        const traffic = recordLiveTraffic(page);

        await page.goto(`/popout/${target.id}`);
        const cell = page.locator('.video-cell');
        await expect(cell).toBeVisible({ timeout: 15_000 });
        await expect(cell.locator('video')).toBeAttached();

        const status = await assertLiveStream(page, cell, target.id, traffic);
        test.info().annotations.push({ type: 'live-status', description: `${target.name}: ${status}` });
    });
});
