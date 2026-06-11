package database_test

// timeline_buckets_test.go — regression coverage for the cross-camera
// timeline leak.
//
// Bug: GetTimelineBuckets aggregated events across EVERY camera whenever it
// was handed an empty cameraIDs slice (neither the len==1 nor the len>1
// branch ran, so no camera_id predicate was emitted). Combined with the
// handler silently dropping non-UUID camera_ids params, a request scoped to
// one camera could return another camera's events — e.g. 504's events
// surfaced on the 5001 timeline.
//
// Invariant under test: an empty camera filter means ZERO rows, never "all
// cameras," and a single-camera filter returns only that camera's events.
//
// Integration test — runs against the same real DB as the rest of this
// package (skips when DATABASE_URL is unset). Serialized with -p 1 per the
// package convention; uses unique IDs so it never collides with other rows.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/testutil"
)

func TestGetTimelineBuckets_EmptyCameraIDsReturnsNoEvents(t *testing.T) {
	db := testutil.IntegrationDB(t)
	ctx := context.Background()

	// Two cameras in a private window. Camera A gets events; camera B gets
	// none. If the empty-filter guard regresses, an empty cameraIDs request
	// would aggregate A's (and everyone else's) events instead of returning
	// nothing.
	camA := seedTimelineCamera(t, db, ctx, "timeline-leak-A")
	camB := seedTimelineCamera(t, db, ctx, "timeline-leak-B")

	// Anchor the window far in the past so no other test/seed data lands in
	// it, then drop A's events squarely inside it.
	base := time.Date(2031, 3, 14, 9, 0, 0, 0, time.UTC)
	start := base.Add(-1 * time.Minute)
	end := base.Add(10 * time.Minute)
	const aEvents = 3
	for i := 0; i < aEvents; i++ {
		insertTimelineEvent(t, db, ctx, camA, base.Add(time.Duration(i)*time.Minute), "motion")
	}

	// 1) Empty cameraIDs MUST return zero buckets — never "all cameras."
	//    This is the defense-in-depth guard that stops the leak regardless of
	//    how the caller mangled its camera_ids param.
	emptyBuckets, err := db.GetTimelineBuckets(ctx, nil, start, end, 1)
	if err != nil {
		t.Fatalf("GetTimelineBuckets(empty): %v", err)
	}
	if got := totalEventCount(emptyBuckets); got != 0 {
		t.Fatalf("empty cameraIDs returned %d events across %d buckets; want 0 (empty filter must NOT mean all cameras)", got, len(emptyBuckets))
	}

	// 2) Filtering to camera A returns exactly A's events.
	aBuckets, err := db.GetTimelineBuckets(ctx, []uuid.UUID{camA}, start, end, 1)
	if err != nil {
		t.Fatalf("GetTimelineBuckets(camA): %v", err)
	}
	if got := totalEventCount(aBuckets); got != aEvents {
		t.Fatalf("camera A timeline returned %d events; want %d", got, aEvents)
	}

	// 3) Filtering to camera B (no events) returns zero — proving A's events
	//    do not leak onto B's timeline even though they share the window.
	bBuckets, err := db.GetTimelineBuckets(ctx, []uuid.UUID{camB}, start, end, 1)
	if err != nil {
		t.Fatalf("GetTimelineBuckets(camB): %v", err)
	}
	if got := totalEventCount(bBuckets); got != 0 {
		t.Fatalf("camera B timeline returned %d events; want 0 (camera A's events leaked across)", got)
	}
}

// seedTimelineCamera creates a throwaway camera and returns its UUID.
func seedTimelineCamera(t *testing.T, db *database.DB, ctx context.Context, name string) uuid.UUID {
	t.Helper()
	c := &database.Camera{
		Name:         name + "-" + uuid.NewString()[:8],
		OnvifAddress: "127.0.0.1:0",
		Status:       "offline",
	}
	if err := db.CreateCamera(ctx, c); err != nil {
		t.Fatalf("CreateCamera(%s): %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(ctx, `DELETE FROM events WHERE camera_id = $1`, c.ID)
		_, _ = db.Pool.Exec(ctx, `DELETE FROM cameras WHERE id = $1`, c.ID)
	})
	return c.ID
}

// insertTimelineEvent inserts a single event row at an exact time so the test
// controls which bucket/window it lands in.
func insertTimelineEvent(t *testing.T, db *database.DB, ctx context.Context, camID uuid.UUID, at time.Time, eventType string) {
	t.Helper()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO events (camera_id, event_time, event_type, details)
		VALUES ($1, $2, $3, '{}'::jsonb)`, camID, at, eventType)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

// totalEventCount sums Total across all buckets.
func totalEventCount(buckets []database.TimelineBucket) int {
	total := 0
	for _, b := range buckets {
		total += b.Total
	}
	return total
}
