package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// HandleSenseWebhook is the inbound endpoint for Milesight Sense / SC4xx
// PIR-triggered cameras. The camera POSTs a multipart/form-data body
// with a JSON metadata blob plus one or more inline snapshot files
// when a PIR event fires. We auth by the per-camera token in the URL,
// translate the payload into the platform's normal event + alarm
// pipeline, and save the snapshot to disk so it appears in the SOC
// console exactly like a continuous-camera alarm.
//
// The endpoint is unauthenticated by design — the long token IS the
// authentication (HMAC-style). Cameras can't carry JWTs.
//
// POST /api/integrations/milesight/sense/{token}
func HandleSenseWebhook(cfg *config.Config, db *database.DB, hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		cam, err := db.GetCameraBySenseToken(r.Context(), token)
		if err != nil {
			http.Error(w, "lookup error", http.StatusInternalServerError)
			return
		}
		if cam == nil || cam.DeviceClass != "sense_pushed" {
			// Don't leak whether the camera exists; just say 401.
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		payload, snapshot, err := parseSensePayload(r)
		if err != nil {
			log.Printf("[SENSE] %s: payload parse error: %v", cam.Name, err)
			http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		// First-event status update: every sense camera starts in
		// "awaiting_first_event" — the moment a real alarm lands we
		// know it's reachable, paired, and pushing correctly.
		if cam.Status != "online" {
			_ = db.UpdateCameraStatus(r.Context(), cam.ID, "online")
		}

		now := time.Now().UTC()
		eventType := mapSenseEventType(payload.EventType)

		// Persist the snapshot so the operator console can render it.
		// Using the same on-disk layout as the continuous-camera path
		// (snapshots/<camera_id>/<filename>) so the existing snapshot
		// HTTP handler serves it without changes.
		var snapshotURL string
		if len(snapshot) > 0 && cfg.StoragePath != "" {
			snapDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", cam.ID.String())
			if err := os.MkdirAll(snapDir, 0o755); err == nil {
				fname := fmt.Sprintf("sense-%d.jpg", now.UnixMilli())
				if err := os.WriteFile(filepath.Join(snapDir, fname), snapshot, 0o644); err == nil {
					snapshotURL = "/snapshots/" + cam.ID.String() + "/" + fname
				}
			}
		}

		// Insert an event row that mirrors the shape ONVIF events use.
		// The details map is the raw payload plus the bounding box —
		// VCA UI / forensic replay reads it without needing to know the
		// payload originated from the Milesight push protocol.
		details := map[string]interface{}{
			"source":                "milesight-sense-webhook",
			"raw_event_type":        payload.EventType,
			"device_name":           payload.DeviceName,
			"mac_address":           payload.MacAddress,
			"latitude":              payload.Latitude,
			"longitude":             payload.Longitude,
			"detection_region":      payload.DetectionRegion,
			"detection_region_name": payload.DetectionRegionName,
			"resolution_width":      payload.ResolutionWidth,
			"resolution_height":     payload.ResolutionHeight,
		}
		if payload.CoordinateX2 > 0 || payload.CoordinateY2 > 0 {
			details["bounding_boxes"] = []map[string]interface{}{
				{
					"x1": payload.CoordinateX1,
					"y1": payload.CoordinateY1,
					"x2": payload.CoordinateX2,
					"y2": payload.CoordinateY2,
				},
			}
		}

		evt := &database.Event{
			CameraID:  cam.ID,
			EventTime: now,
			EventType: eventType,
			Details:   details,
		}
		if err := db.InsertEvent(r.Context(), evt); err != nil {
			log.Printf("[SENSE] %s: insert event: %v", cam.Name, err)
		}

		// Build an active alarm. Sense cameras' confidence is high by
		// design (PIR + on-device classifier already filtered) so we
		// default severity to "medium" rather than "low".
		alarmID := fmt.Sprintf("alarm-%s-%d", cam.ID.String()[:8], now.UnixMilli())
		siteName := ""
		if cam.SiteID != "" {
			if site, err := db.GetSite(r.Context(), cam.SiteID); err == nil && site != nil {
				siteName = site.Name
			}
		}
		alarm := &database.ActiveAlarm{
			ID:                alarmID,
			SiteID:            cam.SiteID,
			SiteName:          siteName,
			CameraID:          cam.ID.String(),
			CameraName:        cam.Name,
			Severity:          "medium",
			Type:              eventType,
			Description:       senseDescription(eventType, payload),
			SnapshotURL:       snapshotURL,
			Ts:                now.UnixMilli(),
			Acknowledged:      false,
			TriggeringEventID: &evt.ID,
		}
		if _, err := db.CreateActiveAlarm(r.Context(), alarm); err != nil {
			log.Printf("[SENSE] %s: create alarm: %v", cam.Name, err)
		}

		// Broadcast to operators. Same envelope shape as ONVIF-driven
		// alarms so the operator console doesn't have to special-case.
		if hub != nil {
			alertMsg, _ := json.Marshal(map[string]interface{}{
				"type": "alarm",
				"data": map[string]interface{}{
					"id":              alarm.ID,
					"site_id":         alarm.SiteID,
					"site_name":       alarm.SiteName,
					"camera_id":       alarm.CameraID,
					"camera_name":     alarm.CameraName,
					"severity":        alarm.Severity,
					"type":            alarm.Type,
					"description":     alarm.Description,
					"ts":              alarm.Ts,
					"acknowledged":    alarm.Acknowledged,
					"escalation_level": 0,
					"snapshot_url":    alarm.SnapshotURL,
				},
			})
			hub.Broadcast(alertMsg)
		}

		log.Printf("[SENSE] %s: %s alarm dispatched (%d-byte snapshot)", cam.Name, eventType, len(snapshot))
		writeJSON(w, map[string]string{"status": "ok", "alarm_id": alarmID})
	}
}

// sensePayload mirrors the JSON template Milesight's Alarm Server page
// renders for SC4xx Sense cameras. Field tags use the camera's exact
// snake_case names; the camera substitutes $var$ / #var# placeholders
// before sending so we receive concrete values.
type sensePayload struct {
	EventID             string  `json:"event_id"`
	EventType           string  `json:"event_type"`
	DeviceName          string  `json:"device_name"`
	MacAddress          string  `json:"mac_address"`
	SerialNumber        string  `json:"sn"`
	Latitude            string  `json:"latitude"`
	Longitude           string  `json:"longitude"`
	Altitude            string  `json:"altitude"`
	Time                string  `json:"time"`
	TimeMsec            string  `json:"time_msec"`
	RetransmitTime      string  `json:"retransmit_time"`
	RetransmitCount     int     `json:"retransmit_count"`
	DetectionRegion     int     `json:"detection_region"`
	DetectionRegionName string  `json:"detection_region_name"`
	ResolutionWidth     int     `json:"resolution_width"`
	ResolutionHeight    int     `json:"resolution_height"`
	CoordinateX1        float64 `json:"coordinate_x1"`
	CoordinateY1        float64 `json:"coordinate_y1"`
	CoordinateX2        float64 `json:"coordinate_x2"`
	CoordinateY2        float64 `json:"coordinate_y2"`
}

// parseSensePayload handles both encodings the camera might send:
//
//   - multipart/form-data with a JSON part + one or more file parts
//     (the Alarm Server "General + Binary" combo, which is the typical
//     factory default)
//   - application/json with a base64-encoded snapshot inline (when
//     "Encoding Type: Base64" is selected)
//
// Returns the parsed metadata and the first attached snapshot bytes
// (decoded if needed). Multiple snapshots-per-event are uncommon on
// SC411 — we keep only the first.
func parseSensePayload(r *http.Request) (*sensePayload, []byte, error) {
	ctype := r.Header.Get("Content-Type")
	if strings.HasPrefix(ctype, "multipart/") {
		// 16 MiB cap — SC411 captures are ~200 KB at default settings.
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			return nil, nil, fmt.Errorf("multipart: %w", err)
		}
		var p sensePayload
		// JSON metadata typically sits in a form field named "data" or
		// "metadata"; firmware varies. Try the common ones.
		for _, name := range []string{"data", "metadata", "json"} {
			if v := r.FormValue(name); v != "" {
				_ = json.Unmarshal([]byte(v), &p)
				break
			}
		}
		var snapshot []byte
		for _, parts := range r.MultipartForm.File {
			for _, fh := range parts {
				if fh.Size <= 0 {
					continue
				}
				f, err := fh.Open()
				if err != nil {
					continue
				}
				snapshot, _ = io.ReadAll(io.LimitReader(f, 16<<20))
				f.Close()
				if len(snapshot) > 0 {
					break
				}
			}
			if len(snapshot) > 0 {
				break
			}
		}
		return &p, snapshot, nil
	}

	// JSON body with optional inline base64 snapshot.
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	// Decode into the typed struct AND a generic map so we can pick up
	// the snapshot field — its key is "snapshot" or "snapshot_list" and
	// the value may be a base64 string or an array of strings.
	var p sensePayload
	_ = json.Unmarshal(body, &p)
	var raw map[string]interface{}
	_ = json.Unmarshal(body, &raw)
	snapshot := extractInlineSnapshot(raw)
	return &p, snapshot, nil
}

// extractInlineSnapshot pulls a base64 snapshot from the JSON body.
// The Milesight "snapshot": "[snapshot_list]" template substitutes
// either a single string or an array; we accept both.
func extractInlineSnapshot(raw map[string]interface{}) []byte {
	if raw == nil {
		return nil
	}
	pickString := func(v interface{}) string {
		switch x := v.(type) {
		case string:
			return x
		case []interface{}:
			if len(x) > 0 {
				if s, ok := x[0].(string); ok {
					return s
				}
			}
		}
		return ""
	}
	for _, key := range []string{"snapshot", "snapshot_list", "image", "picture"} {
		if v, ok := raw[key]; ok {
			s := pickString(v)
			if s == "" {
				continue
			}
			// Strip a possible "data:image/jpeg;base64," prefix.
			if i := strings.Index(s, ","); i >= 0 && strings.Contains(s[:i], "base64") {
				s = s[i+1:]
			}
			if b, err := base64Decode(s); err == nil && len(b) > 0 {
				return b
			}
		}
	}
	return nil
}

// base64Decode tries StdEncoding first, then RawStdEncoding (no padding)
// so we accept whatever the camera firmware emits.
func base64Decode(s string) ([]byte, error) {
	// Strip whitespace the camera firmware sometimes inserts.
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// mapSenseEventType normalises the camera's raw event_type label into
// the platform's existing event taxonomy (the same strings ONVIF
// events produce, so downstream alarm-rules and AI prompts treat
// pushed events identically). Anything we don't recognise falls
// through as the raw label, which the operator UI still renders.
func mapSenseEventType(raw string) string {
	r := strings.ToLower(raw)
	switch {
	case strings.Contains(r, "human"), strings.Contains(r, "person"):
		return "human"
	case strings.Contains(r, "vehicle"), strings.Contains(r, "car"):
		return "vehicle"
	case strings.Contains(r, "intrusion"):
		return "intrusion"
	case strings.Contains(r, "linecross"), strings.Contains(r, "tripwire"):
		return "linecross"
	case strings.Contains(r, "loiter"):
		return "loitering"
	case strings.Contains(r, "motion"), strings.Contains(r, "pir"):
		return "motion"
	}
	if raw == "" {
		return "motion"
	}
	return raw
}

// senseDescription renders a one-line operator-readable summary.
func senseDescription(eventType string, p *sensePayload) string {
	parts := []string{}
	parts = append(parts, fmt.Sprintf("PIR / %s detected", eventType))
	if p.DetectionRegionName != "" {
		parts = append(parts, "in "+p.DetectionRegionName)
	}
	if p.DeviceName != "" {
		parts = append(parts, "from "+p.DeviceName)
	}
	return strings.Join(parts, " ")
}
