import { test, expect } from '../fixtures';
import { authFile, hasAuthState, rolePassword, ROLES } from '../helpers/auth';

const admin = ROLES.find(r => r.role === 'admin')!;

test.describe('auth @core', () => {
    test('login form renders', async ({ page }) => {
        await page.goto('/login');
        await expect(page.locator('#username')).toBeVisible();
        await expect(page.locator('#password')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Sign In' })).toBeVisible();
    });

    test('bad credentials show the login error', async ({ page }) => {
        await page.goto('/login');
        await page.locator('#username').fill('definitely-not-a-user');
        await page.locator('#password').fill('wrong-password-123');
        await page.getByRole('button', { name: 'Sign In' }).click();
        await expect(page.locator('.login-error')).toBeVisible();
        // Still on the login page — no redirect happened.
        expect(new URL(page.url()).pathname).toBe('/login');
    });

    test('admin login lands on the NVR dashboard (/)', async ({ page }) => {
        await page.goto('/login');
        await page.locator('#username').fill(admin.username);
        await page.locator('#password').fill(rolePassword(admin));
        await page.getByRole('button', { name: 'Sign In' }).click();
        // admin role -> roleDestination '/' (login/page.tsx).
        await page.waitForURL(url => new URL(url).pathname === '/', { timeout: 15_000 });
    });

    test('unauthenticated / redirects to /login', async ({ page }) => {
        await page.goto('/');
        // authFetch redirects to /login on the first 401 from the dashboard's
        // API calls (frontend/src/lib/api.ts), so this is eventually-consistent.
        await page.waitForURL(url => new URL(url).pathname === '/login', { timeout: 20_000 });
    });
});

test.describe('auth role enforcement @core', () => {
    // Conditional storageState: evaluated at worker load, which happens
    // after the setup project completed, so the existence check is reliable.
    test.use({ storageState: hasAuthState('operator') ? authFile('operator') : undefined });

    test('operator navigating /admin is redirected away', async ({ page }) => {
        test.skip(!hasAuthState('operator'), 'operator (jhayes) login unavailable — seed not run');
        await page.goto('/admin');
        // RouteGuard sends soc_operator to roleHome -> /operator.
        await page.waitForURL(url => new URL(url).pathname.startsWith('/operator'), { timeout: 15_000 });
        expect(new URL(page.url()).pathname).not.toContain('/admin');
    });
});
