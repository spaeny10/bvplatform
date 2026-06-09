import { test as base, expect, Page } from '@playwright/test';

// Known-noise console errors that must NOT fail a smoke test:
//  - /ws does not proxy through the SSH tunnel -> WebSocket errors are
//    expected on every authenticated page (see scripts/tunnel-bob.ps1).
//  - hls.js logs fatal-looking errors for per-camera stream hiccups; the
//    video helper judges stream health explicitly instead.
//  - net::ERR_ABORTED fires when navigation cancels in-flight fetches.
//  - "[LIVE-PROXY] fatal error" is VideoPlayer.tsx's own console.error for
//    hls.js fatals — same class of noise as /hls\.js/i above.
//  - "Failed to load resource" is Chromium's network-layer echo of any
//    non-2xx response (e.g. the intentional 401 in the bad-creds test);
//    real server failures are still caught by the 5xx response collector.
const CONSOLE_ALLOWLIST: RegExp[] = [
    /ws|websocket/i,
    /hls\.js/i,
    /net::ERR_ABORTED/,
    /\[LIVE-PROXY\]/i,
    /Failed to load resource/i,
];

export interface ErrorCollector {
    consoleErrors: string[];
    pageErrors: string[];
    serverErrors: string[];
}

interface SmokeFixtures {
    errorCollector: ErrorCollector;
    /** 5xx responses observed during the test (exposed for direct assertions). */
    serverErrors: string[];
}

export const test = base.extend<SmokeFixtures>({
    errorCollector: [
        async ({ page }, use) => {
            const collector: ErrorCollector = { consoleErrors: [], pageErrors: [], serverErrors: [] };
            page.on('console', msg => {
                if (msg.type() !== 'error') return;
                const text = msg.text();
                if (CONSOLE_ALLOWLIST.some(re => re.test(text))) return;
                collector.consoleErrors.push(text);
            });
            page.on('pageerror', err => {
                // No allowlist: an uncaught exception in app code is always a bug.
                collector.pageErrors.push(String(err));
            });
            page.on('response', res => {
                if (res.status() >= 500) collector.serverErrors.push(`${res.status()} ${res.url()}`);
            });

            await use(collector);

            // Teardown assertions — zero tolerance.
            expect.soft(collector.pageErrors, 'uncaught page errors').toEqual([]);
            expect.soft(collector.consoleErrors, 'non-allowlisted console errors').toEqual([]);
            expect.soft(collector.serverErrors, '5xx responses').toEqual([]);
        },
        { auto: true },
    ],

    serverErrors: async ({ errorCollector }, use) => {
        await use(errorCollector.serverErrors);
    },
});

export { expect };

/** The shared ErrorBoundary fallback renders "Something went wrong". */
export async function expectNoErrorBoundary(page: Page): Promise<void> {
    await expect(page.getByText('Something went wrong')).toHaveCount(0);
}
