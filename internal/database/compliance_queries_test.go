package database_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/testutil"
)

// seedViolation inserts one pending_review_queue row with the given status.
// Returns the inserted row ID.
func seedViolation(t *testing.T, db *database.DB, ctx context.Context, orgID string, camID uuid.UUID, siteID string, status string, createdAt time.Time) uuid.UUID {
	t.Helper()
	bbJSON := json.RawMessage(`[]`)
	ins := database.PPEQueueInsert{
		OrganizationID:    orgID,
		CameraID:          camID,
		SiteID:            &siteID,
		FramePath:         orgID + "/" + uuid.NewString() + ".jpg",
		FrameToken:        "tok-" + uuid.NewString(),
		FrameTokenExpires: createdAt.Add(24 * time.Hour),
		DetectionClass:    "no-vest",
		MissingLabel:      "Hi-Vis Vest",
		Confidence:        0.80,
		BoundingBoxes:     bbJSON,
		YOLOModel:         "ppe.pt",
	}
	id, err := db.InsertPPEQueueEntry(ctx, ins)
	if err != nil {
		t.Fatalf("seedViolation insert: %v", err)
	}
	if status != "pending" {
		// Update to desired status.
		reviewerID := uuid.New()
		notes := "seeded"
		if err := db.UpdatePPEQueueStatus(ctx, id, orgID, reviewerID, status, &notes); err != nil {
			t.Fatalf("seedViolation update status: %v", err)
		}
	}
	return id
}

func TestComplianceSummary_BasicAggregation(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	// Seed 3 violations + 2 compliant in the period.
	for i := 0; i < 3; i++ {
		seedViolation(t, db, ctx, orgID, camID, siteID, "reviewed_violation", now.Add(-time.Duration(i)*time.Hour))
	}
	for i := 0; i < 2; i++ {
		seedViolation(t, db, ctx, orgID, camID, siteID, "reviewed_compliant", now.Add(-time.Duration(i+3)*time.Hour))
	}

	f := database.ComplianceFilter{
		OrgID:     orgID,
		Start:     start,
		End:       now,
		TruncUnit: "day",
	}

	headline, err := db.GetComplianceHeadline(ctx, f)
	if err != nil {
		t.Fatalf("GetComplianceHeadline: %v", err)
	}
	if headline.TotalViolations < 3 {
		t.Errorf("total_violations: want >= 3, got %d", headline.TotalViolations)
	}
	if headline.TotalReviewed < 5 {
		t.Errorf("total_reviewed: want >= 5, got %d", headline.TotalReviewed)
	}

	cameras, err := db.GetComplianceTopCameras(ctx, f, 5)
	if err != nil {
		t.Fatalf("GetComplianceTopCameras: %v", err)
	}
	if len(cameras) == 0 {
		t.Error("expected at least one camera in top cameras")
	}
}

func TestComplianceSummary_CrossTenantIsolation(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgA, camID, siteID := findTestCamera(t, db, ctx)
	orgB := "fake-org-b-" + uuid.NewString()[:8]

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	// Seed 2 violations for org A.
	seedViolation(t, db, ctx, orgA, camID, siteID, "reviewed_violation", now.Add(-1*time.Hour))
	seedViolation(t, db, ctx, orgA, camID, siteID, "reviewed_violation", now.Add(-2*time.Hour))

	// Query as org B — should see zero violations.
	fB := database.ComplianceFilter{
		OrgID:     orgB,
		Start:     start,
		End:       now,
		TruncUnit: "day",
	}

	headline, err := db.GetComplianceHeadline(ctx, fB)
	if err != nil {
		t.Fatalf("GetComplianceHeadline (org B): %v", err)
	}
	if headline.TotalViolations != 0 {
		t.Errorf("cross-tenant leak: org B sees %d violations (want 0)", headline.TotalViolations)
	}

	cameras, err := db.GetComplianceTopCameras(ctx, fB, 5)
	if err != nil {
		t.Fatalf("GetComplianceTopCameras (org B): %v", err)
	}
	if len(cameras) != 0 {
		t.Errorf("cross-tenant leak: org B top cameras: %d rows (want 0)", len(cameras))
	}
}

func TestComplianceSummary_EmptyPeriod(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, _, _ := findTestCamera(t, db, ctx)
	// Use a future date range that has no data.
	future := time.Now().UTC().AddDate(1, 0, 0)

	f := database.ComplianceFilter{
		OrgID:     orgID,
		Start:     future,
		End:       future.Add(24 * time.Hour),
		TruncUnit: "day",
	}

	headline, err := db.GetComplianceHeadline(ctx, f)
	if err != nil {
		t.Fatalf("GetComplianceHeadline: %v", err)
	}
	if headline.TotalViolations != 0 {
		t.Errorf("want 0 violations in empty period, got %d", headline.TotalViolations)
	}
	if headline.TotalReviewed != 0 {
		t.Errorf("want 0 reviewed in empty period, got %d", headline.TotalReviewed)
	}

	series, err := db.GetComplianceViolationsOverTime(ctx, f)
	if err != nil {
		t.Fatalf("GetComplianceViolationsOverTime: %v", err)
	}
	if len(series) != 0 {
		t.Errorf("want empty time series, got %d buckets", len(series))
	}
}

func TestComplianceSummary_OccupancyAbsent(t *testing.T) {
	// GetComplianceOccupancy should return (nil, nil, nil) and not error
	// when person_track_buckets has no data (or the table is absent).
	// With C-02 shipped, the table exists but may have no rows — still valid.
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, _, _ := findTestCamera(t, db, ctx)
	future := time.Now().UTC().AddDate(1, 0, 0)

	f := database.ComplianceFilter{
		OrgID:     orgID,
		Start:     future,
		End:       future.Add(24 * time.Hour),
		TruncUnit: "day",
	}

	buckets, hours, err := db.GetComplianceOccupancy(ctx, f)
	if err != nil {
		t.Fatalf("GetComplianceOccupancy should not error: %v", err)
	}
	// When there are no rows, buckets should be empty and hours may be a pointer to 0.
	_ = buckets
	_ = hours // nil or &0.0 — both acceptable
}

func TestComplianceSummary_SiteFilter(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -7)

	// Seed 1 violation for the known siteID.
	seedViolation(t, db, ctx, orgID, camID, siteID, "reviewed_violation", now.Add(-30*time.Minute))

	// Query with site filter — should see the violation.
	fFiltered := database.ComplianceFilter{
		OrgID:     orgID,
		SiteID:    &siteID,
		Start:     start,
		End:       now,
		TruncUnit: "day",
	}
	headline, err := db.GetComplianceHeadline(ctx, fFiltered)
	if err != nil {
		t.Fatalf("GetComplianceHeadline (filtered): %v", err)
	}
	if headline.TotalViolations < 1 {
		t.Errorf("site filter: want >= 1 violation for siteID %s, got %d", siteID, headline.TotalViolations)
	}

	// Query with a different (non-existent) site — should see zero.
	otherSite := "site-nonexistent-" + uuid.NewString()[:8]
	fOther := database.ComplianceFilter{
		OrgID:     orgID,
		SiteID:    &otherSite,
		Start:     start,
		End:       now,
		TruncUnit: "day",
	}
	headlineOther, err := db.GetComplianceHeadline(ctx, fOther)
	if err != nil {
		t.Fatalf("GetComplianceHeadline (other site): %v", err)
	}
	if headlineOther.TotalViolations != 0 {
		t.Errorf("site filter: want 0 violations for nonexistent site, got %d", headlineOther.TotalViolations)
	}
}

func TestVerifySiteOwnership_Match(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, _, siteID := findTestCamera(t, db, ctx)
	ok, err := db.VerifySiteOwnership(ctx, siteID, orgID)
	if err != nil {
		t.Fatalf("VerifySiteOwnership: %v", err)
	}
	if !ok {
		t.Errorf("expected site %s to belong to org %s", siteID, orgID)
	}
}

func TestVerifySiteOwnership_Mismatch(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	_, _, siteID := findTestCamera(t, db, ctx)
	wrongOrg := "fake-org-" + uuid.NewString()[:8]

	ok, err := db.VerifySiteOwnership(ctx, siteID, wrongOrg)
	if err != nil {
		t.Fatalf("VerifySiteOwnership: %v", err)
	}
	if ok {
		t.Error("expected mismatch for wrong org, but VerifySiteOwnership returned true")
	}
}
