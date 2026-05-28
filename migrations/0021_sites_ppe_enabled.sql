-- +goose Up
-- P2-C-01: Add ppe_enabled flag to sites so the PPE worker knows which
-- sites have active PPE detection enabled. Defaults false — operators
-- enable per-site via the admin UI or direct DB update until the
-- settings UI is wired (P2-C-02).
ALTER TABLE sites ADD COLUMN IF NOT EXISTS ppe_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial index keeps the worker query fast on large deployments where
-- most sites have PPE disabled.
CREATE INDEX IF NOT EXISTS idx_sites_ppe_enabled ON sites (id) WHERE ppe_enabled = TRUE;

-- +goose Down
DROP INDEX IF EXISTS idx_sites_ppe_enabled;
ALTER TABLE sites DROP COLUMN IF EXISTS ppe_enabled;
