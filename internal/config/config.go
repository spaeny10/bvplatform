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

	// SSO via reverse-proxy header trust. When SSOTrustHeader == "email", the
	// API trusts an "X-Forwarded-Email" header injected by an upstream proxy
	// (oauth2-proxy + NPM in the BigView deployment) and skips JWT entirely.
	// Empty (default) keeps the original JWT-only flow. SSOAdminEmails is a
	// comma-separated allowlist; addresses on it become admin on first sight,
	// so a freshly-deployed system can be administered without a DB seed step.
	// SSODefaultRole is what every other SSO-provisioned user gets ("viewer"
	// is the least-privilege role, recommended).
	SSOTrustHeader  string
	SSOAdminEmails  []string
	SSODefaultRole  string

	// CORS allowlist for the API. Comma-separated origins in the env;
	// the helper splits on commas and trims whitespace. UL 827B
	// reviewers will look for this to be locked down to the
	// production frontend's exact origin (e.g. https://soc.example.com)
	// rather than the dev-time localhost defaults.
	AllowedOrigins []string

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
	RedisURL         string // full Redis DSN, e.g. redis://redis:6379/0
	RedisWSChannel   string // pub/sub channel name; all replicas in a
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
}

// Load reads configuration from environment variables with defaults
func Load() *Config {
	cfg := &Config{
		ServerPort:          getEnv("SERVER_PORT", "8080"),
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
		AllowedOrigins:      parseAllowedOrigins(getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")),
		EvidenceSigningKey:  getEnv("EVIDENCE_SIGNING_KEY", ""),
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

		// MediaMTX: embedded spawn is the historical behaviour, still the
		// default for single-binary dev. Set EMBEDDED_MEDIAMTX=0 to opt out —
		// the Go process then assumes MediaMTX is already running at the
		// addresses below (docker-compose sibling container, K8s sidecar).
		MediaMTXEmbedded:   getEnvBool("EMBEDDED_MEDIAMTX", true),
		MediaMTXWebRTCAddr: getEnv("MEDIAMTX_WEBRTC_ADDR", "127.0.0.1:8889"),
		MediaMTXRTSPAddr:   getEnv("MEDIAMTX_RTSP_ADDR", "127.0.0.1:18554"),
		MediaMTXAPIAddr:    getEnv("MEDIAMTX_API_ADDR", "127.0.0.1:9997"),

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
		SSOTrustHeader:  getEnv("SSO_TRUST_HEADER", ""),
		SSOAdminEmails:  parseAllowedOrigins(getEnv("SSO_ADMIN_EMAILS", "")),
		SSODefaultRole:  getEnv("SSO_DEFAULT_ROLE", "viewer"),
	}
	return cfg
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
