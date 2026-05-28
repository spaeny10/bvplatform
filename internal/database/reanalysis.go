package database

// reanalysis.go — P4-SCHEMA-06 database helpers for the re-analysis engine.
//
// These functions are used exclusively by cmd/reanalyze and the optional admin
// API endpoint.  They do NOT appear in the dualwrite or live-ingest paths.
//
// RLS note: the re-analysis worker connects as 'onvif' which carries the
// service_bypass policy → full read/write access without per-tenant
// SET LOCAL calls.  This is intentional and documented in §P4-SCHEMA-07 and
// the re-analysis PR description.  If future per-org scoping is needed, use
// AcquireWithTenant before calling these functions.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetModelVersion — fetch one model_versions row
// ─────────────────────────────────────────────────────────────────────────────

// GetModelVersion retrieves a model_versions row by ID.
// Returns (nil, nil) when the row is not found.
func (db *DB) GetModelVersion(ctx context.Context, id uuid.UUID) (*ModelVersion, error) {
	mv := &ModelVersion{}
	err := db.Pool.QueryRow(ctx, `
		SELECT id, organization_id, model_name, version_tag, weights_hash,
		       model_domain, deployed_at, retired_at, params, created_at
		FROM model_versions
		WHERE id = $1`, id,
	).Scan(
		&mv.ID, &mv.OrganizationID, &mv.ModelName, &mv.VersionTag, &mv.WeightsHash,
		&mv.ModelDomain, &mv.DeployedAt, &mv.RetiredAt, &mv.Params, &mv.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetModelVersion: %w", err)
	}
	return mv, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateAnalysisRunEnded — stamp ended_at when re-analysis completes
// ─────────────────────────────────────────────────────────────────────────────

// UpdateAnalysisRunEnded sets the ended_at timestamp on an analysis_runs row.
// Called by cmd/reanalyze after all detection inserts are committed.
func (db *DB) UpdateAnalysisRunEnded(ctx context.Context, runID uuid.UUID, endedAt time.Time) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE analysis_runs SET ended_at = $1 WHERE id = $2`,
		endedAt, runID,
	)
	if err != nil {
		return fmt.Errorf("UpdateAnalysisRunEnded: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// ListDetectionsForReanalysis
// ─────────────────────────────────────────────────────────────────────────────

// ReanalysisFilter scopes the detections enumerated by ListDetectionsForReanalysis.
// All fields except From/Until are optional; zero values mean "no constraint."
type ReanalysisFilter struct {
	// OrganizationID limits results to one org.  Empty = all orgs.
	OrganizationID string

	// From and Until are inclusive bounds on detected_at.  Both are required
	// (the caller should default to a sensible range rather than a table scan).
	From  time.Time
	Until time.Time

	// BatchSize is the page size for cursor-based iteration.  0 defaults to 500.
	BatchSize int

	// AfterID is the UUID of the last row returned by a previous call, used as
	// a pagination cursor (combined with AfterDetectedAt for hypertable ordering).
	AfterID        *uuid.UUID
	AfterDetectedAt *time.Time
}

// ReanalysisRow is a trimmed view of a detections row sufficient for re-analysis.
// We only fetch the fields the rule engine needs plus the FK fields needed to
// reproduce an equivalent new row.
type ReanalysisRow struct {
	ID              uuid.UUID
	OrganizationID  string
	SiteID          *string
	CameraID        uuid.UUID
	DetectedAt      time.Time
	DetectionClass  string
	DetectionDomain string
	Confidence      float32
	BoundingBox     json.RawMessage
	ZoneID          *uuid.UUID
	VCARuleID       *uuid.UUID
	SegmentID       *int64
	FrameOffsetMs   *int64
	Details         json.RawMessage
}

// ListDetectionsForReanalysis returns a page of "live" (supersedes IS NULL)
// detection rows matching the filter, ordered (detected_at ASC, id ASC) for
// stable cursor-based iteration.
//
// The returned slice may be shorter than BatchSize at end-of-range (including
// the empty-slice case when no rows match).  The caller advances the cursor
// by taking the last row's (DetectedAt, ID) as (AfterDetectedAt, AfterID).
func (db *DB) ListDetectionsForReanalysis(ctx context.Context, f ReanalysisFilter) ([]ReanalysisRow, error) {
	batchSize := f.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	args := []interface{}{}
	idx := 1

	where := "WHERE supersedes IS NULL"

	// Date range (required).
	where += fmt.Sprintf(" AND detected_at >= $%d", idx)
	args = append(args, f.From)
	idx++

	where += fmt.Sprintf(" AND detected_at <= $%d", idx)
	args = append(args, f.Until)
	idx++

	// Optional org filter.
	if f.OrganizationID != "" {
		where += fmt.Sprintf(" AND organization_id = $%d", idx)
		args = append(args, f.OrganizationID)
		idx++
	}

	// Cursor (pagination).
	if f.AfterDetectedAt != nil && f.AfterID != nil {
		where += fmt.Sprintf(" AND (detected_at, id) > ($%d, $%d)", idx, idx+1)
		args = append(args, *f.AfterDetectedAt, *f.AfterID)
		idx += 2
	}

	args = append(args, batchSize)

	q := fmt.Sprintf(`
		SELECT id, organization_id, site_id, camera_id, detected_at,
		       detection_class, detection_domain, confidence, bounding_box,
		       zone_id, vca_rule_id,
		       segment_id, frame_offset_ms, details
		FROM detections
		%s
		ORDER BY detected_at ASC, id ASC
		LIMIT $%d`, where, idx)

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListDetectionsForReanalysis: %w", err)
	}
	defer rows.Close()

	var out []ReanalysisRow
	for rows.Next() {
		var r ReanalysisRow
		if err := rows.Scan(
			&r.ID, &r.OrganizationID, &r.SiteID, &r.CameraID, &r.DetectedAt,
			&r.DetectionClass, &r.DetectionDomain, &r.Confidence, &r.BoundingBox,
			&r.ZoneID, &r.VCARuleID,
			&r.SegmentID, &r.FrameOffsetMs, &r.Details,
		); err != nil {
			return nil, fmt.Errorf("ListDetectionsForReanalysis scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// ListOrgsForModelVersion
// ─────────────────────────────────────────────────────────────────────────────

// ListOrgsForModelVersion returns the organization IDs that have detections in
// the given date range, scoped to the model_version's org (since model_versions
// are org-scoped, the re-analysis run affects exactly the model_version's org
// unless the caller passes --organization-id to further narrow it).
//
// If modelVersionOrgID is non-empty, returns [modelVersionOrgID].
// Otherwise (should not happen with a well-formed model_versions row) returns [].
//
// This is intentionally simple: the org-scoped model_version design (DECISION-A)
// means one model_version row corresponds to one org.  The function exists as a
// clean API boundary so cmd/reanalyze doesn't need raw SQL.
func ListOrgsForModelVersion(mv *ModelVersion) []string {
	if mv.OrganizationID == "" {
		return nil
	}
	return []string{mv.OrganizationID}
}

// ─────────────────────────────────────────────────────────────────────────────
// FetchDetectionReviewVerdicts
// ─────────────────────────────────────────────────────────────────────────────

// FetchDetectionReviewVerdicts returns, for each detection ID in the given set,
// the most recent verdict.  Detection IDs with no reviews are omitted from the
// result map.
//
// Used by cmd/reanalyze to build the false-positive-rate section of the report.
// The most recent verdict per detection is the "effective" ground truth; earlier
// verdicts are preserved in the table but ignored for rate calculation.
//
// detectionIDs may be empty; the function returns an empty map.
func (db *DB) FetchDetectionReviewVerdicts(ctx context.Context, orgID string, detectionIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(detectionIDs) == 0 {
		return map[uuid.UUID]string{}, nil
	}

	// Build $1,$2,... placeholder list for the IN clause.
	placeholders := make([]interface{}, 0, len(detectionIDs)+1)
	placeholders = append(placeholders, orgID)
	idArgs := ""
	for i, id := range detectionIDs {
		placeholders = append(placeholders, id)
		if i > 0 {
			idArgs += ","
		}
		idArgs += fmt.Sprintf("$%d", i+2)
	}

	// DISTINCT ON picks the most recent review per detection.
	q := fmt.Sprintf(`
		SELECT DISTINCT ON (detection_id) detection_id, verdict
		FROM detection_reviews
		WHERE organization_id = $1
		  AND detection_id IN (%s)
		ORDER BY detection_id, reviewed_at DESC`, idArgs)

	rows, err := db.Pool.Query(ctx, q, placeholders...)
	if err != nil {
		return nil, fmt.Errorf("FetchDetectionReviewVerdicts: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]string)
	for rows.Next() {
		var id uuid.UUID
		var verdict string
		if err := rows.Scan(&id, &verdict); err != nil {
			return nil, fmt.Errorf("FetchDetectionReviewVerdicts scan: %w", err)
		}
		out[id] = verdict
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────────
// ListModelVersionsByOrg — for the detection-list API filter
// ─────────────────────────────────────────────────────────────────────────────

// ListModelVersionsByOrg returns model_versions for an org, ordered by
// deployed_at DESC (newest first).  Used by the detection-listing API to
// populate the "latest model" filter and to validate query params.
func (db *DB) ListModelVersionsByOrg(ctx context.Context, orgID string) ([]ModelVersion, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, organization_id, model_name, version_tag, weights_hash,
		       model_domain, deployed_at, retired_at, params, created_at
		FROM model_versions
		WHERE organization_id = $1
		ORDER BY deployed_at DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("ListModelVersionsByOrg: %w", err)
	}
	defer rows.Close()

	var out []ModelVersion
	for rows.Next() {
		var mv ModelVersion
		if err := rows.Scan(
			&mv.ID, &mv.OrganizationID, &mv.ModelName, &mv.VersionTag, &mv.WeightsHash,
			&mv.ModelDomain, &mv.DeployedAt, &mv.RetiredAt, &mv.Params, &mv.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListModelVersionsByOrg scan: %w", err)
		}
		out = append(out, mv)
	}
	return out, rows.Err()
}
