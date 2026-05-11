package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"ironsight/internal/ai"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/detection"
	"ironsight/internal/notify"
	"ironsight/internal/onvif"
	"ironsight/internal/recording"
	"ironsight/internal/streaming"
)

// NewRouter creates the HTTP router with all API routes
func NewRouter(cfg *config.Config, db *database.DB, hub *Hub, recEngine *recording.Engine, hlsServer *streaming.HLSServer, mtxServer *streaming.MediaMTXServer, det *detection.Manager, player *onvif.BackchannelPlayer, subReg *SubscriberRegistry, notifier *notify.Dispatcher, aiClient *ai.Client) http.Handler {
	r := chi.NewRouter()

	// P1-A-03: spin up the media-serve audit ring buffer. Every
	// /media/v1/<token> hit enqueues one row; the auditor batches 100
	// rows or 5 seconds (whichever is first) into audit_log. The
	// goroutine outlives this constructor — cmd/server doesn't yet
	// have an api-package shutdown hook, so we accept the leak on
	// process exit (the OS reclaims the goroutine when main() returns).
	mediaAuditor := newMediaAuditor(db)
	mediaAuditor.Start()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	// Origins come from cfg.AllowedOrigins which is populated by the
	// ALLOWED_ORIGINS env var (comma-separated). Default at config-load
	// time is the dev-mode localhost pair; production deployments must
	// override with the actual frontend origin(s).
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public auth routes (no JWT required). Rate-limited to 10 attempts
	// per minute per client IP — UL 827B expects a brute-force throttle
	// on authentication endpoints. The limiter is a separate layer of
	// defense from account lockout: the former caps *attempts*, the
	// latter caps *consequences*. Both are needed.
	r.With(RateLimitLogin(10)).Post("/auth/login", HandleLogin(db, cfg))

	// Public health check — liveness probe for Docker HEALTHCHECK and any
	// external uptime monitor. Deliberately unauthenticated and returns a
	// fixed payload; the authenticated /api/system/health endpoint is the
	// richer per-subsystem status dashboard.
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	})

	// Public evidence share endpoint. Unauthenticated by design — the
	// token IS the authorization. Every GET is logged to the append-only
	// evidence_share_opens table for chain-of-custody. A non-existent,
	// revoked, or expired token returns 404 with no detail (don't leak
	// share state to probes).
	r.Get("/share/{token}", HandlePublicEvidenceShare(db))

	// Sense / push-only camera webhook. Public; the long random token
	// in the URL is the authentication. Used by Milesight SC4xx PIR
	// cameras whose Alarm Server config POSTs JSON+snapshot on every
	// triggered event.
	r.Post("/api/integrations/milesight/sense/{token}", HandleSenseWebhook(cfg, db, hub))

	// Public status endpoint. Unauthenticated — trust signals matter
	// most when the customer is worried, which is exactly when they
	// don't want to log in. Returns aggregates only (camera counts,
	// SOC active, last disposition timestamp); no per-customer data.
	// Lives under /api so the Next.js frontend proxy already picks it
	// up; the customer-visible page is at /status (Next route).
	r.Get("/api/status", HandlePublicStatus(db))

	// Authenticated auth routes
	r.With(RequireAuth(cfg, db)).Get("/auth/me", HandleGetMe(db))
	r.With(RequireAuth(cfg, db)).Post("/auth/logout", HandleLogout(db))

	// MFA management routes. All require an authenticated session;
	// enrollment uses the session token to bind the new secret to the
	// caller, and disable requires an active MFA code as proof.
	r.With(RequireAuth(cfg, db)).Post("/api/auth/mfa/enroll", HandleMFAEnroll(db, cfg))
	r.With(RequireAuth(cfg, db)).Post("/api/auth/mfa/confirm", HandleMFAConfirm(db))
	r.With(RequireAuth(cfg, db)).Post("/api/auth/mfa/disable", HandleMFADisable(db))

	// API routes (JWT protected)
	r.Route("/api", func(r chi.Router) {
		r.Use(RequireAuth(cfg, db))
		r.Use(AuditMiddleware(db))
		// Camera CRUD
		r.Route("/cameras", func(r chi.Router) {
			r.Get("/", HandleListCameras(db))
			r.Post("/", HandleCreateCamera(db, recEngine, hlsServer, mtxServer, hub, subReg))
			r.Get("/{id}", HandleGetCamera(db))
			r.Patch("/{id}", HandleUpdateCamera(db))
			r.Delete("/{id}", HandleDeleteCamera(db, recEngine, hlsServer, mtxServer, subReg))

			// Recordings per camera
			r.Get("/{id}/recordings", HandleGetRecordings(db))

			// PTZ
			r.Post("/{id}/ptz/move", HandlePTZMove(db))
			r.Post("/{id}/ptz/stop", HandlePTZStop(db))
			r.Post("/{id}/ptz/prewarm", HandlePTZPrewarm(db))

			// AI Detection
			r.Get("/{id}/detect", HandleDetectLatest(db, det))
			r.Get("/{id}/detect/stream", HandleDetectionStream(db, det))

			// VCA Zone Editor
			r.Get("/{id}/vca/rules", HandleListVCARules(db))
			r.Post("/{id}/vca/rules", HandleCreateVCARule(db))
			r.Put("/{id}/vca/rules/{ruleId}", HandleUpdateVCARule(db))
			r.Delete("/{id}/vca/rules/{ruleId}", HandleDeleteVCARule(db))
			r.Post("/{id}/vca/sync", HandleSyncVCARules(db))

			// VCA bidirectional: pull the camera's current rule set,
			// optionally replace the DB copy. Closes the one-way push
			// loop so edits made via the camera's native web UI don't
			// stay invisible.
			r.Get("/{id}/vca/pull", HandleVCAPull(db))
			r.Post("/{id}/vca/pull", HandleVCAPull(db))

			// Milesight vendor-config pass-through. Each {panel} is an
			// allowlisted CGI action pair (see internal/api/milesight_config.go).
			// GET is gated by camera access, PUT by admin role.
			r.Get("/{id}/milesight/config/{panel}", HandleMilesightGet(db))
			r.Put("/{id}/milesight/config/{panel}", HandleMilesightSet(db))
			// Action endpoints — side-effectful camera commands that
			// don't fit the get/set config shape.
			r.Post("/{id}/milesight/reboot", HandleMilesightReboot(db))
			r.Post("/{id}/milesight/ptz/preset/goto", HandlePTZPresetGoto(db))
		})

		// ONVIF Discovery
		r.Post("/discover", HandleDiscover())
		r.Post("/discover/preview", HandleDiscoverPreview())

		// Events & Timeline
		r.Get("/events", HandleQueryEvents(cfg, db))
		r.Get("/timeline", HandleGetTimeline(db))
		r.Get("/timeline/coverage", HandleGetCoverage(db))

		// Active deterrence — fire camera strobe / siren / alarm-out
		// outputs from an operator click. RBAC-gated (blocks customer /
		// viewer roles) and writes a row to deterrence_audits on every
		// call, success or failure.
		r.Post("/cameras/{id}/deterrence", HandleDeterrence(db))

		// SD-card status probe — reports whether the camera has onboard
		// storage, how much of it is populated, and the replay handles.
		// Used for the recording-health dashboard and as a precondition
		// for the Profile G fallback playback path.
		r.Get("/cameras/{id}/sd/status", HandleSDStatus(db))

			// ONVIF-driven reboot (admin / supervisor only). Useful when
			// the camera gets into a bad state — stuck event-subscription
			// pool, wedged RTSP, stale driver state. Camera goes offline
			// for ~30-90s while it restarts.
			r.Post("/cameras/{id}/reboot", HandleRebootCamera(db))

		// Unified historical search: events (filtered by RBAC) with playback
		// URLs resolved in one round trip. Frontend uses this to render a
		// clickable list — each row carries the segment + seek offset.
		r.Get("/search/events", HandleSearchEvents(cfg, db))

		// Semantic / keyword search over VLM-generated segment descriptions.
		// Populated by the background indexer — any minute of recording is
		// searchable by natural-language content ("red jacket", "ladder",
		// "delivery truck"), not just by event-rule name.
		r.Get("/search/semantic", HandleSemanticSearch(cfg, db))

		// Unified safety + security frame search for the /search page.
		// Returns shaped SearchResult[] matching the frontend type, unioning
		// VLM-described segments with SOC alarms (so PPE violation filters
		// and natural-language queries hit the same result list).
		r.Post("/search/frames", HandleSearchFrames(cfg, db))

		// Recording-health snapshot for operators + customers. Per-camera
		// stats over the last 24h + traffic-light status so silent recording
		// failures surface in the UI instead of in server logs.
		r.Get("/recording/health", HandleRecordingHealth(cfg, db))

		// Evidence export: bundles an event into a .zip with clip.mp4,
		// snapshot.jpg, event.json, README.txt for police / insurance reports.
		// RBAC-gated; audited.
		r.Get("/events/{id}/export", HandleEvidenceExport(db, cfg))

		// Exports
		r.Post("/exports", HandleCreateExport(db))
		r.Get("/exports", HandleListExports(db))

		// (The liveness /api/health is registered above as a public route.)

		// Settings
		r.Get("/settings", HandleGetSettings(db))
		r.Put("/settings", HandleUpdateSettings(db, cfg))

		// User management
		r.Get("/users", HandleListUsers(db))
		r.Post("/users", HandleCreateUser(db))
		r.Delete("/users/{id}", HandleDeleteUser(db))
		r.Patch("/users/{id}", HandleUpdateUserProfile(db))
		r.Patch("/users/{id}/password", HandleUpdateUserPassword(db))
		r.Patch("/users/{id}/role", HandleUpdateUserRole(db))
		r.Post("/users/{id}/mfa/reset", HandleAdminMFAReset(db))

		// Storage status (available to all authenticated users)
		r.Get("/storage/status", func(w http.ResponseWriter, r *http.Request) {
			configured := cfg.StoragePath != ""
			writeJSON(w, map[string]interface{}{
				"configured":   configured,
				"storage_path": cfg.StoragePath,
				"hls_path":     cfg.HLSPath,
			})
		})

		// Storage management (admin only below)
		r.Get("/storage/drives", HandleListDrives())
		r.Get("/storage/browse", HandleBrowsePath())
		r.Get("/storage/disk-usage", HandleGetDiskUsage())
		r.Get("/storage/locations", HandleListStorageLocations(db))
		r.Post("/storage/locations", HandleCreateStorageLocation(db))
		r.Put("/storage/locations/{id}", HandleUpdateStorageLocation(db))
		r.Delete("/storage/locations/{id}", HandleDeleteStorageLocation(db))

		// System health
		r.Get("/system/health", HandleSystemHealth(cfg, db, recEngine, mtxServer))
		r.Get("/system/services", HandleServicesHealth(cfg, db, aiClient))
		r.Get("/system/services/timeseries", HandleAIMetricsTimeseries(db))
		r.Get("/system/services/usage", HandleAIUsageBySite(db))

		// Audit log
		r.Get("/audit", HandleQueryAuditLog(db))

		// Operator response-time / SLA report. UL 827B reviewers expect
		// quantitative answers to "how fast does your SOC actually
		// respond?" — this endpoint is the canonical source.
		r.Get("/reports/sla", HandleSLAReport(db))

		// Customer notification preferences — both /me/notifications
		// and the singular upsert. Available to every authenticated
		// user; the user_id is always pulled from JWT, so a customer
		// can only see/edit their own subscriptions.
		r.Get("/me/notifications", HandleListMyNotificationSubs(db))
		r.Put("/me/notifications", HandleUpsertMyNotificationSub(db))

		// Customer ↔ SOC support tickets. Customer-side users see
		// only their own org's threads; supervisor + admin see
		// everything. Email fires on every new ticket and every
		// reply so neither side has to babysit the UI.
		// Mounted under the existing /api group, so the actual
		// paths are /api/support/tickets/* — short and clean.
		r.Post("/support/tickets",                  HandleCreateSupportTicket(db, notifier))
		r.Get("/support/tickets",                   HandleListSupportTickets(db))
		r.Get("/support/tickets/{id}",              HandleGetSupportTicket(db))
		r.Post("/support/tickets/{id}/messages",    HandleSupportTicketReply(db, notifier))
		r.Patch("/support/tickets/{id}",            HandleUpdateSupportTicket(db))

		// ════════════════════════════════════════
		// Ironsight Platform Routes (/api/v1/*)
		// ════════════════════════════════════════
		r.Route("/v1", func(r chi.Router) {
			// Organizations
			r.Get("/companies", HandleListOrganizations(db))
			r.Post("/companies", HandleCreateOrganization(db))
			r.Put("/companies/{id}", HandleUpdateOrganization(db))
			r.Delete("/companies/{id}", HandleDeleteOrganization(db))
			r.Get("/companies/{companyId}/users", HandleListCompanyUsers(db))
			r.Post("/companies/{companyId}/users", HandleCreateCompanyUser(db))
			r.Delete("/companies/{companyId}/users/{userId}", HandleDeleteCompanyUser(db))

			// Platform camera registry (all cameras with site info — used by admin device modal)
			r.Get("/cameras", HandleListAllPlatformCameras(db))

			// Sites
			r.Get("/sites", HandleListSites(db))
			r.Get("/sites/{id}", HandleGetSite(db))
			r.Post("/sites", HandleCreateSiteP(db))
			r.Put("/sites/{id}", HandleUpdateSite(db))
			// Partial update of a site's recording config — PATCH (not PUT)
			// because the body sets only the recording-related fields and
			// leaves the rest of the site row untouched. Frontend must use
			// PATCH to match.
			r.Patch("/sites/{id}/recording", HandleUpdateSiteRecording(db))
			r.Delete("/sites/{id}", HandleDeleteSiteP(db))
			// Customer-maintained on-site contact list. Distinct
			// from site_sops (operator call tree). Read scoped to
			// site-visible roles; edit restricted to site_manager,
			// soc_supervisor, admin.
			r.Get("/sites/{id}/contacts", HandleListSiteContacts(db))
			r.Put("/sites/{id}/contacts", HandleUpdateSiteContacts(db))
			r.Get("/sites/{siteId}/cameras", HandleGetSiteCameras(db))
			r.Get("/sites/{siteId}/sops", HandleListSiteSOPs(db))
			r.Post("/sites/{siteId}/sops", HandleCreateSiteSOP(db))
			r.Put("/sops/{id}", HandleUpdateSiteSOP(db))
			r.Delete("/sops/{id}", HandleDeleteSiteSOP(db))
			r.Get("/sites/locks", HandleSiteLocks(db))

			// Camera assignments
			r.Post("/sites/{siteId}/camera-assignments", HandleAssignCamera(db))
			r.Delete("/sites/{siteId}/camera-assignments/{cameraId}", HandleUnassignCamera(db))

			// Speaker registry + site assignments
			r.Get("/speakers", HandleListAllPlatformSpeakers(db))
			r.Post("/sites/{siteId}/speaker-assignments", HandleAssignSpeaker(db))
			r.Delete("/sites/{siteId}/speaker-assignments/{speakerId}", HandleUnassignSpeaker(db))

			// Device assignment history (admin only)
			r.Get("/device-history", HandleGetDeviceHistory(db))

			// Operators
			r.Get("/operators", HandleListOperators(db))
			r.Post("/operators", HandleCreateOperator(db))
			r.Get("/operators/current", HandleGetCurrentOperator(db))
			r.Get("/operators/{operatorId}/handoffs", HandleOperatorHandoffs(db))

			// Security Events & Incidents
			r.Post("/events", HandleCreateSecurityEvent(db, notifier))
			r.Get("/events", HandleListSecurityEvents(db))
			// Dual-operator verification (UL 827B four-eyes rule).
			// Restricted to supervisor/admin roles and rejects
			// self-verification by the disposing operator.
			r.Post("/events/{id}/verify", HandleVerifySecurityEvent(db))

			// Evidence share lifecycle. Read path is the public
			// /share/{token} endpoint registered at the top level;
			// these are the authenticated supervisor-only management
			// endpoints. Audit middleware tags the actions as
			// create_evidence_share / revoke_evidence_share.
			r.Post("/incidents/{id}/share", HandleCreateEvidenceShare(db))
			r.Get("/incidents/{id}/shares", HandleListEvidenceShares(db))
			r.Delete("/shares/{token}", HandleRevokeEvidenceShare(db))
			r.Get("/incidents", HandleListIncidents(db))
			r.Get("/incidents/{id}", HandleGetIncident(db))

			r.Get("/portal/summary", HandlePortalSummary(db))

			// Active alarm escalation
			r.Post("/alarms/{alarmId}/escalate", HandleEscalateAlarm(db, hub))
			r.Post("/alarms/{alarmId}/ai-feedback", func(w http.ResponseWriter, req *http.Request) {
				alarmID := chi.URLParam(req, "alarmId")
				var body struct {
					Agreed bool `json:"agreed"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					http.Error(w, "Invalid JSON", 400)
					return
				}
				if err := db.SetAlarmAIFeedback(req.Context(), alarmID, body.Agreed); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				writeJSON(w, map[string]interface{}{"ok": true, "alarm_id": alarmID, "agreed": body.Agreed})
			})

			// Shift handoffs
			r.Get("/handoffs", HandleListHandoffs(db))
			r.Post("/handoffs", HandleCreateHandoff(db))

			// Dispatch
			r.Get("/dispatch/queue", HandleDispatchQueue(db))

			// Features
			r.Get("/features", HandleFeatureFlags(db))

			// Alerts — list active (unacknowledged) alarms from the SOC dispatch queue
			r.Get("/alerts", func(w http.ResponseWriter, req *http.Request) {
				alarms, err := db.ListActiveAlarms(req.Context())
				if err != nil {
					writeJSON(w, []interface{}{})
					return
				}
				if alarms == nil {
					alarms = []database.ActiveAlarm{}
				}
				writeJSON(w, alarms)
			})

			// Incidents — grouped alarms from the same site
			r.Get("/incidents/active", func(w http.ResponseWriter, req *http.Request) {
				incidents, err := db.ListActiveIncidents(req.Context())
				if err != nil {
					writeJSON(w, []interface{}{})
					return
				}
				if incidents == nil {
					incidents = []database.Incident{}
				}
				writeJSON(w, incidents)
			})
			r.Post("/incidents/{incidentId}/acknowledge", func(w http.ResponseWriter, req *http.Request) {
				incidentID := chi.URLParam(req, "incidentId")
				if err := db.AcknowledgeIncident(req.Context(), incidentID); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				writeJSON(w, map[string]interface{}{"ok": true, "incident_id": incidentID})
			})
		})

		// ── ML Labeling (internal staff / admin only) ──────────────────
		// Off-SOC active-learning queue. Passively populated when Qwen
		// analyses an alarm frame; drained by internal annotators at
		// /admin/labeling. SOC operators never see or interact with this.
		r.Route("/admin/labeling", func(r chi.Router) {
			r.Get("/stats",           HandleLabelingStats(db))
			r.Get("/jobs",            HandleListLabelJobs(db))
			r.Post("/jobs/next",      HandleClaimNextLabelJob(db))
			r.Post("/jobs/{id}/claim", HandleClaimLabelJob(db))
			r.Post("/jobs/{id}/label", HandleSubmitLabel(db))
			r.Get("/export",          HandleExportLabeledDataset(db))
		})

		// Bookmarks / Incident markers
		r.Post("/bookmarks", HandleCreateBookmark(db))
		r.Get("/bookmarks", HandleListBookmarks(db))
		r.Delete("/bookmarks/{id}", HandleDeleteBookmark(db))

		// Speakers
		r.Get("/speakers", HandleListSpeakers(db))
		r.Post("/speakers", HandleCreateSpeaker(db))
		r.Delete("/speakers/{id}", HandleDeleteSpeaker(db))
		r.Post("/speakers/{id}/play/{messageId}", HandlePlayMessage(cfg, db, player))
		r.Post("/speakers/stop", HandleStopPlayback(player))
		r.Get("/speakers/status", HandlePlaybackStatus(player))

		// Audio messages
		r.Get("/audio-messages", HandleListAudioMessages(db))
		r.Post("/audio-messages", HandleUploadAudioMessage(cfg, db))
		r.Get("/audio-messages/{id}", HandleGetAudioMessage(db))
		r.Delete("/audio-messages/{id}", HandleDeleteAudioMessage(cfg, db))
		r.Get("/audio-messages/file/{fileName}", HandleServeAudioFile(cfg))
		r.Get("/speaker-info", HandleBulkInfo(db))

		// Playback segment lookup (returns JSON with MP4 segment URLs) and
		// HLS VOD playlist. Inside the auth group — handlers use CanAccessCamera
		// to restrict playback to the caller's authorized cameras.
		r.Get("/playback/{id}", HandlePlayback(cfg, db))
		r.Get("/playback/{id}/playlist.m3u8", HandlePlaybackHLS(cfg, db))

		// P1-A-03 mint endpoint. JWT-authenticated; caller asks for a
		// short-lived signed URL bound to (camera_id, kind, path) and
		// gets back /media/v1/<token>. Tenant scope is enforced here
		// AND re-enforced in HandleMediaServe.
		r.Post("/media/mint", HandleMediaMint(cfg, db))
	})

	// Camera snapshot — public so <img> tags in the SOC feed can load without a Bearer token.
	// The camera UUID is not guessable; all other camera data remains auth-protected.
	r.Get("/api/cameras/{id}/vca/snapshot", HandleVCASnapshot(db))

	// WebSocket endpoints for live events and alert stream
	r.Get("/ws", hub.HandleWebSocket)
	r.Get("/ws/alerts", hub.HandleWebSocket) // alert-filtered stream (client filters type:"alert")

	// Playback endpoints registered inside the /api auth group above.

	// P1-A-03: authenticated media serving. Replaces the three bare
	// http.FileServer registrations that previously served /hls/*,
	// /recordings/*, and /snapshots/* with no authentication and no
	// tenant scoping. The token in the URL is the authorization — the
	// handler validates it, re-checks CanAccessCamera against the
	// current DB state, then streams the file. See docs/media-auth.md.
	r.Get("/media/v1/{token}", HandleMediaServe(cfg, db, mediaAuditor))

	// WebRTC WHEP proxy to MediaMTX
	if mtxServer != nil {
		r.Handle("/webrtc/*", http.StripPrefix("/webrtc", mtxServer.WHEPHandler()))
	}

	// Static file serving for exports. NOTE: /exports is operator-only
	// (admin / supervisor / soc_operator), bundled evidence ZIPs are
	// already gated at the /api/events/{id}/export create path so the
	// resulting download URLs are obscure-enough for the moment. A
	// separate hardening task (P1-A-03 follow-up) can fold this into
	// the same /media/v1/ scheme if Caleb wants belt-and-braces.
	r.Handle("/exports/*", http.StripPrefix("/exports/", http.FileServer(http.Dir(cfg.ExportPath))))

	return r
}

// HandleDiscover scans for ONVIF cameras
func HandleDiscover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		devices, err := onvif.Discover(r.Context(), 5*time.Second)
		if err != nil {
			http.Error(w, "Discovery failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if devices == nil {
			devices = []onvif.DiscoveredDevice{}
		}
		writeJSON(w, devices)
	}
}
