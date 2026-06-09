import { defineConfig } from '@playwright/test';
import * as dotenv from 'dotenv';
import * as path from 'path';

// .env lives next to this config (gitignored). Loaded here so it is also
// available inside every worker process (workers re-evaluate the config).
dotenv.config({ path: path.join(__dirname, '.env') });

const baseURL = process.env.IRONSIGHT_BASE_URL || 'http://127.0.0.1:13000';

export default defineConfig({
    testDir: './src',
    timeout: 60_000,
    expect: { timeout: 10_000 },
    retries: process.env.CI ? 2 : 1,
    workers: 2,
    reporter: [
        ['list'],
        ['html', { outputFolder: 'playwright-report', open: 'never' }],
        ['json', { outputFile: 'results/results.json' }],
    ],
    use: {
        baseURL,
        trace: 'on-first-retry',
        screenshot: 'only-on-failure',
        video: 'retain-on-failure',
        ignoreHTTPSErrors: true,
        actionTimeout: 15_000,
        navigationTimeout: 30_000,
        launchOptions: {
            // Video tiles autoplay muted; this removes any user-gesture gating
            // so currentTime advances without a click.
            args: ['--autoplay-policy=no-user-gesture-required'],
        },
        // PW_CHANNEL=chrome runs against branded Chrome (has HEVC decode on
        // most hosts); unset = bundled Chromium (Tier-1 network checks only).
        ...(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {}),
    },
    projects: [
        {
            name: 'setup',
            testMatch: /setup[\\/].*\.setup\.ts/,
        },
        {
            name: 'smoke',
            testMatch: /specs[\\/].*\.spec\.ts/,
            dependencies: ['setup'],
        },
    ],
});
