-- +goose Up
-- +goose StatementBegin
--
-- Subset 7 of the P1-B-02 extraction. Source: lines 400–470 of the
-- inline block in cmd/server/main.go.
--
-- Three closely-related SOC dispatch tables and their incremental
-- ALTER additions:
--
-- 1) incidents: groups related alarms from the same site within a
--    correlation window. The SOC dispatch UI shows incidents (not raw
--    alarms) so a single break-in across 4 cameras presents as ONE
--    work-item, not four. status='active' partial index lets the
--    dispatch queue query the open set without scanning history.
--
-- 2) active_alarms: live queue from the NVR detection pipeline. One
--    row per alarm; linked to a parent incident via incident_id (FK
--    with ON DELETE SET DEFAULT '' — losing the incident shouldn't
--    nuke the alarm). The trailing ADD COLUMN block extends the table
--    with AI annotation fields (description, threat level, recommended
--    action, false-positive estimate, detection metadata, PPE
--    violations) and the operator-feedback pair the active-learning
--    pipeline reads back into Qwen fine-tuning.
--
-- 3) shift_handoffs: operator-to-operator shift change records.
--    Captures both pieces of the handoff (departing + arriving),
--    pending alarms, currently-locked sites, and a free-form note.
--    Idx on (to_operator_id, status) drives the "what's waiting for
--    me at shift start" panel.

-- Incidents (declare BEFORE active_alarms so the FK targets exist on
-- a fresh DB build).
CREATE TABLE IF NOT EXISTS incidents (
    id TEXT PRIMARY KEY,
    site_id TEXT NOT NULL DEFAULT '',
    site_name TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'medium',
    status TEXT NOT NULL DEFAULT 'active',
    alarm_count INT NOT NULL DEFAULT 1,
    camera_ids TEXT[] DEFAULT '{}',
    camera_names TEXT[] DEFAULT '{}',
    types TEXT[] DEFAULT '{}',
    latest_type TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    snapshot_url TEXT DEFAULT '',
    clip_url TEXT DEFAULT '',
    first_alarm_ts BIGINT NOT NULL DEFAULT 0,
    last_alarm_ts BIGINT NOT NULL DEFAULT 0,
    sla_deadline_ms BIGINT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_incidents_active ON incidents(site_id, last_alarm_ts) WHERE status = 'active';

-- Active alarms.
CREATE TABLE IF NOT EXISTS active_alarms (
    id TEXT PRIMARY KEY,
    incident_id TEXT DEFAULT '' REFERENCES incidents(id) ON DELETE SET DEFAULT,
    site_id TEXT NOT NULL DEFAULT '',
    site_name TEXT NOT NULL DEFAULT '',
    camera_id TEXT NOT NULL DEFAULT '',
    camera_name TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'high',
    type TEXT NOT NULL DEFAULT 'person_detected',
    description TEXT NOT NULL DEFAULT '',
    snapshot_url TEXT DEFAULT '',
    clip_url TEXT DEFAULT '',
    ts BIGINT NOT NULL DEFAULT 0,
    acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
    claimed_by TEXT DEFAULT '',
    escalation_level INT DEFAULT 0,
    sla_deadline_ms BIGINT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_active_alarms_unacked ON active_alarms(ts) WHERE acknowledged = false;

-- Incremental column adds for active_alarms (these guarded the case of
-- an existing table from before incident_id / AI fields were added).
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS incident_id TEXT DEFAULT '';
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_description TEXT DEFAULT '';
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_threat_level TEXT DEFAULT '';
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_recommended_action TEXT DEFAULT '';
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_false_positive_pct REAL DEFAULT 0;
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_detections JSONB DEFAULT '[]';
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_ppe_violations JSONB DEFAULT '[]';
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_operator_agreed BOOLEAN DEFAULT NULL;
ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_was_correct BOOLEAN DEFAULT NULL;

-- Shift handoffs.
CREATE TABLE IF NOT EXISTS shift_handoffs (
    id BIGSERIAL PRIMARY KEY,
    from_operator_id TEXT NOT NULL DEFAULT '',
    from_operator_callsign TEXT NOT NULL DEFAULT '',
    to_operator_id TEXT NOT NULL DEFAULT '',
    to_operator_callsign TEXT NOT NULL DEFAULT '',
    notes TEXT DEFAULT '',
    site_locks JSONB DEFAULT '[]',
    pending_alarms JSONB DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    accepted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_shift_handoffs_to ON shift_handoffs(to_operator_id, status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_shift_handoffs_to;
DROP TABLE IF EXISTS shift_handoffs;
-- active_alarms drops the columns from the incremental block first so a
-- DOWN→UP cycle re-creates them via the trailing ALTERs.
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_was_correct;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_operator_agreed;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_ppe_violations;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_detections;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_false_positive_pct;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_recommended_action;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_threat_level;
ALTER TABLE active_alarms DROP COLUMN IF EXISTS ai_description;
DROP INDEX IF EXISTS idx_active_alarms_unacked;
DROP TABLE IF EXISTS active_alarms;
DROP INDEX IF EXISTS idx_incidents_active;
DROP TABLE IF EXISTS incidents;
-- +goose StatementEnd
