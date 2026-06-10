# 08 — AI & analytics

Everything that runs server-side inference or renders analytics on top of it:
the YOLO/Qwen alarm-enrichment pipeline, the VLM segment indexer and search,
the PPE/compliance suite (worker, review queue, zones, dashboards), labeling
and re-analysis tooling, and the AI runtime metrics cards. Per the 2026-06
MVP descope the **entire area is back-burner** — camera-side VCA alerts (areas
03/04) carry detection duty in the MVP, and all server-side AI is parked with
its state recorded here.

## YOLO detection service {#yolo-detection}

| Field | Value |
|---|---|
| **ID** | `yolo-detection` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Python YOLO sidecar (`services/yolo`) plus the Go client (`internal/ai`) that runs object + PPE detection on snapshot frames. Feeds alarm enrichment, the PPE worker, the indexer's empty-scene gate, and the unified `detections` store. |
| **Frontend** | — |
| **Routes** | `GET /api/v1/detections` · `GET /api/v1/model-versions` |
| **Tables** | detections, model_versions, analysis_runs, ai_runtime_metrics |
| **Flag** | ai_insights |
| **Docs** | — |
| **Smoke test** | With the yolo container up, trigger a camera event → server log prints an `[AI] alarm …` line; `GET /api/v1/detections?limit=5` returns recent rows with bounding boxes and a `model_version_id`. |
| **Notes** | Event-triggered, not continuous: the alarm pipeline in `cmd/server/main.go` grabs a snapshot (Milesight CGI → segment-extract fallback) and calls `aiClient.Analyze`; a per-camera in-flight gate drops redundant inference during bursts. Kill switch is `AI_ENABLED=false` (stubs every call); endpoints via `AI_YOLO_URL`/`AI_QWEN_URL`. `detections` is the Phase-4 unified hypertable (migration 0030). Gotcha: `GET /api/cameras/{id}/detect` is the ONVIF analytics box cache, NOT this pipeline — its zero-caller `fetchDetections` client (and the unbacked `submitAICorrection` → `POST /api/v1/ai-telemetry/corrections`) were deleted in the 2026-06 dead-code cleanup. Flag note: `ai_insights` covers the alarm-panel surface; PPE surfaces ride `vlm_safety` ([[ppe-pending-review]]). |

## Qwen VLM threat assessment {#qwen-vlm-reasoning}

| Field | Value |
|---|---|
| **ID** | `qwen-vlm-reasoning` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Qwen vision-language sidecar (`services/qwen`) gives each alarm snapshot a natural-language description, threat level, recommended action, and false-positive estimate; results stream into the operator alarm view's AI assessment section. |
| **Frontend** | `frontend/src/components/operator/ActiveAlarmView.tsx`, `frontend/src/components/operator/AlarmAssessTab.tsx`, `frontend/src/lib/ws-alerts.ts` |
| **Routes** | — |
| **Tables** | active_alarms |
| **Flag** | ai_insights |
| **Docs** | — |
| **Smoke test** | With qwen + yolo containers up, trigger an alarm → open it in the operator view → AI assessment shows a scene description and threat level within a few seconds. |
| **Notes** | Routes is — because delivery is the `alarm_ai` WebSocket broadcast on `/ws/alerts` (area 04), not REST. YOLO-gated: Qwen is only called when YOLO finds something, so an idle scene costs nothing. The Yes/No accuracy rating is [[alarm-ai-feedback]] (07-soc-operator.md). The same sidecar also serves PPE second opinions ([[ppe-pending-review]]) and segment descriptions ([[vlm-segment-indexer]]) via `/analyze_video` modes. Degrades gracefully: Qwen down → alarms still fire, just without `ai_*` fields. |

## VLM segment indexer {#vlm-segment-indexer}

| Field | Value |
|---|---|
| **ID** | `vlm-segment-indexer` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Background worker (`internal/indexer`) that describes each closed recording segment with Qwen so footage becomes keyword-searchable ("person in red jacket"), writing one `segment_descriptions` row per segment. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | segment_descriptions, segments |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | With workers + AI containers up, wait for a segment to close (~1 min) → newest `segment_descriptions` row (psql) is for that segment; its text shows up in [[semantic-search]] results. |
| **Notes** | No flag because it has no UI — parking is `INDEXER_ENABLED=false` on the worker, and the only surface is gated by `semantic_search`. YOLO-gated: an empty sample frame records a stub description and skips Qwen (saves ~95% of GPU on overnight lulls). Knobs: `INDEXER_CONCURRENCY` (1–16; 1 on a 3070, 4–8 on A40s), `INDEXER_MIN_AGE_SEC`. Runs in the worker container under leader election; the server binary only runs it in single-binary dev (`RunWorkers`). Sends an 8 s mid-segment clip, not the whole minute. Gotcha: early-indexer rows have the raw JSON blob in `description`; the search page cleans this on read. |

## Semantic search {#semantic-search}

| Field | Value |
|---|---|
| **ID** | `semantic-search` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | The `/search` investigation page (frame search with filters) and the portal History page's semantic search box; both match keywords against the indexer's VLM segment descriptions. |
| **Frontend** | `frontend/src/app/search/page.tsx`, `frontend/src/hooks/useSearch.ts`, `frontend/src/app/portal/history/page.tsx` |
| **Routes** | `GET /api/search/semantic` · `POST /api/search/frames` |
| **Tables** | segment_descriptions, active_alarms, detections |
| **Flag** | semantic_search |
| **Docs** | — |
| **Smoke test** | On test.ironsight set `FEATURES_OVERRIDE=semantic_search=true` → `/search` → query "person" → result cards render with VLM captions; portal → History → semantic query returns matching segments. |
| **Notes** | Partial because the page's saved-searches and suggestions call `/api/v1/search/saved` and `/api/v1/search/suggest`, which match no backend route (api-coverage Table C, 404 at runtime) — `getSavedSearches` fires on every page load. Gating gotcha: the nav link is behind `FeatureGate`, but the page itself has only a role `RouteGuard` (no `FeaturePageGate`), so admins/supervisors can still reach `/search` by URL with the flag off. `GET /api/search/events` is the alert-feed text search (area 04), not this. Depends on [[vlm-segment-indexer]] having populated rows. |

## PPE zones & compliance rules {#ppe-zones-compliance-rules}

| Field | Value |
|---|---|
| **ID** | `ppe-zones-compliance-rules` |
| **Tier** | back-burner |
| **Status** | stub |
| **Definition** | Per-camera safety-zone polygon editor (work area / no-go / PPE-required) and compliance-rule CRUD that should configure where the PPE worker enforces what. |
| **Frontend** | `frontend/src/components/PPEZoneEditor.tsx`, `frontend/src/components/ComplianceRulesPanel.tsx` |
| **Routes** | — |
| **Tables** | ppe_zones, compliance_rules |
| **Flag** | vlm_safety |
| **Docs** | — |
| **Smoke test** | Edit a camera → open the PPE zone editor → the zone-list request to `/api/cameras/{id}/ppe/zones` returns 404 (expected — see Notes). |
| **Notes** | Routes is — because the backend half was never wired: complete, role-gated CRUD handlers exist in `internal/api/ppe_zones_handler.go` (zone/rule type validation, site_manager+ writes) but **no route in `router.go` registers them**, so all 8 frontend calls (`/api/cameras/{*}/ppe/zones*`, `/api/cameras/{*}/compliance-rules*`) are api-coverage Table C 404s. The tables exist (migration 0024) but nothing reads or writes them — the PPE worker triggers on `sites.ppe_enabled` + camera assignment and ignores zones entirely. The editors' only mount point (`EditCameraModal.tsx`, itself never imported) was deleted in the 2026-06 dead-code cleanup, so the two panels are unmounted orphans pending revival. Revival cost: ~20 lines of route registration plus a mount point (e.g. CameraManager's edit modal) to make the editor work, plus real work to make [[ppe-pending-review]]'s worker actually evaluate zones/rules during detection. |

## PPE pending-review queue {#ppe-pending-review}

| Field | Value |
|---|---|
| **ID** | `ppe-pending-review` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | The PPE worker polls cameras on safety-enabled sites (default every 30 s), runs YOLO, and queues violation frames into a human review queue on the portal dashboard, where managers confirm or dismiss each finding. |
| **Frontend** | `frontend/src/components/portal/PendingReviewQueue.tsx` |
| **Routes** | `GET /api/v1/portal/pending-review` · `POST /api/v1/portal/pending-review/{id}/review` · `GET /api/v1/portal/pending-review/{id}/frame` |
| **Tables** | pending_review_queue, sites, cameras |
| **Flag** | vlm_safety |
| **Docs** | — |
| **Smoke test** | On a site with `ppe_enabled` and an assigned camera, have someone stand in view without a vest → within ~30 s the portal dashboard's review queue shows the entry with its frame → Confirm → entry leaves the pending list. |
| **Notes** | P2-C-01 (worker + queue) and P2-C-03 (async Qwen second opinion via `internal/safety`; `VLM_WORKER_ENABLED=false` by default, so `vlm_verdict` stays `pending` until Qwen is confirmed healthy). Worker runs leader-elected in the worker container; frames land in `PPE_FRAMES_DIR` with a 7-day retention sweep; the `ppe_detected` WS broadcast only fires from the API binary (hub is nil in the worker). Gating gotcha: `PendingReviewQueue` on `/portal` is not behind a `FeatureGate` yet — it shows for any site whose `feature_mode` includes safety, so flag wiring is pending work. The dead `getPendingSafetyFindings`/`validateSafetyFinding` clients (`/api/v1/safety/findings/*`, never backed by a route) were deleted in the 2026-06 dead-code cleanup. Zone-aware enforcement is [[ppe-zones-compliance-rules]] (stub). |

## Person tracking {#person-tracking}

| Field | Value |
|---|---|
| **ID** | `person-tracking` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | Occupancy analytics: a tracking worker consumes the PPE worker's per-frame person counts (no second YOLO call), an aggregator rolls them into 5-minute buckets per camera, and an API serves the buckets. |
| **Frontend** | — |
| **Routes** | `GET /api/v1/portal/person-tracks` |
| **Tables** | person_track_frames, person_track_buckets |
| **Flag** | person_tracking |
| **Docs** | — |
| **Smoke test** | With the PPE worker observing people, `person_track_buckets` rows accumulate (psql) and `GET /api/v1/portal/person-tracks?site_id=<id>` returns buckets with `person_minutes` and `peak_person_count`. |
| **Notes** | P2-C-02. Partial because the route has **zero frontend callers** (api-coverage Table B) — the planned occupancy dashboard was never built; the only user-visible surface of this data is the occupancy section of [[compliance-dashboard]] (via `GetComplianceOccupancy`). Backend is genuinely live: `TRACKING_ENABLED` defaults true, piggybacks on the PPE worker's `TrackingCh` (non-blocking send, so tracking backpressure can't stall PPE), raw frames kept 7 d / buckets 90 d. Revival cost: build the dashboard UI; the data and API are already there. |

## Compliance dashboard + PDF report {#compliance-dashboard}

| Field | Value |
|---|---|
| **ID** | `compliance-dashboard` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Customer-facing `/portal/compliance` page: headline compliance score, violations-over-time chart, top cameras, recent findings, and occupancy for a chosen period, plus a downloadable PDF report. |
| **Frontend** | `frontend/src/app/portal/compliance/page.tsx` |
| **Routes** | `GET /api/v1/portal/compliance/summary` · `GET /api/v1/portal/compliance/report.pdf` |
| **Tables** | pending_review_queue, person_track_buckets, cameras, sites |
| **Flag** | compliance |
| **Docs** | — |
| **Smoke test** | Set `FEATURES_OVERRIDE=compliance=true` → `/portal/compliance` renders headline/chart/cameras for "Last 7 days" → Download PDF saves a report with the same numbers. |
| **Notes** | P2-C-06. Properly parked: the page wraps itself in `FeaturePageGate flag="compliance"`, so with the flag off the route 404s. Periods: today/week/month/90days/custom; PDF built server-side in `internal/pdf`. Data quality depends entirely on [[ppe-pending-review]] and [[person-tracking]] having run. Not this page: `getSiteCompliance` → `/api/v1/sites/{*}/compliance` is an operator-console call with no backend route (Table C, area 07). |

## Labeling queue {#labeling-queue}

| Field | Value |
|---|---|
| **ID** | `labeling-queue` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Admin-only `/admin/labeling` console for building a fine-tuning dataset: claim queued detection frames, record agree/disagree verdicts with corrected labels, watch queue stats, and export the labeled set as JSONL. |
| **Frontend** | `frontend/src/app/admin/labeling/page.tsx` |
| **Routes** | `GET /api/admin/labeling/stats` · `GET /api/admin/labeling/jobs` · `POST /api/admin/labeling/jobs/next` · `POST /api/admin/labeling/jobs/{id}/claim` · `POST /api/admin/labeling/jobs/{id}/label` · `GET /api/admin/labeling/export` |
| **Tables** | vlm_label_jobs, vlm_labels |
| **Flag** | labeling |
| **Docs** | — |
| **Smoke test** | Set `FEATURES_OVERRIDE=labeling=true` → `/admin/labeling` → "Get next job" → submit a verdict → stats counters move; Export downloads a `.jsonl` file. |
| **Notes** | Properly parked: page wraps in `FeaturePageGate flag="labeling"`, and the admin tab link is flag-gated too. api-coverage lists all six routes as "backend-only" (Table B) — that is a static-scan miss: the page calls them with literal `authedFetch('/api/admin/labeling/…')` paths the scanner can't attribute. Admin/soc_supervisor only (`requireAdminOrSupervisor`). The per-id claim route exists but the UI uses `jobs/next`. |

## Re-analysis admin API {#reanalysis-admin}

| Field | Value |
|---|---|
| **ID** | `reanalysis-admin` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Admin API to re-label existing detection rows under a new rules/model version: kick off an async run, poll its status, and audit results via the detections listing with model-version filters. |
| **Frontend** | — |
| **Routes** | `POST /api/admin/reanalyze/` · `GET /api/admin/reanalyze/{run_id}` |
| **Tables** | analysis_runs, detections, model_versions |
| **Flag** | — |
| **Docs** | [reanalysis.md](../reanalysis.md) |
| **Smoke test** | `curl -X POST …/api/admin/reanalyze/` with a rules body → returns `run_id` → poll `GET …/api/admin/reanalyze/{run_id}` until complete → re-labeled detections appear under the new `model_version_id` via `GET /api/v1/detections`. |
| **Notes** | P4-SCHEMA-06 (PR #29). Scope cap: rules-based re-labeling of **existing** detections via the supersede chain — it does NOT re-run inference on stored footage. No UI by design; it is curl/script tooling, hence no flag (nothing customer-reachable). Mind the trailing slash on the POST route (`r.Route("/admin/reanalyze")` + `r.Post("/")`). Cross-ref [[yolo-detection]] for the detections store these runs rewrite. |

## Analytics page {#analytics-page}

| Field | Value |
|---|---|
| **ID** | `analytics-page` |
| **Tier** | back-burner |
| **Status** | placeholder |
| **Definition** | `/analytics` executive dashboard: fleet-wide compliance scores, alert trends, and an incident timeline. |
| **Frontend** | `frontend/src/app/analytics/page.tsx` |
| **Routes** | — |
| **Tables** | — |
| **Flag** | analytics |
| **Docs** | — |
| **Smoke test** | Open `/analytics` as admin — the page renders, but every number comes from `ironsight-mock.ts` regardless of real system state (that IS the current behavior). |
| **Notes** | 100% mock: imports `MOCK_SITES`/`MOCK_ALERTS`/`MOCK_INCIDENTS`, and the embedded `IncidentTimeline` is also a mock importer. Gating gotcha: the nav link is behind `FeatureGate flag="analytics"`, but the page has only a role `RouteGuard` (no `FeaturePageGate`) — reachable by URL for supervisor/admin/site_manager with the flag off. Do not confuse with `components/AnalyticsDashboard.tsx`, which queries real `GET /api/events` on the main live page (area 04). Revival cost: a real aggregation backend; nothing here queries the API today. |

## AI runtime metrics {#ai-runtime-metrics}

| Field | Value |
|---|---|
| **ID** | `ai-runtime-metrics` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Admin Health-tab charts of the AI pipeline: GPU/latency timeseries per service (YOLO, Qwen) and a per-site usage card that estimates GPU cost from call counts. |
| **Frontend** | `frontend/src/components/admin/AIMetricsChart.tsx`, `frontend/src/components/admin/AIUsageBySiteCard.tsx` |
| **Routes** | `GET /api/system/services/timeseries` · `GET /api/system/services/usage` |
| **Tables** | ai_runtime_metrics |
| **Flag** | ai_insights |
| **Docs** | — |
| **Smoke test** | Admin → Health tab → with AI containers handling calls, the YOLO/Qwen panels plot non-empty series and the usage card lists per-site call counts and cost. |
| **Notes** | `StartAIMetricsSampler` (API process) snapshots `ai.Client` counters into `ai_runtime_metrics` on an interval — charts are empty-but-honest when no AI calls have happened. Table B lists the `usage` route as having no caller; that is a scan miss (`AIUsageBySiteCard` calls it via `getAIUsageBySite`, with editable $-per-1k-call inputs). Gating gotcha: both cards render ungated on the admin Health tab today — the `ai_insights` flag is assigned but not yet enforced there. The neighboring `ServicesHealthCard` (`GET /api/system/services`) is core system health, area 05. |
