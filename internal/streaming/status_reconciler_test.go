package streaming

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakePathsLister implements PathsLister for tests.
type fakePathsLister struct {
	ready map[string]bool
	err   error
}

func (f *fakePathsLister) FetchPathsReady(_ context.Context) (map[string]bool, error) {
	return f.ready, f.err
}

// fakeCameraStore records the DB calls made by the reconciler.
type fakeCameraStore struct {
	cameras       []database.Camera
	statusUpdates map[uuid.UUID]string // last status written ("online" / "offline")
	streamErrors  map[uuid.UUID]string // last error string written; "" = cleared
}

func newFakeStore(cameras ...database.Camera) *fakeCameraStore {
	return &fakeCameraStore{
		cameras:       cameras,
		statusUpdates: make(map[uuid.UUID]string),
		streamErrors:  make(map[uuid.UUID]string),
	}
}

func (s *fakeCameraStore) ListCameras(_ context.Context) ([]database.Camera, error) {
	return s.cameras, nil
}

func (s *fakeCameraStore) UpdateCameraStatus(_ context.Context, id uuid.UUID, status string) error {
	s.statusUpdates[id] = status
	return nil
}

// ClearCameraStreamError models "SET status='online', last_stream_error=NULL".
func (s *fakeCameraStore) ClearCameraStreamError(_ context.Context, id uuid.UUID) error {
	s.statusUpdates[id] = "online"
	s.streamErrors[id] = ""
	return nil
}

// UpdateCameraStreamError models "SET status='offline', last_stream_error=$1".
func (s *fakeCameraStore) UpdateCameraStreamError(_ context.Context, id uuid.UUID, msg string) error {
	s.statusUpdates[id] = "offline"
	s.streamErrors[id] = msg
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func cam(id uuid.UUID, status string) database.Camera {
	return database.Camera{ID: id, Name: "test-" + id.String()[:8], Status: status}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestReconciler_ReadyBecomesOnline: a camera that is currently "offline" whose
// main path is ready in mediamtx must be set online and have its error cleared.
func TestReconciler_ReadyBecomesOnline(t *testing.T) {
	id := uuid.New()
	store := newFakeStore(cam(id, "offline"))
	lister := &fakePathsLister{ready: map[string]bool{id.String(): true}}
	miss := make(map[uuid.UUID]int)

	reconcileOnce(context.Background(), store, lister, miss)

	if store.statusUpdates[id] != "online" {
		t.Fatalf("expected status online, got %q", store.statusUpdates[id])
	}
	if store.streamErrors[id] != "" {
		t.Fatalf("expected stream error cleared, got %q", store.streamErrors[id])
	}
	if miss[id] != 0 {
		t.Fatalf("expected miss counter reset to 0, got %d", miss[id])
	}
}

// TestReconciler_AlreadyOnlineNoWrite: a camera already "online" with a ready
// path must not trigger a redundant DB write (no status update recorded).
func TestReconciler_AlreadyOnlineNoWrite(t *testing.T) {
	id := uuid.New()
	store := newFakeStore(cam(id, "online"))
	lister := &fakePathsLister{ready: map[string]bool{id.String(): true}}
	miss := make(map[uuid.UUID]int)

	reconcileOnce(context.Background(), store, lister, miss)

	if _, wrote := store.statusUpdates[id]; wrote {
		t.Fatalf("expected no DB write for already-online camera, got %q", store.statusUpdates[id])
	}
}

// TestReconciler_OneMissNoChange: one consecutive miss must NOT flip the camera
// offline (debounce: threshold is 2).
func TestReconciler_OneMissNoChange(t *testing.T) {
	id := uuid.New()
	store := newFakeStore(cam(id, "online"))
	lister := &fakePathsLister{ready: map[string]bool{}} // path absent
	miss := make(map[uuid.UUID]int)

	reconcileOnce(context.Background(), store, lister, miss)

	if _, wrote := store.statusUpdates[id]; wrote {
		t.Fatalf("expected no DB write after 1 miss (debounce), got %q", store.statusUpdates[id])
	}
	if miss[id] != 1 {
		t.Fatalf("expected miss counter = 1, got %d", miss[id])
	}
}

// TestReconciler_TwoMissesBecomesOffline: two consecutive misses must flip the
// camera offline and record the reconciler reason string.
func TestReconciler_TwoMissesBecomesOffline(t *testing.T) {
	id := uuid.New()
	store := newFakeStore(cam(id, "online"))
	lister := &fakePathsLister{ready: map[string]bool{}} // path absent both cycles
	miss := make(map[uuid.UUID]int)

	reconcileOnce(context.Background(), store, lister, miss) // miss=1, no write
	reconcileOnce(context.Background(), store, lister, miss) // miss=2, write offline

	if store.statusUpdates[id] != "offline" {
		t.Fatalf("expected status offline after 2 misses, got %q", store.statusUpdates[id])
	}
	if store.streamErrors[id] == "" {
		t.Fatal("expected a stream error message to be recorded")
	}
}

// TestReconciler_APIErrorSkipsCycle: if FetchPathsReady returns an error the
// reconciler must not touch any camera status (resilience against API hiccup).
func TestReconciler_APIErrorSkipsCycle(t *testing.T) {
	id := uuid.New()
	store := newFakeStore(cam(id, "online"))
	lister := &fakePathsLister{err: fmt.Errorf("connection refused")}
	miss := make(map[uuid.UUID]int)

	reconcileOnce(context.Background(), store, lister, miss)

	if len(store.statusUpdates) != 0 {
		t.Fatalf("expected no DB writes on API error, got %v", store.statusUpdates)
	}
}

// TestReconciler_MissCounterResetsOnRecovery: after 1 miss the path recovers;
// the miss counter must reset so a subsequent miss does not immediately use the
// accumulated count from before recovery.
func TestReconciler_MissCounterResetsOnRecovery(t *testing.T) {
	id := uuid.New()
	store := newFakeStore(cam(id, "online"))
	absent := &fakePathsLister{ready: map[string]bool{}}
	present := &fakePathsLister{ready: map[string]bool{id.String(): true}}
	miss := make(map[uuid.UUID]int)

	reconcileOnce(context.Background(), store, absent, miss)  // miss=1
	reconcileOnce(context.Background(), store, present, miss) // recovery → miss=0
	reconcileOnce(context.Background(), store, absent, miss)  // miss=1 again

	// After recovery + one new miss we should still be at miss=1, not miss=2,
	// so no offline write should have happened on the third cycle.
	if store.statusUpdates[id] == "offline" {
		t.Fatal("expected no offline write: miss counter should have reset on recovery")
	}
	if miss[id] != 1 {
		t.Fatalf("expected miss counter = 1 after recovery cycle, got %d", miss[id])
	}
}
