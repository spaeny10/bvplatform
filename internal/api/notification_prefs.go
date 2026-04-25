package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"onvif-tool/internal/auth"
	"onvif-tool/internal/database"
)

// HandleListMyNotificationSubs returns the authenticated user's
// notification subscriptions. The portal's notification-preferences
// page renders this as a grid of (event_type x channel) cells with
// toggles. Customers with no subscriptions yet see the empty state
// and can click "default = email me on every alarm" to opt in.
func HandleListMyNotificationSubs(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}
		subs, err := db.ListNotificationSubscriptions(r.Context(), userID)
		if err != nil {
			http.Error(w, "list failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if subs == nil {
			subs = []database.NotificationSubscription{}
		}
		writeJSON(w, subs)
	}
}

// upsertSubRequest is the JSON shape the portal POSTs/PUTs. The
// channel + event_type are the unique key — sending {channel:"email",
// event_type:"alarm_disposition", enabled:false} effectively
// unsubscribes the user from that combination without deleting the
// row, so their other preferences (severity_min, quiet hours)
// persist for the next time they re-enable.
type upsertSubRequest struct {
	Channel     string   `json:"channel"`
	EventType   string   `json:"event_type"`
	SeverityMin string   `json:"severity_min"`
	SiteIDs     []string `json:"site_ids"`
	QuietStart  string   `json:"quiet_start"`
	QuietEnd    string   `json:"quiet_end"`
	Enabled     bool     `json:"enabled"`
}

// HandleUpsertMyNotificationSub creates-or-updates a single
// subscription for the authenticated user. The user_id is always
// taken from the JWT — the request body cannot specify it. This
// prevents a malicious caller from editing someone else's
// preferences.
func HandleUpsertMyNotificationSub(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}
		var req upsertSubRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		// Validate channel + event_type against the small enums the
		// backend supports. Rejecting unknown values now means a future
		// frontend bug can't silently write garbage rows.
		switch req.Channel {
		case "email", "sms":
		default:
			http.Error(w, "channel must be email or sms", http.StatusBadRequest)
			return
		}
		switch req.EventType {
		case "alarm_disposition", "monthly_summary":
		default:
			http.Error(w, "event_type must be alarm_disposition or monthly_summary", http.StatusBadRequest)
			return
		}
		switch req.SeverityMin {
		case "", "low", "medium", "high", "critical":
		default:
			http.Error(w, "severity_min must be low|medium|high|critical", http.StatusBadRequest)
			return
		}
		if req.SeverityMin == "" {
			req.SeverityMin = "low"
		}

		err = db.UpsertNotificationSubscription(r.Context(), &database.NotificationSubscription{
			UserID:      userID,
			Channel:     req.Channel,
			EventType:   req.EventType,
			SeverityMin: req.SeverityMin,
			SiteIDs:     req.SiteIDs,
			QuietStart:  req.QuietStart,
			QuietEnd:    req.QuietEnd,
			Enabled:     req.Enabled,
		})
		if err != nil {
			http.Error(w, "upsert failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
