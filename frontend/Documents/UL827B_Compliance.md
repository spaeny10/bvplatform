# UL 827B Compliance Statement — Ironsight Platform

**Document version:** Living document. Updated on every commit that
adds or changes a compliance-relevant control.

**Scope:** This document maps the Ironsight platform's software
controls to the requirements typically enforced under UL 827B
(Standard for Outline of Investigation for Remote Video Monitoring
Services). It covers only the **software** surface — backend code,
database schema, frontend client. Facility, personnel, operational,
and physical-security controls (UL 827B's full scope) are addressed
separately by the deployment operator and are out of scope here.

**Disclaimer:** This is an internal compliance-tracking artifact
produced by the engineering team. It is **not** a UL certification
document, nor a substitute for a formal pre-audit by an experienced
827B consultant. The purpose is to give the audit team a precise
"what's in the code today" reference; gaps and "planned" items are
listed honestly so the consultant can advise on remediation and the
auditor can be told the truth on day one.

---

## Control Inventory

Each row maps a UL 827B-typical concern to its current state in the
Ironsight codebase. Status legend:

- ✅ **Implemented** — control is in production code with evidence.
- 🟡 **Partial** — control exists but has documented gaps.
- ⏳ **Planned** — accepted as a gap; tracked in the engineering backlog.
- 🚫 **Out of scope** — not a software concern; addressed elsewhere.

### A. Authentication & Access Control

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| A.1 | Password complexity (server-side enforced) | ✅ | [`internal/auth/password.go`](../../internal/auth/password.go), called from [`api/users.go`](../../internal/api/users.go) `HandleCreateUser` and `HandleUpdateUserPassword` | 12-char minimum, letters + at least one digit/symbol/space, common-password blocklist (~30 entries). Follows NIST SP 800-63B rev 3 — deliberately does **not** require the four-class (upper/lower/digit/symbol) pattern, which NIST recommends against. |
| A.2 | Password hashing (modern algorithm) | ✅ | [`internal/auth/auth.go:25`](../../internal/auth/auth.go#L25) | bcrypt with `bcrypt.DefaultCost`. No plaintext or reversible storage anywhere. |
| A.3 | Account lockout after failed attempts | ✅ | [`internal/database/db.go`](../../internal/database/db.go) — `LockoutThreshold`, `LockoutDuration`, `RegisterFailedLogin`, `IsUserLocked`, `ClearFailedLogins` | 5 consecutive failures → 15-minute lock. Lock is checked **before** the bcrypt comparison so a correct password during the lock window still fails. |
| A.4 | Failed-login auditing | ✅ | [`internal/api/auth_handler.go`](../../internal/api/auth_handler.go) `logFailedLogin` → append-only `audit_log` | Every 401 from `/auth/login` writes a row with attempted username, client IP, and a specific reason: `unknown_user`, `bad_password`, `bad_password_lockout_triggered`, `locked`. Response body to caller is uniform; introspection cannot distinguish reasons. |
| A.5 | Brute-force rate limit on login | ✅ | [`internal/api/ratelimit.go`](../../internal/api/ratelimit.go), wired in [`internal/api/router.go`](../../internal/api/router.go) `RateLimitLogin(10)` | 10 attempts/minute per client IP. Returns 429 with `Retry-After: 60`. Sliding window, in-process. Keyed on IP only via `net.SplitHostPort` (port stripped). |
| A.6 | Session expiry | ✅ | [`internal/auth/auth.go:46`](../../internal/auth/auth.go#L46) — JWT 24h with unique `jti`; [`frontend/src/hooks/useSessionManager.tsx`](../../frontend/src/hooks/useSessionManager.tsx) — 30-min idle, 8h cap; revocation via A.7 | Tokens carry a unique `jti` (uuid) which is the lookup key for the revocation list. A stolen token can now be killed server-side without rotating the global signing secret. |
| A.7 | Server-side logout / token revocation | ✅ | [`internal/database/db.go`](../../internal/database/db.go) `RevokeToken`/`IsTokenRevoked`/`PruneExpiredRevokedTokens`; [`internal/api/auth_handler.go`](../../internal/api/auth_handler.go) `HandleLogout`; route at [`internal/api/router.go`](../../internal/api/router.go) `/auth/logout` | `POST /auth/logout` inserts the bearer token's `jti` into `revoked_tokens`. `RequireAuth` consults the blocklist on every authenticated request and returns 401 "token revoked" when matched. Smoke-tested: login → 200, logout → 204, reuse → 401. The blocklist is also the rotate-JWT_SECRET break-glass: revoke every active session by inserting all live `jti`s. `PruneExpiredRevokedTokens` reclaims rows whose original exp has passed. |
| A.8 | Password rotation policy | ✅ | `users.password_changed_at` column; [`internal/database/db.go`](../../internal/database/db.go) `PasswordExpired`, `PasswordMaxAge`; [`internal/api/auth_handler.go`](../../internal/api/auth_handler.go) `loginResponse.PasswordExpired` | 180-day soft-enforced rotation. Login still succeeds when expired (so the user can authenticate the change-password call), but the response carries `password_expired: true` for the frontend to gate on a forced-change screen. `UpdateUserPassword` stamps `password_changed_at = NOW()` on every change, restarting the clock. |
| A.9 | Multi-factor authentication | ✅ | TOTP (RFC 6238) implemented in [`internal/auth/mfa.go`](../../internal/auth/mfa.go); endpoints in [`internal/api/mfa_handler.go`](../../internal/api/mfa_handler.go); user columns `mfa_enabled`, `mfa_secret`, `mfa_recovery_hashes` | Optional, opt-in per user. SHA-1 / 30s / 6 digits — the parameter set every authenticator app expects. ±1 step drift tolerance. Enrollment is two-step (enroll generates secret + 10 recovery codes; confirm validates first code before flipping `mfa_enabled = true`) so a half-finished enrollment can never lock anyone out. Recovery codes stored as bcrypt hashes; consumed atomically on use. Login flow returns `{"mfa_required": true}` with HTTP 401 when MFA is enabled and code is missing — no preauth-half-token leaks. Bad MFA codes count toward the lockout threshold (#A.3) so a primary-credential leak gets a finite TOTP-guess budget. Implemented in-house rather than via third-party library to keep the parameter set pinned and the crypto inspectable. |
| A.10 | Role-change audit trail | ✅ | [`internal/api/audit.go:131`](../../internal/api/audit.go#L131) — `change_role` action wired in `classifyRequest` middleware | Every PATCH to `/api/users/{id}/role` lands in `audit_log` with target_type=user, target_id={UUID}, IP, timestamp. |
| A.11 | Role separation enforced | ✅ | [`internal/api/users.go`](../../internal/api/users.go) — admin-only checks; [`frontend/src/contexts/AuthContext.tsx`](../../frontend/src/contexts/AuthContext.tsx) — `ROUTE_PERMISSIONS` matrix | Backend rejects unauthorized role mutations with 403; frontend blocks navigation to roles the user can't access. Six roles defined: `admin`, `soc_supervisor`, `soc_operator`, `site_manager`, `customer`, `viewer`. |
| A.12 | JWT signing secret externalized | ✅ | [`internal/config/config.go:43`](../../internal/config/config.go#L43) reads `JWT_SECRET` env | The env-default fallback is a placeholder string and is **explicitly unsafe**. Production deployments must set `JWT_SECRET` (compose enforces this with `${JWT_SECRET:?...}`). Operator instructions: `openssl rand -hex 32`. |

### Dual-operator verification ("four-eyes rule")

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| A.13 | Dual-operator verification on dispositioned events | ✅ | `security_events.disposed_by_user_id`, `verified_by_user_id`, `verified_by_callsign`, `verified_at` columns; `VerifySecurityEvent` in [`internal/database/platform_db.go`](../../internal/database/platform_db.go); handler at [`internal/api/platform.go`](../../internal/api/platform.go) `HandleVerifySecurityEvent`; route `POST /api/v1/events/{id}/verify` | Restricted to `soc_supervisor` and `admin` roles. The verify update is one atomic conditional UPDATE that rejects self-verification (`disposed_by_user_id <> verifier`) and re-verification (`verified_at IS NULL`). 409 distinguishes "already verified" from "self-verify attempt." Smoke-tested all four matrix cells: self → 409, cross → 204, re-verify → 409, non-supervisor → 403. Partial index `idx_security_events_unverified_high` makes "list outstanding high-severity verifications" a fast scan for the supervisor dashboard. |

### B. Audit Trail

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| B.1 | Polymorphic audit_log with target_type / target_id | ✅ | [`internal/api/audit.go`](../../internal/api/audit.go) `classifyRequest` covers cameras, sites, users, settings, exports, speakers, audio_messages, bookmarks, storage, operators, incidents, alarms, security_events, evidence, organizations | Indexed `(target_type, target_id, created_at DESC)` for fast "who touched this" queries. |
| B.2 | Append-only enforcement on audit tables | ✅ | Migration in [`cmd/server/main.go`](../../cmd/server/main.go) — `ironsight_prevent_mutation()` PL/pgSQL trigger; smoke-tested rejects `UPDATE` and `DELETE` | Triggers attached to `audit_log`, `playback_audits`, `deterrence_audits`. App role retains INSERT; UPDATE/DELETE raise `insufficient_privilege`. Triggers can be dropped only during a documented signed-maintenance window. |
| B.3 | Successful login auditing | ✅ | [`internal/api/audit.go`](../../internal/api/audit.go) — `action=login` row on every 200 from `/auth/login` | Captures user_id, username, IP, timestamp. |
| B.4 | Mutation auditing (create/update/delete) | ✅ | `AuditMiddleware` wraps the entire authenticated `/api` subrouter | One row per successful mutating request (POST/PUT/PATCH/DELETE), only for 2xx responses, with full target classification. |
| B.5 | Operator action auditing (claim, ack, dispose alarms) | ✅ | [`internal/api/audit.go`](../../internal/api/audit.go) — `claim_alarm`, `release_alarm`, `ack_alarm`, `dispose_alarm` action verbs | Sub-path-aware classifier: `/api/alarms/{id}/claim` records distinct verb, not generic `update_alarm`. |
| B.6 | Recording playback auditing | ✅ | [`internal/api/audit_playback.go`](../../internal/api/audit_playback.go) → `playback_audits` table (append-only) | Separate table from `audit_log` so playback patterns are queryable on their own. Captures user, segment, IP, time. |
| B.7 | Deterrence-action auditing (strobe/siren) | ✅ | `deterrence_audits` table (append-only) | Records every operator-initiated talk-down/strobe/siren activation with operator id, camera, alarm context. |
| B.8 | Audit log retention | ✅ | Policy constant [`database.MinAuditRetentionDays`](../../internal/database/soc_ids.go) = 365; explicit scope comment on [`recording.RetentionManager`](../../internal/recording/retention.go) listing tables it does NOT touch | Append-only triggers (B.2) make retention "forever, less explicit signed-maintenance-window intervention." Policy constant is exported so dashboards and audit deliverables cite one canonical number. The retention worker's docstring enumerates exactly which tables it owns and which are off-limits, so a future contributor can't accidentally extend it onto an audit table. |
| B.9 | Audit log export | ✅ | [`internal/api/audit.go`](../../internal/api/audit.go) `HandleQueryAuditLog` with `?format=csv` | `GET /api/audit?format=csv` returns the filtered audit log as `text/csv` with a timestamped filename in `Content-Disposition`. Same `username`, `action`, `target_type` filters apply. Limit clamps at 10,000 rows per request; for larger ranges, page by date. |

### C. Incident & Alarm Identifiers

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| C.1 | Phoneticizable alarm code | ✅ | [`internal/database/soc_ids.go`](../../internal/database/soc_ids.go) `NextAlarmCode` | Format: `ALM-YYMMDD-NNNN`. Daily 4-digit sequence. Generated server-side; assigned by `CreateActiveAlarm` if caller leaves it empty. UUID-style PK is preserved as URL/API key. |
| C.2 | Formal incident identifier | ✅ | `NextIncidentID` in same file; enforced inside `CreateIncident` | Format: `INC-YYYY-NNNN`. Annual sequence. Caller-supplied IDs that don't start with `INC-` are overwritten. |
| C.3 | Alarm → triggering event correlation | ✅ | `active_alarms.triggering_event_id` BIGINT column; populated by alert emitter in [`cmd/server/main.go`](../../cmd/server/main.go) | The detection that fired the alarm is a single `SELECT events WHERE id=...` away. Previous behavior required a lossy `(camera_id, ts)` join. |
| C.4 | Incident → constituent alarms | ✅ | `incidents` schema in [`cmd/server/main.go`](../../cmd/server/main.go); `AttachAlarmToIncident` in [`internal/database/platform_db.go`](../../internal/database/platform_db.go) | Alarms within a 5-minute correlation window per site collapse into one incident, preserving the alarm count, camera list, and severity escalation. |

### D. Evidence Management

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| D.1 | Evidence export bundle | ✅ | [`internal/api/evidence_export.go`](../../internal/api/evidence_export.go) `HandleEvidenceExport` | Per-event ZIP including video clip, metadata JSON, branded README. Authenticated endpoint. |
| D.2 | Public share token (read-only) | 🟡 | Schema exists ([`evidence_shares`](../../ironsight_platform.sql)); public read endpoint at [`internal/api/evidence_share.go`](../../internal/api/evidence_share.go) `HandlePublicEvidenceShare` | Read path complete. **Gap:** the share-CREATION handler isn't built yet. Until it lands, the table is empty and every public URL 404s, which is the safe default. |
| D.3 | Public share access logging | ✅ | `evidence_share_opens` table; `LogEvidenceShareOpen` in [`internal/database/soc_ids.go`](../../internal/database/soc_ids.go) | Every GET of `/share/{token}` writes a row with token, IP, user-agent, referrer, opened_at — including 404 attempts on unknown tokens (so probing patterns are visible in audit). Logging is fire-and-forget; an audit-write failure must never block evidence access. |
| D.4 | Chain-of-custody on shared evidence | ✅ | Same table as D.3 | Indexed `(token, opened_at DESC)`; an investigator can produce a complete access history per share token in one query. |
| D.5 | Digital signing of evidence bundles | ✅ | [`internal/evidence/signing.go`](../../internal/evidence/signing.go) `SignedZipWriter`; signing key from env `EVIDENCE_SIGNING_KEY`; export bundles include `event.json` (with content_hashes block) + `SIGNATURE.txt` (HMAC-SHA256 over the manifest bytes) | Two-layer integrity: each binary file (clip, snapshot) has its SHA-256 recorded inside the manifest, and the manifest itself is HMAC-signed by a separate file. Tampering with any binary fails its hash; tampering with the manifest fails the HMAC; tampering with both requires the secret key. Smoke-tested: external Python-based verification matches the embedded signature, and an in-place edit of "Test Cam" → "Forged X" produces a different HMAC, demonstrating tamper detection. |

### E. Operational Telemetry & SLA

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| E.1 | SLA deadline tracked per alarm | ✅ | `active_alarms.sla_deadline_ms`, `acknowledged_at`, `acknowledged_by_user_id`, `acknowledged_by_callsign` columns; `AcknowledgeAlarm` in [`internal/database/platform_db.go`](../../internal/database/platform_db.go) records ack metadata atomically with the disposition write | Index `idx_active_alarms_ack_window` on `(acknowledged_at, ts)` makes the report query touch only acked rows. Operator callsign is denormalized at ack time so a future rename can't rewrite the SLA narrative. |
| E.2 | Operator response-time reporting | ✅ | [`internal/api/reports.go`](../../internal/api/reports.go) `HandleSLAReport` at `GET /api/reports/sla` | Query parameters: `from`, `to` (RFC3339, default 30 days), `group=operator|day`, `format=json|csv`. Returns total/acked/within-SLA/over-SLA counts plus avg, p50, p95 ack-time-in-seconds per bucket. Smoke-tested with seeded data: OP-001 p95=43.5s (within 90s SLA), OP-002 p95=174s (1 of 2 over SLA). |
| E.3 | Recording health visibility | ✅ | [`/api/recording/health`](../../internal/api/router.go) — `HandleRecordingHealth` | Per-camera last-segment timestamp, online/degraded/offline state. Surfaced on the operator dashboard via `RecordingHealthCard`. |
| E.4 | System health visibility | ✅ | [`/api/system/health`](../../internal/api/router.go) — `HandleSystemHealth` | Reports DB pool, recording engine, MediaMTX control API, AI service connectivity, storage paths. Admin dashboard "Health" tab consumes it. |

### F. Communications & Network

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| F.1 | CORS allowlist | ✅ | [`internal/config/config.go`](../../internal/config/config.go) `AllowedOrigins` populated from `ALLOWED_ORIGINS` env (comma-separated); applied in [`internal/api/router.go`](../../internal/api/router.go) `cors.Handler` | Production deployments override the dev default by setting `ALLOWED_ORIGINS=https://soc.example.com` in `.env`. The compose file plumbs the env through to the api container. The `parseAllowedOrigins` helper trims whitespace and drops empty entries. Wildcards (`*`) work but are flagged in the env-example comments as discouraged. |
| F.2 | TLS termination story | 🚫 | Out of scope — handled by reverse proxy (Caddy/nginx) in front of the api container | Documentation in [`MasterDeployment.md`](MasterDeployment.md) covers the deployment topology. UL 827B requires end-to-end encrypted comms; this is satisfied by the reverse proxy, not the application code. |
| F.3 | DC-09 dispatch protocol | ⏳ | Planned (post-certification) | Outbound channel to a UL-listed central station for verified-alarm escalation. Documented as a roadmap item. |

### G. Operator Workstation

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| G.1 | Operator-on-duty identity separate from auth identity | ✅ | `operators` table separate from `users`; `OperatorStatusBar` UI; shift-handoff workflow | The auth principal logs in as `users.id`; the shift role is `operators.id`. Either can be reassigned without affecting the other. |
| G.2 | Multi-site console (split view) | ✅ | [`frontend/src/components/operator/MultiSiteSplitView.tsx`](../../frontend/src/components/operator/MultiSiteSplitView.tsx) | Operator can monitor multiple sites simultaneously. |
| G.3 | Workstation video recording (operator screen capture) | 🚫 | Out of scope — handled by an external recording tool layered over the operator workstation | The 827B plan must name the chosen tool (Verint Impact 360, Imprivata OneSign, RDP-Recorder, etc.). Not a function of the application. |

### H. Data Retention

| # | Control | Status | Evidence | Notes |
|---|---|---|---|---|
| H.1 | Recording retention enforced per site | ✅ | [`internal/retention`](../../internal/retention); `sites.retention_days` column | Worker purges segments older than the site's contracted retention. |
| H.2 | Audit table retention | ✅ | See B.8 | 365-day minimum, codified as `database.MinAuditRetentionDays`. Retention worker's scope comment explicitly enumerates audit tables as off-limits. |

---

## Summary by Status

| Status | Count |
|---|---|
| ✅ Implemented | 36 |
| 🟡 Partial | 1 |
| ⏳ Planned | 2 |
| 🚫 Out of scope | 3 |

The single remaining partial (D.2) closes when the share-creation
handler is built (Phase B.8). The remaining planned items are
httpOnly cookie migration (Phase A.5) and DC-09 dispatch (F.3,
post-cert).

---

## What an Auditor Would Want to See, in One Place

If a UL 827B reviewer asks "show me where you do X," here's the
shortest path:

| Question | Demonstration |
|---|---|
| "Show me a failed login attempt being recorded." | Tail `audit_log` filtered to `action='login_failed'`. Sample query in this doc's appendix. |
| "Show me that I cannot tamper with the audit log." | `DELETE FROM audit_log WHERE id=1;` raises `audit table public.audit_log is append-only (op=DELETE)`. |
| "Show me a locked account." | After 5 wrong attempts, `SELECT username, failed_login_attempts, locked_until FROM users WHERE username='...';` |
| "Show me the chain of custody for this share token." | `SELECT * FROM evidence_share_opens WHERE token='...' ORDER BY opened_at;` |
| "Show me who acknowledged alarm ALM-260424-0042." | Once SLA tracking lands (E.1): `SELECT acknowledged_by_user_id, acknowledged_at FROM active_alarms WHERE alarm_code='ALM-260424-0042';` |
| "How does an admin password reset look in the audit?" | `SELECT * FROM audit_log WHERE action='change_password' AND target_id=...;` |

---

## Maintenance

- Update this document on every commit that adds or modifies a
  compliance-relevant control. The compliance-doc update should be
  in the same commit as the code change (not a follow-up commit) so
  history shows control + evidence together.
- Before any pre-audit walkthrough: open this doc, run the smoke
  tests in the appendix below, capture the output, attach to the
  audit binder.

## Appendix: Auditor-ready Smoke Tests

```bash
# A.3 + A.4 — failed login + lockout
for i in 1 2 3 4 5; do
  curl -s -X POST -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"wrong"}' \
    http://localhost:8080/auth/login
done
psql -U onvif -d onvif_tool -c \
  "SELECT username, action, target_id, ip_address, created_at \
   FROM audit_log WHERE action='login_failed' ORDER BY id DESC LIMIT 10"

# A.5 — rate limit
for i in $(seq 1 14); do
  curl -s -o /dev/null -w "%{http_code} " -X POST \
    -H 'Content-Type: application/json' \
    -d '{"username":"x","password":"x"}' \
    http://localhost:8080/auth/login
done
echo
# Expected: 401 401 401 401 401 401 401 401 401 401 429 429 429 429

# B.2 — append-only audit
psql -U onvif -d onvif_tool -c "DELETE FROM audit_log WHERE id=1"
# Expected: ERROR: audit table public.audit_log is append-only (op=DELETE)

# A.7 — server-side logout / token revocation
TOKEN=$(curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<pw>"}' \
  http://localhost:8080/auth/login | jq -r .token)
curl -sI -H "Authorization: Bearer $TOKEN" http://localhost:8080/auth/me | head -1
# Expected: HTTP/1.1 200 OK
curl -sI -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8080/auth/logout | head -1
# Expected: HTTP/1.1 204 No Content
curl -sI -H "Authorization: Bearer $TOKEN" http://localhost:8080/auth/me | head -1
# Expected: HTTP/1.1 401 Unauthorized   (body: "token revoked")

# A.8 — password rotation flag
psql -U onvif -d onvif_tool -c \
  "UPDATE users SET password_changed_at = NOW() - INTERVAL '181 days' WHERE username='admin'"
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<pw>"}' \
  http://localhost:8080/auth/login | jq .password_expired
# Expected: true

# B.9 — CSV audit export
curl -sH "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/audit?format=csv" | head -3
# Expected: header row + first data rows in CSV

# D.3 — share access logging
curl -s http://localhost:8080/share/probe-token-test
psql -U onvif -d onvif_tool -c \
  "SELECT token, ip, user_agent, opened_at FROM evidence_share_opens \
   ORDER BY opened_at DESC LIMIT 5"

# F.1 — CORS allowlist
podman exec ironsight-api env | grep ALLOWED_ORIGINS
# Expected: ALLOWED_ORIGINS=https://soc.<your-host>   (in production)

# E.1 + E.2 — SLA report
TOKEN=$(curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<pw>"}' \
  http://localhost:8080/auth/login | jq -r .token)
FROM=$(date -u -d '30 days ago' +%Y-%m-%dT%H:%M:%SZ)
TO=$(date -u +%Y-%m-%dT%H:%M:%SZ)
curl -sH "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/reports/sla?from=$FROM&to=$TO&group=operator" | jq .
# Expected: rows[].bucket = operator callsign, with within_sla/over_sla counts
#           and avg/p50/p95 ack times in seconds.

# CSV variant for download
curl -sH "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/reports/sla?from=$FROM&to=$TO&group=day&format=csv" \
  -o sla_report.csv

# A.13 — dual-operator verification
# Disposing operator cannot self-verify
curl -sH "Authorization: Bearer $DISPOSER_TOKEN" -X POST \
  http://localhost:8080/api/v1/events/EVT-2026-0042/verify
# Expected: HTTP 409 "verifier must be a different operator"

# A different supervisor verifies
curl -sH "Authorization: Bearer $SUPERVISOR_TOKEN" -X POST \
  http://localhost:8080/api/v1/events/EVT-2026-0042/verify
# Expected: HTTP 204

# Re-verification rejected
curl -sH "Authorization: Bearer $SUPERVISOR_TOKEN" -X POST \
  http://localhost:8080/api/v1/events/EVT-2026-0042/verify
# Expected: HTTP 409 "event already verified"

# Non-supervisor role rejected
curl -sH "Authorization: Bearer $VIEWER_TOKEN" -X POST \
  http://localhost:8080/api/v1/events/EVT-2026-0042/verify
# Expected: HTTP 403 "supervisor or admin role required"
```
