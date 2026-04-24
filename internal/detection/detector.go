package detection

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// BoundingBox represents a single AI detection result from an ONVIF Profile M camera.
// All coordinates are normalized 0–1 relative to the source frame dimensions.
type BoundingBox struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	X          float64 `json:"x"` // left edge (normalized)
	Y          float64 `json:"y"` // top edge (normalized)
	W          float64 `json:"w"` // width (normalized)
	H          float64 `json:"h"` // height (normalized)
}

// DetectionResult is broadcast to WebSocket clients.
type DetectionResult struct {
	Type     string        `json:"type"` // always "detections"
	CameraID string        `json:"camera_id"`
	Time     string        `json:"time"`
	Boxes    []BoundingBox `json:"boxes"`
}

// Broadcaster is satisfied by the WebSocket Hub.
type Broadcaster interface {
	Broadcast(msg []byte)
}

// Manager receives ONVIF analytics events, converts them to bounding boxes,
// and broadcasts them to all WebSocket clients.
// It also caches the latest detections per camera so the REST endpoint can serve them.
type Manager struct {
	broadcaster Broadcaster

	mu     sync.RWMutex
	latest map[uuid.UUID]*DetectionResult // last result per camera
	ttlSec float64                        // seconds before a result expires (default 3)

	// AlertEmitter is called when a high-confidence detection arrives.
	// Wire this in main.go to convert detections into AlertEvents and broadcast them.
	// If nil, alert generation is disabled.
	AlertEmitter func(result *DetectionResult)
}

// New creates a detection Manager.
func New(broadcaster Broadcaster) *Manager {
	return &Manager{
		broadcaster: broadcaster,
		latest:      make(map[uuid.UUID]*DetectionResult),
		ttlSec:      3.0,
	}
}

// HandleAnalyticsEvent is called from the ONVIF event subscriber whenever an
// analytics event (objectdetect, motion with objects, face, etc.) arrives.
// It expects `boxes` to already be extracted from the ONVIF XML.
// If boxes is nil/empty the previous result stays in the cache.
func (m *Manager) HandleAnalyticsEvent(cameraID uuid.UUID, boxes []BoundingBox) {
	if len(boxes) == 0 {
		return
	}

	result := &DetectionResult{
		Type:     "detections",
		CameraID: cameraID.String(),
		Time:     time.Now().UTC().Format(time.RFC3339),
		Boxes:    boxes,
	}

	m.mu.Lock()
	m.latest[cameraID] = result
	m.mu.Unlock()

	msg, err := json.Marshal(result)
	if err != nil {
		log.Printf("[DET] Marshal error: %v", err)
		return
	}
	m.broadcaster.Broadcast(msg)

	// Generate alert if any box has confidence ≥ 0.70 and AlertEmitter is wired
	if m.AlertEmitter != nil {
		for _, b := range boxes {
			if b.Confidence >= 0.70 {
				m.AlertEmitter(result)
				break
			}
		}
	}
}

// ClearCamera removes cached detections for a camera (e.g. on camera deletion).
func (m *Manager) ClearCamera(cameraID uuid.UUID) {
	m.mu.Lock()
	delete(m.latest, cameraID)
	m.mu.Unlock()
}

// GetLatest returns the most recent detections for a camera, or nil if none / stale.
func (m *Manager) GetLatest(cameraID uuid.UUID) *DetectionResult {
	m.mu.RLock()
	result := m.latest[cameraID]
	m.mu.RUnlock()

	if result == nil {
		return nil
	}

	// Expire old results
	t, err := time.Parse(time.RFC3339, result.Time)
	if err != nil || time.Since(t).Seconds() > m.ttlSec {
		return nil
	}
	return result
}
