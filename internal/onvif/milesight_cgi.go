package onvif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Milesight's web UI drives nearly all camera configuration through two
// parallel CGI paths backed by the same dot-notation action vocabulary:
//
//	GET  /cgi-bin/operator/operator.cgi?action=get.X.Y&format=json
//	POST /cgi-bin/admin/admin.cgi?action=set.X.Y&format=json   (JSON body)
//
// This file exposes thin generic helpers on *Client for both directions.
// Callers supply the full dot-action ("get.camera.setting" / "set.audio")
// and either unmarshal the returned raw JSON themselves or POST a request
// body that the camera echoes with {"setState": "succeed"}.
//
// We keep the digest-auth retry loop from milesight_sd.go and reuse the
// same credentials the ONVIF client already holds.

// MilesightGet issues a typed GET against operator.cgi. The action string
// is inserted verbatim — callers are responsible for validating it against
// an allowlist before calling (see internal/api/milesight_config.go).
func (c *Client) MilesightGet(ctx context.Context, action string) ([]byte, error) {
	base := c.httpBase()
	if base == "" {
		return nil, fmt.Errorf("cannot derive http base from %q", c.Address)
	}
	target := base + "/cgi-bin/operator/operator.cgi?action=" + url.QueryEscape(action) + "&format=json"
	return c.digestGet(ctx, target)
}

// MilesightPost issues a JSON POST to admin.cgi. Body is marshalled from
// `body`. Returns the camera's raw response (typically `{"setState":"succeed"}`
// or `{"setState":"failed", ...}`) so the caller can decide whether to
// treat "failed" as an error.
func (c *Client) MilesightPost(ctx context.Context, action string, body any) ([]byte, error) {
	base := c.httpBase()
	if base == "" {
		return nil, fmt.Errorf("cannot derive http base from %q", c.Address)
	}
	target := base + "/cgi-bin/admin/admin.cgi?action=" + url.QueryEscape(action) + "&format=json"

	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		payload = b
	} else {
		payload = []byte("{}")
	}
	return c.digestJSONPost(ctx, target, payload)
}

// MilesightCheckSetState parses `{"setState":"succeed"}` / `"failed"` and
// returns a non-nil error on failure.
func MilesightCheckSetState(raw []byte) error {
	var wrap struct {
		SetState string `json:"setState"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		// Some actions return empty body on success (204-ish). Treat
		// unparseable as success so we don't flag legitimate 200s.
		return nil
	}
	if wrap.SetState == "failed" {
		if wrap.Message != "" {
			return fmt.Errorf("camera rejected action: %s", wrap.Message)
		}
		return fmt.Errorf("camera rejected action")
	}
	return nil
}

// digestJSONPost performs a JSON POST with digest auth. Mirrors digestGet
// in milesight_sd.go but adds Content-Type and a body reader that can be
// replayed for the second (authed) request.
func (c *Client) digestJSONPost(ctx context.Context, fullURL string, payload []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || challenge == "" {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	u, err := url.Parse(fullURL)
	if err != nil {
		return nil, err
	}
	uri := u.RequestURI()
	authHeader := buildMilesightDigest(c.Username, c.Password, "POST", uri, challenge)

	req2, _ := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(payload))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", authHeader)
	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("digest retry: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 400 {
		return nil, fmt.Errorf("camera returned HTTP %d", resp2.StatusCode)
	}
	return io.ReadAll(resp2.Body)
}
