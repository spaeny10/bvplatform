// Package testutil provides helpers for integration tests that need a
// real Postgres+TimescaleDB connection — primarily the smoke tests
// covering the P1-B-04 acceptance criterion ("critical paths verified
// end-to-end against the real schema, not mocks").
//
// The design assumes a CI service-container running TimescaleDB on
// localhost:5432, identified by the DATABASE_URL env. When the env is
// unset (the local-developer default), tests that call IntegrationDB
// are skipped — `go test ./...` stays green on a plain laptop without
// docker running. The CI workflow exports DATABASE_URL so the same
// tests execute against a fresh container per job.
//
// Isolation pattern: a single shared DB. The baseline migration hardcodes
// the `public.` schema in its CREATE TABLE statements, so per-test schema
// isolation (which would be ideal) isn't viable without rewriting the
// migrations. Instead, every test uses globally-unique identifiers
// (uuid.NewString, random usernames) and trusts that the CI container
// is torn down per job. Cross-test pollution within a single run is the
// remaining risk; in practice the smoke tests are read-only or operate
// on disjoint rows.
package testutil

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"ironsight/internal/database"
	"ironsight/migrations"
)

var (
	sharedDB      *database.DB
	sharedDBErr   error
	sharedDBOnce  sync.Once
	sharedDBSkip  string // non-empty → tests should t.Skip with this reason
)

// IntegrationDB returns a *database.DB connected to the CI service
// container with the full migration chain applied. The DB pool is
// shared across tests in the same `go test` invocation to avoid
// re-applying 20 migrations per test (which would take ~60 s).
//
// Tests are skipped when DATABASE_URL is empty so the regular
// `go test ./...` developer path stays green without docker. Set
// IRONSIGHT_INTEGRATION_REQUIRED=1 in CI to flip the skip into a
// hard fail and prove the env is wired.
func IntegrationDB(t *testing.T) *database.DB {
	t.Helper()

	sharedDBOnce.Do(initSharedDB)

	if sharedDBSkip != "" {
		t.Skip(sharedDBSkip)
	}
	if sharedDBErr != nil {
		t.Fatalf("integration DB setup failed: %v", sharedDBErr)
	}
	return sharedDB
}

func initSharedDB() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		if os.Getenv("IRONSIGHT_INTEGRATION_REQUIRED") == "1" {
			sharedDBErr = errSkipRequired
			return
		}
		sharedDBSkip = "DATABASE_URL not set; skipping integration test"
		return
	}

	ctx := context.Background()

	// Apply migrations via goose. Each invocation of `go test` runs this
	// once. The embedded migrations are the same source the production
	// server uses at startup, so green here proves the chain is
	// internally consistent at the schema level the api will see.
	gooseConn, err := pgx.Connect(ctx, url)
	if err != nil {
		sharedDBErr = err
		return
	}
	defer gooseConn.Close(ctx)

	if err := goose.SetDialect("postgres"); err != nil {
		sharedDBErr = err
		return
	}
	goose.SetBaseFS(migrations.FS)
	stdDB, err := openStdlibDB(url)
	if err != nil {
		sharedDBErr = err
		return
	}
	defer stdDB.Close()
	if err := goose.UpContext(ctx, stdDB, "."); err != nil {
		sharedDBErr = err
		return
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		sharedDBErr = err
		return
	}
	sharedDB = &database.DB{Pool: pool}

	// Activate the P1-A-05 camera-credential encryption path. Without a key,
	// CreateCamera stores plaintext and TestIntegration_CameraCRUD_EncryptsPasswordAtRest
	// (correctly) fails. A fixed 32-byte test key is fine — these are throwaway
	// integration rows, and encrypt-on-write + decrypt-on-read round-trips.
	testKey := sha256.Sum256([]byte("ironsight-integration-test-credentials-key"))
	sharedDB.SetCredentialsKey(testKey[:])

	// P4-SCHEMA-07 RLS test support. The CI/dev `postgres` user is a SUPERUSER
	// (and therefore BYPASSRLS) — Postgres bypasses RLS for superusers regardless
	// of FORCE ROW LEVEL SECURITY or policy grants. To verify RLS policies
	// actually enforce tenant isolation, the rls_test.go suite drops to this
	// non-superuser, no-bypass role via SET LOCAL ROLE inside its test
	// transactions (see testutil.AcquireRLSTenantTx). This role is not used
	// in production.
	for _, stmt := range []string{
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'rls_test_user') THEN
				CREATE ROLE rls_test_user NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION;
			END IF;
		END $$`,
		`GRANT USAGE ON SCHEMA public TO rls_test_user`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO rls_test_user`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO rls_test_user`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO rls_test_user`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO rls_test_user`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			sharedDBErr = fmt.Errorf("rls_test_user setup: %s: %w", stmt, err)
			return
		}
	}
}

// AcquireRLSTenantTx wraps database.AcquireWithTenant for the RLS integration
// tests: it acquires a tenant-scoped transaction AND drops the connection's
// effective role to rls_test_user via SET LOCAL ROLE so RLS policies actually
// enforce (the bare pool runs as postgres-SUPERUSER which would otherwise
// bypass RLS unconditionally). Caller still must defer conn.Release() and
// tx.Rollback(ctx) per the database.AcquireWithTenant contract.
func AcquireRLSTenantTx(ctx context.Context, db *database.DB, tenant string) (*pgxpool.Conn, pgx.Tx, error) {
	conn, tx, err := database.AcquireWithTenant(ctx, db.Pool, tenant)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE rls_test_user"); err != nil {
		_ = tx.Rollback(ctx)
		conn.Release()
		return nil, nil, fmt.Errorf("set local role rls_test_user: %w", err)
	}
	return conn, tx, nil
}

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

const errSkipRequired sentinelErr = "DATABASE_URL is required (IRONSIGHT_INTEGRATION_REQUIRED=1 set)"
