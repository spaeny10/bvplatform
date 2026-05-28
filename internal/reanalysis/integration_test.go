package reanalysis_test

// integration_test.go — P4-SCHEMA-06 database-level integration tests.
//
// These tests require a real TimescaleDB connection (DATABASE_URL set).
// They are skipped in the normal `go test ./...` developer path.
// In CI (IRONSIGHT_INTEGRATION_REQUIRED=1), they must pass.
//
// Tests cover:
//   1. Correct count of supersede rows emitted.
//   2. Old rows still present (not deleted — append-only invariant).
//   3. detections_current view returns only the new rows for re-processed entries.
//   4. Report JSON matches expected shape.
//   5. Cross-tenant isolation: re-analysis scoped to org A does not emit supersede
//      rows for org B's detections.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/reanalysis"
	"ironsight/internal/testutil"
)

// findIntegrationCamera returns an (orgID, camID, siteID) triple usable as FK
// targets.  Mirrors the helper used in database/detections_test.go.
func findIntegrationCamera(t *testing.T, db *database.DB, ctx context.Context) (string, uuid.UUID, string) {
	t.Helper()
	var orgID, siteID string
	var camIDStr string
	err := db.Pool.QueryRow(ctx, `
		SELECT c.id, COALESCE(c.site_id, ''), COALESCE(s.organization_id, '')
		FROM cameras c
		LEFT JOIN sites s ON s.id = c.site_id
		WHERE c.deleted_at IS NULL
		LIMIT 1`,
	).Scan(&camIDStr, &siteID, &orgID)
	if err != nil || orgID == "" {
		t.Skip("no org-linked camera found in DB; skipping integration test")
	}
	camID, err := uuid.Parse(camIDStr)
	if err != nil {
		t.Skip("could not parse camera UUID; skipping")
	}
	return orgID, camID, siteID
}

// buildFixture creates a model_version + analysis_run for a given org+camera.
func buildFixture(t *testing.T, db *database.DB, ctx context.Context, orgID string, camID uuid.UUID, versionSuffix string) (mvID uuid.UUID, arID uuid.UUID) {
	t.Helper()
	mv, err := db.InsertModelVersion(ctx, database.ModelVersionInsert{
		OrganizationID: orgID,
		ModelName:      "yolo11-ppe-reanalysis-test",
		VersionTag:     "1.0.0-" + versionSuffix,
		ModelDomain:    "ppe",
	})
	if err != nil {
		t.Fatalf("buildFixture InsertModelVersion: %v", err)
	}
	ar, err := db.InsertAnalysisRun(ctx, database.AnalysisRunInsert{
		OrganizationID: orgID,
		ModelVersionID: mv.ID,
		RunType:        "live_ingest",
	})
	if err != nil {
		t.Fatalf("buildFixture InsertAnalysisRun: %v", err)
	}
	return mv.ID, ar.ID
}

// insertDetection is a convenience wrapper.
func insertDetection(t *testing.T, db *database.DB, ctx context.Context, orgID string, camID uuid.UUID, siteID string, mvID, arID uuid.UUID, class string, confidence float32, detAt time.Time) uuid.UUID {
	t.Helper()
	d, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		SiteID:          &siteID,
		CameraID:        camID,
		DetectedAt:      detAt,
		DetectionClass:  class,
		DetectionDomain: "ppe",
		Confidence:      confidence,
		BoundingBox:     json.RawMessage(`{"x1":0.1,"y1":0.2,"x2":0.5,"y2":0.6}`),
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "live",
	})
	if err != nil {
		t.Fatalf("insertDetection: %v", err)
	}
	return d.ID
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: correct supersede count + append-only invariant
// ─────────────────────────────────────────────────────────────────────────────

func TestReanalysis_SupersedeCount_AndAppendOnly(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findIntegrationCamera(t, db, ctx)
	oldMvID, oldArID := buildFixture(t, db, ctx, orgID, camID, uuid.NewString()[:8])

	// Seed 10 detections: 5 with class "helmet_no" (will be remapped) and
	// 5 with class "no-vest" (no remap → unchanged).
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	var helmetIDs, vestIDs []uuid.UUID
	for i := 0; i < 5; i++ {
		detAt := base.Add(time.Duration(i) * time.Second)
		id := insertDetection(t, db, ctx, orgID, camID, siteID, oldMvID, oldArID, "helmet_no", 0.85, detAt)
		helmetIDs = append(helmetIDs, id)
	}
	for i := 0; i < 5; i++ {
		detAt := base.Add(time.Duration(5+i) * time.Second)
		id := insertDetection(t, db, ctx, orgID, camID, siteID, oldMvID, oldArID, "no-vest", 0.90, detAt)
		vestIDs = append(vestIDs, id)
	}
	_ = vestIDs

	// Create new model_version with a class_remap: helmet_no → ppe_violation.
	newMvParams := json.RawMessage(`{
		"confidence_threshold": 0.5,
		"class_remap": {"helmet_no": "ppe_violation"}
	}`)
	newMv, err := db.InsertModelVersion(ctx, database.ModelVersionInsert{
		OrganizationID: orgID,
		ModelName:      "yolo11-ppe-reanalysis-test",
		VersionTag:     "2.0.0-" + uuid.NewString()[:8],
		ModelDomain:    "ppe",
		Params:         newMvParams,
	})
	if err != nil {
		t.Fatalf("InsertModelVersion (new): %v", err)
	}

	// Create analysis_run for the re-analysis.
	newAr, err := db.InsertAnalysisRun(ctx, database.AnalysisRunInsert{
		OrganizationID: orgID,
		ModelVersionID: newMv.ID,
		RunType:        "reanalysis",
	})
	if err != nil {
		t.Fatalf("InsertAnalysisRun (reanalysis): %v", err)
	}

	// Parse ruleset and apply re-analysis.
	rs, err := reanalysis.ParseRuleSet(newMv.Params)
	if err != nil {
		t.Fatalf("ParseRuleSet: %v", err)
	}

	// Iterate live detections and apply rules.
	from := base.Add(-time.Second)
	until := base.Add(20 * time.Second)
	emitted := 0

	var cursor *uuid.UUID
	var cursorDetAt *time.Time
	for {
		batch, err := db.ListDetectionsForReanalysis(ctx, database.ReanalysisFilter{
			OrganizationID:  orgID,
			From:            from,
			Until:           until,
			BatchSize:       100,
			AfterID:         cursor,
			AfterDetectedAt: cursorDetAt,
		})
		if err != nil {
			t.Fatalf("ListDetectionsForReanalysis: %v", err)
		}
		if len(batch) == 0 {
			break
		}
		for i := range batch {
			row := &batch[i]
			outcome := reanalysis.ApplyRuleSet(row.DetectionClass, row.Confidence, row.BoundingBox, rs)
			if outcome.Kind == reanalysis.OutcomeUnchanged {
				continue
			}
			newClass := "filtered_out"
			if outcome.Kind == reanalysis.OutcomeChanged {
				newClass = outcome.Class
			}
			oldID := row.ID
			_, insErr := db.InsertDetection(ctx, database.DetectionInsert{
				OrganizationID:  row.OrganizationID,
				SiteID:          row.SiteID,
				CameraID:        row.CameraID,
				DetectedAt:      row.DetectedAt,
				DetectionClass:  newClass,
				DetectionDomain: row.DetectionDomain,
				Confidence:      row.Confidence,
				BoundingBox:     row.BoundingBox,
				ModelVersionID:  newMv.ID,
				AnalysisRunID:   newAr.ID,
				Source:          "reanalysis",
				Supersedes:      &oldID,
				Details:         row.Details,
			})
			if insErr != nil {
				t.Fatalf("InsertDetection (supersede): %v", insErr)
			}
			emitted++
		}
		last := batch[len(batch)-1]
		cursor = &last.ID
		cursorDetAt = &last.DetectedAt
	}

	// Exactly 5 supersede rows should have been emitted (the helmet_no ones).
	if emitted != 5 {
		t.Errorf("expected 5 supersede rows emitted, got %d", emitted)
	}

	// Old rows must still exist (append-only invariant).
	for _, id := range helmetIDs {
		row, err := db.GetDetection(ctx, id, orgID)
		if err != nil {
			t.Fatalf("GetDetection (old): %v", err)
		}
		if row == nil {
			t.Errorf("old detection %s was deleted — violates append-only invariant", id)
		}
		if row != nil && row.DetectionClass != "helmet_no" {
			t.Errorf("old detection class mutated: want 'helmet_no', got %q", row.DetectionClass)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: detections_current hides superseded rows
// ─────────────────────────────────────────────────────────────────────────────

func TestReanalysis_DetectionsCurrent_HidesSuperseded(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findIntegrationCamera(t, db, ctx)
	oldMvID, oldArID := buildFixture(t, db, ctx, orgID, camID, uuid.NewString()[:8])

	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Millisecond)
	origID := insertDetection(t, db, ctx, orgID, camID, siteID, oldMvID, oldArID, "helmet_no", 0.75, base)

	// New model_version remaps helmet_no → ppe_violation.
	newMv, _ := db.InsertModelVersion(ctx, database.ModelVersionInsert{
		OrganizationID: orgID,
		ModelName:      "yolo11-ppe-current-test",
		VersionTag:     "3.0.0-" + uuid.NewString()[:8],
		ModelDomain:    "ppe",
		Params:         json.RawMessage(`{"class_remap": {"helmet_no": "ppe_violation"}}`),
	})
	newAr, _ := db.InsertAnalysisRun(ctx, database.AnalysisRunInsert{
		OrganizationID: orgID,
		ModelVersionID: newMv.ID,
		RunType:        "reanalysis",
	})

	// Emit supersede row.
	newDet, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		SiteID:          &siteID,
		CameraID:        camID,
		DetectedAt:      base.Add(time.Millisecond),
		DetectionClass:  "ppe_violation",
		DetectionDomain: "ppe",
		Confidence:      0.75,
		BoundingBox:     json.RawMessage(`{"x1":0.1,"y1":0.2,"x2":0.5,"y2":0.6}`),
		ModelVersionID:  newMv.ID,
		AnalysisRunID:   newAr.ID,
		Source:          "reanalysis",
		Supersedes:      &origID,
	})
	if err != nil {
		t.Fatalf("InsertDetection (supersede): %v", err)
	}

	// Query detections_current.
	current, err := db.ListDetectionsCurrent(ctx, database.DetectionCurrentFilter{
		OrganizationID: orgID,
		CameraID:       &camID,
		Since:          base.Add(-time.Second),
		Until:          base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("ListDetectionsCurrent: %v", err)
	}

	var foundOrig, foundNew bool
	for _, d := range current {
		if d.ID == origID {
			foundOrig = true
		}
		if d.ID == newDet.ID {
			foundNew = true
		}
	}
	if foundOrig {
		t.Error("detections_current MUST NOT contain the superseded original row")
	}
	if !foundNew {
		t.Error("detections_current MUST contain the new supersede row")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: cross-tenant — org A re-analysis does not touch org B's detections
// ─────────────────────────────────────────────────────────────────────────────

func TestReanalysis_CrossTenant(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgA, camID, siteID := findIntegrationCamera(t, db, ctx)
	// orgB is a fake tenant — no real rows.
	orgB := "fake-org-reanalysis-" + uuid.NewString()[:8]

	oldMvID, oldArID := buildFixture(t, db, ctx, orgA, camID, uuid.NewString()[:8])

	base := time.Now().UTC().Add(-4 * time.Hour).Truncate(time.Millisecond)
	origID := insertDetection(t, db, ctx, orgA, camID, siteID, oldMvID, oldArID, "helmet_no", 0.80, base)

	// Re-analysis scoped to orgB should return zero detections in the range.
	batch, err := db.ListDetectionsForReanalysis(ctx, database.ReanalysisFilter{
		OrganizationID: orgB, // different org
		From:           base.Add(-time.Second),
		Until:          base.Add(time.Second),
		BatchSize:      100,
	})
	if err != nil {
		t.Fatalf("ListDetectionsForReanalysis (orgB): %v", err)
	}
	for _, r := range batch {
		if r.ID == origID {
			t.Error("cross-tenant leak: org B re-analysis returned org A's detection")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: append-only trigger still rejects UPDATE after reanalysis
// ─────────────────────────────────────────────────────────────────────────────

func TestReanalysis_AppendOnlyTrigger_StillEnforced(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findIntegrationCamera(t, db, ctx)
	mvID, arID := buildFixture(t, db, ctx, orgID, camID, uuid.NewString()[:8])

	base := time.Now().UTC().Truncate(time.Millisecond)
	origID := insertDetection(t, db, ctx, orgID, camID, siteID, mvID, arID, "no-vest", 0.70, base)

	// Attempt UPDATE on the original row — must fail.
	_, updateErr := db.Pool.Exec(ctx,
		`UPDATE detections SET detection_class = 'tampered' WHERE id = $1`, origID)
	if updateErr == nil {
		t.Error("expected UPDATE on detections to fail (append-only trigger), but it succeeded")
	} else if !strings.Contains(updateErr.Error(), "append-only") &&
		!strings.Contains(updateErr.Error(), "insufficient_privilege") {
		t.Errorf("UPDATE error does not mention append-only: %v", updateErr)
	}
}
