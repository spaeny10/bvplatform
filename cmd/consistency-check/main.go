// cmd/consistency-check — P4-SCHEMA-02 daily consistency check.
//
// For each (organization_id, UTC day) window in the last 7 days this binary:
//
//   1. Counts legacy rows:
//      - events table (all rows, any event_type)
//      - active_alarms ai_ppe_violations (one row per element of the JSONB array)
//      - pending_review_queue (PPE queue / former vlm_label_jobs)
//
//   2. Counts detections rows in the same window per source/domain.
//
//   3. Computes per-window divergence.  A "settled" window is one whose
//      legacy writes ended > 5 minutes before the check runs.  The last
//      incomplete window (current UTC day) is excluded from the check
//      entirely; only settled windows are flagged.
//
//   4. Writes a JSON report to /opt/ironsight-consistency-reports/YYYY-MM-DD.json
//      (the deploy job ensures the directory exists and is writable by the
//      ironsight user).
//
//   5. Exits 0 when all settled windows are clean; exits 1 when any
//      settled window shows non-zero divergence.
//
// Usage:
//
//	DATABASE_URL=postgres://... consistency-check [--days 7] [--out /opt/...]
//
// Environment:
//
//	DATABASE_URL    — required: PostgreSQL connection string.
//	REPORT_DIR      — optional override for the report output directory
//	                  (default /opt/ironsight-consistency-reports).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	// Ensure migrations embed is imported so the module is a dependency.
	// (Not used directly here but keeps the module graph clean.)
	_ "ironsight/migrations"
)

// WindowReport is the per-day per-org check result.
type WindowReport struct {
	OrganizationID string `json:"organization_id"`
	Day            string `json:"day"` // YYYY-MM-DD UTC

	// Legacy counts
	LegacyEvents         int `json:"legacy_events"`
	LegacyAlarmPPE       int `json:"legacy_alarm_ppe_violations"`
	LegacyPPEQueue       int `json:"legacy_ppe_queue"`
	LegacyTotal          int `json:"legacy_total"`

	// detections counts (by domain)
	DetectionsSecurity   int `json:"detections_security"`
	DetectionsPPE        int `json:"detections_ppe"`
	DetectionsVLM        int `json:"detections_vlm_validation"`
	DetectionsTotal      int `json:"detections_total"`

	// Divergence
	Divergence int  `json:"divergence"` // detections_total - legacy_total
	Settled    bool `json:"settled"`    // true = > 5 min past end of window
	Flagged    bool `json:"flagged"`    // true = settled && abs(divergence) > 0
}

// Report is the top-level output document.
type Report struct {
	GeneratedAt string         `json:"generated_at"`
	CheckedDays int            `json:"checked_days"`
	Windows     []WindowReport `json:"windows"`
	TotalFlagged int           `json:"total_flagged"`
	OK          bool           `json:"ok"`
}

func main() {
	days := flag.Int("days", 7, "number of past UTC days to check")
	outDir := flag.String("out", "", "output directory (overrides REPORT_DIR env)")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	reportDir := *outDir
	if reportDir == "" {
		reportDir = os.Getenv("REPORT_DIR")
	}
	if reportDir == "" {
		reportDir = "/opt/ironsight-consistency-reports"
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("DB ping: %v", err)
	}

	now := time.Now().UTC()
	report, err := runCheck(ctx, pool, now, *days)
	if err != nil {
		log.Fatalf("check failed: %v", err)
	}

	// Write report file.
	if mkErr := os.MkdirAll(reportDir, 0o755); mkErr != nil {
		log.Printf("[WARN] cannot create report dir %s: %v (report will only go to stdout)", reportDir, mkErr)
	} else {
		fname := filepath.Join(reportDir, now.Format("2006-01-02")+".json")
		data, _ := json.MarshalIndent(report, "", "  ")
		if wErr := os.WriteFile(fname, data, 0o644); wErr != nil {
			log.Printf("[WARN] write report %s: %v", fname, wErr)
		} else {
			log.Printf("[CONSISTENCY] report written to %s", fname)
		}
	}

	// Print summary to stdout (captured by systemd journal).
	data, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(data))

	if !report.OK {
		log.Printf("[CONSISTENCY] FAIL: %d window(s) flagged", report.TotalFlagged)
		os.Exit(1)
	}
	log.Printf("[CONSISTENCY] OK: all %d settled windows clean", report.CheckedDays)
}

// settledCutoff is the timestamp before which a window is considered
// "settled" (legacy writes have ceased).  Windows whose end > settledCutoff
// are still in-flight and are excluded from flagging.
const settledGrace = 5 * time.Minute

func runCheck(ctx context.Context, pool *pgxpool.Pool, now time.Time, days int) (*Report, error) {
	// Enumerate distinct organization_ids present in the DB.
	orgRows, err := pool.Query(ctx, `SELECT id FROM organizations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	var orgs []string
	for orgRows.Next() {
		var id string
		if err := orgRows.Scan(&id); err != nil {
			orgRows.Close()
			return nil, err
		}
		orgs = append(orgs, id)
	}
	orgRows.Close()
	if err := orgRows.Err(); err != nil {
		return nil, err
	}

	settledCutoff := now.Add(-settledGrace)

	var windows []WindowReport
	totalFlagged := 0

	for d := 1; d <= days; d++ {
		// Window: the UTC day starting d days ago.
		dayStart := time.Date(now.Year(), now.Month(), now.Day()-d, 0, 0, 0, 0, time.UTC)
		dayEnd := dayStart.Add(24 * time.Hour)
		dayStr := dayStart.Format("2006-01-02")

		settled := dayEnd.Before(settledCutoff)

		for _, orgID := range orgs {
			wr, err := checkWindow(ctx, pool, orgID, dayStr, dayStart, dayEnd)
			if err != nil {
				log.Printf("[CONSISTENCY] window %s org %s: %v", dayStr, orgID, err)
				continue
			}
			wr.Settled = settled
			if settled && wr.Divergence != 0 {
				wr.Flagged = true
				totalFlagged++
			}
			windows = append(windows, *wr)
		}
	}

	return &Report{
		GeneratedAt:  now.Format(time.RFC3339),
		CheckedDays:  days,
		Windows:      windows,
		TotalFlagged: totalFlagged,
		OK:           totalFlagged == 0,
	}, nil
}

func checkWindow(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, dayStr string,
	dayStart, dayEnd time.Time,
) (*WindowReport, error) {
	wr := &WindowReport{
		OrganizationID: orgID,
		Day:            dayStr,
	}

	// ── Legacy counts ─────────────────────────────────────────────────────

	// 1. events table — all events for cameras belonging to this org.
	//    events.camera_id → cameras.id → sites.organization_id
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM events e
		JOIN cameras c ON c.id = e.camera_id
		JOIN sites   s ON s.id = c.site_id
		WHERE s.organization_id = $1
		  AND e.event_time >= $2
		  AND e.event_time <  $3`,
		orgID, dayStart, dayEnd,
	).Scan(&wr.LegacyEvents)
	if err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}

	// 2. active_alarms ai_ppe_violations — count the JSONB array elements.
	//    active_alarms stores org via the camera_id→site join; easier to use
	//    site_id direct join since the column is TEXT.
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(jsonb_array_length(COALESCE(ai_ppe_violations,'[]'::jsonb))),0)
		FROM active_alarms a
		JOIN cameras c ON c.id::text = a.camera_id
		JOIN sites   s ON s.id = c.site_id
		WHERE s.organization_id = $1
		  AND to_timestamp(a.ts / 1000.0) >= $2
		  AND to_timestamp(a.ts / 1000.0) <  $3
		  AND jsonb_array_length(COALESCE(ai_ppe_violations,'[]'::jsonb)) > 0`,
		orgID, dayStart, dayEnd,
	).Scan(&wr.LegacyAlarmPPE)
	if err != nil {
		return nil, fmt.Errorf("count alarm ppe: %w", err)
	}

	// 3. pending_review_queue (PPE violation queue — the authoritative
	//    legacy source for PPE detections pre-dual-write).
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pending_review_queue
		WHERE organization_id = $1
		  AND created_at >= $2
		  AND created_at <  $3`,
		orgID, dayStart, dayEnd,
	).Scan(&wr.LegacyPPEQueue)
	if err != nil {
		return nil, fmt.Errorf("count ppe queue: %w", err)
	}

	wr.LegacyTotal = wr.LegacyEvents + wr.LegacyAlarmPPE + wr.LegacyPPEQueue

	// ── Detections counts ─────────────────────────────────────────────────

	// Aggregate detections by domain in one query.
	dRows, err := pool.Query(ctx, `
		SELECT detection_domain, COUNT(*)
		FROM detections
		WHERE organization_id = $1
		  AND detected_at >= $2
		  AND detected_at <  $3
		GROUP BY detection_domain`,
		orgID, dayStart, dayEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("count detections: %w", err)
	}
	for dRows.Next() {
		var domain string
		var cnt int
		if err := dRows.Scan(&domain, &cnt); err != nil {
			dRows.Close()
			return nil, err
		}
		switch domain {
		case "security":
			wr.DetectionsSecurity = cnt
		case "ppe":
			wr.DetectionsPPE = cnt
		case "vlm_validation":
			wr.DetectionsVLM = cnt
		}
	}
	dRows.Close()
	if err := dRows.Err(); err != nil {
		return nil, err
	}

	wr.DetectionsTotal = wr.DetectionsSecurity + wr.DetectionsPPE + wr.DetectionsVLM
	wr.Divergence = wr.DetectionsTotal - wr.LegacyTotal
	return wr, nil
}
