package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"onvif-tool/internal/auth"
	"onvif-tool/internal/database"
)

// ═══════════════════════════════════════════════════════════════
// Organizations (Companies)
// ═══════════════════════════════════════════════════════════════

func HandleListOrganizations(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgs, err := db.ListOrganizations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, orgs)
	}
}

func HandleCreateOrganization(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.OrganizationCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		org, err := db.CreateOrganization(r.Context(), &input)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, org)
	}
}

func HandleUpdateOrganization(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var input database.OrganizationCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := db.UpdateOrganization(r.Context(), id, &input); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

func HandleDeleteOrganization(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := db.DeleteOrganization(r.Context(), id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

// ═══════════════════════════════════════════════════════════════
// Sites
// ═══════════════════════════════════════════════════════════════

func HandleListSites(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sites, err := db.ListSites(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, sites)
	}
}

func HandleGetSite(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		site, err := db.GetSite(r.Context(), id)
		if err != nil {
			http.Error(w, "Site not found", 404)
			return
		}
		writeJSON(w, site)
	}
}

func HandleCreateSiteP(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.SiteCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		site, err := db.CreateSite(r.Context(), &input)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, site)
	}
}

func HandleDeleteSiteP(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := db.DeleteSite(r.Context(), id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

func HandleUpdateSite(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var input database.SiteCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := db.UpdateSite(r.Context(), id, &input); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		site, err := db.GetSite(r.Context(), id)
		if err != nil {
			writeJSON(w, map[string]string{"ok": "true"})
			return
		}
		writeJSON(w, site)
	}
}

// HandleUpdateSiteRecording is the admin-facing endpoint for the Recording
// tab of the site config modal. Every camera on this site inherits these
// values on its next recording restart (a full recording restart is
// triggered when the admin toggles a camera's Recording flag; we don't
// hot-swap settings on live recorders yet).
//
// PUT /api/v1/sites/{id}/recording
func HandleUpdateSiteRecording(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" && claims.Role != "soc_supervisor" {
			http.Error(w, "forbidden: admin only", http.StatusForbidden)
			return
		}

		id := chi.URLParam(r, "id")
		var input struct {
			RetentionDays     int    `json:"retention_days"`
			RecordingMode     string `json:"recording_mode"`
			PreBufferSec      int    `json:"pre_buffer_sec"`
			PostBufferSec     int    `json:"post_buffer_sec"`
			RecordingTriggers string `json:"recording_triggers"`
			RecordingSchedule string `json:"recording_schedule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		// Soft-validate — the recording engine will reject silly values but
		// clamp here to make the UI explain what happened.
		if input.RetentionDays < 0 {
			input.RetentionDays = 0
		}
		if input.PreBufferSec < 0 {
			input.PreBufferSec = 0
		}
		if input.PostBufferSec < 0 {
			input.PostBufferSec = 0
		}
		if input.RecordingMode != "continuous" && input.RecordingMode != "event" {
			http.Error(w, "recording_mode must be 'continuous' or 'event'", http.StatusBadRequest)
			return
		}

		if err := db.UpdateSiteRecording(r.Context(), id,
			input.RetentionDays, input.PreBufferSec, input.PostBufferSec,
			input.RecordingMode, input.RecordingTriggers, input.RecordingSchedule); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		site, err := db.GetSite(r.Context(), id)
		if err != nil || site == nil {
			writeJSON(w, map[string]string{"ok": "true"})
			return
		}
		writeJSON(w, site)
	}
}

// ═══════════════════════════════════════════════════════════════
// Site SOPs
// ═══════════════════════════════════════════════════════════════

func HandleListSiteSOPs(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "siteId")
		sops, err := db.ListSiteSOPs(r.Context(), siteID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, sops)
	}
}

func HandleCreateSiteSOP(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "siteId")
		var input database.SOPCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		input.SiteID = siteID
		sop, err := db.CreateSiteSOP(r.Context(), &input)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, sop)
	}
}

func HandleDeleteSiteSOP(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := db.DeleteSiteSOP(r.Context(), id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

func HandleUpdateSiteSOP(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var input database.SOPCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := db.UpdateSiteSOP(r.Context(), id, &input); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

func HandleListIncidents(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := r.URL.Query().Get("site_id")
		severity := r.URL.Query().Get("severity")
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		incidents, err := db.ListIncidents(r.Context(), siteID, severity, limit)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, incidents)
	}
}

// ═══════════════════════════════════════════════════════════════
// Company Users
// ═══════════════════════════════════════════════════════════════

func HandleListCompanyUsers(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := chi.URLParam(r, "companyId")
		users, err := db.ListCompanyUsers(r.Context(), orgID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, users)
	}
}

func HandleCreateCompanyUser(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		companyID := chi.URLParam(r, "companyId")
		var input database.CompanyUserCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		input.CompanyID = companyID
		if input.Password == "" {
			input.Password = "demo123"
		}
		user, err := db.CreateCompanyUser(r.Context(), &input)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, user)
	}
}

func HandleDeleteCompanyUser(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "userId")
		if err := db.DeleteCompanyUser(r.Context(), id); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

// ═══════════════════════════════════════════════════════════════
// Operators
// ═══════════════════════════════════════════════════════════════

func HandleListOperators(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops, err := db.ListOperators(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, ops)
	}
}

func HandleCreateOperator(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		var req struct {
			Name     string `json:"name"`
			Callsign string `json:"callsign"`
			Email    string `json:"email"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Name == "" || req.Callsign == "" {
			http.Error(w, "name and callsign required", http.StatusBadRequest)
			return
		}

		// Optionally create a linked user account
		var userID *string
		if req.Username != "" && req.Password != "" {
			hash, err := auth.HashPassword(req.Password)
			if err != nil {
				http.Error(w, "failed to hash password", http.StatusInternalServerError)
				return
			}
			u, err := db.CreateUser(r.Context(), &database.UserCreate{
				Username:    req.Username,
				Password:    req.Password,
				Role:        "soc_operator",
				DisplayName: req.Name,
				Email:       req.Email,
			}, hash)
			if err != nil {
				http.Error(w, "failed to create user account: "+err.Error(), http.StatusConflict)
				return
			}
			uid := u.ID.String()
			userID = &uid
		}

		op, err := db.CreateOperator(r.Context(), req.Name, req.Callsign, req.Email, userID)
		if err != nil {
			http.Error(w, "failed to create operator: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, op)
	}
}

func HandleGetCurrentOperator(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Try to find the operator record linked to this user account
		op, err := db.GetOperatorByUserID(r.Context(), claims.UserID)
		if err == nil {
			writeJSON(w, op)
			return
		}

		// No operator record — synthesize identity from JWT claims.
		// Handles admin users and anyone without an operator row.
		displayName := claims.DisplayName
		if displayName == "" {
			displayName = claims.Username
		}
		callsign := strings.ToUpper(claims.Username)
		if len(callsign) > 8 {
			callsign = callsign[:8]
		}
		writeJSON(w, &database.Operator{
			ID:       "user-" + claims.UserID,
			Name:     displayName,
			Callsign: callsign,
			Status:   "available",
		})
	}
}

// ═══════════════════════════════════════════════════════════════
// Security Events
// ═══════════════════════════════════════════════════════════════

func HandleCreateSecurityEvent(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.SecurityEventCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		// Enrich with operator callsign from JWT
		if claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims); claims != nil {
			if input.OperatorCallsign == "" {
				input.OperatorCallsign = strings.ToUpper(claims.Username)
			}
		}
		event, err := db.CreateSecurityEvent(r.Context(), &input)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// Capture the dispositioning operator's identity for both the
		// SLA report and the dual-operator self-check. The user_id is
		// trusted from JWT claims; the callsign falls back to username
		// only if the client didn't supply one.
		var disposedByID uuid.UUID
		ackCallsign := input.OperatorCallsign
		if claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims); claims != nil {
			if uid, err := uuid.Parse(claims.UserID); err == nil {
				disposedByID = uid
			}
			if ackCallsign == "" {
				ackCallsign = strings.ToUpper(claims.Username)
			}
		}
		input.DisposedByUserID = disposedByID

		// Archive the alarm — remove it from the SOC dispatch queue.
		if input.AlarmID != "" {
			_ = db.AcknowledgeAlarm(r.Context(), input.AlarmID, disposedByID, ackCallsign)
			// Level 1 AI validation: compare AI threat assessment vs operator disposition
			_ = db.ComputeAICorrectness(r.Context(), input.AlarmID, input.DispositionCode)
		}
		writeJSON(w, map[string]string{"event_id": event.ID})
	}
}

func HandleListSecurityEvents(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := r.URL.Query().Get("site_id")
		events, err := db.ListSecurityEvents(r.Context(), siteID, nil)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, events)
	}
}

// HandleVerifySecurityEvent is the dual-operator sign-off endpoint.
// Restricted to soc_supervisor or admin roles, and the verifier must
// not be the same user who originally dispositioned the event. UL 827B
// reviewers expect this for any high-severity disposition that gets
// escalated to law enforcement; TMA-AVS-01 also wants the structured
// "video verified by SOC operator" record.
//
// Idempotent within the success path: re-verifying a verified event
// is a deliberate no-op (returns 409 Already Verified) so an attacker
// with a stolen supervisor token can't rewrite who signed off.
func HandleVerifySecurityEvent(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" && claims.Role != "soc_supervisor" {
			http.Error(w, "supervisor or admin role required", http.StatusForbidden)
			return
		}

		eventID := chi.URLParam(r, "id")
		if eventID == "" {
			http.Error(w, "event id required", http.StatusBadRequest)
			return
		}

		verifierID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}
		callsign := strings.ToUpper(claims.Username)

		switch err := db.VerifySecurityEvent(r.Context(), eventID, verifierID, callsign); err {
		case nil:
			w.WriteHeader(http.StatusNoContent)
		case database.ErrSelfVerification:
			http.Error(w, "verifier must be a different operator", http.StatusConflict)
		case database.ErrAlreadyVerified:
			http.Error(w, "event already verified", http.StatusConflict)
		default:
			http.Error(w, "verification failed: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// ═══════════════════════════════════════════════════════════════
// Camera Site Assignment
// ═══════════════════════════════════════════════════════════════

func HandleGetSiteCameras(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "siteId")
		cameras, err := db.GetSiteCameras(r.Context(), siteID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, cameras)
	}
}

func HandleAssignCamera(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "siteId")
		var input struct {
			CameraID      string `json:"camera_id"`
			LocationLabel string `json:"location_label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := db.AssignCameraToSite(r.Context(), input.CameraID, siteID, input.LocationLabel); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true", "site_id": siteID, "camera_id": input.CameraID})
	}
}

func HandleUnassignCamera(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cameraID := chi.URLParam(r, "cameraId")
		if err := db.UnassignCameraFromSite(r.Context(), cameraID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

// ═══════════════════════════════════════════════════════════════
// Platform Camera Registry
// ═══════════════════════════════════════════════════════════════

// HandleListAllPlatformCameras returns all cameras with site assignment info.
// Used by the admin device management modal.
func HandleListAllPlatformCameras(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cameras, err := db.ListAllPlatformCameras(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, cameras)
	}
}

// ═══════════════════════════════════════════════════════════════
// Speaker Site Assignment
// ═══════════════════════════════════════════════════════════════

func HandleListAllPlatformSpeakers(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		speakers, err := db.ListAllPlatformSpeakers(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, speakers)
	}
}

func HandleAssignSpeaker(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := chi.URLParam(r, "siteId")
		var input struct {
			SpeakerID     string `json:"speaker_id"`
			LocationLabel string `json:"location_label"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		if err := db.AssignSpeakerToSite(r.Context(), input.SpeakerID, siteID, input.LocationLabel); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true", "site_id": siteID, "speaker_id": input.SpeakerID})
	}
}

func HandleUnassignSpeaker(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		speakerID := chi.URLParam(r, "speakerId")
		if err := db.UnassignSpeakerFromSite(r.Context(), speakerID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"ok": "true"})
	}
}

// ═══════════════════════════════════════════════════════════════
// Incident Detail
// ═══════════════════════════════════════════════════════════════

func HandleGetIncident(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		// SOC incident (INC- prefix) → return incident + child alarms
		if strings.HasPrefix(id, "INC-") {
			inc, alarms, err := db.GetIncidentWithAlarms(r.Context(), id)
			if err != nil || inc == nil {
				http.Error(w, "Incident not found", 404)
				return
			}
			writeJSON(w, map[string]interface{}{
				"incident": inc,
				"alarms":   alarms,
			})
			return
		}

		// Legacy security event detail (EVT- prefix, portal view)
		detail, err := db.GetSecurityEventByID(r.Context(), id)
		if err != nil {
			http.Error(w, "Incident not found", 404)
			return
		}
		writeJSON(w, detail)
	}
}

// ═══════════════════════════════════════════════════════════════
// Active Alarm escalation
// ═══════════════════════════════════════════════════════════════

func HandleEscalateAlarm(db *database.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alarmID := chi.URLParam(r, "alarmId")
		var body struct {
			Level int `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		db.EscalateActiveAlarm(r.Context(), alarmID, body.Level)
		writeJSON(w, map[string]interface{}{"ok": true, "alarm_id": alarmID, "level": body.Level})
	}
}

// ═══════════════════════════════════════════════════════════════
// Shift Handoffs
// ═══════════════════════════════════════════════════════════════

func HandleListHandoffs(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		toOperatorID := r.URL.Query().Get("to")
		if toOperatorID == "" {
			writeJSON(w, []interface{}{})
			return
		}
		handoffs, err := db.ListHandoffs(r.Context(), toOperatorID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, handoffs)
	}
}

func HandleCreateHandoff(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.ShiftHandoffCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", 400)
			return
		}
		h, err := db.CreateHandoff(r.Context(), &input)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, h)
	}
}

func HandleGetDeviceHistory(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceType := r.URL.Query().Get("type") // "camera" or "speaker"
		deviceID := r.URL.Query().Get("id")
		if deviceType == "" || deviceID == "" {
			http.Error(w, "type and id required", 400)
			return
		}
		history, err := db.GetDeviceHistory(r.Context(), deviceType, deviceID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, history)
	}
}

// Placeholder handlers for endpoints the frontend expects
func HandleSiteLocks(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { writeJSON(w, []interface{}{}) }
}

func HandleOperatorHandoffs(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operatorID := chi.URLParam(r, "operatorId")
		handoffs, err := db.ListHandoffs(r.Context(), operatorID)
		if err != nil {
			writeJSON(w, []interface{}{})
			return
		}
		writeJSON(w, handoffs)
	}
}

func HandleDispatchQueue(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, oldestTs, err := db.GetActiveAlarmsCount(r.Context())
		if err != nil {
			writeJSON(w, map[string]interface{}{"depth": 0, "oldest_ts": nil})
			return
		}
		var oldest interface{} = nil
		if oldestTs > 0 {
			oldest = oldestTs
		}
		writeJSON(w, map[string]interface{}{"depth": count, "oldest_ts": oldest})
	}
}

func HandleFeatureFlags(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{
			"vlm_safety":         true,
			"semantic_search":    true,
			"evidence_sharing":   true,
			"global_ai_training": true,
		})
	}
}
