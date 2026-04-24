-- ═══════════════════════════════════════════════════════════════
-- Ironsight Platform Schema Migration
-- Adds organizations, sites, SOPs, operators, security events,
-- evidence shares, and safety findings to the existing NVR schema.
-- ═══════════════════════════════════════════════════════════════

-- Organizations (Customers)
CREATE TABLE IF NOT EXISTS organizations (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    plan            TEXT DEFAULT 'professional',
    contact_name    TEXT DEFAULT '',
    contact_email   TEXT DEFAULT '',
    logo_url        TEXT DEFAULT '',
    features        JSONB DEFAULT '{"vlm_safety": true, "semantic_search": true, "evidence_sharing": true, "global_ai_training": true}',
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Sites (belong to an organization)
CREATE TABLE IF NOT EXISTS sites (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    address             TEXT DEFAULT '',
    organization_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    latitude            DOUBLE PRECISION,
    longitude           DOUBLE PRECISION,
    status              TEXT DEFAULT 'active',
    monitoring_start    TEXT DEFAULT '18:00',
    monitoring_end      TEXT DEFAULT '06:00',
    site_notes          JSONB DEFAULT '[]',
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

-- Add site_id to cameras (nullable — null means unassigned)
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS site_id TEXT REFERENCES sites(id) ON DELETE SET NULL;
ALTER TABLE cameras ADD COLUMN IF NOT EXISTS location TEXT DEFAULT '';

-- Site SOPs (Standard Operating Procedures / Call Trees)
CREATE TABLE IF NOT EXISTS site_sops (
    id              TEXT PRIMARY KEY,
    site_id         TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    title           TEXT NOT NULL,
    category        TEXT DEFAULT 'access',
    priority        TEXT DEFAULT 'normal',
    steps           JSONB DEFAULT '[]',
    contacts        JSONB DEFAULT '[]',
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_by      TEXT DEFAULT ''
);

-- Company Users (Site Managers, Executives — separate from NVR operator users)
CREATE TABLE IF NOT EXISTS company_users (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    email               TEXT UNIQUE NOT NULL,
    phone               TEXT DEFAULT '',
    password_hash       TEXT NOT NULL,
    role                TEXT DEFAULT 'site_manager',
    organization_id     TEXT REFERENCES organizations(id) ON DELETE CASCADE,
    assigned_site_ids   JSONB DEFAULT '[]',
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

-- SOC Operators (security monitoring staff)
CREATE TABLE IF NOT EXISTS operators (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    callsign        TEXT NOT NULL,
    email           TEXT DEFAULT '',
    password_hash   TEXT NOT NULL DEFAULT '',
    status          TEXT DEFAULT 'available',
    active_alarm_id TEXT,
    last_active     BIGINT DEFAULT 0
);

-- Security Events (created from operator alarm dispositions)
CREATE TABLE IF NOT EXISTS security_events (
    id                  TEXT PRIMARY KEY,
    alarm_id            TEXT NOT NULL,
    site_id             TEXT REFERENCES sites(id),
    camera_id           TEXT,
    severity            TEXT DEFAULT 'high',
    type                TEXT DEFAULT 'person_detected',
    description         TEXT DEFAULT '',
    disposition_code    TEXT NOT NULL,
    disposition_label   TEXT DEFAULT '',
    operator_id         TEXT,
    operator_callsign   TEXT DEFAULT '',
    operator_notes      TEXT DEFAULT '',
    action_log          JSONB DEFAULT '[]',
    escalation_depth    INT DEFAULT 0,
    clip_url            TEXT DEFAULT '',
    clip_bookmark_id    TEXT,
    ts                  BIGINT NOT NULL,
    resolved_at         BIGINT NOT NULL,
    viewed_by_customer  BOOLEAN DEFAULT FALSE
);

-- Evidence Shares (shareable links with expiry)
CREATE TABLE IF NOT EXISTS evidence_shares (
    token           TEXT PRIMARY KEY,
    incident_id     TEXT NOT NULL,
    created_by      TEXT DEFAULT '',
    expires_at      TIMESTAMPTZ,
    revoked         BOOLEAN DEFAULT FALSE,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Alarm dispatch queue
CREATE TABLE IF NOT EXISTS alarm_queue (
    id              SERIAL PRIMARY KEY,
    alarm_id        TEXT NOT NULL,
    site_id         TEXT,
    camera_id       TEXT,
    severity        TEXT DEFAULT 'high',
    type            TEXT DEFAULT 'person_detected',
    description     TEXT DEFAULT '',
    ts              BIGINT NOT NULL,
    assigned_to     TEXT,
    status          TEXT DEFAULT 'queued',
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sites_org ON sites(organization_id);
CREATE INDEX IF NOT EXISTS idx_cameras_site ON cameras(site_id);
CREATE INDEX IF NOT EXISTS idx_sops_site ON site_sops(site_id);
CREATE INDEX IF NOT EXISTS idx_company_users_org ON company_users(organization_id);
CREATE INDEX IF NOT EXISTS idx_security_events_site ON security_events(site_id);
CREATE INDEX IF NOT EXISTS idx_security_events_viewed ON security_events(viewed_by_customer);
CREATE INDEX IF NOT EXISTS idx_alarm_queue_status ON alarm_queue(status);
