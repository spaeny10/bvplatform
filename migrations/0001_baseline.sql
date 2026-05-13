-- +goose Up
-- +goose StatementBegin
--
-- 0001_baseline: idempotent snapshot of the Ironsight schema captured from
-- fred's running ironsight-db (Postgres 15.17 + TimescaleDB) on 2026-05-08
-- via `pg_dump --schema-only --no-owner --no-privileges
--                   --exclude-schema=_timescaledb_internal
--                   --exclude-schema=_timescaledb_catalog
--                   --exclude-schema=_timescaledb_config
--                   --exclude-schema=_timescaledb_cache
--                   onvif_tool`
-- and post-processed by .tmp_baseline/transform.py (see commit message)
-- so that:
--
--   * Every DDL form has an idempotency guard:
--       CREATE TABLE / CREATE SEQUENCE / CREATE INDEX / CREATE EXTENSION
--           use IF NOT EXISTS.
--       CREATE FUNCTION uses CREATE OR REPLACE.
--       CREATE TRIGGER and ALTER TABLE ... ADD CONSTRAINT (no IF NOT EXISTS
--           form pre-PG17) are wrapped in a DO $idem$ block that catches
--           duplicate_object, invalid_table_definition, duplicate_table,
--           and 'others' — the explicit names act as documentation; the
--           'others' fallthrough is the actual safety net since this
--           baseline is run against fred's already-populated schema where
--           the goal is "no-op silently if the object exists".
--       ALTER TABLE ... ALTER COLUMN ... SET DEFAULT nextval(...) is wrapped
--           in a similar DO block (defensive — these are inherently
--           idempotent but a stale-sequence error would otherwise abort).
--   * The `ONLY` keyword is stripped from every ALTER TABLE: TimescaleDB
--     rejects `ALTER TABLE ONLY` on hypertables (SQLSTATE 0A000), and the
--     only-vs-not difference is a no-op for non-hypertables in our schema
--     (we never use table inheritance), so dropping it everywhere is safe.
--   * TimescaleDB hypertable conversions are made explicit via
--       SELECT create_hypertable(..., if_not_exists => TRUE, migrate_data => TRUE)
--     for the three hypertables (segments, events, ai_runtime_metrics).
--     pg_dump's --schema-only output omits these calls and emits the
--     internal _timescaledb_internal._hyper_* chunk tables instead, which
--     are managed by TimescaleDB and must NOT appear in user-applied DDL.
--     We exclude those internal schemas from the dump entirely.
--   * Owner / privilege / setval / SET-session noise is stripped.
--
-- The whole point of this baseline is that running it against fred's
-- existing populated DB is a NO-OP at the schema level: nothing changes,
-- and goose simply records `version=1, dirty=false` in goose_db_version.
-- From version 2 onward, ordinary additive migrations land in 000N_*.sql.
--
-- IMPORTANT: subsequent migrations should NOT use the catch-WHEN-others
-- pattern. That is specific to this baseline — its entire job is "succeed
-- against an already-populated DB". A normal forward migration must let
-- real errors propagate so a botched deploy fails fast at startup.
--
--
-- PostgreSQL database dump
--

-- Dumped from database version 15.17
-- Dumped by pg_dump version 15.17

--
-- Name: timescaledb; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS timescaledb WITH SCHEMA public;

--
-- Name: EXTENSION timescaledb; Type: COMMENT; Schema: -; Owner: -
--

--
-- Name: ironsight_prevent_mutation(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE OR REPLACE FUNCTION public.ironsight_prevent_mutation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
		BEGIN
			RAISE EXCEPTION 'audit table %.% is append-only (op=%)',
				TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP
				USING ERRCODE = 'insufficient_privilege';
		END;
		$$;

--
-- Name: segments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.segments (
    id bigint NOT NULL,
    camera_id uuid NOT NULL,
    start_time timestamp with time zone NOT NULL,
    end_time timestamp with time zone NOT NULL,
    file_path text NOT NULL,
    file_size bigint DEFAULT 0,
    duration_ms integer DEFAULT 0,
    has_audio boolean DEFAULT false
);

-- Convert segments to a TimescaleDB hypertable on its time column.
-- if_not_exists + migrate_data make this safe on both fresh and
-- already-converted DBs (and keeps existing rows on conversion).
SELECT create_hypertable('public.segments', 'start_time', if_not_exists => TRUE, migrate_data => TRUE);

--
-- Name: events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.events (
    id bigint NOT NULL,
    camera_id uuid NOT NULL,
    event_time timestamp with time zone NOT NULL,
    event_type text NOT NULL,
    details jsonb DEFAULT '{}'::jsonb,
    thumbnail text DEFAULT ''::text,
    segment_id bigint
);

-- Convert events to a TimescaleDB hypertable on its time column.
-- if_not_exists + migrate_data make this safe on both fresh and
-- already-converted DBs (and keeps existing rows on conversion).
SELECT create_hypertable('public.events', 'event_time', if_not_exists => TRUE, migrate_data => TRUE);

--
-- Name: ai_runtime_metrics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.ai_runtime_metrics (
    ts timestamp with time zone NOT NULL,
    service text NOT NULL,
    site_id uuid,
    gpu_util_pct integer,
    gpu_memory_used_mb integer,
    gpu_memory_total_mb integer,
    gpu_temperature_c integer,
    calls_delta integer DEFAULT 0 NOT NULL,
    confirmed_delta integer DEFAULT 0 NOT NULL,
    filtered_delta integer DEFAULT 0 NOT NULL,
    avg_inference_ms integer
);

-- Convert ai_runtime_metrics to a TimescaleDB hypertable on its time column.
-- if_not_exists + migrate_data make this safe on both fresh and
-- already-converted DBs (and keeps existing rows on conversion).
SELECT create_hypertable('public.ai_runtime_metrics', 'ts', if_not_exists => TRUE, migrate_data => TRUE);

--
-- Name: active_alarms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.active_alarms (
    id text NOT NULL,
    incident_id text DEFAULT ''::text,
    site_id text DEFAULT ''::text NOT NULL,
    site_name text DEFAULT ''::text NOT NULL,
    camera_id text DEFAULT ''::text NOT NULL,
    camera_name text DEFAULT ''::text NOT NULL,
    severity text DEFAULT 'high'::text NOT NULL,
    type text DEFAULT 'person_detected'::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    snapshot_url text DEFAULT ''::text,
    clip_url text DEFAULT ''::text,
    ts bigint DEFAULT 0 NOT NULL,
    acknowledged boolean DEFAULT false NOT NULL,
    claimed_by text DEFAULT ''::text,
    escalation_level integer DEFAULT 0,
    sla_deadline_ms bigint DEFAULT 0,
    created_at timestamp with time zone DEFAULT now(),
    ai_description text DEFAULT ''::text,
    ai_threat_level text DEFAULT ''::text,
    ai_recommended_action text DEFAULT ''::text,
    ai_false_positive_pct real DEFAULT 0,
    ai_detections jsonb DEFAULT '[]'::jsonb,
    ai_ppe_violations jsonb DEFAULT '[]'::jsonb,
    ai_operator_agreed boolean,
    ai_was_correct boolean,
    alarm_code text,
    triggering_event_id bigint,
    acknowledged_at timestamp with time zone,
    acknowledged_by_user_id uuid,
    acknowledged_by_callsign text DEFAULT ''::text NOT NULL,
    organization_id text DEFAULT ''::text NOT NULL
);

--
-- Name: alarm_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.alarm_queue (
    id integer NOT NULL,
    alarm_id text NOT NULL,
    site_id text,
    camera_id text,
    severity text DEFAULT 'high'::text,
    type text DEFAULT 'person_detected'::text,
    description text DEFAULT ''::text,
    ts bigint NOT NULL,
    assigned_to text,
    status text DEFAULT 'queued'::text,
    created_at timestamp with time zone DEFAULT now()
);

--
-- Name: alarm_queue_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.alarm_queue_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: alarm_queue_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarm_queue_id_seq OWNED BY public.alarm_queue.id;

--
-- Name: audio_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.audio_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    category text DEFAULT 'custom'::text NOT NULL,
    file_name text NOT NULL,
    duration real DEFAULT 0,
    file_size bigint DEFAULT 0,
    created_at timestamp with time zone DEFAULT now()
);

--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.audit_log (
    id bigint NOT NULL,
    user_id uuid,
    username text DEFAULT ''::text NOT NULL,
    action text NOT NULL,
    target_type text DEFAULT ''::text,
    target_id text DEFAULT ''::text,
    details text DEFAULT ''::text,
    ip_address text DEFAULT ''::text,
    created_at timestamp with time zone DEFAULT now()
);

--
-- Name: audit_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.audit_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: audit_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.audit_log_id_seq OWNED BY public.audit_log.id;

--
-- Name: bookmarks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.bookmarks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    camera_id uuid,
    event_time timestamp with time zone NOT NULL,
    label text NOT NULL,
    notes text DEFAULT ''::text,
    severity text DEFAULT 'info'::text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now()
);

--
-- Name: cameras; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.cameras (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    onvif_address text NOT NULL,
    username text DEFAULT ''::text,
    password text DEFAULT ''::text,
    rtsp_uri text DEFAULT ''::text,
    sub_stream_uri text DEFAULT ''::text,
    retention_days integer DEFAULT 30,
    recording boolean DEFAULT true,
    recording_mode text DEFAULT 'continuous'::text,
    pre_buffer_sec integer DEFAULT 10,
    post_buffer_sec integer DEFAULT 30,
    recording_triggers text DEFAULT 'motion,object'::text,
    status text DEFAULT 'offline'::text,
    profile_token text DEFAULT ''::text,
    has_ptz boolean DEFAULT false,
    manufacturer text DEFAULT ''::text,
    model text DEFAULT ''::text,
    firmware text DEFAULT ''::text,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    site_id text,
    location text DEFAULT ''::text,
    device_class text DEFAULT 'continuous'::text,
    sense_webhook_token text,
    events_enabled boolean DEFAULT true,
    audio_enabled boolean DEFAULT true,
    camera_group text DEFAULT ''::text,
    schedule text DEFAULT ''::text,
    privacy_mask boolean DEFAULT false,
    map_x real DEFAULT 0,
    map_y real DEFAULT 0
);

--
-- Name: company_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.company_users (
    id text NOT NULL,
    name text NOT NULL,
    email text NOT NULL,
    phone text DEFAULT ''::text,
    password_hash text NOT NULL,
    role text DEFAULT 'site_manager'::text,
    organization_id text,
    assigned_site_ids jsonb DEFAULT '[]'::jsonb,
    created_at timestamp with time zone DEFAULT now()
);

--
-- Name: deterrence_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.deterrence_audits (
    id bigint NOT NULL,
    user_id text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    role text DEFAULT ''::text NOT NULL,
    camera_id uuid NOT NULL,
    camera_name text DEFAULT ''::text NOT NULL,
    action text NOT NULL,
    duration_sec integer DEFAULT 0 NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    alarm_id text DEFAULT ''::text NOT NULL,
    success boolean DEFAULT true NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    fired_at timestamp with time zone DEFAULT now() NOT NULL,
    ip text DEFAULT ''::text NOT NULL
);

--
-- Name: deterrence_audits_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.deterrence_audits_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: deterrence_audits_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.deterrence_audits_id_seq OWNED BY public.deterrence_audits.id;

--
-- Name: device_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.device_assignments (
    id bigint NOT NULL,
    device_type text NOT NULL,
    device_id text NOT NULL,
    site_id text NOT NULL,
    location_label text DEFAULT ''::text,
    assigned_at timestamp with time zone DEFAULT now(),
    removed_at timestamp with time zone
);

--
-- Name: device_assignments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.device_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: device_assignments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.device_assignments_id_seq OWNED BY public.device_assignments.id;

--
-- Name: events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.events_id_seq OWNED BY public.events.id;

--
-- Name: evidence_share_opens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.evidence_share_opens (
    id bigint NOT NULL,
    token text NOT NULL,
    ip text DEFAULT ''::text NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    referrer text DEFAULT ''::text NOT NULL,
    opened_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: evidence_share_opens_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.evidence_share_opens_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: evidence_share_opens_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.evidence_share_opens_id_seq OWNED BY public.evidence_share_opens.id;

--
-- Name: evidence_shares; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.evidence_shares (
    token text NOT NULL,
    incident_id text NOT NULL,
    created_by text DEFAULT ''::text,
    expires_at timestamp with time zone,
    revoked boolean DEFAULT false,
    created_at timestamp with time zone DEFAULT now(),
    organization_id text DEFAULT ''::text NOT NULL
);

--
-- Name: exports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.exports (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    camera_id uuid NOT NULL,
    start_time timestamp with time zone NOT NULL,
    end_time timestamp with time zone NOT NULL,
    status text DEFAULT 'pending'::text,
    file_path text DEFAULT ''::text,
    file_size bigint DEFAULT 0,
    error text DEFAULT ''::text,
    created_at timestamp with time zone DEFAULT now(),
    completed_at timestamp with time zone,
    started_at timestamp with time zone
);

--
-- Name: incidents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.incidents (
    id text NOT NULL,
    site_id text DEFAULT ''::text NOT NULL,
    site_name text DEFAULT ''::text NOT NULL,
    severity text DEFAULT 'medium'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    alarm_count integer DEFAULT 1 NOT NULL,
    camera_ids text[] DEFAULT '{}'::text[],
    camera_names text[] DEFAULT '{}'::text[],
    types text[] DEFAULT '{}'::text[],
    latest_type text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    snapshot_url text DEFAULT ''::text,
    clip_url text DEFAULT ''::text,
    first_alarm_ts bigint DEFAULT 0 NOT NULL,
    last_alarm_ts bigint DEFAULT 0 NOT NULL,
    sla_deadline_ms bigint DEFAULT 0,
    created_at timestamp with time zone DEFAULT now(),
    organization_id text DEFAULT ''::text NOT NULL
);

--
-- Name: notification_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.notification_subscriptions (
    id bigint NOT NULL,
    user_id uuid NOT NULL,
    channel text NOT NULL,
    event_type text NOT NULL,
    severity_min text DEFAULT 'low'::text NOT NULL,
    site_ids jsonb,
    quiet_start text DEFAULT ''::text,
    quiet_end text DEFAULT ''::text,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: notification_subscriptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.notification_subscriptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: notification_subscriptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.notification_subscriptions_id_seq OWNED BY public.notification_subscriptions.id;

--
-- Name: operators; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.operators (
    id text NOT NULL,
    name text NOT NULL,
    callsign text NOT NULL,
    email text DEFAULT ''::text,
    password_hash text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'available'::text,
    active_alarm_id text,
    last_active bigint DEFAULT 0,
    user_id uuid
);

--
-- Name: organizations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.organizations (
    id text NOT NULL,
    name text NOT NULL,
    plan text DEFAULT 'professional'::text,
    contact_name text DEFAULT ''::text,
    contact_email text DEFAULT ''::text,
    logo_url text DEFAULT ''::text,
    features jsonb DEFAULT '{"vlm_safety": true, "semantic_search": true, "evidence_sharing": true, "global_ai_training": true}'::jsonb,
    created_at timestamp with time zone DEFAULT now()
);

--
-- Name: playback_audits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.playback_audits (
    id bigint NOT NULL,
    user_id text DEFAULT ''::text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    role text DEFAULT ''::text NOT NULL,
    camera_id uuid,
    segment_id bigint,
    event_id bigint,
    endpoint text DEFAULT ''::text NOT NULL,
    accessed_at timestamp with time zone DEFAULT now() NOT NULL,
    ip text DEFAULT ''::text NOT NULL
);

--
-- Name: playback_audits_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.playback_audits_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: playback_audits_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.playback_audits_id_seq OWNED BY public.playback_audits.id;

--
-- Name: revoked_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.revoked_tokens (
    jti text NOT NULL,
    user_id uuid,
    revoked_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL
);

--
-- Name: security_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.security_events (
    id text NOT NULL,
    alarm_id text NOT NULL,
    site_id text,
    camera_id text,
    severity text DEFAULT 'high'::text,
    type text DEFAULT 'person_detected'::text,
    description text DEFAULT ''::text,
    disposition_code text NOT NULL,
    disposition_label text DEFAULT ''::text,
    operator_id text,
    operator_callsign text DEFAULT ''::text,
    operator_notes text DEFAULT ''::text,
    action_log jsonb DEFAULT '[]'::jsonb,
    escalation_depth integer DEFAULT 0,
    clip_url text DEFAULT ''::text,
    clip_bookmark_id text,
    ts bigint NOT NULL,
    resolved_at bigint NOT NULL,
    viewed_by_customer boolean DEFAULT false,
    ai_description text DEFAULT ''::text,
    ai_threat_level text DEFAULT ''::text,
    ai_operator_agreed boolean,
    ai_was_correct boolean,
    verified_by_user_id uuid,
    verified_by_callsign text DEFAULT ''::text NOT NULL,
    verified_at timestamp with time zone,
    avs_factors jsonb DEFAULT '{}'::jsonb NOT NULL,
    avs_score integer DEFAULT 0 NOT NULL,
    avs_rubric_version text DEFAULT ''::text NOT NULL,
    disposed_by_user_id uuid
);

--
-- Name: segment_descriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.segment_descriptions (
    segment_id bigint NOT NULL,
    camera_id uuid NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    activity_level text DEFAULT 'none'::text NOT NULL,
    entities jsonb DEFAULT '[]'::jsonb NOT NULL,
    detections jsonb DEFAULT '[]'::jsonb NOT NULL,
    indexer_version integer DEFAULT 1 NOT NULL,
    analysis_ms integer DEFAULT 0 NOT NULL,
    indexed_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: segments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.segments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: segments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.segments_id_seq OWNED BY public.segments.id;

--
-- Name: shift_handoffs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.shift_handoffs (
    id bigint NOT NULL,
    from_operator_id text DEFAULT ''::text NOT NULL,
    from_operator_callsign text DEFAULT ''::text NOT NULL,
    to_operator_id text DEFAULT ''::text NOT NULL,
    to_operator_callsign text DEFAULT ''::text NOT NULL,
    notes text DEFAULT ''::text,
    site_locks jsonb DEFAULT '[]'::jsonb,
    pending_alarms jsonb DEFAULT '[]'::jsonb,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    accepted_at timestamp with time zone
);

--
-- Name: shift_handoffs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.shift_handoffs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: shift_handoffs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.shift_handoffs_id_seq OWNED BY public.shift_handoffs.id;

--
-- Name: site_sops; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.site_sops (
    id text NOT NULL,
    site_id text NOT NULL,
    title text NOT NULL,
    category text DEFAULT 'access'::text,
    priority text DEFAULT 'normal'::text,
    steps jsonb DEFAULT '[]'::jsonb,
    contacts jsonb DEFAULT '[]'::jsonb,
    updated_at timestamp with time zone DEFAULT now(),
    updated_by text DEFAULT ''::text
);

--
-- Name: sites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.sites (
    id text NOT NULL,
    name text NOT NULL,
    address text DEFAULT ''::text,
    organization_id text NOT NULL,
    latitude double precision,
    longitude double precision,
    status text DEFAULT 'active'::text,
    monitoring_start text DEFAULT '18:00'::text,
    monitoring_end text DEFAULT '06:00'::text,
    site_notes jsonb DEFAULT '[]'::jsonb,
    created_at timestamp with time zone DEFAULT now(),
    feature_mode text DEFAULT 'security_and_safety'::text NOT NULL,
    monitoring_schedule jsonb DEFAULT '[]'::jsonb,
    snooze jsonb,
    retention_days integer DEFAULT 3,
    recording_mode text DEFAULT 'continuous'::text,
    pre_buffer_sec integer DEFAULT 10,
    post_buffer_sec integer DEFAULT 30,
    recording_triggers text DEFAULT 'motion,object'::text,
    recording_schedule text DEFAULT ''::text,
    recording_backfilled boolean DEFAULT false,
    customer_contacts jsonb DEFAULT '[]'::jsonb NOT NULL
);

--
-- Name: speakers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.speakers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    onvif_address text NOT NULL,
    username text DEFAULT ''::text,
    password text DEFAULT ''::text,
    rtsp_uri text DEFAULT ''::text,
    zone text DEFAULT ''::text,
    status text DEFAULT 'offline'::text,
    manufacturer text DEFAULT ''::text,
    model text DEFAULT ''::text,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    site_id text,
    location text DEFAULT ''::text
);

--
-- Name: storage_locations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.storage_locations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    label text NOT NULL,
    path text NOT NULL,
    purpose text DEFAULT 'recordings'::text NOT NULL,
    retention_days integer DEFAULT 30,
    max_gb integer DEFAULT 0,
    priority integer DEFAULT 0,
    enabled boolean DEFAULT true,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now()
);

--
-- Name: support_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.support_messages (
    id bigint NOT NULL,
    ticket_id bigint NOT NULL,
    author_id uuid NOT NULL,
    author_role text NOT NULL,
    body text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: support_messages_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.support_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: support_messages_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.support_messages_id_seq OWNED BY public.support_messages.id;

--
-- Name: support_tickets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.support_tickets (
    id bigint NOT NULL,
    organization_id text NOT NULL,
    site_id text,
    created_by uuid NOT NULL,
    subject text NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_message_at timestamp with time zone DEFAULT now() NOT NULL,
    last_message_by text DEFAULT 'customer'::text NOT NULL
);

--
-- Name: support_tickets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.support_tickets_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: support_tickets_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.support_tickets_id_seq OWNED BY public.support_tickets.id;

--
-- Name: system_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.system_settings (
    id integer DEFAULT 1 NOT NULL,
    recordings_path text DEFAULT './storage/recordings'::text,
    snapshots_path text DEFAULT './storage/thumbnails'::text,
    exports_path text DEFAULT './storage/exports'::text,
    hls_path text DEFAULT './storage/hls'::text,
    default_retention_days integer DEFAULT 30,
    default_recording_mode text DEFAULT 'continuous'::text,
    default_segment_duration integer DEFAULT 60,
    ffmpeg_path text DEFAULT 'C:\ffmpeg\bin\ffmpeg.exe'::text,
    updated_at timestamp with time zone DEFAULT now(),
    discovery_subnet text DEFAULT ''::text,
    discovery_ports text DEFAULT ''::text,
    notification_webhook_url text DEFAULT ''::text,
    notification_email text DEFAULT ''::text,
    notification_triggers text DEFAULT ''::text,
    CONSTRAINT single_row CHECK ((id = 1))
);

--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    username text NOT NULL,
    password_hash text NOT NULL,
    role text DEFAULT 'operator'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    display_name text DEFAULT ''::text NOT NULL,
    email text DEFAULT ''::text NOT NULL,
    phone text DEFAULT ''::text NOT NULL,
    organization_id text,
    assigned_site_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    failed_login_attempts integer DEFAULT 0 NOT NULL,
    locked_until timestamp with time zone,
    mfa_enabled boolean DEFAULT false NOT NULL,
    mfa_secret text DEFAULT ''::text NOT NULL,
    mfa_recovery_hashes jsonb DEFAULT '[]'::jsonb NOT NULL,
    password_changed_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: vca_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.vca_rules (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    camera_id uuid NOT NULL,
    rule_type text NOT NULL,
    name text DEFAULT ''::text NOT NULL,
    enabled boolean DEFAULT true,
    sensitivity integer DEFAULT 50,
    region jsonb DEFAULT '[]'::jsonb NOT NULL,
    direction text DEFAULT 'both'::text,
    threshold_sec integer DEFAULT 0,
    schedule text DEFAULT 'always'::text,
    actions jsonb DEFAULT '["record", "notify"]'::jsonb,
    synced boolean DEFAULT false,
    sync_error text DEFAULT ''::text,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now()
);

--
-- Name: vlm_label_jobs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.vlm_label_jobs (
    id bigint NOT NULL,
    alarm_id text NOT NULL,
    camera_id text DEFAULT ''::text NOT NULL,
    site_id text DEFAULT ''::text NOT NULL,
    snapshot_url text DEFAULT ''::text NOT NULL,
    vlm_description text DEFAULT ''::text NOT NULL,
    vlm_threat text DEFAULT ''::text NOT NULL,
    vlm_model text DEFAULT ''::text NOT NULL,
    yolo_detections jsonb DEFAULT '[]'::jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    claimed_by uuid,
    claimed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    organization_id text DEFAULT ''::text NOT NULL
);

--
-- Name: vlm_label_jobs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.vlm_label_jobs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: vlm_label_jobs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.vlm_label_jobs_id_seq OWNED BY public.vlm_label_jobs.id;

--
-- Name: vlm_labels; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE IF NOT EXISTS public.vlm_labels (
    id bigint NOT NULL,
    job_id bigint NOT NULL,
    annotator_id uuid NOT NULL,
    verdict text NOT NULL,
    corrected_description text DEFAULT ''::text NOT NULL,
    corrected_threat text DEFAULT ''::text NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    labeled_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: vlm_labels_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE IF NOT EXISTS public.vlm_labels_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: vlm_labels_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.vlm_labels_id_seq OWNED BY public.vlm_labels.id;

--
-- Name: alarm_queue id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.alarm_queue ALTER COLUMN id SET DEFAULT nextval('public.alarm_queue_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: audit_log id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.audit_log ALTER COLUMN id SET DEFAULT nextval('public.audit_log_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: deterrence_audits id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.deterrence_audits ALTER COLUMN id SET DEFAULT nextval('public.deterrence_audits_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: device_assignments id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.device_assignments ALTER COLUMN id SET DEFAULT nextval('public.device_assignments_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: events id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.events ALTER COLUMN id SET DEFAULT nextval('public.events_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: evidence_share_opens id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.evidence_share_opens ALTER COLUMN id SET DEFAULT nextval('public.evidence_share_opens_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: notification_subscriptions id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.notification_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.notification_subscriptions_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: playback_audits id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.playback_audits ALTER COLUMN id SET DEFAULT nextval('public.playback_audits_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: segments id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.segments ALTER COLUMN id SET DEFAULT nextval('public.segments_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: shift_handoffs id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.shift_handoffs ALTER COLUMN id SET DEFAULT nextval('public.shift_handoffs_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: support_messages id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_messages ALTER COLUMN id SET DEFAULT nextval('public.support_messages_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: support_tickets id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_tickets ALTER COLUMN id SET DEFAULT nextval('public.support_tickets_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_label_jobs id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_label_jobs ALTER COLUMN id SET DEFAULT nextval('public.vlm_label_jobs_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_labels id; Type: DEFAULT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_labels ALTER COLUMN id SET DEFAULT nextval('public.vlm_labels_id_seq'::regclass);
EXCEPTION WHEN others THEN NULL;
END $idem$;

--
-- Name: active_alarms active_alarms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.active_alarms
    ADD CONSTRAINT active_alarms_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: active_alarms active_alarms_severity_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.active_alarms
    ADD CONSTRAINT active_alarms_severity_chk CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: alarm_queue alarm_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.alarm_queue
    ADD CONSTRAINT alarm_queue_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: audio_messages audio_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.audio_messages
    ADD CONSTRAINT audio_messages_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: bookmarks bookmarks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.bookmarks
    ADD CONSTRAINT bookmarks_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: cameras cameras_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.cameras
    ADD CONSTRAINT cameras_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: company_users company_users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.company_users
    ADD CONSTRAINT company_users_email_key UNIQUE (email);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: company_users company_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.company_users
    ADD CONSTRAINT company_users_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: deterrence_audits deterrence_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.deterrence_audits
    ADD CONSTRAINT deterrence_audits_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: device_assignments device_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.device_assignments
    ADD CONSTRAINT device_assignments_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: evidence_share_opens evidence_share_opens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.evidence_share_opens
    ADD CONSTRAINT evidence_share_opens_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: evidence_shares evidence_shares_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.evidence_shares
    ADD CONSTRAINT evidence_shares_pkey PRIMARY KEY (token);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: exports exports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.exports
    ADD CONSTRAINT exports_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: incidents incidents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.incidents
    ADD CONSTRAINT incidents_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: incidents incidents_severity_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.incidents
    ADD CONSTRAINT incidents_severity_chk CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: incidents incidents_status_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.incidents
    ADD CONSTRAINT incidents_status_chk CHECK ((status = ANY (ARRAY['active'::text, 'acknowledged'::text, 'resolved'::text, 'closed'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: notification_subscriptions notification_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.notification_subscriptions
    ADD CONSTRAINT notification_subscriptions_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: notification_subscriptions notification_subscriptions_user_id_channel_event_type_key; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.notification_subscriptions
    ADD CONSTRAINT notification_subscriptions_user_id_channel_event_type_key UNIQUE (user_id, channel, event_type);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: operators operators_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.operators
    ADD CONSTRAINT operators_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: organizations organizations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.organizations
    ADD CONSTRAINT organizations_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: playback_audits playback_audits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.playback_audits
    ADD CONSTRAINT playback_audits_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: revoked_tokens revoked_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.revoked_tokens
    ADD CONSTRAINT revoked_tokens_pkey PRIMARY KEY (jti);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: security_events security_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.security_events
    ADD CONSTRAINT security_events_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: segment_descriptions segment_descriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.segment_descriptions
    ADD CONSTRAINT segment_descriptions_pkey PRIMARY KEY (segment_id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: shift_handoffs shift_handoffs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.shift_handoffs
    ADD CONSTRAINT shift_handoffs_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: site_sops site_sops_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.site_sops
    ADD CONSTRAINT site_sops_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: sites sites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.sites
    ADD CONSTRAINT sites_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: speakers speakers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.speakers
    ADD CONSTRAINT speakers_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: storage_locations storage_locations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.storage_locations
    ADD CONSTRAINT storage_locations_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_messages support_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_messages
    ADD CONSTRAINT support_messages_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_tickets support_tickets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_tickets
    ADD CONSTRAINT support_tickets_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_tickets support_tickets_status_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_tickets
    ADD CONSTRAINT support_tickets_status_chk CHECK ((status = ANY (ARRAY['open'::text, 'answered'::text, 'closed'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: system_settings system_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.system_settings
    ADD CONSTRAINT system_settings_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: users users_role_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.users
    ADD CONSTRAINT users_role_chk CHECK ((role = ANY (ARRAY['admin'::text, 'soc_operator'::text, 'soc_supervisor'::text, 'site_manager'::text, 'customer'::text, 'viewer'::text, 'guard'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: users users_username_key; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.users
    ADD CONSTRAINT users_username_key UNIQUE (username);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vca_rules vca_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vca_rules
    ADD CONSTRAINT vca_rules_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_label_jobs vlm_label_jobs_alarm_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_label_jobs
    ADD CONSTRAINT vlm_label_jobs_alarm_id_key UNIQUE (alarm_id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_label_jobs vlm_label_jobs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_label_jobs
    ADD CONSTRAINT vlm_label_jobs_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_label_jobs vlm_label_jobs_status_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_label_jobs
    ADD CONSTRAINT vlm_label_jobs_status_chk CHECK ((status = ANY (ARRAY['pending'::text, 'claimed'::text, 'labeled'::text, 'skipped'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_labels vlm_labels_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_labels
    ADD CONSTRAINT vlm_labels_pkey PRIMARY KEY (id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_labels vlm_labels_verdict_chk; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_labels
    ADD CONSTRAINT vlm_labels_verdict_chk CHECK ((verdict = ANY (ARRAY['correct'::text, 'incorrect'::text, 'needs_correction'::text]))) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: ai_runtime_metrics_ts_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS ai_runtime_metrics_ts_idx ON public.ai_runtime_metrics USING btree (ts DESC);

--
-- Name: events_event_time_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS events_event_time_idx ON public.events USING btree (event_time DESC);

--
-- Name: idx_active_alarms_ack_window; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_active_alarms_ack_window ON public.active_alarms USING btree (acknowledged_at, ts) WHERE (acknowledged_at IS NOT NULL);

--
-- Name: idx_active_alarms_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX IF NOT EXISTS idx_active_alarms_code ON public.active_alarms USING btree (alarm_code) WHERE (alarm_code IS NOT NULL);

--
-- Name: idx_active_alarms_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_active_alarms_event ON public.active_alarms USING btree (triggering_event_id) WHERE (triggering_event_id IS NOT NULL);

--
-- Name: idx_active_alarms_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_active_alarms_org ON public.active_alarms USING btree (organization_id, ts DESC);

--
-- Name: idx_active_alarms_unacked; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_active_alarms_unacked ON public.active_alarms USING btree (ts) WHERE (acknowledged = false);

--
-- Name: idx_ai_metrics_service_ts; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_ai_metrics_service_ts ON public.ai_runtime_metrics USING btree (service, ts DESC);

--
-- Name: idx_ai_metrics_site_ts; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_ai_metrics_site_ts ON public.ai_runtime_metrics USING btree (site_id, ts DESC) WHERE (site_id IS NOT NULL);

--
-- Name: idx_alarm_queue_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_alarm_queue_status ON public.alarm_queue USING btree (status);

--
-- Name: idx_audit_log_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_audit_log_target ON public.audit_log USING btree (target_type, target_id, created_at DESC) WHERE (target_id <> ''::text);

--
-- Name: idx_cameras_sense_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX IF NOT EXISTS idx_cameras_sense_token ON public.cameras USING btree (sense_webhook_token) WHERE (sense_webhook_token IS NOT NULL);

--
-- Name: idx_cameras_site; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_cameras_site ON public.cameras USING btree (site_id);

--
-- Name: idx_company_users_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_company_users_org ON public.company_users USING btree (organization_id);

--
-- Name: idx_deterrence_audits_camera; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_deterrence_audits_camera ON public.deterrence_audits USING btree (camera_id, fired_at DESC);

--
-- Name: idx_deterrence_audits_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_deterrence_audits_user ON public.deterrence_audits USING btree (user_id, fired_at DESC);

--
-- Name: idx_device_assignments_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_device_assignments_active ON public.device_assignments USING btree (device_id, removed_at) WHERE (removed_at IS NULL);

--
-- Name: idx_device_assignments_device; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_device_assignments_device ON public.device_assignments USING btree (device_type, device_id);

--
-- Name: idx_device_assignments_site; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_device_assignments_site ON public.device_assignments USING btree (site_id);

--
-- Name: idx_events_camera_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_events_camera_type ON public.events USING btree (camera_id, event_type, event_time DESC);

--
-- Name: idx_events_details; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_events_details ON public.events USING gin (details);

--
-- Name: idx_events_segment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_events_segment ON public.events USING btree (segment_id);

--
-- Name: idx_evidence_share_opens_token; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_evidence_share_opens_token ON public.evidence_share_opens USING btree (token, opened_at DESC);

--
-- Name: idx_evidence_shares_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_evidence_shares_org ON public.evidence_shares USING btree (organization_id);

--
-- Name: idx_exports_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_exports_pending ON public.exports USING btree (created_at) WHERE (status = 'pending'::text);

--
-- Name: idx_exports_processing; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_exports_processing ON public.exports USING btree (started_at) WHERE (status = 'processing'::text);

--
-- Name: idx_incidents_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_incidents_active ON public.incidents USING btree (site_id, last_alarm_ts) WHERE (status = 'active'::text);

--
-- Name: idx_incidents_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_incidents_org ON public.incidents USING btree (organization_id, last_alarm_ts DESC);

--
-- Name: idx_notification_subs_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_notification_subs_user ON public.notification_subscriptions USING btree (user_id) WHERE (enabled = true);

--
-- Name: idx_playback_audits_camera; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_playback_audits_camera ON public.playback_audits USING btree (camera_id, accessed_at DESC);

--
-- Name: idx_playback_audits_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_playback_audits_user ON public.playback_audits USING btree (user_id, accessed_at DESC);

--
-- Name: idx_revoked_tokens_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_revoked_tokens_expires_at ON public.revoked_tokens USING btree (expires_at);

--
-- Name: idx_security_events_avs_score; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_security_events_avs_score ON public.security_events USING btree (avs_score DESC, ts DESC) WHERE (avs_score >= 2);

--
-- Name: idx_security_events_site; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_security_events_site ON public.security_events USING btree (site_id);

--
-- Name: idx_security_events_unverified_high; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_security_events_unverified_high ON public.security_events USING btree (ts DESC) WHERE ((severity = ANY (ARRAY['critical'::text, 'high'::text])) AND (verified_at IS NULL));

--
-- Name: idx_security_events_viewed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_security_events_viewed ON public.security_events USING btree (viewed_by_customer);

--
-- Name: idx_segment_descriptions_activity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_segment_descriptions_activity ON public.segment_descriptions USING btree (activity_level) WHERE (activity_level <> 'none'::text);

--
-- Name: idx_segment_descriptions_camera; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_segment_descriptions_camera ON public.segment_descriptions USING btree (camera_id, indexed_at DESC);

--
-- Name: idx_segment_descriptions_fts; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_segment_descriptions_fts ON public.segment_descriptions USING gin (to_tsvector('english'::regconfig, description));

--
-- Name: idx_segment_descriptions_tags; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_segment_descriptions_tags ON public.segment_descriptions USING gin (tags);

--
-- Name: idx_segments_camera_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_segments_camera_time ON public.segments USING btree (camera_id, start_time DESC);

--
-- Name: idx_shift_handoffs_to; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_shift_handoffs_to ON public.shift_handoffs USING btree (to_operator_id, status);

--
-- Name: idx_sites_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_sites_org ON public.sites USING btree (organization_id);

--
-- Name: idx_sops_site; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_sops_site ON public.site_sops USING btree (site_id);

--
-- Name: idx_support_messages_ticket; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_support_messages_ticket ON public.support_messages USING btree (ticket_id, created_at);

--
-- Name: idx_support_tickets_open; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_support_tickets_open ON public.support_tickets USING btree (last_message_at DESC) WHERE (status = 'open'::text);

--
-- Name: idx_support_tickets_org_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_support_tickets_org_status ON public.support_tickets USING btree (organization_id, status, last_message_at DESC);

--
-- Name: idx_vca_rules_camera; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_vca_rules_camera ON public.vca_rules USING btree (camera_id);

--
-- Name: idx_vlm_label_jobs_alarm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_vlm_label_jobs_alarm ON public.vlm_label_jobs USING btree (alarm_id);

--
-- Name: idx_vlm_label_jobs_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_vlm_label_jobs_status ON public.vlm_label_jobs USING btree (status, created_at DESC);

--
-- Name: idx_vlm_labels_annotator; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_vlm_labels_annotator ON public.vlm_labels USING btree (annotator_id, labeled_at DESC);

--
-- Name: idx_vlm_labels_job; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS idx_vlm_labels_job ON public.vlm_labels USING btree (job_id);

--
-- Name: segments_start_time_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX IF NOT EXISTS segments_start_time_idx ON public.segments USING btree (start_time DESC);

--
-- Name: audit_log audit_log_append_only; Type: TRIGGER; Schema: public; Owner: -
--

DO $idem$ BEGIN
  CREATE TRIGGER audit_log_append_only BEFORE DELETE OR UPDATE ON public.audit_log FOR EACH ROW EXECUTE FUNCTION public.ironsight_prevent_mutation();
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: deterrence_audits deterrence_audits_append_only; Type: TRIGGER; Schema: public; Owner: -
--

DO $idem$ BEGIN
  CREATE TRIGGER deterrence_audits_append_only BEFORE DELETE OR UPDATE ON public.deterrence_audits FOR EACH ROW EXECUTE FUNCTION public.ironsight_prevent_mutation();
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: playback_audits playback_audits_append_only; Type: TRIGGER; Schema: public; Owner: -
--

DO $idem$ BEGIN
  CREATE TRIGGER playback_audits_append_only BEFORE DELETE OR UPDATE ON public.playback_audits FOR EACH ROW EXECUTE FUNCTION public.ironsight_prevent_mutation();
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: active_alarms active_alarms_incident_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.active_alarms
    ADD CONSTRAINT active_alarms_incident_id_fkey FOREIGN KEY (incident_id) REFERENCES public.incidents(id) ON DELETE SET DEFAULT;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: bookmarks bookmarks_camera_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.bookmarks
    ADD CONSTRAINT bookmarks_camera_id_fkey FOREIGN KEY (camera_id) REFERENCES public.cameras(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: cameras cameras_site_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.cameras
    ADD CONSTRAINT cameras_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.sites(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: company_users company_users_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.company_users
    ADD CONSTRAINT company_users_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: events events_camera_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.events
    ADD CONSTRAINT events_camera_id_fkey FOREIGN KEY (camera_id) REFERENCES public.cameras(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: evidence_shares evidence_shares_incident_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.evidence_shares
    ADD CONSTRAINT evidence_shares_incident_fk FOREIGN KEY (incident_id) REFERENCES public.incidents(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: exports exports_camera_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.exports
    ADD CONSTRAINT exports_camera_id_fkey FOREIGN KEY (camera_id) REFERENCES public.cameras(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: notification_subscriptions notification_subscriptions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.notification_subscriptions
    ADD CONSTRAINT notification_subscriptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: security_events security_events_site_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.security_events
    ADD CONSTRAINT security_events_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.sites(id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: segments segments_camera_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.segments
    ADD CONSTRAINT segments_camera_id_fkey FOREIGN KEY (camera_id) REFERENCES public.cameras(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: site_sops site_sops_site_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.site_sops
    ADD CONSTRAINT site_sops_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.sites(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: sites sites_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.sites
    ADD CONSTRAINT sites_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: speakers speakers_site_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.speakers
    ADD CONSTRAINT speakers_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.sites(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_messages support_messages_author_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_messages
    ADD CONSTRAINT support_messages_author_id_fkey FOREIGN KEY (author_id) REFERENCES public.users(id);
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_messages support_messages_ticket_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_messages
    ADD CONSTRAINT support_messages_ticket_id_fkey FOREIGN KEY (ticket_id) REFERENCES public.support_tickets(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_tickets support_tickets_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_tickets
    ADD CONSTRAINT support_tickets_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_tickets support_tickets_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_tickets
    ADD CONSTRAINT support_tickets_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: support_tickets support_tickets_site_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.support_tickets
    ADD CONSTRAINT support_tickets_site_id_fkey FOREIGN KEY (site_id) REFERENCES public.sites(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vca_rules vca_rules_camera_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vca_rules
    ADD CONSTRAINT vca_rules_camera_id_fkey FOREIGN KEY (camera_id) REFERENCES public.cameras(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_labels vlm_labels_annotator_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_labels
    ADD CONSTRAINT vlm_labels_annotator_id_fkey FOREIGN KEY (annotator_id) REFERENCES public.users(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- Name: vlm_labels vlm_labels_job_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

DO $idem$ BEGIN
  ALTER TABLE public.vlm_labels
    ADD CONSTRAINT vlm_labels_job_id_fkey FOREIGN KEY (job_id) REFERENCES public.vlm_label_jobs(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
          WHEN invalid_table_definition THEN NULL;
          WHEN duplicate_table THEN NULL;
          WHEN others THEN NULL;
END $idem$;

--
-- PostgreSQL database dump complete
--


-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- The 0001 baseline represents the schema we started tracking at; there is
-- no meaningful prior state to revert to, so Down is a deliberate no-op.
-- Use a subsequent migration's Down to undo specific additive changes.
-- Do NOT make this Down destructive — `goose down` against version 1 must
-- never wipe the database.
SELECT 1;
-- +goose StatementEnd
