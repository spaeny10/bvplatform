package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"onvif-tool/internal/database"
	"onvif-tool/internal/drivers"
	"onvif-tool/internal/onvif"
	"onvif-tool/internal/recording"
	"onvif-tool/internal/streaming"
)

// SubscriberRegistry tracks ONVIF event subscribers per camera so they can be
// stopped when a camera is deleted — preventing goroutine leaks.
type SubscriberRegistry struct {
	mu   sync.Mutex
	subs map[uuid.UUID]*onvif.EventSubscriber
}

// NewSubscriberRegistry creates a new registry.
func NewSubscriberRegistry() *SubscriberRegistry {
	return &SubscriberRegistry{subs: make(map[uuid.UUID]*onvif.EventSubscriber)}
}

// Register stores a subscriber for later cleanup.
func (sr *SubscriberRegistry) Register(cameraID uuid.UUID, sub *onvif.EventSubscriber) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	// Stop any previous subscriber for this camera
	if old, ok := sr.subs[cameraID]; ok {
		old.Stop()
	}
	sr.subs[cameraID] = sub
}

// Stop stops and removes the subscriber for a camera.
func (sr *SubscriberRegistry) Stop(cameraID uuid.UUID) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if sub, ok := sr.subs[cameraID]; ok {
		sub.Stop()
		delete(sr.subs, cameraID)
		log.Printf("[EVENTS] Stopped subscriber for camera %s", cameraID)
	}
}

// StopAll stops all subscribers (for graceful shutdown).
func (sr *SubscriberRegistry) StopAll() {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	for id, sub := range sr.subs {
		sub.Stop()
		delete(sr.subs, id)
	}
	log.Println("[EVENTS] All subscribers stopped")
}

// EvictPTZClient removes a cached PTZ client for a deleted camera.
func EvictPTZClient(cameraID uuid.UUID) {
	key := cameraID.String()
	ptzClientCache.mu.Lock()
	delete(ptzClientCache.clients, key)
	ptzClientCache.mu.Unlock()
}

// DiscoverPreviewRequest contains credentials for previewing a discovered camera
type DiscoverPreviewRequest struct {
	Address  string `json:"address"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleDiscoverPreview fetches a live snapshot from a discovered camera using provided credentials
func HandleDiscoverPreview() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DiscoverPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.Address == "" || req.Username == "" || req.Password == "" {
			http.Error(w, "Address, username, and password are required", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		// 1. Connect to the camera
		client := onvif.NewClient(req.Address, req.Username, req.Password)
		log.Printf("[PREVIEW] Connecting to %s", req.Address)
		_, err := client.Connect(ctx)
		if err != nil {
			log.Printf("[PREVIEW] Connect failed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to connect to camera: %v", err), http.StatusInternalServerError)
			return
		}

		// 2. Get profiles
		log.Printf("[PREVIEW] Getting profiles for %s", req.Address)
		profiles, err := client.GetProfiles(ctx)
		if err != nil || len(profiles) == 0 {
			log.Printf("[PREVIEW] GetProfiles failed: %v, length: %d", err, len(profiles))
			http.Error(w, fmt.Sprintf("Failed to get camera profiles: %v", err), http.StatusInternalServerError)
			return
		}

		// 3. Get snapshot URI for the primary profile
		log.Printf("[PREVIEW] Getting snapshot URI for profile %s", profiles[0].Token)
		uri, err := client.GetSnapshotURI(ctx, profiles[0].Token)
		if err != nil {
			log.Printf("[PREVIEW] GetSnapshotURI failed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to get snapshot URI: %v", err), http.StatusInternalServerError)
			return
		}

		// 4. Download snapshot bytes
		log.Printf("[PREVIEW] Fetching snapshot from %s", uri)
		imgBytes, err := client.FetchSnapshot(ctx, uri)
		if err != nil {
			log.Printf("[PREVIEW] FetchSnapshot failed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to fetch snapshot image: %v", err), http.StatusInternalServerError)
			return
		}

		// 5. Serve the image
		log.Printf("[PREVIEW] Success, serving %d bytes", len(imgBytes))
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Write(imgBytes)
	}
}

// HandleListCameras returns all cameras
func HandleListCameras(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cameras, err := db.ListCameras(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if cameras == nil {
			cameras = []database.Camera{}
		}
		writeJSON(w, cameras)
	}
}

// HandleGetCamera returns a single camera by ID
func HandleGetCamera(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		camera, err := db.GetCamera(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if camera == nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}
		writeJSON(w, camera)
	}
}

// HandleCreateCamera adds a new camera
func HandleCreateCamera(db *database.DB, recEngine *recording.Engine, hlsServer *streaming.HLSServer, mtxServer *streaming.MediaMTXServer, hub *Hub, subReg *SubscriberRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input database.CameraCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if input.OnvifAddress == "" {
			http.Error(w, "onvif_address is required", http.StatusBadRequest)
			return
		}
		if input.Name == "" {
			input.Name = input.OnvifAddress
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		client := onvif.NewClient(input.OnvifAddress, input.Username, input.Password)
		info, err := client.Connect(ctx)
		if err != nil {
			http.Error(w, "Failed to connect to ONVIF camera: "+err.Error(), http.StatusBadRequest)
			return
		}

		profiles, err := client.GetProfiles(ctx)
		if err != nil || len(profiles) == 0 {
			http.Error(w, "Failed to get camera profiles", http.StatusBadRequest)
			return
		}

		// Check for a manufacturer-specific driver
		drv := drivers.ForDevice(info)
		if drv != nil {
			log.Printf("[CAMERA] Using %s driver for %s %s", drv.Name(), info.Manufacturer, info.Model)
		}

		// Select main and sub stream profiles
		var mainProfile, subProfile onvif.StreamProfile
		if drv != nil {
			mainProfile, subProfile = drv.SelectProfiles(profiles)
		} else {
			// Generic: highest resolution for main, lowest for sub
			maxRes := 0
			minRes := int(^uint(0) >> 1)
			for _, p := range profiles {
				res := p.Width * p.Height
				if res > maxRes && p.StreamURI != "" {
					maxRes = res
					mainProfile = p
				}
				if res > 0 && res < minRes && p.StreamURI != "" {
					minRes = res
					subProfile = p
				}
			}
		}

		if mainProfile.StreamURI == "" {
			http.Error(w, "Camera did not return a valid RTSP stream URI", http.StatusBadRequest)
			return
		}

		// Normalize RTSP URIs — driver-specific or generic
		var mainUri, subUri string
		if drv != nil {
			mainUri = drv.NormalizeRTSPURI(mainProfile.StreamURI, input.Username, input.Password)
			if subProfile.StreamURI != "" && subProfile.StreamURI != mainProfile.StreamURI {
				subUri = drv.NormalizeRTSPURI(subProfile.StreamURI, input.Username, input.Password)
			}
		} else {
			mainUri = strings.Replace(mainProfile.StreamURI, "udp://", "tcp://", 1)
			if subProfile.StreamURI != "" && subProfile.StreamURI != mainProfile.StreamURI {
				subUri = strings.Replace(subProfile.StreamURI, "udp://", "tcp://", 1)
			}
		}

		// Apply driver defaults or generic defaults
		retentionDays := 30
		recordingMode := ""
		preBuffer := 0
		postBuffer := 0
		triggers := ""
		eventsEnabled := false
		if drv != nil {
			defs := drv.DefaultSettings()
			retentionDays = defs.RetentionDays
			recordingMode = defs.RecordingMode
			preBuffer = defs.PreBufferSec
			postBuffer = defs.PostBufferSec
			triggers = defs.RecordingTriggers
			eventsEnabled = defs.EventsEnabled
		}

		cam := &database.Camera{
			Name:              input.Name,
			OnvifAddress:      input.OnvifAddress,
			Username:          input.Username,
			Password:          input.Password,
			RetentionDays:     retentionDays,
			Recording:         true,
			RecordingMode:     recordingMode,
			PreBufferSec:      preBuffer,
			PostBufferSec:     postBuffer,
			RecordingTriggers: triggers,
			EventsEnabled:     eventsEnabled,
			RTSPUri:           mainUri,
			SubStreamUri:      subUri,
			ProfileToken:      mainProfile.Token,
			HasPTZ:            mainProfile.HasPTZ,
			Manufacturer:      info.Manufacturer,
			Model:             info.Model,
			Firmware:          info.FirmwareVersion,
			Status:            "offline",
		}

		if err := db.CreateCamera(r.Context(), cam); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update to online since we just successfully connected
		db.UpdateCameraStatus(r.Context(), cam.ID, "online")
		cam.Status = "online"

		if cam.Recording {
			// Recording policy comes from the camera's site (if assigned).
			// A brand-new camera created here usually isn't assigned yet —
			// SettingsForCamera returns engine defaults in that case, and
			// the next recording restart picks up the site policy once the
			// admin assigns the camera to a site.
			settings := recording.SettingsForCamera(r.Context(), db, cam)
			if err := recEngine.StartRecording(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri, settings); err != nil {
				log.Printf("[API] Failed to start recording for %s: %v", cam.Name, err)
			}
		} else {
			if err := hlsServer.StartLiveStream(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri); err != nil {
				log.Printf("[API] Failed to start HLS for %s: %v", cam.Name, err)
			}
		}

		// Register stream with MediaMTX. AddStream pushes the new path to
		// MediaMTX's HTTP control API in the background (runtime update,
		// no restart). PersistConfig writes the bootstrap YAML so a later
		// mediamtx process restart recovers with this camera already
		// configured. No more full process restart on every camera add.
		if mtxServer != nil {
			mtxServer.AddStream(cam.ID, cam.Name, cam.RTSPUri, cam.SubStreamUri)
			if err := mtxServer.PersistConfig(); err != nil {
				log.Printf("[API] Failed to persist MediaMTX config for %s: %v", cam.Name, err)
			}
		}

		// Start event subscription and register for cleanup on camera delete
		subscriber := onvif.NewEventSubscriber(client, cam.ID, func(cameraID uuid.UUID, eventType string, details map[string]interface{}) {
			evt := &database.Event{
				CameraID:  cameraID,
				EventTime: time.Now(),
				EventType: eventType,
				Details:   details,
			}
			db.InsertEvent(context.Background(), evt)

			wsMsg, _ := json.Marshal(map[string]interface{}{
				"type":      "event",
				"camera_id": cameraID.String(),
				"event":     eventType,
				"details":   details,
				"time":      time.Now().Format(time.RFC3339),
			})
			hub.Broadcast(wsMsg)

			// ── Generate SOC alarm for VCA/AI events on site-assigned cameras ──
			alarmTypes := map[string]bool{
				"intrusion": true, "linecross": true, "human": true, "vehicle": true,
				"face": true, "loitering": true, "lpr": true, "peoplecount": true,
				"motion": true, "object": true,
			}
			if alarmTypes[eventType] {
				go func() {
					camName, siteID, siteName, err := db.GetCameraWithSite(context.Background(), cameraID.String())
					if err != nil || siteID == "" {
						return // camera not assigned to a site — skip alarm
					}
					now := time.Now().UnixMilli()

					// Map event type to severity
					severity := "medium"
					switch eventType {
					case "intrusion", "human", "face":
						severity = "critical"
					case "vehicle", "linecross", "loitering", "lpr":
						severity = "high"
					}

					snapshotURL := fmt.Sprintf("/api/cameras/%s/vca/snapshot", cameraID.String())
					alarm := &database.ActiveAlarm{
						ID:            fmt.Sprintf("alarm-%s-%d", cameraID.String()[:8], now),
						SiteID:        siteID,
						SiteName:      siteName,
						CameraID:      cameraID.String(),
						CameraName:    camName,
						Severity:      severity,
						Type:          eventType,
						Description:   fmt.Sprintf("%s detected on %s at %s", eventType, camName, siteName),
						SnapshotURL:   snapshotURL,
						ClipURL:       "",
						Ts:            now,
						SlaDeadlineMs: now + 90000,
					}

					created, err := db.CreateActiveAlarm(context.Background(), alarm)
					if err != nil {
						log.Printf("[ALARM] Failed to create alarm: %v", err)
						return
					}
					if !created {
						return
					}

					log.Printf("[ALARM] %s event → SOC alarm %s (%s at %s)", eventType, alarm.ID, camName, siteName)

					alertMsg, _ := json.Marshal(map[string]interface{}{
						"type": "alert",
						"data": map[string]interface{}{
							"id": alarm.ID, "site_id": siteID, "site_name": siteName,
							"camera_id": cameraID.String(), "camera_name": camName,
							"severity": severity, "type": eventType,
							"description": alarm.Description, "ts": now,
							"acknowledged": false, "escalation_level": 0,
							"sla_deadline_ms": alarm.SlaDeadlineMs,
							"snapshot_url": snapshotURL, "clip_url": "",
						},
					})
					hub.Broadcast(alertMsg)
				}()
			}
		})
		// Attach driver-specific event hooks if available
		if drv != nil {
			subscriber.Classify = drv.ClassifyEvent
			subscriber.Enrich = drv.EnrichEvent
		}
		subscriber.Start(context.Background())
		if subReg != nil {
			subReg.Register(cam.ID, subscriber)
		}

		log.Printf("[API] Camera created and started: %s (%s)", cam.Name, cam.ID)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, cam)
	}
}

// HandleUpdateCamera updates camera settings
func HandleUpdateCamera(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		var update database.CameraUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := db.UpdateCamera(r.Context(), id, update); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		camera, _ := db.GetCamera(r.Context(), id)
		writeJSON(w, camera)
	}
}

// HandleDeleteCamera removes a camera and cleans up all associated resources
// (event subscriber, PTZ cache, recording, HLS stream, MediaMTX stream).
func HandleDeleteCamera(db *database.DB, recEngine *recording.Engine, hlsServer *streaming.HLSServer, mtxServer *streaming.MediaMTXServer, subReg *SubscriberRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		// Stop all associated resources before deleting the DB row
		if subReg != nil {
			subReg.Stop(id)
		}
		EvictPTZClient(id)
		recEngine.StopRecording(id) // ignore error if not recording
		hlsServer.StopLiveStream(id)
		if mtxServer != nil {
			// RemoveStream pushes the path delete to the control API in
			// the background; PersistConfig writes the updated bootstrap
			// YAML so a future mediamtx restart doesn't bring the deleted
			// camera back.
			mtxServer.RemoveStream(id)
			if err := mtxServer.PersistConfig(); err != nil {
				log.Printf("[API] Failed to persist MediaMTX config after removing %s: %v", id, err)
			}
		}

		if err := db.DeleteCamera(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[API] Camera deleted (resources cleaned up): %s", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// PTZMoveRequest struct for parsing the move JSON body
type PTZMoveRequest struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Zoom float64 `json:"zoom"`
}

// ptzClientCache stores reusable ONVIF clients keyed by camera ID
var ptzClientCache = struct {
	mu      sync.RWMutex
	clients map[string]*ptzCachedClient
}{clients: make(map[string]*ptzCachedClient)}

type ptzCachedClient struct {
	client       *onvif.Client
	profileToken string
}

func getPTZClient(ctx context.Context, db *database.DB, cameraID uuid.UUID) (*ptzCachedClient, error) {
	key := cameraID.String()

	// Fast path: check cache
	ptzClientCache.mu.RLock()
	cached, ok := ptzClientCache.clients[key]
	ptzClientCache.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// Slow path: create and cache
	camera, err := db.GetCamera(ctx, cameraID)
	if err != nil || camera == nil {
		return nil, fmt.Errorf("camera not found")
	}

	client := onvif.NewClient(camera.OnvifAddress, camera.Username, camera.Password)
	cached = &ptzCachedClient{
		client:       client,
		profileToken: camera.ProfileToken,
	}

	ptzClientCache.mu.Lock()
	ptzClientCache.clients[key] = cached
	ptzClientCache.mu.Unlock()

	return cached, nil
}

// HandlePTZMove handles continuous PTZ movement
func HandlePTZMove(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		var req PTZMoveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		cached, err := getPTZClient(r.Context(), db, id)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		// Respond immediately, process ONVIF call async
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]string{"status": "moving"})

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := cached.client.PTZMove(ctx, cached.profileToken, req.Pan, req.Tilt, req.Zoom); err != nil {
				log.Printf("[PTZ] Move failed: %v", err)
			}
		}()
	}
}

// HandlePTZStop stops all PTZ movement
func HandlePTZStop(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		cached, err := getPTZClient(r.Context(), db, id)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		// Respond immediately, process ONVIF call async
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]string{"status": "stopped"})

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := cached.client.PTZStop(ctx, cached.profileToken); err != nil {
				log.Printf("[PTZ] Stop failed: %v", err)
			}
		}()
	}
}

// HandlePTZPrewarm pre-creates the ONVIF client and warms the TCP connection
// so subsequent PTZ commands execute faster
func HandlePTZPrewarm(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		// Respond immediately
		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]string{"status": "warming"})

		// Create & cache the client + warm TCP connection in background
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cached, err := getPTZClient(ctx, db, id)
			if err != nil {
				log.Printf("[PTZ] Prewarm failed: %v", err)
				return
			}
			// Send a harmless PTZStop to warm the TCP connection
			if err := cached.client.PTZStop(ctx, cached.profileToken); err != nil {
				log.Printf("[PTZ] Prewarm connection warm-up (non-fatal): %v", err)
			} else {
				log.Printf("[PTZ] Connection pre-warmed for camera %s", id)
			}
		}()
	}
}

// HandlePlayback finds the recorded MP4 segment(s) that cover a given time and returns their URLs as JSON
func HandlePlayback(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		// RBAC: caller must have access to this camera (role-based or site-
		// assigned). 404 on denial so unauthorized callers can't probe for
		// camera UUID existence.
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if ok, cErr := CanAccessCamera(r.Context(), db, claims, id); cErr != nil {
			http.Error(w, cErr.Error(), http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		tStr := r.URL.Query().Get("t")
		if tStr == "" {
			http.Error(w, "missing time parameter 't'", http.StatusBadRequest)
			return
		}

		// The frontend passes ISO 8601 strings, which are RFC3339
		t, err := time.Parse(time.RFC3339, tStr)
		if err != nil {
			http.Error(w, "invalid time format, expected RFC3339", http.StatusBadRequest)
			return
		}

		// Search for segments in a 1-hour window around the requested time
		start := t.Add(-30 * time.Minute)
		end := t.Add(30 * time.Minute)

		segments, err := db.GetSegments(r.Context(), id, start, end)
		if err != nil || len(segments) == 0 {
			http.Error(w, "no recordings found for the specified time range", http.StatusNotFound)
			return
		}

		// Build a list of segment URLs
		type segInfo struct {
			URL       string `json:"url"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
			Duration  int    `json:"duration_ms"`
		}
		var result []segInfo
		for _, seg := range segments {
			result = append(result, segInfo{
				URL:       "/recordings/" + id.String() + "/" + strings.ReplaceAll(filepath.Base(seg.FilePath), "\\", "/"),
				StartTime: seg.StartTime.Format(time.RFC3339),
				EndTime:   seg.EndTime.Format(time.RFC3339),
				Duration:  seg.DurationMs,
			})
		}

		w.Header().Set("Cache-Control", "no-cache")
		// Audit: caller requested the list of playable MP4 segments around a
		// specific moment — not yet a single segment, but the intent was to
		// play back. Record at the camera level.
		auditPlayback(db, claims, r, "GET /api/playback/{id}", id, 0, 0)
		writeJSON(w, result)
	}
}

// HandlePlaybackHLS generates a dynamic HLS VOD playlist (M3U8) from recorded segments.
// This allows HLS.js to manage all buffering, seeking, and segment transitions natively.
func HandlePlaybackHLS(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		// RBAC: same guard as HandlePlayback — deny with 404 if the caller
		// can't see this camera (avoids UUID-existence probing).
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if ok, cErr := CanAccessCamera(r.Context(), db, claims, id); cErr != nil {
			http.Error(w, cErr.Error(), http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Parse time range — default to 2 hours around 't'
		tStr := r.URL.Query().Get("t")
		if tStr == "" {
			http.Error(w, "missing time parameter 't'", http.StatusBadRequest)
			return
		}
		t, err := time.Parse(time.RFC3339, tStr)
		if err != nil {
			http.Error(w, "invalid time format", http.StatusBadRequest)
			return
		}

		start := t.Add(-1 * time.Hour)
		end := t.Add(1 * time.Hour)

		// Allow explicit start/end override
		if s := r.URL.Query().Get("start"); s != "" {
			if parsed, err := time.Parse(time.RFC3339, s); err == nil {
				start = parsed
			}
		}
		if e := r.URL.Query().Get("end"); e != "" {
			if parsed, err := time.Parse(time.RFC3339, e); err == nil {
				end = parsed
			}
		}

		segments, err := db.GetSegments(r.Context(), id, start, end)
		if err != nil || len(segments) == 0 {
			http.Error(w, "no recordings found", http.StatusNotFound)
			return
		}
		// Audit: caller requested the HLS VOD playlist — this is the first
		// step of an actual watch session.
		auditPlayback(db, claims, r, "GET /api/playback/{id}/playlist.m3u8", id, 0, 0)

		// Find the longest segment duration for EXT-X-TARGETDURATION
		var maxDurSec float64
		for _, seg := range segments {
			dur := float64(seg.DurationMs) / 1000.0
			if dur > maxDurSec {
				maxDurSec = dur
			}
		}
		if maxDurSec < 1 {
			maxDurSec = 10
		}

		// Build M3U8 playlist
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		b.WriteString("#EXT-X-VERSION:7\n")
		b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(maxDurSec)+1))
		b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
		b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
		b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")

		for i, seg := range segments {
			durSec := float64(seg.DurationMs) / 1000.0
			if durSec <= 0 {
				durSec = 5.0 // fallback
			}

			segURL := "/recordings/" + id.String() + "/" + strings.ReplaceAll(filepath.Base(seg.FilePath), "\\", "/")

			// Add discontinuity marker if there's a gap between segments (>2s gap)
			if i > 0 {
				prevEnd := segments[i-1].EndTime
				gap := seg.StartTime.Sub(prevEnd)
				if gap > 2*time.Second || gap < -2*time.Second {
					b.WriteString("#EXT-X-DISCONTINUITY\n")
				}
			}

			// Each fMP4 segment is self-contained with its own init (moov) section
			b.WriteString(fmt.Sprintf("#EXT-X-MAP:URI=\"%s\"\n", segURL))

			// Program date-time lets HLS.js map currentTime to wall-clock
			b.WriteString(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s\n", seg.StartTime.Format(time.RFC3339Nano)))
			b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", durSec))
			b.WriteString(segURL + "\n")
		}

		b.WriteString("#EXT-X-ENDLIST\n")

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(b.String()))
	}
}
