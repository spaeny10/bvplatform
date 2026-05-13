// Package seed contains the demo-data seeders for staging / dev
// environments. The functions here populate a fresh Ironsight database
// with a believable customer portfolio (3 orgs, 5 sites, ~30 days of
// dispositioned events) and a handful of demo platform users so the
// portal panels and operator console render with real numbers instead
// of zeros on a clean install.
//
// Production deployments NEVER call into this package — it is consumed
// only by the cmd/seed binary, which an operator runs explicitly
// against a staging database (see docs/seeding.md and phase-plan task
// P1-B-09). The api server's startup path does not import seed.
//
// Both seeders are idempotent on the org / user level: re-running them
// against an already-seeded database is a no-op rather than an error.
package seed

import (
	"context"

	"ironsight/internal/database"
)

// Portfolio creates the demo organizations, sites, and ~30 days of
// dispositioned security events. Idempotent: bails out cleanly if the
// first demo org already exists.
//
// See portfolio.go for the actual seed data and event-generation logic.
func Portfolio(ctx context.Context, db *database.DB) error {
	return seedPortfolio(ctx, db)
}

// Users creates the demo platform users (3 SOC operators + 4 portal
// users) if they don't already exist. Linked to the orgs created by
// Portfolio — call Portfolio first if you want the portal users to
// have working org / site assignments.
//
// See users.go for the actual seed data.
func Users(ctx context.Context, db *database.DB) error {
	return seedUsers(ctx, db)
}
