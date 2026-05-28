-- +goose Up
-- +goose StatementBegin
--
-- Subset 5 of the P1-B-02 extraction. Source: lines 309–352 of the
-- inline block in cmd/server/main.go.
--
-- Two new audit tables for legal-evidence trails:
--
-- 1) deterrence_audits: every fire of a camera's strobe/siren/alarm
--    relay. A siren going off on someone's property needs a precise
--    "who, when, why, success/fail" trail if it's ever challenged.
--    Captures the operator's role + identity + the alarm context.
--
-- 2) playback_audits: every access to a recording segment / playback
--    endpoint. Compliance + discovery question: "who watched what,
--    when". Append-only — the 0NNN_audit_append_only_triggers
--    migration (to be added in a future P1-B-02 batch) installs a
--    BEFORE UPDATE/DELETE trigger that raises insufficient_privilege
--    on any mutation. Until then, application code must not UPDATE
--    these tables.

CREATE TABLE IF NOT EXISTS deterrence_audits (
    id           BIGSERIAL   PRIMARY KEY,
    user_id      TEXT        NOT NULL DEFAULT '',
    username     TEXT        NOT NULL DEFAULT '',
    role         TEXT        NOT NULL DEFAULT '',
    camera_id    UUID        NOT NULL,
    camera_name  TEXT        NOT NULL DEFAULT '',
    action       TEXT        NOT NULL,             -- strobe | siren | both | alarm_out
    duration_sec INT         NOT NULL DEFAULT 0,
    reason       TEXT        NOT NULL DEFAULT '',  -- operator-entered justification
    alarm_id     TEXT        NOT NULL DEFAULT '',  -- which alarm triggered it, if any
    success      BOOLEAN     NOT NULL DEFAULT true,
    error        TEXT        NOT NULL DEFAULT '',
    fired_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip           TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_deterrence_audits_camera
    ON deterrence_audits(camera_id, fired_at DESC);
CREATE INDEX IF NOT EXISTS idx_deterrence_audits_user
    ON deterrence_audits(user_id, fired_at DESC);

CREATE TABLE IF NOT EXISTS playback_audits (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT '',
    camera_id UUID,
    segment_id BIGINT,
    event_id BIGINT,
    endpoint TEXT NOT NULL DEFAULT '',
    accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_playback_audits_user
    ON playback_audits(user_id, accessed_at DESC);
CREATE INDEX IF NOT EXISTS idx_playback_audits_camera
    ON playback_audits(camera_id, accessed_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_playback_audits_camera;
DROP INDEX IF EXISTS idx_playback_audits_user;
DROP TABLE IF EXISTS playback_audits;
DROP INDEX IF EXISTS idx_deterrence_audits_user;
DROP INDEX IF EXISTS idx_deterrence_audits_camera;
DROP TABLE IF EXISTS deterrence_audits;
-- +goose StatementEnd
