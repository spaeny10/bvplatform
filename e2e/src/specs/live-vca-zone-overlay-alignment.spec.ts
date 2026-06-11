import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { recordLiveTraffic } from '../helpers/video';

// PIXEL-ALIGNMENT proof for the VCA zone overlay geometry fix.
//
// The bug: VCAZoneOverlay rendered into a 1:1 viewBox="0 0 100 100" with
// preserveAspectRatio="xMidYMid meet", while the <video> uses
// object-fit:contain at the camera's (non-square) aspect ratio. `meet`
// letterboxes the SVG's SQUARE user space inside the cell, which does NOT
// coincide with the video's contain-letterboxed rect unless the camera is
// square. Result: zones offset over the video — pronounced on the panoramic
// 504-front (~3.37:1).
//
// The fix: viewBox = the video's intrinsic dimensions ("0 0 frameW frameH"),
// so the SVG's meet letterbox is IDENTICAL to the video's object-fit:contain.
//
// This test does NOT just assert "shapes present / within cell" (the prior
// e2e missed the bug that way). It computes the painted-video rect from
// object-fit:contain math and asserts the rendered polygon's first vertex
// lands on the API's normalized point ON THAT RECT, within a few px.

test.use({ storageState: authFile('admin') });

const FRONT_504 = 'd99d8b06';
// Tolerance for sub-pixel rounding, getScreenCTM float math, and the 0.01px
// toFixed(2) snapping of polygon coords.
const TOL_PX = 5;

function staticLayout(cameraIds: string[]) {
    const staticAssignments: Record<number, string> = {};
    cameraIds.forEach((id, i) => { staticAssignments[i] = id; });
    return [{
        name: 'e2e-vca-align',
        items: [],
        cols: 1,
        version: 3,
        mode: 'static',
        staticPreset: { w: 1, h: 1 },
        staticAssignments,
    }];
}

test.describe('Live VCA zone overlay pixel-alignment @core', () => {
    test('504-front zone vertex aligns to the painted-video rect (object-fit:contain)', async ({ page }) => {
        test.setTimeout(120_000);

        const camerasRes = await page.request.get('/api/cameras');
        expect(camerasRes.ok(), `GET /api/cameras -> HTTP ${camerasRes.status()}`).toBeTruthy();
        const cameras = (await camerasRes.json()) as { id: string; name: string }[];
        const front = cameras.find(c => c.id.startsWith(FRONT_504));
        expect(front, `test camera 504-front (${FRONT_504}) must be registered`).toBeTruthy();

        // Ground-truth normalized region from the same API the overlay consumes.
        const rulesRes = await page.request.get(`/api/cameras/${front!.id}/vca/rules`);
        expect(rulesRes.ok(), `GET vca/rules -> HTTP ${rulesRes.status()}`).toBeTruthy();
        const rules = (await rulesRes.json()) as {
            enabled: boolean; rule_type: string; region: { x: number; y: number }[];
        }[];
        const drawable = rules.filter(r => r.enabled && Array.isArray(r.region) && r.region.length >= 2);
        expect(drawable.length, '504-front must have >=1 drawable VCA zone').toBeGreaterThanOrEqual(1);
        // Use a polygon zone (intrusion/region/loitering) — its first <polygon>
        // vertex maps 1:1 to region[0]. (linecross renders as <line>.)
        const poly = drawable.find(r => r.rule_type !== 'linecross') ?? drawable[0];
        const p0 = poly.region[0];
        test.info().annotations.push({
            type: 'api-point', description: `zone "${poly.rule_type}" region[0] = (${p0.x}, ${p0.y})`,
        });

        // 1-up static layout: 504-front alone, zones forced ON.
        await page.addInitScript(([layouts, active]) => {
            localStorage.setItem('ironsight-layouts', layouts);
            localStorage.setItem('ironsight-active-layout', active);
            localStorage.setItem('ironsight-vca-zones-visible', 'on');
        }, [JSON.stringify(staticLayout([front!.id])), 'e2e-vca-align'] as const);

        recordLiveTraffic(page);
        await page.goto('/');
        const cell = page.locator('.video-cell', { hasText: front!.name }).first();
        await expect(cell).toBeVisible({ timeout: 20_000 });

        // Wait for the overlay polygon to render (gated on a known resolution,
        // so its presence already implies videoWidth/Height > 0).
        const polygon = cell.locator('svg polygon').first();
        await expect.poll(
            async () => cell.locator('svg polygon').count(),
            { timeout: 30_000, message: '504-front should render its zone polygon' },
        ).toBeGreaterThanOrEqual(1);

        // Read the geometry: cell box, video intrinsic dims, and the SVG viewBox.
        const geo = await cell.evaluate((cellEl) => {
            const video = cellEl.querySelector('video') as HTMLVideoElement | null;
            const svg = cellEl.querySelector('svg') as SVGSVGElement | null;
            const vb = svg?.getAttribute('viewBox') ?? null;
            const cRect = cellEl.getBoundingClientRect();
            const vRect = video?.getBoundingClientRect() ?? null;
            return {
                vw: video?.videoWidth ?? 0,
                vh: video?.videoHeight ?? 0,
                viewBox: vb,
                // Use the VIDEO element box as the contain frame (it equals the
                // cell here; both are inset:0). clientWidth/Height of the video.
                cw: video?.clientWidth ?? 0,
                ch: video?.clientHeight ?? 0,
                videoLeft: vRect?.left ?? cRect.left,
                videoTop: vRect?.top ?? cRect.top,
            };
        });
        test.info().annotations.push({
            type: 'geometry',
            description: `vw=${geo.vw} vh=${geo.vh} cw=${geo.cw} ch=${geo.ch} viewBox="${geo.viewBox}"`,
        });

        // The fix sets viewBox to the intrinsic frame dims. Assert that first —
        // it is the load-bearing geometry change.
        if (geo.vw > 0 && geo.vh > 0) {
            expect(geo.viewBox, 'viewBox must equal the video intrinsic dims').toBe(`0 0 ${geo.vw} ${geo.vh}`);
        }

        if (geo.vw > 0 && geo.vh > 0 && geo.cw > 0 && geo.ch > 0) {
            // ── PRIMARY PROOF: pixel-alignment with decoded video. ──
            // object-fit:contain painted rect, relative to the video element.
            const scaleC = Math.min(geo.cw / geo.vw, geo.ch / geo.vh);
            const rw = geo.vw * scaleC;
            const rh = geo.vh * scaleC;
            const rx = (geo.cw - rw) / 2;
            const ry = (geo.ch - rh) / 2;
            // Expected SCREEN position of region[0] on the painted video rect.
            const expX = geo.videoLeft + rx + p0.x * rw;
            const expY = geo.videoTop + ry + p0.y * rh;

            // Actual SCREEN position of the polygon's first vertex via getScreenCTM.
            const actual = await polygon.evaluate((poly) => {
                const p = poly as unknown as SVGPolygonElement;
                const pt = p.points.getItem(0);
                const ctm = p.getScreenCTM();
                if (!ctm) return null;
                const s = pt.matrixTransform(ctm);
                return { x: s.x, y: s.y };
            });
            expect(actual, 'polygon first vertex must map to a screen point').toBeTruthy();

            const dx = Math.abs(actual!.x - expX);
            const dy = Math.abs(actual!.y - expY);
            test.info().annotations.push({
                type: 'alignment',
                description: `expected=(${expX.toFixed(1)}, ${expY.toFixed(1)}) actual=(${actual!.x.toFixed(1)}, ${actual!.y.toFixed(1)}) dx=${dx.toFixed(2)} dy=${dy.toFixed(2)} tol=${TOL_PX}`,
            });
            expect(dx, `vertex X off by ${dx.toFixed(2)}px (>${TOL_PX})`).toBeLessThanOrEqual(TOL_PX);
            expect(dy, `vertex Y off by ${dy.toFixed(2)}px (>${TOL_PX})`).toBeLessThanOrEqual(TOL_PX);
        } else {
            // ── FALLBACK: video did not decode (videoWidth=0). The overlay is
            // gated on a known resolution so it should not even render here;
            // surface this loudly rather than silently passing.
            test.info().annotations.push({
                type: 'fallback',
                description: 'video did not decode (videoWidth=0); overlay should be gated off',
            });
            // With no resolution the overlay must not draw (the fix's render gate).
            expect(await cell.locator('svg polygon').count(),
                'overlay must NOT draw when video dims are unknown').toBe(0);
        }

        await expectNoErrorBoundary(page);
    });
});
