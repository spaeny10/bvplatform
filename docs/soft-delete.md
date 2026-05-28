# Soft-Delete Pattern (P3-INFRA-05)

Implemented: 2026-05-27  
Migration: `0028_soft_delete.sql`  
Branch: `feat/p3-infra-05-soft-delete`

## Overview

Ironsight uses a soft-delete pattern for all primary entity tables. Instead of removing rows from the database on DELETE, a `deleted_at TIMESTAMPTZ` column is set to the current timestamp. `NULL` means the row is live; any non-null timestamp means the row is deleted.

This preserves referential integrity for audit logs, recorded segments, alarm history, and chain-of-custody records — all of which may reference cameras, users, sites, or organizations long after they've been "deleted" by an operator. The rows persist with their UUID, enabling forensic lookups even after the entity is gone from the operational UI.

## Affected Tables

The following 8 tables participate in soft-delete:

| Table | Cascade on delete |
|-------|-------------------|
| `cameras` | → `ppe_zones`, `compliance_rules`, `vca_rules` |
| `sites` | → cameras → ppe_zones, compliance_rules, vca_rules |
| `organizations` | → sites (and transitively all descendants) |
| `users` | None (user accounts outlive org deletions) |
| `speakers` | None |
| `ppe_zones` | → `compliance_rules` |
| `compliance_rules` | None |
| `vca_rules` | None |

## Excluded Tables

The following tables are **permanently excluded** from soft-delete. They are append-only audit records, TimescaleDB hypertables, SOC state tables, or join tables where hard-delete semantics are correct:

- `audit_log`, `playback_audits`, `deterrence_audits`
- `evidence_manifests`, `segments`, `events`, `person_track_frames`, `ai_runtime_metrics`
- `active_alarms`, `incidents`, `security_events`
- `company_users` (membership join — hard-delete is correct)

Adding `deleted_at` to any of these tables will fail the CI lint test `TestSoftDeleteConventions`.

## _active Views

For every soft-delete table, a corresponding `_active` view exists:

```sql
CREATE VIEW cameras_active AS SELECT * FROM cameras WHERE deleted_at IS NULL;
```

All normal read paths in `internal/database/` query `<table>_active` views, never the base table. Write paths (inserts, updates, soft-deletes) always use the base table.

## Partial Unique Indexes

Unique constraints on mutable fields are replaced by partial unique indexes scoped to live rows:

```sql
-- cameras: token uniqueness only among live cameras
CREATE UNIQUE INDEX idx_cameras_sense_token
  ON cameras (sense_webhook_token)
  WHERE sense_webhook_token IS NOT NULL AND deleted_at IS NULL;

-- users: username uniqueness only among live users
CREATE UNIQUE INDEX users_username_active_key
  ON users (username)
  WHERE deleted_at IS NULL;
```

This means a deleted camera's `sense_webhook_token` and a deleted user's `username` are freed for reuse once the row is soft-deleted. This is intentional ("slug recycling acceptable" — scope decision 2026-05-27).

## SSO User Resurrection

`GetOrCreateUserByEmail` handles the case where a Google SSO user was previously soft-deleted and then re-authenticates:

1. Check `users_active` — live user found → return as-is.
2. Check base `users` table for a deleted match by email → `UPDATE deleted_at=NULL, updated_at=NOW()` (resurrection).
3. Neither found → create fresh row.

Without step 2, the partial unique index on `users_username_active_key` would reject the INSERT for the previously-deleted username, causing a 500 on SSO login.

## Cascade Semantics

### Camera → children (in one transaction)
```
SoftDeleteCamera(cameraID)
  ├─ UPDATE compliance_rules SET deleted_at=now WHERE camera_id=$1 AND deleted_at IS NULL
  ├─ UPDATE ppe_zones        SET deleted_at=now WHERE camera_id=$1 AND deleted_at IS NULL
  └─ UPDATE vca_rules        SET deleted_at=now WHERE camera_id=$1 AND deleted_at IS NULL
  └─ UPDATE cameras          SET deleted_at=now WHERE id=$1 AND deleted_at IS NULL
```

### Site → cameras → children (one transaction)
```
SoftDeleteSite(siteID)
  ├─ UPDATE compliance_rules (camera-bound) via camera subquery
  ├─ UPDATE compliance_rules (site-wide, camera_id IS NULL)
  ├─ UPDATE ppe_zones  via site_id
  ├─ UPDATE vca_rules  via camera subquery
  ├─ UPDATE cameras    via site_id
  └─ UPDATE sites      WHERE id=$1
```

### Organization → sites (sequential, each in own tx)
```
SoftDeleteOrganization(orgID)
  ├─ SELECT live site IDs
  ├─ SoftDeleteSite(site1)   ← own tx
  ├─ SoftDeleteSite(site2)   ← own tx
  └─ UPDATE organizations SET deleted_at=now WHERE id=$1
```

The org-level operation is not all-or-nothing. Partial completion is monotonically safe at pre-launch volume. A compensation log or 2PC approach is deferred to P4.

### PPEZone → compliance_rules (one transaction)
```
SoftDeletePPEZone(zoneID, orgID)
  ├─ UPDATE compliance_rules SET deleted_at=now WHERE zone_id=$1 AND deleted_at IS NULL
  └─ UPDATE ppe_zones        SET deleted_at=now WHERE id=$1 AND deleted_at IS NULL
```

The former 409 guard ("zone has active compliance rules — delete rules first") is removed. With soft-delete, cascading rules preserves their history and the operator never sees data loss.

## API: include_deleted Parameter

Admin-only callers may append `?include_deleted=true` to list endpoints to include soft-deleted rows:

```
GET /api/cameras?include_deleted=true        (admin only)
GET /api/sites?include_deleted=true          (admin only)
GET /api/organizations?include_deleted=true  (admin only)
GET /api/users?include_deleted=true          (admin only)
GET /api/speakers?include_deleted=true       (admin only)
GET /api/cameras/{id}/ppe-zones?include_deleted=true        (admin only)
GET /api/cameras/{id}/compliance-rules?include_deleted=true (admin only)
```

Non-admin callers receive `403 Forbidden` if they pass this flag. Tenant scope (`organization_id`) is still enforced on all scoped endpoints — soft-delete never weakens tenant isolation.

## No Undelete Endpoint

There is no `/restore` or undelete endpoint in v1. This is a deliberate scope decision (P3-INFRA-05 open question #2). If restoration is needed operationally, an admin can run:

```sql
UPDATE cameras SET deleted_at = NULL WHERE id = '<uuid>';
```

A proper undelete API is deferred to a future phase.

## No Hard Purge / GDPR Endpoint

Physical row deletion is out of scope for v1 (P3-INFRA-05 open question #1). The hard-delete functions (`DeleteCamera`, `DeleteUser`, etc.) are retained in the database layer for internal/admin use only and are not exposed via the API. A GDPR purge path is deferred.

## Migration Safety

Migration `0028_soft_delete.sql` uses `IF NOT EXISTS` guards on all `ALTER TABLE ADD COLUMN` and `CREATE INDEX` statements so it is safe to re-run. The `-- +goose Down` section fully reverses the migration (drops columns, views, and indexes; restores the original `users_username_key` UNIQUE constraint).

The migration does NOT backfill `deleted_at` for existing rows — `NULL` means live, and all existing rows are live.
