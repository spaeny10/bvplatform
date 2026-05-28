-- +goose Up
-- +goose StatementBegin
--
-- Subset 14 of the P1-B-02 extraction. Source: lines 839–865 of the
-- inline block in cmd/server/main.go.
--
-- Multi-tenant integrity: add organization_id directly on tables
-- that originally only had site_id, then backfill from sites where
-- the new column is still empty.
--
-- Why direct organization_id instead of just joining sites.org_id?
-- Two reasons:
-- 1) Perf: queries that scope by tenant don't have to JOIN through
--    sites — single-column equality + index hit.
-- 2) Defense in depth: a buggy join that forgets the org-scope
--    predicate would silently cross tenants. With an explicit column
--    on the row, the handler can WHERE organization_id = ? directly
--    and the join becomes a redundant safety net.
--
-- The backfill is idempotent: only updates rows that haven't been
-- backfilled (organization_id = '').

ALTER TABLE incidents       ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE active_alarms   ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_shares ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE vlm_label_jobs  ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_incidents_org       ON incidents(organization_id, last_alarm_ts DESC);
CREATE INDEX IF NOT EXISTS idx_active_alarms_org   ON active_alarms(organization_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_evidence_shares_org ON evidence_shares(organization_id);

-- Backfill: copy organization_id from sites where it's still empty.
UPDATE incidents i
   SET organization_id = COALESCE(s.organization_id, '')
  FROM sites s
 WHERE i.site_id = s.id AND i.organization_id = '';
UPDATE active_alarms a
   SET organization_id = COALESCE(s.organization_id, '')
  FROM sites s
 WHERE a.site_id = s.id AND a.organization_id = '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_evidence_shares_org;
DROP INDEX IF EXISTS idx_active_alarms_org;
DROP INDEX IF EXISTS idx_incidents_org;
ALTER TABLE vlm_label_jobs  DROP COLUMN IF EXISTS organization_id;
ALTER TABLE evidence_shares DROP COLUMN IF EXISTS organization_id;
ALTER TABLE active_alarms   DROP COLUMN IF EXISTS organization_id;
ALTER TABLE incidents       DROP COLUMN IF EXISTS organization_id;
-- +goose StatementEnd
