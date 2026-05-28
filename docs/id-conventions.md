# ID conventions

This document describes the canonical ID-type policy for the Ironsight schema
and explains why two tiers of primary key type exist.  The CI lint test
`internal/database/id_convention_test.go::TestSchemaIDConventions` enforces
these rules on every PR — a migration that violates the rules fails CI before
it can merge.

See also: [migrations.md](migrations.md) for migration authoring conventions.

---

## The two-tier rule

### Tier 1 — UUID PKs (default for all new tables)

All new tables **MUST** use:

```sql
id UUID PRIMARY KEY DEFAULT gen_random_uuid()
```

This is the canonical choice for any table that does not have a specific reason
to use human-readable IDs.  UUID PKs are opaque, globally unique, and correct
by default.

**Tables in this tier (non-exhaustive):** `cameras`, `users`, `speakers`,
`audio_messages`, `storage_locations`, `bookmarks`, `exports`, `vca_rules`,
`pending_review_queue`, `ppe_zones`, `compliance_rules`, `evidence_manifests`.

### Tier 2 — TEXT PKs (grandfathered; do NOT add new entries without sign-off)

The following tables use TEXT primary keys because their IDs are
**human-readable customer or operator codes** that appear in URLs, SOC
operator workflows, and external API responses.  Changing them to UUID would
require a multi-week, multi-phase slug-to-UUID remap, coordinated API-contract
cutover, and rewrite of every FK column in ~15 tables — a high-risk,
low-benefit operation documented as Interpretation A in the scope plan
`ironsight/backlog/plans/P3-INFRA-04-uuid-fk-standardization.md`.

The decision to keep these as TEXT was locked on 2026-05-27 after confirming
against fred's live DB that organization IDs (`co-bv-test`, `T5-903`, etc.) are
slug-format, not UUID-shaped strings.

| Table | Sample ID | Why TEXT |
|---|---|---|
| `organizations` | `co-bv-test`, `co-alpha001` | Customer slug; appears in URLs and breadcrumbs |
| `sites` | `T5-903`, `ACG-301` | Customer site code; appears in URLs |
| `incidents` | `INC-260527-0001` | SOC-generated sequential code |
| `active_alarms` | `ALM-260527-0001` | SOC-generated sequential code |
| `security_events` | `EVT-260527-0001` | SOC-generated sequential code |
| `site_sops` | assigned text keys | Operator-assigned |
| `company_users` | assigned text keys | Customer portal user keys |
| `operators` | `op-001` | SOC operator slug |

Two additional tables have TEXT "PKs" that are not entity record IDs:

| Table | PK column | Why TEXT |
|---|---|---|
| `revoked_tokens` | `jti` | JWT ID string; opaque token, not a record identity |
| `evidence_shares` | `token` | Share token; opaque string, not a record identity |

---

## FK column rule

**FK columns MUST match the type of their target PK.**

- A FK that references a `UUID` PK must be declared `uuid`.
- A FK that references a `TEXT` PK must be declared `text`.
- A FK that references a `BIGINT`/`BIGSERIAL` PK must be declared `bigint`.

There must be **no TEXT-typed FK column that logically points at a UUID PK.**
That was the latent bug in `vlm_label_jobs.camera_id` before migration 0027.

### History: `vlm_label_jobs.camera_id` (fixed in migration 0027)

`vlm_label_jobs.camera_id` was declared `TEXT` in the baseline but logically
referenced `cameras.id` which is `UUID`.  Pre-launch the column stored
UUID-shaped strings without the type system enforcing it, and there was no FK
constraint.  Migration `0027_vlm_label_jobs_camera_fk.sql` (P3-INFRA-04):

1. Converted the column from `TEXT` to `UUID` via `USING camera_id::uuid`
   (safe because all 5 existing rows on fred held valid UUID-shaped values).
2. Added `FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE SET NULL`.

---

## CI lint enforcement

`internal/database/id_convention_test.go::TestSchemaIDConventions` runs as
part of the `backend-integration` CI job (`.github/workflows/ci.yml`), which
spins a TimescaleDB service container and sets `DATABASE_URL`.  The test:

1. Queries `information_schema` for all public tables that have a TEXT primary
   key and fails if any table is not in the grandfathered allowlist above.
2. Queries for all `*_id` columns in non-allowlist tables that are declared
   as `text` or `character varying` and fails if any are found.

**To add a new TEXT-PK table legitimately:**

1. Add the table to `textPKAllowlist` in `id_convention_test.go` with a
   one-line justification string.
2. In the migration file, add a comment referencing this document and
   explaining the decision.
3. Include the justification in the PR description.

Without these steps, CI will block the PR.

---

## Quick reference for new migrations

```sql
-- Good: new entity table
CREATE TABLE IF NOT EXISTS my_things (
    id          UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id   UUID  NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    site_id     TEXT  NOT NULL REFERENCES sites(id),
    ...
);

-- Wrong: TEXT PK on a new entity table (fails CI lint)
CREATE TABLE IF NOT EXISTS my_things (
    id TEXT PRIMARY KEY,  -- blocked by TestSchemaIDConventions
    ...
);

-- Wrong: TEXT FK column pointing at a UUID PK (fails CI lint)
CREATE TABLE IF NOT EXISTS my_things (
    id          UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id   TEXT  NOT NULL,  -- must be UUID; fails TestSchemaIDConventions
    ...
);
```
