package database_test

// id_convention_test.go — P3-INFRA-04 CI lint for ID-type drift.
//
// TestSchemaIDConventions introspects the live schema (via DATABASE_URL,
// post-migration) and asserts two invariants:
//
//  1. No new table has a TEXT PRIMARY KEY unless its name appears in the
//     grandfathered TEXT-PK allowlist.  A new migration that adds a TEXT PK
//     without a conscious decision causes a test failure here before the PR
//     can merge.
//
//  2. No FK column (column name ending in _id) has a type mismatch with its
//     target PK — specifically, no column that stores UUIDs is declared as
//     TEXT.  This catches the class of bug that P3-INFRA-04 was designed
//     to prevent: vlm_label_jobs.camera_id was TEXT while cameras.id is UUID.
//
// Design notes
// ------------
// The test runs against the real migrated schema via testutil.IntegrationDB,
// which is skipped when DATABASE_URL is unset (developer laptops without
// docker).  CI sets DATABASE_URL in the backend-integration job, which has
// a TimescaleDB service container, so the assertions run on every PR.
//
// The test queries information_schema.columns and
// information_schema.table_constraints / key_column_usage, not any
// application-specific tables, so it is independent of application logic.
//
// Allowlist rationale (Interpretation B, locked 2026-05-27)
// ---------------------------------------------------------
// The following tables have TEXT PKs by intentional design.  Their IDs are
// human-readable customer codes and operator-assigned slugs that appear in
// URLs, SOC UIs, and operator workflows.  Converting them to UUID would
// break the external API contract and existing stored data for zero benefit.
//
//   organizations  — slug: co-bv-test, co-alpha001
//   sites          — slug: T5-903, ACG-301
//   incidents      — slug: INC-YYMMDD-NNNN
//   active_alarms  — slug: ALM-YYMMDD-NNNN
//   security_events — slug: EVT-YYMMDD-NNNN
//   site_sops      — assigned text keys
//   company_users  — assigned text keys
//   operators      — slug: op-001
//
// Additionally two tables have text PKs that are NOT entity record IDs:
//   revoked_tokens   — jti (JWT ID string, not an entity PK)
//   evidence_shares  — token (share token string, not an entity PK)
//
// New tables MUST use UUID PRIMARY KEY DEFAULT gen_random_uuid() unless they
// are added to this allowlist with a written justification.  The test will
// fail CI automatically — the author of the migration must add the table
// name here and explain why in a code comment.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ironsight/internal/testutil"
)

// textPKAllowlist is the complete set of tables permitted to have a TEXT
// primary key column.  Any table NOT in this set that has a text PK will
// fail this test.  To add a new entry: update this set AND add a comment
// explaining why the table legitimately uses a text PK.
var textPKAllowlist = map[string]string{
	// Slug-format entity IDs (human-readable, appear in URLs and operator UIs).
	"organizations":   "customer slug e.g. co-bv-test; API contract + URLs depend on this format",
	"sites":           "customer slug e.g. T5-903; API contract + URLs depend on this format",
	"incidents":       "SOC-generated slug e.g. INC-YYMMDD-NNNN",
	"active_alarms":   "SOC-generated slug e.g. ALM-YYMMDD-NNNN",
	"security_events": "SOC-generated slug e.g. EVT-YYMMDD-NNNN",
	"site_sops":       "assigned text keys",
	"company_users":   "customer portal user keys",
	"operators":       "SOC operator slug e.g. op-001",
	// Non-entity PKs (the PK value is a token, not a record identity).
	"revoked_tokens":  "jti is a JWT ID string, not a record-identity UUID",
	"evidence_shares": "token is an opaque share string, not a record-identity UUID",
}

// TestSchemaIDConventions asserts that:
//
//  1. No table outside the allowlist has a TEXT primary key.
//  2. No FK column (name ends in _id) stores UUIDs as TEXT — specifically,
//     no column named camera_id, user_id, or any _id column is of type text
//     when its logical target PK is uuid.  We detect this by checking that
//     every _id column in non-allowlist tables is either uuid, bigint, or
//     integer — never text (the TEXT-PK allowlist tables may have text FK
//     columns pointing at their own TEXT-PK relatives, which is intentional).
func TestSchemaIDConventions(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	// ── Check 1: No new TEXT-PK tables outside the allowlist ─────────────

	// Query information_schema for all user tables that have a PRIMARY KEY
	// constraint where the key column is of type text.
	type textPKRow struct {
		tableName string
		colName   string
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT tc.table_name, kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		    ON kcu.constraint_name = tc.constraint_name
		   AND kcu.table_schema    = tc.table_schema
		JOIN information_schema.columns c
		    ON c.table_name   = kcu.table_name
		   AND c.column_name  = kcu.column_name
		   AND c.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'
		  AND c.data_type         IN ('text', 'character varying')
		  AND tc.table_schema     = 'public'
		ORDER BY tc.table_name
	`)
	if err != nil {
		t.Fatalf("query text PK tables: %v", err)
	}
	defer rows.Close()

	var textPKTables []textPKRow
	for rows.Next() {
		var r textPKRow
		if err := rows.Scan(&r.tableName, &r.colName); err != nil {
			t.Fatalf("scan text PK row: %v", err)
		}
		textPKTables = append(textPKTables, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate text PK rows: %v", err)
	}

	// Fail any table NOT in the allowlist.
	var check1Failures []string
	for _, row := range textPKTables {
		if _, allowed := textPKAllowlist[row.tableName]; !allowed {
			check1Failures = append(check1Failures,
				fmt.Sprintf("  %s.%s (TEXT PK)", row.tableName, row.colName))
		}
	}
	if len(check1Failures) > 0 {
		t.Errorf("new TEXT-PK tables detected outside the grandfathered allowlist.\n"+
			"If this table legitimately needs a TEXT PK (e.g. human-readable slug), add it\n"+
			"to textPKAllowlist in internal/database/id_convention_test.go with a justification.\n"+
			"Otherwise, change the PK to UUID PRIMARY KEY DEFAULT gen_random_uuid().\n\nOffending tables:\n%s",
			strings.Join(check1Failures, "\n"))
	}

	// ── Check 2: No FK _id columns are TEXT in UUID-convention tables ─────
	//
	// We inspect every column named *_id in tables NOT in the TEXT-PK
	// allowlist.  Such columns must be uuid, bigint, or integer — never text.
	// (TEXT _id columns in TEXT-PK allowlist tables are legitimate because
	// they point at other TEXT-PK tables, e.g. sites.organization_id TEXT
	// references organizations.id TEXT.)
	//
	// Build the allowlist as a SQL array literal for the NOT IN clause.
	allowlistNames := make([]string, 0, len(textPKAllowlist))
	for name := range textPKAllowlist {
		allowlistNames = append(allowlistNames, "'"+name+"'")
	}
	// Also exclude goose's own bookkeeping table.
	allowlistNames = append(allowlistNames, "'goose_db_version'")

	query := fmt.Sprintf(`
		SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name NOT IN (%s)
		  AND column_name LIKE '%%_id'
		  AND data_type IN ('text', 'character varying')
		ORDER BY table_name, column_name
	`, strings.Join(allowlistNames, ", "))

	idRows, err := db.Pool.Query(ctx, query)
	if err != nil {
		t.Fatalf("query FK _id columns: %v", err)
	}
	defer idRows.Close()

	var check2Failures []string
	for idRows.Next() {
		var tbl, col, dtype string
		if err := idRows.Scan(&tbl, &col, &dtype); err != nil {
			t.Fatalf("scan _id column row: %v", err)
		}
		check2Failures = append(check2Failures,
			fmt.Sprintf("  %s.%s (type=%s)", tbl, col, dtype))
	}
	if err := idRows.Err(); err != nil {
		t.Fatalf("iterate _id column rows: %v", err)
	}

	if len(check2Failures) > 0 {
		t.Errorf("TEXT-typed *_id FK columns found in UUID-convention tables.\n"+
			"All FK columns referencing a UUID PK must be declared as uuid, bigint, or integer.\n"+
			"If a column genuinely references a TEXT-PK table (e.g. organization_id TEXT\n"+
			"references organizations.id TEXT), the referencing table should be added to\n"+
			"textPKAllowlist or the column should be excluded from this check explicitly.\n\n"+
			"Offending columns:\n%s",
			strings.Join(check2Failures, "\n"))
	}
}
