package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"ironsight/internal/ai"
	"ironsight/internal/api"
	authpkg "ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/detection"
	"ironsight/internal/dualwrite"
	"ironsight/internal/export"
	"ironsight/internal/indexer"
	"ironsight/internal/logging"
	appmetrics "ironsight/internal/metrics"
	msdriver "ironsight/internal/milesight"
	"ironsight/internal/notify"
	"ironsight/internal/onvif"
	"ironsight/internal/recording"
	"ironsight/internal/streaming"
	"ironsight/migrations"
	"strings"
)

func main() {
	// Load configuration first so the log level is available before
	// any log lines fire. Pre-config startup messages go to stderr via
	// the stdlib log defaults; once InstallAsDefault runs, every
	// subsequent log.Printf and slog call emits JSON.
	cfg := config.Load()

	// P1-C-02: Sentry/GlitchTip error reporting. Init BEFORE the
	// logger so the first Error-level line (if any) gets captured.
	// Empty SENTRY_DSN = no-op — the default until the GlitchTip LXC
	// is provisioned. FlushSentry is deferred so events near shutdown
	// are delivered before the process exits.
	if err := logging.InitSentry(cfg.SentryDSN, cfg.SentryEnvironment); err != nil {
		log.Printf("[WARN] sentry init failed: %v", err)
	}
	defer logging.FlushSentry()

	// P1-C-01: structured logging. JSON to stderr, level from
	// LOG_LEVEL env (info default). Also bridges the stdlib log
	// package so the 300+ legacy log.Printf call sites emit
	// structured lines too, migrate-as-touched. P1-C-02: use
	// NewWithSentry so Error-level records are also forwarded to
	// Sentry (no-op when DSN is empty).
	logger := logging.NewWithSentry(cfg.LogLevel)
	logging.InstallAsDefault(logger)
	logger.Info("server_starting",
		slog.String("product", cfg.ProductName),
		slog.String("port", cfg.ServerPort),
		slog.String("log_level", cfg.LogLevel),
	)

	// Ensure storage directories exist
	for _, dir := range []string{cfg.StoragePath, cfg.HLSPath, cfg.ExportPath, cfg.ThumbnailPath} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("[FATAL] Cannot create directory %s: %v", dir, err)
		}
	}

	// Connect to database
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] Database connection failed: %v", err)
	}
	defer db.Close()

	// Apply goose-tracked migrations before any other DB-touching startup
	// runs. The 0001_baseline.sql migration is idempotent against fred's
	// already-populated schema (every CREATE / ADD CONSTRAINT / TRIGGER
	// is guarded), so on fred this just records "version 1 applied" in
	// goose_db_version and changes nothing else. On a fresh DB it builds
	// the full schema from scratch. Subsequent migrations (P1-B-02 onward)
	// will land in migrations/000N_*.sql and be picked up automatically
	// via the //go:embed directive in the migrations package.
	//
	// We bridge the existing pgxpool to database/sql via pgx's stdlib
	// helper so goose (which is database/sql-only) shares the same pool
	// rather than opening a second connection. The bridge *sql.DB is
	// closed before we exit so we don't leak the wrapping handles.
	gooseDB := stdlib.OpenDBFromPool(db.Pool)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("[FATAL] goose.SetDialect: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpContext(context.Background(), gooseDB, "."); err != nil {
		// P1-C-04: emit a discrete app alert before fatally exiting so that if
		// the process is being restarted by a supervisor and briefly scrapes
		// during the boot loop, the metric is visible to alertmanager.
		appmetrics.SetCustomAlert("goose_migration_failure", "critical", err.Error())
		gooseDB.Close()
		log.Fatalf("[FATAL] goose.Up: %v", err)
	}
	// P1-C-03: record the applied migration version in Prometheus so
	// alerts can cross-reference the schema state at incident time.
	if v, verr := goose.GetDBVersion(gooseDB); verr == nil {
		appmetrics.SetGooseMigrationVersion(v)
	}
	gooseDB.Close()
	log.Println("[MIGRATIONS] goose up applied; see goose_db_version for current version")

	// P1-B-02 cleanup (2026-05-26): inline DDL block removed.
	// All schema lives in migrations/0005_camera_settings_columns.sql through
	// 0020_ai_runtime_metrics.sql, applied by goose.UpContext above.

	// Override config paths from DB storage locations (if any are configured)
	storageConfigured := false
	if locs, err := db.ListStorageLocations(context.Background()); err == nil && len(locs) > 0 {
		for _, loc := range locs {
			if !loc.Enabled {
				continue
			}
			// Use the first enabled location with purpose "recordings" or "all"
			if loc.Purpose == "recordings" || loc.Purpose == "all" {
				recPath := filepath.Join(loc.Path, "recordings")
				hlsPath := filepath.Join(loc.Path, "hls")
				os.MkdirAll(recPath, 0755)
				os.MkdirAll(hlsPath, 0755)
				cfg.StoragePath = recPath
				cfg.HLSPath = hlsPath
				log.Printf("[STORAGE] Using configured location: %s (label: %s)", loc.Path, loc.Label)
				storageConfigured = true
				break
			}
		}
	}
	if !storageConfigured {
		// No storage configured — disable recording until user sets one up
		cfg.StoragePath = ""
		cfg.HLSPath = ""
		log.Println("[STORAGE] ⚠️  No storage location configured — recordings disabled.")
		log.Println("[STORAGE]    Use Settings → Storage to add a storage location.")
	}

	// First-run: auto-create default admin if no users exist
	if exists, _ := db.UserExists(context.Background()); !exists {
		hash, err := authpkg.HashPassword(cfg.DefaultAdminPass)
		if err == nil {
			_, err = db.CreateUser(context.Background(), &database.UserCreate{
				Username:    "admin",
				Role:        "admin",
				DisplayName: "System Administrator",
				Email:       "admin@ironsight.io",
			}, hash)
		}
		if err != nil {
			log.Printf("[AUTH] Warning: could not create default admin: %v", err)
		} else {
			log.Printf("[AUTH] Default admin created — username: admin  password: %s", cfg.DefaultAdminPass)
			log.Println("[AUTH] ⚠️  Change this password immediately via the settings page!")
		}
	}

	// Demo data seeding (P1-B-09): the demo-portfolio + demo-users
	// helpers, and the prior env-gate that wrapped them, live in the
	// separate cmd/seed binary now. Server startup never seeds.
	// Operators run `/app/seed --all` (or its --portfolio / --users
	// variants) explicitly against a staging database when they need
	// demo content. See internal/seed/ and cmd/seed/main.go.

	// Root context for all background goroutines. Canceled on SIGINT /
	// SIGTERM so the WS hub, recording engine, retention manager, and
	// other long-runners exit cleanly. Defined here (before hub.Run)
	// rather than down by the signal-wait so every spawn point can use it.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// Initialize subsystems
	hub := api.NewHub()
	// Optional: Redis pub/sub bridge for multi-replica WS fanout. Silently
	// no-ops when REDIS_URL is unset (single-replica deployments don't
	// need it). See internal/api/websocket.go for the bridge design.
	if err := hub.AttachRedisBridge(rootCtx, cfg.RedisURL, cfg.RedisWSChannel); err != nil {
		log.Printf("[WS] Redis bridge attach failed: %v — continuing in-memory only", err)
	}
	// P1-A-04: supply cfg + db so HandleWebSocket can do auth + RBAC
	// before the WebSocket upgrade, and so the RBAC refresher can re-query
	// assignments every 60 s without a reconnect.
	hub.Configure(cfg, db)
	go hub.Run(rootCtx)

	// LOCAL-02: backfill any cameras whose profile_token was never
	// populated (typical for SQL-seeded cameras that bypassed the
	// /api/cameras create flow). Each empty-profile camera gets one
	// ONVIF discovery round-trip up to profileBackfillPerCameraTimeout
	// long, capped at profileBackfillConcurrency in parallel. Runs in
	// a goroutine so a slow camera-fleet doesn't delay HTTP serving;
	// PTZ remains broken for un-backfilled cameras only.
	go api.BackfillProfileTokens(rootCtx, db)

	recEngine := recording.NewEngine(cfg, db)
	hlsServer := streaming.NewHLSServer(cfg, db)
	mtxServer := streaming.NewMediaMTXServer(cfg)

	// Batch-job workers. In single-binary mode (RUN_WORKERS=true, the
	// default) we instantiate and start them in-process below. In the
	// container split (RUN_WORKERS=false) the sibling `worker` service
	// owns these jobs — we skip instantiation so there's no race on the
	// same DB tables. See cmd/worker/main.go and MasterDeployment.md §2.
	var (
		retentionMgr *recording.RetentionManager
		exportWorker *export.Worker
	)
	if cfg.RunWorkers {
		retentionMgr = recording.NewRetentionManager(db)
		exportWorker = export.NewWorker(cfg, db)
	}

	// AI Detection — receives bounding box data from ONVIF Profile M analytics events
	det := detection.New(hub)
	log.Println("[DET] ONVIF analytics detection enabled (Profile M cameras)")

	// AI Pipeline — YOLO (detection) + Qwen (reasoning) for event-triggered analysis.
	// Endpoints and the on/off switch come from cfg (env vars AI_YOLO_URL /
	// AI_QWEN_URL / AI_ENABLED parsed at startup).
	aiClient := ai.NewClient(ai.Config{
		YOLOEndpoint: cfg.AIYOLOURL,
		QwenEndpoint: cfg.AIQwenURL,
		Enabled:      cfg.AIEnabled,
	})
	aiClient.CheckHealth(context.Background())

	// Persisted runtime metrics for the Services dashboard. Samples
	// every 30s; rows go to the ai_runtime_metrics hypertable created
	// during the migration block above.
	api.StartAIMetricsSampler(context.Background(), db, aiClient, cfg.AIYOLOURL, cfg.AIQwenURL, 30*time.Second)

	// Background VLM indexer: enriches every recording segment with a
	// searchable description during idle hours. Scales with INDEXER_CONCURRENCY
	// (1 on a test 3070, 4-8 on production A40s). Disable with
	// INDEXER_ENABLED=false.
	//
	// Gated by RunWorkers so the container-split deployment doesn't run
	// two indexers racing on the same segments (the worker container owns
	// this job in that layout). Single-binary dev keeps the default.
	var vlmIndexer *indexer.Indexer
	if cfg.RunWorkers {
		vlmIndexer = indexer.New(cfg, db, aiClient)
		vlmIndexer.Start()
		defer vlmIndexer.Stop()
	}

	// Wire alert generation: detection events → AlertEvent broadcast via WebSocket
	det.AlertEmitter = func(result *detection.DetectionResult) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			cameraName, siteID, siteName, err := db.GetCameraWithSite(ctx, result.CameraID)
			if err != nil || siteID == "" {
				return // camera not assigned to a site — skip
			}

			// Classify the dominant detection
			alertType, severity := "person_detected", "high"
			for _, box := range result.Boxes {
				switch box.Label {
				case "vehicle", "car", "truck", "van":
					alertType, severity = "vehicle_detected", "medium"
				}
			}

			typeLabel := map[string]string{
				"person_detected":  "Person",
				"vehicle_detected": "Vehicle",
			}[alertType]

			now := time.Now().UnixMilli()
			// One alarm per camera per minute — prevents alert flooding
			alarmID := fmt.Sprintf("ALM-%s-%d", result.CameraID[:8], now/60000)

			// Record the detection as an event row first so the alarm we
			// create has a real foreign key to point at. Without this,
			// triggering_event_id would be NULL and the forensic question
			// "which detection frame fired this alarm?" has no answer in
			// SQL. Best-effort: if the insert fails, the alarm still gets
			// written, just without the event linkage.
			var eventID *int64
			camUUID, _ := uuid.Parse(result.CameraID)
			evt := &database.Event{
				CameraID:  camUUID,
				EventTime: time.UnixMilli(now),
				EventType: alertType,
				Details:   map[string]interface{}{"boxes": result.Boxes},
			}
			if err := db.InsertEvent(ctx, evt); err == nil {
				id := evt.ID
				eventID = &id

				// P4-SCHEMA-02: dual-write the detection event to the new
				// detections table.  Non-fatal on failure.
				go dualWriteSecurityEvent(db, camUUID, siteID, alertType, time.UnixMilli(now), nil)
			}

			alarm := &database.ActiveAlarm{
				ID:                alarmID,
				TriggeringEventID: eventID,
				SiteID:            siteID,
				SiteName:          siteName,
				CameraID:          result.CameraID,
				CameraName:        cameraName,
				Severity:          severity,
				Type:              alertType,
				Description:       fmt.Sprintf("%s detected at %s", typeLabel, cameraName),
				Ts:                now,
				SlaDeadlineMs:     now + 90_000, // 90-second SLA
			}

			created, err := db.CreateActiveAlarm(ctx, alarm)
			if err != nil || !created {
				return // already exists this minute — suppress duplicate broadcast
			}

			// Broadcast { type: "alert", data: ActiveAlarm } to all WS clients
			msg, _ := json.Marshal(map[string]interface{}{
				"type": "alert",
				"data": alarm,
			})
			hub.Broadcast(msg)
			log.Printf("[ALERT] %s → %s (%s) siteID=%s", alertType, cameraName, result.CameraID[:8], siteID)
		}()
	}

	// Subscriber registry — tracks ONVIF event subscribers for cleanup on camera delete
	subReg := api.NewSubscriberRegistry()

	// `ctx` here is the same root context used by the WS hub above —
	// we alias for backwards-compat with the variable name used
	// throughout this function.
	ctx := rootCtx

	// Start background services — only when this process owns them.
	// When RUN_WORKERS=false these are nil and the sibling worker
	// container runs the equivalents.
	if cfg.RunWorkers {
		go retentionMgr.Start(ctx)
		exportWorker.Start(ctx)
		log.Println("[SERVER] Batch workers running in-process (RUN_WORKERS=true)")
	} else {
		log.Println("[SERVER] Batch workers delegated to sibling container (RUN_WORKERS=false)")
	}

	// Auto-start recording and streaming for cameras that have recording enabled
	go autoStartCameras(ctx, db, cfg, recEngine, hlsServer, mtxServer, hub, det, subReg, aiClient)

	// Notification dispatcher — feeds the alarm-disposition emails and
	// (next batch) the monthly-summary report. SMTP and Twilio fall
	// back to stub mailers when their respective env vars aren't set,
	// so dev environments still produce visible log output through
	// the dispatcher without needing real SendGrid / Twilio creds.
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

	// P1-C-03: sync pgxpool stats into Prometheus gauges every 15 s.
	// The pool exposes a Stat() snapshot (no blocking); we read it on a
	// ticker so the Prom scraper always sees a reasonably fresh value.
	if cfg.MetricsEnabled {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s := db.Pool.Stat()
					appmetrics.SyncDBPoolStats(appmetrics.DBPoolStat{
						AcquireCount: s.AcquireCount(),
						IdleConns:    s.IdleConns(),
						TotalConns:   s.TotalConns(),
					})
				}
			}
		}()
	}

	// Create HTTP router (Chi-based, already has all routes including HLS and exports)
	player := onvif.NewBackchannelPlayer()
	router := api.NewRouter(cfg, db, hub, recEngine, hlsServer, mtxServer, det, player, subReg, notifier, aiClient)

	// Start HTTP server
	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Println("============================================")
	log.Printf("  API Server:  http://localhost%s", addr)
	log.Printf("  Frontend:    http://localhost:3000")
	log.Printf("  WebSocket:   ws://localhost%s/ws", addr)
	log.Printf("  Health:      http://localhost%s/api/health", addr)
	log.Println("============================================")

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("[SERVER] Received signal %v, shutting down...", sig)

		recEngine.StopAll()
		hlsServer.StopAll()
		mtxServer.Stop()
		if retentionMgr != nil {
			retentionMgr.Stop()
		}
		subReg.StopAll()
		// Cancels rootCtx → WS hub, retention manager, export worker,
		// and any other long-running consumer of rootCtx all wind down.
		rootCancel()

		os.Exit(0)
	}()

	// Use an explicit *http.Server with ReadHeaderTimeout to defeat slowloris
	// attacks (gosec G114 — http.ListenAndServe has no timeouts at all).
	// Deliberately leaving WriteTimeout and IdleTimeout unset because the
	// router serves WebSockets (/ws, /ws/alerts) and an HLS segment stream
	// path whose write deadlines need to be open-ended; a global WriteTimeout
	// would silently sever those long-lived connections after N seconds.
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[FATAL] Server failed: %v", err)
	}
}

// autoStartCameras initializes recording and HLS for cameras with recording enabled
func autoStartCameras(ctx context.Context, db *database.DB, cfg *config.Config, recEngine *recording.Engine, hlsServer *streaming.HLSServer, mtxServer *streaming.MediaMTXServer, hub *api.Hub, det *detection.Manager, subReg *api.SubscriberRegistry, aiClient *ai.Client) {
	// Wait a moment for the server to be ready
	time.Sleep(2 * time.Second)

	cameras, err := db.ListCameras(ctx)
	if err != nil {
		log.Printf("[STARTUP] Failed to list cameras: %v", err)
		return
	}

	// Alarm rate-limiter. Most VCA topics describe the SAME physical
	// activity from different angles (linecross + object + intrusion +
	// loitering + human + vehicle all fire together when a person walks
	// past). Keying the cooldown by event type lets them all through
	// simultaneously, which (a) buries the operator in duplicate alarms
	// for one event and (b) queues a Qwen inference per topic and
	// saturates the GPU. We bucket those motion-style topics under one
	// camera-level key. lpr/face stay per-type because they carry
	// identifying information that's lost if coalesced.
	motionTopics := map[string]bool{
		"linecross": true, "object": true, "intrusion": true,
		"loitering": true, "human": true, "vehicle": true, "motion": true,
	}
	// Per-camera AI in-flight gate. Qwen takes 5–30s; if a second alarm
	// fires for the same camera while Qwen is still running, queueing
	// another inference behind it just deepens the latency hole and
	// keeps the GPU pegged. Skip the AI step when one is already in
	// flight — the alarm itself still gets created and recorded, the
	// operator just doesn't get the second AI verdict.
	var aiInFlightMu sync.Mutex
	aiInFlight := make(map[string]bool)
	tryAcquireAI := func(camID string) bool {
		aiInFlightMu.Lock()
		defer aiInFlightMu.Unlock()
		if aiInFlight[camID] {
			return false
		}
		aiInFlight[camID] = true
		return true
	}
	releaseAI := func(camID string) {
		aiInFlightMu.Lock()
		defer aiInFlightMu.Unlock()
		delete(aiInFlight, camID)
	}

	var alarmCooldownMu sync.Mutex
	alarmLastFired := make(map[string]time.Time)
	allowAlarm := func(camID, evtType string) bool {
		key := camID + ":" + evtType
		if motionTopics[evtType] {
			key = camID + ":motion"
		}
		alarmCooldownMu.Lock()
		defer alarmCooldownMu.Unlock()
		if t, ok := alarmLastFired[key]; ok && time.Since(t) < 60*time.Second {
			return false
		}
		alarmLastFired[key] = time.Now()
		return true
	}

	for _, cam := range cameras {
		if !cam.Recording || cam.RTSPUri == "" {
			continue
		}

		// Connect to camera via ONVIF to verify
		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		if _, err := client.Connect(ctx); err != nil {
			log.Printf("[STARTUP] Camera %s offline: %v", cam.Name, err)
			db.UpdateCameraStatus(ctx, cam.ID, "offline")
			continue
		}

		db.UpdateCameraStatus(ctx, cam.ID, "online")

		// Only start recording/HLS if storage is configured
		if cfg.StoragePath != "" {
			if cam.Recording {
				// Recording policy now lives on the camera's site — see the
				// 2026-04 migration. SettingsForCamera falls back to engine
				// defaults for cameras that aren't yet site-assigned.
				settings := recording.SettingsForCamera(ctx, db, &cam)
				if err := recEngine.StartRecording(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri, settings); err != nil {
					log.Printf("[STARTUP] Failed to start recording for %s: %v", cam.Name, err)
				}
			} else {
				// Start HLS live stream standalone
				if err := hlsServer.StartLiveStream(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri); err != nil {
					log.Printf("[STARTUP] Failed to start HLS for %s: %v", cam.Name, err)
				}
			}
			log.Printf("[STARTUP] Camera %s: recording + streaming + events active", cam.Name)
		} else {
			log.Printf("[STARTUP] Camera %s: online (no storage — recording disabled)", cam.Name)
		}

		// Start event subscription (only if events are enabled for this camera)
		if cam.EventsEnabled {
			// Semaphore to limit concurrent thumbnail captures per camera
			thumbSem := make(chan struct{}, 3)
			camName := cam.Name
			_ = cam.OnvifAddress // used by driver hooks below
			subscriber := onvif.NewEventSubscriber(client, cam.ID, func(cameraID uuid.UUID, eventType string, details map[string]interface{}) {
				// Store event in database with a timeout to avoid holding pool connections
				dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer dbCancel()
				evt := &database.Event{
					CameraID:  cameraID,
					EventTime: time.Now(),
					EventType: eventType,
					Details:   details,
				}
				if err := db.InsertEvent(dbCtx, evt); err != nil {
					log.Printf("[EVENTS] Failed to store event: %v", err)
				} else {
					// P4-SCHEMA-02: dual-write the ONVIF event to detections.
					// Use cam.SiteID captured at subscription time (safe:
					// closures here capture the outer cam var).
					camSiteID := cam.SiteID
					go dualWriteSecurityEvent(db, cameraID, camSiteID, eventType, evt.EventTime, nil)
				}

				log.Printf("[EVENTS] %s from %s: %s", eventType, camName, details["topic"])

				// Trigger event-based recording clip creation
				recEngine.TriggerEvent(cameraID, eventType)

				// Broadcast to WebSocket clients (include event_id for thumbnail correlation).
				// "source" mirrors the normalized DetectionSource the REST feed
				// projects (decorateEventSources), so boot-time-subscribed live
				// events render the camera-vs-server badge consistently with the
				// HandleCreateCamera live path (which already sets it).
				wsMsg, _ := json.Marshal(map[string]interface{}{
					"type":       "event",
					"id":         evt.ID,
					"camera_id":  cameraID.String(),
					"event":      eventType,
					"event_type": eventType,
					"event_time": evt.EventTime.Format(time.RFC3339),
					"details":    details,
					"source":     api.DetectionSource(details),
					"time":       time.Now().Format(time.RFC3339),
				})
				hub.Broadcast(wsMsg)

				// ── Generate SOC alarm for VCA/AI events on site-assigned cameras ──
				alarmTypes := map[string]bool{
					"intrusion": true, "linecross": true, "human": true, "vehicle": true,
					"face": true, "loitering": true, "lpr": true, "object": true,
				}
				if alarmTypes[eventType] && allowAlarm(cameraID.String(), eventType) {
					eventTimestamp := evt.EventTime // capture before goroutine — this is the exact event time
					go func() {
						// Panic guard: a single bad event must not crash the
						// whole event loop. Recover, log, and move on.
						defer func() {
							if rec := recover(); rec != nil {
								log.Printf("[ALARM] PANIC in alarm-generation goroutine: %v", rec)
							}
						}()
						cn, siteID, siteName, err := db.GetCameraWithSite(context.Background(), cameraID.String())
						if err != nil || siteID == "" {
							return
						}
						now := time.Now().UnixMilli()

						// Extract AI confidence score from event details (0.0–1.0)
						var aiScore float64
						if s, ok := details["score"].(float64); ok {
							aiScore = s
						}
						objType, _ := details["obj_type"].(string)
						ruleName, _ := details["rule_name"].(string)

						severity := "medium"
						switch eventType {
						case "intrusion", "human", "face":
							severity = "critical"
						case "vehicle", "linecross", "loitering", "lpr":
							severity = "high"
						}
						// Downgrade severity for low-confidence detections
						if aiScore > 0 && aiScore < 0.6 {
							severity = "medium"
						}
						alarmID := fmt.Sprintf("alarm-%s-%d", cameraID.String()[:8], now)
						scoreStr := ""
						if aiScore > 0 {
							scoreStr = fmt.Sprintf(" (%.0f%% confidence)", aiScore*100)
						}
						objStr := ""
						if objType != "" {
							objStr = " " + objType
						}
						ruleStr := ""
						if ruleName != "" {
							ruleStr = " — " + ruleName
						}
						description := fmt.Sprintf("%s%s detected on %s at %s%s%s", eventType, objStr, cn, siteName, ruleStr, scoreStr)

						// Find the recording clip link immediately (completed segments only — fast).
						clipRelPath := recording.FindEventClip(cfg.StoragePath, cameraID.String(), eventTimestamp)
						clipURL := ""
						if clipRelPath != "" {
							clipURL = "/recordings/" + clipRelPath
						}

						// ── Incident correlation ──
						// Find or create an incident that groups alarms at the same
						// site within a 5-minute window. Adjacent cameras or repeated
						// events on one camera become a single SOC dispatch item.
						inc, _ := db.FindOpenIncident(context.Background(), siteID, now)
						isNewIncident := inc == nil
						if isNewIncident {
							// ID is intentionally left empty — CreateIncident
							// assigns an INC-YYYY-NNNN identifier from the
							// annual sequence.
							inc = &database.Incident{
								SiteID:        siteID,
								SiteName:      siteName,
								Severity:      severity,
								Status:        "active",
								AlarmCount:    1,
								CameraIDs:     []string{cameraID.String()},
								CameraNames:   []string{cn},
								Types:         []string{eventType},
								LatestType:    eventType,
								Description:   description,
								ClipURL:       clipURL,
								FirstAlarmTs:  now,
								LastAlarmTs:   now,
								SlaDeadlineMs: now + 90000,
							}
							if err := db.CreateIncident(context.Background(), inc); err != nil {
								log.Printf("[ALARM] Failed to create incident: %v", err)
								return
							}
						} else {
							_ = db.AttachAlarmToIncident(context.Background(),
								inc.ID, cameraID.String(), cn, eventType, severity,
								description, "", clipURL, now, now+90000)
							// Refresh local copy for broadcast
							inc.AlarmCount++
							inc.LastAlarmTs = now
							inc.LatestType = eventType
							inc.Description = description
							inc.SlaDeadlineMs = now + 90000
							if severity == "critical" || (severity == "high" && inc.Severity != "critical") {
								inc.Severity = severity
							}
							// Add camera if new
							found := false
							for _, id := range inc.CameraIDs {
								if id == cameraID.String() {
									found = true
									break
								}
							}
							if !found {
								inc.CameraIDs = append(inc.CameraIDs, cameraID.String())
								inc.CameraNames = append(inc.CameraNames, cn)
							}
						}

						// Create the child alarm linked to the incident
						alarm := &database.ActiveAlarm{
							ID:            alarmID,
							IncidentID:    inc.ID,
							SiteID:        siteID,
							SiteName:      siteName,
							CameraID:      cameraID.String(),
							CameraName:    cn,
							Severity:      severity,
							Type:          eventType,
							Description:   description,
							SnapshotURL:   "",
							ClipURL:       clipURL,
							Ts:            now,
							SlaDeadlineMs: now + 90000,
						}
						created, err := db.CreateActiveAlarm(context.Background(), alarm)
						if err != nil || !created {
							// Compensating delete: we just created an incident
							// in this path and now the alarm row didn't take.
							// Without cleanup we'd leave an "incident with 0
							// alarms" orphan. Best-effort — if the delete also
							// fails the retention manager will reap it later.
							if isNewIncident && inc != nil && inc.ID != "" {
								if _, delErr := db.Pool.Exec(context.Background(),
									`DELETE FROM incidents WHERE id=$1 AND alarm_count=1`, inc.ID); delErr != nil {
									log.Printf("[ALARM] orphan incident %s cleanup failed: %v", inc.ID, delErr)
								}
							}
							if err != nil {
								log.Printf("[ALARM] CreateActiveAlarm failed for %s: %v", alarmID, err)
							}
							return
						}
						log.Printf("[ALARM] %s → alarm %s → incident %s (%s at %s, %d alarms)",
							eventType, alarm.ID, inc.ID, cn, siteName, inc.AlarmCount)

						// Broadcast incident-level update to operators.
						// New incidents send "incident_new"; subsequent alarms send "incident_update".
						msgType := "incident_update"
						if isNewIncident {
							msgType = "incident_new"
						}
						incMsg, _ := json.Marshal(map[string]interface{}{
							"type": msgType,
							"data": inc,
						})
						hub.Broadcast(incMsg)

						// Also send the individual alarm for backward compat / detail views
						alertMsg, _ := json.Marshal(map[string]interface{}{
							"type": "alert",
							"data": map[string]interface{}{
								"id": alarm.ID, "incident_id": inc.ID,
								"site_id": siteID, "site_name": siteName,
								"camera_id": cameraID.String(), "camera_name": cn,
								"severity": severity, "type": eventType,
								"description": alarm.Description, "ts": now,
								"acknowledged": false, "escalation_level": 0,
								"sla_deadline_ms": alarm.SlaDeadlineMs,
								"snapshot_url":    "",
								"clip_url":        clipURL,
								"ai_score":        aiScore,
								"obj_type":        objType,
								"rule_name":       ruleName,
								"bounding_boxes":  details["bounding_boxes"],
							},
						})
						hub.Broadcast(alertMsg)

						// Async snapshot + AI analysis pipeline
						{
							snapAlarmID := alarmID
							snapIncidentID := inc.ID
							snapCameraID := cameraID.String()
							camOnvifAddr := cam.OnvifAddress
							camUser := cam.Username
							camPass := cam.Password
							camMfg := cam.Manufacturer
							evtSiteContext := fmt.Sprintf("Site: %s. Camera: %s. Event type: %s.", siteName, cn, eventType)

							go func() {
								// Panic guard: snapshot/AI pipeline touches a lot of
								// external state (RTSP, FFmpeg, AI service). A panic
								// here must not bring down the server.
								defer func() {
									if rec := recover(); rec != nil {
										log.Printf("[ALARM] PANIC in snapshot/AI goroutine for alarm %s: %v", snapAlarmID, rec)
									}
								}()
								var jpegFrame []byte
								var snapshotURL string

								// Strategy 1: Grab snapshot from Milesight /snapshot.cgi (fast, current frame)
								if strings.Contains(strings.ToLower(camMfg), "milesight") && camOnvifAddr != "" {
									msCam := msdriver.New(camOnvifAddr, camUser, camPass)
									snap, err := msCam.Snapshot()
									switch {
									case err != nil:
										log.Printf("[ALARM] Snapshot S1 (Milesight /snapshot.cgi) failed for alarm %s: %v", snapAlarmID, err)
									case len(snap) <= 1000:
										log.Printf("[ALARM] Snapshot S1 (Milesight /snapshot.cgi) returned %d bytes for alarm %s — treating as failure", len(snap), snapAlarmID)
									default:
										jpegFrame = snap
										log.Printf("[ALARM] Snapshot via Milesight /snapshot.cgi (%d bytes)", len(snap))
									}
								}

								// Strategy 2: Extract from recording segment (fallback)
								if len(jpegFrame) == 0 && cfg.StoragePath != "" {
									segAbsPath, _, segStartTime := recording.FindEventClipFull(cfg.StoragePath, snapCameraID, eventTimestamp)
									if segAbsPath == "" {
										log.Printf("[ALARM] Snapshot S2 (segment extract) failed for alarm %s: no segment found for camera %s at %s", snapAlarmID, snapCameraID, eventTimestamp.Format(time.RFC3339))
									} else {
										offsetSec := eventTimestamp.Sub(segStartTime).Seconds()
										snapDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", snapCameraID)
										snapFile := filepath.Join(snapDir, snapAlarmID+".jpg")
										os.MkdirAll(snapDir, 0755)
										if _, err := recording.ExtractFrameFromSegment(cfg.FFmpegPath, segAbsPath, snapFile, offsetSec); err != nil {
											log.Printf("[ALARM] Snapshot S2 (segment extract) failed for alarm %s: seg=%s offset=%.1fs err=%v", snapAlarmID, filepath.Base(segAbsPath), offsetSec, err)
										} else if data, rerr := os.ReadFile(snapFile); rerr == nil && len(data) > 1000 {
											jpegFrame = data
											log.Printf("[ALARM] Snapshot via segment extract (seg=%s offset=%.1fs %d bytes)", filepath.Base(segAbsPath), offsetSec, len(data))
										} else {
											log.Printf("[ALARM] Snapshot S2 (segment extract) produced empty/small file for alarm %s: seg=%s offset=%.1fs", snapAlarmID, filepath.Base(segAbsPath), offsetSec)
										}
									}
								}

								if len(jpegFrame) == 0 {
									log.Printf("[ALARM] No snapshot available for alarm %s — both strategies failed", snapAlarmID)
								}

								// Save snapshot to disk + update DB
								if len(jpegFrame) > 0 {
									snapDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", snapCameraID)
									os.MkdirAll(snapDir, 0755)
									snapFile := filepath.Join(snapDir, snapAlarmID+".jpg")
									os.WriteFile(snapFile, jpegFrame, 0644)

									snapshotURL = "/snapshots/" + snapCameraID + "/" + snapAlarmID + ".jpg"
									_ = db.UpdateActiveAlarmClip(context.Background(), snapAlarmID, clipURL, snapshotURL)
									_ = db.UpdateIncidentSnapshot(context.Background(), snapIncidentID, clipURL, snapshotURL)

									// Push snapshot to operators
									snapMsg, _ := json.Marshal(map[string]interface{}{
										"type": "alarm_snapshot",
										"data": map[string]interface{}{
											"alarm_id":     snapAlarmID,
											"incident_id":  snapIncidentID,
											"snapshot_url": snapshotURL,
										},
									})
									hub.Broadcast(snapMsg)
								}

								// ── AI Pipeline: YOLO → Qwen ──
								// Per-camera in-flight gate: drop the AI step
								// when another inference for this camera is
								// already running. Snapshot + alarm are still
								// captured; we just skip the (now redundant)
								// AI verdict for this burst.
								if len(jpegFrame) > 0 && tryAcquireAI(snapCameraID) {
									defer releaseAI(snapCameraID)
									aiResult := aiClient.Analyze(context.Background(), jpegFrame, siteID, evtSiteContext)
									if aiResult != nil && aiResult.Description != "" {
										ppeInfo := ""
										if len(aiResult.PPEViolations) > 0 {
											ppeInfo = fmt.Sprintf(" PPE_VIOLATIONS=%d", len(aiResult.PPEViolations))
										}
										log.Printf("[AI] alarm %s: %s threat=%s fp=%.0f%%%s (%.0fms total)",
											snapAlarmID, aiResult.Description,
											aiResult.ThreatLevel, aiResult.FalsePositivePct*100, ppeInfo, aiResult.TotalMs)

										// Broadcast AI enrichment to SOC operators
										aiMsg, _ := json.Marshal(map[string]interface{}{
											"type": "alarm_ai",
											"data": map[string]interface{}{
												"alarm_id":              snapAlarmID,
												"incident_id":           snapIncidentID,
												"ai_description":        aiResult.Description,
												"ai_threat_level":       aiResult.ThreatLevel,
												"ai_recommended_action": aiResult.RecommendedAction,
												"ai_false_positive_pct": aiResult.FalsePositivePct,
												"ai_objects":            aiResult.AIObjects,
												"ai_detections":         aiResult.Detections,
												"ai_ppe_detections":     aiResult.PPEDetections,
												"ai_ppe_violations":     aiResult.PPEViolations,
												"ai_yolo_model":         aiResult.YOLOModel,
												"ai_ppe_model":          aiResult.PPEModel,
												"ai_qwen_model":         aiResult.QwenModel,
												"ai_total_ms":           aiResult.TotalMs,
											},
										})
										hub.Broadcast(aiMsg)

										// Persist AI results to DB for REST polling
										detectionsJSON, _ := json.Marshal(aiResult.Detections)
										ppeViolationsJSON, _ := json.Marshal(aiResult.PPEViolations)
										_ = db.UpdateAlarmAI(context.Background(),
											snapAlarmID, aiResult.Description, aiResult.ThreatLevel,
											aiResult.RecommendedAction, aiResult.FalsePositivePct, detectionsJSON, ppeViolationsJSON)

										// P4-SCHEMA-02: dual-write AI PPE violations to detections.
										// One detections row per violation bbox (DECISION-C).
										if len(aiResult.PPEViolations) > 0 {
											go dualWriteAlarmPPEViolations(
												db, cameraID, siteID,
												aiResult.PPEViolations, aiResult.PPEModel,
												time.Now().UTC(),
											)
										}

										// ── Passive labeling capture ──
										// Enqueue the frame + VLM output for off-SOC annotation.
										// Non-blocking best-effort: a failure here must never affect
										// the alarm pipeline. Operators see nothing; internal staff
										// drain this queue via /admin/labeling.
										go func(alarmID string, camID uuid.UUID, siteID, snapURL, desc, threat, model string, det []byte) {
											defer func() {
												if rec := recover(); rec != nil {
													log.Printf("[LABELING] PANIC in enqueue goroutine for alarm %s: %v", alarmID, rec)
												}
											}()
											if err := db.EnqueueLabelJob(
												context.Background(),
												alarmID, camID, siteID, snapURL, desc, threat, model, det,
											); err != nil {
												log.Printf("[LABELING] enqueue failed for alarm %s: %v", alarmID, err)
											}
										}(snapAlarmID, cameraID, siteID, snapshotURL,
											aiResult.Description, aiResult.ThreatLevel, aiResult.QwenModel, detectionsJSON)

										// ── Video enrichment pass ──
										// Now that the initial snapshot analysis is out to operators,
										// extract a short clip around the event and feed it through
										// Qwen's video path for better motion/context reasoning. Runs
										// after the snapshot tier so operators see something fast
										// (~5s) and get the refined video-based verdict (~15-25s
										// later) as a follow-up update.
										if cfg.StoragePath != "" {
											segAbsPath, _, segStartTime := recording.FindEventClipFull(cfg.StoragePath, snapCameraID, eventTimestamp)
											if segAbsPath != "" {
												// Clip window: 1s before event → 3s after = 4s total.
												clipOffset := eventTimestamp.Sub(segStartTime).Seconds() - 1.0
												if clipOffset < 0 {
													clipOffset = 0
												}
												clipDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", snapCameraID)
												clipFile := filepath.Join(clipDir, snapAlarmID+".clip.mp4")
												_, cerr := recording.ExtractClipFromSegment(cfg.FFmpegPath, segAbsPath, clipFile, clipOffset, 4.0)
												var clipBytes []byte
												if cerr == nil {
													clipBytes, _ = os.ReadFile(clipFile)
												}
												// Always clean up — whether we analyze or bail.
												defer os.Remove(clipFile)

												switch {
												case cerr != nil:
													log.Printf("[AI] video clip extract failed for alarm %s (seg=%s offset=%.1fs): %v", snapAlarmID, filepath.Base(segAbsPath), clipOffset, cerr)
												case len(clipBytes) <= 1000:
													log.Printf("[AI] video clip too small (%d bytes) for alarm %s (seg=%s offset=%.1fs) — likely FFmpeg seeked past end of segment", len(clipBytes), snapAlarmID, filepath.Base(segAbsPath), clipOffset)
												default:
													if vidResult := aiClient.AnalyzeVideo(context.Background(), clipBytes, aiResult.Detections, siteID, evtSiteContext); vidResult == nil || vidResult.Description == "" {
														// No-op — network/decode error already logged by the client.
													} else if vidResult.Degraded {
														// Qwen fell back to mock_analysis (video_fps error, OOM, etc).
														// Keep the good snapshot-tier result — do NOT overwrite DB/broadcast.
														mockTail := vidResult.Description
														if len(mockTail) > 60 {
															mockTail = mockTail[:60] + "…"
														}
														log.Printf("[AI] alarm %s (video): degraded — keeping snapshot-tier result (video mock: %s)",
															snapAlarmID, mockTail)
													} else {
														log.Printf("[AI] alarm %s (video): %s threat=%s fp=%.0f%% (%.0fms)",
															snapAlarmID, vidResult.Description,
															vidResult.ThreatLevel, vidResult.FalsePositivePct*100, vidResult.InferenceMs)

														// Broadcast the video-enriched result as a follow-up
														// so operators' UI replaces the snapshot-tier text.
														vidMsg, _ := json.Marshal(map[string]interface{}{
															"type": "alarm_ai",
															"data": map[string]interface{}{
																"alarm_id":              snapAlarmID,
																"incident_id":           snapIncidentID,
																"ai_description":        vidResult.Description,
																"ai_threat_level":       vidResult.ThreatLevel,
																"ai_recommended_action": vidResult.RecommendedAction,
																"ai_false_positive_pct": vidResult.FalsePositivePct,
																"ai_objects":            vidResult.Objects,
																"ai_qwen_model":         vidResult.Model,
																"ai_mode":               "video",
															},
														})
														hub.Broadcast(vidMsg)

														// Overwrite DB with the refined verdict
														_ = db.UpdateAlarmAI(context.Background(),
															snapAlarmID, vidResult.Description, vidResult.ThreatLevel,
															vidResult.RecommendedAction, vidResult.FalsePositivePct, detectionsJSON, ppeViolationsJSON)
													}
												}
											}
										}
									}
								}
							}()
						}
					}()
				}

				// Async thumbnail capture (bounded by semaphore). Primary path
				// reads the LOCAL recording segment covering event_time — a
				// network-free ffmpeg seek that works even when the live RTSP
				// is slow (cellular trailers) or the HEVC main stream chokes a
				// fresh grab. Falls back to an RTSP SUB-stream grab only on a
				// recording gap. Shared with the runtime camera-add path via
				// recording.CaptureEventThumbnail.
				if cam.RTSPUri != "" {
					eventID := evt.ID
					eventTime := evt.EventTime
					camIDStr := cameraID.String()
					subStreamUri := cam.SubStreamUri
					storagePath := cfg.StoragePath
					ffmpegPath := cfg.FFmpegPath
					// Try to acquire semaphore slot; skip thumbnail if too many in-flight
					select {
					case thumbSem <- struct{}{}:
						go func() {
							defer func() { <-thumbSem }()
							defer func() {
								if rec := recover(); rec != nil {
									log.Printf("[THUMB] PANIC in thumbnail goroutine for event %d: %v", eventID, rec)
								}
							}()
							thumb, via, err := recording.CaptureEventThumbnail(
								ffmpegPath, storagePath, camIDStr, eventTime,
								"", time.Time{}, subStreamUri, 12)
							if err != nil {
								log.Printf("[THUMB] Failed to capture thumbnail for event %d: %v", eventID, err)
								return
							}
							thumbCtx, thumbCancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer thumbCancel()
							if err := db.UpdateEventThumbnail(thumbCtx, eventID, thumb); err != nil {
								log.Printf("[THUMB] Failed to store thumbnail for event %d: %v", eventID, err)
								return
							}
							log.Printf("[THUMB] Captured thumbnail for event %d (camera %s) via %s", eventID, camIDStr, via)

							// Broadcast thumbnail update so frontend can patch it in
							thumbMsg, _ := json.Marshal(map[string]interface{}{
								"type":      "event_thumbnail",
								"event_id":  eventID,
								"camera_id": camIDStr,
								"thumbnail": thumb,
							})
							hub.Broadcast(thumbMsg)
						}()
					default:
						// Skip thumbnail — too many captures in flight
					}
				}
				// If the event contains ONVIF analytics bounding boxes, broadcast them
				if boxes, ok := details["bounding_boxes"]; ok && boxes != nil {
					if rawBoxes, err := json.Marshal(boxes); err == nil {
						// Re-parse as []detection.BoundingBox
						var dboxes []detection.BoundingBox
						if err := json.Unmarshal(rawBoxes, &dboxes); err == nil && len(dboxes) > 0 {
							det.HandleAnalyticsEvent(cameraID, dboxes)
						}
					}
				}
			})
			// Vendor-specific event source selection — extracted into
			// api.StartCameraEventSource so the runtime-add path
			// (HandleCreateCamera) routes through the same logic.
			// Without this shared helper, Milesight cameras added at
			// runtime were silently downgraded to ONVIF PullPoint and
			// produced zero events.
			api.StartCameraEventSource(ctx, &cam, subscriber, subReg, "STARTUP")
		} else {
			log.Printf("[STARTUP] Camera %s: events subscription disabled", cam.Name)
		}

		log.Printf("[STARTUP] Camera %s: recording + streaming active", cam.Name)
	}

	// Start MediaMTX with all registered streams
	for _, cam := range cameras {
		if cam.RTSPUri != "" {
			mtxServer.AddStream(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri)
		}
	}
	if err := mtxServer.Start(ctx); err != nil {
		log.Printf("[STARTUP] Failed to start MediaMTX: %v", err)
	}

	if len(cameras) == 0 {
		log.Println("[STARTUP] No cameras configured. Use the API or frontend to add cameras.")
	}
}

// ── P4-SCHEMA-02 dual-write helpers ─────────────────────────────────────────

// dualWriteSecurityEvent inserts one detection row of domain "security" after
// a legacy events INSERT succeeds.  orgID is not available in the AlertEmitter
// closure (the emitter only has cameraID + siteID from a goroutine that ran
// before the ONVIF subscriber's site resolution); we look it up from the
// camera row lazily.  Non-fatal: logs + increments counter on failure.
func dualWriteSecurityEvent(
	db *database.DB,
	camID uuid.UUID,
	siteID string,
	eventType string,
	detectedAt time.Time,
	vcaRuleID *uuid.UUID,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Resolve the organisation for this camera.
	var orgID string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(s.organization_id, '') FROM cameras c LEFT JOIN sites s ON s.id = c.site_id WHERE c.id = $1`,
		camID,
	).Scan(&orgID)
	if err != nil || orgID == "" {
		log.Printf("[DUALWRITE:events] cannot resolve org for camera %s: %v", camID, err)
		dualwrite.DualWriteFailuresTotal.WithLabelValues("events").Inc()
		return
	}

	mvID, err := dualwrite.LookupOrCreateModelVersion(
		ctx, db,
		orgID, "onvif-event", "v1", "", "security",
	)
	if err != nil {
		log.Printf("[DUALWRITE:events] LookupOrCreateModelVersion: %v", err)
		dualwrite.DualWriteFailuresTotal.WithLabelValues("events").Inc()
		return
	}
	run := dualwrite.NewRunHandle(db, orgID, mvID)
	var sitePtr *string
	if siteID != "" {
		sitePtr = &siteID
	}
	dualwrite.Write(ctx, db, "events", orgID, camID, run, dualwrite.MappedDetection{
		DetectedAt:      detectedAt,
		SiteID:          sitePtr,
		DetectionClass:  dualwrite.NormaliseEventTypeToSecurityClass(eventType),
		DetectionDomain: "security",
		Confidence:      0.8, // ONVIF analytic events are high-confidence by design
		BoundingBox:     nil,
		ZoneID:          nil,
		VCARuleID:       vcaRuleID,
	})
}

// dualWriteAlarmPPEViolations inserts one detections row per PPE violation
// surfaced by the alarm AI pipeline (Qwen PPE pass on the alarm snapshot).
// Each violation in the aiResult.PPEViolations array maps to one detection row
// (DECISION-C: one bbox per row).  Non-fatal on any failure.
func dualWriteAlarmPPEViolations(
	db *database.DB,
	camID uuid.UUID,
	siteID string,
	violations []ai.Detection,
	ppeModel string,
	detectedAt time.Time,
) {
	if len(violations) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var orgID string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(s.organization_id, '') FROM cameras c LEFT JOIN sites s ON s.id = c.site_id WHERE c.id = $1`,
		camID,
	).Scan(&orgID)
	if err != nil || orgID == "" {
		log.Printf("[DUALWRITE:alarms] cannot resolve org for camera %s: %v", camID, err)
		dualwrite.DualWriteFailuresTotal.WithLabelValues("alarms").Inc()
		return
	}

	versionTag := ppeModel
	if versionTag == "" {
		versionTag = "unknown"
	}
	mvID, err := dualwrite.LookupOrCreateModelVersion(
		ctx, db,
		orgID, "yolo-ppe", versionTag, "", "ppe",
	)
	if err != nil {
		log.Printf("[DUALWRITE:alarms] LookupOrCreateModelVersion: %v", err)
		dualwrite.DualWriteFailuresTotal.WithLabelValues("alarms").Inc()
		return
	}

	// One analysis_run shared across all violations from this alarm frame.
	run := dualwrite.NewRunHandle(db, orgID, mvID)

	var sitePtr *string
	if siteID != "" {
		sitePtr = &siteID
	}

	for _, v := range violations {
		bboxJSON := dualwrite.BBoxFromX1Y1X2Y2(
			v.BBoxNorm.X1, v.BBoxNorm.Y1, v.BBoxNorm.X2, v.BBoxNorm.Y2,
		)
		dualwrite.Write(ctx, db, "alarms", orgID, camID, run, dualwrite.MappedDetection{
			DetectedAt:      detectedAt,
			SiteID:          sitePtr,
			DetectionClass:  dualwrite.NormalisePPEClass(v.Class),
			DetectionDomain: "ppe",
			Confidence:      float32(v.Confidence),
			BoundingBox:     bboxJSON,
			ZoneID:          nil,
			VCARuleID:       nil,
		})
	}
}
