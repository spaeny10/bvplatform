package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/testutil"
)

// TestInsertAndListTrackFrames inserts one raw frame row and verifies round-trip.
func TestInsertAndListTrackFrames(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)

	now := time.Now().UTC().Truncate(time.Second)
	ins := database.PersonTrackFrameInsert{
		Time:           now,
		CameraID:       camID,
		SiteID:         &siteID,
		OrganizationID: orgID,
		PersonCount:    3,
		FrameSource:    "test",
	}
	if err := db.InsertTrackFrame(ctx, ins); err != nil {
		t.Fatalf("InsertTrackFrame: %v", err)
	}

	// Verify via the bucket aggregation query on the same window.
	start := now.Add(-time.Second)
	end := now.Add(time.Second)
	bucket, err := db.AggregateFramesIntoBucket(ctx, camID, start, end, 5)
	if err != nil {
		t.Fatalf("AggregateFramesIntoBucket: %v", err)
	}
	if bucket == nil {
		t.Fatal("expected aggregation result, got nil")
	}
	if bucket.FrameCount < 1 {
		t.Errorf("frame_count: want >= 1, got %d", bucket.FrameCount)
	}
	if bucket.PeakPersonCount < 3 {
		t.Errorf("peak_person_count: want >= 3, got %d", bucket.PeakPersonCount)
	}
}

// TestUpsertTrackBucket verifies the upsert: second write wins, no duplicate rows.
func TestUpsertTrackBucket(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, camID, siteID := findTestCamera(t, db, ctx)

	base := time.Now().UTC().Truncate(5 * time.Minute).Add(-10 * time.Minute)

	b1 := database.PersonTrackBucket{
		CameraID:        camID,
		SiteID:          &siteID,
		OrganizationID:  orgID,
		BucketStart:     base,
		BucketMinutes:   5,
		PersonMinutes:   5.0,
		PeakPersonCount: 2,
		FrameCount:      10,
		ViolationCount:  1,
	}
	if err := db.UpsertTrackBucket(ctx, b1); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Second upsert with different values — should overwrite.
	b2 := b1
	b2.PersonMinutes = 7.5
	b2.PeakPersonCount = 3
	if err := db.UpsertTrackBucket(ctx, b2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	// Query and check the final state.
	buckets, err := db.ListTrackBuckets(ctx, database.TrackBucketFilter{
		OrganizationID: orgID,
		CameraID:       &camID,
		Start:          base.Add(-time.Second),
		End:            base.Add(time.Second),
		BucketMinutes:  5,
	})
	if err != nil {
		t.Fatalf("ListTrackBuckets: %v", err)
	}

	found := 0
	for _, b := range buckets {
		if b.CameraID == camID && b.BucketStart.Equal(base) {
			found++
			if b.PersonMinutes != 7.5 {
				t.Errorf("person_minutes: want 7.5 after upsert, got %f", b.PersonMinutes)
			}
			if b.PeakPersonCount != 3 {
				t.Errorf("peak_person_count: want 3 after upsert, got %d", b.PeakPersonCount)
			}
		}
	}
	if found == 0 {
		t.Error("bucket not found after upsert")
	}
	if found > 1 {
		t.Errorf("upsert created duplicates: found %d rows for same key", found)
	}
}

// TestListTrackBuckets_CrossTenantDenied verifies org B cannot see org A's buckets.
func TestListTrackBuckets_CrossTenantDenied(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgA, camID, siteID := findTestCamera(t, db, ctx)
	orgB := "fake-org-b-tracking-" + uuid.NewString()

	base := time.Now().UTC().Truncate(5 * time.Minute).Add(-20 * time.Minute)
	b := database.PersonTrackBucket{
		CameraID:       camID,
		SiteID:         &siteID,
		OrganizationID: orgA,
		BucketStart:    base,
		BucketMinutes:  5,
		PersonMinutes:  2.5,
		FrameCount:     5,
	}
	if err := db.UpsertTrackBucket(ctx, b); err != nil {
		t.Fatalf("upsert for org A: %v", err)
	}

	// Query as org B — should get empty result.
	rows, err := db.ListTrackBuckets(ctx, database.TrackBucketFilter{
		OrganizationID: orgB,
		Start:          base.Add(-time.Minute),
		End:            base.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ListTrackBuckets org B: %v", err)
	}
	for _, row := range rows {
		if row.CameraID == camID && row.BucketStart.Equal(base) {
			t.Error("cross-tenant leak: org B can see org A's tracking bucket")
		}
	}
}
