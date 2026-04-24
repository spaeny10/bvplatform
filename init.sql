-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

-- Cameras table
CREATE TABLE IF NOT EXISTS cameras (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    onvif_address   TEXT NOT NULL,
    username        TEXT DEFAULT '',
    password        TEXT DEFAULT '',
    rtsp_uri        TEXT DEFAULT '',
    sub_stream_uri  TEXT DEFAULT '',
    retention_days  INT DEFAULT 30,
    recording       BOOLEAN DEFAULT true,
    recording_mode  TEXT DEFAULT 'continuous',
    pre_buffer_sec  INT DEFAULT 10,
    post_buffer_sec INT DEFAULT 30,
    recording_triggers TEXT DEFAULT 'motion,object',
    status          TEXT DEFAULT 'offline',
    profile_token   TEXT DEFAULT '',
    has_ptz         BOOLEAN DEFAULT false,
    manufacturer    TEXT DEFAULT '',
    model           TEXT DEFAULT '',
    firmware        TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Video segments table (hypertable for time-based partitioning)
CREATE TABLE IF NOT EXISTS segments (
    id          BIGSERIAL,
    camera_id   UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ NOT NULL,
    file_path   TEXT NOT NULL,
    file_size   BIGINT DEFAULT 0,
    duration_ms INT DEFAULT 0
);
SELECT create_hypertable('segments', 'start_time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_segments_camera_time ON segments (camera_id, start_time DESC);

-- Events / metadata table (hypertable for near-instant timeline queries)
CREATE TABLE IF NOT EXISTS events (
    id          BIGSERIAL,
    camera_id   UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    event_time  TIMESTAMPTZ NOT NULL,
    event_type  TEXT NOT NULL,
    details     JSONB DEFAULT '{}',
    thumbnail   TEXT DEFAULT ''
);
SELECT create_hypertable('events', 'event_time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_events_camera_type ON events (camera_id, event_type, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_events_details ON events USING GIN (details);

-- Export jobs table
CREATE TABLE IF NOT EXISTS exports (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id   UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ NOT NULL,
    status      TEXT DEFAULT 'pending',
    file_path   TEXT DEFAULT '',
    file_size   BIGINT DEFAULT 0,
    error       TEXT DEFAULT '',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'operator',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- System settings table (single-row; enforced by CHECK id = 1)
CREATE TABLE IF NOT EXISTS system_settings (
    id                      INT PRIMARY KEY DEFAULT 1,
    recordings_path         TEXT DEFAULT './storage/recordings',
    snapshots_path          TEXT DEFAULT './storage/thumbnails',
    exports_path            TEXT DEFAULT './storage/exports',
    hls_path                TEXT DEFAULT './storage/hls',
    default_retention_days  INT DEFAULT 30,
    default_recording_mode  TEXT DEFAULT 'continuous',
    default_segment_duration INT DEFAULT 60,
    ffmpeg_path             TEXT DEFAULT 'C:\ffmpeg\bin\ffmpeg.exe',
    updated_at              TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT single_row CHECK (id = 1)
);

-- Storage locations table (multi-path recording storage)
CREATE TABLE IF NOT EXISTS storage_locations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label           TEXT NOT NULL,
    path            TEXT NOT NULL,
    purpose         TEXT NOT NULL DEFAULT 'recordings',
    retention_days  INT DEFAULT 30,
    max_gb          INT DEFAULT 0,
    priority        INT DEFAULT 0,
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Speakers table (ONVIF audio devices for talk-down)
CREATE TABLE IF NOT EXISTS speakers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    onvif_address   TEXT NOT NULL,
    username        TEXT DEFAULT '',
    password        TEXT DEFAULT '',
    rtsp_uri        TEXT DEFAULT '',
    zone            TEXT DEFAULT '',
    status          TEXT DEFAULT 'offline',
    manufacturer    TEXT DEFAULT '',
    model           TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Audio messages table (pre-recorded talk-down messages)
CREATE TABLE IF NOT EXISTS audio_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    category        TEXT NOT NULL DEFAULT 'custom',
    file_name       TEXT NOT NULL,
    duration        REAL DEFAULT 0,
    file_size       BIGINT DEFAULT 0,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);
