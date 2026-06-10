# 01 — Live view

Everything an operator uses to watch cameras in real time: the multi-camera
grid with saved layouts, the mediamtx-backed HLS delivery pipeline behind it,
the single-camera popout window, PTZ control, the floorplan map, and the
embedded camera web UI for on-device VCA configuration. All of it rides the
session-cookie-authenticated `/api/live/*` proxy — there are no media tokens
on the live path anymore.

## Live camera grid {#live-camera-grid}

| Field | Value |
|---|---|
| **ID** | `live-camera-grid` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Multi-camera live wall with named, saveable layouts: static presets (1×1 through 6×4) with per-slot camera assignment, or freeform drag/resize canvas. Each tile is a `VideoPlayer` playing the camera's live HLS stream. |
| **Frontend** | `frontend/src/components/CameraGrid.tsx`, `frontend/src/components/VideoPlayer.tsx` |
| **Routes** | `GET /api/cameras/` · `GET /api/live/{cameraID}/*` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [streaming.md](../streaming.md) |
| **Smoke test** | Log in to test.ironsight → Live tab → create a 2×2 static layout, assign two cameras → both tiles show video with a LIVE badge within ~5 s. |
| **Notes** | Layouts live in browser localStorage only (`ironsight-layouts`, with a shim migrating legacy `onvif-tool-*` keys) — per-browser, not per-user/server. Gotcha: the HD/SD quality toggle is vestigial in live mode post-pivot — the proxy hard-codes the `_sub` stream and the URL never changes with quality (the toggle just forces a reconnect); see [[live-hls-pipeline]]. Firefox cannot decode HEVC, so it gets the error overlay on every tile (fleet is H.265-standard). Tile timestamp overlay applies to playback mode only. |

## Live HLS pipeline {#live-hls-pipeline}

| Field | Value |
|---|---|
| **ID** | `live-hls-pipeline` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Server-side delivery path for live view: mediamtx serves native HLS per camera, and `HandleLiveProxy` reverse-proxies it at `/api/live/{cameraID}/*` with per-request camera ACL checks, playlist URL rewriting, and an on-the-fly hvcC `array_completeness` byte-patch on `init.mp4` so Chromium MSE accepts HEVC. |
| **Frontend** | `frontend/src/components/VideoPlayer.tsx` |
| **Routes** | `GET /api/live/{cameraID}/*` |
| **Tables** | cameras |
| **Flag** | — |
| **Docs** | [streaming.md](../streaming.md), [decisions.md](../decisions.md) |
| **Smoke test** | While logged in to test.ironsight, fetch `/api/live/<cameraId>/index.m3u8` — playlist returns 200 with segment URIs rewritten to `/api/live/<cameraId>/...`; the same camera's tile plays in Chrome. |
| **Notes** | Auth is the SSO session cookie — no media tokens, no refresh loop. Only the `_sub` (sub-stream) path is proxied; there is no main-stream live option yet. The previous gohlslib LL-HLS path (`internal/streaming/livehls.go` `LiveHLSManager`, its warm-pool goroutine in `cmd/server/main.go`, and the `live-hls` token kind in `internal/api/media_v1.go` / `frontend/src/lib/media.ts`) was deleted in the 2026-06 dead-code cleanup; the vendored patched mediacommon stays (still used by the Go recorder, and the hvcC byte-patch test in `internal/streaming/livehls_h265_init_test.go` covers it). The "Live View" section of docs/streaming.md still documents the old gohlslib path and is stale. Firefox HEVC gap is surfaced as a per-tile codec error; server-side transcode is the eventual fix. |

## Popout single-camera view {#live-popout}

| Field | Value |
|---|---|
| **ID** | `live-popout` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Dedicated full-window live page for one camera (`/popout/<cameraId>`), opened via the peek modal's "Pop Out" button for multi-monitor setups. Includes scroll-wheel digital zoom/pan and the PTZ overlay when the camera supports it. |
| **Frontend** | `frontend/src/app/popout/[cameraId]/page.tsx` |
| **Routes** | `GET /api/cameras/{id}` · `GET /api/live/{cameraID}/*` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | [streaming.md](../streaming.md) |
| **Smoke test** | Double-click a grid tile to peek → click "↗ Pop Out" → a 960×600 window opens with full-size live video and the camera name in the title bar; scroll to zoom. |
| **Notes** | Fetches the camera record directly with `fetch` + `credentials: 'include'`, so it works as a standalone window with the same session cookie. Always live mode — no playback in the popout. Cross-ref [[live-camera-grid]]. |

## PTZ controls {#ptz-controls}

| Field | Value |
|---|---|
| **ID** | `ptz-controls` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Pan/tilt/zoom control for PTZ-capable cameras: hold-to-move arrow/zoom overlay on live tiles, connection prewarm when a PTZ tile mounts (cuts first-move latency), and Milesight preset-goto from the camera's advanced settings. |
| **Frontend** | `frontend/src/components/VideoPlayer.tsx`, `frontend/src/components/MilesightAdvanced.tsx`, `frontend/src/components/milesight/tabs.tsx` |
| **Routes** | `POST /api/cameras/{id}/ptz/move` · `POST /api/cameras/{id}/ptz/stop` · `POST /api/cameras/{id}/ptz/prewarm` · `POST /api/cameras/{id}/milesight/ptz/preset/goto` |
| **Tables** | cameras |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Select a PTZ camera's live tile → hold an arrow button → camera pans; release → motion stops within ~1 s. In Edit Camera → Milesight → PTZ, goto preset 1. |
| **Notes** | Stop is debounced 50 ms so direction changes don't emit stop/start churn. Overlay only renders when `has_ptz` and live. The separate, unwired operator-console PTZ path (`getPTZCapability`/`sendPTZCommand` → `/api/v1/cameras/{*}/ptz*`, which never had a backend route) was deleted in the 2026-06 dead-code cleanup along with its only consumer, `operator/PTZControls.tsx` (F-23). |

## Map view {#map-view}

| Field | Value |
|---|---|
| **ID** | `map-view` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Floorplan tab on the live page: operator uploads a site map image and drags camera pins onto it; pins show real per-camera online/offline status and click through to the live peek view. |
| **Frontend** | `frontend/src/components/MapView.tsx` |
| **Routes** | — |
| **Tables** | — |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Live page → Map tab → upload a floorplan image → Edit Positions → drag a pin onto the map → Done → click the pin → live peek opens. |
| **Notes** | Camera names/status are real API data (passed from the live page's camera list), but the feature has no backend: the floorplan is stored as a base64 data-URL and pin positions as JSON, both in localStorage (`map-image`, `map-positions`) — per-browser, not shared between operators, and large floorplans can blow the ~5 MB localStorage quota. Kept core because it renders real site data; see open question on whether localStorage-only persistence is acceptable for MVP. Unrelated: the operator console's `getSiteMap`/`updateSiteMap` (`/api/v1/sites/{*}/map`) are frontend-only 404s (Table C) and belong to 07-soc-operator.md, not this component. |

## Camera web-UI proxy {#camera-web-ui-proxy}

| Field | Value |
|---|---|
| **ID** | `camera-web-ui-proxy` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | "Camera VCA (on-device)" tab in the camera editor embeds the camera's own web UI in an iframe so on-device VCA (intrusion, line-cross) is configured at the source of truth instead of via the fragile pull/push sync. |
| **Frontend** | `frontend/src/components/VCAZoneEditor.tsx`, `frontend/src/components/CameraManager.tsx` |
| **Routes** | — |
| **Tables** | cameras |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Edit a camera → VCA section → "Camera VCA (on-device)" tab → iframe loads the camera's login page (currently only from a browser with LAN access to the camera; see Notes). |
| **Notes** | Routes is — on purpose: the backend proxy that makes this work from anywhere lands in open PR #50 and is not on this branch, so listing its routes would fail the router.go lint. Today the iframe points at `http://<camera onvif_address>/` directly, which (a) requires the operator's browser to be on the camera LAN and (b) is blocked as active mixed content when Ironsight is served over HTTPS — hence Status partial. The tab is disabled when the camera IP is unknown; an "Open in new tab" escape hatch exists for cameras that refuse iframe embedding. The legacy VCA pull/push sync buttons were removed from the UI (coordinate-space/slot-mapping fidelity loss) but their backend endpoints remain — see 03-cameras-devices.md. Update Routes here when PR #50 merges. |
