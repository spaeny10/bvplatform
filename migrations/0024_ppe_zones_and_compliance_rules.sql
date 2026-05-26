-- migrations/0024_ppe_zones_and_compliance_rules.sql
-- P2-C-04: PPE/safety zone polygons + compliance rule bindings.
--
-- PPE zones are Ironsight-server-side polygons used to spatially filter YOLO
-- violation detections. They are NEVER pushed to camera firmware (unlike
-- vca_rules which are synced via ONVIF). Coordinates: normalized floats
-- 0.0-1.0, same convention as vca_rules.region.
--
-- FK type convention (from migration 0022 precedent):
--   organization_id → TEXT  (organizations.id is TEXT)
--   site_id         → TEXT  (sites.id is TEXT)
--   camera_id       → UUID  (cameras.id is UUID)

-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS ppe_zones (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id TEXT        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    camera_id       UUID        NOT NULL REFERENCES cameras(id)       ON DELETE CASCADE,
    site_id         TEXT                 REFERENCES sites(id)         ON DELETE SET NULL,

    zone_type       TEXT        NOT NULL
                        CHECK (zone_type IN ('work_area', 'no_go', 'ppe_required', 'ppe_optional')),
    name            TEXT        NOT NULL DEFAULT '',
    region          JSONB       NOT NULL DEFAULT '[]',
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    notes           TEXT,

    created_by      UUID                 REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Primary lookup: list enabled zones for a camera (worker + UI)
CREATE INDEX IF NOT EXISTS idx_ppe_zones_camera
    ON ppe_zones (camera_id)
    WHERE enabled = TRUE;

-- Tenant-scope scan (admin list, compliance dashboard)
CREATE INDEX IF NOT EXISTS idx_ppe_zones_org
    ON ppe_zones (organization_id, camera_id);

-- ---------------------------------------------------------------------------
-- Compliance rules: bind a PPE zone to a PPE requirement or no-go rule.
--
-- camera_id is nullable — NULL means "site-wide, applies to all cameras at
-- site_id". The worker queries: camera_id = $cam OR camera_id IS NULL (where
-- site_id also matches).
--
-- ppe_classes: JSONB array of YOLO violation class prefixes this rule requires
-- to be absent. E.g. ["no-hat", "no-vest"]. Ignored for no_go rules (presence
-- alone is the violation).
--
-- time_window: reserved for restricted_hours (deferred to future task). NULL
-- means always active. Suggested format when implemented:
--   {"days":[1,2,3,4,5], "start":"18:00", "end":"06:00", "tz":"America/Chicago"}

CREATE TABLE IF NOT EXISTS compliance_rules (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id TEXT        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    site_id         TEXT                 REFERENCES sites(id)         ON DELETE SET NULL,
    camera_id       UUID                 REFERENCES cameras(id)       ON DELETE CASCADE,
    zone_id         UUID        NOT NULL REFERENCES ppe_zones(id)     ON DELETE CASCADE,

    rule_type       TEXT        NOT NULL
                        CHECK (rule_type IN ('ppe_required', 'no_go')),
    ppe_classes     JSONB       NOT NULL DEFAULT '[]',
    time_window     JSONB,                    -- NULL = always active; reserved for future use
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    notes           TEXT,

    created_by      UUID                 REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Worker query: active rules for a specific camera OR site-wide rules
CREATE INDEX IF NOT EXISTS idx_compliance_rules_camera_site
    ON compliance_rules (camera_id, site_id, organization_id)
    WHERE enabled = TRUE;

-- Zone lookup: which rules reference a zone (for cascade-delete UI warning)
CREATE INDEX IF NOT EXISTS idx_compliance_rules_zone
    ON compliance_rules (zone_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_compliance_rules_zone;
DROP INDEX IF EXISTS idx_compliance_rules_camera_site;
DROP TABLE IF EXISTS compliance_rules;
DROP INDEX IF EXISTS idx_ppe_zones_org;
DROP INDEX IF EXISTS idx_ppe_zones_camera;
DROP TABLE IF EXISTS ppe_zones;
-- +goose StatementEnd
