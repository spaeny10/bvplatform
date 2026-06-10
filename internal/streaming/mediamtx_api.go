package streaming

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MediaMTX's HTTP control API lets us add/remove/modify paths at runtime
// without restarting the process or round-tripping config YAML through a
// shared volume. Docs: https://bluenviron.github.io/mediamtx/
//
// The API is enabled in the bootstrap YAML (api: true, apiAddress: :9997)
// written by writeConfig(). Once MediaMTX is up, these helpers talk to it
// directly over HTTP.

// Default request timeout. MediaMTX's API is local / LAN; 5 s is generous
// even under load and still keeps a wedged-network case from stalling
// camera add/remove indefinitely.
const apiTimeout = 5 * time.Second

// mtxAPIPath is the subset of MediaMTX's path config that we actually set.
// MediaMTX accepts unknown fields as JSON — we don't need to model its full
// schema, just the ones we care about.
type mtxAPIPath struct {
	Source         string `json:"source"`
	SourceOnDemand bool   `json:"sourceOnDemand"`
	RTSPTransport  string `json:"rtspTransport"`
}

// apiBaseURL normalises the configured MEDIAMTX_API_ADDR into a URL.
// Accepts either "host:port" or "http://host:port" so operators don't
// have to guess which form the env var wants.
func (m *MediaMTXServer) apiBaseURL() string {
	addr := m.cfg.MediaMTXAPIAddr
	if addr == "" {
		return ""
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		return "http://" + addr
	}
	return addr
}

// apiAddPath installs a named path on the running MediaMTX via its HTTP
// control API. Returns nil on success or a "path already exists" no-op.
// Any other error is surfaced so the caller can fall back to writeConfig
// and restart (embedded mode) or log and continue (external mode).
func (m *MediaMTXServer) apiAddPath(ctx context.Context, name string, path mtxAPIPath) error {
	base := m.apiBaseURL()
	if base == "" {
		return errors.New("mediamtx API address not configured")
	}
	body, _ := json.Marshal(path)

	// MediaMTX returns 400 "path already exists" when you POST an /add for
	// a name that's already configured. We treat that as success because
	// our AddStream is called idempotently from the startup-resume loop
	// (the in-memory map is rebuilt from the DB, so the same cameras get
	// re-registered on every boot).
	url := fmt.Sprintf("%s/v3/config/paths/add/%s", base, name)
	if err := m.doAPIPost(ctx, url, body); err != nil {
		if isAlreadyExists(err) {
			// Fall through — try a PATCH to update the config in case
			// the source URI drifted (e.g., camera re-assigned to a new
			// RTSP address). Harmless when values are identical.
			patchURL := fmt.Sprintf("%s/v3/config/paths/patch/%s", base, name)
			if perr := m.doAPIPatch(ctx, patchURL, body); perr != nil && !isAlreadyExists(perr) {
				return perr
			}
			return nil
		}
		return err
	}
	return nil
}

// apiRemovePath deletes a named path via the HTTP control API.
// A 404 ("path not found") is treated as success — the target state is
// "path doesn't exist", which it doesn't.
func (m *MediaMTXServer) apiRemovePath(ctx context.Context, name string) error {
	base := m.apiBaseURL()
	if base == "" {
		return errors.New("mediamtx API address not configured")
	}
	url := fmt.Sprintf("%s/v3/config/paths/delete/%s", base, name)
	if err := m.doAPIDelete(ctx, url); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// apiReady probes the control API and returns nil once MediaMTX is
// accepting calls. Used after Start() in embedded mode to know when the
// process has finished booting and the path-add calls below will land.
func (m *MediaMTXServer) apiReady(ctx context.Context) error {
	base := m.apiBaseURL()
	if base == "" {
		return errors.New("mediamtx API address not configured")
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", base+"/v3/paths/list", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.New("mediamtx API did not become ready within 15s")
}

// pathListResponse is the subset of mediamtx's /v3/paths/list payload we
// read. mediamtx returns more per item; we only need the names.
type pathListResponse struct {
	Items []struct {
		Name string `json:"name"`
	} `json:"items"`
}

// apiPathPresent reports whether a named path appears in the RUNNING
// mediamtx's ACTIVE path list (GET /v3/paths/list). We deliberately use the
// active list rather than /v3/config/paths/{get,list}: on the deployed
// mediamtx the /v3/config/* endpoints can hang (observed: 10s+ timeouts
// while AddStream's PATCH calls returned "context deadline exceeded"),
// whereas /v3/paths/list answers reliably — and a path appearing there is
// the signal that actually matters for BUG-4, because that's exactly what
// /api/live and /api/live2 read from when serving the stream.
//
// Returns (true, nil) when the path is active, (false, nil) when it isn't,
// and (false, err) on any transport/decoding error.
func (m *MediaMTXServer) apiPathPresent(ctx context.Context, name string) (bool, error) {
	base := m.apiBaseURL()
	if base == "" {
		return false, errors.New("mediamtx API address not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", base+"/v3/paths/list", nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return false, &apiError{status: resp.StatusCode, body: string(respBody)}
	}
	var list pathListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return false, err
	}
	for _, it := range list.Items {
		if it.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// EnsureStreamRegistered synchronously guarantees the RUNNING mediamtx has
// the camera's main path active, re-issuing apiAddPath and polling
// /v3/paths/list until it appears or `timeout` elapses.
//
// BUG-4: in external mode, AddStream pushes the paths to mediamtx's control
// API in a fire-and-forget goroutine and only logs failures. If that POST
// loses the race against mediamtx being ready (or fails transiently — the
// /v3/config/* control endpoints are observed to time out under load on the
// deployed instance), the create still succeeds but the running process
// never serves the path, so /api/live and /api/live2 sit on "Connecting"
// until someone bounces the mediamtx container. Re-issuing the add and
// confirming the path is live closes that gap.
//
// Scope note: we only block on the MAIN path. The "_sub" path is
// sourceOnDemand, so mediamtx doesn't instantiate it (and it won't show in
// /v3/paths/list) until a viewer first reads it — waiting on it here would
// always time out. apiAddPath is still re-issued for _sub so its config is
// installed; it comes up on first read. The main path is what proves the
// running mediamtx accepted the camera without a restart.
//
// Embedded mode (dev / single-container) is a no-op: there mediamtx is a
// child process whose config is regenerated from the in-memory map on every
// (re)start, so AddStream + the resume loop already cover it.
//
// Non-fatal by design: the caller (HandleCreateCamera / HandleUpdateCamera)
// should let the create succeed even if this times out — it returns the
// error for logging, but the camera row is already persisted and the next
// mediamtx (re)start recovers the path from the bootstrap YAML.
func (m *MediaMTXServer) EnsureStreamRegistered(ctx context.Context, cameraID uuid.UUID, timeout time.Duration) error {
	if m.cfg.MediaMTXEmbedded {
		return nil
	}

	m.mu.Lock()
	info, ok := m.streams[cameraID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("camera %s not registered in mediamtx stream map", cameraID)
	}

	mainName := cameraID.String()
	mainPath := mtxAPIPath{Source: info.rtspURI, SourceOnDemand: false, RTSPTransport: "tcp"}
	subName := cameraID.String() + "_sub"
	subPath := mtxAPIPath{Source: info.subStreamURI, SourceOnDemand: true, RTSPTransport: "tcp"}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		present, err := m.apiPathPresent(ctx, mainName)
		if err != nil {
			lastErr = err
		} else if present {
			return nil
		} else {
			// The running mediamtx isn't serving the path — (re)install it.
			// Also re-issue the sub path so its config lands; it instantiates
			// lazily on first read so we don't wait on it.
			if aerr := m.apiAddPath(ctx, mainName, mainPath); aerr != nil {
				lastErr = aerr
			}
			if info.subStreamURI != "" {
				if aerr := m.apiAddPath(ctx, subName, subPath); aerr != nil {
					lastErr = aerr
				}
			}
		}
		if !time.Now().Before(deadline) {
			if lastErr != nil {
				return fmt.Errorf("mediamtx path %s not confirmed within %s: %w", mainName, timeout, lastErr)
			}
			return fmt.Errorf("mediamtx path %s not confirmed within %s", mainName, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// ReplaceStreamSource forces the RUNNING mediamtx to pick up a NEW source for
// an existing camera whose path NAME has not changed (the path name is the
// camera UUID, so an rtsp_uri / sub_stream_uri change keeps the same name).
//
// Why EnsureStreamRegistered can't do this: it polls /v3/paths/list FIRST and
// returns immediately when the path is present BY NAME — but after a URI edit
// the stale path is still present under that UUID, so it sees present==true
// and never pushes the new source. /v3/paths/list reports name only; the
// source is never compared. So the only thing that would otherwise push the
// new source is AddStream's fire-and-forget PATCH against /v3/config/* — the
// same endpoint family observed to hang 10s+ on the deployed mediamtx.
//
// Approach: DELETE both paths (clears the stale source), re-ADD them with the
// current source from the stream map, then poll /v3/paths/list until the main
// path re-appears. Delete+add is preferred over PATCH because the runtime
// /v3/config/paths/patch endpoint hangs on the deployed instance.
//
// VERIFIED DEPLOYMENT REALITY (mediamtx v1.19.0 on the bob test stack,
// 2026-06): the ENTIRE /v3/config/* family — add, delete, patch, and even the
// read-only /v3/config/{global,paths}/get — hangs (HTTP 000 after a 12s curl
// timeout), while /v3/paths/list answers reliably. mediamtx also does NOT
// hot-reload its YAML on file change. So on this instance there is no reliable
// way to push a runtime source change at all: this function's delete will time
// out and it returns a clear "live view needs a mediamtx reload for the new
// URI" error, which the caller logs. The existing paths in /v3/paths/list got
// there from the bootstrap YAML at container start, not from the runtime API.
// We still ATTEMPT delete+add (a) so the fix works automatically if a future
// mediamtx restores the /v3/config/* endpoints, and (b) so EnsureStreamRegistered's
// present-by-name short-circuit can never silently keep a stale source — this
// path is source-aware (force delete+add) by construction.
//
// Like EnsureStreamRegistered: embedded mode is a no-op (config regenerates
// from the in-memory map on restart), and it only blocks on the MAIN path —
// the _sub path is sourceOnDemand and won't appear in /v3/paths/list until a
// viewer reads it, so we re-issue its add but don't wait on it.
//
// Non-fatal by design: returns the error for logging, but the camera row is
// already persisted (recording reads the DB directly, so recording is correct
// regardless). On failure the live mediamtx path needs a container reload to
// pick up the new URI — the caller logs that clearly.
func (m *MediaMTXServer) ReplaceStreamSource(ctx context.Context, cameraID uuid.UUID, timeout time.Duration) error {
	if m.cfg.MediaMTXEmbedded {
		return nil
	}

	m.mu.Lock()
	info, ok := m.streams[cameraID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("camera %s not registered in mediamtx stream map", cameraID)
	}

	mainName := cameraID.String()
	mainPath := mtxAPIPath{Source: info.rtspURI, SourceOnDemand: false, RTSPTransport: "tcp"}
	subName := cameraID.String() + "_sub"
	subPath := mtxAPIPath{Source: info.subStreamURI, SourceOnDemand: true, RTSPTransport: "tcp"}

	// Delete the stale path(s) first so the re-add installs the new source.
	// apiRemovePath treats 404 as success, so a not-yet-present path is fine.
	if derr := m.apiRemovePath(ctx, mainName); derr != nil {
		// If delete itself is unreliable on this mediamtx, surface it — the
		// caller logs that a restart may be needed. We still attempt the add
		// below in case the path was already gone.
		return fmt.Errorf("mediamtx delete of stale path %s failed (live view needs a mediamtx reload for the new URI): %w", mainName, derr)
	}
	if info.subStreamURI != "" {
		if derr := m.apiRemovePath(ctx, subName); derr != nil {
			return fmt.Errorf("mediamtx delete of stale sub path %s failed (live view needs a mediamtx reload for the new URI): %w", subName, derr)
		}
	}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		// Always re-issue the add — we just deleted the path, so it is not
		// present and we must (re)install it with the new source. This is the
		// key difference from EnsureStreamRegistered, which short-circuits on a
		// present-by-name path and would never push a changed source.
		if aerr := m.apiAddPath(ctx, mainName, mainPath); aerr != nil {
			lastErr = aerr
		}
		if info.subStreamURI != "" {
			if aerr := m.apiAddPath(ctx, subName, subPath); aerr != nil {
				lastErr = aerr
			}
		}

		present, err := m.apiPathPresent(ctx, mainName)
		if err != nil {
			lastErr = err
		} else if present {
			return nil
		}

		if !time.Now().Before(deadline) {
			if lastErr != nil {
				return fmt.Errorf("mediamtx path %s not re-confirmed within %s (live view needs a mediamtx reload for the new URI): %w", mainName, timeout, lastErr)
			}
			return fmt.Errorf("mediamtx path %s not re-confirmed within %s (live view needs a mediamtx reload for the new URI)", mainName, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// ── low-level helpers ──────────────────────────────────────────

type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("mediamtx API %d: %s", e.status, e.body)
}

func isAlreadyExists(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status == http.StatusBadRequest && strings.Contains(strings.ToLower(ae.body), "already")
	}
	return false
}

func isNotFound(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status == http.StatusNotFound
	}
	return false
}

func (m *MediaMTXServer) doAPIPost(ctx context.Context, url string, body []byte) error {
	return m.doAPIWithMethod(ctx, "POST", url, body)
}
func (m *MediaMTXServer) doAPIPatch(ctx context.Context, url string, body []byte) error {
	return m.doAPIWithMethod(ctx, "PATCH", url, body)
}
func (m *MediaMTXServer) doAPIDelete(ctx context.Context, url string) error {
	return m.doAPIWithMethod(ctx, "DELETE", url, nil)
}

func (m *MediaMTXServer) doAPIWithMethod(ctx context.Context, method, url string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return &apiError{status: resp.StatusCode, body: string(respBody)}
}
