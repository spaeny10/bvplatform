import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { gotoFlagAware } from '../helpers/flags';

test.use({ storageState: authFile('admin') });

test.describe('analytics @backburner', () => {
    test('/analytics renders or is parked', async ({ page }) => {
        if ((await gotoFlagAware(page, '/analytics', 'analytics')) === 'parked') return;
        await expectNoErrorBoundary(page);
        await expect
            .poll(() => new URL(page.url()).pathname, { timeout: 15_000 })
            .toContain('/analytics');
    });
});
