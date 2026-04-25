package notify

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// Dispatcher is the channel-fanout layer. Today it only knows about
// email; when SMS or push land, they slot in here too. The pattern:
//
//   d.AlarmDispositioned(ctx, eventCtx, recipients)
//   d.MonthlySummary(ctx, summaryCtx, recipients)
//
// Callers don't construct Message directly — they hand the dispatcher
// a structured context object and the dispatcher renders it. That
// keeps email copy in one place where a future template-versioning
// story has somewhere to live.

type Dispatcher struct {
	email       Mailer
	sms         Mailer
	productName string // BRAND.name analog from the backend (cfg.ProductName)
	publicURL   string // e.g. https://soc.example.com — used for clickable links in emails
}

// Recipient represents a destination for one notification. Each
// subscriber can have email + sms + push; the dispatcher fans out to
// every channel they have populated. Empty fields are skipped.
type Recipient struct {
	Email string // RFC 5322 mailbox; empty = skip email
	SMS   string // E.164 phone number; empty = skip SMS
}

func NewDispatcher(email, sms Mailer, productName, publicURL string) *Dispatcher {
	if productName == "" {
		productName = "Ironsight"
	}
	if publicURL == "" {
		// A reasonable default for compose-only deployments. Production
		// must override via NOTIFY_PUBLIC_URL so links in customer
		// emails point at the customer-visible hostname.
		publicURL = "http://localhost:3000"
	}
	return &Dispatcher{
		email:       email,
		sms:         sms,
		productName: productName,
		publicURL:   publicURL,
	}
}

// AlarmDispositionContext carries the data needed to render a
// disposition-summary email. Built by the API handler that just
// closed the alarm; passed verbatim to the dispatcher.
type AlarmDispositionContext struct {
	EventID          string // EVT-2026-0042
	AlarmCode        string // ALM-260425-0017 — optional
	SiteID           string // CS-547
	SiteName         string
	CameraName       string
	Severity         string
	DispositionLabel string
	OperatorCallsign string
	OperatorNotes    string
	AVSScore         int
	AVSLabel         string
	HappenedAt       time.Time
	IncidentURL      string // optional deep link into the portal
}

// AlarmDispositioned sends the disposition-summary notification to
// the supplied recipients on every channel they have populated.
// Best-effort: a partial-failure on one channel logs the error but
// doesn't propagate to the others — we'd rather get one out than
// none.
func (d *Dispatcher) AlarmDispositioned(ctx context.Context, ev AlarmDispositionContext, recipients []Recipient) {
	if len(recipients) == 0 {
		return
	}

	// Partition by channel. A recipient with both email AND sms is
	// in both lists and gets both.
	var emails, phones []string
	for _, r := range recipients {
		if r.Email != "" {
			emails = append(emails, r.Email)
		}
		if r.SMS != "" {
			phones = append(phones, r.SMS)
		}
	}

	subject := fmt.Sprintf("[%s] %s · %s — %s",
		d.productName, strings.ToUpper(ev.Severity), ev.SiteID, ev.DispositionLabel)

	url := ev.IncidentURL
	if url == "" {
		url = d.publicURL + "/portal/incidents"
	}

	text := strings.Builder{}
	fmt.Fprintf(&text, "%s monitoring report\n\n", d.productName)
	fmt.Fprintf(&text, "Event: %s\n", ev.EventID)
	if ev.AlarmCode != "" {
		fmt.Fprintf(&text, "Alarm code: %s\n", ev.AlarmCode)
	}
	fmt.Fprintf(&text, "Site: %s (%s)\n", ev.SiteName, ev.SiteID)
	fmt.Fprintf(&text, "Camera: %s\n", ev.CameraName)
	fmt.Fprintf(&text, "Severity: %s\n", strings.ToUpper(ev.Severity))
	fmt.Fprintf(&text, "When: %s UTC\n\n", ev.HappenedAt.UTC().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&text, "DISPOSITION: %s\n", ev.DispositionLabel)
	fmt.Fprintf(&text, "Operator: %s\n", ev.OperatorCallsign)
	if ev.OperatorNotes != "" {
		fmt.Fprintf(&text, "Notes: %s\n", ev.OperatorNotes)
	}
	if ev.AVSLabel != "" {
		fmt.Fprintf(&text, "TMA-AVS-01 score: %d (%s)\n", ev.AVSScore, ev.AVSLabel)
	}
	fmt.Fprintf(&text, "\nView the full incident: %s\n", url)
	fmt.Fprintf(&text, "\n— %s SOC\n", d.productName)

	html := strings.Builder{}
	html.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;max-width:560px;margin:0 auto;padding:24px;color:#1a1a1a">`)
	fmt.Fprintf(&html, `<h2 style="margin:0 0 16px;color:#0a0f08">%s monitoring report</h2>`, d.productName)
	html.WriteString(`<table style="border-collapse:collapse;width:100%;margin-bottom:20px">`)
	row := func(k, v string) {
		fmt.Fprintf(&html, `<tr><td style="padding:6px 0;color:#6b7280;font-size:13px;width:120px">%s</td><td style="padding:6px 0;color:#1a1a1a;font-size:14px">%s</td></tr>`, k, htmlEscape(v))
	}
	row("Event", ev.EventID)
	if ev.AlarmCode != "" {
		row("Alarm code", ev.AlarmCode)
	}
	row("Site", fmt.Sprintf("%s (%s)", ev.SiteName, ev.SiteID))
	row("Camera", ev.CameraName)
	row("Severity", strings.ToUpper(ev.Severity))
	row("When", ev.HappenedAt.UTC().Format("2006-01-02 15:04 UTC"))
	html.WriteString(`</table>`)
	fmt.Fprintf(&html, `<div style="background:#f3f4f6;padding:14px;border-radius:6px;margin-bottom:16px"><div style="font-size:11px;color:#6b7280;text-transform:uppercase;letter-spacing:0.5px;margin-bottom:6px">Disposition</div><div style="font-size:16px;font-weight:600">%s</div>`, htmlEscape(ev.DispositionLabel))
	fmt.Fprintf(&html, `<div style="font-size:13px;color:#4b5563;margin-top:8px">by %s</div>`, htmlEscape(ev.OperatorCallsign))
	if ev.OperatorNotes != "" {
		fmt.Fprintf(&html, `<div style="font-size:13px;margin-top:8px;font-style:italic">%s</div>`, htmlEscape(ev.OperatorNotes))
	}
	if ev.AVSLabel != "" {
		fmt.Fprintf(&html, `<div style="font-size:11px;color:#6b7280;margin-top:10px">TMA-AVS-01 score: <strong>%d (%s)</strong></div>`, ev.AVSScore, htmlEscape(ev.AVSLabel))
	}
	html.WriteString(`</div>`)
	fmt.Fprintf(&html, `<a href="%s" style="display:inline-block;padding:10px 20px;background:#E8732A;color:#fff;text-decoration:none;border-radius:5px;font-weight:600;font-size:14px">View incident</a>`, url)
	fmt.Fprintf(&html, `<div style="margin-top:24px;padding-top:16px;border-top:1px solid #e5e7eb;font-size:11px;color:#9ca3af">— %s SOC. To change which alerts you receive, visit your notification preferences in the portal.</div>`, d.productName)
	html.WriteString(`</div>`)

	// Email channel — full HTML + text bodies, clickable button.
	if len(emails) > 0 && d.email != nil {
		if err := d.email.Send(ctx, Message{
			To:       emails,
			Subject:  subject,
			TextBody: text.String(),
			HTMLBody: html.String(),
			Tag:      "alarm_disposition",
		}); err != nil {
			log.Printf("[NOTIFY] alarm_disposition email send failed (event=%s, n=%d): %v", ev.EventID, len(emails), err)
		}
	}

	// SMS channel — short, plain text. Twilio's body limit + SMS
	// concatenation cost dictates a brutally compact format. Lead
	// with severity and site so a phone-lock-screen preview tells
	// the recipient everything they need to know in one glance.
	if len(phones) > 0 && d.sms != nil {
		smsBody := fmt.Sprintf("[%s] %s at %s — %s. View: %s",
			d.productName, strings.ToUpper(ev.Severity), ev.SiteID, ev.DispositionLabel, url)
		if err := d.sms.Send(ctx, Message{
			To:       phones,
			Subject:  subject, // unused by SMS but harmless
			TextBody: smsBody,
			Tag:      "alarm_disposition",
		}); err != nil {
			log.Printf("[NOTIFY] alarm_disposition sms send failed (event=%s, n=%d): %v", ev.EventID, len(phones), err)
		}
	}
}

// htmlEscape is the smallest correct HTML-escape we can ship — covers
// the four characters that change document semantics. We don't need
// the full html package's escape table because the email template
// renders only short, operator-supplied strings (no marketing copy
// with smart quotes etc.).
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
