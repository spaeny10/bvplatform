# Ironsight — Claude Code Development Kickoff

> **This document is the original project kickoff brief, preserved for design system reference (typography, color tokens, component specs).** The project has been renamed from "SiteGuard AI" to **Ironsight** and the backend has been rebuilt in Go (not FastAPI/Python). For current architecture, API contracts, and project status, refer to `Ironsight_Architecture.md` and `IRONSIGHT_OVERVIEW.md`.

> Paste this entire file into Claude Code at session start for design system context. Refer to the other docs for current implementation status.

---

## Project Brief

**Ironsight** (formerly SiteGuard AI) is a real-time construction site intelligence platform. It serves:

- **800 panoramic cameras** across **160 active job sites**
- **6 user roles:** admin, soc_supervisor, soc_operator, site_manager, customer, viewer
- **5 core workflows:** live monitoring, SOC alarm dispatch, customer safety portal, incident investigation, semantic video search

The stack is **Next.js 14.2 (App Router) + TypeScript** on the frontend, **Go + chi + PostgreSQL** on the backend. AI inference is handled via ONVIF Profile M analytics events piped through `detection.Manager`. **Tailwind CSS was not used** — styling is CSS custom properties + inline styles scoped per route.

---

## Repository Structure

```
BV-Platform/
├── frontend/                     # Next.js 14.2 App Router
│   ├── src/
│   │   ├── app/
│   │   │   ├── page.tsx          # / — IRONSight NVR (camera grid, timeline)
│   │   │   ├── operator/         # /operator — SOC Monitor (dark industrial)
│   │   │   ├── portal/           # /portal — Customer portal (light editorial)
│   │   │   │   ├── sites/[id]/   # /portal/sites/:id — Site drill-down
│   │   │   │   └── incidents/[id]/ # /portal/incidents/:id — Incident detail
│   │   │   ├── search/           # /search — Semantic video search
│   │   │   ├── admin/            # /admin — Admin panel
│   │   │   ├── analytics/        # /analytics — System analytics
│   │   │   └── login/            # /login
│   │   ├── components/
│   │   │   ├── operator/         # Active Alarm UI, AlertFeed, FleetStatusBar...
│   │   │   ├── admin/            # Site/SOP/camera CRUD modals
│   │   │   └── shared/           # RouteGuard, SeverityPill, HLSVideoPlayer...
│   │   ├── contexts/             # AuthContext (JWT, RBAC, role helpers)
│   │   ├── hooks/                # React Query hooks + WebSocket
│   │   ├── stores/               # Zustand: operator, portal, search, admin
│   │   ├── lib/                  # api.ts (NVR), ironsight-api.ts (platform)
│   │   └── types/                # TypeScript domain types (ironsight.ts)
│
├── internal/                     # Go backend packages
│   ├── api/                      # chi HTTP handlers + router
│   │   ├── router.go             # All route definitions
│   │   ├── auth_handler.go       # /auth/login, /auth/me, RequireAuth middleware
│   │   ├── platform.go           # Ironsight platform handlers (sites, SOPs, incidents...)
│   │   ├── cameras.go            # Camera CRUD + PTZ + detection
│   │   └── audit.go              # Audit log middleware
│   ├── database/                 # PostgreSQL (pgx)
│   │   ├── db.go                 # Connection + NVR queries
│   │   ├── platform_db.go        # Ironsight platform DB functions
│   │   └── platform_models.go    # Go structs for all platform entities
│   ├── detection/                # AI detection manager (AlertEmitter)
│   ├── recording/                # Recording engine (MP4 segments)
│   ├── streaming/                # MediaMTX + HLS server
│   ├── onvif/                    # ONVIF discovery, PTZ, backchannel
│   └── auth/                     # JWT sign/parse/verify
│
└── cmd/server/main.go            # Entry point — migrations + server start
```

---



---

### Interface 1 — Operator Console (`/operator`)

**Aesthetic:** Dark industrial command center. Military precision meets live camera room. Everything is functional-first with cyan/orange status signals cutting through near-black backgrounds.

**Typography:**
- Headlines/labels: `Rajdhani` (weight 500–700) — compact, technical
- Monospace data: `JetBrains Mono` (weight 300–500) — timestamps, IDs, metrics
- Condensed UI: `Barlow Condensed` (weight 400–600) — camera labels, nav items

**CSS Design Tokens:**
```css
:root {
  --bg-base: #080c10;
  --bg-panel: #0c1118;
  --bg-card: #101820;
  --bg-elevated: #141e28;
  --bg-highlight: #1a2535;
  --border: rgba(255,255,255,0.06);
  --border-bright: rgba(255,255,255,0.12);
  --accent-primary: #00d4ff;
  --accent-secondary: #ff6b35;
  --accent-green: #00e5a0;
  --accent-yellow: #ffcc00;
  --accent-red: #ff3355;
  --accent-purple: #a855f7;
  --text-primary: #e8f0f8;
  --text-secondary: #8fa8c0;
  --text-dim: #4a6070;
  --glow-blue: 0 0 20px rgba(0,212,255,0.3);
  --glow-red: 0 0 20px rgba(255,51,85,0.4);
  --glow-green: 0 0 20px rgba(0,229,160,0.3);
}
```

**Key Components:**
- Fleet status bar: `762 ONLINE · 38 DEGRADED · 7 CRITICAL` with colored badges and live clock
- 2×3 camera grid with AI overlay bounding boxes (person=cyan, PPE violation=red, equipment=yellow)
- Alert feed: severity pill + site + description + timestamp, newest at top, auto-scroll
- PPE compliance bars: per-category (hard hat, harness, hi-vis, boots) with percentage + color
- Site list: compact rows with status dot, site ID, camera count, incident badge
- Semantic search bar: full-width, purple accent, ⌘K shortcut
- Metric strip: inference latency (ms), ingest rate (Gbps), buffer usage (%)

**Visual Reference:**
![Operator Console](/9j/4AAQSkZJRgABAQEAZABkAAD/2wBDAAUDBAQE...)

---

### Interface 2 — Customer Safety Dashboard (`/portal`)

**Aesthetic:** Warm premium light theme. Serif editorial headings paired with clean sans-serif body. Confidence-inspiring, executive-quality. Like a premium financial dashboard crossed with a quality print report.

**Typography:**
- Display headings: `Playfair Display` (weight 400–700)
- Body/UI: `DM Sans` (weight 300–500)
- Data/code: `DM Mono` (weight 400)

**CSS Design Tokens:**
```css
:root {
  --bg: #f4f2ee;
  --bg-white: #ffffff;
  --bg-warm: #faf9f6;
  --bg-card: #ffffff;
  --border: #e8e4dc;
  --border-strong: #d4cfc4;
  --text-primary: #1a1814;
  --text-secondary: #6b6560;
  --text-dim: #a09990;
  --accent: #c84b2f;
  --accent-light: rgba(200,75,47,0.08);
  --green: #1a7a4a;
  --green-light: rgba(26,122,74,0.08);
  --yellow: #9a6f00;
  --yellow-light: rgba(154,111,0,0.08);
  --blue: #1a4f8a;
  --blue-light: rgba(26,79,138,0.08);
  --shadow-sm: 0 1px 3px rgba(0,0,0,0.06);
  --shadow-md: 0 4px 12px rgba(0,0,0,0.08);
  --shadow-lg: 0 12px 32px rgba(0,0,0,0.1);
  --radius: 10px;
  --radius-sm: 6px;
}
```

**Key Components:**
- Left sidebar: org selector, navigation tree with icon+label, incident badge
- Summary cards (4-up): PPE compliance %, open incidents, zone violations, worker hours — each with sparkline
- Compliance chart: stacked bar chart, 7-day, per-PPE-category colors
- Site health table: site name, compliance score, trend arrow, incidents, last activity, sparkline
- Report downloads: Safety PDF, PPE PDF, Incidents PDF, Equipment XLSX — one-click each

---

### Interface 3 — Site Drill-Down (`/portal/sites/[id]`)

**Aesthetic:** Editorial/journalistic. Dense information beautifully typeset. Dark ink hero header. Serif headings anchor authority; monospaced data feels precise.

**Typography:**
- Headings: `Libre Baskerville` (weight 400–700)
- Body/UI: `IBM Plex Sans` (weight 300–500)
- Data/code: `IBM Plex Mono` (weight 400)

**CSS Design Tokens:**
```css
:root {
  --bg: #f7f4ef;
  --bg-ink: #1c1a17;
  --border: #e0d9cf;
  --text-primary: #1c1a17;
  --text-secondary: #5a5248;
  --text-dim: #9a9088;
  --text-white: #f7f4ef;
  --red: #b83220;
  --amber: #a05800;
  --green: #1e6e42;
  --blue: #1a4878;
}
```

**Key Components:**
- Dark hero header: site name serif, breadcrumbs, 4 KPI chips (PPE score, incidents, violations, workers)
- Elevated risk banner: amber strip with specific compliance issue
- Camera strip: 6 thumbnails, clickable, active = red pulse border
- Main panoramic viewer: 21:9 ratio, AI bounding boxes with labels, REC/AI ACTIVE badges
- Video timeline scrubber: event markers by severity color, playhead, range selector
- Incident table: time, severity pill, type, camera, description, status, evidence link
- Site plan: overhead schematic, camera markers, exclusion zone polygons
- PPE breakdown bars with sparklines per shift
- Workers roster with per-item PPE status icons

---

### Interface 4 — Incident Detail (`/portal/incidents/[id]`)

**Aesthetic:** Forensic dossier meets modern SaaS. Light base with dark chrome header. Red left-border accent. Evidence-locked visual language.

**Typography:**
- Headers: `Syne` (weight 400–800)
- Body: `Epilogue` (weight 300–500)
- Code/data: `Fira Code` (weight 400)

**CSS Design Tokens:**
```css
:root {
  --bg: #f8f7f5;
  --bg-dark: #141210;
  --bg-dark2: #1e1c19;
  --border: #e4e0d8;
  --text: #141210;
  --text-2: #4a4740;
  --text-3: #8a8680;
  --red: #c0311a;
  --red-bg: rgba(192,49,26,0.06);
  --red-border: rgba(192,49,26,0.16);
  --amber: #9a6200;
  --green: #1a6e40;
  --blue: #1448a0;
  --shadow-lg: 0 8px 32px rgba(0,0,0,0.14);
  --r: 8px;
}
```

**Key Components:**
- Dark chrome topbar: breadcrumb, prev/next nav, Export PDF, Escalate to HSE
- Incident header: ID badge, severity pill, status pill, full title, meta chips, action buttons
- Video evidence panel: 16:7 viewer, colored bounding boxes, worker callout popup, keyframe strip, playback scrubber with event markers
- AI Analysis: VLM caption in italic blockquote, 2×2 findings grid
- Detection objects table: class, confidence pill, coordinates (monospace), zone status, violation flag
- Right sidebar: OSHA classification, worker PPE breakdown, event timeline, notification log, related incidents, comments

---

### Interface 5 — Semantic Search (`/search`)

**Aesthetic:** Search engine meets cinema. Near-black backgrounds. Indigo-blue accent (#6c8fff) signals AI intelligence.

**Typography:**
- UI/headings: `Outfit` (weight 300–700)
- Monospace: `Space Mono` (weight 400–700)

**CSS Design Tokens:**
```css
:root {
  --bg: #0e0f11;
  --bg2: #13151a;
  --bg3: #191c23;
  --surface: #252a36;
  --border: rgba(255,255,255,0.07);
  --border2: rgba(255,255,255,0.12);
  --text: #eef0f5;
  --text2: #8890a8;
  --accent: #6c8fff;
  --accent-glow: rgba(108,143,255,0.2);
  --accent-border: rgba(108,143,255,0.35);
  --red: #ff5c48;
  --amber: #ffb340;
  --green: #3ecf8e;
  --purple: #b06fff;
  --teal: #30d5c8;
}
```

**Key Components:**
- Topbar: brand, nav, live camera count badge, open incidents badge, user avatar
- Search zone: large input with AI icon, ⌘K hint, blue glow on focus, filter chips row
- Left refinements: violation type checkboxes with counts, site selector, date range, confidence slider, time of day, model selector (Hybrid / Visual Only / Caption Only)
- Results grid: 3-column, thumbnail + bounding boxes, relevance badge, caption with query highlighting, meta tags, action buttons
- Right preview pane: video player with overlays, similarity score bar, full caption, token match breakdown, detected objects, action buttons

---

## API Contract

Base URL: `/api/v1`. Auth: `Authorization: Bearer <jwt>` on all routes.

```
// Auth (no JWT required)
POST /auth/login                     → { token, user }
GET  /auth/me                        → UserPublic

// Companies (organisations)
GET    /api/v1/companies             → Company[]
POST   /api/v1/companies
PUT    /api/v1/companies/{id}
DELETE /api/v1/companies/{id}
GET    /api/v1/companies/{id}/users
POST   /api/v1/companies/{id}/users
DELETE /api/v1/companies/{id}/users/{userId}

// Sites
GET    /api/v1/sites                 → SiteSummary[]
GET    /api/v1/sites/{id}            → SiteDetail
POST   /api/v1/sites
PUT    /api/v1/sites/{id}
DELETE /api/v1/sites/{id}
GET    /api/v1/sites/{id}/cameras    → Camera[]
GET    /api/v1/sites/{id}/sops       → SiteSOP[]
POST   /api/v1/sites/{id}/sops
PUT    /api/v1/sops/{id}
DELETE /api/v1/sops/{id}

// Camera assignments
POST   /api/v1/sites/{id}/camera-assignments
DELETE /api/v1/sites/{id}/camera-assignments/{cameraId}

// Incidents & Security Events
GET    /api/v1/incidents             → IncidentSummary[]
GET    /api/v1/incidents/{id}        → IncidentDetail
POST   /api/v1/events                // operator disposition → creates SecurityEvent

// Alarm management
POST   /api/v1/alarms/{id}/escalate
GET    /api/v1/dispatch/queue        → { count: N }  (active_alarms table)

// Operators & handoffs
GET    /api/v1/operators
GET    /api/v1/operators/current
GET    /api/v1/operators/{id}/handoffs
GET    /api/v1/handoffs
POST   /api/v1/handoffs

// WebSocket
WS     /ws                           // camera events, detection results
WS     /ws/alerts                    // alert stream — type:"alert" messages only

// NVR (cameras, recordings, PTZ, speakers)
GET    /api/cameras                  → Camera[]
POST   /api/cameras
PATCH  /api/cameras/{id}
DELETE /api/cameras/{id}
POST   /api/cameras/{id}/ptz/move
GET    /api/cameras/{id}/detect      // latest AI detections
GET    /api/cameras/{id}/recordings
```

### Core TypeScript Types

```typescript
type Severity = "critical" | "high" | "medium" | "low";
type IncidentStatus = "open" | "in_review" | "resolved";

interface SiteSummary {
  id: string;                  // "TX-203"
  name: string;
  status: "active" | "idle" | "critical";
  compliance_score: number;    // 0-100
  cameras_online: number;
  open_incidents: number;
  workers_on_site: number;
  last_activity: string;       // ISO timestamp
}

interface AlertEvent {
  id: string;
  site_id: string;
  camera_id: string;
  severity: Severity;
  type: string;                // "no_hard_hat" | "zone_breach" | "no_harness" ...
  description: string;
  snapshot_url: string;
  clip_url: string;
  ts: number;                  // Unix ms
  acknowledged: boolean;
}

interface IncidentDetail {
  id: string;                  // "INC-2026-0847"
  severity: Severity;
  status: IncidentStatus;
  title: string;
  site_id: string;
  camera_id: string;
  ts: number;
  duration_ms: number;
  workers_identified: number;
  ai_confidence: number;       // 0-1
  ai_caption: string;
  findings: Finding[];
  detections: Detection[];
  workers: WorkerPPE[];
  timeline: TimelineEvent[];
  notifications: NotificationLog[];
  comments: Comment[];
  clip_url: string;
  keyframes: Keyframe[];
}

interface SearchResult {
  frame_id: string;
  site_id: string;
  camera_id: string;
  ts: number;
  relevance_score: number;     // 0-1
  caption: string;
  thumbnail_url: string;
  clip_url: string;
  detections: Detection[];
  violation_flags: Record<string, boolean>;
  token_matches: TokenMatch[];
}

interface Detection {
  class: string;               // "person" | "hard_hat" | "excavator" ...
  subclass?: string;           // "no_hard_hat" | "no_harness" ...
  confidence: number;
  bbox: [number, number, number, number]; // [x1, y1, x2, y2] pixels
  track_id?: number;
  in_exclusion_zone: boolean;
  violation: boolean;
}
```

---

## State Management

**Zustand** for global UI state, **TanStack React Query** for server state.

```typescript
// Zustand stores
useOperatorStore    // selectedCamera, alertFeed[], siteFilter, layoutMode
usePortalStore      // selectedOrg, activeSite, dateRange
useSearchStore      // query, filters, results[], selectedResultId

// React Query hooks (implement these first)
useSites()          // polls every 30s
useSite(id)         // polls every 10s
useIncidents(filters)
useIncident(id)
useAlerts()         // backed by WebSocket stream
useSearch(query, filters)  // fires on submit, not onChange
```

### WebSocket Hook

```typescript
// hooks/useAlertStream.ts
function useAlertStream() {
  const addAlert = useOperatorStore(s => s.addAlert);
  
  useEffect(() => {
    const ws = new WebSocket(`${process.env.NEXT_PUBLIC_WS_URL}/ws/alerts?token=${getToken()}`);
    ws.onmessage = (e) => {
      const { type, data } = JSON.parse(e.data);
      if (type === 'alert') addAlert(data as AlertEvent);
    };
    // Reconnect with exponential backoff on close
    return () => ws.close();
  }, []);
}
```

---

## Severity Color System

Consistent across all 5 interfaces — use these exact values:

| Severity | Dark theme | Light theme | Usage |
|----------|-----------|-------------|-------|
| CRITICAL | `#ff3355` | `#c0311a` | Imminent danger, active breach |
| HIGH     | `#ff6b35` | `#a05800` | Active violation, unresolved incident |
| MEDIUM   | `#ffcc00` | `#9a6f00` | Non-compliance warning |
| LOW      | `#00d4ff` | `#1a4f8a` | Info, after-hours, resolved |

PPE compliance score thresholds: ≥90% = green · 75–89% = amber · <75% = red

---

## Implementation Rules

- **Never mix design systems** — each route loads its own font stack via `next/font/google`
- **Camera streams** are HLS (`.m3u8`) — use `hls.js`. Fall back to snapshot polling (1fps JPEG) if unavailable
- **Video evidence** uses pre-generated MinIO clips — standard `<video>` with `playsInline muted`
- **Detection overlays** = SVG on top of video/image. Use `<rect>` with class-specific stroke colors. Coordinates are absolute pixels; convert to % of displayed dimensions
- **Operator console is fullscreen** — design for 1920×1080, no horizontal scroll ever
- **Search is vector similarity**, not text search — send query string to API, render the results; no client-side filtering
- **Incident evidence is immutable** — show lock icon, "Evidence Locked" badge, no edit controls on video panel
- **All timestamps** are Unix epoch milliseconds. Format with `date-fns`. Show site-local time with UTC offset
- **Font loading** — use `next/font/google` with `display: swap`, `preload: true`. Never load fonts in CSS `@import`

---

## File Naming Conventions

```
app/operator/page.tsx
app/portal/page.tsx
app/portal/sites/[id]/page.tsx
app/portal/incidents/[id]/page.tsx
app/search/page.tsx

components/operator/CameraGrid.tsx
components/operator/AlertFeed.tsx
components/operator/FleetStatusBar.tsx
components/operator/PPECompliancePanel.tsx
components/portal/SiteHealthTable.tsx
components/portal/ComplianceChart.tsx
components/portal/SiteHeroHeader.tsx
components/shared/DetectionOverlay.tsx    # SVG overlay for any video/image
components/shared/VideoPlayer.tsx         # HLS + fallback
components/shared/SeverityPill.tsx        # Consistent severity badges
components/shared/PPEStatusIcon.tsx       # Per-item PPE check/cross icon

hooks/useAlertStream.ts
hooks/useSearch.ts
hooks/useSite.ts
hooks/useIncident.ts
lib/api.ts                               # Typed fetch wrapper
lib/ws.ts                                # WebSocket manager with reconnect
lib/format.ts                            # timestamp, score, duration formatters
types/index.ts
```

---

## Running Locally

```bash
# Frontend
cd frontend && npm install && npm run dev
# → http://localhost:3000

# Backend (Go)
cd BV-Platform && go run ./cmd/server/main.go
# → http://localhost:8080
# Runs DB migrations automatically on startup

# Default credentials
# admin / admin
# SOC operator: jhayes / demo123
# Portal: marcus.webb / demo123
```

The frontend falls back to mock data when the backend is unreachable — all API calls have try/catch mock returns.

Next.js proxy (`next.config.js`) forwards:
- `/api/*` → `localhost:8080/api/*`
- `/auth/*` → `localhost:8080/auth/*`
- `/ws` → `localhost:8080/ws`
- `/hls/*` → `localhost:8080/hls/*`
