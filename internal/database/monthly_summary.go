package database

import (
	"context"
	"time"
)

// MonthlySummary is the per-organization rollup the worker emails
// to customer-role users on the 1st of each month. Built by
// MonthlyOrgSummary; rendered by the notify package.
type MonthlySummary struct {
	OrganizationID   string
	OrganizationName string
	PeriodStart      time.Time
	PeriodEnd        time.Time

	SiteCount         int
	CameraCount       int
	IncidentCount     int
	AlarmCount        int
	DispositionCount  int
	VerifiedThreats   int
	FalsePositives    int

	// Response-time metrics for alarms generated at this org's sites,
	// pulled from the same SLA logic as the supervisor dashboard.
	AvgAckSec   float64
	P95AckSec   float64
	WithinSLA   int
	OverSLA     int

	// Top events: at most 5, surfaced in the email body so the
	// customer reads concrete examples instead of just abstract
	// numbers. AI description is preferred over the type code.
	TopEvents []MonthlyTopEvent
}

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

// ListOrganizationsWithEmail returns the (id, name) of every
// organization that has at least one customer-role user with an
// email address. The monthly worker iterates over this; orgs with
// no recipients are skipped entirely so we don't waste cycles
// composing a report nobody will read.
func (db *DB) ListOrganizationsWithEmail(ctx context.Context) ([]Organization, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT DISTINCT o.id, o.name, COALESCE(o.plan,''), COALESCE(o.contact_name,''),
		       COALESCE(o.contact_email,''), COALESCE(o.logo_url,''), o.created_at
		FROM organizations o
		JOIN users u ON u.organization_id = o.id
		WHERE u.role IN ('customer','site_manager')
		  AND COALESCE(u.email,'') <> ''
		ORDER BY o.name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Plan, &o.ContactName, &o.ContactEmail, &o.LogoURL, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// MonthlyOrgSummary builds the rollup for one organization across
// the supplied [start, end) window. Five queries: site/camera counts,
// security_events aggregates, response-time aggregates, top events.
// All deliberately scoped to org_id so a customer's report never
// includes another org's data.
func (db *DB) MonthlyOrgSummary(ctx context.Context, orgID string, start, end time.Time) (*MonthlySummary, error) {
	s := MonthlySummary{
		OrganizationID: orgID,
		PeriodStart:    start,
		PeriodEnd:      end,
	}

	// Org name (best-effort — the caller may have it but we double-check).
	_ = db.Pool.QueryRow(ctx, `SELECT name FROM organizations WHERE id=$1`, orgID).Scan(&s.OrganizationName)

	// Site + camera counts.
	_ = db.Pool.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM sites WHERE organization_id=$1),
		       (SELECT COUNT(*) FROM cameras c JOIN sites s ON c.site_id=s.id WHERE s.organization_id=$1)`,
		orgID,
	).Scan(&s.SiteCount, &s.CameraCount)

	// security_events aggregates: total dispositioned, verified
	// threats vs false positives. The disposition_code prefix
	// distinguishes the two; the schema already follows this naming.
	_ = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE disposition_code LIKE 'verified%' OR disposition_code = 'verified-threat'),
		       COUNT(*) FILTER (WHERE disposition_code LIKE 'false-positive%' OR disposition_code = 'false-positive')
		FROM security_events e
		JOIN sites s ON e.site_id = s.id
		WHERE s.organization_id = $1
		  AND e.ts >= $2 AND e.ts < $3`,
		orgID, start.UnixMilli(), end.UnixMilli(),
	).Scan(&s.DispositionCount, &s.VerifiedThreats, &s.FalsePositives)

	// active_alarms aggregates over the same window.
	_ = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COALESCE(AVG(EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0))))
		           FILTER (WHERE acknowledged_at IS NOT NULL), 0),
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (
		           ORDER BY EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0)))
		       ) FILTER (WHERE acknowledged_at IS NOT NULL), 0),
		       COUNT(*) FILTER (WHERE acknowledged_at IS NOT NULL
		           AND EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0))) * 1000 <= sla_deadline_ms),
		       COUNT(*) FILTER (WHERE acknowledged_at IS NOT NULL
		           AND EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0))) * 1000 > sla_deadline_ms)
		FROM active_alarms a
		JOIN sites s ON a.site_id = s.id
		WHERE s.organization_id = $1
		  AND a.ts >= $2 AND a.ts < $3`,
		orgID, start.UnixMilli(), end.UnixMilli(),
	).Scan(&s.AlarmCount, &s.AvgAckSec, &s.P95AckSec, &s.WithinSLA, &s.OverSLA)

	// Distinct incidents in the window (each represents a clustered
	// alarm sequence).
	_ = db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM incidents i
		JOIN sites s ON i.site_id = s.id
		WHERE s.organization_id = $1
		  AND i.first_alarm_ts >= $2 AND i.first_alarm_ts < $3`,
		orgID, start.UnixMilli(), end.UnixMilli(),
	).Scan(&s.IncidentCount)

	// Top 5 events ranked by AVS score (most actionable first), with
	// AI description joined from the originating alarm where possible.
	rows, err := db.Pool.Query(ctx, `
		SELECT e.id, COALESCE(s.name, e.site_id), COALESCE(c.name, e.camera_id),
		       COALESCE(e.severity,'medium'), e.resolved_at,
		       COALESCE(e.disposition_label,''), COALESCE(a.ai_description,''),
		       COALESCE(e.avs_score, 0)
		FROM security_events e
		LEFT JOIN sites s   ON e.site_id = s.id
		LEFT JOIN cameras c ON e.camera_id::text = c.id::text
		LEFT JOIN active_alarms a ON a.id = e.alarm_id
		WHERE s.organization_id = $1
		  AND e.ts >= $2 AND e.ts < $3
		ORDER BY e.avs_score DESC NULLS LAST, e.ts DESC
		LIMIT 5`,
		orgID, start.UnixMilli(), end.UnixMilli(),
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var top MonthlyTopEvent
			var resolvedMs int64
			if err := rows.Scan(&top.EventID, &top.SiteName, &top.CameraName,
				&top.Severity, &resolvedMs, &top.DispositionLabel, &top.AIDescription, &top.AVSScore); err != nil {
				continue
			}
			top.HappenedAt = time.UnixMilli(resolvedMs).UTC()
			s.TopEvents = append(s.TopEvents, top)
		}
	}

	return &s, nil
}
