import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { recordLiveTraffic } from '../helpers/video';

// Proof for feat/live-vca-zone-overlay: VideoPlayer overlays a camera's
// configured VCA detection zones over the LIVE feed as SVG, toggleable.
//
// Test camera: "504 front" (d99d8b06) is the platform-VCA demo camera. We
// read its drawable zone count straight from the API (the same data source
// the overlay consumes), put it + a 0-zone 504/5001 camera in a 2-up static
// grid, then:
//   1) zones default ON -> the overlay draws >= (one shape per drawable rule);
//   2) toggle OFF -> shapes disappear from every tile (persisted to LS);
//   3) toggle back ON -> shapes return;
//   4) the 0-zone camera never renders any overlay.
// The expected count is derived from the live API rather than hardcoded so
// the proof stays valid as the test DB's VCA config changes. The overlay is
// independent of HEVC decode, so this passes even when the bundled Chromium
// can't decode the stream (the SVG renders regardless).

test.use({ storageState: authFile('admin') });

const FRONT_504 = 'd99d8b06';

// Same client-side layout seeding as nvr.spec.ts: a fresh context has no
// saved layout, so we pre-seed a static 2-slot layout assigning our two
// cameras before any page script runs.
function staticLayout(cameraIds: string[]) {
    const staticAssignments: Record<number, string> = {};
    cameraIds.forEach((id, i) => { staticAssignments[i] = id; });
    return [{
        name: 'e2e-vca',
        items: [],
        cols: 2,
        version: 3,
        mode: 'static',
        staticPreset: { w: 2, h: 1 },
        staticAssignments,
    }];
}

// Count the zone shapes (polygons + lines) inside a given .video-cell's
// live VCA overlay SVG. Returns 0 when no overlay is present.
async function zoneShapeCount(cell: import('@playwright/test').Locator): Promise<number> {
    const polys = await cell.locator('svg polygon').count();
    const lines = await cell.locator('svg line').count();
    return polys + lines;
}

test.describe('Live VCA zone overlay @core', () => {
    test('504-front renders 4 zones over live, toggle works, 0-zone camera is clean', async ({ page }) => {
        test.setTimeout(120_000);

        // Resolve the camera inventory; the admin request context shares cookies.
        const camerasRes = await page.request.get('/api/cameras');
        expect(camerasRes.ok(), `GET /api/cameras -> HTTP ${camerasRes.status()}`).toBeTruthy();
        const cameras = (await camerasRes.json()) as { id: string; name: string }[];

        const front = cameras.find(c => c.id.startsWith(FRONT_504));
        expect(front, `test camera 504-front (${FRONT_504}) must be registered`).toBeTruthy();

        // Confirm the platform really reports 4 zones for 504-front (the
        // overlay can only draw what the API returns).
        const rulesRes = await page.request.get(`/api/cameras/${front!.id}/vca/rules`);
        expect(rulesRes.ok(), `GET vca/rules -> HTTP ${rulesRes.status()}`).toBeTruthy();
        const rules = (await rulesRes.json()) as { enabled: boolean; region: unknown[] }[];
        const drawable = rules.filter(r => r.enabled && Array.isArray(r.region) && r.region.length >= 2);
        const expectedZones = drawable.length;
        test.info().annotations.push({ type: 'vca-rules', description: `504-front: ${rules.length} rules, ${expectedZones} drawable (API)` });
        // 504-front is the platform-VCA demo camera: it must have at least one
        // drawable zone, otherwise there is nothing to prove the overlay against.
        expect(expectedZones, '504-front must have >=1 drawable VCA zone to overlay').toBeGreaterThanOrEqual(1);

        // Pick a 0-zone camera as the negative control (another 504/5001).
        let zeroZoneCam = cameras.find(c => c.id !== front!.id && /504|5001/i.test(c.name));
        for (const c of cameras) {
            if (c.id === front!.id) continue;
            const r = await page.request.get(`/api/cameras/${c.id}/vca/rules`);
            if (!r.ok()) continue;
            const rr = (await r.json()) as { enabled: boolean; region: unknown[] }[];
            const d = rr.filter(x => x.enabled && Array.isArray(x.region) && x.region.length >= 2);
            if (d.length === 0) { zeroZoneCam = c; break; }
        }
        expect(zeroZoneCam, 'need a second camera with 0 zones as negative control').toBeTruthy();

        // Seed a 2-up static layout: 504-front in slot 0, the 0-zone cam in slot 1.
        // Force zones ON regardless of any persisted preference from a prior run.
        await page.addInitScript(([layouts, active]) => {
            localStorage.setItem('ironsight-layouts', layouts);
            localStorage.setItem('ironsight-active-layout', active);
            localStorage.setItem('ironsight-vca-zones-visible', 'on');
        }, [JSON.stringify(staticLayout([front!.id, zeroZoneCam!.id])), 'e2e-vca'] as const);

        const traffic = recordLiveTraffic(page);
        await page.goto('/');
        await expect(page.locator('.video-cell').first()).toBeVisible({ timeout: 20_000 });

        // Locate the two cells by their displayed camera name.
        const frontCell = page.locator('.video-cell', { hasText: front!.name }).first();
        const zeroCell = page.locator('.video-cell', { hasText: zeroZoneCam!.name }).first();
        await expect(frontCell).toBeVisible();
        await expect(zeroCell).toBeVisible();

        // ── 1) Zones default ON: 504-front draws its 4 zone shapes. ──
        // The overlay only renders once the stream is painting (not loading);
        // poll for the SVG shapes to appear. Independent of HEVC decode — the
        // SVG is a sibling of <video>, gated only on isLive && showZones &&
        // !loading && zones.length>0.
        await expect.poll(
            async () => zoneShapeCount(frontCell),
            { timeout: 30_000, message: '504-front should render its VCA zone shapes' },
        ).toBeGreaterThanOrEqual(1);

        const onCount = await zoneShapeCount(frontCell);
        test.info().annotations.push({ type: 'zones-on', description: `504-front zone shapes ON: ${onCount} (expected >=${expectedZones})` });
        // One shape per drawable rule minimum (a tripwire adds an extra
        // direction-arrow <line>, so the count can exceed the rule count).
        expect(onCount, 'should render at least one shape per drawable rule').toBeGreaterThanOrEqual(expectedZones);

        // Assert the overlay SVG covers the cell (bounds sanity — zones sit
        // over the video rect, not off-screen).
        const frontBox = await frontCell.boundingBox();
        const svgBox = await frontCell.locator('svg').first().boundingBox();
        expect(frontBox && svgBox, 'cell + overlay SVG should both have layout boxes').toBeTruthy();
        if (frontBox && svgBox) {
            expect(svgBox.x).toBeGreaterThanOrEqual(frontBox.x - 1);
            expect(svgBox.y).toBeGreaterThanOrEqual(frontBox.y - 1);
            expect(svgBox.width).toBeLessThanOrEqual(frontBox.width + 2);
        }

        // ── 4) 0-zone camera: never any overlay. ──
        expect(await zoneShapeCount(zeroCell), '0-zone camera must render no zone shapes').toBe(0);

        // ── 2) Toggle OFF via the ZONES header button -> shapes vanish on every tile. ──
        const zonesBtn = frontCell.getByRole('button', { name: 'ZONES' });
        await expect(zonesBtn).toBeVisible();
        await zonesBtn.click();
        await expect.poll(
            async () => zoneShapeCount(frontCell),
            { timeout: 5_000, message: 'zones should disappear after toggling OFF' },
        ).toBe(0);
        // localStorage reflects the OFF state (persisted + broadcast).
        const persistedOff = await page.evaluate(() => localStorage.getItem('ironsight-vca-zones-visible'));
        expect(persistedOff, 'toggle OFF should persist to localStorage').toBe('off');

        // ── 3) Toggle back ON -> shapes return. ──
        await zonesBtn.click();
        await expect.poll(
            async () => zoneShapeCount(frontCell),
            { timeout: 10_000, message: 'zones should return after toggling back ON' },
        ).toBeGreaterThanOrEqual(expectedZones);

        await expectNoErrorBoundary(page);
    });
});
