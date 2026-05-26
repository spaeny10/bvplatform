-- +goose NO TRANSACTION
--
-- P2-C-02: Person-tracking aggregation tables.
--
-- person_track_frames: raw per-frame occupancy time series.
--   Hypertable on `time`, 7-day retention via TimescaleDB policy.
--   Feeds the 5-minute roll-up aggregator.
--   C-03 (VLM validation) reads this table to find frames with persons
--   for targeted VLM re-inference.
--
-- person_track_buckets: pre-aggregated 5-minute occupancy windows.
--   Regular table, 90-day retention managed by the Go retention sweep.
--   Consumed by the C-06 compliance dashboard.
--
-- ID type note: organization_id and site_id are TEXT (matching the
-- organizations and sites tables, which pre-date the uuid migration —
-- same convention as migration 0022).
--
-- R5 mitigation: add_retention_policy may not work inside a goose
-- transaction boundary on some TimescaleDB versions. This file uses
-- -- +goose NO TRANSACTION to run each statement directly. The Go
-- retention manager in internal/recording/retention.go also sweeps
-- person_track_frames as a belt-and-suspenders fallback.

-- +goose Up

CREATE TABLE IF NOT EXISTS person_track_frames (
    time                TIMESTAMPTZ NOT NULL,
    camera_id           UUID        NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    site_id             TEXT                 REFERENCES sites(id)   ON DELETE SET NULL,
    organization_id     TEXT        NOT NULL,
    person_count        INT         NOT NULL DEFAULT 0 CHECK (person_count >= 0),
    bounding_boxes      JSONB,
    track_ids           JSONB,
    frame_source        TEXT        NOT NULL DEFAULT ''
);

SELECT create_hypertable('person_track_frames', 'time',
    chunk_time_interval => INTERVAL '7 days',
    if_not_exists => TRUE);

SELECT add_retention_policy('person_track_frames',
    INTERVAL '7 days', if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_ptf_org_time
    ON person_track_frames (organization_id, time DESC);

CREATE INDEX IF NOT EXISTS idx_ptf_camera_time
    ON person_track_frames (camera_id, time DESC);

CREATE INDEX IF NOT EXISTS idx_ptf_person_nonzero
    ON person_track_frames (time DESC)
    WHERE person_count > 0;

CREATE TABLE IF NOT EXISTS person_track_buckets (
    camera_id           UUID        NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    site_id             TEXT                 REFERENCES sites(id)   ON DELETE SET NULL,
    organization_id     TEXT        NOT NULL,
    bucket_start        TIMESTAMPTZ NOT NULL,
    bucket_minutes      INT         NOT NULL DEFAULT 5 CHECK (bucket_minutes > 0),

    person_minutes      REAL        NOT NULL DEFAULT 0 CHECK (person_minutes >= 0),
    peak_person_count   INT         NOT NULL DEFAULT 0 CHECK (peak_person_count >= 0),
    frame_count         INT         NOT NULL DEFAULT 0 CHECK (frame_count >= 0),
    violation_count     INT         NOT NULL DEFAULT 0 CHECK (violation_count >= 0),
    rolled_up_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (camera_id, bucket_start, bucket_minutes)
);

CREATE INDEX IF NOT EXISTS idx_ptb_org_bucket
    ON person_track_buckets (organization_id, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_ptb_site_bucket
    ON person_track_buckets (site_id, bucket_start DESC)
    WHERE site_id IS NOT NULL;

-- +goose Down

SELECT remove_retention_policy('person_track_frames', if_not_exists => TRUE);
DROP INDEX IF EXISTS idx_ptf_person_nonzero;
DROP INDEX IF EXISTS idx_ptf_camera_time;
DROP INDEX IF EXISTS idx_ptf_org_time;
DROP TABLE IF EXISTS person_track_frames;
DROP INDEX IF EXISTS idx_ptb_site_bucket;
DROP INDEX IF EXISTS idx_ptb_org_bucket;
DROP TABLE IF EXISTS person_track_buckets;
