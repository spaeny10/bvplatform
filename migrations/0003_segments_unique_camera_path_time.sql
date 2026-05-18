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

-- Step 2: dedup. Keep MAX(id) per (camera_id, file_path, start_time);
-- delete every other row matching that tuple. This is functionally
-- equivalent to a self-join `DELETE a USING b WHERE a.id < b.id AND
-- <same keys>` but uses a single index scan + GROUP BY MAX rather than
-- the n²-comparison pair-builder that the self-join requires.
--
-- On a fresh DB this is a no-op (no rows match). On a live hypertable
-- carrying the LOCAL-10 dupe set, the self-join variant of this
-- statement was measured at ~100 minutes for 13M rows per camera; the
-- GROUP BY MAX variant below was ~30 seconds for the same data. The
-- runbook in docs/INFRA_MASTER.md (2026-05-13) records the timings;
-- operators applying this to a heavily-duplicated production table
-- should still expect minutes-not-seconds, just not the 1-2 hours the
-- self-join would have cost.
DELETE FROM segments
WHERE id NOT IN (
    SELECT MAX(id) FROM segments
    GROUP BY camera_id, file_path, start_time
);

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
