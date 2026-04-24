package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"onvif-tool/internal/auth"
	"onvif-tool/internal/database"
)

// ──────────────────── Audit Middleware ────────────────────

// AuditMiddleware logs all mutating requests (POST, PUT, PATCH, DELETE) to the audit log.
func AuditMiddleware(db *database.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit mutating methods
			method := r.Method
			if method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
				next.ServeHTTP(w, r)
				return
			}

			// Extract user info from JWT claims (set by RequireAuth)
			var userID uuid.UUID
			var username string
			if claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims); ok {
				if uid, err := uuid.Parse(claims.UserID); err == nil {
					userID = uid
				}
				username = claims.Username
			}

			// Derive action and target from the request path
			action, targetType, targetID := classifyRequest(method, r.URL.Path)

			// Skip noisy internal endpoints
			if action == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap the response writer to capture status code
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)

			// Only log successful mutations (2xx status)
			if sw.status >= 200 && sw.status < 300 {
				entry := &database.AuditEntry{
					UserID:     userID,
					Username:   username,
					Action:     action,
					TargetType: targetType,
					TargetID:   targetID,
					IPAddress:  r.RemoteAddr,
				}
				// Fire-and-forget DB insert (don't slow down the response)
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := db.InsertAuditEntry(ctx, entry); err != nil {
						log.Printf("[AUDIT] Failed to log entry: %v", err)
					}
				}()
			}
		})
	}
}

// statusWriter wraps ResponseWriter to capture the HTTP status code
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// classifyRequest maps HTTP method+path to audit action, target type, and target ID
func classifyRequest(method, path string) (action, targetType, targetID string) {
	// Normalize path
	parts := strings.Split(strings.Trim(path, "/"), "/")

	// /api/cameras, /api/cameras/{id}
	if len(parts) >= 2 && parts[0] == "api" {
		resource := parts[1]
		id := ""
		if len(parts) >= 3 {
			id = parts[2]
		}

		switch resource {
		case "cameras":
			targetType = "camera"
			targetID = id
			subResource := ""
			if len(parts) >= 4 {
				subResource = parts[3]
			}
			switch {
			case subResource == "ptz":
				return "", "", "" // skip PTZ noise
			case method == "POST" && id == "":
				action = "create_camera"
			case method == "PATCH":
				action = "update_camera"
			case method == "DELETE":
				action = "delete_camera"
			}
		case "users":
			targetType = "user"
			targetID = id
			switch method {
			case "POST":
				action = "create_user"
			case "DELETE":
				action = "delete_user"
			case "PATCH":
				if len(parts) >= 4 && parts[3] == "password" {
					action = "change_password"
				} else if len(parts) >= 4 && parts[3] == "role" {
					action = "change_role"
				}
			}
		case "settings":
			targetType = "settings"
			if method == "PUT" {
				action = "update_settings"
			}
		case "exports":
			targetType = "export"
			if method == "POST" {
				action = "create_export"
			}
		case "speakers":
			targetType = "speaker"
			targetID = id
			switch method {
			case "POST":
				if len(parts) >= 5 && parts[3] == "play" {
					action = "play_speaker"
				} else if id == "stop" {
					action = "stop_speaker"
				} else {
					action = "create_speaker"
				}
			case "DELETE":
				action = "delete_speaker"
			}
		case "audio-messages":
			targetType = "audio_message"
			targetID = id
			switch method {
			case "POST":
				action = "upload_audio"
			case "DELETE":
				action = "delete_audio"
			}
		case "bookmarks":
			targetType = "bookmark"
			targetID = id
			switch method {
			case "POST":
				action = "create_bookmark"
			case "DELETE":
				action = "delete_bookmark"
			}
		case "discover":
			targetType = "system"
			action = "discover_cameras"
		case "storage":
			targetType = "storage"
			if len(parts) >= 3 && parts[2] == "locations" {
				switch method {
				case "POST":
					action = "create_storage_location"
				case "PUT":
					action = "update_storage_location"
				case "DELETE":
					action = "delete_storage_location"
				}
			}
		}
	}

	// /auth/login
	if len(parts) >= 2 && parts[0] == "auth" && parts[1] == "login" {
		action = "login"
		targetType = "auth"
	}

	return
}

// ──────────────────── Audit API Handler ────────────────────

// HandleQueryAuditLog returns paginated audit entries
func HandleQueryAuditLog(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := r.URL.Query().Get("username")
		action := r.URL.Query().Get("action")
		targetType := r.URL.Query().Get("target_type")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

		if limit == 0 {
			limit = 50
		}

		entries, total, err := db.QueryAuditLog(r.Context(), username, action, targetType, limit, offset)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []database.AuditEntry{}
		}

		writeJSON(w, map[string]interface{}{
			"entries": entries,
			"total":   total,
			"limit":   limit,
			"offset":  offset,
		})
	}
}

// ──────────────────── Bookmark API Handlers ────────────────────

// HandleCreateBookmark creates a new timeline bookmark
func HandleCreateBookmark(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.BookmarkCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if input.Label == "" {
			http.Error(w, "label is required", http.StatusBadRequest)
			return
		}

		eventTime, err := time.Parse(time.RFC3339, input.EventTime)
		if err != nil {
			http.Error(w, "invalid event_time, expected RFC3339", http.StatusBadRequest)
			return
		}

		var userID uuid.UUID
		if claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims); ok {
			if uid, err := uuid.Parse(claims.UserID); err == nil {
				userID = uid
			}
		}

		severity := input.Severity
		if severity == "" {
			severity = "info"
		}

		bookmark := &database.Bookmark{
			CameraID:  input.CameraID,
			EventTime: eventTime,
			Label:     input.Label,
			Notes:     input.Notes,
			Severity:  severity,
			CreatedBy: userID,
		}

		if err := db.CreateBookmark(r.Context(), bookmark); err != nil {
			http.Error(w, "failed to create bookmark: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, bookmark)
	}
}

// HandleListBookmarks returns bookmarks for a time range, optionally filtered by camera
func HandleListBookmarks(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")

		start, _ := time.Parse(time.RFC3339, startStr)
		end, _ := time.Parse(time.RFC3339, endStr)
		if start.IsZero() {
			start = time.Now().Add(-24 * time.Hour)
		}
		if end.IsZero() {
			end = time.Now()
		}

		var cameraID *uuid.UUID
		if cidStr := r.URL.Query().Get("camera_id"); cidStr != "" {
			if id, err := uuid.Parse(cidStr); err == nil {
				cameraID = &id
			}
		}

		bookmarks, err := db.ListBookmarks(r.Context(), cameraID, start, end)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if bookmarks == nil {
			bookmarks = []database.Bookmark{}
		}
		writeJSON(w, bookmarks)
	}
}

// HandleDeleteBookmark removes a bookmark by ID
func HandleDeleteBookmark(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid ID", http.StatusBadRequest)
			return
		}
		if err := db.DeleteBookmark(r.Context(), id); err != nil {
			http.Error(w, "delete failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
