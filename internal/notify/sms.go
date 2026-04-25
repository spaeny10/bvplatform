package notify

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SMS notification sender via the Twilio REST API.
//
// We deliberately call the HTTPS Messages endpoint directly rather
// than pulling in the Twilio Go SDK — the SDK is a large dependency
// graph for what amounts to "POST a form with three fields and parse
// the JSON response." Doing it in stdlib keeps go.mod lean and lets
// us swap providers (Plivo, Vonage, MessageBird) by writing a sibling
// 60-line file.
//
// Auth: HTTP Basic with AccountSid as username, AuthToken as password.
// The Twilio docs are explicit about this pattern; we replicate it
// without storing the token plaintext anywhere except the env (loaded
// into config.Config at startup).

// TwilioConfig holds the credentials needed to call the Messages API.
// Empty AccountSid disables SMS sending — the dispatcher routes
// through StubSMSMailer instead so dev environments still produce
// observable log output.
type TwilioConfig struct {
	AccountSid string // ACxxxxxxxxxxxx
	AuthToken  string // 32-char token
	From       string // E.164 sending number, e.g. "+15551234567"
}

// SMSMailer is a Mailer specialization that ignores the email-only
// fields (Subject, HTMLBody) and sends only TextBody via SMS. Each
// recipient in msg.To becomes a separate Twilio API call — Twilio
// doesn't support batched sends in this endpoint, and per-recipient
// errors should be isolated anyway.
type SMSMailer struct {
	cfg    TwilioConfig
	client *http.Client
}

func NewSMSMailer(cfg TwilioConfig) *SMSMailer {
	return &SMSMailer{
		cfg: cfg,
		// 10s timeout per request — Twilio is normally <500ms; anything
		// above 10s is a service issue and the worker should retry on
		// the next event rather than block the request thread.
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (m *SMSMailer) Send(ctx context.Context, msg Message) error {
	// SMS body cap — Twilio splits messages over 160 chars into
	// segments (and bills per segment). We truncate at 320 (2 segments)
	// for cost predictability. Operators sending longer narratives
	// should use email; this channel is for "alarm dispositioned —
	// view in app" style summaries.
	body := msg.TextBody
	if body == "" {
		body = msg.Subject
	}
	if len(body) > 320 {
		body = body[:317] + "…"
	}

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", m.cfg.AccountSid)

	var firstErr error
	for _, to := range msg.To {
		form := url.Values{}
		form.Set("To", to)
		form.Set("From", m.cfg.From)
		form.Set("Body", body)

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		req.SetBasicAuth(m.cfg.AccountSid, m.cfg.AuthToken)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := m.client.Do(req)
		if err != nil {
			log.Printf("[NOTIFY-SMS] send to %s failed: %v", to, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// Drain + close to allow connection reuse. We only need the
		// status code; Twilio returns a JSON envelope but for a
		// fire-and-forget channel we don't need to parse it.
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			log.Printf("[NOTIFY-SMS] twilio %d for %s: %s", resp.StatusCode, to, snippet(bodyBytes))
			if firstErr == nil {
				firstErr = fmt.Errorf("twilio status %d", resp.StatusCode)
			}
		}
	}
	return firstErr
}

// StubSMSMailer mirrors StubMailer for the SMS path. Used when
// Twilio isn't configured.
type StubSMSMailer struct{}

func NewStubSMSMailer() *StubSMSMailer { return &StubSMSMailer{} }

func (s *StubSMSMailer) Send(ctx context.Context, msg Message) error {
	body := msg.TextBody
	if len(body) > 160 {
		body = body[:160] + "…"
	}
	log.Printf("[NOTIFY-SMS-STUB] to=%v tag=%s body=%q", msg.To, msg.Tag, body)
	return nil
}

// SelectSMSMailer mirrors SelectMailer — returns the live sender when
// Twilio creds are present, the stub when not.
func SelectSMSMailer(cfg TwilioConfig) Mailer {
	if cfg.AccountSid == "" || cfg.AuthToken == "" || cfg.From == "" {
		log.Println("[NOTIFY] Twilio not fully configured — SMS will be logged to stderr only (stub mode)")
		return NewStubSMSMailer()
	}
	log.Printf("[NOTIFY] Twilio SMS mailer ready (from=%s)", cfg.From)
	return NewSMSMailer(cfg)
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
