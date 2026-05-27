-- +goose Up
-- +goose StatementBegin
--
-- 0004_check_constraints_enum_text_cols (P1-B-08):
-- add CHECK constraints to enum-like TEXT columns that didn't already
-- have them at baseline. Catches typos and stale-string-literal writes
-- at insert/update time instead of letting them silently rot in the DB
-- until something downstream filters by status/mode.
--
-- Scope decisions:
--
--   * Only columns I could verify exist in BOTH the baseline migration
--     AND the live fred production DB. The baseline + prod diverged at
--     some prior point (events.severity is in baseline but not in
--     production's events table) so I'm being conservative — anything
--     that might be missing in some deployment gets deferred to a
--     future migration that targets the specific column.
--
--   * All constraints use NOT VALID — matches the pattern of the
--     baseline's existing CHECKs (incidents_severity_chk etc.). Existing
--     rows aren't scanned, only new writes get checked. If a row that
--     violates already exists, the migration applies cleanly and the
--     violating row is caught on its next UPDATE. A separate cleanup
--     pass can VALIDATE CONSTRAINT later once we're confident no
--     violators remain.
--
--   * Allowed-value lists derived from grep over the Go codebase
--     (writer sites: db.UpdateCameraStatus(... "online"|"offline"),
--     RecordingMode = "continuous"|"event"), with a small safety
--     margin for values that aren't currently written but match the
--     TS frontend types ("degraded" status). Future writers that need
--     a new value have to ALTER the constraint first — that's the
--     point.
--
-- Documented allowed-value sets also live in docs/data-model.md so the
-- list is one git-grep away when someone needs to add a value.

ALTER TABLE cameras
    ADD CONSTRAINT cameras_status_chk
    CHECK (status IN ('online', 'offline', 'degraded'))
    NOT VALID;

ALTER TABLE cameras
    ADD CONSTRAINT cameras_recording_mode_chk
    CHECK (recording_mode IN ('continuous', 'event'))
    NOT VALID;

ALTER TABLE speakers
    ADD CONSTRAINT speakers_status_chk
    CHECK (status IN ('online', 'offline', 'degraded'))
    NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Guard with to_regclass: in a full `goose reset` the down chain runs
-- 0029→0001, and a table this migration constrained may already have been
-- dropped by a LATER migration's down (speakers is re-declared in 0007, whose
-- down drops it before this runs). Down is never run in prod (up-only), but
-- the CI up/down/up round-trip exercises every down section, so it must be
-- resilient to tables already being gone.
DO $$
BEGIN
    IF to_regclass('public.cameras') IS NOT NULL THEN
        ALTER TABLE cameras DROP CONSTRAINT IF EXISTS cameras_status_chk;
        ALTER TABLE cameras DROP CONSTRAINT IF EXISTS cameras_recording_mode_chk;
    END IF;
    IF to_regclass('public.speakers') IS NOT NULL THEN
        ALTER TABLE speakers DROP CONSTRAINT IF EXISTS speakers_status_chk;
    END IF;
END $$;
-- +goose StatementEnd
