# Ironsight — Quick Start

This is the shortest path from "fresh repo clone" to "logged into the
SOC console at `localhost:3000`". It covers the two stacks the team
actually develops on:

| Host | Container runtime | Status |
|---|---|---|
| **Linux (Ubuntu 22.04+)** | Docker Engine 24+ | Production target. `MasterDeployment.md` is the long-form reference. |
| **Windows 10/11 + WSL2** | Podman in an Ubuntu/Workbench distro | Day-to-day developer setup. |

Both flows use the same `docker-compose.yml` — Podman speaks Compose v2 natively.

> **Not production yet.** The defaults in `.env.example` (admin/admin, weak `JWT_SECRET`) are for local dev. Don't ship them. See [`SecurityOverview.md`](frontend/Documents/SecurityOverview.md) before exposing this to the internet.

---

## 1. Prerequisites

### All hosts
- **Git**
- **A copy of the repo**: `git clone https://github.com/spaeny10/bvplatform.git && cd bvplatform`
- **An `.env` file**: `cp .env.example .env` (then edit — see §3 below)

### Linux + Docker
- Docker Engine 24+ and Compose v2 ([official install guide](https://docs.docker.com/engine/install/ubuntu/), not the snap).
- For YOLO + Qwen GPU services: NVIDIA driver + [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html) and `sudo nvidia-ctk runtime configure --runtime=docker`.

### Windows + WSL + Podman
- WSL 2 with an Ubuntu or NVIDIA-Workbench distro (`wsl --install -d Ubuntu` if you don't have one).
- **Podman** inside the distro (`sudo apt-get install podman podman-compose` on Ubuntu).
- For GPU: NVIDIA Windows driver + `nvidia-container-toolkit` inside the WSL distro.
- **`C:\Users\<you>\.wslconfig`** with at minimum:
  ```ini
  [wsl2]
  vmIdleTimeout=-1
  ```
  Without this WSL kills the distro after 60 s of idle, taking the whole stack with it. After editing, run `wsl --shutdown` once.

---

## 2. The `Start Ironsight.bat` shortcut (Windows)

`bin/Start Ironsight.bat` is the one-click launcher for the WSL+Podman path. Double-click it after step 3. It:

1. Deletes `/etc/cdi/nvidia.json` (gets re-created on every WSL boot by `/etc/wsl.conf`'s NVIDIA hook and conflicts with our `nvidia.yaml`, leading to `unresolvable CDI devices` on yolo/qwen).
2. Syncs the system-scope CDI spec into `~/.config/cdi/` so rootless podman sees it.
3. Runs `podman compose up -d` against the full stack (db, mediamtx, api, worker, frontend, yolo, qwen).
4. Prints `docker-compose ps` so you can see what came up.

If you're on Linux you don't need it — just `docker compose up -d`.

---

## 3. The `.env` file

Copy `.env.example` to `.env` and edit — at minimum the variables marked **required**.

```bash
# REQUIRED — anything you don't change here is a security risk.
POSTGRES_PASSWORD=<long-random>     # Postgres superuser password
JWT_SECRET=<32+ random bytes>       # JWT signing key
ADMIN_PASSWORD=<dev only>           # First-boot admin user (also "admin/admin" by default)

# Optional — leave blank for defaults
ALLOWED_ORIGINS=http://localhost:3000
PUBLIC_BASE_URL=                    # Set when running behind a reverse proxy / tunnel
                                    # (used for sense-camera webhook URLs, share links)
```

The full annotated example lives in `.env.example`.

---

## 4. Bring it up

### Linux + Docker
```bash
docker compose up -d --build
docker compose ps
```

### Windows + WSL + Podman
- **Easy path**: double-click `bin/Start Ironsight.bat`.
- **Manual**: `wsl -d <distro>` → `cd /mnt/c/path/to/bvplatform` → `podman compose up -d --build`.

Either path takes 3–8 minutes the first time (image builds + model downloads on yolo/qwen).

---

## 5. Verify

```bash
# 1. All seven containers are running
docker compose ps

# 2. API answers
curl http://localhost:8080/api/health
# → {"status":"ok"}

# 3. Frontend renders
curl -sI http://localhost:3000 | head -1
# → HTTP/1.1 200 OK
```

Then open **http://localhost:3000** and log in with `admin / admin` (override via `ADMIN_PASSWORD` in `.env`).

The SOC operator console lives at `/operator`, the customer portal at `/portal`, and the admin pages at `/admin`.

---

## 6. Common first-time failures

| Symptom | Cause | Fix |
|---|---|---|
| **`Failed to fetch`** at login screen | api container isn't responding yet (still migrating, or already crashed) | `docker compose logs api`. If it's a TLS / DB issue, fix `.env`. If WSL keeps killing the distro, see §1's `vmIdleTimeout=-1`. |
| **`unresolvable CDI devices nvidia.com/gpu=all`** on `yolo`/`qwen` | Two CDI specs in `/etc/cdi/` (.json + .yaml) conflict, registry registers zero | Delete `/etc/cdi/nvidia.json`. The Windows `Start Ironsight.bat` does this automatically every cold boot. |
| **`yolo`/`qwen` exit immediately with `nvml init failed`** | NVIDIA Container Toolkit not installed in the distro | Install `nvidia-container-toolkit` and run `sudo nvidia-ctk runtime configure`. |
| **`api` log says `dial tcp ... timeout`** when adding a camera | Camera isn't reachable from the api container's network | Verify the camera IP is reachable from the host, then check the api container can reach the host network (compose default bridge does, host network is fine too). |
| **`Maximum number of Subscribe reached`** in api log for a Milesight camera | Older firmware caps concurrent ONVIF subscriptions to 4 — leaked subs from prior restarts | Reboot the camera (`Reboot device` button on its admin settings card) or wait ~1 hour for stale subs to TTL out. |
| **Console says "Add Camera failed: TLS handshake error"** on older Milesight Sense cams | Camera supports TLS 1.0/1.1 only; modern Go defaults reject | Already patched in `internal/onvif/client.go` — if it still fires, file an issue with the firmware version. |

---

## 7. Bringing the stack down

```bash
docker compose down            # stops + removes containers, keeps volumes
docker compose down -v         # ALSO drops pgdata + recordings — destructive
```

On Windows:
```cmd
wsl -d <distro> -- bash -c "cd /mnt/c/path/to/bvplatform && podman compose down"
```

---

## 8. Where to read next

- **Architecture deep-dive**: [`frontend/Documents/Ironsight_Architecture.md`](frontend/Documents/Ironsight_Architecture.md)
- **Production deployment**: [`frontend/Documents/MasterDeployment.md`](frontend/Documents/MasterDeployment.md)
- **Sense / push-only cameras** (Milesight SC4xx): [`frontend/Documents/SenseCamera.md`](frontend/Documents/SenseCamera.md)
- **Operator runbook + DR**: [`frontend/Documents/DisasterRecovery.md`](frontend/Documents/DisasterRecovery.md)
- **What's in the latest builds**: [`frontend/Documents/CHANGELOG.md`](frontend/Documents/CHANGELOG.md)
