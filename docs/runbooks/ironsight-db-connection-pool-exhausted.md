# Alert: IronsightDBPoolExhausted

## Severity
critical

## What fired

`ironsight_db_pool_idle == 0 and ironsight_db_pool_acquire_count > 0` for
2 minutes. The pgxpool (MaxConns=50) has zero idle connections while the
application is trying to acquire more. Every new query is blocking, causing
cascading latency across all API routes.

## Immediate actions

1. Check current connections: `docker exec ironsight-db psql -U ironsight -c "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"`
2. Look for long-running queries blocking connections:
   `docker exec ironsight-db psql -U ironsight -c "SELECT pid, now()-query_start as duration, query FROM pg_stat_activity WHERE state='active' ORDER BY query_start LIMIT 10;"`
3. If a query has been running for >5 minutes: `SELECT pg_terminate_backend(<pid>);`
4. Check for transaction leaks: `SELECT pid, now()-state_change, query FROM pg_stat_activity WHERE state='idle in transaction' ORDER BY state_change;`

## Likely causes

- Long-running AI metric sampler or VLM indexer query holding connections
- Export worker stuck in a transaction
- N+1 query loop (rare but possible during bulk operations)
- Postgres `idle_in_transaction_session_timeout` not set (unbounded waits)

## Resolution

Pool is exhausted when `ironsight_db_pool_idle == 0`. Alert resolves when
idle connections return above 0. Watch `ironsight_db_pool_idle` in Grafana.

## Escalation

If the pool remains exhausted for more than 10 minutes and terminating long
queries doesn't help, restart the Ironsight container:
`docker restart ironsight` (brief service interruption). Contact Caleb if
the exhaustion recurs within 1 hour.
