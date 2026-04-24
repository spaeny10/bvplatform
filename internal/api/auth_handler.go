package api

import (
	"encoding/json"
	"net/http"
	"strings"

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
	Token string              `json:"token"`
	User  *database.UserPublic `json:"user"`
}

// HandleLogin authenticates a user by username or email and returns a JWT
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
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		if !auth.CheckPassword(user.PasswordHash, req.Password) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

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
