-- +goose Up
-- +goose StatementBegin
--
-- Subset 6 of the P1-B-02 extraction. Source: lines 355–398 of the
-- inline block in cmd/server/main.go.
--
-- Additive columns on security_events. All ADD COLUMN IF NOT EXISTS,
-- all nullable or with safe defaults, so existing rows need no
-- backfill — old rows simply have empty/zero values for the new
-- fields. Three semantic clusters:
--
-- 1) Display / classification (severity / type / description /
--    disposition_label / operator_id / operator_callsign / clip_url):
--    surface-the-event metadata the SOC dispatch UI needs.
--
-- 2) AI annotation (ai_description / ai_threat_level /
--    ai_operator_agreed / ai_was_correct): VLM-emitted attributes
--    used by the active-learning telemetry pipeline.
--
-- 3) UL 827B + TMA-AVS-01 audit (verified_by_user_id / verified_by_callsign /
--    verified_at + avs_factors / avs_score / avs_rubric_version +
--    disposed_by_user_id): structured evidence + dual-operator "four
--    eyes" verification trail. Required for high-severity event review
--    workflows. avs_factors is the operator's raw attestations; avs_score
--    is the deterministic mapping computed by internal/avs at disposition
--    time; rubric_version pins the calculation to a release. The
--    supervisor's verification queue uses idx_security_events_unverified_high
--    to surface high/critical dispositions awaiting a second sign-off.

ALTER TABLE security_events ADD COLUMN IF NOT EXISTS severity TEXT DEFAULT 'medium';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS type TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS disposition_label TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS operator_id TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS operator_callsign TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS clip_url TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS ai_description TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS ai_threat_level TEXT DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS ai_operator_agreed BOOLEAN DEFAULT NULL;
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS ai_was_correct BOOLEAN DEFAULT NULL;

-- UL 827B dual-operator verification ("four eyes" rule)
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS verified_by_user_id UUID;
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS verified_by_callsign TEXT NOT NULL DEFAULT '';
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;

-- TMA-AVS-01 Alarm Validation Score
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS avs_factors JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS avs_score INT NOT NULL DEFAULT 0;
ALTER TABLE security_events ADD COLUMN IF NOT EXISTS avs_rubric_version TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_security_events_avs_score
    ON security_events(avs_score DESC, ts DESC) WHERE avs_score >= 2;

ALTER TABLE security_events ADD COLUMN IF NOT EXISTS disposed_by_user_id UUID;
CREATE INDEX IF NOT EXISTS idx_security_events_unverified_high
    ON security_events(ts DESC) WHERE severity IN ('critical', 'high') AND verified_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_security_events_unverified_high;
ALTER TABLE security_events DROP COLUMN IF EXISTS disposed_by_user_id;
DROP INDEX IF EXISTS idx_security_events_avs_score;
ALTER TABLE security_events DROP COLUMN IF EXISTS avs_rubric_version;
ALTER TABLE security_events DROP COLUMN IF EXISTS avs_score;
ALTER TABLE security_events DROP COLUMN IF EXISTS avs_factors;
ALTER TABLE security_events DROP COLUMN IF EXISTS verified_at;
ALTER TABLE security_events DROP COLUMN IF EXISTS verified_by_callsign;
ALTER TABLE security_events DROP COLUMN IF EXISTS verified_by_user_id;
ALTER TABLE security_events DROP COLUMN IF EXISTS ai_was_correct;
ALTER TABLE security_events DROP COLUMN IF EXISTS ai_operator_agreed;
ALTER TABLE security_events DROP COLUMN IF EXISTS ai_threat_level;
ALTER TABLE security_events DROP COLUMN IF EXISTS ai_description;
ALTER TABLE security_events DROP COLUMN IF EXISTS clip_url;
ALTER TABLE security_events DROP COLUMN IF EXISTS operator_callsign;
ALTER TABLE security_events DROP COLUMN IF EXISTS operator_id;
ALTER TABLE security_events DROP COLUMN IF EXISTS disposition_label;
ALTER TABLE security_events DROP COLUMN IF EXISTS description;
ALTER TABLE security_events DROP COLUMN IF EXISTS type;
ALTER TABLE security_events DROP COLUMN IF EXISTS severity;
-- +goose StatementEnd
