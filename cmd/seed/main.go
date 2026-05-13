// Package main — Ironsight demo-seed binary.
//
// One-shot CLI that populates a staging / dev database with the demo
// portfolio (3 orgs, 5 sites, ~30 days of dispositioned events) and
// the demo platform users (3 SOC operators + 4 portal users). All
// the actual seed logic lives in internal/seed; this file is a thin
// entry point that opens a DB pool and dispatches based on flags.
//
// Production deployments must NOT run this binary — that's the whole
// point of P1-B-09 (separating demo seeds from server startup). The
// api server's startup path no longer touches any of the seed
// functions, so production stays clean as long as nobody invokes
// /app/seed by hand.
//
// Typical usage:
//
//	docker compose run --rm api /app/seed --all          # both seeders
//	docker compose run --rm api /app/seed --portfolio    # orgs + sites + events
//	docker compose run --rm api /app/seed --users        # platform users
//	docker compose run --rm api /app/seed --dry-run --all
//
// DATABASE_URL is read from the environment — same plumbing the api
// server uses (config.Load + database.New). No new connection logic.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/seed"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	var (
		doPortfolio bool
		doUsers     bool
		doAll       bool
		dryRun      bool
	)
	flag.BoolVar(&doPortfolio, "portfolio", false, "seed demo orgs, sites, and ~30 days of dispositioned events")
	flag.BoolVar(&doUsers, "users", false, "seed demo platform users (3 SOC operators + 4 portal users)")
	flag.BoolVar(&doAll, "all", false, "seed everything (--portfolio then --users)")
	flag.BoolVar(&dryRun, "dry-run", false, "log what would be seeded without writing to the database")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Ironsight demo-data seeder. Populates a staging / dev database with")
		fmt.Fprintln(flag.CommandLine.Output(), "the demo portfolio and platform users. Reads DATABASE_URL from env.")
		fmt.Fprintln(flag.CommandLine.Output(), "")
		fmt.Fprintln(flag.CommandLine.Output(), "DO NOT run against a production database.")
		fmt.Fprintln(flag.CommandLine.Output(), "")
		fmt.Fprintln(flag.CommandLine.Output(), "Flags:")
		flag.PrintDefaults()
	}

	flag.Parse()

	if doAll {
		doPortfolio = true
		doUsers = true
	}
	if !doPortfolio && !doUsers {
		fmt.Fprintln(os.Stderr, "error: must pass at least one of --portfolio, --users, or --all")
		flag.Usage()
		os.Exit(2)
	}

	log.Println("============================================")
	log.Println("  Ironsight Seed — demo data loader")
	log.Println("============================================")
	if dryRun {
		log.Println("[SEED] --dry-run set; no DB writes will be performed")
		if doPortfolio {
			log.Println("[SEED] would seed: 3 orgs (co-alpha001, co-beta002, co-gamma003), 5 sites, ~30 days of dispositioned events")
		}
		if doUsers {
			log.Println("[SEED] would seed: 7 demo users (3 SOC operators + 4 portal users; password 'demo123')")
		}
		log.Println("[SEED] dry-run complete")
		return
	}

	cfg := config.Load()

	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database connect: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Portfolio first so users can be linked to existing orgs.
	if doPortfolio {
		log.Println("[SEED] seeding demo portfolio (orgs, sites, events)...")
		if err := seed.Portfolio(ctx, db); err != nil {
			log.Fatalf("[FATAL] portfolio seed failed: %v", err)
		}
	}
	if doUsers {
		log.Println("[SEED] seeding demo platform users...")
		if err := seed.Users(ctx, db); err != nil {
			log.Fatalf("[FATAL] users seed failed: %v", err)
		}
	}

	log.Println("[SEED] done")
}
