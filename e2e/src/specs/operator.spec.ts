import { test, expect, expectNoErrorBoundary } from '../fixtures';
import { authFile, hasAuthState } from '../helpers/auth';
import { gotoFlagAware } from '../helpers/flags';

test.use({ storageState: hasAuthState('supervisor') ? authFile('supervisor') : undefined });

test.describe('operator console @backburner', () => {
    test.beforeEach(() => {
        test.skip(!hasAuthState('supervisor'), 'supervisor (rmorgan) login unavailable — seed not run');
    });

    test('/operator renders or is parked', async ({ page }) => {
        if ((await gotoFlagAware(page, '/operator', 'operator_console')) === 'parked') return;
        await expect(page.locator('.op-shell')).toBeVisible({ timeout: 15_000 });
        await expectNoErrorBoundary(page);
    });

    test('/operator/alarm/<id> renders or is parked', async ({ page }) => {
        // Resolve a real alarm id when possible; a garbage id is fine for the
        // parked-404 path but not for asserting a working alarm screen.
        let alarmId = 'e2e-nonexistent-alarm';
        let haveRealAlarm = false;
        const res = await page.request.get('/api/v1/alerts');
        if (res.ok()) {
            const alarms = (await res.json()) as { id?: string; alarm_id?: string }[];
            const first = Array.isArray(alarms) ? alarms.find(a => a.id || a.alarm_id) : undefined;
            if (first) {
                alarmId = String(first.id ?? first.alarm_id);
                haveRealAlarm = true;
            }
        }

        if ((await gotoFlagAware(page, `/operator/alarm/${alarmId}`, 'operator_console')) === 'parked') return;

        if (!haveRealAlarm) {
            test.info().annotations.push({
                type: 'no-alarms',
                description: 'no active alarms to open — only asserting the route does not crash',
            });
        }
        await expectNoErrorBoundary(page);
    });
});
