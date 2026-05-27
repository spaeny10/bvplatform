-- +goose Up
-- +goose StatementBegin

-- ══════════════════════════════════════════════════════════════════
-- Phase A — Add deleted_at column to the 8 soft-delete tables.
--
-- All additions are nullable with no default, so existing rows are
-- unaffected (deleted_at IS NULL = "live"). This is a safe, instant
-- DDL operation in PostgreSQL — no table rewrite, no downtime.
-- ══════════════════════════════════════════════════════════════════

ALTER TABLE cameras          ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE sites            ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE organizations    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE users            ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE speakers         ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE ppe_zones        ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE compliance_rules ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE vca_rules        ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- ══════════════════════════════════════════════════════════════════
-- Phase B — Create _active views.
--
-- Each view is a simple SELECT * WHERE deleted_at IS NULL.  All
-- normal read paths (ListX, GetX) are repointed to the _active
-- view.  The base table is only touched directly by:
--   • soft-delete setters  (UPDATE SET deleted_at = NOW())
--   • admin include-deleted paths  (SELECT ... FROM <table> WHERE ...)
--   • future purge paths  (hard DELETE — deferred to P4)
--
-- CREATE OR REPLACE VIEW is idempotent — safe to re-run.
-- ══════════════════════════════════════════════════════════════════

CREATE OR REPLACE VIEW cameras_active AS
    SELECT * FROM cameras WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW sites_active AS
    SELECT * FROM sites WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW organizations_active AS
    SELECT * FROM organizations WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW users_active AS
    SELECT * FROM users WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW speakers_active AS
    SELECT * FROM speakers WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW ppe_zones_active AS
    SELECT * FROM ppe_zones WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW compliance_rules_active AS
    SELECT * FROM compliance_rules WHERE deleted_at IS NULL;

CREATE OR REPLACE VIEW vca_rules_active AS
    SELECT * FROM vca_rules WHERE deleted_at IS NULL;

-- ══════════════════════════════════════════════════════════════════
-- Phase C — Partial unique indexes.
--
-- Without partial indexes, a soft-deleted row's unique values (e.g.
-- username, sense_webhook_token) block reuse.  We convert the
-- affected full unique constraints to partial ones scoped to
-- WHERE deleted_at IS NULL, freeing the slot for new rows.
--
-- cameras: replace the existing partial index (IS NOT NULL only)
--          with one that also excludes soft-deleted rows.
-- users:   replace the full UNIQUE constraint on username with a
--          partial index.  Email uniqueness is NOT enforced by the
--          current schema (no constraint exists on users.email), so
--          no change is needed there.
-- ══════════════════════════════════════════════════════════════════

-- cameras.sense_webhook_token — was: UNIQUE WHERE sense_webhook_token IS NOT NULL
-- now:  UNIQUE WHERE sense_webhook_token IS NOT NULL AND deleted_at IS NULL
DROP INDEX IF EXISTS idx_cameras_sense_token;
CREATE UNIQUE INDEX IF NOT EXISTS idx_cameras_sense_token
    ON cameras (sense_webhook_token)
    WHERE sense_webhook_token IS NOT NULL AND deleted_at IS NULL;

-- users.username — was: full UNIQUE constraint users_username_key
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_username_key;
CREATE UNIQUE INDEX IF NOT EXISTS users_username_active_key
    ON users (username)
    WHERE deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore original unique index on cameras.sense_webhook_token
DROP INDEX IF EXISTS idx_cameras_sense_token;
CREATE UNIQUE INDEX IF NOT EXISTS idx_cameras_sense_token
    ON cameras (sense_webhook_token)
    WHERE sense_webhook_token IS NOT NULL;

-- Restore original full unique constraint on users.username
DROP INDEX IF EXISTS users_username_active_key;
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);

-- Drop _active views
DROP VIEW IF EXISTS vca_rules_active;
DROP VIEW IF EXISTS compliance_rules_active;
DROP VIEW IF EXISTS ppe_zones_active;
DROP VIEW IF EXISTS speakers_active;
DROP VIEW IF EXISTS users_active;
DROP VIEW IF EXISTS organizations_active;
DROP VIEW IF EXISTS sites_active;
DROP VIEW IF EXISTS cameras_active;

-- Remove deleted_at columns
ALTER TABLE vca_rules        DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE compliance_rules DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE ppe_zones        DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE speakers         DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE users            DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE organizations    DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE sites            DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE cameras          DROP COLUMN IF EXISTS deleted_at;

-- +goose StatementEnd
