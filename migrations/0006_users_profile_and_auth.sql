-- +goose Up
-- +goose StatementBegin
--
-- Subset 2 of the P1-B-02 extraction. Source: lines 167–171, 218–230 of
-- the inline block in cmd/server/main.go.
--
-- system_settings discovery + notification columns: feature-flag-ish
-- knobs surfaced in the System tab; nullable defaults so existing rows
-- need no backfill.
--
-- users profile + UL-827B account-lockout columns:
--   display_name / email / phone: identity fields the SSO path populates.
--   organization_id:              tenant scope for customer-side users.
--   assigned_site_ids:            JSONB array of site IDs the user can
--                                 see (empty array = no sites for a
--                                 customer; admin/soc_* ignore this).
--   failed_login_attempts / locked_until: UL 827B account lockout state.
--                                 Reset to 0 on any successful login;
--                                 locked_until rejects auth while
--                                 NOW() < locked_until.
--
-- The UPDATE at the bottom is a one-time role rename: the original
-- shipping role was 'operator'; we renamed to 'soc_operator' to
-- distinguish from on-site security guards (also "operators" in some
-- contexts). Idempotent — no rows match after the first run.

ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS discovery_subnet TEXT DEFAULT '';
ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS discovery_ports TEXT DEFAULT '';
ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS notification_webhook_url TEXT DEFAULT '';
ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS notification_email TEXT DEFAULT '';
ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS notification_triggers TEXT DEFAULT '';

ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS organization_id TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS assigned_site_ids JSONB NOT NULL DEFAULT '[]';
ALTER TABLE users ADD COLUMN IF NOT EXISTS failed_login_attempts INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
ALTER TABLE operators ADD COLUMN IF NOT EXISTS user_id UUID;

-- Idempotent role rename. After the first run, no row matches.
UPDATE users SET role = 'soc_operator' WHERE role = 'operator';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- Reverses the role rename (best-effort — if any user manually flipped
-- to 'soc_operator' under the new scheme, this undoes that too) and
-- drops the added columns.
UPDATE users SET role = 'operator' WHERE role = 'soc_operator';
ALTER TABLE operators DROP COLUMN IF EXISTS user_id;
ALTER TABLE users DROP COLUMN IF EXISTS locked_until;
ALTER TABLE users DROP COLUMN IF EXISTS failed_login_attempts;
ALTER TABLE users DROP COLUMN IF EXISTS assigned_site_ids;
ALTER TABLE users DROP COLUMN IF EXISTS organization_id;
ALTER TABLE users DROP COLUMN IF EXISTS phone;
ALTER TABLE users DROP COLUMN IF EXISTS email;
ALTER TABLE users DROP COLUMN IF EXISTS display_name;
ALTER TABLE system_settings DROP COLUMN IF EXISTS notification_triggers;
ALTER TABLE system_settings DROP COLUMN IF EXISTS notification_email;
ALTER TABLE system_settings DROP COLUMN IF EXISTS notification_webhook_url;
ALTER TABLE system_settings DROP COLUMN IF EXISTS discovery_ports;
ALTER TABLE system_settings DROP COLUMN IF EXISTS discovery_subnet;
-- +goose StatementEnd
