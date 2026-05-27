package database_test

// detections_test.go — P4-SCHEMA-01 integration tests.
//
// Tests exercise the foundation tables (model_versions, analysis_runs,
// detections, detection_reviews) against a real TimescaleDB schema.
// Pattern follows ppe_queue_test.go and tracking_test.go.
//
// Tests run with -p 1 (serialized) as per CI convention. Each test uses
// globally-unique IDs so parallel execution within the same DB is safe,
// but -p 1 is the documented constraint for this package.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/testutil"
)

// findTestUser returns any user ID present in the DB for use as a FK in
// detection_reviews. Skips the test if no users exist (fresh schema with
// no seed data). Mirrors the findTestCamera pattern.
func findTestUser(t *testing.T, db *database.DB, ctx context.Context) uuid.UUID {
	t.Helper()
	var idStr string
	if err := db.Pool.QueryRow(ctx, `SELECT id FROM users LIMIT 1`).Scan(&idStr); err != nil {
		t.Skip("no users found in DB; skipping detection_reviews FK test")
	}
	id, err := uuid.Parse(idStr)
	if err != nil || id == uuid.Nil {
		t.Skip("could not parse user UUID; skipping")
	}
	return id
}

// buildDetectionFixture creates a model_version + analysis_run for a given
// org+camera, suitable for use as FK dependencies in detection inserts.
func buildDetectionFixture(
	t *testing.T,
	db *database.DB,
	ctx context.Context,
	orgID string,
	camID uuid.UUID,
) (mvID uuid.UUID, arID uuid.UUID) {
	t.Helper()

	mv, err := db.InsertModelVersion(ctx, database.ModelVersionInsert{
		OrganizationID: orgID,
		ModelName:      "yolo11-ppe",
		VersionTag:     "11.0.0-test-" + uuid.NewString()[:8],
		WeightsHash:    "abc123",
		ModelDomain:    "ppe",
	})
	if err != nil {
		t.Fatalf("buildDetectionFixture InsertModelVersion: %v", err)
	}

	ar, err := db.InsertAnalysisRun(ctx, database.AnalysisRunInsert{
		OrganizationID: orgID,
		ModelVersionID: mv.ID,
		RunType:        "live_ingest",
	})
	if err != nil {
		t.Fatalf("buildDetectionFixture InsertAnalysisRun: %v", err)
	}
	return mv.ID, ar.ID
}

// ─────────────────────────────────────────────────────────────
// Test: basic insert + GetDetection round-trip
// ─────────────────────────────────────────────────────────────

func TestInsertDetection_RoundTrip(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)
	mvID, arID := buildDetectionFixture(t, db, ctx, orgID, camID)

	detectedAt := time.Now().UTC().Truncate(time.Millisecond)
	bbox := json.RawMessage(`{"x1":0.1,"y1":0.2,"x2":0.4,"y2":0.6}`)
	details := json.RawMessage(`{"track_id":"abc"}`)

	ins := database.DetectionInsert{
		OrganizationID:  orgID,
		SiteID:          &siteID,
		CameraID:        camID,
		DetectedAt:      detectedAt,
		DetectionClass:  "no-hardhat",
		DetectionDomain: "ppe",
		Confidence:      0.85,
		BoundingBox:     bbox,
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "live",
		Details:         details,
	}

	d, err := db.InsertDetection(ctx, ins)
	if err != nil {
		t.Fatalf("InsertDetection: %v", err)
	}
	if d.ID == uuid.Nil {
		t.Fatal("expected non-nil UUID from InsertDetection")
	}

	// Round-trip via GetDetection.
	got, err := db.GetDetection(ctx, d.ID, orgID)
	if err != nil {
		t.Fatalf("GetDetection: %v", err)
	}
	if got == nil {
		t.Fatal("GetDetection returned nil for a just-inserted row")
	}
	if got.DetectionClass != "no-hardhat" {
		t.Errorf("detection_class: want 'no-hardhat', got %q", got.DetectionClass)
	}
	if got.DetectionDomain != "ppe" {
		t.Errorf("detection_domain: want 'ppe', got %q", got.DetectionDomain)
	}
	if got.Confidence != 0.85 {
		t.Errorf("confidence: want 0.85, got %v", got.Confidence)
	}
	if got.OrganizationID != orgID {
		t.Errorf("organization_id mismatch")
	}
	if got.Source != "live" {
		t.Errorf("source: want 'live', got %q", got.Source)
	}
	if got.Supersedes != nil {
		t.Errorf("supersedes: want nil for new detection, got %v", got.Supersedes)
	}
}

// ─────────────────────────────────────────────────────────────
// Test: append-only trigger rejects UPDATE and DELETE
// ─────────────────────────────────────────────────────────────

func TestDetections_AppendOnly(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, _ := findTestCamera(t, db, ctx)
	mvID, arID := buildDetectionFixture(t, db, ctx, orgID, camID)

	d, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		CameraID:        camID,
		DetectedAt:      time.Now().UTC(),
		DetectionClass:  "no-vest",
		DetectionDomain: "ppe",
		Confidence:      0.70,
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "live",
	})
	if err != nil {
		t.Fatalf("InsertDetection: %v", err)
	}

	// Attempt UPDATE — must fail with the append-only trigger error.
	_, updateErr := db.Pool.Exec(ctx,
		`UPDATE detections SET detection_class = 'tampered' WHERE id = $1`, d.ID)
	if updateErr == nil {
		t.Error("expected UPDATE on detections to fail (append-only trigger), but it succeeded")
	} else if !strings.Contains(updateErr.Error(), "append-only") &&
		!strings.Contains(updateErr.Error(), "insufficient_privilege") {
		t.Errorf("UPDATE error does not mention append-only: %v", updateErr)
	}

	// Attempt DELETE — must fail with the append-only trigger error.
	_, deleteErr := db.Pool.Exec(ctx,
		`DELETE FROM detections WHERE id = $1`, d.ID)
	if deleteErr == nil {
		t.Error("expected DELETE on detections to fail (append-only trigger), but it succeeded")
	} else if !strings.Contains(deleteErr.Error(), "append-only") &&
		!strings.Contains(deleteErr.Error(), "insufficient_privilege") {
		t.Errorf("DELETE error does not mention append-only: %v", deleteErr)
	}
}

// ─────────────────────────────────────────────────────────────
// Test: supersede chain → detections_current excludes superseded rows
// ─────────────────────────────────────────────────────────────

func TestDetections_SupersedeChain(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)
	mvID, arID := buildDetectionFixture(t, db, ctx, orgID, camID)

	// Insert the original detection.
	base := time.Now().UTC().Add(-2 * time.Second).Truncate(time.Millisecond)
	orig, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		SiteID:          &siteID,
		CameraID:        camID,
		DetectedAt:      base,
		DetectionClass:  "no-vest",
		DetectionDomain: "ppe",
		Confidence:      0.80,
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "live",
	})
	if err != nil {
		t.Fatalf("insert original: %v", err)
	}

	// Insert a correction that supersedes the original.
	correction, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		SiteID:          &siteID,
		CameraID:        camID,
		DetectedAt:      base.Add(time.Second),
		DetectionClass:  "no-vest",
		DetectionDomain: "ppe",
		Confidence:      0.92,
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "reanalysis",
		Supersedes:      &orig.ID,
	})
	if err != nil {
		t.Fatalf("insert correction: %v", err)
	}

	// detections_current should contain the correction but NOT the original.
	current, err := db.ListDetectionsCurrent(ctx, database.DetectionCurrentFilter{
		OrganizationID: orgID,
		CameraID:       &camID,
		Since:          base.Add(-time.Second),
		Until:          base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("ListDetectionsCurrent: %v", err)
	}

	var foundOrig, foundCorrection bool
	for _, d := range current {
		if d.ID == orig.ID {
			foundOrig = true
		}
		if d.ID == correction.ID {
			foundCorrection = true
		}
	}
	if foundOrig {
		t.Error("detections_current should NOT contain the superseded original row")
	}
	if !foundCorrection {
		t.Error("detections_current should contain the correction row")
	}
}

// ─────────────────────────────────────────────────────────────
// Test: detection_reviews FK integrity
// ─────────────────────────────────────────────────────────────

func TestDetectionReview_InsertAndRetrieve(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, _ := findTestCamera(t, db, ctx)
	reviewerID := findTestUser(t, db, ctx)
	mvID, arID := buildDetectionFixture(t, db, ctx, orgID, camID)

	d, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		CameraID:        camID,
		DetectedAt:      time.Now().UTC(),
		DetectionClass:  "no-hardhat",
		DetectionDomain: "ppe",
		Confidence:      0.75,
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "live",
	})
	if err != nil {
		t.Fatalf("InsertDetection: %v", err)
	}

	notes := "confirmed violation — hard hat visible in adjacent frame"

	rev, err := db.InsertDetectionReview(ctx, database.DetectionReviewInsert{
		OrganizationID: orgID,
		DetectionID:    d.ID,
		ReviewerUserID: reviewerID,
		Verdict:        "confirmed",
		Notes:          &notes,
	})
	if err != nil {
		t.Fatalf("InsertDetectionReview: %v", err)
	}
	if rev.ID == uuid.Nil {
		t.Fatal("expected non-nil review ID")
	}
	if rev.Verdict != "confirmed" {
		t.Errorf("verdict: want 'confirmed', got %q", rev.Verdict)
	}
	if rev.Notes == nil || *rev.Notes != notes {
		t.Errorf("notes: want %q, got %v", notes, rev.Notes)
	}

	// Insert a second review (false_positive) on the same detection —
	// multiple reviews per detection are permitted (full audit trail).
	// Reuse the same reviewer (no unique constraint on reviewer per detection).
	rev2, err := db.InsertDetectionReview(ctx, database.DetectionReviewInsert{
		OrganizationID: orgID,
		DetectionID:    d.ID,
		ReviewerUserID: reviewerID,
		Verdict:        "false_positive",
	})
	if err != nil {
		t.Fatalf("InsertDetectionReview (2nd): %v", err)
	}
	if rev2.ID == rev.ID {
		t.Error("second review should have a different ID")
	}
	if rev2.Verdict != "false_positive" {
		t.Errorf("second review verdict: want 'false_positive', got %q", rev2.Verdict)
	}

	// Verify that reviewer_user_id FK to users is still enforced.
	// (users is a regular table, not a hypertable, so its FK works normally.)
	// We can't easily create a real users row here, so instead verify that
	// the review round-trips the correct detection_id and org_id.
	if rev.DetectionID != d.ID {
		t.Errorf("review detection_id: want %v, got %v", d.ID, rev.DetectionID)
	}
	if rev.OrganizationID != orgID {
		t.Errorf("review organization_id mismatch")
	}
}

// ─────────────────────────────────────────────────────────────
// Test: tenant scope — org B cannot read org A's detections
// ─────────────────────────────────────────────────────────────

func TestDetections_CrossTenantDenied(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgA, camID, siteID := findTestCamera(t, db, ctx)
	orgB := "fake-org-b-det-" + uuid.NewString()

	mvID, arID := buildDetectionFixture(t, db, ctx, orgA, camID)

	base := time.Now().UTC().Truncate(time.Millisecond)

	d, err := db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgA,
		SiteID:          &siteID,
		CameraID:        camID,
		DetectedAt:      base,
		DetectionClass:  "no-vest",
		DetectionDomain: "ppe",
		Confidence:      0.88,
		ModelVersionID:  mvID,
		AnalysisRunID:   arID,
		Source:          "live",
	})
	if err != nil {
		t.Fatalf("InsertDetection (org A): %v", err)
	}

	// GetDetection as org B — must return nil (not found / not owned).
	got, err := db.GetDetection(ctx, d.ID, orgB)
	if err != nil {
		t.Fatalf("GetDetection org B: %v", err)
	}
	if got != nil {
		t.Error("cross-tenant leak: GetDetection returned a row for wrong org")
	}

	// ListDetectionsCurrent as org B — must not contain org A's row.
	rows, err := db.ListDetectionsCurrent(ctx, database.DetectionCurrentFilter{
		OrganizationID: orgB,
		Since:          base.Add(-time.Second),
		Until:          base.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("ListDetectionsCurrent org B: %v", err)
	}
	for _, r := range rows {
		if r.ID == d.ID {
			t.Error("cross-tenant leak: org B can see org A's detection in ListDetectionsCurrent")
		}
	}
}
