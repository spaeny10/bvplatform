-- +goose Up
-- +goose StatementBegin
--
-- 0003_segments_unique_camera_path_time: dedup + enforce uniqueness on
-- (camera_id, file_path, start_time) for the segments hypertable.
--
-- Motivation: the recorder's segment indexer had two stacked bugs that
-- accumulated millions of phantom + duplicate rows in the segments table
-- before they were caught (LOCAL-10 in ironsight/backlog/phase-1.md):
--
--   * Bug 1 — rescans on ffmpeg restart re-`InsertSegment`'d every existing
--     file. The in-mem `seen` map reset to empty on each restart and the
--     schema had no UNIQUE constraint to catch the second insert. Front
--     camera on trailer-570 held 12.6M rows for 9,487 distinct files
--     (~1,330 dupes per file).
--   * Bug 2 — the size-based "skip incomplete" filter let
--     `ffmpeg_stderr.log` (4-9 KB, written by the same recorder into its
--     output dir) get indexed as a segment. Confirmed ~3,916 such rows on
--     trailer-570 front + back combined; the serve handler then returned
--     log-text bytes to `<video>` tags, which errored.
--
-- The Go-side fixes shipped in commits 1d7f15e (file-type filter) and
-- 509a8e0 (mtime-gate on rescan). This migration handles the data side:
-- (a) drops the phantom ffmpeg_stderr.log rows, (b) dedupes the
-- (camera_id, file_path) duplicates accumulated pre-fix, (c) installs a
-- UNIQUE index so the failure class can never come back even if the Go
-- guards regress in a future refactor.
--
-- Hypertable constraint: TimescaleDB requires the partitioning column
-- (start_time here) to be part of any unique index. Including it is fine
-- for the bug class — `parseSegmentTimestamp` is deterministic from the
-- filename, so the pre-fix bug re-inserted rows with IDENTICAL start_time,
-- meaning (camera_id, file_path, start_time) catches every observed dupe.
--
-- Performance note: on a fresh DB this migration is instant. On fred's
-- live segments hypertable (~25M rows pre-cleanup, 14 daily chunks) the
-- DELETE USING is the slow part — measured ~2-4 minutes per chunk during
-- a dry-run on a copy. Run during a maintenance window. The api binary
-- can keep serving from the partially-cleaned table while the migration
-- runs (DELETE doesn't block SELECT under MVCC), but cache writes to
-- .h264-cache/ may briefly contend on the disk.

-- Step 1: phantom rows
DELETE FROM segments WHERE file_path LIKE '%/ffmpeg_stderr.log';

-- Step 2: dedup. Match the exact predicate of the unique index we're
-- about to create — DELETE rows that would violate (camera_id, file_path,
-- start_time) only, preserving any legitimate same-path-different-start_time
-- rows (none expected for the bug class we're fixing, but a stricter
-- DELETE risks data loss if the workload ever produced them).
DELETE FROM segments a
USING segments b
WHERE a.id < b.id
  AND a.camera_id = b.camera_id
  AND a.file_path = b.file_path
  AND a.start_time = b.start_time;

-- Step 3: install the constraint. IF NOT EXISTS lets the migration be
-- re-run idempotently if it crashes mid-way (the DELETEs above are also
-- idempotent — re-running deletes 0 rows on a clean table).
CREATE UNIQUE INDEX IF NOT EXISTS segments_camera_path_time_unq
    ON segments (camera_id, file_path, start_time);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS segments_camera_path_time_unq;
-- The data DELETEs are not reversible. Down only removes the index,
-- leaving the (now-clean) segments table intact, which is the correct
-- behavior — we don't want a Down to re-introduce phantom rows.
-- +goose StatementEnd
