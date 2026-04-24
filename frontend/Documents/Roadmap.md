# Ironsight Roadmap — Status & Next Steps

Snapshot of what's shipped, what's queued, and what's explicitly out of scope. Update this file whenever a phase transition happens so the next engineer (or the next-you) can resume without paging in the whole conversation.

Last updated: 2026-04-24.

---

## 1. Where we are

**Phase 1** (cross-platform / single-container Linux) — **done.**
**Phase 2** (multi-container production shape) — **done.** All four items shipped.
**Phase 3** (horizontal-scale + compliance hardening) — four items queued, none in flight.

Production deployment on a single Ubuntu host using `docker compose up -d` is fully supported today. Scaling the `api` tier past one replica is blocked only by the ONVIF event subscriber split (P3.4 below). Everything else — WS fanout, MediaMTX runtime updates, export processing, retention — is already multi-replica safe.

---

## 2. What shipped

Items are grouped by the workstream they belong to, not chronologically.

### 2.1 Container readiness (Phase 1)

*Outcome: the Go server produces a clean statically-linked Linux ELF and docker-compose brings up the full stack.*

- **Cross-platform build guards** — [internal/streaming/mediamtx_kill_unix.go](../../internal/streaming/mediamtx_kill_unix.go) + [`…_windows.go`](../../internal/streaming/mediamtx_kill_windows.go); [internal/api/storage_unix.go](../../internal/api/storage_unix.go) reads `/proc/mounts` + `syscall.Statfs`; Windows equivalents isolated behind build tags.
- **FFmpeg default switches by OS** — Windows devs keep `C:\ffmpeg\bin\ffmpeg.exe`; Linux/Docker expects `ffmpeg` on `$PATH` (installed by the Dockerfile).
- **MediaMTX spawn is opt-in** — `EMBEDDED_MEDIAMTX=0` in compose; external container lifecycle owned by Docker.
- **Env-varised service addresses** — `MEDIAMTX_WEBRTC_ADDR`, `MEDIAMTX_RTSP_ADDR`, `MEDIAMTX_API_ADDR`, `AI_YOLO_URL`, `AI_QWEN_URL`.
- **Dockerfile + docker-compose + .env.example + .dockerignore** — complete first-run stack.
- **Required secrets fail fast** — `JWT_SECRET` and `ADMIN_PASSWORD` use `${VAR:?…}` in compose so a missing secret blocks boot instead of silently using the unsafe default.

Deep detail: [MasterDeployment.md](MasterDeployment.md).

### 2.2 Phase 2 — multi-container shape

*Outcome: the stack supports the api/worker split, safe WS fanout across future replicas, and runtime MediaMTX updates without process restarts.*

- **P2.1 — Worker binary split.** New [cmd/worker/main.go](../../cmd/worker/main.go) (15 MB Linux ELF) owns retention + VLM indexer + export worker. API owns HTTP, recording engine, ONVIF subscribers. `RUN_WORKERS=false` on the api service (default in compose), `true` for single-binary dev.
- **P2.2 — Export worker fix.** Worker was dead code before this — `Submit()` was defined but never called, exports silently died on creation. Rewritten to DB-polling with atomic claim (`UPDATE … FOR UPDATE SKIP LOCKED`) + stuck-job requeue on startup + partial indexes on `status='pending'`/`'processing'`.
- **P2.3 — Redis pub/sub bridge for WS hub.** In-memory hub transparently fans out to a Redis channel when `REDIS_URL` is set; sender-ID dedup, auto-reconnect with exponential backoff. Profile-gated `redis` service in compose (`docker compose --profile scale-out up -d`).
- **P2.4 — MediaMTX HTTP control API.** Runtime path add/remove via `POST /v3/config/paths/add/{name}` and `DELETE …`, not YAML rewrite. Fixed a significant latent bug: in external (docker) mode, camera-add used to silently not register the new path with MediaMTX (config-file reload isn't automatic). New `PersistConfig()` method for durability-only writes so camera-add no longer restarts the whole MediaMTX process.

Deep detail: [MasterDeployment.md §2, §9](MasterDeployment.md).

### 2.3 Rebrand tooling

*Outcome: changing the product name or accent colors is a one-file edit.*

- **[branding.ts](../src/lib/branding.ts)** — single source of truth for name, tagline, description, support emails, logo, three brand colors.
- **Dynamic PWA manifest** — [src/app/manifest.ts](../src/app/manifest.ts) reads from `BRAND`.
- **Generated logo component** — [Logo.tsx](../src/components/shared/Logo.tsx) auto-detects the capital-letter split point so different names ("SkyWatch", "BluVigil") render correctly.
- **CSS brand cascade** — `--brand-primary/secondary/tertiary` injected from `BRAND.colors` at the root; `--accent-orange` etc. alias these so every component picks up a brand color change.
- **Light / dark theme infrastructure** — `[data-theme="light"]` block in globals.css ready for future global-light toggle; portal's intentional warm-cream palette scoped to `.portal-shell`.
- **Hardcoded-hex sweep** — 100+ `#XXXXXX` values replaced with CSS tokens across the CSS files (token definitions at the top of globals.css intentionally preserved).
- **Backend `PRODUCT_NAME` env var** — flows into evidence ZIP README header + footer. Cross-platform: frontend reads `BRAND`, backend reads env; duplicated intentionally.

Deep detail: [Rebrand.md](Rebrand.md), [HOUSEKEEPING.md](HOUSEKEEPING.md).

### 2.4 Other infrastructure shipped earlier

These landed in prior sessions but are part of the current architecture anyone picking up should know about:

- **Site-scoped recording policy** — retention, mode, buffers, triggers, schedule all live on the site row; cameras inherit. See [Ironsight_Architecture.md §8](Ironsight_Architecture.md).
- **Multi-window recording schedule editor** — shared component used by both Monitoring Schedule and Recording Schedule tabs.
- **VCA bidirectional sync** — Milesight cameras' intrusion / linecross / region-entrance / loitering rules pull back into the platform DB, not just push.
- **Milesight vendor config panels** — OSD, image, audio, day/night, privacy mask, auto-reboot, PTZ presets, alarm I/O exposed via a generic `/api/cameras/{id}/milesight/config/{panel}` allowlist + `MilesightAdvanced` React modal.
- **Mobile planning** — tech stack + scope decisions captured in [MobileAppPlan.md](MobileAppPlan.md). Paused.

---

## 3. What's next

Priority order based on "value if you never do the rest / effort to land":

### 3.1 Phase 3 items (container hardening)

| # | Item | Effort | Blocks what |
|---|---|---|---|
| **P3.1** | Postgres advisory-lock leader election in the worker | ~1 day | Running `worker` with `replicas>=2` for HA. Today it's a single-replica SPOF; a crash creates a retention/indexer/export gap until restart. |
| **P3.2** | Object-store recording tier | ~1 week | Real multi-host recording where recorders sit close to cameras, not central. Today `recordings` is a single named volume; fine for one host, painful at scale. |
| **P3.3** | Digital signing of exported evidence clips | ~2-3 days | Chain-of-custody compliance story. Referenced but not implemented in [Ironsight_Architecture.md §5](Ironsight_Architecture.md). |
| **P3.4** | ONVIF event subscriber split out of api | ~3-4 days | Horizontally scaling the `api` tier. Subscribers today are coupled to the recording-engine event-mode trigger path; decoupling requires a small message-bus layer between worker (subscriber) and api (recorder). |

**Recommended order:** P3.1 → P3.4 → P3.3 → P3.2.

- P3.1 is small and immediately improves reliability (worker SPOF is the weakest link in the current stack).
- P3.4 unlocks api horizontal scaling which is the other "real Phase 3" win.
- P3.3 is a compliance checkbox — defer until a real customer asks.
- P3.2 is the biggest lift and only matters at ~10+ site scale.

### 3.2 Mobile apps (separate track)

Customer app first, operator app later. Paused. See [MobileAppPlan.md](MobileAppPlan.md) for:
- Tech stack (React Native, one repo, two build flavors)
- Scope for the customer app MVP
- **PTZ-over-WebRTC-on-cellular spike** — the 2-day validation to run *before* committing to the full build
- Backend prep (~3 days: thumbnail endpoint, device-ID JWT claim, TURN mint endpoint, coturn container)
- Container count goes from 8 → 9 when coturn lands

### 3.3 Other deferred items

Items that are known and not forgotten, but not actively scheduled:

- **Visual schedule editor for recording windows** — right now operators can edit monitoring *and* recording schedules via the same window editor. UX is fine.
- **White-label per customer** — intentionally not supported; see [Rebrand.md §5.5](Rebrand.md).
- **Full offline mode for mobile** — intentionally out of scope; cached thumbnails + incident metadata is the ceiling.
- **Push notifications** — planned for after safety-engine events land; not needed for customer app MVP.

---

## 4. Current deployment surface

### 4.1 Container count

| Deployment | Containers |
|---|---|
| Dev / lab (single binary) | 6 — db + mediamtx + yolo + qwen + api + frontend |
| Single-host production (default compose) | 8 — above + worker + caddy |
| Multi-replica API (scale-out profile active) | 9 — above + redis |
| Multi-host production (future) | 9 central + per-site recorders |

### 4.2 Which container owns what

- **api** — HTTP/WS, recording engine (FFmpeg), HLS server, MediaMTX integration, ONVIF event subscribers (still here, blocks api replicas >1)
- **worker** — retention purge, VLM indexer, video-export concat worker
- **mediamtx** — RTSP relay + WHEP + HTTP control API (9997 internal-only)
- **yolo / qwen** — AI services (GPU-friendly, optional)
- **db** — Postgres + TimescaleDB
- **frontend** — Next.js SPA (stateless, scales freely)
- **redis** (profile `scale-out`) — WS fanout across api replicas
- **caddy** (production) — TLS + reverse proxy

### 4.3 Build commands

```bash
# Windows dev
go build ./...                                    # both binaries
./server.exe                                       # runs api + workers in-process (RUN_WORKERS=true default)

# Linux cross-compile (what the Dockerfile does)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/api ./cmd/server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/worker ./cmd/worker

# Docker stack
docker compose up -d --build                       # default: 7 services
docker compose --profile scale-out up -d          # adds redis
```

---

## 5. How to pick this up next session

1. Open [MasterDeployment.md](MasterDeployment.md) first — everything in §2–§9 reflects the current state.
2. Check this file for the priority order of remaining work.
3. For Phase 3 items, the implementation is localized — each one touches ~1-2 packages, not a cross-cutting refactor.
4. Mobile is an independent track; the plan document is self-contained.

**Known good state right now:**
- Go build: both binaries compile clean on Windows + cross-compile to Linux ELF.
- Frontend: `npx tsc --noEmit` + `npx next build` both pass.
- No tests exist in this repo — verification is "it builds + smoke-tests in browser + logs look right." Adding a test layer is a separate unscheduled task.

**Known gotchas when resuming:**
- Dev server (`next dev`) gets confused by large refactors + mixing `next build` artifacts. Fix: `rm -rf frontend/.next` and restart the dev server. See the note in an earlier session about "Cannot find module './682.js'".
- On Windows the Go `server.exe` started via MSYS `nohup` is vulnerable to shell-session exit killing it. Not a concern for Linux/Docker production.
- The Go server auto-migrates schema at startup (`ALTER TABLE … ADD COLUMN IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS`). Rollback to a previous image is safe — additive-only.

---

## 6. Document index

- [Ironsight_Architecture.md](Ironsight_Architecture.md) — product / runtime architecture, RBAC, SOC + customer workflows, recording policy model (§8), Milesight integration (§7).
- [MasterDeployment.md](MasterDeployment.md) — Docker + Ubuntu deployment, GPU setup, firewall, upgrade strategy, operational runbook.
- [Rebrand.md](Rebrand.md) — what changes when the product name or colors change, complete file inventory with rationale for why each file is or isn't wired to `BRAND`.
- [HOUSEKEEPING.md](HOUSEKEEPING.md) — design token system, light/dark mode, theming cascade.
- [MobileAppPlan.md](MobileAppPlan.md) — mobile apps (customer + operator), RN tech stack, PTZ spike plan, backend prep, deferred push work.
- [MSDriver/MILESIGHT_DRIVER_BRIEF.MD](MSDriver/MILESIGHT_DRIVER_BRIEF.MD) — camera driver implementation notes.
- **Roadmap.md** (this file) — cross-cutting status + priority.
