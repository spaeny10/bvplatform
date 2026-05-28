// Package main — Ironsight worker binary.
//
// Runs the background batch workloads that previously lived inside the
// api process:
//
//   - RetentionManager  — hourly segment purge per site policy
//   - VLM Indexer       — enriches recording segments with AI descriptions
//   - Export Worker     — processes queued video-export jobs
//   - PPE Worker        — polls cameras on PPE-enabled sites, feeds violations into pending_review_queue (P2-C-01)
//   - Tracking Worker   — aggregates person counts from PPE worker frames (P2-C-02)
//   - Tracking Aggregator — 5-min bucket roll-up (P2-C-02)
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
//     trigger path; split is Phase 3)
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
	"ironsight/internal/crypto"
	"ironsight/internal/database"
	"ironsight/internal/export"
	"ironsight/internal/indexer"
	"ironsight/internal/logging"
	"ironsight/internal/notify"
	"ironsight/internal/ppe"
	"ironsight/internal/recording"
	"ironsight/internal/safety"
	"ironsight/internal/tracking"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("============================================")
	log.Println("  Ironsight Worker — batch background jobs")
	log.Println("============================================")

	cfg := config.Load()

	// P1-C-02: Sentry/GlitchTip error reporting — same init as cmd/server.
	// Empty DSN = no-op (default until GlitchTip LXC is provisioned).
	if err := logging.InitSentry(cfg.SentryDSN, cfg.SentryEnvironment); err != nil {
		log.Printf("[WARN] sentry init failed: %v", err)
	}
	defer logging.FlushSentry()

	// P1-A-05: same key the api uses. Indexer/export don't currently
	// read cameras.password through *database.DB (they touch segments
	// and exports), but plumbing the key here keeps the two binaries
	// configured identically so a future worker query that does hit
	// cameras.password decrypts correctly without a config surprise.
	// Empty key is tolerated — worker doesn't fail-fast like the server
	// because none of its current paths need it.
	credentialsKey, err := crypto.ParseKey(cfg.CameraCredentialsKey)
	if err != nil && cfg.CameraCredentialsKey != "" {
		log.Fatalf("[FATAL] CAMERA_CREDENTIALS_KEY: %v", err)
	}

	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database connect: %v", err)
	}
	defer db.Close()
	if len(credentialsKey) > 0 {
		db.SetCredentialsKey(credentialsKey)
	}

	// AI client — same wiring as api. Indexer needs it; retention and
	// exports don't. Cheap to initialize either way; skipping keeps the
	// worker dependent on AI service availability only at indexing time.
	aiClient := ai.NewClient(ai.Config{
		YOLOEndpoint: cfg.DetectionServiceURL, // repurposed; kept non-critical
		QwenEndpoint: cfg.AIQwenURL,
		Enabled:      cfg.AIEnabled,
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
	if !cfg.WorkerLeaderDisabled {
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
	// P2-C-01: configure PPE frame sweep alongside recording retention.
	retentionMgr.SetPPERetention(cfg.PPEFramesDir, cfg.PPEFrameRetentionDays)
	// P2-C-02: tracking data retention (raw frames + buckets).
	retentionMgr.SetTrackingRetention(cfg.TrackingRawRetentionDays, cfg.TrackingBucketRetentionDays)
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

	// ── PPE worker — P2-C-01 ─────────────────────────────────────
	// The YOLO AI client reuses the existing ai.Client configured for
	// the detection service. Hub is nil in the worker binary — WS
	// broadcast fires on the API side when the frontend polls. If a
	// Redis pub/sub bridge is needed later, wire the hub here.
	ppeWorker := ppe.New(cfg, db, aiClient, nil /* no hub in worker binary */)

	// ── Tracking worker + aggregator — P2-C-02 ────────────────────
	// The tracking worker subscribes to CameraFrameResult values produced
	// by the PPE worker. Channel buffer = 32; the PPE worker uses a
	// non-blocking send so tracking backpressure cannot stall PPE.
	if cfg.TrackingEnabled {
		const trackingChannelBuf = 32
		trackingCh := make(chan ppe.CameraFrameResult, trackingChannelBuf)
		ppeWorker.TrackingCh = trackingCh

		trackingWorker := tracking.New(db, trackingCh)
		trackingWorker.Start(ctx)
		log.Println("[WORKER] Tracking worker started")

		trackingAggregator := tracking.NewAggregator(db, cfg.TrackingBucketMinutes)
		trackingAggregator.Start(ctx)
		log.Println("[WORKER] Tracking aggregator started")
	} else {
		log.Println("[WORKER] Tracking disabled (TRACKING_ENABLED=false)")
	}

	ppeWorker.Start(ctx)
	log.Println("[WORKER] PPE worker started")

	// ── VLM validation worker — P2-C-03 ──────────────────────────────────
	// Default disabled (VLM_WORKER_ENABLED=false) because Qwen is not
	// running on fred at C-03 deploy time. Enable only after confirming
	// Qwen is healthy: GET <AI_QWEN_URL>/health returns {"degraded":false}.
	var vlmWorker *safety.VLMWorker
	if cfg.VLMWorkerEnabled {
		vlmWorker = safety.NewVLMWorker(cfg, db, aiClient)
		vlmWorker.Start(ctx)
		log.Println("[WORKER] VLM validation worker started")
	} else {
		log.Println("[VLM] worker disabled (VLM_WORKER_ENABLED=false) — candidates stay at vlm_verdict='pending'")
	}

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

	go runWeeklyDigest(ctx, cfg, db, notifier)
	log.Println("[WORKER] Weekly digest scheduler started (leader-only)")

	// Graceful shutdown — signal arrives, cancel ctx, wait for workers
	// to finish. Retention respects ctx.Done() immediately; export worker
	// completes any in-flight FFmpeg then exits.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("[WORKER] Signal %v received, shutting down...", sig)
	case <-ctx.Done():
		log.Println("[WORKER] Context canceled (likely lost leadership), shutting down...")
	}

	cancel()
	retentionMgr.Stop()
	vlmIndexer.Stop()
	exportWorker.Wait()
	ppeWorker.Stop()
	if vlmWorker != nil {
		vlmWorker.Stop()
	}

	log.Println("[WORKER] Clean shutdown complete")
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

// runWeeklyDigest is the weekly PPE compliance digest scheduler.
//
// Polls every 30 minutes (vs. hourly for monthly, to keep the send
// window tight — worst-case 30 minutes late, acceptable for a weekly
// report). When the current UTC day-of-week matches cfg.DigestSendDay
// AND the hour matches cfg.DigestSendHour AND the digest_sends table
// has no row for the current ISO week, it runs per-org digest sends.
//
// Durable idempotency: UpsertDigestSend records each successful org
// send in the digest_sends table. A worker restart during the send
// window re-checks the table and skips orgs already recorded.
//
// Per-org error isolation: a failed query or send for one org logs
// and continues to the next; other orgs are unaffected.
//
// Soft-delete composability: compliance queries read pending_review_queue
// which references camera_id/site_id — those are scoped by org_id and
// the query layer already filters deleted cameras/sites via the _active
// views (see compliance_queries.go).
func runWeeklyDigest(ctx context.Context, cfg *config.Config, db *database.DB, dispatcher *notify.Dispatcher) {
	const scope = "org"
	tick := time.NewTicker(30 * time.Minute)
	defer tick.Stop()

	// weekStart returns the Monday 00:00:00 UTC of the ISO week
	// containing t. Used as the idempotency key and the period start.
	weekStart := func(t time.Time) time.Time {
		t = t.UTC()
		// Weekday(): Sunday=0, Monday=1, ..., Saturday=6
		// We want Monday=0 for the offset calculation.
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7 // treat Sunday as day 7 so Monday-offset is 0
		}
		daysToMonday := weekday - 1
		mon := t.AddDate(0, 0, -daysToMonday)
		return time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.UTC)
	}

	check := func() {
		now := time.Now().UTC()
		if int(now.Weekday()) != cfg.DigestSendDay {
			return
		}
		if now.Hour() != cfg.DigestSendHour {
			return
		}

		periodStart := weekStart(now)
		// Digest covers Mon 00:00 through Sun 23:59:59 UTC of the
		// *previous* week, relative to the send day.
		// If send day is Monday, the window is the 7 days ending Sunday.
		periodEnd := periodStart.Add(-time.Second)          // Sunday 23:59:59
		periodStart = periodStart.AddDate(0, 0, -7)         // prior Monday

		log.Printf("[DIGEST] Checking weekly digest for week starting %s...",
			periodStart.Format("2006-01-02"))

		orgs, err := db.ListOrganizationsWithEmail(ctx)
		if err != nil {
			log.Printf("[DIGEST] list orgs failed: %v", err)
			return
		}

		sent := 0
		skipped := 0
		for _, org := range orgs {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Durable idempotency check: has this org already been sent
			// a digest for this ISO week?
			existing, err := db.GetLastDigestSend(ctx, org.ID, scope, periodStart)
			if err != nil {
				log.Printf("[DIGEST] idempotency check failed (org=%s): %v", org.ID, err)
				continue
			}
			if existing != nil {
				// Already sent for this week — skip.
				continue
			}

			dispatched := runWeeklyDigestForOrg(ctx, cfg, db, dispatcher, org.ID, org.Name, periodStart, periodEnd)
			if dispatched {
				if inserted, err := db.UpsertDigestSend(ctx, org.ID, scope, periodStart); err != nil {
					log.Printf("[DIGEST] UpsertDigestSend failed (org=%s): %v", org.ID, err)
				} else if inserted {
					sent++
				}
			} else {
				skipped++
			}

			// Small rate-limit between sends to be polite to the relay.
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}

		log.Printf("[DIGEST] week=%s sent=%d skipped=%d",
			periodStart.Format("2006-01-02"), sent, skipped)
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

// runWeeklyDigestForOrg builds and dispatches the digest for a single
// organization. Returns true if the digest was dispatched (i.e., there
// were recipients and the no-activity check passed), false if skipped.
//
// Soft-delete: ListSitesScoped already reads from sites_active (deleted_at
// IS NULL). Compliance queries filter org_id; cameras are scoped by
// site_id which is already filtered out for deleted sites.
func runWeeklyDigestForOrg(
	ctx context.Context,
	cfg *config.Config,
	db *database.DB,
	dispatcher *notify.Dispatcher,
	orgID, orgName string,
	periodStart, periodEnd time.Time,
) (dispatched bool) {
	f := database.ComplianceFilter{
		OrgID:     orgID,
		Start:     periodStart,
		End:       periodEnd,
		TruncUnit: "day",
	}

	headline, err := db.GetComplianceHeadline(ctx, f)
	if err != nil {
		log.Printf("[DIGEST] org=%s headline query failed: %v", orgID, err)
		return false
	}

	// No-activity skip: if zero violations and zero pending items and
	// DIGEST_NO_ACTIVITY_SKIP is set, don't send a noise email.
	if cfg.DigestNoActivitySkip &&
		headline.TotalViolations == 0 &&
		headline.PendingCount == 0 {
		// Check occupancy too — if tracking has data, send anyway.
		_, hrs, occErr := db.GetComplianceOccupancy(ctx, f)
		if occErr == nil && (hrs == nil || *hrs == 0) {
			log.Printf("[DIGEST] org=%s skipped — no activity in window", orgID)
			return false
		}
	}

	// Top cameras — up to 5.
	topCamsDB, err := db.GetComplianceTopCameras(ctx, f, 5)
	if err != nil {
		log.Printf("[DIGEST] org=%s top-cameras query failed: %v", orgID, err)
		topCamsDB = nil // non-fatal; proceed with empty list
	}

	// Occupancy / person-hours.
	_, personHours, _ := db.GetComplianceOccupancy(ctx, f)
	personHoursAvail := personHours != nil
	var personHoursVal float64
	if personHours != nil {
		personHoursVal = *personHours
	}

	// Compliance rate: (reviewed - violations) / reviewed * 100.
	var complianceRate float64
	if headline.TotalReviewed > 0 {
		compliant := headline.TotalReviewed - headline.TotalViolations
		if compliant < 0 {
			compliant = 0
		}
		complianceRate = float64(compliant) / float64(headline.TotalReviewed) * 100
	}

	// Resolve recipients via digest-specific subscriptions.
	// Sites are fetched to scope MatchWeeklyDigestRecipients (same
	// pattern as runMonthlyForOrg uses for MatchMonthlySummaryRecipients).
	sites, err := db.ListSitesScoped(ctx, database.CallerScope{
		Role:           "admin",
		OrganizationID: orgID,
	})
	if err != nil || len(sites) == 0 {
		// No active sites for this org.
		log.Printf("[DIGEST] org=%s no active sites, skipping", orgID)
		return false
	}
	siteIDs := make([]string, 0, len(sites))
	for _, s := range sites {
		siteIDs = append(siteIDs, s.ID)
	}

	alarmRecipients, err := db.MatchWeeklyDigestRecipients(ctx, siteIDs)
	if err != nil {
		log.Printf("[DIGEST] org=%s recipients query failed: %v", orgID, err)
		return false
	}
	if len(alarmRecipients) == 0 {
		// No subscribed recipients for this org — no-op, not an error.
		return false
	}

	dispatchRecipients := make([]notify.Recipient, 0, len(alarmRecipients))
	for _, r := range alarmRecipients {
		dispatchRecipients = append(dispatchRecipients, notify.Recipient{Email: r.Email})
	}

	// Build top-cameras list for the notify layer.
	topCams := make([]notify.DigestTopCamera, 0, len(topCamsDB))
	for _, c := range topCamsDB {
		topCams = append(topCams, notify.DigestTopCamera{
			CameraName:     c.CameraName,
			ViolationCount: c.ViolationCount,
			PctOfTotal:     c.PctOfTotal,
		})
	}

	baseURL := cfg.PublicURL
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}

	digestCtx := notify.WeeklyDigestContext{
		OrganizationName:     orgName,
		PeriodStart:          periodStart,
		PeriodEnd:            periodEnd,
		TotalViolations:      headline.TotalViolations,
		TotalReviewed:        headline.TotalReviewed,
		PendingCount:         headline.PendingCount,
		ComplianceRate:       complianceRate,
		ViolationTrend:       0, // trend comparison deferred (no prior-week query yet)
		TopCameras:           topCams,
		PersonHoursAvailable: personHoursAvail,
		PersonHours:          personHoursVal,
		ComplianceURL:        baseURL + "/portal/compliance",
		PendingReviewURL:     baseURL + "/portal/compliance?tab=pending",
		UnsubscribeURL:       baseURL + "/portal/notifications",
	}

	dispatcher.WeeklyDigest(ctx, digestCtx, dispatchRecipients)
	log.Printf("[DIGEST] org=%s sent to %d recipient(s)", orgName, len(dispatchRecipients))
	return true
}
