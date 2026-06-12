# Ironsight VMS — Internal Company Testing Roadmap

> Synthesis of 8 grounded current-state assessments (2026-06).
> Builds on `docs/ROADMAP.md` (M0–M3 scheme), `docs/feature-registry/`, and `docs/reported-issues.md`.
> Target: promote a "halfway-working build" for **internal company testing** — not customer launch.
>
> **Decisions (2026-06-12, Caleb):** Start **M1 now**. **Tree-nav pulled earlier** — build the unified
> `Company ▸ Site ▸ Camera` shell as **M1.5**, immediately after M1 and *before* the M2 data features,
> since it's the frame the rest of the product hangs off. Effective order: **M1 → M1.5 (tree-nav, was M3)
> → M2 (tags/locks/export) → M4 (arm/disarm + schedule)**.

## Where we are (honest read)

The core VMS is genuinely solid. Live view works (NVR grid, `/api/live/{id}/*` proxy via mediamtx), recording runs to disk with a segments index, playback basics are real, and **PR #72 already shipped the timeline/transport overhaul** — tick ruler, event markers, speed 0.5–4×, ±10/±30s skip, frame-step, prev/next-event, event-type filters — at ~90% with e2e proof (`e2e/src/specs/timeline-seekbar-ux.spec.ts`). Clip export to MP4, ed25519-signed evidence ZIP bundles, and incident auto-correlation are working backend-complete. The customer portal is wired end-to-end for site list, site detail, history search, and contacts with real RBAC + RLS. The deterrence path (ONVIF relay strobe/siren) is fully implemented at the API layer. **What's missing is not the engine — it's trust and surface.** Two camera-onboarding bugs (B-13 false "online", B-14 NAT RTSP port collapse) mean a multi-camera test fleet will show green cameras that serve no video and silently record broken streams — this poisons every other test. Beyond that, the gaps are mostly *frontend re-wiring of complete backends* (bookmarks UI deleted in cleanup, footage-locks endpoint is a stub, support widget ungated, evidence viewer is mock data) plus a handful of net-new features (footage locks, tags, OSD overlay, arm/disarm button, schedule enforcement, the unified tree-nav shell). The single biggest leverage move is fixing onboarding first so the fleet is trustworthy, then hanging the wishlist off a unified navigation shell.

---

## Milestone plan

Four milestones. M1 is the gate to *trustworthy* multi-camera testing; M2–M4 layer the wishlist. Effort: S(<1d) / M(1–3d) / L(1–2wk) / XL(>2wk), one engineer + AI.

### M1 — Trustworthy fleet + playback completion (the gate)

**Exit criteria:** A tester can onboard 5+ NAT'd trailer cameras and every camera that shows "online" actually serves video; broken cameras show a *diagnosable* status instead of false-green. Bookmarks visible on the timeline. Calendar/date jump and download-from-timeline work. Support widget no longer dangles for customers.

| Item | Current status | Effort | Depends-on |
|---|---|---|---|
| **B-13: false "online" — probe RTSP before status=online** | MISSING. `cameras.go:386` sets `online` right after ONVIF connect; `ProbeRTSPStream` exists, tested, has **zero callers** | M | `ProbeRTSPStream` (done), ffprobe (present) |
| **B-14: NAT RTSP port discovery** | MISSING. `rewriteStreamHost` (client.go:619–653) preserves advertised :554; `NormalizeRTSPURI` force-collapses to 554; convention is `554+(onvif-8080)` | L | B-13 probe wiring, `RTSPCandidateURIs` (done) |
| Wire `ProbeRTSPStream` into create/update/autostart | PLACEHOLDER (functions exist, never called) | S | — |
| `cameras.last_stream_error` column + status_detail in API | MISSING (no error column today) | S | one-line ALTER |
| Bookmarks timeline UI (restore deleted client + markers) | PARTIAL — backend CRUD complete, frontend deleted in cleanup | M | Timeline.tsx (exists) |
| Calendar/date picker behind "Go To" | PARTIAL — native datetime-local works, no widget | M | Timeline.tsx |
| Download-from-timeline button | PARTIAL — ExportDialog standalone, needs wiring | S | ExportDialog (done) |
| Footage-gap indicator on scrubber | PLACEHOLDER — coverage bars computed, gaps not highlighted | S | Timeline.tsx coverage |
| Gate SupportWidget behind `support_tickets` flag | PARTIAL — mounted unconditionally (portal/layout.tsx:27) | S | FeatureGate (exists) |

**Rough sum:** ~1× L + 3× M + 4× S ≈ **8–11 engineer-days.**

---

### M2 — Markers, tags, footage locks, export polish

**Exit criteria:** Operators can tag/lock footage and the retention engine honors locks (no purge of held segments). OSD timestamp burns onto exports. Tags drive batch export. Evidence sharing + viewer wired to real data (no mock).

| Item | Current status | Effort | Depends-on |
|---|---|---|---|
| Footage locks / retention holds | MISSING — `/api/v1/sites/locks` is a stub `[]`; retention.go has no lock exemption | M | retention.go enforce path, segments migration |
| Tags / notations (per-segment) | MISSING — no table/route/model | M | new tables + migration |
| OSD timestamp overlay on exports | MISSING — no drawtext in export FFmpeg args | M | export.go FFmpeg chain |
| Tag/lock list view + admin locks page | MISSING | L | tags + locks backend (above) |
| Select-and-export by tag | MISSING — ExportDialog is camera+range only | M | tags backend, export worker (reuse) |
| Evidence viewer `/evidence/[token]` → real backend | PARTIAL — uses `mockFetchEvidence` hardcoded | M | `GET /share/{token}` (done) |
| Evidence sharing end-to-end retest (incident-scoped) | PARTIAL — backend done, incident wiring loose | M | incident lifecycle |
| Multi-camera bulk tag/lock application | MISSING — no batch-write endpoint | M | tags + locks backend |
| Timeline range tag → batch export | PLACEHOLDER — bookmarks are moment-level not range | L | tags, batch export job |

**Rough sum:** ~2× L + 6× M ≈ **3–4 engineer-weeks.**

---

### M3 — Unified Company › Site › Camera tree-nav shell

**Exit criteria:** A single left-sidebar tree (Company › Site › Camera) drives the camera grid for both admin and customer; clicking a node scopes the grid; layout persists across navigation; role-based subtree visibility correct. This is the shell the rest of the product hangs off.

| Item | Current status | Effort | Depends-on |
|---|---|---|---|
| Left-side expandable tree component | PLACEHOLDER — no tree exists | M | useSites, listCameras (done) |
| Shell layout integration (tree + grid, shared) | PLACEHOLDER — no unified shell; nav scattered 5+ places | L | tree component |
| Consolidate scattered navigation | PARTIAL — 5+ inline nav locations | M | shell |
| Grid scoping to tree node | MISSING — grid loads all or `?site_id=` | S | CameraGrid (done), tree |
| Role-based subtree visibility | MISSING — ROUTE_PERMISSIONS has no tree rules | S | AuthContext, `assigned_site_ids` |
| Layout persistence across navigation | PARTIAL — localStorage global, not site-scoped | M | CameraGrid layout keys |
| Multi-company admin drill-down in shell | PARTIAL — card/tab based today | M | shell |
| Camera grid layout engine | WORKING — reuse as-is | S | — |

**Rough sum:** ~1× L + 4× M + 3× S ≈ **2–3 engineer-weeks.**

---

### M4 — Customer arm/disarm + schedule enforcement + operator notification + deterrence surface

**Exit criteria:** Customers see a clear ARM/DISARM toggle; monitoring-schedule + snooze actually gate alarm dispatch; exception/pause requests notify an operator; deterrence (strobe/siren) is reachable from the (non-parked) alert feed. Customer login reachable past oauth2-proxy.

| Item | Current status | Effort | Depends-on |
|---|---|---|---|
| Customer ARM/DISARM one-touch button | MISSING — only snooze exists | M | portal site detail, snooze backend |
| Snooze persistence + auto-rearm on expiry | PARTIAL — frontend sends generic PUT, no timer | M | UpdateSite |
| Monitoring-schedule enforcement (alarm gating) | PLACEHOLDER — AlertEmitter ignores schedule/snooze | L | detection AlertEmitter |
| Exception/pause request → operator notify + action | MISSING — snooze is unilateral, no approval loop | XL | new schedule_requests table, dispatcher |
| Operator action-required notification channel | WORKING infra + new method/template | M | dispatcher (done) |
| Operator ack/approve via signed link | MISSING | L | schedule_requests, HMAC link |
| Surface deterrence button in alert feed | WORKING API, parked in operator console only | M | alert-feed UI |
| `GetRelayOutputs` ONVIF discovery (drop hardcoded tokens) | MISSING — only comment + hardcoded constants | M | onvif client |
| Public-URL auth bypass (reach `/auth/login`) | MISSING — oauth2-proxy gates whole domain | L | oauth2-proxy / NPM config |
| Customer live-view access (site-scoped grid) | WORKING engine, gated to admin/soc today | M | tree shell (M3), `?site_id=` |
| Scheduled siren trigger (arm/disarm time pulses) | MISSING — Schedule field exists, no evaluator | L | worker job, siren_schedules table |

**Rough sum:** ~1× XL + 3× L + 5× M ≈ **4–6 engineer-weeks.** *(The XL approval loop is the swing item — see Decision #6; notify-only mode collapses it to M.)*

---

## Sequencing rationale

- **M1 is the gate and must go first.** Every other milestone's testing assumes the camera fleet is trustworthy. B-13/B-14 are the only items that *invalidate other test results* if left broken — a false-online camera makes "playback works" and "export works" untestable for that camera. B-13 (probe-before-online) is the prerequisite for B-14 (port discovery) because the candidate-port sweep needs the probe as its success oracle. Both reuse `ProbeRTSPStream` / `RTSPCandidateURIs`, which are written and tested but have **zero callers** — this is wiring, not new logic, which is why B-13 is M not L.
- **Bookmarks before tags.** Bookmarks are the quick-win (backend complete, UI-only restore) and establish the timeline-marker rendering pattern that tags reuse.
- **Footage locks depend on understanding retention/purge.** Locks are not a UI feature — they're an *exemption in the 3-pass retention engine* (`retention.go enforceRetentionDays`). The risk is touching the append-only audit/evidence guarantee (UL 827B), so locks must be built with the retention path in mind, not bolted on after.
- **Tags → export depends on the export pipeline.** The export worker (`internal/export/export.go`, FOR-UPDATE-SKIP-LOCKED polling) already concats N segments. Tag-export is "query segments by tag → submit existing export jobs," so it follows tags + reuses the proven worker. Range-tag export is the hard variant (timeline range picker + batch queue) — defer to the tail of M2.
- **Tree-nav (M3) is a frontend shell that everything hangs off.** It's deliberately *after* the data features (M1/M2) so those features have surfaces to plug into, but *before* M4's customer-facing arm/disarm and site-scoped live view, which need the tree to scope cameras per site/customer. CameraGrid (1030 lines, working) and the data hierarchy (orgs→sites→cameras FKs) already exist — M3 is assembling, not inventing.
- **M4 schedule enforcement gates everything notification-shaped.** Arm/disarm, snooze, and exception-requests are meaningless until `AlertEmitter` actually checks schedule + snooze before firing — that's the placeholder bug at the center of the area. Build the gate first, then the buttons that flip it. The operator-approval loop is the one XL; everything else in M4 is M/L and reuses the working dispatcher.

---

## Decisions for Caleb

1. **B-13 probe: block-add on failure, or warn-and-add?** *Recommend:* block with HTTP 400 + ffprobe error; add an admin-only `force-add` flag later (non-MVP). A camera that can't stream shouldn't silently record garbage.
2. **B-14 port formula scope.** Document `external_rtsp_port = 554 + (onvif_port − 8080)` as a **general BigView NAT convention** (Pass-1 heuristic), with the full `RTSPCandidateURIs` sweep as fallback — not as a Milesight-only quirk. *Recommend:* general; it applies to any sequential port-mapped trailer.
3. **Tags vs bookmarks — per-event or per-range?** *Recommend:* bookmarks stay moment-level (event); tags are per-segment with a junction table. Don't store tags in the immutable events JSONB. Range-tag (drag-select) is a later L, not MVP.
4. **Footage-lock expiry.** Indefinite hold (admin must unlock) vs time-bounded auto-expire. *Recommend:* indefinite for internal testing, with a visible "locked by / when / reason" admin list so nothing is silently forgotten; add 90-day auto-expiry only if it becomes a retention problem.
5. **Export-by-tag output.** One ZIP per segment vs one concat MP4 per tag. *Recommend:* per-segment ZIP (reuses the proven per-event bundler, no re-transcode/re-sign complexity), with a file-size cap + warning to avoid multi-GB exports.
6. **Exception/pause: SOC approval loop or notify-only?** This is the M4 swing decision — full approval is XL and UL 827B–compliant; notify-only is M but not rigorous. *Recommend:* ship **notify-only** for internal testing (operator is informed, snooze applies immediately), and flag the full approval loop as a pre-customer-launch must-do. Internal testing has no real customer/regulatory exposure yet.
7. **ARM/DISARM default state.** All sites ARMED on startup (customer disarms for maintenance) vs DISARMED. *Recommend:* ARMED by default — safer posture, matches "monitoring is on unless explicitly paused."
8. **Schedule suppression point.** Suppress at capture (no event in DB) vs at dispatch (event stored, notification withheld). *Recommend:* suppress at **dispatch** — keep all events in the DB for audit/replay, add `event.suppression_reason`. More auditable, costs DB size.
9. **Calendar picker.** Native `<input type=date/time>` vs React library. *Recommend:* native for M1 (zero new dependency, already functional); upgrade only if testers complain.
10. **Public-URL auth bypass design (M4).** oauth2-proxy allow-list on `/auth/login` vs separate login subdomain. *Recommend:* allow-list bypass + backend JWT validation on `/auth/me` — fewer moving parts than a subdomain, reuses existing auth handler. (Internal testers can use the existing Google SSO path meanwhile, so this is not an M1 blocker.)
11. **Operator notification routing.** Who gets exception-request emails? *Recommend:* operators assigned to the site (`assigned_site_ids`), fallback to `site.customer_contacts` if empty; opt-in via `notification_subscriptions`, respect quiet hours.

---

## Biggest risks

1. **False-online (B-13) poisons all multi-camera testing.** Until fixed, every "camera X works/doesn't" result is unreliable. This is why it's the M1 gate, not a backlog item.
2. **Footage locks vs the append-only audit guarantee (UL 827B).** Retention holds must *never* exempt audit/evidence purge — `retention.go` explicitly forbids touching `audit_log`, and migration 0017 has append-only triggers as backstop. A careless lock-exemption that's too broad is a compliance break. Test the exemption boundary hard.
3. **Schedule enforcement is currently absent.** A site with an 18:00–06:00 window will still alarm + notify at 10 AM today (`AlertEmitter` ignores schedule/snooze). For internal testing this looks like a flood of false alerts; for customers it's a trust break. M4 must land the gate, not just the buttons.
4. **Mock data masquerading as features.** The evidence viewer (`/evidence/[token]`) serves `mockFetchEvidence` hardcoded data and *looks* functional; the portal dashboard hardcodes "Turner Construction" + `Math.random()` PPE trend. Testers may report these as working. Strip/wire before promoting the build.
5. **Single FFmpeg export worker has no backpressure.** Batch/range exports across hours of footage or many cameras can overwhelm the one worker; large tag-exports can balloon to multi-GB. Cap and queue.
6. **Tree-nav touches the shared frontend shell.** Per house rule, never run parallel frontend agents on the shell/`lib/api.ts` — one dangling import breaks the whole Next.js build. M3 must be serialized.
7. **Scheduled siren touches physical relay wiring (gate interlocks, facility alarms).** Correctness is safety-critical; verify physical wiring on a bench rig before any scheduled-fire feature goes near a real trailer. Also no rate-limit on the deterrence API today (relay/PSU stress).
8. **Worker duplicate-fire on scheduled triggers.** Schedule evaluation running on multiple API/worker replicas risks double-firing relays without an idempotency key or distributed lock.

---

### Effort summary

| Milestone | Focus | Rough effort |
|---|---|---|
| M1 | Trustworthy fleet + playback completion | ~8–11 days |
| M2 | Markers / tags / locks / export polish | ~3–4 weeks |
| M3 | Unified tree-nav shell | ~2–3 weeks |
| M4 | Arm/disarm + schedule + notify + deterrence | ~4–6 weeks (notify-only path; XL approval loop deferred) |

M1 alone delivers a build genuinely worth promoting for internal testing. M2–M4 are the wishlist, sequenced so each rests on a proven foundation.
