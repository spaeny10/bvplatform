-- +goose Up
-- +goose StatementBegin
--
-- 0002_add_segments_video_codec: capture the video codec per segment.
--
-- Motivation: BigView's trailer fleet has standardised on H.265 recording for
-- the bandwidth/storage win, but Chrome/Firefox cannot decode HEVC in a
-- <video> element. The recorded-playback serve handler needs to know each
-- segment's codec so it can pass H.264 through unchanged and route HEVC
-- through a transcode-on-demand path. Storing the codec at index time is
-- vastly cheaper than probing every segment on every request.
--
-- The column is nullable on purpose: existing segments captured before this
-- migration have NULL, and the serve handler treats NULL as "probe on first
-- access and write back" so old data backfills lazily without needing a
-- separate sweeper. Once the column converges to NOT NULL across the live
-- DB, a follow-up migration can enforce that constraint — explicitly out of
-- scope here to keep the rollout reversible.
--
-- video_codec values mirror ffprobe's stream.codec_name field: "h264",
-- "hevc", "av1", "vp9", etc. Lowercase. The serve handler's decision is a
-- simple set membership test — anything we know browsers handle natively
-- passes through; anything else gets transcoded.

ALTER TABLE segments
    ADD COLUMN IF NOT EXISTS video_codec TEXT;

-- Partial index for the transcode-cache GC sweep and the
-- needs-transcoding-soon query. We only ever search for "rows where
-- video_codec is HEVC and no transcode cache yet" / "rows whose codec is
-- still unknown" — covering both with one partial index keeps it tiny.
-- The full segments table is a Timescale hypertable so a btree without a
-- WHERE clause would balloon across every chunk.
CREATE INDEX IF NOT EXISTS idx_segments_video_codec_needs_work
    ON segments (camera_id, start_time DESC)
    WHERE video_codec IS NULL OR video_codec = 'hevc';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_segments_video_codec_needs_work;
ALTER TABLE segments DROP COLUMN IF EXISTS video_codec;
-- +goose StatementEnd
