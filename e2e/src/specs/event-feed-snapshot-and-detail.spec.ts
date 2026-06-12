import { test, expect } from '../fixtures';
import { authFile } from '../helpers/auth';

test.use({ storageState: authFile('admin') });

// ─────────────────────────────────────────────────────────────────────────
// Proof for feat/event-feed-snapshot-and-detail.
//
// Two related Alert Feed (EventListPanel) improvements:
//   1. Captured snapshots actually SHOW in the feed. The async FFmpeg
//      thumbnail grab now backfills live rows via the "event_thumbnail" WS
//      patch (boot-time AND runtime-added cameras), and the REST list already
//      returns the thumbnail — so a row scoped to a thumbnail-bearing camera
//      renders the <img> (data-testid="alert-thumbnail"), not a blank ribbon.
//   2. Clicking a row opens a DETAIL MODAL with the larger snapshot + bbox,
//      the metadata fields, the Camera-VCA / Server-AI source badge, the raw
//      details payload, and a "Jump to video" control. Close works.
//
// Camera-agnostic: we DO NOT hardcode a UUID. We resolve a thumbnail-bearing
// camera from the live inventory, preferring "5001 front" (the sanctioned
// primary test camera — 504 is a live CUSTOMER site and must not be touched).
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

// Pick the camera to drive the feed with: the sanctioned 5001 front if it has
// thumbnail-bearing events, otherwise the first camera in the inventory that
// has thumbnail-bearing events. NEVER 504 (live customer site).
async function pickThumbnailCamera(page: any): Promise<{ cam: Cam; withThumb: number } | null> {
    const res = await page.request.get('/api/cameras');
    expect(res.ok(), `GET /api/cameras -> ${res.status()}`).toBeTruthy();
    const cameras = (await res.json()) as Cam[];
    expect(cameras.length, 'expected at least one camera in inventory').toBeGreaterThan(0);

    const is504 = (c: Cam) => /(^|\W)504(\W|$)/.test(c.name);
    const preferred = cameras.find(c => /5001\s*front/i.test(c.name));
    // Candidate order: 5001 front first, then any non-504 camera.
    const candidates = [
        ...(preferred ? [preferred] : []),
        ...cameras.filter(c => c !== preferred && !is504(c)),
    ];

    for (const cam of candidates) {
        const evRes = await page.request.get(`/api/events?camera_id=${cam.id}&limit=50`);
        if (!evRes.ok()) continue;
        const events = (await evRes.json()) as any[];
        const withThumb = events.filter(e => e.thumbnail && e.thumbnail.length > 10).length;
        if (withThumb > 0) return { cam, withThumb };
    }
    // Soft signal: no non-504 camera has thumbnail-bearing events right now.
    // The caller turns this into a test.skip (not a hard failure) — the test
    // environment can transiently lack captured thumbnails (fresh deploy, idle
    // fleet) and that's not a regression in THIS feature. We never fall back to
    // 504 (live customer site).
    return null;
}

test.describe('Alert feed shows snapshots + click-to-detail modal @core', () => {
    test('a feed row renders the snapshot, and clicking it opens the detail modal', async ({ page }) => {
        test.setTimeout(120_000);

        // 1. Resolve a thumbnail-bearing camera (prefers 5001 front; never 504).
        const picked = await pickThumbnailCamera(page);
        test.skip(
            !picked,
            'No non-504 camera has thumbnail-bearing events yet (capture pending / idle fleet) — '
            + 'nothing to assert without touching the live 504 customer site.',
        );
        const { cam, withThumb } = picked!;
        test.info().annotations.push({
            type: 'api-proof',
            description: `camera="${cam.name}" id=${cam.id} eventsWithThumbnail(first50)=${withThumb}`,
        });

        // 2. Seed a layout scoped to that one camera before any page script runs.
        const layout = staticLayout('e2e-feed-snap', [cam.id]);
        await page.addInitScript(([ls, active]) => {
            localStorage.setItem('ironsight-layouts', ls as string);
            localStorage.setItem('ironsight-active-layout', active as string);
        }, [JSON.stringify([layout]), 'e2e-feed-snap'] as const);

        await page.goto('/');
        await expect(page.locator('.video-cell').first()).toBeVisible({ timeout: 20_000 });

        // 3. Open the Alert Feed panel and scope it to the active layout so we
        //    only see the chosen camera's rows.
        await page.getByRole('button', { name: /Alert Feed/ }).click();
        await expect(page.locator('.event-list-panel.open')).toBeVisible({ timeout: 10_000 });
        const scopeBtn = page.getByTestId('scope-layout-btn');
        await expect(scopeBtn).toBeEnabled({ timeout: 10_000 });
        await scopeBtn.click();

        // 4. Rows must appear.
        const rows = page.getByTestId('alert-row');
        await expect(rows.first()).toBeVisible({ timeout: 20_000 });
        const rowCount = await rows.count();
        expect(rowCount, 'feed rows visible').toBeGreaterThan(0);

        // 5. TASK 1: at least one row renders the snapshot <img>, and it is a
        //    real, non-blank base64/data image (not an empty ribbon).
        const thumb = page.getByTestId('alert-thumbnail').first();
        await expect(thumb).toBeVisible({ timeout: 20_000 });
        const thumbSrc = await thumb.getAttribute('src');
        expect(thumbSrc, 'thumbnail <img> src present').toBeTruthy();
        expect(thumbSrc!.startsWith('data:image') || thumbSrc!.length > 100, 'thumbnail src is image data').toBeTruthy();
        // The image actually decoded to non-zero pixels (proves it isn't a 0×0
        // broken-image placeholder).
        const natural = await thumb.evaluate((el: HTMLImageElement) => ({ w: el.naturalWidth, h: el.naturalHeight }));
        expect(natural.w, 'thumbnail decoded width > 0').toBeGreaterThan(0);
        expect(natural.h, 'thumbnail decoded height > 0').toBeGreaterThan(0);

        // 6. TASK 2: clicking a row opens the detail modal. Click the row that
        //    has the visible thumbnail so the modal's snapshot path is exercised.
        const rowWithThumb = page.locator('[data-testid="alert-row"]').filter({
            has: page.getByTestId('alert-thumbnail'),
        }).first();
        await rowWithThumb.click();

        const modal = page.getByTestId('event-detail-modal');
        await expect(modal, 'detail modal opens on row click').toBeVisible({ timeout: 10_000 });

        // Modal shows the larger snapshot.
        const modalThumb = page.getByTestId('event-detail-thumbnail');
        await expect(modalThumb, 'modal snapshot visible').toBeVisible({ timeout: 10_000 });
        const modalNatural = await modalThumb.evaluate((el: HTMLImageElement) => ({ w: el.naturalWidth, h: el.naturalHeight }));
        expect(modalNatural.w, 'modal snapshot decoded width > 0').toBeGreaterThan(0);

        // Detail fields block + source badge + raw payload + jump-to-video.
        await expect(page.getByTestId('event-detail-fields'), 'metadata fields block').toBeVisible();
        await expect(modal, 'a metadata field label is present').toContainText(/Camera|Event type|Time/);
        const sourceBadge = page.getByTestId('event-detail-source-badge');
        await expect(sourceBadge, 'source badge in modal').toBeVisible();
        await expect(sourceBadge).toContainText(/Camera VCA|Server AI/);
        await expect(page.getByTestId('event-detail-raw'), 'raw details payload present').toBeAttached();
        const jump = page.getByTestId('event-detail-jump');
        await expect(jump, 'jump-to-video control present').toBeVisible();
        await expect(jump).toContainText(/Jump to video/i);

        const badgeText = await sourceBadge.innerText();

        // 7. Close the modal (X button) and confirm it goes away.
        await page.getByTestId('event-detail-close').click();
        await expect(modal, 'modal closes via X').toBeHidden({ timeout: 10_000 });

        // 8. Re-open and confirm Escape also closes it.
        await rowWithThumb.click();
        await expect(modal).toBeVisible({ timeout: 10_000 });
        await page.keyboard.press('Escape');
        await expect(modal, 'modal closes via Escape').toBeHidden({ timeout: 10_000 });

        test.info().annotations.push({
            type: 'ui-proof',
            description: `rows=${rowCount} rowThumbnail=${natural.w}x${natural.h} `
                + `modalThumbnail=${modalNatural.w}x${modalNatural.h} sourceBadge="${badgeText.replace(/\s+/g, ' ').trim()}" `
                + `modalFields+rawPayload+jumpToVideo=present closeX+Escape=ok`,
        });
    });
});
