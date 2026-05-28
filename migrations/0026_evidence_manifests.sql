-- migrations/0026_evidence_manifests.sql
-- P3-INFRA-03: Evidence chain-of-custody manifests.
--
-- Creates the evidence_manifests append-only table.  Each row is an
-- immutable record that a specific artifact (clip export, compliance PDF,
-- evidence share) was generated, including:
--
--   • a canonical JSON payload (manifest_json) that was serialised and
--     signed with an ed25519 private key,
--   • the base64-encoded ed25519 signature,
--   • the key fingerprint (key_id) so any verifier can locate the right
--     public key from the keyring after a key rotation,
--   • the SHA-256 of the primary artifact bytes (artifact_sha256) so
--     the manifest anchors to a specific generated file,
--   • optional parent_manifest_id for chaining (e.g. a share manifest
--     that references the underlying export manifest).
--
-- Append-only enforcement: a BEFORE UPDATE/DELETE trigger reuses the
-- ironsight_prevent_mutation() function introduced in migration 0017.
-- The function is CREATE OR REPLACE so this migration is safe to replay.
--
-- Indices:
--   • (organization_id, created_at DESC)  — tenant timeline queries
--   • (artifact_type, artifact_id)        — look up manifest by artifact

-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS evidence_manifests (
    manifest_id        UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_type      TEXT        NOT NULL
                           CHECK (artifact_type IN ('clip_export','compliance_report','evidence_share')),
    artifact_id        TEXT        NOT NULL,
    organization_id    TEXT        NOT NULL,
    created_by         UUID        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    camera_ids         JSONB       NOT NULL DEFAULT '[]',
    segment_ids        JSONB       NOT NULL DEFAULT '[]',
    time_range_start   TIMESTAMPTZ,
    time_range_end     TIMESTAMPTZ,
    source_segment_hashes JSONB   NOT NULL DEFAULT '{}',
    artifact_sha256    TEXT        NOT NULL,
    signature          TEXT        NOT NULL,
    key_id             TEXT        NOT NULL,
    sig_algorithm      TEXT        NOT NULL DEFAULT 'ed25519'
                           CHECK (sig_algorithm IN ('ed25519')),
    parent_manifest_id UUID        REFERENCES evidence_manifests(manifest_id),
    manifest_json      JSONB       NOT NULL
);

-- Tenant timeline: list manifests for an org ordered by recency.
CREATE INDEX IF NOT EXISTS idx_evidence_manifests_org_created
    ON evidence_manifests (organization_id, created_at DESC);

-- Artifact look-up: given a type + id (e.g. report_id or event_id),
-- fetch the corresponding manifest row(s).
CREATE INDEX IF NOT EXISTS idx_evidence_manifests_artifact
    ON evidence_manifests (artifact_type, artifact_id);

-- Append-only trigger: reuse the shared prevention function from
-- migration 0017. CREATE OR REPLACE makes this idempotent.
CREATE OR REPLACE FUNCTION ironsight_prevent_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit table %.% is append-only (op=%)',
        TG_TABLE_SCHEMA, TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS evidence_manifests_append_only ON evidence_manifests;
CREATE TRIGGER evidence_manifests_append_only
    BEFORE UPDATE OR DELETE ON evidence_manifests
    FOR EACH ROW EXECUTE FUNCTION ironsight_prevent_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS evidence_manifests_append_only ON evidence_manifests;
DROP INDEX  IF EXISTS idx_evidence_manifests_artifact;
DROP INDEX  IF EXISTS idx_evidence_manifests_org_created;
DROP TABLE  IF EXISTS evidence_manifests;
-- Note: ironsight_prevent_mutation() is shared with migration 0017 audit
-- tables; do NOT drop the function here.

-- +goose StatementEnd
