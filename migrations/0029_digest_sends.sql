-- +goose Up
-- +goose StatementBegin

-- digest_sends records one row per (org, scope, week_start) after a
-- successful weekly digest send. The scheduler checks this table before
-- composing each org's digest so a worker restart mid-send-window does
-- not re-send the week (durable idempotency vs. the monthly summary's
-- in-memory lastSentYM approach).
--
-- org_id  — TEXT to match organizations.id (slug PK, TEXT convention).
-- scope   — reserved for future DIGEST_SCOPE=site expansion; for the
--           initial org-level implementation this is always 'org'.
-- period_start — Monday 00:00:00 UTC of the ISO week that was digested.
-- sent_at — wall-clock time the send completed (informational; NOT the
--           period boundary).
--
-- The unique constraint on (org_id, scope, period_start) is the
-- idempotency key: INSERT ... ON CONFLICT DO NOTHING ensures two racing
-- workers (or a restart during the send window) only record one row per
-- org per week.

CREATE TABLE IF NOT EXISTS digest_sends (
    id           BIGSERIAL PRIMARY KEY,
    org_id       TEXT        NOT NULL,
    scope        TEXT        NOT NULL DEFAULT 'org',
    period_start TIMESTAMPTZ NOT NULL,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT digest_sends_unique UNIQUE (org_id, scope, period_start)
);

-- Index on (org_id, period_start) for the per-org idempotency check.
CREATE INDEX IF NOT EXISTS idx_digest_sends_org_period
    ON digest_sends (org_id, period_start DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_digest_sends_org_period;
DROP TABLE IF EXISTS digest_sends;

-- +goose StatementEnd
