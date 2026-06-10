# Cameras & devices

Everything about getting devices into the platform and configuring them: camera
CRUD and ONVIF discovery, at-rest credential encryption, Milesight vendor config
panels, VCA rule management, the Sense push-camera webhook, and the (parked)
speaker/talk-down stack. The live UI for all of it is the Cameras tab of
`SettingsPage.tsx` → `CameraManager.tsx` — note that several "extracted" modal
components in `frontend/src/components/` are orphans from an unfinished refactor
(see Notes in each block).

## Camera management (CRUD + reboot) {#camera-crud}

| Field | Value |
|---|---|
| **ID** | `camera-crud` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Add, edit, and remove cameras (ONVIF address + credentials, continuous or sense-push device class), with bulk select/delete and a remote ONVIF reboot action. The create path probes the camera over ONVIF and enumerates RTSP profiles. |
| **Frontend** | `frontend/src/components/CameraManager.tsx`, `frontend/src/components/SettingsPage.tsx` |
| **Routes** | `GET /api/cameras/` · `POST /api/cameras/` · `GET /api/cameras/{id}` · `PATCH /api/cameras/{id}` · `DELETE /api/cameras/{id}` · `POST /api/cameras/{id}/reboot` |
| **Tables** | cameras |
| **Flag** | — |
| **Docs** | [soft-delete](../soft-delete.md) |
| **Smoke test** | Settings → Cameras → Add Camera with an ONVIF address + creds; camera appears and goes online. Rename it via Edit, then delete it. |
| **Notes** | Delete is a soft delete that cascades to `vca_rules`/`ppe_zones`/`compliance_rules`. Gotcha: `PATCH /api/cameras/{id}` can update username but NOT password (`CameraUpdate` in `internal/database/models.go` has no password field) — rotating a camera password means delete + re-add. Reboot is admin/soc_supervisor-gated, send-and-forget (camera drops the socket mid-reply). `AddCameraModal.tsx` and `SenseWebhookFields.tsx` were extracted from CameraManager (P1-B-11) but the modal is imported by nothing — the inline versions in CameraManager.tsx are what runs; finish the refactor or delete the orphans (`EditCameraModal.tsx` was already deleted in the 2026-06 dead-code cleanup). |

## ONVIF discovery {#onvif-discovery}

| Field | Value |
|---|---|
| **ID** | `onvif-discovery` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Scans the local network for ONVIF cameras (WS-Discovery, 5 s window), shows each device with an optional snapshot preview, and bulk-adds the selected ones with shared credentials. |
| **Frontend** | `frontend/src/components/CameraManager.tsx` |
| **Routes** | `POST /api/discover` · `POST /api/discover/preview` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Settings → Cameras → Discover; scan lists LAN cameras; load a preview, select one, enter creds, Add Selected — camera appears in the list. |
| **Notes** | WS-Discovery is multicast: the API server must sit on the same L2 segment as the cameras (it will find nothing across routed subnets — relevant for the 192.168.50.0/24 trailer LANs). Discovery itself persists nothing — bulk add calls [[camera-crud]]'s create endpoint once per selected device, with the shared username/password typed into the modal, so wrong creds fail per-device, not at scan time. `DiscoveryModal.tsx` is another orphaned extraction — the live modal is inline in CameraManager.tsx. |

## Camera credentials at rest {#camera-credentials-at-rest}

| Field | Value |
|---|---|
| **ID** | `camera-credentials-at-rest` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Camera passwords are AES-256-GCM encrypted before they hit the database (`crypt:v1:` prefixed wire format) and transparently decrypted on the read path for ONVIF/RTSP consumers. No UI — purely a storage property. |
| **Frontend** | — |
| **Routes** | — |
| **Tables** | cameras |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | `SELECT password FROM cameras LIMIT 5` — every non-empty row starts with `crypt:v1:`. Open live view on one camera — stream plays, proving the decrypt path. |
| **Notes** | Implementation: `internal/crypto/credentials.go` (the package doc is the canonical reference; `CAMERA_CREDENTIALS_KEY` is missing from docs/configuration.md — doc gap). Key via env, 64-char hex or 44-char base64; `cmd/server` hard-requires it at boot, decrypt fails closed (wrong key → empty password, auth fails rather than leaking ciphertext). No escrow: losing the key means re-entering every camera password. Legacy plaintext rows are tolerated on read and re-encryption is idempotent (prefix check). `sense_webhook_token` is deliberately stored plaintext — it is the lookup key for [[sense-webhook]]. |

## Milesight vendor config panels {#milesight-config}

| Field | Value |
|---|---|
| **ID** | `milesight-config` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Typed pass-through to Milesight CGI config: streams, OSD, image, audio, network, privacy masks, auto-reboot, PTZ presets, and alarm I/O, plus a vendor reboot action. Rendered as the "Milesight" tab in camera settings when the manufacturer resolves to Milesight. |
| **Frontend** | `frontend/src/components/MilesightAdvanced.tsx`, `frontend/src/lib/milesight.ts` |
| **Routes** | `GET /api/cameras/{id}/milesight/config/{panel}` · `PUT /api/cameras/{id}/milesight/config/{panel}` · `POST /api/cameras/{id}/milesight/reboot` · `POST /api/cameras/{id}/milesight/ptz/preset/goto` |
| **Tables** | — |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Edit a Milesight camera → Milesight tab → OSD: change the overlay text, Save, reload the panel — the new value reads back from the camera. |
| **Notes** | The `{panel}` slug allowlist in `internal/api/milesight_config.go` is the security boundary — no free-form CGI actions reach the camera. Writes and reboot are admin-only; saves are explicit (no autosave) because a bad write can brick a production camera. Response shapes mirror vendor JSON verbatim; `lib/milesight.ts` holds the typed contract per panel. Live PTZ (move/stop/prewarm) is a separate feature in 01-live-view.md. |

## VCA rules (platform zones + camera sync) {#vca-rules}

| Field | Value |
|---|---|
| **ID** | `vca-rules` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Draw intrusion/line-cross/region-entrance/loitering zones on a camera snapshot and store them platform-side; backend endpoints can push rules to Milesight firmware or pull the camera's rule set back into the DB. |
| **Frontend** | `frontend/src/components/VCAZoneEditor.tsx`, `frontend/src/lib/vca-zones.ts`, `frontend/src/hooks/useVCASnapshot.ts`, `frontend/src/hooks/useVCACanvas.ts` |
| **Routes** | `GET /api/cameras/{id}/vca/rules` · `POST /api/cameras/{id}/vca/rules` · `PUT /api/cameras/{id}/vca/rules/{ruleId}` · `DELETE /api/cameras/{id}/vca/rules/{ruleId}` · `POST /api/cameras/{id}/vca/sync` · `GET /api/cameras/{id}/vca/pull` · `POST /api/cameras/{id}/vca/pull` · `GET /api/cameras/{id}/vca/snapshot` |
| **Tables** | vca_rules |
| **Flag** | — |
| **Docs** | [soft-delete](../soft-delete.md) |
| **Smoke test** | Edit camera → VCA tab → Platform zones: draw an intrusion polygon — it saves and lists; toggle it off/on; delete it. |
| **Notes** | Why partial: zone CRUD + snapshot work end-to-end, but the sync/pull UI is gone — the dead `{false && ...}` block and its handlers/clients (`syncVCARules` in api.ts, `vcaPullPreview`/`vcaPullApply` in milesight.ts) were deleted in the 2026-06 dead-code cleanup, so `/vca/sync` and `/vca/pull` are unreachable from the UI even though the backend (Milesight `dataloader.cgi` driver) is real and stays. The round-trip was abandoned as fragile (coordinate-space/slot-mapping fidelity loss); on-device VCA is now configured via the "Camera VCA" tab, which iframes `http://<camera-ip>/` directly — that only works when the browser can reach the camera LAN; the PR #50 camera web-UI proxy (01-live-view.md) is the fix. Camera-emitted VCA events flow into the alert feed regardless (04-alerts-notifications.md). Caveat on tier: the platform-side zones are consumed by server-side AI detection, which is back-burner — the editor stays core per the 2026-06 scope, but it has no consumer at MVP until camera-side alerts are the source. |

## Sense push webhook {#sense-webhook}

| Field | Value |
|---|---|
| **ID** | `sense-webhook` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Inbound endpoint for Milesight Sense / SC4xx PIR cameras: the camera POSTs JSON + snapshot on every event, authenticated by a long per-camera token in the URL. Events land in the normal event/alarm pipeline and broadcast to operators like any other alarm. |
| **Frontend** | `frontend/src/components/CameraManager.tsx` |
| **Routes** | `POST /api/integrations/milesight/sense/{token}` |
| **Tables** | cameras, events, active_alarms, detections |
| **Flag** | — |
| **Docs** | [soft-delete](../soft-delete.md) |
| **Smoke test** | Add a camera with device class "Sense (push)"; copy the four webhook fields into the camera's Alarm Server config; wave at the PIR — an alarm appears in the feed and the camera flips from awaiting_first_event to online. |
| **Notes** | Unauthenticated by design — the token IS the auth (cameras can't carry JWTs); lookups don't leak camera existence (uniform 401). Webhook-first matches the platform-wide ingest preference. The token is minted at create and shown in a one-time setup overlay (CameraManager renders it via a local `SenseWebhookFields` helper, line 1078 — the standalone `SenseWebhookFields.tsx` file is an orphaned copy); it is also visible later in the Edit → General tab. Snapshots are written to `snapshots/<camera_id>/` with a legacy-shaped URL that the frontend rewrites through `/api/media/mint`. Also dual-writes to `detections` (P4-SCHEMA-02). Operator guide: `frontend/Documents/SenseCamera.md`. |

## Speakers + audio messages (talk-down) {#speakers-audio}

| Field | Value |
|---|---|
| **ID** | `speakers-audio` |
| **Tier** | back-burner |
| **Status** | partial |
| **Definition** | Register IP speakers, upload audio message clips, play/stop a clip through a speaker's ONVIF RTSP backchannel, and assign speakers to sites. Talk-down dock on the live dashboard plus a management tab in Settings. |
| **Frontend** | `frontend/src/components/SettingsPage.tsx`, `frontend/src/app/page.tsx`, `frontend/src/components/admin/AssignCameraModal.tsx` |
| **Routes** | `GET /api/speakers` · `POST /api/speakers` · `DELETE /api/speakers/{id}` · `POST /api/speakers/{id}/play/{messageId}` · `POST /api/speakers/stop` · `GET /api/speakers/status` · `GET /api/speaker-info` · `GET /api/audio-messages` · `POST /api/audio-messages` · `DELETE /api/audio-messages/{id}` · `GET /api/v1/speakers` · `POST /api/v1/sites/{siteId}/speaker-assignments` · `DELETE /api/v1/sites/{siteId}/speaker-assignments/{speakerId}` |
| **Tables** | speakers, audio_messages, device_assignments |
| **Flag** | `speakers` |
| **Docs** | — |
| **Smoke test** | Settings → Speakers: add a speaker with an RTSP backchannel URI, upload a clip, press Play — `/api/speakers/status` reports it playing on that speaker. |
| **Notes** | Parked for MVP (not in the basic-VMS list). Why partial: management CRUD, uploads, and site assignment are fully wired with no mock data, but playback goes through `onvif.BackchannelPlayer` and fails fast without a configured RTSP backchannel URI — end-to-end talk-down is unverified without a physical speaker on the test bench. Parking gotcha: the `speakers` flag does NOT yet exist in `HandleFeatureFlags` (`internal/api/platform.go` currently returns only vlm_safety/semantic_search/evidence_sharing/global_ai_training, and no `FEATURES_OVERRIDE` handling is implemented) — hiding this needs backend flag plumbing plus gating the Settings tab, the dashboard talk-down dock, and the speaker section of AssignCameraModal. `settings/SpeakersTab.tsx` is an orphaned extraction; the live UI is inline in SettingsPage.tsx. Revival cost: low — code is complete, just needs hardware verification. |
