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
}

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

const errSkipRequired sentinelErr = "DATABASE_URL is required (IRONSIGHT_INTEGRATION_REQUIRED=1 set)"
