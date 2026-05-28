-- +goose Up
-- +goose StatementBegin
--
-- Subset 15 of the P1-B-02 extraction. Source: lines 867–951 of the
-- inline block in cmd/server/main.go.
--
-- Two DO blocks that wrap ALTER TABLE ... ADD CONSTRAINT in
-- per-statement exception handlers so re-running the migration is
-- always safe.
--
-- 1) FK + uniqueness backfill:
--    - vlm_label_jobs.alarm_id UNIQUE — the Go enqueue path uses
--      ON CONFLICT (alarm_id) DO NOTHING; without the constraint, the
--      ON CONFLICT clause silently inserts duplicates. If duplicates
--      already exist when the migration runs, the inner DELETE
--      removes the older copies (a.id < b.id) and retries the ALTER.
--    - evidence_shares.incident_id FK → incidents(id) with NOT VALID:
--      skip the backfill validation (some legacy rows pre-date the
--      column population), but enforce on new inserts. Validate later
--      with `ALTER TABLE evidence_shares VALIDATE CONSTRAINT ...`
--      once the legacy rows are reconciled.
--
-- 2) CHECK constraints on TEXT enums with NOT VALID:
--    Guards against typos in code creating orphan rows that no
--    query filter recognizes. NOT VALID = don't scan history; some
--    legacy rows may have written values from the old enum set
--    before we tightened the allow-list.
-- +goose StatementEnd

-- +goose StatementBegin
DO $migrate$
BEGIN
    BEGIN
        ALTER TABLE vlm_label_jobs ADD CONSTRAINT vlm_label_jobs_alarm_id_key UNIQUE (alarm_id);
    EXCEPTION WHEN duplicate_object THEN NULL;
        WHEN duplicate_table THEN NULL;
        WHEN unique_violation THEN
            -- Existing duplicates would block the constraint. Drop the
            -- older copies first, then retry.
            DELETE FROM vlm_label_jobs a USING vlm_label_jobs b
             WHERE a.alarm_id = b.alarm_id AND a.id < b.id;
            BEGIN
                ALTER TABLE vlm_label_jobs ADD CONSTRAINT vlm_label_jobs_alarm_id_key UNIQUE (alarm_id);
            EXCEPTION WHEN duplicate_object THEN NULL;
            END;
    END;

    BEGIN
        ALTER TABLE evidence_shares
            ADD CONSTRAINT evidence_shares_incident_fk
            FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE
            NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
        WHEN undefined_table THEN NULL;
    END;
END;
$migrate$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $checks$
BEGIN
    BEGIN
        ALTER TABLE users ADD CONSTRAINT users_role_chk
            CHECK (role IN ('admin','soc_operator','soc_supervisor','site_manager','customer','viewer','guard')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        ALTER TABLE incidents ADD CONSTRAINT incidents_status_chk
            CHECK (status IN ('active','acknowledged','resolved','closed')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        ALTER TABLE incidents ADD CONSTRAINT incidents_severity_chk
            CHECK (severity IN ('low','medium','high','critical')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        ALTER TABLE active_alarms ADD CONSTRAINT active_alarms_severity_chk
            CHECK (severity IN ('low','medium','high','critical')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        ALTER TABLE support_tickets ADD CONSTRAINT support_tickets_status_chk
            CHECK (status IN ('open','answered','closed')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
        WHEN undefined_table THEN NULL;
    END;
    BEGIN
        ALTER TABLE vlm_labels ADD CONSTRAINT vlm_labels_verdict_chk
            CHECK (verdict IN ('correct','incorrect','needs_correction')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
    BEGIN
        ALTER TABLE vlm_label_jobs ADD CONSTRAINT vlm_label_jobs_status_chk
            CHECK (status IN ('pending','claimed','labeled','skipped')) NOT VALID;
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
END;
$checks$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE vlm_label_jobs  DROP CONSTRAINT IF EXISTS vlm_label_jobs_status_chk;
ALTER TABLE vlm_labels      DROP CONSTRAINT IF EXISTS vlm_labels_verdict_chk;
ALTER TABLE support_tickets DROP CONSTRAINT IF EXISTS support_tickets_status_chk;
ALTER TABLE active_alarms   DROP CONSTRAINT IF EXISTS active_alarms_severity_chk;
ALTER TABLE incidents       DROP CONSTRAINT IF EXISTS incidents_severity_chk;
ALTER TABLE incidents       DROP CONSTRAINT IF EXISTS incidents_status_chk;
ALTER TABLE users           DROP CONSTRAINT IF EXISTS users_role_chk;
ALTER TABLE evidence_shares DROP CONSTRAINT IF EXISTS evidence_shares_incident_fk;
ALTER TABLE vlm_label_jobs  DROP CONSTRAINT IF EXISTS vlm_label_jobs_alarm_id_key;
-- +goose StatementEnd
