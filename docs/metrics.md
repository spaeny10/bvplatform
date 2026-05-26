# Ironsight metrics catalog

Prometheus exposition endpoint: `GET /metrics`

Auth: controlled by `METRICS_AUTH` env var (see [`configuration.md`](./configuration.md#metrics-p1-c-03)).  
Registry: non-default (`internal/metrics.Registry`) — standard Go runtime + process collectors plus the series below.

Decision context: [`docs/decisions.md` D-02](../../docs/decisions.md) — self-host Prom + Grafana LXC.  
Prom LXC provisioning is platform-ops work separate from this endpoint.

---

## HTTP layer

| Series | Type | Labels | Source |
|---|---|---|---|
| `ironsight_http_requests_total` | Counter | `route`, `method`, `status` | `internal/metrics.HTTPMiddleware` increments on every completed request. `route` is the chi pattern (e.g. `/cameras/{id}`) not the resolved URL — cardinality stays bounded. |
| `ironsight_http_request_duration_seconds` | Histogram | `route`, `method` | Same middleware; uses `prometheus.DefBuckets` (.005→10 s). |

**Cardinality note**: `route` uses `chi.RouteContext.RoutePattern()` so 90 cameras share one series (`/cameras/{id}`) rather than 90 series with resolved UUIDs. Do not add `user_id`, `tenant_id`, or full URL to these labels.

---

## Recording engine

| Series | Type | Labels | Source |
|---|---|---|---|
| `ironsight_recording_active_cameras` | Gauge | — | Set by `recording.Engine` on `StartRecording`, `StopRecording`, `StopAll`. Reflects both FFmpeg and gortsplib recorders. |
| `ironsight_recording_ffmpeg_subprocesses` | Gauge | — | Same hooks; counts only FFmpeg recorders (excludes gortsplib). |
| `ironsight_recording_segments_written_total` | Counter | `camera_id` | Incremented by `watchSegments` after each successful `db.InsertSegment`. Cardinality: 90 cameras × 1 series = 90 series — safe. Do not add `tenant_id`. |

---

## Database pool

All three series are synced from `db.Pool.Stat()` every 15 seconds by a background goroutine in `cmd/server/main.go`.

| Series | Type | Labels | Source |
|---|---|---|---|
| `ironsight_db_pool_acquire_count` | Gauge | — | `pgxpool.Stat().AcquireCount()` — cumulative successful acquisitions. Use `rate()` in Grafana for per-second throughput. |
| `ironsight_db_pool_idle` | Gauge | — | `pgxpool.Stat().IdleConns()`. |
| `ironsight_db_pool_total` | Gauge | — | `pgxpool.Stat().TotalConns()`. Pool is configured `MinConns=4, MaxConns=50`. |

---

## WebSocket hub

| Series | Type | Labels | Source |
|---|---|---|---|
| `ironsight_ws_clients_connected` | Gauge | — | Set by `Hub.Run` on every register/unregister event. |

---

## RBAC cache (P1-A-04 refresher)

| Series | Type | Labels | Source |
|---|---|---|---|
| `ironsight_rbac_cache_refresh_total` | Counter | — | Incremented each time the RBAC refresher goroutine runs a full cycle. |
| `ironsight_rbac_cache_refresh_errors_total` | Counter | — | Incremented when the refresher hits a DB error and falls back to the cached allow-set. |

---

## Boot / migration

| Series | Type | Labels | Source |
|---|---|---|---|
| `ironsight_goose_migration_version` | Gauge | — | Set once at startup to the goose schema version applied. |

---

## Standard Go runtime / process metrics

The non-default registry also includes `collectors.NewGoCollector()` and `collectors.NewProcessCollector()`, which expose the standard `go_*` and `process_*` families (goroutine count, GC stats, heap, open FDs, CPU seconds, etc.).

---

## Adding new metrics

1. Define the metric in `internal/metrics/metrics.go`.
2. Register it in the `init()` block of the same file.
3. Add a convenience setter/incrementer if needed to keep call sites thin.
4. Add a row to this catalog.
5. Do **not** add high-cardinality labels (`user_id`, `tenant_id`, full URLs, error message strings). A label with N distinct values creates N series — at 1 000 values Prom performance degrades visibly, at 100 000 it dies.

---

## Grafana dashboard sketch

Recommended panels for the initial dashboard (to be built when the Prom LXC is provisioned):

- **Request rate**: `rate(ironsight_http_requests_total[5m])` grouped by `route`
- **Error rate**: `rate(ironsight_http_requests_total{status=~"5.."}[5m])`
- **P99 latency**: `histogram_quantile(0.99, rate(ironsight_http_request_duration_seconds_bucket[5m]))`
- **Active cameras**: `ironsight_recording_active_cameras`
- **Segment write rate**: `rate(ironsight_recording_segments_written_total[5m])`
- **DB pool saturation**: `ironsight_db_pool_total - ironsight_db_pool_idle`
- **WS clients**: `ironsight_ws_clients_connected`
- **RBAC errors**: `rate(ironsight_rbac_cache_refresh_errors_total[5m])`
