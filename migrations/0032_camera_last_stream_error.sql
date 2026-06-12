-- +goose Up
-- B-13 / B-14: surface the RTSP probe failure reason so operators can
-- see WHY a camera is offline rather than just that it is.
--
-- last_stream_error stores the ffprobe error from the most recent failed
-- RTSP stream probe. NULL means never failed or last probe succeeded.
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS last_stream_error TEXT;

-- Recreate cameras_active so it picks up the new column.
-- PostgreSQL's SELECT * in a view is expanded at creation time and does
-- NOT automatically include columns added to the base table afterwards.
CREATE OR REPLACE VIEW cameras_active AS
    SELECT * FROM cameras WHERE deleted_at IS NULL;

-- +goose Down
DROP VIEW IF EXISTS cameras_active;
ALTER TABLE cameras DROP COLUMN IF EXISTS last_stream_error;
CREATE VIEW cameras_active AS
    SELECT * FROM cameras WHERE deleted_at IS NULL;
