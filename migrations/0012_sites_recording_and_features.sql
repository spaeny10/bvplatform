-- +goose Up
-- +goose StatementBegin
--
-- Subset 8 of the P1-B-02 extraction. Source: lines 472–527 +
-- 573–593 of the inline block in cmd/server/main.go.
--
-- Site-level recording config + product-tier flag + customer-managed
-- contacts.
--
-- 1) feature_mode: 'security_only' | 'security_and_safety'. Controls
--    whether the PPE/OSHA/vLM-safety surfaces are active for the
--    site. Default 'security_and_safety' for backward compat.
--
-- 2) monitoring_schedule / snooze: JSONB time-window controls — the
--    SOC respects these when deciding whether to escalate alarms
--    from this site at this moment.
--
-- 3) Recording policy moved from per-camera to per-site as of the
--    2026-04 migration. Old camera-side columns stay in DB for one
--    release as a rollback cushion. The recorder reads site-level
--    values at start/restart via recording.SettingsForCamera().
--
-- 4) customer_contacts: customer-maintained on-site call list,
--    distinct from the SOC-side site_sops.contacts. Source of truth
--    for the customer portal's contact editor.
--
-- 5) The trailing UPDATE is a one-time backfill that copies per-camera
--    settings into the new per-site columns where the site hadn't been
--    customized yet. Idempotent via the recording_backfilled = false
--    guard — once a row is backfilled, the flag flips and we never
--    overwrite admin-set values.

ALTER TABLE sites ADD COLUMN IF NOT EXISTS feature_mode TEXT NOT NULL DEFAULT 'security_and_safety';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS monitoring_schedule JSONB DEFAULT '[]';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS snooze JSONB DEFAULT NULL;

ALTER TABLE sites ADD COLUMN IF NOT EXISTS retention_days       INT  DEFAULT 3;
-- Tighten the default for any FUTURE site row inserted without an
-- explicit retention. Existing rows are unaffected.
ALTER TABLE sites ALTER COLUMN retention_days SET DEFAULT 3;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_mode       TEXT DEFAULT 'continuous';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS pre_buffer_sec       INT  DEFAULT 10;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS post_buffer_sec      INT  DEFAULT 30;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_triggers   TEXT DEFAULT 'motion,object';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_schedule   TEXT DEFAULT '';
ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_backfilled BOOLEAN DEFAULT false;

ALTER TABLE sites ADD COLUMN IF NOT EXISTS customer_contacts JSONB NOT NULL DEFAULT '[]';

-- One-time per-site backfill from the most-recently-updated camera
-- on each site. Idempotent: only touches recording_backfilled = false.
UPDATE sites s
SET retention_days      = COALESCE(bf.retention_days, s.retention_days),
    recording_mode      = COALESCE(NULLIF(bf.recording_mode, ''), s.recording_mode),
    pre_buffer_sec      = COALESCE(bf.pre_buffer_sec, s.pre_buffer_sec),
    post_buffer_sec     = COALESCE(bf.post_buffer_sec, s.post_buffer_sec),
    recording_triggers  = COALESCE(NULLIF(bf.recording_triggers, ''), s.recording_triggers),
    recording_schedule  = COALESCE(NULLIF(bf.schedule, ''), s.recording_schedule),
    recording_backfilled = true
FROM (
    SELECT DISTINCT ON (site_id)
           site_id, retention_days, recording_mode, pre_buffer_sec,
           post_buffer_sec, recording_triggers, schedule
    FROM cameras
    WHERE site_id IS NOT NULL AND site_id <> ''
    ORDER BY site_id, updated_at DESC NULLS LAST
) bf
WHERE s.id = bf.site_id AND s.recording_backfilled = false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- The backfill UPDATE can't be cleanly reversed (we'd have to remember
-- what each row's retention_days was BEFORE the COALESCE). Acceptable
-- because re-running UP after DOWN+UP would re-apply the backfill from
-- the same source data. Order: drop columns in reverse-add order; the
-- retention_days SET DEFAULT undoes back to the inferred prior default
-- of 3 (which is the same value — so it's effectively a no-op DOWN for
-- that statement specifically).
ALTER TABLE sites DROP COLUMN IF EXISTS customer_contacts;
ALTER TABLE sites DROP COLUMN IF EXISTS recording_backfilled;
ALTER TABLE sites DROP COLUMN IF EXISTS recording_schedule;
ALTER TABLE sites DROP COLUMN IF EXISTS recording_triggers;
ALTER TABLE sites DROP COLUMN IF EXISTS post_buffer_sec;
ALTER TABLE sites DROP COLUMN IF EXISTS pre_buffer_sec;
ALTER TABLE sites DROP COLUMN IF EXISTS recording_mode;
ALTER TABLE sites DROP COLUMN IF EXISTS retention_days;
ALTER TABLE sites DROP COLUMN IF EXISTS snooze;
ALTER TABLE sites DROP COLUMN IF EXISTS monitoring_schedule;
ALTER TABLE sites DROP COLUMN IF EXISTS feature_mode;
-- +goose StatementEnd
