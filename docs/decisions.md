# Architecture Decision Log

This file records cross-cutting architectural decisions made during Ironsight
platform development. Major decisions within a single domain (e.g. detection
schema decisions D-A through D-E) are noted inline in the relevant migration
or doc file; this log captures platform-level choices.

---

## Security

### RLS: Postgres Row-Level Security as defense-in-depth (P4-SCHEMA-07)

**Decision**: Add PostgreSQL Row-Level Security policies to all customer-data
tables that carry `organization_id`. The DB enforces tenant isolation as a
second layer independent of application code.

**Mechanism**: A GUC (`app.current_tenant`) is set per-request via
`SET LOCAL` inside an explicit transaction. RLS USING/WITH CHECK policies
compare the column value against `app_current_tenant()`. Schema-owner roles
(`onvif`, `postgres`) have a `service_bypass` policy for migration/worker paths.

**Option A vs Option B**: Option B (explicit `AcquireWithTenant` helper) was
chosen over Option A (connection-in-context for all handlers) because Option A
would require rerouting ~200 `db.Pool.Query` call sites across 15 files — too
wide a blast radius for a defense-in-depth layer. Option B scopes the new
pattern to handlers that opt in, with the existing application-layer filtering
remaining the primary enforcement mechanism.

**Full details**: [docs/security/rls.md](./security/rls.md)

---

## Data Model

### TEXT primary keys for organizations and sites

Organizations and sites use TEXT slug IDs (`id TEXT NOT NULL`) rather than
UUIDs. This predates the UUID migration. All FK columns referencing these
tables are TEXT. Do not change this without a full cross-schema migration.

See [docs/id-conventions.md](./id-conventions.md).

### Append-only tables

`detections`, `detection_reviews`, `evidence_manifests`, `audit_log`,
`playback_audits`, `deterrence_audits` are enforced append-only by the
`ironsight_prevent_mutation()` trigger. No UPDATE or DELETE is permitted.
Corrections are expressed as new rows (supersede chains for detections,
new review rows for detection_reviews).

### TimescaleDB hypertables

`segments`, `events`, `ai_runtime_metrics`, `person_track_frames`, `detections`
are TimescaleDB hypertables. FK constraints and standalone UNIQUE indexes on
the partition key are not supported. See migration 0030 header for the full
constraint policy.
