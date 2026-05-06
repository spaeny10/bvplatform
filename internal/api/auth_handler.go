package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"onvif-tool/internal/auth"
	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
)

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
			log.Printf("[AUTH] Failed login for %q from %s (attempt %d%s)",
				req.Username, clientIP(r), attempts,
				map[bool]string{true: ", locked", false: ""}[nowLocked])
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
				log.Printf("[AUTH] Bad MFA for %q from %s (attempt %d%s)",
					req.Username, clientIP(r), attempts,
					map[bool]string{true: ", locked", false: ""}[nowLocked])
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
		writeJSON(w, loginResponse{Token: token, User: pub, PasswordExpired: expired})
	}
}

// HandleLogout revokes the bearer token used to authenticate this
// request. The jti is added to revoked_tokens; the JWT remains
// cryptographically valid until its natural exp but the auth
// middleware will reject any further use.
//
// Returning 204 No Content even on errors below the JWT-parse layer is
// deliberate — a logout failure should never leak diagnostic info to
// an unauthenticated caller. The audit log captures the attempt
// regardless via the AuditMiddleware on the parent route group.
func HandleLogout(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if !ok || claims == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		userUUID, _ := uuid.Parse(claims.UserID)
		exp := time.Now().Add(24 * time.Hour)
		if claims.ExpiresAt != nil {
			exp = claims.ExpiresAt.Time
		}
		_ = db.RevokeToken(r.Context(), claims.ID, userUUID, exp)
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

// RequireAuth is a middleware that validates the Bearer token. After
// the cryptographic check it consults the revoked_tokens blocklist —
// a logged-out token is rejected even though its signature is still
// valid. The DB hit is one indexed point lookup on the jti primary
// key, so the cost is negligible compared to the bcrypt and JWT
// parsing already on the path.
//
// Reverse-proxy header-trust SSO: when cfg.SSOTrustHeader == "email" AND the
// inbound request carries X-Forwarded-Email (injected by oauth2-proxy via NPM
// in the BigView deployment), we trust the header, look up or auto-provision
// the user, synthesize *auth.Claims into context, and skip JWT entirely. The
// JWT path stays alive as an emergency local-login fallback and for
// service-to-service tokens. SECURITY: only enable behind a trusted reverse
// proxy that strips client-supplied X-Forwarded-Email — see config docs.
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
				// Header missing — fall through to JWT (emergency local login).
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				http.Error(w, "invalid authorization format", http.StatusUnauthorized)
				return
			}

			claims, err := auth.ParseToken(parts[1], cfg.JWTSecret)
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
