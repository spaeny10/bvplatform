package config

import (
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

	// CORS allowlist for the API. Comma-separated origins in the env;
	// the helper splits on commas and trims whitespace. UL 827B
	// reviewers will look for this to be locked down to the
	// production frontend's exact origin (e.g. https://soc.example.com)
	// rather than the dev-time localhost defaults.
	AllowedOrigins []string

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
		JWTSecret:           getEnv("JWT_SECRET", "onvif-tool-change-me-in-production"),
		DefaultAdminPass:    getEnv("ADMIN_PASSWORD", "admin"),
		AllowedOrigins:      parseAllowedOrigins(getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")),
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
