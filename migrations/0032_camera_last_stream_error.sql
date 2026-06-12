-- +goose Up
-- +goose StatementBegin
--
-- B-13 / B-14: surface the RTSP probe failure reason so operators can
-- see WHY a camera is offline rather than just that it is.
--
-- last_stream_error — stores the ffprobe error string from the most
--   recent failed stream probe. Cleared (set NULL) when a probe
--   succeeds. NULL means either "never failed" or "last probe was OK".
--   TEXT (unbounded) to avoid truncating long ffprobe stderr output.
--
-- This column is intentionally nullable; existing rows default to NULL,
-- which is correct — no probe has run against them yet.
-- +goose StatementEnd

ALTER TABLE cameras
    ADD COLUMN IF NOT EXISTS last_stream_error TEXT;

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cameras
    DROP COLUMN IF EXISTS last_stream_error;
-- +goose StatementEnd
