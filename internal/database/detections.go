package database

// detections.go — P4-SCHEMA-01 foundation.
//
// Typed structs and minimal tenant-scoped CRUD for:
//   model_versions, analysis_runs, detections, detection_reviews
//
// Append-only contract: there are NO Update or Delete methods on detections.
// The ironsight_prevent_mutation() DB trigger enforces this at the schema
// level too. Corrections are expressed as new rows with supersedes set.
//
// Tenant scope: every function that reads or writes requires organization_id
// and applies it as the leading WHERE/VALUES predicate. No cross-tenant data
// can be returned by any function in this file.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ─────────────────────────────────────────────
// Structs
// ─────────────────────────────────────────────

// ModelVersion records which inference model binary produced a detection.
// Org-scoped: deployed_at/retired_at track when each org activated/deactivated
// the version independently (DECISION-A).
type ModelVersion struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID string          `json:"organization_id"`
	ModelName      string          `json:"model_name"`
	VersionTag     string          `json:"version_tag"`
	WeightsHash    string          `json:"weights_hash"`
	ModelDomain    string          `json:"model_domain"` // ppe|security|person_tracking|vlm_validation
	DeployedAt     time.Time       `json:"deployed_at"`
	RetiredAt      *time.Time      `json:"retired_at,omitempty"`
	Params         json.RawMessage `json:"params"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ModelVersionInsert is the input for InsertModelVersion.
type ModelVersionInsert struct {
	OrganizationID string
	ModelName      string
	VersionTag     string
	WeightsHash    string          // "" is valid (legacy import)
	ModelDomain    string          // ppe|security|person_tracking|vlm_validation
	DeployedAt     time.Time       // zero → NOW()
	Params         json.RawMessage // nil → '{}'
}

// AnalysisRun is a bounded execution context. Every detection is produced
// within exactly one run. Enables historical re-analysis comparisons.
type AnalysisRun struct {
	ID              uuid.UUID       `json:"id"`
	OrganizationID  string          `json:"organization_id"`
	ModelVersionID  uuid.UUID       `json:"model_version_id"`
	RunType         string          `json:"run_type"` // live_ingest|reanalysis
	StartedAt       time.Time       `json:"started_at"`
	EndedAt         *time.Time      `json:"ended_at,omitempty"`
	Params          json.RawMessage `json:"params"`
	CreatedAt       time.Time       `json:"created_at"`
}

// AnalysisRunInsert is the input for InsertAnalysisRun.
type AnalysisRunInsert struct {
	OrganizationID string
	ModelVersionID uuid.UUID
	RunType        string          // live_ingest|reanalysis
	StartedAt      time.Time       // zero → NOW()
	Params         json.RawMessage // nil → '{}'
}

// Detection is one discrete detection event. The table is append-only;
// corrections are expressed as new rows with Supersedes pointing at the
// row being replaced.
type Detection struct {
	ID              uuid.UUID       `json:"id"`
	OrganizationID  string          `json:"organization_id"`
	SiteID          *string         `json:"site_id,omitempty"`
	CameraID        uuid.UUID       `json:"camera_id"`
	DetectedAt      time.Time       `json:"detected_at"`
	DetectionClass  string          `json:"detection_class"`
	DetectionDomain string          `json:"detection_domain"` // ppe|security|person_tracking|vlm_validation
	Confidence      float32         `json:"confidence"`
	BoundingBox     json.RawMessage `json:"bounding_box"`
	ZoneID          *uuid.UUID      `json:"zone_id,omitempty"`
	VCARuleID       *uuid.UUID      `json:"vca_rule_id,omitempty"`
	ModelVersionID  uuid.UUID       `json:"model_version_id"`
	AnalysisRunID   uuid.UUID       `json:"analysis_run_id"`
	SegmentID       *int64          `json:"segment_id,omitempty"` // no DB FK (segments is a hypertable with no PK constraint)
	FrameOffsetMs   *int64          `json:"frame_offset_ms,omitempty"`
	Source          string          `json:"source"` // live|reanalysis
	Supersedes      *uuid.UUID      `json:"supersedes,omitempty"`
	Details         json.RawMessage `json:"details"`
	CreatedAt       time.Time       `json:"created_at"`
}

// DetectionInsert is the input for InsertDetection.
type DetectionInsert struct {
	OrganizationID  string
	SiteID          *string
	CameraID        uuid.UUID
	DetectedAt      time.Time
	DetectionClass  string
	DetectionDomain string          // ppe|security|person_tracking|vlm_validation
	Confidence      float32
	BoundingBox     json.RawMessage // nil → '{}'
	ZoneID          *uuid.UUID      // mutually exclusive with VCARuleID (app layer)
	VCARuleID       *uuid.UUID
	ModelVersionID  uuid.UUID
	AnalysisRunID   uuid.UUID
	SegmentID       *int64
	FrameOffsetMs   *int64
	Source          string     // live|reanalysis
	Supersedes      *uuid.UUID // nil = new finding; non-nil = replaces an earlier row
	Details         json.RawMessage // nil → '{}'
}

// DetectionReview is a human operator annotation on a detection.
// Corrections live here, not as new detection rows (DECISION-D2).
// detection_id is stored as a plain UUID (no DB FK constraint — detections
// is a hypertable; see migration 0030 header for the FK constraint rationale).
// Application code verifies the detection exists before inserting a review.
type DetectionReview struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID string     `json:"organization_id"`
	DetectionID    uuid.UUID  `json:"detection_id"`
	ReviewerUserID uuid.UUID  `json:"reviewer_user_id"`
	Verdict        string     `json:"verdict"` // confirmed|false_positive|uncertain
	Notes          *string    `json:"notes,omitempty"`
	ReviewedAt     time.Time  `json:"reviewed_at"`
}

// DetectionReviewInsert is the input for InsertDetectionReview.
type DetectionReviewInsert struct {
	OrganizationID string
	DetectionID    uuid.UUID
	ReviewerUserID uuid.UUID
	Verdict        string  // confirmed|false_positive|uncertain
	Notes          *string // nil = no notes
}

// ─────────────────────────────────────────────
// InsertModelVersion
// ─────────────────────────────────────────────

// InsertModelVersion records a new model version registration for an org.
// Returns the populated ModelVersion (with server-generated ID + timestamps).
// OrganizationID and ModelName and VersionTag and ModelDomain are required.
func (db *DB) InsertModelVersion(ctx context.Context, ins ModelVersionInsert) (*ModelVersion, error) {
	if ins.OrganizationID == "" {
		return nil, fmt.Errorf("InsertModelVersion: organization_id is required")
	}
	if ins.ModelName == "" {
		return nil, fmt.Errorf("InsertModelVersion: model_name is required")
	}
	if ins.VersionTag == "" {
		return nil, fmt.Errorf("InsertModelVersion: version_tag is required")
	}
	if ins.ModelDomain == "" {
		return nil, fmt.Errorf("InsertModelVersion: model_domain is required")
	}
	params := ins.Params
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	deployedAt := ins.DeployedAt
	if deployedAt.IsZero() {
		deployedAt = time.Now().UTC()
	}

	mv := &ModelVersion{}
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO model_versions
		    (organization_id, model_name, version_tag, weights_hash,
		     model_domain, deployed_at, params)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, organization_id, model_name, version_tag, weights_hash,
		          model_domain, deployed_at, retired_at, params, created_at`,
		ins.OrganizationID,
		ins.ModelName,
		ins.VersionTag,
		ins.WeightsHash,
		ins.ModelDomain,
		deployedAt,
		[]byte(params),
	).Scan(
		&mv.ID,
		&mv.OrganizationID,
		&mv.ModelName,
		&mv.VersionTag,
		&mv.WeightsHash,
		&mv.ModelDomain,
		&mv.DeployedAt,
		&mv.RetiredAt,
		&mv.Params,
		&mv.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("InsertModelVersion: %w", err)
	}
	return mv, nil
}

// ─────────────────────────────────────────────
// InsertAnalysisRun
// ─────────────────────────────────────────────

// InsertAnalysisRun records a new analysis run (live_ingest or reanalysis).
// OrganizationID, ModelVersionID, and RunType are required.
func (db *DB) InsertAnalysisRun(ctx context.Context, ins AnalysisRunInsert) (*AnalysisRun, error) {
	if ins.OrganizationID == "" {
		return nil, fmt.Errorf("InsertAnalysisRun: organization_id is required")
	}
	if ins.ModelVersionID == uuid.Nil {
		return nil, fmt.Errorf("InsertAnalysisRun: model_version_id is required")
	}
	if ins.RunType == "" {
		return nil, fmt.Errorf("InsertAnalysisRun: run_type is required")
	}
	params := ins.Params
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	startedAt := ins.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	ar := &AnalysisRun{}
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO analysis_runs
		    (organization_id, model_version_id, run_type, started_at, params)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, organization_id, model_version_id, run_type,
		          started_at, ended_at, params, created_at`,
		ins.OrganizationID,
		ins.ModelVersionID,
		ins.RunType,
		startedAt,
		[]byte(params),
	).Scan(
		&ar.ID,
		&ar.OrganizationID,
		&ar.ModelVersionID,
		&ar.RunType,
		&ar.StartedAt,
		&ar.EndedAt,
		&ar.Params,
		&ar.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("InsertAnalysisRun: %w", err)
	}
	return ar, nil
}

// ─────────────────────────────────────────────
// InsertDetection
// ─────────────────────────────────────────────

// InsertDetection writes one detection event. The table is append-only;
// this is the only mutation function. No UPDATE or DELETE methods exist.
// Returns the full Detection with server-generated ID + created_at.
func (db *DB) InsertDetection(ctx context.Context, ins DetectionInsert) (*Detection, error) {
	if ins.OrganizationID == "" {
		return nil, fmt.Errorf("InsertDetection: organization_id is required")
	}
	if ins.CameraID == uuid.Nil {
		return nil, fmt.Errorf("InsertDetection: camera_id is required")
	}
	if ins.DetectedAt.IsZero() {
		return nil, fmt.Errorf("InsertDetection: detected_at is required")
	}
	if ins.DetectionClass == "" {
		return nil, fmt.Errorf("InsertDetection: detection_class is required")
	}
	if ins.DetectionDomain == "" {
		return nil, fmt.Errorf("InsertDetection: detection_domain is required")
	}
	if ins.ModelVersionID == uuid.Nil {
		return nil, fmt.Errorf("InsertDetection: model_version_id is required")
	}
	if ins.AnalysisRunID == uuid.Nil {
		return nil, fmt.Errorf("InsertDetection: analysis_run_id is required")
	}
	if ins.Source == "" {
		return nil, fmt.Errorf("InsertDetection: source is required")
	}
	bbox := ins.BoundingBox
	if len(bbox) == 0 {
		bbox = json.RawMessage(`{}`)
	}
	details := ins.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}

	d := &Detection{}
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO detections
		    (organization_id, site_id, camera_id, detected_at,
		     detection_class, detection_domain, confidence, bounding_box,
		     zone_id, vca_rule_id,
		     model_version_id, analysis_run_id,
		     segment_id, frame_offset_ms,
		     source, supersedes, details)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, organization_id, site_id, camera_id, detected_at,
		          detection_class, detection_domain, confidence, bounding_box,
		          zone_id, vca_rule_id,
		          model_version_id, analysis_run_id,
		          segment_id, frame_offset_ms,
		          source, supersedes, details, created_at`,
		ins.OrganizationID,
		ins.SiteID,
		ins.CameraID,
		ins.DetectedAt,
		ins.DetectionClass,
		ins.DetectionDomain,
		ins.Confidence,
		[]byte(bbox),
		ins.ZoneID,
		ins.VCARuleID,
		ins.ModelVersionID,
		ins.AnalysisRunID,
		ins.SegmentID,
		ins.FrameOffsetMs,
		ins.Source,
		ins.Supersedes,
		[]byte(details),
	).Scan(
		&d.ID,
		&d.OrganizationID,
		&d.SiteID,
		&d.CameraID,
		&d.DetectedAt,
		&d.DetectionClass,
		&d.DetectionDomain,
		&d.Confidence,
		&d.BoundingBox,
		&d.ZoneID,
		&d.VCARuleID,
		&d.ModelVersionID,
		&d.AnalysisRunID,
		&d.SegmentID,
		&d.FrameOffsetMs,
		&d.Source,
		&d.Supersedes,
		&d.Details,
		&d.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("InsertDetection: %w", err)
	}
	return d, nil
}

// ─────────────────────────────────────────────
// GetDetection
// ─────────────────────────────────────────────

// GetDetection retrieves a single detection row by ID, scoped to the given
// organization. Returns (nil, nil) when the row is not found or belongs to
// a different org (tenant isolation: callers treat both as 404).
func (db *DB) GetDetection(ctx context.Context, id uuid.UUID, orgID string) (*Detection, error) {
	if orgID == "" {
		return nil, fmt.Errorf("GetDetection: organization_id is required")
	}
	d := &Detection{}
	err := db.Pool.QueryRow(ctx, `
		SELECT id, organization_id, site_id, camera_id, detected_at,
		       detection_class, detection_domain, confidence, bounding_box,
		       zone_id, vca_rule_id,
		       model_version_id, analysis_run_id,
		       segment_id, frame_offset_ms,
		       source, supersedes, details, created_at
		FROM detections
		WHERE id = $1 AND organization_id = $2`,
		id, orgID,
	).Scan(
		&d.ID,
		&d.OrganizationID,
		&d.SiteID,
		&d.CameraID,
		&d.DetectedAt,
		&d.DetectionClass,
		&d.DetectionDomain,
		&d.Confidence,
		&d.BoundingBox,
		&d.ZoneID,
		&d.VCARuleID,
		&d.ModelVersionID,
		&d.AnalysisRunID,
		&d.SegmentID,
		&d.FrameOffsetMs,
		&d.Source,
		&d.Supersedes,
		&d.Details,
		&d.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetDetection: %w", err)
	}
	return d, nil
}

// ─────────────────────────────────────────────
// ListDetectionsCurrent
// ─────────────────────────────────────────────

// DetectionCurrentFilter scopes a ListDetectionsCurrent query.
// OrganizationID is required. All other fields are optional.
type DetectionCurrentFilter struct {
	OrganizationID  string
	DetectionDomain string     // empty = all domains
	CameraID        *uuid.UUID // nil = all cameras
	Since           time.Time  // zero = no lower bound
	Until           time.Time  // zero = no upper bound
	Limit           int        // 0 → 100; max 1000
}

// ListDetectionsCurrent returns non-superseded detections from the
// detections_current view, scoped to the caller's org. Results are ordered
// detected_at DESC.
func (db *DB) ListDetectionsCurrent(ctx context.Context, f DetectionCurrentFilter) ([]Detection, error) {
	if f.OrganizationID == "" {
		return nil, fmt.Errorf("ListDetectionsCurrent: organization_id is required")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	args := []interface{}{f.OrganizationID}
	where := "WHERE d.organization_id = $1"
	idx := 2

	if f.DetectionDomain != "" {
		where += fmt.Sprintf(" AND d.detection_domain = $%d", idx)
		args = append(args, f.DetectionDomain)
		idx++
	}
	if f.CameraID != nil {
		where += fmt.Sprintf(" AND d.camera_id = $%d", idx)
		args = append(args, *f.CameraID)
		idx++
	}
	if !f.Since.IsZero() {
		where += fmt.Sprintf(" AND d.detected_at >= $%d", idx)
		args = append(args, f.Since)
		idx++
	}
	if !f.Until.IsZero() {
		where += fmt.Sprintf(" AND d.detected_at <= $%d", idx)
		args = append(args, f.Until)
		idx++
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT d.id, d.organization_id, d.site_id, d.camera_id, d.detected_at,
		       d.detection_class, d.detection_domain, d.confidence, d.bounding_box,
		       d.zone_id, d.vca_rule_id,
		       d.model_version_id, d.analysis_run_id,
		       d.segment_id, d.frame_offset_ms,
		       d.source, d.supersedes, d.details, d.created_at
		FROM detections_current d
		%s
		ORDER BY d.detected_at DESC
		LIMIT $%d`, where, idx)

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListDetectionsCurrent: %w", err)
	}
	defer rows.Close()

	var out []Detection
	for rows.Next() {
		var d Detection
		if err := rows.Scan(
			&d.ID,
			&d.OrganizationID,
			&d.SiteID,
			&d.CameraID,
			&d.DetectedAt,
			&d.DetectionClass,
			&d.DetectionDomain,
			&d.Confidence,
			&d.BoundingBox,
			&d.ZoneID,
			&d.VCARuleID,
			&d.ModelVersionID,
			&d.AnalysisRunID,
			&d.SegmentID,
			&d.FrameOffsetMs,
			&d.Source,
			&d.Supersedes,
			&d.Details,
			&d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListDetectionsCurrent scan: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────
// InsertDetectionReview
// ─────────────────────────────────────────────

// InsertDetectionReview records a human operator verdict on a detection.
// Multiple reviews are allowed per detection (audit trail).
// OrganizationID, DetectionID, ReviewerUserID, and Verdict are required.
func (db *DB) InsertDetectionReview(ctx context.Context, ins DetectionReviewInsert) (*DetectionReview, error) {
	if ins.OrganizationID == "" {
		return nil, fmt.Errorf("InsertDetectionReview: organization_id is required")
	}
	if ins.DetectionID == uuid.Nil {
		return nil, fmt.Errorf("InsertDetectionReview: detection_id is required")
	}
	if ins.ReviewerUserID == uuid.Nil {
		return nil, fmt.Errorf("InsertDetectionReview: reviewer_user_id is required")
	}
	if ins.Verdict == "" {
		return nil, fmt.Errorf("InsertDetectionReview: verdict is required")
	}

	dr := &DetectionReview{}
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO detection_reviews
		    (organization_id, detection_id, reviewer_user_id, verdict, notes)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, organization_id, detection_id, reviewer_user_id,
		          verdict, notes, reviewed_at`,
		ins.OrganizationID,
		ins.DetectionID,
		ins.ReviewerUserID,
		ins.Verdict,
		ins.Notes,
	).Scan(
		&dr.ID,
		&dr.OrganizationID,
		&dr.DetectionID,
		&dr.ReviewerUserID,
		&dr.Verdict,
		&dr.Notes,
		&dr.ReviewedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("InsertDetectionReview: %w", err)
	}
	return dr, nil
}
