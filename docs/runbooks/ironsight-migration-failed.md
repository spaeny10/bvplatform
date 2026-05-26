# Alert: IronsightMigrationBehind

## Severity
warning

## What fired

`ironsight_goose_migration_version < <CURRENT_VERSION>` for 5 minutes.
The DB schema version at startup is below the expected version. Either goose
failed to apply a migration, or a new binary was deployed without running
migrations first.

## Immediate actions

1. Check startup logs: `docker logs ironsight --tail 100 2>&1 | grep -i "migration\|goose"`
2. Look for "[FATAL] goose.Up:" lines indicating a migration failure.
3. Identify the current DB version:
   `docker exec ironsight-db psql -U ironsight -c "SELECT * FROM goose_db_version ORDER BY id DESC LIMIT 5;"`
4. Compare with the expected version in `deploy/monitoring/alerts.yml`
   (`IronsightMigrationBehind` expr threshold).
5. If the binary is ahead of the DB: run migrations manually:
   `docker exec ironsight /app/migrate up`
6. If migration fails: check the migration SQL for conflicts with existing schema.

## Likely causes

- New binary deployed before running `migrate up`
- A migration failed with a SQL error (e.g. constraint violation, missing table)
- goose_db_version table missing or inaccessible

## Resolution

Alert resolves when `ironsight_goose_migration_version >= <CURRENT_VERSION>`.
After running migrations, restart the Ironsight binary so it re-records the
version in the gauge at startup:
`docker restart ironsight`

## After each migration release

Update the threshold in `/etc/prometheus/alerts.yml` on the monitoring LXC
to match the new migration version number, then reload Prometheus:
`systemctl reload prometheus`

## Escalation

If a migration is failing with SQL errors, do not retry blindly — contact
Caleb before running any manual SQL on the production database.
