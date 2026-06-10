# 04 · Alerts & notifications

How a camera-side detection becomes something a human sees: event ingestion
(ONVIF PullPoint, Milesight WebSocket relay, Sense push webhook) into the
`events` table and the SOC alarm pipeline, the live WebSocket fan-out, the
alert feed with acknowledge, and the outbound email/SMS notification layer.
Server-side AI enrichment of alarms is covered in
[08-ai-analytics.md](08-ai-analytics.md); the full SOC console around the
alert feed is in [07-soc-operator.md](07-soc-operator.md).

## Event ingestion {#event-ingestion}

| Field | Value |
|---|---|
| **ID** | `event-ingestion` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Receives camera-side VCA/analytics events over three transports — ONVIF PullPoint subscription, Milesight proprietary `/webstream/track` WebSocket relay (probed first, PullPoint fallback), and the token-authenticated Sense push webhook for PIR cameras — writes them to the `events` table, triggers event recording, raises SOC alarms (deduped per camera/minute), and broadcasts to WebSocket clients. |
| **Frontend** | `frontend/src/components/EventListPanel.tsx` · `frontend/src/components/settings/EventLogTab.tsx` · `frontend/src/app/page.tsx` |
| **Routes** | `GET /api/events` · `POST /api/integrations/milesight/sense/{token}` |
| **Tables** | events, active_alarms, incidents, detections (dual-write) |
| **Flag** | — |
| **Docs** | [docs/data-model.md](../data-model.md) |
| **Smoke test** | 1) Trip a VCA rule (walk in front of a camera) on test.ironsight. 2) Confirm a new row in Settings → Event Log (or `GET /api/events`) within seconds. 3) For a Sense camera, confirm the webhook 200s and an alarm card appears. |
| **Notes** | Ingestion lives in `cmd/server/main.go` (ONVIF subscriber callback), `internal/api/event_source.go` (Milesight WS relay with Probe-then-fallback), `internal/api/sense_webhook.go`, and `internal/detection` (Profile M bounding-box cache + AlertEmitter at confidence ≥ 0.70). Alarm generation cooldown is 60 s per camera+type. Sense webhook is unauthenticated by design — the long URL token is the credential. P4 dual-writes every event to `detections` (non-fatal on failure). Feeds [[alert-websocket-stream]] and [[alert-feed-acknowledge]]. |

## Live alert WebSocket stream {#alert-websocket-stream}

| Field | Value |
|---|---|
| **ID** | `alert-websocket-stream` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Real-time push of events, alarms, incident updates, snapshot-ready and AI-enrichment messages to the browser over `/ws` and `/ws/alerts`, authenticated by a 5-minute ticket minted from the session cookie and RBAC-filtered per camera. |
| **Frontend** | `frontend/src/lib/ws-alerts.ts` · `frontend/src/hooks/useAlerts.ts` · `frontend/src/app/page.tsx` |
| **Routes** | `GET /ws` · `GET /ws/alerts` · `GET /api/auth/ws-ticket` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [docs/metrics.md](../metrics.md) |
| **Smoke test** | 1) Open the dashboard on test.ironsight, devtools → Network → WS. 2) Confirm `/ws/alerts?ticket=…` upgrades (101). 3) Trip a camera event and watch a `type:"event"`/`type:"alarm"` frame arrive. |
| **Notes** | P1-A-04 ticket auth: `AlertStream.connect()` fetches `GET /api/auth/ws-ticket` then opens the socket; on ticket failure it falls back to a ticketless upgrade for SSO (X-Forwarded-Email) sessions. Hub (`internal/api/websocket.go`) refreshes each client's camera allow-set every 60 s and supports optional Redis cross-replica fan-out (`REDIS_URL`). `/ws` and `/ws/alerts` are the same handler — clients filter by message type. Message types: `event`, `alert`, `alarm`, `incident_new/update`, `alarm_snapshot`, `alarm_ai` (the last is AI enrichment — back-burner content riding a core channel). |

## Alert feed + acknowledge {#alert-feed-acknowledge}

| Field | Value |
|---|---|
| **ID** | `alert-feed-acknowledge` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Lists active (unacknowledged) alarms and incident groups, and lets an operator acknowledge an incident, which clears it and all child alarms from the feed. This simple feed is the MVP replacement for the full SOC console. |
| **Frontend** | `frontend/src/components/operator/AlertFeed.tsx` · `frontend/src/components/operator/ActiveAlarmView.tsx` · `frontend/src/components/operator/AlertDetailSlideout.tsx` · `frontend/src/hooks/useAlerts.ts` |
| **Routes** | `GET /api/v1/alerts` · `GET /api/v1/incidents/active` · `POST /api/v1/incidents/{incidentId}/acknowledge` |
| **Tables** | active_alarms, incidents |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | 1) With `operator_console` enabled on test.ironsight, open /operator and trip a camera event. 2) Alert appears in the feed (WS push, REST poll fallback every 15 s). 3) Acknowledge the incident; it disappears and `incidents.status` flips to acknowledged. |
| **Notes** | Partial, not working, because the claim/release buttons call `PUT/DELETE /api/v1/alerts/{id}/claim` which have NO backend route (api-coverage Table C — 404 at runtime); the Zustand store updates locally so the UI lies about claim state. Claim/release is parked SOC-console surface — strip or stub the buttons for MVP. Bigger gotcha: the only feed UI today lives inside the /operator console, which is parked behind `operator_console` — the MVP needs this feed extracted to an unflagged surface (open question for Caleb). List + acknowledge backend paths are verified working. Cross-refs: [[event-ingestion]], [[alert-websocket-stream]], full console in 07-soc-operator.md. |

## Notification preferences {#notification-preferences}

| Field | Value |
|---|---|
| **ID** | `notification-preferences` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Per-user opt-in matrix for outbound notifications — email/SMS on alarm disposition (with minimum-severity filter and quiet hours) and the monthly email summary. Customers edit only their own subscriptions; user_id always comes from the JWT. |
| **Frontend** | `frontend/src/app/portal/notifications/page.tsx` |
| **Routes** | `GET /api/me/notifications` · `PUT /api/me/notifications` |
| **Tables** | notification_subscriptions |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | 1) Log in as a customer on test.ironsight, open /portal/notifications. 2) Toggle "Email me when an alarm is dispositioned", pick a severity floor. 3) Reload — the toggle persists (row upserted, not deleted, on disable). |
| **Notes** | Backend (`internal/api/notification_prefs.go`) validates channel ∈ email/sms and event_type ∈ alarm_disposition/monthly_summary — it REJECTS `weekly_digest`, so digest subscriptions cannot be created from the UI or API (see [[weekly-digest]]). Recipient matching at dispatch time (`MatchAlarmRecipients`) applies severity_min, site scoping, and quiet hours. SMS toggle saves fine but actual delivery needs Twilio env config ([[notify-dispatch]]). |

## Email/SMS dispatch (SMTP + Twilio) {#notify-dispatch}

| Field | Value |
|---|---|
| **ID** | `notify-dispatch` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Outbound notification engine (`internal/notify`): renders and sends alarm-disposition emails/SMS, the monthly summary, weekly digest, and support-ticket emails. Falls back to stub mailers that log to stderr when SMTP/Twilio env vars are unset, so the pipeline stays observable in dev/CI. |
| **Frontend** | — |
| **Routes** | `POST /api/v1/events` |
| **Tables** | notification_subscriptions, users, active_alarms |
| **Flag** | — |
| **Docs** | [docs/configuration.md](../configuration.md) |
| **Smoke test** | 1) On bob test (SMTP unset), disposition an alarm (POST /api/v1/events from the alarm view). 2) `docker logs` the API and grep `[NOTIFY-STUB]` — rendered email with subject/body appears. 3) With SMTP_HOST set, confirm a real email lands. |
| **Notes** | Trigger path: `HandleCreateSecurityEvent` → `dispatchAlarmNotifications` (internal/api/platform.go) → `Dispatcher.AlarmDispositioned`; monthly summary + weekly digest fire from `cmd/worker`. SMTP is stdlib net/smtp (STARTTLS, port 587 default); SMS is direct Twilio REST, body capped at 320 chars, per-recipient calls. Env: SMTP_HOST/PORT/USER/PASS/FROM, TWILIO_ACCOUNT_SID/AUTH_TOKEN/FROM, NOTIFY_PUBLIC_URL for deep links (defaults to localhost:3000 — must be set in prod or email links break). Note docs/alerting.md is the separate Prometheus→ntfy INFRA alerting pipeline, not this customer-facing path. Open question: prod SMTP/Twilio creds not yet verified with a live send. |

## Deterrence outputs (strobe/siren) {#deterrence-outputs}

| Field | Value |
|---|---|
| **ID** | `deterrence-outputs` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Operator-triggered camera relay outputs — strobe, siren, both, or alarm-out — fired over ONVIF SetRelayOutputState, with optional re-fire sustain beyond the relay's monostable pulse. Every attempt (success or failure) writes a `deterrence_audits` row. |
| **Frontend** | `frontend/src/components/operator/ActiveAlarmView.tsx` |
| **Routes** | `POST /api/cameras/{id}/deterrence` |
| **Tables** | deterrence_audits, cameras |
| **Flag** | operator_console |
| **Docs** | — |
| **Smoke test** | 1) With `operator_console` enabled on test.ironsight, open an active alarm on a relay-equipped Milesight camera. 2) Fire Strobe — light flashes, toast confirms. 3) Confirm a `deterrence_audits` row with action/reason/ip. |
| **Notes** | Parked per 2026-06 descope (not in the MVP list) but flagged as an OPEN QUESTION: this is camera-native hardware (no server-side AI), fully implemented, RBAC-gated (customer/viewer 403), and audited — revival cost is near zero, it only needs a button on whatever MVP surface replaces the SOC console ([[alert-feed-acknowledge]]). Flag is `operator_console` because the only buttons live inside ActiveAlarmView in the parked /operator console; the API route itself stays live (RBAC-gated) regardless of the flag. DurationSec > 10 spawns a sustain goroutine re-firing every 8 s. |

## Weekly digest email {#weekly-digest}

| Field | Value |
|---|---|
| **ID** | `weekly-digest` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | Per-org weekly PPE-compliance summary email (violations, compliance rate, pending review queue, top cameras) sent by the worker on a configurable UTC day/hour, with durable per-ISO-week idempotency via the `digest_sends` table. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | notification_subscriptions, digest_sends, pending_review_queue |
| **Flag** | weekly_digest |
| **Docs** | — |
| **Smoke test** | 1) On test, hand-insert a notification_subscriptions row with event_type='weekly_digest'. 2) Set DIGEST_SEND_DAY/DIGEST_SEND_HOUR to now and restart the worker. 3) Within 30 min, grep worker logs for `[DIGEST] … sent=1` and check the stub/real email. |
| **Notes** | Partial because the send pipeline is complete and unit-tested (`internal/notify/dispatcher_weekly_digest_test.go`, `cmd/worker/main.go runWeeklyDigest`) but unreachable end-to-end: the prefs API rejects event_type `weekly_digest` and the portal page has no toggle ([[notification-preferences]]), so recipients exist only via manual SQL. Gotcha: the worker does NOT check the `weekly_digest` flag — the flag only hides (nonexistent) UI; parked state currently holds because zero subscriptions exist. Content is PPE compliance, itself back-burner — revival means (a) allowing the event_type in `notification_prefs.go` + portal row, and (b) unparking the compliance pipeline, else the digest sends empty stats. |
