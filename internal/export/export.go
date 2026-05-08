// Package export processes video-export jobs that were created via
// POST /api/exports and stored in the exports DB table.
//
// The worker used to wait on an in-memory Go channel, but nothing ever
// pushed to that channel — exports silently died on creation. This rewrite
// drives the worker from the DB instead:
//
//  1. On startup, RequeueStuckExports flips any exports left in
//     'processing' by a crashed previous process back to 'pending'.
//  2. The worker polls every few seconds for the next pending job via
//     ClaimNextExport, which atomically flips pending→processing using
//     FOR UPDATE SKIP LOCKED. This makes the worker safe to run in N
//     replicas simultaneously — a concern for the Phase 2 worker-split.
//  3. Each job runs an FFmpeg concat; on completion / failure the status
//     is updated back through UpdateExportStatus.
//
// Still one worker per process for now — parallelism can be added by
// running multiple goroutines through the same ClaimNextExport entry
// point; each call is atomic so the workers won't collide.
package export

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// pollInterval is how often the worker checks the DB for pending jobs.
// Short enough that exports feel responsive (~few seconds after Create)
// but long enough that the DB isn't hammered by an empty-queue loop.
const pollInterval = 3 * time.Second

// stuckJobTimeout is how long an export can stay 'processing' before
// RequeueStuckExports considers it abandoned. FFmpeg concats of long
// clips can legitimately run for minutes; 10 minutes keeps us clear of
// false positives.
const stuckJobTimeout = 10 * time.Minute

// Worker processes video export jobs from the DB queue.
type Worker struct {
	cfg *config.Config
	db  *database.DB
	wg  sync.WaitGroup
}

// NewWorker creates a new export worker.
func NewWorker(cfg *config.Config, db *database.DB) *Worker {
	return &Worker{cfg: cfg, db: db}
}

// Start spawns the poll loop. Call Wait() after cancelling the context
// to block until the current job (if any) finishes and the worker exits.
func (w *Worker) Start(ctx context.Context) {
	// Recover from a previous crash: any exports still in 'processing'
	// beyond stuckJobTimeout get flipped back to 'pending' so we pick
	// them up on the first poll.
	if requeued, err := w.db.RequeueStuckExports(ctx, stuckJobTimeout); err != nil {
		log.Printf("[EXPORT] Startup requeue check failed: %v", err)
	} else if requeued > 0 {
		log.Printf("[EXPORT] Requeued %d stuck export(s) from previous run", requeued)
	}

	log.Println("[EXPORT] Worker started (poll-based)")
	w.wg.Add(1)

	go w.runLoop(ctx)
}

// runLoop polls the DB for pending jobs until the context is cancelled.
// Separate from Start so the tick cadence can be tested independently.
func (w *Worker) runLoop(ctx context.Context) {
	defer w.wg.Done()

	// Fire an immediate poll before the ticker — otherwise a job enqueued
	// right as the server starts waits a full pollInterval before running.
	w.drainQueue(ctx)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[EXPORT] Worker stopping")
			return
		case <-ticker.C:
			w.drainQueue(ctx)
		}
	}
}

// drainQueue processes every pending job currently in the queue, then
// returns. Keeps the poll interval from becoming the cap on throughput
// when jobs are queued in a burst.
func (w *Worker) drainQueue(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := w.db.ClaimNextExport(ctx)
		if err != nil {
			log.Printf("[EXPORT] Claim failed: %v", err)
			return
		}
		if job == nil {
			return // queue empty
		}
		w.processExport(ctx, job)
	}
}

// Wait blocks until the worker's goroutine has fully exited. Call this
// after context cancellation so the shutdown path waits for any in-flight
// FFmpeg to finish before the process exits.
func (w *Worker) Wait() {
	w.wg.Wait()
}

// processExport handles a single claimed export job — the status is
// already 'processing' when we get here (set atomically by ClaimNextExport).
func (w *Worker) processExport(ctx context.Context, job *database.Export) {
	log.Printf("[EXPORT] Processing %s (camera %s, %s → %s)",
		job.ID, job.CameraID,
		job.StartTime.Format("15:04:05"), job.EndTime.Format("15:04:05"))

	segments, err := w.db.GetSegments(ctx, job.CameraID, job.StartTime, job.EndTime)
	if err != nil {
		w.markFailed(ctx, job.ID, fmt.Sprintf("get segments: %v", err))
		return
	}
	if len(segments) == 0 {
		w.markFailed(ctx, job.ID, "no recordings found for the specified time range")
		return
	}

	// Prepare FFmpeg concat inputs — temp list file enumerates the
	// segment paths; -c copy means no re-encoding (fast; preserves quality).
	outputFile := filepath.Join(w.cfg.ExportPath, fmt.Sprintf("export_%s.mp4", job.ID.String()[:8]))
	listFile := filepath.Join(w.cfg.ExportPath, fmt.Sprintf("concat_%s.txt", job.ID.String()[:8]))

	var fileList string
	for _, seg := range segments {
		fileList += fmt.Sprintf("file '%s'\n", seg.FilePath)
	}
	if err := os.WriteFile(listFile, []byte(fileList), 0644); err != nil {
		w.markFailed(ctx, job.ID, fmt.Sprintf("write concat list: %v", err))
		return
	}
	defer os.Remove(listFile)

	args := []string{
		"-f", "concat",
		"-safe", "0",
		"-i", listFile,
		"-c", "copy",
		"-movflags", "+faststart", // allow progressive playback / streaming
		"-y",
		outputFile,
	}
	cmd := exec.CommandContext(ctx, w.cfg.FFmpegPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		tail := string(output[:min(len(output), 500)])
		w.markFailed(ctx, job.ID, fmt.Sprintf("ffmpeg concat: %v\nOutput: %s", err, tail))
		return
	}

	info, err := os.Stat(outputFile)
	if err != nil {
		w.markFailed(ctx, job.ID, fmt.Sprintf("stat output: %v", err))
		return
	}

	if err := w.db.UpdateExportStatus(ctx, job.ID, "completed", outputFile, info.Size(), ""); err != nil {
		log.Printf("[EXPORT] Status update failed for %s: %v", job.ID, err)
	}
	log.Printf("[EXPORT] Completed: %s (%.2f MB)", outputFile, float64(info.Size())/1024/1024)
}

// markFailed is a small helper to centralise the status update + log for
// every error branch in processExport. Keeps the hot path readable.
func (w *Worker) markFailed(ctx context.Context, id uuid.UUID, reason string) {
	if err := w.db.UpdateExportStatus(ctx, id, "failed", "", 0, reason); err != nil {
		log.Printf("[EXPORT] Mark-failed DB update failed for %s: %v", id, err)
	}
	log.Printf("[EXPORT] Failed %s: %s", id, reason)
}

// GenerateExportID creates a unique short ID for export filenames. Kept
// for external callers that were importing it pre-rewrite — not actually
// used by the worker itself (it uses the UUID from the DB row).
func GenerateExportID() string {
	return uuid.New().String()[:8]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
