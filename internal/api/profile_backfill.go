package api

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"ironsight/internal/database"
	"ironsight/internal/onvif"
)

// LOCAL-02 — ONVIF profile-token startup backfill.
//
// Cameras added via SQL seed (or any path that bypasses POST /api/cameras)
// have `profile_token=""`. PTZ controls then silently fail because
// `getPTZClient` reads the empty token and submits ONVIF PTZ commands
// that the camera rejects with a generic SOAP fault.
//
// This backfill runs once at server startup. For every camera whose
// stored profile_token is empty, it re-runs the ONVIF GetProfiles flow
// the camera-add handler uses and writes the discovered token + URIs
// via UpdateCameraRTSP. Per-camera errors are logged and skipped so
// one offline camera doesn't block backfilling its siblings.
//
// Heuristic for picking the profile: prefer the one whose StreamURI
// matches the camera's currently-stored rtsp_uri (this guarantees we
// re-bind the SAME profile the operator originally selected at create
// time — important for PTZ since each profile has its own PTZ config).
// Fall back to the highest-resolution profile if no URI matches —
// matches the camera-add handler's default selection.
//
// The backfill is fail-open: if ONVIF discovery fails for any reason
// (camera offline, wrong credentials, network blip), the camera stays
// with an empty profile_token and PTZ continues to silently fail for
// it. The next server restart retries. There's no exponential backoff
// here because operator-visible "camera offline" already prompts
// remediation via the camera-edit UI.

// PerCameraTimeout caps any single camera's discovery sequence
// (Connect → GetCapabilities → GetProfiles → GetStreamURI). Matches
// the 60s budget used by the camera-add handler for the same reason —
// a cellular-connected camera can need up to 15s per ONVIF call.
const profileBackfillPerCameraTimeout = 60 * time.Second

// profileBackfillConcurrency caps how many cameras are discovered in
// parallel. Real fleets are small (<50 cameras) so a small worker pool
// drains the queue quickly without saturating the network or the
// camera-side ONVIF stacks (some Milesight firmware serializes ONVIF
// requests internally and chokes on parallel hits from the same client).
const profileBackfillConcurrency = 4

// BackfillProfileTokens iterates every camera in the DB whose
// profile_token is empty, runs ONVIF discovery against it, and writes
// the result back via UpdateCameraRTSP. Returns the count of (attempted,
// successfully backfilled) for the log line at the call site. Safe to
// call multiple times — already-populated cameras are skipped.
//
// The outer ctx bounds the whole backfill pass; per-camera work uses
// its own derived timeout so a single slow camera doesn't starve the
// rest. Cancelling the outer ctx mid-flight stops accepting new work
// but lets in-flight cameras finish.
func BackfillProfileTokens(ctx context.Context, db *database.DB) (attempted, backfilled int) {
	cameras, err := db.ListCameras(ctx)
	if err != nil {
		log.Printf("[BACKFILL] ListCameras failed: %v", err)
		return 0, 0
	}

	// Build the worklist first so we can log the planned size up front.
	worklist := make([]database.Camera, 0)
	for _, cam := range cameras {
		if cam.ProfileToken == "" && cam.OnvifAddress != "" {
			worklist = append(worklist, cam)
		}
	}
	if len(worklist) == 0 {
		log.Printf("[BACKFILL] no cameras need profile_token backfill")
		return 0, 0
	}
	log.Printf("[BACKFILL] starting profile_token backfill for %d camera(s)", len(worklist))

	var wg sync.WaitGroup
	var (
		mu          sync.Mutex
		successCount int
	)

	sem := make(chan struct{}, profileBackfillConcurrency)
	for i := range worklist {
		cam := worklist[i] // capture by value
		select {
		case <-ctx.Done():
			break
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if backfillOneCamera(ctx, db, cam) {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	log.Printf("[BACKFILL] profile_token backfill done: %d/%d succeeded", successCount, len(worklist))
	return len(worklist), successCount
}

// backfillOneCamera does the discovery + DB write for a single camera.
// Returns true on success (any code path that wrote a non-empty
// profile_token). Errors are logged but never bubbled — the caller
// continues with the next camera.
func backfillOneCamera(parent context.Context, db *database.DB, cam database.Camera) bool {
	ctx, cancel := context.WithTimeout(parent, profileBackfillPerCameraTimeout)
	defer cancel()

	client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
	info, err := client.Connect(ctx)
	if err != nil {
		log.Printf("[BACKFILL] %s (%s): connect failed: %v", cam.Name, cam.ID, err)
		return false
	}
	profiles, err := client.GetProfiles(ctx)
	if err != nil || len(profiles) == 0 {
		detail := "no profiles returned"
		if err != nil {
			detail = err.Error()
		}
		log.Printf("[BACKFILL] %s (%s): GetProfiles failed: %s", cam.Name, cam.ID, detail)
		return false
	}

	// Pick the profile to use. First preference: the one whose StreamURI
	// matches the camera's currently-stored rtsp_uri. This guarantees we
	// re-bind the same profile the operator originally selected at
	// create time — important for PTZ since each profile has its own
	// PTZ configuration token.
	var pick onvif.StreamProfile
	if cam.RTSPUri != "" {
		camPath := stripCreds(cam.RTSPUri)
		for _, p := range profiles {
			if p.StreamURI == "" {
				continue
			}
			if stripCreds(p.StreamURI) == camPath {
				pick = p
				break
			}
		}
	}
	// Fallback: highest-resolution profile (matches the camera-add
	// handler's generic selection). Skips any profile with empty
	// StreamURI since UpdateCameraRTSP would write garbage.
	if pick.Token == "" {
		maxRes := 0
		for _, p := range profiles {
			if p.StreamURI == "" {
				continue
			}
			res := p.Width * p.Height
			if res > maxRes {
				maxRes = res
				pick = p
			}
		}
	}
	if pick.Token == "" {
		log.Printf("[BACKFILL] %s (%s): no profile had a usable StreamURI", cam.Name, cam.ID)
		return false
	}

	// UpdateCameraRTSP also refreshes manufacturer/model/firmware from
	// the ONVIF Connect response. We preserve the camera's current
	// rtsp_uri and sub_stream_uri values rather than overwriting from
	// the freshly-discovered profile — the operator may have edited
	// the URI (e.g., switched main → sub for bandwidth) and we don't
	// want to silently revert that. The backfill's job is the
	// profile_token, not a URI re-sync.
	mainUri := cam.RTSPUri
	subUri := cam.SubStreamUri
	manufacturer := cam.Manufacturer
	if manufacturer == "" {
		manufacturer = info.Manufacturer
	}
	model := cam.Model
	if model == "" {
		model = info.Model
	}
	firmware := cam.Firmware
	if firmware == "" {
		firmware = info.FirmwareVersion
	}

	if err := db.UpdateCameraRTSP(ctx, cam.ID, mainUri, subUri, pick.Token, manufacturer, model, firmware); err != nil {
		log.Printf("[BACKFILL] %s (%s): UpdateCameraRTSP failed: %v", cam.Name, cam.ID, err)
		return false
	}
	log.Printf("[BACKFILL] %s (%s): profile_token=%s", cam.Name, cam.ID, pick.Token)
	return true
}

// stripCreds returns the RTSP URI with any embedded user:pass@ removed
// so two URIs can be compared structurally. Cameras hand back
// credentialless StreamURIs from GetStreamURI; the DB-stored rtsp_uri
// has credentials injected at create time. Comparing the raw strings
// would never match. Only the @ in the userinfo section (before the
// first / of the path) is treated as a separator — a @ inside the
// path is left alone.
func stripCreds(u string) string {
	const sep = "://"
	i := strings.Index(u, sep)
	if i < 0 {
		return u
	}
	scheme := u[:i+len(sep)]
	rest := u[i+len(sep):]
	// Limit the @ search to the authority section (everything up to
	// the first / of the path). A @ that appears later is part of
	// the path and must not be treated as a userinfo separator.
	authorityEnd := strings.Index(rest, "/")
	if authorityEnd < 0 {
		authorityEnd = len(rest)
	}
	if at := strings.Index(rest[:authorityEnd], "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return scheme + rest
}
