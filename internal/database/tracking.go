package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InsertTrackFrame persists one raw per-frame person-count row.
// OrganizationID is required; an empty value returns an error.
func (db *DB) InsertTrackFrame(ctx context.Context, ins PersonTrackFrameInsert) error {
	if ins.OrganizationID == "" {
		return fmt.Errorf("InsertTrackFrame: organization_id is required")
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO person_track_frames
		    (time, camera_id, site_id, organization_id, person_count, frame_source)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		ins.Time,
		ins.CameraID,
		ins.SiteID,
		ins.OrganizationID,
		ins.PersonCount,
		ins.FrameSource,
	)
	if err != nil {
		return fmt.Errorf("InsertTrackFrame: %w", err)
	}
	return nil
}

// UpsertTrackBucket inserts or updates one pre-aggregated bucket row.
// The conflict target is (camera_id, bucket_start, bucket_minutes).
// Re-running for the same window overwrites with recomputed values — safe
// because the raw frame data is authoritative and immutable once written.
func (db *DB) UpsertTrackBucket(ctx context.Context, b PersonTrackBucket) error {
	if b.OrganizationID == "" {
		return fmt.Errorf("UpsertTrackBucket: organization_id is required")
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO person_track_buckets
		    (camera_id, site_id, organization_id,
		     bucket_start, bucket_minutes,
		     person_minutes, peak_person_count, frame_count,
		     violation_count, rolled_up_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (camera_id, bucket_start, bucket_minutes) DO UPDATE SET
		    person_minutes    = EXCLUDED.person_minutes,
		    peak_person_count = EXCLUDED.peak_person_count,
		    frame_count       = EXCLUDED.frame_count,
		    violation_count   = EXCLUDED.violation_count,
		    rolled_up_at      = EXCLUDED.rolled_up_at`,
		b.CameraID,
		b.SiteID,
		b.OrganizationID,
		b.BucketStart,
		b.BucketMinutes,
		b.PersonMinutes,
		b.PeakPersonCount,
		b.FrameCount,
		b.ViolationCount,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("UpsertTrackBucket: %w", err)
	}
	return nil
}

// ListTrackBuckets returns pre-aggregated bucket rows scoped to one
// organization. All fields in filter are optional except OrganizationID.
// Results are ordered bucket_start DESC. Limit defaults to 500, max 2000.
func (db *DB) ListTrackBuckets(ctx context.Context, f TrackBucketFilter) ([]PersonTrackBucket, error) {
	if f.OrganizationID == "" {
		return nil, fmt.Errorf("ListTrackBuckets: organization_id is required")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}

	// Build parameterised query. organization_id is always the first filter.
	args := []interface{}{f.OrganizationID}
	where := "WHERE ptb.organization_id = $1"
	idx := 2

	if f.CameraID != nil {
		where += fmt.Sprintf(" AND ptb.camera_id = $%d", idx)
		args = append(args, f.CameraID)
		idx++
	}
	if f.SiteID != nil {
		where += fmt.Sprintf(" AND ptb.site_id = $%d", idx)
		args = append(args, f.SiteID)
		idx++
	}
	if !f.Start.IsZero() {
		where += fmt.Sprintf(" AND ptb.bucket_start >= $%d", idx)
		args = append(args, f.Start)
		idx++
	}
	if !f.End.IsZero() {
		where += fmt.Sprintf(" AND ptb.bucket_start <= $%d", idx)
		args = append(args, f.End)
		idx++
	}
	if f.BucketMinutes > 0 {
		where += fmt.Sprintf(" AND ptb.bucket_minutes = $%d", idx)
		args = append(args, f.BucketMinutes)
		idx++
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT
		    ptb.camera_id,
		    ptb.site_id,
		    ptb.organization_id,
		    ptb.bucket_start,
		    ptb.bucket_minutes,
		    ptb.person_minutes,
		    ptb.peak_person_count,
		    ptb.frame_count,
		    ptb.violation_count,
		    ptb.rolled_up_at,
		    COALESCE(c.name, '') AS camera_name,
		    COALESCE(s.name, '') AS site_name
		FROM person_track_buckets ptb
		LEFT JOIN cameras c ON c.id = ptb.camera_id
		LEFT JOIN sites   s ON s.id = ptb.site_id
		%s
		ORDER BY ptb.bucket_start DESC
		LIMIT $%d`, where, idx)

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListTrackBuckets: %w", err)
	}
	defer rows.Close()

	var out []PersonTrackBucket
	for rows.Next() {
		var b PersonTrackBucket
		if err := rows.Scan(
			&b.CameraID,
			&b.SiteID,
			&b.OrganizationID,
			&b.BucketStart,
			&b.BucketMinutes,
			&b.PersonMinutes,
			&b.PeakPersonCount,
			&b.FrameCount,
			&b.ViolationCount,
			&b.RolledUpAt,
			&b.CameraName,
			&b.SiteName,
		); err != nil {
			return nil, fmt.Errorf("ListTrackBuckets scan: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AggregateFramesIntoBucket reads person_track_frames rows in the given
// [windowStart, windowEnd) interval for a specific camera and computes the
// bucket metrics. Returns nil, nil if there are no rows in the window.
func (db *DB) AggregateFramesIntoBucket(ctx context.Context, cameraID uuid.UUID, windowStart, windowEnd time.Time, bucketMinutes int) (*PersonTrackBucket, error) {
	var b PersonTrackBucket
	err := db.Pool.QueryRow(ctx, `
		SELECT
		    camera_id,
		    site_id,
		    organization_id,
		    $2::timestamptz                          AS bucket_start,
		    $4::int                                  AS bucket_minutes,
		    SUM(person_count)::real * 0.5            AS person_minutes,
		    MAX(person_count)                         AS peak_person_count,
		    COUNT(*)                                  AS frame_count
		FROM person_track_frames
		WHERE camera_id     = $1
		  AND time          >= $2
		  AND time          <  $3
		GROUP BY camera_id, site_id, organization_id`,
		cameraID, windowStart, windowEnd, bucketMinutes,
	).Scan(
		&b.CameraID,
		&b.SiteID,
		&b.OrganizationID,
		&b.BucketStart,
		&b.BucketMinutes,
		&b.PersonMinutes,
		&b.PeakPersonCount,
		&b.FrameCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("AggregateFramesIntoBucket: %w", err)
	}
	return &b, nil
}

// CountPendingReviewViolations returns the number of pending_review_queue
// rows for the given camera in [windowStart, windowEnd). Used by the
// aggregator to populate bucket.violation_count.
func (db *DB) CountPendingReviewViolations(ctx context.Context, cameraID uuid.UUID, windowStart, windowEnd time.Time) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pending_review_queue
		WHERE camera_id  = $1
		  AND created_at >= $2
		  AND created_at <  $3`,
		cameraID, windowStart, windowEnd,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountPendingReviewViolations: %w", err)
	}
	return n, nil
}

// ListCamerasWithUnbucketedFrames returns distinct camera_id values that have
// person_track_frames rows in [sinceTime, beforeTime) but no bucket row
// covering any part of that window with the given bucket granularity. Used by
// the aggregator's backfill sweep to detect gaps.
//
// Uses a simplified heuristic rather than exact bucket-boundary SQL arithmetic
// (which has INTERVAL type-casting issues on some PG versions). The aggregator
// then attempts to compute each bucket window — UpsertTrackBucket is idempotent.
func (db *DB) ListCamerasWithUnbucketedFrames(ctx context.Context, sinceTime, beforeTime time.Time, bucketMinutes int) ([]uuid.UUID, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT ptf.camera_id
		FROM person_track_frames ptf
		WHERE ptf.time >= $1
		  AND ptf.time <  $2
		  AND NOT EXISTS (
		      SELECT 1 FROM person_track_buckets ptb
		      WHERE ptb.camera_id    = ptf.camera_id
		        AND ptb.bucket_minutes = $3
		        AND ptb.bucket_start >= $1
		        AND ptb.bucket_start <  $2
		  )`,
		sinceTime, beforeTime, bucketMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("ListCamerasWithUnbucketedFrames: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SweepTrackFrames deletes person_track_frames rows older than retentionDays.
// This is the Go-side belt-and-suspenders fallback for the TimescaleDB
// add_retention_policy call in migration 0023 (R2 / R5 mitigation).
func (db *DB) SweepTrackFrames(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM person_track_frames WHERE time < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("SweepTrackFrames: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SweepTrackBuckets deletes person_track_buckets rows whose bucket_start
// is older than retentionDays. The bucket table is a regular table with
// no TimescaleDB retention policy, so this is the only sweep path.
func (db *DB) SweepTrackBuckets(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM person_track_buckets WHERE bucket_start < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("SweepTrackBuckets: %w", err)
	}
	return tag.RowsAffected(), nil
}
