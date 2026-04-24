# IRONSIGHT — Platform Architecture & Workflow Specification

> **NOTE:** This application has been officially renamed and consolidated to **Ironsight** (formerly referred to as "SiteGuard AI" in earlier documentation).

## 1. Platform Overview & Tech Stack

Ironsight provides real-time construction site intelligence. It acts as the unified frontend for both high-urgency security monitoring (via ONVIF Profile M AI analytics / YOLOv8) and proactive safety/compliance auditing (via asynchronous Vision-Language Models).

**Frontend Stack:**
- **Framework:** Next.js 14.2 (App Router)
- **Language:** TypeScript 5.3
- **State Management:** Zustand (client, persisted), TanStack React Query (server)
- **Real-Time Data:** WebSockets (`/ws` for NVR events, `/ws/alerts` for alarm stream)
- **Video:** hls.js (HLS via MediaMTX proxy) + WebRTC WHEP fallback
- **Styling:** CSS custom properties + inline styles (no Tailwind)
- **Auth:** JWT stored in `localStorage`, injected as `Authorization: Bearer` on all API calls

**Backend Stack:**
- **Language:** Go
- **HTTP Router:** chi v5
- **Database:** PostgreSQL (pgx v5) — auto-migrations on startup
- **Auth:** HS256 JWT signed in `internal/auth`, validated by `RequireAuth` middleware
- **Video Pipeline:** MediaMTX (RTSP → HLS/WebRTC), ONVIF discovery/PTZ/backchannel
- **AI Detection:** `internal/detection.Manager` — receives ONVIF analytics events, caches latest result per camera, emits alerts via `AlertEmitter` hook
- **Recording:** Internal engine writing MP4 segments to configurable storage path
- **Entry point:** `cmd/server/main.go` — runs migrations, wires all subsystems, starts HTTP server on `:8080`

---

## 2. Multi-Tenant Data Architecture & RBAC

The system follows a strict hierarchical multi-tenant structure to support construction workflows:
- **Organization:** The Parent Customer (e.g., "Apex Construction").
- **Sites:** Geographically distinct job sites. **Crucially, Site SOPs and Call Trees are tied to the Site, not the Organization.**
- **Access Control:** Users are explicitly assigned to specific Sites. React Query hooks automatically filter macro-dashboards and access based on the JWT claims.

### Role Definitions

| Role | Login Lands On | Route Access | Description |
|------|---------------|-------------|-------------|
| `admin` | `/` | All routes | Full system access |
| `soc_supervisor` | `/operator` | `/operator`, `/analytics`, `/search`, `/portal`, `/` | Oversee operators and SLA queue |
| `soc_operator` | `/operator` | `/operator` **only** | Monitor sites, claim and disposition alarms |
| `site_manager` | `/portal` | `/portal`, `/analytics`, `/` | View portal, manage site SOPs |
| `customer` | `/portal` | `/portal`, `/` | View compliance reports and portal |
| `viewer` | `/` | `/` | Camera feeds — read only |

### Route Permission Matrix

| Route | `admin` | `soc_supervisor` | `soc_operator` | `site_manager` | `customer` | `viewer` |
|-------|:-------:|:----------------:|:--------------:|:--------------:|:----------:|:--------:|
| `/` — NVR / Camera Grid | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ |
| `/operator` — SOC Monitor | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| `/admin` — Admin Panel | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `/portal` — Customer Portal | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ |
| `/search` — Incident Search | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `/analytics` — Analytics | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ |

### Enforcement Layers

1. **Post-login redirect** (`login/page.tsx`) — routes to role home immediately on auth
2. **`RouteGuard` component** — placed in each route segment's `layout.tsx`; redirects before any content renders
3. **Root page redirect** (`page.tsx`) — `soc_operator` bounced to `/operator` on mount
4. **`ROUTE_PERMISSIONS` matrix** (`AuthContext.tsx`) — single source of truth, used by `hasPermission()` and `canAccess()` helpers
5. **`RequireAuth` middleware** (Go backend) — JWT validation on all `/api/*` routes

---

## 3. The SOC Operator Workflow (Security)

SOC Operators handle high-stakes, off-hours security alarms. The workflow is designed to minimize cognitive load and alarm fatigue using a **Directed Dispatch Model**.

### Routing & Queue Logic
- **State Tracking:** Operators have statuses: `Available`, `Engaged` (working an event), or `Away`.
- **Assignment:** Verified alarms are pushed directly to the longest-idle `Available` operator.
- **The Queue:** If all operators are `Engaged`, alarms enter a strict First-In-First-Out (FIFO) queue.
- **SLA Tracking:** No auto-escalation. Timers strictly track "Time-in-Queue" and "Time-to-Resolution" for analytics. Supervisors can monitor the queue and manually intervene if SLA thresholds (30s/90s/180s) bleed into the red.

### The "Active Alarm" UI Interface
When an operator claims an alarm, the 2x3 grid is replaced by a high-urgency 3-pane focused view:
1. **Video Investigation (Center/Left):** Loops the AI event clip, features a toggle for Live View, instantly loads adjacent cameras, and displays a mini Site Map.
2. **Site Intelligence (Top Right):** Automatically pulls the Site-specific SOP and Call Tree. Features "Desk Phone V1" quick-log buttons (e.g., `[Spoke to Contact]`, `[Left Voicemail]`) with phone number copy functionality. Space reserved for future VOIP dialer.
3. **Action & Disposition (Bottom Right):** Action log and definitive disposition dropdown (e.g., "Verified - Police Dispatched"). Submitting creates a **Security Event**.

---

## 4. Customer Portal Workflow (Safety & Security)

The Customer Portal (`/portal`) acts as the passive discovery and dashboarding center for Site Managers and Executives.

### The Macro Landing Page
- **Command View:** Displays a Map and/or Grid of assigned sites.
- **Surface KPIs:** Each site card shows PPE Compliance Score, Open/Unread Incidents, and Camera Fleet Status.
- **Security Triage:** "Morning Briefing" UI prioritizes overnight SOC Security Events with high-contrast Severity Pills, ensuring they aren't lost among routine safety metrics.

### Site Drill-Down: The "NVR-Plus" Interface (`/portal/sites/[id]`)
Instead of a completely separate UI, the site drill-down relies on the battle-tested **IRONSight NVR interface (`src/app/page.tsx`)**, augmented with AI data:
- **Core Video:** The standard NVR 21:8 panoramic viewer and multi-grid timeline.
- **KPI Injection:** A collapsible side-panel or top-bar overlays the PPE breakdown bars, worker counts, and elevated risk banners.
- **AI Overlay:** Bounding boxes map directly over the NVR video canvas.
- **Navigation:** A prominent `[ ← Back to Site Selector ]` button clears the active site state and returns the user to the macro view.

---

## 5. Evidence Handling & Export Architecture

Clips are inherently stored on the local job site NVR, meaning Ironsight operates a proxy streaming architecture. 

**Offline Handling:** If a site loses connection, the UI gracefully disables playback and alerts the user that evidence is safely bookmarked locally.

**Evidence Export Capabilities:**
- **MP4 Download:** Direct file download pulled from the NVR.
- **Secure Shareable Link:** Users can generate a public URL for law enforcement (`/evidence/[token]`).
- **User-Defined Expiry:** Links can expire in 1hr, 1 day, 1 week, 1 month, or never.
- **Cloud Sync:** Generating a shareable link triggers the backend to copy the local NVR clip to cloud storage, ensuring the link remains active even if the job site NVR is stolen or loses power.

---

## 6. vLM Safety Engine & Active Learning Flywheel

Safety features are decoupled from Security. They are powered by an asynchronous Vision-Language Model (vLM) and are implemented as a **feature-flagged add-on tier**.

### Semantic Search & Proactive Auditing
- Safety Managers can search natural language queries (e.g., "missing hardhat near crane") across indexed vLM metadata.
- "Create Incident" action converts a search result into an official Safety Incident dossier.

### The Validation Loop & Training Flywheel
To prevent false-positive fatigue, the vLM auto-pushes potential violations into a **Pending Review Queue** on the portal dashboard.
- Managers quickly click `[ ✅ Valid ]` (adds to official KPI stats) or `[ ❌ False ]`.
- **Active Learning Payload:** Clicking False triggers a micro-interaction ("What did the AI see?"). The system captures the image, the failed caption, and the user correction.
- **Anonymization & Telemetry:** Customer and Site IDs are stripped from the payload. The generic data pair is pushed to an AI Telemetry Data Lake.
- **Global Model:** This data strictly trains a single Global AI Model, providing a massive network effect for all tenants.
- **Admin Opt-Out:** Organizations can opt-out of global data sharing via the `/admin` panel.

---

## 7. Vendor Camera Integration — Milesight

Milesight cameras (MS-C* / MS-N* families) expose ~30 configuration panels through a dot-notation CGI vocabulary that the vendor's own web UI uses. The platform exposes the operationally relevant subset as a typed pass-through layer, so operators never need to leave Ironsight to configure a Milesight device.

### 7.1 Architecture

- **CGI transport** — `internal/onvif/milesight_cgi.go` adds `MilesightGet` / `MilesightPost` to `*onvif.Client`. Reads go to `/cgi-bin/operator/operator.cgi?action=get.X.Y&format=json`, writes to `/cgi-bin/admin/admin.cgi?action=set.X.Y&format=json`. HTTP Digest auth, same credentials as ONVIF.
- **Allowlisted pass-through** — `internal/api/milesight_config.go` exposes `GET/PUT /api/cameras/{id}/milesight/config/{panel}`. The `{panel}` slug is validated against a hard-coded map to the vendor's get/set action pair. No free-form action strings reach the camera.
- **Typed contract on the client** — `frontend/src/lib/milesight.ts` owns the JSON shape for each panel. The backend does not re-shape the vendor payload; the server passes bytes through and the TS types are the single source of truth for field names.
- **UI** — `MilesightAdvanced.tsx` renders as a **Milesight** tab in the camera settings modal. Only shown when the camera's manufacturer resolves to Milesight. Eight sub-tabs, one `SaveBar` per panel (explicit saves — no autosave, since a bad write can silently misconfigure a production device).

### 7.2 Panel inventory

| Sub-tab | Panel slug | GET action | SET action | Notes |
|---|---|---|---|---|
| OSD | `osd` | `get.video.advanced` | `set.video.advanced` | Text overlay, date/time, font, background per stream |
| Image | `image` | `get.camera.setting` | `set.camera.setting` | Brightness, contrast, saturation, sharpness, defog, rotation |
| Audio | `audio` | `get.audio` | `set.audio` | Enable, codec, gain, volumes, denoise |
| Network | `network` | `get.system.information` | `set.system.information` | DHCP, IP/mask/gateway, DNS, device name |
| Privacy | `privacyMask` | `get.camera.mask` | `set.camera.mask` | Up to 8 rectangular masks with per-mask color |
| System | `system` + `datetime` + `autoReboot` | various | various | Firmware readout, NTP sync, scheduled auto-reboot, immediate reboot |
| PTZ | `ptzPresets` | `get.ptz.preset` | `set.ptz.basic` | List + recall (goto preset); create via native UI |
| Alarm I/O | `alarmInput` + `alarmOutput` | `get.event.input` / `get.event.externoutput` | `set.event.input` / `set.event.externoutput` | Wired sensor input + relay output (cameras without a wired output degrade gracefully) |

Additional panels exposed but not surfaced as top-level sub-tabs: `dayNight`, `imageCaps`, `networkPlatform` (GB28181), `networkSnmp`.

### 7.3 Action endpoints

Two side-effectful operations don't fit the get/set shape and have dedicated routes:

| Route | Role | Effect |
|---|---|---|
| `POST /api/cameras/{id}/milesight/reboot` | `admin` only | Immediate reboot via `reboot.system.maintenance` |
| `POST /api/cameras/{id}/milesight/ptz/preset/goto` | camera-access | Recall a preset; body `{ "preset": <1-255> }` |

### 7.4 RBAC

| Operation | Allowed roles |
|---|---|
| `GET` panel state | Any role with access to the camera |
| `PUT` panel state | `admin`, `soc_supervisor` |
| `POST` reboot | `admin` only |
| `POST` preset goto | Any role with access to the camera |

Camera access itself follows the usual site-scoped rules (`AuthorizedCameraIDs` / `CanAccessCamera`). Reads are operator-accessible because the same data feeds troubleshooting diagnostics.

### 7.5 VCA bidirectional sync

VCA (video content analysis) rules — intrusion zones, line crossings, region entrances, loitering — previously flowed in one direction only: platform DB → camera, via `POST /api/cameras/{id}/vca/sync`. Edits made from the camera's native web UI were invisible to the platform.

The new pull path closes the loop:

- `GET  /api/cameras/{id}/vca/pull` — preview only. Reads `get.vca.intrusion`, `get.vca.alllinecrossing`, `get.vca.regionentrance`, `get.vca.loitering`, normalizes each rule's polygon-slot pixel coordinates (the `"x1:x2:...:-1:-1:"` colon-separated format) into our 0.0–1.0 `[{x,y}]` schema, and returns a diff: `db_only`, `camera_only`, `modified`.
- `POST /api/cameras/{id}/vca/pull?apply=1` — admin-only. Replaces the DB rule set with the camera's state.

In the UI, the `VCAZoneEditor` toolbar now shows **↑ Push to camera** and **↓ Pull from camera** buttons. Pull presents a diff preview; applying requires an explicit `window.confirm` because it drops platform-side edits.

### 7.6 Why pass-through rather than per-panel typed Go structs

Milesight's firmware adds fields between minor revisions. Keeping 14 Go structs in lockstep with `CQ_63.x` was assessed as strictly worse than holding the contract in TypeScript, which only affects the one surface (the settings modal) that consumes it. The Go layer validates JSON well-formedness before forwarding and checks `{"setState":"failed"}` on writes; it does not try to understand field-level semantics.

### 7.7 File reference

| Concern | File |
|---|---|
| CGI client (digest auth) | `internal/onvif/milesight_cgi.go` |
| SD status fallback (same transport) | `internal/onvif/milesight_sd.go` |
| Allowlisted config handlers | `internal/api/milesight_config.go` |
| VCA bidirectional pull | `internal/api/vca_pull.go` |
| Route registration | `internal/api/router.go` |
| TypeScript contracts + helpers | `frontend/src/lib/milesight.ts` |
| Advanced settings UI (8 sub-tabs) | `frontend/src/components/MilesightAdvanced.tsx` |
| VCA editor with Pull button | `frontend/src/components/VCAZoneEditor.tsx` |

---

## 8. Recording & Retention Policy (Site-Scoped)

Before 2026-04 every camera carried its own `retention_days`, `recording_mode`, `pre_buffer_sec`, `post_buffer_sec`, `recording_triggers` and `schedule` columns. In practice these values were almost always identical across cameras on the same site — and when they weren't, the divergence caused a recurring support problem (*"Camera A had 7-day retention, evidence is gone"*). The policy is a compliance surface and wants to live closer to the customer contract.

### 8.1 Where it lives now

The six fields moved to `sites`. Every camera assigned to a site inherits the site's policy at recording start / restart. **There is no per-camera override.**

| Field | Type | Default | Meaning |
|---|---|---|---|
| `retention_days` | `INT` | 30 | Segments older than this are purged on the next hourly retention pass. `0` falls through to the storage-location default. |
| `recording_mode` | `TEXT` | `continuous` | `continuous` = always recording. `event` = ring buffer that only writes clips around triggers. |
| `pre_buffer_sec` | `INT` | 10 | Event mode only — seconds of footage prepended to each triggered clip. |
| `post_buffer_sec` | `INT` | 30 | Event mode only — seconds of footage kept after each trigger ends. |
| `recording_triggers` | `TEXT` | `motion,object` | Comma-separated event tokens that wake the event-mode recorder. |
| `recording_schedule` | `TEXT` | `''` | JSON array of `MonitoringWindow`-shaped entries `[{days, start_time, end_time, enabled, label}]`. Empty string / empty array = always record. Legacy single-object form `{days, start, end}` is still accepted on read and silently migrated to the array form the next time the schedule is saved. |

A companion `recording_backfilled BOOLEAN` flag records whether the migration's one-time backfill has run for that site, so resetting a site to defaults on purpose later does not get clobbered by a re-run.

### 8.2 Migration (2026-04)

The migration lives in [cmd/server/main.go](cmd/server/main.go) alongside the other auto-run migrations.

1. `ALTER TABLE sites ADD COLUMN IF NOT EXISTS …` for the six policy columns and the `recording_backfilled` flag.
2. A one-time backfill: for each site with `recording_backfilled=false`, adopt values from its most-recently-updated camera (`DISTINCT ON (site_id) … ORDER BY site_id, updated_at DESC`). Any site without cameras keeps the defaults.
3. The legacy `cameras.retention_days` / `recording_mode` / `pre_buffer_sec` / `post_buffer_sec` / `recording_triggers` / `schedule` columns are **intentionally left in place** for one release as a rollback cushion. The engine stops reading them; the DB still holds them.

Rollback = code revert only, no schema undo.

### 8.3 Runtime resolution

Recording start / retention both resolve the policy per camera at call time:

- **Engine start** — [internal/recording/site_policy.go](internal/recording/site_policy.go) `SettingsForCamera(ctx, db, cam)`: returns `RecordingSettings` from the camera's site; empty struct ("engine defaults") when the camera has no site or the lookup fails. Used by [internal/api/cameras.go](internal/api/cameras.go) (camera create) and [cmd/server/main.go](cmd/server/main.go) (startup resume loop).
- **Retention purge** — [internal/recording/retention.go](internal/recording/retention.go) `enforceRetentionDays`: per-pass site cache avoids N round-trips when many cameras share a site. Resolution order: site policy → storage-location default → skip (no policy at any level).

Hot-reloading settings onto a live recorder is **not** supported — an admin policy change takes effect when the affected cameras' recorders next restart. Today that happens on the camera's Recording toggle or on server restart.

### 8.4 API

| Route | Method | Role | Notes |
|---|---|---|---|
| `/api/v1/sites/{id}` | GET | any | Returns the full site including the six recording fields. |
| `/api/v1/sites/{id}/recording` | PUT | `admin` / `soc_supervisor` | Validates `recording_mode ∈ {continuous, event}`, clamps negative buffers/retention to 0, writes all six fields atomically, sets `recording_backfilled=true`. |

The dedicated endpoint is deliberately separate from the generic `PUT /sites/{id}` — this action has stricter semantics (fleet-wide effect on next restart) and the UI wants it to surface distinct confirmation / status.

### 8.5 UI

- **Site config modal** ([frontend/src/components/admin/SiteConfigModal.tsx](frontend/src/components/admin/SiteConfigModal.tsx)) — new **Recording & Retention** tab under the Devices section, rendered by [SiteRecordingPanel.tsx](frontend/src/components/admin/SiteRecordingPanel.tsx). Controls: retention days, continuous/event mode selector, pre/post-buffer inputs (event mode only), trigger chip picker, and a multi-window **recording schedule** editor.
- **Shared window editor** ([frontend/src/components/admin/ScheduleWindowsEditor.tsx](frontend/src/components/admin/ScheduleWindowsEditor.tsx)) — one component drives both the Monitoring Schedule (*when the SOC watches*) and the Recording Schedule (*when cameras write to disk*). Takes `windows`, `onChange`, optional `presets` and `accent` colour. Day-picker row + time-range inputs + enable toggle + delete per window, with an "overnight" hint when `start > end`. Both features share the shape so operators learn the editor once.
- **Camera settings modal** ([frontend/src/components/CameraManager.tsx](frontend/src/components/CameraManager.tsx)) — the per-camera **Recording** tab kept its name and position but now holds only operational flags (audio encoding, ONVIF event subscription, Milesight AI readout, privacy-mask flag). A blue notice at the top points admins to the site page for policy changes.

### 8.6 Explicitly out of scope

- Per-camera override (e.g., *"hide this one camera from retention"*). The point of the move is a single compliance surface.
- Hot-reload without recording restart. Acceptable today because policy changes are rare; revisit if a compliance event forces a mid-flight retention bump.
- Automatic recorder restart on policy save. A future enhancement — currently an admin toggles the camera's Recording flag or restarts the server to force re-evaluation.

---

## 9. Implementation Status

### ✅ Implemented

**Frontend**
- All 5 interface shells with route-specific design systems and font stacks
- SOC Monitor (`/operator`) — Active Alarm 3-pane UI fully wired to real API; disposition persists to backend
- SOP management — inline add, edit (per-SOP form), delete
- Admin panel — site CRUD (including edit via `PUT /api/v1/sites/{id}`), camera/user/SOP management
- Customer portal — real incident data from DB (`GET /api/v1/incidents` + `GET /api/v1/incidents/{id}`)
- Semantic search, analytics, site drill-down, incident forensic detail
- `RouteGuard` component enforcing role-based route access in all layout files
- Login → role-based redirect (`soc_operator` → `/operator`, `site_manager` → `/portal`, etc.)

**Backend (Go)**
- JWT auth (`/auth/login`, `/auth/me`, `RequireAuth` middleware)
- Full camera lifecycle: ONVIF discovery, add/edit/delete, PTZ, HLS via MediaMTX, recordings
- AI detection pipeline: ONVIF analytics → `detection.Manager.AlertEmitter` → `active_alarms` INSERT (ON CONFLICT dedup, 1/camera/minute) → WebSocket broadcast on `/ws/alerts`
- Full Ironsight platform CRUD: companies, sites, SOPs, camera assignments, operators
- Security events: `POST /api/v1/events` accepts enriched disposition payload (severity, type, description, disposition_label, operator_callsign, action_log, escalation_depth)
- Incidents: `GET /api/v1/incidents` (with site JOIN), `GET /api/v1/incidents/{id}` (full detail)
- Dispatch queue: `GET /api/v1/dispatch/queue` → live count from `active_alarms` table
- Shift handoffs: `GET/POST /api/v1/handoffs` with DB persistence
- Alarm escalation: `POST /api/v1/alarms/{id}/escalate`
- Audit log middleware on all authenticated routes
- Auto-migrations for: `security_events` enriched columns, `active_alarms` table + index, `shift_handoffs` table + index
- Milesight vendor-config pass-through: 15 panel actions (OSD, image, audio, network, privacy mask, day/night, NTP, auto-reboot, PTZ presets, alarm I/O, SNMP, GB28181 platform, image capabilities, system info), reboot action, PTZ preset recall. Writes admin-gated.
- Milesight VCA bidirectional sync: preview diff + optional DB overwrite from the camera's current rule set, closing the unidirectional push loop.
- Recording & retention policy moved to the site level (2026-04): six per-site columns, auto-backfill migration, fleet-wide write via `PUT /api/v1/sites/{id}/recording`, new Recording & Retention tab on the site config modal, per-camera Recording tab slimmed to operational flags only. See §8.

### 🔜 Remaining

- **Backend role guards:** `RequireRole` middleware for admin-only endpoints (currently any valid JWT can call any route)
- **vLM safety engine:** async PPE compliance scoring, Pending Review Queue API, active learning payload pipeline
- **Evidence shareable links:** `/evidence/[token]` route, expiry management, cloud sync on generation
- **"Smash and Grab" protection:** auto cloud-copy on `Critical - Police Dispatched` disposition
- **"Morning Briefing":** read/unread state on portal overnight events
- **Feature flag gating:** Safety add-on tier behind `customer.features.includes('vlm_safety')`