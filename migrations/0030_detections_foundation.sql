-- +goose NO TRANSACTION
--
-- P4-SCHEMA-01: Detections Foundation Tables.
--
-- Creates the unified detection data architecture for Phase 4.
-- Four tables:
--
--   model_versions   — org-scoped registry of inference model binaries
--                      (DECISION-A: org-scoped; one row per org×model×version)
--   analysis_runs    — bounded execution context for live ingest and
--                      re-analysis runs (D-07 locked design)
--   detections       — TimescaleDB hypertable; canonical append-only detection
--                      event store; one row per detected object per frame
--                      (DECISION-C: one bbox per row)
--   detection_reviews — human operator correction annotations; corrections live
--                       here, NOT as new detection rows (DECISION-D2)
--
-- View:
--   detections_current — rows not superseded by a newer detection
--
-- Zones: detections carries BOTH zone_id (→ ppe_zones) AND vca_rule_id
-- (→ vca_rules) as mutually-exclusive nullable FKs (DECISION-B: keep separate).
-- person_track_buckets is untouched (DECISION-E: stays standalone).
--
-- NO TRANSACTION required: create_hypertable cannot run inside a transaction
-- block on all TimescaleDB versions. Same pattern as migration 0023.
--
-- Down-section safety note: DROP TABLE on a hypertable automatically removes
-- its retention policies and compression jobs. Do NOT call
-- remove_retention_policy() — the named-arg signature is absent from some
-- TimescaleDB versions and broke the CI round-trip in 0023.
--
-- FK type convention (from 0022/0023/0024 precedent):
--   organization_id → TEXT  (organizations.id is TEXT)
--   site_id         → TEXT  (sites.id is TEXT)
--   camera_id       → UUID  (cameras.id is UUID)
--   all other FKs   → UUID
--
-- TimescaleDB FK constraints on hypertables:
--   PostgreSQL requires a unique constraint on the referenced column for FK
--   declarations. When 'detections' is converted to a hypertable its 'id'
--   column cannot serve as a single-column PK/UNIQUE target because TimescaleDB
--   requires the partition key (detected_at) to be included in any primary key
--   on a hypertable (i.e. PRIMARY KEY would have to be (id, detected_at)).
--   A compound PK cannot be referenced by a single-column FK
--   (detection_reviews.detection_id, supersedes) from other tables.
--   Resolution: id is NOT NULL + UNIQUE via a post-hypertable index; supersedes
--   and detection_reviews.detection_id are plain UUID columns (no FK declared).
--   Application code enforces the referential semantics; the append-only
--   trigger prevents deletion so orphaned reviews cannot occur in practice.
--   Similarly, segments.id has no PK in the schema (hypertable, id is bigserial
--   NOT NULL without an explicit PRIMARY KEY constraint), so segment_id is a
--   plain BIGINT column with no FK declared.

-- +goose Up

-- ────────────────────────────────────────────────────────────────
-- model_versions
-- ────────────────────────────────────────────────────────────────
-- Tracks which model binary produced a detection. Org-scoped so each
-- org independently controls when it upgrades a model version and
-- RLS (P4-SCHEMA-07) can cover the table without cross-tenant joins.

CREATE TABLE IF NOT EXISTS model_versions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id TEXT        NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,

    model_name      TEXT        NOT NULL,
    -- e.g. 'yolo11-ppe', 'qwen2.5-vl', 'yolo11-security'

    version_tag     TEXT        NOT NULL,
    -- e.g. '11.0.0', '2.5-7B-Instruct'

    weights_hash    TEXT        NOT NULL DEFAULT '',
    -- SHA-256 of the weights file. '' if unknown (legacy import).

    model_domain    TEXT        NOT NULL
        CHECK (model_domain IN ('ppe', 'security', 'person_tracking', 'vlm_validation')),

    deployed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- When this version was first put into service for this org.

    retired_at      TIMESTAMPTZ,
    -- NULL = still active. Set when superseded or removed.

    params          JSONB       NOT NULL DEFAULT '{}',
    -- Catch-all for model-specific metadata.

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Prevents duplicate registrations for the same version within an org.
CREATE UNIQUE INDEX IF NOT EXISTS idx_mv_org_name_tag
    ON model_versions (organization_id, model_name, version_tag);

-- Org-scoped list (admin UI, RLS base).
CREATE INDEX IF NOT EXISTS idx_mv_org_domain
    ON model_versions (organization_id, model_domain);

-- ────────────────────────────────────────────────────────────────
-- analysis_runs
-- ────────────────────────────────────────────────────────────────
-- A bounded execution context. Every detection is produced within
-- exactly one run. Enables "re-run model v2 over last month and compare."

CREATE TABLE IF NOT EXISTS analysis_runs (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  TEXT        NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    model_version_id UUID        NOT NULL REFERENCES model_versions(id) ON DELETE RESTRICT,

    run_type         TEXT        NOT NULL
        CHECK (run_type IN ('live_ingest', 'reanalysis')),

    started_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at         TIMESTAMPTZ,
    -- NULL = still running (live_ingest) or open range.

    params           JSONB       NOT NULL DEFAULT '{}',
    -- Run-specific metadata: camera scope, footage window, reason text, etc.

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ar_org_type_started
    ON analysis_runs (organization_id, run_type, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_ar_model_version
    ON analysis_runs (model_version_id);

-- ────────────────────────────────────────────────────────────────
-- detections — hypertable on detected_at
-- ────────────────────────────────────────────────────────────────
-- id is NOT NULL only (no PRIMARY KEY clause) so that create_hypertable
-- can convert the table without requiring (id, detected_at) as a compound
-- PK. A UNIQUE index is created post-conversion for application lookups.
-- See note at top of file on TimescaleDB FK constraints.

CREATE TABLE IF NOT EXISTS detections (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    organization_id  TEXT        NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    -- RESTRICT: we do not want detections to silently disappear if an org
    -- is soft-deleted. Orgs are soft-deleted, not hard-deleted.

    site_id          TEXT                 REFERENCES sites(id) ON DELETE SET NULL,
    camera_id        UUID        NOT NULL REFERENCES cameras(id) ON DELETE RESTRICT,
    -- RESTRICT: camera soft-delete (0028) handles this cleanly; detection
    -- history survives even after a camera is soft-deleted.

    -- TEMPORAL ANCHOR -------------------------------------------------
    detected_at      TIMESTAMPTZ NOT NULL,
    -- Hypertable partition column. For live_ingest ≈ wall-clock.
    -- For reanalysis = footage timestamp, NOT the reanalysis run time.

    -- CLASSIFICATION --------------------------------------------------
    detection_class  TEXT        NOT NULL,
    -- YOLO class label ('no-hardhat', 'no-vest', 'person', etc.) or
    -- VLM-derived class. No CHECK: class space grows. Sub-views enforce.

    detection_domain TEXT        NOT NULL
        CHECK (detection_domain IN ('ppe', 'security', 'person_tracking', 'vlm_validation')),

    confidence       REAL        NOT NULL
        CHECK (confidence >= 0.0 AND confidence <= 1.0),

    -- SPATIAL ---------------------------------------------------------
    bounding_box     JSONB       NOT NULL DEFAULT '{}',
    -- {"x1":0.1,"y1":0.2,"x2":0.4,"y2":0.6} normalized 0.0–1.0.
    -- {} is valid for VLM detections with no pixel bbox.
    -- DECISION-C: one bounding box per row (not an array).

    zone_id          UUID                 REFERENCES ppe_zones(id) ON DELETE SET NULL,
    -- For PPE-domain detections: which zone triggered this.
    -- DECISION-B: stays as separate FK from vca_rule_id below.

    vca_rule_id      UUID                 REFERENCES vca_rules(id) ON DELETE SET NULL,
    -- For security-domain detections triggered by a VCA rule.
    -- Mutually exclusive with zone_id in application code.

    -- LINEAGE ---------------------------------------------------------
    model_version_id UUID        NOT NULL REFERENCES model_versions(id) ON DELETE RESTRICT,
    analysis_run_id  UUID        NOT NULL REFERENCES analysis_runs(id)  ON DELETE RESTRICT,

    segment_id       BIGINT,
    -- Evidence anchor: segments.id that produced this detection.
    -- No FK constraint: segments is a TimescaleDB hypertable with no
    -- standalone PK constraint (see file header note). Plain BIGINT;
    -- application code enforces lineage.

    frame_offset_ms  BIGINT,
    -- ms offset within the segment. NULL = sustained violation (time range).

    -- SOURCE ----------------------------------------------------------
    source           TEXT        NOT NULL
        CHECK (source IN ('live', 'reanalysis')),
    -- Denormalized from analysis_run.run_type for fast filtering.

    -- SUPERSEDE CHAIN -------------------------------------------------
    supersedes       UUID,
    -- Plain UUID (no FK declared — see file header note on hypertable
    -- FK constraints). When a reanalysis or correction produces a new row
    -- superseding an old one, the NEW row carries supersedes = <old_row_id>.
    -- The old row is NEVER updated or deleted (append-only trigger below).
    -- detections_current view: rows NOT referenced by any supersedes value.

    -- METADATA --------------------------------------------------------
    details          JSONB       NOT NULL DEFAULT '{}',
    -- Escape hatch: track_id, crop_params, raw VLM reasoning, etc.

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
    -- No updated_at: append-only.
);

-- Convert to TimescaleDB hypertable. 7-day chunks match person_track_frames
-- precedent; at fleet scale (~650k rows/day) a 7-day chunk is ~4.5M rows,
-- within TimescaleDB's sweet spot.
SELECT create_hypertable('detections', 'detected_at',
    chunk_time_interval => INTERVAL '7 days',
    if_not_exists => TRUE);

-- Non-unique index on id for GetDetection point lookups.
-- TimescaleDB requires any UNIQUE index on a hypertable to include the
-- partition key (detected_at), so we cannot declare a standalone UNIQUE
-- index on id alone (SQLSTATE TS103). A non-unique index covers the
-- GetDetection(id, org) query path; uniqueness is enforced at the
-- application layer (gen_random_uuid() collision probability is negligible).
CREATE INDEX IF NOT EXISTS idx_det_id
    ON detections (id);

-- Append-only trigger: reuse the ironsight_prevent_mutation() function
-- defined in migration 0001 / 0017. Same function used by audit_log,
-- playback_audits, deterrence_audits.
DROP TRIGGER IF EXISTS detections_append_only ON detections;
CREATE TRIGGER detections_append_only
    BEFORE UPDATE OR DELETE ON detections
    FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();

-- Primary operator queue: org + domain + time.
CREATE INDEX IF NOT EXISTS idx_det_org_domain_detected
    ON detections (organization_id, detection_domain, detected_at DESC);

-- Camera timeline (single-camera drill-down).
CREATE INDEX IF NOT EXISTS idx_det_camera_detected
    ON detections (camera_id, detected_at DESC);

-- VLM worker poll: PPE detections not yet reviewed.
CREATE INDEX IF NOT EXISTS idx_det_org_class_detected
    ON detections (organization_id, detected_at ASC)
    WHERE detection_domain = 'ppe';

-- Lineage query: all detections from an analysis run.
CREATE INDEX IF NOT EXISTS idx_det_analysis_run
    ON detections (analysis_run_id, detected_at DESC);

-- Supersede chain traversal — used by detections_current view.
CREATE INDEX IF NOT EXISTS idx_det_supersedes
    ON detections (supersedes)
    WHERE supersedes IS NOT NULL;

-- Compliance dashboard: site + domain + time range.
CREATE INDEX IF NOT EXISTS idx_det_site_domain_detected
    ON detections (site_id, detection_domain, detected_at DESC)
    WHERE site_id IS NOT NULL;

-- ────────────────────────────────────────────────────────────────
-- detection_reviews
-- ────────────────────────────────────────────────────────────────
-- Human operator correction annotations (DECISION-D2).
-- Corrections are not detection rows; they are annotations on an
-- existing detection. Allows multiple review passes without polluting
-- the detections sequence.
--
-- detection_id: plain UUID column (no FK to detections — hypertable FK
-- constraint limitation, see file header). Application code verifies
-- the detection exists and belongs to the same org before insert.
-- The append-only trigger on detections prevents deletion so orphaned
-- reviews cannot occur in practice.

CREATE TABLE IF NOT EXISTS detection_reviews (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  TEXT        NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    detection_id     UUID        NOT NULL,
    -- No FK: see note above re: hypertable FK constraints.

    reviewer_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    verdict          TEXT        NOT NULL
        CHECK (verdict IN ('confirmed', 'false_positive', 'uncertain')),

    notes            TEXT,

    reviewed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Primary lookup: list reviews for a detection.
CREATE INDEX IF NOT EXISTS idx_dr_detection
    ON detection_reviews (detection_id, reviewed_at DESC);

-- Org-scoped review history.
CREATE INDEX IF NOT EXISTS idx_dr_org_reviewed
    ON detection_reviews (organization_id, reviewed_at DESC);

-- ────────────────────────────────────────────────────────────────
-- detections_current view
-- ────────────────────────────────────────────────────────────────
-- Rows that have NOT been superseded by any other detection.
-- "Current" = latest version in the supersede chain.
-- NOT EXISTS + idx_det_supersedes resolves instantly when the index
-- covers few rows (pre-reanalysis epoch). Convert to MATERIALIZED
-- VIEW post-P4-SCHEMA-06 if query plans degrade.

CREATE OR REPLACE VIEW detections_current AS
    SELECT d.*
    FROM   detections d
    WHERE  NOT EXISTS (
        SELECT 1 FROM detections s WHERE s.supersedes = d.id
    );

-- +goose Down

-- Drop in reverse-dependency order.
DROP VIEW  IF EXISTS detections_current;
DROP TABLE IF EXISTS detection_reviews;

-- Dropping detections as a hypertable automatically removes retention
-- policies and compression jobs — no explicit remove_retention_policy call
-- needed (and the named-arg form is not in all TimescaleDB versions).
DROP TRIGGER IF EXISTS detections_append_only ON detections;
-- DROP INDEX statements before DROP TABLE are redundant for hypertables
-- (the hypertable drop cascades all chunk indexes) but are included for
-- documentation clarity on what was created.
DROP INDEX IF EXISTS idx_det_site_domain_detected;
DROP INDEX IF EXISTS idx_det_supersedes;
DROP INDEX IF EXISTS idx_det_analysis_run;
DROP INDEX IF EXISTS idx_det_org_class_detected;
DROP INDEX IF EXISTS idx_det_camera_detected;
DROP INDEX IF EXISTS idx_det_org_domain_detected;
DROP INDEX IF EXISTS idx_det_id;
DROP TABLE IF EXISTS detections;

DROP INDEX IF EXISTS idx_ar_model_version;
DROP INDEX IF EXISTS idx_ar_org_type_started;
DROP TABLE IF EXISTS analysis_runs;

DROP INDEX IF EXISTS idx_mv_org_domain;
DROP INDEX IF EXISTS idx_mv_org_name_tag;
DROP TABLE IF EXISTS model_versions;
