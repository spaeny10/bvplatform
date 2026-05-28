# Data model — enum-like TEXT column conventions

Ironsight stores enum-like values as `TEXT` columns rather than Postgres
enum types. Trade-off: easier to add new values (no `ALTER TYPE` with
downtime), harder to keep accurate (free-text accepts typos). To narrow
the typo-acceptance window, the schema layers `CHECK (column IN (...))`
constraints on every column where the allowed value set is small + stable.

When you need to write a value to one of these columns that isn't in the
list below, **add it to the constraint via a new migration first**, then
add it to the writer. Don't disable the constraint, don't go around it
with `DELETE ... CHECK_constraint_violation; rewrite ...`.

All CHECK constraints in this schema use `NOT VALID`. That means:
- New writes are checked → typos fail with a clear constraint-violation
  error.
- Existing rows are not retroactively scanned. If you suspect a row
  with an invalid value exists, query it first, then `VALIDATE
  CONSTRAINT` once cleaned.

## Allowed-value catalog

Updated 2026-05-18 (P1-B-08). Migration source-of-truth is in
`migrations/000X_*.sql` files; this table is the human-readable index.

| Table | Column | Allowed values | Constraint name | Migration |
|---|---|---|---|---|
| `cameras` | `status` | `online`, `offline`, `degraded` | `cameras_status_chk` | 0004 |
| `cameras` | `recording_mode` | `continuous`, `event` | `cameras_recording_mode_chk` | 0004 |
| `speakers` | `status` | `online`, `offline`, `degraded` | `speakers_status_chk` | 0004 |
| `active_alarms` | `severity` | `low`, `medium`, `high`, `critical` | `active_alarms_severity_chk` | baseline (0001) |
| `incidents` | `severity` | `low`, `medium`, `high`, `critical` | `incidents_severity_chk` | baseline (0001) |
| `incidents` | `status` | `active`, `acknowledged`, `resolved`, `closed` | `incidents_status_chk` | baseline (0001) |
| `support_tickets` | `status` | `open`, `answered`, `closed` | `support_tickets_status_chk` | baseline (0001) |
| `users` | `role` | `admin`, `soc_operator`, `soc_supervisor`, `site_manager`, `customer`, `viewer`, `guard` | `users_role_chk` | baseline (0001) |
| `vlm_label_jobs` | `status` | `pending`, `claimed`, `labeled`, `skipped` | `vlm_label_jobs_status_chk` | baseline (0001) |
| `vlm_labels` | `verdict` | `correct`, `incorrect`, `needs_correction` | `vlm_labels_verdict_chk` | baseline (0001) |

## Columns considered but NOT constrained yet

These exist with enum-like-looking defaults but were left unconstrained
in 0004 because either the column doesn't universally exist across all
deployments (baseline migration + fred production drifted), or the
writer set is uncertain enough to risk false-positives on legitimate
values. Future migrations will fold them in.

- `events.severity` — present in baseline migration, absent from fred's
  live `events` table. Investigate divergence first.
- `events.status` — same divergence as above.
- `operators.role` — single writer site sets `"operator"`; would
  constrain to `("operator", "supervisor")` but no current callers
  confirm "supervisor" is actually in use.
- `recording_jobs.status` — writer set spans multiple goroutines that
  weren't fully audited.
- `vlm_indexer_jobs.status` — same as recording_jobs.
- `notification_subscriptions.severity_min` — should constrain to the
  same set as `incidents.severity`, but the writer flow goes through
  a separate filter step. Audit before constraining.

## Adding a new allowed value

1. Write a new migration that drops + recreates the CHECK with the
   expanded value list. Example for adding `maintenance` to
   `cameras.status`:
   ```sql
   ALTER TABLE cameras DROP CONSTRAINT cameras_status_chk;
   ALTER TABLE cameras
       ADD CONSTRAINT cameras_status_chk
       CHECK (status IN ('online', 'offline', 'degraded', 'maintenance'))
       NOT VALID;
   ```
2. Update this table above.
3. Add the value to the writer code in the same PR.
4. Reverse-migrate (down) drops the constraint cleanly so a rollback
   doesn't trap rows with the new value.

## Adding a new constrained column

1. Identify the column + writer sites (`grep -rE 'columnname\s*=\s*"' internal/`).
2. Confirm the column exists in production (`\d table` against the live DB).
3. Confirm no existing rows have unexpected values
   (`SELECT DISTINCT column FROM table` against the live DB).
4. Write a NEW migration (not editing 0004 once shipped) with `NOT VALID`.
5. Add a row to the catalog table above.
6. Optional: schedule a `VALIDATE CONSTRAINT` follow-up once you're
   confident no violators exist.
