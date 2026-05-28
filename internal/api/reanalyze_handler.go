package api

// reanalyze_handler.go — P4-SCHEMA-06 API endpoints.
//
// Deliverable §2 (optional admin async endpoint) + Deliverable §5
// (backend model_version_id filter on the detection-listing path).
//
// Admin async endpoint
// ─────────────────────
// POST /api/admin/reanalyze
//   Body: {model_version_id, from, to, organization_id?, dry_run?}
//   Auth: admin role only (requireAdmin)
//   Returns immediately with {run_id, status:"queued"}.
//   The reanalysis runs in a goroutine. Poll GET /api/admin/reanalyze/{run_id}
//   for completion (status: "running"|"done"|"error").
//
// GET /api/admin/reanalyze/{run_id}
//   Returns the analysis_runs row for run_id.  The ended_at field is NULL
//   while the run is in progress and set when it completes.
//
// Detection-listing filter
// ─────────────────────────
// GET /api/v1/detections
//   Query params:
//     model_version_id (optional UUID) — filter to this model version.
//     When absent, returns detections from the latest model_version for the
//     caller's org (most recent deployed_at, across all domains).
//     domain (optional) — filter by detection_domain.
//     since / until (optional, RFC3339) — time range.
//     limit (optional int, default 100, max 1000).
//
// "Latest model" semantics: we pick the single most-recent model_version by
// deployed_at across all domains.  This is a deliberate simplification over
// per-domain "latest" because operators think of "the current model" as a
// single entity.  If the org has separate domain models at different versions,
// an operator can use ?model_version_id=<uuid> to drill into a specific one.
// This choice is documented in the PR description.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/reanalysis"
)

// ─────────────────────────────────────────────────────────────────────────────
// Detection listing with model_version_id filter
// ─────────────────────────────────────────────────────────────────────────────

// HandleListDetections handles GET /api/v1/detections.
// Auth: RequireAuth (all authenticated roles — tenant scoped by JWT claims).
func HandleListDetections(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		orgID := claims.OrganizationID
		if orgID == "" && !globalViewRoles[claims.Role] {
			http.Error(w, "no organization_id in token", http.StatusForbidden)
			return
		}
		// SOC roles may pass ?org= to scope to a specific org.
		if globalViewRoles[claims.Role] {
			if q := r.URL.Query().Get("org"); q != "" {
				orgID = q
			}
		}
		if orgID == "" {
			http.Error(w, "organization_id required", http.StatusBadRequest)
			return
		}

		// Parse optional model_version_id.
		var mvIDFilter *uuid.UUID
		if raw := r.URL.Query().Get("model_version_id"); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				http.Error(w, "invalid model_version_id", http.StatusBadRequest)
				return
			}
			mvIDFilter = &parsed
		}

		// If no model_version_id given, resolve the latest for this org.
		if mvIDFilter == nil {
			mvs, err := db.ListModelVersionsByOrg(r.Context(), orgID)
			if err != nil {
				http.Error(w, "failed to list model versions", http.StatusInternalServerError)
				return
			}
			if len(mvs) > 0 {
				// ListModelVersionsByOrg returns newest-first.
				mvIDFilter = &mvs[0].ID
			}
		}

		// Parse optional filters.
		f := database.DetectionCurrentFilter{OrganizationID: orgID}
		if d := r.URL.Query().Get("domain"); d != "" {
			f.DetectionDomain = d
		}
		if raw := r.URL.Query().Get("since"); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(w, "invalid since: must be RFC3339", http.StatusBadRequest)
				return
			}
			f.Since = t
		}
		if raw := r.URL.Query().Get("until"); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(w, "invalid until: must be RFC3339", http.StatusBadRequest)
				return
			}
			f.Until = t
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			f.Limit = n
		}

		detections, err := db.ListDetectionsCurrent(r.Context(), f)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		if detections == nil {
			detections = []database.Detection{}
		}

		// Filter by model_version_id in-memory (DetectionCurrentFilter doesn't
		// expose this column yet — keeping the SQL layer minimal per the task).
		// For fleet scale, a DB-level predicate is preferable; this can be
		// promoted to SQL once the query-planner cost is measured.
		if mvIDFilter != nil {
			filtered := detections[:0]
			for _, d := range detections {
				if d.ModelVersionID == *mvIDFilter {
					filtered = append(filtered, d)
				}
			}
			detections = filtered
		}

		writeJSON(w, detections)
	}
}

// HandleListModelVersions handles GET /api/v1/model-versions.
// Returns available model_versions for the caller's org.
// Used by the frontend dropdown to populate the filter.
func HandleListModelVersions(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		orgID := claims.OrganizationID
		if orgID == "" && !globalViewRoles[claims.Role] {
			http.Error(w, "no organization_id in token", http.StatusForbidden)
			return
		}
		if globalViewRoles[claims.Role] {
			if q := r.URL.Query().Get("org"); q != "" {
				orgID = q
			}
		}
		if orgID == "" {
			http.Error(w, "organization_id required", http.StatusBadRequest)
			return
		}
		mvs, err := db.ListModelVersionsByOrg(r.Context(), orgID)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		if mvs == nil {
			mvs = []database.ModelVersion{}
		}
		writeJSON(w, mvs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Admin reanalyze endpoint (§2 — async wrapper around the CLI logic)
// ─────────────────────────────────────────────────────────────────────────────

// reanalyzeRequest is the JSON body for POST /api/admin/reanalyze.
type reanalyzeRequest struct {
	ModelVersionID string `json:"model_version_id"`
	From           string `json:"from"`
	To             string `json:"to"`
	OrganizationID string `json:"organization_id,omitempty"`
	DryRun         bool   `json:"dry_run,omitempty"`
}

// HandleAdminReanalyze handles POST /api/admin/reanalyze.
// Validates the request, inserts an analysis_runs row, then kicks off the
// re-analysis in a goroutine.  Returns {run_id, status:"running"} immediately.
func HandleAdminReanalyze(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(r) {
			http.Error(w, "forbidden: admin only", http.StatusForbidden)
			return
		}

		var req reanalyzeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		mvID, err := uuid.Parse(req.ModelVersionID)
		if err != nil {
			http.Error(w, "invalid model_version_id", http.StatusBadRequest)
			return
		}
		fromTime, err := time.Parse(time.RFC3339, req.From)
		if err != nil {
			http.Error(w, "invalid from: must be RFC3339", http.StatusBadRequest)
			return
		}
		toTime, err := time.Parse(time.RFC3339, req.To)
		if err != nil {
			http.Error(w, "invalid to: must be RFC3339", http.StatusBadRequest)
			return
		}
		if toTime.Before(fromTime) {
			http.Error(w, "to must not be before from", http.StatusBadRequest)
			return
		}

		// Validate model_version exists.
		mv, err := db.GetModelVersion(r.Context(), mvID)
		if err != nil || mv == nil {
			http.Error(w, "model_version not found", http.StatusNotFound)
			return
		}

		// Validate org scope.
		orgID := req.OrganizationID
		if orgID != "" && mv.OrganizationID != orgID {
			http.Error(w, "organization_id does not match model_version scope", http.StatusBadRequest)
			return
		}
		if orgID == "" {
			orgID = mv.OrganizationID
		}

		paramsJSON, _ := json.Marshal(map[string]interface{}{
			"from":            fromTime.UTC().Format(time.RFC3339),
			"to":              toTime.UTC().Format(time.RFC3339),
			"organization_id": orgID,
			"dry_run":         req.DryRun,
		})

		ar, err := db.InsertAnalysisRun(r.Context(), database.AnalysisRunInsert{
			OrganizationID: orgID,
			ModelVersionID: mv.ID,
			RunType:        "reanalysis",
			StartedAt:      time.Now().UTC(),
			Params:         paramsJSON,
		})
		if err != nil {
			http.Error(w, "failed to create analysis_run: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Launch the worker goroutine.  It runs detached — the handler has already
		// responded; the goroutine uses a background context so the HTTP request
		// cancellation doesn't abort the run mid-flight.
		go func() {
			ctx := context.Background()
			_ = runReanalysis(ctx, db, mv, ar.ID, orgID, fromTime, toTime, req.DryRun)
		}()

		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]interface{}{
			"run_id": ar.ID,
			"status": "running",
		})
	}
}

// HandleGetReanalyzeRun handles GET /api/admin/reanalyze/{run_id}.
// Returns the analysis_runs row for the given run_id (as a status poll).
func HandleGetReanalyzeRun(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireAdmin(r) {
			http.Error(w, "forbidden: admin only", http.StatusForbidden)
			return
		}
		runIDStr := chi.URLParam(r, "run_id")
		runID, err := uuid.Parse(runIDStr)
		if err != nil {
			http.Error(w, "invalid run_id", http.StatusBadRequest)
			return
		}

		var ar database.AnalysisRun
		err = db.Pool.QueryRow(r.Context(), `
			SELECT id, organization_id, model_version_id, run_type,
			       started_at, ended_at, params, created_at
			FROM analysis_runs WHERE id = $1`, runID,
		).Scan(
			&ar.ID, &ar.OrganizationID, &ar.ModelVersionID, &ar.RunType,
			&ar.StartedAt, &ar.EndedAt, &ar.Params, &ar.CreatedAt,
		)
		if err != nil {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}

		status := "running"
		if ar.EndedAt != nil {
			status = "done"
		}
		writeJSON(w, map[string]interface{}{
			"run":    ar,
			"status": status,
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runReanalysis — shared logic used by both CLI and async API handler
// ─────────────────────────────────────────────────────────────────────────────

// runReanalysis is the core loop: iterates over live detections in range,
// applies the model rule set, and inserts supersede rows.
// It is called from cmd/reanalyze/main.go (directly) and from the async
// goroutine spawned by HandleAdminReanalyze.
//
// Returns the number of supersede rows emitted (changed + dropped combined).
func runReanalysis(
	ctx context.Context,
	db *database.DB,
	mv *database.ModelVersion,
	runID uuid.UUID,
	orgID string,
	fromTime, toTime time.Time,
	dryRun bool,
) int {
	rs, err := reanalysis.ParseRuleSet(mv.Params)
	if err != nil {
		return 0
	}

	var (
		cursor      *uuid.UUID
		cursorDetAt *time.Time
		emitted     int
	)

	for {
		batch, err := db.ListDetectionsForReanalysis(ctx, database.ReanalysisFilter{
			OrganizationID:  orgID,
			From:            fromTime,
			Until:           toTime,
			BatchSize:       500,
			AfterID:         cursor,
			AfterDetectedAt: cursorDetAt,
		})
		if err != nil || len(batch) == 0 {
			break
		}

		for i := range batch {
			row := &batch[i]
			outcome := reanalysis.ApplyRuleSet(row.DetectionClass, row.Confidence, row.BoundingBox, rs)
			if outcome.Kind == reanalysis.OutcomeUnchanged {
				continue
			}

			newClass := "filtered_out"
			if outcome.Kind == reanalysis.OutcomeChanged {
				newClass = outcome.Class
			}

			if dryRun {
				emitted++
				continue
			}

			oldID := row.ID
			_, insErr := db.InsertDetection(ctx, database.DetectionInsert{
				OrganizationID:  row.OrganizationID,
				SiteID:          row.SiteID,
				CameraID:        row.CameraID,
				DetectedAt:      row.DetectedAt,
				DetectionClass:  newClass,
				DetectionDomain: row.DetectionDomain,
				Confidence:      row.Confidence,
				BoundingBox:     row.BoundingBox,
				ZoneID:          row.ZoneID,
				VCARuleID:       row.VCARuleID,
				ModelVersionID:  mv.ID,
				AnalysisRunID:   runID,
				SegmentID:       row.SegmentID,
				FrameOffsetMs:   row.FrameOffsetMs,
				Source:          "reanalysis",
				Supersedes:      &oldID,
				Details:         row.Details,
			})
			if insErr == nil {
				emitted++
			}
		}

		last := batch[len(batch)-1]
		cursor = &last.ID
		cursorDetAt = &last.DetectedAt
	}

	if !dryRun {
		_ = db.UpdateAnalysisRunEnded(ctx, runID, time.Now().UTC())
	}
	return emitted
}
