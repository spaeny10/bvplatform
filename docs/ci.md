# CI pipeline

GitHub Actions workflow at `.github/workflows/ci.yml`. Runs on every
PR and on direct pushes to `main`. Branch protection on the upstream
repo requires all four jobs to pass before merge.

## Jobs

| Job | What | Local reproduction |
|---|---|---|
| **Backend (Go)** | `go vet`, `golangci-lint`, `go build` (all 4 binaries), `go test ./... -race`, docs drift check (warn-only) | `make ci-backend` (TODO) or run the four commands by hand; `make docs-check` for the drift check |
| **Frontend (Next.js)** | `npm ci`, `tsc --noEmit`, `npm run lint`, `npm run build` | `cd frontend && npm ci && npx tsc --noEmit -p . && npm run lint && npm run build` |
| **Migrations** | Spin up Postgres+TimescaleDB, `goose up` → `goose reset` → `goose up` (idempotency round-trip) | See "Migration tests locally" below |
| **Secret scan (gitleaks)** | gitleaks v2 with the `.gitleaks.toml` allowlist | `gitleaks detect --source . --config .gitleaks.toml` |

Target wall-clock: under 10 minutes. The longest pole is the frontend
build (`next build` ≈ 3 min); the others run in parallel.

## How to triage failures

### golangci-lint fail

The job prints the offending file + line + rule name. Each enabled
linter is documented in `.golangci.yml` with a justification, so the
fix is usually obvious from the rule name (e.g., `errcheck` → check
the error; `gosec G401` → use `crypto/rand` not `math/rand`).

If you're sure it's a false positive, add an exclusion in
`.golangci.yml` under `issues.exclude-rules` — **don't** sprinkle
`//nolint:rule` directives across the codebase. The config is the
single source of truth.

Reproduce locally:
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0
golangci-lint run ./...
```

### Migration tests locally

The migration job needs a Postgres + TimescaleDB instance. Use the
same image CI uses:

```bash
docker run -d --name ci-migrations-test --rm \
    -e POSTGRES_PASSWORD=test -e POSTGRES_DB=testdb \
    -p 5432:5432 timescale/timescaledb:latest-pg15

# Wait for the DB to be ready
until docker exec ci-migrations-test pg_isready -U postgres; do sleep 1; done

# Install goose + run the same three steps CI runs
go install github.com/pressly/goose/v3/cmd/goose@latest
export DATABASE_URL="postgres://postgres:test@localhost:5432/testdb?sslmode=disable"
goose -dir migrations postgres "$DATABASE_URL" up
goose -dir migrations postgres "$DATABASE_URL" reset
goose -dir migrations postgres "$DATABASE_URL" up

docker stop ci-migrations-test
```

Common failure: a new migration's `Down` section is incomplete (forgets
to drop one of the objects the `Up` section created). The `reset` step
fails on the duplicate-object error during the second `up`. Fix by
adding the missing `DROP` to `-- +goose Down`.

### gitleaks fail

The job report identifies the file + commit + matched pattern. **First
ask: is this an actual secret?** If yes, rotate it immediately, then
amend the commit / open a new clean commit. If it's a fixture or
known-safe pattern, add it to `.gitleaks.toml` in the right section
(paths, stopwords, or regexes) — keep the addition narrow.

Reproduce locally:
```bash
docker run --rm -v "$PWD:/repo" zricethezav/gitleaks:latest \
    detect --source /repo --config /repo/.gitleaks.toml
```

### Frontend type-check (`tsc --noEmit`) fail

Usually an API-shape drift: the backend changed a `database.X` struct
field, the frontend's hand-typed `lib/api.ts` is now out of date. Fix:
update `frontend/src/types/ironsight.ts` or `frontend/src/lib/api.ts`
to match. A separate task P1-B-10 will dedupe the duplicate
API-client implementations that make this drift painful to catch.

### Frontend `npm run build` fail

`next build` runs its own type-check + ESLint pass too, but those run
AFTER the dedicated `tsc --noEmit` and `npm run lint` jobs — if both
of those passed in CI and the build still failed, the issue is
build-time only (server components / route conventions / static
generation). Run `npm run build` locally to repro.

### Docs drift warning

`cmd/docgen` cross-references the route table extracted from
`internal/api/router.go` against every fetch-family call site under
`frontend/src/`, and lints the hand-authored blocks in
`docs/feature-registry/`. The CI step is **warn-only** (`::warning`
annotations on the run; exit 0) so it never blocks a merge.

Triage:
- "api-coverage.md is stale" / "rollup table is stale" → run
  `make docs-gen` (or `go run ./cmd/docgen -write`) and commit.
- "frontend calls X but no backend route matches" → the call 404s at
  runtime. Fix the path/method, register the missing route, or delete
  the dead caller.
- "registry …" warnings → the feature block violates the schema
  documented in `docs/feature-registry/README.md` (field order, enums,
  nonexistent file paths, routes not in router.go).

To make the check blocking later, change the CI step to
`go run ./cmd/docgen -check -strict`.

## Adding a new check

1. Add the step to the appropriate job in `.github/workflows/ci.yml`.
2. Document it in the table above + a triage section here.
3. If it has a config file, keep that at the repo root (consistent
   with `.golangci.yml` and `.gitleaks.toml`).

## Notes

- **No code-coverage gate.** The phase plan calls it nice-to-have, not
  blocking. Reconsider when the test suite is mature enough that
  meaningful coverage thresholds are possible.
- **No production deploy automation here.** That's a separate task —
  this workflow is PR-quality gates only.
- **Cache.** Both backend and frontend jobs use the built-in GH Actions
  cache (Go modules, npm). First run is ~3 min slower until the cache
  warms up.
