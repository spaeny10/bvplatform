-- +goose Up
-- +goose StatementBegin
--
-- Subset 4 of the P1-B-02 extraction. Source: lines 261–307 of the
-- inline block in cmd/server/main.go.
--
-- 1) events.segment_id: forensic link from an event row to the video
--    segment that contains the event moment. BIGINT with NO FK (see the
--    inline comment in main.go — Timescale doesn't allow FKs targeting
--    a hypertable PK because it isn't enforced across chunks). Backfill
--    populates the column for events that existed before the column was
--    added; idempotent (only touches NULL rows).
--
-- 2) segment_descriptions: VLM indexer output. One row per indexed
--    segment, written by the background indexer in internal/indexer.
--    Tag list + JSONB entities/detections enable Postgres full-text +
--    GIN-array search. Retention job in internal/retention deletes rows
--    when their referenced segment is purged (no FK for the same
--    hypertable reason).

-- Events → segment linkage
ALTER TABLE events ADD COLUMN IF NOT EXISTS segment_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_events_segment ON events(segment_id);

-- One-time backfill: for each NULL segment_id, find a covering segment
-- on the same camera whose time range contains the event timestamp.
-- Stays NULL if no segment matches (recording was down during the event).
-- Re-running this migration is a no-op for the same reason.
UPDATE events e
SET segment_id = (
    SELECT s.id FROM segments s
    WHERE s.camera_id = e.camera_id
      AND s.start_time <= e.event_time
      AND s.end_time   >= e.event_time
    ORDER BY s.start_time DESC
    LIMIT 1
)
WHERE e.segment_id IS NULL;

-- VLM segment descriptions
CREATE TABLE IF NOT EXISTS segment_descriptions (
    segment_id       BIGINT       PRIMARY KEY,
    camera_id        UUID         NOT NULL,
    description      TEXT         NOT NULL DEFAULT '',
    tags             TEXT[]       NOT NULL DEFAULT '{}',
    activity_level   TEXT         NOT NULL DEFAULT 'none',
    entities         JSONB        NOT NULL DEFAULT '[]',
    detections       JSONB        NOT NULL DEFAULT '[]',
    indexer_version  INT          NOT NULL DEFAULT 1,
    analysis_ms      INT          NOT NULL DEFAULT 0,
    indexed_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_segment_descriptions_camera
    ON segment_descriptions(camera_id, indexed_at DESC);
CREATE INDEX IF NOT EXISTS idx_segment_descriptions_tags
    ON segment_descriptions USING GIN(tags);
-- GIN on the tsvector expression lets /api/search/semantic rank
-- results with to_tsquery() + english stemming so "runs"/"running"/
-- "ran" all match.
CREATE INDEX IF NOT EXISTS idx_segment_descriptions_fts
    ON segment_descriptions USING GIN(to_tsvector('english', description));
CREATE INDEX IF NOT EXISTS idx_segment_descriptions_activity
    ON segment_descriptions(activity_level) WHERE activity_level != 'none';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- Reverses 0008. The events.segment_id backfill is a data change —
-- the down migration can't undo it (we'd have to know which rows were
-- NULL before the UP, which we don't). Acceptable trade since rerunning
-- UP after DOWN+UP would re-apply the backfill anyway. Index drops
-- before the table drop so the column DROP doesn't block.
DROP INDEX IF EXISTS idx_segment_descriptions_activity;
DROP INDEX IF EXISTS idx_segment_descriptions_fts;
DROP INDEX IF EXISTS idx_segment_descriptions_tags;
DROP INDEX IF EXISTS idx_segment_descriptions_camera;
DROP TABLE IF EXISTS segment_descriptions;
DROP INDEX IF EXISTS idx_events_segment;
ALTER TABLE events DROP COLUMN IF EXISTS segment_id;
-- +goose StatementEnd
