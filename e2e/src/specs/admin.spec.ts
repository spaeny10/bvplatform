import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { getFlags } from '../helpers/flags';

test.use({ storageState: authFile('admin') });

// Core tab labels from frontend/src/app/admin/page.tsx. Counts render
// inside the buttons (accessible name becomes e.g. "Sites & Customers 12"),
// so match on a ^label prefix regex rather than exact names.
const CORE_TABS = ['Sites & Customers', 'Operators', 'Users', 'NVR Settings', 'Health', 'Audit Trail'];

function escapeRe(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

test.describe('admin console @backburner', () => {
    test('tab bar renders and every core tab opens cleanly', async ({ page }) => {
        test.setTimeout(120_000);
        await page.goto('/admin');

        for (const label of CORE_TABS) {
            const tab = page.getByRole('button', { name: new RegExp(`^${escapeRe(label)}`) }).first();
            await expect(tab, `tab "${label}" should be visible`).toBeVisible({ timeout: 15_000 });
        }

        for (const label of CORE_TABS) {
            const tab = page.getByRole('button', { name: new RegExp(`^${escapeRe(label)}`) }).first();
            await tab.click();
            // Give the tab's lazy loads a beat, then check nothing blew up.
            await page.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => { /* live polls */ });
            await expectNoErrorBoundary(page);
        }
        // 5xx during any tab load are caught by the auto fixture.
    });

    test('parked tabs (Integrations / ML Labeling) follow their flags', async ({ page }) => {
        const flags = await getFlags();
        await page.goto('/admin');
        await expect(page.getByRole('button', { name: /^Sites & Customers/ }).first()).toBeVisible({ timeout: 15_000 });

        const checks: { label: string; flag: string }[] = [
            { label: 'Integrations', flag: 'integrations' },
            { label: 'ML Labeling', flag: 'labeling' },
        ];
        for (const { label, flag } of checks) {
            const el = page.getByText(label, { exact: true }).first();
            if (flags && flags[flag] === true) {
                await expect(el, `"${label}" tab should render while ${flag}=true`).toBeVisible();
            } else if (flags && flags[flag] === false) {
                await expect(el, `"${label}" tab must be hidden while ${flag}=false`).toHaveCount(0);
                test.info().annotations.push({ type: 'parked', description: `${label} tab hidden (${flag}=false)` });
            } else {
                // Legacy deploy: flag absent from /api/v1/features — no assertion.
                test.info().annotations.push({
                    type: 'flag-unknown',
                    description: `${flag} not present in /api/v1/features — skipped ${label} tab assertion`,
                });
            }
        }
    });
});
