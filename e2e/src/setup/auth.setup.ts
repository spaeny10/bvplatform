import { test as setup, expect, request, APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { ROLES, RoleDef, authFile, hasAuthState, rolePassword, BASE_URL } from '../helpers/auth';

// API-login per role -> storage state. Cookie-based session: POST
// /auth/login sets ironsight_session (+ ironsight_csrf); storageState
// captures them for the spec projects.
//
// admin is REQUIRED — a failure here is a hard stop. The four demo users
// only exist after the seed has run on bob; a 401 soft-skips that role
// (specs depending on it skip via hasAuthState()).
//
// Rate-limit awareness: /auth/login allows 10 attempts/min per client IP
// (internal/api/ratelimit.go) and ALL tunnel traffic shares one IP. Two
// mitigations keep repeated runs under budget:
//   1. an existing .auth/<role>.json that still passes /auth/me is reused
//      without logging in again;
//   2. a 429 waits out Retry-After once before failing.

setup.beforeAll(() => {
    fs.mkdirSync(path.resolve(__dirname, '..', '..', '.auth'), { recursive: true });
});

// The API sets ironsight_session/ironsight_csrf with the Secure attribute.
// Browsers exempt loopback origins, so page navigation works over the
// http:// tunnel — but Playwright's APIRequestContext does NOT send Secure
// cookies over plain http to 127.0.0.1, which would 401 every page.request
// call and silently defeat the reuse check below. Tests only ever target
// the tunnel / LAN over http, so strip the Secure bit from the saved state.
function normalizeSecureCookies(file: string): void {
    if (!BASE_URL.startsWith('http://')) return;
    const state = JSON.parse(fs.readFileSync(file, 'utf8'));
    for (const cookie of state.cookies ?? []) cookie.secure = false;
    fs.writeFileSync(file, JSON.stringify(state, null, 2));
}

async function existingStateValid(def: RoleDef): Promise<boolean> {
    if (!hasAuthState(def.role)) return false;
    let ctx: APIRequestContext | undefined;
    try {
        ctx = await request.newContext({ baseURL: BASE_URL, storageState: authFile(def.role) });
        const me = await ctx.get('/auth/me');
        if (!me.ok()) return false;
        const user = await me.json();
        return user?.username === def.username;
    } catch {
        return false;
    } finally {
        await ctx?.dispose();
    }
}

async function loginWith429Backoff(ctx: APIRequestContext, def: RoleDef) {
    let res = await ctx.post('/auth/login', {
        data: { username: def.username, password: rolePassword(def) },
    });
    if (res.status() === 429) {
        const retryAfter = Math.min(65, Number(res.headers()['retry-after'] || 60) + 2);
        console.warn(`[auth.setup] 429 on ${def.username} login — waiting ${retryAfter}s for the limiter window`);
        await new Promise(r => setTimeout(r, retryAfter * 1000));
        res = await ctx.post('/auth/login', {
            data: { username: def.username, password: rolePassword(def) },
        });
    }
    return res;
}

for (const def of ROLES) {
    setup(`authenticate ${def.role} (${def.username})`, async () => {
        setup.setTimeout(120_000); // may sit out one 60s rate-limit window

        if (await existingStateValid(def)) {
            setup.info().annotations.push({ type: 'reused', description: `${def.role}: existing .auth state still valid` });
            return;
        }

        const ctx = await request.newContext({ baseURL: BASE_URL });
        const res = await loginWith429Backoff(ctx, def);

        if (!res.ok()) {
            const body = await res.text().catch(() => '');
            if (def.required) {
                expect(
                    res.ok(),
                    `admin login MUST succeed (HTTP ${res.status()} ${body.trim()}). `
                    + 'Check IRONSIGHT_ADMIN_PASSWORD in e2e/.env and that the tunnel is up.',
                ).toBeTruthy();
            }
            // Demo user missing — most likely the seed has not been run on
            // bob (see README). Soft-skip: don't write a storage state.
            console.warn(
                `[auth.setup] ${def.role} (${def.username}) login failed with HTTP ${res.status()} — `
                + 'soft-skipping this role. Run the seed on bob if these specs matter for this run.',
            );
            setup.info().annotations.push({
                type: 'soft-skip',
                description: `${def.role} login HTTP ${res.status()} — seed probably not run`,
            });
            try { fs.rmSync(authFile(def.role), { force: true }); } catch { /* best effort */ }
            await ctx.dispose();
            return;
        }

        const body = await res.json();
        expect(body.mfa_required, `${def.username} unexpectedly has MFA enabled`).toBeFalsy();

        await ctx.storageState({ path: authFile(def.role) });
        await ctx.dispose();
        normalizeSecureCookies(authFile(def.role));
    });
}
