# Ironsight configuration

Single-source reference for every environment variable the Go binaries read. Every entry below is parsed at startup by [`internal/config/config.go`](../internal/config/config.go); the rest of the codebase reads values off `*config.Config`, never the environment directly (enforced by P1-B-05).

- **Required**: process exits with `log.Fatalf` at boot if unset and `DEV_MODE` is empty. Production deploys must set these.
- **Optional**: an empty / unset env var falls back to the default. Empty `DEFAULT` column means "empty string" (no default).
- **Required-in-dev**: `DEV_MODE=1` lets the process auto-generate an ephemeral value with a loud warning. Useful for local dev only — never for production.

## Auth & access

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `ADMIN_PASSWORD` | string | — | required-in-dev | Seed password for the auto-created `admin` user on first boot. Production refuses to start without this. With `DEV_MODE=1` an ephemeral hex value is generated and printed to logs. |
| `ALLOWED_ORIGINS` | csv-string | `http://localhost:3000,http://localhost:8080` | no | CORS allowlist. Comma-separated origins; wildcards (`*`) honoured but discouraged. Production must override the localhost defaults with the actual frontend origin. |
| `DEV_MODE` | string | — | no | When non-empty, unblocks ephemeral fallbacks for `JWT_SECRET` and `ADMIN_PASSWORD`. Never set in production. |
| `JWT_SECRET` | string | — | required-in-dev | HMAC secret used to sign and verify JWTs. Generate with `openssl rand -hex 32`. Required at boot when `DEV_MODE` is empty. |
| `SSO_ADMIN_EMAILS` | csv-string | (empty) | no | Email addresses auto-promoted to `admin` on first SSO sight. Comma-separated; entries are trimmed. Only consulted when `SSO_TRUST_HEADER=email`. |
| `SSO_DEFAULT_ROLE` | string | `viewer` | no | Role assigned to SSO-provisioned users not in `SSO_ADMIN_EMAILS`. |
| `SSO_TRUST_HEADER` | string | (empty) | no | Set to `email` to opt the api into trusting the upstream proxy's `X-Forwarded-Email` header (oauth2-proxy + NPM in BigView). Empty keeps the original JWT-only flow. Only enable behind a proxy that strips inbound copies. |

## Server & HTTP

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `SERVER_PORT` | string | `8080` | no | Port the api binary listens on. String (not int) because the codebase passes it through unchanged. |

## Database

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `DATABASE_URL` | string | `postgres://onvif:onvif_dev_password@localhost:5432/onvif_tool?sslmode=disable` | no | Postgres connection string. Used by `cmd/server`, `cmd/worker`, `cmd/migrate`, `cmd/seed`. The legacy `onvif_tool` DB name in the default is tracked for rename under LOCAL-07. |

## Storage paths

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `EXPORT_PATH` | string | `./storage/exports` | no | Directory the evidence-export worker writes ZIP bundles into. Auto-created at startup. |
| `HLS_PATH` | string | `./storage/hls` | no | Directory the HLS server emits `.m3u8` / `.ts` segments into. Auto-created at startup. |
| `STORAGE_PATH` | string | `./storage/recordings` | no | Root directory for camera segment recordings (one subdir per camera UUID). Auto-created at startup. |
| `THUMBNAIL_PATH` | string | `./storage/thumbnails` | no | Directory for cached event thumbnail JPEGs. Auto-created at startup. |

## Recording engine

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `GORT_CAMERAS` | csv-string | (empty) | no | Comma-separated list of full camera UUIDs or 8-char prefixes that should use the pure-Go (`gortsplib`) recorder instead of FFmpeg. Consulted at camera-start time by `internal/recording/engine.go` and by `/api/recording/health` for engine-choice reporting. |
| `SEGMENT_DURATION` | int (seconds) | `60` | no | Length of each recorded segment file in seconds. Affects how often the recorder rolls over to a new file. |

## FFmpeg

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `FFMPEG_PATH` | string | `ffmpeg` (linux) / `C:\ffmpeg\bin\ffmpeg.exe` (windows) | no | Absolute or `$PATH`-relative path to the FFmpeg binary. The docker image apt-installs it onto `$PATH`. |

## AI pipeline

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `AI_ENABLED` | bool-ish | `true` | no | Kill-switch for the AI pipeline. Any value other than the literal string `false` enables. Matches the historical inline check `os.Getenv("AI_ENABLED") != "false"`. |
| `AI_QWEN_URL` | string | `http://127.0.0.1:8502` | no | HTTP base URL of the Qwen VLM (reasoning) sidecar. The api binary reads this for event-triggered analysis; the worker reads it for background VLM indexing. |
| `AI_YOLO_URL` | string | `http://127.0.0.1:8501` | no | HTTP base URL of the YOLO (detection) sidecar. Consumed by both the api and the worker. |
| `DETECTION_INTERVAL_MS` | int (ms) | `500` | no | How often (in milliseconds) the ONVIF Profile M analytics polling loop checks for new bounding boxes. |
| `DETECTION_SERVICE_URL` | string | (empty) | no | Legacy AI-detection-service URL. Repurposed in cmd/worker as the YOLO endpoint for the AI client; kept non-critical (the worker mostly cares about Qwen). |

## Indexer (background VLM)

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `INDEXER_CONCURRENCY` | int | `1` | no | Number of concurrent goroutines processing the segment-description queue. Clamped to `[1,16]`; values outside the range fall back to `1`. Typical: 1 on an 8 GB 3070, 4-8 on A40. |
| `INDEXER_ENABLED` | bool-ish | `true` | no | Set to `0` or `false` (case-insensitive) to disable the background VLM indexer entirely. Any other non-empty value keeps it on. Narrower than the shared bool helper to preserve historical behaviour. |
| `INDEXER_MIN_AGE_SEC` | int (seconds) | `90` | no | Minimum age in seconds before a closed recording segment becomes eligible for indexing. Guards against racing a still-writing file. |

## Worker

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `RUN_WORKERS` | bool | `true` | no | When true, the api binary also runs the retention / indexer / export loops in-process. Set to `false` in multi-container deploys where a sibling `worker` container owns these jobs (avoids racing on the same tables). The worker binary itself unconditionally runs the loops. |
| `WORKER_LEADER_DISABLED` | string | (empty) | no | Set to the literal string `1` to skip the Postgres advisory-lock leader election in the worker binary. Anything else (including `true` / `yes`) keeps leader election ON — exact-string match preserved from the historical inline check. |

## MediaMTX

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `EMBEDDED_MEDIAMTX` | bool | `true` | no | When true, the api binary spawns `bin/mediamtx` as a child process (dev + single-container). Set to `0` / `false` when MediaMTX runs as a sibling container or K8s sidecar. |
| `MEDIAMTX_API_ADDR` | string | `127.0.0.1:9997` | no | `host[:port]` of MediaMTX's HTTP control API. Runtime path adds/removes go here so config doesn't round-trip through a shared YAML volume. |
| `MEDIAMTX_RTSP_ADDR` | string | `127.0.0.1:18554` | no | `host[:port]` of the local RTSP relay. The recording engine pulls camera streams from this. |
| `MEDIAMTX_WEBRTC_ADDR` | string | `127.0.0.1:8889` | no | `host[:port]` exposing the WHEP endpoint the Go api reverse-proxies to. |
| `WEBRTC_ADDITIONAL_HOSTS` | csv-string | (empty) | no | Hostnames / IPs MediaMTX advertises in WebRTC ICE candidates in addition to its bound interface. In compose deployments MediaMTX otherwise only sees its bridge IP, which browsers can't reach. Set to fred's LAN IP / public hostname. Emitted into MediaMTX config under `webrtcAdditionalHosts`. |

## Mail (SMTP)

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `NOTIFY_PUBLIC_URL` | string | (empty) | no | Customer-visible base URL the mailer embeds in links ("View incident: ..."). Production must set this so emailed links point at the real frontend hostname, not localhost. |
| `SMTP_FROM` | string | (empty) | no | `From:` address on outbound mail. |
| `SMTP_HOST` | string | (empty) | no | SMTP relay hostname. Empty falls back to a stub mailer that logs notifications to stderr instead of sending. |
| `SMTP_PASS` | string | (empty) | no | SMTP password. |
| `SMTP_PORT` | string | `587` | no | SMTP port. String because it's threaded straight through to net/smtp without parsing. |
| `SMTP_USER` | string | (empty) | no | SMTP username. |

## SMS (Twilio)

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `TWILIO_ACCOUNT_SID` | string | (empty) | no | Twilio account SID. Any of the three Twilio vars being empty drops SMS to stub mode (stderr log). |
| `TWILIO_AUTH_TOKEN` | string | (empty) | no | Twilio auth token. |
| `TWILIO_FROM` | string | (empty) | no | E.164 sender number (e.g. `+15551234567`). |

## Redis (multi-replica fanout)

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `REDIS_URL` | string | (empty) | no | Full Redis DSN, e.g. `redis://redis:6379/0`. Empty = in-memory only; single-replica deployments don't need Redis. |
| `REDIS_WS_CHANNEL` | string | `ironsight:ws:broadcast` | no | Pub/sub channel name. All api replicas in a deployment must share this value to see each other's WebSocket events. |

## Evidence

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `EVIDENCE_SIGNING_KEY` | string | (empty) | no | Hex-encoded HMAC key signing evidence-export bundles. Missing or weak key disables signing — exports still succeed but ship without `SIGNATURE.txt`. For UL 827B / TMA-AVS-01 readiness this should be at least 32 bytes (64 hex chars); generate with `openssl rand -hex 32`. |

## Branding

| Name | Type | Default | Required | Description |
|---|---|---|---|---|
| `PRODUCT_NAME` | string | `Ironsight` | no | User-visible product name used in evidence bundles, log headers, and backend-emitted strings that reach customers. Frontend duplicates the value in `frontend/src/lib/branding.ts`. |

---

## Migration: how to consume new env vars

If you find yourself reaching for `os.Getenv` inside `internal/*` or `cmd/*`, **stop**: the pattern is

1. Add a typed field to `Config` (alphabetised within its group), with a comment describing what it gates.
2. Populate it in `Load()` using `getEnv` / `getEnvInt` / `getEnvBool` / `requireSecret` (the last for must-be-set-in-prod values).
3. Add a row to this table.
4. Make the consuming code take `*config.Config` (or a small typed subset struct) as a constructor parameter and read `cfg.YourField`.

The rule is enforced by [`conventions.md`](../../ironsight/backlog/conventions.md) line 16: "No `os.Getenv` outside `internal/config/`". The acceptance grep `grep -rn 'os.Getenv' --include='*.go' .` must return only `internal/config/` hits.
