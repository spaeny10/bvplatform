-- +goose Up
-- P2-C-01: pending_review_queue — holds PPE violation frames awaiting
-- human review. Each row is one detected violation (one missing PPE item)
-- from one camera frame.
--
-- Lifecycle:
--   pending             → just inserted by PPE worker
--   reviewed_compliant  → reviewer confirmed the worker's detection was
--                         a false positive (person was actually compliant)
--   reviewed_violation  → reviewer confirmed the violation is real
--   dismissed           → reviewer marked as irrelevant (training scenario
--                         staging, test, etc.)
--
-- VLM columns (vlm_*) are nullable stubs for Phase P2-C-03 automated
-- pre-triage — left blank until that feature lands.
--
-- NOTE on FK types: organizations.id and sites.id are TEXT primary keys
-- (pre-dating the uuid migration). cameras.id and users.id are UUID.

CREATE TABLE IF NOT EXISTS pending_review_queue (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     TEXT        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    camera_id           UUID        NOT NULL REFERENCES cameras(id)       ON DELETE CASCADE,
    site_id             TEXT                 REFERENCES sites(id)         ON DELETE SET NULL,

    -- Frame location on disk and a short-lived bearer token so the
    -- frontend can fetch the JPEG without re-authing. frame_path is
    -- relative to PPEFramesDir (e.g. "<org_id>/2026-05-26/1748900000000.jpg").
    frame_path          TEXT        NOT NULL,
    frame_token         TEXT        NOT NULL DEFAULT '',
    frame_token_expires TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- What the YOLO model detected.
    detection_class     TEXT        NOT NULL,
    missing_label       TEXT        NOT NULL,
    confidence          REAL        NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    bounding_boxes      JSONB       NOT NULL DEFAULT '[]',
    yolo_model          TEXT        NOT NULL DEFAULT '',

    -- Review outcome.
    status              TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'reviewed_compliant',
                                              'reviewed_violation', 'dismissed')),
    reviewed_by         UUID                 REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at         TIMESTAMPTZ,
    notes               TEXT,

    -- Phase P2-C-03 VLM pre-triage (nullable stubs).
    vlm_validated       BOOLEAN,
    vlm_validated_at    TIMESTAMPTZ,
    vlm_confidence      REAL        CHECK (vlm_confidence IS NULL OR
                                          (vlm_confidence >= 0 AND vlm_confidence <= 1)),
    vlm_notes           TEXT,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Primary lookup: org-scoped list by status, newest first.
CREATE INDEX IF NOT EXISTS idx_prq_org_status_created
    ON pending_review_queue (organization_id, status, created_at DESC);

-- Worker dedup / camera activity timeline.
CREATE INDEX IF NOT EXISTS idx_prq_camera_created
    ON pending_review_queue (camera_id, created_at DESC);

-- VLM worker: find pending rows with no VLM pass yet (P2-C-03).
CREATE INDEX IF NOT EXISTS idx_prq_vlm_null_created
    ON pending_review_queue (created_at DESC)
    WHERE status = 'pending' AND vlm_validated IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_prq_vlm_null_created;
DROP INDEX IF EXISTS idx_prq_camera_created;
DROP INDEX IF EXISTS idx_prq_org_status_created;
DROP TABLE IF EXISTS pending_review_queue;
