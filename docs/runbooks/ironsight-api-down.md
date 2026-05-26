# Alert: IronsightAPIDown / IronsightSlowRequests / IronsightAppAlert

## Severity
`IronsightAPIDown` — critical  
`IronsightSlowRequests` — warning  
`IronsightAppAlert` — critical or warning (varies by event)

## What fired

**IronsightAPIDown**: Prometheus cannot scrape the `/metrics` endpoint on the
Ironsight API server. The `up{job="ironsight-api"}` target has been `0` for
more than 2 minutes. This covers: process crash, container restart loop, network
partition, NPM misconfiguration, or the API server simply not responding.

**IronsightSlowRequests**: The p95 response time for at least one route has
exceeded 2 seconds for 10 consecutive minutes.

**IronsightAppAlert**: A discrete application event was emitted via
`metrics.SetCustomAlert`. Common sources: goose migration failure,
camera credential encrypt failure.

## Immediate actions (IronsightAPIDown)

1. SSH to fred: `ssh fred`
2. Check container status: `docker ps | grep ironsight`
3. If container is down/restarting: `docker logs ironsight --tail 100`
4. Look for: `[FATAL]`, `panic:`, `OOM killed` in logs.
5. If startup is looping: check `[MIGRATIONS]` and `[DB]` lines — a bad
   migration or DB outage will prevent startup.
6. Verify NPM proxy is up: `docker ps | grep npm`
7. If NPM is healthy but scrape still fails: check the bearer token file on
   the Prom LXC: `cat /etc/prometheus/ironsight_bearer_token` is non-empty
   and not expired.

## Immediate actions (IronsightSlowRequests)

1. Check DB pool: `ironsight_db_pool_idle` in Grafana — if 0, see
   `ironsight-db-connection-pool-exhausted.md`.
2. Check for a slow query: SSH to fred, run `docker exec ironsight-db psql -U ironsight -c "SELECT pid, now()-query_start, query FROM pg_stat_activity WHERE state='active' ORDER BY query_start LIMIT 10;"`.
3. Check Qwen/YOLO sidecar health: `curl http://localhost:8502/health` from fred.

## Immediate actions (IronsightAppAlert)

1. Check structured logs for the alert name label:
   `docker logs ironsight --tail 200 2>&1 | grep <alert-name>`
2. For `goose_migration_failure`: run `docker exec ironsight /app/migrate up`
   and check for SQL errors.
3. For `camera_credentials_encrypt_failure`: verify `CAMERA_CREDENTIALS_KEY`
   env is set correctly in the `.env` file.

## Likely causes

- Process OOM-killed (check `dmesg | grep -i oom` on fred)
- PostgreSQL connection refused (check DB container)
- NFT/iptables rule blocking port 8080 (unlikely on fred)
- Prometheus bearer token expired or missing

## Resolution

Alert auto-resolves when `up{job="ironsight-api"} == 1` for one scrape
interval (15s). Verify in Grafana or:
```
curl -s http://<prom-lxc>:9090/api/v1/query?query=up{job="ironsight-api"}
```

## Escalation

If the API is down for more than 15 minutes and you cannot identify the cause,
contact Caleb (caleb@jetstreamsys.com).
