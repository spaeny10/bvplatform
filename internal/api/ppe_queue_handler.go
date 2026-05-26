package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// reviewRoles is the set of roles that may submit a review verdict.
// Customers can view findings (via the GET handler) but may not
// mark them reviewed/dismissed — that is a management action.
var reviewRoles = map[string]bool{
	"admin":          true,
	"soc_supervisor": true,
	"soc_operator":   true,
	"site_manager":   true,
}

// pendingReviewEntry is the API response shape for one queue entry.
// frame_url points at the dedicated frame-serve sub-route, scoped to
// the authenticated caller's session.
type pendingReviewEntry struct {
	ID             string          `json:"id"`
	CameraID       string          `json:"camera_id"`
	CameraName     string          `json:"camera_name"`
	SiteID         *string         `json:"site_id,omitempty"`
	SiteName       string          `json:"site_name,omitempty"`
	DetectionClass string          `json:"detection_class"`
	MissingLabel   string          `json:"missing_label"`
	Confidence     float64         `json:"confidence"`
	BoundingBoxes  json.RawMessage `json:"bounding_boxes"`
	FrameURL       string          `json:"frame_url"`
	Status         string          `json:"status"`
	CreatedAt      string          `json:"created_at"`
	ReviewedBy     *string         `json:"reviewed_by,omitempty"`
	ReviewedAt     *string         `json:"reviewed_at,omitempty"`
	Notes          *string         `json:"notes,omitempty"`
}

// HandleListPendingReview handles GET /api/portal/pending-review.
// Returns PPE violation findings for the caller's organization only.
func HandleListPendingReview(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.OrganizationID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}
		// OrganizationID is a TEXT column in the DB (not UUID); use directly.
		orgID := claims.OrganizationID

		// Query params.
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		validStatuses := map[string]bool{
			"pending": true, "reviewed_compliant": true,
			"reviewed_violation": true, "dismissed": true,
		}
		if !validStatuses[status] {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}

		f := database.PPEQueueFilter{
			OrganizationID: orgID,
			Status:         status,
		}
		if cidStr := r.URL.Query().Get("camera_id"); cidStr != "" {
			if cid, err := uuid.Parse(cidStr); err == nil {
				f.CameraID = &cid
			}
		}
		if beforeStr := r.URL.Query().Get("before"); beforeStr != "" {
			if t, err := time.Parse(time.RFC3339, beforeStr); err == nil {
				f.Before = &t
			}
		}

		entries, err := db.ListPPEQueueEntries(r.Context(), f)
		if err != nil {
			log.Printf("[PPE] ListPPEQueueEntries: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []database.PendingReviewQueueRow{}
		}

		totalPending, _ := db.CountPPEQueuePending(r.Context(), orgID)

		// Build response, constructing frame_url for each entry.
		out := make([]pendingReviewEntry, 0, len(entries))
		for _, e := range entries {
			re := pendingReviewEntry{
				ID:             e.ID.String(),
				CameraID:       e.CameraID.String(),
				CameraName:     e.CameraName,
				SiteID:         e.SiteID, // already *string
				SiteName:       e.SiteName,
				DetectionClass: e.DetectionClass,
				MissingLabel:   e.MissingLabel,
				Confidence:     e.Confidence,
				BoundingBoxes:  e.BoundingBoxes,
				Status:         e.Status,
				CreatedAt:      e.CreatedAt.UTC().Format(time.RFC3339),
				Notes:          e.Notes,
			}
			if e.ReviewedBy != nil {
				s := e.ReviewedBy.String()
				re.ReviewedBy = &s
			}
			if e.ReviewedAt != nil {
				s := e.ReviewedAt.UTC().Format(time.RFC3339)
				re.ReviewedAt = &s
			}
			// Frame URL: dedicated serve sub-route, authenticated by session cookie.
			re.FrameURL = "/api/portal/pending-review/" + e.ID.String() + "/frame"
			out = append(out, re)
		}

		var nextCursor *string
		if len(entries) > 0 {
			last := entries[len(entries)-1].CreatedAt.UTC().Format(time.RFC3339)
			nextCursor = &last
		}

		writeJSON(w, map[string]interface{}{
			"entries":       out,
			"total_pending": totalPending,
			"next_cursor":   nextCursor,
		})
	}
}

// HandleReviewPendingEntry handles POST /api/portal/pending-review/{id}/review.
// Requires site_manager or above. Returns 404 when the row doesn't exist or
// belongs to a different organization (intentional — don't leak cross-tenant
// existence).
func HandleReviewPendingEntry(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !reviewRoles[claims.Role] {
			http.Error(w, "forbidden: site_manager or above required", http.StatusForbidden)
			return
		}
		if claims.OrganizationID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}

		entryID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		// OrganizationID is TEXT in the DB; use directly.
		orgID := claims.OrganizationID

		reviewerID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user_id in token", http.StatusBadRequest)
			return
		}

		var body struct {
			Status string  `json:"status"`
			Notes  *string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		validReviewStatuses := map[string]bool{
			"reviewed_compliant": true,
			"reviewed_violation": true,
			"dismissed":          true,
		}
		if !validReviewStatuses[body.Status] {
			http.Error(w, "invalid status: must be reviewed_compliant, reviewed_violation, or dismissed", http.StatusUnprocessableEntity)
			return
		}
		if body.Notes != nil && len(*body.Notes) > 2000 {
			http.Error(w, "notes exceeds 2000 character limit", http.StatusBadRequest)
			return
		}

		// Verify the row exists and belongs to the caller's org before updating.
		existing, err := db.GetPPEQueueEntry(r.Context(), entryID, orgID)
		if err != nil {
			log.Printf("[PPE] GetPPEQueueEntry: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if existing == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if err := db.UpdatePPEQueueStatus(r.Context(), entryID, orgID, reviewerID, body.Status, body.Notes); err != nil {
			log.Printf("[PPE] UpdatePPEQueueStatus: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		now := time.Now().UTC()
		writeJSON(w, map[string]interface{}{
			"id":          entryID.String(),
			"status":      body.Status,
			"reviewed_at": now.Format(time.RFC3339),
		})
	}
}

// HandleServePPEFrame handles GET /api/portal/pending-review/{id}/frame.
// Streams the PPE evidence JPEG directly from disk. The row is fetched to
// re-verify tenant scope — the caller's session cookie is the auth.
func HandleServePPEFrame(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.OrganizationID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}

		entryID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		orgID := claims.OrganizationID

		row, err := db.GetPPEQueueEntry(r.Context(), entryID, orgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if row == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		framesDir := cfg.PPEFramesDir
		if framesDir == "" {
			framesDir = "/tank/data/ironsight/ppe-frames"
		}

		// frame_path is relative to PPEFramesDir: org_id/YYYY-MM-DD/timestamp.jpg
		// Reject any path that escapes the base directory.
		rel := filepath.Clean(row.FramePath)
		if strings.HasPrefix(rel, "..") {
			http.Error(w, "invalid frame path", http.StatusBadRequest)
			return
		}

		absPath := filepath.Join(framesDir, rel)
		f, err := os.Open(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "frame not found", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeContent(w, r, "frame.jpg", row.CreatedAt, f)
	}
}
