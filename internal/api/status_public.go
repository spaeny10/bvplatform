package api

import (
	"context"
	"net/http"
	"time"

	"ironsight/internal/database"
)

// HandlePublicStatus is the GET /status endpoint that backs a
// customer-visible service-status page. Unauthenticated by design —
// trust signals matter most at the moment a customer is worried,
// which is exactly when they don't want to log in to find out
// "is everything OK?".
//
// Deliberately returns aggregates only, never anything that leaks
// per-customer data: total camera count vs camera-online count, total
// alarm volume in the last hour, last-incident-disposition timestamp.
// Nothing here would help a competitor or attacker.
//
// The endpoint is cheap: three simple aggregates that hit indexed
// columns. Response cached at the CDN tier in production via a
// short Cache-Control header (60s) so a panicky refresh frenzy
// doesn't pile up DB queries.
func HandlePublicStatus(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var (
			totalCameras   int
			onlineCameras  int
			alarmsLastHour int
			lastDispoTs    *time.Time
		)
		// Camera totals — ignore site_id filter; this is platform-wide.
		_ = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*),
			       COUNT(*) FILTER (WHERE status = 'online')
			FROM cameras`,
		).Scan(&totalCameras, &onlineCameras)

		// Active-alarm volume in the last 60 minutes — proxy for "is
		// the SOC seeing traffic." Zero for an hour might indicate
		// the detection pipeline is down.
		_ = db.Pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM active_alarms
			WHERE ts >= $1`,
			time.Now().Add(-time.Hour).UnixMilli(),
		).Scan(&alarmsLastHour)

		// Most recent dispositioned event — proves operators are still
		// closing alarms. A long gap here is the loudest "something is
		// wrong" signal we can surface without per-customer detail.
		var lastDispoMillis int64
		_ = db.Pool.QueryRow(ctx, `
			SELECT COALESCE(MAX(resolved_at), 0) FROM security_events`,
		).Scan(&lastDispoMillis)
		if lastDispoMillis > 0 {
			t := time.UnixMilli(lastDispoMillis).UTC()
			lastDispoTs = &t
		}

		// SOC active = at least one operator with status='available' or
		// 'busy' in the last 30 minutes. A SOC with no live operators
		// for half an hour is a SOC the customer should worry about.
		var socActive bool
		_ = db.Pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM operators
				WHERE status IN ('available', 'busy')
				  AND last_active >= $1
			)`,
			time.Now().Add(-30*time.Minute).UnixMilli(),
		).Scan(&socActive)

		w.Header().Set("Cache-Control", "public, max-age=60")
		writeJSON(w, map[string]interface{}{
			"status":           overallStatus(socActive, totalCameras, onlineCameras),
			"soc_active":       socActive,
			"cameras_total":    totalCameras,
			"cameras_online":   onlineCameras,
			"alarms_last_hour": alarmsLastHour,
			"last_disposition": lastDispoTs,
			"as_of":            time.Now().UTC(),
		})
	}
}

// overallStatus rolls the per-subsystem signals into one of three
// strings the frontend renders as the headline indicator:
//
//	"operational" → all green
//	"degraded"    → partial outage
//	"critical"    → major service impact
//
// Conservative thresholds: a single offline camera doesn't degrade,
// but >25% offline does. SOC-not-active immediately flips degraded
// even if cameras look fine.
func overallStatus(socActive bool, total, online int) string {
	if !socActive {
		return "degraded"
	}
	if total == 0 {
		return "operational"
	}
	pctOffline := float64(total-online) / float64(total)
	switch {
	case pctOffline >= 0.5:
		return "critical"
	case pctOffline >= 0.25:
		return "degraded"
	default:
		return "operational"
	}
}
