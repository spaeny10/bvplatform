package recording

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// segInfo is used internally for segment lookup.
type segInfo struct {
	name      string
	absPath   string
	modTime   time.Time
	size      int64
	startTime time.Time // parsed from filename, or estimated from modTime
	endTime   time.Time // actual end — populated after all segments are listed
}

// fillEndTimes sets endTime on each segment using the next segment's startTime
// when available, or modTime for the most recent still-writing segment. The
// nominal 60s duration is unreliable because FFmpeg restarts produce short
// segments that end well before the next :00 boundary — picking such a segment
// for an event that falls in its nominal-but-not-actual range produces a clip
// that seeks past end-of-file.
func fillEndTimes(segments []segInfo) {
	if len(segments) == 0 {
		return
	}
	// Sort ascending by startTime so we can walk neighbors.
	byStart := make([]int, len(segments))
	for i := range segments {
		byStart[i] = i
	}
	sort.Slice(byStart, func(i, j int) bool {
		return segments[byStart[i]].startTime.Before(segments[byStart[j]].startTime)
	})
	for rank, idx := range byStart {
		if rank+1 < len(byStart) {
			segments[idx].endTime = segments[byStart[rank+1]].startTime
		} else {
			// Final segment is still being written. modTime lags real-time by
			// a few seconds because FFmpeg buffers before flushing moof boxes,
			// and because Windows' FindFirstFile updates modTime coarsely.
			// Treat the open segment as extending into the future so events
			// that just fired land in it instead of bouncing back to the
			// previous (closed) segment.
			segments[idx].endTime = time.Now().Add(1 * time.Minute)
		}
	}
}

// listSegments reads all MP4 segments for a camera from the filesystem.
// includeOpen controls whether segments still being written are included.
func listSegments(storagePath, cameraID string, includeOpen bool) ([]segInfo, string) {
	recordingsDir := filepath.Join(storagePath, "recordings", cameraID)
	entries, err := os.ReadDir(recordingsDir)
	if err != nil {
		recordingsDir = filepath.Join(storagePath, cameraID)
		entries, err = os.ReadDir(recordingsDir)
		if err != nil {
			return nil, ""
		}
	}

	const typicalSegDur = 60 * time.Second
	var segments []segInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mp4") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			continue // empty (FFmpeg crashed before writing)
		}
		if !includeOpen && time.Since(info.ModTime()) < 3*time.Second {
			continue // still being written
		}

		absPath := filepath.Join(recordingsDir, entry.Name())

		// Determine segment start time: prefer filename-embedded timestamp,
		// fall back to modTime - typicalSegDur.
		startTime := parseSegmentTimestamp(entry.Name())
		if startTime.IsZero() {
			startTime = info.ModTime().Add(-typicalSegDur)
		}

		segments = append(segments, segInfo{
			name:      entry.Name(),
			absPath:   absPath,
			modTime:   info.ModTime(),
			size:      info.Size(),
			startTime: startTime,
		})
	}

	fillEndTimes(segments)
	sort.Slice(segments, func(i, j int) bool {
		return segments[i].modTime.After(segments[j].modTime)
	})
	return segments, recordingsDir
}

// relURL converts an absolute segment path back to the relative URL used by the /recordings/ handler.
func relURL(storagePath, cameraID, absPath string) string {
	// Check recordings/ subdirectory first
	testPath := filepath.Join(storagePath, "recordings", cameraID, filepath.Base(absPath))
	if _, err := os.Stat(testPath); err == nil {
		return fmt.Sprintf("recordings/%s/%s", cameraID, filepath.Base(absPath))
	}
	return fmt.Sprintf("%s/%s", cameraID, filepath.Base(absPath))
}

// FindEventClip finds the recording segment that contains the event timestamp.
// Returns the relative path with a #t= seek offset so playback starts at the event moment.
// Returns "" if no suitable segment is found.
func FindEventClip(storagePath string, cameraID string, eventTime time.Time) string {
	// Always include the currently-open segment — the segment that *contains* the event
	// is almost always the one still being written at event time.
	segments, dir := listSegments(storagePath, cameraID, true)
	if len(segments) == 0 {
		log.Printf("[CLIP] No segments for camera %s (searched %q, event=%s)", cameraID, dir, eventTime.Format(time.RFC3339))
		return ""
	}

	best := pickContainingSegment(segments, eventTime)
	if best == nil {
		closest := closestSegment(segments, eventTime)
		best = &closest
	}

	url := relURL(storagePath, cameraID, best.absPath)

	// Add #t= seek offset so the video starts at the event moment
	offset := eventTime.Sub(best.startTime).Seconds()
	if offset > 0 && offset < 300 {
		url += fmt.Sprintf("#t=%.1f", offset)
	}

	log.Printf("[CLIP] camera %s event=%s → %s (offset=%.1fs, seg=%s)",
		cameraID, eventTime.Format("15:04:05"), url, offset, best.name)
	return url
}

// FindEventClipFull returns the absolute filesystem path, relative URL, and start time
// of the segment best suited for extracting a frame at eventTime.
// Unlike FindEventClip, it also considers the currently-open segment (fragmented MP4
// segments can be read by FFmpeg before they are closed).
// Returns ("", "", zero) if no segment is found.
func FindEventClipFull(storagePath, cameraID string, eventTime time.Time) (absPath, relPath string, startTime time.Time) {
	// Include open segments — fMP4 segments are readable mid-write.
	segments, _ := listSegments(storagePath, cameraID, true)
	if len(segments) == 0 {
		return "", "", time.Time{}
	}

	// Dump the last few candidates so we can diagnose why the wrong one
	// gets picked when the current segment is still being written.
	log.Printf("[CLIP-FULL] camera %s event=%s candidates (newest by modTime):",
		cameraID, eventTime.Format("15:04:05.000"))
	dumped := 0
	for i := range segments {
		if dumped >= 4 {
			break
		}
		s := segments[i]
		log.Printf("[CLIP-FULL]   %s start=%s end=%s size=%d",
			s.name, s.startTime.Format("15:04:05.000"), s.endTime.Format("15:04:05.000"), s.size)
		dumped++
	}

	best := pickContainingSegment(segments, eventTime)
	if best == nil {
		closest := closestSegment(segments, eventTime)
		best = &closest
		log.Printf("[CLIP-FULL] no contained segment → closest by modTime: %s", best.name)
	} else {
		log.Printf("[CLIP-FULL] picked: %s", best.name)
	}

	return best.absPath, relURL(storagePath, cameraID, best.absPath), best.startTime
}

// pickContainingSegment returns the segment that actually holds eventTime in
// its [startTime, endTime) window. endTime is derived from the next segment's
// start (or "open-ended now+1min" for the latest), so short segments produced
// by FFmpeg restart aren't mistakenly chosen for events that fall past their
// real end. Returns nil if no segment contains the event.
func pickContainingSegment(segments []segInfo, eventTime time.Time) *segInfo {
	const slack = 5 * time.Second
	var best *segInfo
	for i := range segments {
		s := &segments[i]
		if s.endTime.IsZero() {
			continue
		}
		segEnd := s.endTime.Add(slack)
		if (s.startTime.Before(eventTime) || s.startTime.Equal(eventTime)) && eventTime.Before(segEnd) {
			if best == nil || s.startTime.After(best.startTime) {
				best = s
			}
		}
	}
	if best == nil && len(segments) > 0 {
		// No "contained" match — dump candidates so we can see why.
		log.Printf("[CLIP] pickContainingSegment: no match for event=%s. Candidates (start/end):", eventTime.Format("15:04:05.000"))
		for i := range segments {
			s := &segments[i]
			log.Printf("[CLIP]   %s start=%s end=%s size=%d",
				s.name, s.startTime.Format("15:04:05.000"), s.endTime.Format("15:04:05.000"), s.size)
		}
	}
	return best
}

// closestSegment returns the segment whose mod time is closest to eventTime.
func closestSegment(segments []segInfo, eventTime time.Time) segInfo {
	best := segments[0]
	bestDiff := absDur(eventTime.Sub(best.modTime))
	for _, s := range segments[1:] {
		d := absDur(eventTime.Sub(s.modTime))
		if d < bestDiff {
			bestDiff = d
			best = s
		}
	}
	if bestDiff > 5*time.Minute {
		return segments[0]
	}
	return best
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
