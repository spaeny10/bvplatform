package milesight

import (
	"bufio"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// loadGoldenFrames reads the captured real binary frames from
// testdata/binary_track_frames.txt into a name→bytes map.
func loadGoldenFrames(t *testing.T) map[string][]byte {
	t.Helper()
	f, err := os.Open("testdata/binary_track_frames.txt")
	if err != nil {
		t.Fatalf("open golden frames: %v", err)
	}
	defer f.Close()

	out := make(map[string][]byte)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("bad golden line: %q", line)
		}
		b, err := hex.DecodeString(parts[1])
		if err != nil {
			t.Fatalf("decode hex for %q: %v", parts[0], err)
		}
		out[parts[0]] = b
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan golden: %v", err)
	}
	return out
}

// TestIsBinaryTrackFrame confirms detection: magic + whole-record length.
func TestIsBinaryTrackFrame(t *testing.T) {
	frames := loadGoldenFrames(t)
	for _, name := range []string{"idle_0tracks", "1track", "2tracks", "3tracks"} {
		if !isBinaryTrackFrame(frames[name]) {
			t.Errorf("%s: expected isBinaryTrackFrame=true (len=%d)", name, len(frames[name]))
		}
	}
	// JSON frames must NOT be misdetected as binary.
	if isBinaryTrackFrame([]byte(`{"trackData":{"trackNum":0,"trackList":[]}}`)) {
		t.Error("JSON frame wrongly detected as binary")
	}
	// Right magic, but a length that is not header+N*record must be rejected.
	bad := append([]byte{}, frames["1track"]...)
	bad = bad[:len(bad)-3]
	if isBinaryTrackFrame(bad) {
		t.Error("truncated (non-record-aligned) frame wrongly accepted")
	}
}

// TestDecodeBinaryTrack_Golden decodes the captured frames and asserts the
// track fields match the values validated against the live camera
// (trajectory-coherent, scene-aligned overlay) on 2026-06-11.
func TestDecodeBinaryTrack_Golden(t *testing.T) {
	frames := loadGoldenFrames(t)

	if got := decodeBinaryTrack(frames["idle_0tracks"]); len(got) != 0 {
		t.Fatalf("idle frame: expected 0 tracks, got %d: %#v", len(got), got)
	}

	// 1-track frame: trackID 3020, vehicle, bbox (193,100,6,7), showEvent=1.
	one := decodeBinaryTrack(frames["1track"])
	if len(one) != 1 {
		t.Fatalf("1track: expected 1 track, got %d", len(one))
	}
	w := one[0]
	if w.TrackID != 3020 || w.X != 193 || w.Y != 100 || w.W != 6 || w.H != 7 || w.Class != 2 {
		t.Errorf("1track decode mismatch: %#v", w)
	}
	if w.VcaAdvancedMotion != 1 {
		t.Errorf("1track: showEvent=1 should map to VcaAdvancedMotion=1, got %d", w.VcaAdvancedMotion)
	}

	// 3-track frame: three vehicles with the captured geometry.
	three := decodeBinaryTrack(frames["3tracks"])
	if len(three) != 3 {
		t.Fatalf("3tracks: expected 3 tracks, got %d", len(three))
	}
	want := []msTrack{
		{TrackID: 3005, X: 116, Y: 109, W: 11, H: 12, Class: 2, VcaAdvancedMotion: 1},
		{TrackID: 3001, X: 226, Y: 104, W: 8, H: 9, Class: 2, VcaAdvancedMotion: 1},
		{TrackID: 3006, X: 243, Y: 106, W: 7, H: 8, Class: 2, VcaAdvancedMotion: 1},
	}
	for i, exp := range want {
		if three[i] != exp {
			t.Errorf("3tracks[%d] mismatch:\n got  %#v\n want %#v", i, three[i], exp)
		}
	}

	// Every decoded box must fall inside the 320×180 analytics frame.
	for _, tr := range append(one, three...) {
		if tr.X < 0 || tr.Y < 0 || tr.X+tr.W > msAnalyticsFrameW+8 || tr.Y+tr.H > msAnalyticsFrameH+8 {
			t.Errorf("box outside analytics frame: %#v (frame %dx%d)", tr, msAnalyticsFrameW, msAnalyticsFrameH)
		}
	}
}

// TestParseBinaryTrack_EmitsMotion runs a golden binary frame through the full
// driver path (decode → shared emitTrackEvents) and asserts it produces a
// motion event with a bbox + the analytics frame dims, and that a second
// identical frame does NOT re-fire (edge-detect, shared with the JSON path).
func TestParseBinaryTrack_EmitsMotion(t *testing.T) {
	frames := loadGoldenFrames(t)
	var events []capturedEvent
	es := newCapturingStream(&events)

	es.parseBinaryTrack(frames["1track"])

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 motion event from binary frame, got %d: %#v", len(events), events)
	}
	ev := events[0]
	if ev.eventType != "motion" {
		t.Fatalf("expected event type \"motion\", got %q", ev.eventType)
	}
	if got := ev.details["source"]; got != "milesight_ws" {
		t.Errorf("expected source milesight_ws, got %v", got)
	}
	if got := ev.details["track_id"]; got != 3020 {
		t.Errorf("expected track_id 3020, got %v", got)
	}
	if got := ev.details["frame_w"]; got != msAnalyticsFrameW {
		t.Errorf("expected frame_w %d, got %v", msAnalyticsFrameW, got)
	}
	if got := ev.details["frame_h"]; got != msAnalyticsFrameH {
		t.Errorf("expected frame_h %d, got %v", msAnalyticsFrameH, got)
	}
	b := bbox(t, ev)
	if b["x"] != 193 || b["y"] != 100 || b["w"] != 6 || b["h"] != 7 {
		t.Errorf("bbox mismatch, got x=%v y=%v w=%v h=%v", b["x"], b["y"], b["w"], b["h"])
	}
	if b["label"] != "vehicle" {
		t.Errorf("expected bbox label \"vehicle\" (Class 2), got %v", b["label"])
	}

	// Second identical frame: track 3020 is still active → edge-detect suppresses.
	es.parseBinaryTrack(frames["1track"])
	if len(events) != 1 {
		t.Fatalf("expected no re-fire on identical second binary frame (edge-detect), got %d", len(events))
	}
}

// TestParseMessage_RoutesBinary verifies the top-level dispatch in parseMessage
// routes a magic-tagged binary frame to the binary decoder (not findJSONStart,
// which previously dropped these frames — the root of O-07).
func TestParseMessage_RoutesBinary(t *testing.T) {
	frames := loadGoldenFrames(t)
	var events []capturedEvent
	es := newCapturingStream(&events)

	es.parseMessage(frames["2tracks"])

	if len(events) != 1 {
		t.Fatalf("expected 1 motion event routed through parseMessage, got %d: %#v", len(events), events)
	}
	if events[0].eventType != "motion" {
		t.Errorf("expected motion, got %q", events[0].eventType)
	}
}
