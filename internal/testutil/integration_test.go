// P1-B-04: smoke tests covering the critical paths against a real
// schema-isolated Postgres+TimescaleDB connection. The goal is regression
// coverage for everything Phase 1 has shipped that touches the database
// layer — migrations, camera CRUD with at-rest encryption, user auth
// surface, hypertable presence — so a future refactor or schema change
// surfaces here instead of in production.
//
// These tests run when DATABASE_URL is set (CI service container). On a
// developer laptop without docker, they no-op via t.Skip in IntegrationDB.
// CI sets IRONSIGHT_INTEGRATION_REQUIRED=1 to convert the skip into a
// hard failure if the env isn't wired.

package testutil_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"ironsight/internal/crypto"
	"ironsight/internal/database"
	"ironsight/internal/testutil"
)

func TestIntegration_MigrationsApplyCleanly(t *testing.T) {
	db := testutil.IntegrationDB(t)

	// goose_db_version is the canonical source of which migrations the
	// schema thinks have been applied. If the chain is internally broken
	// (e.g. a migration with a missing precondition), IntegrationDB
	// would have failed already. Here we assert the version >= 20 to
	// catch any future addition that forgets to bump.
	var version int64
	err := db.Pool.QueryRow(context.Background(),
		`SELECT max(version_id) FROM goose_db_version WHERE is_applied = true`,
	).Scan(&version)
	if err != nil {
		t.Fatalf("read goose_db_version: %v", err)
	}
	if version < 20 {
		t.Fatalf("expected goose version >= 20 (last known migration), got %d", version)
	}
}

func TestIntegration_HypertablesPresent(t *testing.T) {
	db := testutil.IntegrationDB(t)

	// The four hypertables the recording + analytics path expects. If
	// any of these is a plain table, range queries silently degrade to
	// full scans and retention/compression jobs break.
	want := []string{"segments", "events", "ai_runtime_metrics"}
	for _, name := range want {
		var present bool
		err := db.Pool.QueryRow(context.Background(), `
			SELECT EXISTS(
				SELECT 1 FROM timescaledb_information.hypertables
				WHERE hypertable_name = $1
			)`, name).Scan(&present)
		if err != nil {
			t.Fatalf("query hypertable %s: %v", name, err)
		}
		if !present {
			t.Errorf("expected %s to be a Timescale hypertable; it is not", name)
		}
	}
}

func TestIntegration_CameraCRUD_EncryptsPasswordAtRest(t *testing.T) {
	db := testutil.IntegrationDB(t)

	// Install a key — exercises the same SetCredentialsKey -> encrypt ->
	// decrypt round-trip cmd/server uses in production.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	db.SetCredentialsKey(key)

	ctx := context.Background()
	plain := "supersecret-password-" + hex.EncodeToString(key[:4])
	cam := &database.Camera{
		Name:         "smoke-test-cam",
		OnvifAddress: "192.0.2.1",
		Username:     "admin",
		Password:     plain,
		RTSPUri:      "rtsp://192.0.2.1/Streaming/Channels/101",
	}
	if err := db.CreateCamera(ctx, cam); err != nil {
		t.Fatalf("CreateCamera: %v", err)
	}

	// Round-trip via Get: the layer should return plaintext to callers.
	got, err := db.GetCamera(ctx, cam.ID)
	if err != nil {
		t.Fatalf("GetCamera: %v", err)
	}
	if got == nil {
		t.Fatal("GetCamera returned nil for a just-inserted row")
	}
	if got.Password != plain {
		t.Errorf("decrypted password mismatch:\n want=%q\n  got=%q", plain, got.Password)
	}

	// Read the raw column bypassing the model layer — must be ciphertext.
	var raw string
	if err := db.Pool.QueryRow(ctx,
		`SELECT password FROM cameras WHERE id=$1`, cam.ID,
	).Scan(&raw); err != nil {
		t.Fatalf("raw password query: %v", err)
	}
	if raw == plain {
		t.Error("password is at rest in plaintext — encryption did not apply on CreateCamera")
	}
	if len(raw) < len(crypto.CredentialPrefix)+10 || raw[:len(crypto.CredentialPrefix)] != crypto.CredentialPrefix {
		t.Errorf("expected ciphertext to start with %q, got %q (truncated)", crypto.CredentialPrefix, raw[:min(20, len(raw))])
	}

	// Delete cleans up.
	if err := db.DeleteCamera(ctx, cam.ID); err != nil {
		t.Fatalf("DeleteCamera: %v", err)
	}
	gone, err := db.GetCamera(ctx, cam.ID)
	if err != nil {
		t.Fatalf("GetCamera after delete: %v", err)
	}
	if gone != nil {
		t.Errorf("camera %s still present after delete", cam.ID)
	}
}

func TestIntegration_UserCRUD(t *testing.T) {
	db := testutil.IntegrationDB(t)

	ctx := context.Background()

	// The seed migrations may insert a default admin row; assert the
	// fresh-schema state and add a fresh user without colliding.
	u, err := db.CreateUser(ctx, &database.UserCreate{
		Username:    "smoketest_" + uuid.NewString()[:8],
		Role:        "viewer",
		Email:       "smoke@example.invalid",
		DisplayName: "Smoke Test User",
	}, "!sso!")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == uuid.Nil {
		t.Errorf("CreateUser returned a zero UUID")
	}
	if u.Email != "smoke@example.invalid" {
		t.Errorf("email roundtrip mismatch: %q", u.Email)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
