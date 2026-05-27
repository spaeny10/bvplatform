package database

// vlm_queue.go — P2-C-03 VLM validation queue helpers.
//
// These functions service the async VLM worker (internal/safety/vlm_worker.go).
// The worker is queue-wide (no org filter on reads) because VLM validation is
// tenant-agnostic image analysis. Tenant data is never exposed to the worker:
// VLMQueueRow contains only the fields needed for Qwen inference + retry bookkeeping.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/ai"
)

// VLMQueueRow is the minimal row shape the VLM worker needs from
// pending_review_queue. No organization PII is included — the worker
// only reads frame evidence and detection metadata.
type VLMQueueRow struct {
	ID             uuid.UUID
	FramePath      string
	DetectionClass string
	MissingLabel   string
	Confidence     float64
	BoundingBoxes  []ai.Detection // deserialized from JSONB
	Attempts       int
	CreatedAt      time.Time
}

// ListPendingVLM returns up to limit rows from pending_review_queue that
// require VLM validation:
//   - status = 'pending'
//   - vlm_verdict IN ('pending', 'error')
//   - vlm_attempts < maxAttempts
//
// Ordered by created_at ASC (oldest first) to prioritize the longest-waiting
// candidates. No organization_id filter — the VLM worker is queue-wide.
func (db *DB) ListPendingVLM(ctx context.Context, limit, maxAttempts int) ([]VLMQueueRow, error) {
	if limit <= 0 {
		limit = 5
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT
		    id, frame_path, detection_class, missing_label,
		    confidence, bounding_boxes, vlm_attempts, created_at
		FROM pending_review_queue
		WHERE status = 'pending'
		  AND vlm_verdict IN ('pending', 'error')
		  AND vlm_attempts < $1
		ORDER BY created_at ASC
		LIMIT $2`,
		maxAttempts, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListPendingVLM: %w", err)
	}
	defer rows.Close()

	var out []VLMQueueRow
	for rows.Next() {
		var r VLMQueueRow
		var bbRaw []byte
		if err := rows.Scan(
			&r.ID, &r.FramePath, &r.DetectionClass, &r.MissingLabel,
			&r.Confidence, &bbRaw, &r.Attempts, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListPendingVLM scan: %w", err)
		}
		// Deserialize bounding_boxes JSONB into []ai.Detection.
		if len(bbRaw) > 0 {
			_ = json.Unmarshal(bbRaw, &r.BoundingBoxes)
		}
		out = append(out, r)
	}
	return out, nil
}

// UpdateVLMVerdict atomically writes the verdict from one VLM inference pass
// to the row identified by id. No organization filter — id is the UUID PK.
//
// Fields updated:
//   - vlm_verdict, vlm_reasoning, vlm_checked_at, vlm_model
//   - vlm_attempts (incremented by the caller — pass row.Attempts+1)
//   - updated_at
func (db *DB) UpdateVLMVerdict(
	ctx context.Context,
	id uuid.UUID,
	verdict, reasoning, model string,
	attempts int,
) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE pending_review_queue
		SET vlm_verdict    = $1,
		    vlm_reasoning  = $2,
		    vlm_checked_at = NOW(),
		    vlm_model      = $3,
		    vlm_attempts   = $4,
		    updated_at     = NOW()
		WHERE id = $5`,
		verdict, reasoning, model, attempts, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateVLMVerdict: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("UpdateVLMVerdict: row %s not found", id)
	}
	return nil
}

// ExpireStalePendingVLM sets vlm_verdict='uncertain' for rows that have been
// sitting in vlm_verdict='pending' for longer than maxAgeHours. This prevents
// queue build-up during a persistent Qwen outage: old unvalidated candidates
// are promoted to uncertain so a human can still review them instead of them
// remaining frozen at 'pending' indefinitely.
//
// Only rows with status='pending' AND vlm_verdict='pending' are touched —
// 'error' rows have already had at least one attempt and keep their retry
// lifecycle.
func (db *DB) ExpireStalePendingVLM(ctx context.Context, maxAgeHours int) error {
	if maxAgeHours <= 0 {
		maxAgeHours = 24
	}
	_, err := db.Pool.Exec(ctx, `
		UPDATE pending_review_queue
		SET vlm_verdict = 'uncertain',
		    vlm_reasoning = 'aged out: no VLM response within age limit',
		    vlm_checked_at = NOW(),
		    updated_at = NOW()
		WHERE status = 'pending'
		  AND vlm_verdict = 'pending'
		  AND created_at < NOW() - make_interval(hours => $1)`,
		maxAgeHours,
	)
	if err != nil {
		return fmt.Errorf("ExpireStalePendingVLM: %w", err)
	}
	return nil
}
