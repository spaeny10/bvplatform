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

	"onvif-tool/internal/ai"
	"onvif-tool/internal/indexer"
	"onvif-tool/internal/api"
	authpkg "onvif-tool/internal/auth"
	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
	"onvif-tool/internal/detection"
	"onvif-tool/internal/drivers"
	"onvif-tool/internal/export"
	msdriver "onvif-tool/internal/milesight"
	"onvif-tool/internal/onvif"
	"onvif-tool/internal/recording"
	"onvif-tool/internal/streaming"
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

	// Auto-migrate: add new columns if they don't exist
	_, err = db.Pool.Exec(context.Background(), `
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
			retention_days INT DEFAULT 30,
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
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS retention_days       INT  DEFAULT 30;
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_mode       TEXT DEFAULT 'continuous';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS pre_buffer_sec       INT  DEFAULT 10;
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS post_buffer_sec      INT  DEFAULT 30;
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_triggers   TEXT DEFAULT 'motion,object';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_schedule   TEXT DEFAULT '';
		ALTER TABLE sites ADD COLUMN IF NOT EXISTS recording_backfilled BOOLEAN DEFAULT false;

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

		-- UL 827B password rotation. password_changed_at is the source of
		-- truth for "is this password too old"; the login handler reads it
		-- and decides whether to flag the response with a forced-change
		-- indicator. NOW() default keeps existing rows valid for 180 days
		-- from the moment the migration runs (an operator-friendly grace
		-- period rather than locking everyone out at deploy time).
		ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

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
	`)
	if err != nil {
		log.Printf("[DB] Migration warning (non-fatal): %v", err)
	} else {
		log.Println("[DB] Schema migration check complete")
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

	// Seed demo platform users (SOC operators and portal users) if they don't exist
	seedDemoUsers(context.Background(), db)

	// Initialize subsystems
	hub := api.NewHub()
	// Optional: Redis pub/sub bridge for multi-replica WS fanout. Silently
	// no-ops when REDIS_URL is unset (single-replica deployments don't
	// need it). See internal/api/websocket.go for the bridge design.
	if err := hub.AttachRedisBridge(context.Background(), cfg.RedisURL, cfg.RedisWSChannel); err != nil {
		log.Printf("[WS] Redis bridge attach failed: %v — continuing in-memory only", err)
	}
	go hub.Run()

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Create HTTP router (Chi-based, already has all routes including HLS and exports)
	player := onvif.NewBackchannelPlayer()
	router := api.NewRouter(cfg, db, hub, recEngine, hlsServer, mtxServer, det, player, subReg)

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
		cancel()

		os.Exit(0)
	}()

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("[FATAL] Server failed: %v", err)
	}
}

// seedDemoUsers creates demo user accounts for each platform role if they don't already exist.
// Runs on every startup but only inserts rows that are missing.
func seedDemoUsers(ctx context.Context, db *database.DB) {
	type seed struct {
		username    string
		password    string
		role        string
		displayName string
		email       string
		phone       string
		orgID       string
		siteIDs     []string
	}

	demo := []seed{
		// SOC operators — linked to operators table
		{"jhayes", "demo123", "soc_operator", "Jordan Hayes", "jhayes@ironsight.io", "312-555-0111", "", nil},
		{"ctorres", "demo123", "soc_operator", "Casey Torres", "ctorres@ironsight.io", "312-555-0122", "", nil},
		{"rmorgan", "demo123", "soc_supervisor", "Riley Morgan", "rmorgan@ironsight.io", "312-555-0133", "", nil},
		// Portal users — linked to organizations/sites
		{"marcus.webb", "demo123", "site_manager", "Marcus Webb", "marcus.webb@apexcg.com", "312-555-0147", "co-alpha001", []string{"ACG-301", "ACG-302"}},
		{"spierce", "demo123", "customer", "Sandra Pierce", "spierce@apexcg.com", "312-555-0198", "co-alpha001", []string{"ACG-301", "ACG-302"}},
		{"priya.sharma", "demo123", "site_manager", "Priya Sharma", "priya@meridiandv.com", "512-555-0293", "co-beta002", []string{"MDV-501"}},
		{"derek.lawson", "demo123", "site_manager", "Derek Lawson", "dlawson@ironcladsites.com", "602-555-0311", "co-gamma003", []string{"ISS-201"}},
	}

	for _, s := range demo {
		// Skip if user already exists
		existing, _ := db.GetUserByUsernameOrEmail(ctx, s.username)
		if existing != nil {
			continue
		}
		hash, err := authpkg.HashPassword(s.password)
		if err != nil {
			log.Printf("[SEED] Failed to hash password for %s: %v", s.username, err)
			continue
		}
		u, err := db.CreateUser(ctx, &database.UserCreate{
			Username:        s.username,
			Role:            s.role,
			DisplayName:     s.displayName,
			Email:           s.email,
			Phone:           s.phone,
			OrganizationID:  s.orgID,
			AssignedSiteIDs: s.siteIDs,
		}, hash)
		if err != nil {
			log.Printf("[SEED] Could not create user %s: %v", s.username, err)
			continue
		}
		log.Printf("[SEED] Created user %s (%s)", s.username, s.role)

		// Link SOC users to their operators table row
		switch s.username {
		case "jhayes":
			db.Pool.Exec(ctx, `UPDATE operators SET user_id=$1 WHERE id='op-001'`, u.ID)
		case "ctorres":
			db.Pool.Exec(ctx, `UPDATE operators SET user_id=$1 WHERE id='op-002'`, u.ID)
		case "rmorgan":
			db.Pool.Exec(ctx, `UPDATE operators SET user_id=$1 WHERE id='op-003'`, u.ID)
		}
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

	// Alarm rate-limiter: max 1 SOC alarm per camera+type per 60s
	var alarmCooldownMu sync.Mutex
	alarmLastFired := make(map[string]time.Time)
	allowAlarm := func(camID, evtType string) bool {
		key := camID + ":" + evtType
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
								if len(jpegFrame) > 0 {
									aiResult := aiClient.Analyze(context.Background(), jpegFrame, evtSiteContext)
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
													if vidResult := aiClient.AnalyzeVideo(context.Background(), clipBytes, aiResult.Detections, evtSiteContext); vidResult == nil || vidResult.Description == "" {
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
			// Milesight cameras: use WebSocket /webstream/track instead of ONVIF PullPoint.
			// The WS provides real-time JSON analytics with AI confidence scores,
			// object types, and rule metadata — and avoids the ONVIF subscription
			// limit and ResourceUnknownFault issues on Milesight firmware.
			isMilesight := strings.Contains(strings.ToLower(cam.Manufacturer), "milesight")
			if isMilesight && cam.OnvifAddress != "" {
				msCam := msdriver.New(cam.OnvifAddress, cam.Username, cam.Password)
				camIDForMS := cam.ID
				msStream := msdriver.NewEventStream(msCam, cam.Name, func(eventType string, metadata map[string]interface{}) {
					subscriber.InjectEvent(camIDForMS, eventType, metadata)
				})
				msStream.Start(ctx)
				log.Printf("[STARTUP] Camera %s: Milesight WebSocket event stream active (ONVIF PullPoint skipped)", cam.Name)
			} else {
				// Non-Milesight cameras: use standard ONVIF PullPoint subscription
				info := &onvif.DeviceInfo{Manufacturer: cam.Manufacturer, Model: cam.Model}
				if drv := drivers.ForDevice(info); drv != nil {
					subscriber.Classify = drv.ClassifyEvent
					subscriber.Enrich = drv.EnrichEvent
					log.Printf("[STARTUP] Camera %s: %s driver attached for events", cam.Name, drv.Name())
				}
				subscriber.Start(ctx)
				subReg.Register(cam.ID, subscriber)
				log.Printf("[STARTUP] Camera %s: ONVIF events subscription active", cam.Name)
			}
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
