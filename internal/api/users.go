package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/database"
)

// HandleListUsers returns all users (any authenticated user can list)
func HandleListUsers(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := db.ListUsers(r.Context())
		if err != nil {
			http.Error(w, "failed to list users: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, users)
	}
}

// HandleCreateUser adds a new unified platform user (admin only)
func HandleCreateUser(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		var req database.UserCreate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username and password required", http.StatusBadRequest)
			return
		}
		if err := auth.ValidatePassword(req.Password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Role == "" {
			req.Role = "viewer"
		}
		if !database.ValidRoles[req.Role] {
			http.Error(w, "role must be one of: admin, soc_operator, soc_supervisor, site_manager, customer, viewer", http.StatusBadRequest)
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}

		u, err := db.CreateUser(r.Context(), &req, hash)
		if err != nil {
			http.Error(w, "failed to create user: "+err.Error(), http.StatusConflict)
			return
		}

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, database.UserPublic{
			ID:              u.ID,
			Username:        u.Username,
			Role:            u.Role,
			DisplayName:     u.DisplayName,
			Email:           u.Email,
			Phone:           u.Phone,
			OrganizationID:  u.OrganizationID,
			AssignedSiteIDs: u.AssignedSiteIDs,
			CreatedAt:       u.CreatedAt,
			UpdatedAt:       u.UpdatedAt,
		})
	}
}

// HandleDeleteUser removes a user (admin only; cannot delete self)
func HandleDeleteUser(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		if id.String() == claims.UserID {
			http.Error(w, "cannot delete your own account", http.StatusBadRequest)
			return
		}

		if err := db.DeleteUser(r.Context(), id); err != nil {
			http.Error(w, "failed to delete user: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type changePasswordRequest struct {
	Password string `json:"password"`
}

// HandleUpdateUserPassword lets admins or the user themselves change a password
func HandleUpdateUserPassword(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		if claims.Role != "admin" && id.String() != claims.UserID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var req changePasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
			http.Error(w, "password required", http.StatusBadRequest)
			return
		}
		if err := auth.ValidatePassword(req.Password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		if err := db.UpdateUserPassword(r.Context(), id, hash); err != nil {
			http.Error(w, "failed to update password: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type changeRoleRequest struct {
	Role string `json:"role"`
}

// HandleUpdateUserRole changes a user's role (admin only)
func HandleUpdateUserRole(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}

		var req changeRoleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" {
			http.Error(w, "role required", http.StatusBadRequest)
			return
		}
		if !database.ValidRoles[req.Role] {
			http.Error(w, "role must be one of: admin, soc_operator, soc_supervisor, site_manager, customer, viewer", http.StatusBadRequest)
			return
		}

		if err := db.UpdateUserRole(r.Context(), id, req.Role); err != nil {
			http.Error(w, "failed to update role: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleUpdateUserProfile updates non-auth profile fields (display_name, email, phone, org, sites)
func HandleUpdateUserProfile(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		// Users can update their own profile; admins can update anyone
		if claims.Role != "admin" && id.String() != claims.UserID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var update database.UserProfileUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if update.AssignedSiteIDs == nil {
			update.AssignedSiteIDs = []string{}
		}

		if err := db.UpdateUserProfile(r.Context(), id, &update); err != nil {
			http.Error(w, "failed to update profile: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Return fresh user
		u, err := db.GetUserByID(r.Context(), id)
		if err != nil || u == nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		writeJSON(w, database.UserPublic{
			ID:              u.ID,
			Username:        u.Username,
			Role:            u.Role,
			DisplayName:     u.DisplayName,
			Email:           u.Email,
			Phone:           u.Phone,
			OrganizationID:  u.OrganizationID,
			AssignedSiteIDs: u.AssignedSiteIDs,
			CreatedAt:       u.CreatedAt,
			UpdatedAt:       u.UpdatedAt,
		})
	}
}
