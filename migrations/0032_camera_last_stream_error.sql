-- +goose Up
-- B-13 / B-14: surface the RTSP probe failure reason so operators can
-- see WHY a camera is offline rather than just that it is.
--
-- last_stream_error stores the ffprobe error from the most recent failed
-- RTSP stream probe. NULL means never failed or last probe succeeded.
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS last_stream_error TEXT;

-- +goose Down
ALTER TABLE cameras DROP COLUMN IF EXISTS last_stream_error;
