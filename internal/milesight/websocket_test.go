package milesight

import (
	"testing"
)

// capturedEvent records one callback invocation from parseMilesightTrack.
type capturedEvent struct {
	eventType string
	details   map[string]interface{}
}

// newCapturingStream builds an EventStream whose callback appends to *out.
func newCapturingStream(out *[]capturedEvent) *EventStream {
	return NewEventStream(
		&Camera{Host: "test", User: "u", Password: "p"},
		"test-cam",
		func(eventType string, metadata map[string]interface{}) {
			*out = append(*out, capturedEvent{eventType: eventType, details: metadata})
		},
	)
}

// bbox pulls the first bounding box out of an emitted motion event.
func bbox(t *testing.T, ev capturedEvent) map[string]interface{} {
	t.Helper()
	raw, ok := ev.details["bounding_boxes"]
	if !ok {
		t.Fatalf("event %q missing bounding_boxes: %#v", ev.eventType, ev.details)
	}
	boxes, ok := raw.([]map[string]interface{})
	if !ok || len(boxes) == 0 {
		t.Fatalf("event %q bounding_boxes wrong shape: %#v", ev.eventType, raw)
	}
	return boxes[0]
}

// TestParseMilesightTrack_AdvancedMotionEmitsMotion verifies that a trackList
// entry carrying vcaAdvancedMotion:1 (basic motion — the field the cameras on
// the WS path actually report) is mapped to exactly one "motion" event with the
// track's bounding box, and that a second identical frame does NOT re-fire
// thanks to the 0→1 edge-detect (B-11 regression).
func TestParseMilesightTrack_AdvancedMotionEmitsMotion(t *testing.T) {
	var events []capturedEvent
	es := newCapturingStream(&events)

	// Real-shaped frame: one vehicle track reporting basic (advanced) motion.
	frame := []byte(`{"objAttrList":[],"trackData":{"trackNum":1,"timeUsec":123,"trackList":[` +
		`{"trackID":729,"x":224,"y":66,"w":22,"h":12,"Class":2,"vcaAdvancedMotion":1}` +
		`]}}`)

	es.parseMilesightTrack(frame)

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event from vcaAdvancedMotion frame, got %d: %#v", len(events), events)
	}
	ev := events[0]
	if ev.eventType != "motion" {
		t.Fatalf("expected event type \"motion\", got %q", ev.eventType)
	}
	if got := ev.details["source"]; got != "milesight_ws" {
		t.Errorf("expected source milesight_ws, got %v", got)
	}
	if got := ev.details["track_id"]; got != 729 {
		t.Errorf("expected track_id 729, got %v", got)
	}
	if got := ev.details["rule_name"]; got != "Advanced Motion" {
		t.Errorf("expected rule_name \"Advanced Motion\", got %v", got)
	}
	b := bbox(t, ev)
	if b["x"] != 224 || b["y"] != 66 || b["w"] != 22 || b["h"] != 12 {
		t.Errorf("bbox mismatch, got x=%v y=%v w=%v h=%v", b["x"], b["y"], b["w"], b["h"])
	}
	if b["label"] != "vehicle" {
		t.Errorf("expected bbox label \"vehicle\" (Class 2), got %v", b["label"])
	}

	// Second identical frame: motion is still active for trackID 729, so the
	// edge-detect must suppress it — no re-fire every frame.
	es.parseMilesightTrack(frame)
	if len(events) != 1 {
		t.Fatalf("expected no re-fire on identical second frame (edge-detect), got %d events", len(events))
	}
}

// TestParseMilesightTrack_AiAndAdvancedMotionSingleEvent verifies that when a
// single track reports BOTH vcaAdvancedMotion:1 and aiMotion:1, only ONE motion
// event is emitted (both flags share the "trackID:motion" edge-detect key), not
// two — no double-counting of the same transition.
func TestParseMilesightTrack_AiAndAdvancedMotionSingleEvent(t *testing.T) {
	var events []capturedEvent
	es := newCapturingStream(&events)

	frame := []byte(`{"objAttrList":[],"trackData":{"trackNum":1,"timeUsec":1,"trackList":[` +
		`{"trackID":42,"x":10,"y":20,"w":30,"h":40,"Class":1,"vcaAdvancedMotion":1,"aiMotion":1}` +
		`]}}`)

	es.parseMilesightTrack(frame)

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 motion event when both motion flags set, got %d: %#v", len(events), events)
	}
	if events[0].eventType != "motion" {
		t.Fatalf("expected event type \"motion\", got %q", events[0].eventType)
	}
}

// TestParseMilesightTrack_AiMotionEmitsMotion verifies aiMotion alone (AI models)
// also surfaces as a "motion" event.
func TestParseMilesightTrack_AiMotionEmitsMotion(t *testing.T) {
	var events []capturedEvent
	es := newCapturingStream(&events)

	frame := []byte(`{"objAttrList":[],"trackData":{"trackNum":1,"timeUsec":1,"trackList":[` +
		`{"trackID":7,"x":1,"y":2,"w":3,"h":4,"Class":1,"aiMotion":1}` +
		`]}}`)

	es.parseMilesightTrack(frame)

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event from aiMotion frame, got %d: %#v", len(events), events)
	}
	if events[0].eventType != "motion" {
		t.Fatalf("expected event type \"motion\", got %q", events[0].eventType)
	}
	if got := events[0].details["rule_name"]; got != "AI Motion" {
		t.Errorf("expected rule_name \"AI Motion\", got %v", got)
	}
}

// TestParseMilesightTrack_MotionReFiresAfterClear verifies that once a track's
// motion flag drops to 0 (cleared from activeFlags), a subsequent 0→1 transition
// re-fires — the edge-detect must not permanently latch.
func TestParseMilesightTrack_MotionReFiresAfterClear(t *testing.T) {
	var events []capturedEvent
	es := newCapturingStream(&events)

	on := []byte(`{"objAttrList":[],"trackData":{"trackNum":1,"timeUsec":1,"trackList":[` +
		`{"trackID":5,"x":0,"y":0,"w":1,"h":1,"Class":1,"vcaAdvancedMotion":1}` +
		`]}}`)
	// A frame with the track present but motion off — clears the active flag.
	off := []byte(`{"objAttrList":[],"trackData":{"trackNum":1,"timeUsec":2,"trackList":[` +
		`{"trackID":5,"x":0,"y":0,"w":1,"h":1,"Class":1,"vcaAdvancedMotion":0}` +
		`]}}`)

	es.parseMilesightTrack(on)  // fire
	es.parseMilesightTrack(off) // clear
	es.parseMilesightTrack(on)  // fire again

	if len(events) != 2 {
		t.Fatalf("expected 2 motion events across on/off/on, got %d: %#v", len(events), events)
	}
}
