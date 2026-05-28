// Package main — Ironsight re-analysis CLI (P4-SCHEMA-06).
//
// # Scope cap: what "re-analysis" means here
//
// Re-analysis re-processes existing per-detection outputs through a new
// model_version's rule set.  It does NOT re-run YOLO or Qwen against raw
// video frames.  If you find yourself touching services/yolo/ or services/qwen/
// you are out of scope.
//
// Inputs:  existing detections rows (bounding_box, confidence, detection_class,
//          details JSONB).
// Outputs: new detections rows produced by applying the new model_version's
//          rules/thresholds/class-remap to those inputs.
//
// # Usage
//
//	reanalyze \
//	  --model-version <uuid> \
//	  --from 2026-04-01T00:00:00Z \
//	  --to   2026-04-30T23:59:59Z \
//	  [--organization-id <text>] \
//	  [--dry-run] \
//	  [--report-out /path/report.json] \
//	  [--report-dir /path/to/dir]
//
// # RLS / auth
//
// The binary connects to Postgres via DATABASE_URL as the configured user.
// In production deployments that user is 'onvif', which carries the
// service_bypass RLS policy → full read/write access without per-tenant
// SET LOCAL calls.  This is the intentional worker mode documented in
// P4-SCHEMA-07.  No AcquireWithTenant call is needed in this binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/reanalysis"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var (
		modelVersionIDStr string
		fromStr           string
		toStr             string
		orgID             string
		dryRun            bool
		reportOut         string
		reportDir         string
	)

	flag.StringVar(&modelVersionIDStr, "model-version", "", "UUID of the model_versions row to re-process against (required)")
	flag.StringVar(&fromStr, "from", "", "RFC3339 start of detected_at range (required)")
	flag.StringVar(&toStr, "to", "", "RFC3339 end of detected_at range (required)")
	flag.StringVar(&orgID, "organization-id", "", "Limit to one organization (default: all orgs under this model_version)")
	flag.BoolVar(&dryRun, "dry-run", false, "Compute diff without inserting any rows; print report and exit 0")
	flag.StringVar(&reportOut, "report-out", "", "Write JSON report to this path (default: stdout only)")
	flag.StringVar(&reportDir, "report-dir", "", "Write JSON report to <dir>/reanalysis-<run_id>.json")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: reanalyze [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Re-processes existing detections through a new model_version's rule set.\n")
		fmt.Fprintf(os.Stderr, "Scope: rule-set application only — NOT raw-frame re-inference.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Validate required flags.
	if modelVersionIDStr == "" {
		fmt.Fprintln(os.Stderr, "error: --model-version is required")
		flag.Usage()
		os.Exit(2)
	}
	mvID, err := uuid.Parse(modelVersionIDStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: --model-version %q is not a valid UUID: %v\n", modelVersionIDStr, err)
		os.Exit(2)
	}
	if fromStr == "" || toStr == "" {
		fmt.Fprintln(os.Stderr, "error: --from and --to are required")
		flag.Usage()
		os.Exit(2)
	}
	fromTime, err := time.Parse(time.RFC3339, fromStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: --from %q: %v\n", fromStr, err)
		os.Exit(2)
	}
	toTime, err := time.Parse(time.RFC3339, toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: --to %q: %v\n", toStr, err)
		os.Exit(2)
	}
	if toTime.Before(fromTime) {
		fmt.Fprintln(os.Stderr, "error: --to must not be before --from")
		os.Exit(2)
	}

	log.Println("============================================")
	log.Println("  Ironsight Re-analysis — P4-SCHEMA-06")
	log.Println("============================================")
	if dryRun {
		log.Println("[REANALYZE] --dry-run set: no rows will be inserted")
	}

	cfg := config.Load()
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("[FATAL] database connect: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	startWall := time.Now()

	// ── 1. Validate model_version ─────────────────────────────────────────
	mv, err := db.GetModelVersion(ctx, mvID)
	if err != nil {
		log.Fatalf("[FATAL] GetModelVersion: %v", err)
	}
	if mv == nil {
		fmt.Fprintf(os.Stderr, "error: model_version %s not found\n", mvID)
		os.Exit(1)
	}
	log.Printf("[REANALYZE] model_version: %s (%s %s, domain=%s)", mv.ID, mv.ModelName, mv.VersionTag, mv.ModelDomain)

	rs, err := reanalysis.ParseRuleSet(mv.Params)
	if err != nil {
		log.Fatalf("[FATAL] ParseRuleSet: %v", err)
	}

	// ── 2. Resolve org scope ──────────────────────────────────────────────
	orgs := database.ListOrgsForModelVersion(mv)
	if orgID != "" {
		// Validate the requested org is within scope of this model_version.
		if mv.OrganizationID != orgID {
			fmt.Fprintf(os.Stderr, "error: --organization-id %q does not match model_version org %q\n", orgID, mv.OrganizationID)
			os.Exit(1)
		}
		orgs = []string{orgID}
	}
	if len(orgs) == 0 {
		fmt.Fprintln(os.Stderr, "error: model_version has no associated organization_id")
		os.Exit(1)
	}
	log.Printf("[REANALYZE] scope: org(s)=%v  from=%s  to=%s", orgs, fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))

	// ── 3. Insert analysis_run ────────────────────────────────────────────
	// One run row per org (they share the same model_version).
	// For simplicity in the single-org case (which is the norm, since
	// model_versions are org-scoped), we insert exactly one run.
	effectiveOrgID := orgs[0] // model_version is org-scoped → always one org
	paramsJSON, _ := json.Marshal(map[string]interface{}{
		"from":            fromTime.UTC().Format(time.RFC3339),
		"to":              toTime.UTC().Format(time.RFC3339),
		"organization_id": effectiveOrgID,
		"dry_run":         dryRun,
	})

	var runID uuid.UUID
	if !dryRun {
		ar, err := db.InsertAnalysisRun(ctx, database.AnalysisRunInsert{
			OrganizationID: effectiveOrgID,
			ModelVersionID: mv.ID,
			RunType:        "reanalysis",
			StartedAt:      startWall,
			Params:         paramsJSON,
		})
		if err != nil {
			log.Fatalf("[FATAL] InsertAnalysisRun: %v", err)
		}
		runID = ar.ID
		log.Printf("[REANALYZE] analysis_run: %s", runID)
	} else {
		runID = uuid.New() // ephemeral for report shape
		log.Printf("[REANALYZE] dry-run analysis_run (not persisted): %s", runID)
	}

	// ── 4. Iterate detections ─────────────────────────────────────────────
	stats := runStats{
		RunID:          runID,
		ModelVersionID: mv.ID,
		From:           fromTime,
		To:             toTime,
		OrgID:          effectiveOrgID,
		DryRun:         dryRun,
	}

	var (
		cursor         *uuid.UUID
		cursorDetAt    *time.Time
		// all old IDs seen (for FP-rate lookup at end)
		allOldIDs      []uuid.UUID
		// new IDs emitted (for new-model FP-rate lookup)
		newIDs         []uuid.UUID
	)

	for {
		f := database.ReanalysisFilter{
			OrganizationID:  effectiveOrgID,
			From:            fromTime,
			Until:           toTime,
			BatchSize:       500,
			AfterID:         cursor,
			AfterDetectedAt: cursorDetAt,
		}

		batch, err := db.ListDetectionsForReanalysis(ctx, f)
		if err != nil {
			log.Fatalf("[FATAL] ListDetectionsForReanalysis: %v", err)
		}
		if len(batch) == 0 {
			break
		}

		for i := range batch {
			row := &batch[i]
			allOldIDs = append(allOldIDs, row.ID)
			stats.OldTotal++

			outcome := reanalysis.ApplyRuleSet(row.DetectionClass, row.Confidence, row.BoundingBox, rs)

			switch outcome.Kind {
			case reanalysis.OutcomeUnchanged:
				// No supersede emitted; old row stays current in detections_current.

			case reanalysis.OutcomeDropped, reanalysis.OutcomeChanged:
				newClass := "filtered_out"
				if outcome.Kind == reanalysis.OutcomeChanged {
					newClass = outcome.Class
				}

				stats.recordChange(row.DetectionClass, newClass)

				if !dryRun {
					oldID := row.ID
					newDet, err := db.InsertDetection(ctx, database.DetectionInsert{
						OrganizationID:  row.OrganizationID,
						SiteID:          row.SiteID,
						CameraID:        row.CameraID,
						DetectedAt:      row.DetectedAt,
						DetectionClass:  newClass,
						DetectionDomain: row.DetectionDomain,
						Confidence:      row.Confidence,
						BoundingBox:     row.BoundingBox,
						ZoneID:          row.ZoneID,
						VCARuleID:       row.VCARuleID,
						ModelVersionID:  mv.ID,
						AnalysisRunID:   runID,
						SegmentID:       row.SegmentID,
						FrameOffsetMs:   row.FrameOffsetMs,
						Source:          "reanalysis",
						Supersedes:      &oldID,
						Details:         row.Details,
					})
					if err != nil {
						log.Printf("[WARN] InsertDetection supersede failed for %s: %v", row.ID, err)
						continue
					}
					newIDs = append(newIDs, newDet.ID)
				}
			}
		}

		// Advance cursor to last row of this batch.
		last := batch[len(batch)-1]
		cursor = &last.ID
		cursorDetAt = &last.DetectedAt
	}

	// ── 5. Compute FP-rate from detection_reviews ground truth ────────────
	oldVerdicts, err := db.FetchDetectionReviewVerdicts(ctx, effectiveOrgID, allOldIDs)
	if err != nil {
		log.Printf("[WARN] FetchDetectionReviewVerdicts (old): %v", err)
	}
	newVerdicts, err := db.FetchDetectionReviewVerdicts(ctx, effectiveOrgID, newIDs)
	if err != nil {
		log.Printf("[WARN] FetchDetectionReviewVerdicts (new): %v", err)
	}

	// Build verdict slices for FP-rate calculation.
	oldVerdictsSlice := make([]string, 0, len(oldVerdicts))
	for _, v := range oldVerdicts {
		oldVerdictsSlice = append(oldVerdictsSlice, v)
	}
	newVerdictsSlice := make([]string, 0, len(newVerdicts))
	for _, v := range newVerdicts {
		newVerdictsSlice = append(newVerdictsSlice, v)
	}

	oldFP := reanalysis.ComputeFPRate(oldVerdictsSlice)
	newFP := reanalysis.ComputeFPRate(newVerdictsSlice)

	// ── 6. Stamp ended_at ─────────────────────────────────────────────────
	endWall := time.Now()
	durationMS := endWall.Sub(startWall).Milliseconds()

	if !dryRun {
		if err := db.UpdateAnalysisRunEnded(ctx, runID, endWall); err != nil {
			log.Printf("[WARN] UpdateAnalysisRunEnded: %v", err)
		}
	}

	// ── 7. Build report ───────────────────────────────────────────────────
	newTotal := stats.OldTotal - stats.NumChanged - stats.NumDropped // unchanged rows stay as-is
	if newTotal < 0 {
		newTotal = 0
	}
	// Actually: new_model_detections = unchanged + new supersede rows emitted
	// (where new supersede rows may be "filtered_out" or a remapped class).
	// The "detections after reanalysis" count is:
	//   unchanged rows (still current) + OutcomeChanged rows (new supersede is current)
	// OutcomeDropped rows produce filtered_out supersedes → those are no longer
	// "visible" in detections_current.
	newModelDetections := (stats.OldTotal - stats.NumChanged - stats.NumDropped) + stats.NumChanged
	// = OldTotal - NumDropped
	newModelDetections = stats.OldTotal - stats.NumDropped

	delta := newModelDetections - stats.OldTotal

	var deltaPercent float64
	if stats.OldTotal > 0 {
		deltaPercent = float64(delta) / float64(stats.OldTotal) * 100
		deltaPercent = math.Round(deltaPercent*10) / 10
	}

	type classChange struct {
		FromClass string `json:"from_class"`
		ToClass   string `json:"to_class"`
		Count     int    `json:"count"`
	}
	byClass := make([]classChange, 0, len(stats.ClassChanges))
	for k, n := range stats.ClassChanges {
		byClass = append(byClass, classChange{
			FromClass: k.From,
			ToClass:   k.To,
			Count:     n,
		})
	}

	report := map[string]interface{}{
		"run_id":           runID,
		"model_version_id": mv.ID,
		"from":             fromTime.UTC().Format(time.RFC3339),
		"to":               toTime.UTC().Format(time.RFC3339),
		"organization_id":  effectiveOrgID,
		"dry_run":          dryRun,
		"totals": map[string]interface{}{
			"old_model_detections": stats.OldTotal,
			"new_model_detections": newModelDetections,
			"delta":                delta,
			"delta_pct":            deltaPercent,
		},
		"by_class_change": byClass,
		"duration_ms":     durationMS,
	}

	// False-positive block (only when ground truth exists).
	if oldFP.IsAvailable() || newFP.IsAvailable() {
		fpBlock := map[string]interface{}{
			"ground_truth_samples": oldFP.TotalReviewed,
		}
		if oldFP.IsAvailable() {
			fpBlock["old_model"] = math.Round(oldFP.Rate*1000) / 1000
		} else {
			fpBlock["old_model"] = nil
		}
		if newFP.IsAvailable() {
			fpBlock["new_model"] = math.Round(newFP.Rate*1000) / 1000
			fpBlock["ground_truth_samples"] = newFP.TotalReviewed
		} else {
			fpBlock["new_model"] = nil
		}
		if oldFP.IsAvailable() && newFP.IsAvailable() {
			delta := newFP.Rate - oldFP.Rate
			fpBlock["delta"] = math.Round(delta*1000) / 1000
		}
		report["false_positive_rate"] = fpBlock
	}

	// ── 8. Write report ───────────────────────────────────────────────────
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		log.Fatalf("[FATAL] marshal report: %v", err)
	}

	// Always print to stdout.
	fmt.Println(string(reportJSON))

	// Optional: write to --report-out path.
	if reportOut != "" {
		if err := os.WriteFile(reportOut, reportJSON, 0o644); err != nil {
			log.Printf("[WARN] write report to %s: %v", reportOut, err)
		} else {
			log.Printf("[REANALYZE] report written to %s", reportOut)
		}
	}

	// Optional: write to --report-dir/<name>.json.
	if reportDir != "" {
		name := fmt.Sprintf("reanalysis-%s.json", runID)
		path := filepath.Join(reportDir, name)
		if err := os.WriteFile(path, reportJSON, 0o644); err != nil {
			log.Printf("[WARN] write report to %s: %v", path, err)
		} else {
			log.Printf("[REANALYZE] report written to %s", path)
		}
	}

	log.Printf("[REANALYZE] done: old=%d new=%d delta=%d duration=%dms",
		stats.OldTotal, newModelDetections, delta, durationMS)
}

// ─────────────────────────────────────────────────────────────────────────────
// runStats — mutable accumulator during iteration
// ─────────────────────────────────────────────────────────────────────────────

type classPair struct {
	From string
	To   string
}

type runStats struct {
	RunID          uuid.UUID
	ModelVersionID uuid.UUID
	From           time.Time
	To             time.Time
	OrgID          string
	DryRun         bool

	OldTotal   int
	NumChanged int
	NumDropped int

	ClassChanges map[classPair]int
}

func (s *runStats) recordChange(fromClass, toClass string) {
	if s.ClassChanges == nil {
		s.ClassChanges = make(map[classPair]int)
	}
	if toClass == "filtered_out" {
		s.NumDropped++
	} else {
		s.NumChanged++
	}
	s.ClassChanges[classPair{From: fromClass, To: toClass}]++
}
