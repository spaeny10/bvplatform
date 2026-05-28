-- +goose Up
-- +goose StatementBegin
--
-- Subset 11 of the P1-B-02 extraction. Source: lines 662–729 +
-- 731–753 of the inline block in cmd/server/main.go.
--
-- Four tables / column families that together close out the UL-827B
-- compliance baseline + the customer notification preferences:
--
-- 1) evidence_share_opens: per-GET access log for share URLs. The
--    chain-of-custody artifact for legal discovery. No FK to
--    evidence_shares — a revoked share should still show its access
--    history.
--
-- 2) revoked_tokens: server-side JWT revocation list. RequireAuth
--    consults this on every authed request. The jti column is the
--    JWT's per-mint unique id (auth.SignToken adds it). expires_at
--    is the original JWT exp — rows can be reaped after that point.
--
-- 3) notification_subscriptions: customer-facing notification prefs.
--    One row per (user, channel, event_type). The trailing INSERT
--    seed-fills defaults: customer + site_manager roles get an
--    email subscription for alarm_disposition events. Users opt
--    OUT, not IN — the monitoring relationship implies they want to
--    know. Idempotent via the NOT EXISTS guard.
--
-- 4) users MFA + password-rotation columns (UL 827B): mfa_enabled,
--    mfa_secret (plaintext at rest — see comment in source for
--    threat-model justification), mfa_recovery_hashes (bcrypt'd
--    one-time codes), password_changed_at (drives forced-rotation
--    flag in the login response).

CREATE TABLE IF NOT EXISTS evidence_share_opens (
    id          BIGSERIAL PRIMARY KEY,
    token       TEXT NOT NULL,
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    referrer    TEXT NOT NULL DEFAULT '',
    opened_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_evidence_share_opens_token
    ON evidence_share_opens(token, opened_at DESC);

CREATE TABLE IF NOT EXISTS revoked_tokens (
    jti        TEXT PRIMARY KEY,
    user_id    UUID,
    revoked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires_at
    ON revoked_tokens(expires_at);

CREATE TABLE IF NOT EXISTS notification_subscriptions (
    id           BIGSERIAL PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel      TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    severity_min TEXT NOT NULL DEFAULT 'low',
    site_ids     JSONB,
    quiet_start  TEXT DEFAULT '',
    quiet_end    TEXT DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, channel, event_type)
);
CREATE INDEX IF NOT EXISTS idx_notification_subs_user
    ON notification_subscriptions(user_id) WHERE enabled = true;

-- Seed default subscriptions for any customer/site_manager user
-- that doesn't already have one. Idempotent.
INSERT INTO notification_subscriptions (user_id, channel, event_type, severity_min)
SELECT u.id, 'email', 'alarm_disposition', 'low'
FROM users u
WHERE u.role IN ('customer', 'site_manager')
  AND COALESCE(u.email, '') <> ''
  AND NOT EXISTS (
    SELECT 1 FROM notification_subscriptions s
    WHERE s.user_id = u.id AND s.channel = 'email' AND s.event_type = 'alarm_disposition'
  );

-- MFA + password rotation (UL 827B)
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_secret  TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_recovery_hashes JSONB NOT NULL DEFAULT '[]';
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
--
-- The notification_subscriptions seed INSERT and the password_changed_at
-- timestamp can't be cleanly reversed (the original NULL vs. NOW()
-- distinction is lost). Acceptable — DOWN→UP re-seeds from the same
-- source data.
ALTER TABLE users DROP COLUMN IF EXISTS password_changed_at;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_recovery_hashes;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_secret;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_enabled;
DROP INDEX IF EXISTS idx_notification_subs_user;
DROP TABLE IF EXISTS notification_subscriptions;
DROP INDEX IF EXISTS idx_revoked_tokens_expires_at;
DROP TABLE IF EXISTS revoked_tokens;
DROP INDEX IF EXISTS idx_evidence_share_opens_token;
DROP TABLE IF EXISTS evidence_share_opens;
-- +goose StatementEnd
