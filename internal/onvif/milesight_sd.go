package onvif

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MilesightStorage mirrors the shape returned by the vendor's
// `/cgi-bin/operator/operator.cgi?action=get.storage.settings` endpoint.
// Only the SD fields we care about are mapped. Sizes are reported by the
// camera in kilobytes as *strings*.
type MilesightStorage struct {
	SDCardStatus     int    `json:"sdcardStatus"`     // 0=missing, 1=detected-but-uninit, 3=present/normal
	SDCardDiskStatus int    `json:"sdcardDiskStatus"` // 0=needs format, 1=normal, 2=checking, 4=damaged
	SDCardFullStatus int    `json:"sdcardFullStatus"`
	SDCardTotalSize  string `json:"sdcardTotalSize"`
	SDCardFreeSize   string `json:"sdcardFreeSize"`
	SDCardUseSize    string `json:"sdcardUseSize"`
	SDCardFileSystem int    `json:"sdcardFileSystem"`
	SDFormatTime     int    `json:"sdFormatTime"`
}

// Present returns true when the camera reports an inserted, mounted,
// non-damaged card with a non-zero capacity. Matches the healthy-check
// the Milesight web UI uses:
//
//	!!totalSize && sdcardStatus != 0 && sdcardDiskStatus ∉ {0, 4}
func (m MilesightStorage) Present() bool {
	total, _ := strconv.ParseInt(m.SDCardTotalSize, 10, 64)
	if total <= 0 {
		return false
	}
	if m.SDCardStatus == 0 {
		return false
	}
	if m.SDCardDiskStatus == 0 || m.SDCardDiskStatus == 4 {
		return false
	}
	return true
}

// GetMilesightStorage hits the vendor-specific CGI used by the camera's own
// web UI. Milesight firmware (CQ_63.x) doesn't implement ONVIF
// GetStorageConfigurations, so this is the fallback that actually works.
//
// Uses HTTP Digest auth — the same credentials as ONVIF.
func (c *Client) GetMilesightStorage(ctx context.Context) (MilesightStorage, error) {
	base := c.httpBase()
	if base == "" {
		return MilesightStorage{}, fmt.Errorf("cannot derive http base from %q", c.Address)
	}
	target := base + "/cgi-bin/operator/operator.cgi?action=get.storage.settings&format=json"

	body, err := c.digestGet(ctx, target)
	if err != nil {
		return MilesightStorage{}, err
	}

	var out MilesightStorage
	if err := json.Unmarshal(body, &out); err != nil {
		return MilesightStorage{}, fmt.Errorf("parse storage json: %w", err)
	}
	return out, nil
}

// httpBase returns the scheme://host[:port] portion of the configured
// ONVIF address, stripping the `/onvif/device_service` suffix that
// NewClient appends.
func (c *Client) httpBase() string {
	u, err := url.Parse(c.Address)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// digestGet performs a GET with HTTP Digest authentication. First request
// is sent unauthenticated to capture the WWW-Authenticate challenge, then
// retried with the computed Authorization header.
func (c *Client) digestGet(ctx context.Context, fullURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, err
	}
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
	authHeader := buildMilesightDigest(c.Username, c.Password, "GET", uri, challenge)

	req2, _ := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
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

func buildMilesightDigest(username, password, method, uri, challenge string) string {
	realm := digestField(challenge, "realm")
	nonce := digestField(challenge, "nonce")
	qop := digestField(challenge, "qop")

	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + uri)

	nc := "00000001"
	cnonce := md5hex(fmt.Sprintf("%d", time.Now().UnixNano()))[:16]

	var response string
	if strings.Contains(qop, "auth") {
		response = md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	} else {
		response = md5hex(ha1 + ":" + nonce + ":" + ha2)
	}
	h := fmt.Sprintf(
		`Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		username, realm, nonce, uri, response,
	)
	if strings.Contains(qop, "auth") {
		h += fmt.Sprintf(`, qop=auth, nc=%s, cnonce="%s"`, nc, cnonce)
	}
	return h
}

func digestField(challenge, field string) string {
	search := field + `="`
	idx := strings.Index(challenge, search)
	if idx < 0 {
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

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
