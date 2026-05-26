package tracking

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// AggregatorDB is the subset of database.DB the aggregator needs.
type AggregatorDB interface {
	AggregateFramesIntoBucket(ctx context.Context, cameraID uuid.UUID, windowStart, windowEnd time.Time, bucketMinutes int) (*database.PersonTrackBucket, error)
	CountPendingReviewViolations(ctx context.Context, cameraID uuid.UUID, windowStart, windowEnd time.Time) (int, error)
	UpsertTrackBucket(ctx context.Context, b database.PersonTrackBucket) error
	ListCamerasWithUnbucketedFrames(ctx context.Context, sinceTime, beforeTime time.Time, bucketMinutes int) ([]uuid.UUID, error)
}

// Aggregator rolls up person_track_frames into person_track_buckets on a
// fixed cadence. Each tick covers the previous complete bucket window.
// On startup it also runs a backfill sweep to catch any windows missed
// while the worker was down.
type Aggregator struct {
	db            AggregatorDB
	bucketMinutes int
}

// NewAggregator creates an aggregator. bucketMinutes must be > 0 (default 5).
func NewAggregator(db AggregatorDB, bucketMinutes int) *Aggregator {
	if bucketMinutes <= 0 {
		bucketMinutes = 5
	}
	return &Aggregator{db: db, bucketMinutes: bucketMinutes}
}

// Start launches the aggregator loop in a goroutine. It fires immediately
// (backfill), then every bucketMinutes thereafter.
func (a *Aggregator) Start(ctx context.Context) {
	log.Printf("[TRACKING] aggregator started (bucket=%dmin)", a.bucketMinutes)
	go a.run(ctx)
}

func (a *Aggregator) run(ctx context.Context) {
	// Immediate backfill on startup.
	a.backfill(ctx)

	interval := time.Duration(a.bucketMinutes) * time.Minute
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[TRACKING] aggregator context cancelled, stopping")
			return
		case <-tick.C:
			a.rollup(ctx)
		}
	}
}

// floorToBucket floors t to the nearest bucket boundary.
// For bucketMinutes=5: 14:07 → 14:05, 14:00 → 14:00.
func (a *Aggregator) floorToBucket(t time.Time) time.Time {
	t = t.UTC().Truncate(time.Minute)
	offset := t.Minute() % a.bucketMinutes
	return t.Add(-time.Duration(offset) * time.Minute)
}

// prevBucketWindow returns [start, end) of the last complete bucket window
// relative to now.
func (a *Aggregator) prevBucketWindow() (time.Time, time.Time) {
	now := time.Now().UTC()
	end := a.floorToBucket(now)
	start := end.Add(-time.Duration(a.bucketMinutes) * time.Minute)
	return start, end
}

// rollup runs a single bucket roll-up for the previous complete window.
func (a *Aggregator) rollup(ctx context.Context) {
	start, end := a.prevBucketWindow()
	a.rollupWindow(ctx, start, end)
}

// rollupWindow aggregates all cameras with frames in [start, end).
func (a *Aggregator) rollupWindow(ctx context.Context, start, end time.Time) {
	// Find cameras with frames in this window (including already-bucketed
	// ones — the upsert is idempotent so re-running is safe).
	cameras, err := a.db.ListCamerasWithUnbucketedFrames(ctx, start, end, a.bucketMinutes)
	if err != nil {
		log.Printf("[TRACKING] aggregator ListCamerasWithUnbucketedFrames [%s,%s): %v",
			start.Format(time.RFC3339), end.Format(time.RFC3339), err)
		return
	}

	for _, cameraID := range cameras {
		if err := ctx.Err(); err != nil {
			return
		}
		a.rollupCamera(ctx, cameraID, start, end)
	}
}

// rollupCamera aggregates frames for one camera in [start, end) and upserts
// the bucket. This is idempotent — repeated calls for the same window
// overwrite with recomputed values.
func (a *Aggregator) rollupCamera(ctx context.Context, cameraID uuid.UUID, start, end time.Time) {
	bucket, err := a.db.AggregateFramesIntoBucket(ctx, cameraID, start, end, a.bucketMinutes)
	if err != nil {
		log.Printf("[TRACKING] AggregateFramesIntoBucket camera %s: %v", cameraID, err)
		return
	}
	if bucket == nil {
		// No frames in this window — skip.
		return
	}

	// Join violation count from pending_review_queue.
	violations, err := a.db.CountPendingReviewViolations(ctx, cameraID, start, end)
	if err != nil {
		log.Printf("[TRACKING] CountPendingReviewViolations camera %s: %v", cameraID, err)
		// Non-fatal — upsert bucket with violation_count=0 rather than skip.
	}
	bucket.ViolationCount = violations

	if err := a.db.UpsertTrackBucket(ctx, *bucket); err != nil {
		log.Printf("[TRACKING] UpsertTrackBucket camera %s: %v", cameraID, err)
	}
}

// backfill finds all camera+window combinations with raw frames but no
// corresponding bucket row, and aggregates them oldest-first. This handles
// worker downtime: if the worker was down for 1 hour, the next start
// back-fills 12 missing 5-minute buckets.
func (a *Aggregator) backfill(ctx context.Context) {
	now := time.Now().UTC()
	end := a.floorToBucket(now) // exclusive: don't aggregate the current open bucket
	// Look back at most 24h to keep startup fast. Older gaps are
	// covered by the TimescaleDB retention sweep (they would be purged
	// anyway after TrackingRawRetentionDays).
	start := end.Add(-24 * time.Hour)

	log.Printf("[TRACKING] aggregator backfill: scanning [%s, %s)",
		start.Format(time.RFC3339), end.Format(time.RFC3339))

	cameras, err := a.db.ListCamerasWithUnbucketedFrames(ctx, start, end, a.bucketMinutes)
	if err != nil {
		log.Printf("[TRACKING] backfill ListCamerasWithUnbucketedFrames: %v", err)
		return
	}
	if len(cameras) == 0 {
		log.Println("[TRACKING] aggregator backfill: no gaps found")
		return
	}

	log.Printf("[TRACKING] aggregator backfill: %d camera(s) with unbucketed frames", len(cameras))

	// Walk each bucket window from oldest to newest.
	for ws := start; ws.Before(end); ws = ws.Add(time.Duration(a.bucketMinutes) * time.Minute) {
		we := ws.Add(time.Duration(a.bucketMinutes) * time.Minute)
		for _, cameraID := range cameras {
			if err := ctx.Err(); err != nil {
				return
			}
			a.rollupCamera(ctx, cameraID, ws, we)
		}
	}
	log.Println("[TRACKING] aggregator backfill complete")
}
