package streaming

// StatusReconciler drives camera status from mediamtx /v3/paths/list
// instead of relying on boot-time ffprobe results.
//
// Background: PR #81 added a one-shot ffprobe in autoStartCameras to detect
// unreachable streams at boot (B-15). Cellular cameras are slow to settle at
// startup (network + mediamtx not yet fully connected), so the probe times out
// → camera is written "offline" → never re-checked → permanent false-offline.
//
// Fix: this goroutine polls mediamtx's /v3/paths/list every ~30s. For each
// non-deleted camera it checks whether the MAIN path (bare UUID) is present
// and ready (source connected + bytes flowing). A single transient miss does
// not flip status; only 2 consecutive misses (~60s of confirmed absence)
// trigger an "offline" write. If the mediamtx API call itself fails the whole
// cycle is skipped so a transient API hiccup can never mass-mark every camera
// offline.
//
// /v3/paths/list is reliable on deployed mediamtx v1.19 (the /v3/config/*
// family hangs, but /v3/paths/list always answers — see mediamtx_api.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

const (
	// reconcilerInterval is how often the reconciler polls mediamtx.
	reconcilerInterval = 30 * time.Second

	// reconcilerAPITimeout caps each /v3/paths/list GET. The endpoint is
	// local-LAN fast; 5s is generous while still bounding a wedged network.
	reconcilerAPITimeout = 5 * time.Second

	// reconcilerMissThreshold is the number of consecutive "not ready" polls
	// before a camera is written "offline". Two misses = ~60s debounce.
	reconcilerMissThreshold = 2
)

// pathsListItem is the per-path payload returned by mediamtx /v3/paths/list.
// We decode only the fields we need; mediamtx returns more and Go's
// json.Decoder ignores unknown keys by default.
type pathsListItem struct {
	Name      string `json:"name"`
	Ready     bool   `json:"ready"`
	// BytesReceived is cumulative since the path was created; we only use it
	// as a secondary "stream is alive" signal when Ready is false (some
	// mediamtx builds or source configs leave ready=false while data flows).
	BytesReceived int64 `json:"bytesReceived"`
}

type pathsListFull struct {
	Items []pathsListItem `json:"items"`
}

// PathsLister is a narrow interface satisfied by *MediaMTXServer.
// Extracted so the unit tests can inject a fake.
type PathsLister interface {
	// FetchPathsReady returns a set of path names whose main RTSP source is
	// considered healthy (ready==true OR bytesReceived>0 on the current poll).
	// Returns (nil, err) when the mediamtx API is unreachable — the
	// reconciler treats this as "skip the cycle, do nothing".
	FetchPathsReady(ctx context.Context) (map[string]bool, error)
}

// FetchPathsReady implements PathsLister on the real MediaMTXServer.
// It calls /v3/paths/list with a short timeout and returns a set of path
// names whose source is currently connected (ready=true or bytesReceived>0).
func (m *MediaMTXServer) FetchPathsReady(ctx context.Context) (map[string]bool, error) {
	base := m.apiBaseURL()
	if base == "" {
		return nil, fmt.Errorf("mediamtx API address not configured")
	}

	reqCtx, cancel := context.WithTimeout(ctx, reconcilerAPITimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", base+"/v3/paths/list", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("mediamtx /v3/paths/list returned %d: %s", resp.StatusCode, string(body))
	}

	var pl pathsListFull
	if err := json.NewDecoder(resp.Body).Decode(&pl); err != nil {
		return nil, fmt.Errorf("mediamtx /v3/paths/list decode: %w", err)
	}

	out := make(map[string]bool, len(pl.Items))
	for _, it := range pl.Items {
		if it.Ready || it.BytesReceived > 0 {
			out[it.Name] = true
		}
	}
	return out, nil
}

// CameraStatusStore is the subset of *database.DB the reconciler uses.
// Extracted for testability.
type CameraStatusStore interface {
	ListCameras(ctx context.Context) ([]database.Camera, error)
	UpdateCameraStatus(ctx context.Context, id uuid.UUID, status string) error
	ClearCameraStreamError(ctx context.Context, id uuid.UUID) error
	UpdateCameraStreamError(ctx context.Context, id uuid.UUID, streamErr string) error
}

// RunStatusReconciler starts a background goroutine that keeps camera status
// consistent with the live mediamtx paths list. It exits when ctx is canceled.
//
// Call this once from main, AFTER autoStartCameras has returned (so mediamtx
// has had a chance to register paths). The first tick is intentionally delayed
// by reconcilerInterval (not immediate) to let the system settle.
func RunStatusReconciler(ctx context.Context, db CameraStatusStore, mtx PathsLister) {
	go runReconcilerLoop(ctx, db, mtx)
}

func runReconcilerLoop(ctx context.Context, db CameraStatusStore, mtx PathsLister) {
	ticker := time.NewTicker(reconcilerInterval)
	defer ticker.Stop()

	// miss[cameraID] counts consecutive cycles where the main path was NOT
	// ready. Reset to 0 when the path is healthy.
	miss := make(map[uuid.UUID]int)

	log.Printf("[RECONCILER] camera status reconciler started (interval=%s, miss_threshold=%d)",
		reconcilerInterval, reconcilerMissThreshold)

	for {
		select {
		case <-ctx.Done():
			log.Println("[RECONCILER] context canceled, stopping")
			return
		case <-ticker.C:
			reconcileOnce(ctx, db, mtx, miss)
		}
	}
}

// reconcileOnce is the single-cycle body, split out so it can be called
// synchronously in tests.
func reconcileOnce(ctx context.Context, db CameraStatusStore, mtx PathsLister, miss map[uuid.UUID]int) {
	ready, err := mtx.FetchPathsReady(ctx)
	if err != nil {
		// API unreachable — skip cycle completely. Do NOT touch any status.
		log.Printf("[RECONCILER] mediamtx paths/list unavailable, skipping cycle: %v", err)
		return
	}

	cameras, err := db.ListCameras(ctx)
	if err != nil {
		log.Printf("[RECONCILER] failed to list cameras: %v", err)
		return
	}

	for _, cam := range cameras {
		mainPath := cam.ID.String()
		if ready[mainPath] {
			// Stream is healthy.
			miss[cam.ID] = 0
			if cam.Status != "online" {
				log.Printf("[RECONCILER] camera %s (%s) path ready — setting online", cam.Name, cam.ID)
				if err := db.ClearCameraStreamError(ctx, cam.ID); err != nil {
					log.Printf("[RECONCILER] ClearCameraStreamError %s: %v", cam.Name, err)
				}
			}
		} else {
			// Path absent or not ready.
			miss[cam.ID]++
			if miss[cam.ID] >= reconcilerMissThreshold {
				if cam.Status != "offline" {
					log.Printf("[RECONCILER] camera %s (%s) path not ready for %d cycles — setting offline",
						cam.Name, cam.ID, miss[cam.ID])
					if err := db.UpdateCameraStreamError(ctx, cam.ID, "no active stream (reconciler)"); err != nil {
						log.Printf("[RECONCILER] UpdateCameraStreamError %s: %v", cam.Name, err)
					}
				}
			} else {
				log.Printf("[RECONCILER] camera %s (%s) path not ready (miss %d/%d, debouncing)",
					cam.Name, cam.ID, miss[cam.ID], reconcilerMissThreshold)
			}
		}
	}
}
