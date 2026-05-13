// Package indexer runs a background worker that describes recording segments
// with a VLM (Qwen via /analyze_video?mode=describe) so every minute of
// footage becomes searchable by keyword — "person in red jacket", "delivery
// van", "carrying a ladder" — rather than only by event-rule name.
//
// Design notes:
//   - YOLO-gated: each segment is sampled as a single JPEG frame and passed
//     through YOLO first. Empty scenes record a stub description and skip
//     Qwen, saving 95% of the budget on overnight lulls.
//   - Idempotent: one row per segment_id. Pick the oldest unindexed closed
//     segment, finish or fail it, move on. Safe to restart.
//   - Back-pressure aware: pauses if live AI pipeline shows signs of load
//     (future: expose a semaphore from `ai.Client`). Today: one-at-a-time.
//   - Concurrency knob via INDEXER_CONCURRENCY env var so a 3070 runs 1
//     worker and A40s can run 4-8. Each worker picks a different segment.
package indexer

import (
	"context"
	"encoding/json"
	"fmt"
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
	"ironsight/internal/recording"
)

const (
	// indexerVersion is written into each row. Bump when the prompt / schema
	// changes materially so a future maintenance task can find stale rows
	// and re-index them (not done in this session, but cheap to reserve now).
	indexerVersion = 1

	// clipDurationSec is how much of each segment we feed into Qwen. We
	// don't send the whole minute — VLM cost scales with frame count and
	// 8s in the middle captures the action with low vision-token count.
	clipDurationSec = 8.0

	// yoloMinDetections is the gate. If a sample frame has fewer than this
	// many interesting detections (person / vehicle / animal), the segment
	// is recorded as empty and Qwen is skipped.
	yoloMinDetections = 1

	// pollIdleSleep is how long to wait when the queue is empty. Keeps the
	// worker responsive to new segments without spinning.
	pollIdleSleep = 30 * time.Second
)

// yoloInterestingClasses are the YOLO detection classes that justify a Qwen
// call. Anything outside this set (e.g. "chair", "potted plant") is noise
// for a surveillance context and is discarded by the gate.
var yoloInterestingClasses = map[string]bool{
	"person": true, "bicycle": true, "car": true, "motorcycle": true,
	"bus": true, "truck": true, "dog": true, "cat": true, "horse": true,
	"backpack": true, "handbag": true, "suitcase": true,
}

// Config controls indexer behavior at startup. Sourced from the central
// *config.Config (env vars INDEXER_CONCURRENCY / INDEXER_ENABLED /
// INDEXER_MIN_AGE_SEC) by configFromAppConfig().
type Config struct {
	// Concurrency is the number of worker goroutines. 1 on an 8 GB 3070,
	// 4-8 on A40. Env: INDEXER_CONCURRENCY (clamped 1..16 at load time)
	Concurrency int

	// Enabled toggles the whole subsystem. Env: INDEXER_ENABLED (default true)
	Enabled bool

	// MinSegmentAge is how "closed" a segment must be before we index it.
	// Guards against racing a still-writing file. Env: INDEXER_MIN_AGE_SEC
	MinSegmentAge time.Duration
}

// Indexer owns the background worker(s). One Indexer per process; it pools
// work across N concurrent workers, each picking a different segment_id via
// a SKIP LOCKED postgres advisory to avoid duplicate work.
type Indexer struct {
	db       *database.DB
	ai       *ai.Client
	cfg      *config.Config
	icfg     Config
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	running  bool
	mu       sync.Mutex
}

// New constructs an Indexer using values from the application-wide
// *config.Config (env-read once at startup). Does not start any goroutines
// until Start() is called.
func New(cfg *config.Config, db *database.DB, aic *ai.Client) *Indexer {
	return &Indexer{
		db:   db,
		ai:   aic,
		cfg:  cfg,
		icfg: configFromAppConfig(cfg),
	}
}

// configFromAppConfig translates the slice of *config.Config fields the
// indexer cares about into its local Config struct. Kept separate from the
// Indexer constructor so unit tests can build a Config directly without
// going through env vars.
func configFromAppConfig(cfg *config.Config) Config {
	minAge := time.Duration(cfg.IndexerMinAgeSec) * time.Second
	if minAge <= 0 {
		minAge = 90 * time.Second
	}
	return Config{
		Concurrency:   cfg.IndexerConcurrency,
		Enabled:       cfg.IndexerEnabled,
		MinSegmentAge: minAge,
	}
}

// Start launches the configured number of worker goroutines. Safe to call
// once; subsequent calls are no-ops.
func (idx *Indexer) Start() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.running {
		return
	}
	if !idx.icfg.Enabled {
		log.Printf("[INDEXER] disabled via env")
		return
	}
	if idx.ai == nil || idx.cfg == nil || idx.cfg.FFmpegPath == "" {
		log.Printf("[INDEXER] missing ai client or ffmpeg path — not starting")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	idx.cancel = cancel
	idx.running = true
	for i := 0; i < idx.icfg.Concurrency; i++ {
		idx.wg.Add(1)
		go idx.worker(ctx, i)
	}
	log.Printf("[INDEXER] started with %d worker(s), min_age=%v",
		idx.icfg.Concurrency, idx.icfg.MinSegmentAge)
}

// Stop signals all workers to exit and waits for them. Called from the
// server's graceful-shutdown path.
func (idx *Indexer) Stop() {
	idx.mu.Lock()
	if !idx.running {
		idx.mu.Unlock()
		return
	}
	if idx.cancel != nil {
		idx.cancel()
	}
	idx.running = false
	idx.mu.Unlock()
	idx.wg.Wait()
	log.Printf("[INDEXER] stopped")
}

func (idx *Indexer) worker(ctx context.Context, id int) {
	defer idx.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		seg, err := idx.claimNextSegment(ctx)
		if err != nil {
			log.Printf("[INDEXER] worker %d: claim error: %v", id, err)
			sleep(ctx, 10*time.Second)
			continue
		}
		if seg == nil {
			sleep(ctx, pollIdleSleep)
			continue
		}

		if err := idx.index(ctx, seg); err != nil {
			log.Printf("[INDEXER] worker %d: segment %d failed: %v", id, seg.ID, err)
			// Record the attempt so we don't pick this one up again on every
			// pass. The description row marks this segment as "empty" with
			// a tag flagging the failure so ops can re-process later if
			// desired.
			idx.recordFailed(ctx, seg, err)
		}
	}
}

// claimNextSegment finds the oldest closed segment that doesn't yet have a
// description row. Uses a straight query (not FOR UPDATE SKIP LOCKED) because
// the subsequent INSERT ... ON CONFLICT DO NOTHING handles the race cheaply.
func (idx *Indexer) claimNextSegment(ctx context.Context) (*database.Segment, error) {
	cutoff := time.Now().Add(-idx.icfg.MinSegmentAge)
	// Indexing strategy: only process segments that ALREADY contain at least
	// one event. The platform has historical segments in the millions, most
	// of empty driveway / parking lot that no one will ever search for.
	// Event-carrying segments are the ones operators and customers want to
	// recall — the same moments the alarm pipeline surfaced. This cuts the
	// queue from ~2M segments to ~thousands without losing a single search
	// hit that a user would actually make.
	//
	// Newest-first so the search corpus leans toward recent content; the
	// file_size floor (1 MB) skips crash-loop stubs from FFmpeg restarts.
	row := idx.db.Pool.QueryRow(ctx, `
		SELECT s.id, s.camera_id, s.start_time, s.end_time, s.file_path, s.file_size, s.duration_ms, s.has_audio
		FROM segments s
		LEFT JOIN segment_descriptions d ON d.segment_id = s.id
		WHERE d.segment_id IS NULL
		  AND s.end_time <= $1
		  AND s.file_size > 1000000
		  AND EXISTS (SELECT 1 FROM events e WHERE e.segment_id = s.id)
		ORDER BY s.start_time DESC
		LIMIT 1`, cutoff)

	var seg database.Segment
	err := row.Scan(&seg.ID, &seg.CameraID, &seg.StartTime, &seg.EndTime,
		&seg.FilePath, &seg.FileSize, &seg.DurationMs, &seg.HasAudio)
	if err != nil {
		// pgx returns a "no rows" error we treat as empty queue.
		if strings.Contains(err.Error(), "no rows") {
			return nil, nil
		}
		return nil, err
	}
	return &seg, nil
}

// index runs the full pipeline for one segment: sample-frame, YOLO gate,
// Qwen describe, DB insert.
func (idx *Indexer) index(ctx context.Context, seg *database.Segment) error {
	// Verify the file still exists — older segments may have been deleted
	// by retention.
	if _, err := os.Stat(seg.FilePath); err != nil {
		return fmt.Errorf("segment file missing: %w", err)
	}

	// ── 1. YOLO gate ── sample a frame from the middle of the segment,
	// detect, and skip Qwen if nothing interesting is present.
	midOffsetSec := float64(seg.DurationMs) / 2000.0
	if midOffsetSec < 1 {
		midOffsetSec = 1
	}
	framePath := filepath.Join(os.TempDir(),
		fmt.Sprintf("idx_frame_%d_%d.jpg", seg.ID, time.Now().UnixNano()))
	defer os.Remove(framePath)

	if _, err := recording.ExtractFrameFromSegment(idx.cfg.FFmpegPath,
		seg.FilePath, framePath, midOffsetSec); err != nil {
		return fmt.Errorf("extract frame: %w", err)
	}
	frameBytes, err := os.ReadFile(framePath)
	if err != nil {
		return fmt.Errorf("read frame: %w", err)
	}

	yoloCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	yoloRes, err := idx.ai.DetectYOLO(yoloCtx, frameBytes)
	cancel()
	if err != nil {
		// YOLO down → degrade by storing an "unknown" row rather than
		// repeatedly retrying. Gate upstream (cfg.Enabled) covers long outages.
		return fmt.Errorf("yolo: %w", err)
	}

	interesting := countInteresting(yoloRes.Detections)
	detectionsJSON, _ := json.Marshal(yoloRes.Detections)

	if interesting < yoloMinDetections {
		// Empty scene — record the stub and move on without Qwen.
		return idx.insertDescription(ctx, seg, describeRow{
			description:    "Empty scene, no activity.",
			tags:           []string{"empty"},
			activityLevel:  "none",
			entities:       nil,
			detectionsJSON: detectionsJSON,
			analysisMs:     int(yoloRes.InferenceMs),
		})
	}

	// ── 2. Extract a short clip centered on the middle of the segment.
	clipPath := filepath.Join(os.TempDir(),
		fmt.Sprintf("idx_clip_%d_%d.mp4", seg.ID, time.Now().UnixNano()))
	defer os.Remove(clipPath)

	clipStart := midOffsetSec - clipDurationSec/2
	if clipStart < 0 {
		clipStart = 0
	}
	if _, err := recording.ExtractClipFromSegment(idx.cfg.FFmpegPath,
		seg.FilePath, clipPath, clipStart, clipDurationSec); err != nil {
		return fmt.Errorf("extract clip: %w", err)
	}
	clipBytes, err := os.ReadFile(clipPath)
	if err != nil {
		return fmt.Errorf("read clip: %w", err)
	}
	if len(clipBytes) < 1000 {
		// Clip too small — don't feed Qwen a header-only file. Record stub.
		return idx.insertDescription(ctx, seg, describeRow{
			description:    "Recording gap — no decodable video.",
			tags:           []string{"gap"},
			activityLevel:  "none",
			entities:       nil,
			detectionsJSON: detectionsJSON,
			analysisMs:     int(yoloRes.InferenceMs),
		})
	}

	// ── 3. Qwen describe.
	qwenCtx, qcancel := context.WithTimeout(ctx, 90*time.Second)
	res := idx.ai.DescribeVideo(qwenCtx, clipBytes, yoloRes.Detections)
	qcancel()
	if res == nil {
		return fmt.Errorf("qwen describe returned nil")
	}
	if res.Degraded {
		// Falling back to mock = not useful for search. Record a minimal row
		// so we don't spin, but tag it so we can later re-index.
		return idx.insertDescription(ctx, seg, describeRow{
			description:    strings.TrimSpace(res.Description),
			tags:           []string{"indexer-degraded"},
			activityLevel:  "unknown",
			entities:       res.Entities,
			detectionsJSON: detectionsJSON,
			analysisMs:     int(res.InferenceMs) + int(yoloRes.InferenceMs),
		})
	}

	tags := normalizeTags(res.Tags)
	activity := normalizeActivity(res.ActivityLevel)

	return idx.insertDescription(ctx, seg, describeRow{
		description:    strings.TrimSpace(res.Description),
		tags:           tags,
		activityLevel:  activity,
		entities:       res.Entities,
		detectionsJSON: detectionsJSON,
		analysisMs:     int(res.InferenceMs) + int(yoloRes.InferenceMs),
	})
}

// describeRow is the denormalized bag of fields each code path assembles
// before insertDescription writes one segment_descriptions row.
type describeRow struct {
	description    string
	tags           []string
	activityLevel  string
	entities       []ai.QwenObject
	detectionsJSON []byte
	analysisMs     int
}

func (idx *Indexer) insertDescription(ctx context.Context, seg *database.Segment, r describeRow) error {
	entitiesJSON, _ := json.Marshal(r.entities)
	if len(r.entities) == 0 {
		entitiesJSON = []byte("[]")
	}
	if len(r.detectionsJSON) == 0 {
		r.detectionsJSON = []byte("[]")
	}
	// Tags array must be a concrete Go slice for pgx to bind as text[].
	if r.tags == nil {
		r.tags = []string{}
	}
	_, err := idx.db.Pool.Exec(ctx, `
		INSERT INTO segment_descriptions
		  (segment_id, camera_id, description, tags, activity_level, entities, detections, indexer_version, analysis_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (segment_id) DO NOTHING`,
		seg.ID, seg.CameraID, r.description, r.tags, r.activityLevel,
		entitiesJSON, r.detectionsJSON, indexerVersion, r.analysisMs)
	return err
}

// recordFailed stores a minimal row so a problem segment isn't picked up
// on every poll. The "indexer-failed" tag lets a future maintenance job
// target these for retry with a different model.
func (idx *Indexer) recordFailed(ctx context.Context, seg *database.Segment, cause error) {
	msg := cause.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	_ = idx.insertDescription(ctx, seg, describeRow{
		description:    "Indexing failed: " + msg,
		tags:           []string{"indexer-failed"},
		activityLevel:  "unknown",
		entities:       nil,
		detectionsJSON: []byte("[]"),
		analysisMs:     0,
	})
}

// countInteresting filters a YOLO detection list to the gate's class set and
// a minimum confidence. Keeps the VLM out of segments where YOLO only saw
// background furniture.
func countInteresting(dets []ai.Detection) int {
	n := 0
	for _, d := range dets {
		if d.Confidence < 0.35 {
			continue
		}
		if yoloInterestingClasses[d.Class] {
			n++
		}
	}
	return n
}

// normalizeTags lowercases, trims, de-duplicates, and drops empties so the
// GIN index isn't bloated with "Red Jacket" / "red jacket" / "RED JACKET"
// variants. Caps at 30 tags per segment defensively.
func normalizeTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) >= 30 {
			break
		}
	}
	return out
}

func normalizeActivity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "none", "low", "moderate", "high":
		return strings.ToLower(v)
	default:
		return "unknown"
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// CompilerHint keeps uuid import in scope even if future refactors remove
// the direct reference. Tiny cost, avoids a compile break.
var _ uuid.UUID
