package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/logging"
)

// ── Cookie names ──────────────────────────────────────────────────────────────

const (
	// sessionCookieName carries the session JWT. HttpOnly; not JS-readable,
	// prevents XSS from exfiltrating the credential.
	sessionCookieName = "ironsight_session"
	// csrfCookieName carries the CSRF double-submit token. NOT HttpOnly —
	// intentionally JS-readable so authFetch can echo it in X-CSRF-Token.
	csrfCookieName = "ironsight_csrf"
)

// generateCSRFToken produces a random 32-byte hex string suitable for
// use as a session-bound CSRF double-submit token.
func generateCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// setSessionCookies writes the ironsight_session (HttpOnly) and
// ironsight_csrf (JS-readable) cookies onto the response. Both share
// the same Max-Age derived from the JWT expiry so stale cookies expire
// in sync with their payloads. The Secure flag follows cfg.CookieSecure.
func setSessionCookies(w http.ResponseWriter, token, csrfToken string, expiry time.Time, cfg *config.Config) {
	maxAge := int(time.Until(expiry).Seconds())
	if maxAge <= 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: false, // must be JS-readable for the double-submit pattern
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookies removes both auth cookies by setting Max-Age=0.
func clearSessionCookies(w http.ResponseWriter, cfg *config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   0,
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   0,
		HttpOnly: false,
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ── CSRF middleware ────────────────────────────────────────────────────────────

// CSRFMiddleware implements the double-submit cookie CSRF check for all
// non-idempotent requests (POST / PUT / PATCH / DELETE). It reads the
// ironsight_csrf cookie and the X-CSRF-Token request header, and returns
// 403 if they do not match or if either is absent.
//
// GET / HEAD / OPTIONS are unconditionally passed through — they must not
// cause side-effects per HTTP semantics, so CSRF is not required.
//
// This middleware must be mounted INSIDE the RequireAuth group so that
// unauthenticated requests are already rejected before we try to read
// the CSRF cookie. Routes that don't carry a session cookie (public
// endpoints, the login route, the Milesight sense webhook) are mounted
// OUTSIDE the RequireAuth group and are therefore never seen by this
// middleware.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "csrf cookie missing", http.StatusForbidden)
			return
		}
		header := r.Header.Get("X-CSRF-Token")
		if header == "" || header != cookie.Value {
			http.Error(w, "csrf token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ── Login ─────────────────────────────────────────────────────────────────

type loginRequest struct {
	Username string `json:"username"` // accepts username or email
	Password string `json:"password"`
	// MFACode is supplied on the second pass after the first pass returned
	// `mfa_required: true`. Empty on initial requests; if MFA is enabled
	// for the user it must be a valid TOTP code or one of the user's
	// remaining recovery codes.
	MFACode string `json:"mfa_code,omitempty"`
}

type loginResponse struct {
	Token string               `json:"token"`
	User  *database.UserPublic `json:"user"`
	// PasswordExpired is true when the user's password is past its
	// rotation horizon (database.PasswordMaxAge). The frontend should
	// route to a forced-change screen when this is set; the API still
	// honors the token in the meantime so the change-password call
	// itself can authenticate.
	PasswordExpired bool `json:"password_expired,omitempty"`
}

// mfaChallengeResponse is what the API returns when the username +
// password are correct but the user has MFA enabled and the request
// did not include a code. The frontend uses `mfa_required: true` to
// switch to the TOTP input form and resubmit with `mfa_code`.
//
// No token is issued at this stage — there is no preauth-half-token
// in the API. The user must replay username + password + code on the
// second submission. This avoids the entire "preauth token leaks"
// attack class at a small UX cost.
type mfaChallengeResponse struct {
	MFARequired bool `json:"mfa_required"`
}

// logFailedLogin emits one audit_log row for each 401 on /auth/login.
// Fire-and-forget: the response to the caller is not delayed by the
// audit write, and an audit failure is logged but never bubbled up —
// blocking authentication on a broken audit sink would be a bigger
// problem than the missing row.
//
// The reason string is coarse on purpose ("unknown_user", "bad_password",
// "locked") — enough for an auditor to see patterns, not enough to help
// an attacker refine their guesses from the audit log itself (we return
// the same "invalid credentials" response regardless).
func logFailedLogin(db *database.DB, r *http.Request, attemptedUsername, reason string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		entry := &database.AuditEntry{
			// UserID left zero — the whole point is that we don't have a
			// verified identity yet. The attempted username + IP is what
			// matters for forensic review.
			Username:   attemptedUsername,
			Action:     "login_failed",
			TargetType: "auth",
			TargetID:   reason,
			IPAddress:  clientIP(r),
		}
		if err := db.InsertAuditEntry(ctx, entry); err != nil {
			log.Printf("[AUDIT] Failed to log failed-login: %v", err)
		}
	}()
}

// HandleLogin authenticates a user by username or email and returns a JWT.
//
// UL 827B hardening layered on top of the original flow:
//   - Account lockout after LockoutThreshold consecutive failures
//   - Every 401 is written to the (append-only) audit_log with reason
//   - Rate limiting is enforced by RateLimitLogin middleware upstream,
//     not here — this function trusts that it's only invoked when the
//     per-IP budget allows.
//
// The response payload is identical across failure modes so response
// introspection can't distinguish "user exists, wrong password" from
// "user doesn't exist" from "locked" — the authoritative record of
// why each attempt failed lives only in audit_log.
func HandleLogin(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}

		user, err := db.GetUserByUsernameOrEmail(r.Context(), req.Username)
		if err != nil || user == nil {
			logFailedLogin(db, r, req.Username, "unknown_user")
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		// Lockout check before password verification: a correct password
		// during a lockout window must still be rejected. This is what
		// makes the control meaningful — otherwise an attacker who gets
		// lucky on attempt 6 during the lockout still wins.
		locked, until, _ := db.IsUserLocked(r.Context(), user.ID)
		if locked {
			logFailedLogin(db, r, req.Username, "locked")
			if until != nil {
				w.Header().Set("Retry-After", until.UTC().Format(time.RFC1123))
			}
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		if !auth.CheckPassword(user.PasswordHash, req.Password) {
			attempts, nowLocked, _ := db.RegisterFailedLogin(r.Context(), req.Username)
			reason := "bad_password"
			if nowLocked {
				reason = "bad_password_lockout_triggered"
			}
			logFailedLogin(db, r, req.Username, reason)
			logging.FromContext(r.Context()).LogAttrs(r.Context(), slog.LevelWarn, "auth_failed_login",
				slog.String("username", req.Username),
				slog.String("client_ip", clientIP(r)),
				slog.Int("attempts", attempts),
				slog.Bool("now_locked", nowLocked),
				slog.String("reason", reason),
			)
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		// MFA gate. Password was right; if the user has MFA enabled we
		// either consume the supplied code or signal the frontend to
		// prompt for one. Failures here count as failed logins so a
		// password leak doesn't get an unlimited TOTP-guess budget.
		mfaState, _ := db.GetMFAState(r.Context(), user.ID)
		if mfaState != nil && mfaState.Enabled {
			if req.MFACode == "" {
				// Don't increment failed_login_attempts here — the user
				// supplied a correct password and we just need a second
				// step. Returning the challenge object lets the frontend
				// re-prompt without showing a "wrong credentials" error.
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(mfaChallengeResponse{MFARequired: true})
				return
			}
			if !verifyMFACode(r.Context(), mfaState, req.MFACode, db, user.ID) {
				attempts, nowLocked, _ := db.RegisterFailedLogin(r.Context(), req.Username)
				reason := "bad_mfa"
				if nowLocked {
					reason = "bad_mfa_lockout_triggered"
				}
				logFailedLogin(db, r, req.Username, reason)
				logging.FromContext(r.Context()).LogAttrs(r.Context(), slog.LevelWarn, "auth_bad_mfa",
					slog.String("username", req.Username),
					slog.String("client_ip", clientIP(r)),
					slog.Int("attempts", attempts),
					slog.Bool("now_locked", nowLocked),
					slog.String("reason", reason),
				)
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
		}

		// Success — clear any stale failure counter so a legitimate user
		// who typo'd a couple of times before getting it right doesn't
		// carry a near-lockout state into future sessions.
		_ = db.ClearFailedLogins(r.Context(), user.ID)

		token, _, err := auth.SignToken(
			user.ID.String(),
			user.Username,
			user.Role,
			user.DisplayName,
			user.OrganizationID,
			cfg.JWTSecret,
		)
		if err != nil {
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}

		// Password rotation check — soft enforcement: expired = flag in
		// the response, not a 401. The token is still issued so the user
		// can authenticate the change-password call itself.
		expired, _ := db.PasswordExpired(r.Context(), user.ID)

		pub := &database.UserPublic{
			ID:              user.ID,
			Username:        user.Username,
			Role:            user.Role,
			DisplayName:     user.DisplayName,
			Email:           user.Email,
			Phone:           user.Phone,
			OrganizationID:  user.OrganizationID,
			AssignedSiteIDs: user.AssignedSiteIDs,
			CreatedAt:       user.CreatedAt,
			UpdatedAt:       user.UpdatedAt,
		}

		// P1-A-02 PR3: set ironsight_session (HttpOnly) and ironsight_csrf
		// (JS-readable) cookies. The session JWT is also returned in the
		// response body for legacy clients that still parse it, but RequireAuth
		// no longer reads the Authorization header — the cookie IS the auth.
		csrfToken, err := generateCSRFToken()
		if err != nil {
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}
		expiry := time.Now().Add(24 * time.Hour)
		setSessionCookies(w, token, csrfToken, expiry, cfg)

		writeJSON(w, loginResponse{Token: token, User: pub, PasswordExpired: expired})
	}
}

// HandleLogout revokes the bearer token used to authenticate this
// request. The jti is added to revoked_tokens; the JWT remains
// cryptographically valid until its natural exp but the auth
// middleware will reject any further use.
//
// P1-A-02 part 2: both ironsight_session and ironsight_csrf cookies are
// cleared (Max-Age=0) regardless of whether revocation succeeds, so the
// browser drops them immediately.
//
// Returning 204 No Content even on errors below the JWT-parse layer is
// deliberate — a logout failure should never leak diagnostic info to
// an unauthenticated caller. The audit log captures the attempt
// regardless via the AuditMiddleware on the parent route group.
func HandleLogout(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if !ok || claims == nil {
			clearSessionCookies(w, cfg)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		userUUID, _ := uuid.Parse(claims.UserID)
		exp := time.Now().Add(24 * time.Hour)
		if claims.ExpiresAt != nil {
			exp = claims.ExpiresAt.Time
		}
		_ = db.RevokeToken(r.Context(), claims.ID, userUUID, exp)
		clearSessionCookies(w, cfg)
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleGetMe returns the current user's full profile from the DB
func HandleGetMe(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}
		user, err := db.GetUserByID(r.Context(), id)
		if err != nil || user == nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		pub := &database.UserPublic{
			ID:              user.ID,
			Username:        user.Username,
			Role:            user.Role,
			DisplayName:     user.DisplayName,
			Email:           user.Email,
			Phone:           user.Phone,
			OrganizationID:  user.OrganizationID,
			AssignedSiteIDs: user.AssignedSiteIDs,
			CreatedAt:       user.CreatedAt,
			UpdatedAt:       user.UpdatedAt,
		}
		writeJSON(w, pub)
	}
}

// ── JWT Middleware ────────────────────────────────────────────────────────

type contextKey string

const ContextKeyClaims contextKey = "claims"

// RequireAuth is a middleware that validates the session credential.
// After the cryptographic check it consults the revoked_tokens blocklist —
// a logged-out token is rejected even though its signature is still valid.
// The DB hit is one indexed point lookup on the jti primary key, so the
// cost is negligible compared to the bcrypt and JWT parsing already on the
// path.
//
// Two accepted auth paths (in priority order):
//
//  1. Reverse-proxy header-trust SSO: when cfg.SSOTrustHeader == "email" AND
//     the inbound request carries X-Forwarded-Email (injected by oauth2-proxy
//     via NPM in the BigView deployment), we trust the header, look up or
//     auto-provision the user, synthesize *auth.Claims into context, and skip
//     JWT entirely. SECURITY: only enable behind a trusted reverse proxy that
//     strips client-supplied X-Forwarded-Email — see config docs.
//
//  2. ironsight_session HttpOnly cookie: set by /auth/login (P1-A-02 part 2).
//     The JWT value is read from the cookie value and verified/revocation-
//     checked normally.
//
// The Authorization: Bearer <sessionJWT> header path was removed in P1-A-02
// PR3. A bare Authorization header without a valid cookie now returns 401.
func RequireAuth(cfg *config.Config, db *database.DB) func(http.Handler) http.Handler {
	// Build a fast lookup set of admin-allowlisted emails (case-insensitive).
	adminSet := make(map[string]struct{}, len(cfg.SSOAdminEmails))
	for _, e := range cfg.SSOAdminEmails {
		adminSet[strings.ToLower(strings.TrimSpace(e))] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Header-trust SSO path.
			if cfg.SSOTrustHeader == "email" {
				email := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Email")))
				if email != "" {
					role := cfg.SSODefaultRole
					if _, isAdmin := adminSet[email]; isAdmin {
						role = "admin"
					}
					user, err := db.GetOrCreateUserByEmail(r.Context(), email, role)
					if err != nil || user == nil {
						http.Error(w, "sso provisioning failed", http.StatusInternalServerError)
						return
					}
					// Promote allowlisted users that pre-existed at a lower role.
					if _, isAdmin := adminSet[email]; isAdmin && user.Role != "admin" {
						_, _ = db.Pool.Exec(r.Context(),
							`UPDATE users SET role='admin', updated_at=NOW() WHERE id=$1`, user.ID)
						user.Role = "admin"
					}
					claims := &auth.Claims{
						UserID:         user.ID.String(),
						Username:       user.Username,
						Role:           user.Role,
						DisplayName:    user.DisplayName,
						OrganizationID: user.OrganizationID,
					}
					ctx := contextWithValue(r.Context(), ContextKeyClaims, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Header missing — fall through to cookie path (emergency local login).
			}

			// P1-A-02 PR3: the Authorization: Bearer header path has been retired.
			// The ONLY accepted session credential is the ironsight_session HttpOnly
			// cookie set by /auth/login. Never log the raw token value.
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}
			rawToken := cookie.Value

			claims, err := auth.ParseToken(rawToken, cfg.JWTSecret)
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Revocation check. claims.ID is the jti. Empty jti means a
			// pre-revocation-feature token — accept it; those will age
			// out within 24h naturally.
			if claims.ID != "" && db != nil {
				revoked, err := db.IsTokenRevoked(r.Context(), claims.ID)
				if err == nil && revoked {
					http.Error(w, "token revoked", http.StatusUnauthorized)
					return
				}
			}

			// Store claims in context for downstream handlers
			ctx := r.Context()
			ctx = contextWithValue(ctx, ContextKeyClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
