# Ironsight alert catalog

Alerting pipeline: `Prometheus` → `Alertmanager` → `ntfy` → phone.

Decision context: D-03 (2026-05-26) — alertmanager + ntfy. Runbook home D-05 (2026-05-26) — canonical tree at `bigview-platform/docs/runbooks/` (outside this repo). The alertmanager LXC mounts that tree at `/etc/runbooks/`; `runbook_url` in alerts.yml uses `file:///etc/runbooks/<name>.md`.

Config files: [`deploy/monitoring/alerts.yml`](../deploy/monitoring/alerts.yml),
[`deploy/monitoring/alertmanager.yml`](../deploy/monitoring/alertmanager.yml).  
LXC provisioning guide: [`deploy/monitoring/DEPLOY.md`](../deploy/monitoring/DEPLOY.md).

---

## Alert catalog

| Alert name | Expression (abbreviated) | `for` | Severity | Runbook |
|---|---|---|---|---|
| `IronsightAPIDown` | `up{job="ironsight-api"} == 0` | 2m | **critical** | `ironsight-api-down.md` |
| `IronsightHighErrorRate` | `rate(5xx[5m]) / rate(all[5m]) > 0.05` | 5m | **critical** | `ironsight-ws-clients-disconnecting.md` |
| `IronsightSlowRequests` | `histogram_quantile(0.95, ...) > 2.0` | 10m | warning | `ironsight-api-down.md` |
| `IronsightRecordingStalled` | `rate(segments_written[5m]) == 0 and active_cameras > 0` | 5m | **critical** | `ironsight-recording-gap.md` |
| `IronsightCameraDropoff` | `delta(active_cameras[10m]) <= -2` | 5m | warning | `ironsight-camera-offline.md` |
| `IronsightFFmpegMismatch` | `active_cameras != ffmpeg_subprocesses` | 5m | warning | `ironsight-ffmpeg-subprocess-crash.md` |
| `IronsightDBPoolExhausted` | `db_pool_idle == 0 and db_pool_acquire_count > 0` | 2m | **critical** | `ironsight-db-connection-pool-exhausted.md` |
| `IronsightWSClientsLost` | `delta(ws_clients_connected[5m]) <= -5` | 2m | warning | `ironsight-ws-clients-disconnecting.md` |
| `IronsightRBACRefreshErrors` | `rate(rbac_refresh_errors[10m]) > 0` | 10m | warning | `ironsight-rbac-deny-all.md` |
| `IronsightMigrationBehind` | `goose_migration_version < <CURRENT_VERSION>` | 5m | warning | `ironsight-migration-failed.md` |
| `IronsightAppAlert` | `increase(app_alert_total{severity="critical"}[5m]) > 0` | 0s | **critical** | `ironsight-api-down.md` |
| `IronsightAppAlertWarning` | `increase(app_alert_total{severity="warning"}[5m]) > 0` | 0s | warning | `ironsight-api-down.md` |

---

## Alert pipeline

```
Prometheus scrapes /metrics every 15s
        |
        | evaluates alerts.yml rules every 15s
        v
Alertmanager (port 9093 on monitoring LXC)
        |
        | routes all alerts to receiver: ntfy-caleb
        | grouping: [alertname, route], group_wait: 30s
        | critical: repeat every 4h
        | warning:  repeat every 12h
        | inhibition: critical suppresses same-name warnings
        v
ntfy.sh/<private-topic> (HTTPS POST webhook)
        |
        v
ntfy app on Caleb's phone
```

---

## Metric → alert → runbook pipeline

```
ironsight binary emits metric
        |
        |-- standard gauges/counters (P1-C-03 series)
        |       recorded automatically by middleware / engine hooks
        |
        |-- discrete one-shot events (P1-C-04 SetCustomAlert)
        |       call: metrics.SetCustomAlert(name, severity, msg)
        |       increments: ironsight_app_alert_total{name, severity}
        |
        v
Prometheus scrapes /metrics → evaluates alerting rules
        |
        v
Alertmanager routes alert → ntfy push notification
        |
        v
Runbook: docs/runbooks/ironsight-<topic>.md
```

---

## Severity definitions

| Severity | Meaning | repeat_interval | Examples |
|---|---|---|---|
| **critical** | Immediate operator action required. Service is degraded or down for users. | 4h | API down, DB pool exhausted, recording stalled |
| warning | Investigate soon. Service is running but showing signs of stress. | 12h | Camera dropoff, RBAC errors, slow requests |

---

## Inhibition rules

A `critical` alert suppresses `warning` alerts for the same `alertname`. This
prevents alert floods when e.g. `IronsightAPIDown` (critical) fires while
`IronsightHighErrorRate` (which would auto-resolve) is still pending.

---

## Runbooks cross-reference

Runbooks live at `docs/runbooks/ironsight-<topic>.md`. Each alert's
`runbook_url` annotation points at the corresponding file. The full list:

- `ironsight-api-down.md` — API scrape unreachable
- `ironsight-recording-gap.md` — segment write stall
- `ironsight-camera-offline.md` — camera dropoff
- `ironsight-ffmpeg-subprocess-crash.md` — FFmpeg process died
- `ironsight-db-connection-pool-exhausted.md` — Postgres pool full
- `ironsight-ws-clients-disconnecting.md` — WebSocket mass disconnect / high error rate
- `ironsight-rbac-deny-all.md` — RBAC cache refresh errors
- `ironsight-migration-failed.md` — goose schema version lag
- `ironsight-frontend-build-fail-deploy.md` — deploy-related failures

---

## SetCustomAlert call sites (P1-C-04)

These are the discrete one-shot events currently wired:

| Name label | Severity | Source file | Trigger |
|---|---|---|---|
| `goose_migration_failure` | critical | `cmd/server/main.go` | `goose.UpContext` returns non-nil |
| `ffmpeg_subprocess_crash` | warning | `internal/recording/engine.go` | FFmpeg exits non-zero (non-context-cancel) |
| `camera_credentials_encrypt_failure` | critical | `cmd/server/camera_creds_sweep.go` | AES-GCM encrypt fails during boot sweep |

---

## Adding a new alert

1. Define the Prometheus expression in `deploy/monitoring/alerts.yml` — follow
   the existing rule structure (alert name, expr, for, labels.severity,
   annotations.summary + description + runbook_url).
2. Add a runbook at `docs/runbooks/ironsight-<topic>.md`.
3. Add a row to the catalog table above.
4. If the condition is a discrete one-shot event (not a metric that naturally
   accumulates), add a `metrics.SetCustomAlert(name, severity, msg)` call at
   the event source and add a row to the SetCustomAlert call-sites table.
5. After deploy, reload Prometheus: `systemctl reload prometheus` on the
   monitoring LXC.
