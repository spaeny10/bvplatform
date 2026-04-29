package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"onvif-tool/internal/database"
)

// HandleLabelingStats returns queue health counts (admin only).
func HandleLabelingStats(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdminOrSupervisor(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		stats, err := db.GetLabelingStats(r.Context())
		if err != nil {
			http.Error(w, "stats query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, stats)
	}
}

// HandleListLabelJobs lists jobs, optionally filtered by status.
// GET /api/admin/labeling/jobs?status=pending&limit=50
func HandleListLabelJobs(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdminOrSupervisor(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		status := r.URL.Query().Get("status")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		jobs, err := db.ListLabelJobs(r.Context(), status, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if jobs == nil {
			jobs = []database.VLMLabelJob{}
		}
		writeJSON(w, jobs)
	}
}

// HandleClaimNextLabelJob claims the oldest pending job for the caller.
// POST /api/admin/labeling/jobs/next
func HandleClaimNextLabelJob(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdminOrSupervisor(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		annotatorID, err := uuid.Parse(claimsFromRequest(r).UserID)
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		job, err := db.NextPendingLabelJob(r.Context(), annotatorID)
		if err != nil {
			// No pending jobs returns pgx ErrNoRows
			if strings.Contains(err.Error(), "no rows") {
				writeJSON(w, map[string]interface{}{"job": nil, "queue_empty": true})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"job": job, "queue_empty": false})
	}
}

// HandleClaimLabelJob claims a specific job by ID.
// POST /api/admin/labeling/jobs/{id}/claim
func HandleClaimLabelJob(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdminOrSupervisor(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		jobID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid job id", http.StatusBadRequest)
			return
		}
		annotatorID, err := uuid.Parse(claimsFromRequest(r).UserID)
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		job, err := db.ClaimLabelJob(r.Context(), jobID, annotatorID)
		if err != nil {
			if strings.Contains(err.Error(), "no rows") {
				http.Error(w, "job not found or already claimed", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, job)
	}
}

// HandleSubmitLabel submits ground-truth for a claimed job.
// POST /api/admin/labeling/jobs/{id}/label
func HandleSubmitLabel(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdminOrSupervisor(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		jobID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid job id", http.StatusBadRequest)
			return
		}
		annotatorID, err := uuid.Parse(claimsFromRequest(r).UserID)
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}

		var body struct {
			Verdict              string   `json:"verdict"`
			CorrectedDescription string   `json:"corrected_description"`
			CorrectedThreat      string   `json:"corrected_threat"`
			Tags                 []string `json:"tags"`
			Notes                string   `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Verdict == "" {
			http.Error(w, "verdict required", http.StatusBadRequest)
			return
		}

		if err := db.SubmitLabel(r.Context(), jobID, annotatorID,
			body.Verdict, body.CorrectedDescription, body.CorrectedThreat,
			body.Tags, body.Notes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "job_id": jobID})
	}
}

// HandleExportLabeledDataset streams a JSONL file for fine-tuning.
// GET /api/admin/labeling/export?verdict=all
func HandleExportLabeledDataset(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(r) {
			http.Error(w, "forbidden — admin only", http.StatusForbidden)
			return
		}
		verdict := r.URL.Query().Get("verdict") // "all" | "correct" | "incorrect" | "needs_correction"
		data, err := db.ExportLabeledDataset(r.Context(), verdict)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ironsight_labels_%s.jsonl"`, verdict))
		w.Write(data)
	}
}
