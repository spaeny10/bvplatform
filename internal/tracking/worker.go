// Package tracking implements the person-tracking aggregation worker (P2-C-02).
//
// The worker subscribes to a CameraFrameResult channel published by the PPE
// worker after each camera poll. For each frame it:
//   1. Counts person detections from YOLOResult.Detections (the security-model
//      output — NOT PPEViolations, which is the PPE-model output).
//   2. Persists one row to person_track_frames with the per-frame person count.
//
// R1 mitigation: personClass constant + countPersons helper make the
// Detections-only filter explicit and testable. The test
// TestTrackingWorker_IgnoresNonPerson guards against regression.
//
// The worker uses a non-blocking channel receive so a slow DB write cannot
// cause frames to queue up unboundedly. The PPE worker uses a non-blocking
// send so channel backpressure cannot stall PPE violation detection.
package tracking

import (
	"context"
	"log"
	"time"

	"ironsight/internal/ai"
	"ironsight/internal/database"
	"ironsight/internal/ppe"
)

// personClass is the COCO class name for a detected person in the security
// model output (YOLOResult.Detections). Named constant so the filter is
// explicit and grep-able.
const personClass = "person"

// countPersons counts detections whose class is exactly "person".
// R1: reads from detections only — never from ppe_detections or ppe_violations.
func countPersons(detections []ai.Detection) int {
	n := 0
	for _, d := range detections {
		if d.Class == personClass {
			n++
		}
	}
	return n
}

// TrackingDB is the subset of database.DB the tracking worker needs.
// Using an interface keeps the package independently testable.
type TrackingDB interface {
	InsertTrackFrame(ctx context.Context, ins database.PersonTrackFrameInsert) error
}

// Worker receives CameraFrameResult values from the PPE worker and
// persists per-frame person counts to person_track_frames.
type Worker struct {
	db  TrackingDB
	ch  <-chan ppe.CameraFrameResult
}

// New creates a tracking worker. ch is the channel the PPE worker
// publishes to; db is the database handle.
func New(db TrackingDB, ch <-chan ppe.CameraFrameResult) *Worker {
	return &Worker{db: db, ch: ch}
}

// Start launches the receive loop in a goroutine.
// Returns when ctx is cancelled.
func (w *Worker) Start(ctx context.Context) {
	log.Println("[TRACKING] worker started")
	go w.run(ctx)
}

func (w *Worker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("[TRACKING] worker context cancelled, stopping")
			return
		case result, ok := <-w.ch:
			if !ok {
				log.Println("[TRACKING] channel closed, stopping")
				return
			}
			w.handleFrame(ctx, result)
		}
	}
}

// handleFrame processes one CameraFrameResult. A nil YOLO result means the
// sidecar call failed; we skip the DB write rather than persisting a
// misleading zero count (the raw-frame gap is acceptable — tracking can
// have holes when inference is unavailable).
func (w *Worker) handleFrame(ctx context.Context, result ppe.CameraFrameResult) {
	if result.YOLO == nil {
		// YOLO call failed for this frame — skip silently.
		// The aggregator's backfill sweep handles the resulting gap.
		return
	}

	// R1: count from Detections (security model), not PPEViolations.
	count := countPersons(result.YOLO.Detections)

	cam := result.Camera
	var siteIDPtr *string
	if cam.SiteID != "" {
		s := cam.SiteID
		siteIDPtr = &s
	}

	ins := database.PersonTrackFrameInsert{
		Time:           result.FetchedAt,
		CameraID:       cam.CameraID,
		SiteID:         siteIDPtr,
		OrganizationID: cam.OrganizationID,
		PersonCount:    count,
		FrameSource:    "ppe_worker",
	}

	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := w.db.InsertTrackFrame(writeCtx, ins); err != nil {
		log.Printf("[TRACKING] InsertTrackFrame camera %s: %v", cam.CameraID, err)
	}
}
