package api

import (
	"encoding/json"
	"net/http"

	"onvif-tool/internal/auth"
	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
)

// HandleGetSettings returns the current system settings (any authenticated user)
func HandleGetSettings(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := db.GetSettings(r.Context())
		if err != nil {
			http.Error(w, "failed to load settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, settings)
	}
}

// HandleUpdateSettings saves new system settings (admin only)
func HandleUpdateSettings(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Admin-only guard
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || claims.Role != "admin" {
			http.Error(w, "admin access required", http.StatusForbidden)
			return
		}

		var s database.SystemSettings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := db.UpsertSettings(r.Context(), &s); err != nil {
			http.Error(w, "failed to save settings: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Reflect storage paths into the live config so new requests see them
		// immediately (a full restart is needed for recording engine changes)
		if s.RecordingsPath != "" {
			cfg.StoragePath = s.RecordingsPath
		}
		if s.ExportsPath != "" {
			cfg.ExportPath = s.ExportsPath
		}
		if s.HLSPath != "" {
			cfg.HLSPath = s.HLSPath
		}
		if s.SnapshotsPath != "" {
			cfg.ThumbnailPath = s.SnapshotsPath
		}
		if s.FFmpegPath != "" {
			cfg.FFmpegPath = s.FFmpegPath
		}
		if s.DefaultSegmentDuration > 0 {
			cfg.SegmentDuration = s.DefaultSegmentDuration
		}

		updated, err := db.GetSettings(r.Context())
		if err != nil {
			writeJSON(w, s)
			return
		}
		writeJSON(w, updated)
	}
}
