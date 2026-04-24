package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"onvif-tool/internal/auth"
	"onvif-tool/internal/database"
)

// Roles that can see every camera regardless of site assignment. These are the
// staff-side roles; the customer-facing roles (site_manager / customer /
// viewer) are always restricted to their `users.assigned_site_ids`.
var globalViewRoles = map[string]bool{
	"admin":          true,
	"soc_operator":   true,
	"soc_supervisor": true,
}

// claimsFromRequest extracts the caller's JWT claims from the request context.
// Returns nil if the request was not authenticated — callers should return 401
// in that case rather than trust an empty-claims user.
func claimsFromRequest(r *http.Request) *auth.Claims {
	if c, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims); ok {
		return c
	}
	return nil
}

// AuthorizedCameraIDs returns the list of camera UUIDs the user is permitted
// to see. The second return value is true when the list reflects an actual
// RBAC restriction (finite allowlist); false when the user has global view
// (admin/soc_operator/soc_supervisor) and should not be filtered.
//
// The "empty allowlist + restricted=true" case is legitimate: a customer
// with no sites assigned can see nothing. Callers must treat that as "return
// zero rows," not "apply no filter."
func AuthorizedCameraIDs(ctx context.Context, db *database.DB, claims *auth.Claims) (ids []uuid.UUID, restricted bool, err error) {
	if claims == nil {
		return nil, true, nil // no auth → no cameras
	}
	if globalViewRoles[claims.Role] {
		return nil, false, nil
	}

	// Customer-side roles: fetch the user's assigned_site_ids, then look up
	// every camera whose cameras.site_id is in that set.
	uid, perr := uuid.Parse(claims.UserID)
	if perr != nil {
		return nil, true, nil
	}
	user, err := db.GetUserByID(ctx, uid)
	if err != nil || user == nil {
		return nil, true, err
	}
	if len(user.AssignedSiteIDs) == 0 {
		return nil, true, nil
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT id FROM cameras WHERE site_id = ANY($1)`, user.AssignedSiteIDs)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, true, err
		}
		ids = append(ids, id)
	}
	return ids, true, nil
}

// CanAccessCamera is the single-camera form of AuthorizedCameraIDs. Returns
// true when the caller is permitted to view the given camera (by role or by
// site assignment). Used by handlers that take a :cameraID URL param.
func CanAccessCamera(ctx context.Context, db *database.DB, claims *auth.Claims, cameraID uuid.UUID) (bool, error) {
	if claims == nil {
		return false, nil
	}
	if globalViewRoles[claims.Role] {
		return true, nil
	}
	ids, restricted, err := AuthorizedCameraIDs(ctx, db, claims)
	if err != nil {
		return false, err
	}
	if !restricted {
		return true, nil
	}
	for _, id := range ids {
		if id == cameraID {
			return true, nil
		}
	}
	return false, nil
}
