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

// textIDColumnAllowlist is the set of "table.column" *_id columns that are
// legitimately TEXT even though their table is not a TEXT-PK table. Three
// reasons: (a) FK to a TEXT-PK table (site_id→sites, organization_id/org_id→
// organizations, alarm_id→active_alarms, *_operator_id→operators); (b) a
// polymorphic / non-record id (audit target_id, manifest artifact_id, key_id
// fingerprint, device_id); (c) a denormalized SOC/audit copy of an id kept as
// text on purpose (the referenced row may be deleted; audit rows are immutable).
// A NEW *_id TEXT column not in this list still fails the check — that's the
// drift guard. Views (*_active) are excluded separately (BASE TABLE filter).
var textIDColumnAllowlist = map[string]string{
	// FK → TEXT-PK tables
	"cameras.site_id":                       "→ sites.id (TEXT)",
	"speakers.site_id":                      "→ sites.id (TEXT)",
	"compliance_rules.site_id":              "→ sites.id (TEXT)",
	"compliance_rules.organization_id":      "→ organizations.id (TEXT)",
	"ppe_zones.site_id":                     "→ sites.id (TEXT)",
	"ppe_zones.organization_id":             "→ organizations.id (TEXT)",
	"pending_review_queue.site_id":          "→ sites.id (TEXT)",
	"pending_review_queue.organization_id":  "→ organizations.id (TEXT)",
	"person_track_frames.site_id":           "→ sites.id (TEXT)",
	"person_track_frames.organization_id":   "→ organizations.id (TEXT)",
	"person_track_buckets.site_id":          "→ sites.id (TEXT)",
	"person_track_buckets.organization_id":  "→ organizations.id (TEXT)",
	"support_tickets.site_id":               "→ sites.id (TEXT)",
	"support_tickets.organization_id":       "→ organizations.id (TEXT)",
	"users.organization_id":                 "→ organizations.id (TEXT)",
	"digest_sends.org_id":                   "→ organizations.id (TEXT)",
	"evidence_manifests.organization_id":    "→ organizations.id (TEXT)",
	"device_assignments.site_id":            "→ sites.id (TEXT)",
	"shift_handoffs.from_operator_id":       "→ operators.id (TEXT)",
	"shift_handoffs.to_operator_id":         "→ operators.id (TEXT)",
	// Polymorphic / non-record ids
	"audit_log.target_id":          "polymorphic audit target (any entity type)",
	"evidence_manifests.artifact_id": "polymorphic (report_id / event_id / share token)",
	"evidence_manifests.key_id":      "ed25519 public-key fingerprint, not a record id",
	"device_assignments.device_id":   "polymorphic device reference (camera/speaker/etc.)",
	// Denormalized SOC/audit copies (referenced row may be deleted; audit is immutable)
	"alarm_queue.alarm_id":        "denormalized SOC queue copy of active_alarms.id (TEXT)",
	"alarm_queue.site_id":         "denormalized SOC queue copy of sites.id (TEXT)",
	"alarm_queue.camera_id":       "denormalized SOC queue copy of camera id (TEXT, not enforced FK)",
	"deterrence_audits.alarm_id":  "denormalized audit copy of active_alarms.id (TEXT)",
	"deterrence_audits.user_id":   "denormalized audit copy of user id (TEXT, immutable audit row)",
	"playback_audits.user_id":     "denormalized audit copy of user id (TEXT, immutable audit row)",
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

	// BASE TABLE only — the soft-delete *_active VIEWS mirror their base
	// tables' columns and would double-report. We check the base tables.
	query := fmt.Sprintf(`
		SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public'
		  AND t.table_type = 'BASE TABLE'
		  AND c.table_name NOT IN (%s)
		  AND c.column_name LIKE '%%_id'
		  AND c.data_type IN ('text', 'character varying')
		ORDER BY c.table_name, c.column_name
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
		// Skip documented-legitimate TEXT *_id columns (FK to TEXT-PK tables,
		// polymorphic ids, or denormalized audit copies).
		if _, ok := textIDColumnAllowlist[tbl+"."+col]; ok {
			continue
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
