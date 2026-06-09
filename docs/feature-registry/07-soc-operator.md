# 07 — SOC operator console

The full SOC console: the `/operator` dispatch screen, the three-tab alarm
investigation workflow, and its coordination machinery (site locks, shift
handoffs, presence, dispatch queue, SOPs). The entire area is **back-burner**
for the 2026-06 MVP behind the `operator_console` flag — the MVP ships a
simple alert feed with acknowledge instead (see
[04-alerts-notifications.md](04-alerts-notifications.md)). Note the
`operator_console` flag is not yet in the `HandleFeatureFlags` default map
(`internal/api/platform.go`), so gating these pages is pending work, not done
work.

## Operator console shell {#operator-console-shell}

| Field | Value |
|---|---|
| **ID** | `operator-console-shell` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | The `/operator` dispatch-ready screen for SOC staff: operator identity + availability picker, fleet status bar, live alarm feed (REST + WebSocket merge), keyboard shortcuts, post-alarm wrap-up overlay, and a print-only evidence report modal. |
| **Frontend** | `frontend/src/app/operator/page.tsx` `frontend/src/app/operator/layout.tsx` `frontend/src/components/operator/FleetStatusBar.tsx` `frontend/src/components/operator/AlertFeed.tsx` `frontend/src/components/operator/WrapUpOverlay.tsx` `frontend/src/components/operator/ShortcutOverlay.tsx` `frontend/src/components/operator/EvidenceExportModal.tsx` `frontend/src/stores/operator-store.ts` |
| **Routes** | `GET /api/v1/operators` · `GET /api/v1/operators/current` · `GET /api/v1/alerts` · `GET /api/v1/sites` |
| **Tables** | operators, active_alarms, sites |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | Log in as a soc_operator role on test.ironsight, open `/operator`. Expect your callsign + green AVAILABLE chip, LIVE WebSocket badge, and real alarms in the right-hand feed. Trigger a VCA event on a test camera and watch it appear without reload. |
| **Notes** | Core loop (identity, feed, engage-alarm click-through, wrap-up) runs on real routes. Broken inside the shell: operator roster renders empty ([[operator-presence-metrics]] 404s), queue badge never shows ([[dispatch-queue]] is never fetched), site-lock init gets a hardcoded `[]` ([[site-locks]]). Claim/Release buttons in `AlertFeed.tsx` call `PUT/DELETE /api/v1/alerts/{id}/claim` — Table C 404s; ownership is local-state-only. `EvidenceExportModal` is purely client-side print HTML, no backend. Layout gates by role (`RouteGuard`: soc_operator/soc_supervisor/admin) but NOT by feature flag yet — flag wiring is part of parking this. Revival cost: low for the shell itself; the holes are in the sub-features below. |

## Alarm investigation view {#alarm-investigation}

| Field | Value |
|---|---|
| **ID** | `alarm-investigation` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Full-screen alarm workspace at `/operator/alarm/[id]` (alarm or `INC-*` incident ID): live video pane plus a 1-ASSESS / 2-RESPOND / 3-RESOLVE tab workflow ending in a disposition code, AVS factor score, and action log persisted as a security event. |
| **Frontend** | `frontend/src/app/operator/alarm/[id]/page.tsx` `frontend/src/components/operator/ActiveAlarmView.tsx` `frontend/src/components/operator/AVSFactorChecklist.tsx` `frontend/src/components/operator/AlarmVideoFeed.tsx` `frontend/src/components/operator/SLATimer.tsx` |
| **Routes** | `GET /api/v1/sites/{id}` · `GET /api/v1/sites/{siteId}/cameras` · `GET /api/v1/sites/{siteId}/sops` · `GET /api/v1/events` · `POST /api/v1/events` · `POST /api/v1/incidents/{incidentId}/acknowledge` |
| **Tables** | active_alarms, security_events, site_sops, sites |
| **Flag** | operator_console |
| **Docs** | [../id-conventions.md](../id-conventions.md) |
| **Smoke test** | Click an alarm in the `/operator` feed. Walk Assess → Respond → Resolve, pick a disposition (or keyboard `F`/`V` + 1–5), submit. Expect return to console and the new event in `GET /api/v1/events?site_id=...`. |
| **Notes** | All calls hit real routes; resolve also fires incident acknowledge (best-effort) and `ComputeAICorrectness` server-side. AVS scoring (`previewAVSScore`) is client-side only. Dead code: `AlarmAssessTab.tsx`, `AlarmRespondTab.tsx`, `AlarmResolveTab.tsx` were extracted (P1-B-11) but nothing imports them — `ActiveAlarmView.tsx` still renders the tabs inline; delete or finish the refactor on revival. Talk-down/deterrence buttons inside the view belong to the speakers/deterrence features (see 03/04). SLA timer is a client countdown off `sla_deadline_ms`, not server-enforced. Depends on live HLS (area [01-live-view.md](01-live-view.md)) for the video pane. |

## Site locks {#site-locks}

| Field | Value |
|---|---|
| **ID** | `site-locks` |
| **Tier** | back-burner |
| **Status** | stub |
| **Definition** | Intended cross-operator coordination: an operator "locks" a site while working it so others see ownership and don't double-respond. |
| **Frontend** | `frontend/src/stores/operator-store.ts` `frontend/src/lib/ironsight-api.ts` |
| **Routes** | `GET /api/v1/sites/locks` |
| **Tables** | — |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | `curl /api/v1/sites/locks` as an authed user — always returns `[]` regardless of state. |
| **Notes** | `HandleSiteLocks` is an explicit placeholder (`platform.go`: "Placeholder handlers for endpoints the frontend expects") returning `[]`. `lockSite`/`unlockSite` in `ironsight-api.ts` call `POST/DELETE /api/v1/sites/{id}/lock` which are Table C 404s — and no component even calls them; the zustand store's lock state is browser-local only, so two operators never see each other's locks. Status cannot be working per Table C. Revival cost: full backend (table, handlers, WS broadcast) + wiring the store to the API. |

## Shift handoffs {#shift-handoffs}

| Field | Value |
|---|---|
| **ID** | `shift-handoffs` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | End-of-shift transfer of context between operators: locked sites, active alerts, and notes go to a named colleague, who reviews and accepts them. |
| **Frontend** | `frontend/src/components/operator/ShiftHandoffModal.tsx` `frontend/src/app/operator/page.tsx` |
| **Routes** | `GET /api/v1/handoffs` · `POST /api/v1/handoffs` · `GET /api/v1/operators/{operatorId}/handoffs` |
| **Tables** | shift_handoffs, operators |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | `curl -X POST /api/v1/handoffs` with from/to operator IDs, then `GET /api/v1/handoffs?to=<id>` — row round-trips. In the UI, the Handoff badge count on `/operator` increments. |
| **Notes** | Backend list/create are real (`shift_handoffs` table). Two breaks: `PUT /api/v1/handoffs/{id}/accept` is Table C — accepting 404s and the pending card never clears; and the modal's init does `Promise.all([getPendingHandoffs, getOperatorPresence])`, so the presence 404 ([[operator-presence-metrics]]) rejects the whole batch — the modal sticks on "Loading…" and the End Shift tab has no operators to hand off to. So handoffs can only realistically be created via curl today. `GET /api/v1/handoffs` also ignores the `status=pending` query param the client sends. Revival cost: accept endpoint + presence endpoint, then the existing UI mostly works. |

## Dispatch queue {#dispatch-queue}

| Field | Value |
|---|---|
| **ID** | `dispatch-queue` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | Queue-depth indicator for the SOC: how many unacknowledged alarms are waiting and how old the oldest one is, surfaced as a badge in the fleet status bar. |
| **Frontend** | `frontend/src/components/operator/FleetStatusBar.tsx` `frontend/src/lib/ironsight-api.ts` |
| **Routes** | `GET /api/v1/dispatch/queue` |
| **Tables** | active_alarms |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | With ≥1 unacked alarm, `curl /api/v1/dispatch/queue` — expect `{"depth": N, "oldest_ts": ...}` with real values. |
| **Notes** | Backend is real (`GetActiveAlarmsCount` over `active_alarms WHERE acknowledged=false`), but it's orphaned: `getAlarmQueue()` in `ironsight-api.ts` has zero callers and the store's `setQueueDepth` is never invoked, so `FleetStatusBar`'s "N IN QUEUE" badge never renders (queueDepth is always 0 and the badge hides at 0). Revival cost: trivial — a poll or WS push wiring queue depth into the store. |

## Operator presence + metrics {#operator-presence-metrics}

| Field | Value |
|---|---|
| **ID** | `operator-presence-metrics` |
| **Tier** | back-burner |
| **Status** | stub |
| **Definition** | Who's-on-shift roster with live status (on shift / break / off shift), per-operator performance metrics for admins, and SLA response-time reporting. |
| **Frontend** | `frontend/src/components/operator/OperatorRoster.tsx` `frontend/src/components/admin/OperatorAnalyticsPanel.tsx` `frontend/src/components/reports/SLAReportCard.tsx` |
| **Routes** | `GET /api/reports/sla` |
| **Tables** | operators, security_events |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | Open `/operator` — roster shows "(0 on shift)" with no rows; devtools shows `GET /api/v1/operators/presence` 404 every 10s. `curl '/api/reports/sla?from=...'` does return real rows. |
| **Notes** | Every UI call is Table C: `GET/PUT /api/v1/operators/presence`, `PUT /api/v1/operators/{id}/presence`, `GET /api/v1/operators/metrics`, `GET /api/v1/sla`, `GET /api/v1/reports/sla` — all 404. Status cannot be working. The one real backend piece, `HandleSLAReport` at `GET /api/reports/sla` (curl-verified shape in `internal/api/reports.go`), is unreachable from the UI because `getSLAReport()` calls the `/api/v1/` prefix — a one-line client fix would light up `SLAReportCard` (which lives on `/reports`, see 06 for the cards themselves). `setOperatorStatus` in the store fire-and-forgets `updatePresence` into the 404 on every status change. Roster's own-status row works locally only. Revival cost: presence needs a real backend (table or in-memory + WS); metrics need a query over security_events. |

## Site SOPs {#site-sops}

| Field | Value |
|---|---|
| **ID** | `site-sops` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Per-site standard operating procedures (title, category, priority, steps, contacts) managed by admins and shown to operators as a checkable checklist in the alarm Assess tab. |
| **Frontend** | `frontend/src/components/admin/SiteSOPModal.tsx` `frontend/src/components/operator/ActiveAlarmView.tsx` `frontend/src/components/operator/AlertDetailSlideout.tsx` |
| **Routes** | `GET /api/v1/sites/{siteId}/sops` · `POST /api/v1/sites/{siteId}/sops` · `PUT /api/v1/sops/{id}` · `DELETE /api/v1/sops/{id}` |
| **Tables** | site_sops |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | Admin: open a site's config modal → SOPs tab, create an SOP with 2 steps. Open an alarm for that site — the SOP appears as a checklist in Assess. Delete it; it disappears. |
| **Notes** | Full CRUD verified against real handlers + `site_sops` table — the healthiest feature in this area. Editing UI mounts inside `SiteConfigModal` (admin surface), so it survives even if `/operator` pages are flag-hidden; decide on revival whether SOP authoring stays admin-side. Step check-state in the alarm view is local-only (not persisted to the action log unless the operator logs it). |

## Alarm escalation {#alarm-escalation}

| Field | Value |
|---|---|
| **ID** | `alarm-escalation` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Operator bumps an active alarm's escalation level (ESC 0→1→2…) from the investigation view; the level persists and colors the feed badge. |
| **Frontend** | `frontend/src/components/operator/ActiveAlarmView.tsx` `frontend/src/stores/operator-store.ts` |
| **Routes** | `POST /api/v1/alarms/{alarmId}/escalate` |
| **Tables** | active_alarms |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | In an open alarm, click Escalate. Verify `active_alarms.escalation_level` bumped (psql) and the ESC badge color changes in the feed. |
| **Notes** | Route and DB write (`EscalateActiveAlarm`) are real. Gotcha: `HandleEscalateAlarm` discards the DB error and always returns `{"ok": true}`, and it takes a `*Hub` it never uses — no WS broadcast, so other operators only see the new level after a REST refresh. Store updates optimistically and fire-and-forgets the API call. |

## AI assessment feedback {#alarm-ai-feedback}

| Field | Value |
|---|---|
| **ID** | `alarm-ai-feedback` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | "Was this accurate? Yes/No" buttons under the AI assessment in the alarm view; records operator agreement on the alarm for AI quality tracking. |
| **Frontend** | `frontend/src/components/operator/AIFeedbackButtons.tsx` `frontend/src/components/operator/ActiveAlarmView.tsx` |
| **Routes** | `POST /api/v1/alarms/{alarmId}/ai-feedback` |
| **Tables** | active_alarms |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | Open an alarm with an AI assessment, click Yes under "Was this accurate?". Verify `active_alarms.ai_operator_agreed = true` (psql); buttons lock after one click. |
| **Notes** | Inline handler in `router.go` → `SetAlarmAIFeedback` sets `ai_operator_agreed`; separately, resolving the alarm computes `ai_was_correct` from disposition vs `ai_threat_level` (`ComputeAICorrectness`). Only meaningful when the server-side AI pipeline (back-burner, see 08) has enriched the alarm — without `ai_*` fields the panel has nothing to rate. Distinct from `submitAICorrection` (`POST /api/v1/ai-telemetry/corrections`), which is Table C and belongs to the AI area. |
