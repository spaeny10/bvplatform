package logging

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// RequestID is an HTTP middleware that injects a per-request UUIDv7
// into the context AND echoes it as the X-Request-Id response header.
// If the caller already supplied an X-Request-Id we trust it and
// reuse it — useful when the API is behind oauth2-proxy / NPM and the
// edge has already generated an ID for the access log.
//
// Mount BEFORE RequestLogger so the logger middleware can pick up the
// id from the context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = NewRequestID()
		}
		w.Header().Set("X-Request-Id", id)

		ctx := WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestLogger returns an HTTP middleware that emits one structured
// log line per request at completion (slog level info for 2xx/3xx,
// warn for 4xx, error for 5xx). Fields: method, path, status, bytes,
// duration_ms, remote, request_id. Also attaches a per-request
// *slog.Logger (with request_id pre-bound as a default attr) to the
// context so downstream handlers can pull it via FromContext.
//
// Pass the base logger from cmd/server/main.go (the process default
// configured by logging.New). Nil base is tolerated — falls back to
// slog.Default().
func RequestLogger(base *slog.Logger) func(http.Handler) http.Handler {
	base = ensureBaseLogger(base)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			id := RequestIDFromContext(r.Context())

			// Per-request logger has request_id pre-bound so every
			// log line in this request's call tree carries the same
			// id automatically.
			reqLogger := base.With(slog.String("request_id", id))
			ctx := WithLogger(r.Context(), reqLogger)

			// Wrap the ResponseWriter so we can read the eventual
			// status code + bytes written for the completion log
			// line. Keep the wrapping minimal — chi has its own
			// (more featureful) wrap_writer but pulling that in just
			// to read a status field would couple this package to
			// chi unnecessarily. The Hijack / Flush surfaces are
			// preserved by the embedded interface methods below.
			rw := &recordingResponseWriter{ResponseWriter: w, status: 200}

			next.ServeHTTP(rw, r.WithContext(ctx))

			level := slog.LevelInfo
			switch {
			case rw.status >= 500:
				level = slog.LevelError
			case rw.status >= 400:
				level = slog.LevelWarn
			}

			reqLogger.LogAttrs(r.Context(), level, "http_request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Int("bytes", rw.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote", r.RemoteAddr),
			)
		})
	}
}

// recordingResponseWriter is a small ResponseWriter shim that captures
// the response status code and byte count for the completion log line.
// Defaults to 200 because handlers that never call WriteHeader (the
// common 200-with-body path) report nothing to net/http until Write is
// called, and net/http defaults to 200 in that case.
type recordingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (rw *recordingResponseWriter) WriteHeader(code int) {
	if rw.wrote {
		return
	}
	rw.status = code
	rw.wrote = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recordingResponseWriter) Write(b []byte) (int, error) {
	if !rw.wrote {
		// Implicit 200 (net/http does this on first Write if
		// WriteHeader was never called) — record it so the log
		// line shows the right status.
		rw.status = 200
		rw.wrote = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

// Flush passes through to the underlying ResponseWriter if it
// supports the http.Flusher interface. Necessary because Ironsight
// has streaming endpoints (the detection SSE feed, the alerts
// WebSocket upgrade path) that depend on Flush.
func (rw *recordingResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack passes through to the underlying ResponseWriter's Hijacker.
// Required for the WS upgrade path (gorilla/websocket Upgrade) — without
// this method on the wrapper, http.ResponseWriter.(http.Hijacker) type
// assertion fails and the WS upgrade returns 500. The handler that
// follows (HandleWebSocket) never even gets called because the upgrade
// dies first.
func (rw *recordingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("recordingResponseWriter: underlying ResponseWriter is not a Hijacker")
}

// ReadFrom delegates to the underlying ResponseWriter's io.ReaderFrom.
// This must be present (not just Hijack) because upstream wrappers in
// the middleware chain — notably sentry-go's NewWrapResponseWriter —
// only preserve Hijacker when the wrapped writer is "fully fancy"
// (Flusher AND Hijacker AND ReaderFrom). Same fix applied to
// metrics.statusRecorder; both shims must be uniform to keep the WS
// upgrade path working regardless of which order they wrap.
func (rw *recordingResponseWriter) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		// Treat ReadFrom as "data written" for status semantics.
		return rf.ReadFrom(src)
	}
	return io.Copy(writeOnlyWriter{rw}, src)
}

// writeOnlyWriter strips ReadFrom so io.Copy doesn't infinite-loop into
// it on the fallback path.
type writeOnlyWriter struct{ http.ResponseWriter }

func (w writeOnlyWriter) Write(p []byte) (int, error) { return w.ResponseWriter.Write(p) }
