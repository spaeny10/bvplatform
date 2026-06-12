package recording

import (
	"strings"
	"testing"
	"time"
)

// TestCaptureEventThumbnail_NoSegmentNoFallback verifies that when there is no
// recording segment on disk (empty/nonexistent storage dir) AND no sub-stream
// URI to fall back to, CaptureEventThumbnail returns a clean, descriptive error
// rather than an empty-but-nil result. This is the "recording gap + no sub
// stream configured" corner the original RTSP-only grab silently produced an
// empty thumbnail for.
func TestCaptureEventThumbnail_NoSegmentNoFallback(t *testing.T) {
	uri, via, err := CaptureEventThumbnail(
		"ffmpeg",                          // never invoked — no segment, no sub
		t.TempDir(),                       // empty storage dir → FindEventClipFull finds nothing
		"00000000-0000-0000-0000-000000000000",
		time.Now(),
		"",          // no segment hint
		time.Time{}, // no hint start
		"",          // no sub-stream fallback
		12,
	)
	if err == nil {
		t.Fatalf("expected error when no segment and no sub-stream, got uri=%q via=%q", uri, via)
	}
	if uri != "" || via != "" {
		t.Fatalf("expected empty uri/via on error, got uri=%q via=%q", uri, via)
	}
	if !strings.Contains(err.Error(), "no local segment") {
		t.Fatalf("expected 'no local segment' error, got: %v", err)
	}
}

// TestCaptureEventThumbnail_MissingHintFileSkipped verifies that a stale/bogus
// segment-hint path (file does not exist on disk) is skipped without invoking
// ffmpeg, and the function proceeds to the next resolution stage. With no real
// segment on disk and no sub-stream, that ultimately surfaces the no-segment
// error — proving the hint branch was a no-op rather than a hard failure.
func TestCaptureEventThumbnail_MissingHintFileSkipped(t *testing.T) {
	_, _, err := CaptureEventThumbnail(
		"ffmpeg",
		t.TempDir(),
		"00000000-0000-0000-0000-000000000000",
		time.Now(),
		"/does/not/exist/seg_19700101_000000.mp4", // hint path absent → skipped
		time.Now().Add(-time.Minute),
		"",
		12,
	)
	if err == nil {
		t.Fatal("expected error (no real segment, no fallback)")
	}
	if !strings.Contains(err.Error(), "no local segment") {
		t.Fatalf("expected to fall through to no-segment error, got: %v", err)
	}
}
