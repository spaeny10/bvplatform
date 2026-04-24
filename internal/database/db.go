package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the PostgreSQL connection pool and provides query methods
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool
func New(databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	config.MaxConns = 50
	config.MinConns = 4
	config.MaxConnIdleTime = 5 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	log.Println("[DB] Connected to PostgreSQL")
	return &DB{Pool: pool}, nil
}

// Close closes the database connection pool
func (db *DB) Close() {
	db.Pool.Close()
}

// ============================================================
// Camera Operations
// ============================================================

// CreateCamera inserts a new camera record
func (db *DB) CreateCamera(ctx context.Context, c *Camera) error {
	c.ID = uuid.New()
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	if c.Status == "" {
		c.Status = "offline"
	}
	if c.RecordingMode == "" {
		c.RecordingMode = "continuous"
	}
	if c.PreBufferSec == 0 {
		c.PreBufferSec = 10
	}
	if c.PostBufferSec == 0 {
		c.PostBufferSec = 30
	}
	if c.RecordingTriggers == "" {
		c.RecordingTriggers = "motion,object"
	}

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO cameras (id, name, onvif_address, username, password, rtsp_uri, sub_stream_uri,
			retention_days, recording, recording_mode, pre_buffer_sec, post_buffer_sec, recording_triggers,
			events_enabled, audio_enabled, camera_group, schedule, privacy_mask,
			status, profile_token, has_ptz, manufacturer, model, firmware, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`,
		c.ID, c.Name, c.OnvifAddress, c.Username, c.Password, c.RTSPUri, c.SubStreamUri,
		c.RetentionDays, c.Recording, c.RecordingMode, c.PreBufferSec, c.PostBufferSec, c.RecordingTriggers,
		c.EventsEnabled, c.AudioEnabled, c.CameraGroup, c.Schedule, c.PrivacyMask,
		c.Status, c.ProfileToken, c.HasPTZ, c.Manufacturer, c.Model, c.Firmware,
		c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// GetCamera retrieves a single camera by ID
func (db *DB) GetCamera(ctx context.Context, id uuid.UUID) (*Camera, error) {
	c := &Camera{}
	err := db.Pool.QueryRow(ctx, `
		SELECT id, name, onvif_address, username, password, rtsp_uri, sub_stream_uri,
			retention_days, recording, recording_mode, pre_buffer_sec, post_buffer_sec, recording_triggers,
			events_enabled, audio_enabled, camera_group, schedule, privacy_mask,
			status, profile_token, has_ptz, manufacturer, model, firmware,
			COALESCE(site_id, ''),
			created_at, updated_at
		FROM cameras WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &c.OnvifAddress, &c.Username, &c.Password, &c.RTSPUri, &c.SubStreamUri,
		&c.RetentionDays, &c.Recording, &c.RecordingMode, &c.PreBufferSec, &c.PostBufferSec, &c.RecordingTriggers,
		&c.EventsEnabled, &c.AudioEnabled, &c.CameraGroup, &c.Schedule, &c.PrivacyMask,
		&c.Status, &c.ProfileToken, &c.HasPTZ, &c.Manufacturer, &c.Model,
		&c.Firmware, &c.SiteID, &c.CreatedAt, &c.UpdatedAt)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// ListCameras retrieves all cameras
func (db *DB) ListCameras(ctx context.Context) ([]Camera, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, name, onvif_address, username, password, rtsp_uri, sub_stream_uri,
			retention_days, recording, recording_mode, pre_buffer_sec, post_buffer_sec, recording_triggers,
			events_enabled, audio_enabled, camera_group, schedule, privacy_mask,
			status, profile_token, has_ptz, manufacturer, model, firmware,
			COALESCE(site_id, ''),
			created_at, updated_at
		FROM cameras ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var c Camera
		if err := rows.Scan(&c.ID, &c.Name, &c.OnvifAddress, &c.Username, &c.Password, &c.RTSPUri,
			&c.SubStreamUri, &c.RetentionDays, &c.Recording, &c.RecordingMode, &c.PreBufferSec,
			&c.PostBufferSec, &c.RecordingTriggers,
			&c.EventsEnabled, &c.AudioEnabled, &c.CameraGroup, &c.Schedule, &c.PrivacyMask,
			&c.Status, &c.ProfileToken, &c.HasPTZ,
			&c.Manufacturer, &c.Model, &c.Firmware, &c.SiteID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		cameras = append(cameras, c)
	}
	return cameras, nil
}

// UpdateCamera updates a camera's fields
func (db *DB) UpdateCamera(ctx context.Context, id uuid.UUID, update CameraUpdate) error {
	sets := []string{}
	args := []interface{}{}
	argN := 1

	if update.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argN))
		args = append(args, *update.Name)
		argN++
	}
	if update.OnvifAddress != nil {
		sets = append(sets, fmt.Sprintf("onvif_address = $%d", argN))
		args = append(args, *update.OnvifAddress)
		argN++
	}
	if update.RtspURI != nil {
		sets = append(sets, fmt.Sprintf("rtsp_uri = $%d", argN))
		args = append(args, *update.RtspURI)
		argN++
	}
	if update.SubStreamURI != nil {
		sets = append(sets, fmt.Sprintf("sub_stream_uri = $%d", argN))
		args = append(args, *update.SubStreamURI)
		argN++
	}
	if update.Username != nil {
		sets = append(sets, fmt.Sprintf("username = $%d", argN))
		args = append(args, *update.Username)
		argN++
	}
	if update.RetentionDays != nil {
		sets = append(sets, fmt.Sprintf("retention_days = $%d", argN))
		args = append(args, *update.RetentionDays)
		argN++
	}
	if update.Recording != nil {
		sets = append(sets, fmt.Sprintf("recording = $%d", argN))
		args = append(args, *update.Recording)
		argN++
	}
	if update.RecordingMode != nil {
		sets = append(sets, fmt.Sprintf("recording_mode = $%d", argN))
		args = append(args, *update.RecordingMode)
		argN++
	}
	if update.PreBufferSec != nil {
		sets = append(sets, fmt.Sprintf("pre_buffer_sec = $%d", argN))
		args = append(args, *update.PreBufferSec)
		argN++
	}
	if update.PostBufferSec != nil {
		sets = append(sets, fmt.Sprintf("post_buffer_sec = $%d", argN))
		args = append(args, *update.PostBufferSec)
		argN++
	}
	if update.RecordingTriggers != nil {
		sets = append(sets, fmt.Sprintf("recording_triggers = $%d", argN))
		args = append(args, *update.RecordingTriggers)
		argN++
	}
	if update.EventsEnabled != nil {
		sets = append(sets, fmt.Sprintf("events_enabled = $%d", argN))
		args = append(args, *update.EventsEnabled)
		argN++
	}
	if update.AudioEnabled != nil {
		sets = append(sets, fmt.Sprintf("audio_enabled = $%d", argN))
		args = append(args, *update.AudioEnabled)
		argN++
	}
	if update.CameraGroup != nil {
		sets = append(sets, fmt.Sprintf("camera_group = $%d", argN))
		args = append(args, *update.CameraGroup)
		argN++
	}
	if update.Schedule != nil {
		sets = append(sets, fmt.Sprintf("schedule = $%d", argN))
		args = append(args, *update.Schedule)
		argN++
	}
	if update.PrivacyMask != nil {
		sets = append(sets, fmt.Sprintf("privacy_mask = $%d", argN))
		args = append(args, *update.PrivacyMask)
		argN++
	}

	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, fmt.Sprintf("updated_at = $%d", argN))
	args = append(args, time.Now())
	argN++

	args = append(args, id)
	query := fmt.Sprintf("UPDATE cameras SET %s WHERE id = $%d", strings.Join(sets, ", "), argN)

	_, err := db.Pool.Exec(ctx, query, args...)
	return err
}

// UpdateCameraStatus sets the camera status
func (db *DB) UpdateCameraStatus(ctx context.Context, id uuid.UUID, status string) error {
	_, err := db.Pool.Exec(ctx, "UPDATE cameras SET status = $1, updated_at = $2 WHERE id = $3",
		status, time.Now(), id)
	return err
}

// UpdateCameraRTSP sets the RTSP URIs after ONVIF profile enumeration
func (db *DB) UpdateCameraRTSP(ctx context.Context, id uuid.UUID, rtspUri, subStreamUri, profileToken, manufacturer, model, firmware string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE cameras SET rtsp_uri=$1, sub_stream_uri=$2, profile_token=$3,
			manufacturer=$4, model=$5, firmware=$6, updated_at=$7
		WHERE id=$8`,
		rtspUri, subStreamUri, profileToken, manufacturer, model, firmware, time.Now(), id)
	return err
}

// DeleteCamera removes a camera and all associated data
func (db *DB) DeleteCamera(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, "DELETE FROM cameras WHERE id = $1", id)
	return err
}

// ============================================================
// VCA Rule Operations
// ============================================================

func (db *DB) ListVCARules(ctx context.Context, cameraID uuid.UUID) ([]VCARule, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, camera_id, rule_type, name, enabled, sensitivity, region, direction,
		        threshold_sec, schedule, actions, synced, sync_error, created_at, updated_at
		 FROM vca_rules WHERE camera_id = $1 ORDER BY created_at`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []VCARule
	for rows.Next() {
		var r VCARule
		var regionJSON, actionsJSON []byte
		if err := rows.Scan(&r.ID, &r.CameraID, &r.RuleType, &r.Name, &r.Enabled,
			&r.Sensitivity, &regionJSON, &r.Direction, &r.ThresholdSec, &r.Schedule,
			&actionsJSON, &r.Synced, &r.SyncError, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(regionJSON, &r.Region)
		json.Unmarshal(actionsJSON, &r.Actions)
		if r.Region == nil {
			r.Region = []Point{}
		}
		if r.Actions == nil {
			r.Actions = []string{}
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func (db *DB) CreateVCARule(ctx context.Context, cameraID uuid.UUID, c *VCARuleCreate) (*VCARule, error) {
	r := &VCARule{
		ID:           uuid.New(),
		CameraID:     cameraID,
		RuleType:     c.RuleType,
		Name:         c.Name,
		Enabled:      c.Enabled,
		Sensitivity:  c.Sensitivity,
		Region:       c.Region,
		Direction:    c.Direction,
		ThresholdSec: c.ThresholdSec,
		Schedule:     c.Schedule,
		Actions:      c.Actions,
	}
	if r.Schedule == "" {
		r.Schedule = "always"
	}
	if r.Actions == nil {
		r.Actions = []string{"record", "notify"}
	}
	regionJSON, _ := json.Marshal(r.Region)
	actionsJSON, _ := json.Marshal(r.Actions)
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO vca_rules (id, camera_id, rule_type, name, enabled, sensitivity,
		 region, direction, threshold_sec, schedule, actions)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		r.ID, r.CameraID, r.RuleType, r.Name, r.Enabled, r.Sensitivity,
		regionJSON, r.Direction, r.ThresholdSec, r.Schedule, actionsJSON)
	return r, err
}

func (db *DB) UpdateVCARule(ctx context.Context, id uuid.UUID, c *VCARuleCreate) error {
	regionJSON, _ := json.Marshal(c.Region)
	actionsJSON, _ := json.Marshal(c.Actions)
	_, err := db.Pool.Exec(ctx,
		`UPDATE vca_rules SET name=$2, enabled=$3, sensitivity=$4, region=$5,
		 direction=$6, threshold_sec=$7, schedule=$8, actions=$9,
		 synced=false, updated_at=NOW()
		 WHERE id=$1`,
		id, c.Name, c.Enabled, c.Sensitivity, regionJSON,
		c.Direction, c.ThresholdSec, c.Schedule, actionsJSON)
	return err
}

func (db *DB) DeleteVCARule(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, "DELETE FROM vca_rules WHERE id = $1", id)
	return err
}

func (db *DB) UpdateVCARuleSync(ctx context.Context, id uuid.UUID, synced bool, syncError string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE vca_rules SET synced=$2, sync_error=$3, updated_at=NOW() WHERE id=$1`,
		id, synced, syncError)
	return err
}

// ============================================================
// Segment Operations
// ============================================================

// InsertSegment records a new video segment
func (db *DB) InsertSegment(ctx context.Context, s *Segment) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO segments (camera_id, start_time, end_time, file_path, file_size, duration_ms, has_audio)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		s.CameraID, s.StartTime, s.EndTime, s.FilePath, s.FileSize, s.DurationMs, s.HasAudio)
	return err
}

// GetSegments returns segments for a camera within a time range
func (db *DB) GetSegments(ctx context.Context, cameraID uuid.UUID, start, end time.Time) ([]Segment, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, camera_id, start_time, end_time, file_path, file_size, duration_ms, COALESCE(has_audio, false)
		FROM segments
		WHERE camera_id = $1 AND start_time <= $2 AND end_time >= $3
		ORDER BY start_time ASC`, cameraID, end, start)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []Segment
	for rows.Next() {
		var s Segment
		if err := rows.Scan(&s.ID, &s.CameraID, &s.StartTime, &s.EndTime, &s.FilePath, &s.FileSize, &s.DurationMs, &s.HasAudio); err != nil {
			return nil, err
		}
		segments = append(segments, s)
	}
	return segments, nil
}

// SegmentCoverage is a lightweight span used for the timeline coverage bars.
type SegmentCoverage struct {
	CameraID  string `json:"camera_id"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	HasAudio  bool   `json:"has_audio"`
}

// GetSegmentCoverage returns lightweight coverage spans for multiple cameras in a time window.
func (db *DB) GetSegmentCoverage(ctx context.Context, cameraIDs []uuid.UUID, start, end time.Time) ([]SegmentCoverage, error) {
	if len(cameraIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(cameraIDs))
	args := []interface{}{end, start}
	for i, id := range cameraIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT camera_id, start_time, end_time, COALESCE(has_audio, false)
		FROM segments
		WHERE start_time <= $1 AND end_time >= $2
		  AND camera_id IN (%s)
		ORDER BY camera_id, start_time ASC`,
		strings.Join(placeholders, ","))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spans []SegmentCoverage
	for rows.Next() {
		var camID uuid.UUID
		var s, e time.Time
		var hasAudio bool
		if err := rows.Scan(&camID, &s, &e, &hasAudio); err != nil {
			return nil, err
		}
		spans = append(spans, SegmentCoverage{
			CameraID:  camID.String(),
			StartTime: s.UTC().Format(time.RFC3339),
			EndTime:   e.UTC().Format(time.RFC3339),
			HasAudio:  hasAudio,
		})
	}
	return spans, nil
}

// DeleteOldSegments removes segments older than the given time and returns their file paths for cleanup
func (db *DB) DeleteOldSegments(ctx context.Context, cameraID uuid.UUID, before time.Time) ([]string, error) {
	rows, err := db.Pool.Query(ctx, `
		DELETE FROM segments WHERE camera_id = $1 AND end_time < $2
		RETURNING file_path`, cameraID, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// GetStorageUsageByPath returns total bytes of all segments whose file_path starts with the given prefix
func (db *DB) GetStorageUsageByPath(ctx context.Context, pathPrefix string) (int64, error) {
	var total int64
	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(file_size), 0) FROM segments
		WHERE file_path LIKE $1 || '%'`, pathPrefix).Scan(&total)
	return total, err
}

// DeleteOldestSegmentsByPath deletes the N oldest segments under a path prefix, returning file paths and freed bytes
func (db *DB) DeleteOldestSegmentsByPath(ctx context.Context, pathPrefix string, limit int) ([]string, int64, error) {
	rows, err := db.Pool.Query(ctx, `
		DELETE FROM segments WHERE id IN (
			SELECT id FROM segments
			WHERE file_path LIKE $1 || '%'
			ORDER BY start_time ASC
			LIMIT $2
		)
		RETURNING file_path, file_size`, pathPrefix, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var paths []string
	var totalFreed int64
	for rows.Next() {
		var path string
		var size int64
		if err := rows.Scan(&path, &size); err != nil {
			return nil, 0, err
		}
		paths = append(paths, path)
		totalFreed += size
	}
	return paths, totalFreed, nil
}

// ============================================================
// Event Operations
// ============================================================

// InsertEvent records a metadata event and populates e.ID with the generated row id
func (db *DB) InsertEvent(ctx context.Context, e *Event) error {
	detailsJSON, err := json.Marshal(e.Details)
	if err != nil {
		return err
	}

	// Best-effort: find the segment that contains event_time for this camera
	// so the event row is directly linked to its video. If no segment covers
	// the moment (recording down, edge event), segment_id stays NULL.
	var segID *int64
	var sid int64
	sErr := db.Pool.QueryRow(ctx, `
		SELECT id FROM segments
		WHERE camera_id = $1 AND start_time <= $2 AND end_time >= $2
		ORDER BY start_time DESC
		LIMIT 1`, e.CameraID, e.EventTime).Scan(&sid)
	if sErr == nil {
		segID = &sid
	}

	err = db.Pool.QueryRow(ctx, `
		INSERT INTO events (camera_id, event_time, event_type, details, thumbnail, segment_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		e.CameraID, e.EventTime, e.EventType, detailsJSON, e.Thumbnail, segID).Scan(&e.ID)
	if err == nil {
		e.SegmentID = segID
	}
	return err
}

// UpdateEventThumbnail sets the base64 thumbnail for an event after async capture
func (db *DB) UpdateEventThumbnail(ctx context.Context, eventID int64, thumbnail string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE events SET thumbnail = $1 WHERE id = $2`,
		thumbnail, eventID)
	return err
}

// QueryEvents returns events matching the given filters
func (db *DB) QueryEvents(ctx context.Context, q EventQuery) ([]Event, error) {
	// All predicates reference columns on the `events` table (aliased `e` in
	// the final SELECT). Using the alias up-front avoids "ambiguous column"
	// errors once we JOIN `segments` (which also has camera_id/start_time).
	where := []string{"e.event_time >= $1", "e.event_time <= $2"}
	args := []interface{}{q.StartTime, q.EndTime}
	argN := 3

	if q.CameraID != nil {
		where = append(where, fmt.Sprintf("e.camera_id = $%d", argN))
		args = append(args, *q.CameraID)
		argN++
	}

	if len(q.EventTypes) > 0 {
		where = append(where, fmt.Sprintf("e.event_type = ANY($%d)", argN))
		args = append(args, q.EventTypes)
		argN++
	}

	if q.Search != "" {
		where = append(where, fmt.Sprintf("e.details::text ILIKE $%d", argN))
		args = append(args, "%"+q.Search+"%")
		argN++
	}

	// Optional camera-ID whitelist (RBAC: restrict to user's assigned cameras).
	// An empty slice with CameraIDsNonNil=true forces zero rows, which is the
	// correct behavior for an authenticated user with no camera access.
	if q.CameraIDsNonNil {
		if len(q.CameraIDs) == 0 {
			return []Event{}, nil
		}
		where = append(where, fmt.Sprintf("e.camera_id = ANY($%d)", argN))
		args = append(args, q.CameraIDs)
		argN++
	}

	limit := q.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	// Join segments so we can return the segment file path + start_time in
	// one round-trip; the handler turns that into a seekable playback URL.
	query := fmt.Sprintf(`
		SELECT e.id, e.camera_id, e.event_time, e.event_type, e.details, e.thumbnail,
		       e.segment_id, s.file_path, s.start_time
		FROM events e
		LEFT JOIN segments s ON s.id = e.segment_id
		WHERE %s
		ORDER BY e.event_time DESC
		LIMIT %d OFFSET %d`,
		strings.Join(where, " AND "), limit, q.Offset)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var detailsJSON []byte
		var segID *int64
		var segPath *string
		var segStart *time.Time
		if err := rows.Scan(&e.ID, &e.CameraID, &e.EventTime, &e.EventType, &detailsJSON, &e.Thumbnail,
			&segID, &segPath, &segStart); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(detailsJSON, &e.Details); err != nil {
			e.Details = map[string]interface{}{}
		}
		e.SegmentID = segID
		if segPath != nil && segStart != nil {
			e.PlaybackURL = buildPlaybackURL(e.CameraID.String(), *segPath, *segStart, e.EventTime)
		}
		events = append(events, e)
	}
	return events, nil
}

// buildPlaybackURL returns a /recordings/... URL for a segment with a #t= seek
// offset positioned at the event moment. segFilePath is the absolute filesystem
// path the segmenter wrote (e.g. D:/recordings/<camera>/seg_…mp4); we drop the
// directory and prefix with /recordings/<camera>/ so the static file server
// can serve it.
func buildPlaybackURL(cameraID, segFilePath string, segStart, eventTime time.Time) string {
	base := filepath.Base(segFilePath)
	offset := eventTime.Sub(segStart).Seconds()
	if offset < 0 || offset > 7200 {
		return fmt.Sprintf("/recordings/%s/%s", cameraID, base)
	}
	return fmt.Sprintf("/recordings/%s/%s#t=%.1f", cameraID, base, offset)
}

// GetTimelineBuckets returns aggregated event counts per time interval for the timeline UI
func (db *DB) GetTimelineBuckets(ctx context.Context, cameraIDs []uuid.UUID, start, end time.Time, intervalMinutes int) ([]TimelineBucket, error) {
	where := []string{"event_time >= $1", "event_time <= $2"}
	args := []interface{}{start, end}
	argN := 3

	if len(cameraIDs) == 1 {
		where = append(where, fmt.Sprintf("camera_id = $%d", argN))
		args = append(args, cameraIDs[0])
		argN++
	} else if len(cameraIDs) > 1 {
		placeholders := make([]string, len(cameraIDs))
		for i, id := range cameraIDs {
			placeholders[i] = fmt.Sprintf("$%d", argN)
			args = append(args, id)
			argN++
		}
		where = append(where, fmt.Sprintf("camera_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	// Use standard PostgreSQL date_trunc + interval math instead of TimescaleDB's time_bucket
	// This works with or without TimescaleDB installed
	intervalSecs := intervalMinutes * 60

	query := fmt.Sprintf(`
		SELECT to_timestamp(floor(extract(epoch FROM event_time) / %d) * %d) AS bucket,
			   event_type,
			   COUNT(*) as cnt
		FROM events
		WHERE %s
		GROUP BY bucket, event_type
		ORDER BY bucket ASC`,
		intervalSecs, intervalSecs, strings.Join(where, " AND "))

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Aggregate into buckets
	bucketMap := make(map[time.Time]*TimelineBucket)
	for rows.Next() {
		var bucket time.Time
		var eventType string
		var count int
		if err := rows.Scan(&bucket, &eventType, &count); err != nil {
			return nil, err
		}
		if b, ok := bucketMap[bucket]; ok {
			b.Counts[eventType] = count
			b.Total += count
		} else {
			bucketMap[bucket] = &TimelineBucket{
				BucketTime: bucket,
				Counts:     map[string]int{eventType: count},
				Total:      count,
			}
		}
	}

	// Convert to slice sorted by time
	var buckets []TimelineBucket
	for _, b := range bucketMap {
		buckets = append(buckets, *b)
	}
	return buckets, nil
}

// ============================================================
// Export Operations
// ============================================================

// CreateExport inserts a new export job
func (db *DB) CreateExport(ctx context.Context, e *Export) error {
	e.ID = uuid.New()
	e.CreatedAt = time.Now()
	e.Status = "pending"

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO exports (id, camera_id, start_time, end_time, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID, e.CameraID, e.StartTime, e.EndTime, e.Status, e.CreatedAt)
	return err
}

// UpdateExportStatus updates the status and optional file info for an export
func (db *DB) UpdateExportStatus(ctx context.Context, id uuid.UUID, status, filePath string, fileSize int64, errMsg string) error {
	var completedAt *time.Time
	if status == "completed" || status == "failed" {
		now := time.Now()
		completedAt = &now
	}
	_, err := db.Pool.Exec(ctx, `
		UPDATE exports SET status=$1, file_path=$2, file_size=$3, error=$4, completed_at=$5
		WHERE id=$6`,
		status, filePath, fileSize, errMsg, completedAt, id)
	return err
}

// ClaimNextExport atomically picks the oldest pending export and flips it
// to processing. Returns (nil, nil) when the queue is empty — that's the
// normal "nothing to do" path, not an error.
//
// The claim uses UPDATE ... WHERE id = (SELECT ... FOR UPDATE SKIP LOCKED)
// so multiple workers (today: one; post-Phase-2: N) can poll concurrently
// without collisions. Each row is visible to exactly one SELECT at a time;
// losers skip that row and move on. Portable Postgres 9.5+ behaviour.
//
// started_at is set to NOW() so RequeueStuckExports can later identify
// jobs that a crashed worker left in processing.
func (db *DB) ClaimNextExport(ctx context.Context) (*Export, error) {
	var e Export
	err := db.Pool.QueryRow(ctx, `
		UPDATE exports SET
			status     = 'processing',
			started_at = NOW()
		WHERE id = (
			SELECT id FROM exports
			WHERE status = 'pending'
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, camera_id, start_time, end_time, status, file_path, file_size, error, created_at, completed_at
	`).Scan(&e.ID, &e.CameraID, &e.StartTime, &e.EndTime, &e.Status,
		&e.FilePath, &e.FileSize, &e.Error, &e.CreatedAt, &e.CompletedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // empty queue
		}
		return nil, err
	}
	return &e, nil
}

// RequeueStuckExports flips any exports stuck in 'processing' back to
// 'pending' so a fresh worker picks them up. Called once at server start
// to recover from a crash that left jobs in limbo.
//
// `timeout` bounds how long a legitimately-running job can hold the
// processing state before being considered stuck. FFmpeg concats of long
// clips can take a few minutes; 10 minutes is a safe default.
//
// Returns the number of jobs requeued.
func (db *DB) RequeueStuckExports(ctx context.Context, timeout time.Duration) (int, error) {
	ct, err := db.Pool.Exec(ctx, `
		UPDATE exports SET
			status     = 'pending',
			started_at = NULL,
			error      = COALESCE(NULLIF(error, ''), '') || 'requeued after worker crash; '
		WHERE status = 'processing'
		  AND started_at IS NOT NULL
		  AND started_at < NOW() - $1::interval
	`, fmt.Sprintf("%d seconds", int(timeout.Seconds())))
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// ListExports returns recent export jobs
func (db *DB) ListExports(ctx context.Context) ([]Export, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, camera_id, start_time, end_time, status, file_path, file_size, error, created_at, completed_at
		FROM exports ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var exports []Export
	for rows.Next() {
		var e Export
		if err := rows.Scan(&e.ID, &e.CameraID, &e.StartTime, &e.EndTime, &e.Status, &e.FilePath,
			&e.FileSize, &e.Error, &e.CreatedAt, &e.CompletedAt); err != nil {
			return nil, err
		}
		exports = append(exports, e)
	}
	return exports, nil
}

// ============================================================
// User / Auth Operations
// ============================================================

// CreateUser inserts a new user with full profile (password must already be bcrypt-hashed)
func (db *DB) CreateUser(ctx context.Context, c *UserCreate, passwordHash string) (*User, error) {
	if len(c.AssignedSiteIDs) == 0 {
		c.AssignedSiteIDs = []string{}
	}
	siteIDsJSON, _ := json.Marshal(c.AssignedSiteIDs)
	u := &User{
		ID:              uuid.New(),
		Username:        c.Username,
		PasswordHash:    passwordHash,
		Role:            c.Role,
		DisplayName:     c.DisplayName,
		Email:           c.Email,
		Phone:           c.Phone,
		OrganizationID:  c.OrganizationID,
		AssignedSiteIDs: c.AssignedSiteIDs,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, role, display_name, email, phone, organization_id, assigned_site_ids, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.Email, u.Phone, nullableStr(u.OrganizationID), siteIDsJSON, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByUsernameOrEmail retrieves a user by username or email (case-insensitive email match)
func (db *DB) GetUserByUsernameOrEmail(ctx context.Context, identifier string) (*User, error) {
	var u User
	var siteIDsJSON []byte
	var orgID *string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, username, password_hash, role, display_name, email, phone, organization_id, assigned_site_ids, created_at, updated_at
		 FROM users WHERE username=$1 OR (email != '' AND lower(email)=lower($1)) LIMIT 1`,
		identifier).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Email, &u.Phone, &orgID, &siteIDsJSON, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, nil // not found → nil
	}
	if orgID != nil {
		u.OrganizationID = *orgID
	}
	json.Unmarshal(siteIDsJSON, &u.AssignedSiteIDs)
	if u.AssignedSiteIDs == nil {
		u.AssignedSiteIDs = []string{}
	}
	return &u, nil
}

// GetUserByID retrieves a user by UUID
func (db *DB) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	var siteIDsJSON []byte
	var orgID *string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, username, password_hash, role, display_name, email, phone, organization_id, assigned_site_ids, created_at, updated_at
		 FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Email, &u.Phone, &orgID, &siteIDsJSON, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if orgID != nil {
		u.OrganizationID = *orgID
	}
	json.Unmarshal(siteIDsJSON, &u.AssignedSiteIDs)
	if u.AssignedSiteIDs == nil {
		u.AssignedSiteIDs = []string{}
	}
	return &u, nil
}

// UserExists returns true if any user row exists in the table
func (db *DB) UserExists(ctx context.Context) (bool, error) {
	var count int
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count > 0, err
}

// ListUsers returns all users without password hashes
func (db *DB) ListUsers(ctx context.Context) ([]UserPublic, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, username, role, display_name, email, phone, COALESCE(organization_id, ''), assigned_site_ids, created_at, updated_at
		 FROM users ORDER BY role, username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserPublic
	for rows.Next() {
		var u UserPublic
		var siteIDsJSON []byte
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.Email, &u.Phone, &u.OrganizationID, &siteIDsJSON, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(siteIDsJSON, &u.AssignedSiteIDs)
		if u.AssignedSiteIDs == nil {
			u.AssignedSiteIDs = []string{}
		}
		users = append(users, u)
	}
	if users == nil {
		users = []UserPublic{}
	}
	return users, nil
}

// DeleteUser removes a user by ID
func (db *DB) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// UpdateUserPassword updates only the bcrypt hash for a given user
func (db *DB) UpdateUserPassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3`,
		hash, time.Now(), id)
	return err
}

// UpdateUserRole sets a new role for the given user
func (db *DB) UpdateUserRole(ctx context.Context, id uuid.UUID, role string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE users SET role = $1, updated_at = $2 WHERE id = $3`,
		role, time.Now(), id)
	return err
}

// UpdateUserProfile updates non-auth profile fields
func (db *DB) UpdateUserProfile(ctx context.Context, id uuid.UUID, u *UserProfileUpdate) error {
	siteIDsJSON, _ := json.Marshal(u.AssignedSiteIDs)
	_, err := db.Pool.Exec(ctx, `
		UPDATE users SET
			display_name     = COALESCE($2, display_name),
			email            = COALESCE($3, email),
			phone            = COALESCE($4, phone),
			organization_id  = $5,
			assigned_site_ids = $6,
			updated_at       = NOW()
		WHERE id = $1`,
		id, u.DisplayName, u.Email, u.Phone, nullableStr(derefStr(u.OrganizationID)), siteIDsJSON)
	return err
}

// nullableStr converts an empty string to nil (for nullable DB columns)
func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ============================================================
// System Settings Operations
// ============================================================

// GetSettings retrieves the system settings row, or returns defaults if not yet seeded
func (db *DB) GetSettings(ctx context.Context) (*SystemSettings, error) {
	s := &SystemSettings{}
	err := db.Pool.QueryRow(ctx, `
		SELECT recordings_path, snapshots_path, exports_path, hls_path,
		       default_retention_days, default_recording_mode, default_segment_duration,
		       ffmpeg_path,
		       COALESCE(discovery_subnet, ''), COALESCE(discovery_ports, ''),
		       COALESCE(notification_webhook_url, ''), COALESCE(notification_email, ''),
		       COALESCE(notification_triggers, ''),
		       updated_at
		FROM system_settings WHERE id = 1`,
	).Scan(
		&s.RecordingsPath, &s.SnapshotsPath, &s.ExportsPath, &s.HLSPath,
		&s.DefaultRetentionDays, &s.DefaultRecordingMode, &s.DefaultSegmentDuration,
		&s.FFmpegPath,
		&s.DiscoverySubnet, &s.DiscoveryPorts,
		&s.NotificationWebhook, &s.NotificationEmail, &s.NotificationTriggers,
		&s.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		// Return defaults — row hasn't been seeded yet
		return &SystemSettings{
			RecordingsPath:         "./storage/recordings",
			SnapshotsPath:          "./storage/thumbnails",
			ExportsPath:            "./storage/exports",
			HLSPath:                "./storage/hls",
			DefaultRetentionDays:   30,
			DefaultRecordingMode:   "continuous",
			DefaultSegmentDuration: 60,
			FFmpegPath:             `C:\ffmpeg\bin\ffmpeg.exe`,
			UpdatedAt:              time.Now(),
		}, nil
	}
	return s, err
}

// UpsertSettings creates or fully replaces the system settings row
func (db *DB) UpsertSettings(ctx context.Context, s *SystemSettings) error {
	s.UpdatedAt = time.Now()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO system_settings
			(id, recordings_path, snapshots_path, exports_path, hls_path,
			 default_retention_days, default_recording_mode, default_segment_duration,
			 ffmpeg_path,
			 discovery_subnet, discovery_ports,
			 notification_webhook_url, notification_email, notification_triggers,
			 updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE SET
			recordings_path         = EXCLUDED.recordings_path,
			snapshots_path          = EXCLUDED.snapshots_path,
			exports_path            = EXCLUDED.exports_path,
			hls_path                = EXCLUDED.hls_path,
			default_retention_days  = EXCLUDED.default_retention_days,
			default_recording_mode  = EXCLUDED.default_recording_mode,
			default_segment_duration = EXCLUDED.default_segment_duration,
			ffmpeg_path             = EXCLUDED.ffmpeg_path,
			discovery_subnet        = EXCLUDED.discovery_subnet,
			discovery_ports         = EXCLUDED.discovery_ports,
			notification_webhook_url= EXCLUDED.notification_webhook_url,
			notification_email      = EXCLUDED.notification_email,
			notification_triggers   = EXCLUDED.notification_triggers,
			updated_at              = EXCLUDED.updated_at`,
		s.RecordingsPath, s.SnapshotsPath, s.ExportsPath, s.HLSPath,
		s.DefaultRetentionDays, s.DefaultRecordingMode, s.DefaultSegmentDuration,
		s.FFmpegPath,
		s.DiscoverySubnet, s.DiscoveryPorts,
		s.NotificationWebhook, s.NotificationEmail, s.NotificationTriggers,
		s.UpdatedAt,
	)
	return err
}

// ============================================================
// Storage Location Operations
// ============================================================

// ListStorageLocations returns all configured storage locations
func (db *DB) ListStorageLocations(ctx context.Context) ([]StorageLocation, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, label, path, purpose, retention_days, max_gb, priority, enabled, created_at, updated_at
		FROM storage_locations ORDER BY priority ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locs []StorageLocation
	for rows.Next() {
		var loc StorageLocation
		if err := rows.Scan(&loc.ID, &loc.Label, &loc.Path, &loc.Purpose,
			&loc.RetentionDays, &loc.MaxGB, &loc.Priority, &loc.Enabled,
			&loc.CreatedAt, &loc.UpdatedAt); err != nil {
			return nil, err
		}
		locs = append(locs, loc)
	}
	return locs, nil
}

// CreateStorageLocation inserts a new storage location
func (db *DB) CreateStorageLocation(ctx context.Context, c *StorageLocationCreate) (*StorageLocation, error) {
	loc := &StorageLocation{}
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO storage_locations (label, path, purpose, retention_days, max_gb, priority)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, label, path, purpose, retention_days, max_gb, priority, enabled, created_at, updated_at`,
		c.Label, c.Path, c.Purpose, c.RetentionDays, c.MaxGB, c.Priority,
	).Scan(&loc.ID, &loc.Label, &loc.Path, &loc.Purpose,
		&loc.RetentionDays, &loc.MaxGB, &loc.Priority, &loc.Enabled,
		&loc.CreatedAt, &loc.UpdatedAt)
	return loc, err
}

// UpdateStorageLocation updates an existing storage location
func (db *DB) UpdateStorageLocation(ctx context.Context, id uuid.UUID, c *StorageLocationCreate) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE storage_locations
		SET label = $1, path = $2, purpose = $3, retention_days = $4, max_gb = $5, priority = $6, updated_at = $7
		WHERE id = $8`,
		c.Label, c.Path, c.Purpose, c.RetentionDays, c.MaxGB, c.Priority, time.Now(), id)
	return err
}

// DeleteStorageLocation removes a storage location by ID
func (db *DB) DeleteStorageLocation(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM storage_locations WHERE id = $1`, id)
	return err
}

// ============================================================
// Speaker Operations
// ============================================================

// CreateSpeaker inserts a new speaker device
func (db *DB) CreateSpeaker(ctx context.Context, s *Speaker) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO speakers (id, name, onvif_address, username, password, rtsp_uri, zone,
			status, manufacturer, model, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.ID, s.Name, s.OnvifAddress, s.Username, s.Password, s.RTSPUri, s.Zone,
		s.Status, s.Manufacturer, s.Model, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

// GetSpeaker retrieves a speaker by ID
func (db *DB) GetSpeaker(ctx context.Context, id uuid.UUID) (*Speaker, error) {
	s := &Speaker{}
	err := db.Pool.QueryRow(ctx, `
		SELECT id, name, onvif_address, username, password, rtsp_uri, zone,
			status, manufacturer, model, created_at, updated_at
		FROM speakers WHERE id = $1`, id,
	).Scan(&s.ID, &s.Name, &s.OnvifAddress, &s.Username, &s.Password, &s.RTSPUri, &s.Zone,
		&s.Status, &s.Manufacturer, &s.Model, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ListSpeakers returns all configured speakers
func (db *DB) ListSpeakers(ctx context.Context) ([]Speaker, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, name, onvif_address, username, password, rtsp_uri, zone,
			status, manufacturer, model, created_at, updated_at
		FROM speakers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var speakers []Speaker
	for rows.Next() {
		var s Speaker
		if err := rows.Scan(&s.ID, &s.Name, &s.OnvifAddress, &s.Username, &s.Password, &s.RTSPUri,
			&s.Zone, &s.Status, &s.Manufacturer, &s.Model, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		speakers = append(speakers, s)
	}
	return speakers, rows.Err()
}

// DeleteSpeaker removes a speaker by ID
func (db *DB) DeleteSpeaker(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM speakers WHERE id = $1`, id)
	return err
}

// UpdateSpeakerStatus sets the status and optionally RTSP URI for a speaker
func (db *DB) UpdateSpeakerStatus(ctx context.Context, id uuid.UUID, status, rtspUri, manufacturer, model string) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE speakers SET status = $2, rtsp_uri = $3, manufacturer = $4, model = $5, updated_at = NOW()
		WHERE id = $1`, id, status, rtspUri, manufacturer, model)
	return err
}

// ============================================================
// Audio Message Operations
// ============================================================

// CreateAudioMessage inserts a new audio message record
func (db *DB) CreateAudioMessage(ctx context.Context, m *AudioMessage) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO audio_messages (id, name, category, file_name, duration, file_size, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		m.ID, m.Name, m.Category, m.FileName, m.Duration, m.FileSize, m.CreatedAt,
	)
	return err
}

// ListAudioMessages returns all audio messages ordered by category then name
func (db *DB) ListAudioMessages(ctx context.Context) ([]AudioMessage, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, name, category, file_name, duration, file_size, created_at
		FROM audio_messages ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []AudioMessage
	for rows.Next() {
		var m AudioMessage
		if err := rows.Scan(&m.ID, &m.Name, &m.Category, &m.FileName, &m.Duration, &m.FileSize, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// GetAudioMessage retrieves a single audio message by ID
func (db *DB) GetAudioMessage(ctx context.Context, id uuid.UUID) (*AudioMessage, error) {
	m := &AudioMessage{}
	err := db.Pool.QueryRow(ctx, `
		SELECT id, name, category, file_name, duration, file_size, created_at
		FROM audio_messages WHERE id = $1`, id,
	).Scan(&m.ID, &m.Name, &m.Category, &m.FileName, &m.Duration, &m.FileSize, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteAudioMessage removes an audio message by ID
func (db *DB) DeleteAudioMessage(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM audio_messages WHERE id = $1`, id)
	return err
}

// ──────────────────── Audit Log ────────────────────

// InsertAuditEntry records a user action in the audit log
func (db *DB) InsertAuditEntry(ctx context.Context, entry *AuditEntry) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO audit_log (user_id, username, action, target_type, target_id, details, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.UserID, entry.Username, entry.Action, entry.TargetType, entry.TargetID, entry.Details, entry.IPAddress,
	)
	return err
}

// QueryAuditLog returns paginated audit entries with optional filters
func (db *DB) QueryAuditLog(ctx context.Context, username, action, targetType string, limit, offset int) ([]AuditEntry, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Build WHERE clause
	conditions := []string{}
	args := []interface{}{}
	argIdx := 1

	if username != "" {
		conditions = append(conditions, fmt.Sprintf("username ILIKE $%d", argIdx))
		args = append(args, "%"+username+"%")
		argIdx++
	}
	if action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, action)
		argIdx++
	}
	if targetType != "" {
		conditions = append(conditions, fmt.Sprintf("target_type = $%d", argIdx))
		args = append(args, targetType)
		argIdx++
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total
	var total int
	err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_log"+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Query with pagination
	query := fmt.Sprintf("SELECT id, user_id, username, action, target_type, target_id, COALESCE(details,''), COALESCE(ip_address,''), created_at FROM audit_log%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.TargetType, &e.TargetID, &e.Details, &e.IPAddress, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		entries = append(entries, e)
	}
	return entries, total, nil
}

// ──────────────────── Bookmarks ────────────────────

// CreateBookmark inserts a new bookmark
func (db *DB) CreateBookmark(ctx context.Context, b *Bookmark) error {
	return db.Pool.QueryRow(ctx, `
		INSERT INTO bookmarks (camera_id, event_time, label, notes, severity, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
		b.CameraID, b.EventTime, b.Label, b.Notes, b.Severity, b.CreatedBy,
	).Scan(&b.ID, &b.CreatedAt)
}

// ListBookmarks returns bookmarks for a camera within a time range
func (db *DB) ListBookmarks(ctx context.Context, cameraID *uuid.UUID, start, end time.Time) ([]Bookmark, error) {
	query := `SELECT b.id, b.camera_id, b.event_time, b.label, COALESCE(b.notes,''), b.severity,
	                  b.created_by, COALESCE(u.username,''), b.created_at
	           FROM bookmarks b LEFT JOIN users u ON b.created_by = u.id
	           WHERE b.event_time BETWEEN $1 AND $2`
	args := []interface{}{start, end}

	if cameraID != nil {
		query += " AND b.camera_id = $3"
		args = append(args, *cameraID)
	}
	query += " ORDER BY b.event_time DESC"

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookmarks []Bookmark
	for rows.Next() {
		var b Bookmark
		if err := rows.Scan(&b.ID, &b.CameraID, &b.EventTime, &b.Label, &b.Notes, &b.Severity, &b.CreatedBy, &b.Username, &b.CreatedAt); err != nil {
			return nil, err
		}
		bookmarks = append(bookmarks, b)
	}
	return bookmarks, nil
}

// DeleteBookmark removes a bookmark by ID
func (db *DB) DeleteBookmark(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM bookmarks WHERE id = $1`, id)
	return err
}
