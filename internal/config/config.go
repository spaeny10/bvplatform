package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Config holds all application configuration
type Config struct {
	// Server
	ServerPort string

	// Logging — slog level filter. Valid: debug, info (default), warn, error.
	// P1-C-01: env-driven knob so operators can crank verbosity in an
	// incident without redeploying. Unknown values fall through to info.
	LogLevel string

	// Sentry / GlitchTip error reporting (P1-C-02). SENTRY_DSN is the
	// project DSN from the GlitchTip UI. Empty DSN disables the SDK
	// entirely — the logging handler degrades to JSON-only. This is
	// the production default until the GlitchTip LXC is stood up.
	// SENTRY_ENVIRONMENT tags every captured event ("production",
	// "staging", etc.) so events can be filtered by deploy tier.
	// Empty = no environment tag (acceptable for dev).
	SentryDSN         string
	SentryEnvironment string

	// Database
	DatabaseURL string

	// Storage
	StoragePath   string
	HLSPath       string
	ExportPath    string
	ThumbnailPath string

	// Recording
	SegmentDuration int // seconds

	// FFmpeg
	FFmpegPath string

	// AI Detection
	DetectionServiceURL string
	DetectionIntervalMs int

	// Auth
	JWTSecret        string // sign/verify JWTs; set JWT_SECRET in env
	DefaultAdminPass string // auto-created admin password on first run

	// CameraCredentialsKey is the 32-byte AES-256 key used to encrypt
	// camera passwords at rest (P1-A-05). Operators supply it via
	// CAMERA_CREDENTIALS_KEY env (64-char hex or 44-char base64).
	// Required for cmd/server; cmd/worker/migrate/seed tolerate empty.
	CameraCredentialsKey string

	// SSO via reverse-proxy header trust. When SSOTrustHeader == "email", the
	// API trusts an "X-Forwarded-Email" header injected by an upstream proxy
	// (oauth2-proxy + NPM in the BigView deployment) and skips JWT entirely.
	// Empty (default) keeps the original JWT-only flow. SSOAdminEmails is a
	// comma-separated allowlist; addresses on it become admin on first sight,
	// so a freshly-deployed system can be administered without a DB seed step.
	// SSODefaultRole is what every other SSO-provisioned user gets ("viewer"
	// is the least-privilege role, recommended).
	SSOTrustHeader string
	SSOAdminEmails []string
	SSODefaultRole string

	// CORS allowlist for the API. Comma-separated origins in the env;
	// the helper splits on commas and trims whitespace. UL 827B
	// reviewers will look for this to be locked down to the
	// production frontend's exact origin (e.g. https://soc.example.com)
	// rather than the dev-time localhost defaults.
	AllowedOrigins []string

	// CookieSecure controls whether the ironsight_session and ironsight_csrf
	// cookies are emitted with the Secure attribute (HTTPS-only). Default
	// true — fred runs behind NPM TLS and cookies without Secure are a
	// footgun on any public deployment. Set COOKIE_SECURE=false only for
	// an isolated local dev session where the backend is served over plain
	// HTTP. Never ship with Secure=false on a reachable host.
	CookieSecure bool

	// SMTPHost / SMTPPort / SMTPUser / SMTPPass / SMTPFrom configure the
	// outbound notification mailer. When SMTPHost is empty we fall back
	// to a stub mailer that logs notifications to stderr instead of
	// sending — useful for dev / CI environments without an SMTP relay.
	// PublicURL is the customer-visible base URL the mailer embeds in
	// links ("View incident: <url>"); production must override the
	// localhost default so emailed links land on the right hostname.
	SMTPHost  string
	SMTPPort  string
	SMTPUser  string
	SMTPPass  string
	SMTPFrom  string
	PublicURL string

	// Twilio credentials for SMS notifications. AccountSid + AuthToken
	// from the Twilio console; From is the E.164 number you provisioned
	// (e.g. "+15551234567"). When any of these are empty SMS sending
	// falls back to stub mode (logs to stderr) so dev environments
	// without Twilio credentials still produce visible output through
	// the dispatcher.
	TwilioAccountSid string
	TwilioAuthToken  string
	TwilioFrom       string

	// EvidenceSigningKey is a hex-encoded secret used to HMAC-sign
	// evidence export bundles so a downstream consumer (insurer, court,
	// PSAP) can detect tampering. The signed manifest sits alongside the
	// ZIP contents as SIGNATURE.txt; verification requires the same key.
	// A missing or weak key disables signing — exports still succeed but
	// without a SIGNATURE.txt file. For UL 827B / TMA-AVS-01 readiness
	// this should be at least 32 bytes (64 hex chars). Generate with:
	//   openssl rand -hex 32
	EvidenceSigningKey string

	// EvidenceED25519PrivateKey is a hex-encoded ed25519 private key used to
	// sign chain-of-custody manifests (P3-INFRA-03).  Unlike the HMAC
	// EvidenceSigningKey, ed25519 signatures are asymmetric: anyone with the
	// corresponding PUBLIC key can verify a manifest independently — no shared
	// secret required.  This is the "courtroom will verify findings" story.
	//
	// Format: 128 hex chars (64-byte full ed25519 private key), or 64 hex chars
	// (32-byte seed from which the full key is derived).
	//
	// Generate a keypair:
	//   openssl genpkey -algorithm ed25519 -out ed25519.pem
	//   openssl pkey -in ed25519.pem -outform DER | tail -c 32 | xxd -p -c 64
	//   # Or simply: openssl rand -hex 32   (generates seed; key derived at boot)
	//
	// When empty: manifest signing is skipped, manifests are still inserted
	// (with empty signature/key_id) so the table exists but is not yet
	// cryptographically anchored.  UL 827B deployments MUST set this.
	//
	// Env var: EVIDENCE_ED25519_PRIVATE_KEY
	EvidenceED25519PrivateKey string

	// EvidenceSigningKeyring is a JSON map of key_id → hex-encoded ed25519
	// public key, used for key rotation.  After rotating to a new private key
	// the OLD public key stays in the keyring so manifests signed with the
	// old key remain verifiable.
	//
	// Format: JSON object, e.g.:
	//   {"abcd1234ef567890":"<64-hex-public-key>","<new-id>":"<new-hex-pub>"}
	//
	// The active key's entry is added automatically at boot from
	// EvidenceED25519PrivateKey; callers do not need to duplicate it here
	// (though it's harmless to include it).
	//
	// Env var: EVIDENCE_SIGNING_KEYRING
	EvidenceSigningKeyring string

	// Branding — user-visible product name. Used in generated evidence
	// bundles, log headers, and any other text the backend produces that
	// reaches a customer. The frontend gets the same value from
	// frontend/src/lib/branding.ts; they're intentionally duplicated rather
	// than plumbed through an API, because backend strings are often
	// generated without a request context (e.g., retention cleanup logs).
	ProductName string

	// Redis — pub/sub bridge for the WebSocket hub. When running more than
	// one API replica, every replica's clients need to see events emitted
	// on any replica; a shared Redis channel delivers that fanout. Empty
	// string = in-memory only (single-replica deployments don't need Redis
	// and shouldn't have to run it).
	RedisURL       string // full Redis DSN, e.g. redis://redis:6379/0
	RedisWSChannel string // pub/sub channel name; all replicas in a
	// deployment must share this value

	// RunWorkers controls whether the api binary also runs the batch
	// workloads (retention, VLM indexer, export worker). Default: true.
	// Set to false in multi-container deployments where a sibling
	// `worker` container owns these jobs — otherwise the api and worker
	// would race on the same tables (retention purges running twice,
	// indexer double-processing segments, etc.).
	//
	// The worker binary itself unconditionally runs these jobs; this
	// flag only gates the in-process copy inside the api binary.
	RunWorkers bool

	// MediaMTX — either embedded (we spawn the binary) or external (another
	// container). MediaMTXEmbedded=false expects MediaMTX to be reachable at
	// the URLs below via service DNS in a docker-compose / K8s deployment.
	MediaMTXEmbedded   bool   // true = spawn bin/mediamtx as a child process (dev + single-container)
	MediaMTXWebRTCAddr string // host[:port] that hosts the WHEP endpoint the Go API reverse-proxies to
	MediaMTXRTSPAddr   string // host[:port] the local RTSP relay listens on (recording engine pulls from this)
	MediaMTXAPIAddr    string // host[:port] of MediaMTX's HTTP control API — runtime path adds/removes go here
	// so we stop round-tripping YAML config through a shared volume.
	// MediaMTXHLSAddr is the host:port of mediamtx's built-in HLS server.
	// The Go API proxies /api/live/* requests to this address.
	// Default: ironsight-test-mediamtx:8888 for compose; 127.0.0.1:8888 for embedded.
	// Env var: MEDIAMTX_HLS_ADDR
	MediaMTXHLSAddr string

	// WebRTCAdditionalHosts is a list of hostnames or IPs MediaMTX advertises
	// in WebRTC ICE candidates in addition to its own bound interface(s). In
	// docker-compose deployments MediaMTX otherwise only sees its bridge IP
	// (e.g. 172.19.0.5), which the browser cannot reach — every WHEP session
	// then times out with "deadline exceeded while waiting connection". Set
	// WEBRTC_ADDITIONAL_HOSTS to fred's LAN IP and/or public hostname.
	WebRTCAdditionalHosts []string

	// AI pipeline endpoints — the YOLO (detection) and Qwen (reasoning) HTTP
	// sidecars that the api and the worker call for event analysis and
	// background VLM indexing. Defaults point at the in-host loopback ports
	// our docker-compose layout publishes; production overrides set the
	// sibling-container hostnames. AIEnabled is the kill-switch — set
	// AI_ENABLED=false to short-circuit every AI call back to a stub.
	AIYOLOURL string
	AIQwenURL string
	AIEnabled bool

	// GortCameras opts a subset of cameras out of the FFmpeg recorder and into
	// the pure-Go (gortsplib) recorder. Comma-separated list of full UUIDs or
	// 8-char prefixes; entries are parsed once at startup. Empty (default)
	// keeps every camera on FFmpeg. The recorder engine consults this at
	// camera-start time, and /api/recording/health reports per-camera engine
	// choice using the same set.
	GortCameras []string

	// Indexer — VLM segment-description background worker. IndexerConcurrency
	// is the number of goroutines (clamped 1-16 at load time; values outside
	// the range fall back to default 1). IndexerEnabled gates the whole
	// subsystem. IndexerMinAgeSec is the minimum age in seconds before a
	// closed segment becomes eligible for indexing — guards against racing a
	// still-writing file.
	IndexerConcurrency int
	IndexerEnabled     bool
	IndexerMinAgeSec   int

	// WorkerLeaderDisabled skips the Postgres advisory-lock leader election
	// in the worker binary. Single-binary dev (api runs the worker loops
	// in-process via RUN_WORKERS=true) sets this to true so a sibling worker
	// process — if one ever launches by accident — doesn't deadlock on the
	// lock. Default false: multi-container deploys want exactly-one-leader
	// semantics.
	WorkerLeaderDisabled bool

	// Metrics (P1-C-03): Prometheus exposition endpoint at /metrics.
	//
	// MetricsEnabled gates the whole endpoint. Default true — set
	// METRICS_ENABLED=false to disable entirely (useful during a
	// misconfigured deploy before a Prom scraper is pointed at the host).
	//
	// MetricsAuth controls who may reach /metrics:
	//   "none" — (PRODUCTION DEFAULT, P1-A-02 PR3) no application-layer auth
	//            check. The /metrics route is NOT wrapped in RequireAuth.
	//            REQUIRED: NPM must restrict /metrics to the monitoring LXC
	//            IP and/or trusted LAN CIDR before traffic reaches the app.
	//            Do NOT expose /metrics publicly with METRICS_AUTH=none — it
	//            leaks internal observability data. See docs/metrics.md.
	//   "sso"  — behind the same RequireAuth JWT/SSO middleware as /api/*.
	//            Useful for dev/testing when network restrictions are not in
	//            place. Not the production scraping path post-P1-A-02-PR3
	//            (the bearer token path has been retired).
	// Default "none" (P1-A-02 PR3, previously "sso").
	MetricsEnabled bool
	MetricsAuth    string // "none" (prod) | "sso" (dev/test)

	// PPE worker (P2-C-01). PPEPollIntervalSec is the cadence between
	// snapshot polls per camera. PPEConfidenceThreshold is the
	// application-layer gate above the sidecar's own threshold (0.35):
	// only violations above this value create a pending_review_queue row.
	// PPEFramesDir is the root directory for PPE evidence JPEGs; frames are
	// stored as <PPEFramesDir>/<org_id>/<YYYY-MM-DD>/<timestamp_ms>.jpg.
	// PPEFrameRetentionDays controls how long reviewed/dismissed rows (and
	// their frames) are kept before the retention sweep removes them.
	//
	// Env vars:
	//   PPE_POLL_INTERVAL_SEC         default 30
	//   PPE_CONFIDENCE_THRESHOLD      default 0.50
	//   PPE_FRAMES_DIR                default /tank/data/ironsight/ppe-frames
	//   PPE_FRAME_RETENTION_DAYS      default 7
	PPEPollIntervalSec      int
	PPEConfidenceThreshold  float64
	PPEFramesDir            string
	PPEFrameRetentionDays   int

	// Person-tracking worker (P2-C-02). TrackingEnabled gates the whole
	// subsystem; false skips the tracking worker and aggregator loops.
	// TrackingBucketMinutes is the bucket granularity written by the
	// aggregator (only 5 is currently stored; reserved for future use).
	// TrackingRawRetentionDays is how many days person_track_frames rows
	// are kept (TimescaleDB policy + Go sweep fallback).
	// TrackingBucketRetentionDays is how many days person_track_buckets
	// rows are kept (Go sweep only — regular table, no TS policy).
	//
	// Env vars:
	//   TRACKING_ENABLED                  default true
	//   TRACKING_BUCKET_MINUTES           default 5
	//   TRACKING_RAW_RETENTION_DAYS       default 7
	//   TRACKING_BUCKET_RETENTION_DAYS    default 90
	TrackingEnabled             bool
	TrackingBucketMinutes       int
	TrackingRawRetentionDays    int
	TrackingBucketRetentionDays int

	// VLM validation worker (P2-C-03). VLMWorkerEnabled gates the whole
	// async validation loop — default FALSE because Qwen is not running on
	// fred at C-03 deploy time. When disabled, PPE candidates remain at
	// vlm_verdict='pending' and the human review queue shows them normally.
	// Do NOT enable until `GET <AIQwenURL>/health` returns {"degraded":false}.
	//
	// VLMWorkerBatchSize: rows processed per poll cycle (default 5).
	// VLMWorkerPollIntervalSec: sleep between cycles in seconds (default 10).
	// VLMWorkerMaxConcurrent: parallel Qwen calls per batch (default 1 to
	//   avoid VRAM contention on the RTX 3070 alongside the VLM indexer).
	// VLMWorkerMaxRetries: vlm_attempts cap; after this many errors a row
	//   stays at vlm_verdict='error' permanently (default 3).
	// VLMWorkerMaxAgeHours: pending rows older than this are aged out to
	//   vlm_verdict='uncertain' so the human queue doesn't fill with stale
	//   unvalidated candidates during a Qwen outage (default 24).
	//
	// Env vars:
	//   VLM_WORKER_ENABLED           default false
	//   VLM_WORKER_BATCH_SIZE        default 5
	//   VLM_WORKER_POLL_INTERVAL_SEC default 10
	//   VLM_WORKER_MAX_CONCURRENT    default 1
	//   VLM_WORKER_MAX_RETRIES       default 3
	//   VLM_WORKER_MAX_AGE_HOURS     default 24
	VLMWorkerEnabled         bool
	VLMWorkerBatchSize        int
	VLMWorkerPollIntervalSec  int
	VLMWorkerMaxConcurrent    int
	VLMWorkerMaxRetries       int
	VLMWorkerMaxAgeHours      int

	// VLMCropPaddingFactor controls how much context surrounds the detection
	// bounding box when CropToROI crops the frame before sending to Qwen
	// (P2-C-05). A value of 0.25 pads by 25% of max(bboxWidth, bboxHeight)
	// in each direction, clamped to frame bounds. Accepted range: 0.0–1.0;
	// values outside are silently clamped by CropToROI.
	//
	// Env var: VLM_CROP_PADDING_FACTOR  default 0.25
	VLMCropPaddingFactor float64

	// Weekly digest emailer (P3-INFRA-08). See Load() for env var names
	// and defaults. DigestScope is always "org" for this release;
	// "site" is reserved for a future per-site expansion.
	DigestSendDay        int    // 0=Sunday ... 6=Saturday
	DigestSendHour       int    // 0-23 UTC
	DigestScope          string // "org" | "site" (only "org" implemented)
	DigestNoActivitySkip bool   // skip digest for orgs with no PPE activity
}

// Load reads configuration from environment variables with defaults
func Load() *Config {
	cfg := &Config{
		ServerPort:          getEnv("SERVER_PORT", "8080"),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
		SentryDSN:           getEnv("SENTRY_DSN", ""),
		SentryEnvironment:   getEnv("SENTRY_ENVIRONMENT", ""),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://onvif:onvif_dev_password@localhost:5432/onvif_tool?sslmode=disable"),
		StoragePath:         getEnv("STORAGE_PATH", "./storage/recordings"),
		HLSPath:             getEnv("HLS_PATH", "./storage/hls"),
		ExportPath:          getEnv("EXPORT_PATH", "./storage/exports"),
		ThumbnailPath:       getEnv("THUMBNAIL_PATH", "./storage/thumbnails"),
		SegmentDuration:     getEnvInt("SEGMENT_DURATION", 60),
		FFmpegPath:          getEnv("FFMPEG_PATH", defaultFFmpegPath()),
		DetectionServiceURL: getEnv("DETECTION_SERVICE_URL", ""),
		DetectionIntervalMs: getEnvInt("DETECTION_INTERVAL_MS", 500),
		JWTSecret:           requireSecret("JWT_SECRET"),
		DefaultAdminPass:    requireAdminPassword("ADMIN_PASSWORD"),
		// P1-A-05: optional at config-load time; cmd/server hard-requires via ParseKey at boot.
		CameraCredentialsKey: getEnv("CAMERA_CREDENTIALS_KEY", ""),
		AllowedOrigins:      parseAllowedOrigins(getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")),
		CookieSecure:        getEnvBool("COOKIE_SECURE", true),
		EvidenceSigningKey:  getEnv("EVIDENCE_SIGNING_KEY", ""),
		EvidenceED25519PrivateKey: getEnv("EVIDENCE_ED25519_PRIVATE_KEY", ""),
		EvidenceSigningKeyring:    getEnv("EVIDENCE_SIGNING_KEYRING", ""),
		SMTPHost:            getEnv("SMTP_HOST", ""),
		SMTPPort:            getEnv("SMTP_PORT", "587"),
		SMTPUser:            getEnv("SMTP_USER", ""),
		SMTPPass:            getEnv("SMTP_PASS", ""),
		SMTPFrom:            getEnv("SMTP_FROM", ""),
		PublicURL:           getEnv("NOTIFY_PUBLIC_URL", ""),
		TwilioAccountSid:    getEnv("TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:     getEnv("TWILIO_AUTH_TOKEN", ""),
		TwilioFrom:          getEnv("TWILIO_FROM", ""),
		ProductName:         getEnv("PRODUCT_NAME", "Ironsight"),

		// MediaMTX: embedded spawn is the historical behavior, still the
		// default for single-binary dev. Set EMBEDDED_MEDIAMTX=0 to opt out —
		// the Go process then assumes MediaMTX is already running at the
		// addresses below (docker-compose sibling container, K8s sidecar).
		MediaMTXEmbedded:   getEnvBool("EMBEDDED_MEDIAMTX", true),
		MediaMTXWebRTCAddr: getEnv("MEDIAMTX_WEBRTC_ADDR", "127.0.0.1:8889"),
		MediaMTXRTSPAddr:   getEnv("MEDIAMTX_RTSP_ADDR", "127.0.0.1:18554"),
		MediaMTXAPIAddr:    getEnv("MEDIAMTX_API_ADDR", "127.0.0.1:9997"),
		// MEDIAMTX_HLS_ADDR: compose deployments use the mediamtx container name;
		// embedded/dev uses loopback. Must NOT include a scheme — http:// is prepended
		// in the live_proxy handler.
		MediaMTXHLSAddr: getEnv("MEDIAMTX_HLS_ADDR", "127.0.0.1:8888"),

		// Empty = only advertise interfaces MediaMTX picks up itself
		// (the docker bridge IP, in compose). For LAN browsers, set
		// WEBRTC_ADDITIONAL_HOSTS to the host's LAN IP (or public hostname).
		WebRTCAdditionalHosts: parseAllowedOrigins(getEnv("WEBRTC_ADDITIONAL_HOSTS", "")),

		RedisURL:       getEnv("REDIS_URL", ""),
		RedisWSChannel: getEnv("REDIS_WS_CHANNEL", "ironsight:ws:broadcast"),

		// Default true keeps dev-workflow unchanged (single binary, all
		// jobs in-process). Docker-compose prod sets RUN_WORKERS=false on
		// the api service and spins a separate `worker` container.
		RunWorkers: getEnvBool("RUN_WORKERS", true),

		// SSO header-trust. Empty == JWT-only (default). Set
		// SSO_TRUST_HEADER=email to opt in, then SSO_ADMIN_EMAILS to
		// auto-promote initial admins, and SSO_DEFAULT_ROLE for the
		// rest. Only enable behind a trusted reverse proxy (oauth2-proxy
		// + NPM in the BigView deployment) that strips inbound copies
		// of X-Forwarded-Email; otherwise any client could impersonate
		// any user.
		SSOTrustHeader: getEnv("SSO_TRUST_HEADER", ""),
		SSOAdminEmails: parseAllowedOrigins(getEnv("SSO_ADMIN_EMAILS", "")),
		SSODefaultRole: getEnv("SSO_DEFAULT_ROLE", "viewer"),

		// AI pipeline endpoints. Defaults match the in-host loopback ports
		// the docker-compose layout publishes; sibling-container deployments
		// override with the service DNS names. AI_ENABLED semantics: any
		// value other than the literal string "false" enables the pipeline
		// (matches the historical inline check `os.Getenv("AI_ENABLED") != "false"`).
		AIYOLOURL: getEnv("AI_YOLO_URL", "http://127.0.0.1:8501"),
		AIQwenURL: getEnv("AI_QWEN_URL", "http://127.0.0.1:8502"),
		AIEnabled: getEnv("AI_ENABLED", "true") != "false",

		// GORT_CAMERAS — comma-separated UUIDs / 8-char prefixes routed to the
		// pure-Go recorder. Default empty (everyone on FFmpeg).
		GortCameras: parseAllowedOrigins(getEnv("GORT_CAMERAS", "")),

		// Indexer settings — see internal/indexer/indexer.go for the worker
		// loop. Concurrency outside 1..16 falls back to default 1 (matches
		// the historical clamping behavior). INDEXER_ENABLED uses the
		// narrower "0 or case-insensitive false disables" rule that the
		// indexer's original inline parser used — wider getEnvBool ("no" /
		// "off" / etc) would silently flip behavior on existing
		// deployments using those strings.
		IndexerConcurrency: clampIndexerConcurrency(getEnvInt("INDEXER_CONCURRENCY", 1)),
		IndexerEnabled:     indexerEnabledFromEnv(),
		IndexerMinAgeSec:   getEnvInt("INDEXER_MIN_AGE_SEC", 90),

		// WORKER_LEADER_DISABLED — historical contract is "set to literal 1
		// to skip leader election". Preserved as an exact-string check so a
		// value like "true" doesn't silently start triggering the skip path.
		WorkerLeaderDisabled: os.Getenv("WORKER_LEADER_DISABLED") == "1",

		// Metrics — P1-C-03. Default on; P1-A-02 PR3 changed default auth to
		// "none" (network-trust via NPM). Set METRICS_AUTH=sso for dev.
		MetricsEnabled: getEnvBool("METRICS_ENABLED", true),
		MetricsAuth:    metricsAuthFromEnv(),

		// PPE worker — P2-C-01.
		PPEPollIntervalSec:     getEnvInt("PPE_POLL_INTERVAL_SEC", 30),
		PPEConfidenceThreshold: getEnvFloat64("PPE_CONFIDENCE_THRESHOLD", 0.50),
		PPEFramesDir:           getEnv("PPE_FRAMES_DIR", "/tank/data/ironsight/ppe-frames"),
		PPEFrameRetentionDays:  getEnvInt("PPE_FRAME_RETENTION_DAYS", 7),

		// Person-tracking worker — P2-C-02.
		TrackingEnabled:             getEnvBool("TRACKING_ENABLED", true),
		TrackingBucketMinutes:       getEnvInt("TRACKING_BUCKET_MINUTES", 5),
		TrackingRawRetentionDays:    getEnvInt("TRACKING_RAW_RETENTION_DAYS", 7),
		TrackingBucketRetentionDays: getEnvInt("TRACKING_BUCKET_RETENTION_DAYS", 90),

		// VLM validation worker — P2-C-03.
		// Default false: Qwen is not running on fred at C-03 deploy time.
		// Enable only after `GET <AI_QWEN_URL>/health` returns {"degraded":false}.
		VLMWorkerEnabled:        getEnvBool("VLM_WORKER_ENABLED", false),
		VLMWorkerBatchSize:       getEnvInt("VLM_WORKER_BATCH_SIZE", 5),
		VLMWorkerPollIntervalSec: getEnvInt("VLM_WORKER_POLL_INTERVAL_SEC", 10),
		VLMWorkerMaxConcurrent:   getEnvInt("VLM_WORKER_MAX_CONCURRENT", 1),
		VLMWorkerMaxRetries:      getEnvInt("VLM_WORKER_MAX_RETRIES", 3),
		VLMWorkerMaxAgeHours:     getEnvInt("VLM_WORKER_MAX_AGE_HOURS", 24),

		// VLM crop padding — P2-C-05.
		// 0.25 = 25% of max(bboxW, bboxH) pad in each direction. Range 0–1;
		// CropToROI silently clamps values outside this range.
		VLMCropPaddingFactor: getEnvFloat64("VLM_CROP_PADDING_FACTOR", 0.25),

		// Weekly digest emailer — P3-INFRA-08.
		// DigestSendDay is the day of week (0=Sunday … 6=Saturday) to send
		// the digest. Default 1 (Monday) so the digest covers the prior
		// Mon–Sun week and lands first thing the following Monday morning.
		// DigestSendHour is the UTC hour (0–23) at which the scheduler
		// fires; default 13 (1 PM UTC ≈ 9 AM US Eastern, 6 AM US Pacific).
		// DigestScope is reserved for a future per-site expansion; only
		// "org" (one email per org, all sites aggregated) is implemented.
		// DigestNoActivitySkip controls whether a site/org with zero PPE
		// activity in the window gets a digest at all — default true (skip
		// empty windows to avoid noise emails).
		//
		// Env vars:
		//   DIGEST_SEND_DAY          default 1 (Monday)
		//   DIGEST_SEND_HOUR         default 13 (UTC)
		//   DIGEST_SCOPE             default "org"
		//   DIGEST_NO_ACTIVITY_SKIP  default true
		DigestSendDay:         clampWeekday(getEnvInt("DIGEST_SEND_DAY", 1)),
		DigestSendHour:        clampHour(getEnvInt("DIGEST_SEND_HOUR", 13)),
		DigestScope:           digestScopeFromEnv(),
		DigestNoActivitySkip:  getEnvBool("DIGEST_NO_ACTIVITY_SKIP", true),
	}
	return cfg
}

// metricsAuthFromEnv parses METRICS_AUTH. Valid values are "none" (default,
// P1-A-02 PR3) and "sso" (dev/test). Any unrecognised value falls back to
// "none" to match the documented production default. "sso" must be set
// explicitly when application-layer auth is desired.
//
// IMPORTANT: "none" means /metrics has NO application-layer auth. The
// operator MUST restrict the endpoint at the reverse proxy (NPM) to the
// monitoring LXC IP / trusted LAN CIDR. See docs/metrics.md.
func metricsAuthFromEnv() string {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("METRICS_AUTH")))
	if v == "sso" {
		return "sso"
	}
	return "none"
}

// clampIndexerConcurrency mirrors the historical inline check in
// internal/indexer/indexer.go: only positive values in [1,16] are honored;
// everything else falls back to the default 1.
func clampIndexerConcurrency(n int) int {
	if n < 1 || n > 16 {
		return 1
	}
	return n
}

// indexerEnabledFromEnv preserves the indexer's original on/off semantics:
// default true; INDEXER_ENABLED=0 or INDEXER_ENABLED=false (case-insensitive)
// disables. Other values keep it on. This is intentionally narrower than the
// shared getEnvBool helper so a value of "no" / "off" doesn't silently flip
// behavior on a deployment that previously left those untreated.
func indexerEnabledFromEnv() bool {
	v := os.Getenv("INDEXER_ENABLED")
	if v == "" {
		return true
	}
	if v == "0" || strings.EqualFold(v, "false") {
		return false
	}
	return true
}

// defaultFFmpegPath picks a sensible default for each platform. Linux ships
// ffmpeg on $PATH (the docker image apt-installs it), Windows devs usually
// drop the prebuilt binary in C:\ffmpeg\bin. Override with FFMPEG_PATH env.
func defaultFFmpegPath() string {
	if runtime.GOOS == "windows" {
		return `C:\ffmpeg\bin\ffmpeg.exe`
	}
	return "ffmpeg"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat64(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

// requireSecret returns the env var's value, or in dev (when DEV_MODE
// is set) generates an ephemeral random secret with a loud warning.
// In production (DEV_MODE unset) the process refuses to start without
// the env var — so a deploy that forgets JWT_SECRET fails loudly at
// boot rather than silently using a known fallback that lets anyone
// forge tokens.
func requireSecret(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if os.Getenv("DEV_MODE") == "" {
		log.Fatalf("[CONFIG] %s is required but not set. "+
			"Generate one with `openssl rand -hex 32` and set it in the environment. "+
			"Set DEV_MODE=1 to allow an ephemeral random secret for local dev only.", key)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("[CONFIG] failed to generate ephemeral %s: %v", key, err)
	}
	ephemeral := hex.EncodeToString(buf)
	log.Printf("[CONFIG] WARNING: %s not set, using ephemeral random secret. "+
		"All sessions will be invalidated on every restart. Set %s in the environment for stable auth.",
		key, key)
	return ephemeral
}

// requireAdminPassword returns the seed admin password from the env.
// Refuses to start in production without one — falling back to "admin"
// would give every fresh deployment a known-credential admin account.
func requireAdminPassword(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if os.Getenv("DEV_MODE") == "" {
		log.Fatalf("[CONFIG] %s is required but not set. "+
			"This is the seed password for the auto-created admin account. "+
			"Set DEV_MODE=1 to fall back to a randomized password printed on first run.", key)
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("[CONFIG] failed to generate ephemeral %s: %v", key, err)
	}
	pw := hex.EncodeToString(buf)
	log.Printf("[CONFIG] DEV: generated admin password: %s (set %s in env to override)", pw, key)
	return pw
}

// getEnvBool treats the common "false, 0, no, off" family as false and
// anything else non-empty as true. Unset = fallback.
func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "0", "false", "FALSE", "False", "no", "NO", "No", "off", "OFF", "Off":
		return false
	}
	return true
}

// clampWeekday clamps n into [0,6]. Values outside the range default to 1
// (Monday) — the intended default for the weekly digest send day.
func clampWeekday(n int) int {
	if n < 0 || n > 6 {
		return 1
	}
	return n
}

// clampHour clamps n into [0,23]. Values outside the range default to 13
// (1 PM UTC) — the intended default for the weekly digest send hour.
func clampHour(n int) int {
	if n < 0 || n > 23 {
		return 13
	}
	return n
}

// digestScopeFromEnv parses DIGEST_SCOPE. Only "org" is implemented; any
// unrecognised value falls through to "org" to ensure a safe default.
// "site" is reserved for a future per-site expansion (P3-INFRA-08 v2).
func digestScopeFromEnv() string {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("DIGEST_SCOPE")))
	if v == "site" {
		return "site"
	}
	return "org"
}

// parseAllowedOrigins splits a comma-separated env value into a list of
// CORS origins. Empty entries are dropped. The result is passed to the
// chi cors.Handler as-is — wildcards like "*" are honored but heavily
// discouraged for any deployment that an auditor will see; the env
// shape lets you list each production frontend origin explicitly.
func parseAllowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
