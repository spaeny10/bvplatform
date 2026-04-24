package drivers

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ═══════════════════════════════════════════════════════════════
// Milesight VCA (Video Content Analytics) Push Client
//
// Pushes VCA rules to the camera via two methods:
//   1. Milesight dataloader.cgi API (proprietary, HTTP Digest Auth)
//   2. Fallback: logs the rule config for manual application
//
// The dataloader.cgi API format:
//   GET  /dataloader.cgi?action=getconfig&vca/intrusion
//   POST /dataloader.cgi?action=setconfig  (body: vca/intrusion.xxx=yyy)
//
// Authentication: HTTP Digest Auth (RFC 7616)
// ═══════════════════════════════════════════════════════════════

// Milesight dataloader.cgi config keys by rule type.
var milesightVCAConfigKeys = map[string]string{
	"intrusion":      "vca/intrusion",
	"linecross":      "vca/linecross",
	"regionentrance": "vca/regionentrance",
	"loitering":      "vca/loitering",
}

// SupportedRuleTypes returns the VCA rule types the Milesight driver can push.
func (d *MilesightDriver) SupportedRuleTypes() []string {
	return []string{"intrusion", "linecross", "regionentrance", "loitering"}
}

// PushVCARules sends VCA rules to the Milesight camera via its dataloader.cgi API.
// Format: POST /dataloader.cgi?action=setconfig with form-encoded body.
// Rules are grouped by type and pushed to the corresponding config key.
func (d *MilesightDriver) PushVCARules(ctx context.Context, cameraIP, username, password string, rules []VCARuleCompact) error {
	client := &milesightHTTPClient{
		baseURL:  "http://" + cameraIP,
		username: username,
		password: password,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Group rules by type
	byType := map[string][]VCARuleCompact{}
	for _, r := range rules {
		byType[r.RuleType] = append(byType[r.RuleType], r)
	}

	for ruleType, typeRules := range byType {
		configKey, ok := milesightVCAConfigKeys[ruleType]
		if !ok {
			log.Printf("[VCA-MS] Unknown rule type %q, skipping", ruleType)
			continue
		}

		// Build dataloader.cgi form body
		formBody := buildMilesightFormBody(configKey, ruleType, typeRules)

		log.Printf("[VCA-MS] Pushing %d %s rules to %s via dataloader.cgi (%d params)",
			len(typeRules), ruleType, cameraIP, len(strings.Split(formBody, "&")))

		if err := client.postForm(ctx, "/dataloader.cgi?action=setconfig", formBody); err != nil {
			return fmt.Errorf("push %s rules: %w", ruleType, err)
		}
	}

	// Disable rule types that have no rules
	for ruleType, configKey := range milesightVCAConfigKeys {
		if _, hasRules := byType[ruleType]; !hasRules {
			formBody := configKey + ".enable=0"
			if err := client.postForm(ctx, "/dataloader.cgi?action=setconfig", formBody); err != nil {
				log.Printf("[VCA-MS] Warning: failed to disable %s on %s: %v", ruleType, cameraIP, err)
			}
		}
	}

	return nil
}

// buildMilesightFormBody builds a dataloader.cgi form-encoded body for VCA rules.
// Milesight uses 0-10000 integer coordinates (we store 0.0-1.0 normalized).
//
// Example output for intrusion region 1:
//   vca/intrusion.enable=1&vca/intrusion.region1.enable=1&vca/intrusion.region1.name=Zone+1
//   &vca/intrusion.region1.sensitivity=50&vca/intrusion.region1.pointnum=4
//   &vca/intrusion.region1.point1x=1000&vca/intrusion.region1.point1y=2000&...
func buildMilesightFormBody(configKey, ruleType string, rules []VCARuleCompact) string {
	var params []string
	params = append(params, configKey+".enable=1")

	for i, rule := range rules {
		if !rule.Enabled {
			continue
		}
		regionIdx := i + 1
		prefix := fmt.Sprintf("%s.region%d", configKey, regionIdx)

		params = append(params, prefix+".enable=1")
		params = append(params, prefix+".name="+strings.ReplaceAll(rule.Name, " ", "+"))
		params = append(params, fmt.Sprintf("%s.sensitivity=%d", prefix, rule.Sensitivity))
		params = append(params, fmt.Sprintf("%s.pointnum=%d", prefix, len(rule.Region)))

		for j, pt := range rule.Region {
			ptIdx := j + 1
			params = append(params, fmt.Sprintf("%s.point%dx=%d", prefix, ptIdx, int(pt.X*10000)))
			params = append(params, fmt.Sprintf("%s.point%dy=%d", prefix, ptIdx, int(pt.Y*10000)))
		}

		// Rule-type-specific fields
		if ruleType == "linecross" {
			dirMap := map[string]string{"both": "0", "left_to_right": "1", "right_to_left": "2"}
			if v, ok := dirMap[rule.Direction]; ok {
				params = append(params, prefix+".direction="+v)
			}
		}
		if ruleType == "loitering" && rule.ThresholdSec > 0 {
			params = append(params, fmt.Sprintf("%s.duration=%d", prefix, rule.ThresholdSec))
		}
	}

	return strings.Join(params, "&")
}

// ── Milesight HTTP Client with Digest Auth ──

type milesightHTTPClient struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// postForm sends a POST with application/x-www-form-urlencoded body, handling Digest Auth.
func (c *milesightHTTPClient) postForm(ctx context.Context, path string, formBody string) error {
	return c.doDigest(ctx, "POST", path, "application/x-www-form-urlencoded", []byte(formBody))
}

// doDigest sends an HTTP request with Digest Auth handling.
// First tries without auth; if 401, retries with digest credentials.
func (c *milesightHTTPClient) doDigest(ctx context.Context, method, path, contentType string, body []byte) error {
	fullURL := c.baseURL + path

	// First request — will get 401 with WWW-Authenticate header
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connect to camera: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		if challenge == "" {
			return fmt.Errorf("camera returned 401 without WWW-Authenticate header")
		}

		authHeader := buildDigestAuth(c.username, c.password, method, path, challenge)

		req2, _ := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(body))
		req2.Header.Set("Content-Type", contentType)
		req2.Header.Set("Authorization", authHeader)

		resp2, err := c.http.Do(req2)
		if err != nil {
			return fmt.Errorf("digest auth request: %w", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode >= 400 {
			respBody, _ := io.ReadAll(resp2.Body)
			return fmt.Errorf("camera returned HTTP %d: %s", resp2.StatusCode, string(respBody))
		}
		return nil
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("camera returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// putJSON sends a PUT with JSON body via Digest Auth.
func (c *milesightHTTPClient) putJSON(ctx context.Context, path string, body []byte) error {
	return c.doDigest(ctx, "PUT", path, "application/json", body)
}

// buildDigestAuth constructs an HTTP Digest Authorization header from a 401 challenge.
func buildDigestAuth(username, password, method, uri, challenge string) string {
	// Parse challenge fields
	realm := extractDigestField(challenge, "realm")
	nonce := extractDigestField(challenge, "nonce")
	qop := extractDigestField(challenge, "qop")

	// Calculate digest
	ha1 := md5sum(username + ":" + realm + ":" + password)
	ha2 := md5sum(method + ":" + uri)

	nc := "00000001"
	cnonce := md5sum(fmt.Sprintf("%d", time.Now().UnixNano()))[:16]

	var response string
	if strings.Contains(qop, "auth") {
		response = md5sum(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	} else {
		response = md5sum(ha1 + ":" + nonce + ":" + ha2)
	}

	header := fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		username, realm, nonce, uri, response,
	)
	if strings.Contains(qop, "auth") {
		header += fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s"`, nc, cnonce)
	}
	return header
}

func extractDigestField(challenge, field string) string {
	search := field + `="`
	idx := strings.Index(challenge, search)
	if idx < 0 {
		// Try without quotes
		search = field + "="
		idx = strings.Index(challenge, search)
		if idx < 0 {
			return ""
		}
		start := idx + len(search)
		end := strings.IndexAny(challenge[start:], ", ")
		if end < 0 {
			return challenge[start:]
		}
		return challenge[start : start+end]
	}
	start := idx + len(search)
	end := strings.Index(challenge[start:], `"`)
	if end < 0 {
		return ""
	}
	return challenge[start : start+end]
}

func md5sum(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
