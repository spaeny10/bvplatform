-- migrations/0027_vlm_label_jobs_camera_fk.sql
-- P3-INFRA-04 (Interpretation B): Fix vlm_label_jobs.camera_id type + FK constraint.
--
-- Background
-- ----------
-- vlm_label_jobs.camera_id was declared TEXT in the baseline, but the column
-- semantically references cameras.id which is UUID.  The scope plan (P3-INFRA-04)
-- confirmed on 2026-05-27 that all 5 existing rows on fred hold valid UUID-shaped
-- strings ("862bb7e9-a2fe-4ae3-9d48-588f7bee948d", etc.), so a USING cast is safe.
--
-- This migration:
--   1. Converts vlm_label_jobs.camera_id from TEXT to UUID via USING camera_id::uuid.
--   2. Adds a FK constraint to cameras(id) with ON DELETE SET NULL — matching the
--      pattern used by pending_review_queue.camera_id (the other camera-referencing
--      table added post-baseline).  SET NULL is preferred over CASCADE for a labeling
--      queue: if a camera is deleted we lose the FK reference but keep the annotation
--      history; the annotator UI already handles NULL gracefully.
--
-- Pre-flight check (run before this migration on any target DB):
--   SELECT camera_id, count(*) FROM vlm_label_jobs GROUP BY camera_id LIMIT 20;
-- All values must be UUID-shaped strings.  If any value is NOT a valid UUID the
-- ALTER COLUMN ... USING cast will fail loudly here — that is the correct behavior.
--
-- Convention alignment (P3-INFRA-04 Interpretation B)
-- ---------------------------------------------------
-- After this migration:
--   - All FK columns that reference cameras.id (UUID PK) are of type UUID.
--   - The TEXT-PK tables (organizations, sites, incidents, active_alarms,
--     security_events, site_sops, company_users, operators) are grandfathered
--     and NOT changed.  Their FK columns throughout the schema are TEXT and
--     remain TEXT.
-- See docs/id-conventions.md for the full policy.

-- +goose Up
-- +goose StatementBegin

-- Step 1: Convert the column type.
-- If any existing row contains a non-UUID string this will fail with:
--   "invalid input syntax for type uuid: ..."
-- That is intentional — surfacing bad data is better than silent truncation.
ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id TYPE uuid USING camera_id::uuid;

-- Step 2: Add the FK constraint, idempotent via DO block.
-- ON DELETE SET NULL: a deleted camera sets camera_id to NULL rather than
-- cascading a delete of the label job (preserving annotation history).
-- NOT VALID is NOT used here because the table is small (pre-launch) and
-- an immediate full-table validate is cheap; NOT VALID would add operational
-- complexity without benefit.
DO $idem$
BEGIN
    ALTER TABLE vlm_label_jobs
        ADD CONSTRAINT fk_vlm_label_jobs_camera
        FOREIGN KEY (camera_id) REFERENCES cameras(id)
        ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN
    NULL; -- constraint already exists; idempotent
END
$idem$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop FK first, then revert the column type.
ALTER TABLE vlm_label_jobs
    DROP CONSTRAINT IF EXISTS fk_vlm_label_jobs_camera;

-- Revert UUID → TEXT.  All UUID values round-trip cleanly through ::text.
ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id TYPE text USING camera_id::text;

-- +goose StatementEnd
