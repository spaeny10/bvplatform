import { request, expect, test, Page } from '@playwright/test';
import { authFile, hasAuthState, BASE_URL } from './auth';

// DESCOPE-AWARENESS: a feature-flag rollout is in flight. The deployed
// build may return only the 4 legacy flags (older deploy) or the full
// 2026-06 descope map (analytics / operator_console / compliance /
// semantic_search / labeling / integrations / ... all default false, with
// parked pages hard-404ing). flagOff() therefore returns true ONLY when
// the response explicitly carries `<name>: false` — an absent key means
// "this deploy predates the flag, the page should still render".

let cached: Record<string, boolean> | null | undefined;

export async function getFlags(): Promise<Record<string, boolean> | null> {
    if (cached !== undefined) return cached;
    try {
        const ctx = await request.newContext({
            baseURL: BASE_URL,
            ...(hasAuthState('admin') ? { storageState: authFile('admin') } : {}),
        });
        const res = await ctx.get('/api/v1/features');
        cached = res.ok() ? ((await res.json()) as Record<string, boolean>) : null;
        await ctx.dispose();
    } catch {
        cached = null;
    }
    return cached ?? null;
}

/** True only when GET /api/v1/features explicitly reports `name: false`. */
export async function flagOff(name: string): Promise<boolean> {
    const flags = await getFlags();
    return !!flags && flags[name] === false;
}

/** True only when the flag is explicitly `name: true`. */
export async function flagOn(name: string): Promise<boolean> {
    const flags = await getFlags();
    return !!flags && flags[name] === true;
}

/**
 * Navigate to a possibly-parked route.
 *
 * If the gating flag is explicitly off, asserts the route serves the Next
 * 404 (status 404 or the visible "This page could not be found" text),
 * pushes a `parked` annotation, and returns 'parked' — callers must then
 * skip their deeper assertions. Otherwise returns 'on' after navigation.
 */
export async function gotoFlagAware(page: Page, route: string, flag: string): Promise<'parked' | 'on'> {
    const off = await flagOff(flag);
    const resp = await page.goto(route, { waitUntil: 'domcontentloaded' });
    if (off) {
        const status = resp?.status();
        if (status !== 404) {
            await expect(
                page.getByText('This page could not be found'),
                `${route} should 404 while ${flag}=false (got HTTP ${status})`,
            ).toBeVisible();
        }
        test.info().annotations.push({
            type: 'parked',
            description: `${route} is parked (${flag}=false) — 404 verified, deeper assertions skipped`,
        });
        return 'parked';
    }
    return 'on';
}
