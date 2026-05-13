# Database migrations

Ironsight uses [`pressly/goose`](https://github.com/pressly/goose) v3 to
manage Postgres / TimescaleDB schema changes. Migrations are SQL files
in `migrations/` that are embedded into the api binary via
`//go:embed *.sql` (see `migrations/embed.go`) and applied at api startup.

This is a deliberate deployment shape: **the api server runs `goose.Up`
at startup, before any other DB-touching code executes.** A deployment
cannot accidentally skip a schema change — if the binary in the image
contains migration `0007_add_something.sql` and the running DB is at
version 6, version 7 lands the moment the new container boots, and
*then* the rest of startup runs.

## File layout

```
migrations/
├── embed.go                    # //go:embed *.sql — picks up new files automatically
├── 0001_baseline.sql           # idempotent snapshot captured from fred 2026-05-08
├── 0002_<...>.sql              # P1-B-02 (extracts the inline ALTER block from cmd/server)
└── ...                         # subsequent additive migrations
```

Migrations are numbered with a four-digit serial prefix (not Unix
timestamps) because the project lives at a small, hand-managed number of
migrations and a four-digit prefix is easier to scan in a PR diff.

## Authoring a new migration

```bash
# from the repo root, on a developer's machine (not inside the api container —
# the container's embed FS is read-only):
./migrate create add_camera_zone_column sql
# → created migrations/0007_add_camera_zone_column.sql
```

The scaffold lays out both the `Up` and `Down` directions inside
`-- +goose StatementBegin / -- +goose StatementEnd` markers. Every
Ironsight migration uses `StatementBegin/End` so a future `DO $$ ... $$`
block (or any statement that contains a `;`) can't break goose's parser.

### Idempotency convention

Forward migrations should be **safe to re-run on a partially-applied
database**. The reasoning is twofold:

1. The 0001 baseline runs against fred's already-populated schema. If
   we hadn't made every CREATE / ADD CONSTRAINT in it idempotent, the
   first goose-driven api boot would have failed with `relation already
   exists` errors.
2. If a future migration crashes halfway through and goose doesn't
   record `dirty=true` for it (rare, but possible — e.g. if the api
   process is killed mid-migration), we want a re-run on the next boot
   to recover gracefully rather than abort.

The patterns we standardised on in 0001:

| Statement form | Idempotent variant |
| --- | --- |
| `CREATE TABLE foo` | `CREATE TABLE IF NOT EXISTS foo` |
| `CREATE INDEX ix_foo` | `CREATE INDEX IF NOT EXISTS ix_foo` |
| `CREATE UNIQUE INDEX ...` | `CREATE UNIQUE INDEX IF NOT EXISTS ...` |
| `CREATE SEQUENCE foo` | `CREATE SEQUENCE IF NOT EXISTS foo` |
| `CREATE EXTENSION timescaledb` | `CREATE EXTENSION IF NOT EXISTS timescaledb` |
| `CREATE FUNCTION foo()` | `CREATE OR REPLACE FUNCTION foo()` |
| `CREATE TRIGGER ...` | wrap in `DO $idem$ BEGIN ... EXCEPTION WHEN duplicate_object THEN NULL; END $idem$;` |
| `ALTER TABLE foo ADD CONSTRAINT ...` | same DO-block wrap (no `IF NOT EXISTS` form pre-PG17) |
| `ALTER TABLE foo ADD COLUMN bar INT` | `ALTER TABLE foo ADD COLUMN IF NOT EXISTS bar INT` |
| `ALTER TABLE foo DROP COLUMN bar` | `ALTER TABLE foo DROP COLUMN IF EXISTS bar` |
| `SELECT create_hypertable('foo', 'ts')` | `SELECT create_hypertable('foo', 'ts', if_not_exists => TRUE, migrate_data => TRUE)` |

Down migrations get the same treatment — `DROP ... IF EXISTS`, `ALTER
TABLE ... DROP COLUMN IF EXISTS`. A failed-then-retried `down` should
recover rather than wedge.

## Testing a migration locally

```bash
# Spin a throwaway TimescaleDB-on-Postgres container:
docker run --rm -d --name ironsight-pg-test \
  -e POSTGRES_PASSWORD=test -p 15432:5432 \
  timescale/timescaledb:latest-pg15

export DATABASE_URL='postgres://postgres:test@127.0.0.1:15432/postgres?sslmode=disable'

# From the repo root (golang:1.25-bookworm has the right toolchain):
go run ./cmd/migrate up
go run ./cmd/migrate status     # expect: applied
go run ./cmd/migrate down
go run ./cmd/migrate status     # expect: pending
go run ./cmd/migrate up         # second up — confirms idempotency
go run ./cmd/migrate redo       # smoke-tests down+up of the latest migration

docker stop ironsight-pg-test
```

If `redo` fails on a migration you authored, the `Down` is broken; the
correct fix is to make `Down` reversible and/or wrap destructive parts
in `IF EXISTS`.

## Production behaviour

* The api server (`cmd/server/main.go`) calls `goose.UpContext` after
  `database.New` succeeds and **before** the rest of startup. Failing
  to apply migrations is fatal — the binary exits with `[FATAL]
  goose.Up: ...` rather than serving with a stale schema.
* The legacy inline `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` block in
  `cmd/server/main.go` runs immediately after `goose.Up` for now (it
  will be deleted in P1-B-02 once each ALTER becomes a numbered
  migration file). Both passes use `IF NOT EXISTS`, so the order is
  safe — goose never undoes what the inline block adds.
* Operators inspect / repair via the `migrate` CLI:

  ```bash
  docker compose run --rm api /app/migrate status
  docker compose run --rm api /app/migrate version
  docker compose run --rm api /app/migrate down       # rare; rolls back last applied
  docker compose run --rm api /app/migrate up         # roll forward again
  ```

  Both binaries (`/app/server` and `/app/migrate`) ship from the same
  build, so they always agree on what migrations exist — there is no
  separate "migration image."

## How rollback works

`migrate down` reverts the most recent applied migration by running its
`-- +goose Down` block, then decrements the `goose_db_version` row. A
subsequent `migrate up` re-applies it. The standard up→down→up cycle is
the defacto smoke test for any new migration before merge.

If a `down` is too dangerous to run in production (e.g. it would drop a
column with months of data), authoring guidance is to make the `down`
explicitly refuse:

```sql
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
  RAISE EXCEPTION 'rollback of 0042 is intentionally disabled — see runbook';
END $$;
-- +goose StatementEnd
```

…and then provide a follow-up migration with the actual revert if it's
ever needed.

## One-time bootstrap: why 0001 is idempotent

When P1-B-01 landed, fred was already running a populated Ironsight DB
that had been ALTER'd into shape by the inline block in
`cmd/server/main.go` over many deploys. We needed to:

1. Capture the schema as it actually existed (`pg_dump --schema-only`).
2. Get goose to track that schema as version 1.
3. **Not** disturb the live DB while doing so.

Step 3 is what makes 0001 different from a normal forward migration:
*every* DDL statement in it has to succeed silently when the object
already exists. Hence the extensive `IF NOT EXISTS` / `DO $idem$ BEGIN
... EXCEPTION` rewrites described above.

When the api binary first boots against fred with goose wired in, the
0001 baseline runs against a DB that already matches its target
schema, every DDL is a no-op, and goose simply records `version=1,
dirty=false` in `goose_db_version`. From that point on, ordinary
incremental migrations land normally.

A fresh DB (CI, a developer laptop, a new customer install) sees the
opposite: 0001 builds the schema from scratch, 0002+ apply on top, and
the `goose_db_version` table ends up at the same version as fred's.
