package api

import (
	"testing"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// TestDetectionSource covers every origin shape the alert feed must classify
// into a "Camera VCA" vs "Server AI" badge: the explicit source keys emitted by
// the Milesight WebSocket and Sense webhook paths, the server-side AI pipeline
// keys, and the keyless ONVIF rule-engine shape that only carries driver/topic
// (the dominant shape in the live test DB).
func TestDetectionSource(t *testing.T) {
	cases := []struct {
		name    string
		details map[string]interface{}
		want    string
	}{
		{"milesight_ws explicit source", map[string]interface{}{"source": "milesight_ws"}, "camera"},
		{"sense webhook explicit source", map[string]interface{}{"source": "milesight-sense-webhook"}, "camera"},
		{"source case-insensitive", map[string]interface{}{"source": "Milesight_WS"}, "camera"},
		{"yolo server source", map[string]interface{}{"source": "yolo"}, "server"},
		{"qwen server source", map[string]interface{}{"source": "qwen"}, "server"},
		{"generic ai server source", map[string]interface{}{"source": "ai"}, "server"},
		{
			"onvif rule-engine via driver (test-DB shape)",
			map[string]interface{}{"driver": "milesight", "topic": "tns1:RuleEngine/HumanDetector/Human", "rule": "MyHumanDetectorRule"},
			"camera",
		},
		{
			"onvif rule-engine via topic only",
			map[string]interface{}{"topic": "tns1:RuleEngine/LineDetector/Crossed"},
			"camera",
		},
		{"milesight: topic prefix", map[string]interface{}{"topic": "milesight:intrusion"}, "camera"},
		{"unknown origin", map[string]interface{}{"foo": "bar"}, ""},
		{"nil details", nil, ""},
		{"non-string source value ignored", map[string]interface{}{"source": 42, "driver": "milesight"}, "camera"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DetectionSource(c.details); got != c.want {
				t.Errorf("DetectionSource(%v) = %q, want %q", c.details, got, c.want)
			}
		})
	}
}

// TestDecorateEventSources verifies the in-place projection fills Source from
// each event's Details, including leaving unknown-origin events blank.
func TestDecorateEventSources(t *testing.T) {
	events := []database.Event{
		{EventType: "human", Details: map[string]interface{}{"driver": "milesight", "topic": "tns1:RuleEngine/HumanDetector/Human"}},
		{EventType: "intrusion", Details: map[string]interface{}{"source": "milesight_ws"}},
		{EventType: "person", Details: map[string]interface{}{"source": "yolo"}},
		{EventType: "motion", Details: map[string]interface{}{"foo": "bar"}},
	}

	decorateEventSources(events)

	want := []string{"camera", "camera", "server", ""}
	for i, w := range want {
		if events[i].Source != w {
			t.Errorf("events[%d].Source = %q, want %q", i, events[i].Source, w)
		}
	}
}

// TestApplyEventRBAC covers the multi-camera scoping the alert feed relies on:
// a single camera must be authorized, a multi-camera selection is intersected
// with the allowed set (so a restricted user can't widen their view), and an
// unscoped request defaults to the full allowed whitelist. Global-view callers
// are untouched.
func TestApplyEventRBAC(t *testing.T) {
	camA := uuid.New()
	camB := uuid.New()
	camC := uuid.New() // never authorized
	allowed := []uuid.UUID{camA, camB}

	t.Run("global view untouched", func(t *testing.T) {
		q := database.EventQuery{CameraIDs: []uuid.UUID{camC}, CameraIDsNonNil: true}
		if denied := applyEventRBAC(&q, false, allowed); denied {
			t.Fatal("global-view caller should never be denied")
		}
		// Unchanged: handler trusts an unrestricted caller's own filter.
		if len(q.CameraIDs) != 1 || q.CameraIDs[0] != camC {
			t.Errorf("global-view filter mutated: %v", q.CameraIDs)
		}
	})

	t.Run("single authorized camera passes", func(t *testing.T) {
		q := database.EventQuery{CameraID: &camA}
		if denied := applyEventRBAC(&q, true, allowed); denied {
			t.Error("authorized single camera should not be denied")
		}
	})

	t.Run("single unauthorized camera denied", func(t *testing.T) {
		q := database.EventQuery{CameraID: &camC}
		if denied := applyEventRBAC(&q, true, allowed); !denied {
			t.Error("unauthorized single camera must be denied")
		}
	})

	t.Run("multi-camera intersected with allowed", func(t *testing.T) {
		// Caller asks for A + C; only A is authorized → scope narrows to {A}.
		q := database.EventQuery{CameraIDs: []uuid.UUID{camA, camC}, CameraIDsNonNil: true}
		if denied := applyEventRBAC(&q, true, allowed); denied {
			t.Fatal("partial-overlap multi-camera should not be denied")
		}
		if len(q.CameraIDs) != 1 || q.CameraIDs[0] != camA {
			t.Errorf("expected intersection {A}, got %v", q.CameraIDs)
		}
	})

	t.Run("multi-camera all unauthorized denied", func(t *testing.T) {
		q := database.EventQuery{CameraIDs: []uuid.UUID{camC}, CameraIDsNonNil: true}
		if denied := applyEventRBAC(&q, true, allowed); !denied {
			t.Error("multi-camera with empty intersection must be denied")
		}
	})

	t.Run("unscoped defaults to allowed whitelist", func(t *testing.T) {
		q := database.EventQuery{}
		if denied := applyEventRBAC(&q, true, allowed); denied {
			t.Fatal("unscoped restricted request should default to allowed, not denied")
		}
		if !q.CameraIDsNonNil || len(q.CameraIDs) != 2 {
			t.Errorf("expected full allowed whitelist, got nonNil=%v ids=%v", q.CameraIDsNonNil, q.CameraIDs)
		}
	})
}
