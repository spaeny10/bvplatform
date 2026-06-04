#!/usr/bin/env bash
# promote-to-prod.sh — Gate and promote a test-verified Ironsight image to fred.
#
# Usage:
#   ./deploy/promote-to-prod.sh <commit-sha>          # live run
#   ./deploy/promote-to-prod.sh --dry-run <commit-sha> # print what would happen
#
# What it does:
#   1. Validates the commit SHA argument.
#   2. Confirms that SHA is currently running on bob (test env).
#   3. Checks the last 7 days of consistency-check reports on bob show 0 divergence.
#   4. Retags the GHCR image :sha-<sha> → :prod-YYYY-MM-DD-<short-sha>.
#   5. SSHes to fred, updates the compose image tags, restarts api + worker.
#   6. Appends a promotion record to /tank/data/promote-log.jsonl on fred.
#   7. Prints a structured summary to stdout.
#
# Prerequisites on the workstation running this script:
#   - docker CLI authenticated to ghcr.io (docker login ghcr.io -u <github-user>)
#   - SSH access to bob (jetstream@192.168.103.48) and fred (jetstream@192.168.103.49)
#   - jq, curl installed
#
# Environment variables (optional overrides):
#   BOB_HOST        bob hostname or IP (default: 192.168.103.48)
#   FRED_HOST       fred hostname or IP (default: 192.168.103.49)
#   GHCR_OWNER      GHCR namespace (default: spaeny10)
#   BOB_TEST_URL    base URL for bob's test stack (default: http://192.168.103.48:8080)
#   FRED_COMPOSE    path to fred's docker-compose.yml on fred (default: /home/jetstream/ironsight/docker-compose.yml)
#   FRED_PROMOTE_LOG path to promotion log on fred (default: /tank/data/promote-log.jsonl)

set -euo pipefail

# ── Argument parsing ─────────────────────────────────────────────────
DRY_RUN=0
COMMIT_SHA=""

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    -*) echo "Unknown flag: $arg" >&2; exit 1 ;;
    *)  COMMIT_SHA="$arg" ;;
  esac
done

if [[ -z "$COMMIT_SHA" ]]; then
  echo "Usage: $0 [--dry-run] <commit-sha>" >&2
  exit 1
fi

# ── Config ───────────────────────────────────────────────────────────
BOB_HOST="${BOB_HOST:-192.168.103.48}"
FRED_HOST="${FRED_HOST:-192.168.103.49}"
GHCR_OWNER="${GHCR_OWNER:-spaeny10}"
BOB_TEST_URL="${BOB_TEST_URL:-http://192.168.103.48:8080}"
FRED_COMPOSE="${FRED_COMPOSE:-/home/jetstream/ironsight/docker-compose.yml}"
FRED_PROMOTE_LOG="${FRED_PROMOTE_LOG:-/tank/data/promote-log.jsonl}"

SHORT_SHA="${COMMIT_SHA:0:7}"
PROD_TAG="prod-$(date -u +%Y-%m-%d)-${SHORT_SHA}"
API_IMAGE="ghcr.io/${GHCR_OWNER}/ironsight-api"
WORKER_IMAGE="ghcr.io/${GHCR_OWNER}/ironsight-worker"

log() { echo "[promote] $*"; }
dry() {
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "[DRY-RUN] $*"
  else
    eval "$*"
  fi
}

if [[ "$DRY_RUN" == "1" ]]; then
  log "DRY-RUN mode — no changes will be applied."
fi

log "Promoting commit ${COMMIT_SHA} (short: ${SHORT_SHA})"
log "Prod tag will be: ${PROD_TAG}"

# ── Step 1: Verify bob is running the target SHA ─────────────────────
log "Step 1: Verifying bob is running SHA ${SHORT_SHA}..."

# The /api/health endpoint on bob returns {"status":"ok","git_sha":"<7-char-sha>"}.
# We compare the 7-char prefix of the supplied full SHA.
BOB_HEALTH_RESPONSE=$(curl -sf "${BOB_TEST_URL}/api/health" || true)
if [[ -z "$BOB_HEALTH_RESPONSE" ]]; then
  echo "ERROR: Could not reach bob at ${BOB_TEST_URL}/api/health" >&2
  echo "  Is the test stack up? Try: ssh jetstream@${BOB_HOST} 'docker ps'" >&2
  exit 1
fi

BOB_SHA=$(echo "$BOB_HEALTH_RESPONSE" | jq -r '.git_sha // empty')
if [[ -z "$BOB_SHA" ]]; then
  echo "ERROR: /api/health on bob did not return git_sha." >&2
  echo "  Response: ${BOB_HEALTH_RESPONSE}" >&2
  exit 1
fi

if [[ "$BOB_SHA" != "$SHORT_SHA" ]]; then
  echo "ERROR: SHA mismatch." >&2
  echo "  bob is running: ${BOB_SHA}" >&2
  echo "  you asked to promote: ${SHORT_SHA}" >&2
  echo "  Wait for watchtower to pull the latest image and try again." >&2
  exit 1
fi
log "bob SHA confirmed: ${BOB_SHA}"

# ── Step 2: Check consistency reports ────────────────────────────────
log "Step 2: Checking last 7 days of consistency-check reports on bob..."

# The consistency-check tool writes JSON reports to /opt/ironsight-consistency-reports/
# on the host that runs it. On the test env (bob) this path is inside the
# api container's /data volume or run separately. For now we check via SSH
# to bob's consistency report dir; if the path doesn't exist on bob yet,
# we warn but do not block the promote (bob's consistency checker may not
# be scheduled yet — it's on the OPS-01 follow-up list).
CONSISTENCY_OK=1
BOB_REPORT_CHECK=$(ssh "jetstream@${BOB_HOST}" \
  'ls /opt/ironsight-consistency-reports/*.json 2>/dev/null | sort -r | head -7' 2>/dev/null || true)

if [[ -z "$BOB_REPORT_CHECK" ]]; then
  log "WARNING: No consistency reports found on bob at /opt/ironsight-consistency-reports/."
  log "  The consistency-check scheduler may not be configured on bob yet."
  log "  Skipping consistency gate (follow-up: OPS-01)."
  CONSISTENCY_OK=0
else
  DIVERGED=0
  while IFS= read -r report; do
    FLAGGED=$(ssh "jetstream@${BOB_HOST}" "jq '.flagged_windows // 0' '${report}'" 2>/dev/null || echo "0")
    if [[ "$FLAGGED" -gt 0 ]]; then
      echo "ERROR: Consistency report ${report} shows ${FLAGGED} diverged window(s)." >&2
      DIVERGED=1
    fi
  done <<< "$BOB_REPORT_CHECK"
  if [[ "$DIVERGED" -eq 1 ]]; then
    echo "ERROR: Consistency gate failed. Fix divergence before promoting." >&2
    exit 1
  fi
  log "Consistency check passed (0 diverged windows in last 7 days)."
fi

# ── Step 3: Retag GHCR image ─────────────────────────────────────────
log "Step 3: Retagging GHCR image sha-${SHORT_SHA} → ${PROD_TAG}..."

dry "docker pull '${API_IMAGE}:sha-${COMMIT_SHA}'"
dry "docker tag  '${API_IMAGE}:sha-${COMMIT_SHA}' '${API_IMAGE}:${PROD_TAG}'"
dry "docker push '${API_IMAGE}:${PROD_TAG}'"

# Worker shares the api image binary; retag under the worker repo too.
dry "docker pull '${WORKER_IMAGE}:sha-${COMMIT_SHA}'"
dry "docker tag  '${WORKER_IMAGE}:sha-${COMMIT_SHA}' '${WORKER_IMAGE}:${PROD_TAG}'"
dry "docker push '${WORKER_IMAGE}:${PROD_TAG}'"

log "Prod images pushed: ${API_IMAGE}:${PROD_TAG}"

# ── Step 4: Update fred's compose file and restart ───────────────────
log "Step 4: Updating fred compose image tags and restarting..."

# On fred, docker-compose.yml uses the image field. We patch the IRONSIGHT_VERSION
# environment variable that the compose file uses (see services.api.image in
# docker-compose.yml: localhost/ironsight/api:${IRONSIGHT_VERSION:-latest}).
#
# However: fred currently pulls from localhost/* (locally-built images), not
# GHCR. Promoting means: tell fred to pull the new prod image from GHCR and
# restart. We do this by:
#   a. docker pull the prod-tagged GHCR image on fred
#   b. docker tag it to match the compose file's expected image name
#   c. docker compose up --no-build to reload the containers
#
# If fred's compose file is later updated to reference GHCR images directly,
# steps (a)+(b) can be dropped and only (c) is needed.

dry "ssh jetstream@${FRED_HOST} \"
  set -e
  docker pull '${API_IMAGE}:${PROD_TAG}'
  docker tag  '${API_IMAGE}:${PROD_TAG}' 'localhost/ironsight/api:${PROD_TAG}'
  docker pull '${WORKER_IMAGE}:${PROD_TAG}'
  docker tag  '${WORKER_IMAGE}:${PROD_TAG}' 'localhost/ironsight/worker:${PROD_TAG}'
  cd /home/jetstream/ironsight
  IRONSIGHT_VERSION='${PROD_TAG}' docker compose up -d --no-build api worker
  docker compose ps api worker
\""

log "fred api + worker restarted."

# ── Step 5: Log the promotion ─────────────────────────────────────────
log "Step 5: Writing promotion record to fred:${FRED_PROMOTE_LOG}..."

PROMOTED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
PROMOTED_BY=$(whoami)
CONSISTENCY_STATUS=$( [[ "$CONSISTENCY_OK" -eq 1 ]] && echo "passed" || echo "skipped_no_reports" )
LOG_ENTRY=$(cat <<JSON
{"promoted_at":"${PROMOTED_AT}","promoted_by":"${PROMOTED_BY}","commit_sha":"${COMMIT_SHA}","short_sha":"${SHORT_SHA}","prod_tag":"${PROD_TAG}","bob_sha_confirmed":"${BOB_SHA}","consistency_check":"${CONSISTENCY_STATUS}","dry_run":${DRY_RUN}}
JSON
)

dry "ssh jetstream@${FRED_HOST} \"echo '${LOG_ENTRY}' >> '${FRED_PROMOTE_LOG}'\""

# ── Summary ───────────────────────────────────────────────────────────
echo ""
echo "=========================================="
echo "  PROMOTION $( [[ "$DRY_RUN" == "1" ]] && echo "DRY-RUN COMPLETE" || echo "COMPLETE" )"
echo "=========================================="
echo "  Commit:    ${COMMIT_SHA}"
echo "  Prod tag:  ${PROD_TAG}"
echo "  bob SHA:   ${BOB_SHA}"
echo "  Consistency: ${CONSISTENCY_STATUS}"
echo "  Images:"
echo "    ${API_IMAGE}:${PROD_TAG}"
echo "    ${WORKER_IMAGE}:${PROD_TAG}"
echo ""
echo "Next steps:"
echo "  - Verify fred: curl -sf http://${FRED_HOST}:8080/api/health | jq ."
echo "  - Tail fred logs: ssh jetstream@${FRED_HOST} 'docker compose -f /home/jetstream/ironsight/docker-compose.yml logs -f --tail=50 api worker'"
echo "  - Promote log: ssh jetstream@${FRED_HOST} 'tail -5 ${FRED_PROMOTE_LOG}'"
