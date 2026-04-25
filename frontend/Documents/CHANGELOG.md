# Ironsight — Commit Log

Living changelog mapping every commit on `main` to the engineering
intent behind it. Useful for: onboarding a new engineer, briefing a
UL 827B consultant, reconstructing why a particular control was
implemented the way it was, and feeding into release notes.

Entries are reverse-chronological (newest first) within each phase.
Each entry lists:

- The commit hash and date
- A one-line summary
- The phase it ships against (where applicable — UL 827B / TMA-AVS-01
  / operational hardening / setup)
- The files it touched and the magnitude of the change
- Why the commit happened — the engineering rationale, not just the
  diff

When updating, keep entries in chronological order **within their
phase**, and put the most recent phase at the top.

---

## Phase D — Polish & Customer-Facing Completeness · 2026-04-25

These commits close the last visible gaps before a customer-facing
demo or UL 827B walkthrough.

### `77405ab` — Polish: route login wordmark + particle colors through BRAND module
**Date:** 2026-04-25 11:24 CDT
**Files:** `frontend/src/app/login/page.tsx` (1 file, +24/-21)

The Phase D audit flagged the login page as the worst offender for
hardcoded color literals — 50+ hex values that wouldn't adapt to
light mode and a hand-rendered "IRON-S-ight" wordmark that would
literally render the wrong product name after a rebrand. Replaced
the inline wordmark with `<Logo />` (derives letters from
`BRAND.name`, dots from `BRAND.colors`) and routed the floating
particle colors through `BRAND.colors.{primary,secondary,tertiary}`
instead of hardcoded hex.

### `f42c9f6` — Polish: loading skeletons on admin user list + portal site grid
**Date:** 2026-04-25 11:18 CDT
**Files:** `frontend/src/components/shared/Skeleton.tsx` (new), `frontend/src/app/admin/page.tsx`, `frontend/src/app/portal/page.tsx` (3 files, +109/-2)

Phase D audit flagged blank panels during data fetches as a
demo-killer ("appears unresponsive on first impression"). New
shared `Skeleton` component with shimmer keyframes injected at
mount time. Applied to admin "Internal Staff" loading state (4
rows) and the portal site card grid (6 placeholder cards). Other
surfaces (search results, evidence viewer, /reports cards)
already render skeleton-equivalent loading states.

### `47b292e` — CHANGELOG: document the supervisor reports + previous commit
**Date:** 2026-04-25 11:11 CDT
**Files:** `frontend/Documents/CHANGELOG.md` (1 file, +26/-2)

Self-documenting policy: every commit that ships compliance-
relevant work updates the CHANGELOG in the same push so the
history is always navigable from one document.

### `1181200` — Supervisor /reports surface — SLA, verification queue, evidence shares
**Date:** 2026-04-25 11:08 CDT
**Files:** 11 files (+1,023/-3) — including new `frontend/src/app/reports/` route + 3 card components

Until this commit, every reporting/audit feature added today only
had a backend; supervisors couldn't use any of it. SOC supervisors
land on `/operator` (not `/admin`) by route policy, so the reports
needed their own home that both supervisors and admins can reach.
New `/reports` route gated to `[admin, soc_supervisor]` with three
tabs: Performance (SLA report card with date presets + CSV
download), Verification (queue of un-verified high-severity events
with one-click ✓ Verify), Evidence shares (per-incident lookup
with active/revoked/expired tokens + Revoke). Nav link added to
both `/operator` (gated to supervisor/admin) and `/admin` topbars.
New API helpers documented in-place for future reuse.

### `eb961fd` — Add CHANGELOG.md — every commit documented
**Date:** 2026-04-25 10:53 CDT
**Files:** `frontend/Documents/CHANGELOG.md` (new, +421)

This document. Living changelog mapping every commit on `main` to
the engineering intent behind it. Updated in-place on every
follow-up commit.

### `5efb6b2` — Polish: visible feedback on admin apiFetch failures
**Date:** 2026-04-25 10:50 CDT
**Files:** `frontend/src/app/admin/page.tsx` (1 file, +56/-9)

The Phase D.12 frontend polish audit flagged silent failures on the
admin page — `apiFetch(...)` calls had no error path, so a network
glitch or backend 500 just looked like "the action did nothing."
Reframed `apiFetch` to throw on non-2xx (carrying the response body
into the error message) and introduced `useApiAction()` — a toast-
wrapped runner that admin handlers route through. Successes show
green toasts; failures show red toasts with the server message.
Migrated `CompanyCard`'s save/delete flows; remaining call sites
will migrate as related work touches them.

### `33dd263` — Cross-tenant RBAC enforcement + frontend share-endpoint wireup (Phase D.14)
**Date:** 2026-04-25 10:47 CDT
**Files:** `internal/database/platform_db.go`, `internal/api/platform.go`, `frontend/src/lib/ironsight-api.ts`, `UL827B_Compliance.md` (4 files, +247/-19)

Closed two ship-blockers from the customer-portal audit:

1. **Cross-tenant data scoping (A.13a)** — the original
   `HandleListSites` returned every site in the database to any
   authenticated caller. Replaced with `database.CallerScope` +
   `ListSitesScoped`, which filters at the SQL layer based on JWT
   claims plus a fresh DB lookup of `users.assigned_site_ids`. SOC
   roles bypass; customer / site_manager get their organization's
   sites only. Smoke-tested cross-tenant: ACME customer can't see
   Zenith's site, gets 404 (not 403) so existence isn't leaked.
2. **Evidence share frontend wireup** — `createEvidenceShareLink`
   was POSTing to `/api/v1/evidence/share` (which doesn't exist) and
   silently mocking. Repointed to the real endpoint we built in
   Phase B.8. Removed the silent mock fallback so future regressions
   are loud.

### `ef997d7` — Frontend polish: route brand strings through BRAND module (Phase D.12)
**Date:** 2026-04-25 10:41 CDT
**Files:** `frontend/src/app/admin/page.tsx`, `frontend/src/app/login/page.tsx` (2 files, +6/-4)

The Phase D audit flagged four hardcoded brand-name leaks in
customer-visible text — exactly the inconsistency that makes a UL
827B reviewer or first-time customer think "incomplete rebrand."
Routed each string through the `BRAND` module per the Rebrand.md
checklist:
- "Ironsight / Jetstream employees" → `${BRAND.name} employees…`
- "Ironsight Staff" section title → `${BRAND.name} Staff`
- "Assign only to Ironsight staff" → `… ${BRAND.name} staff`
- Login subtitle "Surveillance Platform" (placeholder that
  contradicted `BRAND.tagline`) → `BRAND.tagline`

### `ff15847` — AVS factor capture in operator disposition UI (Phase D.13)
**Date:** 2026-04-25 10:36 CDT
**Files:** `frontend/src/lib/ironsight-api.ts`, `frontend/src/components/operator/ActiveAlarmView.tsx`, `frontend/src/components/operator/AVSFactorChecklist.tsx` (new), `UL827B_Compliance.md` (4 files, +235/-8)

Closed the last 🟡 in the compliance doc (I.6). The TMA-AVS-01
backend has been live since `b7a60c3`; this commit puts the
structured 11-factor checklist in front of the operator at
disposition time, where the attestations actually get captured.
Three priority bands (Foundational / Corroborating / Priority
signals); live score badge updates on every toggle, color-coded
UNVERIFIED → CRITICAL with a DISPATCH suffix when ≥2. Backend
remains authoritative on the score; frontend preview is UX only.

---

## Phase C — Operational Hardening · 2026-04-25

### `54f98cc` — Worker HA via Postgres advisory-lock leader election (Phase C.9)
**Date:** 2026-04-25 10:30 CDT
**Files:** `internal/database/leader.go` (new), `cmd/worker/main.go`, `UL827B_Compliance.md` (3 files, +207/-3)

The worker container runs retention, the VLM indexer, and the export
queue. Two workers racing on the same export job could double-send
evidence to a customer. Postgres session-scoped advisory locks fit
this perfectly: `pg_try_advisory_lock(key)` is non-blocking, locks
auto-release on connection drop (no etcd needed), and the DB is
already a hard dependency. `AcquireLeader` polls until acquired,
then runs a heartbeat goroutine; on connection failure, the
returned handle's `Lost()` channel closes and the worker shuts
down so a standby can take over. Smoke-tested two replicas:
worker-2 blocks on lock, stop worker-1, worker-2 takes over within
30s.

**Phases C.10 (object-store recording tier) and C.11 (ONVIF
event subscriber split) remain on the post-cert roadmap.**

---

## Phase B — Evidence Integrity & TMA-AVS-01 Readiness · 2026-04-25

### `5f91918` — Evidence-share creation lifecycle (UL 827B D.2)
**Date:** 2026-04-25 10:25 CDT
**Files:** `internal/database/soc_ids.go`, `internal/api/evidence_share.go`, `internal/api/router.go`, `UL827B_Compliance.md` (4 files, +265/-8)

Closed the read/write loop on public evidence shares. Public read
path (`/share/{token}`) had been live since the SOC audit batch.
Added supervisor-only management endpoints: POST `/api/v1/incidents/
{id}/share`, DELETE `/api/v1/shares/{token}`, GET `/api/v1/incidents/
{id}/shares`. Tokens are 256-bit URL-safe random. Default TTL 7
days; hard server-side ceiling at 90 days (UL reviewers reject
"never expires" links). List endpoint denormalizes per-token
`open_count` from `evidence_share_opens`. Smoke-tested 7 paths:
create, public access logged, list with counts, ceiling rejection,
revoke, revoked-now-404, viewer-role-403.

### `259d4c0` — TMA-AVS-01 readiness — scoring + signed-bundle integration
**Date:** 2026-04-25 10:22 CDT
**Files:** `internal/avs/scoring.go` (new), `cmd/server/main.go`, `internal/database/platform_models.go`, `internal/database/platform_db.go`, `internal/api/evidence_export.go`, `UL827B_Compliance.md` (6 files, +420/-12)

A separate-but-adjacent compliance lane: TMA's published Alarm
Validation Score standard governs how monitoring centers
communicate alarm confidence to PSAPs. New `internal/avs` package
holds the 11-factor `Factors` struct + the deterministic 0–4
`ComputeScore` mapping. `security_events` schema gains
`avs_factors`, `avs_score`, `avs_rubric_version` columns. Server-side
scoring: clients submit factors, never the score. Evidence export
bundle (`event.json` + `README.txt`) carries the AVS section,
covered by the existing HMAC signature. Smoke-tested all three
score buckets (4 / 2 / 0).

### `f446829` — Digital evidence signing — UL 827B D.5
**Date:** 2026-04-25 10:04 CDT
**Files:** `internal/evidence/signing.go` (new), `internal/api/evidence_export.go`, `internal/config/config.go`, `.env.example`, `docker-compose.yml`, `UL827B_Compliance.md` (6 files, +298/-23)

Two-layer integrity on evidence bundles: each binary file (clip,
snapshot) gets its SHA-256 in the manifest's `content_hashes` map,
and the manifest itself is HMAC-SHA256-signed by a key held only
by the monitoring center. `SignedZipWriter` wraps `zip.Writer` and
records hashes as files are added. `EVIDENCE_SIGNING_KEY` env var
is hex-encoded; ≥16 bytes required to enable. Smoke-tested:
Python recompute matches embedded signature; in-place edit of
"Test Cam" → "Forged X" produces a different HMAC (tamper detected).

---

## Phase A — UL 827B Auth + Audit Hardening · 2026-04-25

### `b7a60c3` — MFA (TOTP + recovery codes) — UL 827B A.9
**Date:** 2026-04-25 09:57 CDT
**Files:** `cmd/server/main.go`, `internal/auth/mfa.go` (new), `internal/database/db.go`, `internal/api/mfa_handler.go` (new), `internal/api/auth_handler.go`, `internal/api/router.go`, `UL827B_Compliance.md` (7 files, +580/-6)

Optional, opt-in TOTP authentication. Implemented in-house rather
than via third-party library so the parameter set is pinned and
the crypto is inspectable in one ~150-line file: SHA-1, 30s period,
6 digits, ±1 step drift, 160-bit secret. 10 recovery codes per
enrollment, format `xxxx-xxxx-xxxx`, stored as bcrypt hashes,
consumed atomically on use. Two-step enroll: POST `/enroll` returns
secret + recovery codes (only ever shown this once); POST
`/confirm` validates the first code and flips `mfa_enabled`. Login
flow returns `{"mfa_required": true}` with HTTP 401 — no preauth
half-token. Bad MFA counts toward the lockout threshold so a
primary-credential leak doesn't grant unlimited TOTP guesses.
Smoke-tested 8 paths.

### `0892a85` — Formalize audit retention policy (UL 827B B.8 + H.2)
**Date:** 2026-04-25 09:49 CDT
**Files:** `internal/recording/retention.go`, `internal/database/soc_ids.go`, `UL827B_Compliance.md` (3 files, +44/-7)

UL reviewers don't just want absence-of-purge — they want to see
intent. `database.MinAuditRetentionDays = 365` is the single
canonical constant. `RetentionManager` docstring now enumerates
the tables it owns (cameras, segments, exports, thumbnails, hls,
on-disk recordings) and the audit/evidence tables it MUST NOT
touch. Append-only triggers (B.2) remain the runtime backstop;
this commit makes the expectation visible to a future contributor
before they ever reach for a `DELETE`.

### `704981b` — Dual-operator verification (UL 827B four-eyes rule)
**Date:** 2026-04-25 09:47 CDT
**Files:** `cmd/server/main.go`, `internal/database/platform_db.go`, `internal/database/platform_models.go`, `internal/api/platform.go`, `internal/api/router.go`, `UL827B_Compliance.md` (6 files, +218/-24)

Adds the second-supervisor sign-off step for high-severity
dispositions. `security_events` schema gains `disposed_by_user_id`,
`verified_by_user_id`, `verified_by_callsign`, `verified_at`.
`VerifySecurityEvent` runs the entire enforcement in one atomic
UPDATE — the WHERE clause encodes both the four-eyes rule
(`disposed_by_user_id <> verifier`) AND the no-double-verify rule
(`verified_at IS NULL`). POST `/api/v1/events/{id}/verify`
restricted to supervisor/admin. Smoke-tested all four matrix
cells: self → 409, cross → 204, replay → 409, non-supervisor → 403.

### `f867824` — SLA ack tracking + response-time reporting (UL 827B E.1/E.2)
**Date:** 2026-04-25 09:40 CDT
**Files:** `cmd/server/main.go`, `internal/database/platform_db.go`, `internal/api/platform.go`, `internal/api/reports.go` (new), `internal/api/router.go`, `UL827B_Compliance.md` (6 files, +289/-15)

Adds the column trio `active_alarms` needs to answer the question
every UL 827B reviewer asks first: "what was your 95th-percentile
ack time last quarter?" `acknowledged_at`, `acknowledged_by_user_id`,
`acknowledged_by_callsign` (denormalized so a future operator
rename can't rewrite the SLA narrative). `GetSLAReport` groups by
operator or day in one round trip, computing avg/p50/p95 in SQL via
`percentile_cont`. `GET /api/reports/sla` exposes JSON or CSV with
`from`/`to`/`group` filters. Smoke-tested with seeded alarms:
OP-001 p95=43.5s, OP-002 p95=174s.

### `a175c6f` — UL 827B compliance doc + auth hardening batch 2
**Date:** 2026-04-25 08:38 CDT
**Files:** 10 files (+477/-14)

Birth of the living compliance document at
[frontend/Documents/UL827B_Compliance.md](UL827B_Compliance.md).
Maps every implemented control to file:line evidence.

Four more controls in this batch:
- **A.7 Server-side logout / JWT revocation** — `SignToken` now
  mints a unique `jti` per token; POST `/auth/logout` blocklists it
  via a new `revoked_tokens` table; `RequireAuth` consults the
  blocklist on every request. Login → logout → reuse → 401.
- **A.8 Password rotation** — `users.password_changed_at` column;
  `PasswordExpired` returns true at 180 days; login response
  includes `password_expired` flag. Soft enforcement.
- **B.9 Audit log CSV export** — `GET /api/audit?format=csv`.
  10k cap; for larger ranges, page by date.
- **F.1 CORS allowlist from env** — `ALLOWED_ORIGINS` comma-
  separated; default localhost; production must override.

---

### `e9fff07` — UL 827B auth hardening: batch 1 of 4
**Date:** 2026-04-24 17:27 CDT
**Files:** 7 files (+383/-6)

Four must-fix items from the 827B auth control inventory:
- **A.1 Password complexity** — `internal/auth/password.go`:
  12-char floor, letters + at least one other class, common-
  password blocklist. NIST SP 800-63B-aligned (deliberately not
  the four-class rule).
- **A.3 Account lockout** — 5 failures → 15-min lock. Lock checked
  before bcrypt comparison so a correct password during the lock
  window still fails.
- **A.8 Failed-login auditing** — every 401 from `/auth/login`
  emits an audit_log row with attempted username, IP, and a
  specific reason (`unknown_user`, `bad_password`,
  `bad_password_lockout_triggered`, `locked`). Response stays
  uniform.
- **Rate limiting** — in-memory sliding window, 10 attempts/min per
  IP. Keyed on IP only via `net.SplitHostPort` (port stripped).
  Returns 429 with `Retry-After: 60`.

---

## Phase 0 — SOC Audit Surface Foundation · 2026-04-24

### `fd5709a` — SOC-grade audit trail + phoneticizable identifiers
**Date:** 2026-04-24 17:08 CDT
**Files:** 7 files (+447/-16)

Six-part batch that lifted the audit surface from "demo quality" to
something defensible in a customer conversation or discovery
request:
1. Phoneticizable alarm codes (`ALM-YYMMDD-NNNN`)
2. Event → alarm correlation (`active_alarms.triggering_event_id`)
3. Polymorphic audit_log targets (cases for sites, operators,
   incidents, alarms, security_events, evidence, organizations)
4. Incident ID format (`INC-YYYY-NNNN`, server-assigned)
5. Append-only audit tables via PL/pgSQL trigger that raises on
   UPDATE/DELETE
6. Evidence share open tracking — every GET of
   `/share/{token}` writes IP + user-agent + referrer + timestamp

### `681f5f0` — Hide on-duty operator badge for non-SOC roles
**Date:** 2026-04-24 16:30 CDT
**Files:** `frontend/src/components/operator/FleetStatusBar.tsx` (1 file, +21/-10)

The operator header was showing two identity tiles that visually
collapsed into the same person because the seed data linked the
admin user to op-001. Only render the on-duty operator badge when
the authenticated user is actually on a SOC shift role.

---

## Setup — Containerized Stack First-Boot · 2026-04-24

### `327f967` — Frontend can reach api container (was hitting own loopback)
**Date:** 2026-04-24 16:14 CDT
**Files:** `frontend/next.config.js`, `frontend/Dockerfile`, `docker-compose.yml` (3 files, +36/-32)

Next.js bakes static rewrites into `routes-manifest.json` at build
time. The old config hardcoded `http://localhost:8080` — fine on
the host for native dev, but inside the frontend container that
loopback hit the frontend itself. Users saw "Internal Server
Error" on login. Fix: `API_INTERNAL_URL` build ARG, set to
`http://api:8080` in compose so the rewrites bake in pointing at
the api container.

### `f7de930` — Worker: disable HEALTHCHECK, pass AI_YOLO_URL
**Date:** 2026-04-24 15:58 CDT
**Files:** `docker-compose.yml` (1 file, +9)

The worker uses the same image as api but runs a different
entrypoint. The Dockerfile HEALTHCHECK targets the server binary's
HTTP port 8080; the worker binary has no HTTP surface. Override
with `healthcheck: disable: true` so compose sees the service as
up. Also added `AI_YOLO_URL` to the worker env (cosmetic but
quiets log noise).

### `fa1d8b8` — First-boot fixes for containerized full stack
**Date:** 2026-04-24 15:56 CDT
**Files:** 8 files (+106/-15)

Eight distinct first-boot failures fixed in one batch — each
cascaded into the next:
1. Hypertable FK abort in in-code migrations (events.segment_id
   and segment_descriptions.segment_id both FK'd into a Timescale
   hypertable, which Timescale rejects)
2. `ironsight_platform.sql` wasn't mounted into the db init dir
3. Rootless podman named-volume ownership (used `user: "0:0"`
   since `:U` mount flag is silently dropped by docker-compose)
4. `/api/health` was behind `RequireAuth` — Dockerfile HEALTHCHECK
   got 401
5. `transformers ≥ 4.55` silently disables PyTorch with
   `torch < 2.4` — pinned `transformers < 4.55`
6. qwen needed `bitsandbytes` for int4 quantization
7. Stale `QWEN_MODEL` in `.env.example`
8. `frontend/.dockerignore` was missing — node_modules in build
   context

### `5a11766` — yolo/qwen Dockerfiles + compose build wiring
**Date:** 2026-04-24 15:10 CDT
**Files:** 5 files (+138/-2)

Base image for both AI services is the official `pytorch-runtime`
image with torch+cuDNN pre-compiled against CUDA 12.1 — saves ~5
minutes per build vs a fresh torch install. YOLO bakes weights
into the image; Qwen downloads from HuggingFace on first run and
persists to the new `hf-cache` named volume so subsequent boots
come up in ~30s instead of ~5 minutes. Fixed two latent compose
bugs: env var was `MODEL=` but `server.py` reads `YOLO_MODEL`;
default was `yolov8n` (unknown to ultralytics) → matches the
`yolo11n.pt` weights actually shipped.

### `6460421` — WSL-safe GPU filtering via CUDA_VISIBLE_DEVICES
**Date:** 2026-04-24 15:02 CDT
**Files:** `.env.example`, `docker-compose.yml`, `frontend/Documents/MasterDeployment.md` (3 files, +48/-19)

`NVIDIA_VISIBLE_DEVICES` is a no-op under WSL because WSL
virtualizes every GPU through a single `/dev/dxg` node.
`CUDA_VISIBLE_DEVICES` is read by libcuda itself and filters
identically on WSL and native Linux. Also documents the CDI
spec-conflict footgun (nvidia.yaml + nvidia.json both claiming
`nvidia.com/gpu=all` causes nvidia-ctk to report 0 devices).

### `a7f53b9` — WSL-compatible GPU device request pattern
**Date:** 2026-04-24 14:50 CDT
**Files:** `docker-compose.yml`, `.env.example` (2 files, +26/-18)

Earlier draft of the GPU device-request fix. Iteration before
landing on the `CUDA_VISIBLE_DEVICES` final.

### `ff58b35` — Podman + CDI compatibility for the compose stack
**Date:** 2026-04-24 13:54 CDT
**Files:** `docker-compose.yml`, `Dockerfile`, `frontend/Dockerfile`, `frontend/Documents/MasterDeployment.md` (4 files, +61/-35)

Migration from Docker to Podman. Fully-qualified base image names
(`docker.io/...` prefix required by Podman, ignored by Docker).
CDI GPU passthrough wiring. First step of the rootless-podman
target deployment.

---

## Initial Imports

### `af24d31` — Initial code import — Phase 2 complete
**Date:** 2026-04-24 11:54 CDT
**Files:** 267 files (+96033/-1)

Bulk import of the existing Ironsight codebase at the end of
Phase 2 development. Includes Go backend, Next.js frontend, AI
service stubs, and Phase 2 deliverables (worker container split,
Redis pub/sub WebSocket bridge, MediaMTX HTTP control API,
evidence export, atomic export-claim worker).

### `7236a5a` — Initial commit
**Date:** 2026-04-24 11:48 CDT
**Files:** 1 file (+1)

Repository genesis.

---

## Aggregate stats

| Phase | Commits | Lines added | Lines removed |
|---|---:|---:|---:|
| Phase D — Polish & Customer Completeness | 9 | 2,147 | 68 |
| Phase C — Operational Hardening | 1 | 207 | 3 |
| Phase B — Evidence Integrity & TMA-AVS-01 | 3 | 983 | 43 |
| Phase A — UL 827B Auth & Audit | 5 | 1,572 | 51 |
| Phase 0 — SOC Audit Surface Foundation | 2 | 468 | 26 |
| Setup — Containerized Stack First-Boot | 6 | 418 | 121 |
| Initial Imports | 2 | 96,034 | 1 |

Total post-import work (excluding the initial 96k-line code drop):
**26 commits, +5,795 / -312 lines.** All 26 are documented above.
