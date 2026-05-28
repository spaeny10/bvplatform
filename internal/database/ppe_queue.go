package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PendingReviewQueueRow is a single PPE violation finding awaiting human review.
// DB table: pending_review_queue (migration 0022, altered by 0025).
//
// NOTE on ID types:
//   - id, camera_id, reviewed_by are UUID in the DB (gen_random_uuid() defaults).
//   - organization_id, site_id are TEXT in the DB (matching the organizations and
//     sites tables, which pre-date the uuid-primary-key migration).
//
// VLM columns (migration 0025): the C-01 stub columns were renamed/replaced.
//   - vlm_validated BOOLEAN → vlm_verdict TEXT
//   - vlm_notes → vlm_reasoning
//   - vlm_validated_at → vlm_checked_at
//   - vlm_confidence REAL kept as-is
//   - vlm_model TEXT (new)
//   - vlm_attempts INT (new)
type PendingReviewQueueRow struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID string    `json:"organization_id"`
	CameraID       uuid.UUID `json:"camera_id"`
	SiteID         *string   `json:"site_id,omitempty"`

	// Frame evidence
	FramePath         string    `json:"frame_path"`
	FrameToken        string    `json:"frame_token,omitempty"`
	FrameTokenExpires time.Time `json:"frame_token_expires,omitempty"`

	// YOLO output
	DetectionClass string          `json:"detection_class"`
	MissingLabel   string          `json:"missing_label"`
	Confidence     float64         `json:"confidence"`
	BoundingBoxes  json.RawMessage `json:"bounding_boxes"`
	YOLOModel      string          `json:"yolo_model"`

	// Review lifecycle
	Status     string     `json:"status"`
	ReviewedBy *uuid.UUID `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	Notes      *string    `json:"notes,omitempty"`

	// VLM validation (migration 0025 / P2-C-03).
	// VLMVerdict: 'pending' | 'confirmed' | 'dismissed' | 'uncertain' | 'error'
	VLMVerdict    *string    `json:"vlm_verdict,omitempty"`
	VLMReasoning  *string    `json:"vlm_reasoning,omitempty"`
	VLMCheckedAt  *time.Time `json:"vlm_checked_at,omitempty"`
	VLMModel      *string    `json:"vlm_model,omitempty"`
	VLMConfidence *float64   `json:"vlm_confidence,omitempty"`
	VLMAttempts   int        `json:"vlm_attempts"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Joined fields — populated by ListPPEQueueEntries, not stored here.
	CameraName string `json:"camera_name,omitempty"`
	SiteName   string `json:"site_name,omitempty"`
}

// PPEQueueInsert carries the data the worker provides when persisting a
// new PPE violation. OrganizationID is required and enforced at the
// query level.
type PPEQueueInsert struct {
	OrganizationID string
	CameraID       uuid.UUID
	SiteID         *string

	FramePath         string
	FrameToken        string
	FrameTokenExpires time.Time

	DetectionClass string
	MissingLabel   string
	Confidence     float64
	BoundingBoxes  json.RawMessage
	YOLOModel      string
}

// PPEQueueFilter is the query filter for ListPPEQueueEntries.
// OrganizationID is mandatory.
//
// ExcludeVLMDismissed (P2-C-03): when true the query adds
// AND vlm_verdict != 'dismissed' so VLM-auto-dismissed rows are hidden
// from the default operator view. Set false to include them (e.g. audit
// or ?include_dismissed=true handler path).
type PPEQueueFilter struct {
	OrganizationID      string
	Status              string
	CameraID            *uuid.UUID
	Limit               int
	Before              *time.Time
	ExcludeVLMDismissed bool // P2-C-03: hide vlm_verdict='dismissed' from default list
}

// InsertPPEQueueEntry persists one violation finding. Returns the assigned UUID.
func (db *DB) InsertPPEQueueEntry(ctx context.Context, ins PPEQueueInsert) (uuid.UUID, error) {
	bbJSON := ins.BoundingBoxes
	if bbJSON == nil {
		bbJSON = json.RawMessage("[]")
	}

	var id uuid.UUID
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO pending_review_queue
		    (organization_id, camera_id, site_id,
		     frame_path, frame_token, frame_token_expires,
		     detection_class, missing_label, confidence, bounding_boxes, yolo_model)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id`,
		ins.OrganizationID, ins.CameraID, ins.SiteID,
		ins.FramePath, ins.FrameToken, ins.FrameTokenExpires,
		ins.DetectionClass, ins.MissingLabel, ins.Confidence, bbJSON, ins.YOLOModel,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("InsertPPEQueueEntry: %w", err)
	}
	return id, nil
}

// ListPPEQueueEntries returns violation findings scoped to the caller's org.
// When f.ExcludeVLMDismissed is true, rows with vlm_verdict='dismissed' are
// hidden from the result (P2-C-03 default view — VLM auto-dismissed rows are
// kept for audit but not shown in the operator review queue by default).
func (db *DB) ListPPEQueueEntries(ctx context.Context, f PPEQueueFilter) ([]PendingReviewQueueRow, error) {
	status := f.Status
	if status == "" {
		status = "pending"
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args := []interface{}{f.OrganizationID, status, limit}
	cond := ""
	if f.CameraID != nil {
		args = append(args, *f.CameraID)
		cond += fmt.Sprintf(" AND q.camera_id = $%d", len(args))
	}
	if f.Before != nil {
		args = append(args, *f.Before)
		cond += fmt.Sprintf(" AND q.created_at < $%d", len(args))
	}
	if f.ExcludeVLMDismissed {
		cond += " AND (q.vlm_verdict IS NULL OR q.vlm_verdict != 'dismissed')"
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT
		    q.id, q.organization_id, q.camera_id, q.site_id,
		    q.frame_path, q.frame_token, q.frame_token_expires,
		    q.detection_class, q.missing_label, q.confidence,
		    q.bounding_boxes, q.yolo_model,
		    q.status, q.reviewed_by, q.reviewed_at, q.notes,
		    q.vlm_verdict, q.vlm_reasoning, q.vlm_checked_at,
		    q.vlm_model, q.vlm_confidence, q.vlm_attempts,
		    q.created_at, q.updated_at,
		    COALESCE(c.name, '') AS camera_name,
		    COALESCE(s.name, '') AS site_name
		FROM pending_review_queue q
		LEFT JOIN cameras c ON c.id = q.camera_id
		LEFT JOIN sites   s ON s.id = q.site_id
		WHERE q.organization_id = $1
		  AND q.status = $2`+cond+`
		ORDER BY q.created_at DESC
		LIMIT $3`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("ListPPEQueueEntries: %w", err)
	}
	defer rows.Close()

	var out []PendingReviewQueueRow
	for rows.Next() {
		var r PendingReviewQueueRow
		var siteID *string
		var reviewedBy *uuid.UUID
		var reviewedAt *time.Time
		var notes *string
		var vlmVerdict *string
		var vlmReasoning *string
		var vlmCheckedAt *time.Time
		var vlmModel *string
		var vlmConf *float64
		var bbRaw []byte

		if err := rows.Scan(
			&r.ID, &r.OrganizationID, &r.CameraID, &siteID,
			&r.FramePath, &r.FrameToken, &r.FrameTokenExpires,
			&r.DetectionClass, &r.MissingLabel, &r.Confidence,
			&bbRaw, &r.YOLOModel,
			&r.Status, &reviewedBy, &reviewedAt, &notes,
			&vlmVerdict, &vlmReasoning, &vlmCheckedAt,
			&vlmModel, &vlmConf, &r.VLMAttempts,
			&r.CreatedAt, &r.UpdatedAt,
			&r.CameraName, &r.SiteName,
		); err != nil {
			return nil, fmt.Errorf("ListPPEQueueEntries scan: %w", err)
		}
		r.SiteID = siteID
		r.ReviewedBy = reviewedBy
		r.ReviewedAt = reviewedAt
		r.Notes = notes
		r.VLMVerdict = vlmVerdict
		r.VLMReasoning = vlmReasoning
		r.VLMCheckedAt = vlmCheckedAt
		r.VLMModel = vlmModel
		r.VLMConfidence = vlmConf
		r.BoundingBoxes = json.RawMessage(bbRaw)
		out = append(out, r)
	}
	return out, nil
}

// CountPPEQueuePending returns the count of pending rows for an org.
func (db *DB) CountPPEQueuePending(ctx context.Context, orgID string) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_review_queue WHERE organization_id=$1 AND status='pending'`,
		orgID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountPPEQueuePending: %w", err)
	}
	return n, nil
}

// GetPPEQueueEntry fetches one row scoped to org. Returns (nil, nil) when
// not found or wrong org — callers should 404 without distinguishing.
func (db *DB) GetPPEQueueEntry(ctx context.Context, id uuid.UUID, orgID string) (*PendingReviewQueueRow, error) {
	var r PendingReviewQueueRow
	var siteID *string
	var reviewedBy *uuid.UUID
	var reviewedAt *time.Time
	var notes *string
	var vlmVerdict *string
	var vlmReasoning *string
	var vlmCheckedAt *time.Time
	var vlmModel *string
	var vlmConf *float64
	var bbRaw []byte

	err := db.Pool.QueryRow(ctx, `
		SELECT
		    q.id, q.organization_id, q.camera_id, q.site_id,
		    q.frame_path, q.frame_token, q.frame_token_expires,
		    q.detection_class, q.missing_label, q.confidence,
		    q.bounding_boxes, q.yolo_model,
		    q.status, q.reviewed_by, q.reviewed_at, q.notes,
		    q.vlm_verdict, q.vlm_reasoning, q.vlm_checked_at,
		    q.vlm_model, q.vlm_confidence, q.vlm_attempts,
		    q.created_at, q.updated_at
		FROM pending_review_queue q
		WHERE q.id = $1 AND q.organization_id = $2`,
		id, orgID,
	).Scan(
		&r.ID, &r.OrganizationID, &r.CameraID, &siteID,
		&r.FramePath, &r.FrameToken, &r.FrameTokenExpires,
		&r.DetectionClass, &r.MissingLabel, &r.Confidence,
		&bbRaw, &r.YOLOModel,
		&r.Status, &reviewedBy, &reviewedAt, &notes,
		&vlmVerdict, &vlmReasoning, &vlmCheckedAt,
		&vlmModel, &vlmConf, &r.VLMAttempts,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("GetPPEQueueEntry: %w", err)
	}
	r.SiteID = siteID
	r.ReviewedBy = reviewedBy
	r.ReviewedAt = reviewedAt
	r.Notes = notes
	r.VLMVerdict = vlmVerdict
	r.VLMReasoning = vlmReasoning
	r.VLMCheckedAt = vlmCheckedAt
	r.VLMModel = vlmModel
	r.VLMConfidence = vlmConf
	r.BoundingBoxes = json.RawMessage(bbRaw)
	return &r, nil
}

// UpdatePPEQueueStatus sets the review outcome on a row scoped to org.
// reviewedByID is the UUID of the reviewing user.
func (db *DB) UpdatePPEQueueStatus(ctx context.Context, id uuid.UUID, orgID string, reviewedByID uuid.UUID, status string, notes *string) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE pending_review_queue
		SET status=$1, reviewed_by=$2, reviewed_at=NOW(), notes=$3, updated_at=NOW()
		WHERE id=$4 AND organization_id=$5`,
		status, reviewedByID, notes, id, orgID,
	)
	if err != nil {
		return fmt.Errorf("UpdatePPEQueueStatus: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("UpdatePPEQueueStatus: row not found or wrong org")
	}
	return nil
}

// SweepPPEFrameRows deletes reviewed/dismissed rows older than retentionDays
// and returns their frame_path values so the caller can remove the files.
// Pending rows are kept to preserve queue UX regardless of age.
func (db *DB) SweepPPEFrameRows(ctx context.Context, retentionDays int) ([]string, error) {
	rows, err := db.Pool.Query(ctx, `
		DELETE FROM pending_review_queue
		WHERE created_at < NOW() - make_interval(days => $1)
		  AND status != 'pending'
		RETURNING frame_path`,
		retentionDays,
	)
	if err != nil {
		return nil, fmt.Errorf("SweepPPEFrameRows: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("SweepPPEFrameRows scan: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// PPECamera is the minimal camera record the PPE worker needs.
//
// OrganizationID and SiteID are strings matching the organizations/sites
// tables (TEXT primary keys). CameraID is UUID (cameras table uses UUID PK).
type PPECamera struct {
	CameraID       uuid.UUID
	OrganizationID string
	SiteID         string
	CameraName     string
	OnvifAddress   string
	ProfileToken   string
	Manufacturer   string
}

// ListCamerasForPPE returns cameras assigned to PPE-enabled sites that
// are actively recording and not offline. Does not return credentials —
// the worker fetches them via the normal camera-load path.
func (db *DB) ListCamerasForPPE(ctx context.Context) ([]PPECamera, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT c.id, s.organization_id, s.id, c.name, c.onvif_address,
		       COALESCE(c.profile_token, ''), COALESCE(c.manufacturer, '')
		FROM cameras c
		JOIN sites s ON s.id = c.site_id
		WHERE s.ppe_enabled = TRUE
		  AND c.recording = TRUE
		  AND c.status != 'offline'`)
	if err != nil {
		return nil, fmt.Errorf("ListCamerasForPPE: %w", err)
	}
	defer rows.Close()

	var out []PPECamera
	for rows.Next() {
		var cam PPECamera
		if err := rows.Scan(
			&cam.CameraID, &cam.OrganizationID, &cam.SiteID,
			&cam.CameraName, &cam.OnvifAddress, &cam.ProfileToken, &cam.Manufacturer,
		); err != nil {
			return nil, fmt.Errorf("ListCamerasForPPE scan: %w", err)
		}
		out = append(out, cam)
	}
	return out, nil
}
