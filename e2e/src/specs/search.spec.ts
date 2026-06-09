import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { gotoFlagAware } from '../helpers/flags';

test.use({ storageState: authFile('admin') });

test.describe('semantic search @backburner', () => {
    test('/search renders or is parked', async ({ page }) => {
        if ((await gotoFlagAware(page, '/search', 'semantic_search')) === 'parked') return;
        await expectNoErrorBoundary(page);
        // Page is allowed for admin (RouteGuard) so it must not bounce to /login.
        await expect
            .poll(() => new URL(page.url()).pathname, { timeout: 15_000 })
            .toContain('/search');
    });
});
