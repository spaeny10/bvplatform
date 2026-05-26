package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
)

// validZoneTypes is the allowed set for ppe_zones.zone_type.
var validZoneTypes = map[string]bool{
	"work_area":    true,
	"no_go":        true,
	"ppe_required": true,
	"ppe_optional": true,
}

// validRuleTypes is the allowed set for compliance_rules.rule_type.
var validRuleTypes = map[string]bool{
	"ppe_required": true,
	"no_go":        true,
}

// canWriteZones returns true if the caller has site_manager or above.
// customer and viewer roles are read-only.
var zoneWriteRoles = map[string]bool{
	"admin":          true,
	"soc_operator":   true,
	"soc_supervisor": true,
	"site_manager":   true,
}

func canWriteZones(r *http.Request) bool {
	c := claimsFromRequest(r)
	return c != nil && zoneWriteRoles[c.Role]
}

// ── PPE Zone handlers ─────────────────────────────────────────────────────────

// HandleListPPEZones returns all PPE zones for a camera.
// GET /api/cameras/{id}/ppe-zones
func HandleListPPEZones(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		ok, err := CanAccessCamera(r.Context(), db, claims, cameraID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		zones, err := db.ListPPEZones(r.Context(), cameraID, claims.OrganizationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, zones)
	}
}

// HandleCreatePPEZone creates a new PPE zone for a camera.
// POST /api/cameras/{id}/ppe-zones
func HandleCreatePPEZone(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !canWriteZones(r) {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		ok, err := CanAccessCamera(r.Context(), db, claims, cameraID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var inp database.PPEZoneCreate
		if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validatePPEZoneCreate(inp); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Resolve site_id from the camera row (for FK population).
		cam, err := db.GetCamera(r.Context(), cameraID)
		if err != nil || cam == nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}
		var siteIDPtr *string
		if cam.SiteID != "" {
			siteIDPtr = &cam.SiteID
		}

		// Resolve caller UUID for created_by.
		callerUID, perr := uuid.Parse(claims.UserID)
		var createdByPtr *uuid.UUID
		if perr == nil {
			createdByPtr = &callerUID
		}

		zone, err := db.CreatePPEZone(r.Context(), cameraID, claims.OrganizationID, siteIDPtr, createdByPtr, inp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, zone)
	}
}

// HandleUpdatePPEZone updates an existing PPE zone.
// PUT /api/cameras/{id}/ppe-zones/{zoneId}
func HandleUpdatePPEZone(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !canWriteZones(r) {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		zoneID, err := uuid.Parse(chi.URLParam(r, "zoneId"))
		if err != nil {
			http.Error(w, "invalid zone id", http.StatusBadRequest)
			return
		}

		var inp database.PPEZoneCreate
		if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validatePPEZoneCreate(inp); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		n, err := db.UpdatePPEZone(r.Context(), zoneID, claims.OrganizationID, inp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n == 0 {
			http.Error(w, "zone not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleDeletePPEZone deletes a PPE zone. Returns 409 if compliance rules
// still reference it (per scope plan D1 — enforce pre-check so operators
// don't lose rules unexpectedly, even though the FK is ON DELETE CASCADE).
// DELETE /api/cameras/{id}/ppe-zones/{zoneId}
func HandleDeletePPEZone(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !canWriteZones(r) {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		zoneID, err := uuid.Parse(chi.URLParam(r, "zoneId"))
		if err != nil {
			http.Error(w, "invalid zone id", http.StatusBadRequest)
			return
		}

		// Pre-check: are there compliance rules still referencing this zone?
		n, err := db.CountComplianceRulesForZone(r.Context(), zoneID, claims.OrganizationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n > 0 {
			msg := fmt.Sprintf("zone has %d active compliance rule(s) — delete rules first", n)
			http.Error(w, msg, http.StatusConflict)
			return
		}

		affected, err := db.DeletePPEZone(r.Context(), zoneID, claims.OrganizationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(w, "zone not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Compliance Rule handlers ──────────────────────────────────────────────────

// HandleListComplianceRules returns compliance rules for a camera.
// GET /api/cameras/{id}/compliance-rules
func HandleListComplianceRules(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		ok, err := CanAccessCamera(r.Context(), db, claims, cameraID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		rules, err := db.ListComplianceRules(r.Context(), cameraID, claims.OrganizationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, rules)
	}
}

// HandleCreateComplianceRule creates a new compliance rule for a camera.
// POST /api/cameras/{id}/compliance-rules
func HandleCreateComplianceRule(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !canWriteZones(r) {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		ok, err := CanAccessCamera(r.Context(), db, claims, cameraID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var inp database.ComplianceRuleCreate
		if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validateComplianceRuleCreate(inp); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Verify the zone belongs to this organization.
		zone, err := db.GetPPEZone(r.Context(), inp.ZoneID, claims.OrganizationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if zone == nil {
			http.Error(w, "zone not found or does not belong to this organization", http.StatusBadRequest)
			return
		}

		cam, err := db.GetCamera(r.Context(), cameraID)
		if err != nil || cam == nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}
		var siteIDPtr *string
		if cam.SiteID != "" {
			siteIDPtr = &cam.SiteID
		}

		callerUID, perr := uuid.Parse(claims.UserID)
		var createdByPtr *uuid.UUID
		if perr == nil {
			createdByPtr = &callerUID
		}

		newID, err := db.CreateComplianceRule(r.Context(), cameraID, claims.OrganizationID, siteIDPtr, createdByPtr, inp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch the created row to return it.
		created, err := db.GetComplianceRule(r.Context(), newID, claims.OrganizationID)
		if err != nil || created == nil {
			// Rule was created; return minimal response.
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]string{"id": newID.String()})
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, created)
	}
}

// HandleUpdateComplianceRule updates an existing compliance rule.
// PUT /api/cameras/{id}/compliance-rules/{ruleId}
func HandleUpdateComplianceRule(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !canWriteZones(r) {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
		if err != nil {
			http.Error(w, "invalid rule id", http.StatusBadRequest)
			return
		}

		var inp database.ComplianceRuleCreate
		if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if err := validateComplianceRuleCreate(inp); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		n, err := db.UpdateComplianceRule(r.Context(), ruleID, claims.OrganizationID, cameraID, inp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if n == 0 {
			http.Error(w, "rule not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleDeleteComplianceRule deletes a compliance rule.
// DELETE /api/cameras/{id}/compliance-rules/{ruleId}
func HandleDeleteComplianceRule(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !canWriteZones(r) {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
		if err != nil {
			http.Error(w, "invalid rule id", http.StatusBadRequest)
			return
		}

		affected, err := db.DeleteComplianceRule(r.Context(), ruleID, claims.OrganizationID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(w, "rule not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── Validation helpers ────────────────────────────────────────────────────────

func validatePPEZoneCreate(inp database.PPEZoneCreate) error {
	if !validZoneTypes[inp.ZoneType] {
		return fmt.Errorf("invalid zone_type %q: must be work_area, no_go, ppe_required, or ppe_optional", inp.ZoneType)
	}
	if len(inp.Region) < 3 {
		return fmt.Errorf("region must have at least 3 points (got %d)", len(inp.Region))
	}
	if len(inp.Name) > 120 {
		return fmt.Errorf("name must be 120 characters or fewer")
	}
	return nil
}

func validateComplianceRuleCreate(inp database.ComplianceRuleCreate) error {
	if !validRuleTypes[inp.RuleType] {
		return fmt.Errorf("invalid rule_type %q: must be ppe_required or no_go", inp.RuleType)
	}
	if inp.RuleType == "ppe_required" && len(inp.PPEClasses) == 0 {
		return fmt.Errorf("ppe_classes must be non-empty for ppe_required rules")
	}
	if inp.ZoneID == uuid.Nil {
		return fmt.Errorf("zone_id is required")
	}
	return nil
}
