# 09 — System & infrastructure

The cross-cutting plumbing every product area rides on: Prometheus metrics,
structured logging with Sentry fan-out, the goose migration framework, demo
seeding, signed media-token serving, the HEVC recorded-playback transcode
bridge, evidence chain-of-custody manifests, and the bob→fred test/promote
deploy pipeline. Almost none of this has a UI — status here is verified
against the Go source, ops scripts, and CI workflows, not screenshots.

## Prometheus metrics {#prometheus-metrics}

| Field | Value |
|---|---|
| **ID** | `prometheus-metrics` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Prometheus exposition endpoint serving HTTP request/latency, recording-engine, DB-pool, WebSocket-hub, RBAC-cache, and migration-version series from a non-default registry (`internal/metrics`). The `METRICS_AUTH` env var picks unauthenticated network-trust (`none`, prod default) or session-auth (`sso`). |
| **Frontend** | — |
| **Routes** | `GET /metrics` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [metrics.md](../metrics.md), [configuration.md](../configuration.md) |
| **Smoke test** | `curl -s http://192.168.103.48:8080/metrics \| grep ironsight_http_requests_total` on bob returns counter series; hit any API page and the counter increments on the next scrape. |
| **Notes** | With `METRICS_AUTH=none` the endpoint has NO app-layer auth — NPM must IP-restrict `/metrics` to the monitoring LXC / trusted CIDR or it leaks internal observability data (docs/metrics.md security section). Scrape + alert configs live in `deploy/monitoring/`; the Prom/Grafana LXC itself is platform-ops work outside this repo. Metrics middleware is registered before the Recoverer so handler panics are counted as 500s. Full series catalog in docs/metrics.md — keep it updated when adding metrics. |

## Structured logging + Sentry {#structured-logging-sentry}

| Field | Value |
|---|---|
| **ID** | `structured-logging-sentry` |
| **Tier** | core |
| **Status** | working |
| **Definition** | JSON `slog` logging for the whole backend: per-request UUIDv7 `request_id` middleware binds request-scoped loggers into context, the stdlib `log` package is bridged into slog, and a wrapping handler forwards Error-level records to Sentry/GlitchTip with secret redaction (`internal/logging`). |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [configuration.md](../configuration.md) |
| **Smoke test** | `docker logs ironsight-test-api --tail 20` on bob shows one JSON object per line with `request_id`, `route`, and `level` fields; request any API URL and its access line appears. |
| **Notes** | Backend-only, no UI. `SENTRY_DSN` empty (the current state of every deployment — the GlitchTip LXC is not provisioned yet) makes the Sentry path a clean no-op; the forwarding handler, tag promotion, and redaction are unit-tested (`internal/logging/sentry_test.go`) but have never been exercised against a live DSN. `LOG_LEVEL=debug` on test, `info` on prod. Attrs whose keys contain password/secret/token/key/credential/auth are replaced with `[REDACTED]` before leaving the process. |

## DB migrations (goose) {#db-migrations}

| Field | Value |
|---|---|
| **ID** | `db-migrations` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Schema managed with pressly/goose v3: 31 numbered SQL migrations in `migrations/` are embedded into the api binary (`//go:embed`) and applied automatically at startup, so a deploy can never skip a schema change. A `/app/migrate` operator CLI covers status/version/up/down and scaffolding new files. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | goose_db_version |
| **Flag** | — |
| **Docs** | [migrations.md](../migrations.md), [id-conventions.md](../id-conventions.md), [ci.md](../ci.md) |
| **Smoke test** | `docker compose exec api /app/migrate status` shows all 31 applied; `SELECT max(version_id) FROM goose_db_version WHERE is_applied` returns 31 (or higher). |
| **Notes** | Forward migrations are idempotent by convention (safe to re-run on a partially-applied DB — required because 0001 baselines fred's pre-existing schema). CI runs an up → reset → up round-trip against TimescaleDB on every PR. `migrate create` is the one subcommand that writes to the on-disk `migrations/` dir (the embed FS is read-only) — run it on a dev box, not in the container. Applied version is exported as the `ironsight_goose_migration_version` gauge, see [[prometheus-metrics]]. |

## Demo data seed {#demo-seed}

| Field | Value |
|---|---|
| **ID** | `demo-seed` |
| **Tier** | core |
| **Status** | working |
| **Definition** | One-shot CLI (`/app/seed`, `cmd/seed` + `internal/seed`) that populates a dev/test database with the demo portfolio — 3 orgs, 5 sites, ~30 days of dispositioned events — and demo platform users (3 SOC operators, 4 portal users). Dev/test tooling only; it never runs in production or at server startup. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [seeding.md](../seeding.md) |
| **Smoke test** | `docker compose run --rm api /app/seed --dry-run --all` logs what would be inserted without writing; run without `--dry-run` on a scratch DB and log in as a demo operator. |
| **Notes** | P1-B-09 removed all seeding from api startup — docs/seeding.md still describes an `IRONSIGHT_SEED_DEMO=true` startup path that no longer exists anywhere in `cmd/server` (doc drift, needs a fix). Demo account passwords come from `SEED_DEMO_PASSWORD` / `_OPERATOR` / `_PORTAL` env vars (LOCAL-08 killed the hardcoded `demo123`). Refuses to run unless at least one of `--portfolio`/`--users`/`--all` is passed. |

## Media token auth {#media-token-auth}

| Field | Value |
|---|---|
| **ID** | `media-token-auth` |
| **Tier** | core |
| **Status** | working |
| **Definition** | All recorded media (MP4 segments, VOD HLS playlists, snapshots) is served through short-lived signed URLs: an authenticated client mints a JWT URL via `POST /api/media/mint`, and `GET /media/v1/{token}` re-verifies tenant scope against the live DB on every serve before streaming bytes (P1-A-03). |
| **Frontend** | `frontend/src/lib/media.ts`, `frontend/src/components/shared/SignedImage.tsx`, `frontend/src/components/shared/HLSVideoPlayer.tsx` |
| **Routes** | `POST /api/media/mint` · `GET /media/v1/{token}` |
| **Tables** | audit_log |
| **Flag** | — |
| **Docs** | [media-auth.md](../media-auth.md) |
| **Smoke test** | On test.ironsight, play any recorded event clip; paste its `/media/v1/...` URL into an incognito tab — it plays until TTL expiry (default 5 min); flip one token character → request is rejected. |
| **Notes** | Underpins playback, exports, and every snapshot thumbnail. Cross-tenant access returns 404, never 403 (don't leak camera existence). Every serve is audit-logged through a batched ring buffer — a crash loses at most ~5 s of `media_serve` rows. The live path no longer uses media tokens ([[live-hls-pipeline]] rides the session cookie via `/api/live/*`); the `live-hls` token kind in `media_v1.go`/`media.ts` is dead code. Known gap: `/exports/*` evidence-ZIP downloads still bypass this scheme (bare FileServer, gated at create time) — open follow-up in docs/media-auth.md. |

## HEVC recorded-playback transcode {#hevc-transcode}

| Field | Value |
|---|---|
| **ID** | `hevc-transcode` |
| **Tier** | core |
| **Status** | working |
| **Definition** | The fleet records H.265 by policy, which Chrome/Firefox cannot decode in `<video>`. On segment serve, the media handler transcodes HEVC to H.264 on demand with ffmpeg into a per-camera `.h264-cache` dir, serializing concurrent requests per path through `transcodeRegistry` so duplicate ffmpeg runs never spawn (LOCAL-11, `internal/api/media_transcode.go`). |
| **Frontend** | — |
| **Routes** | `GET /media/v1/{token}` |
| **Tables** | segments |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Play a recorded HEVC event clip in Chrome on test.ironsight — first play stalls ~2-5 s (transcode), replay starts instantly; a `.h264-cache/` dir appears beside the source segment on disk. |
| **Notes** | H.264 sources pass through untouched (fast path). Cache files are written atomically (.tmp + rename) and GC'd by the retention sweeper together with the source segment; 90 s ffmpeg timeout caps a wedged process. Live HEVC is solved separately — mediamtx native HLS plus the hvcC `array_completeness` byte-patch in the live proxy (PR #47), see [[live-hls-pipeline]]. Codec choice is fleet policy: playback gaps get fixed server-side, never by reconfiguring cameras to H.264. |

## Evidence chain-of-custody manifests {#evidence-manifests}

| Field | Value |
|---|---|
| **ID** | `evidence-manifests` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Every generated evidence artifact (clip export, compliance PDF, evidence share) gets an ed25519-signed manifest row in the append-only `evidence_manifests` table, anchoring the artifact's SHA-256, cameras, time window, and creator so courts/insurers can verify authenticity with only the public key (P3-INFRA-03). |
| **Frontend** | — |
| **Routes** | `GET /api/v1/evidence/manifests` · `GET /api/v1/evidence/manifests/{id}` · `GET /api/v1/evidence/manifests/{id}/verify` |
| **Tables** | evidence_manifests |
| **Flag** | `evidence_sharing` |
| **Docs** | [chain-of-custody.md](../chain-of-custody.md) |
| **Smoke test** | Export an event clip, then `GET /api/v1/evidence/manifests` — the newest row references the export; `GET /api/v1/evidence/manifests/{id}/verify` returns valid. |
| **Notes** | Backend-only — no frontend calls these routes (api-coverage Table B); consumers are auditors and the offline `cmd/verify-manifest` CLI, so "working" is judged on write + verify, not UI. If `EVIDENCE_ED25519_PRIVATE_KEY` is unset or invalid, manifests are still written but UNSIGNED (warning in logs) — set the key before relying on the courtroom story. Manifest writes are async (`go writeManifest(...)`) and keep firing even with the flag off, because clip exports stay core; the flag parks the share-lifecycle UI surface (evidence shares area, 06). Revival cost: UI only — backend is complete with unit tests. |

## Test environment + GHCR pipeline {#test-env-pipeline}

| Field | Value |
|---|---|
| **ID** | `test-env-pipeline` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Every push to main builds api/worker/frontend/yolo/qwen images and pushes them to `ghcr.io/spaeny10/ironsight-*:latest` (`.github/workflows/build-test-images.yml`); bob (192.168.103.48) runs the test stack from those tags, and watchtower polls GHCR every 5 minutes and auto-restarts updated containers (OPS-01). |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [ci.md](../ci.md) |
| **Smoke test** | Push to main; within ~10 min `curl -s http://192.168.103.48:8080/api/health` shows the new `git_sha`; `docker logs watchtower --tail 20` on bob shows the pull + restart. |
| **Notes** | `deploy/test/` is bob-local and gitignored upstream (PR #34) — copies present in a checkout are reference snapshots; bob's live files are authoritative. GHCR packages are public so bob pulls unauthenticated (accepted pre-launch posture; revisit before source confidentiality matters). Test stack divergences: separate Postgres at `/tank/data/ironsight-test/pgdata`, 1-day retention, 7B-AWQ Qwen — safe to wipe and reseed at any time. For sub-minute iteration there is also the scp+docker-cp fast-deploy loop that bypasses GHA entirely (off-pipeline, leaves bob ahead of `:latest` until the next push). Cross-refs [[promote-to-prod]], [[health-endpoint]]. |

## Promote-to-prod gate {#promote-to-prod}

| Field | Value |
|---|---|
| **ID** | `promote-to-prod` |
| **Tier** | core |
| **Status** | working |
| **Definition** | `deploy/promote-to-prod.sh` promotes a test-verified image to fred: confirms bob is actually running the target SHA (via `/api/health` `git_sha`), checks 7 days of consistency-check reports for zero divergence, retags GHCR `:sha-<sha>` → `:prod-YYYY-MM-DD-<short-sha>`, SSHes to fred to pull + restart api/worker, and appends a JSON record to fred's promote log. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | — |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | `./deploy/promote-to-prod.sh --dry-run <full-sha-running-on-bob>` walks all 5 steps, prints the would-be commands, exits 0 without touching anything. |
| **Notes** | The consistency gate currently SOFT-FAILS: if no reports exist on bob it warns and skips instead of blocking (the consistency-check scheduler isn't configured on bob yet — OPS-01 follow-up; the gate is weaker than it looks until then). Needs workstation `docker login ghcr.io` plus SSH to bob and fred. fred's compose still references `localhost/ironsight/*` image names, so the script pulls the GHCR prod tag on fred and retags locally — drop that shim if fred's compose moves to GHCR refs directly. Cross-refs [[test-env-pipeline]], [[health-endpoint]]. |

## Public health endpoint {#health-endpoint}

| Field | Value |
|---|---|
| **ID** | `health-endpoint` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Unauthenticated `GET /api/health` liveness probe returning `{"status":"ok","git_sha":"<sha>"}`. Consumed by Docker HEALTHCHECK, external uptime monitors, the frontend's `healthCheck` helper, and the promote gate's SHA comparison. |
| **Frontend** | — |
| **Routes** | `GET /api/health` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | `curl -s http://192.168.103.48:8080/api/health` returns `status: ok` and a 7-char `git_sha` matching the commit watchtower last deployed. |
| **Notes** | `git_sha` is injected at build time via ldflags (`internal/buildinfo`); it is empty in local builds without ldflags — expected, not a bug. Deliberately a fixed dependency-free payload: do not add DB or subsystem checks here, that's what the authenticated `GET /api/system/health` dashboard endpoint is for (05-auth-users-admin area). Cross-refs [[promote-to-prod]], [[test-env-pipeline]]. |
