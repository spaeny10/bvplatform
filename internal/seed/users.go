package seed

import (
	"context"
	"log"
	"os"

	authpkg "ironsight/internal/auth"
	"ironsight/internal/database"
)

// LOCAL-08: demo password defaults.
//
// The seed binary creates a fixed set of demo accounts (SOC operators,
// site managers, a customer user) so a fresh deployment is usable for
// product demos and integration testing without hand-rolling rows.
// Until P1-B-09, these passwords were hardcoded to "demo123" in this
// file. That made every fresh install share the same trivial password
// in the audit log, including the few real customer pilots we've
// pointed at the build.
//
// This change reads the demo password from env vars at seed time:
//
//   SEED_DEMO_PASSWORD          — single shared password for every demo
//                                  account (highest priority — set this
//                                  if you don't care about role splits).
//   SEED_DEMO_PASSWORD_OPERATOR — SOC operator/supervisor accounts only
//                                  (jhayes, ctorres, rmorgan).
//   SEED_DEMO_PASSWORD_PORTAL   — customer + site_manager accounts.
//
// All three default to "demo123" so existing dev workflows (`docker
// compose run --rm seed` against an empty DB) keep working. Real
// deployments override at least SEED_DEMO_PASSWORD.
//
// See docs/seeding.md for the operator runbook.
const defaultDemoPassword = "demo123"

// demoPasswords resolves the per-role password from env. The single
// SEED_DEMO_PASSWORD master override wins if set; otherwise each role
// pulls its own var, falling through to the default.
type demoPasswords struct {
	operator string
	portal   string
}

func loadDemoPasswords() demoPasswords {
	master := os.Getenv("SEED_DEMO_PASSWORD")
	op := os.Getenv("SEED_DEMO_PASSWORD_OPERATOR")
	pt := os.Getenv("SEED_DEMO_PASSWORD_PORTAL")

	if master != "" {
		// Master override applies to both — log so the operator running
		// the seed knows what's happening (without printing the value).
		log.Printf("[SEED] using SEED_DEMO_PASSWORD master override for all demo accounts")
		return demoPasswords{operator: master, portal: master}
	}
	if op == "" {
		op = defaultDemoPassword
	}
	if pt == "" {
		pt = defaultDemoPassword
	}
	return demoPasswords{operator: op, portal: pt}
}

// seedUsers creates demo user accounts for each platform role if they
// don't already exist. Runs idempotently: only inserts rows that are
// missing, so repeated invocations are safe.
//
// Errors on individual users are logged and continue — a single bad
// row shouldn't abort the whole seed pass. The function returns nil
// in all cases; callers that need stricter semantics should add their
// own validation against the DB after.
func seedUsers(ctx context.Context, db *database.DB) error {
	type seedUser struct {
		username    string
		password    string
		role        string
		displayName string
		email       string
		phone       string
		orgID       string
		siteIDs     []string
	}

	pw := loadDemoPasswords()

	demo := []seedUser{
		// SOC operators — linked to operators table
		{"jhayes", pw.operator, "soc_operator", "Jordan Hayes", "jhayes@ironsight.io", "312-555-0111", "", nil},
		{"ctorres", pw.operator, "soc_operator", "Casey Torres", "ctorres@ironsight.io", "312-555-0122", "", nil},
		{"rmorgan", pw.operator, "soc_supervisor", "Riley Morgan", "rmorgan@ironsight.io", "312-555-0133", "", nil},
		// Portal users — linked to organizations/sites
		{"marcus.webb", pw.portal, "site_manager", "Marcus Webb", "marcus.webb@apexcg.com", "312-555-0147", "co-alpha001", []string{"ACG-301", "ACG-302"}},
		{"spierce", pw.portal, "customer", "Sandra Pierce", "spierce@apexcg.com", "312-555-0198", "co-alpha001", []string{"ACG-301", "ACG-302"}},
		{"priya.sharma", pw.portal, "site_manager", "Priya Sharma", "priya@meridiandv.com", "512-555-0293", "co-beta002", []string{"MDV-501"}},
		{"derek.lawson", pw.portal, "site_manager", "Derek Lawson", "dlawson@ironcladsites.com", "602-555-0311", "co-gamma003", []string{"ISS-201"}},
	}

	for _, s := range demo {
		// Skip if user already exists
		existing, _ := db.GetUserByUsernameOrEmail(ctx, s.username)
		if existing != nil {
			continue
		}
		hash, err := authpkg.HashPassword(s.password)
		if err != nil {
			log.Printf("[SEED] Failed to hash password for %s: %v", s.username, err)
			continue
		}
		u, err := db.CreateUser(ctx, &database.UserCreate{
			Username:        s.username,
			Role:            s.role,
			DisplayName:     s.displayName,
			Email:           s.email,
			Phone:           s.phone,
			OrganizationID:  s.orgID,
			AssignedSiteIDs: s.siteIDs,
		}, hash)
		if err != nil {
			log.Printf("[SEED] Could not create user %s: %v", s.username, err)
			continue
		}
		log.Printf("[SEED] Created user %s (%s)", s.username, s.role)

		// Link SOC users to their operators table row
		switch s.username {
		case "jhayes":
			db.Pool.Exec(ctx, `UPDATE operators SET user_id=$1 WHERE id='op-001'`, u.ID)
		case "ctorres":
			db.Pool.Exec(ctx, `UPDATE operators SET user_id=$1 WHERE id='op-002'`, u.ID)
		case "rmorgan":
			db.Pool.Exec(ctx, `UPDATE operators SET user_id=$1 WHERE id='op-003'`, u.ID)
		}
	}

	return nil
}
