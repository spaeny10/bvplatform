// Package main — Ironsight worker binary.
//
// Runs the background batch workloads that previously lived inside the
// api process:
//
//   - RetentionManager  — hourly segment purge per site policy
//   - VLM Indexer       — enriches recording segments with AI descriptions
//   - Export Worker     — processes queued video-export jobs
//
// Designed for docker-compose / K8s deployments where the api service
// runs with RUN_WORKERS=false and this binary runs as a single-replica
// sibling container. In dev, run the api binary with default
// RUN_WORKERS=true and skip this process entirely.
//
// Explicitly NOT moved here yet:
//
//   - Recording engine  (stateful per-camera FFmpeg, stays with api)
//   - HLS server        (same reason)
//   - MediaMTX control  (colocated with recording)
//   - ONVIF event subs  (tightly coupled to the event-mode recording
//                        trigger path; split is Phase 3)
//
// The worker does NOT serve HTTP. Health can be observed via its logs
// and via the DB rows it writes (e.g., export status transitions).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ironsight/internal/ai"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/export"
	"ironsight/internal/indexer"
	"ironsight/internal/notify"
	"ironsight/internal/recording"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("============================================")
	log.Println("  Ironsight Worker — batch background jobs")
	log.Println("============================================")

	cfg := config.Load()

	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database connect: %v", err)
	}
	defer db.Close()

	// AI client — same wiring as api. Indexer needs it; retention and
	// exports don't. Cheap to initialise either way; skipping keeps the
	// worker dependent on AI service availability only at indexing time.
	aiClient := ai.NewClient(ai.Config{
		YOLOEndpoint: cfg.DetectionServiceURL, // repurposed; kept non-critical
		QwenEndpoint: envOr("AI_QWEN_URL", "http://127.0.0.1:8502"),
		Enabled:      envOr("AI_ENABLED", "true") != "false",
	})
	// Non-fatal — indexer handles a missing AI gracefully (jobs fail but
	// retention + exports keep working).
	aiClient.CheckHealth(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Leader election ──────────────────────────────────────────
	// Acquire a Postgres advisory lock before starting any of the
	// retention / indexer / export loops. If another worker process
	// already holds the lock, this call blocks (with periodic retry
	// logging) until it's free — at which point this process takes
	// over. Combined with restart: unless-stopped on the worker
	// container, this gives us hot-standby semantics for free: bring
	// up N worker replicas, exactly one runs jobs at a time, and
	// failover happens within ~30s of the leader dying. Set
	// WORKER_LEADER_DISABLED=1 to skip the lock entirely (single-binary
	// dev where the api process runs the workers in-process).
	if os.Getenv("WORKER_LEADER_DISABLED") != "1" {
		leader, err := database.AcquireLeader(ctx, cfg.DatabaseURL, "ironsight-worker-loops", 30*time.Second)
		if err != nil {
			log.Fatalf("[FATAL] leader election: %v", err)
		}
		defer leader.Release()
		// If the connection drops out from under us mid-flight, treat
		// it as a fatal signal: another standby will pick up
		// leadership, and we exit so the container restart loop
		// doesn't leave us in a half-leader state.
		go func() {
			<-leader.Lost()
			log.Println("[WORKER] leadership lost, initiating shutdown")
			cancel()
		}()
	} else {
		log.Println("[WORKER] WORKER_LEADER_DISABLED=1 — skipping advisory-lock leader election")
	}

	// ── Retention — hourly segment purge ─────────────────────────
	retentionMgr := recording.NewRetentionManager(db)
	go retentionMgr.Start(ctx)
	log.Println("[WORKER] Retention manager started")

	// ── VLM Indexer — AI enrichment of recorded segments ─────────
	vlmIndexer := indexer.New(cfg, db, aiClient)
	vlmIndexer.Start()
	log.Println("[WORKER] VLM indexer started")

	// ── Export worker — video concat jobs ────────────────────────
	exportWorker := export.NewWorker(cfg, db)
	exportWorker.Start(ctx)
	log.Println("[WORKER] Export worker started")

	// ── Monthly summary emailer ──────────────────────────────────
	// Polls every hour. When the wall clock crosses into the first
	// hour of the 1st of a month AND we haven't already sent for
	// that month, we generate per-org rollups and email each
	// customer-role recipient who's subscribed. Idempotency is
	// in-memory: this leader-elected worker is the only sender, so
	// "already sent this month" is just a date check on a local
	// variable. If the worker process restarts mid-month before
	// sending, the next tick on the 1st picks it up.
	emailMailer := notify.SelectMailer(notify.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUser,
		Password: cfg.SMTPPass,
		From:     cfg.SMTPFrom,
	})
	smsMailer := notify.SelectSMSMailer(notify.TwilioConfig{
		AccountSid: cfg.TwilioAccountSid,
		AuthToken:  cfg.TwilioAuthToken,
		From:       cfg.TwilioFrom,
	})
	notifier := notify.NewDispatcher(emailMailer, smsMailer, cfg.ProductName, cfg.PublicURL)
	go runMonthlySummary(ctx, db, notifier)
	log.Println("[WORKER] Monthly summary scheduler started")

	// Graceful shutdown — signal arrives, cancel ctx, wait for workers
	// to finish. Retention respects ctx.Done() immediately; export worker
	// completes any in-flight FFmpeg then exits.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("[WORKER] Signal %v received, shutting down...", sig)
	case <-ctx.Done():
		log.Println("[WORKER] Context cancelled (likely lost leadership), shutting down...")
	}

	cancel()
	retentionMgr.Stop()
	vlmIndexer.Stop()
	exportWorker.Wait()

	log.Println("[WORKER] Clean shutdown complete")
}

// envOr is a tiny helper so the worker binary doesn't pull in config.Load()
// for every one-off env var. Redundant for values cfg already carries; useful
// for AI endpoints that the worker wants to override independently of the
// api's config.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// runMonthlySummary is the auto-emailed monthly report scheduler.
// Polls every hour; on the 1st of a new month (UTC) it runs the
// per-org summary query and emails subscribed customers. Tracks
// "last sent month" in a local variable so a worker restart between
// hourly checks doesn't double-send.
//
// Per-org error isolation: a failed query for one organization
// logs and continues to the next, rather than aborting the whole
// month's run.
//
// In-memory idempotency is acceptable because leader election
// (internal/database/leader.go) guarantees exactly one worker
// process runs this loop at a time. A leader handoff that happens
// AFTER a partial send and BEFORE finishing all orgs would result
// in some orgs being re-sent — accepted as the lesser evil vs
// adding a per-month state table the worker has to coordinate.
func runMonthlySummary(ctx context.Context, db *database.DB, dispatcher *notify.Dispatcher) {
	var lastSentYM string
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()

	// Run an immediate check at startup so a worker spinning up on
	// the 1st doesn't have to wait an hour for the first tick.
	check := func() {
		now := time.Now().UTC()
		// Only fire on the first day of the month, after midnight.
		if now.Day() != 1 {
			return
		}
		ym := now.Format("2006-01")
		if ym == lastSentYM {
			return
		}
		// Window = previous month, full calendar bounds.
		periodEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		periodStart := periodEnd.AddDate(0, -1, 0)

		log.Printf("[MONTHLY] Generating summary for %s..%s",
			periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02"))

		orgs, err := db.ListOrganizationsWithEmail(ctx)
		if err != nil {
			log.Printf("[MONTHLY] list orgs failed: %v", err)
			return
		}
		for _, org := range orgs {
			runMonthlyForOrg(ctx, db, dispatcher, org.ID, org.Name, periodStart, periodEnd)
		}
		lastSentYM = ym
		log.Printf("[MONTHLY] %d organizations processed for %s", len(orgs), ym)
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			check()
		}
	}
}

func runMonthlyForOrg(ctx context.Context, db *database.DB, dispatcher *notify.Dispatcher,
	orgID, orgName string, periodStart, periodEnd time.Time) {
	sum, err := db.MonthlyOrgSummary(ctx, orgID, periodStart, periodEnd)
	if err != nil {
		log.Printf("[MONTHLY] org %s summary failed: %v", orgID, err)
		return
	}
	if sum == nil {
		return
	}
	if sum.OrganizationName == "" {
		sum.OrganizationName = orgName
	}

	// Find sites for this org so MatchMonthlySummaryRecipients can
	// scope the recipient query.
	sites, err := db.ListSitesScoped(ctx, database.CallerScope{
		Role:           "admin", // unscoped read; recipient match enforces user scope
		OrganizationID: orgID,
	})
	if err != nil || len(sites) == 0 {
		// No sites for this org — nobody to send to.
		return
	}
	siteIDs := make([]string, 0, len(sites))
	for _, s := range sites {
		siteIDs = append(siteIDs, s.ID)
	}

	recipients, err := db.MatchMonthlySummaryRecipients(ctx, siteIDs)
	if err != nil {
		log.Printf("[MONTHLY] org %s recipients failed: %v", orgID, err)
		return
	}
	if len(recipients) == 0 {
		return
	}
	dispatchRecipients := make([]notify.Recipient, 0, len(recipients))
	for _, r := range recipients {
		dispatchRecipients = append(dispatchRecipients, notify.Recipient{Email: r.Email})
	}

	topEvents := make([]notify.MonthlyTopEvent, 0, len(sum.TopEvents))
	for _, e := range sum.TopEvents {
		topEvents = append(topEvents, notify.MonthlyTopEvent{
			EventID:          e.EventID,
			SiteName:         e.SiteName,
			CameraName:       e.CameraName,
			Severity:         e.Severity,
			HappenedAt:       e.HappenedAt,
			DispositionLabel: e.DispositionLabel,
			AIDescription:    e.AIDescription,
			AVSScore:         e.AVSScore,
		})
	}

	dispatcher.MonthlySummary(ctx, notify.MonthlySummaryContext{
		OrganizationName: sum.OrganizationName,
		PeriodStart:      sum.PeriodStart,
		PeriodEnd:        sum.PeriodEnd,
		SiteCount:        sum.SiteCount,
		CameraCount:      sum.CameraCount,
		IncidentCount:    sum.IncidentCount,
		AlarmCount:       sum.AlarmCount,
		DispositionCount: sum.DispositionCount,
		VerifiedThreats:  sum.VerifiedThreats,
		FalsePositives:   sum.FalsePositives,
		AvgAckSec:        sum.AvgAckSec,
		P95AckSec:        sum.P95AckSec,
		WithinSLA:        sum.WithinSLA,
		OverSLA:          sum.OverSLA,
		TopEvents:        topEvents,
	}, dispatchRecipients)

	log.Printf("[MONTHLY] org %s sent to %d recipient(s)", orgName, len(dispatchRecipients))
}
