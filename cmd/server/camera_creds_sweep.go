package main

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"

	"ironsight/internal/crypto"
	"ironsight/internal/database"
	appmetrics "ironsight/internal/metrics"
)

// encryptPlaintextCameraPasswords is the P1-A-05 one-time at-rest
// encryption sweep. It SELECTs every cameras.password row whose value
// isn't already prefixed with crypto.CredentialPrefix and isn't empty,
// re-encrypts the plaintext with the configured key, and UPDATEs the
// row. The function is idempotent: subsequent boots find no matching
// rows (because the prefix filter excludes already-encrypted values)
// and return immediately.
//
// The sweep runs UNDER goose so it executes AFTER any schema migration
// that might rename the cameras table or its password column. The
// per-row work is intentionally serial — a fred-scale deployment has
// dozens of cameras at most, and a sequential UPDATE per row keeps the
// at-rest crypto cost negligible (single-digit milliseconds total) vs.
// the operational complexity of batching.
//
// Errors fail the boot: if we can't reach the database, encrypt, or
// UPDATE, that's a critical fault. The operator should see the log
// line, fix the underlying issue (usually a wrong CAMERA_CREDENTIALS_KEY
// or a transient Postgres outage), and restart.
func encryptPlaintextCameraPasswords(ctx context.Context, db *database.DB, key []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("sweep: empty credentials key (parsed earlier — should not happen)")
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT id, password
		FROM cameras
		WHERE password IS NOT NULL
		  AND password != ''
		  AND password NOT LIKE 'crypt:v1:%'`)
	if err != nil {
		return fmt.Errorf("query plaintext rows: %w", err)
	}
	defer rows.Close()

	type row struct {
		id        uuid.UUID
		plaintext string
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.plaintext); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iter: %w", err)
	}
	if len(batch) == 0 {
		// Common case after the first successful sweep on a deployment.
		log.Println("[STARTUP] camera credential sweep: no plaintext rows")
		return nil
	}

	log.Printf("[STARTUP] camera credential sweep: encrypting %d plaintext password(s)", len(batch))
	for _, r := range batch {
		enc, err := crypto.EncryptCredential(r.plaintext, key)
		if err != nil {
			// P1-C-04: emit a discrete app alert so alertmanager can surface
			// credential encrypt failures even during the boot-time sweep.
			// The error is also returned so the caller (main) can log.Fatalf.
			appmetrics.SetCustomAlert("camera_credentials_encrypt_failure", "critical",
				fmt.Sprintf("camera %s: %v", r.id, err))
			return fmt.Errorf("encrypt camera %s: %w", r.id, err)
		}
		if _, err := db.Pool.Exec(ctx, `UPDATE cameras SET password = $1, updated_at = NOW() WHERE id = $2`, enc, r.id); err != nil {
			return fmt.Errorf("update camera %s: %w", r.id, err)
		}
	}
	log.Printf("[STARTUP] camera credential sweep: %d row(s) encrypted", len(batch))
	return nil
}
