# Ironsight — Mobile Apps Plan

> **Status:** planning only. Development is paused. This document captures the scope, tech choices, and prep work so whoever picks it up can start without re-litigating decisions.

Two mobile apps are on the roadmap:

1. **Customer app** — priority, built first. Live camera view (with PTZ control), recent clips, site snooze, basic incident list. Scoped to a customer's own sites.
2. **Operator supplement app** — secondary. After-hours / mobile-operator companion to the `/operator` desk console. Alarm queue, claim, dispose, live view. Does *not* replace the desk workflow.

---

## 1. Scope decisions (locked)

| Decision | Chosen |
|---|---|
| Operator app role | **Supplement**, not replacement. Slimmed feature set. |
| Customer app role | Fully functional for day-to-day viewing and PTZ; no admin. |
| Priority order | Customer first, operator later. |
| White-label per customer | **No.** Single-brand app. Themeable per-org is fine; separate app-store submissions per customer is not. |
| Push notifications at launch | **No.** SOC handles emergencies. Safety-violation pushes become Phase 2 of mobile when the safety engine is running. |
| Offline mode | Cached thumbnails + incident metadata only. Live streams and controls require network. |

---

## 2. Tech stack

**React Native, one codebase, two build flavors** (`app-operator`, `app-customer`) sharing ~70% of code.

Alternatives considered and rejected:

- **Native (Swift + Kotlin):** doubles the maintenance surface for no win at our current scale.
- **Flutter:** throws away the existing TypeScript investment.
- **PWA:** would have worked for the customer app in isolation, but iOS PWAs can't reliably hold WebSocket alarms, can't do two-way audio for the operator app, and can't do background push. Not worth the split.

**Reuses from the web frontend:**

- TypeScript types from [frontend/src/types/](../src/types/) via npm workspace or direct import.
- API client shape from [frontend/src/lib/api.ts](../src/lib/api.ts).
- JWT auth — same `Authorization: Bearer` flow.
- Zustand + TanStack React Query — both work on RN unchanged.

**Mobile-specific libs:**

- `react-native-webrtc` — WebRTC for live video and PTZ feedback.
- `@react-native-async-storage/async-storage` — token persistence.
- `react-native-vector-icons` or `phosphor-react-native`.
- `react-navigation` — standard router.

**Layout:**

```
mobile/
├── shared/                  # ~70% of the code
│   ├── api/                 # API client
│   ├── types/               # or npm workspace → frontend/src/types
│   ├── video/               # WebRTC + HLS player wrappers
│   └── auth/
├── app-operator/            # SOC operator flavor
└── app-customer/            # Customer portal flavor
```

---

## 3. Customer app — MVP scope

| Screen | Contents | Backend dependency |
|---|---|---|
| Login | Username + password → JWT | `POST /auth/login` (exists) |
| Site list | Sites user is assigned to, with online-camera count | `GET /api/v1/sites` (exists, RBAC-scoped) |
| Site detail | Camera tiles (thumbnails), snooze toggle, recent incidents | `GET /api/v1/sites/{id}/cameras`, `GET /api/v1/incidents?site_id=...` |
| Live view | WebRTC stream via WHEP, PTZ touch overlay, zoom pinch | WHEP reverse-proxy (exists), `POST /api/cameras/{id}/ptz/move` (exists), new `/api/webrtc/credentials` for TURN |
| Clip viewer | Playback of recent clips, scrub, download | `GET /api/v1/incidents/{id}` (exists), `GET /events/{id}/export` (exists) |
| Settings | Change password, logout, support contact | Existing user management endpoints |

Everything the customer app needs exists on the backend today except for **TURN credentials** and **cached thumbnails** (see §6).

---

## 4. PTZ spike plan — do this first

**Goal:** prove PTZ-over-WebRTC-on-cellular feels responsive before committing to 4–5 weeks of RN work. A sub-second move-then-see-it-move loop is the bar; anything over ~1.5 s feels broken.

**Duration:** 2 days, one engineer + one real PTZ camera + one LTE-connected phone.

### 4.1 What to build

Minimum viable RN app, throwaway:

1. Bare `npx react-native init` project.
2. One screen: WHEP-over-`react-native-webrtc` player pointed at a MediaMTX path for a known PTZ camera.
3. Touch-drag on the video surface → POST to `/api/cameras/{id}/ptz/move` with normalized pan/tilt vectors.
4. On-screen HUD showing:
   - Command-sent timestamp
   - Frame-arrived timestamp (from RTP timestamp or server-side watermark)
   - End-to-end delta in ms

### 4.2 What to measure

Run **20+ moves in each of three network conditions**:

| Condition | How to simulate |
|---|---|
| Same-LAN WiFi | Phone + camera + server on same network |
| Good LTE | Phone on cellular, server + camera remote |
| Degraded LTE | `tc` netem or Network Link Conditioner on mac → "Edge" profile |

Record for each move: command-sent → pixel-movement-visible time. Plot p50/p95/p99.

### 4.3 Pass / fail bar

| Condition | p95 target | Fail action |
|---|---|---|
| WiFi | < 400 ms | Investigate MediaMTX forwarding overhead — shouldn't ever miss this |
| Good LTE | < 800 ms | Add PTZ command WebSocket (avoid HTTP round trip) |
| Degraded LTE | < 1500 ms | Degrade gracefully: disable PTZ in the UI, show "PTZ requires stable connection" |

### 4.4 What a failure forces into the design

If LTE p95 exceeds 1500 ms the architecture needs one of:

- **PTZ command over WebSocket**, not HTTP. Saves ~150 ms per move on cellular. Requires a new `/ws/cameras/{id}/ptz` subscription, tiny addition.
- **ONVIF timeout tuning on the camera side.** Some Milesight firmwares default to >400 ms for PTZ ack. The Milesight driver at [internal/drivers/milesight_vca.go](../internal/drivers/milesight_vca.go) already tunes this; verify the tune applies to PTZ not just VCA.
- **Camera-side PTZ continuous mode** — instead of one HTTP call per drag delta, send a single "start pan" and a "stop" on finger-up. Slightly worse for fine control but radically lower latency for sweeps.

These are all backend-ish changes, not mobile-ish. Landing the spike before the RN build means we design around the right constraint.

### 4.5 Deliverable

A one-page writeup with the latency numbers and a verdict of:

- **Go** — build per plan, no architectural changes.
- **Go with tweak** — build per plan but include one of the mitigations above.
- **Rethink** — PTZ doesn't work on mobile with WebRTC, need to explore native RTSP players or accept disabled-on-cellular.

---

## 5. Effort estimate (when un-paused)

| Work | Effort |
|---|---|
| PTZ spike (§4) | 2 days |
| Backend prep (§6) | ~3 days |
| RN scaffolding + shared layer | ~1 week |
| Customer app MVP | ~2 weeks |
| App store submission + TestFlight + Play internal track | ~1 week |
| **Customer app TestFlight-ready** | **~4–5 weeks** |
| Operator supplement app (after customer ships) | ~2 weeks |

---

## 6. Backend prep required before RN coding starts

Additive changes, do not land until mobile work resumes. Listed here so nothing is forgotten.

| # | Item | Effort | Purpose |
|---|---|---|---|
| 1 | **Mobile thumbnail endpoint.** `GET /api/cameras/{id}/thumb?w=320` — serves the most-recent cached thumbnail immediately. Falls back to live snapshot if none cached. | ~4 h | Full `/vca/snapshot` can take 10 s; site-list tiles need sub-second. |
| 2 | **Device-ID JWT claim.** New optional `device_id` claim + `GET /api/users/me/devices` + `DELETE /api/users/me/devices/{id}`. Lets a user revoke one stolen phone without logging out everywhere. | ~1 day | Retrofitting JWT claims after shipping is painful. See §8 for why this might want to land sooner. |
| 3 | **TURN credential mint endpoint.** `POST /api/webrtc/credentials` returns time-limited username/password for coturn. Minted per-session, not a static shared secret. | ~1 day | Required for customer-at-home → site-behind-NAT WebRTC fallback. |
| 4 | **coturn container in docker-compose.** Adds one service, one volume, standard config. Takes single-host production from 7 → 8 containers (+1 to the table in MasterDeployment.md §2). | ~½ day | See §7. |

Total: **~3 days of backend work** before RN can start in earnest.

---

## 7. Container implications

Adds one container to the production stack.

| # | Service | Image | Role |
|---|---|---|---|
| +1 | `coturn` | `coturn/coturn` | WebRTC TURN relay for mobile clients behind NAT |

Port mapping: UDP 3478 + UDP relay range (49152-65535 by default; narrow with config). Authentication via the time-limited credentials minted from item (3) above. Static shared secrets are a well-known WebRTC footgun — the server signs ephemeral credentials using a secret known only to `coturn` and the Go API.

Nothing else in the compose file changes to support mobile.

---

## 8. Framework-level decisions to consider NOW

The user asked specifically what must be baked into the general framework *before* mobile ships so it's not painful to retrofit. Here's the short list.

### 8.1 Device-ID JWT claim — recommend landing soon

The JWT claim structure in [internal/auth/](../internal/auth/) today identifies the *user*, not the *session*. Adding a `device_id` field after the fact means every token issued between now and mobile-launch is silently non-revocable-per-device. Not catastrophic — you just can't cleanly handle "user's phone was stolen, revoke that session without logging out the browser" until tokens naturally expire.

**Cost to add now:** 30 minutes. An additional field in `auth.Claims`, populated by `HandleLogin`, ignored by existing middleware. Fully backwards-compatible.

**Cost to retrofit after 6 months of live users:** forces a global re-login when the old token format is deprecated.

**Recommendation:** land this next time someone touches auth, even before mobile work resumes.

### 8.2 API versioning discipline — already mostly fine

The backend has two namespaces today:

- `/api/*` — newer, less stable routes (cameras, milesight config, VCA)
- `/api/v1/*` — platform routes (sites, incidents, companies)

Mobile apps in the wild can't force-refresh the way a SPA can — once a version is out, its API routes must keep working for months. **Rule of thumb: anything consumed by mobile must live under a versioned prefix.** In practice that means when we wire the mobile app, we move any unversioned endpoints it needs under `/api/v1/`. Not a change to make now — just a constraint to remember when picking the endpoints mobile will call.

### 8.3 CORS policy audit — check before mobile work starts

RN apps don't send a `Origin` header by default (they're not browser contexts), so CORS is usually a non-issue. But if the current middleware refuses requests with missing Origin or enforces a specific one, RN will be rejected. **Check whenever the mobile spike starts**; don't change now in case it breaks the web frontend silently.

### 8.4 WebSocket auth pattern — worth confirming

The customer app probably wants WebSocket subscriptions for real-time incident updates. The web frontend uses `Authorization: Bearer` on the WS upgrade. Some RN WebSocket libs don't support headers on upgrade and require token-in-query-string. Worth confirming `react-native-websocket` supports headers during the spike — if not, we'd add query-string token as an alternate auth path on the server.

### 8.5 Rate limiting — flag only

No rate limiting exists today on the API. A buggy RN app that retries aggressively on a flaky connection can hammer the server. Not urgent now; flag as an item to add before mobile goes to production. Basic token-bucket per user_id + device_id is sufficient.

### 8.6 Talkdown codec (operator app only, when that work starts)

Defer. Only matters when the operator app gets built. The ONVIF backchannel work the existing driver did (see `internal/onvif/backchannel.go`) assumed desktop. Mobile's Opus-over-WebRTC will need a codec-negotiation pass against the camera's G.711 speaker. 1-day spike when the time comes.

---

## 9. Deferred work (explicit list)

These are intentionally not part of the customer app MVP. Re-open when their trigger lands.

| Item | Trigger |
|---|---|
| Push notifications — customer app | Safety engine starts emitting safety-violation events the customer cares about |
| Push notifications — operator app | Ever (alarms need to wake operators). Blocks operator app MVP. |
| Operator mobile app | Customer app is stable and in production |
| Talkdown 2-way audio | Operator app MVP |
| White-label / per-org theming | Never, unless a customer specifically pays for it |
| PTZ command WebSocket | PTZ spike (§4) reveals HTTP round-trip is too slow |
| Full offline mode | Explicitly out of scope — cached thumbnails + incident metadata is as far as we go |

---

## 10. Open questions / to decide when resuming

1. **Clip download UX.** Direct download of an MP4 via `Content-Disposition`, or streamed viewer with a "save to device" button? Download-to-device on iOS has a specific permission flow — worth deciding early.
2. **Site snooze on mobile.** The web portal has a snooze UI; should mobile mirror it exactly or simplify to an "off for 8 hours" quick-action?
3. **Error UX for camera offline.** Today the web shows a red badge. Mobile needs a clearer "camera is offline" state in the live view since a black frame is ambiguous.
4. **TestFlight beta group.** Which customer(s) get the first build? Pick a design-partner customer before starting the MVP.
5. **Backend team capacity for §6 prep.** Three days of backend work gate the mobile project — schedule before the RN engineer starts, not in parallel.

---

## 11. Reference — related documents

- [Ironsight_Architecture.md](Ironsight_Architecture.md) — platform architecture, RBAC model, SOC + customer workflows.
- [MasterDeployment.md](MasterDeployment.md) — Docker / Ubuntu deployment. When mobile work resumes, coturn gets added to §2 and §4; the container count in §1 goes 7 → 8.
- [MILESIGHT_DRIVER_BRIEF.MD](MSDriver/MILESIGHT_DRIVER_BRIEF.MD) — camera driver implementation notes.
