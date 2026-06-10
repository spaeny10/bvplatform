# 05 — Auth, users & admin

How anyone gets into Ironsight and what admins manage once inside: password
login with lockout for customers, header-trust SSO for BigView staff, the
cookie/CSRF session layer both ride on, MFA, user/role management, the
append-only audit log, system settings, and the system-health surfaces.
Everything here is MVP core except the AI services health cards, which park
with the rest of the server-side AI stack.

## Password login {#password-login}

| Field | Value |
|---|---|
| **ID** | `password-login` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Username/email + password sign-in at `/login` for users without SSO (customers). Issues the session cookie pair and redirects by role; hardened with account lockout, per-IP rate limiting, and audit of every failed attempt. |
| **Frontend** | `frontend/src/app/login/page.tsx`, `frontend/src/contexts/AuthContext.tsx` |
| **Routes** | `POST /auth/login` |
| **Tables** | users, audit_log |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Incognito → test.ironsight `/login` → sign in as a known user → role-based redirect (`/`, `/operator`, or `/portal`). Then enter a wrong password 5+ times → even the correct password now returns "invalid credentials" (lockout). |
| **Notes** | Defense layers in `internal/api/auth_handler.go` + `ratelimit.go`: per-IP limiter (10/min, in-memory — per-replica if api ever scales out), lockout via `users.failed_login_attempts`/`locked_until` checked *before* password verify, identical 401 body across failure modes (real reason only in audit_log). Gotchas: (1) the login page footer prints demo credentials ("admin / admin", "demo123") — remove before customer exposure; (2) `password_expired` soft-rotation flag is returned but the frontend has no forced-change screen yet; (3) a user with MFA enabled cannot complete UI login — see [[mfa-totp]]. Demo mode (`?demo=1` / `NEXT_PUBLIC_DEMO_MODE`) bypasses auth entirely with a fake customer user — must be off in prod. |

## SSO header trust (oauth2-proxy) {#sso-header-trust}

| Field | Value |
|---|---|
| **ID** | `sso-header-trust` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Staff path: Google OAuth via oauth2-proxy + NPM injects `X-Forwarded-Email`; when `SSO_TRUST_HEADER=email` the `RequireAuth` middleware trusts it, auto-provisions the user, and skips JWT entirely. No password form is ever shown to staff. |
| **Frontend** | `frontend/src/contexts/AuthContext.tsx` |
| **Routes** | — |
| **Tables** | users |
| **Flag** | — |
| **Docs** | [configuration.md](../configuration.md), [media-auth.md](../media-auth.md) |
| **Smoke test** | Hit the oauth2-proxy-fronted hostname with a jetstreamsys Google account → land in the app with no password prompt; `GET /auth/me` returns your provisioned user. A first-time email gets a new users row at `SSO_DEFAULT_ROLE`. |
| **Notes** | Routes is — because this is middleware, not an endpoint (`RequireAuth` in `internal/api/auth_handler.go`; session rehydration happens through [[session-csrf]]'s `/auth/me`). `SSO_ADMIN_EMAILS` allowlist auto-promotes to admin, including pre-existing lower-role rows. SECURITY: only safe behind a proxy that strips client-supplied `X-Forwarded-Email` — never enable on a directly-exposed deployment. SSO sessions never hit `/auth/login`, so the middleware mints the `ironsight_csrf` cookie on the first authenticated request (with `GET /api/auth/csrf` as the frontend's safety-net bootstrap). Header absent → falls through to the cookie path, which is the emergency local-login escape hatch. Customers will NOT have SSO — [[password-login]] is their path. |

## Session cookies + CSRF {#session-csrf}

| Field | Value |
|---|---|
| **ID** | `session-csrf` |
| **Tier** | core |
| **Status** | working |
| **Definition** | The session layer everything authenticated rides on: `ironsight_session` HttpOnly JWT cookie + `ironsight_csrf` double-submit cookie, `/auth/me` rehydration on page load, server-side token revocation, and two-stage logout that also kills the oauth2-proxy cookie. |
| **Frontend** | `frontend/src/contexts/AuthContext.tsx`, `frontend/src/lib/api.ts` |
| **Routes** | `GET /auth/me` · `POST /auth/logout` · `GET /api/auth/csrf` · `GET /api/auth/ws-ticket` |
| **Tables** | revoked_tokens |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Log in → DevTools shows `ironsight_session` (HttpOnly) + `ironsight_csrf` cookies. Replay a POST to any `/api/*` route without `X-CSRF-Token` → 403. Sign out → you land back on `/login` and `GET /auth/me` returns 401. |
| **Notes** | The `Authorization: Bearer` path was retired in P1-A-02 PR3 — the cookie IS the auth (`auth_header_retire_test.go`, `cookie_csrf_test.go` cover this). CSRF is double-submit: JS reads `ironsight_csrf` and echoes it as `X-CSRF-Token` on non-idempotent methods; GET/HEAD/OPTIONS exempt. Logout is deliberately two-stage: `POST /auth/logout` revokes the JWT's jti into revoked_tokens and clears cookies, then the frontend redirects to `/oauth2/sign_out?rd=%2Flogin` — without stage 2 the oauth2-proxy cookie silently re-authenticates staff ("sign out doesn't sign out"). `GET /api/auth/ws-ticket` mints a 5-minute WS-audience token so the 24 h session JWT never appears in a WebSocket URL; consumers are the alert/live sockets (04-alerts-notifications.md). |

## MFA (TOTP) {#mfa-totp}

| Field | Value |
|---|---|
| **ID** | `mfa-totp` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | TOTP second factor: enroll (secret + 10 one-time recovery codes), confirm, disable, login-time challenge, and admin reset for locked-out users. |
| **Frontend** | `frontend/src/app/admin/page.tsx` |
| **Routes** | `POST /api/auth/mfa/enroll` · `POST /api/auth/mfa/confirm` · `POST /api/auth/mfa/disable` · `POST /api/users/{id}/mfa/reset` |
| **Tables** | users |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | (Backend-only today.) With a session cookie + CSRF token, `curl -X POST /api/auth/mfa/enroll` → otpauth URL + 10 recovery codes; confirm with a TOTP code → next `POST /auth/login` without `mfa_code` returns 401 `{"mfa_required":true}`. |
| **Notes** | Backend is complete and solid (`internal/api/mfa_handler.go`): bcrypt-hashed one-time recovery codes, MFA failures count toward lockout, no preauth half-token (password+code replayed on pass 2). Frontend is NOT: (1) no enroll/confirm/disable UI exists anywhere (api-coverage Table B — zero callers); (2) the login page never prompts for a code, so an MFA-enabled user sees raw `{"mfa_required":true}` as an error and is locked out of the UI; (3) the admin "Reset MFA" button (admin/page.tsx) calls `POST /api/users/{id}/mfa/reset` — the wrong-`/v1` path was fixed in the F-02 batch, and the orphaned `UsersTab.tsx` copy of the button was deleted in the 2026-06 dead-code cleanup. Admin reset allows admin or soc_supervisor. `mfa_secret` is plaintext at rest (threat model in migration 0015 comments). Finish cost: TOTP step on the login form + an enroll modal + the one-line path fix. Open question: is customer-facing MFA an MVP blocker, given staff get Google MFA via [[sso-header-trust]]? |

## Users + roles {#users-roles}

| Field | Value |
|---|---|
| **ID** | `users-roles` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Platform user CRUD with six roles (admin, soc_operator, soc_supervisor, site_manager, customer, viewer): create with role + optional company, edit profile, change role, reset password, soft-delete — all from the admin Users tab. |
| **Frontend** | `frontend/src/app/admin/page.tsx`, `frontend/src/contexts/AuthContext.tsx` |
| **Routes** | `GET /api/users` · `POST /api/users` · `DELETE /api/users/{id}` · `PATCH /api/users/{id}` · `PATCH /api/users/{id}/password` · `PATCH /api/users/{id}/role` |
| **Tables** | users |
| **Flag** | — |
| **Docs** | [soft-delete.md](../soft-delete.md) |
| **Smoke test** | Admin → Users tab → create an internal user with role soc_operator → appears in list; change their role, reset their password; log in as them in incognito → lands on `/operator`. |
| **Notes** | List is any-authenticated; create/delete/role are admin-only; password change is admin-or-self; users are soft-deleted (migration 0028; `?include_deleted=true` is admin-only). The `ROUTE_PERMISSIONS` matrix in AuthContext gates frontend routes per role but is client-side only — real RBAC is per-handler on the server. The MFA reset button in this tab works since the F-02 path fix (see [[mfa-totp]]); the never-imported `admin/UsersTab.tsx` extraction of this tab was deleted in the 2026-06 dead-code cleanup. Customer-company user management (`/api/v1/companies/{companyId}/users`) is a separate surface — 06-portal-platform.md. |

## Audit log {#audit-log}

| Field | Value |
|---|---|
| **ID** | `audit-log` |
| **Tier** | core |
| **Status** | partial |
| **Definition** | Append-only record of every mutating API call, login failure, logout, and media fetch, with a filterable viewer + CSV export for UL 827B deliverables. |
| **Frontend** | `frontend/src/components/SettingsPage.tsx`, `frontend/src/components/admin/AuditLogPanel.tsx`, `frontend/src/components/admin/AuditLogExport.tsx` |
| **Routes** | `GET /api/audit` |
| **Tables** | audit_log |
| **Flag** | — |
| **Docs** | [media-auth.md](../media-auth.md) |
| **Smoke test** | Admin → Settings → Audit tab → Search → rows show recent mutations including your own actions; hit `/api/audit?format=csv` → CSV file downloads. |
| **Notes** | Backend is working: `AuditMiddleware` logs every 2xx mutating `/api/*` request (fire-and-forget), failed logins land with coarse reasons, media serves batch in via the ring buffer (media-auth.md), and migration 0017's `ironsight_prevent_mutation` trigger makes audit_log (+ playback_audits, deterrence_audits) append-only at the DB layer. CSV export caps at 10k rows. Status is partial because two of the three UIs are dead: `AuditLogPanel` (on /admin) calls `GET /api/v1/audit` — a Table C 404, so it spins forever — and `AuditLogExport` renders `generateMockAuditLog()` fabricated entries. The SettingsPage Audit tab is the one real viewer (`queryAuditLog` → `GET /api/audit`). Fix: rewire or delete the two admin-page components; `logAuditAction` (`POST /api/v1/audit`, Table C) is also a 404 but unneeded — the middleware already records server-side. `frontend/src/components/settings/AuditLogTab.tsx` is an extracted-but-never-imported duplicate. |

## System settings {#system-settings}

| Field | Value |
|---|---|
| **ID** | `system-settings` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Global system configuration (storage paths, ffmpeg path, default segment duration, etc.) read by any authenticated user and saved by admins from the Settings page. |
| **Frontend** | `frontend/src/components/SettingsPage.tsx` |
| **Routes** | `GET /api/settings` · `PUT /api/settings` |
| **Tables** | system_settings |
| **Flag** | — |
| **Docs** | [configuration.md](../configuration.md) |
| **Smoke test** | Admin → Settings → System tab → change default segment duration → Save → reload the page → the value persists. |
| **Notes** | PUT is admin-only and reflects path changes into the live config for new requests, but the recording engine and live-HLS keep their startup values until an api restart (documented in `internal/api/settings.go` / router.go comments). Storage *locations* (drive browsing, retention) are their own feature — 02-recording-playback.md. SettingsPage itself is the multi-tab admin surface (cameras/storage/speakers/system/events/audit); only the settings + system + audit tabs belong to this area. |

## System health {#system-health}

| Field | Value |
|---|---|
| **ID** | `system-health` |
| **Tier** | core |
| **Status** | working |
| **Definition** | Authenticated server health snapshot — uptime, memory, goroutines, camera online/offline/recording counts, active streams, per-storage-location disk usage — rendered on the Settings → System tab, plus a per-camera health grid on the admin page. |
| **Frontend** | `frontend/src/components/SettingsPage.tsx`, `frontend/src/components/HealthDashboard.tsx` |
| **Routes** | `GET /api/system/health` |
| **Tables** | cameras, storage_locations |
| **Flag** | — |
| **Docs** | — |
| **Smoke test** | Admin → Settings → System tab → health card shows non-zero uptime/memory/goroutines and camera counts matching reality; refresh → numbers update. |
| **Notes** | The endpoint + SystemTab card are real end-to-end. Gotcha: `HealthDashboard.tsx` (admin page camera-health grid) never calls this endpoint — its name/status/recording columns are real (derived from the camera list prop) but `uptime_pct` (hardcoded 100/0), `stream_fps` (0), and `bitrate_kbps` (0) are fabricated; either wire those columns to `GET /api/recording/health` (02-recording-playback.md) or drop them before a customer reads 0 fps as an outage. The unauthenticated `GET /api/health` liveness probe (status + git_sha) is deploy plumbing — 09-system-infra.md; the public customer `/status` page is 06-portal-platform.md. |

## AI services health {#ai-services-health}

| Field | Value |
|---|---|
| **ID** | `ai-services-health` |
| **Tier** | back-burner |
| **Status** | working |
| **Definition** | Admin cards monitoring the AI/backing services: up/down probes for YOLO, Qwen, mediamtx, and the worker; GPU/latency time-series charts; and a per-site AI usage/cost breakdown. |
| **Frontend** | `frontend/src/components/admin/ServicesHealthCard.tsx`, `frontend/src/components/admin/AIMetricsChart.tsx`, `frontend/src/components/admin/AIUsageBySiteCard.tsx` |
| **Routes** | `GET /api/system/services` · `GET /api/system/services/timeseries` · `GET /api/system/services/usage` |
| **Tables** | ai_runtime_metrics |
| **Flag** | `ai_insights` |
| **Docs** | [metrics.md](../metrics.md) |
| **Smoke test** | Admin page → System Health section → services card lists YOLO/Qwen/mediamtx/worker with live up/down status (15 s poll); usage card shows per-site request counts over the selected window. |
| **Notes** | Code-wise this works — all three endpoints have real frontend consumers (api-coverage marks `/usage` backend-only, but `getAIUsageBySite` at `frontend/src/lib/api.ts:396` does call it; the scan missed the template-literal URL). Parked with the server-side AI descope: with no YOLO/Qwen containers deployed at MVP the cards just report "down"/empty, which is noise on a core admin page — hide behind `ai_insights` alongside the rest of the AI stack (08-ai-analytics.md). The mediamtx probe inside `HandleServicesHealth` is the one non-AI tenant here; if a core mediamtx health signal is wanted at MVP it should move into [[system-health]]. Revival cost: zero code — redeploy AI containers and unhide. |
