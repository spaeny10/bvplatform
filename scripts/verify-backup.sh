#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────
# Ironsight — backup verification
#
# A backup that has never been restored is theatre. This script
# proves a backup actually round-trips by:
#
#   1. Picking the most recent backup (or one passed as $1).
#   2. Spinning up a throwaway TimescaleDB container.
#   3. Restoring the dump into that container.
#   4. Running a small set of sanity queries.
#   5. Tearing the container down.
#
# Designed to run weekly from cron — the day after the weekly
# backup is promoted is a good slot:
#
#   30 3 * * 1  /path/to/scripts/verify-backup.sh
#
# Output goes to stdout; pair with cron MAILTO or a `tee` to file.
#
# Exit codes:
#   0  verification passed
#   1  no backup found, container failed to start, or restore failed
#   2  restore succeeded but sanity queries returned implausible values
# ─────────────────────────────────────────────────────────────────

set -euo pipefail

BACKUP_DIR="${IRONSIGHT_BACKUP_DIR:-./backups}"
DB_USER="${IRONSIGHT_DB_USER:-onvif}"
DB_NAME="${IRONSIGHT_DB_NAME:-onvif_tool}"
DB_PASSWORD="${IRONSIGHT_DB_PASSWORD:-verify_only_throwaway}"
SANDBOX_CONTAINER="ironsight-backup-verify-$$"
SANDBOX_PORT="${IRONSIGHT_VERIFY_PORT:-55432}"
IMAGE="${IRONSIGHT_DB_IMAGE:-docker.io/timescale/timescaledb:latest-pg15}"

log() { printf '[%s] %s\n' "$(date +%Y-%m-%dT%H:%M:%S%z)" "$*"; }

cleanup() {
  if docker ps -a --filter "name=^${SANDBOX_CONTAINER}$" --format '{{.Names}}' | grep -q "^${SANDBOX_CONTAINER}$"; then
    log "Cleaning up sandbox container ${SANDBOX_CONTAINER}"
    docker rm -f "$SANDBOX_CONTAINER" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# ── 1. Choose backup ────────────────────────────────────────────
if [ "$#" -ge 1 ] && [ -n "$1" ]; then
  BACKUP="$1"
else
  BACKUP=$(ls -1t "$BACKUP_DIR"/daily/*.dump 2>/dev/null | head -n 1 || true)
fi

if [ -z "${BACKUP:-}" ] || ! [ -f "$BACKUP" ]; then
  log "ERROR: no backup file found (looked in $BACKUP_DIR/daily/*.dump)"
  exit 1
fi

size=$(stat -c '%s' "$BACKUP" 2>/dev/null || stat -f '%z' "$BACKUP")
log "Verifying $BACKUP ($(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size} bytes"))"

# ── 2. Spin up sandbox ──────────────────────────────────────────
log "Starting sandbox container ${SANDBOX_CONTAINER} on host port ${SANDBOX_PORT}"
docker run -d --rm \
  --name "$SANDBOX_CONTAINER" \
  -e POSTGRES_USER="$DB_USER" \
  -e POSTGRES_DB="$DB_NAME" \
  -e POSTGRES_PASSWORD="$DB_PASSWORD" \
  -p "127.0.0.1:${SANDBOX_PORT}:5432" \
  "$IMAGE" >/dev/null

# Wait for readiness — pg_isready inside the container.
for _ in $(seq 1 30); do
  if docker exec "$SANDBOX_CONTAINER" pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! docker exec "$SANDBOX_CONTAINER" pg_isready -U "$DB_USER" -d "$DB_NAME" >/dev/null 2>&1; then
  log "ERROR: sandbox DB never became ready"
  exit 1
fi
log "Sandbox ready"

# ── 3. Restore ──────────────────────────────────────────────────
log "Restoring backup into sandbox"
if ! docker exec -i "$SANDBOX_CONTAINER" \
    pg_restore -U "$DB_USER" -d "$DB_NAME" --no-owner --no-privileges --clean --if-exists \
    < "$BACKUP" 2>/tmp/verify-restore-stderr-$$; then
  # pg_restore often emits a few non-fatal NOTICEs about extensions or
  # ownership. We tolerate those, but we also surface them so a human
  # can scan the output if curiosity strikes.
  if grep -qiE 'fatal|could not' /tmp/verify-restore-stderr-$$; then
    log "ERROR: pg_restore reported fatal errors:"
    cat /tmp/verify-restore-stderr-$$
    rm -f /tmp/verify-restore-stderr-$$
    exit 1
  fi
fi
rm -f /tmp/verify-restore-stderr-$$
log "Restore completed"

# ── 4. Sanity queries ───────────────────────────────────────────
# These are deliberately structural ("are the tables we expect
# present?") rather than data-shape ("are there at least N events?").
# A fresh customer might legitimately have zero security events —
# that's not a backup failure.
required_tables=(
  users
  organizations
  sites
  cameras
  audit_log
  security_events
)

failed=0
for t in "${required_tables[@]}"; do
  count=$(docker exec -i "$SANDBOX_CONTAINER" \
    psql -U "$DB_USER" -d "$DB_NAME" -tAc \
    "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='${t}';" 2>/dev/null || echo "0")
  count=$(echo "$count" | tr -d '[:space:]')
  if [ "$count" != "1" ]; then
    log "FAIL: table '${t}' missing from restored DB"
    failed=1
  fi
done

# Extra: confirm the audit_log append-only trigger is present.
trig=$(docker exec -i "$SANDBOX_CONTAINER" \
  psql -U "$DB_USER" -d "$DB_NAME" -tAc \
  "SELECT count(*) FROM pg_trigger WHERE tgrelid = 'audit_log'::regclass AND NOT tgisinternal;" 2>/dev/null || echo "0")
trig=$(echo "$trig" | tr -d '[:space:]')
if [ "$trig" = "0" ]; then
  log "WARN: audit_log has no user trigger after restore (may be expected on fresh systems)"
fi

if [ "$failed" -ne 0 ]; then
  log "Verification FAILED — required tables missing"
  exit 2
fi

log "Verification PASSED — all required tables restored"
