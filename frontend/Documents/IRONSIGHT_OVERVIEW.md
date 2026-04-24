# Ironsight — Platform Overview

> Real-time construction site intelligence. AI-powered security monitoring via YOLOv8 real-time inference, with optional proactive safety/compliance auditing via asynchronous Vision-Language Models.

**Note:** This application was formerly referred to as "SiteGuard AI" in earlier documentation. All references have been consolidated under the **Ironsight** brand.

---

## What Is This

Ironsight is the unified web frontend for construction site security and safety intelligence. It serves two distinct, parallel workflows:

### Security (Core Product)
Real-time, high-urgency monitoring of off-hours security alarms triggered by AI-verified person/vehicle detection (YOLOv8). SOC operators investigate, validate, and disposition alarms using site-specific SOPs and call trees.

### Safety (Premium Add-On, Feature-Flagged)
Asynchronous PPE compliance auditing and hazard detection powered by a Vision-Language Model (vLM). Safety managers review auto-generated violations, validate or reject them, and conduct natural-language video searches for proactive auditing.

---

## Users & Roles

| Role | Login Lands On | Primary Interface | Purpose |
|------|---------------|------------------|---------|
| **`admin`** | `/` | All routes | Full system access — provision orgs, sites, users, cameras, SOPs, call trees, settings, audit log |
| **`soc_supervisor`** | `/operator` | `/operator`, `/analytics`, `/search`, `/portal`, `/` | Oversee SOC operators, monitor SLA queue, access analytics and full NVR |
| **`soc_operator`** | `/operator` | `/operator` **only** | Claim and investigate alarms, contact call trees, dispatch police, log dispositions |
| **`site_manager`** | `/portal` | `/portal`, `/analytics`, `/` | Review overnight security events, validate vLM safety findings, view compliance dashboards |
| **`customer`** | `/portal` | `/portal`, `/` | Macro-level view across all org sites, compliance trends, report generation |
| **`viewer`** | `/` | `/` | Camera feeds — read only |

### Route Access Matrix

| Route | `admin` | `soc_supervisor` | `soc_operator` | `site_manager` | `customer` | `viewer` |
|-------|:-------:|:----------------:|:--------------:|:--------------:|:----------:|:--------:|
| `/` — NVR / Camera Grid | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ |
| `/operator` — SOC Monitor | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| `/admin` — Admin Panel | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `/portal` — Customer Portal | ✅ | ✅ | ❌ | ✅ | ✅ | ❌ |
| `/search` — Incident Search | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `/analytics` — Analytics | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ |

Unauthorized access redirects to the user's role home page (e.g. `soc_operator` → `/operator`). Unauthenticated access redirects to `/login`.

### Permission Enforcement Layers

| Layer | Mechanism |
|-------|-----------|
| Post-login redirect | `login/page.tsx` routes by role immediately after authentication |
| Route guard (layout) | `RouteGuard` component in `/operator`, `/portal`, `/search`, `/admin`, `/analytics` layouts — redirects before rendering |
| Root page redirect | `page.tsx` bounces `soc_operator` to `/operator` on mount |
| Permission matrix | `ROUTE_PERMISSIONS` in `AuthContext.tsx` — single source of truth for all role checks |
| API authentication | `RequireAuth` JWT middleware on all `/api/*` routes — valid token required for every request |

> **Note:** Backend API routes currently enforce authentication but not per-endpoint role checks. A `RequireRole` middleware should be added to the Go router if stricter endpoint-level isolation is required (e.g. blocking `soc_operator` from `PUT /api/v1/sites`).

### RBAC & Multi-Tenant Architecture

```
Organization (Customer)
  └── Site (job site, with its own SOP + Call Tree)
        ├── Cameras (assigned from IRONSight NVR)
        └── Users (Site Managers/Foremen assigned to specific sites)
```

- **Site SOPs and Call Trees are tied to the Site, not the Organization**
- Users are explicitly assigned to specific Sites via an ACL join table
- React Query hooks filter all data based on the user's token and site assignments
- Executives (`customer` role) bypass ACL filtering and see all sites under their organization

---

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| Framework | Next.js (App Router) | 14.2 |
| Language | TypeScript | 5.3 |
| UI | React | 18.2 |
| State (client) | Zustand (with persist middleware) | 5.0 |
| State (server) | TanStack React Query | 5.99 |
| Video | hls.js (local NVR proxy streaming) | 1.5 |
| Dates | date-fns | 4.1 |
| Styling | CSS custom properties + inline styles | — |

---

## The SOC Operator Workflow (Security)

SOC Operators handle high-stakes, off-hours security alarms. The workflow uses a **Directed Dispatch Model** (similar to a 911 dispatch center) to minimize cognitive load and alarm fatigue.

### Operator State Machine

| State | Description | Receives New Alarms? |
|-------|-------------|---------------------|
| **Available** | Monitoring idle grid, ready for dispatch | Yes (longest-idle gets priority) |
| **Engaged** | Locked into active alarm investigation | No |
| **Post-Event Wrap-Up** | Filling out disposition/notes | No |
| **Away/Break** | Temporarily out of routing pool | No |

### Alarm Routing Logic (Backend)

1. AI verifies person/vehicle detection (YOLOv8)
2. Backend checks pool of **Available** operators
3. Assigns alarm to the **longest-idle** Available operator (round-robin)
4. Alert pushed to that specific operator's WebSocket channel
5. If **zero operators available** → alarm enters a **FIFO priority queue**
6. When any operator finishes and hits "Submit" → instantly receives oldest queued alarm

### SLA Tracking (Passive — No Auto-Escalation)

SLA timers are **strictly passive performance metrics**. No auto-escalation, no auto-rerouting.

- **Time-in-Queue**: How long an alarm sat waiting for an operator
- **Time-to-Resolution**: How long the operator spent investigating
- Visual urgency: timer badge changes color at 30s (yellow), 90s (orange), 180s (red)
- Supervisors monitor the queue and manually intervene if thresholds bleed into red
- All timestamps feed `/analytics` dashboards for performance reviews

### The Active Alarm UI (3-Pane Layout)

When an operator claims an alarm, the 2x3 camera grid is **replaced** by a focused investigation view:

#### Pane 1 — Video Investigation (Center/Left, largest area)
- **Primary viewer**: Loops the AI event clip with a toggle to switch to **Live View** of the triggered camera
- **Adjacent feeds**: 2-4 camera feeds from physically nearby cameras, auto-populated from the site map
- **Mini site map**: Shows triggered camera location and coverage zones

#### Pane 2 — Site Intelligence (Top Right)
- **Site notes**: High-priority persistent notes (e.g., "Guard dog on premises", "Gate 3 stuck open")
- **Dynamic SOP**: Interactive checklist of response steps, pulled by Site ID
- **Call tree**: Escalation contacts in explicit order, with one-click phone number copy
- **Quick-log buttons** (Desk Phone V1):
  - `[ Spoke to Contact ]`
  - `[ Left Voicemail ]`
  - `[ No Answer ]`
  - Each auto-populates the action log with a timestamped entry
- *Space reserved for future VOIP dialer integration*

#### Pane 3 — Action & Disposition (Bottom Right)
- **Action log**: Timestamped text entries of all actions taken
- **Disposition selector**: Dropdown to categorize outcome:
  - `False Positive - Animal`
  - `False Positive - Weather`
  - `Verified - Customer Notified`
  - `Verified - Police Dispatched`
  - `Verified - Guard Responded`
- **Submit button**: Resolves the alarm, unlocks the operator, creates a **Security Event** in the customer portal

### Data Architecture: Alarm → Security Event

When the operator submits:
1. Backend merges the raw alert data (clip, timestamp, AI bounding boxes) with the operator's disposition and notes
2. Creates a **Security Event** with structured fields:
   - `disposition_code` (separate from raw `operator_notes`)
   - `escalation_depth` (which level of the call tree was reached)
   - `external_call_id` (nullable — future VOIP placeholder)
   - `call_recording_url` (nullable — future VOIP placeholder)
3. Security Event immediately populates the Customer Portal

---

## The Customer Portal Workflow (Passive Discovery)

Site Managers discover overnight security events and vLM safety findings when they log in the next morning. **No active push notifications in V1** — purely passive.

### The "Morning Briefing" Dashboard (`/portal`)

- **Command View**: Map and/or grid of assigned sites with KPIs (PPE score, open incidents, camera fleet status)
- **"Critical Overnight Events" banner**: Prioritizes SOC Security Events above routine safety metrics
- **Visual hierarchy**: High-severity Security Events use red `Critical` pills; routine safety uses yellow `Warning`
- **Read/Unread state**: `viewed_by_customer` boolean — unread events show bold with red left-border accent

### Site Drill-Down (`/portal/sites/[id]`)

**Per the architecture spec, the site drill-down reuses the battle-tested IRONSight NVR interface** (`src/app/page.tsx`), augmented with AI data:

- **Core video**: Standard NVR 21:8 panoramic viewer and multi-grid timeline
- **KPI injection**: Collapsible side-panel overlays PPE breakdown bars, worker counts, risk banners
- **AI overlay**: Bounding boxes rendered over the NVR video canvas
- **Navigation**: Prominent `[ <- Back to Site Selector ]` button returns to macro view

### Incident Types

| Category | Source | Flow |
|----------|--------|------|
| **Security Event** | SOC operator disposition | Auto-published to portal after operator submits |
| **Safety Incident (Auto)** | vLM auto-detection | Pushed to Pending Review Queue for True/False validation |
| **Safety Incident (Manual)** | Manager creates from `/search` | Published directly to portal as verified |

---

## Evidence Handling & Export

Video clips are **bookmarked on the local job site NVR**. Ironsight operates a proxy streaming architecture.

### Playback

- Frontend's HLS player requests clips via the backend proxy
- Backend queries the local NVR using the bookmark pointer
- NVR streams the chunk back through the proxy to the frontend

### Offline Handling

If the NVR is offline (power outage, severed internet):
- Video panel shows: *"Site connection lost. Evidence safely bookmarked on local NVR and will be viewable when connection is restored."*
- SOC operator notes and AI snapshots remain visible for context

### Export Options

The `EvidenceExportButton` presents a split-button/dropdown:

| Option | Behavior |
|--------|----------|
| **Download MP4** | Backend fetches the time-bounded clip from the NVR, streams it as an `.mp4` attachment. Loading state: "Packaging Evidence..." Disabled if NVR offline. |
| **Generate Secure Link** | Creates a cryptographic token, generates a public URL (`/evidence/[token]`). **Triggers cloud sync** — copies the clip from the NVR to cloud storage to ensure availability. |

### Shareable Links

- **User-defined expiry**: 1 Hour, 1 Day, 1 Week, 1 Month, Never
- **Link management**: Modal shows active link, expiration date, and "Revoke Access" button
- **Expired state**: Branded page: *"This evidence link has expired. Please contact the Site Manager to request a new access link."*
- **Database schema**: `EvidenceShares` table with `token`, `incident_id`, `created_by`, `expires_at` (NULL for "Never")

### The "Smash and Grab" Protection (V2)

If operator selects `Critical - Police Dispatched`, the system auto-copies the bookmarked clip to cloud storage as an immutable backup, protecting against NVR theft/destruction.

---

## vLM Safety Engine (Premium Add-On)

Safety features are **decoupled from Security**, powered by an asynchronous Vision-Language Model, and implemented as a **feature-flagged add-on tier**.

### Feature Gating

- **Security-Only customers**: Portal hides PPE charts, worker counts, search. Safety panels show locked upsell state.
- **Safety-enabled customers**: Full access to compliance dashboards, Pending Review Queue, Semantic Search.
- **Component-level toggle**: `customer.features.includes('vlm_safety')` guards data fetching and rendering.

### Background Indexing

The vLM processes video frames periodically (~every 5 seconds), generating:
- Text captions and metadata
- PPE status per detected worker
- Hazard recognition flags

This metadata populates the Semantic Search database and auto-generates safety findings.

### The Pending Review Queue

When safety features are enabled for a site, vLM findings flow automatically to the customer portal.

**Rapid Validation Interface:**
- Thumbnail + AI bounding box + caption
- Two buttons: `[ Valid ]` and `[ False ]`
- Clicking Valid → moves to official incident timeline, updates compliance metrics
- Clicking False → dismisses from dashboard, triggers active learning payload
- Deep-dive option: click thumbnail to open full incident dossier

**Dashboard Filtering:**
- PPE compliance charts and Site Health tables **only calculate from `validation_status = true`**
- `Pending` events shown separately in the review queue, not in official metrics
- `False` events scrubbed from active views but retained in database for auditing

### Semantic Search (`/search`)

The proactive, natural-language auditing tool for Safety Managers:

- Searches indexed vLM metadata (not real-time detection)
- Left refinement panel: violation types, site selector, date range, confidence slider, time of day, model selector
- "Create Incident" from search results → opens in full portal dossier
- Complements the auto-push queue: queue handles known violations, search handles unknown/specific audits

### Active Learning Flywheel (Global AI Training)

Every `[ False ]` click feeds a training pipeline:

1. **The Micro-Interaction**: On False, a toast appears: *"Help us improve: What did the AI actually see?"* with quick-select options (Animal, Equipment, Shadow, Other)
2. **The Payload**: `incident_id` + bounding box coordinates + original vLM caption + human correction
3. **Anonymization**: Backend strips `customer_id` and `site_id` before forwarding. The "Golden Dataset" is purely: `[Image Frame]` + `[Original Caption]` + `[Human Correction]`
4. **AI Telemetry Queue**: Fires to a separate data pipeline (SQS/Kafka), not the production database
5. **Training Data Lake**: Anonymized pairs land in a dedicated bucket for ML fine-tuning
6. **Global Model**: One massive model trained on all tenants' corrections — creates a data network effect (161st site is smart on Day 1)
7. **Admin Opt-Out**: Organizations can disable data sharing via `/admin` panel toggle ("Data Sharing / Global AI Training")

---

## Architecture

```
src/
 app/                          # Next.js App Router pages
   operator/                   # SOC operator console (dark industrial theme)
   portal/                     # Customer portal (light editorial theme)
     sites/[id]/               # Site drill-down (NVR-augmented interface)
     incidents/[id]/           # Incident/event forensic detail
   search/                     # Semantic video search (cinema dark theme)
   admin/                      # Administration panel
   analytics/                  # System-wide analytics & SLA tracking
   evidence/[token]/           # Public evidence viewer (shareable links)
   login/                      # Authentication
   popout/[cameraId]/          # Pop-out camera window (multi-monitor)
 components/
   operator/                   # SOC console components (Active Alarm UI)
   admin/                      # Admin CRUD modals & panels
   shared/                     # Cross-route components
 hooks/                        # React Query hooks, WebSocket, utilities
 stores/                       # Zustand stores (operator, portal, search, admin)
 lib/                          # API clients, WebSocket, formatters, fonts
 contexts/                     # Auth context + RBAC
 types/                        # TypeScript domain types
```

---

## Real-Time Architecture

| Channel | Protocol | Purpose |
|---------|----------|---------|
| Operator dispatch | WebSocket (targeted per-operator) | Directed alarm delivery to specific operator |
| Operator presence | WebSocket (global) | Track operator status (Available/Engaged/Away) for dispatch routing |
| Camera feeds | HLS (proxied through backend from local NVR) | Live video streaming with adaptive bitrate |
| Site/incident data | REST polling (React Query) | Auto-refetch with stale-time caching |
| vLM findings | REST polling or server-push | Safety violations pushed to portal review queue |

---

## Running Locally

```bash
npm install
npm run dev
# → http://localhost:3000
```

The app renders fully with mock data — no backend required. API calls fail silently and fall back to built-in mock responses.

| URL | What you'll see |
|-----|----------------|
| `localhost:3000/operator` | SOC operator console |
| `localhost:3000/portal` | Customer safety dashboard |
| `localhost:3000/portal/sites/TX-203` | Site drill-down |
| `localhost:3000/portal/incidents/INC-2026-0847` | Incident forensic detail |
| `localhost:3000/search` | Semantic video search |
| `localhost:3000/admin` | Administration panel |
| `localhost:3000/analytics` | System analytics |

Backend proxy configured in `next.config.js` to forward `/api/*`, `/ws`, `/hls/*` to `localhost:8080`.

---

## What's Built vs. What's Next

### Built — Frontend
- All 5 interface shells with route-specific design systems and font stacks
- IRONSight NVR integration (camera grid, HLS playback, timeline, events)
- **SOC Operator Active Alarm UI** — fully built and wired to real API
  - 3-pane layout: video investigation + site intelligence + action/disposition
  - Real `useSite` + `useSiteCameras` data (no mocks)
  - Disposition persists to backend via `POST /api/v1/events`
  - SOP inline edit, call tree quick-log buttons, action log
- Customer portal with compliance dashboards, date range picker, CSV export, real incident data
- Site drill-down with panoramic viewer, AI overlays, incident table
- Incident detail with forensic dossier pulling real DB data
- Semantic search with left refinement panel and saved searches
- Admin panel with full CRUD for sites (including edit), cameras, users, SOPs (add/edit/delete), maps, notification rules, integrations
- **Auth/RBAC with 6 roles and `RouteGuard` enforcement** in every layout
- i18n (EN/ES/FR), accessibility (WCAG skip-to-content, aria-labels, focus traps)
- HLS video player, error boundaries, Zustand persistence, session management

### Built — Backend (Go)
- JWT auth with `RequireAuth` middleware on all `/api/*` routes
- Full ONVIF camera lifecycle (discovery, add, PTZ, HLS via MediaMTX)
- Recording engine (MP4 segments) + HLS VOD playback
- AI detection pipeline: ONVIF analytics → `detection.Manager` → `AlertEmitter` → `active_alarms` table → WebSocket broadcast → deduplication (1 alarm/camera/minute)
- All Ironsight platform routes: companies, sites, SOPs, cameras, incidents, security events, operators, handoffs, dispatch queue
- Alarm escalation endpoint
- Auto-migrations run on startup (`main.go`)
- Audit log middleware on all authenticated routes

### Remaining
- Backend per-endpoint role enforcement (`RequireRole` middleware for admin-only routes)
- vLM safety engine, PPE compliance scoring, Pending Review Queue
- Secure evidence shareable links (`/evidence/[token]`) with expiry and cloud sync
- Active learning pipeline (False-click correction capture → anonymized training data)
- "Morning Briefing" unread state on portal
- Feature flag gating for Safety add-on tier
