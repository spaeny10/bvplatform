-- +goose Up
-- +goose StatementBegin
--
-- Camera-side per-device settings columns. All ADD COLUMN IF NOT EXISTS
-- so this is a no-op on fred (the inline block in cmd/server/main.go
-- already ran against it) and a clean install on a fresh DB.
--
-- Subset 1 of the P1-B-02 extraction. Source: lines 122–146 +
-- 216–217 of cmd/server/main.go's inline block.
--
-- device_class:        'continuous' (default) | 'sense_pushed' — controls
--                      whether the recorder pulls RTSP + subscribes to
--                      ONVIF events, or skips both and waits for Milesight
--                      Sense webhook POSTs.
-- sense_webhook_token: per-camera bearer in /api/integrations/milesight/sense/
--                      URL; populated only for sense_pushed cameras.
-- recording_mode:      'continuous' | 'event' — the recorder reads this
--                      via SettingsForCamera() to decide whether to record
--                      24/7 or only around triggered events.
-- pre_buffer_sec / post_buffer_sec / recording_triggers: event-mode-only
--                      settings, ignored when recording_mode='continuous'.
-- has_ptz / events_enabled / audio_enabled / camera_group / schedule /
-- privacy_mask:         operational flags surfaced in the Settings UI.
-- map_x / map_y:        future site-map overlay coords (currently 0).

ALTER TABLE cameras ADD COLUMN IF NOT EXISTS device_class TEXT DEFAULT 'continuous';
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS sense_webhook_token TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_cameras_sense_token
    ON cameras(sense_webhook_token) WHERE sense_webhook_token IS NOT NULL;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS recording_mode TEXT DEFAULT 'continuous';
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS pre_buffer_sec INT DEFAULT 10;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS post_buffer_sec INT DEFAULT 30;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS recording_triggers TEXT DEFAULT 'motion,object';
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS has_ptz BOOLEAN DEFAULT false;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS events_enabled BOOLEAN DEFAULT true;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS audio_enabled BOOLEAN DEFAULT true;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS camera_group TEXT DEFAULT '';
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS schedule TEXT DEFAULT '';
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS privacy_mask BOOLEAN DEFAULT false;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS map_x REAL DEFAULT 0;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS map_y REAL DEFAULT 0;
ALTER TABLE segments ADD COLUMN IF NOT EXISTS has_audio BOOLEAN DEFAULT false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- Reverses 0005. Each DROP COLUMN IF EXISTS so a partial-up-then-down
-- doesn't error. The UNIQUE index is dropped before the column it
-- targets so the order doesn't matter on Postgres.
DROP INDEX IF EXISTS idx_cameras_sense_token;
ALTER TABLE segments DROP COLUMN IF EXISTS has_audio;
ALTER TABLE cameras DROP COLUMN IF EXISTS map_y;
ALTER TABLE cameras DROP COLUMN IF EXISTS map_x;
ALTER TABLE cameras DROP COLUMN IF EXISTS privacy_mask;
ALTER TABLE cameras DROP COLUMN IF EXISTS schedule;
ALTER TABLE cameras DROP COLUMN IF EXISTS camera_group;
ALTER TABLE cameras DROP COLUMN IF EXISTS audio_enabled;
ALTER TABLE cameras DROP COLUMN IF EXISTS events_enabled;
ALTER TABLE cameras DROP COLUMN IF EXISTS has_ptz;
ALTER TABLE cameras DROP COLUMN IF EXISTS recording_triggers;
ALTER TABLE cameras DROP COLUMN IF EXISTS post_buffer_sec;
ALTER TABLE cameras DROP COLUMN IF EXISTS pre_buffer_sec;
ALTER TABLE cameras DROP COLUMN IF EXISTS recording_mode;
ALTER TABLE cameras DROP COLUMN IF EXISTS sense_webhook_token;
ALTER TABLE cameras DROP COLUMN IF EXISTS device_class;
-- +goose StatementEnd
