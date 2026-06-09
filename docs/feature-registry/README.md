# Feature registry

Every user-facing feature of Ironsight, defined once: what it does, where
it lives in code, which backend routes it uses, what state it's actually
in, and whether it ships in the MVP. The registry answers **what / where /
state / tier**; the architecture docs in [`docs/`](../) answer **how** —
blocks link out via their `Docs` field and never duplicate implementation
detail.

Maintained by hand per area file; validated and cross-referenced by
[`cmd/docgen`](../../cmd/docgen/) (`make docs-check` lints, `make docs-gen`
regenerates [`api-coverage.md`](api-coverage.md) and the rollup table
below).

## Area files

| File | Scope |
|---|---|
| [01-live-view.md](01-live-view.md) | Live grid, live HLS, popout, PTZ, map, camera web-UI proxy |
| [02-recording-playback.md](02-recording-playback.md) | Recording engine, retention, playback, exports, bookmarks, storage |
| [03-cameras-devices.md](03-cameras-devices.md) | Camera CRUD, discovery, Milesight config, VCA rules, speakers |
| [04-alerts-notifications.md](04-alerts-notifications.md) | Event ingestion, alert feed, notifications, deterrence |
| [05-auth-users-admin.md](05-auth-users-admin.md) | Login, MFA, roles, users, audit, settings, system health |
| [06-portal-platform.md](06-portal-platform.md) | Companies, sites, incidents, portal pages, support, status page |
| [07-soc-operator.md](07-soc-operator.md) | Operator console, alarms, handoffs, SOPs, dispatch |
| [08-ai-analytics.md](08-ai-analytics.md) | YOLO/Qwen, indexer, search, PPE/compliance, labeling, analytics |
| [09-system-infra.md](09-system-infra.md) | Metrics, logging, migrations, seed, media auth, deploy pipeline |

## Block schema

Each feature is one H2 block. `cmd/docgen -check` enforces this shape —
field rows in exactly this order, enums exact:

```markdown
## Feature Name {#kebab-case-id}

| Field | Value |
|---|---|
| **ID** | `kebab-case-id` |
| **Tier** | core \| back-burner \| cut |
| **Status** | working \| partial \| placeholder \| stub |
| **Definition** | One or two sentences: what it does, for whom. |
| **Frontend** | `frontend/src/...` paths in backticks, or — |
| **Routes** | `METHOD /path` entries in backticks, ` · ` separated, or — |
| **Tables** | table names, or — |
| **Flag** | feature-flag name, or — (always on) |
| **Docs** | links to docs/*.md, or — |
| **Smoke test** | 1–3 terse manual steps proving it works. |
| **Notes** | gotchas, revival cost if parked, cross-refs like [[other-id]]. |
```

**Tier** is the product decision (MVP descope, 2026-06): `core` ships in
the MVP; `back-burner` is parked — hidden behind a feature flag, code
stays; `cut` is scheduled for deletion. **Status** is the code reality:
`working` (verified end-to-end), `partial` (some paths work), `placeholder`
(UI renders mock/static data), `stub` (endpoint or component exists but
returns nothing real). The two are orthogonal on purpose: a parked feature
keeps its known state, so reviving it starts from facts.

Lint rules beyond the table shape: block anchor must equal the ID field;
`Frontend` paths must exist on disk; `Routes` entries must exist in
`router.go`; a `back-burner` feature with a page under `frontend/src/app/`
must name a `Flag` (otherwise the page stays customer-reachable at MVP).

### Canonical flag names

`analytics`, `operator_console`, `compliance`, `person_tracking`,
`speakers`, `semantic_search`, `vlm_safety`, `evidence_sharing`,
`labeling`, `support_tickets`, `integrations`, `ai_insights`,
`weekly_digest`. Backend default map lives in `HandleFeatureFlags`
(`internal/api/platform.go`); override per-environment with the
`FEATURES_OVERRIDE` env var.

## Rollup

<!-- BEGIN GENERATED: rollup -->
_82 features. Regenerate with `go run ./cmd/docgen -write`._

| Feature | ID | Area | Tier | Status |
|---|---|---|---|---|
| [Camera web-UI proxy](01-live-view.md#camera-web-ui-proxy) | `camera-web-ui-proxy` | 01-live-view | core | partial |
| [Live camera grid](01-live-view.md#live-camera-grid) | `live-camera-grid` | 01-live-view | core | working |
| [Live HLS pipeline](01-live-view.md#live-hls-pipeline) | `live-hls-pipeline` | 01-live-view | core | working |
| [Popout single-camera view](01-live-view.md#live-popout) | `live-popout` | 01-live-view | core | working |
| [Map view](01-live-view.md#map-view) | `map-view` | 01-live-view | core | working |
| [PTZ controls](01-live-view.md#ptz-controls) | `ptz-controls` | 01-live-view | core | working |
| [Bookmarks](02-recording-playback.md#bookmarks) | `bookmarks` | 02-recording-playback | core | partial |
| [Clip Export & Evidence Download](02-recording-playback.md#clip-export) | `clip-export` | 02-recording-playback | core | working |
| [Playback & Timeline](02-recording-playback.md#playback-timeline) | `playback-timeline` | 02-recording-playback | core | working |
| [Recording Engine](02-recording-playback.md#recording-engine) | `recording-engine` | 02-recording-playback | core | working |
| [Recording Health](02-recording-playback.md#recording-health) | `recording-health` | 02-recording-playback | core | working |
| [Recording Schedules & Site Policy](02-recording-playback.md#recording-schedules) | `recording-schedules` | 02-recording-playback | core | working |
| [Retention & Purge](02-recording-playback.md#retention) | `retention` | 02-recording-playback | core | working |
| [SD-Card Status](02-recording-playback.md#sd-card-status) | `sd-card-status` | 02-recording-playback | core | working |
| [Storage Locations Admin](02-recording-playback.md#storage-locations) | `storage-locations` | 02-recording-playback | core | working |
| [Camera credentials at rest](03-cameras-devices.md#camera-credentials-at-rest) | `camera-credentials-at-rest` | 03-cameras-devices | core | working |
| [Camera management (CRUD + reboot)](03-cameras-devices.md#camera-crud) | `camera-crud` | 03-cameras-devices | core | working |
| [Milesight vendor config panels](03-cameras-devices.md#milesight-config) | `milesight-config` | 03-cameras-devices | core | working |
| [ONVIF discovery](03-cameras-devices.md#onvif-discovery) | `onvif-discovery` | 03-cameras-devices | core | working |
| [Sense push webhook](03-cameras-devices.md#sense-webhook) | `sense-webhook` | 03-cameras-devices | core | working |
| [Speakers + audio messages (talk-down)](03-cameras-devices.md#speakers-audio) | `speakers-audio` | 03-cameras-devices | back-burner | partial |
| [VCA rules (platform zones + camera sync)](03-cameras-devices.md#vca-rules) | `vca-rules` | 03-cameras-devices | core | partial |
| [Alert feed + acknowledge](04-alerts-notifications.md#alert-feed-acknowledge) | `alert-feed-acknowledge` | 04-alerts-notifications | core | partial |
| [Live alert WebSocket stream](04-alerts-notifications.md#alert-websocket-stream) | `alert-websocket-stream` | 04-alerts-notifications | core | working |
| [Deterrence outputs (strobe/siren)](04-alerts-notifications.md#deterrence-outputs) | `deterrence-outputs` | 04-alerts-notifications | back-burner | working |
| [Event ingestion](04-alerts-notifications.md#event-ingestion) | `event-ingestion` | 04-alerts-notifications | core | working |
| [Notification preferences](04-alerts-notifications.md#notification-preferences) | `notification-preferences` | 04-alerts-notifications | core | working |
| [Email/SMS dispatch (SMTP + Twilio)](04-alerts-notifications.md#notify-dispatch) | `notify-dispatch` | 04-alerts-notifications | core | working |
| [Weekly digest email](04-alerts-notifications.md#weekly-digest) | `weekly-digest` | 04-alerts-notifications | back-burner | partial |
| [AI services health](05-auth-users-admin.md#ai-services-health) | `ai-services-health` | 05-auth-users-admin | back-burner | working |
| [Audit log](05-auth-users-admin.md#audit-log) | `audit-log` | 05-auth-users-admin | core | partial |
| [MFA (TOTP)](05-auth-users-admin.md#mfa-totp) | `mfa-totp` | 05-auth-users-admin | core | partial |
| [Password login](05-auth-users-admin.md#password-login) | `password-login` | 05-auth-users-admin | core | working |
| [Session cookies + CSRF](05-auth-users-admin.md#session-csrf) | `session-csrf` | 05-auth-users-admin | core | working |
| [SSO header trust (oauth2-proxy)](05-auth-users-admin.md#sso-header-trust) | `sso-header-trust` | 05-auth-users-admin | core | working |
| [System health](05-auth-users-admin.md#system-health) | `system-health` | 05-auth-users-admin | core | working |
| [System settings](05-auth-users-admin.md#system-settings) | `system-settings` | 05-auth-users-admin | core | working |
| [Users + roles](05-auth-users-admin.md#users-roles) | `users-roles` | 05-auth-users-admin | core | working |
| [Companies management](06-portal-platform.md#companies-management) | `companies-management` | 06-portal-platform | core | partial |
| [Evidence share links](06-portal-platform.md#evidence-shares) | `evidence-shares` | 06-portal-platform | back-burner | partial |
| [Feature flags service](06-portal-platform.md#feature-flags) | `feature-flags` | 06-portal-platform | core | working |
| [Incidents list & detail](06-portal-platform.md#incidents) | `incidents` | 06-portal-platform | core | partial |
| [Monitoring schedule](06-portal-platform.md#monitoring-schedule) | `monitoring-schedule` | 06-portal-platform | core | partial |
| [Portal dashboard](06-portal-platform.md#portal-dashboard) | `portal-dashboard` | 06-portal-platform | core | partial |
| [Portal history](06-portal-platform.md#portal-history) | `portal-history` | 06-portal-platform | core | working |
| [Device & user assignment to sites](06-portal-platform.md#site-assignments) | `site-assignments` | 06-portal-platform | core | partial |
| [Site contacts](06-portal-platform.md#site-contacts) | `site-contacts` | 06-portal-platform | core | working |
| [Sites CRUD + site detail](06-portal-platform.md#sites-crud) | `sites-crud` | 06-portal-platform | core | working |
| [Public status page](06-portal-platform.md#status-page) | `status-page` | 06-portal-platform | core | working |
| [Support tickets](06-portal-platform.md#support-tickets) | `support-tickets` | 06-portal-platform | back-burner | working |
| [AI assessment feedback](07-soc-operator.md#alarm-ai-feedback) | `alarm-ai-feedback` | 07-soc-operator | back-burner | working |
| [Alarm escalation](07-soc-operator.md#alarm-escalation) | `alarm-escalation` | 07-soc-operator | back-burner | working |
| [Alarm investigation view](07-soc-operator.md#alarm-investigation) | `alarm-investigation` | 07-soc-operator | back-burner | working |
| [Dispatch queue](07-soc-operator.md#dispatch-queue) | `dispatch-queue` | 07-soc-operator | back-burner | partial |
| [Operator console shell](07-soc-operator.md#operator-console-shell) | `operator-console-shell` | 07-soc-operator | back-burner | partial |
| [Operator presence + metrics](07-soc-operator.md#operator-presence-metrics) | `operator-presence-metrics` | 07-soc-operator | back-burner | stub |
| [Shift handoffs](07-soc-operator.md#shift-handoffs) | `shift-handoffs` | 07-soc-operator | back-burner | partial |
| [Site locks](07-soc-operator.md#site-locks) | `site-locks` | 07-soc-operator | back-burner | stub |
| [Site SOPs](07-soc-operator.md#site-sops) | `site-sops` | 07-soc-operator | back-burner | working |
| [AI insights panel](08-ai-analytics.md#ai-insights-panel) | `ai-insights-panel` | 08-ai-analytics | back-burner | placeholder |
| [AI runtime metrics](08-ai-analytics.md#ai-runtime-metrics) | `ai-runtime-metrics` | 08-ai-analytics | back-burner | working |
| [Analytics page](08-ai-analytics.md#analytics-page) | `analytics-page` | 08-ai-analytics | back-burner | placeholder |
| [Compliance dashboard + PDF report](08-ai-analytics.md#compliance-dashboard) | `compliance-dashboard` | 08-ai-analytics | back-burner | working |
| [Labeling queue](08-ai-analytics.md#labeling-queue) | `labeling-queue` | 08-ai-analytics | back-burner | working |
| [Person tracking](08-ai-analytics.md#person-tracking) | `person-tracking` | 08-ai-analytics | back-burner | partial |
| [PPE pending-review queue](08-ai-analytics.md#ppe-pending-review) | `ppe-pending-review` | 08-ai-analytics | back-burner | working |
| [PPE zones & compliance rules](08-ai-analytics.md#ppe-zones-compliance-rules) | `ppe-zones-compliance-rules` | 08-ai-analytics | back-burner | stub |
| [Qwen VLM threat assessment](08-ai-analytics.md#qwen-vlm-reasoning) | `qwen-vlm-reasoning` | 08-ai-analytics | back-burner | working |
| [Re-analysis admin API](08-ai-analytics.md#reanalysis-admin) | `reanalysis-admin` | 08-ai-analytics | back-burner | working |
| [Semantic search](08-ai-analytics.md#semantic-search) | `semantic-search` | 08-ai-analytics | back-burner | partial |
| [VLM segment indexer](08-ai-analytics.md#vlm-segment-indexer) | `vlm-segment-indexer` | 08-ai-analytics | back-burner | working |
| [YOLO detection service](08-ai-analytics.md#yolo-detection) | `yolo-detection` | 08-ai-analytics | back-burner | working |
| [DB migrations (goose)](09-system-infra.md#db-migrations) | `db-migrations` | 09-system-infra | core | working |
| [Demo data seed](09-system-infra.md#demo-seed) | `demo-seed` | 09-system-infra | core | working |
| [Evidence chain-of-custody manifests](09-system-infra.md#evidence-manifests) | `evidence-manifests` | 09-system-infra | back-burner | working |
| [Public health endpoint](09-system-infra.md#health-endpoint) | `health-endpoint` | 09-system-infra | core | working |
| [HEVC recorded-playback transcode](09-system-infra.md#hevc-transcode) | `hevc-transcode` | 09-system-infra | core | working |
| [Media token auth](09-system-infra.md#media-token-auth) | `media-token-auth` | 09-system-infra | core | working |
| [Prometheus metrics](09-system-infra.md#prometheus-metrics) | `prometheus-metrics` | 09-system-infra | core | working |
| [Promote-to-prod gate](09-system-infra.md#promote-to-prod) | `promote-to-prod` | 09-system-infra | core | working |
| [Structured logging + Sentry](09-system-infra.md#structured-logging-sentry) | `structured-logging-sentry` | 09-system-infra | core | working |
| [Test environment + GHCR pipeline](09-system-infra.md#test-env-pipeline) | `test-env-pipeline` | 09-system-infra | core | working |
<!-- END GENERATED -->
