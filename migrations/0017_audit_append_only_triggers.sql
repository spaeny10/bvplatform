-- +goose Up
-- +goose StatementBegin
--
-- Subset 13 of the P1-B-02 extraction. Source: lines 809–837 of the
-- inline block in cmd/server/main.go.
--
-- Append-only enforcement on the three audit tables via a BEFORE
-- UPDATE/DELETE trigger. We deliberately don't REVOKE UPDATE/DELETE
-- from the app role — migrations and scripted maintenance run as this
-- role too. Instead a trigger raises insufficient_privilege on any
-- mutation; equally effective, leaves one obvious place to disable
-- (with a signed maintenance-window comment) if a GDPR right-to-
-- erasure ever lands.
--
-- The trigger function is shared across all three audit tables;
-- DROP TRIGGER IF EXISTS lets the migration re-attach cleanly on
-- subsequent runs without piling up duplicates.
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ironsight_prevent_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit table %.% is append-only (op=%)',
        TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS audit_log_append_only       ON audit_log;
DROP TRIGGER IF EXISTS playback_audits_append_only ON playback_audits;
DROP TRIGGER IF EXISTS deterrence_audits_append_only ON deterrence_audits;

CREATE TRIGGER audit_log_append_only
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();
CREATE TRIGGER playback_audits_append_only
    BEFORE UPDATE OR DELETE ON playback_audits
    FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();
CREATE TRIGGER deterrence_audits_append_only
    BEFORE UPDATE OR DELETE ON deterrence_audits
    FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS deterrence_audits_append_only ON deterrence_audits;
DROP TRIGGER IF EXISTS playback_audits_append_only   ON playback_audits;
DROP TRIGGER IF EXISTS audit_log_append_only         ON audit_log;
DROP FUNCTION IF EXISTS ironsight_prevent_mutation();
-- +goose StatementEnd
