# ── Stage 1: build ─────────────────────────────────────────────
# Pinned Go version matches go.mod; pin bookworm so apt layers stay cached.
# Fully-qualified registry prefix for Podman compatibility — Docker ignores
# the docker.io/ prefix, Podman needs it when its unqualified-search-registries
# isn't configured.
FROM docker.io/library/golang:1.25-bookworm AS build

WORKDIR /src

# Copy module files first so `go mod download` caches independently of
# source changes. This keeps incremental builds fast.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is off by default for a static binary. The pgx driver is pure Go,
# websocket and chi are pure Go, ONVIF/SOAP is pure Go — nothing in this
# tree needs cgo. If you ever add SQLite or libmagic, drop CGO_ENABLED=1
# and switch the runtime image away from distroless.
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

# Four binaries: api (the HTTP server), worker (batch jobs), seed (one-shot
# demo-data loader for staging only — see cmd/seed/main.go and phase-plan
# task P1-B-09), and migrate (operator CLI for goose-tracked migrations,
# P1-B-01). They live in a single image so the ops story stays simple:
# `docker compose` picks which entrypoint each service runs, operators run
# `docker compose run --rm api /app/seed --all` for demo data (staging only),
# and `docker compose run --rm api /app/migrate <subcommand>` for migration
# inspection / rollback. Sharing the base layer keeps the registry footprint
# low; the binaries are each ~10–25 MB compressed.
RUN go build -trimpath -ldflags "-s -w" -o /out/server  ./cmd/server  && \
    go build -trimpath -ldflags "-s -w" -o /out/worker  ./cmd/worker  && \
    go build -trimpath -ldflags "-s -w" -o /out/seed    ./cmd/seed    && \
    go build -trimpath -ldflags "-s -w" -o /out/migrate ./cmd/migrate

# ── Stage 2: runtime ───────────────────────────────────────────
# We pick bookworm-slim over distroless because the server shells out to
# ffmpeg for recording/HLS and needs a real glibc + tools layer. If you
# later split recording into its own container you can swap this image
# for distroless/static and slim the API down further.
FROM docker.io/library/debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        ffmpeg \
        tzdata \
    && rm -rf /var/lib/apt/lists/*

# Non-root user. The UID must match whatever owns the shared recordings
# volume on the host (or bind-mount with fsGroup in K8s). 10001 is
# arbitrary; override at build time with `--build-arg APP_UID=...`.
ARG APP_UID=10001
ARG APP_GID=10001
RUN groupadd --system --gid ${APP_GID} ironsight && \
    useradd  --system --uid ${APP_UID} --gid ironsight --home /app ironsight

WORKDIR /app
COPY --from=build --chown=ironsight:ironsight /out/server  /app/server
COPY --from=build --chown=ironsight:ironsight /out/worker  /app/worker
COPY --from=build --chown=ironsight:ironsight /out/seed    /app/seed
COPY --from=build --chown=ironsight:ironsight /out/migrate /app/migrate

# Storage paths live under /data by convention. docker-compose mounts a
# named volume (or a host path) at /data; the Go server gets pointed at
# subdirectories via STORAGE_PATH / HLS_PATH / EXPORT_PATH / THUMBNAIL_PATH.
RUN mkdir -p /data/recordings /data/hls /data/exports /data/thumbnails /data/mediamtx && \
    chown -R ironsight:ironsight /data

# /app/bin is where the Go server writes mediamtx_runtime.yml. In compose
# this path is a named volume (mediamtx-config) shared with the mediamtx
# container. Named volumes inherit ownership from the mount point in the
# image on first use — so we create the dir here, owned by ironsight, to
# avoid "permission denied" when the non-root api process writes the file.
RUN mkdir -p /app/bin && chown -R ironsight:ironsight /app/bin

USER ironsight

# Tell the Go code MediaMTX is *not* embedded — another container hosts it.
# All other defaults come from config.Load(); the compose file supplies the
# per-service URLs and secrets.
ENV EMBEDDED_MEDIAMTX=0 \
    STORAGE_PATH=/data/recordings \
    HLS_PATH=/data/hls \
    EXPORT_PATH=/data/exports \
    THUMBNAIL_PATH=/data/thumbnails

EXPOSE 8080

# Simple healthcheck. /api/health is the only unauthenticated endpoint
# exposed by the router; see internal/api/router.go.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/api/health || exit 1

ENTRYPOINT ["/app/server"]
