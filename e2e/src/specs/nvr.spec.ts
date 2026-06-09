import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { recordLiveTraffic, surveyLiveGrid } from '../helpers/video';

test.use({ storageState: authFile('admin') });

// Camera layouts are purely client-side (localStorage `ironsight-layouts` /
// `ironsight-active-layout`, see frontend/src/components/CameraGrid.tsx).
// A fresh browser context therefore lands on the "Create a Layout" empty
// state with zero .video-cell elements. Pre-seed a static layout assigning
// every camera to a slot so the grid renders deterministically.
function smokeLayout(cameraIds: string[]) {
    const presets = [
        { w: 1, h: 1 }, { w: 2, h: 1 }, { w: 2, h: 2 }, { w: 3, h: 2 },
        { w: 3, h: 3 }, { w: 4, h: 3 }, { w: 4, h: 4 },
    ];
    const preset = presets.find(p => p.w * p.h >= cameraIds.length) ?? presets[presets.length - 1];
    const staticAssignments: Record<number, string> = {};
    cameraIds.forEach((id, i) => { staticAssignments[i] = id; });
    return [{
        name: 'e2e-smoke',
        items: [],
        cols: preset.w,
        version: 3,
        mode: 'static',
        staticPreset: preset,
        staticAssignments,
    }];
}

test.describe('NVR live grid @core', () => {
    test('grid renders and at least one camera streams', async ({ page }) => {
        test.setTimeout(150_000);

        // Camera inventory first (request context shares the admin cookies).
        const camerasRes = await page.request.get('/api/cameras');
        expect(camerasRes.ok(), `GET /api/cameras -> HTTP ${camerasRes.status()}`).toBeTruthy();
        const cameras = (await camerasRes.json()) as { id: string; name: string; status?: string }[];
        expect(cameras.length, 'at least one camera registered').toBeGreaterThan(0);

        // Seed the layout before any page script runs.
        await page.addInitScript(([layouts, active]) => {
            localStorage.setItem('ironsight-layouts', layouts);
            localStorage.setItem('ironsight-active-layout', active);
        }, [JSON.stringify(smokeLayout(cameras.map(c => c.id))), 'e2e-smoke'] as const);

        // Attach the live-traffic recorder before navigation so the very
        // first manifest/init fetches are captured.
        const traffic = recordLiveTraffic(page);

        await page.goto('/');
        await expect(page.locator('.video-cell').first()).toBeVisible({ timeout: 20_000 });

        // Timeline transport bar is part of the core NVR surface.
        await expect(page.locator('.timeline-container')).toBeVisible({ timeout: 15_000 });

        // Per-camera live results; cellular cameras drop individually so the
        // page passes when >=1 camera streams (each camera gets an annotation).
        const results = await surveyLiveGrid(page, traffic, cameras, 30_000);
        const healthy = results.filter(r => r.status !== 'failed');
        expect(
            healthy.length,
            `>=1 camera must stream. Results: ${results.map(r => `${r.cameraName}=${r.status}`).join(', ')}`,
        ).toBeGreaterThan(0);

        await expectNoErrorBoundary(page);
    });
});
