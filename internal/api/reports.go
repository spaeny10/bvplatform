package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"onvif-tool/internal/database"
)

// HandleSLAReport returns the operator response-time report. Two query
// parameters control the time window — `from` and `to`, both RFC3339;
// missing values default to "the last 30 days." `group=operator|day`
// chooses the bucketing dimension (default day). `format=csv` emits
// CSV instead of JSON for direct download.
//
// This is the report a UL 827B reviewer typically asks for first
// when probing the audit trail: "what was your 95th-percentile ack
// time last quarter, broken out by operator?" The answer should be
// one HTTP call away, not a SQL session.
func HandleSLAReport(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		from := now.AddDate(0, 0, -30)
		to := now

		if v := r.URL.Query().Get("from"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				from = t
			} else {
				http.Error(w, "invalid from (expected RFC3339)", http.StatusBadRequest)
				return
			}
		}
		if v := r.URL.Query().Get("to"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				to = t
			} else {
				http.Error(w, "invalid to (expected RFC3339)", http.StatusBadRequest)
				return
			}
		}
		if !from.Before(to) {
			http.Error(w, "from must be before to", http.StatusBadRequest)
			return
		}

		group := r.URL.Query().Get("group")
		if group == "" {
			group = "day"
		}

		rows, err := db.GetSLAReport(r.Context(), from, to, group)
		if err != nil {
			http.Error(w, "report query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if rows == nil {
			rows = []database.SLAReportRow{}
		}

		if r.URL.Query().Get("format") == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf(
				`attachment; filename="sla_report_%s_to_%s_by_%s.csv"`,
				from.Format("20060102"), to.Format("20060102"), group,
			))
			cw := csv.NewWriter(w)
			_ = cw.Write([]string{
				"bucket", "total_alarms", "acked_alarms",
				"within_sla", "over_sla",
				"avg_ack_sec", "p50_ack_sec", "p95_ack_sec",
			})
			for _, row := range rows {
				_ = cw.Write([]string{
					row.Bucket,
					strconv.Itoa(row.TotalAlarms),
					strconv.Itoa(row.AckedAlarms),
					strconv.Itoa(row.WithinSLA),
					strconv.Itoa(row.OverSLA),
					formatFloat(row.AvgAckSec),
					formatFloat(row.P50AckSec),
					formatFloat(row.P95AckSec),
				})
			}
			cw.Flush()
			return
		}

		writeJSON(w, map[string]interface{}{
			"from":  from.Format(time.RFC3339),
			"to":    to.Format(time.RFC3339),
			"group": group,
			"rows":  rows,
		})
	}
}

// formatFloat trims gratuitous trailing zeroes — the SLA report works
// in seconds with millisecond precision, but rendering "12.5" reads
// better than "12.500000" in a spreadsheet.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 3, 64)
}
