-- +goose Up
-- +goose StatementBegin
--
-- Subset 10 of the P1-B-02 extraction. Source: lines 595–655 of the
-- inline block in cmd/server/main.go.
--
-- Four small clusters: export worker claim tracking, alarm short-code
-- + forensic linkage + SLA tracking, audit_log polymorphic target.
--
-- 1) exports.started_at + the two partial indexes give the export
--    worker a quick "what jobs are in queue" lookup and the
--    "what's stuck" detection at startup. Stuck = status='processing'
--    AND started_at > N minutes ago.
--
-- 2) active_alarms.alarm_code: phoneticizable ALM-YYMMDD-NNNN. The
--    UUID PK is fine for URLs but unreadable over a radio bridge.
--    Legacy rows stay NULL — the generator backfills on next write.
--
-- 3) triggering_event_id: forensic link from alarm back to the event
--    that fired it. BIGINT no-FK (events is a Timescale hypertable
--    and can't be the target of a FK constraint).
--
-- 4) UL 827B SLA tracking columns. The original sla_deadline_ms
--    captures the deadline; these three capture the actual ACK to
--    enable single-SELECT "did we meet SLA" reports.
--
-- 5) audit_log.target_type/target_id polymorphic columns + composite
--    index let us answer "who touched this camera" via a single
--    indexed query instead of LIKE-scanning route paths.

ALTER TABLE exports ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_exports_pending
    ON exports (created_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_exports_processing
    ON exports (started_at) WHERE status = 'processing';

ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS alarm_code TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_active_alarms_code
    ON active_alarms(alarm_code) WHERE alarm_code IS NOT NULL;

ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS triggering_event_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_active_alarms_event
    ON active_alarms(triggering_event_id) WHERE triggering_event_id IS NOT NULL;

ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMPTZ;
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS acknowledged_by_user_id UUID;
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS acknowledged_by_callsign TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_active_alarms_ack_window
    ON active_alarms(acknowledged_at, ts) WHERE acknowledged_at IS NOT NULL;

ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_type TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_id   TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_audit_log_target
    ON audit_log(target_type, target_id, created_at DESC)
    WHERE target_id <> '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_audit_log_target;
ALTER TABLE audit_log DROP COLUMN IF EXISTS target_id;
ALTER TABLE audit_log DROP COLUMN IF EXISTS target_type;
DROP INDEX IF EXISTS idx_active_alarms_ack_window;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS acknowledged_by_callsign;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS acknowledged_by_user_id;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS acknowledged_at;
DROP INDEX IF EXISTS idx_active_alarms_event;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS triggering_event_id;
DROP INDEX IF EXISTS idx_active_alarms_code;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS alarm_code;
DROP INDEX IF EXISTS idx_exports_processing;
DROP INDEX IF EXISTS idx_exports_pending;
ALTER TABLE exports DROP COLUMN IF EXISTS started_at;
-- +goose StatementEnd
