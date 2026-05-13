// Package main — Ironsight migration operator CLI.
//
// Thin wrapper around pressly/goose v3 for ops use. The api server applies
// migrations automatically at startup (see cmd/server/main.go), so this
// CLI is for the cases startup-apply doesn't cover:
//
//   - Inspecting state ahead of a deploy: `migrate status` / `migrate version`.
//   - Recovering from a botched deploy: `migrate down` reverts the last
//     migration so a fresh build can be rolled forward again.
//   - Authoring new migrations: `migrate create <name> sql` scaffolds
//     a 000N_<name>.sql file in the on-disk migrations/ directory with
//     the goose StatementBegin/StatementEnd markers ready to fill in.
//
// Reads DATABASE_URL from the environment via internal/config — same
// plumbing the api server and the seed binary use. No new flags or env
// vars beyond what's already wired up.
//
// Typical operator invocations (run from the api container so the CLI
// has the same view of migrations as the server that just ran them):
//
//	docker compose run --rm api /app/migrate status
//	docker compose run --rm api /app/migrate version
//	docker compose run --rm api /app/migrate up
//	docker compose run --rm api /app/migrate down
//	docker compose run --rm api /app/migrate up-to 5
//	# create new migration on dev box (not in container; needs disk write):
//	./migrate create add_camera_zone_column sql
//
// All subcommands except `create` operate against the embedded migrations
// FS so the CLI stays in lockstep with the server binary it shipped with.
// `create` is the exception: it scaffolds onto disk for the developer to
// commit, since the embed FS is read-only.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/migrations"
)

// onDiskMigrationsDir is the path goose writes new migration files into
// when `migrate create <name> sql` is invoked. We deliberately resolve
// this relative to the current working directory rather than the binary's
// install location: the CLI is meant to be run from the repo root on a
// developer's machine when scaffolding a new migration.
const onDiskMigrationsDir = "migrations"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		return errors.New("no subcommand provided")
	}

	// Handle --help / -h before anything else so the binary works without
	// DATABASE_URL configured (useful in CI / for new operators).
	switch args[0] {
	case "-h", "--help", "help":
		usage()
		return nil
	}

	sub := args[0]
	subArgs := args[1:]

	// `create` is special — it doesn't need a DB connection and writes to
	// the on-disk migrations directory rather than reading from the embed.
	if sub == "create" {
		return cmdCreate(subArgs)
	}

	// Everything else needs a live DB.
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required (set in env or via internal/config defaults)")
	}

	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database connect: %w", err)
	}
	defer db.Close()

	// goose works against database/sql; pgx's stdlib package bridges the
	// pgxpool we already have. The bridged *sql.DB shares the underlying
	// pool, so closing the pool above is sufficient.
	sqlDB := stdlib.OpenDBFromPool(db.Pool)
	defer sqlDB.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose set dialect: %w", err)
	}
	goose.SetBaseFS(migrations.FS)

	ctx := context.Background()

	switch sub {
	case "status":
		return goose.StatusContext(ctx, sqlDB, ".")
	case "version":
		return goose.VersionContext(ctx, sqlDB, ".")
	case "up":
		return goose.UpContext(ctx, sqlDB, ".")
	case "up-to":
		if len(subArgs) != 1 {
			return errors.New("up-to requires a target version, e.g. `migrate up-to 5`")
		}
		v, err := strconv.ParseInt(subArgs[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", subArgs[0], err)
		}
		return goose.UpToContext(ctx, sqlDB, ".", v)
	case "up-by-one":
		return goose.UpByOneContext(ctx, sqlDB, ".")
	case "down":
		return goose.DownContext(ctx, sqlDB, ".")
	case "down-to":
		if len(subArgs) != 1 {
			return errors.New("down-to requires a target version, e.g. `migrate down-to 0`")
		}
		v, err := strconv.ParseInt(subArgs[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", subArgs[0], err)
		}
		return goose.DownToContext(ctx, sqlDB, ".", v)
	case "redo":
		return goose.RedoContext(ctx, sqlDB, ".")
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

// cmdCreate scaffolds a new migration file on disk. We bypass goose.Create
// for type=sql because goose's built-in template doesn't include the
// StatementBegin/StatementEnd markers we standardised on (every Ironsight
// migration uses StatementBegin/End so a future DO/BEGIN block can't break
// the parser). For type=go we delegate to goose.Create which has a sane
// Go-source template.
func cmdCreate(args []string) error {
	if len(args) < 1 {
		return errors.New("create requires a name, e.g. `migrate create add_foo_table sql`")
	}
	name := args[0]
	kind := "sql"
	if len(args) >= 2 {
		kind = args[1]
	}
	if kind != "sql" && kind != "go" {
		return fmt.Errorf("unsupported migration type %q (expected sql or go)", kind)
	}

	if err := os.MkdirAll(onDiskMigrationsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", onDiskMigrationsDir, err)
	}

	if kind == "go" {
		// goose handles Go templates well; let it run.
		return goose.Create(nil, onDiskMigrationsDir, name, "go")
	}

	// SQL template: deliberately includes StatementBegin/StatementEnd.
	// Filename uses goose's standard timestamp-prefix scheme so files
	// sort in author order and the next sequential number is obvious.
	stamp, err := nextSerial(onDiskMigrationsDir)
	if err != nil {
		return fmt.Errorf("compute next migration number: %w", err)
	}
	safeName := sanitizeName(name)
	fileName := fmt.Sprintf("%04d_%s.sql", stamp, safeName)
	path := filepath.Join(onDiskMigrationsDir, fileName)

	body := `-- +goose Up
-- +goose StatementBegin
-- TODO: forward migration. Prefer idempotent forms — IF NOT EXISTS for
-- CREATE, DO $idem$ BEGIN ... EXCEPTION WHEN duplicate_object THEN NULL
-- END $idem$ for ADD CONSTRAINT / CREATE TRIGGER / CREATE TYPE — so that
-- re-running the migration on a partially-applied database is safe.
SELECT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- TODO: reversal. Must not depend on data state; use DROP ... IF EXISTS
-- and ALTER TABLE ... DROP COLUMN IF EXISTS.
SELECT 1;
-- +goose StatementEnd
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("created %s\n", path)
	return nil
}

// nextSerial scans the migrations directory for files matching NNNN_*.sql
// and returns one greater than the largest NNNN found, or 1 if none exist.
// We use a 4-digit serial rather than goose's default Unix-timestamp prefix
// because the codebase already lives at a small number of migrations and
// reviewing a PR with `0007_` is easier than `20260508153012_`.
func nextSerial(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	maxN := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		if !strings.HasSuffix(base, ".sql") {
			continue
		}
		// Pull the leading digits up to the first non-digit.
		end := 0
		for end < len(base) && base[end] >= '0' && base[end] <= '9' {
			end++
		}
		if end == 0 {
			continue
		}
		n, err := strconv.Atoi(base[:end])
		if err != nil {
			continue
		}
		if n > maxN {
			maxN = n
		}
	}
	return maxN + 1, nil
}

// sanitizeName turns a migration name like "add cameras.zone column!" into
// "add_cameras_zone_column" so the resulting filename is path-safe and
// matches the snake_case convention used by the existing migrations.
func sanitizeName(name string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.TrimRight(b.String(), "_")
}

func usage() {
	fmt.Fprintln(os.Stderr, "Ironsight migration operator CLI (wraps pressly/goose v3)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  migrate <subcommand> [args...]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Subcommands:")
	fmt.Fprintln(os.Stderr, "  status              show applied / pending migrations")
	fmt.Fprintln(os.Stderr, "  version             print the current applied version")
	fmt.Fprintln(os.Stderr, "  up                  apply all pending migrations")
	fmt.Fprintln(os.Stderr, "  up-to <version>     apply migrations up to and including <version>")
	fmt.Fprintln(os.Stderr, "  up-by-one           apply the next single pending migration")
	fmt.Fprintln(os.Stderr, "  down                revert the last applied migration")
	fmt.Fprintln(os.Stderr, "  down-to <version>   revert migrations down to <version>")
	fmt.Fprintln(os.Stderr, "  redo                revert and re-apply the last migration (smoke test)")
	fmt.Fprintln(os.Stderr, "  create <name> [sql|go]  scaffold a new migration in ./migrations/")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Environment:")
	fmt.Fprintln(os.Stderr, "  DATABASE_URL        Postgres connection string (required for all")
	fmt.Fprintln(os.Stderr, "                      subcommands except `create`).")
}
