package seed

import (
	"context"
	"log"

	authpkg "ironsight/internal/auth"
	"ironsight/internal/database"
)

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

	demo := []seedUser{
		// SOC operators — linked to operators table
		{"jhayes", "demo123", "soc_operator", "Jordan Hayes", "jhayes@ironsight.io", "312-555-0111", "", nil},
		{"ctorres", "demo123", "soc_operator", "Casey Torres", "ctorres@ironsight.io", "312-555-0122", "", nil},
		{"rmorgan", "demo123", "soc_supervisor", "Riley Morgan", "rmorgan@ironsight.io", "312-555-0133", "", nil},
		// Portal users — linked to organizations/sites
		{"marcus.webb", "demo123", "site_manager", "Marcus Webb", "marcus.webb@apexcg.com", "312-555-0147", "co-alpha001", []string{"ACG-301", "ACG-302"}},
		{"spierce", "demo123", "customer", "Sandra Pierce", "spierce@apexcg.com", "312-555-0198", "co-alpha001", []string{"ACG-301", "ACG-302"}},
		{"priya.sharma", "demo123", "site_manager", "Priya Sharma", "priya@meridiandv.com", "512-555-0293", "co-beta002", []string{"MDV-501"}},
		{"derek.lawson", "demo123", "site_manager", "Derek Lawson", "dlawson@ironcladsites.com", "602-555-0311", "co-gamma003", []string{"ISS-201"}},
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
