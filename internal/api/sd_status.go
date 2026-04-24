package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"onvif-tool/internal/database"
	"onvif-tool/internal/onvif"
)

// SDStatusResponse is the shape the /api/cameras/{id}/sd/status endpoint
// returns. Combines:
//   - whether the camera reports any onboard storage (card seated?)
//   - whether that storage actually has recordings (card provisioned?)
//   - the oldest / newest recorded moment available for fallback playback
//   - which recording handles exist (for GetReplayUri calls later)
type SDStatusResponse struct {
	CameraID   string `json:"camera_id"`
	CameraName string `json:"camera_name"`
	Reachable  bool   `json:"reachable"`
	Error      string `json:"error,omitempty"`

	Present        bool      `json:"present"`
	StorageType    string    `json:"storage_type,omitempty"`
	RecordingCount int       `json:"recording_count"`
	DataFrom       time.Time `json:"data_from,omitempty"`
	DataUntil      time.Time `json:"data_until,omitempty"`

	// Capacity fields come from the vendor probe on firmware that doesn't
	// implement ONVIF GetStorageConfigurations (current Milesight CQ_63.x).
	// Bytes, not KiB — normalised from the camera's kilobyte strings.
	TotalBytes int64 `json:"total_bytes,omitempty"`
	UsedBytes  int64 `json:"used_bytes,omitempty"`
	FreeBytes  int64 `json:"free_bytes,omitempty"`
	// Source reports which probe populated the response: "onvif" | "milesight" | "none"
	Source string `json:"source,omitempty"`

	// Recordings is the list of recording handles we can ask Replay about.
	// Empty when the card is missing OR when no recording job is running.
	Recordings []onvif.CameraRecording `json:"recordings,omitempty"`

	// Status is a single-word traffic-light summary:
	//   ok        — storage present, recordings available, nothing to do
	//   no_data   — storage present but empty (card inserted but not yet
	//               enrolled in a recording job, or formatted empty)
	//   no_card   — camera reports no storage device at all
	//   unreachable — camera offline / bad credentials / wrong address
	Status string `json:"status"`
}

// HandleSDStatus probes the camera's ONVIF Profile G endpoints and reports
// SD-card health. Used by:
//   - the ops dashboard tile (shows per-camera fallback-recording readiness)
//   - future FindEventClip fallback path (skip the camera when no_card)
//
// RBAC: same rules as /api/cameras/{id}/recordings — customer roles only
// see their assigned cameras.
func HandleSDStatus(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}
		if ok, cErr := CanAccessCamera(r.Context(), db, claims, cameraID); cErr != nil {
			http.Error(w, cErr.Error(), http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		cam, err := db.GetCamera(r.Context(), cameraID)
		if err != nil {
			http.Error(w, "camera lookup failed", http.StatusInternalServerError)
			return
		}

		resp := SDStatusResponse{
			CameraID:   cameraID.String(),
			CameraName: cam.Name,
			Status:     "unreachable",
		}

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		connectCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		if _, cErr := client.Connect(connectCtx); cErr != nil {
			cancel()
			resp.Error = cErr.Error()
			writeJSON(w, resp)
			return
		}
		cancel()
		resp.Reachable = true

		// Fetch storage + recording summary in a shared 10s budget. ONVIF
		// responses on a healthy link are ~200ms each, but cellular /
		// bandwidth-starved cameras can take seconds per call.
		probeCtx, probeCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer probeCancel()

		storage, _ := client.GetStorageConfigurations(probeCtx)
		resp.Present = storage.Present
		resp.StorageType = storage.StorageType
		resp.RecordingCount = storage.RecordingCount
		resp.DataFrom = storage.DataFrom
		resp.DataUntil = storage.DataUntil
		if resp.Present {
			resp.Source = "onvif"
		}

		// Milesight fallback: current firmware (CQ_63.x) doesn't implement
		// ONVIF GetStorageConfigurations, so Present comes back false even
		// when a card is seated. Re-probe via the vendor CGI when ONVIF
		// claims "no card" — that's also the UI's source of truth.
		if !resp.Present {
			if ms, mErr := client.GetMilesightStorage(probeCtx); mErr == nil && ms.Present() {
				resp.Present = true
				resp.StorageType = "SD"
				resp.Source = "milesight"
				total, _ := strconv.ParseInt(ms.SDCardTotalSize, 10, 64)
				used, _ := strconv.ParseInt(ms.SDCardUseSize, 10, 64)
				free, _ := strconv.ParseInt(ms.SDCardFreeSize, 10, 64)
				const kib = 1024
				resp.TotalBytes = total * kib
				resp.UsedBytes = used * kib
				resp.FreeBytes = free * kib
			}
		}

		// Recording handles let the fallback player pick a specific
		// recording to ask Replay about. Skip when we already know there
		// are zero recordings — saves a round trip.
		if storage.RecordingCount > 0 {
			if recs, rErr := client.GetRecordings(probeCtx); rErr == nil {
				resp.Recordings = recs
			}
		}

		// Traffic-light summary. Keeps the UI rendering trivial.
		// no_data means "card is present but no ONVIF recording job is
		// enrolled" — the card may still hold vendor-native clips that
		// the future Replay fallback can surface.
		switch {
		case !resp.Present:
			resp.Status = "no_card"
		case resp.RecordingCount == 0:
			resp.Status = "no_data"
		default:
			resp.Status = "ok"
		}

		writeJSON(w, resp)
	}
}
