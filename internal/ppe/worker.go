// Package ppe implements the background PPE violation detection worker.
//
// The worker polls each camera assigned to a PPE-enabled site on a
// configurable cadence (default 30s), fetches a JPEG snapshot, calls the
// YOLO sidecar, and — for violations above the confidence threshold —
// persists a row to pending_review_queue, saves the frame to disk, and
// broadcasts a ppe_detected WebSocket event through the Hub.
//
// Frame source: Milesight vendor CGI snapshot (Camera.Snapshot()) first;
// falls back to ONVIF FetchSnapshot on failure.
//
// This worker runs inside the ironsight-worker container alongside the
// existing retention, VLM indexer, and export workers. It participates
// in the same leader-election lock so it runs on exactly one replica.
package ppe

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/ai"
	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
	appmetrics "ironsight/internal/metrics"
	"ironsight/internal/milesight"
	"ironsight/internal/onvif"
)

// Broadcaster is the subset of api.Hub that the worker needs. Using an
// interface keeps the ppe package independent of the api package (which
// would create an import cycle — api imports database, database cannot
// import api).
type Broadcaster interface {
	Broadcast(msg []byte)
}

// Worker polls PPE-enabled cameras and feeds violations into the queue.
type Worker struct {
	cfg      *config.Config
	db       *database.DB
	aiClient *ai.Client
	hub      Broadcaster
	stopCh   chan struct{}
}

// New creates a PPE worker. hub may be nil if WS broadcast is not available
// (worker-only binary without a connected hub). In that case violations are
// still persisted to the DB.
func New(cfg *config.Config, db *database.DB, aiClient *ai.Client, hub Broadcaster) *Worker {
	return &Worker{
		cfg:      cfg,
		db:       db,
		aiClient: aiClient,
		hub:      hub,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the polling loop in a goroutine. Call Stop to terminate.
func (w *Worker) Start(ctx context.Context) {
	log.Println("[PPE] worker started")
	go w.run(ctx)
}

// Stop signals the worker to stop after the current poll cycle.
func (w *Worker) Stop() {
	close(w.stopCh)
}

func (w *Worker) run(ctx context.Context) {
	interval := time.Duration(w.cfg.PPEPollIntervalSec) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	tick := time.NewTicker(interval)
	defer tick.Stop()

	// Run one pass immediately at start so operators see results without
	// waiting for the first tick.
	w.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("[PPE] worker context cancelled, stopping")
			return
		case <-w.stopCh:
			log.Println("[PPE] worker stop signal received")
			return
		case <-tick.C:
			w.poll(ctx)
		}
	}
}

// poll runs one full sweep of all PPE-eligible cameras.
func (w *Worker) poll(ctx context.Context) {
	cameras, err := w.db.ListCamerasForPPE(ctx)
	if err != nil {
		log.Printf("[PPE] ListCamerasForPPE: %v", err)
		return
	}
	if len(cameras) == 0 {
		return
	}

	log.Printf("[PPE] polling %d camera(s)", len(cameras))
	for _, cam := range cameras {
		if err := ctx.Err(); err != nil {
			return
		}
		w.processCamera(ctx, cam)
	}
}

// consecutiveFailures tracks per-camera snapshot failure runs for the
// SetCustomAlert threshold (3+ failures triggers an alert).
var consecutiveFailures = map[uuid.UUID]int{}

func (w *Worker) processCamera(ctx context.Context, cam database.PPECamera) {
	// Fetch snapshot: Milesight CGI first, ONVIF fallback.
	frameBytes, source, err := w.fetchSnapshot(ctx, cam)
	if err != nil {
		consecutiveFailures[cam.CameraID]++
		n := consecutiveFailures[cam.CameraID]
		log.Printf("[PPE] frame fetch failed for camera %s (%s): %v (consecutive=%d)",
			cam.CameraID, cam.CameraName, err, n)
		if n >= 3 {
			appmetrics.SetCustomAlert("ppe_frame_fetch_failure", "warning",
				fmt.Sprintf("camera %s: %v", cam.CameraID, err))
		}
		return
	}
	consecutiveFailures[cam.CameraID] = 0

	// YOLO inference.
	if !w.cfg.AIEnabled {
		return
	}
	pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	yolo, err := w.aiClient.DetectYOLO(pollCtx, frameBytes)
	if err != nil {
		log.Printf("[PPE] YOLO call failed for camera %s: %v", cam.CameraID, err)
		appmetrics.SetCustomAlert("ppe_yolo_call_failure", "warning",
			fmt.Sprintf("camera %s: %v", cam.CameraID, err))
		return
	}

	if len(yolo.PPEViolations) == 0 {
		return
	}

	threshold := w.cfg.PPEConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.50
	}

	for _, v := range yolo.PPEViolations {
		if v.Confidence < threshold {
			log.Printf("[PPE] violation below threshold (%.2f < %.2f) for camera %s — skipping",
				v.Confidence, threshold, cam.CameraID)
			continue
		}

		if err := w.persistViolation(ctx, cam, v, yolo, frameBytes, source); err != nil {
			log.Printf("[PPE] persist violation for camera %s: %v", cam.CameraID, err)
		}
	}
}

// fetchSnapshot tries Milesight CGI first, falls back to ONVIF.
func (w *Worker) fetchSnapshot(ctx context.Context, cam database.PPECamera) ([]byte, string, error) {
	// Attempt Milesight CGI snapshot only if the camera is a known Milesight
	// or if we have no Manufacturer info (optimistic try).
	isMilesight := cam.Manufacturer == "" ||
		cam.Manufacturer == "Milesight" ||
		cam.Manufacturer == "milesight"

	if isMilesight {
		// milesight.Camera.Snapshot() is a blocking HTTP GET with its own
		// internal timeout; it does not accept a context. We call it
		// directly and rely on its internal timeout (~10s).
		msCam := milesight.New(cam.OnvifAddress, "", "")
		frame, err := msCam.Snapshot()
		if err == nil && len(frame) > 0 {
			return frame, "milesight_cgi", nil
		}
		log.Printf("[PPE] Milesight snapshot failed for %s, trying ONVIF: %v", cam.CameraName, err)
	}

	// ONVIF fallback.
	if cam.ProfileToken == "" {
		return nil, "", fmt.Errorf("no profile token available for ONVIF fallback on camera %s", cam.CameraID)
	}
	onvifClient := onvif.NewClient(cam.OnvifAddress, "", "")
	snapshotURI, err := onvifClient.GetSnapshotURI(ctx, cam.ProfileToken)
	if err != nil {
		return nil, "", fmt.Errorf("GetSnapshotURI: %w", err)
	}
	snapCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	frame, err := onvifClient.FetchSnapshot(snapCtx, snapshotURI)
	if err != nil {
		return nil, "", fmt.Errorf("FetchSnapshot (ONVIF): %w", err)
	}
	return frame, "onvif", nil
}

// persistViolation saves the frame, inserts the DB row, and broadcasts the WS event.
func (w *Worker) persistViolation(
	ctx context.Context,
	cam database.PPECamera,
	v ai.Detection,
	yolo *ai.YOLOResult,
	frameBytes []byte,
	frameSource string,
) error {
	now := time.Now().UTC()
	frameFilename := fmt.Sprintf("%d.jpg", now.UnixMilli())
	dateDir := now.Format("2006-01-02")
	relPath := filepath.Join(cam.OrganizationID, dateDir, frameFilename)

	// Write frame to disk.
	framesDir := w.cfg.PPEFramesDir
	if framesDir == "" {
		framesDir = "/tank/data/ironsight/ppe-frames"
	}
	absDir := filepath.Join(framesDir, cam.OrganizationID, dateDir)
	if err := os.MkdirAll(absDir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", absDir, err)
	}
	absPath := filepath.Join(absDir, frameFilename)
	if err := os.WriteFile(absPath, frameBytes, 0o640); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	// Mint a media token for the WS broadcast payload (5-min TTL).
	// The GET list handler re-mints fresh tokens at response time;
	// this one is only for the immediate WS delivery.
	frameToken, err := auth.SignMediaToken(
		"worker", cam.CameraID.String(),
		auth.MediaKindPPEFrame, frameFilename,
		w.cfg.JWTSecret, 5*time.Minute,
	)
	if err != nil {
		// Non-fatal — we can persist without a WS token.
		log.Printf("[PPE] SignMediaToken: %v", err)
		frameToken = ""
	}
	tokenExpires := now.Add(5 * time.Minute)

	// Build bounding-box JSON from the single violation.
	type bbEntry struct {
		Class      string    `json:"class"`
		Confidence float64   `json:"confidence"`
		BBox       ai.BBox   `json:"bbox"`
		BBoxNorm   ai.BBox   `json:"bbox_normalized"`
	}
	bb := bbEntry{
		Class:      v.Class,
		Confidence: v.Confidence,
		BBox:       v.BBox,
		BBoxNorm:   v.BBoxNorm,
	}
	bbJSON, _ := json.Marshal([]bbEntry{bb})

	// Determine human-readable label — YOLO Detection has no Missing field in
	// the current struct; derive it from the class name.
	missingLabel := classToLabel(v.Class)

	siteIDPtr := &cam.SiteID

	ins := database.PPEQueueInsert{
		OrganizationID:    cam.OrganizationID,
		CameraID:          cam.CameraID,
		SiteID:            siteIDPtr,
		FramePath:         relPath,
		FrameToken:        frameToken,
		FrameTokenExpires: tokenExpires,
		DetectionClass:    v.Class,
		MissingLabel:      missingLabel,
		Confidence:        v.Confidence,
		BoundingBoxes:     json.RawMessage(bbJSON),
		YOLOModel:         yolo.PPEModel,
	}

	entryID, err := w.db.InsertPPEQueueEntry(ctx, ins)
	if err != nil {
		return fmt.Errorf("InsertPPEQueueEntry: %w", err)
	}

	log.Printf("[PPE] detected violation %s (%.0f%%) on camera %s org %s — entry %s (source=%s)",
		v.Class, v.Confidence*100, cam.CameraID, cam.OrganizationID, entryID, frameSource)

	// Broadcast WS event. Top-level camera_id is required by the Hub's
	// tenant-scoped fanout (P1-A-04 writeToClients + routeKeyFromMessage).
	if w.hub != nil {
		envelope := map[string]interface{}{
			"type":            "ppe_detected",
			"camera_id":       cam.CameraID.String(),
			"organization_id": cam.OrganizationID,
			"data": map[string]interface{}{
				"queue_entry_id":  entryID.String(),
				"detection_class": v.Class,
				"missing_label":   missingLabel,
				"confidence":      v.Confidence,
				"frame_token":     frameToken,
				"created_at":      now.Format(time.RFC3339),
			},
		}
		if msg, err := json.Marshal(envelope); err == nil {
			w.hub.Broadcast(msg)
		}
	}

	return nil
}

// classToLabel converts a raw YOLO PPE violation class name to a human-readable label.
func classToLabel(class string) string {
	switch class {
	case "nohat", "no-hat", "no_hat", "no-hardhat", "no_hardhat", "no-helmet", "no_helmet":
		return "Hard Hat"
	case "novest", "no-vest", "no_vest", "no-safety-vest", "no_safety_vest":
		return "Hi-Vis Vest"
	case "no-mask", "no_mask":
		return "Face Mask"
	case "no-glove", "no_glove", "no-gloves", "no_gloves":
		return "Gloves"
	case "no-goggles", "no_goggles":
		return "Safety Goggles"
	case "no-shoes", "no_shoes":
		return "Safety Shoes"
	default:
		return "PPE Item"
	}
}
