-- +goose Up
-- +goose StatementBegin
--
-- Subset 9 of the P1-B-02 extraction. Source: lines 481–499 (vca_rules)
-- + 545–571 (support_tickets/messages) of the inline block.
--
-- VCA rules and the customer-to-SOC support ticket system.
--
-- 1) vca_rules: intrusion zones, tripwires, etc. configured against a
--    specific camera. region is a JSONB polygon; rule_type drives the
--    detection backend. synced/sync_error track whether the rule has
--    been pushed to the camera firmware (Milesight + Hikvision via
--    their respective drivers).
--
-- 2) support_tickets + support_messages: lightweight customer-to-SOC
--    chat. Scoped by organization_id. status enum:
--      open      — customer's most recent message hasn't been answered
--      answered  — supervisor replied; awaiting customer follow-up
--      closed    — explicitly resolved by either party
--    Email fires on every new message in either direction so neither
--    side has to babysit the UI.

CREATE TABLE IF NOT EXISTS vca_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id       UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    rule_type       TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    enabled         BOOLEAN DEFAULT true,
    sensitivity     INT DEFAULT 50,
    region          JSONB NOT NULL DEFAULT '[]',
    direction       TEXT DEFAULT 'both',
    threshold_sec   INT DEFAULT 0,
    schedule        TEXT DEFAULT 'always',
    actions         JSONB DEFAULT '["record","notify"]',
    synced          BOOLEAN DEFAULT false,
    sync_error      TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_vca_rules_camera ON vca_rules(camera_id);

CREATE TABLE IF NOT EXISTS support_tickets (
    id                BIGSERIAL PRIMARY KEY,
    organization_id   TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    site_id           TEXT REFERENCES sites(id) ON DELETE SET NULL,
    created_by        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject           TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'open',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_message_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_message_by   TEXT NOT NULL DEFAULT 'customer'
);
CREATE INDEX IF NOT EXISTS idx_support_tickets_org_status
    ON support_tickets(organization_id, status, last_message_at DESC);
CREATE INDEX IF NOT EXISTS idx_support_tickets_open
    ON support_tickets(last_message_at DESC) WHERE status = 'open';

CREATE TABLE IF NOT EXISTS support_messages (
    id          BIGSERIAL PRIMARY KEY,
    ticket_id   BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    author_id   UUID NOT NULL REFERENCES users(id),
    author_role TEXT NOT NULL,  -- customer | site_manager | soc_supervisor | admin
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_support_messages_ticket
    ON support_messages(ticket_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_support_messages_ticket;
DROP TABLE IF EXISTS support_messages;
DROP INDEX IF EXISTS idx_support_tickets_open;
DROP INDEX IF EXISTS idx_support_tickets_org_status;
DROP TABLE IF EXISTS support_tickets;
DROP INDEX IF EXISTS idx_vca_rules_camera;
DROP TABLE IF EXISTS vca_rules;
-- +goose StatementEnd
