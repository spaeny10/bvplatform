# Ironsight — Platform Overview

> **Note:** This document was originally written under the "SiteGuard AI" brand. The project is now officially named **Ironsight**. All references below have been updated accordingly.

> Real-time construction site intelligence. AI-powered security monitoring via YOLOv8, with optional PPE compliance auditing via Vision-Language Models. 800+ cameras across 160+ job sites.

---

## What Is This

Ironsight is a full-stack construction site security and safety intelligence platform. It serves three audiences:

- **SOC Operators** claim and investigate real-time security alarms, follow site SOPs and call trees, disposition events, and coordinate shift handoffs
- **Safety Managers & Executives** (customers) view compliance dashboards, review overnight security events, validate vLM safety findings, and investigate incidents
- **Administrators** manage organisations, sites, cameras, users, SOPs, call trees, notification rules, and integrations

The platform has a **Go backend** (chi router, PostgreSQL) handling all API, WebSocket, recording, and AI detection logic, with **Next.js 14** on the frontend.

---

## Tech Stack

### Frontend

| Layer | Technology | Version |
|-------|-----------|---------|
| Framework | Next.js (App Router) | 14.2 |
| Language | TypeScript | 5.3 |
| UI | React | 18.2 |
| State (client) | Zustand (with persist middleware) | 5.0 |
| State (server) | TanStack React Query | 5.99 |
| Video | hls.js | 1.5 |
| Dates | date-fns | 4.1 |
| Styling | CSS custom properties + inline styles (no Tailwind) | — |

### Backend

| Layer | Technology |
|-------|-----------|
| Language | Go |
| HTTP Router | chi v5 |
| Database | PostgreSQL (pgx v5) |
| Auth | JWT (HS256) — `RequireAuth` middleware on all `/api/*` |
| Video | MediaMTX (RTSP → HLS/WebRTC), ONVIF |
| Recording | Internal recording engine (MP4 segments) |
| AI Detection | ONVIF Profile M analytics → `detection.Manager` → WebSocket |
| WebSocket | Single hub at `/ws` and `/ws/alerts` |

---

## Architecture

```
src/
 app/                          # Next.js App Router pages
   operator/                   # SOC operator console (dark theme)
   portal/                     # Customer safety dashboard (light theme)
     sites/[id]/               # Site drill-down (editorial theme)
     incidents/[id]/           # Incident detail (forensic dossier theme)
   search/                     # Semantic video search (cinema dark theme)
   admin/                      # Administration panel
   analytics/                  # System-wide analytics
   login/                      # Authentication
   popout/[cameraId]/          # Pop-out camera window
 components/
   operator/                   # 15 operator-specific components
   admin/                      # 11 admin components
   shared/                     # 9 cross-route components
 hooks/                        # 20 React hooks
 stores/                       # 4 Zustand stores
 lib/                          # API clients, WebSocket, formatters, fonts
 contexts/                     # Auth context + RBAC
 types/                        # TypeScript domain types
```

---

## The Five Interfaces

Each interface has its own design language, font stack, and color system. Fonts are loaded via `next/font/google` and scoped per route through layout files.

### 1. Operator Console (`/operator`)

**Aesthetic:** Dark industrial command center. Cyan/orange signals cutting through near-black.

**Fonts:** Rajdhani (headlines) + JetBrains Mono (data) + Barlow Condensed (compact UI)

**Key capabilities:**
- 2x3 camera grid with AI detection bounding boxes (person=cyan, violation=red, equipment=yellow)
- HLS video streaming with fallback to mock scene rendering
- Real-time WebSocket alert feed with severity filtering and SLA countdown timers
- Site locking — operators claim exclusive monitoring responsibility
- Alert claim/release workflow with escalation engine (auto-escalates at 30s/90s/180s thresholds)
- Drag-and-drop camera layout with pin and column controls
- Shift handoff modal with pending acknowledgment badges
- Multi-site split view for side-by-side comparison
- AI insights panel, operator roster, PPE compliance breakdown
- Event correlation timeline with visual time axis
- Fleet status bar: online cameras, degraded count, critical alerts, SLA breaches

**Keyboard shortcuts:** `?` help, `T` timeline, `M` multi-site, `I` insights, `B` sidebar, `H` handoff, `Escape` close

### 2. Customer Portal (`/portal`)

**Aesthetic:** Warm premium light theme. Serif editorial headings with clean sans-serif body.

**Fonts:** Playfair Display (display) + DM Sans (body) + DM Mono (data)

**Key capabilities:**
- Summary cards: PPE compliance %, open incidents, zone violations, workers on site
- 7-day PPE compliance trend chart (grouped bar by category)
- Site health table with compliance scores, trend arrows, sparklines
- Recent incidents list linked to detail pages
- Interactive date range picker (persisted in store)
- CSV export for sites and incidents
- Theme toggle (light/dark)
- Quick report download buttons

### 3. Site Drill-Down (`/portal/sites/[id]`)

**Aesthetic:** Editorial/journalistic. Dense information beautifully typeset.

**Fonts:** Libre Baskerville (headings) + IBM Plex Sans (body) + IBM Plex Mono (data)

**Key capabilities:**
- Dark hero header with site name, address, KPI chips (PPE score, incidents, violations, workers)
- Elevated risk banner for active compliance issues
- Camera strip with 6 selectable thumbnails (red pulse border on alerts)
- 21:8 panoramic viewer with AI bounding boxes, REC/AI ACTIVE badges
- Video timeline scrubber with color-coded incident markers
- Incident table: time, severity, type, camera, status
- Site plan with building shapes and camera markers
- PPE breakdown bars per category
- Worker count display

### 4. Incident Detail (`/portal/incidents/[id]`)

**Aesthetic:** Forensic dossier meets modern SaaS. Red left-border accent.

**Fonts:** Syne (headers) + Epilogue (body) + Fira Code (data)

**Key capabilities:**
- Dark chrome topbar with breadcrumb navigation
- Incident header: ID badge, severity pill, status pill, meta chips, escalate/resolve actions
- 16:7 video evidence panel with detection bounding boxes and "Evidence Locked" badge
- AI analysis panel: VLM caption, confidence indicator, 2x2 findings grid
- Detection objects table: class, confidence pill, bbox coordinates, zone status, violation flag
- Right sidebar: OSHA classification, worker PPE breakdown, event timeline, notification log, comments

### 5. Semantic Search (`/search`)

**Aesthetic:** Search engine meets cinema. Near-black backgrounds with indigo-blue accent.

**Fonts:** Outfit (UI) + Space Mono (data)

**Key capabilities:**
- Natural language search input with focus glow and keyboard shortcut hint
- Left refinement panel: violation type checkboxes, site selector, date range picker, confidence slider, time-of-day range, model selector (Hybrid/Visual/Caption), reset button
- Saved searches: save, load, delete, shared/private toggle
- 3-column result grid with thumbnails, detection box overlays, relevance badges
- Preview pane: similarity score bar, caption, token match breakdown, site/camera metadata
- Create Incident action from search results

---

## State Management

### Zustand Stores (client state, persisted to localStorage)

| Store | Purpose | Persisted Fields |
|-------|---------|-----------------|
| `operator-store` | Operator identity, site selection, locks, alert feed, claim/release | operator, site, locks, layout mode |
| `portal-store` | Customer/org selection, site drill-down, date range | customer, site, date range |
| `search-store` | Query, filters, results, selection | — (not persisted) |
| `admin-store` | Modal visibility flags, editing context | — (not persisted) |

### React Query (server state, auto-refetching)

| Hook | Endpoint | Polling Interval |
|------|----------|-----------------|
| `useSites()` | GET /sites | 30s |
| `useSite(id)` | GET /sites/:id | 10s |
| `useAlerts()` | GET /alerts | 15s |
| `useIncidents()` | GET /incidents | 30s |
| `useSearch()` | POST /search | On-demand (mutation) |
| `useAlertStream()` | WS /ws/alerts | Real-time |

---

## Real-Time Capabilities

| Channel | Protocol | Purpose |
|---------|----------|---------|
| Alert stream | WebSocket | Live alert delivery to operator console with auto-reconnect (3s-30s exponential backoff) |
| Camera feeds | HLS | Live video streaming via hls.js with adaptive bitrate |
| Site/incident data | REST polling | React Query auto-refetch with stale-time caching |
| Escalation engine | Client-side timers | Auto-escalates unacknowledged alerts: L1 at 30s, L2 at 90s, L3 at 180s |

---

## Authentication & RBAC

Six roles enforced by a `RouteGuard` component in every route layout. Unauthorized access redirects to the user's role home page.

| Role | Login Lands On | `/` | `/operator` | `/portal` | `/search` | `/admin` | `/analytics` |
|------|---------------|:---:|:-----------:|:---------:|:---------:|:--------:|:------------:|
| `admin` | `/` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `soc_supervisor` | `/operator` | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ |
| `soc_operator` | `/operator` | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `site_manager` | `/portal` | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ |
| `customer` | `/portal` | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ |
| `viewer` | `/` | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |

**Enforcement layers:**
1. Post-login redirect in `login/page.tsx`
2. `RouteGuard` client component in each route's `layout.tsx`
3. Root page redirect — `soc_operator` bounced to `/operator` on mount
4. `ROUTE_PERMISSIONS` map in `AuthContext.tsx` — single source of truth
5. `RequireAuth` JWT middleware on all Go backend `/api/*` routes

**Default credentials (development):** admin / admin · SOC: jhayes / demo123 · Portal: marcus.webb / demo123

---

## API Layer

Two API clients with automatic mock fallbacks when the backend is unavailable:

| Client | Base URL | Purpose |
|--------|----------|---------|
| `lib/api.ts` | `/api` | NVR core: cameras, events, recordings, speakers, storage |
| `lib/ironsight-api.ts` | `/api/v1` | Ironsight platform: sites, incidents, alarms, operators, handoffs, SOPs, companies |

All fetch calls include Bearer token injection and 401 redirect to `/login`. Every endpoint has a try/catch with mock data fallback for offline development.

Backend proxy configured in `next.config.js`:
- `/api/*` → `http://localhost:8080/api/*`
- `/auth/*` → `http://localhost:8080/auth/*`
- `/ws` → `http://localhost:8080/ws`
- `/hls/*` → `http://localhost:8080/hls/*`
- `/webrtc/*` → `http://localhost:8080/webrtc/*`

### Live Backend API Endpoints (Go — all implemented)

```
POST /auth/login                           → { token, user }
GET  /auth/me                              → UserPublic

GET/POST         /api/v1/companies
PUT/DELETE       /api/v1/companies/{id}
GET/POST         /api/v1/sites
GET/PUT/DELETE   /api/v1/sites/{id}
GET              /api/v1/sites/{id}/cameras
GET/POST         /api/v1/sites/{id}/sops
PUT/DELETE       /api/v1/sops/{id}

GET/POST         /api/v1/incidents
GET              /api/v1/incidents/{id}
POST             /api/v1/events             → security event (operator disposition)
POST             /api/v1/alarms/{id}/escalate

GET              /api/v1/operators
GET              /api/v1/operators/current
GET              /api/v1/dispatch/queue     → { count: N }  (from active_alarms table)
GET/POST         /api/v1/handoffs

WS               /ws                        → camera events, detection results
WS               /ws/alerts                 → alert stream (type:"alert" messages)
```

---

## Accessibility & i18n

- **WCAG 2.4.1:** Skip-to-content link in root layout
- **WCAG 2.4.3:** Focus trap utility for modals
- **WCAG 4.1.3:** Screen reader announcer for dynamic content
- **ARIA:** Labels on navigation, site list, camera grid, buttons
- **Keyboard:** Full keyboard navigation in operator console
- **i18n:** English, Spanish, French — language switcher in operator topbar
- **Reduced motion:** Detection hook available (`useReducedMotion`)
- **High contrast:** Detection hook available (`useHighContrastMode`)

---

## Running Locally

```bash
# Install dependencies
npm install

# Start development server
npm run dev
# → http://localhost:3000

# The app renders fully with mock data — no backend required.
# API calls fail silently and fall back to built-in mock responses.
```

### Routes to test

| URL | What you'll see |
|-----|----------------|
| `localhost:3000/operator` | Operator console with live alert feed |
| `localhost:3000/portal` | Customer safety dashboard |
| `localhost:3000/portal/sites/TX-203` | Site drill-down with camera viewer |
| `localhost:3000/portal/incidents/INC-2026-0847` | Incident forensic detail |
| `localhost:3000/search` | Semantic video search |
| `localhost:3000/admin` | Administration panel |
| `localhost:3000/analytics` | System analytics |

### Connecting a real backend

Start a backend server on `localhost:8080` implementing the API contract in `SITEGUARD_KICKOFF.md`. The frontend will automatically route API calls through the Next.js proxy and stop falling back to mocks.

---

## Project Status

### Frontend — Complete
All 5 interface shells built and wired to real backend data. Active Alarm 3-pane UI fully functional.

### Backend — Substantially Complete
Go backend live at `localhost:8080`. All core Ironsight platform routes implemented and database-backed.

### What's Working End-to-End
- ✅ JWT auth (login → token → all API calls)
- ✅ Active Alarm workflow: AI detection → `active_alarms` table → WebSocket broadcast → operator claims → disposition → `security_events` table
- ✅ Site CRUD including edit (PUT /api/v1/sites/{id})
- ✅ SOP CRUD: create, read, update (inline edit UI), delete
- ✅ Portal incidents list and detail from real DB
- ✅ Dispatch queue count from live `active_alarms` table
- ✅ Operator shift handoffs persisted to DB
- ✅ Role-based route enforcement (RouteGuard in all layouts)
- ✅ ONVIF camera discovery, PTZ, HLS streaming, recordings
- ✅ Detection pipeline: ONVIF analytics → AlertEmitter → alarm deduplication (1/camera/minute)

### Remaining Work
- Backend per-endpoint role enforcement (`RequireRole` middleware for sensitive routes)
- vLM safety engine and Pending Review Queue
- Evidence secure shareable links (`/evidence/[token]`)
- Cloud sync on evidence export
- Active learning micro-interaction pipeline

---

## File Inventory

### Pages (11 routes)
| File | Route |
|------|-------|
| `src/app/page.tsx` | `/` — IRONSight NVR |
| `src/app/login/page.tsx` | `/login` |
| `src/app/operator/page.tsx` | `/operator` |
| `src/app/admin/page.tsx` | `/admin` |
| `src/app/analytics/page.tsx` | `/analytics` |
| `src/app/portal/page.tsx` | `/portal` |
| `src/app/portal/sites/[id]/page.tsx` | `/portal/sites/:id` |
| `src/app/portal/incidents/[id]/page.tsx` | `/portal/incidents/:id` |
| `src/app/search/page.tsx` | `/search` |
| `src/app/popout/[cameraId]/page.tsx` | `/popout/:cameraId` |

### Components (35 total)
| Directory | Count | Components |
|-----------|-------|-----------|
| `shared/` | 9 | ErrorBoundary, RoleSwitcher, SessionWarningWrapper, SeverityPill, DetectionOverlay, PPEStatusIcon, HLSVideoPlayer, EvidenceExportButton, CreateIncidentModal |
| `operator/` | 15 | AlertFeed, AlertDetailSlideout, AlertToastSystem, OperatorCameraGrid, CameraFullscreenModal, FleetStatusBar, OperatorRoster, PPECompliancePanel, ShiftHandoffModal, ShortcutOverlay, MultiSiteSplitView, IncidentTimeline, AIInsightsPanel, PTZControls, SLATimer |
| `admin/` | 11 | CreateSiteModal, AssignCameraModal, CustomerAccessModal, SiteSOPModal, SiteMapModal, NotificationRulesModal, AuditLogPanel, AuditLogExport, OperatorAnalyticsPanel, ReportSchedulerPanel, IntegrationHub |

### Hooks (20)
useAlerts, useAlertStream, useSites, useSite, useSiteCameras, useSiteCompliance, useIncidents, useIncident, useSearch, useCustomers, useCameraLayout, useEscalationEngine, useKeyboardShortcuts, useSessionManager, usePushNotifications, useTheme, useI18n, useAccessibility, useAuth (context), useCameraAssignment

### Stores (4)
operator-store, portal-store, search-store, admin-store

### Lib (7)
api.ts, siteguard-api.ts, siteguard-mock.ts, ws-alerts.ts, fonts.ts, format.ts, query-provider.tsx
