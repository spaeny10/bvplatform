import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile, hasAuthState } from '../helpers/auth';
import { gotoFlagAware } from '../helpers/flags';

test.use({ storageState: hasAuthState('manager') ? authFile('manager') : undefined });

test.describe('customer portal @backburner', () => {
    test.beforeEach(() => {
        test.skip(!hasAuthState('manager'), 'manager (marcus.webb) login unavailable — seed not run');
    });

    test('/portal renders the shell', async ({ page }) => {
        await page.goto('/portal');
        // Portal itself is core (not flag-gated); 5xx are caught by fixtures.
        await expect(page.locator('.portal-shell')).toBeVisible({ timeout: 20_000 });

        // Site cards only exist when the seed created sites — annotate, don't fail.
        const siteBadge = page.locator('.portal-nav-badge').first();
        const badgeText = ((await siteBadge.textContent().catch(() => '')) ?? '').trim();
        test.info().annotations.push({
            type: 'portal-sites',
            description: badgeText ? `sidebar reports ${badgeText} site(s)` : 'no site count badge found (unseeded?)',
        });
        await expectNoErrorBoundary(page);
    });

    test('/portal/compliance renders or is parked', async ({ page }) => {
        if ((await gotoFlagAware(page, '/portal/compliance', 'compliance')) === 'parked') return;
        await expectNoErrorBoundary(page);
        // Still inside the portal shell when the flag is on.
        await expect(page.locator('.portal-shell')).toBeVisible({ timeout: 20_000 });
    });
});
