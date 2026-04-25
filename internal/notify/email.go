// Package notify is the outbound notification subsystem — email today,
// SMS / web push / webhook later. The subscription model lives here too
// so a single dispatcher can fan out one event to N channels per
// recipient.
//
// Design notes:
//   - We use net/smtp from the standard library. Most managed SMTP
//     relays (Postmark, SES, SendGrid SMTP, Mailgun) speak this just
//     fine. Switching to an HTTP API later means swapping the Send
//     implementation, not the package shape.
//   - When SMTP isn't configured (SMTP_HOST is empty), Send writes
//     the rendered email to stderr instead of sending. This keeps
//     local dev and CI usable without an SMTP relay, and the on-call
//     gets a visible heartbeat in the logs to confirm notifications
//     are firing.
//   - Templates are plain Go string formatting for now. Two reasons:
//     (1) html/template is overkill for the small handful of emails
//     we send, (2) we want the rendered email visible in the audit
//     log later, and string-template diffs are easier to read than
//     compiled-template AST diffs.
package notify

import (
	"context"
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"time"
)

// Mailer is the abstract surface every notification channel
// implements. Today only SMTPMailer + StubMailer; tomorrow we add
// SMSMailer, WebPushMailer, etc., all behind the same interface so
// the dispatcher doesn't need to know which channel it's talking to.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// Message is the channel-agnostic carrier for a single notification.
// Different channels use different fields:
//   - email uses To, Subject, HTMLBody (with TextBody as fallback)
//   - SMS uses To (E.164 phone) + TextBody only
//   - push uses To (subscription endpoint) + Subject (notification
//     title) + TextBody (body text)
type Message struct {
	To       []string // email addresses, phone numbers, or push endpoints
	Subject  string
	TextBody string
	HTMLBody string
	// Tag classifies the message for downstream filtering by the SMTP
	// relay (Postmark/SES/SendGrid all support this) and for our own
	// audit reporting. Examples: "alarm_disposition", "monthly_summary".
	Tag string
}

// SMTPConfig holds the connection params loaded from env vars. Empty
// Host disables sending — the dispatcher will route through StubMailer
// instead so dev environments without SMTP still produce visible logs.
type SMTPConfig struct {
	Host     string // e.g. smtp.postmarkapp.com
	Port     string // e.g. "587"
	Username string
	Password string
	From     string // RFC 5322 mailbox: "Ironsight Alerts <alerts@ironsight.io>"
}

// SMTPMailer implements Mailer using net/smtp. STARTTLS is implicit
// in the smtp.SendMail call when the server advertises it; for ports
// that require implicit TLS (465), we'd need to wrap a tls.Conn
// manually — flagged here for the day we run our own relay.
type SMTPMailer struct {
	cfg SMTPConfig
}

func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer { return &SMTPMailer{cfg: cfg} }

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if len(msg.To) == 0 {
		return nil
	}
	addr := m.cfg.Host + ":" + m.cfg.Port
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	body := buildMIME(m.cfg.From, msg)
	// net/smtp doesn't honor ctx natively. We respect deadline by
	// running the send on a goroutine and racing against ctx.Done().
	// If the caller times out we abandon the goroutine — the worst
	// case is a leaked TCP connection that the kernel reaps shortly.
	errCh := make(chan error, 1)
	go func() {
		errCh <- smtp.SendMail(addr, auth, m.cfg.From, msg.To, body)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// StubMailer logs the rendered email to stderr instead of sending.
// Used when SMTP isn't configured (dev / CI) — keeps the whole
// notification pipeline functional and observable, just doesn't put
// bytes on the wire.
type StubMailer struct{}

func NewStubMailer() *StubMailer { return &StubMailer{} }

func (s *StubMailer) Send(ctx context.Context, msg Message) error {
	preview := msg.TextBody
	if len(preview) > 240 {
		preview = preview[:240] + "…"
	}
	log.Printf("[NOTIFY-STUB] to=%v tag=%s subject=%q body=%q",
		msg.To, msg.Tag, msg.Subject, preview)
	return nil
}

// SelectMailer returns SMTPMailer when configured, StubMailer when
// not. The dispatcher should call this once at startup and reuse the
// returned Mailer — no per-send setup overhead.
func SelectMailer(cfg SMTPConfig) Mailer {
	if cfg.Host == "" {
		log.Println("[NOTIFY] SMTP_HOST is empty — emails will be logged to stderr only (stub mode)")
		return NewStubMailer()
	}
	if cfg.From == "" {
		log.Println("[NOTIFY] SMTP_FROM is empty — falling back to noreply@localhost (set SMTP_FROM in env)")
		cfg.From = "noreply@localhost"
	}
	if cfg.Port == "" {
		cfg.Port = "587"
	}
	log.Printf("[NOTIFY] SMTP mailer ready (host=%s, from=%s)", cfg.Host, cfg.From)
	return NewSMTPMailer(cfg)
}

// buildMIME constructs a minimal multipart MIME message that includes
// both the text and HTML representations. Most managed relays
// re-process the body anyway; we keep it readable so a stderr-stubbed
// preview shows the actual content.
func buildMIME(from string, msg Message) []byte {
	boundary := "ironsight-mime-" + fmt.Sprintf("%d", time.Now().UnixNano())
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(msg.To, ", ") + "\r\n")
	b.WriteString("Subject: " + msg.Subject + "\r\n")
	if msg.Tag != "" {
		// Postmark / SES respect this header for delivery filtering and
		// per-tag bounce reporting. Other relays ignore it harmlessly.
		b.WriteString("X-Tag: " + msg.Tag + "\r\n")
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")

	if msg.TextBody != "" {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		b.WriteString(msg.TextBody + "\r\n\r\n")
	}
	if msg.HTMLBody != "" {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(msg.HTMLBody + "\r\n\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}
