# Ironsight Platform

Real-time construction-site security + safety monitoring. Operator-facing
SOC console, customer portal, recording + AI-enrichment pipeline, vendor
camera integration (Milesight).

## Stack

- **Backend:** Go 1.25 — chi router, pgx, WebSocket hub, FFmpeg-driven
  recording engine, ONVIF + Milesight camera drivers.
- **Frontend:** Next.js 14 (App Router, TypeScript, Zustand + TanStack
  Query).
- **Video:** MediaMTX for RTSP / WHEP, HLS via the Go backend.
- **AI:** YOLO (detection) + Qwen-VL (reasoning) as sidecar HTTP services.
- **DB:** Postgres + TimescaleDB.

## Orientation

Everything worth knowing lives under [`frontend/Documents/`](frontend/Documents):

| File | What it covers |
|---|---|
| [`Roadmap.md`](frontend/Documents/Roadmap.md) | **Start here.** What's shipped, what's next, priority order. |
| [`Ironsight_Architecture.md`](frontend/Documents/Ironsight_Architecture.md) | Runtime architecture, RBAC, workflows, recording policy, Milesight integration. |
| [`MasterDeployment.md`](frontend/Documents/MasterDeployment.md) | Ubuntu + Docker deployment, GPU setup, operational runbook. |
| [`Rebrand.md`](frontend/Documents/Rebrand.md) | How to rename the product / swap colors / replace assets. |
| [`HOUSEKEEPING.md`](frontend/Documents/HOUSEKEEPING.md) | Design token system, light/dark theming. |
| [`MobileAppPlan.md`](frontend/Documents/MobileAppPlan.md) | Mobile apps (customer + operator) — paused, planning captured. |
| [`MSDriver/MILESIGHT_DRIVER_BRIEF.MD`](frontend/Documents/MSDriver/MILESIGHT_DRIVER_BRIEF.MD) | Camera driver implementation notes. |
| [`SenseCamera.md`](frontend/Documents/SenseCamera.md) | Push-only PIR / solar perimeter cameras (Milesight SC4xx). Webhook integration, payload format, security model. |

## Quick start

**The canonical 5-minute "clone and launch" guide:** [`QUICKSTART.md`](QUICKSTART.md). Covers Linux+Docker, Windows+WSL+Podman, GPU CDI setup, and `Failed to fetch` troubleshooting.

Shortest path:

```bash
git clone https://github.com/spaeny10/bvplatform.git && cd bvplatform
cp .env.example .env                    # edit secrets — see QUICKSTART §3
docker compose up -d --build            # or podman compose up -d --build
# → http://localhost:3000  (admin / admin)
```

For Windows, double-click [`bin/Start Ironsight.bat`](bin/Start%20Ironsight.bat) after the `.env` step — it handles the WSL+Podman+CDI bring-up.

Long-form production walk-through: [`MasterDeployment.md`](frontend/Documents/MasterDeployment.md).

## Status

Phase 1 (cross-platform builds) and Phase 2 (multi-container split) are
complete. Phase 3 items (worker HA, object-store recording tier,
evidence signing, ONVIF subscriber split) are queued — see
[`Roadmap.md`](frontend/Documents/Roadmap.md) §3 for priority.
