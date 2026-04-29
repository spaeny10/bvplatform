package api

import (
	"net/http"
	"strconv"
	"time"

	"onvif-tool/internal/database"
)

// HandlePortalSummary returns a customer-scoped rollup over the last
// N days (default 7, capped at 90). Reuses MonthlyOrgSummary so the
// in-portal numbers match the monthly email exactly.
func HandlePortalSummary(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// SOC roles may spectate via ?org=; customers are pinned to their own.
		orgID := claims.OrganizationID
		if globalViewRoles[claims.Role] && r.URL.Query().Get("org") != "" {
			orgID = r.URL.Query().Get("org")
		}
		if orgID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}

		days := 7
		if v := r.URL.Query().Get("days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				if n > 90 {
					n = 90
				}
				days = n
			}
		}
		end := time.Now()
		start := end.Add(-time.Duration(days) * 24 * time.Hour)

		summary, err := db.MonthlyOrgSummary(r.Context(), orgID, start, end)
		if err != nil {
			http.Error(w, "summary build failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, map[string]interface{}{
			"period_days":         days,
			"period_start":        summary.PeriodStart,
			"period_end":          summary.PeriodEnd,
			"sites":               summary.SiteCount,
			"cameras":             summary.CameraCount,
			"events_handled":      summary.DispositionCount,
			"verified_threats":    summary.VerifiedThreats,
			"false_positives":     summary.FalsePositives,
			"alarms_total":        summary.AlarmCount,
			"avg_response_sec":    summary.AvgAckSec,
			"p95_response_sec":    summary.P95AckSec,
			"within_sla":          summary.WithinSLA,
			"over_sla":            summary.OverSLA,
			"top_events":          summary.TopEvents,
		})
	}
}
