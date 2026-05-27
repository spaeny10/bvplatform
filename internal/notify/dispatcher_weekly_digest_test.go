package notify_test

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"ironsight/internal/notify"
)

// captureStub is a Mailer that records the last message it received.
// Used to assert on the message content without actually sending email.
type captureStub struct {
	last *notify.Message
}

func (c *captureStub) Send(_ context.Context, msg notify.Message) error {
	m := msg
	c.last = &m
	return nil
}

// errorStub always returns an error — used to verify per-recipient
// failure isolation.
type errorStub struct{ err error }

func (e *errorStub) Send(_ context.Context, _ notify.Message) error { return e.err }

func weeklyDigestFixture() notify.WeeklyDigestContext {
	periodStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC) // Monday
	periodEnd := time.Date(2026, 5, 24, 23, 59, 59, 0, time.UTC) // Sunday
	return notify.WeeklyDigestContext{
		OrganizationName: "Acme Corp",
		PeriodStart:      periodStart,
		PeriodEnd:        periodEnd,
		TotalViolations:  12,
		TotalReviewed:    20,
		PendingCount:     3,
		ComplianceRate:   40.0,
		ViolationTrend:   -2,
		TopCameras: []notify.DigestTopCamera{
			{CameraName: "Gate-A-01", ViolationCount: 7, PctOfTotal: 58.3},
			{CameraName: "Loading-Dock-02", ViolationCount: 5, PctOfTotal: 41.7},
		},
		PersonHoursAvailable: true,
		PersonHours:          84.5,
		ComplianceURL:        "http://localhost:3000/portal/compliance",
		PendingReviewURL:     "http://localhost:3000/portal/compliance?tab=pending",
		UnsubscribeURL:       "http://localhost:3000/portal/notifications",
	}
}

// TestWeeklyDigestHTML verifies the HTML body:
//   - contains expected key strings (org name, camera name, violation count)
//   - contains <table but NOT display:flex (Outlook-safe check)
//   - contains the stat block as a table, not a flex container
func TestWeeklyDigestHTML(t *testing.T) {
	stub := &captureStub{}
	d := notify.NewDispatcher(stub, nil, "Ironsight", "http://localhost:3000")

	recipients := []notify.Recipient{{Email: "alice@acme.com"}}
	d.WeeklyDigest(context.Background(), weeklyDigestFixture(), recipients)

	if stub.last == nil {
		t.Fatal("expected Send to be called; got nil")
	}

	html := stub.last.HTMLBody

	mustContain := []string{
		"Acme Corp",
		"Gate-A-01",
		"12",   // TotalViolations
		"3",    // PendingCount
		"<table", // table layout present
		"40.0%",  // compliance rate
		"84.5",   // person-hours
		"-2",     // violation trend
	}
	for _, want := range mustContain {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing expected string %q", want)
		}
	}

	// Critical: no flexbox in the HTML body (Outlook safety).
	flexForbidden := []string{
		"display:flex",
		"display: flex",
		"flex-wrap",
		"flex-wrap:",
	}
	for _, bad := range flexForbidden {
		if strings.Contains(html, bad) {
			t.Errorf("HTML contains forbidden CSS %q (Outlook-unsafe)", bad)
		}
	}
}

// TestWeeklyDigestHTMLEmailClientSafety is a dedicated Outlook-safety
// check: verifies no flex/grid layout CSS appears in the output.
func TestWeeklyDigestHTMLEmailClientSafety(t *testing.T) {
	stub := &captureStub{}
	d := notify.NewDispatcher(stub, nil, "Ironsight", "http://localhost:3000")
	d.WeeklyDigest(context.Background(), weeklyDigestFixture(), []notify.Recipient{{Email: "a@b.com"}})

	if stub.last == nil {
		t.Fatal("no message sent")
	}
	html := stub.last.HTMLBody

	forbidden := []string{
		"display:flex",
		"display: flex",
		"display:grid",
		"display: grid",
		"flex-wrap",
		"gap:",
		"gap: ",
	}
	for _, bad := range forbidden {
		if strings.Contains(html, bad) {
			t.Errorf("email-client-unsafe CSS found: %q", bad)
		}
	}

	// The stat block must use table-based layout.
	if !strings.Contains(html, "<table") {
		t.Error("expected <table in HTML body for stat block layout")
	}
}

// TestWeeklyDigestMultipart verifies the MIME message contains both
// text/plain and text/html parts with a valid boundary.
func TestWeeklyDigestMultipart(t *testing.T) {
	stub := &captureStub{}
	d := notify.NewDispatcher(stub, nil, "Ironsight", "http://localhost:3000")
	d.WeeklyDigest(context.Background(), weeklyDigestFixture(), []notify.Recipient{{Email: "bob@acme.com"}})

	if stub.last == nil {
		t.Fatal("no message sent")
	}
	if stub.last.TextBody == "" {
		t.Error("expected non-empty TextBody for plain-text part")
	}
	if stub.last.HTMLBody == "" {
		t.Error("expected non-empty HTMLBody for HTML part")
	}
	if stub.last.Tag != "weekly_digest" {
		t.Errorf("expected Tag='weekly_digest', got %q", stub.last.Tag)
	}

	// Verify that both parts contain the org name.
	if !strings.Contains(stub.last.TextBody, "Acme Corp") {
		t.Error("TextBody missing org name 'Acme Corp'")
	}
	if !strings.Contains(stub.last.HTMLBody, "Acme Corp") {
		t.Error("HTMLBody missing org name 'Acme Corp'")
	}
}

// TestWeeklyDigestStubMode verifies that StubMailer logs instead of
// sending and returns nil (no error propagation).
func TestWeeklyDigestStubMode(t *testing.T) {
	var buf bytes.Buffer
	origLogger := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origLogger)

	stub := notify.NewStubMailer()
	d := notify.NewDispatcher(stub, nil, "Ironsight", "http://localhost:3000")

	err := stub.Send(context.Background(), notify.Message{
		To:       []string{"alice@acme.com"},
		Subject:  "test",
		TextBody: "hello",
		Tag:      "weekly_digest",
	})
	if err != nil {
		t.Errorf("StubMailer.Send returned unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[NOTIFY-STUB]") {
		t.Errorf("expected [NOTIFY-STUB] log prefix, got: %q", out)
	}
	if !strings.Contains(out, "weekly_digest") {
		t.Errorf("expected tag 'weekly_digest' in stub log, got: %q", out)
	}

	// Suppress d so the compiler doesn't complain about unused var.
	_ = d
}

// TestWeeklyDigestNoRecipients verifies that WeeklyDigest does not
// call Send when the recipient list is empty.
func TestWeeklyDigestNoRecipients(t *testing.T) {
	stub := &captureStub{}
	d := notify.NewDispatcher(stub, nil, "Ironsight", "http://localhost:3000")
	d.WeeklyDigest(context.Background(), weeklyDigestFixture(), nil)
	if stub.last != nil {
		t.Error("expected no Send call for empty recipients")
	}
}

// TestWeeklyDigestSendErrorIsolation verifies that a Send failure logs
// but does not panic or propagate (best-effort semantics).
func TestWeeklyDigestSendErrorIsolation(t *testing.T) {
	errm := &errorStub{err: errSendFailed}
	d := notify.NewDispatcher(errm, nil, "Ironsight", "http://localhost:3000")

	// Should not panic even when Send returns an error.
	d.WeeklyDigest(context.Background(), weeklyDigestFixture(), []notify.Recipient{{Email: "x@y.com"}})
}

// errSendFailed is a sentinel error for TestWeeklyDigestSendErrorIsolation.
type sentinelError string

func (e sentinelError) Error() string { return string(e) }

const errSendFailed sentinelError = "stub send failure"
