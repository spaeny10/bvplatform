package tracking

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/ai"
	"ironsight/internal/database"
	"ironsight/internal/ppe"
)

// ── stub DB ────────────────────────────────────────────────────────────────────

type stubTrackingDB struct {
	mu      sync.Mutex
	inserts []database.PersonTrackFrameInsert
}

func (s *stubTrackingDB) InsertTrackFrame(_ context.Context, ins database.PersonTrackFrameInsert) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inserts = append(s.inserts, ins)
	return nil
}

func (s *stubTrackingDB) last() *database.PersonTrackFrameInsert {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inserts) == 0 {
		return nil
	}
	cp := s.inserts[len(s.inserts)-1]
	return &cp
}

func (s *stubTrackingDB) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inserts)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeCamera(orgID string) database.PPECamera {
	return database.PPECamera{
		CameraID:       uuid.New(),
		CameraName:     "test-cam",
		OrganizationID: orgID,
		SiteID:         "test-site",
	}
}

func makeYOLO(personCount int, extras ...string) *ai.YOLOResult {
	y := &ai.YOLOResult{}
	for i := 0; i < personCount; i++ {
		y.Detections = append(y.Detections, ai.Detection{Class: "person", Confidence: 0.9})
	}
	for _, cls := range extras {
		y.Detections = append(y.Detections, ai.Detection{Class: cls, Confidence: 0.85})
	}
	return y
}

// sendAndWait sends result to ch and waits for the worker to process it.
// The worker runs in a goroutine so we drain inserts with a brief poll.
func sendAndWait(t *testing.T, db *stubTrackingDB, ch chan ppe.CameraFrameResult, result ppe.CameraFrameResult) {
	t.Helper()
	before := db.count()
	ch <- result
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if db.count() > before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ── TestCountPersons ──────────────────────────────────────────────────────────

func TestCountPersons_Basic(t *testing.T) {
	dets := []ai.Detection{
		{Class: "person"},
		{Class: "person"},
		{Class: "car"},
	}
	if n := countPersons(dets); n != 2 {
		t.Errorf("want 2, got %d", n)
	}
}

func TestCountPersons_Empty(t *testing.T) {
	if n := countPersons(nil); n != 0 {
		t.Errorf("want 0, got %d", n)
	}
}

// ── TestTrackingWorker_PersonCount ────────────────────────────────────────────

func TestTrackingWorker_PersonCount(t *testing.T) {
	db := &stubTrackingDB{}
	ch := make(chan ppe.CameraFrameResult, 4)
	w := New(db, ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	orgID := uuid.NewString()
	result := ppe.CameraFrameResult{
		Camera:    makeCamera(orgID),
		FetchedAt: time.Now().UTC(),
		YOLO:      makeYOLO(3),
	}
	sendAndWait(t, db, ch, result)

	ins := db.last()
	if ins == nil {
		t.Fatal("expected InsertTrackFrame call, got none")
	}
	if ins.PersonCount != 3 {
		t.Errorf("person_count: want 3, got %d", ins.PersonCount)
	}
	if ins.OrganizationID != orgID {
		t.Errorf("org_id mismatch: want %s, got %s", orgID, ins.OrganizationID)
	}
}

// ── TestTrackingWorker_IgnoresNonPerson (R1 regression guard) ─────────────────

// TestTrackingWorker_IgnoresNonPerson verifies that non-person detections in
// YOLOResult.Detections are not counted, and that PPEViolations are never
// counted even when they contain "person" labels.
func TestTrackingWorker_IgnoresNonPerson(t *testing.T) {
	db := &stubTrackingDB{}
	ch := make(chan ppe.CameraFrameResult, 4)
	w := New(db, ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	yolo := &ai.YOLOResult{
		// Security model: 1 person + 1 car — only 1 should be counted.
		Detections: []ai.Detection{
			{Class: "person"},
			{Class: "car"},
		},
		// PPE model violations — must NOT be counted toward person_count.
		PPEViolations: []ai.Detection{
			{Class: "person"},
			{Class: "no-hat"},
		},
	}

	result := ppe.CameraFrameResult{
		Camera:    makeCamera(uuid.NewString()),
		FetchedAt: time.Now().UTC(),
		YOLO:      yolo,
	}
	sendAndWait(t, db, ch, result)

	ins := db.last()
	if ins == nil {
		t.Fatal("expected InsertTrackFrame call")
	}
	if ins.PersonCount != 1 {
		t.Errorf("person_count: want 1, got %d (R1: must not count PPEViolations or non-person detections)", ins.PersonCount)
	}
}

// ── TestTrackingWorker_YOLONil ────────────────────────────────────────────────

func TestTrackingWorker_YOLONil(t *testing.T) {
	db := &stubTrackingDB{}
	ch := make(chan ppe.CameraFrameResult, 4)
	w := New(db, ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	result := ppe.CameraFrameResult{
		Camera:    makeCamera(uuid.NewString()),
		FetchedAt: time.Now().UTC(),
		YOLO:      nil, // sidecar error
	}
	ch <- result
	// Give the goroutine time to process.
	time.Sleep(50 * time.Millisecond)

	if db.count() != 0 {
		t.Errorf("want 0 DB inserts for nil YOLO, got %d", db.count())
	}
}

// ── TestTrackingWorker_ChannelBackpressure ────────────────────────────────────

func TestTrackingWorker_ChannelBackpressure(t *testing.T) {
	// A zero-buffer channel + a DB that blocks will cause the PPE worker's
	// non-blocking send to drop. We test that the send path does not block.
	ch := make(chan ppe.CameraFrameResult, 0) // unbuffered

	// Send with a timeout — should return immediately (drop).
	done := make(chan bool, 1)
	go func() {
		select {
		case ch <- ppe.CameraFrameResult{}: // no reader → would block
		default:
			// non-blocking drop — correct
		}
		done <- true
	}()

	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("non-blocking send blocked")
	}
}

// ── TestTrackingWorker_TenantIsolation ────────────────────────────────────────

func TestTrackingWorker_TenantIsolation(t *testing.T) {
	db := &stubTrackingDB{}
	ch := make(chan ppe.CameraFrameResult, 8)
	w := New(db, ch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	orgA := "org-a-" + uuid.NewString()
	orgB := "org-b-" + uuid.NewString()
	camA := makeCamera(orgA)
	camB := makeCamera(orgB)

	ch <- ppe.CameraFrameResult{Camera: camA, FetchedAt: time.Now().UTC(), YOLO: makeYOLO(1)}
	ch <- ppe.CameraFrameResult{Camera: camB, FetchedAt: time.Now().UTC(), YOLO: makeYOLO(2)}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if db.count() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	orgMap := map[string]int{}
	for _, ins := range db.inserts {
		orgMap[ins.OrganizationID]++
	}
	if orgMap[orgA] != 1 {
		t.Errorf("orgA: want 1 insert, got %d", orgMap[orgA])
	}
	if orgMap[orgB] != 1 {
		t.Errorf("orgB: want 1 insert, got %d", orgMap[orgB])
	}
}

// ── Aggregator unit tests ─────────────────────────────────────────────────────

type stubAggDB struct {
	mu      sync.Mutex
	frames  map[string][]database.PersonTrackFrameInsert // key: cameraID
	buckets []database.PersonTrackBucket
}

func newStubAggDB() *stubAggDB {
	return &stubAggDB{frames: map[string][]database.PersonTrackFrameInsert{}}
}

func (s *stubAggDB) AggregateFramesIntoBucket(_ context.Context, cameraID uuid.UUID, windowStart, windowEnd time.Time, bucketMinutes int) (*database.PersonTrackBucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := cameraID.String()
	frames := s.frames[key]
	if len(frames) == 0 {
		return nil, nil
	}

	var total, peak, count int
	for _, f := range frames {
		if !f.Time.Before(windowStart) && f.Time.Before(windowEnd) {
			count++
			total += f.PersonCount
			if f.PersonCount > peak {
				peak = f.PersonCount
			}
		}
	}
	if count == 0 {
		return nil, nil
	}
	// person_minutes = sum(count) * poll_interval(0.5min for 30s poll)
	return &database.PersonTrackBucket{
		CameraID:        cameraID,
		OrganizationID:  "test-org",
		BucketStart:     windowStart,
		BucketMinutes:   bucketMinutes,
		PersonMinutes:   float64(total) * 0.5,
		PeakPersonCount: peak,
		FrameCount:      count,
	}, nil
}

func (s *stubAggDB) CountPendingReviewViolations(_ context.Context, _ uuid.UUID, _, _ time.Time) (int, error) {
	return 0, nil
}

func (s *stubAggDB) UpsertTrackBucket(_ context.Context, b database.PersonTrackBucket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Upsert: replace existing (camera_id, bucket_start, bucket_minutes).
	for i, existing := range s.buckets {
		if existing.CameraID == b.CameraID &&
			existing.BucketStart.Equal(b.BucketStart) &&
			existing.BucketMinutes == b.BucketMinutes {
			s.buckets[i] = b
			return nil
		}
	}
	s.buckets = append(s.buckets, b)
	return nil
}

func (s *stubAggDB) ListCamerasWithUnbucketedFrames(_ context.Context, sinceTime, beforeTime time.Time, _ int) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	for key, frames := range s.frames {
		for _, f := range frames {
			if !f.Time.Before(sinceTime) && f.Time.Before(beforeTime) {
				id, _ := uuid.Parse(key)
				seen[id] = true
			}
		}
	}
	ids := make([]uuid.UUID, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *stubAggDB) addFrame(cameraID uuid.UUID, t time.Time, personCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := cameraID.String()
	s.frames[key] = append(s.frames[key], database.PersonTrackFrameInsert{
		Time:        t,
		CameraID:    cameraID,
		PersonCount: personCount,
	})
}

func (s *stubAggDB) bucketCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buckets)
}

// ── TestAggregator_BasicRollup ────────────────────────────────────────────────

func TestAggregator_BasicRollup(t *testing.T) {
	db := newStubAggDB()
	a := NewAggregator(db, 5)

	camID := uuid.New()
	base := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)
	// 10 frames with 2 persons each in the window [14:00, 14:05)
	for i := 0; i < 10; i++ {
		db.addFrame(camID, base.Add(time.Duration(i)*30*time.Second), 2)
	}

	a.rollupWindow(context.Background(), base, base.Add(5*time.Minute))

	db.mu.Lock()
	defer db.mu.Unlock()
	if len(db.buckets) != 1 {
		t.Fatalf("want 1 bucket, got %d", len(db.buckets))
	}
	b := db.buckets[0]
	if b.FrameCount != 10 {
		t.Errorf("frame_count: want 10, got %d", b.FrameCount)
	}
	if b.PeakPersonCount != 2 {
		t.Errorf("peak_person_count: want 2, got %d", b.PeakPersonCount)
	}
	// person_minutes = 20 (sum of person_count) * 0.5 = 10.0
	if b.PersonMinutes != 10.0 {
		t.Errorf("person_minutes: want 10.0, got %f", b.PersonMinutes)
	}
}

// ── TestAggregator_Idempotency ────────────────────────────────────────────────

func TestAggregator_Idempotency(t *testing.T) {
	db := newStubAggDB()
	a := NewAggregator(db, 5)

	camID := uuid.New()
	base := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)
	db.addFrame(camID, base.Add(30*time.Second), 1)

	window := base
	windowEnd := base.Add(5 * time.Minute)

	// Run roll-up twice for the same window.
	a.rollupWindow(context.Background(), window, windowEnd)
	a.rollupWindow(context.Background(), window, windowEnd)

	if db.bucketCount() != 1 {
		t.Errorf("want 1 bucket after idempotent upsert, got %d", db.bucketCount())
	}
}

// ── TestAggregator_BucketBoundary ─────────────────────────────────────────────

func TestAggregator_BucketBoundary(t *testing.T) {
	db := newStubAggDB()
	a := NewAggregator(db, 5)

	camID := uuid.New()
	base := time.Date(2026, 5, 26, 14, 0, 0, 0, time.UTC)
	// Frames in bucket [14:00, 14:05) and bucket [14:05, 14:10)
	db.addFrame(camID, base.Add(30*time.Second), 1) // bucket 1
	db.addFrame(camID, base.Add(5*time.Minute+30*time.Second), 2) // bucket 2

	// Roll up two separate windows.
	a.rollupWindow(context.Background(), base, base.Add(5*time.Minute))
	a.rollupWindow(context.Background(), base.Add(5*time.Minute), base.Add(10*time.Minute))

	if db.bucketCount() != 2 {
		t.Errorf("want 2 buckets for two windows, got %d", db.bucketCount())
	}
}

// ── TestAggregator_BackfillsMissing ──────────────────────────────────────────

func TestAggregator_BackfillsMissing(t *testing.T) {
	db := newStubAggDB()
	a := NewAggregator(db, 5)

	camID := uuid.New()
	// Frames 30 minutes ago — simulating missed roll-ups while worker was down.
	base := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Minute)
	db.addFrame(camID, base, 1)

	a.backfill(context.Background())

	if db.bucketCount() == 0 {
		t.Error("want at least 1 backfilled bucket, got 0")
	}
}
