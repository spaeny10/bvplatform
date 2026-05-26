# Deploy Guide — Ironsight Monitoring LXC

Platform-ops reference for standing up the Prometheus + Grafana + Alertmanager
LXC that consumes `deploy/monitoring/`. The Ironsight application and its
container on fred are out of scope — this guide only covers the monitoring
infrastructure.

## Prerequisites

- Proxmox host with available storage and bridge network
- LXC template: Ubuntu 24.04 (or Debian 12)
- SSH access to the Proxmox host
- The ntfy app installed on Caleb's phone (iOS/Android)
- A private ntfy topic generated with `openssl rand -hex 16`

## Step 1 — Create the LXC

```bash
# On the Proxmox host. Adjust ctid, storage, bridge, and IP to your environment.
pct create 200 local:vztmpl/ubuntu-24.04-standard_24.04-1_amd64.tar.zst \
  --hostname ironsight-mon \
  --cores 2 \
  --memory 2048 \
  --rootfs local-lvm:16 \
  --net0 name=eth0,bridge=vmbr0,ip=192.168.103.60/24,gw=192.168.103.1 \
  --nameserver 1.1.1.1 \
  --start 1 \
  --unprivileged 1

# Start and enter the container
pct start 200
pct enter 200
```

## Step 2 — Install software

```bash
apt update && apt install -y curl wget gnupg2 apt-transport-https software-properties-common

# Prometheus
PROM_VER=2.52.0
wget -qO- https://github.com/prometheus/prometheus/releases/download/v${PROM_VER}/prometheus-${PROM_VER}.linux-amd64.tar.gz \
  | tar -xz -C /usr/local/bin --strip-components=1 prometheus-${PROM_VER}.linux-amd64/prometheus prometheus-${PROM_VER}.linux-amd64/promtool
mkdir -p /etc/prometheus /var/lib/prometheus
useradd --no-create-home --shell /bin/false prometheus
chown -R prometheus:prometheus /etc/prometheus /var/lib/prometheus

# Alertmanager
AM_VER=0.27.0
wget -qO- https://github.com/prometheus/alertmanager/releases/download/v${AM_VER}/alertmanager-${AM_VER}.linux-amd64.tar.gz \
  | tar -xz -C /usr/local/bin --strip-components=1 alertmanager-${AM_VER}.linux-amd64/alertmanager alertmanager-${AM_VER}.linux-amd64/amtool
mkdir -p /etc/alertmanager /var/lib/alertmanager
useradd --no-create-home --shell /bin/false alertmanager
chown -R alertmanager:alertmanager /etc/alertmanager /var/lib/alertmanager

# Grafana
apt install -y adduser libfontconfig1
wget -qO /tmp/grafana.deb https://dl.grafana.com/oss/release/grafana_10.4.3_amd64.deb
dpkg -i /tmp/grafana.deb
systemctl enable grafana-server
```

## Step 3 — Sync monitoring config from repo

Run this from the bigview-platform repo on your workstation:

```bash
# Replace <MON_LXC_IP> with the LXC's IP (e.g. 192.168.103.60)
MON_LXC_IP=192.168.103.60

rsync -av --checksum \
  deploy/monitoring/prometheus.yml \
  deploy/monitoring/alerts.yml \
  root@${MON_LXC_IP}:/etc/prometheus/

rsync -av --checksum \
  deploy/monitoring/alertmanager.yml \
  deploy/monitoring/ntfy-template.tpl \
  root@${MON_LXC_IP}:/etc/alertmanager/
```

## Step 4 — Replace the ntfy topic placeholder

On the monitoring LXC:

```bash
# Generate a fresh private topic
NTFY_TOPIC=$(openssl rand -hex 16)
echo "Your ntfy topic: ${NTFY_TOPIC}"
echo "Subscribe in the ntfy app: https://ntfy.sh/${NTFY_TOPIC}"

# Replace the placeholder in alertmanager.yml
sed -i "s|3f8a1c2e9d4b7f0e5a2c1d3e8f6b9a0c|${NTFY_TOPIC}|g" \
  /etc/alertmanager/alertmanager.yml
```

Subscribe to the topic on your phone: open the ntfy app, tap "+", enter
`https://ntfy.sh/<NTFY_TOPIC>`.

## Step 5 — Populate the Ironsight scrape bearer token

On the monitoring LXC:

```bash
# Option A: JWT from a service-account login (recommended)
# 1. POST to Ironsight login endpoint with a dedicated metrics service account:
curl -s -X POST https://ironsight.bigviewsecurity.com/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"metrics-svc","password":"<service-account-password>"}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])"

# 2. Write the token to the credentials file
echo -n "<paste_token_here>" > /etc/prometheus/ironsight_bearer_token
chmod 600 /etc/prometheus/ironsight_bearer_token
chown prometheus:prometheus /etc/prometheus/ironsight_bearer_token

# Option B: Set METRICS_AUTH=none on the Ironsight API + NPM CIDR restrict /metrics
# Then remove the authorization block from /etc/prometheus/prometheus.yml.
```

## Step 6 — Create systemd unit files

```bash
# Prometheus
cat > /etc/systemd/system/prometheus.service << 'EOF'
[Unit]
Description=Prometheus
After=network.target

[Service]
User=prometheus
Group=prometheus
Type=simple
ExecStart=/usr/local/bin/prometheus \
  --config.file=/etc/prometheus/prometheus.yml \
  --storage.tsdb.path=/var/lib/prometheus \
  --storage.tsdb.retention.time=30d \
  --web.listen-address=:9090
Restart=always

[Install]
WantedBy=multi-user.target
EOF

# Alertmanager
cat > /etc/systemd/system/alertmanager.service << 'EOF'
[Unit]
Description=Alertmanager
After=network.target

[Service]
User=alertmanager
Group=alertmanager
Type=simple
ExecStart=/usr/local/bin/alertmanager \
  --config.file=/etc/alertmanager/alertmanager.yml \
  --storage.path=/var/lib/alertmanager \
  --web.listen-address=:9093 \
  --template /etc/alertmanager/ntfy-template.tpl
Restart=always

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable prometheus alertmanager
systemctl start prometheus alertmanager grafana-server
```

## Step 7 — Verify services are running

```bash
systemctl status prometheus alertmanager grafana-server

# Prom targets — ironsight-api should show state=UP
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool | grep -A5 '"health"'

# Alertmanager config — should show no errors
amtool check-config /etc/alertmanager/alertmanager.yml

# Prometheus rule check
promtool check rules /etc/prometheus/alerts.yml
```

## Step 8 — Trigger a test alert and verify ntfy delivery

```bash
# Send a test alert through alertmanager via amtool
amtool --alertmanager.url=http://localhost:9093 alert add \
  alertname=TestAlert \
  severity=warning \
  job=ironsight-api \
  --annotation summary="Test alert from amtool — P1-C-04 verification" \
  --annotation description="If you see this on your phone, ntfy delivery is working." \
  --annotation runbook_url="file:///etc/runbooks/ironsight-api-down.md"
```

Check your phone for the ntfy notification within 30 seconds.

## Step 9 — Grafana initial setup

1. Browse to `http://<MON_LXC_IP>:3000` (default login: admin / admin)
2. Change the admin password immediately
3. Add Prometheus as a data source: `http://localhost:9090`
4. Import the Ironsight dashboard (JSON coming in a future commit) or
   manually create panels from the queries in `docs/metrics.md`

## Ongoing operations

- **Restart after config change**: `systemctl reload prometheus` (hot-reload)
  or `systemctl reload alertmanager` (hot-reload via SIGHUP)
- **After a new migration**: update the threshold in `/etc/prometheus/alerts.yml`
  `IronsightMigrationBehind` expr, then reload Prometheus
- **After bearer token rotation**: update the service account JWT in
  `/etc/prometheus/ironsight_bearer_token`, then `systemctl reload prometheus`
- **Logs**: `journalctl -u prometheus -f` / `journalctl -u alertmanager -f`
