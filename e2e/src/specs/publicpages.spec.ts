import { test, expect, expectNoErrorBoundary } from '../fixtures';

// All tests here run WITHOUT storageState — these surfaces must work for
// anonymous visitors.

const STATUS_HEADLINES = /All systems operational|Service degraded|Major service impact/;

test.describe('public pages @core', () => {
    test('/status renders a headline state without auth', async ({ page }) => {
        await page.goto('/status');
        await expect(page.getByText(STATUS_HEADLINES)).toBeVisible({ timeout: 15_000 });
        await expectNoErrorBoundary(page);
    });

    test('/login is reachable', async ({ page }) => {
        const resp = await page.goto('/login');
        expect(resp?.status()).toBeLessThan(400);
        await expect(page.getByRole('button', { name: 'Sign In' })).toBeVisible();
    });

    test('/evidence/<garbage-token> degrades gracefully', async ({ page }) => {
        const resp = await page.goto('/evidence/garbage-token-e2e-smoke');
        const status = resp?.status() ?? 0;
        if (status === 404) {
            // evidence_sharing parked -> Next 404 is the correct behavior.
            test.info().annotations.push({ type: 'parked', description: '/evidence 404 (evidence_sharing flag off)' });
        } else {
            // Older deploy / flag on: page renders either mock share content
            // (the deployed build shows an "EVIDENCE VIEWER" mock for any
            // token) or the explicit not-found state — never the error boundary.
            await expect(
                page.getByText(/Evidence Not Found|This page could not be found|EVIDENCE VIEWER/).first(),
            ).toBeVisible({ timeout: 15_000 });
        }
        await expectNoErrorBoundary(page);
    });
});
