# Demo data seeding

The `cmd/seed` binary populates a fresh Ironsight database with the
demo dataset used for product demos, integration tests, and
local-dev sessions: a handful of organizations + sites, fake SOC
operators, fake portal users, and a fixed roster of cameras. It is
**not** for production deployments — see the warning at the bottom.

## When it runs

The seeder runs:

- Manually, via `docker compose run --rm seed` or `bin/seed` against
  a database with the baseline migration applied.
- Automatically inside the api binary at startup **only when**
  `IRONSIGHT_SEED_DEMO=true` is set. The default for a fresh
  deployment is to skip seeding (changed in P1-B-09 — previously
  the api unconditionally seeded on every boot).

## Demo passwords

Pre-LOCAL-08, the seeder hardcoded `demo123` as the password for
every demo account. This put the same trivial password in the audit
log of every fresh installation, including the few customer pilots
the build had been pointed at.

The seeder now reads the demo password from environment variables:

| Env var | Affects | Default |
|---|---|---|
| `SEED_DEMO_PASSWORD` | **All** demo accounts (master override; wins if set) | unset |
| `SEED_DEMO_PASSWORD_OPERATOR` | SOC accounts (`jhayes`, `ctorres`, `rmorgan`) | `demo123` |
| `SEED_DEMO_PASSWORD_PORTAL` | Customer + site-manager accounts (`marcus.webb`, `spierce`, `priya.sharma`, `derek.lawson`) | `demo123` |

All three default to `demo123` so existing dev workflows keep working
without changes. The master `SEED_DEMO_PASSWORD` is the simplest
override for "I just want one password across all demo accounts."

Set the env vars on the seed-runner container (the api or the standalone
`seed` service in `docker-compose.yml`). Example for a customer demo
running on shared hardware:

```bash
SEED_DEMO_PASSWORD='$(openssl rand -base64 16)' \
  docker compose run --rm seed
```

The seed binary logs whether it's using the master override or the
per-role defaults so the operator can verify their env is in scope:

```
[SEED] using SEED_DEMO_PASSWORD master override for all demo accounts
```

It does **not** log the password value.

## Demo dataset shape

The seed creates:

- **3 SOC operator users**: Jordan Hayes (operator), Casey Torres
  (operator), Riley Morgan (supervisor). Linked to rows in the
  `operators` table (`op-001`/`op-002`/`op-003`).
- **3 customer organizations**: Apex Construction Group, Meridian
  Development, Ironclad Sites.
- **4 sites** spread across those orgs.
- **4 portal users**: site managers + one customer-role user. Linked
  to their respective org + site IDs.
- A small fixed camera roster (see `internal/seed/cameras.go`) so the
  Monitor page has tiles to render.

The script is idempotent — re-running against an already-seeded DB
is a no-op (each user is `INSERT ... WHERE username NOT EXISTS`).

## Wipe + reseed

For a clean slate during development:

```bash
docker compose down
docker volume rm ironsight_db_data    # wipe the postgres volume
docker compose up -d db
docker compose run --rm migrate up
docker compose run --rm seed
docker compose up -d api worker frontend
```

For partial re-seed (delete just the demo users and let the seeder
recreate them, e.g. after rotating passwords):

```sql
DELETE FROM users
WHERE username IN (
    'jhayes', 'ctorres', 'rmorgan',
    'marcus.webb', 'spierce', 'priya.sharma', 'derek.lawson'
);
```

…then re-run `docker compose run --rm seed`. The orgs/sites/cameras
stay intact.

## Production — DO NOT seed

The demo dataset is fixture data. Running the seeder against a
production database creates fake user accounts and fake organizations
that will sit in the customer-visible UI and the audit log forever.
Per `CLAUDE v2.md` the production deployment workflow is:

1. Apply migrations: `docker compose run --rm migrate up`
2. Leave `IRONSIGHT_SEED_DEMO` unset (or explicitly `false`).
3. Create the first real admin user via the admin UI's first-run
   bootstrap flow, or by hand via SQL.

If `cmd/seed` is invoked against a production DB by mistake, the
fix is the partial-reseed `DELETE` above followed by manually
recreating any real users whose usernames collided with the demo
set (unlikely — the demo usernames are fictional).
