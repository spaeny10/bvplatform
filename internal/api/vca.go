package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/drivers"
	"ironsight/internal/onvif"
)

// HandleListVCARules returns all VCA rules for a camera.
// GET /api/cameras/{id}/vca/rules
func HandleListVCARules(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		camID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		rules, err := db.ListVCARules(r.Context(), camID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if rules == nil {
			rules = []database.VCARule{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rules)
	}
}

// HandleCreateVCARule creates a new VCA zone/line rule.
// POST /api/cameras/{id}/vca/rules
func HandleCreateVCARule(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		camID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		var input database.VCARuleCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		validTypes := map[string]bool{"intrusion": true, "linecross": true, "regionentrance": true, "loitering": true}
		if !validTypes[input.RuleType] {
			http.Error(w, "invalid rule_type — must be intrusion, linecross, regionentrance, or loitering", http.StatusBadRequest)
			return
		}
		if len(input.Region) < 2 {
			http.Error(w, "region must have at least 2 points", http.StatusBadRequest)
			return
		}

		rule, err := db.CreateVCARule(r.Context(), camID, &input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(rule)
	}
}

// HandleUpdateVCARule updates an existing VCA rule.
// PUT /api/cameras/{id}/vca/rules/{ruleId}
func HandleUpdateVCARule(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
		if err != nil {
			http.Error(w, "invalid rule id", http.StatusBadRequest)
			return
		}
		var input database.VCARuleCreate
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := db.UpdateVCARule(r.Context(), ruleID, &input); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleDeleteVCARule deletes a VCA rule.
// DELETE /api/cameras/{id}/vca/rules/{ruleId}
func HandleDeleteVCARule(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
		if err != nil {
			http.Error(w, "invalid rule id", http.StatusBadRequest)
			return
		}
		if err := db.DeleteVCARule(r.Context(), ruleID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleSyncVCARules pushes all VCA rules for a camera to the camera firmware
// via the manufacturer driver (Milesight HTTP API, etc.).
// POST /api/cameras/{id}/vca/sync
func HandleSyncVCARules(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		camID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}

		cam, err := db.GetCamera(r.Context(), camID)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		// Check if driver supports VCA
		info := &onvif.DeviceInfo{Manufacturer: cam.Manufacturer, Model: cam.Model}
		drv := drivers.ForDevice(info)
		if drv == nil {
			http.Error(w, "no driver found for this camera", http.StatusBadRequest)
			return
		}
		vcaDrv, ok := drv.(drivers.VCACapable)
		if !ok {
			http.Error(w, "camera driver does not support VCA rule sync", http.StatusBadRequest)
			return
		}

		rules, err := db.ListVCARules(r.Context(), camID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Extract camera IP from ONVIF address
		cameraIP := cam.OnvifAddress
		if idx := strings.Index(cameraIP, "/onvif"); idx > 0 {
			cameraIP = cameraIP[:idx]
		}
		cameraIP = strings.TrimPrefix(cameraIP, "http://")
		cameraIP = strings.TrimPrefix(cameraIP, "https://")
		if colonIdx := strings.LastIndex(cameraIP, ":"); colonIdx > 0 {
			// Keep host only if port is 80 (ONVIF default), otherwise keep host:port
			port := cameraIP[colonIdx+1:]
			if port == "80" || port == "" {
				cameraIP = cameraIP[:colonIdx]
			}
		}

		syncCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// Convert DB rules to driver-compact format
		compact := make([]drivers.VCARuleCompact, len(rules))
		for i, rule := range rules {
			region := make([]struct{ X, Y float64 }, len(rule.Region))
			for j, pt := range rule.Region {
				region[j] = struct{ X, Y float64 }{pt.X, pt.Y}
			}
			compact[i] = drivers.VCARuleCompact{
				RuleType:     rule.RuleType,
				Name:         rule.Name,
				Enabled:      rule.Enabled,
				Sensitivity:  rule.Sensitivity,
				Region:       region,
				Direction:    rule.Direction,
				ThresholdSec: rule.ThresholdSec,
			}
		}
		err = vcaDrv.PushVCARules(syncCtx, cameraIP, cam.Username, cam.Password, compact)

		synced := 0
		errors := 0
		if err != nil {
			log.Printf("[VCA] Sync failed for camera %s: %v", cam.Name, err)
			for _, rule := range rules {
				db.UpdateVCARuleSync(r.Context(), rule.ID, false, err.Error())
			}
			errors = len(rules)
		} else {
			log.Printf("[VCA] Synced %d rules to camera %s via %s driver", len(rules), cam.Name, drv.Name())
			for _, rule := range rules {
				db.UpdateVCARuleSync(r.Context(), rule.ID, true, "")
			}
			synced = len(rules)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"synced": synced, "errors": errors})
	}
}

// HandleVCASnapshot returns a fresh JPEG snapshot from the camera for zone drawing.
// Strategy: FFmpeg RTSP frame grab (fast, works for any streaming camera) → ONVIF snapshot fallback.
// GET /api/cameras/{id}/vca/snapshot
func HandleVCASnapshot(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		camID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}

		cam, err := db.GetCamera(r.Context(), camID)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		// Attempt 1: FFmpeg RTSP frame grab (most reliable)
		if cam.RTSPUri != "" {
			jpeg, err := ffmpegFrameGrab(cam.RTSPUri)
			if err == nil && len(jpeg) > 0 {
				log.Printf("[VCA] Snapshot via FFmpeg RTSP for %s (%d bytes)", cam.Name, len(jpeg))
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Cache-Control", "no-cache")
				w.Write(jpeg)
				return
			}
			log.Printf("[VCA] FFmpeg snapshot failed for %s: %v, trying ONVIF", cam.Name, err)
		}

		// Attempt 2: ONVIF snapshot
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		if _, err := client.Connect(ctx); err != nil {
			http.Error(w, "failed to connect to camera: "+err.Error(), http.StatusBadGateway)
			return
		}

		profileToken := cam.ProfileToken
		if profileToken == "" {
			profiles, perr := client.GetProfiles(ctx)
			if perr != nil || len(profiles) == 0 {
				http.Error(w, "failed to get camera profiles", http.StatusBadGateway)
				return
			}
			profileToken = profiles[0].Token
		}

		snapURI, err := client.GetSnapshotURI(ctx, profileToken)
		if err != nil {
			http.Error(w, "failed to get snapshot URI: "+err.Error(), http.StatusBadGateway)
			return
		}

		jpeg, err := client.FetchSnapshot(ctx, snapURI)
		if err != nil {
			http.Error(w, "failed to fetch snapshot: "+err.Error(), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(jpeg)
	}
}

// ffmpegFrameGrab captures a single JPEG frame from an RTSP stream using FFmpeg.
// Timeout is generous because cellular/WAN cameras can take several seconds
// to negotiate RTSP and produce a first I-frame. We don't run this on a hot
// path — it fires on demand when the operator opens the VCA zone editor.
func ffmpegFrameGrab(rtspURI string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-rtsp_transport", "tcp",
		"-i", rtspURI,
		"-frames:v", "1",
		"-q:v", "2",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Pull the last line of FFmpeg's stderr for the caller's log —
		// otherwise "exit status 1" tells us nothing.
		tail := stderr.String()
		if i := strings.LastIndex(strings.TrimRight(tail, "\n"), "\n"); i >= 0 {
			tail = tail[i+1:]
		}
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(tail))
	}
	return stdout.Bytes(), nil
}
