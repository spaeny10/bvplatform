# Runbook: Frontend build / deploy failures

This runbook covers deploy-time failures that aren't covered by a specific
Prometheus alert — e.g., a failed Docker build, an NPM proxy misconfiguration,
or a stale frontend bundle.

## Symptoms

- API returns correct data but the browser shows a blank page or an outdated UI
- Docker build fails with a Go compilation error or an npm build error
- NPM proxy returns 502 for the frontend or API

## Immediate actions

**Blank page / stale bundle**:
1. Force-reload the browser (Ctrl+Shift+R or Cmd+Shift+R).
2. Check NPM proxy: verify the frontend location block points at the correct
   container port.
3. Check the static files: `docker exec ironsight ls /app/frontend/dist/`
   — if empty or missing, the frontend build step failed.

**Docker build failure**:
1. Check the build log: `docker build . 2>&1 | tail -50`
2. For Go compile errors: `go build ./...` locally first.
3. For npm errors: `cd frontend && npm ci && npm run build` locally first.

**NPM proxy 502**:
1. Verify the Ironsight container is running: `docker ps | grep ironsight`
2. Verify the port binding: `docker port ironsight`
3. Check NPM proxy config for the correct upstream address and port.

## Recovery

After a failed deploy, roll back to the previous container image:
```bash
docker stop ironsight
docker run -d --name ironsight <previous-image-tag> ...
```

## Escalation

Contact Caleb for deploy script failures or NPM proxy configuration changes.
