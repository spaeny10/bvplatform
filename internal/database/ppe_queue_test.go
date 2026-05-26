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

// TestInsertAndListPPEQueue inserts one violation row and verifies it round-trips.
func TestInsertAndListPPEQueue(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	// We need a real camera+org+site for FK constraints. Use the existing
	// test-seed data or insert minimal rows. For the integration test we
	// skip if we can't find a suitable camera (the CI container has migrations
	// applied so some seed data should exist).
	orgID, camID, siteID := findTestCamera(t, db, ctx)

	bbJSON := json.RawMessage(`[{"class":"no-vest","confidence":0.72}]`)
	ins := database.PPEQueueInsert{
		OrganizationID:    orgID,
		CameraID:          camID,
		SiteID:            &siteID,
		FramePath:         orgID + "/2026-05-26/1748894400000.jpg",
		FrameToken:        "test-token",
		FrameTokenExpires: time.Now().Add(5 * time.Minute),
		DetectionClass:    "no-vest",
		MissingLabel:      "Hi-Vis Vest",
		Confidence:        0.72,
		BoundingBoxes:     bbJSON,
		YOLOModel:         "ppe.pt",
	}

	id, err := db.InsertPPEQueueEntry(ctx, ins)
	if err != nil {
		t.Fatalf("InsertPPEQueueEntry: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected non-nil UUID from insert")
	}

	// List and verify the row comes back.
	rows, err := db.ListPPEQueueEntries(ctx, database.PPEQueueFilter{
		OrganizationID: orgID,
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("ListPPEQueueEntries: %v", err)
	}

	found := false
	for _, r := range rows {
		if r.ID == id {
			found = true
			if r.DetectionClass != "no-vest" {
				t.Errorf("detection_class: want 'no-vest', got %q", r.DetectionClass)
			}
			if r.MissingLabel != "Hi-Vis Vest" {
				t.Errorf("missing_label: want 'Hi-Vis Vest', got %q", r.MissingLabel)
			}
			if r.Status != "pending" {
				t.Errorf("status: want 'pending', got %q", r.Status)
			}
			if r.OrganizationID != orgID {
				t.Errorf("organization_id mismatch")
			}
		}
	}
	if !found {
		t.Error("inserted row not found in list")
	}
}

// TestUpdatePPEQueueStatus verifies that reviewing a row flips status + sets reviewed_at.
func TestUpdatePPEQueueStatus(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)
	userID := uuid.New() // fake reviewer; no FK on reviewed_by for test

	ins := database.PPEQueueInsert{
		OrganizationID:    orgID,
		CameraID:          camID,
		SiteID:            &siteID,
		FramePath:         orgID + "/2026-05-26/test.jpg",
		FrameToken:        "tok",
		FrameTokenExpires: time.Now().Add(5 * time.Minute),
		DetectionClass:    "no-hat",
		MissingLabel:      "Hard Hat",
		Confidence:        0.80,
		BoundingBoxes:     json.RawMessage("[]"),
		YOLOModel:         "ppe.pt",
	}
	id, err := db.InsertPPEQueueEntry(ctx, ins)
	if err != nil {
		t.Fatalf("InsertPPEQueueEntry: %v", err)
	}

	notes := "confirmed violation"
	if err := db.UpdatePPEQueueStatus(ctx, id, orgID, userID, "reviewed_violation", &notes); err != nil {
		t.Fatalf("UpdatePPEQueueStatus: %v", err)
	}

	row, err := db.GetPPEQueueEntry(ctx, id, orgID)
	if err != nil {
		t.Fatalf("GetPPEQueueEntry: %v", err)
	}
	if row == nil {
		t.Fatal("GetPPEQueueEntry returned nil after update")
	}
	if row.Status != "reviewed_violation" {
		t.Errorf("status: want 'reviewed_violation', got %q", row.Status)
	}
	if row.ReviewedAt == nil {
		t.Error("reviewed_at should be non-nil after update")
	}
	if row.Notes == nil || *row.Notes != notes {
		t.Errorf("notes: want %q, got %v", notes, row.Notes)
	}
}

// TestPPEQueue_CrossTenantDenied verifies org B cannot see org A's findings.
func TestPPEQueue_CrossTenantDenied(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgA, camID, siteID := findTestCamera(t, db, ctx)
	orgB := "fake-org-b-" + uuid.NewString() // different org with no rows

	ins := database.PPEQueueInsert{
		OrganizationID:    orgA,
		CameraID:          camID,
		SiteID:            &siteID,
		FramePath:         orgA + "/2026-05-26/crosstenant.jpg",
		FrameToken:        "tok",
		FrameTokenExpires: time.Now().Add(5 * time.Minute),
		DetectionClass:    "no-vest",
		MissingLabel:      "Hi-Vis Vest",
		Confidence:        0.75,
		BoundingBoxes:     json.RawMessage("[]"),
		YOLOModel:         "ppe.pt",
	}
	insertedID, err := db.InsertPPEQueueEntry(ctx, ins)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// List as org B — should get empty result.
	rows, err := db.ListPPEQueueEntries(ctx, database.PPEQueueFilter{
		OrganizationID: orgB,
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("list org B: %v", err)
	}
	for _, r := range rows {
		if r.ID == insertedID {
			t.Error("cross-tenant leak: org B can see org A's pending review row")
		}
	}

	// GetPPEQueueEntry as org B — should return nil.
	row, err := db.GetPPEQueueEntry(ctx, insertedID, orgB)
	if err != nil {
		t.Fatalf("get org B: %v", err)
	}
	if row != nil {
		t.Error("cross-tenant leak: GetPPEQueueEntry returned a row for wrong org")
	}
}

// findTestCamera finds the first camera+org+site triple in the DB suitable for
// FK-constrained test inserts. Skips the test if none found.
// Returns orgID and siteID as strings (TEXT PKs) and camID as UUID.
func findTestCamera(t *testing.T, db *database.DB, ctx context.Context) (orgID string, camID uuid.UUID, siteID string) {
	t.Helper()
	row := db.Pool.QueryRow(ctx, `
		SELECT s.organization_id, c.id, s.id
		FROM cameras c
		JOIN sites s ON s.id = c.site_id
		LIMIT 1`)
	var camStr string
	if err := row.Scan(&orgID, &camStr, &siteID); err != nil {
		t.Skip("no camera with site assignment found; skipping PPE integration test")
	}
	var err error
	camID, err = uuid.Parse(camStr)
	if err != nil || camID == uuid.Nil {
		t.Skip("could not parse camera UUID")
	}
	if orgID == "" || siteID == "" {
		t.Skip("empty org or site ID")
	}
	return
}
