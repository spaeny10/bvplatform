#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────
# Ironsight — database backup
#
# Takes a logical backup (pg_dump custom format) of the platform
# database from the running ironsight-db container, writes to the
# host backup directory, and rotates older backups using a
# daily/weekly/monthly grandfather-father-son scheme.
#
# Designed to be run from cron / Task Scheduler nightly:
#
#   0 2 * * *  /path/to/scripts/backup-db.sh
#
# Exit codes:
#   0  backup taken and rotation completed
#   1  backup container not running, dump failed, or write failed
#   2  rotation failed (the new backup is fine; older files are
#      not in the expected layout — needs a human)
#
# Configuration via environment (all optional, sensible defaults):
#   IRONSIGHT_DB_CONTAINER  default: ironsight-db
#   IRONSIGHT_DB_NAME       default: onvif_tool
#   IRONSIGHT_DB_USER       default: onvif
#   IRONSIGHT_BACKUP_DIR    default: ./backups
#   IRONSIGHT_KEEP_DAILY    default: 7
#   IRONSIGHT_KEEP_WEEKLY   default: 4   (Sunday's daily promoted)
#   IRONSIGHT_KEEP_MONTHLY  default: 12  (1st-of-month daily promoted)
#
# Off-site copy: this script does NOT push to S3/B2/etc. Add a
# follow-up step in cron after this one if you need off-site
# replication. See Documents/DisasterRecovery.md §3.
# ─────────────────────────────────────────────────────────────────

set -euo pipefail

CONTAINER="${IRONSIGHT_DB_CONTAINER:-ironsight-db}"
DB_NAME="${IRONSIGHT_DB_NAME:-onvif_tool}"
DB_USER="${IRONSIGHT_DB_USER:-onvif}"
BACKUP_DIR="${IRONSIGHT_BACKUP_DIR:-./backups}"
KEEP_DAILY="${IRONSIGHT_KEEP_DAILY:-7}"
KEEP_WEEKLY="${IRONSIGHT_KEEP_WEEKLY:-4}"
KEEP_MONTHLY="${IRONSIGHT_KEEP_MONTHLY:-12}"

mkdir -p "$BACKUP_DIR/daily" "$BACKUP_DIR/weekly" "$BACKUP_DIR/monthly"

ts="$(date +%Y%m%d-%H%M%S)"
filename="ironsight-${ts}.dump"
target="$BACKUP_DIR/daily/$filename"

log() { printf '[%s] %s\n' "$(date +%Y-%m-%dT%H:%M:%S%z)" "$*"; }

# ── 1. Confirm the DB container is up ────────────────────────────
if ! docker ps --filter "name=^${CONTAINER}$" --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
  log "ERROR: container '${CONTAINER}' is not running"
  exit 1
fi

# ── 2. Take the dump ─────────────────────────────────────────────
log "Dumping ${DB_NAME} from ${CONTAINER} to ${target}"
if ! docker exec -i "$CONTAINER" \
    pg_dump -U "$DB_USER" -d "$DB_NAME" \
            --format=custom --compress=6 --no-owner --no-privileges \
    > "$target"; then
  log "ERROR: pg_dump failed"
  rm -f "$target"
  exit 1
fi

size=$(stat -c '%s' "$target" 2>/dev/null || stat -f '%z' "$target")
if [ "$size" -lt 1024 ]; then
  log "ERROR: dump is suspiciously small (${size} bytes); discarding"
  rm -f "$target"
  exit 1
fi

log "Dump OK — $(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size} bytes")"

# ── 3. Promote to weekly / monthly slots ─────────────────────────
# Sunday → also keep in weekly. 1st of month → also keep in monthly.
# Promotion is via hard link (or copy if the FS doesn't support it),
# so the daily can still be rotated out of the daily slot without
# losing the weekly/monthly snapshot.
dow=$(date +%u)   # 1=Mon .. 7=Sun
dom=$(date +%d)

if [ "$dow" = "7" ]; then
  if ln "$target" "$BACKUP_DIR/weekly/$filename" 2>/dev/null; then
    log "Promoted to weekly (hardlink)"
  else
    cp "$target" "$BACKUP_DIR/weekly/$filename"
    log "Promoted to weekly (copy — FS does not support hardlinks)"
  fi
fi
if [ "$dom" = "01" ]; then
  if ln "$target" "$BACKUP_DIR/monthly/$filename" 2>/dev/null; then
    log "Promoted to monthly (hardlink)"
  else
    cp "$target" "$BACKUP_DIR/monthly/$filename"
    log "Promoted to monthly (copy — FS does not support hardlinks)"
  fi
fi

# ── 4. Rotate ────────────────────────────────────────────────────
# Drop oldest files until the bucket is at the configured size.
rotate_bucket() {
  local dir="$1" keep="$2"
  if ! [ -d "$dir" ]; then return 0; fi
  # Newest first; drop everything past `keep`.
  mapfile -t files < <(ls -1t "$dir"/*.dump 2>/dev/null || true)
  local idx=0
  for f in "${files[@]}"; do
    idx=$((idx + 1))
    if [ "$idx" -gt "$keep" ]; then
      log "Rotating out: $f"
      rm -f -- "$f"
    fi
  done
}

rotate_bucket "$BACKUP_DIR/daily"   "$KEEP_DAILY"   || exit 2
rotate_bucket "$BACKUP_DIR/weekly"  "$KEEP_WEEKLY"  || exit 2
rotate_bucket "$BACKUP_DIR/monthly" "$KEEP_MONTHLY" || exit 2

log "Done."
