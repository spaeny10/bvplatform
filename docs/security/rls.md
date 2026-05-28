# Row-Level Security (RLS) — P4-SCHEMA-07

## What is enforced

Migration `0031_rls_policies.sql` enables PostgreSQL Row-Level Security as a
**defense-in-depth** layer on top of the existing application-level tenant
filtering. Even if an application bug, future refactor, or raw-SQL leak
bypasses the `WHERE organization_id = ?` predicate, the database itself will
reject cross-tenant reads and writes.

### Tables covered

Every customer-data table that carries a direct `organization_id` column:

| Table | Policy column |
|---|---|
| `sites` | `organization_id` |
| `organizations` | `id` (the PK *is* the tenant ID) |
| `users` | `organization_id` |
| `active_alarms` | `organization_id` |
| `incidents` | `organization_id` |
| `evidence_shares` | `organization_id` |
| `vlm_label_jobs` | `organization_id` |
| `ppe_zones` | `organization_id` |
| `compliance_rules` | `organization_id` |
| `pending_review_queue` | `organization_id` |
| `support_tickets` | `organization_id` |
| `evidence_manifests` | `organization_id` |
| `person_track_frames` (hypertable) | `organization_id` |
| `person_track_buckets` | `organization_id` |
| `model_versions` | `organization_id` |
| `analysis_runs` | `organization_id` |
| `detections` (hypertable) | `organization_id` |
| `detection_reviews` | `organization_id` |
| `digest_sends` | `org_id` |

### Tables NOT covered by RLS

Tables that lack a direct `organization_id` column, or where RLS would add
unacceptable join overhead without security benefit, are excluded:

- `cameras`, `segments`, `events` — scoped by `camera_id → sites → organizations`; RBAC-gated at the application layer. Adding a join inside every RLS check would degrade query plans on the heaviest hypertables.
- `vca_rules` — camera-scoped only, no `organization_id`.
- `audit_log` — no `organization_id` column; scoped by `user_id`.
- `goose_db_version`, `revoked_tokens`, `system_settings`, `storage_locations`, `alarm_queue`, `deterrence_audits`, `playback_audits`, `device_assignments`, `shift_handoffs`, `operators`, `site_sops`, `security_events`, `exports`, `evidence_share_opens`, `notification_subscriptions`, `vlm_labels` — no tenant concept or already protected by other means.

## The `app.current_tenant` contract

RLS policies compare `organization_id` against a per-connection GUC:

```sql
SELECT current_setting('app.current_tenant', true)
```

The `true` flag prevents an error when the GUC is unset; it returns `NULL`
instead. A `NULL` tenant causes the USING clause to evaluate to `NULL = NULL`
(unknown, not TRUE) so zero rows are visible to a non-bypass connection.

**Setting the GUC** — application code must call:

```go
conn, tx, err := database.AcquireWithTenant(ctx, db.Pool, tenantOrgID)
if err != nil { ... }
defer conn.Release()
defer tx.Rollback(ctx)
// ... run queries on tx ...
tx.Commit(ctx)
```

`AcquireWithTenant` uses `SET LOCAL` (transaction-scoped) so the GUC is
automatically cleared when the transaction ends. The pool connection is safe
to reuse without leaking tenant state.

**Where the tenant comes from** — `RLSMiddleware` in `internal/api/rls_middleware.go`
reads `claims.OrganizationID` from the JWT/SSO claims resolved by `RequireAuth`
and stores it in the request context. Handlers retrieve it with
`api.TenantFromContext(r.Context())`.

## Service mode

The `onvif` and `postgres` database roles have a `service_bypass` policy
(PERMISSIVE FOR ALL, USING (true)) that grants unrestricted access regardless
of the GUC. These roles are used by:

- Migration runner (`cmd/migrate`)
- Seed tool (`cmd/seed`)
- Background workers (PPE, VLM, consistency-check, digest sender)
- Emergency DBA access

Workers that iterate per-org for correctness (e.g. monthly digest) should
still call `AcquireWithTenant` for each org to get a belt-and-suspenders
cross-tenant check on their own queries.

## Debugging "I can't see my rows"

Common causes:

1. **GUC not set** — The handler is using `db.Pool.Query` directly instead of
   going through `AcquireWithTenant`. The query runs without a tenant GUC;
   with service-bypass role the data is visible, but if the table has FORCE
   RLS the bypass only helps the bypass-role connection.

2. **Wrong tenant format** — `app.current_tenant` is set as a quoted string
   (e.g. `'myorg'` with extra single quotes) instead of a plain string
   (`myorg`). Always pass the value via parameterised query (`$1`) not string
   interpolation.

3. **Empty string vs NULL** — An empty-string tenant (`SET LOCAL app.current_tenant = ''`)
   causes `'' = organization_id` which is FALSE for any non-empty org ID.
   The RLS helper (`AcquireWithTenant`) does not validate that the tenant is
   non-empty. Callers must ensure they do not pass `""` for a customer request.

4. **Service-role connection bypassing** — If a worker or admin endpoint
   connects as `onvif` (the default pool user), it has service_bypass and
   sees all rows. That is correct behavior. If a customer-scope endpoint
   appears to bypass RLS, check that it is using `AcquireWithTenant` and not
   a direct pool query.

5. **TimescaleDB chunk visibility** — RLS on hypertables propagates to chunks.
   If you ALTER a chunk directly (never do this) it will not have the policy.
   Always ALTER the hypertable itself; TimescaleDB handles chunk inheritance.

## Round-trip safety

The DOWN section of migration 0031 disables RLS on every table and drops all
policies, then drops `app_current_tenant()`. CI runs a full up/down/up
round-trip on every PR to verify this is clean.

## INFRA_MASTER note

No new env vars, cron jobs, or system services are introduced by this migration.
The only operational contract is:

- `app.current_tenant` GUC must be set (via `AcquireWithTenant`) on any
  connection that should see tenant-scoped data.
- `onvif` role retains full access via `service_bypass` policy.
