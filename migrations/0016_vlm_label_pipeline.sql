-- +goose Up
-- +goose StatementBegin
--
-- Subset 12 of the P1-B-02 extraction. Source: lines 768–807 of the
-- inline block in cmd/server/main.go.
--
-- VLM active-learning labeling pipeline. Two tables:
--
-- 1) vlm_label_jobs: every time Qwen analyzes an alarm frame, a row
--    lands here so internal annotators can review the VLM output and
--    submit ground-truth labels for fine-tuning. status lifecycle:
--      pending → claimed (annotator opens the job)
--      claimed → labeled | skipped
--    Operators never see this table — it's drained off-hours via
--    /admin/labeling by internal staff only.
--
-- 2) vlm_labels: the actual ground-truth submissions. verdict enum:
--      correct          — VLM nailed it
--      incorrect        — VLM got it wrong
--      needs_correction — partially right, see corrected_description
--    Tags are free-form seed strings ('false_positive', 'ppe_violation',
--    'person_with_weapon', etc.) for filtering the training dataset.

CREATE TABLE IF NOT EXISTS vlm_label_jobs (
    id              BIGSERIAL    PRIMARY KEY,
    alarm_id        TEXT         NOT NULL,
    camera_id       TEXT         NOT NULL DEFAULT '',
    site_id         TEXT         NOT NULL DEFAULT '',
    snapshot_url    TEXT         NOT NULL DEFAULT '',
    vlm_description TEXT         NOT NULL DEFAULT '',
    vlm_threat      TEXT         NOT NULL DEFAULT '',
    vlm_model       TEXT         NOT NULL DEFAULT '',
    yolo_detections JSONB        NOT NULL DEFAULT '[]',
    status          TEXT         NOT NULL DEFAULT 'pending',
    claimed_by      UUID,
    claimed_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vlm_label_jobs_status
    ON vlm_label_jobs(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vlm_label_jobs_alarm
    ON vlm_label_jobs(alarm_id);

CREATE TABLE IF NOT EXISTS vlm_labels (
    id                     BIGSERIAL    PRIMARY KEY,
    job_id                 BIGINT       NOT NULL REFERENCES vlm_label_jobs(id) ON DELETE CASCADE,
    annotator_id           UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    verdict                TEXT         NOT NULL,
    corrected_description  TEXT         NOT NULL DEFAULT '',
    corrected_threat       TEXT         NOT NULL DEFAULT '',
    tags                   TEXT[]       NOT NULL DEFAULT '{}',
    notes                  TEXT         NOT NULL DEFAULT '',
    labeled_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vlm_labels_job
    ON vlm_labels(job_id);
CREATE INDEX IF NOT EXISTS idx_vlm_labels_annotator
    ON vlm_labels(annotator_id, labeled_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_vlm_labels_annotator;
DROP INDEX IF EXISTS idx_vlm_labels_job;
DROP TABLE IF EXISTS vlm_labels;
DROP INDEX IF EXISTS idx_vlm_label_jobs_alarm;
DROP INDEX IF EXISTS idx_vlm_label_jobs_status;
DROP TABLE IF EXISTS vlm_label_jobs;
-- +goose StatementEnd
