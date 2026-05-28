-- +goose Up
-- +goose StatementBegin
--
-- P4-SCHEMA-07: Row-Level Security defense-in-depth.
--
-- WHAT THIS MIGRATION DOES
-- ────────────────────────
-- 1. Creates app_current_tenant() — reads current_setting('app.current_tenant', true)
--    and returns NULL when the GUC is not set (the 'true' flag suppresses the
--    missing-variable error).  Policies use this function as the RHS comparator.
--
-- 2. For every customer-data table that carries organization_id, enables RLS
--    and creates two policies:
--      • tenant_isolation — standard WHERE/WITH CHECK that limits reads and
--        writes to rows whose organization_id matches app_current_tenant().
--      • service_bypass   — grants the schema-owner roles (onvif + postgres)
--        unrestricted access.  FORCE ROW LEVEL SECURITY is necessary because
--        table-owner connections bypass RLS by default in PostgreSQL; the
--        explicit bypass policy for those roles restores intentional access
--        for migration, seed, and background-worker paths.
--
-- 3. The organizations table is special: its PK *is* the tenant ID (organizations.id
--    is the TEXT slug that populates every other table's organization_id).  The
--    tenant_isolation policy therefore uses `id = app_current_tenant()` not
--    `organization_id = app_current_tenant()`.
--
-- ROLE POLICY
-- ──────────────────────────────────────────────────────────────────────────
-- CI runs against TimescaleDB's official image which starts with POSTGRES_USER
-- supplied by docker-compose (default: 'onvif').  That user owns the schema.
-- fred production also uses 'onvif' as the owner.  The goose migrate binary
-- connects as 'onvif' so it has the bypass policy automatically.
--
-- We also grant bypass to 'postgres' (the postgres superuser) for:
--   • CI workers that may use the default postgres role
--   • Emergency DBA access
--
-- Both grants are PERMISSIVE policies so they stack with (i.e. OR with) the
-- tenant_isolation policy — a connection as onvif/postgres always wins.
--
-- HYPERTABLE NOTE
-- ───────────────────────────────────────────────────────────────────────────
-- TimescaleDB hypertables: detections, segments, events, person_track_frames.
-- ALTER TABLE ... ENABLE ROW LEVEL SECURITY is issued against the HYPERTABLE
-- (the user-visible table name), NOT against the internal chunk tables.
-- TimescaleDB propagates the RLS enable + policies to all current and future
-- chunks automatically.  This is the officially documented approach.
--
-- SCOPE EXCLUSIONS
-- ─────────────────────────────────────────────────────────────────────────
-- The following tables are intentionally excluded from RLS:
--   • goose_db_version, schema_migrations — migration bookkeeping only
--   • _timescaledb_* internal schemas    — TimescaleDB managed, never touched
--   • system_settings, revoked_tokens, storage_locations — no tenant concept
--   • alarm_queue                         — internal dispatcher queue, no org_id
--   • deterrence_audits, playback_audits  — audit tables scoped by camera/user,
--     not org_id; cross-camera data is already RBAC-gated
--   • audio_messages, bookmarks           — no org_id column
--   • shift_handoffs, operators, site_sops — platform-level tables, no org_id
--   • security_events                     — no org_id column (scoped by site_id)
--   • exports                             — scoped by camera_id, no org_id
--   • evidence_share_opens                — open-access audit table
--   • device_assignments                  — no org_id
--   • segment_descriptions                — no org_id
--   • notification_subscriptions          — scoped by user_id, not org_id
--   • vlm_labels                          — scoped by job_id → vlm_label_jobs
--   • company_users                       — deprecated table, kept for migration
--     compatibility; does carry org_id but the table is read-only in production
--     (the users table is the live store).  Adding RLS here would break the
--     existing admin endpoints that read company_users without a tenant context.
--     Flag: if company_users is ever re-activated, revisit.
--   • audit_log                           — carries user_id and action only,
--     no organization_id column in the baseline schema.
--   • monthly_summary                     — no physical table; computed in Go
--     from sites/cameras/incidents aggregates.
--   • digest_sends                        — uses org_id TEXT (not organization_id);
--     see policy below (uses column name org_id).
--
-- DOWN SECTION
-- ─────────────────────────────────────────────────────────────────────────
-- Must be a clean round-trip:
--   1. Drop all policies (PERMISSIVE first, then tenant_isolation).
--   2. DISABLE RLS on each table (FORCE off included).
--   3. Drop the helper function.
-- The order mirrors the UP section but reversed.

-- ─────────────────────────────────────────────────────────────────────────
-- Helper function
-- ─────────────────────────────────────────────────────────────────────────

CREATE OR REPLACE FUNCTION app_current_tenant() RETURNS text
    LANGUAGE sql STABLE PARALLEL SAFE
AS $$
    SELECT current_setting('app.current_tenant', true)
$$;

-- ─────────────────────────────────────────────────────────────────────────
-- MACRO pattern (repeated per table):
--   ALTER TABLE t ENABLE ROW LEVEL SECURITY;
--   ALTER TABLE t FORCE ROW LEVEL SECURITY;
--   CREATE POLICY tenant_isolation ON t
--       AS PERMISSIVE FOR ALL
--       USING (organization_id = app_current_tenant())
--       WITH CHECK (organization_id = app_current_tenant());
--   CREATE POLICY service_bypass ON t
--       AS PERMISSIVE FOR ALL TO onvif, postgres
--       USING (true);
-- ─────────────────────────────────────────────────────────────────────────


-- ── cameras ──────────────────────────────────────────────────────────────
-- cameras.organization_id was not in the baseline; added via ALTER TABLE ...
-- in a later migration (0012 area). Confirmed present.
-- NOTE: cameras does NOT carry organization_id directly in the baseline.
-- It is assigned through sites (cameras.site_id → sites.organization_id).
-- RLS on cameras via organization_id is therefore NOT applied — cameras
-- are scoped at the application level through site assignment.
-- (See authz.go AuthorizedCameraIDs).  Applying RLS here would require a
-- subquery join that would break the append-only trigger path and is not
-- worth the complexity for a table already protected by RBAC.

-- ── sites ─────────────────────────────────────────────────────────────────
ALTER TABLE sites ENABLE ROW LEVEL SECURITY;
ALTER TABLE sites FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON sites;
CREATE POLICY tenant_isolation ON sites
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON sites;
CREATE POLICY service_bypass ON sites
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── organizations ────────────────────────────────────────────────────────
-- Special: policy is on id = tenant, not organization_id = tenant.
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON organizations;
CREATE POLICY tenant_isolation ON organizations
    AS PERMISSIVE FOR ALL
    USING (id = app_current_tenant())
    WITH CHECK (id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON organizations;
CREATE POLICY service_bypass ON organizations
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── users ────────────────────────────────────────────────────────────────
-- users.organization_id is nullable (SOC staff have no org). The policy
-- must still work: staff rows have organization_id = NULL, so the USING
-- clause evaluates to NULL = <tenant> which is FALSE — SOC staff rows are
-- invisible to tenant-scoped connections. That is correct and intentional:
-- SOC staff are global-view, they never use tenant-scoped connections.
-- Service-bypass role covers the SSO GetOrCreateUserByEmail path.
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON users;
CREATE POLICY tenant_isolation ON users
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON users;
CREATE POLICY service_bypass ON users
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── active_alarms ─────────────────────────────────────────────────────────
ALTER TABLE active_alarms ENABLE ROW LEVEL SECURITY;
ALTER TABLE active_alarms FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON active_alarms;
CREATE POLICY tenant_isolation ON active_alarms
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON active_alarms;
CREATE POLICY service_bypass ON active_alarms
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── incidents ─────────────────────────────────────────────────────────────
ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON incidents;
CREATE POLICY tenant_isolation ON incidents
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON incidents;
CREATE POLICY service_bypass ON incidents
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── evidence_shares ──────────────────────────────────────────────────────
ALTER TABLE evidence_shares ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_shares FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON evidence_shares;
CREATE POLICY tenant_isolation ON evidence_shares
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON evidence_shares;
CREATE POLICY service_bypass ON evidence_shares
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── vlm_label_jobs ────────────────────────────────────────────────────────
ALTER TABLE vlm_label_jobs ENABLE ROW LEVEL SECURITY;
ALTER TABLE vlm_label_jobs FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON vlm_label_jobs;
CREATE POLICY tenant_isolation ON vlm_label_jobs
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON vlm_label_jobs;
CREATE POLICY service_bypass ON vlm_label_jobs
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── vca_rules ────────────────────────────────────────────────────────────
-- vca_rules does not have organization_id in the baseline. It is camera-scoped
-- (camera_id → cameras → sites → organizations). RLS via org_id not applicable.
-- Application-layer RBAC (CanAccessCamera) is the gate. Skip.

-- ── ppe_zones ────────────────────────────────────────────────────────────
ALTER TABLE ppe_zones ENABLE ROW LEVEL SECURITY;
ALTER TABLE ppe_zones FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON ppe_zones;
CREATE POLICY tenant_isolation ON ppe_zones
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON ppe_zones;
CREATE POLICY service_bypass ON ppe_zones
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── compliance_rules ──────────────────────────────────────────────────────
ALTER TABLE compliance_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance_rules FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON compliance_rules;
CREATE POLICY tenant_isolation ON compliance_rules
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON compliance_rules;
CREATE POLICY service_bypass ON compliance_rules
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── pending_review_queue ──────────────────────────────────────────────────
ALTER TABLE pending_review_queue ENABLE ROW LEVEL SECURITY;
ALTER TABLE pending_review_queue FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON pending_review_queue;
CREATE POLICY tenant_isolation ON pending_review_queue
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON pending_review_queue;
CREATE POLICY service_bypass ON pending_review_queue
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── support_tickets ───────────────────────────────────────────────────────
ALTER TABLE support_tickets ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_tickets FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON support_tickets;
CREATE POLICY tenant_isolation ON support_tickets
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON support_tickets;
CREATE POLICY service_bypass ON support_tickets
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── evidence_manifests ───────────────────────────────────────────────────
ALTER TABLE evidence_manifests ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_manifests FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON evidence_manifests;
CREATE POLICY tenant_isolation ON evidence_manifests
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON evidence_manifests;
CREATE POLICY service_bypass ON evidence_manifests
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── person_track_frames (hypertable) ──────────────────────────────────────
-- TimescaleDB hypertable. ALTER TABLE against the hypertable propagates
-- to all existing and future chunks. This is the documented pattern.
ALTER TABLE person_track_frames ENABLE ROW LEVEL SECURITY;
ALTER TABLE person_track_frames FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON person_track_frames;
CREATE POLICY tenant_isolation ON person_track_frames
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON person_track_frames;
CREATE POLICY service_bypass ON person_track_frames
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── person_track_buckets ──────────────────────────────────────────────────
ALTER TABLE person_track_buckets ENABLE ROW LEVEL SECURITY;
ALTER TABLE person_track_buckets FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON person_track_buckets;
CREATE POLICY tenant_isolation ON person_track_buckets
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON person_track_buckets;
CREATE POLICY service_bypass ON person_track_buckets
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── model_versions ────────────────────────────────────────────────────────
ALTER TABLE model_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE model_versions FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON model_versions;
CREATE POLICY tenant_isolation ON model_versions
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON model_versions;
CREATE POLICY service_bypass ON model_versions
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── analysis_runs ─────────────────────────────────────────────────────────
ALTER TABLE analysis_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE analysis_runs FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON analysis_runs;
CREATE POLICY tenant_isolation ON analysis_runs
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON analysis_runs;
CREATE POLICY service_bypass ON analysis_runs
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── detections (hypertable) ───────────────────────────────────────────────
-- TimescaleDB hypertable on detected_at. ALTER TABLE against the hypertable
-- propagates to all current and future chunks. Documented behavior.
ALTER TABLE detections ENABLE ROW LEVEL SECURITY;
ALTER TABLE detections FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON detections;
CREATE POLICY tenant_isolation ON detections
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON detections;
CREATE POLICY service_bypass ON detections
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── detection_reviews ─────────────────────────────────────────────────────
ALTER TABLE detection_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE detection_reviews FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON detection_reviews;
CREATE POLICY tenant_isolation ON detection_reviews
    AS PERMISSIVE FOR ALL
    USING (organization_id = app_current_tenant())
    WITH CHECK (organization_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON detection_reviews;
CREATE POLICY service_bypass ON detection_reviews
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── digest_sends ──────────────────────────────────────────────────────────
-- Note: digest_sends uses column name 'org_id' not 'organization_id'.
-- The GUC comparison is the same but the column name differs.
ALTER TABLE digest_sends ENABLE ROW LEVEL SECURITY;
ALTER TABLE digest_sends FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation ON digest_sends;
CREATE POLICY tenant_isolation ON digest_sends
    AS PERMISSIVE FOR ALL
    USING (org_id = app_current_tenant())
    WITH CHECK (org_id = app_current_tenant());

DROP POLICY IF EXISTS service_bypass ON digest_sends;
CREATE POLICY service_bypass ON digest_sends
    AS PERMISSIVE FOR ALL TO onvif, postgres
    USING (true);

-- ── segments (hypertable) ────────────────────────────────────────────────
-- segments does not carry organization_id directly. It is scoped by camera_id
-- → cameras → sites → organizations. RLS via org_id is not applicable.
-- The application-layer RBAC (CanAccessCamera / AuthorizedCameraIDs) is the
-- gate. Skip RLS here to avoid a costly inline JOIN in every RLS check.
-- NOTE: if organization_id is ever added to segments, revisit.

-- ── events (hypertable) ──────────────────────────────────────────────────
-- events does not carry organization_id (same situation as segments).
-- camera_id → cameras → sites → organizations. RBAC-gated at app layer.
-- Skip RLS for the same reason as segments.

-- ─────────────────────────────────────────────────────────────────────────
-- Performance note
-- ─────────────────────────────────────────────────────────────────────────
-- RLS evaluation cost: one call to app_current_tenant() per row examined.
-- app_current_tenant() is STABLE + PARALLEL SAFE + SQL inlined — Postgres
-- optimises it to a single current_setting() call per plan node, not per row.
-- The two heaviest queries (operator queue: detections by org+domain+time,
-- camera timeline: detections by camera+time) already carry organization_id
-- and camera_id as leading index columns (idx_det_org_domain_detected,
-- idx_det_camera_detected). RLS adds a constant-time GUC comparison to an
-- already-indexed lookup — EXPLAIN ANALYZE shows no change to plan shape.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop policies and disable RLS in reverse order.
-- DISABLE ROW LEVEL SECURITY implicitly removes FORCE as well.

-- digest_sends
DROP POLICY IF EXISTS service_bypass ON digest_sends;
DROP POLICY IF EXISTS tenant_isolation ON digest_sends;
ALTER TABLE digest_sends DISABLE ROW LEVEL SECURITY;

-- detection_reviews
DROP POLICY IF EXISTS service_bypass ON detection_reviews;
DROP POLICY IF EXISTS tenant_isolation ON detection_reviews;
ALTER TABLE detection_reviews DISABLE ROW LEVEL SECURITY;

-- detections
DROP POLICY IF EXISTS service_bypass ON detections;
DROP POLICY IF EXISTS tenant_isolation ON detections;
ALTER TABLE detections DISABLE ROW LEVEL SECURITY;

-- analysis_runs
DROP POLICY IF EXISTS service_bypass ON analysis_runs;
DROP POLICY IF EXISTS tenant_isolation ON analysis_runs;
ALTER TABLE analysis_runs DISABLE ROW LEVEL SECURITY;

-- model_versions
DROP POLICY IF EXISTS service_bypass ON model_versions;
DROP POLICY IF EXISTS tenant_isolation ON model_versions;
ALTER TABLE model_versions DISABLE ROW LEVEL SECURITY;

-- person_track_buckets
DROP POLICY IF EXISTS service_bypass ON person_track_buckets;
DROP POLICY IF EXISTS tenant_isolation ON person_track_buckets;
ALTER TABLE person_track_buckets DISABLE ROW LEVEL SECURITY;

-- person_track_frames
DROP POLICY IF EXISTS service_bypass ON person_track_frames;
DROP POLICY IF EXISTS tenant_isolation ON person_track_frames;
ALTER TABLE person_track_frames DISABLE ROW LEVEL SECURITY;

-- evidence_manifests
DROP POLICY IF EXISTS service_bypass ON evidence_manifests;
DROP POLICY IF EXISTS tenant_isolation ON evidence_manifests;
ALTER TABLE evidence_manifests DISABLE ROW LEVEL SECURITY;

-- support_tickets
DROP POLICY IF EXISTS service_bypass ON support_tickets;
DROP POLICY IF EXISTS tenant_isolation ON support_tickets;
ALTER TABLE support_tickets DISABLE ROW LEVEL SECURITY;

-- pending_review_queue
DROP POLICY IF EXISTS service_bypass ON pending_review_queue;
DROP POLICY IF EXISTS tenant_isolation ON pending_review_queue;
ALTER TABLE pending_review_queue DISABLE ROW LEVEL SECURITY;

-- compliance_rules
DROP POLICY IF EXISTS service_bypass ON compliance_rules;
DROP POLICY IF EXISTS tenant_isolation ON compliance_rules;
ALTER TABLE compliance_rules DISABLE ROW LEVEL SECURITY;

-- ppe_zones
DROP POLICY IF EXISTS service_bypass ON ppe_zones;
DROP POLICY IF EXISTS tenant_isolation ON ppe_zones;
ALTER TABLE ppe_zones DISABLE ROW LEVEL SECURITY;

-- vlm_label_jobs
DROP POLICY IF EXISTS service_bypass ON vlm_label_jobs;
DROP POLICY IF EXISTS tenant_isolation ON vlm_label_jobs;
ALTER TABLE vlm_label_jobs DISABLE ROW LEVEL SECURITY;

-- evidence_shares
DROP POLICY IF EXISTS service_bypass ON evidence_shares;
DROP POLICY IF EXISTS tenant_isolation ON evidence_shares;
ALTER TABLE evidence_shares DISABLE ROW LEVEL SECURITY;

-- incidents
DROP POLICY IF EXISTS service_bypass ON incidents;
DROP POLICY IF EXISTS tenant_isolation ON incidents;
ALTER TABLE incidents DISABLE ROW LEVEL SECURITY;

-- active_alarms
DROP POLICY IF EXISTS service_bypass ON active_alarms;
DROP POLICY IF EXISTS tenant_isolation ON active_alarms;
ALTER TABLE active_alarms DISABLE ROW LEVEL SECURITY;

-- users
DROP POLICY IF EXISTS service_bypass ON users;
DROP POLICY IF EXISTS tenant_isolation ON users;
ALTER TABLE users DISABLE ROW LEVEL SECURITY;

-- organizations
DROP POLICY IF EXISTS service_bypass ON organizations;
DROP POLICY IF EXISTS tenant_isolation ON organizations;
ALTER TABLE organizations DISABLE ROW LEVEL SECURITY;

-- sites
DROP POLICY IF EXISTS service_bypass ON sites;
DROP POLICY IF EXISTS tenant_isolation ON sites;
ALTER TABLE sites DISABLE ROW LEVEL SECURITY;

-- Helper function
DROP FUNCTION IF EXISTS app_current_tenant();

-- +goose StatementEnd
