// Package logging is the Ironsight platform-wide structured logging
// home (P1-C-01).
//
// Why this exists: pre-P1-C-01 the codebase used stdlib `log.Printf`
// everywhere — 365 call sites scattered across the API, the indexer,
// the recording engine, and the workers. Output was line-based text
// with no per-request correlation, no level filtering, no structured
// fields. When a user reported "/api/cameras/{id} returned 500 around
// 14:32" the only way to find the matching log line was to grep by
// timestamp range and hope it was unique. Once a Sentry/GlitchTip
// integration lands (P1-C-02) and a Prometheus + dashboard backend
// lands (P1-C-03), they need a structured stream with stable field
// names to ingest. This package is that foundation.
//
// Design:
//
//   - Output is JSON to stderr (12-factor: the platform — systemd,
//     docker, k8s — captures stderr and routes it). Every log line is
//     one self-contained JSON object so a downstream log pipeline can
//     parse without buffering or framing rules.
//   - Level is configurable via env (LOG_LEVEL=debug|info|warn|error,
//     default info). Wired through internal/config so a single source
//     of truth for log config matches the P1-B-05 centralisation.
//   - Every HTTP request gets a UUIDv7 request_id (UUIDv7 is
//     time-ordered — it sorts roughly by arrival in the log stream,
//     which is what you want for incident triage). The id is exposed
//     in the X-Request-Id response header so a customer can quote it
//     in a support ticket and we can grep one row out of millions.
//   - Loggers are propagated via context.Context (the standard idiom
//     since slog 1.21). Handlers pull `logger := logging.FromContext(r.Context())`
//     and any log calls automatically include request_id + route +
//     user_id (when available).
//   - Conversion of the 365 existing `log.Printf` call sites is
//     intentionally NOT part of this PR. The infrastructure lands here;
//     individual files migrate as they're touched for other work.
//     stdlib log continues to function and writes to the same stderr.
package logging

import (
	"context"
	"io"
	stdlog "log"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
)

// New constructs a JSON slog.Logger configured for the given level.
// Output goes to stderr (12-factor: stdout is reserved for the
// application's own data stream, not its diagnostics).
func New(level string) *slog.Logger {
	return newWithWriter(level, os.Stderr)
}

// newWithWriter is the test seam. Production code uses New() which
// pins to stderr; tests can capture into a bytes.Buffer.
func newWithWriter(level string, w io.Writer) *slog.Logger {
	lvl := parseLevel(level)
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: lvl,
		// AddSource is off by default. The slog source field adds
		// noise to every line (file:line of the call) and the value
		// in production tail-grepping is low. Re-enable per-call via
		// slog.LogAttrs with slog.SourceKey if a specific debug
		// session wants it.
		AddSource: false,
	})
	return slog.New(handler)
}

// parseLevel maps the env-friendly level names to slog levels. Unknown
// values fall through to info (fail-open: a typo in LOG_LEVEL should
// not silence the entire log stream).
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// InstallAsDefault makes the given slog.Logger the process-wide
// default. This is what `slog.Info("...")` and friends use when no
// logger is passed explicitly. Called once at startup from
// cmd/server/main.go after config is loaded; subsequent calls
// overwrite (useful for tests).
//
// Also routes the standard library's `log` package through the same
// JSON handler — every legacy `log.Printf` call site now emits a
// JSON line at level info. This is the migration bridge: we don't
// have to convert all 365 call sites to land structured logging.
// As individual files are touched for other work they can move from
// log.Printf to slog.Info/Warn/Error gradually.
func InstallAsDefault(logger *slog.Logger) {
	slog.SetDefault(logger)
	// Bridge stdlib log -> slog. slog.NewLogLogger wraps a Handler so
	// that the stdlib log.Default() emits via the slog handler.
	// stdlib log writes at LevelInfo (chosen — most legacy calls are
	// informational; the ones that aren't would be migrated by hand).
	bridged := slog.NewLogLogger(logger.Handler(), slog.LevelInfo)
	// log.Default() is the singleton used by package-level log.Printf
	// etc.; resetting its output + flags routes those calls through
	// the slog handler.
	stdlog.SetFlags(0)
	stdlog.SetPrefix("")
	stdlog.SetOutput(bridged.Writer())
}

// contextKey is private so callers can't construct one and accidentally
// collide with our value. Mandatory pattern for context keys per the
// stdlib docs.
type contextKey int

const (
	loggerKey contextKey = iota
	requestIDKey
)

// WithLogger returns a derived context carrying the given logger. Use
// at request entry (in middleware) to attach a per-request logger that
// has request_id + route + user_id pre-bound; downstream handlers pull
// it via FromContext.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext extracts the request-scoped logger from ctx. If none was
// set (e.g., a code path that didn't go through HTTP middleware — a
// background goroutine, a unit test), falls back to the process default
// so callers don't need a nil-check.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

// WithRequestID returns a derived context carrying the given request ID
// string. Stored alongside the logger so handlers that don't take a
// logger argument can still pull the ID for echoing in response bodies
// or passing to downstream services (e.g., the Sentry / Prometheus
// labels we'll wire in P1-C-02 + C-03).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext returns the request ID set by WithRequestID, or
// the empty string if none was set. Empty rather than a nil-check so
// callers can always concatenate without a guard.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// NewRequestID generates a fresh UUIDv7. UUIDv7 is preferred over v4
// for request IDs because the first 48 bits are a millisecond Unix
// timestamp — log entries sort roughly by arrival order without an
// explicit timestamp lookup, which speeds up "find all rows for
// requests that arrived between T1 and T2" queries in a structured
// log backend. Falls back to v4 on the (extremely unlikely) RNG
// failure rather than returning an error — a request without an ID
// is still better than a 500 because we couldn't generate one.
func NewRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString() // fallback to v4
}

// ensureBaseLogger returns logger or, if nil, slog.Default(). Used
// inside middleware so callers that pass nil get sensible behaviour
// rather than a panic on the first log call.
func ensureBaseLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// NewWithSentry constructs a JSON slog.Logger that fans out Error-level
// records to the Sentry/GlitchTip client (P1-C-02). The underlying
// JSON-to-stderr behaviour is identical to New. Call this instead of
// New after InitSentry has been called; when DSN was empty, InitSentry
// is a no-op and the SentryHandler degrades to a pure pass-through.
func NewWithSentry(level string) *slog.Logger {
	return newWithSentryAndWriter(level, os.Stderr)
}

// newWithSentryAndWriter is the test seam for Sentry-wrapped loggers.
func newWithSentryAndWriter(level string, w io.Writer) *slog.Logger {
	lvl := parseLevel(level)
	jsonHandler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:     lvl,
		AddSource: false,
	})
	return slog.New(NewSentryHandler(jsonHandler))
}
