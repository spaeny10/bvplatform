# Ironsight runbooks

Operational response guides for Prometheus alerts. Each runbook corresponds to
one or more alert rules in `deploy/monitoring/alerts.yml`.

## Metric → alert → runbook pipeline

```
ironsight binary
    |
    |-- standard Prometheus series (P1-C-03)
    |   ironsight_http_requests_total
    |   ironsight_recording_active_cameras
    |   ironsight_db_pool_idle
    |   ironsight_ws_clients_connected
    |   ... (12 series total, see docs/metrics.md)
    |
    |-- discrete one-shot events (P1-C-04)
    |   metrics.SetCustomAlert(name, severity, msg)
    |   → ironsight_app_alert_total{name, severity}
    |
    v
Prometheus (scrapes /metrics every 15s, evaluates alerts.yml every 15s)
    |
    v
Alertmanager (groups, deduplicates, routes, inhibits)
    |
    v
ntfy.sh push notification → phone
    |
    v
Runbook (this directory)
    |
    v
Operator action
```

## Runbook index

| File | Alert(s) | Severity |
|---|---|---|
| `ironsight-api-down.md` | `IronsightAPIDown`, `IronsightSlowRequests`, `IronsightAppAlert` | critical / warning |
| `ironsight-recording-gap.md` | `IronsightRecordingStalled` | critical |
| `ironsight-camera-offline.md` | `IronsightCameraDropoff` | warning |
| `ironsight-ffmpeg-subprocess-crash.md` | `IronsightFFmpegMismatch` | warning |
| `ironsight-db-connection-pool-exhausted.md` | `IronsightDBPoolExhausted` | critical |
| `ironsight-ws-clients-disconnecting.md` | `IronsightWSClientsLost`, `IronsightHighErrorRate` | critical / warning |
| `ironsight-rbac-deny-all.md` | `IronsightRBACRefreshErrors` | warning |
| `ironsight-migration-failed.md` | `IronsightMigrationBehind` | warning |
| `ironsight-frontend-build-fail-deploy.md` | deploy-related failures | — |

## Runbook format

Each runbook follows this structure:

```
# Alert: <AlertName>

## Severity
critical | warning

## What fired
One-paragraph description of what the alert measures and why it fired.

## Immediate actions
Numbered checklist of first-response steps.

## Likely causes
Bullet list of common root causes.

## Resolution
How to confirm the alert has resolved.

## Escalation
Who to contact if first-response steps don't resolve the issue.
```

## Alert catalog

The full alert catalog with expressions, thresholds, and runbook cross-links
is in [`docs/alerting.md`](../alerting.md).
