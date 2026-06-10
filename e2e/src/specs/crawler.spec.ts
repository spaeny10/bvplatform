import { test, expect } from '@playwright/test';
import * as path from 'path';
import { authFile, hasAuthState, RoleName } from '../helpers/auth';

// Route crawler: navigates every route a role can reach (mirrors
// ROUTE_PERMISSIONS in frontend/src/contexts/AuthContext.tsx), waits for
// the network to settle, asserts the error boundary never rendered, and
// screenshots each page to results/crawl/<role>/<route>.png.
//
// Deliberately uses the base Playwright test (NOT ../fixtures): parked
// pages, empty seeds and offline cameras produce console noise that is
// the smoke specs' business, not the crawler's. The crawler only answers
// "does any reachable route hard-crash?".

// ROUTE_PERMISSIONS mirror (sorted by role):
//   /operator:  soc_operator, soc_supervisor, admin
//   /analytics: soc_supervisor, admin, site_manager
//   /portal:    site_manager, customer, soc_supervisor, admin
//   /admin:     admin
//   /reports:   admin, soc_supervisor
//   /search:    soc_supervisor, admin
//   /:          everyone except soc_operator
const ROLE_ROUTES: Record<RoleName, string[]> = {
    admin: [
        '/', '/admin', '/admin/labeling', '/operator', '/analytics',
        '/portal', '/portal/compliance', '/portal/history', '/portal/notifications',
        '/portal/privacy', '/portal/subprocessors',
        '/reports', '/search', '/status',
    ],
    supervisor: ['/', '/operator', '/analytics', '/portal', '/reports', '/search', '/status'],
    operator: ['/operator', '/status'],
    manager: ['/', '/analytics', '/portal', '/portal/compliance', '/portal/history', '/portal/notifications', '/status'],
    customer: ['/', '/portal', '/portal/history', '/status'],
};

function sanitizeRoute(route: string): string {
    if (route === '/') return 'home';
    return route.replace(/^\//, '').replace(/[^a-zA-Z0-9-]+/g, '_');
}

const OUT_DIR = path.resolve(__dirname, '..', '..', 'results', 'crawl');

for (const [role, staticRoutes] of Object.entries(ROLE_ROUTES) as [RoleName, string[]][]) {
    test(`crawl ${role} routes @crawl`, async ({ browser }) => {
        test.setTimeout(300_000);
        test.skip(!hasAuthState(role), `${role} login unavailable — seed not run`);

        const context = await browser.newContext({ storageState: authFile(role) });
        const page = await context.newPage();

        // Resolve dynamic IDs via API using this role's cookies.
        const routes = [...staticRoutes];
        if (role === 'admin') {
            const res = await page.request.get('/api/cameras').catch(() => null);
            if (res?.ok()) {
                const cams = (await res.json()) as { id: string }[];
                if (cams.length > 0) routes.push(`/popout/${cams[0].id}`);
            }
        }

        const failures: string[] = [];
        for (const route of routes) {
            try {
                await page.goto(route, { waitUntil: 'domcontentloaded' });
                await page.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => { /* HLS polls forever */ });
                const boundaryCount = await page.getByText('Something went wrong').count();
                if (boundaryCount > 0) failures.push(`${route}: error boundary rendered`);
                await page.screenshot({
                    path: path.join(OUT_DIR, role, `${sanitizeRoute(route)}.png`),
                    fullPage: false,
                });
                test.info().annotations.push({
                    type: 'crawled',
                    description: `${role} ${route} -> ${new URL(page.url()).pathname}`,
                });
            } catch (err) {
                failures.push(`${route}: ${String(err).split('\n')[0]}`);
            }
        }

        await context.close();
        expect(failures, `routes that hard-crashed for ${role}`).toEqual([]);
    });
}
