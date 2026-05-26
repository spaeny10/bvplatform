# Alert: IronsightRBACRefreshErrors

## Severity
warning

## What fired

`rate(ironsight_rbac_cache_refresh_errors_total[10m]) > 0` for 10 minutes.
The RBAC background refresher is encountering database errors and falling
back to the previously cached allow-set. Clients may see stale camera
permissions until the DB recovers.

## Immediate actions

1. Check RBAC logs: `docker logs ironsight --tail 200 2>&1 | grep -i "rbac\|refresh"`
2. Look for "RBAC refresh error" lines with the underlying error (usually a
   pgx/postgres error).
3. Check DB health: `docker exec ironsight-db psql -U ironsight -c "SELECT NOW();"`
4. If the DB is healthy, check whether the refresher is hitting a specific
   slow query: look for `ironsight_db_pool_idle == 0` coinciding with the
   RBAC errors.

## Likely causes

- PostgreSQL temporary outage or slow query blocking the RBAC refresh query
- DB pool exhaustion causing acquire timeouts in the refresher goroutine
- A schema migration dropped or renamed a table the RBAC query touches

## Impact

While RBAC refresh errors persist, clients see the last-good cached allow-set.
For most deployments this is acceptable for 10–30 minutes. The refresher
runs every ~30 seconds and retries automatically.

## Resolution

Alert resolves when `rate(rbac_cache_refresh_errors_total[10m]) == 0`. The
refresher does not need a restart — it retries on the next cycle automatically.

## Escalation

If RBAC errors persist for more than 1 hour and the DB appears healthy, the
issue may be a schema change that broke the refresh query. Contact Caleb.
