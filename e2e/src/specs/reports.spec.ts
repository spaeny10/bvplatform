import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile } from '../helpers/auth';
import { gotoFlagAware } from '../helpers/flags';

test.use({ storageState: authFile('admin') });

test.describe('reports @backburner', () => {
    test('/reports renders or is parked', async ({ page }) => {
        // /reports is part of the SOC console surface -> operator_console flag.
        if ((await gotoFlagAware(page, '/reports', 'operator_console')) === 'parked') return;
        await expectNoErrorBoundary(page);
        await expect
            .poll(() => new URL(page.url()).pathname, { timeout: 15_000 })
            .toContain('/reports');
    });
});
