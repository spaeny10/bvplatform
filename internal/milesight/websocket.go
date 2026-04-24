package milesight

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// EventCallback is called when an analytics event is received from the camera.
type EventCallback func(eventType string, metadata map[string]interface{})

// EventStream connects to the Milesight /webstream/track WebSocket and
// dispatches parsed analytics events (motion, intrusion, lpr, etc.).
type EventStream struct {
	cam      *Camera
	callback EventCallback
	label    string // camera name for logging
	stopCh   chan struct{}
	// Edge detection: only fire when a flag transitions from 0→1, not on every frame.
	// Key: "trackID:eventType", Value: true if currently active.
	activeFlags map[string]bool
}

// NewEventStream creates a WebSocket event stream for the given camera.
func NewEventStream(cam *Camera, label string, callback EventCallback) *EventStream {
	return &EventStream{
		cam:         cam,
		callback:    callback,
		label:       label,
		stopCh:      make(chan struct{}),
		activeFlags: make(map[string]bool),
	}
}

// Start connects to the WebSocket and begins reading events.
// It reconnects automatically on disconnection with exponential backoff.
func (es *EventStream) Start(ctx context.Context) {
	go es.runLoop(ctx)
}

// Stop terminates the event stream.
func (es *EventStream) Stop() {
	close(es.stopCh)
}

func (es *EventStream) runLoop(ctx context.Context) {
	backoff := 3 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-es.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		wasConnected, err := es.connectAndRead(ctx)
		if err != nil {
			if wasConnected {
				// Was connected and lost connection — reconnect immediately
				log.Printf("[MILESIGHT] %s: WebSocket disconnected: %v (reconnecting immediately)", es.label, err)
				backoff = 3 * time.Second
			} else {
				log.Printf("[MILESIGHT] %s: WebSocket error: %v (reconnecting in %v)", es.label, err, backoff)
			}
		}

		select {
		case <-es.stopCh:
			return
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			if !wasConnected {
				backoff = min(backoff*2, maxBackoff)
			}
		}
	}
}

// connectAndRead connects to the WebSocket and reads until error.
// Returns (true, err) if it was connected and lost connection,
// or (false, err) if it failed to connect at all.
func (es *EventStream) connectAndRead(ctx context.Context) (wasConnected bool, retErr error) {
	// Get Digest auth challenge
	if err := es.cam.refreshChallenge(); err != nil {
		return false, fmt.Errorf("auth challenge: %w", err)
	}

	wsURL := fmt.Sprintf("ws://%s/webstream/track", es.cam.Host)
	authHeader := es.cam.digestHeader("GET", "/webstream/track")

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	headers := http.Header{}
	headers.Set("Authorization", authHeader)

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	log.Printf("[MILESIGHT] %s: WebSocket connected to %s", es.label, wsURL)

	for {
		select {
		case <-es.stopCh:
			return true, nil
		case <-ctx.Done():
			return true, nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return true, fmt.Errorf("read: %w", err)
		}

		es.parseMessage(data)
	}
}

var debugMsgCount int32

// parseMessage decodes the binary envelope from /webstream/track.
// The Milesight binary format has a header followed by a payload.
// We try multiple strategies to extract event data.
func (es *EventStream) parseMessage(data []byte) {
	if len(data) < 8 {
		return
	}

	// Debug: log messages with non-empty trackList or objAttrList
	count := int(debugMsgCount)
	debugMsgCount++
	hasContent := len(data) > 50 && (
		!strings.Contains(string(data[:min(len(data), 200)]), `"trackList":[]`) ||
		!strings.Contains(string(data[:min(len(data), 200)]), `"objAttrList":[]`))
	if count < 3 || hasContent {
		log.Printf("[MILESIGHT] %s: msg #%d len=%d sample=%s", es.label, count, len(data), string(data[:min(len(data), 500)]))
	}

	// Milesight /webstream/track sends pure JSON frames:
	// {"objAttrList":[...],"trackData":{"trackNum":N,"timeUsec":...,"trackList":[...]}}
	// objAttrList contains detected objects; trackData.trackList has rule-triggered events.
	if data[0] == '{' {
		es.parseMilesightTrack(data)
		return
	}

	// Fallback: look for embedded JSON in binary envelope
	if idx := findJSONStart(data); idx >= 0 {
		es.parseMilesightTrack(data[idx:])
		return
	}
}

// Milesight /webstream/track JSON frame — flag-based analytics events.
// Each trackList entry has boolean/int flag fields for each VCA rule type.
type msTrackFrame struct {
	ObjAttrList []json.RawMessage `json:"objAttrList"`
	TrackData   struct {
		TrackNum  int   `json:"trackNum"`
		TimeUsec  int64 `json:"timeUsec"`
		TimeHd    int64 `json:"timeHd"`
		TrackList []struct {
			TrackID              int `json:"trackID"`
			X                    int `json:"x"`
			Y                    int `json:"y"`
			W                    int `json:"w"`
			H                    int `json:"h"`
			Class                int `json:"Class"` // 1=human, 2=vehicle
			VcaIntrusionDetection int `json:"vcaIntrusionDetection"`
			VcaIntrusionEnter    int `json:"vcaIntrusionEnter"`
			VcaIntrusionExit     int `json:"vcaIntrusionExit"`
			LineCrossing         int `json:"lineCrossing"`
			ObjectLoitering      int `json:"objectLoitering"`
			HumanDetection       int `json:"humanDetection"`
			VcaAdvancedMotion    int `json:"vcaAdvancedMotion"`
			AiMotion             int `json:"aiMotion"`
			ObjectLeftRemoved    int `json:"objectLeftRemoved"`
			TamperDefocus        int `json:"tamperDefocus"`
			ObjectCounting       int `json:"objectCounting"`
			PeopleCountRgn       int `json:"peopleCountRgn"`
			VehicleCountLine     int `json:"vehicleCountLine"`
		} `json:"trackList"`
	} `json:"trackData"`
}

// classNames maps Milesight Class IDs to object type strings.
var classNames = map[int]string{1: "human", 2: "vehicle", 3: "face"}

// parseMilesightTrack handles the flag-based JSON frames from /webstream/track.
// Uses edge detection: only fires callback on 0→1 transitions, not every frame.
func (es *EventStream) parseMilesightTrack(data []byte) {
	var msg msTrackFrame
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// Track which flags are active THIS frame (to detect 1→0 transitions)
	currentActive := make(map[string]bool)

	for _, track := range msg.TrackData.TrackList {
		objType := classNames[track.Class]
		if objType == "" {
			objType = fmt.Sprintf("class_%d", track.Class)
		}

		type flagDef struct {
			val       int
			eventType string
			ruleName  string
		}
		flags := []flagDef{
			{track.VcaIntrusionDetection, "intrusion", "Intrusion Detection"},
			{track.VcaIntrusionEnter, "intrusion", "Intrusion Zone Enter"},
			{track.VcaIntrusionExit, "intrusion", "Intrusion Zone Exit"},
			{track.LineCrossing, "linecross", "Line Crossing"},
			{track.ObjectLoitering, "loitering", "Object Loitering"},
			{track.HumanDetection, "human", "Human Detection"},
			{track.ObjectLeftRemoved, "object", "Object Left/Removed"},
			{track.TamperDefocus, "intrusion", "Tamper/Defocus"},
		}

		for _, f := range flags {
			key := fmt.Sprintf("%d:%s", track.TrackID, f.eventType)

			if f.val != 0 {
				currentActive[key] = true

				// Edge detect: only fire on 0→1 transition
				if es.activeFlags[key] {
					continue // already active, skip
				}
				es.activeFlags[key] = true

				details := map[string]interface{}{
					"source":    "milesight_ws",
					"topic":     "milesight:" + f.eventType,
					"rule_name": f.ruleName,
					"obj_type":  objType,
					"score":     0.95,
					"track_id":  track.TrackID,
					"bounding_boxes": []map[string]interface{}{
						{"x": track.X, "y": track.Y, "w": track.W, "h": track.H, "label": objType},
					},
				}

				log.Printf("[MILESIGHT] %s: %s %s (rule=%s track=%d pos=%d,%d %dx%d)",
					es.label, f.eventType, objType, f.ruleName, track.TrackID, track.X, track.Y, track.W, track.H)

				es.callback(f.eventType, details)
				return // one event per transition
			}
		}
	}

	// Clear flags that are no longer active (1→0 transition)
	for key := range es.activeFlags {
		if !currentActive[key] {
			delete(es.activeFlags, key)
		}
	}
}

// normalizeEventType maps Milesight event names to our internal type names.
func normalizeEventType(raw string) string {
	raw = strings.ToLower(raw)
	switch {
	case raw == "motion" || strings.Contains(raw, "motion"):
		return "motion"
	case raw == "intrusion" || strings.Contains(raw, "intrusion") || strings.Contains(raw, "fielddete"):
		return "intrusion"
	case raw == "linecross" || strings.Contains(raw, "linecross") || strings.Contains(raw, "tripwire"):
		return "linecross"
	case raw == "loitering" || strings.Contains(raw, "loiter"):
		return "loitering"
	case raw == "human" || strings.Contains(raw, "human") || strings.Contains(raw, "person"):
		return "human"
	case raw == "vehicle" || strings.Contains(raw, "vehicle") || raw == "vehiclecount":
		return "vehicle"
	case raw == "face" || strings.Contains(raw, "face"):
		return "face"
	case raw == "lpr" || strings.Contains(raw, "lpr") || strings.Contains(raw, "license"):
		return "lpr"
	case raw == "alarm" || strings.Contains(raw, "alarm"):
		return "intrusion" // alarm I/O → treat as intrusion
	case raw == "parking" || raw == "parkingviolation" || strings.Contains(raw, "parking"):
		return "loitering" // parking violation → treat as loitering
	case strings.Contains(raw, "object"):
		return "object"
	default:
		return "" // ignore unknown types
	}
}

func findJSONStart(data []byte) int {
	for i := range data {
		if data[i] == '{' && i+2 < len(data) && data[i+1] == '"' {
			return i
		}
	}
	return -1
}
