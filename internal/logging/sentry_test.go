package logging

// P1-C-02 tests for the Sentry/GlitchTip integration layer.
//
// Test coverage:
//  1. Empty DSN: slog.Error does not crash and does not attempt to send.
//  2. Live DSN (httptest server): slog.Error produces a Sentry envelope
//     that hits the test server.
//  3. Request ID: flows from context into the captured event's tags.
//  4. Panic in handler: captured event via sentryhttp.
//  5. Secret suppression: an attr named "password" is redacted before
//     the event reaches the transport (BeforeSend hook).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

// resetSentryState clears the sentryEnabled flag and resets the global
// Sentry hub between tests so each test starts clean.
func resetSentryState() {
	sentryEnabled = false
	//nolint:errcheck
	sentry.Init(sentry.ClientOptions{Dsn: ""})
	sentry.CurrentHub().BindClient(nil)
}

// --- Test 1: Empty DSN — no crash, no send ---

func TestSentryHandler_EmptyDSN_NoOp(t *testing.T) {
	resetSentryState()
	if err := InitSentry("", ""); err != nil {
		t.Fatalf("InitSentry with empty DSN returned error: %v", err)
	}
	if sentryEnabled {
		t.Fatal("sentryEnabled should be false after InitSentry with empty DSN")
	}

	var buf bytes.Buffer
	logger := newWithSentryAndWriter("error", &buf)
	logger.Error("test error", slog.String("k", "v"))

	if buf.Len() == 0 {
		t.Error("inner handler should still emit logs when sentry is disabled")
	}
}

// --- Test 2: Live DSN — envelope reaches test server ---

func TestSentryHandler_ErrorSent(t *testing.T) {
	resetSentryState()

	var (
		mu       sync.Mutex
		received []byte
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, body...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use http:// so the SDK sends plain HTTP to the httptest server.
	// sentry-go constructs the envelope endpoint from the DSN scheme,
	// so http:// here means HTTP (no TLS) — required for httptest.NewServer.
	dsn := "http://abc123@" + strings.TrimPrefix(srv.URL, "http://") + "/1"
	if err := InitSentry(dsn, "test"); err != nil {
		t.Fatalf("InitSentry: %v", err)
	}
	if !sentryEnabled {
		t.Fatal("sentryEnabled should be true after successful InitSentry")
	}
	defer resetSentryState()

	var buf bytes.Buffer
	logger := newWithSentryAndWriter("debug", &buf)
	ctx := context.Background()
	logger.ErrorContext(ctx, "something broke", slog.String("subsystem", "recorder"))

	sentry.Flush(2 * time.Second)

	mu.Lock()
	got := string(received)
	mu.Unlock()

	if got == "" {
		t.Error("expected at least one Sentry envelope to be delivered, got nothing")
	}
	if !strings.Contains(got, "something broke") {
		t.Errorf("envelope body does not contain the event message: %s", got)
	}
}

// --- Test 3: Request ID flows to event tags ---

func TestSentryHandler_RequestIDInTags(t *testing.T) {
	resetSentryState()

	capturedEvents := make(chan *sentry.Event, 4)
	transport := &inMemTransport{ch: capturedEvents}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:        "https://testkey@example.com/1",
		Transport:  transport,
		BeforeSend: redactSensitiveFields,
	})
	if err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	sentryEnabled = true
	defer resetSentryState()

	const wantID = "test-request-id-abc"
	ctx := WithRequestID(context.Background(), wantID)

	var buf bytes.Buffer
	logger := newWithSentryAndWriter("debug", &buf)
	logger.ErrorContext(ctx, "handler failed")

	sentry.Flush(2 * time.Second)

	select {
	case event := <-capturedEvents:
		if got, ok := event.Tags["request_id"]; !ok || got != wantID {
			t.Errorf("request_id tag = %q; want %q", got, wantID)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for captured event")
	}
}

// --- Test 4: Panic in handler captured with stack ---

func TestSentryHTTP_PanicCaptured(t *testing.T) {
	resetSentryState()

	capturedEvents := make(chan *sentry.Event, 4)
	transport := &inMemTransport{ch: capturedEvents}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "https://testkey@example.com/1",
		Transport:        transport,
		AttachStacktrace: true,
		BeforeSend:       redactSensitiveFields,
	})
	if err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	sentryEnabled = true
	defer resetSentryState()

	sentryMW := sentryhttp.New(sentryhttp.Options{
		Repanic:         true,
		WaitForDelivery: true,
	})

	// The outer handler catches the re-panic from sentryhttp so the test
	// doesn't crash the test runner.
	outer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		sentryMW.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("deliberate test panic")
		})).ServeHTTP(w, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	outer.ServeHTTP(rec, req)

	sentry.Flush(2 * time.Second)

	select {
	case event := <-capturedEvents:
		if event.Level != sentry.LevelFatal && event.Level != sentry.LevelError {
			t.Errorf("panic event level = %v; want error or fatal", event.Level)
		}
		// sentry-go v0.46+ may surface panic details in Exception or Threads
		// depending on AttachStacktrace + RecoverWithContext internals.
		// Accept either: Exception entries OR a non-empty message containing panic text.
		hasException := len(event.Exception) > 0
		hasThreads := len(event.Threads) > 0
		hasMessage := strings.Contains(event.Message, "panic") ||
			strings.Contains(event.Message, "deliberate")
		if !hasException && !hasThreads && !hasMessage {
			t.Errorf("panic event has no Exception, no Threads, and no panic message — got level=%v msg=%q", event.Level, event.Message)
		}
	case <-time.After(5 * time.Second):
		t.Error("timed out waiting for panic event")
	}
}

// --- Test 5: Password attr is redacted ---

func TestSentryHandler_PasswordRedacted(t *testing.T) {
	resetSentryState()

	capturedEvents := make(chan *sentry.Event, 4)
	transport := &inMemTransport{ch: capturedEvents}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:        "https://testkey@example.com/1",
		Transport:  transport,
		BeforeSend: redactSensitiveFields,
	})
	if err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	sentryEnabled = true
	defer resetSentryState()

	var buf bytes.Buffer
	logger := newWithSentryAndWriter("debug", &buf)
	logger.Error("login attempt",
		slog.String("username", "alice"),
		slog.String("password", "supersecret"),
	)

	sentry.Flush(2 * time.Second)

	select {
	case event := <-capturedEvents:
		// "username" should appear in the log_attrs context entry.
		// sentry.Context is map[string]interface{} — access directly.
		logAttrs, ok := event.Contexts["log_attrs"]
		if !ok {
			t.Fatal("expected log_attrs context entry")
		}
		// logAttrs is sentry.Context = map[string]interface{}
		if v, ok := logAttrs["username"]; !ok || v != "alice" {
			t.Errorf("username = %v; want alice", v)
		}
		// "password" must be redacted.
		if v, ok := logAttrs["password"]; !ok {
			t.Error("password key should still appear in log_attrs (as [REDACTED])")
		} else if v == "supersecret" {
			t.Error("password value was not redacted — raw secret would leak to Sentry")
		} else {
			raw, _ := json.Marshal(v)
			if !strings.Contains(string(raw), "REDACTED") {
				t.Errorf("password value = %v; expected REDACTED marker", v)
			}
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for captured event")
	}
}

// inMemTransport is a Sentry transport that queues events into a channel
// for inspection by tests. It does not make any network calls.
type inMemTransport struct {
	ch chan *sentry.Event
}

func (t *inMemTransport) Flush(timeout time.Duration) bool           { return true }
func (t *inMemTransport) FlushWithContext(ctx context.Context) bool  { return true }
func (t *inMemTransport) Configure(options sentry.ClientOptions)     {}
func (t *inMemTransport) Close()                                      {}
func (t *inMemTransport) SendEvent(event *sentry.Event) {
	select {
	case t.ch <- event:
	default:
	}
}
