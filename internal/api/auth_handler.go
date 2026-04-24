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
}

type loginResponse struct {
	Token string               `json:"token"`
	User  *database.UserPublic `json:"user"`
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

		// Success — clear any stale failure counter so a legitimate user
		// who typo'd a couple of times before getting it right doesn't
		// carry a near-lockout state into future sessions.
		_ = db.ClearFailedLogins(r.Context(), user.ID)

		token, err := auth.SignToken(
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
		writeJSON(w, loginResponse{Token: token, User: pub})
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

// RequireAuth is a middleware that validates the Bearer token
func RequireAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			// Store claims in context for downstream handlers
			ctx := r.Context()
			ctx = contextWithValue(ctx, ContextKeyClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
