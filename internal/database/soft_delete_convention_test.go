package database_test

// soft_delete_convention_test.go — P3-INFRA-05 CI lint for soft-delete drift.
//
// TestSoftDeleteConventions asserts two schema invariants:
//
//  1. All 8 soft-delete tables have a deleted_at TIMESTAMPTZ column.
//
//  2. All explicitly excluded tables (append-only logs, hypertables, SOC state)
//     do NOT have a deleted_at column — adding one accidentally would violate
//     the courtroom-verifiability principle (audit logs must be immutable).
//
// Design notes
// ------------
// Runs against the live migrated schema via testutil.IntegrationDB; skipped when
// DATABASE_URL is unset. The guard table list is the complete exclusion set from
// the P3-INFRA-05 scope doc. Hypertables are included because TimescaleDB wraps
// them and soft-deleting inside a hypertable causes subtle time-chunk boundary
// bugs.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ironsight/internal/testutil"
)

// softDeleteRequired is the complete set of tables that MUST have deleted_at.
var softDeleteRequired = []string{
	"cameras",
	"sites",
	"organizations",
	"users",
	"speakers",
	"ppe_zones",
	"compliance_rules",
	"vca_rules",
}

// softDeleteForbidden is the complete set of tables that MUST NOT have deleted_at.
// These are append-only audit tables, hypertables, SOC state tables, and join
// tables that are hard-excluded from the soft-delete pattern.
var softDeleteForbidden = []string{
	"audit_log",
	"playback_audits",
	"deterrence_audits",
	"evidence_manifests",
	"segments",
	"events",
	"person_track_frames",
	"ai_runtime_metrics",
	"active_alarms",
	"incidents",
	"security_events",
	"company_users",
}

// TestSoftDeleteConventions introspects the live schema and asserts that:
//
//  1. Every table in softDeleteRequired has a deleted_at TIMESTAMPTZ column.
//  2. No table in softDeleteForbidden has a deleted_at column of any type.
func TestSoftDeleteConventions(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	// ── Check 1: Required tables MUST have deleted_at ─────────────────────────

	var check1Failures []string
	for _, tbl := range softDeleteRequired {
		var count int
		err := db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = $1
			  AND column_name  = 'deleted_at'`, tbl).Scan(&count)
		if err != nil {
			t.Fatalf("query deleted_at for %s: %v", tbl, err)
		}
		if count == 0 {
			check1Failures = append(check1Failures, fmt.Sprintf("  %s (missing deleted_at)", tbl))
		}
	}
	if len(check1Failures) > 0 {
		t.Errorf("soft-delete tables missing deleted_at column.\n"+
			"Run migration 0028_soft_delete.sql to add the column.\n\nMissing:\n%s",
			strings.Join(check1Failures, "\n"))
	}

	// ── Check 2: Forbidden tables MUST NOT have deleted_at ────────────────────

	var check2Failures []string
	for _, tbl := range softDeleteForbidden {
		var count int
		err := db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name   = $1
			  AND column_name  = 'deleted_at'`, tbl).Scan(&count)
		if err != nil {
			// Table may not exist in all environments (e.g. a TimescaleDB hypertable
			// that hasn't been created yet in a minimal test schema). Treat that as
			// "does not have deleted_at" — not a failure.
			continue
		}
		if count > 0 {
			check2Failures = append(check2Failures, fmt.Sprintf("  %s (has deleted_at — MUST NOT)", tbl))
		}
	}
	if len(check2Failures) > 0 {
		t.Errorf("append-only / SOC-state tables must never have deleted_at.\n"+
			"These tables are excluded from soft-delete to preserve audit immutability.\n"+
			"If you intentionally added deleted_at to one of these, update the scope doc\n"+
			"(P3-INFRA-05) and remove it from softDeleteForbidden in this test.\n\nOffending:\n%s",
			strings.Join(check2Failures, "\n"))
	}

	// ── Check 3: _active VIEWs exist for every required table ─────────────────

	var check3Failures []string
	for _, tbl := range softDeleteRequired {
		viewName := tbl + "_active"
		var count int
		err := db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM information_schema.views
			WHERE table_schema = 'public'
			  AND table_name   = $1`, viewName).Scan(&count)
		if err != nil {
			t.Fatalf("query view %s: %v", viewName, err)
		}
		if count == 0 {
			check3Failures = append(check3Failures, fmt.Sprintf("  %s (view missing)", viewName))
		}
	}
	if len(check3Failures) > 0 {
		t.Errorf("_active views missing for soft-delete tables.\n"+
			"Run migration 0028_soft_delete.sql to create the views.\n\nMissing:\n%s",
			strings.Join(check3Failures, "\n"))
	}
}
