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
//
// AI-* fields come from the Qwen Vision-Language Model pipeline that
// already runs on every alarm. Surfacing the model's narrative and
// threat assessment in the customer-facing notification is the
// difference between a good email ("person_detected at TS-100001")
// and an actually informative one ("Subject in dark clothing
// approached the loading-dock fence and attempted to scale it.
// Threat level: high. Recommended: dispatch police."). When the AI
// fields are empty (Qwen unavailable, alarm pre-dates indexer rollout)
// the dispatcher falls back gracefully to the operator-supplied
// description.
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

	// AI-generated narrative + assessment from the Qwen VLM pipeline.
	// All fields are best-effort — empty when the indexer hasn't
	// produced output yet for this incident.
	AIDescription       string  // 1–2 sentence factual scene description
	AIThreatLevel       string  // "low" | "medium" | "high" | "critical"
	AIRecommendedAction string  // operator-facing recommendation, fine for customers too
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
	fmt.Fprintf(&text, "When: %s UTC\n", ev.HappenedAt.UTC().Format("2006-01-02 15:04:05"))

	// VLM narrative section. Lead with what happened (description),
	// then the model's threat assessment and recommendation. A
	// customer skimming this email on their phone reads the
	// description first and immediately knows "is this serious?"
	// without parsing operator codes.
	if ev.AIDescription != "" || ev.AIThreatLevel != "" {
		text.WriteString("\nWhat the AI saw:\n")
		if ev.AIDescription != "" {
			fmt.Fprintf(&text, "  %s\n", ev.AIDescription)
		}
		if ev.AIThreatLevel != "" {
			fmt.Fprintf(&text, "  Threat assessment: %s\n", strings.ToUpper(ev.AIThreatLevel))
		}
		if ev.AIRecommendedAction != "" {
			fmt.Fprintf(&text, "  Recommended action: %s\n", ev.AIRecommendedAction)
		}
	}

	text.WriteString("\n")
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

	// VLM narrative card — visually distinct so the customer's eye
	// reads it before the operator's structured fields. The threat
	// pill is color-coded (low → green, medium → amber, high → orange,
	// critical → red) using inline styles since email clients strip
	// <style> blocks.
	if ev.AIDescription != "" || ev.AIThreatLevel != "" {
		html.WriteString(`<div style="background:#0f172a;color:#e2e8f0;padding:14px 16px;border-radius:6px;margin-bottom:16px;border-left:3px solid #3b82f6">`)
		html.WriteString(`<div style="font-size:10px;color:#94a3b8;text-transform:uppercase;letter-spacing:0.6px;margin-bottom:6px">AI Vision Assessment</div>`)
		if ev.AIDescription != "" {
			fmt.Fprintf(&html, `<div style="font-size:14px;line-height:1.5;margin-bottom:8px">%s</div>`, htmlEscape(ev.AIDescription))
		}
		if ev.AIThreatLevel != "" {
			pillBg, pillFg := threatPillColors(ev.AIThreatLevel)
			fmt.Fprintf(&html,
				`<div style="display:inline-block;padding:3px 10px;border-radius:10px;background:%s;color:%s;font-size:10px;font-weight:700;letter-spacing:0.6px">THREAT: %s</div>`,
				pillBg, pillFg, strings.ToUpper(ev.AIThreatLevel))
		}
		if ev.AIRecommendedAction != "" {
			fmt.Fprintf(&html,
				`<div style="font-size:12px;color:#cbd5e1;margin-top:8px"><strong>Recommended:</strong> %s</div>`,
				htmlEscape(ev.AIRecommendedAction))
		}
		html.WriteString(`</div>`)
	}

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
	// concatenation cost dictates a brutally compact format. The
	// lock-screen preview is the most-read part of any SMS, so the
	// first 80 characters earn their keep. When the VLM has a
	// description we lead with its first sentence — that's what
	// tells the customer "what's actually happening" in plain
	// English. The disposition label is the fallback for events
	// without AI enrichment.
	if len(phones) > 0 && d.sms != nil {
		summary := firstSentence(ev.AIDescription, 140)
		if summary == "" {
			summary = ev.DispositionLabel
		}
		smsBody := fmt.Sprintf("[%s] %s · %s — %s View: %s",
			d.productName, strings.ToUpper(ev.Severity), ev.SiteID, summary, url)
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

// MonthlySummaryContext is the rendering payload for the auto-emailed
// monthly summary. Built by the worker job from the per-org rollup
// query; rendered into both HTML (rich layout for desktop email) and
// text (plain fallback for legacy clients and CLI MUAs).
type MonthlySummaryContext struct {
	OrganizationName string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	SiteCount        int
	CameraCount      int
	IncidentCount    int
	AlarmCount       int
	DispositionCount int
	VerifiedThreats  int
	FalsePositives   int
	AvgAckSec        float64
	P95AckSec        float64
	WithinSLA        int
	OverSLA          int
	TopEvents        []MonthlyTopEvent
	PortalURL        string
}

// MonthlyTopEvent mirrors the database type so the notify package
// doesn't need to import internal/database — keeps the layering
// clean.
type MonthlyTopEvent struct {
	EventID          string
	SiteName         string
	CameraName       string
	Severity         string
	HappenedAt       time.Time
	DispositionLabel string
	AIDescription    string
	AVSScore         int
}

// MonthlySummary emails the per-org rollup to all subscribed
// recipients. Best-effort across channels; logs any send error but
// doesn't propagate (one customer's flaky email shouldn't block
// the next org's summary).
func (d *Dispatcher) MonthlySummary(ctx context.Context, sum MonthlySummaryContext, recipients []Recipient) {
	if len(recipients) == 0 {
		return
	}
	var emails []string
	for _, r := range recipients {
		if r.Email != "" {
			emails = append(emails, r.Email)
		}
	}
	if len(emails) == 0 {
		return // monthly summary is email-only, no fallback
	}

	period := fmt.Sprintf("%s %d", sum.PeriodStart.Format("January"), sum.PeriodStart.Year())
	subject := fmt.Sprintf("[%s] %s monitoring summary — %s", d.productName, sum.OrganizationName, period)
	url := sum.PortalURL
	if url == "" {
		url = d.publicURL + "/portal"
	}

	verifiedPct := 0
	if sum.DispositionCount > 0 {
		verifiedPct = (sum.VerifiedThreats * 100) / sum.DispositionCount
	}

	// Plain text body — readable in any email client + the audit log.
	text := strings.Builder{}
	fmt.Fprintf(&text, "%s monitoring summary for %s\nPeriod: %s\n\n",
		d.productName, sum.OrganizationName, period)
	fmt.Fprintf(&text, "AT A GLANCE\n  %d sites · %d cameras under monitoring\n  %d alarms received this month\n  %d operator dispositions (%d verified threats, %d false positives)\n",
		sum.SiteCount, sum.CameraCount, sum.AlarmCount,
		sum.DispositionCount, sum.VerifiedThreats, sum.FalsePositives)
	if sum.AlarmCount > 0 {
		fmt.Fprintf(&text, "\nRESPONSE TIMES\n  Average ack: %ds · 95th percentile: %ds\n  %d alarms within SLA · %d over\n",
			int(sum.AvgAckSec), int(sum.P95AckSec), sum.WithinSLA, sum.OverSLA)
	}
	if len(sum.TopEvents) > 0 {
		text.WriteString("\nNOTABLE EVENTS\n")
		for _, ev := range sum.TopEvents {
			line := ev.AIDescription
			if line == "" {
				line = ev.DispositionLabel
			}
			fmt.Fprintf(&text, "  · %s — %s · %s\n    %s\n",
				ev.HappenedAt.Format("Jan 2 15:04"), strings.ToUpper(ev.Severity), ev.SiteName, line)
		}
	}
	fmt.Fprintf(&text, "\nView the full portal: %s\n\n— %s SOC\n", url, d.productName)

	// HTML body — richer rendering for the in-browser case.
	html := strings.Builder{}
	html.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;max-width:600px;margin:0 auto;padding:24px;color:#1a1a1a">`)
	fmt.Fprintf(&html, `<h2 style="margin:0 0 4px">%s monitoring summary</h2>`, d.productName)
	fmt.Fprintf(&html, `<div style="font-size:14px;color:#6b7280;margin-bottom:24px">%s &middot; %s</div>`, htmlEscape(sum.OrganizationName), period)

	html.WriteString(`<div style="display:flex;gap:8px;flex-wrap:wrap;margin-bottom:20px">`)
	statTile(&html, "Alarms", fmt.Sprintf("%d", sum.AlarmCount), "")
	statTile(&html, "Dispositions", fmt.Sprintf("%d", sum.DispositionCount), fmt.Sprintf("%d%% verified threat", verifiedPct))
	statTile(&html, "Cameras", fmt.Sprintf("%d", sum.CameraCount), fmt.Sprintf("%d sites", sum.SiteCount))
	if sum.AlarmCount > 0 {
		statTile(&html, "P95 ack", fmt.Sprintf("%ds", int(sum.P95AckSec)), fmt.Sprintf("%d/%d within SLA", sum.WithinSLA, sum.WithinSLA+sum.OverSLA))
	}
	html.WriteString(`</div>`)

	if len(sum.TopEvents) > 0 {
		html.WriteString(`<h3 style="font-size:14px;margin:24px 0 10px;color:#0a0f08">Notable events</h3>`)
		for _, ev := range sum.TopEvents {
			line := ev.AIDescription
			if line == "" {
				line = ev.DispositionLabel
			}
			html.WriteString(`<div style="padding:12px;border:1px solid #e5e7eb;border-radius:6px;margin-bottom:8px">`)
			fmt.Fprintf(&html, `<div style="font-size:11px;color:#6b7280;margin-bottom:4px">%s &middot; %s &middot; %s</div>`,
				ev.HappenedAt.Format("Jan 2 15:04"), strings.ToUpper(ev.Severity), htmlEscape(ev.SiteName))
			fmt.Fprintf(&html, `<div style="font-size:13px;line-height:1.4">%s</div>`, htmlEscape(line))
			html.WriteString(`</div>`)
		}
	}

	fmt.Fprintf(&html, `<a href="%s" style="display:inline-block;margin-top:16px;padding:10px 20px;background:#E8732A;color:#fff;text-decoration:none;border-radius:5px;font-weight:600;font-size:14px">View portal</a>`, url)
	fmt.Fprintf(&html, `<div style="margin-top:24px;padding-top:16px;border-top:1px solid #e5e7eb;font-size:11px;color:#9ca3af">— %s SOC. To stop receiving the monthly summary, visit your notification preferences in the portal.</div>`, d.productName)
	html.WriteString(`</div>`)

	if err := d.email.Send(ctx, Message{
		To:       emails,
		Subject:  subject,
		TextBody: text.String(),
		HTMLBody: html.String(),
		Tag:      "monthly_summary",
	}); err != nil {
		log.Printf("[NOTIFY] monthly_summary send failed (org=%s, n=%d): %v", sum.OrganizationName, len(emails), err)
	}
}

func statTile(b *strings.Builder, label, value, sub string) {
	fmt.Fprintf(b, `<div style="flex:1;min-width:120px;padding:12px;background:#f9fafb;border-radius:6px;border:1px solid #e5e7eb">`)
	fmt.Fprintf(b, `<div style="font-size:10px;color:#6b7280;text-transform:uppercase;letter-spacing:0.5px;margin-bottom:4px">%s</div>`, label)
	fmt.Fprintf(b, `<div style="font-size:22px;font-weight:700;color:#0a0f08">%s</div>`, value)
	if sub != "" {
		fmt.Fprintf(b, `<div style="font-size:11px;color:#6b7280;margin-top:2px">%s</div>`, sub)
	}
	b.WriteString(`</div>`)
}

// firstSentence returns the first sentence of s, capped at maxLen.
// "Sentence" is "everything up to the first . ! or ? followed by a
// space"; if no boundary exists within the cap we hard-truncate at
// the last word boundary <= maxLen and append an ellipsis. Used to
// turn a multi-sentence VLM description into a one-line SMS body
// without cutting words in half ("Subject in dark hooded clothi…").
func firstSentence(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Look for a sentence terminator within bounds.
	cut := -1
	for i := 0; i < len(s)-1 && i < maxLen; i++ {
		c := s[i]
		if (c == '.' || c == '!' || c == '?') && (s[i+1] == ' ' || i == len(s)-2) {
			cut = i + 1
			break
		}
	}
	if cut > 0 {
		return strings.TrimSpace(s[:cut])
	}
	if len(s) <= maxLen {
		return s
	}
	// Hard-truncate at the last word boundary so we don't slice in
	// the middle of a word. Reserve 1 char for the ellipsis.
	trimmed := s[:maxLen-1]
	if sp := strings.LastIndexByte(trimmed, ' '); sp > 0 {
		trimmed = trimmed[:sp]
	}
	return trimmed + "…"
}

// threatPillColors picks (background, foreground) pairs for the AI
// threat-level pill rendered in the email body. Tuned for legibility
// on a white email background; values are inline-styled because most
// email clients drop <style> blocks.
func threatPillColors(level string) (string, string) {
	switch strings.ToLower(level) {
	case "critical":
		return "#dc2626", "#ffffff"
	case "high":
		return "#ea580c", "#ffffff"
	case "medium":
		return "#ca8a04", "#ffffff"
	case "low":
		return "#16a34a", "#ffffff"
	default:
		return "#475569", "#ffffff"
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
