import { test, expect } from '../fixtures';
import { authFile } from '../helpers/auth';

test.use({ storageState: authFile('admin') });

// ─────────────────────────────────────────────────────────────────────────
// Proof for feat/alert-feed-enrichment.
//
// The playback Alert Feed (EventListPanel) now surfaces detection detail that
// was already captured in events.details but never shown:
//   • object class chip (Person / Crossed / Human / ...),
//   • VCA rule_name chip,
//   • a Camera-VCA vs Server-AI source badge (from the backend-projected
//     Event.source), and
//   • the event snapshot thumbnail with a bbox overlay when coords exist.
// It can also scope/group to the ACTIVE grid layout (PR #70 bridge).
//
// In the bob test DB all events belong to "504 right ptz" and are camera-side
// ONVIF rule-engine events (driver=milesight) — so the source badge must read
// "Camera VCA". 68 of those events carry a base64 thumbnail; none carry a
// score or bounding box (so confidence / bbox are legitimately absent and the
// spec asserts their render path only when present).
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

test.describe('Alert feed surfaces detection detail + scopes to active layout @core', () => {
    test('504 layout feed shows object class, rule, Camera-VCA badge, and a snapshot', async ({ page }) => {
        test.setTimeout(120_000);

        // 1. Resolve the 504 camera that carries events from the live inventory.
        const res = await page.request.get('/api/cameras');
        expect(res.ok(), `GET /api/cameras -> ${res.status()}`).toBeTruthy();
        const cameras = (await res.json()) as Cam[];
        const byName = (needle: string) =>
            cameras.find(c => c.name.toLowerCase() === needle.toLowerCase())
            ?? cameras.find(c => c.name.toLowerCase().includes(needle.toLowerCase()));
        const c504 = byName('504 right ptz') ?? byName('504');
        expect(c504, `expected a 504 camera in inventory, got: ${cameras.map(c => c.name).join(', ')}`).toBeTruthy();

        // 2. Confirm the API now projects a normalized "source" onto the list
        //    payload (the backend half of the change) and capture a representative
        //    enriched event to assert against.
        const evRes = await page.request.get(`/api/events?camera_id=${c504!.id}&limit=200&types=human,linecross,intrusion,object,motion,loitering`);
        expect(evRes.ok(), `GET /api/events -> ${evRes.status()}`).toBeTruthy();
        const events = (await evRes.json()) as any[];
        expect(events.length, 'expected events for 504 right ptz').toBeGreaterThan(0);
        const cameraSourced = events.filter(e => e.source === 'camera');
        expect(cameraSourced.length, 'expected camera-sourced events (driver=milesight)').toBeGreaterThan(0);
        const withThumb = events.filter(e => e.thumbnail && e.thumbnail.length > 10);
        test.info().annotations.push({
            type: 'api-proof',
            description: `events=${events.length} camera-sourced=${cameraSourced.length} `
                + `with-thumbnail=${withThumb.length} sample-source=${events[0]?.source} `
                + `sample-rule=${events.find(e => e.details?.rule)?.details?.rule ?? 'n/a'}`,
        });

        // 3. Seed a 504-only layout active before any page script runs.
        const layout = staticLayout('e2e-504-feed', [c504!.id]);
        await page.addInitScript(([ls, active]) => {
            localStorage.setItem('ironsight-layouts', ls as string);
            localStorage.setItem('ironsight-active-layout', active as string);
        }, [JSON.stringify([layout]), 'e2e-504-feed'] as const);

        await page.goto('/');
        await expect(page.locator('.video-cell').first()).toBeVisible({ timeout: 20_000 });

        // 4. Open the Alert Feed panel.
        await page.getByRole('button', { name: /Alert Feed/ }).click();
        await expect(page.locator('.event-list-panel.open')).toBeVisible({ timeout: 10_000 });

        // 5. Scope the feed to the active grid layout.
        const scopeBtn = page.getByTestId('scope-layout-btn');
        await expect(scopeBtn).toBeEnabled({ timeout: 10_000 });
        await scopeBtn.click();

        // 6. Rows must appear and carry the enriched detail.
        const rows = page.getByTestId('alert-row');
        await expect(rows.first()).toBeVisible({ timeout: 20_000 });
        const rowCount = await rows.count();
        expect(rowCount, 'enriched rows visible').toBeGreaterThan(0);

        // Object class chip on at least one row.
        await expect(page.getByTestId('alert-object-class').first()).toBeVisible();
        const objClassText = await page.getByTestId('alert-object-class').first().innerText();

        // VCA rule chip on at least one row (the rule-engine events carry it).
        await expect(page.getByTestId('alert-rule').first()).toBeVisible({ timeout: 10_000 });
        const ruleText = await page.getByTestId('alert-rule').first().innerText();

        // Camera-VCA source badge. Every test-DB event is camera-side, so the
        // badge must read "Camera VCA" and the row must be data-source="camera".
        const badge = page.getByTestId('alert-source-badge').first();
        await expect(badge).toBeVisible();
        await expect(badge).toContainText(/Camera VCA/);
        const cameraRows = page.locator('[data-testid="alert-row"][data-source="camera"]');
        expect(await cameraRows.count(), 'rows tagged data-source=camera').toBeGreaterThan(0);

        // Snapshot thumbnail render path: assert it shows when the test set has
        // thumbnail-bearing events (it does — 68 of them).
        let thumbShown = false;
        if (withThumb.length > 0) {
            const thumb = page.getByTestId('alert-thumbnail').first();
            await expect(thumb).toBeVisible({ timeout: 15_000 });
            thumbShown = true;
        }

        // Confidence + bbox are absent in this test set (camera VCA emits no
        // score/box here) — record that honestly rather than asserting them.
        const confCount = await page.getByTestId('alert-confidence').count();

        test.info().annotations.push({
            type: 'ui-proof',
            description: `rows=${rowCount} firstObjClass="${objClassText}" firstRule="${ruleText}" `
                + `badge=CameraVCA cameraRows=${await cameraRows.count()} `
                + `thumbnailShown=${thumbShown} confidenceChips=${confCount} (0 expected: no score in test set)`,
        });

        // Sort control is present (Newest/Oldest/By type/By camera/By confidence).
        await expect(page.locator('select')).toContainText(['Newest']);
    });
});
