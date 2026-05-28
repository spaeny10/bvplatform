-- migrations/0027_vlm_label_jobs_camera_fk.sql
-- P3-INFRA-04 (Interpretation B): Fix vlm_label_jobs.camera_id type + FK constraint.
--
-- Background
-- ----------
-- vlm_label_jobs.camera_id was declared TEXT NOT NULL DEFAULT '' in the baseline,
-- but the column semantically references cameras.id which is UUID.  The scope plan
-- (P3-INFRA-04) confirmed on 2026-05-27 that all 5 existing rows on fred hold
-- valid UUID-shaped strings ("862bb7e9-a2fe-4ae3-9d48-588f7bee948d", etc.), so
-- the USING cast is safe.
--
-- However, those 5 camera UUIDs no longer exist in cameras (pre-launch test rows
-- whose cameras were deleted).  Because the FK is declared ON DELETE SET NULL, we
-- must:
--   1. Drop the NOT NULL constraint and the ''::text DEFAULT.
--   2. NULL out any existing rows whose camera_id has no matching cameras.id.
--   3. Convert the column type TEXT → UUID.
--   4. Add the FK constraint.
--
-- Semantic note: camera_id is now NULLABLE.  That is correct — ON DELETE SET NULL
-- means a camera deletion leaves the label-job row alive but with camera_id=NULL.
-- The annotator UI already renders these gracefully (it checks snapshot_url, not
-- camera identity).  New rows from EnqueueLabelJob will always supply a non-NULL
-- camera UUID; the nullable column only exists for orphan tolerance.
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

-- Step 1: Drop the NOT NULL constraint and the ''::text default.
-- The column needs to be nullable so that ON DELETE SET NULL can work when a
-- camera is deleted.  The empty-string default must be dropped before the type
-- change because Postgres cannot cast '' to uuid.
ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id DROP NOT NULL;

ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id DROP DEFAULT;

-- Step 2: NULL out orphaned rows (camera deleted before this migration ran).
-- Any camera_id that is non-empty and does not match a cameras.id row is an
-- orphan.  We set it to NULL now so the FK constraint can be added cleanly.
-- On fred at 2026-05-27 all 5 pre-launch test rows are in this category.
UPDATE vlm_label_jobs
SET camera_id = NULL
WHERE camera_id <> ''
  AND camera_id NOT IN (SELECT id::text FROM cameras);

-- Also NULL out the empty-string sentinel rows (inserted via the old DEFAULT '').
UPDATE vlm_label_jobs
SET camera_id = NULL
WHERE camera_id = '';

-- Step 3: Convert the column type TEXT → UUID.
-- Any non-NULL, non-empty value that survived Step 2 is a valid UUID-shaped
-- string (confirmed by pre-flight query); the USING cast is safe.
-- If an unexpected non-UUID value slipped through, Postgres will fail loudly
-- here with "invalid input syntax for type uuid" — that is intentional.
ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id TYPE uuid USING camera_id::uuid;

-- Step 4: Add the FK constraint.
-- ON DELETE SET NULL: a deleted camera sets camera_id to NULL rather than
-- cascading a delete of the label job (preserving annotation history).
-- Wrapped in a DO block for idempotency in case of replay.
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

-- Drop FK first, then revert the column type back to text.
ALTER TABLE vlm_label_jobs
    DROP CONSTRAINT IF EXISTS fk_vlm_label_jobs_camera;

-- Revert UUID → TEXT.  Non-NULL UUIDs round-trip cleanly; NULLs stay NULL.
ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id TYPE text USING camera_id::text;

-- Restore the original NOT NULL + DEFAULT '' that the baseline had.
ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id SET DEFAULT '';

UPDATE vlm_label_jobs SET camera_id = '' WHERE camera_id IS NULL;

ALTER TABLE vlm_label_jobs
    ALTER COLUMN camera_id SET NOT NULL;

-- +goose StatementEnd
