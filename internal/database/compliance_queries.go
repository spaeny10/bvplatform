package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ComplianceFilter is the query filter for all compliance aggregation functions.
// OrgID is mandatory; SiteID is optional (nil = all sites for the org).
// Start and End define the time range (inclusive). TruncUnit is the
// date_trunc argument: 'hour' for today, 'day' otherwise.
type ComplianceFilter struct {
	OrgID     string
	SiteID    *string
	Start     time.Time
	End       time.Time
	TruncUnit string // 'hour' | 'day'
}

// ComplianceHeadlineMetrics is the top-level aggregate for the period.
type ComplianceHeadlineMetrics struct {
	TotalViolations int `json:"total_violations"`
	TotalReviewed   int `json:"total_reviewed"`
	PendingCount    int `json:"pending_count"`
}

// ComplianceTimeBucket is one data point in the violations-over-time series.
type ComplianceTimeBucket struct {
	Bucket time.Time `json:"bucket"`
	Count  int       `json:"count"`
}

// ComplianceCamera is one row in the top-cameras table.
type ComplianceCamera struct {
	CameraID       uuid.UUID `json:"camera_id"`
	CameraName     string    `json:"camera_name"`
	ViolationCount int       `json:"violation_count"`
	PctOfTotal     float64   `json:"pct_of_total"`
}

// ComplianceFinding is one row in the recent-findings table.
type ComplianceFinding struct {
	ID             uuid.UUID `json:"id"`
	CameraID       uuid.UUID `json:"camera_id"`
	CameraName     string    `json:"camera_name"`
	SiteID         *string   `json:"site_id,omitempty"`
	SiteName       string    `json:"site_name,omitempty"`
	DetectionClass string    `json:"detection_class"`
	MissingLabel   string    `json:"missing_label"`
	Confidence     float64   `json:"confidence"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	FramePath      string    `json:"-"` // used to mint media token; not in JSON
}

// OccupancyBucket is one pre-aggregated bucket from person_track_buckets.
type OccupancyBucket struct {
	Bucket      time.Time `json:"bucket"`
	PersonCount float64   `json:"person_count"`
}

// GetComplianceHeadline returns the violation counts for the filter period.
// Errors if org or time range is missing.
func (db *DB) GetComplianceHeadline(ctx context.Context, f ComplianceFilter) (*ComplianceHeadlineMetrics, error) {
	if f.OrgID == "" {
		return nil, fmt.Errorf("GetComplianceHeadline: org_id is required")
	}

	siteFilter := ""
	args := []interface{}{f.OrgID, f.Start, f.End}
	if f.SiteID != nil {
		args = append(args, *f.SiteID)
		siteFilter = fmt.Sprintf(" AND site_id = $%d", len(args))
	}

	var m ComplianceHeadlineMetrics
	err := db.Pool.QueryRow(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE status = 'reviewed_violation') AS total_violations,
		    COUNT(*) FILTER (WHERE status IN ('reviewed_violation','reviewed_compliant','dismissed')) AS total_reviewed,
		    COUNT(*) FILTER (WHERE status = 'pending') AS pending_count
		FROM pending_review_queue
		WHERE organization_id = $1
		  AND created_at BETWEEN $2 AND $3`+siteFilter,
		args...,
	).Scan(&m.TotalViolations, &m.TotalReviewed, &m.PendingCount)
	if err != nil {
		return nil, fmt.Errorf("GetComplianceHeadline: %w", err)
	}
	return &m, nil
}

// GetComplianceViolationsOverTime returns bucketed violation counts for charting.
// Bucket granularity is controlled by f.TruncUnit ('hour' or 'day').
func (db *DB) GetComplianceViolationsOverTime(ctx context.Context, f ComplianceFilter) ([]ComplianceTimeBucket, error) {
	if f.OrgID == "" {
		return nil, fmt.Errorf("GetComplianceViolationsOverTime: org_id is required")
	}
	trunc := f.TruncUnit
	if trunc == "" {
		trunc = "day"
	}

	siteFilter := ""
	args := []interface{}{f.OrgID, f.Start, f.End, trunc}
	if f.SiteID != nil {
		args = append(args, *f.SiteID)
		siteFilter = fmt.Sprintf(" AND site_id = $%d", len(args))
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT date_trunc($4, created_at) AS bucket, COUNT(*) AS violation_count
		FROM pending_review_queue
		WHERE organization_id = $1
		  AND status = 'reviewed_violation'
		  AND created_at BETWEEN $2 AND $3`+siteFilter+`
		GROUP BY 1
		ORDER BY 1`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("GetComplianceViolationsOverTime: %w", err)
	}
	defer rows.Close()

	var out []ComplianceTimeBucket
	for rows.Next() {
		var b ComplianceTimeBucket
		if err := rows.Scan(&b.Bucket, &b.Count); err != nil {
			return nil, fmt.Errorf("GetComplianceViolationsOverTime scan: %w", err)
		}
		out = append(out, b)
	}
	if out == nil {
		out = []ComplianceTimeBucket{}
	}
	return out, rows.Err()
}

// GetComplianceTopCameras returns the top cameras by violation count, with
// percentage of total violations. limit must be >= 1; capped at 10.
func (db *DB) GetComplianceTopCameras(ctx context.Context, f ComplianceFilter, limit int) ([]ComplianceCamera, error) {
	if f.OrgID == "" {
		return nil, fmt.Errorf("GetComplianceTopCameras: org_id is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	siteFilter := ""
	args := []interface{}{f.OrgID, f.Start, f.End, limit}
	if f.SiteID != nil {
		args = append(args, *f.SiteID)
		siteFilter = fmt.Sprintf(" AND q.site_id = $%d", len(args))
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT q.camera_id, COALESCE(c.name, q.camera_id::text) AS camera_name,
		       COUNT(*) AS violation_count
		FROM pending_review_queue q
		LEFT JOIN cameras c ON c.id = q.camera_id
		WHERE q.organization_id = $1
		  AND q.status = 'reviewed_violation'
		  AND q.created_at BETWEEN $2 AND $3`+siteFilter+`
		GROUP BY q.camera_id, c.name
		ORDER BY violation_count DESC
		LIMIT $4`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("GetComplianceTopCameras: %w", err)
	}
	defer rows.Close()

	var raw []ComplianceCamera
	var total int
	for rows.Next() {
		var cam ComplianceCamera
		if err := rows.Scan(&cam.CameraID, &cam.CameraName, &cam.ViolationCount); err != nil {
			return nil, fmt.Errorf("GetComplianceTopCameras scan: %w", err)
		}
		total += cam.ViolationCount
		raw = append(raw, cam)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Compute percentage of total after collecting all rows.
	for i := range raw {
		if total > 0 {
			raw[i].PctOfTotal = float64(raw[i].ViolationCount) / float64(total) * 100
		}
	}
	if raw == nil {
		raw = []ComplianceCamera{}
	}
	return raw, nil
}

// GetComplianceRecentFindings returns the most recent reviewed_violation rows.
// limit must be >= 1; capped at 20.
func (db *DB) GetComplianceRecentFindings(ctx context.Context, f ComplianceFilter, limit int) ([]ComplianceFinding, error) {
	if f.OrgID == "" {
		return nil, fmt.Errorf("GetComplianceRecentFindings: org_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}

	siteFilter := ""
	args := []interface{}{f.OrgID, f.Start, f.End, limit}
	if f.SiteID != nil {
		args = append(args, *f.SiteID)
		siteFilter = fmt.Sprintf(" AND q.site_id = $%d", len(args))
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT q.id, q.camera_id, COALESCE(c.name, q.camera_id::text),
		       q.site_id, COALESCE(s.name, ''),
		       q.detection_class, q.missing_label, q.confidence,
		       q.status, q.created_at, q.frame_path
		FROM pending_review_queue q
		LEFT JOIN cameras c ON c.id = q.camera_id
		LEFT JOIN sites   s ON s.id = q.site_id
		WHERE q.organization_id = $1
		  AND q.status = 'reviewed_violation'
		  AND q.created_at BETWEEN $2 AND $3`+siteFilter+`
		ORDER BY q.created_at DESC
		LIMIT $4`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("GetComplianceRecentFindings: %w", err)
	}
	defer rows.Close()

	var out []ComplianceFinding
	for rows.Next() {
		var f ComplianceFinding
		if err := rows.Scan(
			&f.ID, &f.CameraID, &f.CameraName,
			&f.SiteID, &f.SiteName,
			&f.DetectionClass, &f.MissingLabel, &f.Confidence,
			&f.Status, &f.CreatedAt, &f.FramePath,
		); err != nil {
			return nil, fmt.Errorf("GetComplianceRecentFindings scan: %w", err)
		}
		out = append(out, f)
	}
	if out == nil {
		out = []ComplianceFinding{}
	}
	return out, rows.Err()
}

// GetComplianceOccupancy queries person_track_buckets to return bucketed
// person-presence data and the total person-hours for the period.
//
// Graceful degradation: if the person_track_buckets table does not exist
// (C-02 migration not yet applied), returns nil, nil, nil so the caller can
// set person_hours_available=false without a 500.
func (db *DB) GetComplianceOccupancy(ctx context.Context, f ComplianceFilter) ([]OccupancyBucket, *float64, error) {
	if f.OrgID == "" {
		return nil, nil, fmt.Errorf("GetComplianceOccupancy: org_id is required")
	}
	trunc := f.TruncUnit
	if trunc == "" {
		trunc = "day"
	}

	siteFilter := ""
	args := []interface{}{f.OrgID, f.Start, f.End, trunc}
	if f.SiteID != nil {
		args = append(args, *f.SiteID)
		siteFilter = fmt.Sprintf(" AND site_id = $%d", len(args))
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT date_trunc($4, bucket_start) AS bucket, SUM(person_minutes) AS person_minutes
		FROM person_track_buckets
		WHERE organization_id = $1
		  AND bucket_start BETWEEN $2 AND $3`+siteFilter+`
		GROUP BY 1
		ORDER BY 1`,
		args...,
	)
	if err != nil {
		errStr := err.Error()
		// Graceful degradation: C-02 migration not yet applied.
		if strings.Contains(errStr, "does not exist") || strings.Contains(errStr, "relation") {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("GetComplianceOccupancy: %w", err)
	}
	defer rows.Close()

	var buckets []OccupancyBucket
	var totalMinutes float64
	for rows.Next() {
		var b OccupancyBucket
		var pm float64
		if err := rows.Scan(&b.Bucket, &pm); err != nil {
			return nil, nil, fmt.Errorf("GetComplianceOccupancy scan: %w", err)
		}
		b.PersonCount = pm
		totalMinutes += pm
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if buckets == nil {
		buckets = []OccupancyBucket{}
	}

	hours := totalMinutes / 60.0
	return buckets, &hours, nil
}

// VerifySiteOwnership checks that a site belongs to the given org.
// Returns (true, nil) on match, (false, nil) if site not found or wrong org.
func (db *DB) VerifySiteOwnership(ctx context.Context, siteID, orgID string) (bool, error) {
	var gotOrgID string
	err := db.Pool.QueryRow(ctx,
		`SELECT organization_id FROM sites WHERE id = $1`, siteID,
	).Scan(&gotOrgID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("VerifySiteOwnership: %w", err)
	}
	return gotOrgID == orgID, nil
}
