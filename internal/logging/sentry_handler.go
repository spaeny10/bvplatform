package logging

// sentryHandler is a slog.Handler wrapper that forwards Error-level log
// records to Sentry / GlitchTip as captured events (P1-C-02).
//
// Design decisions:
//
//   - Only Error-level records are forwarded. Warn / Info / Debug emit
//     to the underlying handler only. Sentry quotas and noise both argue
//     for a high signal-to-noise bar.
//   - slog attrs on the record are stored in the event's "log_attrs"
//     Context entry (Sentry Contexts = map[string]Context, appears in
//     the detail view). The sentry-go v0.46+ API removed the legacy
//     Extra field; Context is the canonical replacement.
//   - The request_id attr is promoted to a sentry tag so it's first-class
//     searchable. route, method, status, path also become tags. Other
//     attrs land in the log_attrs context entry.
//   - Secret suppression: any attr whose key contains a sensitive
//     substring (password, secret, token, key, credential, authorization,
//     auth) is replaced with "[REDACTED]" before being sent. This is
//     defense-in-depth — callers should never log secrets in the first
//     place, but a mis-placed log.Printf("password=%s") shouldn't leak
//     into an external service. A BeforeSend hook applies the same rule
//     at the SDK level as a second line of defense.
//   - DSN empty = no-op. sentry.Init is never called when SentryDSN is
//     empty, so this wrapper degrades cleanly to the underlying handler.
//   - The wrapper clones the current hub per-event so concurrent goroutines
//     with different request scopes don't share mutable state.

import (
	"context"
	"log/slog"
	"strings"

	"github.com/getsentry/sentry-go"
)

// sentryEnabled is the process-level gate. Set by InitSentry once at
// startup; all handler instances read it.
var sentryEnabled bool

// InitSentry initialises the global Sentry/GlitchTip client. Must be
// called once at process start, before any log lines fire.
//
// When dsn is empty, InitSentry is a no-op and all subsequent sentry
// calls become no-ops via the SDK's own nil-hub handling. This is the
// intended production default until the GlitchTip LXC is stood up.
//
// The environment label (e.g. "production", "staging") appears in every
// Sentry event. When empty, the SDK omits the tag — fine for dev.
func InitSentry(dsn, environment string) error {
	if dsn == "" {
		return nil
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Environment:      environment,
		SampleRate:       1.0,
		TracesSampleRate: 0.0,
		EnableTracing:    false,
		AttachStacktrace: true,
		BeforeSend:       redactSensitiveFields,
	})
	if err != nil {
		return err
	}
	sentryEnabled = true
	return nil
}

// FlushSentry blocks until all queued Sentry events are delivered or
// the timeout elapses. Call on process exit (deferred after InitSentry)
// so events captured near shutdown are not dropped. No-op when DSN was
// empty.
func FlushSentry() {
	if !sentryEnabled {
		return
	}
	sentry.Flush(sentry.DefaultFlushTimeout)
}

// SentryHandler wraps an inner slog.Handler. Error records are forwarded
// to Sentry; all records are passed to the inner handler unchanged.
type SentryHandler struct {
	inner slog.Handler
	// preAttrs holds attrs pre-bound via WithAttrs (e.g. request_id
	// injected by the logging middleware's base.With() call). They're
	// merged into every captured event.
	preAttrs []slog.Attr
	preGroup string
}

// NewSentryHandler wraps inner with Sentry fan-out. When Sentry is not
// initialised (empty DSN path) the wrapper degrades to a pass-through.
func NewSentryHandler(inner slog.Handler) *SentryHandler {
	return &SentryHandler{inner: inner}
}

// Enabled reports whether the inner handler is enabled for the given level.
func (h *SentryHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle passes the record to the inner handler and, for Error-level
// records (when Sentry is initialised), also captures the event.
func (h *SentryHandler) Handle(ctx context.Context, r slog.Record) error {
	innerErr := h.inner.Handle(ctx, r)

	if sentryEnabled && r.Level >= slog.LevelError {
		h.captureRecord(ctx, r)
	}

	return innerErr
}

// WithAttrs returns a new SentryHandler with the given attrs pre-bound.
func (h *SentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, len(h.preAttrs)+len(attrs))
	copy(merged, h.preAttrs)
	copy(merged[len(h.preAttrs):], attrs)
	return &SentryHandler{
		inner:    h.inner.WithAttrs(attrs),
		preAttrs: merged,
		preGroup: h.preGroup,
	}
}

// WithGroup returns a new SentryHandler in the given group namespace.
func (h *SentryHandler) WithGroup(name string) slog.Handler {
	return &SentryHandler{
		inner:    h.inner.WithGroup(name),
		preAttrs: h.preAttrs,
		preGroup: name,
	}
}

// captureRecord converts a slog.Record into a Sentry event and submits
// it via the current hub.
func (h *SentryHandler) captureRecord(ctx context.Context, r slog.Record) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		// Collect structured attrs into two buckets:
		// - tags: first-class searchable (request_id, route, method, etc.)
		// - logAttrs: everything else, stored as a sentry Context entry
		tags := make(map[string]string)
		logAttrs := make(map[string]interface{})

		// Merge pre-bound attrs (e.g. request_id set via logger.With).
		for _, a := range h.preAttrs {
			classifyAttr(a, tags, logAttrs)
		}

		// Merge attrs on this specific record.
		r.Attrs(func(a slog.Attr) bool {
			classifyAttr(a, tags, logAttrs)
			return true
		})

		// Pull request_id from context as well (belt-and-suspenders:
		// the middleware puts it on the logger AND the context).
		if rid := RequestIDFromContext(ctx); rid != "" {
			tags["request_id"] = rid
		}

		scope.SetTags(tags)
		if len(logAttrs) > 0 {
			scope.SetContext("log_attrs", logAttrs)
		}

		event := sentry.NewEvent()
		event.Level = sentry.LevelError
		event.Message = r.Message
		hub.CaptureEvent(event)
	})
}

// classifyAttr sorts a single slog.Attr into tags or logAttrs.
// Sensitive keys are redacted into logAttrs and never promoted to tags.
func classifyAttr(a slog.Attr, tags map[string]string, logAttrs map[string]interface{}) {
	key := a.Key
	val := a.Value.Any()

	if isSensitiveKey(key) {
		logAttrs[key] = "[REDACTED]"
		return
	}

	// Promote specific keys to searchable Sentry tags.
	switch key {
	case "request_id", "route", "method", "status", "path":
		tags[key] = a.Value.String()
	default:
		logAttrs[key] = val
	}
}

// sensitiveKeyPrefixes are lower-cased substrings that flag a key as
// carrying secret material.
var sensitiveKeyPrefixes = []string{
	"password",
	"secret",
	"token",
	"key",
	"credential",
	"authorization",
	"auth",
}

func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	for _, prefix := range sensitiveKeyPrefixes {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

// redactSensitiveFields is a BeforeSend hook called by the Sentry SDK
// before transmitting an event. It walks the event's Contexts map and
// replaces any value whose key is sensitive with "[REDACTED]".
// This is the second line of defense — the per-attr check in
// classifyAttr is the first.
//
// sentry.Context is map[string]interface{}, not an interface type, so
// the loop range is over the map entries directly.
func redactSensitiveFields(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil {
		return nil
	}
	for ctxKey, ctxVal := range event.Contexts {
		// sentry.Context is map[string]interface{} — iterate directly.
		for k := range ctxVal {
			if isSensitiveKey(k) {
				ctxVal[k] = "[REDACTED]"
			}
		}
		event.Contexts[ctxKey] = ctxVal
	}
	return event
}
