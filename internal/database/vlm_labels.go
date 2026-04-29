package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// VLMLabelJob is one entry in the active-learning labeling queue.
// Created passively whenever Qwen produces a description for an alarm frame;
// drained by internal annotators via /admin/labeling (never by SOC operators).
type VLMLabelJob struct {
	ID             int64     `json:"id"`
	AlarmID        string    `json:"alarm_id"`
	CameraID       string    `json:"camera_id"`
	SiteID         string    `json:"site_id"`
	SnapshotURL    string    `json:"snapshot_url"`
	VLMDescription string    `json:"vlm_description"`
	VLMThreat      string    `json:"vlm_threat"`
	VLMModel       string    `json:"vlm_model"`
	YOLODetections []byte    `json:"yolo_detections"`
	Status         string    `json:"status"` // pending | claimed | labeled | skipped
	ClaimedBy      *uuid.UUID `json:"claimed_by,omitempty"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// VLMLabel is a ground-truth label submitted by an internal annotator.
type VLMLabel struct {
	ID                   int64     `json:"id"`
	JobID                int64     `json:"job_id"`
	AnnotatorID          uuid.UUID `json:"annotator_id"`
	Verdict              string    `json:"verdict"` // correct | incorrect | needs_correction
	CorrectedDescription string    `json:"corrected_description"`
	CorrectedThreat      string    `json:"corrected_threat"`
	Tags                 []string  `json:"tags"`
	Notes                string    `json:"notes"`
	LabeledAt            time.Time `json:"labeled_at"`
}

// LabelingStats is a summary of the labeling queue health.
type LabelingStats struct {
	Pending  int `json:"pending"`
	Claimed  int `json:"claimed"`
	Labeled  int `json:"labeled"`
	Skipped  int `json:"skipped"`
	Total    int `json:"total"`
	// label breakdown
	Correct          int `json:"correct"`
	Incorrect        int `json:"incorrect"`
	NeedsCorrection  int `json:"needs_correction"`
}

// EnqueueLabelJob inserts a new pending job into the labeling queue.
// Called by the alarm pipeline after a successful Qwen inference.
// Idempotent on alarm_id: if a job already exists for this alarm we skip.
func (db *DB) EnqueueLabelJob(ctx context.Context, alarmID, cameraID, siteID, snapshotURL, description, threat, model string, detectionsJSON []byte) error {
	if detectionsJSON == nil {
		detectionsJSON = []byte("[]")
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO vlm_label_jobs
		  (alarm_id, camera_id, site_id, snapshot_url, vlm_description, vlm_threat, vlm_model, yolo_detections, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending')
		ON CONFLICT DO NOTHING`,
		alarmID, cameraID, siteID, snapshotURL, description, threat, model, detectionsJSON,
	)
	return err
}

// ClaimLabelJob atomically flips a pending job to claimed by the given annotator.
// Returns the full job record so the UI can display the frame immediately.
// Returns sql.ErrNoRows (pgx: pgx.ErrNoRows) if the job is not pending or does not exist.
func (db *DB) ClaimLabelJob(ctx context.Context, jobID int64, annotatorID uuid.UUID) (*VLMLabelJob, error) {
	row := db.Pool.QueryRow(ctx, `
		UPDATE vlm_label_jobs
		SET status='claimed', claimed_by=$2, claimed_at=NOW()
		WHERE id=$1 AND status='pending'
		RETURNING id, alarm_id, camera_id, site_id, snapshot_url,
		          vlm_description, vlm_threat, vlm_model, yolo_detections,
		          status, claimed_by, claimed_at, created_at`,
		jobID, annotatorID,
	)
	return scanLabelJob(row)
}

// SubmitLabel records the annotator's verdict and flips the job to labeled.
// If verdict == "skipped" the job moves to skipped without a label row.
func (db *DB) SubmitLabel(ctx context.Context, jobID int64, annotatorID uuid.UUID, verdict, correctedDesc, correctedThreat string, tags []string, notes string) error {
	if verdict == "skipped" {
		_, err := db.Pool.Exec(ctx,
			`UPDATE vlm_label_jobs SET status='skipped' WHERE id=$1 AND claimed_by=$2`,
			jobID, annotatorID)
		return err
	}

	allowed := map[string]bool{"correct": true, "incorrect": true, "needs_correction": true}
	if !allowed[verdict] {
		return fmt.Errorf("invalid verdict %q", verdict)
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO vlm_labels
		  (job_id, annotator_id, verdict, corrected_description, corrected_threat, tags, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		jobID, annotatorID, verdict, correctedDesc, correctedThreat, tags, notes,
	)
	if err != nil {
		return fmt.Errorf("insert label: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE vlm_label_jobs SET status='labeled' WHERE id=$1 AND claimed_by=$2`,
		jobID, annotatorID)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}

	return tx.Commit(ctx)
}

// ListLabelJobs returns jobs filtered by status. Pass "" for all statuses.
// Results are ordered newest-first with limit capped at 200.
func (db *DB) ListLabelJobs(ctx context.Context, status string, limit int) ([]VLMLabelJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	q := `SELECT id, alarm_id, camera_id, site_id, snapshot_url,
	             vlm_description, vlm_threat, vlm_model, yolo_detections,
	             status, claimed_by, claimed_at, created_at
	      FROM vlm_label_jobs`
	args := []interface{}{}
	if status != "" {
		q += " WHERE status=$1"
		args = append(args, status)
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VLMLabelJob
	for rows.Next() {
		j, err := scanLabelJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// GetLabelingStats returns counts by job status and label verdict.
func (db *DB) GetLabelingStats(ctx context.Context) (*LabelingStats, error) {
	var s LabelingStats
	err := db.Pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status='pending')  AS pending,
		  COUNT(*) FILTER (WHERE status='claimed')  AS claimed,
		  COUNT(*) FILTER (WHERE status='labeled')  AS labeled,
		  COUNT(*) FILTER (WHERE status='skipped')  AS skipped,
		  COUNT(*)                                   AS total
		FROM vlm_label_jobs`).
		Scan(&s.Pending, &s.Claimed, &s.Labeled, &s.Skipped, &s.Total)
	if err != nil {
		return nil, err
	}
	_ = db.Pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE verdict='correct')          AS correct,
		  COUNT(*) FILTER (WHERE verdict='incorrect')        AS incorrect,
		  COUNT(*) FILTER (WHERE verdict='needs_correction') AS needs_correction
		FROM vlm_labels`).
		Scan(&s.Correct, &s.Incorrect, &s.NeedsCorrection)
	return &s, nil
}

// ExportLabeledDataset returns labeled jobs as HuggingFace-compatible JSONL.
// Each line is a JSON object with "messages" in the chat-ml format:
//
//	{"messages":[{"role":"user","content":"<image>..."},{"role":"assistant","content":"<description>"}]}
//
// verdict_filter: "all" | "correct" | "incorrect" | "needs_correction"
func (db *DB) ExportLabeledDataset(ctx context.Context, verdictFilter string) ([]byte, error) {
	q := `
		SELECT j.snapshot_url, j.vlm_description, j.vlm_threat, j.yolo_detections,
		       l.verdict, l.corrected_description, l.corrected_threat, l.tags
		FROM vlm_labels l
		JOIN vlm_label_jobs j ON j.id = l.job_id
		WHERE j.status = 'labeled'`

	args := []interface{}{}
	if verdictFilter != "" && verdictFilter != "all" {
		q += " AND l.verdict=$1"
		args = append(args, verdictFilter)
	}
	q += " ORDER BY l.labeled_at DESC"

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var (
			snapshotURL, vlmDesc, vlmThreat string
			yoloBuf                         []byte
			verdict, corrDesc, corrThreat   string
			tags                            []string
		)
		if err := rows.Scan(&snapshotURL, &vlmDesc, &vlmThreat, &yoloBuf,
			&verdict, &corrDesc, &corrThreat, &tags); err != nil {
			return nil, err
		}

		// Use corrected values when available, fall back to original VLM output
		finalDesc := vlmDesc
		if corrDesc != "" {
			finalDesc = corrDesc
		}
		finalThreat := vlmThreat
		if corrThreat != "" {
			finalThreat = corrThreat
		}

		tagStr := ""
		if len(tags) > 0 {
			tagStr = " Tags: " + strings.Join(tags, ", ") + "."
		}

		systemPrompt := "You are a security camera analyst. Describe what you observe in the image, assess the threat level, and recommend an action."
		userContent := fmt.Sprintf("<image>\nAnalyze this security camera frame. Site context: surveillance footage.%s", tagStr)
		assistantContent := fmt.Sprintf(`{"description":"%s","threat_level":"%s","recommended_action":"Monitor and document."}`,
			strings.ReplaceAll(finalDesc, `"`, `\"`),
			finalThreat)

		entry := map[string]interface{}{
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userContent},
				{"role": "assistant", "content": assistantContent},
			},
			"metadata": map[string]interface{}{
				"snapshot_url": snapshotURL,
				"verdict":      verdict,
				"yolo":         json.RawMessage(yoloBuf),
			},
		}
		b, _ := json.Marshal(entry)
		lines = append(lines, string(b))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// NextPendingLabelJob fetches the oldest pending job and claims it atomically.
// Used by the "Save & next" flow so the UI can pre-fetch the next frame.
func (db *DB) NextPendingLabelJob(ctx context.Context, annotatorID uuid.UUID) (*VLMLabelJob, error) {
	row := db.Pool.QueryRow(ctx, `
		UPDATE vlm_label_jobs
		SET status='claimed', claimed_by=$1, claimed_at=NOW()
		WHERE id = (
		  SELECT id FROM vlm_label_jobs
		  WHERE status='pending'
		  ORDER BY created_at ASC
		  LIMIT 1
		  FOR UPDATE SKIP LOCKED
		)
		RETURNING id, alarm_id, camera_id, site_id, snapshot_url,
		          vlm_description, vlm_threat, vlm_model, yolo_detections,
		          status, claimed_by, claimed_at, created_at`,
		annotatorID,
	)
	return scanLabelJob(row)
}

// ── helpers ──────────────────────────────────────────────────────────────────

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanLabelJob(row scannable) (*VLMLabelJob, error) {
	var j VLMLabelJob
	var yoloBuf []byte
	if err := row.Scan(
		&j.ID, &j.AlarmID, &j.CameraID, &j.SiteID, &j.SnapshotURL,
		&j.VLMDescription, &j.VLMThreat, &j.VLMModel, &yoloBuf,
		&j.Status, &j.ClaimedBy, &j.ClaimedAt, &j.CreatedAt,
	); err != nil {
		return nil, err
	}
	j.YOLODetections = yoloBuf
	return &j, nil
}

func scanLabelJobRow(rows interface{ Scan(...interface{}) error }) (*VLMLabelJob, error) {
	return scanLabelJob(rows)
}
