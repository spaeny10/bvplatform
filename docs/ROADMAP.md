# Ironsight VMS — Product Roadmap

> Owner: Caleb · Task index: `ironsight/backlog/mvp-milestones.md` (monorepo)
> Feature inventory: [docs/feature-registry/](feature-registry/) · Upstream phase plan: `claude_code_phase_plan.md` (read-only, monorepo root)
> Reported bugs/requests (intake log): [docs/reported-issues.md](reported-issues.md)

## Strategic pivot (2026-06)

Ironsight grew a wide surface — SOC dispatch console, PPE compliance
suite, server-side AI pipeline, semantic search, talk-down audio —
while still pre-launch. The June 2026 descope narrows the launch target
to a **basic VMS/NVR**: recording, playback, live view, alerts (from
camera-side VCA), and customer login. Everything else is *parked*, not
deleted: code stays, routes stay registered behind auth, pages hide
behind feature flags (`internal/api/platform.go` `DefaultFeatureFlags`,
overridable per-environment via `FEATURES_OVERRIDE`). The
[feature registry](feature-registry/) carries the authoritative
per-feature Tier (core / back-burner) and Status (working / partial /
placeholder / stub).

## Milestone tree

```
M0  Stabilize & descope (now)
 ├─ docgen + api-coverage drift check          [done — PR #52]
 ├─ feature registry authored                  [this PR]
 ├─ feature flags + parked-page gating         [this batch]
 ├─ ROADMAP + backlog index                    [this PR]
 └─ Phase A review findings triaged            [report → approved fixes]
M1  MVP core hardening
 ├─ recording reliability + health
 ├─ playback / export / bookmarks
 ├─ camera CRUD / ONVIF / VCA
 ├─ M1-ALERT-01 simple alert feed (replaces SOC console)
 └─ M1-ALERT-02 SMTP notify verified on real VCA events
M2  Customer auth & tenancy
 ├─ password login → portal flow polish, MFA UX
 ├─ RLS / tenant-scoping verification
 └─ minimal customer portal (live + playback + alerts + profile)
M3  Launch readiness (app + infra)
 ├─ P3-INFRA-01 B2 object storage
 ├─ P3-INFRA-02 VPN migration
 ├─ test→prod promotion formalized
 ├─ backup/restore drill + service monitoring
 └─ launch checklist: all core-tier smoke tests green + security review
M4  Post-MVP A: multi-stream, speakers/talk-down, support portal
M5  Post-MVP B: AI revival (YOLO/Qwen/indexer/search/analytics),
    full SOC console, compliance suite, person tracking
```

## M0 — Stabilize & descope

**Goal:** know exactly what exists and what state it's in; make parked
surfaces invisible to customers; put a drift check in CI so the map
stays current.

**Exit criteria:** registry lints clean (`make docs-check`); parked
pages 404 with flags off; e2e smoke harness runs green on `@core` specs
against the test stack.

| ID | Title | Registry area | Status |
|---|---|---|---|
| M0-TOOL-01 | `cmd/docgen` route/call cross-reference + registry lint + CI step | 09 | done (PR #52) |
| M0-DOC-01 | Author feature registry (9 area files, tiers assigned) | all | done (this PR) |
| M0-FLAG-01 | Backend `DefaultFeatureFlags` + `FEATURES_OVERRIDE` | 06 | done |
| M0-FLAG-02 | Frontend flags hydrate from API + `FeatureGate`/`FeaturePageGate` | 06 | done |
| M0-FLAG-03 | Gate parked pages + nav (analytics, operator, reports, search, compliance, labeling, integrations, evidence) | various | done |
| M0-DOC-02 | ROADMAP.md + `ironsight/backlog/mvp-milestones.md` | — | done (this PR) |
| M0-REVIEW-01 | Phase A review findings doc → Caleb triage → themed fix PRs | — | in progress |
| M0-E2E-01 | Playwright smoke harness (`e2e/`) + per-role auth + `@core` specs | — | in progress |

## M1 — MVP core hardening

**Goal:** the four core surfaces (record, play, watch, alert) survive a
week of real use on the test trailer without intervention.

**Exit criteria:** 7 consecutive days on bob with zero recording gaps
>5 min, zero playback dead-ends on recent footage, every camera-side
VCA event visible in the alert feed within 10 s, email notification
delivered for each alert.

| ID | Title | Notes |
|---|---|---|
| M1-REC-01 | Recording-gap audit: instrument + close residual stalls | continues PR #49 (cellular stalls) |
| M1-REC-02 | hot-reload `cfg.StoragePath` in recording + livehls | carryover P2 follow-up |
| M1-PLAY-01 | Playback dead-end sweep: corrupt-segment filter verified on aged footage | continues HandlePlayback filter |
| M1-CAM-01 | ONVIF probe wrong default RTSP URI when PTZ on different port | carryover P2 follow-up |
| M1-CAM-02 | Camera web-UI proxy verified across vendor firmwares | PR #50 |
| M1-ALERT-01 | Simple alert feed: list + acknowledge + click-through to live/playback | reuses `/api/v1/alerts` + `ws-alerts.ts`; **replaces** SOC console for MVP |
| M1-ALERT-02 | SMTP notify verified end-to-end on real VCA events | internal/notify already implemented; needs SMTP_HOST on test + verification |
| M1-CLEAN-01 | Delete dead gohlslib live-HLS path (muxer warm-up still runs at boot) | from Phase A findings |

## M2 — Customer auth & tenancy

**Goal:** a customer who isn't in our Google Workspace can log in with
username/password, see only their sites/cameras, and use the four core
surfaces.

**Exit criteria:** a seeded `customer`-role account can log in at the
public URL, sees only its org's cameras (RLS verified by test), and
cannot reach any parked or admin surface.

| ID | Title | Notes |
|---|---|---|
| M2-AUTH-01 | Password login flow polish: error states, forced password change, MFA enrollment UX | backend /auth/login + lockout + MFA already shipped (P1-A) |
| M2-AUTH-02 | Public-URL auth path for customers (NPM/oauth2-proxy bypass design for /auth/login) | today oauth2-proxy gates everything; customers need a non-Google path |
| M2-TEN-01 | RLS verification suite per docs/security/rls.md against seeded multi-org data | rls_test.go exists; extend to API-level checks |
| M2-PORT-01 | Minimal customer portal: live + playback + alerts + profile; parked cards stripped | portal home currently mixes parked panels |

## M3 — Launch readiness (app + infra)

| ID | Title | Notes |
|---|---|---|
| P3-INFRA-01 | B2 object storage tiering | existing ticket, blocked on bucket/key provisioning |
| P3-INFRA-02 | VPN migration | Peplink-side work first |
| M3-OPS-01 | test→prod promotion formalized | promote-to-prod.sh exists; document + drill |
| M3-OPS-02 | Backup/restore drill | restore from a real backup onto scratch host |
| M3-OPS-03 | Service self-monitoring: Sentry/GlitchTip + Prometheus alerts reviewed | deploy/monitoring exists |
| M3-LAUNCH-01 | Launch checklist: all core-tier registry smoke tests pass + security review of auth surface | registry Smoke test fields are the checklist |

## Post-MVP — M4 / M5

**M4:** multi-stream support (sub-stream selection per view), speakers /
talk-down audio (flag `speakers`), customer support-ticket portal (flag
`support_tickets`), evidence-share lifecycle wired to the real backend
(flag `evidence_sharing`).

**M5:** AI revival — YOLO/Qwen pipeline, VLM indexer, semantic search
(flag `semantic_search`), analytics with real aggregation endpoints
(flag `analytics`), full SOC console (flag `operator_console` — site
locks/handoffs/SLA/presence; note `GET /api/v1/sites/locks` is a stub
and several operator endpoints were never registered — see
api-coverage.md Table C), PPE compliance suite (flags `compliance`,
`vlm_safety`, `person_tracking`), labeling queue (flag `labeling`).

## Parked features & revival cost

The registry's `Tier: back-burner` blocks each carry revival notes. The
big-ticket items:

| Feature | Flag | Revival cost beyond flipping the flag |
|---|---|---|
| Analytics page | `analytics` | Build real aggregation endpoints — page is 100% mock today |
| SOC console | `operator_console` | Register/implement missing endpoints (locks, claim/release, presence, SLA, metrics — api-coverage.md Table C) |
| PPE zones/rules | `vlm_safety` | Register the 8 handler routes that exist in `ppe_zones_handler.go` but were never added to router.go |
| Evidence viewer | `evidence_sharing` | Wire `/evidence/[token]` page to the real `/share/{token}` backend (page is mock) |
| Speakers | `speakers` | Re-verify ONVIF backchannel on current camera firmware |
| Semantic search | `semantic_search` | Re-enable indexer + GPU capacity planning |

## Relationship to the phase plan

`claude_code_phase_plan.md` (monorepo root) is the upstream Shawn-era
artifact and stays untouched. Disposition of its phases under this
roadmap:

| Phase-plan track | Disposition |
|---|---|
| P1 (foundation/security/CI) | shipped — predates this roadmap |
| P2 (PPE pilot, tracking, compliance) | **parked** → M5 |
| P3-INFRA-01, -02 | **kept as-is** → M3 (same IDs) |
| P3-INFRA-03..08 (evidence, IDs, soft-delete, live-HLS, PWA, digest) | shipped or parked; see registry |
| P4-SCHEMA (detections architecture) | shipped through -07; reader migration (-03) still gated on dwell |

New tickets use the `M`-prefix so provenance stays greppable. Status
tracking lives in `ironsight/backlog/mvp-milestones.md` following the
existing `phase-N.md` conventions (`[ ]`/`[~]`/`[x]`, agents update
status only).
