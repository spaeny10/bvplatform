-- migrations/0025_prq_vlm_verdict.sql
-- P2-C-03: Expand the four VLM stub columns from C-01 (migration 0022)
-- into the richer verdict model needed for the async VLM validation worker.
--
-- Changes applied:
--   vlm_validated BOOLEAN  → vlm_verdict TEXT (named enum + constraint)
--   vlm_notes              → vlm_reasoning
--   vlm_validated_at       → vlm_checked_at
--   vlm_confidence REAL    — kept as-is (mapped from false_positive_likelihood)
--   vlm_model TEXT         — new column (tracks which Qwen version produced the verdict)
--   vlm_attempts INT       — new column (retry cap guard)
--   Index replaced: idx_prq_vlm_null_created → idx_prq_vlm_pending
--
-- NOTE: fred is pre-launch; all existing rows have vlm_validated IS NULL
-- (never had VLM data), so the data migration below is a best-effort
-- mapping that is correct for the actual data state.
--
-- The Down migration is lossy for 'uncertain' and 'error' verdict rows:
-- those become NULL in vlm_validated, which is the same state they were
-- in before C-01 wrote the stubs. Acceptable — pre-launch, no customer data.

-- +goose Up
-- +goose StatementBegin

-- Step 1: Add new columns alongside the old stubs.
-- ADD COLUMN IF NOT EXISTS is idempotent (safe to replay).
ALTER TABLE pending_review_queue
    ADD COLUMN IF NOT EXISTS vlm_verdict     TEXT    DEFAULT 'pending'
        CHECK (vlm_verdict IN ('pending','confirmed','dismissed','uncertain','error')),
    ADD COLUMN IF NOT EXISTS vlm_reasoning   TEXT,
    ADD COLUMN IF NOT EXISTS vlm_checked_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS vlm_model       TEXT,
    ADD COLUMN IF NOT EXISTS vlm_attempts    INT     NOT NULL DEFAULT 0;

-- Step 2: Migrate existing data from the old stubs into the new columns.
-- Rows inserted by C-01 with vlm_validated IS NULL (the common case on fred)
-- become 'pending'. vlm_validated = TRUE → 'confirmed', FALSE → 'dismissed'.
UPDATE pending_review_queue
    SET vlm_verdict    = CASE
                            WHEN vlm_validated IS TRUE  THEN 'confirmed'
                            WHEN vlm_validated IS FALSE THEN 'dismissed'
                            ELSE 'pending'
                         END,
        vlm_reasoning  = vlm_notes,
        vlm_checked_at = vlm_validated_at
    WHERE vlm_verdict IS NULL;

-- Step 3: Drop the old stub columns.
-- DROP COLUMN IF EXISTS is idempotent.
ALTER TABLE pending_review_queue
    DROP COLUMN IF EXISTS vlm_validated,
    DROP COLUMN IF EXISTS vlm_notes,
    DROP COLUMN IF EXISTS vlm_validated_at;

-- Step 4: Replace the old partial index (filtered on vlm_validated IS NULL)
-- with the new one (filtered on vlm_verdict IN ('pending','error')).
DROP INDEX IF EXISTS idx_prq_vlm_null_created;

-- VLM worker poll query: status=pending AND vlm_verdict not yet resolved
-- AND below the retry cap. ORDER BY created_at ASC keeps oldest-first.
CREATE INDEX IF NOT EXISTS idx_prq_vlm_pending
    ON pending_review_queue (created_at ASC)
    WHERE status = 'pending' AND vlm_verdict IN ('pending', 'error');

-- Audit / dismissed-review query: find VLM-dismissed rows by org.
CREATE INDEX IF NOT EXISTS idx_prq_vlm_dismissed
    ON pending_review_queue (organization_id, created_at DESC)
    WHERE vlm_verdict = 'dismissed';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_prq_vlm_dismissed;
DROP INDEX IF EXISTS idx_prq_vlm_pending;

-- Restore old stub columns.
ALTER TABLE pending_review_queue
    ADD COLUMN IF NOT EXISTS vlm_validated    BOOLEAN,
    ADD COLUMN IF NOT EXISTS vlm_notes        TEXT,
    ADD COLUMN IF NOT EXISTS vlm_validated_at TIMESTAMPTZ;

-- Reverse-migrate data (best-effort; 'uncertain'/'error'/'pending' → NULL).
UPDATE pending_review_queue
    SET vlm_validated    = CASE vlm_verdict
                              WHEN 'confirmed' THEN TRUE
                              WHEN 'dismissed' THEN FALSE
                              ELSE NULL
                           END,
        vlm_notes        = vlm_reasoning,
        vlm_validated_at = vlm_checked_at;

-- Drop new columns.
ALTER TABLE pending_review_queue
    DROP COLUMN IF EXISTS vlm_verdict,
    DROP COLUMN IF EXISTS vlm_reasoning,
    DROP COLUMN IF EXISTS vlm_checked_at,
    DROP COLUMN IF EXISTS vlm_model,
    DROP COLUMN IF EXISTS vlm_attempts;

-- Restore old index.
CREATE INDEX IF NOT EXISTS idx_prq_vlm_null_created
    ON pending_review_queue (created_at DESC)
    WHERE status = 'pending' AND vlm_validated IS NULL;

-- +goose StatementEnd
