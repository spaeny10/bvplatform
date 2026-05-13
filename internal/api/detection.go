package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/detection"
)

// HandleDetectLatest handles GET /api/cameras/{id}/detect
// Returns the most recently cached bounding boxes from the ONVIF analytics stream.
func HandleDetectLatest(db *database.DB, det *detection.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		cam, err := db.GetCamera(r.Context(), id)
		if err != nil || cam == nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		result := det.GetLatest(id)
		if result == nil {
			writeJSON(w, map[string]interface{}{
				"camera_id": id.String(),
				"boxes":     []interface{}{},
			})
			return
		}

		writeJSON(w, result)
	}
}

// HandleDetectionStream handles GET /api/cameras/{id}/detect/stream
// Opens a Server-Sent Events stream and pushes cached detection results at the requested interval.
// Clients can poll this independently on top of the WebSocket push for guaranteed delivery.
func HandleDetectionStream(db *database.DB, det *detection.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		cam, err := db.GetCamera(r.Context(), id)
		if err != nil || cam == nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}
		_ = cam

		// Setup SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result := det.GetLatest(id)
				var data []byte
				if result != nil {
					data, _ = json.Marshal(result)
				} else {
					data, _ = json.Marshal(map[string]interface{}{
						"type":      "detections",
						"camera_id": id.String(),
						"time":      time.Now().UTC().Format(time.RFC3339),
						"boxes":     []interface{}{},
					})
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
