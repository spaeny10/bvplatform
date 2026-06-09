# Ironsight e2e smoke harness

Playwright smoke tests for the Ironsight stack, run against the **bob test
deploy** (`192.168.103.48:3000`) through an SSH tunnel. Standalone npm
package — nothing here touches the Go/Next builds.

## Prerequisites

1. **Node 18+** and `npm install` in this directory, then
   `npm run install-browsers` (downloads Chromium).

2. **The tunnel.** The public URL is oauth2-proxy (Google SSO) gated, which
   Playwright cannot drive; bob's port 3000 serves the app-native
   `/auth/login` instead. Next.js rewrites `/auth`, `/api`, `/hls`
   same-origin, so one forwarded port carries the whole app
   (`/ws` does **not** proxy — an allowlisted WebSocket console error is
   expected). Open it with either:

   ```powershell
   .\scripts\tunnel-bob.ps1     # PowerShell
   ./scripts/tunnel-bob.sh      # bash
   # both run: ssh -N -L 13000:192.168.103.48:3000 fred
   ```

3. **Admin password** of the test deploy:

   ```
   ssh fred
   ssh jetstream@192.168.103.48 'sudo grep ADMIN_PASSWORD /etc/ironsight-test/db.env'
   ```

   Copy `.env.example` to `.env` and set `IRONSIGHT_ADMIN_PASSWORD` (and
   `IRONSIGHT_BASE_URL=http://127.0.0.1:13000` for tunnel runs).

4. **Seed (for the non-admin roles).** The supervisor/operator/manager/
   customer specs use seeded demo users (`rmorgan`, `jhayes`,
   `marcus.webb`, `spierce`, password `demo123`). If they 401, run the
   idempotent seed on bob:

   ```
   cd /home/jetstream/external/bvplatform-main && \
   sudo docker compose --env-file /etc/ironsight-test/db.env \
     -f docker-compose.yml -f deploy/test/docker-compose.test.yml \
     run --rm --entrypoint /app/seed api --all
   ```

   Unseeded roles are soft-skipped (the run stays green); admin login is
   mandatory and hard-fails the setup project.

## Running

```bash
npm run smoke      # @core only: auth, public pages, NVR grid, popout
npm run smoke:all  # everything except the crawler (@core + @backburner)
npm run crawl      # route crawler -> results/crawl/<role>/<route>.png
npm run report     # open the HTML report
npm run registry   # results/results.json -> results/feature-status.json
```

Auth setup runs as a Playwright `setup` project before the specs and
writes per-role cookies to `.auth/<role>.json` (gitignored).

## Descope awareness

A feature-flag rollout is in flight: `GET /api/v1/features` may return
only 4 legacy flags (older deploy) or the full 2026-06 descope map where
parked flags are `false` and parked pages 404. `@backburner` specs check
the flag first: explicitly-false flag -> assert the Next 404 and annotate
`parked`; flag true or unknown -> assert the page renders. Neither state
fails the run by itself.

## Video assertions (HEVC reality)

The fleet records H.265. Bundled Chromium has no HEVC decoder, so the
default assertion tier is network-level (Tier 1): a 200 `.m3u8` plus a 200
media segment under `/api/live/`, with the only acceptable error overlay
being the exact "Browser cannot decode this stream (HEVC support missing)"
headline (annotated `decode-unavailable`). To assert real decode (Tier 2:
`video.currentTime` advances), run a browser that can decode HEVC and set:

```
PW_CHANNEL=chrome
IRONSIGHT_EXPECT_DECODE=1
```

The NVR grid test passes when >=1 camera streams — the cameras are
cellular, single-camera dropouts are real and recorded as per-camera
annotations rather than failures.

## Flake notes

- **watchtower may swap images mid-run** on bob — a sudden burst of 502s
  or a container restart mid-suite is most likely a deploy, not a
  regression. Re-run before digging.
- **`/ws` console errors are expected** through the tunnel (see above) and
  allowlisted in `src/fixtures.ts`.
- Retries are 1 locally / 2 in CI; traces and videos are kept on failure
  (`npm run report`).

## CI (phase 2 plan)

Self-hosted runner on fred, nightly schedule, no tunnel needed:
`IRONSIGHT_BASE_URL=http://192.168.103.48:3000`. Publish
`results/feature-status.json` as the registry-status artifact and the
HTML report on failure.
