package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"ironsight/internal/ai"
	"ironsight/internal/indexer"
	"ironsight/internal/api"
	authpkg "ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/detection"
	"ironsight/internal/export"
	msdriver "ironsight/internal/milesight"
	"ironsight/internal/notify"
	"ironsight/internal/onvif"
	"ironsight/internal/recording"
	"ironsight/internal/streaming"
	"ironsight/migrations"
	"strings"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("============================================")
	log.Println("  ONVIF Tool - Starting Server")
	log.Println("============================================")

	// Load configuration
	cfg := config.Load()

	// Ensure storage directories exist
	for _, dir := range []string{cfg.StoragePath, cfg.HLSPath, cfg.ExportPath, cfg.ThumbnailPath} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("[FATAL] Cannot create directory %s: %v", dir, err)
		}
	}

	// Connect to database
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database connection failed: %v", err)
	}
	defer db.Close()

	// Apply goose-tracked migrations before any other DB-touching startup
	// runs. The 0001_baseline.sql migration is idempotent against fred's
	// already-populated schema (every CREATE / ADD CONSTRAINT / TRIGGER
	// is guarded), so on fred this just records "version 1 applied" in
	// goose_db_version and changes nothing else. On a fresh DB it builds
	// the full schema from scratch. Subsequent migrations (P1-B-02 onward)
	// will land in migrations/000N_*.sql and be picked up automatically
	// via the //go:embed directive in the migrations package.
	//
	// We bridge the existing pgxpool to database/sql via pgx's stdlib
	// helper so goose (which is database/sql-only) shares the same pool
	// rather than opening a second connection. The bridge *sql.DB is
	// closed before we exit so we don't leak the wrapping handles.
	gooseDB := stdlib.OpenDBFromPool(db.Pool)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("[FATAL] goose.SetDialect: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpContext(context.Background(), gooseDB, "."); err != nil {
		gooseDB.Close()
		log.Fatalf("[FATAL] goose.Up: %v", err)
	}
	gooseDB.Close()
	log.Println("[MIGRATIONS] goose up applied; see goose_db_version for current version")

	// Auto-migrate: add new columns if they don't exist
	// NOTE: this inline block is the legacy path scheduled for extraction
	// in P1-B-02. It runs AFTER goose so any column/index it adds is also
	// present on a fresh-from-baseline DB. Once P1-B-02 lands, every ALTER
	// here becomes a numbered migration file and this block is deleted.
	_, err = db.Pool.Exec(context.Background(), `
		-- Camera device class: 'continuous' = traditional always-on IP
		-- camera (RTSP stream + ONVIF subscription, current behavior),
		-- 'sense_pushed' = battery-powered PIR-triggered cameras like
		-- the Milesight SC4xx Sense series that POST event payloads to
		-- our webhook instead of streaming. Default 'continuous' so
		-- the migration is a no-op for existing rows.
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS device_class TEXT DEFAULT 'continuous';
		-- Per-camera secret for the inbound webhook URL we hand to the
		-- camera's Alarm Server config. Only populated for sense_pushed
		-- cameras; null for continuous.
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS sense_webhook_token TEXT;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_cameras_sense_token
			ON cameras(sense_webhook_token) WHERE sense_webhook_token IS NOT NULL;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS recording_mode TEXT DEFAULT 'continuous';
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS pre_buffer_sec INT DEFAULT 10;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS post_buffer_sec INT DEFAULT 30;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS recording_triggers TEXT DEFAULT 'motion,object';
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS has_ptz BOOLEAN DEFAULT false;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS events_enabled BOOLEAN DEFAULT true;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS audio_enabled BOOLEAN DEFAULT true;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS camera_group TEXT DEFAULT '';
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS schedule TEXT DEFAULT '';
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS privacy_mask BOOLEAN DEFAULT false;
		ALTER TABLE segments ADD COLUMN IF NOT EXISTS has_audio BOOLEAN DEFAULT false;
		CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'operator',
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		);
		CREATE TABLE IF NOT EXISTS storage_locations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			label TEXT NOT NULL,
			path TEXT NOT NULL,
			purpose TEXT NOT NULL DEFAULT 'recordings',
			retention_days INT DEFAULT 3,
			max_gb INT DEFAULT 0,
			priority INT DEFAULT 0,
			enabled BOOLEAN DEFAULT true,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS discovery_subnet TEXT DEFAULT '';
		ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS discovery_ports TEXT DEFAULT '';
		ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS notification_webhook_url TEXT DEFAULT '';
		ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS notification_email TEXT DEFAULT '';
		ALTER TABLE system_settings ADD COLUMN IF NOT EXISTS notification_triggers TEXT DEFAULT '';
		CREATE TABLE IF NOT EXISTS speakers (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			onvif_address TEXT NOT NULL,
			username TEXT DEFAULT '',
			password TEXT DEFAULT '',
			rtsp_uri TEXT DEFAULT '',
			zone TEXT DEFAULT '',
			status TEXT DEFAULT 'offline',
			manufacturer TEXT DEFAULT '',
			model TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS audio_messages (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT 'custom',
			file_name TEXT NOT NULL,
			duration REAL DEFAULT 0,
			file_size BIGINT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id BIGSERIAL PRIMARY KEY,
			user_id UUID,
			username TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			target_type TEXT DEFAULT '',
			target_id TEXT DEFAULT '',
			details TEXT DEFAULT '',
			ip_address TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS bookmarks (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			camera_id UUID REFERENCES cameras(id) ON DELETE CASCADE,
			event_time TIMESTAMPTZ NOT NULL,
			label TEXT NOT NULL,
			notes TEXT DEFAULT '',
			severity TEXT DEFAULT 'info',
			created_by UUID,
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS map_x REAL DEFAULT 0;
		ALTER TABLE cameras ADD COLUMN IF NOT EXISTS map_y REAL DEFAULT 0;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';
		ALTER TABLE users ADD COLUMN IF NOT EXISTS organization_id TEXT;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS assigned_site_ids JSONB NOT NULL DEFAULT '[]';
		-- UL 827B: account lockout state. failed_login_attempts tracks
		-- consecutive failures; resets to 0 on any successful login.
		-- locked_until holds a future timestamp after the threshold is
		-- breached; the login handler rejects auth while NOW() < locked_until.
		ALTER TABLE users ADD COLUMN IF NOT EXISTS failed_login_attempts INT NOT NULL DEFAULT 0;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
		ALTER TABLE operators ADD COLUMN IF NOT EXISTS user_id UUID;
		UPDATE users SET role='soc_operator' WHERE role='operator';

		-- Device site assignment for speakers
		ALTER TABLE speakers ADD COLUMN IF NOT EXISTS site_id TEXT REFERENCES sites(id) ON DELETE SET NULL;
		ALTER TABLE speakers ADD COLUMN IF NOT EXISTS location TEXT DEFAULT '';

		-- Device assignment history: tracks when each camera/speaker was at each site.
		-- Used for temporal data isolation (Site B can't see recordings from Site A period).
		CREATE TABLE IF NOT EXISTS device_assignments (
			id BIGSERIAL PRIMARY KEY,
			device_type TEXT NOT NULL,
			device_id TEXT NOT NULL,
			site_id TEXT NOT NULL,
			location_label TEXT DEFAULT '',
			assigned_at TIMESTAMPTZ DEFAULT NOW(),
			removed_at TIMESTAMPTZ
		);
		CREATE INDEX IF NOT EXISTS idx_device_assignments_device ON device_assignments(device_type, device_id);
		CREATE INDEX IF NOT EXISTS idx_device_assignments_site ON device_assignments(site_id);
		CREATE INDEX IF NOT EXISTS idx_device_assignments_active ON device_assignments(device_id, removed_at) WHERE removed_at IS NULL;

		-- Event-to-segment linkage: when a camera event fires we also record the
		-- video segment file that contains the event moment so the UI can deep-
		-- link an event row straight to the right clip. Nullable because events
		-- can arrive while recording is down / not configured.
		-- No FK to segments(id) here: both events and segments are TimescaleDB
		-- hypertables, and Timescale rejects FKs between hypertables. Referential
		-- integrity isn't load-bearing for this column — the backfill below
		-- resolves it via camera_id + time range, and application code nulls out
		-- stale references on segment deletion. A FK constraint would break the
		-- migration outright on any fresh (non-grandfathered) database.
		ALTER TABLE events ADD COLUMN IF NOT EXISTS segment_id BIGINT;
		CREATE INDEX IF NOT EXISTS idx_events_segment ON events(segment_id);

		-- Backfill: one-time population of segment_id for events that existed
		-- before the column was added. Idempotent — only touches NULL rows,
		-- which stay NULL if no covering segment exists (recording was down).
		UPDATE events e
		SET segment_id = (
			SELECT s.id FROM segments s
			WHERE s.camera_id = e.camera_id
			  AND s.start_time <= e.event_time
			  AND s.end_time   >= e.event_time
			ORDER BY s.start_time DESC
			LIMIT 1
		)
		WHERE e.segment_id IS NULL;

		-- VLM-generated descriptions of recording segments. Populated by the
		-- background indexer (internal/indexer) during idle hours; the segment
		-- is gated by YOLO first so empty scenes don't burn VLM time. Enables
		-- Postgres full-text and tag search over every minute of footage.
		-- No FK to segments(id): segments is a Timescale hypertable, and
		-- hypertables can't be the target of a FK constraint (the primary key
		-- isn't enforced across chunks). Retention worker is responsible for
		-- deleting descriptions when segments expire — see internal/retention.
		CREATE TABLE IF NOT EXISTS segment_descriptions (
			segment_id       BIGINT       PRIMARY KEY,
			camera_id        UUID         NOT NULL,
			description      TEXT         NOT NULL DEFAULT '',
			tags             TEXT[]       NOT NULL DEFAULT '{}',
			activity_level   TEXT         NOT NULL DEFAULT 'none',
			entities         JSONB        NOT NULL DEFAULT '[]',
			detections       JSONB        NOT NULL DEFAULT '[]',
			indexer_version  INT          NOT NULL DEFAULT 1,
			analysis_ms      INT          NOT NULL DEFAULT 0,
			indexed_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_segment_descriptions_camera
			ON segment_descriptions(camera_id, indexed_at DESC);
		CREATE INDEX IF NOT EXISTS idx_segment_descriptions_tags
			ON segment_descriptions USING GIN(tags);
		-- GIN on the tsvector expression lets /api/search/semantic rank results
		-- with to_tsquery() and english stemming (so "runs"/"running"/"ran" match).
		CREATE INDEX IF NOT EXISTS idx_segment_descriptions_fts
			ON segment_descriptions USING GIN(to_tsvector('english', description));
		CREATE INDEX IF NOT EXISTS idx_segment_descriptions_activity
			ON segment_descriptions(activity_level) WHERE activity_level != 'none';

		-- Deterrence audit log: every time an operator fires a camera's
		-- strobe / siren / alarm relay, a row lands here. This is a legal
		-- must-have — a siren going off on someone's property needs a
		-- precise "who, when, why" trail if it's ever challenged.
		CREATE TABLE IF NOT EXISTS deterrence_audits (
			id           BIGSERIAL   PRIMARY KEY,
			user_id      TEXT        NOT NULL DEFAULT '',
			username     TEXT        NOT NULL DEFAULT '',
			role         TEXT        NOT NULL DEFAULT '',
			camera_id    UUID        NOT NULL,
			camera_name  TEXT        NOT NULL DEFAULT '',
			action       TEXT        NOT NULL,        -- strobe | siren | both | alarm_out
			duration_sec INT         NOT NULL DEFAULT 0,
			reason       TEXT        NOT NULL DEFAULT '',  -- operator-entered justification
			alarm_id     TEXT        NOT NULL DEFAULT '',  -- which alarm triggered it, if any
			success      BOOLEAN     NOT NULL DEFAULT true,
			error        TEXT        NOT NULL DEFAULT '',
			fired_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ip           TEXT        NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_deterrence_audits_camera
			ON deterrence_audits(camera_id, fired_at DESC);
		CREATE INDEX IF NOT EXISTS idx_deterrence_audits_user
			ON deterrence_audits(user_id, fired_at DESC);

		-- Playback audit log: every access to a recording segment or playback
		-- endpoint writes a row so we can answer "who watched what, when"
		-- for compliance / discovery. Append-only; trimmed by retention job.
		CREATE TABLE IF NOT EXISTS playback_audits (
			id BIGSERIAL PRIMARY KEY,
			user_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT '',
			camera_id UUID,
			segment_id BIGINT,
			event_id BIGINT,
			endpoint TEXT NOT NULL DEFAULT '',
			accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ip TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_playback_audits_user
			ON playback_audits(user_id, accessed_at DESC);
		CREATE INDEX IF NOT EXISTS idx_playback_audits_camera
			ON playback_audits(camera_id, accessed_at DESC);

		-- Missing security_events columns (added incrementally)
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

		-- UL 827B dual-operator verification ("four-eyes rule"). High-
		-- severity dispositions that get escalated to law enforcement
		-- need a second operator's sign-off. We capture verifier
		-- identity + timestamp here; the supervisor endpoint enforces
		-- "must not be the same user as the disposing operator." This
		-- is also the structured-evidence trail TMA-AVS-01 wants for
		-- the "video verified by SOC operator" factor.
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS verified_by_user_id UUID;
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS verified_by_callsign TEXT NOT NULL DEFAULT '';
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;

		-- TMA-AVS-01 Alarm Validation Score capture. Factors is the raw
		-- attestation set the operator filled in at disposition; score is
		-- the deterministic mapping computed by internal/avs at the time.
		-- We store both (rather than just factors) so a list-events query
		-- doesn't have to re-run the scoring function on every row, and
		-- so an auditor can see the score that PSAP actually received
		-- alongside the factors that produced it. rubric_version pins the
		-- score to a specific algorithm release.
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS avs_factors JSONB NOT NULL DEFAULT '{}'::jsonb;
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS avs_score INT NOT NULL DEFAULT 0;
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS avs_rubric_version TEXT NOT NULL DEFAULT '';
		-- Index for quick "show me all critical-score dispositions in the
		-- last hour" queries; common on the supervisor dashboard.
		CREATE INDEX IF NOT EXISTS idx_security_events_avs_score
			ON security_events(avs_score DESC, ts DESC) WHERE avs_score >= 2;
		-- The user who originally dispositioned the event. We had
		-- operator_callsign already but not the user_id, which the
		-- self-verification check needs to compare against.
		ALTER TABLE security_events ADD COLUMN IF NOT EXISTS disposed_by_user_id UUID;
		CREATE INDEX IF NOT EXISTS idx_security_events_unverified_high
			ON security_events(ts DESC) WHERE severity IN ('critical', 'high') AND verified_at IS NULL;

		-- Incidents: group related alarms from the same site within a correlation window.
		-- The SOC dispatch queue shows incidents rather than individual alarms.
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

		-- Active alarms: live queue from the NVR detection pipeline.
		-- One row per alarm; linked to a parent incident for grouping.
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
		-- Migration: add incident_id if table already exists
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS incident_id TEXT DEFAULT '';
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_description TEXT DEFAULT '';
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_threat_level TEXT DEFAULT '';
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_recommended_action TEXT DEFAULT '';
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_false_positive_pct REAL DEFAULT 0;
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_detections JSONB DEFAULT '[]';
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_ppe_violations JSONB DEFAULT '[]';
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_operator_agreed BOOLEAN DEFAULT NULL;
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS ai_was_correct BOOLEAN DEFAULT NULL;

		-- Shift handoffs: operator-to-operator shift change records.
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

		-- Site feature mode: controls which product tier is active per site.
		-- "security_only"       = cameras/recordings/SOC events/security reports
		-- "security_and_safety" = + PPE compliance, OSHA, vLM safety engine
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS feature_mode TEXT NOT NULL DEFAULT 'security_and_safety';

		-- Monitoring schedule (JSON array of time windows) and snooze state (JSON object)
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS monitoring_schedule JSONB DEFAULT '[]';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS snooze JSONB DEFAULT NULL;

		-- VCA (Video Content Analytics) rules: intrusion zones, tripwires, etc.
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

		-- Recording / retention settings live at the site level. These were
		-- previously per-camera columns on cameras. Keeping the old columns
		-- in the DB for one release as a rollback cushion, but the engine
		-- reads from sites after this migration. Backfill below copies a
		-- reasonable default from the most-recently-updated camera on each
		-- site on first run.
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS retention_days       INT  DEFAULT 3;
		-- Tighten the default for any future site row inserted without
		-- an explicit retention. Existing rows are unaffected; an admin
		-- bumps a site to a higher tier (7/14/30/60/90) per contract.
		ALTER TABLE sites ALTER COLUMN retention_days SET DEFAULT 3;
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_mode       TEXT DEFAULT 'continuous';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS pre_buffer_sec       INT  DEFAULT 10;
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS post_buffer_sec      INT  DEFAULT 30;
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_triggers   TEXT DEFAULT 'motion,object';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_schedule   TEXT DEFAULT '';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_backfilled BOOLEAN DEFAULT false;

		-- Customer-maintained on-site contact list. Distinct from the
		-- SOC-side site_sops.contacts (operators' call tree) — this is
		-- what the site owner / site manager edits themselves to keep
		-- their own contact info current. Stored as JSONB array of
		-- {name, role, phone, email, notify_on_alarm, notes}; the
		-- portal UI is the source of truth for customer-facing edits,
		-- and the SOC can mirror the values into a SOP's call tree
		-- when running through escalation.
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS customer_contacts JSONB NOT NULL DEFAULT '[]';

		-- Lightweight customer-to-SOC support ticket system. The customer
		-- creates a ticket from the portal; SOC supervisors / admins
		-- respond from the /reports surface. Email fires on every new
		-- message in either direction so neither side has to babysit
		-- the UI.
		--
		-- Scoped by organization_id: a customer can only see their own
		-- org's tickets. site_id is optional — most tickets attach to a
		-- specific site, but "I have a billing question" doesn't.
		--
		-- status enum: open / answered / closed.
		--   open      — customer's most recent message hasn't been
		--               responded to yet
		--   answered  — supervisor replied; ticket awaits customer
		--               follow-up or closure
		--   closed    — explicitly resolved by either party
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
			author_role TEXT NOT NULL,  -- 'customer' / 'site_manager' / 'soc_supervisor' / 'admin'
			body        TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_support_messages_ticket
			ON support_messages(ticket_id, created_at);

		-- One-time backfill: for each site whose recording settings are still
		-- at the default, adopt values from the most-recently-updated camera
		-- on that site. The recording_backfilled flag prevents re-running
		-- if an operator later resets a site to defaults on purpose.
		UPDATE sites s
		SET retention_days      = COALESCE(bf.retention_days, s.retention_days),
		    recording_mode      = COALESCE(NULLIF(bf.recording_mode, ''), s.recording_mode),
		    pre_buffer_sec      = COALESCE(bf.pre_buffer_sec, s.pre_buffer_sec),
		    post_buffer_sec     = COALESCE(bf.post_buffer_sec, s.post_buffer_sec),
		    recording_triggers  = COALESCE(NULLIF(bf.recording_triggers, ''), s.recording_triggers),
		    recording_schedule  = COALESCE(NULLIF(bf.schedule, ''), s.recording_schedule),
		    recording_backfilled = true
		FROM (
		    SELECT DISTINCT ON (site_id)
		           site_id, retention_days, recording_mode, pre_buffer_sec,
		           post_buffer_sec, recording_triggers, schedule
		    FROM cameras
		    WHERE site_id IS NOT NULL AND site_id <> ''
		    ORDER BY site_id, updated_at DESC NULLS LAST
		) bf
		WHERE s.id = bf.site_id AND s.recording_backfilled = false;

		-- Export worker: claim tracking. started_at records when a worker
		-- atomically claimed a job (status pending→processing), so on startup
		-- we can detect stuck jobs (status=processing for > N minutes) left
		-- behind by a crashed worker and requeue them. Partial index makes
		-- the poll-for-next-pending query touch only the short queue, not
		-- the full historical exports table.
		ALTER TABLE exports ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
		CREATE INDEX IF NOT EXISTS idx_exports_pending
			ON exports (created_at) WHERE status = 'pending';
		CREATE INDEX IF NOT EXISTS idx_exports_processing
			ON exports (started_at) WHERE status = 'processing';

		-- ── SOC audit-trail upgrades ────────────────────────────────────
		-- These columns and triggers turn the "demo-grade" audit surface
		-- into something we can defend in a customer conversation or
		-- discovery request. Each change is idempotent; the whole block is
		-- safe to re-run on an existing database.

		-- Phoneticizable per-alarm short code. The UUID-ish PK is fine in
		-- URLs but unusable over a radio or phone bridge ("ack alarm
		-- a-7-b-3-d-9-1-e-dash..."). ALM-YYMMDD-NNNN gives a 4-digit
		-- daily sequence, readable aloud, and still unique. Legacy rows
		-- stay NULL — the generator backfills on next write if needed.
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS alarm_code TEXT;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_active_alarms_code
			ON active_alarms(alarm_code) WHERE alarm_code IS NOT NULL;

		-- Forensic linkage: which detection event actually fired this
		-- alarm? Previously recoverable only by (camera_id, ts) join,
		-- which is ambiguous when multiple events land on the same
		-- second. BIGINT intentionally has no FK — events is a Timescale
		-- hypertable and can't be the target of a FK constraint.
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS triggering_event_id BIGINT;
		CREATE INDEX IF NOT EXISTS idx_active_alarms_event
			ON active_alarms(triggering_event_id) WHERE triggering_event_id IS NOT NULL;

		-- UL 827B SLA tracking. We already track sla_deadline_ms (the
		-- deadline the alarm was created with); these columns capture the
		-- *actual* response so a reviewer can compute "did we meet our
		-- SLA?" with one SELECT instead of joining audit_log to alarms by
		-- timestamp. acknowledged_by_callsign denormalizes the operator's
		-- callsign at ack time so the report stays meaningful even if the
		-- operator's callsign changes later.
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMPTZ;
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS acknowledged_by_user_id UUID;
		ALTER TABLE active_alarms ADD COLUMN IF NOT EXISTS acknowledged_by_callsign TEXT NOT NULL DEFAULT '';
		-- Index on (acknowledged_at, ts) so the SLA report can scan a date
		-- range without touching un-acked alarms.
		CREATE INDEX IF NOT EXISTS idx_active_alarms_ack_window
			ON active_alarms(acknowledged_at, ts) WHERE acknowledged_at IS NOT NULL;

		-- Polymorphic target on audit_log. target_type is one of a small
		-- enum ("camera", "site", "user", "alarm", etc.) and target_id is
		-- whatever format that entity uses. Lets us answer "who touched
		-- this camera" with a single indexed query instead of LIKE-scans
		-- over the route path.
		ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_type TEXT NOT NULL DEFAULT '';
		ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_id   TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_audit_log_target
			ON audit_log(target_type, target_id, created_at DESC)
			WHERE target_id <> '';

		-- Evidence share access log. One row per GET of a share URL.
		-- This is the chain-of-custody bit courts want: not just who
		-- generated the share, but every IP/agent that actually opened
		-- it, timestamped. No FK to evidence_shares — a revoked share
		-- should still show its access history.
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

		-- UL 827B server-side logout / token revocation. The auth layer
		-- adds a unique jti to every signed JWT; logout inserts that jti
		-- here, and RequireAuth checks for membership before honoring a
		-- token. expires_at is the original JWT exp — once it's past, the
		-- row is harmless (token is invalid anyway) and a future cleanup
		-- job can reclaim space.
		CREATE TABLE IF NOT EXISTS revoked_tokens (
			jti        TEXT PRIMARY KEY,
			user_id    UUID,
			revoked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires_at
			ON revoked_tokens(expires_at);

		-- Customer notification subscriptions. One row per (user, channel,
		-- event_type) combo so a user can independently opt into "email
		-- on critical alarms" + "sms on any alarm at site CS-547" + "no
		-- monthly summary." event_type is a small enum:
		--   alarm_disposition: per-event email/sms when SOC closes an alarm
		--   monthly_summary:   the auto-emailed monthly performance report
		-- channel is "email" or "sms"; future "push" / "webhook" slot in.
		-- severity_min lets a user say "only critical+high" — events
		-- below the threshold are filtered before send.
		-- site_ids: NULL = all sites visible to user; otherwise array of
		-- specific site ids the subscription applies to.
		-- quiet hours: HH:MM in user's display timezone (stored UTC, the
		-- match logic does the conversion). NULL = always on.
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
		-- For each customer/site_manager user that doesn't already have
		-- alarm_disposition subscriptions, seed defaults: email on any
		-- alarm at any of their sites. Users opt OUT, not IN — the
		-- monitoring relationship implies they want to know.
		INSERT INTO notification_subscriptions (user_id, channel, event_type, severity_min)
		SELECT u.id, 'email', 'alarm_disposition', 'low'
		FROM users u
		WHERE u.role IN ('customer', 'site_manager')
		  AND COALESCE(u.email, '') <> ''
		  AND NOT EXISTS (
		    SELECT 1 FROM notification_subscriptions s
		    WHERE s.user_id = u.id AND s.channel = 'email' AND s.event_type = 'alarm_disposition'
		  );

		-- UL 827B multi-factor authentication. TOTP only for the first
		-- pass — WebAuthn / hardware keys can layer in later. We keep
		-- the secret in plaintext at rest because the threat model here
		-- is database-row exfiltration via a compromised app role, and a
		-- column-level encryption that uses a key the same app can read
		-- doesn't change that calculus. Production deployments that need
		-- KMS-managed secrets (AWS KMS, HashiCorp Vault) can layer that
		-- on without changing the schema.
		ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_enabled BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_secret  TEXT NOT NULL DEFAULT '';
		-- One-time recovery codes. Each entry is a bcrypt hash of the
		-- code so a leak of this column doesn't immediately bypass MFA.
		-- The login flow checks each hash, marks the matching one used,
		-- and leaves the rest in place for future recovery attempts.
		ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_recovery_hashes JSONB NOT NULL DEFAULT '[]';

		-- UL 827B password rotation. password_changed_at is the source of
		-- truth for "is this password too old"; the login handler reads it
		-- and decides whether to flag the response with a forced-change
		-- indicator. NOW() default keeps existing rows valid for 180 days
		-- from the moment the migration runs (an operator-friendly grace
		-- period rather than locking everyone out at deploy time).
		ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

		-- ── VLM Active-Learning Labeling Queue ─────────────────────────
		-- Passive capture: every time Qwen successfully analyses an alarm
		-- frame, a row lands here so internal annotators can review the
		-- VLM output and submit ground-truth labels for fine-tuning.
		-- Operators never see this table; it is drained off-hours by
		-- internal staff via /admin/labeling.
		--
		-- status lifecycle:
		--   pending  → claimed (annotator opens the job)
		--   claimed  → labeled | skipped
		--
		-- snapshot_url may be a relative /snapshots/… path or an
		-- absolute URL depending on deployment.
		CREATE TABLE IF NOT EXISTS vlm_label_jobs (
			id              BIGSERIAL    PRIMARY KEY,
			alarm_id        TEXT         NOT NULL,
			camera_id       TEXT         NOT NULL DEFAULT '',
			site_id         TEXT         NOT NULL DEFAULT '',
			snapshot_url    TEXT         NOT NULL DEFAULT '',
			vlm_description TEXT         NOT NULL DEFAULT '',
			vlm_threat      TEXT         NOT NULL DEFAULT '',
			vlm_model       TEXT         NOT NULL DEFAULT '',
			yolo_detections JSONB        NOT NULL DEFAULT '[]',
			status          TEXT         NOT NULL DEFAULT 'pending',
			claimed_by      UUID,
			claimed_at      TIMESTAMPTZ,
			created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_vlm_label_jobs_status
			ON vlm_label_jobs(status, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_vlm_label_jobs_alarm
			ON vlm_label_jobs(alarm_id);

		-- Ground-truth labels submitted by internal annotators.
		-- verdict: 'correct' | 'incorrect' | 'needs_correction'
		-- corrected_description is non-empty only when verdict != correct.
		-- Tags are free-form strings (e.g. 'false_positive', 'ppe_violation',
		-- 'person_with_weapon') to seed the training dataset filter UI.
		CREATE TABLE IF NOT EXISTS vlm_labels (
			id                     BIGSERIAL    PRIMARY KEY,
			job_id                 BIGINT       NOT NULL REFERENCES vlm_label_jobs(id) ON DELETE CASCADE,
			annotator_id           UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			verdict                TEXT         NOT NULL,
			corrected_description  TEXT         NOT NULL DEFAULT '',
			corrected_threat       TEXT         NOT NULL DEFAULT '',
			tags                   TEXT[]       NOT NULL DEFAULT '{}',
			notes                  TEXT         NOT NULL DEFAULT '',
			labeled_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_vlm_labels_job
			ON vlm_labels(job_id);
		CREATE INDEX IF NOT EXISTS idx_vlm_labels_annotator
			ON vlm_labels(annotator_id, labeled_at DESC);

		-- Append-only audit tables via trigger. We deliberately don't
		-- REVOKE UPDATE/DELETE from the app role: the migration itself
		-- and future scripted maintenance run as this role. Instead a
		-- BEFORE trigger raises an exception on any mutation, which is
		-- just as effective and leaves one obvious place to disable
		-- (with comment, during a signed maintenance window) if a
		-- GDPR right-to-erasure request ever comes through.
		CREATE OR REPLACE FUNCTION ironsight_prevent_mutation()
		RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'audit table %.% is append-only (op=%)',
				TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP
				USING ERRCODE = 'insufficient_privilege';
		END;
		$$ LANGUAGE plpgsql;

		DROP TRIGGER IF EXISTS audit_log_append_only       ON audit_log;
		DROP TRIGGER IF EXISTS playback_audits_append_only ON playback_audits;
		DROP TRIGGER IF EXISTS deterrence_audits_append_only ON deterrence_audits;

		CREATE TRIGGER audit_log_append_only
			BEFORE UPDATE OR DELETE ON audit_log
			FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();
		CREATE TRIGGER playback_audits_append_only
			BEFORE UPDATE OR DELETE ON playback_audits
			FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();
		CREATE TRIGGER deterrence_audits_append_only
			BEFORE UPDATE OR DELETE ON deterrence_audits
			FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();

		-- ════════════════════════════════════════════════════════════
		-- Multi-tenant integrity backfill
		-- ════════════════════════════════════════════════════════════
		-- These ALTERs add the organization_id column and tenancy
		-- indexes to tables that originally only had site_id. Until the
		-- column is populated, queries still scope correctly via
		-- site→org joins; the new column is for direct-filter perf and
		-- defence-in-depth (a buggy join can't cross tenants if the
		-- handler also filters by organization_id directly).
		ALTER TABLE incidents       ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE active_alarms   ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE evidence_shares ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE vlm_label_jobs  ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_incidents_org       ON incidents(organization_id, last_alarm_ts DESC);
		CREATE INDEX IF NOT EXISTS idx_active_alarms_org   ON active_alarms(organization_id, ts DESC);
		CREATE INDEX IF NOT EXISTS idx_evidence_shares_org ON evidence_shares(organization_id);

		-- Backfill organization_id from sites where it's still empty.
		-- Idempotent: only updates rows that haven't been backfilled.
		UPDATE incidents i
		   SET organization_id = COALESCE(s.organization_id, '')
		  FROM sites s
		 WHERE i.site_id = s.id AND i.organization_id = '';
		UPDATE active_alarms a
		   SET organization_id = COALESCE(s.organization_id, '')
		  FROM sites s
		 WHERE a.site_id = s.id AND a.organization_id = '';

		-- ════════════════════════════════════════════════════════════
		-- Foreign key + uniqueness backfill
		-- ════════════════════════════════════════════════════════════
		-- Wrapped in DO blocks so re-running the migration is a no-op.
		-- We catch undefined_table for tables only created by external
		-- SQL files (evidence_shares lives in ironsight_platform.sql),
		-- and duplicate_object for constraints already added.
		DO $migrate$
		BEGIN
			-- vlm_label_jobs.alarm_id should be unique. The Go enqueue
			-- path uses ON CONFLICT (alarm_id) DO NOTHING which silently
			-- inserts duplicates without this constraint.
			BEGIN
				ALTER TABLE vlm_label_jobs ADD CONSTRAINT vlm_label_jobs_alarm_id_key UNIQUE (alarm_id);
			EXCEPTION WHEN duplicate_object THEN NULL;
				WHEN duplicate_table THEN NULL;
				WHEN unique_violation THEN
					-- Existing duplicates would block the constraint. Drop
					-- the older copies first, then retry.
					DELETE FROM vlm_label_jobs a USING vlm_label_jobs b
					 WHERE a.alarm_id = b.alarm_id AND a.id < b.id;
					BEGIN
						ALTER TABLE vlm_label_jobs ADD CONSTRAINT vlm_label_jobs_alarm_id_key UNIQUE (alarm_id);
					EXCEPTION WHEN duplicate_object THEN NULL;
					END;
			END;

			-- evidence_shares.incident_id → incidents(id). Skip silently
			-- if either side is missing (shouldn't happen in prod).
			BEGIN
				ALTER TABLE evidence_shares
					ADD CONSTRAINT evidence_shares_incident_fk
					FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE
					NOT VALID; -- NOT VALID skips backfill check; new rows enforced
			EXCEPTION WHEN duplicate_object THEN NULL;
				WHEN undefined_table THEN NULL;
			END;
		END;
		$migrate$;

		-- ════════════════════════════════════════════════════════════
		-- CHECK constraints on TEXT enums
		-- ════════════════════════════════════════════════════════════
		-- Guards against typos in code creating orphan rows that no
		-- query filter recognizes. NOT VALID = don't scan history (some
		-- legacy rows may have been written with the old enum set).
		DO $checks$
		BEGIN
			BEGIN
				ALTER TABLE users ADD CONSTRAINT users_role_chk
					CHECK (role IN ('admin','soc_operator','soc_supervisor','site_manager','customer','viewer','guard')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
			END;
			BEGIN
				ALTER TABLE incidents ADD CONSTRAINT incidents_status_chk
					CHECK (status IN ('active','acknowledged','resolved','closed')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
			END;
			BEGIN
				ALTER TABLE incidents ADD CONSTRAINT incidents_severity_chk
					CHECK (severity IN ('low','medium','high','critical')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
			END;
			BEGIN
				ALTER TABLE active_alarms ADD CONSTRAINT active_alarms_severity_chk
					CHECK (severity IN ('low','medium','high','critical')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
			END;
			BEGIN
				ALTER TABLE support_tickets ADD CONSTRAINT support_tickets_status_chk
					CHECK (status IN ('open','answered','closed')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
				WHEN undefined_table THEN NULL;
			END;
			BEGIN
				ALTER TABLE vlm_labels ADD CONSTRAINT vlm_labels_verdict_chk
					CHECK (verdict IN ('correct','incorrect','needs_correction')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
			END;
			BEGIN
				ALTER TABLE vlm_label_jobs ADD CONSTRAINT vlm_label_jobs_status_chk
					CHECK (status IN ('pending','claimed','labeled','skipped')) NOT VALID;
			EXCEPTION WHEN duplicate_object THEN NULL;
			END;
		END;
		$checks$;
	`)
	if err != nil {
		log.Printf("[DB] Migration warning (non-fatal): %v", err)
	} else {
		log.Println("[DB] Schema migration check complete")
	}

	// AI runtime metrics hypertable. One row per (service, sample_ts) tick;
	// we record deltas (calls, confirmed, filtered) since the previous tick
	// rather than cumulative counters, so range queries can SUM() without
	// worrying about api restarts that reset in-process atomics. GPU
	// fields are absolute readings sampled at tick time. Kept in its own
	// migration block so a Timescale-specific failure (e.g. extension not
	// loaded) doesn't cascade into the main schema migrations above.
	if _, err := db.Pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS ai_runtime_metrics (
			ts                  TIMESTAMPTZ NOT NULL,
			service             TEXT NOT NULL,
			site_id             UUID,
			gpu_util_pct        INT,
			gpu_memory_used_mb  INT,
			gpu_memory_total_mb INT,
			gpu_temperature_c   INT,
			calls_delta         INT NOT NULL DEFAULT 0,
			confirmed_delta     INT NOT NULL DEFAULT 0,
			filtered_delta      INT NOT NULL DEFAULT 0,
			avg_inference_ms    INT
		);
		ALTER TABLE ai_runtime_metrics ADD COLUMN IF NOT EXISTS site_id UUID;
		SELECT create_hypertable('ai_runtime_metrics', 'ts', if_not_exists => TRUE);
		CREATE INDEX IF NOT EXISTS idx_ai_metrics_service_ts
			ON ai_runtime_metrics (service, ts DESC);
		CREATE INDEX IF NOT EXISTS idx_ai_metrics_site_ts
			ON ai_runtime_metrics (site_id, ts DESC) WHERE site_id IS NOT NULL;
	`); err != nil {
		log.Printf("[DB] ai_runtime_metrics migration warning (non-fatal): %v", err)
	}

	// Override config paths from DB storage locations (if any are configured)
	storageConfigured := false
	if locs, err := db.ListStorageLocations(context.Background()); err == nil && len(locs) > 0 {
		for _, loc := range locs {
			if !loc.Enabled {
				continue
			}
			// Use the first enabled location with purpose "recordings" or "all"
			if loc.Purpose == "recordings" || loc.Purpose == "all" {
				recPath := filepath.Join(loc.Path, "recordings")
				hlsPath := filepath.Join(loc.Path, "hls")
				os.MkdirAll(recPath, 0755)
				os.MkdirAll(hlsPath, 0755)
				cfg.StoragePath = recPath
				cfg.HLSPath = hlsPath
				log.Printf("[STORAGE] Using configured location: %s (label: %s)", loc.Path, loc.Label)
				storageConfigured = true
				break
			}
		}
	}
	if !storageConfigured {
		// No storage configured — disable recording until user sets one up
		cfg.StoragePath = ""
		cfg.HLSPath = ""
		log.Println("[STORAGE] ⚠️  No storage location configured — recordings disabled.")
		log.Println("[STORAGE]    Use Settings → Storage to add a storage location.")
	}

	// First-run: auto-create default admin if no users exist
	if exists, _ := db.UserExists(context.Background()); !exists {
		hash, err := authpkg.HashPassword(cfg.DefaultAdminPass)
		if err == nil {
			_, err = db.CreateUser(context.Background(), &database.UserCreate{
				Username:    "admin",
				Role:        "admin",
				DisplayName: "System Administrator",
				Email:       "admin@ironsight.io",
			}, hash)
		}
		if err != nil {
			log.Printf("[AUTH] Warning: could not create default admin: %v", err)
		} else {
			log.Printf("[AUTH] Default admin created — username: admin  password: %s", cfg.DefaultAdminPass)
			log.Println("[AUTH] ⚠️  Change this password immediately via the settings page!")
		}
	}

	// Demo data seeding (P1-B-09): the demo-portfolio + demo-users
	// helpers, and the prior env-gate that wrapped them, live in the
	// separate cmd/seed binary now. Server startup never seeds.
	// Operators run `/app/seed --all` (or its --portfolio / --users
	// variants) explicitly against a staging database when they need
	// demo content. See internal/seed/ and cmd/seed/main.go.

	// Root context for all background goroutines. Cancelled on SIGINT /
	// SIGTERM so the WS hub, recording engine, retention manager, and
	// other long-runners exit cleanly. Defined here (before hub.Run)
	// rather than down by the signal-wait so every spawn point can use it.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// Initialize subsystems
	hub := api.NewHub()
	// Optional: Redis pub/sub bridge for multi-replica WS fanout. Silently
	// no-ops when REDIS_URL is unset (single-replica deployments don't
	// need it). See internal/api/websocket.go for the bridge design.
	if err := hub.AttachRedisBridge(rootCtx, cfg.RedisURL, cfg.RedisWSChannel); err != nil {
		log.Printf("[WS] Redis bridge attach failed: %v — continuing in-memory only", err)
	}
	go hub.Run(rootCtx)

	recEngine := recording.NewEngine(cfg, db)
	hlsServer := streaming.NewHLSServer(cfg, db)
	mtxServer := streaming.NewMediaMTXServer(cfg)

	// Batch-job workers. In single-binary mode (RUN_WORKERS=true, the
	// default) we instantiate and start them in-process below. In the
	// container split (RUN_WORKERS=false) the sibling `worker` service
	// owns these jobs — we skip instantiation so there's no race on the
	// same DB tables. See cmd/worker/main.go and MasterDeployment.md §2.
	var (
		retentionMgr *recording.RetentionManager
		exportWorker *export.Worker
	)
	if cfg.RunWorkers {
		retentionMgr = recording.NewRetentionManager(db)
		exportWorker = export.NewWorker(cfg, db)
	}

	// AI Detection — receives bounding box data from ONVIF Profile M analytics events
	det := detection.New(hub)
	log.Println("[DET] ONVIF analytics detection enabled (Profile M cameras)")

	// AI Pipeline — YOLO (detection) + Qwen (reasoning) for event-triggered analysis
	aiYOLO := os.Getenv("AI_YOLO_URL")
	if aiYOLO == "" {
		aiYOLO = "http://127.0.0.1:8501"
	}
	aiQwen := os.Getenv("AI_QWEN_URL")
	if aiQwen == "" {
		aiQwen = "http://127.0.0.1:8502"
	}
	aiClient := ai.NewClient(ai.Config{
		YOLOEndpoint: aiYOLO,
		QwenEndpoint: aiQwen,
		Enabled:      os.Getenv("AI_ENABLED") != "false",
	})
	aiClient.CheckHealth(context.Background())

	// Persisted runtime metrics for the Services dashboard. Samples
	// every 30s; rows go to the ai_runtime_metrics hypertable created
	// during the migration block above.
	api.StartAIMetricsSampler(context.Background(), db, aiClient, aiYOLO, aiQwen, 30*time.Second)

	// Background VLM indexer: enriches every recording segment with a
	// searchable description during idle hours. Scales with INDEXER_CONCURRENCY
	// (1 on a test 3070, 4-8 on production A40s). Disable with
	// INDEXER_ENABLED=false.
	//
	// Gated by RunWorkers so the container-split deployment doesn't run
	// two indexers racing on the same segments (the worker container owns
	// this job in that layout). Single-binary dev keeps the default.
	var vlmIndexer *indexer.Indexer
	if cfg.RunWorkers {
		vlmIndexer = indexer.New(cfg, db, aiClient)
		vlmIndexer.Start()
		defer vlmIndexer.Stop()
	}

	// Wire alert generation: detection events → AlertEvent broadcast via WebSocket
	det.AlertEmitter = func(result *detection.DetectionResult) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			cameraName, siteID, siteName, err := db.GetCameraWithSite(ctx, result.CameraID)
			if err != nil || siteID == "" {
				return // camera not assigned to a site — skip
			}

			// Classify the dominant detection
			alertType, severity := "person_detected", "high"
			for _, box := range result.Boxes {
				switch box.Label {
				case "vehicle", "car", "truck", "van":
					alertType, severity = "vehicle_detected", "medium"
				}
			}

			typeLabel := map[string]string{
				"person_detected":  "Person",
				"vehicle_detected": "Vehicle",
			}[alertType]

			now := time.Now().UnixMilli()
			// One alarm per camera per minute — prevents alert flooding
			alarmID := fmt.Sprintf("ALM-%s-%d", result.CameraID[:8], now/60000)

			// Record the detection as an event row first so the alarm we
			// create has a real foreign key to point at. Without this,
			// triggering_event_id would be NULL and the forensic question
			// "which detection frame fired this alarm?" has no answer in
			// SQL. Best-effort: if the insert fails, the alarm still gets
			// written, just without the event linkage.
			var eventID *int64
			camUUID, _ := uuid.Parse(result.CameraID)
			evt := &database.Event{
				CameraID:  camUUID,
				EventTime: time.UnixMilli(now),
				EventType: alertType,
				Details:   map[string]interface{}{"boxes": result.Boxes},
			}
			if err := db.InsertEvent(ctx, evt); err == nil {
				id := evt.ID
				eventID = &id
			}

			alarm := &database.ActiveAlarm{
				ID:                alarmID,
				TriggeringEventID: eventID,
				SiteID:            siteID,
				SiteName:          siteName,
				CameraID:          result.CameraID,
				CameraName:        cameraName,
				Severity:          severity,
				Type:              alertType,
				Description:       fmt.Sprintf("%s detected at %s", typeLabel, cameraName),
				Ts:                now,
				SlaDeadlineMs:     now + 90_000, // 90-second SLA
			}

			created, err := db.CreateActiveAlarm(ctx, alarm)
			if err != nil || !created {
				return // already exists this minute — suppress duplicate broadcast
			}

			// Broadcast { type: "alert", data: ActiveAlarm } to all WS clients
			msg, _ := json.Marshal(map[string]interface{}{
				"type": "alert",
				"data": alarm,
			})
			hub.Broadcast(msg)
			log.Printf("[ALERT] %s → %s (%s) siteID=%s", alertType, cameraName, result.CameraID[:8], siteID)
		}()
	}

	// Subscriber registry — tracks ONVIF event subscribers for cleanup on camera delete
	subReg := api.NewSubscriberRegistry()

	// `ctx` here is the same root context used by the WS hub above —
	// we alias for backwards-compat with the variable name used
	// throughout this function.
	ctx := rootCtx

	// Start background services — only when this process owns them.
	// When RUN_WORKERS=false these are nil and the sibling worker
	// container runs the equivalents.
	if cfg.RunWorkers {
		go retentionMgr.Start(ctx)
		exportWorker.Start(ctx)
		log.Println("[SERVER] Batch workers running in-process (RUN_WORKERS=true)")
	} else {
		log.Println("[SERVER] Batch workers delegated to sibling container (RUN_WORKERS=false)")
	}

	// Auto-start recording and streaming for cameras that have recording enabled
	go autoStartCameras(ctx, db, cfg, recEngine, hlsServer, mtxServer, hub, det, subReg, aiClient)

	// Notification dispatcher — feeds the alarm-disposition emails and
	// (next batch) the monthly-summary report. SMTP and Twilio fall
	// back to stub mailers when their respective env vars aren't set,
	// so dev environments still produce visible log output through
	// the dispatcher without needing real SendGrid / Twilio creds.
	emailMailer := notify.SelectMailer(notify.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUser,
		Password: cfg.SMTPPass,
		From:     cfg.SMTPFrom,
	})
	smsMailer := notify.SelectSMSMailer(notify.TwilioConfig{
		AccountSid: cfg.TwilioAccountSid,
		AuthToken:  cfg.TwilioAuthToken,
		From:       cfg.TwilioFrom,
	})
	notifier := notify.NewDispatcher(emailMailer, smsMailer, cfg.ProductName, cfg.PublicURL)

	// Create HTTP router (Chi-based, already has all routes including HLS and exports)
	player := onvif.NewBackchannelPlayer()
	router := api.NewRouter(cfg, db, hub, recEngine, hlsServer, mtxServer, det, player, subReg, notifier, aiClient)

	// Start HTTP server
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Println("============================================")
	log.Printf("  API Server:  http://localhost%s", addr)
	log.Printf("  Frontend:    http://localhost:3000")
	log.Printf("  WebSocket:   ws://localhost%s/ws", addr)
	log.Printf("  Health:      http://localhost%s/api/health", addr)
	log.Println("============================================")

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("[SERVER] Received signal %v, shutting down...", sig)

		recEngine.StopAll()
		hlsServer.StopAll()
		mtxServer.Stop()
		if retentionMgr != nil {
			retentionMgr.Stop()
		}
		subReg.StopAll()
		// Cancels rootCtx → WS hub, retention manager, export worker,
		// and any other long-running consumer of rootCtx all wind down.
		rootCancel()

		os.Exit(0)
	}()

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("[FATAL] Server failed: %v", err)
	}
}

// autoStartCameras initializes recording and HLS for cameras with recording enabled
func autoStartCameras(ctx context.Context, db *database.DB, cfg *config.Config, recEngine *recording.Engine, hlsServer *streaming.HLSServer, mtxServer *streaming.MediaMTXServer, hub *api.Hub, det *detection.Manager, subReg *api.SubscriberRegistry, aiClient *ai.Client) {
	// Wait a moment for the server to be ready
	time.Sleep(2 * time.Second)

	cameras, err := db.ListCameras(ctx)
	if err != nil {
		log.Printf("[STARTUP] Failed to list cameras: %v", err)
		return
	}

	// Alarm rate-limiter. Most VCA topics describe the SAME physical
	// activity from different angles (linecross + object + intrusion +
	// loitering + human + vehicle all fire together when a person walks
	// past). Keying the cooldown by event type lets them all through
	// simultaneously, which (a) buries the operator in duplicate alarms
	// for one event and (b) queues a Qwen inference per topic and
	// saturates the GPU. We bucket those motion-style topics under one
	// camera-level key. lpr/face stay per-type because they carry
	// identifying information that's lost if coalesced.
	motionTopics := map[string]bool{
		"linecross": true, "object": true, "intrusion": true,
		"loitering": true, "human": true, "vehicle": true, "motion": true,
	}
	// Per-camera AI in-flight gate. Qwen takes 5–30s; if a second alarm
	// fires for the same camera while Qwen is still running, queueing
	// another inference behind it just deepens the latency hole and
	// keeps the GPU pegged. Skip the AI step when one is already in
	// flight — the alarm itself still gets created and recorded, the
	// operator just doesn't get the second AI verdict.
	var aiInFlightMu sync.Mutex
	aiInFlight := make(map[string]bool)
	tryAcquireAI := func(camID string) bool {
		aiInFlightMu.Lock()
		defer aiInFlightMu.Unlock()
		if aiInFlight[camID] {
			return false
		}
		aiInFlight[camID] = true
		return true
	}
	releaseAI := func(camID string) {
		aiInFlightMu.Lock()
		defer aiInFlightMu.Unlock()
		delete(aiInFlight, camID)
	}

	var alarmCooldownMu sync.Mutex
	alarmLastFired := make(map[string]time.Time)
	allowAlarm := func(camID, evtType string) bool {
		key := camID + ":" + evtType
		if motionTopics[evtType] {
			key = camID + ":motion"
		}
		alarmCooldownMu.Lock()
		defer alarmCooldownMu.Unlock()
		if t, ok := alarmLastFired[key]; ok && time.Since(t) < 60*time.Second {
			return false
		}
		alarmLastFired[key] = time.Now()
		return true
	}

	for _, cam := range cameras {
		if !cam.Recording || cam.RTSPUri == "" {
			continue
		}

		// Connect to camera via ONVIF to verify
		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		if _, err := client.Connect(ctx); err != nil {
			log.Printf("[STARTUP] Camera %s offline: %v", cam.Name, err)
			db.UpdateCameraStatus(ctx, cam.ID, "offline")
			continue
		}

		db.UpdateCameraStatus(ctx, cam.ID, "online")

		// Only start recording/HLS if storage is configured
		if cfg.StoragePath != "" {
			if cam.Recording {
				// Recording policy now lives on the camera's site — see the
				// 2026-04 migration. SettingsForCamera falls back to engine
				// defaults for cameras that aren't yet site-assigned.
				settings := recording.SettingsForCamera(ctx, db, &cam)
				if err := recEngine.StartRecording(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri, settings); err != nil {
					log.Printf("[STARTUP] Failed to start recording for %s: %v", cam.Name, err)
				}
			} else {
				// Start HLS live stream standalone
				if err := hlsServer.StartLiveStream(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri); err != nil {
					log.Printf("[STARTUP] Failed to start HLS for %s: %v", cam.Name, err)
				}
			}
			log.Printf("[STARTUP] Camera %s: recording + streaming + events active", cam.Name)
		} else {
			log.Printf("[STARTUP] Camera %s: online (no storage — recording disabled)", cam.Name)
		}

		// Start event subscription (only if events are enabled for this camera)
		if cam.EventsEnabled {
			// Semaphore to limit concurrent thumbnail captures per camera
			thumbSem := make(chan struct{}, 3)
			camName := cam.Name
			_ = cam.OnvifAddress // used by driver hooks below
			subscriber := onvif.NewEventSubscriber(client, cam.ID, func(cameraID uuid.UUID, eventType string, details map[string]interface{}) {
				// Store event in database with a timeout to avoid holding pool connections
				dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer dbCancel()
				evt := &database.Event{
					CameraID:  cameraID,
					EventTime: time.Now(),
					EventType: eventType,
					Details:   details,
				}
				if err := db.InsertEvent(dbCtx, evt); err != nil {
					log.Printf("[EVENTS] Failed to store event: %v", err)
				}

				log.Printf("[EVENTS] %s from %s: %s", eventType, camName, details["topic"])

				// Trigger event-based recording clip creation
				recEngine.TriggerEvent(cameraID, eventType)

				// Broadcast to WebSocket clients (include event_id for thumbnail correlation)
				wsMsg, _ := json.Marshal(map[string]interface{}{
					"type":       "event",
					"id":         evt.ID,
					"camera_id":  cameraID.String(),
					"event":      eventType,
					"event_type": eventType,
					"event_time": evt.EventTime.Format(time.RFC3339),
					"details":    details,
					"time":       time.Now().Format(time.RFC3339),
				})
				hub.Broadcast(wsMsg)

				// ── Generate SOC alarm for VCA/AI events on site-assigned cameras ──
				alarmTypes := map[string]bool{
					"intrusion": true, "linecross": true, "human": true, "vehicle": true,
					"face": true, "loitering": true, "lpr": true, "object": true,
				}
				if alarmTypes[eventType] && allowAlarm(cameraID.String(), eventType) {
					eventTimestamp := evt.EventTime // capture before goroutine — this is the exact event time
					go func() {
						// Panic guard: a single bad event must not crash the
						// whole event loop. Recover, log, and move on.
						defer func() {
							if rec := recover(); rec != nil {
								log.Printf("[ALARM] PANIC in alarm-generation goroutine: %v", rec)
							}
						}()
						cn, siteID, siteName, err := db.GetCameraWithSite(context.Background(), cameraID.String())
						if err != nil || siteID == "" {
							return
						}
						now := time.Now().UnixMilli()

						// Extract AI confidence score from event details (0.0–1.0)
						var aiScore float64
						if s, ok := details["score"].(float64); ok {
							aiScore = s
						}
						objType, _ := details["obj_type"].(string)
						ruleName, _ := details["rule_name"].(string)

						severity := "medium"
						switch eventType {
						case "intrusion", "human", "face":
							severity = "critical"
						case "vehicle", "linecross", "loitering", "lpr":
							severity = "high"
						}
						// Downgrade severity for low-confidence detections
						if aiScore > 0 && aiScore < 0.6 {
							severity = "medium"
						}
						alarmID := fmt.Sprintf("alarm-%s-%d", cameraID.String()[:8], now)
						scoreStr := ""
						if aiScore > 0 {
							scoreStr = fmt.Sprintf(" (%.0f%% confidence)", aiScore*100)
						}
						objStr := ""
						if objType != "" {
							objStr = " " + objType
						}
						ruleStr := ""
						if ruleName != "" {
							ruleStr = " — " + ruleName
						}
						description := fmt.Sprintf("%s%s detected on %s at %s%s%s", eventType, objStr, cn, siteName, ruleStr, scoreStr)

						// Find the recording clip link immediately (completed segments only — fast).
						clipRelPath := recording.FindEventClip(cfg.StoragePath, cameraID.String(), eventTimestamp)
						clipURL := ""
						if clipRelPath != "" {
							clipURL = "/recordings/" + clipRelPath
						}

						// ── Incident correlation ──
						// Find or create an incident that groups alarms at the same
						// site within a 5-minute window. Adjacent cameras or repeated
						// events on one camera become a single SOC dispatch item.
						inc, _ := db.FindOpenIncident(context.Background(), siteID, now)
						isNewIncident := inc == nil
						if isNewIncident {
							// ID is intentionally left empty — CreateIncident
							// assigns an INC-YYYY-NNNN identifier from the
							// annual sequence.
							inc = &database.Incident{
								SiteID:        siteID,
								SiteName:      siteName,
								Severity:      severity,
								Status:        "active",
								AlarmCount:    1,
								CameraIDs:     []string{cameraID.String()},
								CameraNames:   []string{cn},
								Types:         []string{eventType},
								LatestType:    eventType,
								Description:   description,
								ClipURL:       clipURL,
								FirstAlarmTs:  now,
								LastAlarmTs:   now,
								SlaDeadlineMs: now + 90000,
							}
							if err := db.CreateIncident(context.Background(), inc); err != nil {
								log.Printf("[ALARM] Failed to create incident: %v", err)
								return
							}
						} else {
							_ = db.AttachAlarmToIncident(context.Background(),
								inc.ID, cameraID.String(), cn, eventType, severity,
								description, "", clipURL, now, now+90000)
							// Refresh local copy for broadcast
							inc.AlarmCount++
							inc.LastAlarmTs = now
							inc.LatestType = eventType
							inc.Description = description
							inc.SlaDeadlineMs = now + 90000
							if severity == "critical" || (severity == "high" && inc.Severity != "critical") {
								inc.Severity = severity
							}
							// Add camera if new
							found := false
							for _, id := range inc.CameraIDs {
								if id == cameraID.String() {
									found = true
									break
								}
							}
							if !found {
								inc.CameraIDs = append(inc.CameraIDs, cameraID.String())
								inc.CameraNames = append(inc.CameraNames, cn)
							}
						}

						// Create the child alarm linked to the incident
						alarm := &database.ActiveAlarm{
							ID:            alarmID,
							IncidentID:    inc.ID,
							SiteID:        siteID,
							SiteName:      siteName,
							CameraID:      cameraID.String(),
							CameraName:    cn,
							Severity:      severity,
							Type:          eventType,
							Description:   description,
							SnapshotURL:   "",
							ClipURL:       clipURL,
							Ts:            now,
							SlaDeadlineMs: now + 90000,
						}
						created, err := db.CreateActiveAlarm(context.Background(), alarm)
						if err != nil || !created {
							// Compensating delete: we just created an incident
							// in this path and now the alarm row didn't take.
							// Without cleanup we'd leave an "incident with 0
							// alarms" orphan. Best-effort — if the delete also
							// fails the retention manager will reap it later.
							if isNewIncident && inc != nil && inc.ID != "" {
								if _, delErr := db.Pool.Exec(context.Background(),
									`DELETE FROM incidents WHERE id=$1 AND alarm_count=1`, inc.ID); delErr != nil {
									log.Printf("[ALARM] orphan incident %s cleanup failed: %v", inc.ID, delErr)
								}
							}
							if err != nil {
								log.Printf("[ALARM] CreateActiveAlarm failed for %s: %v", alarmID, err)
							}
							return
						}
						log.Printf("[ALARM] %s → alarm %s → incident %s (%s at %s, %d alarms)",
							eventType, alarm.ID, inc.ID, cn, siteName, inc.AlarmCount)

						// Broadcast incident-level update to operators.
						// New incidents send "incident_new"; subsequent alarms send "incident_update".
						msgType := "incident_update"
						if isNewIncident {
							msgType = "incident_new"
						}
						incMsg, _ := json.Marshal(map[string]interface{}{
							"type": msgType,
							"data": inc,
						})
						hub.Broadcast(incMsg)

						// Also send the individual alarm for backward compat / detail views
						alertMsg, _ := json.Marshal(map[string]interface{}{
							"type": "alert",
							"data": map[string]interface{}{
								"id": alarm.ID, "incident_id": inc.ID,
								"site_id": siteID, "site_name": siteName,
								"camera_id": cameraID.String(), "camera_name": cn,
								"severity": severity, "type": eventType,
								"description": alarm.Description, "ts": now,
								"acknowledged": false, "escalation_level": 0,
								"sla_deadline_ms": alarm.SlaDeadlineMs,
								"snapshot_url": "",
								"clip_url":     clipURL,
								"ai_score":       aiScore,
								"obj_type":       objType,
								"rule_name":      ruleName,
								"bounding_boxes": details["bounding_boxes"],
							},
						})
						hub.Broadcast(alertMsg)

						// Async snapshot + AI analysis pipeline
						{
							snapAlarmID := alarmID
							snapIncidentID := inc.ID
							snapCameraID := cameraID.String()
							camOnvifAddr := cam.OnvifAddress
							camUser := cam.Username
							camPass := cam.Password
							camMfg := cam.Manufacturer
							evtSiteContext := fmt.Sprintf("Site: %s. Camera: %s. Event type: %s.", siteName, cn, eventType)

							go func() {
								// Panic guard: snapshot/AI pipeline touches a lot of
								// external state (RTSP, FFmpeg, AI service). A panic
								// here must not bring down the server.
								defer func() {
									if rec := recover(); rec != nil {
										log.Printf("[ALARM] PANIC in snapshot/AI goroutine for alarm %s: %v", snapAlarmID, rec)
									}
								}()
								var jpegFrame []byte
								var snapshotURL string

								// Strategy 1: Grab snapshot from Milesight /snapshot.cgi (fast, current frame)
								if strings.Contains(strings.ToLower(camMfg), "milesight") && camOnvifAddr != "" {
									msCam := msdriver.New(camOnvifAddr, camUser, camPass)
									snap, err := msCam.Snapshot()
									switch {
									case err != nil:
										log.Printf("[ALARM] Snapshot S1 (Milesight /snapshot.cgi) failed for alarm %s: %v", snapAlarmID, err)
									case len(snap) <= 1000:
										log.Printf("[ALARM] Snapshot S1 (Milesight /snapshot.cgi) returned %d bytes for alarm %s — treating as failure", len(snap), snapAlarmID)
									default:
										jpegFrame = snap
										log.Printf("[ALARM] Snapshot via Milesight /snapshot.cgi (%d bytes)", len(snap))
									}
								}

								// Strategy 2: Extract from recording segment (fallback)
								if len(jpegFrame) == 0 && cfg.StoragePath != "" {
									segAbsPath, _, segStartTime := recording.FindEventClipFull(cfg.StoragePath, snapCameraID, eventTimestamp)
									if segAbsPath == "" {
										log.Printf("[ALARM] Snapshot S2 (segment extract) failed for alarm %s: no segment found for camera %s at %s", snapAlarmID, snapCameraID, eventTimestamp.Format(time.RFC3339))
									} else {
										offsetSec := eventTimestamp.Sub(segStartTime).Seconds()
										snapDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", snapCameraID)
										snapFile := filepath.Join(snapDir, snapAlarmID+".jpg")
										os.MkdirAll(snapDir, 0755)
										if _, err := recording.ExtractFrameFromSegment(cfg.FFmpegPath, segAbsPath, snapFile, offsetSec); err != nil {
											log.Printf("[ALARM] Snapshot S2 (segment extract) failed for alarm %s: seg=%s offset=%.1fs err=%v", snapAlarmID, filepath.Base(segAbsPath), offsetSec, err)
										} else if data, rerr := os.ReadFile(snapFile); rerr == nil && len(data) > 1000 {
											jpegFrame = data
											log.Printf("[ALARM] Snapshot via segment extract (seg=%s offset=%.1fs %d bytes)", filepath.Base(segAbsPath), offsetSec, len(data))
										} else {
											log.Printf("[ALARM] Snapshot S2 (segment extract) produced empty/small file for alarm %s: seg=%s offset=%.1fs", snapAlarmID, filepath.Base(segAbsPath), offsetSec)
										}
									}
								}

								if len(jpegFrame) == 0 {
									log.Printf("[ALARM] No snapshot available for alarm %s — both strategies failed", snapAlarmID)
								}

								// Save snapshot to disk + update DB
								if len(jpegFrame) > 0 {
									snapDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", snapCameraID)
									os.MkdirAll(snapDir, 0755)
									snapFile := filepath.Join(snapDir, snapAlarmID+".jpg")
									os.WriteFile(snapFile, jpegFrame, 0644)

									snapshotURL = "/snapshots/" + snapCameraID + "/" + snapAlarmID + ".jpg"
									_ = db.UpdateActiveAlarmClip(context.Background(), snapAlarmID, clipURL, snapshotURL)
									_ = db.UpdateIncidentSnapshot(context.Background(), snapIncidentID, clipURL, snapshotURL)

									// Push snapshot to operators
									snapMsg, _ := json.Marshal(map[string]interface{}{
										"type": "alarm_snapshot",
										"data": map[string]interface{}{
											"alarm_id":     snapAlarmID,
											"incident_id":  snapIncidentID,
											"snapshot_url": snapshotURL,
										},
									})
									hub.Broadcast(snapMsg)
								}

								// ── AI Pipeline: YOLO → Qwen ──
								// Per-camera in-flight gate: drop the AI step
								// when another inference for this camera is
								// already running. Snapshot + alarm are still
								// captured; we just skip the (now redundant)
								// AI verdict for this burst.
								if len(jpegFrame) > 0 && tryAcquireAI(snapCameraID) {
									defer releaseAI(snapCameraID)
									aiResult := aiClient.Analyze(context.Background(), jpegFrame, siteID, evtSiteContext)
									if aiResult != nil && aiResult.Description != "" {
										ppeInfo := ""
										if len(aiResult.PPEViolations) > 0 {
											ppeInfo = fmt.Sprintf(" PPE_VIOLATIONS=%d", len(aiResult.PPEViolations))
										}
										log.Printf("[AI] alarm %s: %s threat=%s fp=%.0f%%%s (%.0fms total)",
											snapAlarmID, aiResult.Description,
											aiResult.ThreatLevel, aiResult.FalsePositivePct*100, ppeInfo, aiResult.TotalMs)

										// Broadcast AI enrichment to SOC operators
										aiMsg, _ := json.Marshal(map[string]interface{}{
											"type": "alarm_ai",
											"data": map[string]interface{}{
												"alarm_id":             snapAlarmID,
												"incident_id":          snapIncidentID,
												"ai_description":       aiResult.Description,
												"ai_threat_level":      aiResult.ThreatLevel,
												"ai_recommended_action": aiResult.RecommendedAction,
												"ai_false_positive_pct": aiResult.FalsePositivePct,
												"ai_objects":           aiResult.AIObjects,
												"ai_detections":        aiResult.Detections,
												"ai_ppe_detections":    aiResult.PPEDetections,
												"ai_ppe_violations":    aiResult.PPEViolations,
												"ai_yolo_model":        aiResult.YOLOModel,
												"ai_ppe_model":         aiResult.PPEModel,
												"ai_qwen_model":        aiResult.QwenModel,
												"ai_total_ms":          aiResult.TotalMs,
											},
										})
										hub.Broadcast(aiMsg)

										// Persist AI results to DB for REST polling
										detectionsJSON, _ := json.Marshal(aiResult.Detections)
										ppeViolationsJSON, _ := json.Marshal(aiResult.PPEViolations)
									_ = db.UpdateAlarmAI(context.Background(),
										snapAlarmID, aiResult.Description, aiResult.ThreatLevel,
										aiResult.RecommendedAction, aiResult.FalsePositivePct, detectionsJSON, ppeViolationsJSON)

										// ── Passive labeling capture ──
										// Enqueue the frame + VLM output for off-SOC annotation.
										// Non-blocking best-effort: a failure here must never affect
										// the alarm pipeline. Operators see nothing; internal staff
										// drain this queue via /admin/labeling.
										go func(alarmID, camID, siteID, snapURL, desc, threat, model string, det []byte) {
											defer func() {
												if rec := recover(); rec != nil {
													log.Printf("[LABELING] PANIC in enqueue goroutine for alarm %s: %v", alarmID, rec)
												}
											}()
											if err := db.EnqueueLabelJob(
												context.Background(),
												alarmID, camID, siteID, snapURL, desc, threat, model, det,
											); err != nil {
												log.Printf("[LABELING] enqueue failed for alarm %s: %v", alarmID, err)
											}
										}(snapAlarmID, snapCameraID, siteID, snapshotURL,
											aiResult.Description, aiResult.ThreatLevel, aiResult.QwenModel, detectionsJSON)

										// ── Video enrichment pass ──
										// Now that the initial snapshot analysis is out to operators,
										// extract a short clip around the event and feed it through
										// Qwen's video path for better motion/context reasoning. Runs
										// after the snapshot tier so operators see something fast
										// (~5s) and get the refined video-based verdict (~15-25s
										// later) as a follow-up update.
										if cfg.StoragePath != "" {
											segAbsPath, _, segStartTime := recording.FindEventClipFull(cfg.StoragePath, snapCameraID, eventTimestamp)
											if segAbsPath != "" {
												// Clip window: 1s before event → 3s after = 4s total.
												clipOffset := eventTimestamp.Sub(segStartTime).Seconds() - 1.0
												if clipOffset < 0 {
													clipOffset = 0
												}
												clipDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", snapCameraID)
												clipFile := filepath.Join(clipDir, snapAlarmID+".clip.mp4")
												_, cerr := recording.ExtractClipFromSegment(cfg.FFmpegPath, segAbsPath, clipFile, clipOffset, 4.0)
												var clipBytes []byte
												if cerr == nil {
													clipBytes, _ = os.ReadFile(clipFile)
												}
												// Always clean up — whether we analyze or bail.
												defer os.Remove(clipFile)

												switch {
												case cerr != nil:
													log.Printf("[AI] video clip extract failed for alarm %s (seg=%s offset=%.1fs): %v", snapAlarmID, filepath.Base(segAbsPath), clipOffset, cerr)
												case len(clipBytes) <= 1000:
													log.Printf("[AI] video clip too small (%d bytes) for alarm %s (seg=%s offset=%.1fs) — likely FFmpeg seeked past end of segment", len(clipBytes), snapAlarmID, filepath.Base(segAbsPath), clipOffset)
												default:
													if vidResult := aiClient.AnalyzeVideo(context.Background(), clipBytes, aiResult.Detections, siteID, evtSiteContext); vidResult == nil || vidResult.Description == "" {
														// No-op — network/decode error already logged by the client.
													} else if vidResult.Degraded {
														// Qwen fell back to mock_analysis (video_fps error, OOM, etc).
														// Keep the good snapshot-tier result — do NOT overwrite DB/broadcast.
														mockTail := vidResult.Description
														if len(mockTail) > 60 {
															mockTail = mockTail[:60] + "…"
														}
														log.Printf("[AI] alarm %s (video): degraded — keeping snapshot-tier result (video mock: %s)",
															snapAlarmID, mockTail)
													} else {
														log.Printf("[AI] alarm %s (video): %s threat=%s fp=%.0f%% (%.0fms)",
															snapAlarmID, vidResult.Description,
															vidResult.ThreatLevel, vidResult.FalsePositivePct*100, vidResult.InferenceMs)

														// Broadcast the video-enriched result as a follow-up
														// so operators' UI replaces the snapshot-tier text.
														vidMsg, _ := json.Marshal(map[string]interface{}{
															"type": "alarm_ai",
															"data": map[string]interface{}{
																"alarm_id":              snapAlarmID,
																"incident_id":           snapIncidentID,
																"ai_description":        vidResult.Description,
																"ai_threat_level":       vidResult.ThreatLevel,
																"ai_recommended_action": vidResult.RecommendedAction,
																"ai_false_positive_pct": vidResult.FalsePositivePct,
																"ai_objects":            vidResult.Objects,
																"ai_qwen_model":         vidResult.Model,
																"ai_mode":               "video",
															},
														})
														hub.Broadcast(vidMsg)

														// Overwrite DB with the refined verdict
														_ = db.UpdateAlarmAI(context.Background(),
															snapAlarmID, vidResult.Description, vidResult.ThreatLevel,
															vidResult.RecommendedAction, vidResult.FalsePositivePct, detectionsJSON, ppeViolationsJSON)
													}
												}
											}
										}
									}
								}
							}()
						}
					}()
				}

				// Async thumbnail capture via FFmpeg (bounded by semaphore)
				if cam.RTSPUri != "" {
					eventID := evt.ID
					rtspUri := cam.RTSPUri
					// Try to acquire semaphore slot; skip thumbnail if too many in-flight
					select {
					case thumbSem <- struct{}{}:
						go func() {
							defer func() { <-thumbSem }()
							defer func() {
								if rec := recover(); rec != nil {
									log.Printf("[THUMB] PANIC in thumbnail goroutine for event %d: %v", eventID, rec)
								}
							}()
							thumb, err := recording.CaptureFrame(cfg.FFmpegPath, rtspUri, 3)
							if err != nil {
								log.Printf("[THUMB] Failed to capture thumbnail for event %d: %v", eventID, err)
								return
							}
							thumbCtx, thumbCancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer thumbCancel()
							if err := db.UpdateEventThumbnail(thumbCtx, eventID, thumb); err != nil {
								log.Printf("[THUMB] Failed to store thumbnail for event %d: %v", eventID, err)
								return
							}
							log.Printf("[THUMB] Captured thumbnail for event %d (camera %s)", eventID, cameraID.String())

							// Broadcast thumbnail update so frontend can patch it in
							thumbMsg, _ := json.Marshal(map[string]interface{}{
								"type":      "event_thumbnail",
								"event_id":  eventID,
								"camera_id": cameraID.String(),
								"thumbnail": thumb,
							})
							hub.Broadcast(thumbMsg)
						}()
					default:
						// Skip thumbnail — too many captures in flight
					}
				}
				// If the event contains ONVIF analytics bounding boxes, broadcast them
				if boxes, ok := details["bounding_boxes"]; ok && boxes != nil {
					if rawBoxes, err := json.Marshal(boxes); err == nil {
						// Re-parse as []detection.BoundingBox
						var dboxes []detection.BoundingBox
						if err := json.Unmarshal(rawBoxes, &dboxes); err == nil && len(dboxes) > 0 {
							det.HandleAnalyticsEvent(cameraID, dboxes)
						}
					}
				}
			})
			// Vendor-specific event source selection — extracted into
			// api.StartCameraEventSource so the runtime-add path
			// (HandleCreateCamera) routes through the same logic.
			// Without this shared helper, Milesight cameras added at
			// runtime were silently downgraded to ONVIF PullPoint and
			// produced zero events.
			api.StartCameraEventSource(ctx, &cam, subscriber, subReg, "STARTUP")
		} else {
			log.Printf("[STARTUP] Camera %s: events subscription disabled", cam.Name)
		}

		log.Printf("[STARTUP] Camera %s: recording + streaming active", cam.Name)
	}

	// Start MediaMTX with all registered streams
	for _, cam := range cameras {
		if cam.RTSPUri != "" {
			mtxServer.AddStream(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri)
		}
	}
	if err := mtxServer.Start(ctx); err != nil {
		log.Printf("[STARTUP] Failed to start MediaMTX: %v", err)
	}

	if len(cameras) == 0 {
		log.Println("[STARTUP] No cameras configured. Use the API or frontend to add cameras.")
	}
}
