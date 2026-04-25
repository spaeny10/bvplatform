package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"onvif-tool/internal/database"
)

// HandleListSiteContacts returns the customer-maintained contact list
// for one site. RBAC-scoped: SOC roles see any site's contacts;
// customers / site_managers only see contacts at sites they have
// access to (org match or explicit assignment). Out-of-scope returns
// 404 — no existence leak.
func HandleListSiteContacts(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "id")
		if siteID == "" {
			http.Error(w, "site id required", http.StatusBadRequest)
			return
		}

		// Verify the caller can see this site at all. Reusing the same
		// scope-check we use for HandleGetSite so behavior stays
		// consistent.
		scope := callerScope(r, db)
		if !scope.IsUnscoped() && !canAccessSite(scope, siteID, db, r) {
			http.NotFound(w, r)
			return
		}

		contacts, err := db.GetCustomerContacts(r.Context(), siteID)
		if err != nil {
			http.Error(w, "fetch failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, contacts)
	}
}

// HandleUpdateSiteContacts replaces the full customer contact list
// for one site. Only site_manager / admin / soc_supervisor can edit
// — customers (the "viewer" tier of the customer side) can read but
// not modify. The site-scope check is identical to read so a site
// manager can't edit a different org's contacts even if they manage
// to guess the site_id.
func HandleUpdateSiteContacts(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "id")
		if siteID == "" {
			http.Error(w, "site id required", http.StatusBadRequest)
			return
		}

		scope := callerScope(r, db)
		switch scope.Role {
		case "admin", "soc_supervisor", "site_manager":
			// proceed
		default:
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !scope.IsUnscoped() && !canAccessSite(scope, siteID, db, r) {
			http.NotFound(w, r)
			return
		}

		var contacts []database.CustomerContact
		if err := json.NewDecoder(r.Body).Decode(&contacts); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := db.SetCustomerContacts(r.Context(), siteID, contacts); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// canAccessSite checks whether the supplied scope grants access to a
// specific site. SOC roles bypass via scope.IsUnscoped(). For customer
// / site_manager we check organization match first (cheapest), then
// the explicit assigned_site_ids list. Pulling the site row's
// organization_id is one indexed query.
func canAccessSite(scope database.CallerScope, siteID string, db *database.DB, r *http.Request) bool {
	for _, sid := range scope.AssignedSiteIDs {
		if sid == siteID {
			return true
		}
	}
	if scope.OrganizationID == "" {
		return false
	}
	var orgID string
	err := db.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(organization_id, '') FROM sites WHERE id=$1`, siteID,
	).Scan(&orgID)
	if err != nil {
		return false
	}
	return orgID == scope.OrganizationID
}
