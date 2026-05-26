# Alert: IronsightWSClientsLost / IronsightHighErrorRate

## Severity
`IronsightWSClientsLost` — warning  
`IronsightHighErrorRate` — critical

## What fired

**IronsightWSClientsLost**: `delta(ironsight_ws_clients_connected[5m]) <= -5`
for 2 minutes. Five or more WebSocket clients disconnected within a 5-minute
window.

**IronsightHighErrorRate**: More than 5% of requests on at least one route
returned 5xx responses over the last 5 minutes.

## Immediate actions (WSClientsLost)

1. Check the WS hub logs: `docker logs ironsight --tail 200 2>&1 | grep -i "hub\|ws\|websocket"`
2. Look for "panic" or "broadcast error" lines.
3. If Redis is in use (`REDIS_URL` set), check the Redis bridge:
   `docker logs ironsight --tail 200 2>&1 | grep -i "redis\|bridge"`
4. Verify the NPM WebSocket proxy is still connected (check NPM logs for
   `502 Bad Gateway` on the `/ws` location).

## Immediate actions (HighErrorRate)

1. Identify the failing route from the `route` label in the alert.
2. Check structured logs: `docker logs ironsight --tail 500 2>&1 | grep -i "error\|panic\|500"`
3. Check DB pool: `ironsight_db_pool_idle` — pool exhaustion causes 5xx on
   any DB-touching route.
4. Check AI sidecar health if the route is AI-related (`/api/events/*/analyze`).

## Likely causes

- Hub goroutine panic (all clients drop at once)
- Network interruption between clients and NPM proxy
- Redis pub/sub bridge failure (multi-replica deploys)
- DB error causing handlers to return 500

## Resolution

WSClientsLost: alert resolves when the client count stabilizes. Clients
reconnect automatically (the frontend uses exponential backoff).

HighErrorRate: alert resolves when the 5xx rate drops below 5% for 5 minutes.
Check Grafana for the error rate trend.

## Escalation

If WS clients are not reconnecting within 5 minutes or 5xx rates remain
elevated: restart the Ironsight container. Contact Caleb if the issue
recurs.
