# Ironsight — Master Deployment Guide (Ubuntu + Docker)

This document is the canonical reference for deploying Ironsight on Ubuntu hosts using Docker. It assumes Docker Engine 24+ and Compose v2. Every code path the document mentions is file:line-referenced so you can audit the claim.

The target deployments this guide covers, in order of complexity:

1. **Single-host dev / staging** — one Ubuntu VM, `docker compose up -d`.
2. **Single-host production** — the same, plus reverse proxy, TLS, host-level hardening.
3. **Multi-host production** — multiple recorder hosts close to cameras, a central API host, a managed Postgres, shared object storage. Requires the Phase 2 / Phase 3 work tracked separately.

If you're standing up the system for the first time, work through (1) end-to-end on a throwaway VM before you touch (2).

---

## 1. Repository layout relevant to deployment

```
.
├── Dockerfile                       # Go API image (multi-stage, CGO off, Debian slim)
├── docker-compose.yml               # Stack definition for single-host deploys
├── .env.example                     # Copy to .env, fill in secrets
├── .dockerignore                    # Keeps build context small
├── frontend/Dockerfile              # Next.js image (alpine base)
├── init.sql                         # Postgres init script, runs once on fresh DB
└── frontend/Documents/
    ├── Ironsight_Architecture.md    # Product / runtime architecture
    └── MasterDeployment.md          # This file
```

The Go server is a cross-platform binary — the Windows-specific code paths are behind build tags so `GOOS=linux go build ./cmd/server` produces a clean ELF. The Dockerfile uses `CGO_ENABLED=0` and expects no host libraries beyond `ca-certificates`, `curl`, `ffmpeg`, and `tzdata`.

Platform-specific files you should be aware of:

- [internal/streaming/mediamtx_kill_unix.go](internal/streaming/mediamtx_kill_unix.go) — no-op orphan-kill on Linux.
- [internal/streaming/mediamtx_kill_windows.go](internal/streaming/mediamtx_kill_windows.go) — Windows `taskkill` shim.
- [internal/api/storage_unix.go](internal/api/storage_unix.go) — parses `/proc/mounts` for drive enumeration; `syscall.Statfs` for disk usage.
- [internal/api/storage_windows.go](internal/api/storage_windows.go) — `golang.org/x/sys/windows` equivalents.

---

## 2. Architecture in containers

```
                         ┌──────────────────┐
                 HTTPS ──┤  reverse proxy   ├── (Caddy / nginx / Traefik, terminates TLS)
                         └────────┬─────────┘
                                  │
        ┌─────────────────────────┼─────────────────────────┐
        │                         │                         │
┌───────▼────────┐       ┌────────▼─────────┐       ┌───────▼────────┐
│   frontend     │       │      api         │       │    mediamtx    │
│  Next.js :3000 │       │    Go :8080      │       │   :8554/:8889  │
└────────────────┘       └───┬──────────┬───┘       └──────┬─────────┘
                             │          │                  │
                   ┌─────────┘          └────────┐         │ writes
                   │                             │         │ runtime.yml
           ┌───────▼──────┐             ┌────────▼──────┐  │
           │   postgres   │             │  yolo / qwen  │  │
           │   :5432      │             │  GPU services │  │
           └──────────────┘             └───────────────┘  │
                                                           │
                                                ┌──────────▼────────┐
                                                │  mediamtx-config  │
                                                │  (shared volume)  │
                                                └───────────────────┘
                                                ┌───────────────────┐
                                                │    recordings     │
                                                │   (shared volume) │
                                                └───────────────────┘
```

Containers in the default stack (**7 services, single-replica**):

| Service | Image | Purpose | Exposes to host |
|---|---|---|---|
| `frontend` | local build (`./frontend/Dockerfile`) | Next.js SPA | 3000 |
| `api` | local build (`./Dockerfile`, `server` entrypoint) | Go HTTP + WS server, recording engine, ONVIF subscribers | 8080 |
| `worker` | same image (`worker` entrypoint) | Retention purge, VLM indexer, export-job concat worker | (internal — no HTTP) |
| `mediamtx` | `bluenviron/mediamtx` | RTSP relay + WHEP for browser WebRTC; HTTP control API for runtime path updates | 8554, 8889 (API 9997 internal-only) |
| `yolo` | `ironsight/yolo` | Object detection HTTP service | (internal) |
| `qwen` | `ironsight/qwen` | Vision-language model HTTP service | (internal) |
| `db` | `timescale/timescaledb` | Postgres with TimescaleDB extension | (internal) |

Plus Caddy in front for production (see §5.1): **8 containers single-host prod.**

**api vs worker split** — both run from the same image. `api` serves HTTP and owns the recording engine (stateful per-camera FFmpeg). `worker` runs the batch jobs that don't need HTTP: hourly retention purge, VLM indexer, video-export concat queue. They're split so the api process stays focused on request latency and can be restarted without losing in-flight batch work. `api` has `RUN_WORKERS=false`; `worker` runs those jobs unconditionally. For a simpler all-in-one deployment (lab / single box), set `RUN_WORKERS_API=true` in `.env` and remove the `worker` service — the api then runs everything in-process as it did before Phase 2.

**Optional when scaling the API tier past one replica** (profile-gated, not started by default):

| Service | Image | Purpose |
|---|---|---|
| `redis` | `redis:7-alpine` | Pub/sub fanout for WebSocket events across API replicas |

Start with `docker compose --profile scale-out up -d` and set `REDIS_URL=redis://redis:6379/0` in `.env`. In-memory WS hub is the default; Redis bridge activates when that env var is non-empty.

Volumes:

| Volume | Written by | Read by | Notes |
|---|---|---|---|
| `pgdata` | db | db | Back up off-box. §6 covers cadence. |
| `recordings` | api | api | All recording segments, HLS output, exports, thumbnails. Size with [§5.3](#53-sizing-storage). |
| `mediamtx-config` | api (once at startup) | mediamtx (bootstrap + restart-recovery) | Bootstrap YAML only. Runtime path adds/removes go through the MediaMTX HTTP control API — no per-camera config rewrite. |

Retention, VLM indexer, and the export worker live in the separate `worker` container as of Phase 2. ONVIF event subscriptions still run inside `api` because they're tightly coupled to the recording-engine event-mode trigger path; that extraction is tracked as Phase 3 work. See §9 for the current breakdown.

---

## 3. Ubuntu host prep

### 3.1 Supported versions

Tested on Ubuntu 22.04 LTS and 24.04 LTS. Earlier releases ship Docker versions too old for Compose v2.

### 3.2 Install Docker Engine (official repo, not the snap)

```bash
sudo apt-get remove docker docker-engine docker.io containerd runc
sudo apt-get update && sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
  | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
  https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Let your user run docker without sudo. Log out + back in after this.
sudo usermod -aG docker "$USER"
```

Verify:

```bash
docker --version              # Docker version 27.x or newer
docker compose version        # Docker Compose version v2.20.x or newer
```

### 3.3 If you have GPUs (for YOLO / Qwen)

Install NVIDIA drivers via the Ubuntu `ubuntu-drivers` tool or the official `.run` installer, then add the NVIDIA container runtime:

```bash
distribution=$(. /etc/os-release; echo $ID$VERSION_ID)
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
  | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -s -L https://nvidia.github.io/libnvidia-container/$distribution/libnvidia-container.list \
  | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
  | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit
sudo nvidia-ctk runtime configure --runtime=docker
sudo systemctl restart docker
docker run --rm --gpus all nvidia/cuda:12.3.0-base-ubuntu22.04 nvidia-smi    # sanity check
```

Then uncomment the `deploy:` blocks for `yolo` and `qwen` in `docker-compose.yml`.

### 3.4 Firewall

UFW is Ubuntu's default. The only ports the outside world needs are whatever your reverse proxy listens on:

```bash
sudo ufw allow 22/tcp           # SSH — lock this down to a jump-host if possible
sudo ufw allow 80/tcp           # HTTP for ACME challenges
sudo ufw allow 443/tcp          # HTTPS
sudo ufw enable
```

Postgres (5432), MediaMTX (8554, 8889), and the API (8080) should **not** be exposed to the internet directly — the reverse proxy fronts them. The compose file only publishes those ports to `localhost`; a hostile network sees only 80/443.

---

## 4. First deploy (single-host)

```bash
git clone <repo> ironsight
cd ironsight

# Seed secrets. Generate real values, don't ship .env.example unchanged.
cp .env.example .env
nano .env        # set POSTGRES_PASSWORD, JWT_SECRET, ADMIN_PASSWORD

# Build images + bring the stack up.
docker compose build
docker compose up -d

# Watch it come up.
docker compose logs -f
```

On first start:

1. `db` runs `init.sql` to create the schema.
2. `api` runs its own auto-migrations (see [cmd/server/main.go](cmd/server/main.go) around the schema migration block) — site recording-policy columns, deterrence_audits table, VCA rules table, etc.
3. `api` writes the initial `mediamtx_runtime.yml` to the shared volume; `mediamtx` picks it up.
4. A default `admin` user is created with `ADMIN_PASSWORD` — change it via the UI on first login.

### 4.1 Verifying

```bash
curl -fsS http://localhost:8080/api/health
# → {"status":"ok"}

docker compose ps
# All services should be 'healthy' or 'running' (yolo/qwen show 'running' without healthcheck).
```

Open `http://<host>:3000` for the frontend.

### 4.2 Common first-deploy failures

| Symptom | Likely cause |
|---|---|
| `api` keeps restarting with `set JWT_SECRET in .env` | `.env` missing a required variable. The compose file uses the `${VAR:?message}` form to fail fast — read the exact message. |
| `mediamtx` exits immediately, logs "cannot find config" | Shared-volume race on cold start. The `api` container writes the config on boot; restart `mediamtx` once `api` is healthy: `docker compose restart mediamtx`. |
| `api` healthcheck fails but `/api/health` works from host | Inside the container, `curl` isn't on PATH. The Dockerfile installs it — if you see this, you're running an older image. Rebuild: `docker compose build --no-cache api`. |
| Frontend loads but every API call is 502 | The browser is hitting the frontend container, not the API. Set `NEXT_PUBLIC_API_BASE=http://<host>:8080/api` in `.env` or proxy through the frontend (recommended — see §5.1). |

---

## 5. Production hardening

### 5.1 Reverse proxy with TLS

The recommended shape is Caddy in front of the stack — automatic ACME, single config file, native Docker integration. Add a `caddy` service to `docker-compose.yml`:

```yaml
caddy:
  image: caddy:2-alpine
  restart: unless-stopped
  ports:
    - "80:80"
    - "443:443"
  volumes:
    - ./deploy/Caddyfile:/etc/caddy/Caddyfile:ro
    - caddy-data:/data
    - caddy-config:/config
  networks: [backplane]
```

Minimal `deploy/Caddyfile`:

```caddyfile
ironsight.example.com {
    # Frontend
    reverse_proxy frontend:3000

    # API and WebSocket — same origin, under /api
    reverse_proxy /api/* api:8080
    reverse_proxy /ws/* api:8080

    # MediaMTX WHEP for browser WebRTC
    reverse_proxy /whep/* mediamtx:8889

    # HSTS and the usual security headers
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        Referrer-Policy "strict-origin-when-cross-origin"
    }
}
```

Close the host-level port publications on `api`, `frontend`, and `mediamtx` in `docker-compose.yml` once Caddy is routing.

### 5.2 Secret management

`.env` files on disk are fine for small deployments. For anything with a compliance story:

- Use Docker Swarm secrets or Kubernetes Secret resources instead of `environment:` env-vars. The Go config layer reads from the environment either way — both map via `env_file` / mounted files.
- Rotate `JWT_SECRET` on incident response (rotating invalidates every existing JWT — a feature, not a bug).
- Never commit `.env`. The repo's `.dockerignore` and a `.gitignore` rule both exclude it.

### 5.3 Sizing storage

Rough per-camera disk use, continuous recording, 1080p main stream at 4 Mbps, 60 s segments:

- Main stream: **4 Mbps ≈ 42 GB / camera / day**
- Sub stream (HLS, optional): ~4 GB / camera / day
- Event clips (event-mode): varies wildly; budget **20% of main-stream** as a starting point.

For a 10-camera site with 30-day retention: `10 × 42 × 30 = 12.6 TB` plus 20% headroom ≈ **15 TB**. Provision the `recordings` volume on an NVMe or fast SAS array — cold-storage S3 retrieval for retention purges is a future optimisation, not a fit for live recording.

Retention is enforced hourly by [internal/recording/retention.go](internal/recording/retention.go). The policy lives on the site row (see Ironsight_Architecture.md §8).

### 5.4 Host kernel / OS tuning

```bash
# Allow more open files — FFmpeg + pgx + the WebSocket hub can push this past 1024.
sudo tee /etc/security/limits.d/ironsight.conf <<'EOF'
*  soft  nofile  65536
*  hard  nofile  65536
EOF

# Journald log retention (Docker logs via journald by default)
sudo sed -i 's/#SystemMaxUse=/SystemMaxUse=2G/' /etc/systemd/journald.conf
sudo systemctl restart systemd-journald
```

### 5.5 Non-root, read-only root filesystem

The api and frontend images already run as UID 10001 / 10002. For additional hardening, add to the compose service:

```yaml
read_only: true
tmpfs:
  - /tmp
cap_drop: [ALL]
```

The Go server will fail to write `mediamtx_runtime.yml` if you do this without keeping `/app/bin` writable — the volume mount in the compose file already takes care of that, so `read_only: true` works today. Verify before pushing to prod.

---

## 6. Backup and disaster recovery

### 6.1 Postgres

The authoritative state (sites, cameras, VCA rules, events, incidents, users) lives in Postgres. Snapshots of the `pgdata` volume alone aren't enough — do a proper `pg_dump`:

```bash
# Nightly full dump to a mounted backup volume. Rotate with logrotate or
# a custom retention script.
docker compose exec -T db pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom \
  > /backup/ironsight-$(date +%F).dump
```

Restore into a fresh volume:

```bash
docker compose exec -T db pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists \
  < /backup/ironsight-2026-04-20.dump
```

### 6.2 Recordings

Recordings are much bigger than the DB and usually have a legal/insurance retention clock attached. A couple of patterns:

- **Evidence-only off-box copy.** The evidence-export flow (when it lands — currently a roadmap item) triggers an S3 push on shareable-link generation. Day-to-day recordings stay on the local volume; only exported evidence is replicated.
- **Full mirror.** `rsync` the `recordings` volume to a NAS or second host nightly. Expensive bandwidth-wise but straightforward.
- **Object-store backed volumes.** rclone-mount or Docker's S3 volume driver. Works but will pick up latency — test before committing.

### 6.3 Configuration

Keep `docker-compose.yml`, `.env` (in an encrypted vault — **not** git), and `init.sql` under version control. The schema is rebuilt by `init.sql` + the Go auto-migrations, so a fresh host + a `pg_dump` restore + a volume restore is a complete rebuild.

---

## 7. Upgrades

The repo follows a single canonical image tag (`IRONSIGHT_VERSION` in `.env`).

```bash
git pull                                 # grab new code
docker compose build                     # build new images
docker compose up -d                     # recreate containers with new images
docker compose logs -f api               # watch the schema migration block come through
```

Migration strategy:

- **Additive migrations only.** The auto-migration block in `cmd/server/main.go` uses `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` and `CREATE TABLE IF NOT EXISTS`. Rolling back to the previous image is safe — new columns are ignored by the old binary.
- **Legacy columns are left in place** for one release after a field moves (see the site recording-policy migration, Ironsight_Architecture.md §8.2). If you're running images spanning more than one release apart, update through the intermediate release first.

### 7.1 Zero-downtime upgrade (single host)

The current architecture doesn't support this cleanly — `api` owns ONVIF subscriptions and the in-memory WebSocket hub, so two running replicas would duplicate events. Practical single-host approach: accept a ~30 s blip.

```bash
docker compose up -d --no-deps --build api
```

For true zero-downtime you need Phase 2 (worker split + Redis pub/sub). Tracked as todo.

---

## 8. Multi-host production (roadmap)

When you outgrow one host — typically when recording bandwidth or camera count blows past what a single NIC / NVMe can handle — the target topology is:

```
         ┌────────────────────────────────────────────┐
         │                 central host                │
         │  ┌──────────┐  ┌────────┐  ┌────────────┐   │
         │  │   api    │  │ worker │  │  postgres  │   │
         │  └──────────┘  └────────┘  └────────────┘   │
         │  ┌──────────┐  ┌─────────────┐              │
         │  │  redis   │  │  yolo/qwen  │ (GPU host)   │
         │  └──────────┘  └─────────────┘              │
         └────────────────────────────────────────────┘
                      ▲                ▲
               object │                │ events over mTLS
               store  │                │
                      │                │
         ┌────────────┴──┐  ┌──────────┴───┐  ┌─────┐
         │ site-1 recorder│  │site-2 recorder│  │ … │
         │  mediamtx       │  │ mediamtx      │
         │  recorder-worker│  │recorder-worker│
         └─────────────────┘  └───────────────┘
```

The seams this document already calls out as prerequisites:

| Gap | Where documented | Status |
|---|---|---|
| MediaMTX as own container, not child process | §P1.3 of this doc; [internal/streaming/mediamtx.go](internal/streaming/mediamtx.go) | ✅ Done (EMBEDDED_MEDIAMTX=0 in compose) |
| AI service URLs as env vars | §P1.2 | ✅ Done |
| Worker split (retention/indexer/exports/subscriptions) | Phase 2 todo | Not started |
| WebSocket fan-out via Redis | Phase 2 todo | Not started |
| Export job atomic claim | Phase 2 todo | Not started |
| Recorder colocated with storage | Phase 2 design | Not started |
| Leader election for hot-standby workers | Phase 3 todo | Not started |

---

## 9. Known single-process assumptions (reproduced from audit)

For quick reference when you hit one of these in an incident. Detail and file:line refs are in the audit (search the project for "Container-readiness scorecard").

1. **WebSocket hub is in-memory** (`internal/api/websocket.go`). Running two `api` replicas today will strand half of each client's events. Don't scale horizontally until Phase 2 ships.
2. **ONVIF subscriptions are started per-boot for every camera** (`cmd/server/main.go:743` area). Running two `api` replicas = double subscription = duplicate events. Worker split fixes this.
3. **Export worker polls the DB without row-level locking** (`internal/export/export.go`). Two replicas would double-encode the same evidence bundle. Phase 2 adds the atomic claim.
4. **Retention is idempotent** (`internal/recording/retention.go`). Safe to run in multiple replicas but wasteful. Run in a singleton worker in Phase 2.
5. **Recording output path is a local filesystem** (`internal/recording/engine.go`). Works with NFS / shared volume but you'll want per-recorder-host colocation in the multi-host topology.

---

## 10. Operational runbook

### 10.1 Check service health

```bash
docker compose ps
docker compose exec api curl -fsS http://127.0.0.1:8080/api/health
docker compose logs --tail=100 api
docker compose logs --tail=100 mediamtx
```

### 10.2 Tail the recording pipeline

```bash
docker compose logs -f api | grep -E '\[REC\]|\[MEDIAMTX\]|\[RETENTION\]'
```

### 10.3 Force a MediaMTX config rebuild

Triggered automatically when a camera is added/removed via the API. To force manually:

```bash
docker compose restart mediamtx
```

The `api` container writes the current config on its own startup as well — if `mediamtx-config` was wiped, a `docker compose restart api` rewrites it.

### 10.4 Reset the admin password

The password is only auto-created on first boot. To reset after that, run an SQL UPDATE via `db` against a freshly bcrypt-hashed value (use any bcrypt tool; cost 10). Or in an emergency, wipe the `users` row with `username='admin'` and restart `api` — it recreates the admin user using the current `ADMIN_PASSWORD` env.

### 10.5 Shut down cleanly

```bash
docker compose down                 # stops containers, keeps volumes
docker compose down -v              # ALSO deletes volumes — DESTRUCTIVE
```

### 10.6 Rotate logs

Docker keeps per-container logs under `/var/lib/docker/containers/*/*-json.log` by default. Cap them in `daemon.json`:

```json
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "50m", "max-file": "5" }
}
```

`sudo systemctl restart docker` after editing.

---

## 11. Development workflow

### 11.1 Running locally without Docker

The code still supports running the Go server directly on Windows or Linux for faster iteration:

```bash
# Windows: rebuild + run as documented in earlier ops sessions.
# Linux:
go run ./cmd/server
```

The only difference from the containerised deployment is `EMBEDDED_MEDIAMTX=1` (the default) — the Go process spawns `./bin/mediamtx` itself. For any multi-service work, prefer compose.

### 11.2 Cross-compiling manually

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /tmp/server ./cmd/server
```

This produces the same 21 MB statically-linked ELF the Dockerfile builds, without Docker. Useful for staging drops to a bare host.

---

## 12. Open questions / decisions deferred

- **Object-store recordings** — a real multi-tenant deployment wants S3-backed cold storage with a local NVMe hot tier. Not yet designed; current recording path assumes local FS only.
- **Video evidence chain-of-custody** — digital signing of exported clips. Flagged in Ironsight_Architecture.md §5 but not implemented.
- **Multi-region** — current topology assumes one datacentre. Cross-region WebRTC for talkdown is particularly painful and will need a TURN relay.

Update this section as decisions land — it's the authoritative place for "we looked at this and chose X because Y" so future-you doesn't re-litigate.
