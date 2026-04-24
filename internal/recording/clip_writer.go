package recording

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"onvif-tool/internal/database"
)

// ClipWriter manages event-triggered clip creation with pre/post buffer.
// It watches a ring buffer directory of temp segments, and when triggered
// by an event, it copies the pre-buffer segments and continues recording
// for the post-buffer duration.
type ClipWriter struct {
	cameraID      uuid.UUID
	cameraName    string
	ringDir       string // temp segment directory (ring buffer)
	outputDir     string // permanent clip storage
	preBufferSec  int
	postBufferSec int
	triggers      map[string]bool // event types that trigger recording

	recording    bool
	recordingEnd time.Time
	mu           sync.Mutex
	db           *database.DB
}

// NewClipWriter creates a new clip writer for a camera
func NewClipWriter(cameraID uuid.UUID, cameraName, ringDir, outputDir string,
	preBufferSec, postBufferSec int, triggerCSV string, db *database.DB) *ClipWriter {

	triggers := make(map[string]bool)
	for _, t := range strings.Split(triggerCSV, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			triggers[t] = true
		}
	}

	return &ClipWriter{
		cameraID:      cameraID,
		cameraName:    cameraName,
		ringDir:       ringDir,
		outputDir:     outputDir,
		preBufferSec:  preBufferSec,
		postBufferSec: postBufferSec,
		triggers:      triggers,
		db:            db,
	}
}

// ShouldTrigger returns true if the given event type should trigger recording
func (cw *ClipWriter) ShouldTrigger(eventType string) bool {
	return cw.triggers[eventType]
}

// TriggerEvent is called when a matching event is detected.
// It starts or extends clip recording.
func (cw *ClipWriter) TriggerEvent(ctx context.Context, eventType string, eventTime time.Time) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	newEnd := eventTime.Add(time.Duration(cw.postBufferSec) * time.Second)

	if cw.recording {
		// Extend the recording window if a new event arrives during recording
		if newEnd.After(cw.recordingEnd) {
			cw.recordingEnd = newEnd
			log.Printf("[CLIP] Extended recording for %s until %s (event: %s)",
				cw.cameraName, cw.recordingEnd.Format("15:04:05"), eventType)
		}
		return
	}

	// Start a new clip recording
	cw.recording = true
	cw.recordingEnd = newEnd
	clipStart := eventTime.Add(-time.Duration(cw.preBufferSec) * time.Second)

	log.Printf("[CLIP] Starting clip for %s: pre-buffer from %s, post-buffer until %s (trigger: %s)",
		cw.cameraName, clipStart.Format("15:04:05"), cw.recordingEnd.Format("15:04:05"), eventType)

	go cw.writeClip(ctx, clipStart, eventTime, eventType)
}

// writeClip waits for the post-buffer to expire, then collects all segments
// from the ring buffer that fall within the clip window and saves them.
func (cw *ClipWriter) writeClip(ctx context.Context, clipStart, eventTime time.Time, eventType string) {
	// Wait for the post-buffer to expire (check periodically in case it's extended)
	for {
		cw.mu.Lock()
		remaining := time.Until(cw.recordingEnd)
		cw.mu.Unlock()

		if remaining <= 0 {
			break
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(minDuration(remaining, 1*time.Second)):
		}
	}

	cw.mu.Lock()
	actualEnd := cw.recordingEnd
	cw.recording = false
	cw.mu.Unlock()

	// Collect ring buffer segments that overlap with [clipStart, actualEnd]
	segments, err := cw.collectRingSegments(clipStart, actualEnd)
	if err != nil {
		log.Printf("[CLIP] Error collecting segments for %s: %v", cw.cameraName, err)
		return
	}

	if len(segments) == 0 {
		log.Printf("[CLIP] No ring segments found for %s clip %s to %s",
			cw.cameraName, clipStart.Format("15:04:05"), actualEnd.Format("15:04:05"))
		return
	}

	// Copy segments to permanent storage
	clipID := fmt.Sprintf("clip_%s_%s", eventType, eventTime.Format("20060102_150405"))
	clipDir := filepath.Join(cw.outputDir, clipID)
	os.MkdirAll(clipDir, 0755)

	var totalSize int64
	for _, seg := range segments {
		src := seg
		dst := filepath.Join(clipDir, filepath.Base(seg))
		data, err := os.ReadFile(src)
		if err != nil {
			log.Printf("[CLIP] Failed to read ring segment %s: %v", src, err)
			continue
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			log.Printf("[CLIP] Failed to write clip segment %s: %v", dst, err)
			continue
		}
		totalSize += int64(len(data))
	}

	// Register the clip as a segment in the database
	durationMs := int(actualEnd.Sub(clipStart).Milliseconds())
	dbSeg := &database.Segment{
		CameraID:   cw.cameraID,
		StartTime:  clipStart,
		EndTime:    actualEnd,
		FilePath:   clipDir,
		FileSize:   totalSize,
		DurationMs: durationMs,
	}
	if err := cw.db.InsertSegment(ctx, dbSeg); err != nil {
		log.Printf("[CLIP] Failed to register clip in DB: %v", err)
	} else {
		log.Printf("[CLIP] Saved clip for %s: %s (%d segments, %d bytes, %ds)",
			cw.cameraName, clipID, len(segments), totalSize, durationMs/1000)
	}
}

// collectRingSegments finds temp segment files whose timestamps overlap with
// the requested time window. Ring segments are named like: ring_20260220_171312.mp4
func (cw *ClipWriter) collectRingSegments(start, end time.Time) ([]string, error) {
	entries, err := os.ReadDir(cw.ringDir)
	if err != nil {
		return nil, fmt.Errorf("read ring dir: %w", err)
	}

	var result []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".mp4") {
			continue
		}

		// Parse timestamp from filename: ring_20260220_171312.mp4
		segTime, err := parseRingTimestamp(name)
		if err != nil {
			continue
		}

		// Include segment if it overlaps with the clip window
		// (segment covers segTime to segTime + segDuration; we use a generous window)
		if segTime.Before(end) && segTime.After(start.Add(-5*time.Second)) {
			result = append(result, filepath.Join(cw.ringDir, name))
		}
	}

	sort.Strings(result)
	return result, nil
}

// CleanRingBuffer removes ring buffer segments older than the pre-buffer window
func (cw *ClipWriter) CleanRingBuffer() {
	cw.mu.Lock()
	isRecording := cw.recording
	cw.mu.Unlock()

	if isRecording {
		return // don't delete segments while recording a clip
	}

	cutoff := time.Now().Add(-time.Duration(cw.preBufferSec+5) * time.Second)

	entries, err := os.ReadDir(cw.ringDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mp4") {
			continue
		}
		segTime, err := parseRingTimestamp(entry.Name())
		if err != nil {
			continue
		}
		if segTime.Before(cutoff) {
			os.Remove(filepath.Join(cw.ringDir, entry.Name()))
		}
	}
}

// parseRingTimestamp extracts time from a ring segment filename like ring_20260220_171312.mp4
func parseRingTimestamp(filename string) (time.Time, error) {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	// Handle both "ring_YYYYMMDD_HHMMSS" and "seg_YYYYMMDD_HHMMSS" formats
	parts := strings.SplitN(base, "_", 2)
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("invalid segment filename: %s", filename)
	}
	timeStr := parts[1] // "20260220_171312"
	t, err := time.ParseInLocation("20060102_150405", timeStr, time.Now().Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time from %s: %w", filename, err)
	}
	return t, nil
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
