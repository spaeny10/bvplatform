package safety

// vlm_worker.go — P2-C-03 async VLM validation worker.
//
// The VLMWorker polls pending_review_queue for rows that need a Qwen
// second-opinion pass and updates their vlm_verdict in place. It is
// intentionally decoupled from the PPE worker (which writes rows) and
// from the human review flow (which reads rows via the API).
//
// Degradation contract: Qwen down → rows accumulate 'error' verdicts up
// to VLMWorkerMaxRetries, then stay at 'error' (visible to humans). No
// crash, no blocking the human queue. VLM_WORKER_ENABLED=false (default)
// keeps the worker dormant so Qwen absence has zero impact.

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/ai"
	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/dualwrite"
)

// VLMWorker is the async VLM validation polling worker.
type VLMWorker struct {
	cfg    *config.Config
	db     *database.DB
	client AIClient
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewVLMWorker creates a new VLMWorker. Call Start to begin polling.
func NewVLMWorker(cfg *config.Config, db *database.DB, client AIClient) *VLMWorker {
	return &VLMWorker{
		cfg:    cfg,
		db:     db,
		client: client,
		stopCh: make(chan struct{}),
	}
}

// Start launches the VLM worker poll loop in a background goroutine.
// It returns immediately; the loop runs until Stop is called or ctx is
// canceled. The loop is not started when VLMWorkerEnabled=false.
func (w *VLMWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.loop(ctx)
	}()
}

// Stop signals the worker to cease polling and waits for the current
// cycle to finish. Safe to call multiple times.
func (w *VLMWorker) Stop() {
	select {
	case <-w.stopCh:
		// already stopped
	default:
		close(w.stopCh)
	}
	w.wg.Wait()
}

// loop is the main poll/sleep cycle.
func (w *VLMWorker) loop(ctx context.Context) {
	batchSize := w.cfg.VLMWorkerBatchSize
	if batchSize <= 0 {
		batchSize = 5
	}
	pollInterval := time.Duration(w.cfg.VLMWorkerPollIntervalSec) * time.Second
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}
	maxRetries := w.cfg.VLMWorkerMaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	maxAgeHours := w.cfg.VLMWorkerMaxAgeHours
	if maxAgeHours <= 0 {
		maxAgeHours = 24
	}
	maxConcurrent := w.cfg.VLMWorkerMaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	log.Printf("[VLM] worker started (batch=%d poll=%s maxRetries=%d maxAge=%dh concurrent=%d)",
		batchSize, pollInterval, maxRetries, maxAgeHours, maxConcurrent)

	for {
		select {
		case <-w.stopCh:
			log.Println("[VLM] worker stopped")
			return
		case <-ctx.Done():
			log.Println("[VLM] worker: context done")
			return
		default:
		}

		w.runCycle(ctx, batchSize, maxRetries, maxAgeHours, maxConcurrent)

		select {
		case <-w.stopCh:
			log.Println("[VLM] worker stopped")
			return
		case <-ctx.Done():
			log.Println("[VLM] worker: context done")
			return
		case <-time.After(pollInterval):
		}
	}
}

// runCycle fetches a batch of pending VLM rows, validates each one, and
// also triggers the age-out sweep. Panics inside per-row processing are
// recovered so a single bad row cannot crash the worker.
func (w *VLMWorker) runCycle(ctx context.Context, batchSize, maxRetries, maxAgeHours, maxConcurrent int) {
	// Age-out stale pending rows first so they don't keep re-appearing.
	if err := w.db.ExpireStalePendingVLM(ctx, maxAgeHours); err != nil {
		log.Printf("[VLM] ExpireStalePendingVLM error: %v", err)
	}

	rows, err := w.db.ListPendingVLM(ctx, batchSize, maxRetries)
	if err != nil {
		log.Printf("[VLM] ListPendingVLM error: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	log.Printf("[VLM] processing %d candidate(s)", len(rows))

	if maxConcurrent <= 1 {
		// Serial path — no goroutine overhead.
		for _, row := range rows {
			w.processRow(ctx, row)
		}
		return
	}

	// Concurrent path — bounded parallelism via a semaphore channel.
	sem := make(chan struct{}, maxConcurrent)
	var rowWG sync.WaitGroup
	for _, row := range rows {
		row := row // capture
		rowWG.Add(1)
		sem <- struct{}{}
		go func() {
			defer rowWG.Done()
			defer func() { <-sem }()
			w.processRow(ctx, row)
		}()
	}
	rowWG.Wait()
}

// processRow validates one pending_review_queue row. It reads the frame
// from disk, calls Qwen, and writes back the verdict. Panics are recovered
// so a single row cannot crash the worker cycle.
func (w *VLMWorker) processRow(ctx context.Context, row database.VLMQueueRow) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[VLM] panic in processRow %s: %v", row.ID, r)
		}
	}()

	framesDir := w.cfg.PPEFramesDir
	if framesDir == "" {
		framesDir = "/tank/data/ironsight/ppe-frames"
	}

	// Validate the frame path to prevent directory traversal.
	rel := filepath.Clean(row.FramePath)
	if strings.HasPrefix(rel, "..") {
		log.Printf("[VLM] row %s: invalid frame path %q", row.ID, row.FramePath)
		_ = w.db.UpdateVLMVerdict(ctx, row.ID,
			string(VerdictError), "invalid frame path", "", row.Attempts+1)
		return
	}

	absPath := filepath.Join(framesDir, rel)
	frameBytes, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[VLM] row %s: frame file not found: %s", row.ID, absPath)
		} else {
			log.Printf("[VLM] row %s: read frame error: %v", row.ID, err)
		}
		_ = w.db.UpdateVLMVerdict(ctx, row.ID,
			string(VerdictError), "frame file not found", "", row.Attempts+1)
		return
	}

	// Pick the first bounding box for the VLM prompt; fall back to zero bbox.
	// C-05: crops to BoundingBoxes[0]. If multi-bbox rows are introduced,
	// update to use the specific violation's bbox.
	var bboxNorm ai.BBox
	if len(row.BoundingBoxes) > 0 {
		bboxNorm = row.BoundingBoxes[0].BBoxNorm
	}

	// C-05: Crop the frame to the detection's ROI before sending to Qwen.
	// A focused thumbnail reduces visual noise and speeds up inference.
	// Falls back to the full frame on crop failure — VLM validation still
	// proceeds; only Qwen transport/inference failure sets VerdictError.
	paddingFactor := w.cfg.VLMCropPaddingFactor
	if paddingFactor <= 0 {
		paddingFactor = 0.25
	}
	if croppedBytes, cropErr := CropToROI(frameBytes, bboxNorm, paddingFactor); cropErr != nil {
		log.Printf("[VLM] row %s: crop failed (%v), falling back to full frame", row.ID, cropErr)
		// frameBytes is already set to the full frame; use it unchanged.
	} else {
		frameBytes = croppedBytes
	}

	result := ValidatePPECandidate(ctx, w.client,
		frameBytes,
		row.DetectionClass,
		row.MissingLabel,
		row.Confidence,
		bboxNorm,
	)

	if err := w.db.UpdateVLMVerdict(ctx, row.ID,
		string(result.Verdict),
		result.Reasoning,
		result.Model,
		row.Attempts+1,
	); err != nil {
		log.Printf("[VLM] row %s: UpdateVLMVerdict error: %v", row.ID, err)
		return
	}

	log.Printf("[VLM] row %s: verdict=%s attempts=%d model=%s",
		row.ID, result.Verdict, row.Attempts+1, result.Model)

	// P4-SCHEMA-02: dual-write to detections after the VLM verdict is
	// persisted.  We write for all terminal verdicts (confirmed, dismissed,
	// uncertain) so the detections table reflects the full VLM outcome set.
	// Failure is non-fatal: log + count.
	go w.dualWriteVLMVerdict(row, result)
}

// dualWriteVLMVerdict writes one detection row to the P4 detections table.
// Called in a goroutine after UpdateVLMVerdict; failures are non-fatal.
// We use a background context with a 10 s deadline so a slow DB write
// does not block the next VLM cycle.
func (w *VLMWorker) dualWriteVLMVerdict(
	row database.VLMQueueRow,
	result VLMValidationResult,
) {
	// Skip rows without the required FK fields (should not happen now that
	// ListPendingVLM selects them, but guard defensively).
	if row.OrganizationID == "" || row.CameraID == (uuid.UUID{}) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Model: Qwen model name from the result; version tag = model string.
	modelName := "qwen-vlm"
	versionTag := result.Model
	if versionTag == "" {
		versionTag = "unknown"
	}

	mvID, err := dualwrite.LookupOrCreateModelVersion(
		ctx, w.db,
		row.OrganizationID, modelName, versionTag, "", "vlm_validation",
	)
	if err != nil {
		log.Printf("[DUALWRITE:vlm] LookupOrCreateModelVersion: %v", err)
		dualwrite.DualWriteFailuresTotal.WithLabelValues("vlm").Inc()
		return
	}

	run := dualwrite.NewRunHandle(w.db, row.OrganizationID, mvID)

	// Confidence: use the PRQ Confidence field (the YOLO score), not a VLM
	// floating-point score (Qwen produces a verdict string, not a score).
	var bboxJSON []byte
	if len(row.BoundingBoxes) > 0 {
		bb := row.BoundingBoxes[0]
		bboxJSON = dualwrite.BBoxFromX1Y1X2Y2(
			bb.BBoxNorm.X1, bb.BBoxNorm.Y1, bb.BBoxNorm.X2, bb.BBoxNorm.Y2,
		)
	}

	detailsMap := map[string]string{
		"vlm_verdict":   string(result.Verdict),
		"vlm_reasoning": result.Reasoning,
	}
	detailsJSON, _ := json.Marshal(detailsMap)

	dualwrite.Write(ctx, w.db, "vlm", row.OrganizationID, row.CameraID, run, dualwrite.MappedDetection{
		DetectedAt:      row.CreatedAt, // use the original detection time
		SiteID:          row.SiteID,
		DetectionClass:  dualwrite.NormalisePPEClass(row.DetectionClass),
		DetectionDomain: "vlm_validation",
		Confidence:      float32(row.Confidence),
		BoundingBox:     bboxJSON,
		ZoneID:          nil,
		VCARuleID:       nil,
		Details:         detailsJSON,
	})
}
