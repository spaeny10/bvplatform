package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NotificationSubscription is one user's opt-in for one channel + one
// event type. The unique constraint (user_id, channel, event_type)
// means a single row per cell — toggling email-on-disposition is a
// PUT against the existing row, not a delete + create.
type NotificationSubscription struct {
	ID          int64     `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Channel     string    `json:"channel"`     // "email" | "sms"
	EventType   string    `json:"event_type"`  // "alarm_disposition" | "monthly_summary"
	SeverityMin string    `json:"severity_min"` // "low" | "medium" | "high" | "critical"
	SiteIDs     []string  `json:"site_ids,omitempty"` // nil = all visible sites
	QuietStart  string    `json:"quiet_start,omitempty"` // "HH:MM" UTC; "" = no quiet hours
	QuietEnd    string    `json:"quiet_end,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListNotificationSubscriptions returns every subscription for one
// user. Used by the customer portal's notification-preferences page
// — they see the full grid (channel x event_type) and toggle from
// there.
func (db *DB) ListNotificationSubscriptions(ctx context.Context, userID uuid.UUID) ([]NotificationSubscription, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, user_id, channel, event_type, severity_min,
		       site_ids, COALESCE(quiet_start,''), COALESCE(quiet_end,''),
		       enabled, created_at, updated_at
		FROM notification_subscriptions
		WHERE user_id = $1
		ORDER BY event_type, channel`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationSubscription
	for rows.Next() {
		var s NotificationSubscription
		var siteIDsJSON []byte
		if err := rows.Scan(&s.ID, &s.UserID, &s.Channel, &s.EventType, &s.SeverityMin,
			&siteIDsJSON, &s.QuietStart, &s.QuietEnd,
			&s.Enabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if len(siteIDsJSON) > 0 {
			_ = json.Unmarshal(siteIDsJSON, &s.SiteIDs)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpsertNotificationSubscription creates or updates a single
// (user, channel, event_type) row. The unique constraint makes this
// idempotent — repeated calls with the same key just refresh the
// other fields.
func (db *DB) UpsertNotificationSubscription(ctx context.Context, s *NotificationSubscription) error {
	siteIDsJSON, err := json.Marshal(s.SiteIDs)
	if err != nil {
		return fmt.Errorf("marshal site_ids: %w", err)
	}
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO notification_subscriptions
		  (user_id, channel, event_type, severity_min, site_ids, quiet_start, quiet_end, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, channel, event_type) DO UPDATE SET
		  severity_min = EXCLUDED.severity_min,
		  site_ids     = EXCLUDED.site_ids,
		  quiet_start  = EXCLUDED.quiet_start,
		  quiet_end    = EXCLUDED.quiet_end,
		  enabled      = EXCLUDED.enabled,
		  updated_at   = NOW()`,
		s.UserID, s.Channel, s.EventType, s.SeverityMin, siteIDsJSON,
		s.QuietStart, s.QuietEnd, s.Enabled,
	)
	return err
}

// AlarmRecipient is one user's resolved (email, sms) pair for sending
// to. Returned by MatchAlarmRecipients after the channel/severity/
// site/quiet-hours filter resolves who actually gets pinged for a
// given event.
type AlarmRecipient struct {
	UserID uuid.UUID
	Email  string // empty if user isn't subscribed via email
	SMS    string // empty if user isn't subscribed via SMS
}

// severityRank maps severity strings to a comparable integer. Used by
// the severity_min gate — a subscription with severity_min='high'
// gets pinged on high + critical, skipped on medium + low.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// withinQuietHours returns true when the supplied UTC moment falls
// inside the [start, end) HH:MM window. Empty start = no quiet hours.
// Wrap-around windows (e.g. 22:00–06:00) are supported.
func withinQuietHours(now time.Time, start, end string) bool {
	if start == "" || end == "" {
		return false
	}
	hhmm := now.UTC().Format("15:04")
	if start <= end {
		return hhmm >= start && hhmm < end
	}
	// Wrap: e.g. start=22:00, end=06:00 — inside if either side matches.
	return hhmm >= start || hhmm < end
}

// MatchAlarmRecipients resolves which subscribers should be notified
// for an alarm at the given site with the given severity. Joins
// notification_subscriptions with users to get the email/phone, then
// filters by (a) enabled, (b) site scope (NULL site_ids = all sites
// visible to user; non-NULL = explicit list must contain this site),
// (c) severity_min, (d) quiet hours. Aggregates per-user across
// channels so the dispatcher gets one Recipient with both email and
// SMS populated when both are subscribed.
//
// The user's organization scope is enforced upstream: a customer of
// org A doesn't get notified about org B's events because the alarm
// flow only invokes this with org-scoped events. Site-list filter
// here is the user's *additional* preference within their own org.
func (db *DB) MatchAlarmRecipients(ctx context.Context, siteID, severity string, now time.Time) ([]AlarmRecipient, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT s.user_id, s.channel, s.severity_min, s.site_ids,
		       COALESCE(s.quiet_start,''), COALESCE(s.quiet_end,''),
		       COALESCE(u.email,''), COALESCE(u.phone,'')
		FROM notification_subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE s.enabled = true
		  AND s.event_type = 'alarm_disposition'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	want := severityRank[severity]
	if want == 0 {
		want = 1 // unknown severity: treat as 'low' so all subscribers get it
	}

	merged := map[uuid.UUID]*AlarmRecipient{}
	for rows.Next() {
		var (
			uid          uuid.UUID
			channel      string
			minSev       string
			siteIDsJSON  []byte
			qStart, qEnd string
			email, phone string
		)
		if err := rows.Scan(&uid, &channel, &minSev, &siteIDsJSON, &qStart, &qEnd, &email, &phone); err != nil {
			return nil, err
		}

		// (b) site scope
		if len(siteIDsJSON) > 0 && string(siteIDsJSON) != "null" {
			var sites []string
			_ = json.Unmarshal(siteIDsJSON, &sites)
			if len(sites) > 0 {
				ok := false
				for _, s := range sites {
					if s == siteID {
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
			}
		}
		// (c) severity_min
		if severityRank[minSev] > want {
			continue
		}
		// (d) quiet hours
		if withinQuietHours(now, qStart, qEnd) {
			continue
		}

		r := merged[uid]
		if r == nil {
			r = &AlarmRecipient{UserID: uid}
			merged[uid] = r
		}
		switch channel {
		case "email":
			if email != "" {
				r.Email = email
			}
		case "sms":
			if phone != "" {
				r.SMS = phone
			}
		}
	}

	out := make([]AlarmRecipient, 0, len(merged))
	for _, r := range merged {
		// Skip recipients with no usable channel — a subscription
		// without an email or phone on the user record is a no-op.
		if r.Email == "" && r.SMS == "" {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

// MatchMonthlySummaryRecipients returns the email-only list of users
// subscribed to the monthly summary for any of the supplied sites.
// SMS doesn't make sense for a multi-paragraph report; we hardcode
// the channel to email here.
func (db *DB) MatchMonthlySummaryRecipients(ctx context.Context, siteIDs []string) ([]AlarmRecipient, error) {
	if len(siteIDs) == 0 {
		return nil, nil
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT s.user_id, COALESCE(u.email,'')
		FROM notification_subscriptions s
		JOIN users u ON u.id = s.user_id
		WHERE s.enabled = true
		  AND s.event_type = 'monthly_summary'
		  AND s.channel = 'email'
		  AND COALESCE(u.email,'') <> ''
		  AND (
		    s.site_ids IS NULL
		    OR s.site_ids = 'null'::jsonb
		    OR (jsonb_typeof(s.site_ids) = 'array' AND s.site_ids ?| $1)
		  )`,
		siteIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlarmRecipient
	for rows.Next() {
		var r AlarmRecipient
		if err := rows.Scan(&r.UserID, &r.Email); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
