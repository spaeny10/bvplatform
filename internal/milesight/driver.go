// Package milesight implements a camera driver for Milesight IP cameras.
// It provides HTTP Digest-authenticated access to configuration, snapshots,
// and real-time event streaming via the proprietary /webstream/track WebSocket.
package milesight

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Camera is the main Milesight camera driver.
type Camera struct {
	Host     string
	User     string
	Password string

	baseURL string
	mu      sync.Mutex
	realm   string
	nonce   string
	nc      int
}

// New creates a new Milesight camera driver.
func New(host, user, password string) *Camera {
	return &Camera{
		Host:     host,
		User:     user,
		Password: password,
		baseURL:  "http://" + host,
	}
}

// ── Digest Auth ──

func (c *Camera) refreshChallenge() error {
	resp, err := http.Get(c.baseURL + "/vb.htm?onlineheartbeat")
	if err != nil {
		return fmt.Errorf("challenge request: %w", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	auth := resp.Header.Get("WWW-Authenticate")
	if auth == "" && resp.StatusCode == 200 {
		return nil // already authed or no auth needed
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, part := range strings.Split(auth, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "Digest ")
		if strings.HasPrefix(part, "realm=") {
			c.realm = strings.Trim(strings.TrimPrefix(part, "realm="), "\"")
		}
		if strings.HasPrefix(part, "nonce=") {
			c.nonce = strings.Trim(strings.TrimPrefix(part, "nonce="), "\"")
		}
	}
	if c.realm == "" || c.nonce == "" {
		return fmt.Errorf("could not parse Digest challenge from: %s", auth)
	}
	return nil
}

func (c *Camera) digestHeader(method, uri string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nc++
	nc := fmt.Sprintf("%08x", c.nc)
	cnonce := fmt.Sprintf("%08x", rand.Intn(100000000))

	ha1 := md5hex(c.User + ":" + c.realm + ":" + c.Password)
	ha2 := md5hex(method + ":" + uri)
	response := md5hex(ha1 + ":" + c.nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)

	return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s", qop=auth, nc=%s, cnonce="%s"`,
		c.User, c.realm, c.nonce, uri, response, nc, cnonce)
}

func (c *Camera) authedGet(path string) (*http.Response, error) {
	if c.nonce == "" {
		if err := c.refreshChallenge(); err != nil {
			return nil, err
		}
	}
	req, _ := http.NewRequest("GET", c.baseURL+path, nil)
	req.Header.Set("Authorization", c.digestHeader("GET", path))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	// If 401, refresh challenge and retry once
	if resp.StatusCode == 401 {
		resp.Body.Close()
		if err := c.refreshChallenge(); err != nil {
			return nil, err
		}
		req, _ = http.NewRequest("GET", c.baseURL+path, nil)
		req.Header.Set("Authorization", c.digestHeader("GET", path))
		return client.Do(req)
	}
	return resp, nil
}

// ── Config ──

// GetConfig fetches camera parameters via /vb.htm.
// Returns a map of key=value pairs from the "OK key=value;" response lines.
func (c *Camera) GetConfig(params ...string) (map[string]string, error) {
	qs := strings.Join(params, "&")
	resp, err := c.authedGet("/vb.htm?" + qs)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	result := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "OK ") {
			kv := strings.TrimSuffix(strings.TrimPrefix(line, "OK "), ";")
			if idx := strings.Index(kv, "="); idx > 0 {
				result[kv[:idx]] = kv[idx+1:]
			}
		}
	}
	return result, nil
}

// ── Snapshot ──

// Snapshot captures a JPEG frame from the camera.
func (c *Camera) Snapshot() ([]byte, error) {
	resp, err := c.authedGet("/snapshot.cgi")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ── RTSP URL ──

// RTSPURL returns the RTSP stream URL for the given stream name.
func (c *Camera) RTSPURL(stream string) string {
	return fmt.Sprintf("rtsp://%s:%s@%s:554/%s", c.User, c.Password, c.Host, stream)
}

// ── Heartbeat ──

// Heartbeat sends periodic keepalive requests. Blocks until stop is closed.
func (c *Camera) Heartbeat(stop <-chan struct{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.GetConfig("onlineheartbeat")
		}
	}
}

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
