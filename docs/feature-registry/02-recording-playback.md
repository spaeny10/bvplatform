# Recording & Playback

Everything between the camera's RTSP stream and an operator watching or
exporting footage: the per-camera recording engine, retention/purge,
site-level recording policy, recording-health monitoring, the playback
timeline, clip/evidence export, bookmarks, and storage-location admin.
All features in this area are MVP core (basic VMS/NVR scope).

## Recording Engine {#recording-engine}

| Field | Value |
|---|---|
| **ID** | `recording-engine` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Runs one FFmpeg process per recording-enabled camera (pure-Go gortsplib recorder opt-in via `GORT_CAMERAS`), pulling RTSP from the local mediamtx relay and writing MP4 segments to disk, indexed in the `segments` table. Supports continuous and event (ring-buffer) modes. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | segments, cameras |
| **Flag** | — |
| **Docs** | [streaming.md](../streaming.md) |
| **Smoke test** | Admin → toggle Recording on for a camera. Within ~1 min the Recording Health card shows a fresh segment and new `.mp4` files appear under `<storage>/<camera-uuid>/`. |
| **Notes** | Code lives in `internal/recording/` (engine.go, gort_recorder.go, clip_writer.go); started from `cmd/server/main.go`. Fleet is HEVC-standard — segments record H.265 natively and browser compatibility is solved server-side via the LOCAL-11 `.h264-cache/` transcode sibling dir, never by reconfiguring cameras. FFmpeg killed mid-segment (cellular stall) leaves moov-less files; the codec probe writes an empty `video_codec` so [[playback-timeline]] can filter them. Settings come from site policy ([[recording-schedules]]); recorders pick up changes on next restart only (no hot-swap). |

## Retention & Purge {#retention}

| Field | Value |
|---|---|
| **ID** | `retention` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Hourly background sweep that deletes expired recording segments. Pass order: per-location `max_gb` caps, then per-site retention days (storage-location default as fallback), then an 85%-disk capacity safety valve that prunes oldest-first down to 80%. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | segments, storage_locations, sites |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Set a site's retention to 3 days where older footage exists. After the next hourly pass, `[RETENTION]` log lines confirm deletes and timeline coverage before the cutoff disappears. |
| **Notes** | `internal/recording/retention.go` (RetentionManager, tested in retention_test.go). Deletes the `.h264-cache/` transcode alongside each segment. Explicitly MUST NOT touch audit/evidence tables (UL 827B / TMA-AVS-01, 365-day minimum — append-only triggers backstop this). Also hosts unrelated sweeps (closed support tickets 180d, PPE frames, person-tracking rows) that belong to back-burner features. Runs only where `RUN_WORKERS=true` (the worker container owns it in split deploys). |

## Recording Schedules & Site Policy {#recording-schedules}

| Field | Value |
|---|---|
| **ID** | `recording-schedules` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Site-level recording policy — retention tier, continuous vs event mode, pre/post buffers, triggers, and a multi-window recording schedule. Every camera on the site inherits it; outside scheduled windows the recorder pauses (re-checks every 30s). |
| **Frontend** | `frontend/src/components/admin/SiteRecordingPanel.tsx`, `frontend/src/components/admin/SiteConfigModal.tsx` |
| **Routes** | `PATCH /api/v1/sites/{id}/recording` |
| **Tables** | sites |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Admin → Sites → Configure → Recording tab. Set the "Business hours" preset, save; outside the window the server logs "outside scheduled hours, pausing" for the site's cameras. |
| **Notes** | api-coverage Table B lists this route as backend-only, but SiteRecordingPanel calls it via raw `fetch` the static scan can't resolve — it IS wired. Policy is deliberately site-level with no per-camera override (compliance uniformity after a lost-evidence incident). Changes apply on next recording restart, not hot-swapped. Legacy single-object schedule JSON (`{days,start,end}`) is still parsed alongside the current window-array format. Retention enforcement itself is [[retention]]. |

## Recording Health {#recording-health}

| Field | Value |
|---|---|
| **ID** | `recording-health` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Per-camera traffic-light dashboard (healthy/degraded/stale/off) computed from a rolling 24h of segments — counts, bytes, last-segment age, longest gap — so silent recording failures surface without reading server logs. |
| **Frontend** | `frontend/src/components/RecordingHealthCard.tsx`, `frontend/src/app/admin/page.tsx` |
| **Routes** | `GET /api/recording/health` |
| **Tables** | segments, cameras |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Open /admin → Recording Health: recording cameras show green. Stop one camera's recording; its badge moves to degraded (>2 min gap) then stale (>10 min). |
| **Notes** | Thresholds are duplicated between `internal/api/recording_health.go` and the card's tooltip text — keep in sync when tuning. RBAC-filtered: customer roles only see their assigned cameras. The same card also renders [[sd-card-status]]. |

## SD-Card Status {#sd-card-status}

| Field | Value |
|---|---|
| **ID** | `sd-card-status` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Live probe of a camera's onboard SD card: present/empty/missing/unreachable, capacity, and the recording handles available for ONVIF Profile G replay. Shown per-camera on the Recording Health card. |
| **Frontend** | `frontend/src/components/RecordingHealthCard.tsx` |
| **Routes** | `GET /api/cameras/{id}/sd/status` |
| **Tables** | cameras |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | On /admin → Recording Health, a card-equipped camera shows SD "OK" with capacity; take the camera offline and the column flips to "Offline". |
| **Notes** | `internal/api/sd_status.go`. Probes ONVIF first, falls back to the Milesight vendor endpoint (current CQ_63.x firmware lacks `GetStorageConfigurations`). Uncached live probe — slow on cellular links. Intended precondition for a future Profile G fallback-playback path (FindEventClip) that is not built yet. |

## Playback & Timeline {#playback-timeline}

| Field | Value |
|---|---|
| **ID** | `playback-timeline` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Scrub-to-play historical video: a zoomable timeline (15s–24h windows) with event-density buckets, per-camera coverage bands, and event-type filters; seeking loads the recorded MP4 segment via short-lived signed media URLs. |
| **Frontend** | `frontend/src/components/Timeline.tsx`, `frontend/src/components/VideoPlayer.tsx`, `frontend/src/app/page.tsx` |
| **Routes** | `GET /api/timeline` · `GET /api/timeline/coverage` · `GET /api/playback/{id}` · `GET /api/playback/{id}/playlist.m3u8` · `GET /api/cameras/{id}/recordings` |
| **Tables** | segments, events, playback_audits |
| **Flag** | — |
| **Docs** | [streaming.md](../streaming.md), [media-auth.md](../media-auth.md) |
| **Smoke test** | Main page → select a camera → scrub the timeline back 10 min. Recorded video plays, coverage bands match recorded spans, event ticks filter by type. |
| **Notes** | The player uses the direct-MP4 path (`/api/playback/{id}` returns 5-min signed `/media/v1/<token>` URLs; client caches a ~1h segment window and re-fetches on token expiry). The HLS VOD playlist route and `GET /api/cameras/{id}/recordings` have NO frontend caller — alternate/legacy paths, candidates to cut. Corrupt moov-less segments are filtered server-side (`video_codec=''`, tiny size/duration) and the player also walks forward past bad files. Every playback request is audited to `playback_audits`. |

## Clip Export & Evidence Download {#clip-export}

| Field | Value |
|---|---|
| **ID** | `clip-export` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Two export paths: (1) operator picks camera + time range, a DB-queued worker FFmpeg-concats the segments to a downloadable MP4; (2) one-click per-event evidence ZIP from portal history, bundling the clip with an `event.json` manifest and optional HMAC signature. |
| **Frontend** | `frontend/src/components/ExportDialog.tsx`, `frontend/src/app/portal/history/page.tsx` |
| **Routes** | `POST /api/exports` · `GET /api/exports` · `GET /api/events/{id}/export` · `ANY /exports/*` |
| **Tables** | exports, segments, events |
| **Flag** | — |
| **Docs** | [chain-of-custody.md](../chain-of-custody.md) |
| **Smoke test** | Main page → Export → camera + last hour → "Export to MP4"; job reaches completed and the Download link serves the file. Portal → History → "Download evidence package (.zip)" on any event row. |
| **Notes** | Worker (`internal/export/export.go`) is DB-polled with `FOR UPDATE SKIP LOCKED` + stuck-job requeue (the old channel-driven version silently dropped jobs); runs under `RUN_WORKERS=true`. Gotcha: `/exports/*` MP4 downloads are served by a bare FileServer with NO auth — obscure-URL only; router.go notes a follow-up to fold them into `/media/v1/`. Evidence ZIP (`internal/api/evidence_export.go`) embeds AI verdict + AVS score when present; signing requires `EVIDENCE_SIGNING_KEY` else the bundle is unsigned. Evidence share links / manifests lifecycle is a separate back-burner feature (area 06/07 files). |

## Bookmarks {#bookmarks}

| Field | Value |
|---|---|
| **ID** | `bookmarks` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Timeline bookmarks: label + notes + severity pinned to a camera and a moment, for marking footage of interest. |
| **Frontend** | `frontend/src/lib/api.ts` |
| **Routes** | `POST /api/bookmarks` · `GET /api/bookmarks` · `DELETE /api/bookmarks/{id}` |
| **Tables** | bookmarks |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | `curl -X POST /api/bookmarks` with `{camera_id, event_time, label}` (auth + CSRF), then `GET /api/bookmarks?start=&end=` returns the row. No UI step exists. |
| **Notes** | Backend CRUD (`internal/api/audit.go`, db.go) and the api.ts client functions are complete, but NO component imports `createBookmark`/`listBookmarks`/`deleteBookmark` — there is no user-facing surface, hence partial. api-coverage Table A counts the lib functions as callers, which overstates the wiring. Completion cost: small — add a bookmark button + marker row to [[playback-timeline]]. |

## Storage Locations Admin {#storage-locations}

| Field | Value |
|---|---|
| **ID** | `storage-locations` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Admin UI for where recordings live: add/edit/remove storage locations with purpose, priority, retention default, and `max_gb` cap; includes a drive list, folder browser, and live disk-usage bars. |
| **Frontend** | `frontend/src/components/SettingsPage.tsx`, `frontend/src/app/admin/page.tsx` |
| **Routes** | `GET /api/storage/locations` · `POST /api/storage/locations` · `PUT /api/storage/locations/{id}` · `DELETE /api/storage/locations/{id}` · `GET /api/storage/drives` · `GET /api/storage/browse` · `GET /api/storage/disk-usage` · `GET /api/storage/status` |
| **Tables** | storage_locations |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Admin → Settings → Storage: add a location via the drive/folder browser; usage bar renders; new segments land under `<path>/recordings/`. |
| **Notes** | Gotcha: `GET /api/storage/status` reads the DB live (2026-06-08 fix) but the recording engine + live-HLS still cache `cfg.StoragePath` from startup — adding a location needs an API restart before recorders use it (documented follow-up). `frontend/src/components/settings/StorageTab.tsx` is an unwired duplicate of SettingsPage's storage section (never imported) — dead code. Caps and retention defaults set here are enforced by [[retention]]. |
