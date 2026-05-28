package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo, // default
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"err":     slog.LevelError,
		"garbage": slog.LevelInfo, // fail-open
	}
	for input, want := range cases {
		got := parseLevel(input)
		if got != want {
			t.Errorf("parseLevel(%q) = %v; want %v", input, got, want)
		}
	}
}

func TestNew_EmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := newWithWriter("info", &buf)
	logger.Info("hello", slog.String("key", "value"))

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, buf.String())
	}
	if parsed["msg"] != "hello" {
		t.Errorf("msg field = %v; want hello", parsed["msg"])
	}
	if parsed["key"] != "value" {
		t.Errorf("key field = %v; want value", parsed["key"])
	}
	if parsed["level"] != "INFO" {
		t.Errorf("level field = %v; want INFO", parsed["level"])
	}
}

func TestNew_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := newWithWriter("warn", &buf)
	logger.Info("should be filtered")
	if buf.Len() != 0 {
		t.Errorf("info-level line emitted under warn filter: %s", buf.String())
	}
	logger.Warn("should appear")
	if buf.Len() == 0 {
		t.Errorf("warn-level line missing under warn filter")
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	// Empty context falls back to slog.Default()
	if FromContext(ctx) == nil {
		t.Errorf("FromContext on bare ctx returned nil")
	}
	if RequestIDFromContext(ctx) != "" {
		t.Errorf("RequestIDFromContext on bare ctx returned non-empty")
	}

	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	ctx = WithLogger(ctx, logger)
	if FromContext(ctx) != logger {
		t.Errorf("WithLogger/FromContext round-trip lost the logger")
	}

	ctx = WithRequestID(ctx, "test-id-123")
	if RequestIDFromContext(ctx) != "test-id-123" {
		t.Errorf("WithRequestID/RequestIDFromContext round-trip mismatch")
	}
}

func TestNewRequestID_IsUUIDv7(t *testing.T) {
	id := NewRequestID()
	if len(id) != 36 {
		t.Errorf("UUID length = %d; want 36", len(id))
	}
	// The version nibble is the 13th hex char (index 14 in the dashed
	// form). v7 sets it to 7. Test the happy path; the v4 fallback
	// would set it to 4. Either is acceptable for the function's
	// contract but v7 should be the normal output.
	if id[14] != '7' && id[14] != '4' {
		t.Errorf("expected UUIDv7 or UUIDv4 version nibble, got %c in %s", id[14], id)
	}
}

func TestRequestID_GeneratesAndSetsHeader(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestIDFromContext(r.Context())
		if id == "" {
			t.Error("request ID missing from context inside handler")
		}
		w.WriteHeader(204)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Request-Id")
	if got == "" {
		t.Errorf("X-Request-Id response header not set")
	}
}

func TestRequestID_PreservesCallerSuppliedID(t *testing.T) {
	const upstream = "edge-supplied-id-abc"
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got != upstream {
			t.Errorf("caller-supplied id was overwritten: got %q want %q", got, upstream)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-Id", upstream)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != upstream {
		t.Errorf("response X-Request-Id = %q; want %q", got, upstream)
	}
}

func TestRequestLogger_EmitsCompletionLine(t *testing.T) {
	var buf bytes.Buffer
	base := newWithWriter("debug", &buf)

	mw := RequestLogger(base)
	chain := RequestID(mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler pulls the per-request logger and emits its own
		// line — should also carry the same request_id automatically.
		logger := FromContext(r.Context())
		logger.Info("inside_handler")
		w.WriteHeader(201)
		w.Write([]byte("created"))
	})))

	req := httptest.NewRequest(http.MethodPost, "/things", nil)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	// Buffer now contains two JSON lines: the inside_handler one and
	// the completion http_request one. Both should share request_id.
	lines := bytesLines(buf.Bytes())
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d:\n%s", len(lines), buf.String())
	}

	var handlerLine, completionLine map[string]any
	if err := json.Unmarshal(lines[0], &handlerLine); err != nil {
		t.Fatalf("handler line not JSON: %v", err)
	}
	if err := json.Unmarshal(lines[1], &completionLine); err != nil {
		t.Fatalf("completion line not JSON: %v", err)
	}

	if handlerLine["msg"] != "inside_handler" {
		t.Errorf("first line msg = %v; want inside_handler", handlerLine["msg"])
	}
	if completionLine["msg"] != "http_request" {
		t.Errorf("second line msg = %v; want http_request", completionLine["msg"])
	}

	rid1, rid2 := handlerLine["request_id"], completionLine["request_id"]
	if rid1 == "" || rid1 == nil {
		t.Errorf("handler line missing request_id")
	}
	if rid1 != rid2 {
		t.Errorf("request_id drifted between handler line and completion line: %v vs %v", rid1, rid2)
	}

	if completionLine["status"].(float64) != 201 {
		t.Errorf("completion status = %v; want 201", completionLine["status"])
	}
	if completionLine["method"] != "POST" {
		t.Errorf("completion method = %v; want POST", completionLine["method"])
	}
	if completionLine["path"] != "/things" {
		t.Errorf("completion path = %v; want /things", completionLine["path"])
	}
	// duration_ms could legitimately be 0 on a fast handler; just
	// confirm the field exists and is numeric.
	if _, ok := completionLine["duration_ms"].(float64); !ok {
		t.Errorf("completion duration_ms missing or wrong type: %v", completionLine["duration_ms"])
	}
}

func TestRequestLogger_LevelsByStatus(t *testing.T) {
	cases := []struct {
		status    int
		wantLevel string
	}{
		{200, "INFO"},
		{201, "INFO"},
		{304, "INFO"},
		{400, "WARN"},
		{404, "WARN"},
		{499, "WARN"},
		{500, "ERROR"},
		{502, "ERROR"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		base := newWithWriter("debug", &buf)
		chain := RequestID(RequestLogger(base)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		})))

		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)

		lines := bytesLines(buf.Bytes())
		var line map[string]any
		_ = json.Unmarshal(lines[len(lines)-1], &line)
		if line["level"] != c.wantLevel {
			t.Errorf("status %d -> level %v; want %v", c.status, line["level"], c.wantLevel)
		}
	}
}

func bytesLines(b []byte) [][]byte {
	out := [][]byte{}
	for _, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if ln != "" {
			out = append(out, []byte(ln))
		}
	}
	return out
}
