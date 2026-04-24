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
