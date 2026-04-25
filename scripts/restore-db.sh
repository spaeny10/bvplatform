#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────
# Ironsight — restore database from a backup file
#
# DESTRUCTIVE. This drops and recreates the target database before
# restoring. Confirm twice before running against production.
#
# Usage:
#   ./restore-db.sh <backup-file>
#   ./restore-db.sh ./backups/daily/ironsight-20260425-020000.dump
#
# Configuration via environment (same defaults as backup-db.sh):
#   IRONSIGHT_DB_CONTAINER  default: ironsight-db
#   IRONSIGHT_DB_NAME       default: onvif_tool
#   IRONSIGHT_DB_USER       default: onvif
#
# Confirmation: prompts unless IRONSIGHT_RESTORE_CONFIRM=YES is
# exported (used by automated verify-backup.sh which already
# operates against a sandbox container).
# ─────────────────────────────────────────────────────────────────

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <backup-file>" >&2
  exit 64
fi

BACKUP_FILE="$1"
CONTAINER="${IRONSIGHT_DB_CONTAINER:-ironsight-db}"
DB_NAME="${IRONSIGHT_DB_NAME:-onvif_tool}"
DB_USER="${IRONSIGHT_DB_USER:-onvif}"

log() { printf '[%s] %s\n' "$(date +%Y-%m-%dT%H:%M:%S%z)" "$*"; }

# ── 1. File checks ───────────────────────────────────────────────
if ! [ -f "$BACKUP_FILE" ]; then
  log "ERROR: backup file not found: $BACKUP_FILE"
  exit 1
fi
size=$(stat -c '%s' "$BACKUP_FILE" 2>/dev/null || stat -f '%z' "$BACKUP_FILE")
if [ "$size" -lt 1024 ]; then
  log "ERROR: backup file is suspiciously small (${size} bytes); refusing"
  exit 1
fi

# ── 2. Container check ───────────────────────────────────────────
if ! docker ps --filter "name=^${CONTAINER}$" --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
  log "ERROR: container '${CONTAINER}' is not running"
  exit 1
fi

# ── 3. Confirmation ──────────────────────────────────────────────
if [ "${IRONSIGHT_RESTORE_CONFIRM:-}" != "YES" ]; then
  echo "ABOUT TO DROP AND RESTORE database '${DB_NAME}' in container '${CONTAINER}'."
  echo "Source: $BACKUP_FILE ($(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size} bytes"))"
  echo "All current data in this database will be PERMANENTLY LOST."
  read -r -p "Type the database name to confirm: " confirm
  if [ "$confirm" != "$DB_NAME" ]; then
    log "Aborted — confirmation did not match"
    exit 1
  fi
fi

# ── 4. Stop API/worker connections (best-effort) ─────────────────
# We don't auto-stop the API; an admin should do that explicitly
# during a real restore. This loop just kills idle backends so the
# DROP DATABASE doesn't fight live sessions.
log "Terminating idle connections to ${DB_NAME}"
docker exec -i "$CONTAINER" psql -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 <<SQL >/dev/null
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '${DB_NAME}' AND pid <> pg_backend_pid();
SQL

# ── 5. Drop + recreate ───────────────────────────────────────────
log "Dropping and recreating ${DB_NAME}"
docker exec -i "$CONTAINER" psql -U "$DB_USER" -d postgres -v ON_ERROR_STOP=1 <<SQL
DROP DATABASE IF EXISTS ${DB_NAME};
CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};
SQL

# ── 6. Restore ───────────────────────────────────────────────────
log "Restoring from ${BACKUP_FILE}"
if ! docker exec -i "$CONTAINER" \
    pg_restore -U "$DB_USER" -d "$DB_NAME" --no-owner --no-privileges \
    < "$BACKUP_FILE"; then
  log "ERROR: pg_restore failed"
  exit 1
fi

# ── 7. Sanity SELECT ─────────────────────────────────────────────
log "Sanity check"
docker exec -i "$CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc \
  "SELECT 'tables=' || count(*) FROM information_schema.tables WHERE table_schema='public';"

log "Restore complete. Restart the API/worker services to pick up the new state."
