# Ironsight Monitoring Config

This directory holds the operational config for Prometheus, Alertmanager, and
Grafana for the Ironsight API server. These files are scraped from this
directory and synced to the Prometheus LXC via `rsync` (manually for now, or
via a future CD pipeline).

## Files

| File | Purpose |
|---|---|
| `prometheus.yml` | Prom scrape config — scrapes `/metrics` from fred's Ironsight API |
| `alerts.yml` | Prometheus alerting rules — 11 rules covering API, recording, DB, WS, RBAC |
| `alertmanager.yml` | Alertmanager routing — sends all alerts to ntfy via webhook receiver |
| `ntfy-template.tpl` | Go template for ntfy message body and headers |
| `DEPLOY.md` | Step-by-step LXC provisioning guide for platform-ops |

## Alert → runbook mapping

Every alert in `alerts.yml` carries a `runbook_url` annotation pointing at the
corresponding runbook in `docs/runbooks/`. The full catalog is in
`docs/alerting.md`.

Runbook paths (relative to repo root):

```
docs/runbooks/ironsight-api-down.md
docs/runbooks/ironsight-frontend-build-fail-deploy.md
docs/runbooks/ironsight-recording-gap.md
docs/runbooks/ironsight-camera-offline.md
docs/runbooks/ironsight-ffmpeg-subprocess-crash.md
docs/runbooks/ironsight-db-connection-pool-exhausted.md
docs/runbooks/ironsight-ws-clients-disconnecting.md
docs/runbooks/ironsight-rbac-deny-all.md
docs/runbooks/ironsight-migration-failed.md
```

## Bearer token for scraping

The `/metrics` endpoint is SSO-gated by default (`METRICS_AUTH=sso`). The Prom
LXC needs a long-lived Ironsight service-account JWT to authenticate scrapes.

**How to populate the token file** (run on the Prom LXC after provisioning):

```bash
# 1. Generate a service-account JWT from the Ironsight API
#    (POST /api/auth/login with the metrics service account credentials,
#    copy the token from the response)

# 2. Write it to the file that prometheus.yml references
echo -n "<paste_token_here>" > /etc/prometheus/ironsight_bearer_token
chmod 600 /etc/prometheus/ironsight_bearer_token
chown prometheus:prometheus /etc/prometheus/ironsight_bearer_token
```

Alternatively set `METRICS_AUTH=none` on the Ironsight API *and* configure
NPM to restrict `/metrics` to the cluster's internal CIDR — then remove the
`bearer_token_file` stanza from `prometheus.yml`.

## ntfy topic

The ntfy topic in `alertmanager.yml` is a placeholder UUID. Replace it with a
private topic before deploying:

```bash
# Generate a fresh random topic (opaque, unguessable)
openssl rand -hex 16
# Example output: 3f8a1c2e9d4b7f0e5a2c1d3e8f6b9a0c
```

Update both `alertmanager.yml` (the `url:` line under `ntfy-caleb`) and
subscribe to the same topic on the ntfy app on your phone.

Subscribe: `https://ntfy.sh/<your-topic>` in the ntfy iOS/Android app.

## Runbook URL template variable

Alert `runbook_url` annotations currently use the local file path:
`file:///etc/runbooks/ironsight-<topic>.md`

When the runbooks are published to a web URL (e.g. hosted on the internal wiki
or GitHub Pages), update the `RUNBOOK_BASE_URL` comment in `alerts.yml` and
replace the prefix in all `runbook_url` values. A simple sed one-liner:

```bash
sed -i 's|file:///etc/runbooks|https://docs.internal/runbooks|g' alerts.yml
```
