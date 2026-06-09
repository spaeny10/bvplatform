// P3-INFRA-06 (pivot): mediamtx native HLS proxy.
//
// /api/live/{cameraID}/* reverse-proxies to mediamtx's built-in HLS server
// at http://<MediaMTXHLSAddr>/<cameraID>_sub/<wildcard>.
//
// Why a custom proxy instead of httputil.NewSingleHostReverseProxy:
//   - We need per-request auth (CanAccessCamera) before forwarding.
//   - .m3u8 responses must be rewritten so segment URLs stay inside
//     /api/live/<cameraID>/ and inherit the session cookie auth.
//   - httputil.ReverseProxy's Director + ModifyResponse can do this but
//     the buffering and error-handling is cleaner written explicitly.

package api

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// HandleLiveProxy proxies /api/live/{cameraID}/* to mediamtx native HLS.
//
// Auth: route is inside the /api group which already runs RequireAuth +
// CSRFMiddleware. Session cookie is the authorization — no media tokens.
//
// Upstream URL: http://<MediaMTXHLSAddr>/<cameraID>_sub/<wildcard>
// where <wildcard> is the path after the cameraID (e.g. "index.m3u8",
// "seg0.mp4", "init.mp4").
//
// .m3u8 responses are buffered and rewritten: every bare URI line
// (non-comment) is replaced with /api/live/<cameraID>/<leaf> so segment
// fetches also traverse this auth handler.
func HandleLiveProxy(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		cameraIDStr := chi.URLParam(r, "cameraID")
		cameraID, err := uuid.Parse(cameraIDStr)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		ok, cErr := CanAccessCamera(r.Context(), db, claims, cameraID)
		if cErr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// chi wildcard ("*") captures everything after /api/live/{cameraID}/
		// including the leading slash. Strip any leading slash before forwarding.
		wildcard := chi.URLParam(r, "*")
		wildcard = strings.TrimPrefix(wildcard, "/")

		// Forward to mediamtx HLS: always use the sub-stream path.
		// Preserve the query string (mediamtx uses ?session= for playlist
		// session affinity — stripping it causes 401 on variant-playlist fetches).
		upstreamBase := fmt.Sprintf("http://%s/%s_sub", cfg.MediaMTXHLSAddr, cameraIDStr)
		upstreamURL := upstreamBase + "/" + wildcard
		if r.URL.RawQuery != "" {
			upstreamURL += "?" + r.URL.RawQuery
		}

		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
		if err != nil {
			log.Printf("[LIVE-PROXY] build request error cam=%s path=%s: %v", cameraIDStr, wildcard, err)
			http.Error(w, "proxy error", http.StatusBadGateway)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[LIVE-PROXY] upstream error cam=%s path=%s: %v", cameraIDStr, wildcard, err)
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Pass through non-200 from mediamtx (404 = stream not ready, etc.)
			w.WriteHeader(resp.StatusCode)
			return
		}

		ct := resp.Header.Get("Content-Type")

		// For m3u8 playlists: buffer, rewrite segment URIs, then serve.
		if strings.Contains(ct, "mpegurl") || strings.HasSuffix(strings.ToLower(wildcard), ".m3u8") {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				http.Error(w, "read error", http.StatusBadGateway)
				return
			}
			rewritten := rewriteLiveProxyPlaylist(body, cameraIDStr)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			w.Write(rewritten)
			return
		}

		// For binary segments (init.mp4, seg*.mp4): stream directly.
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		io.Copy(w, resp.Body)
	}
}

// rewriteLiveProxyPlaylist rewrites a mediamtx HLS playlist so that every
// segment URI points at /api/live/<cameraID>/<leaf> instead of a bare
// filename. This keeps all segment fetches behind the same auth handler.
//
// mediamtx fmp4 playlists use relative URIs (just the filename, no path
// prefix). We preserve comment lines and #EXT-X-MAP URI= attributes.
func rewriteLiveProxyPlaylist(body []byte, cameraID string) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			// Rewrite URI= attribute inside HLS tags (e.g. #EXT-X-MAP:URI="init.mp4")
			if strings.Contains(trimmed, `URI="`) {
				out.WriteString(rewriteLiveProxyAttributeURI(line, cameraID))
			} else {
				out.WriteString(line)
			}
			out.WriteByte('\n')
			continue
		}
		// Bare URI line: replace with our proxy path.
		// Preserve the query string (mediamtx ?session= param).
		// Strip any path prefix from mediamtx, keep only leaf + query.
		uri := trimmed
		query := ""
		if idx := strings.Index(uri, "?"); idx >= 0 {
			query = uri[idx:] // includes the "?"
			uri = uri[:idx]
		}
		leaf := uri
		if idx := strings.LastIndex(leaf, "/"); idx >= 0 {
			leaf = leaf[idx+1:]
		}
		out.WriteString("/api/live/" + cameraID + "/" + leaf + query)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// rewriteLiveProxyAttributeURI rewrites the URI="..." attribute in an HLS
// tag line so it points at /api/live/<cameraID>/<leaf>.
func rewriteLiveProxyAttributeURI(line, cameraID string) string {
	const marker = `URI="`
	idx := strings.Index(line, marker)
	if idx < 0 {
		return line
	}
	start := idx + len(marker)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return line
	}
	uri := line[start : start+end]
	leaf := uri
	if i := strings.LastIndex(leaf, "/"); i >= 0 {
		leaf = leaf[i+1:]
	}
	return line[:start] + "/api/live/" + cameraID + "/" + leaf + line[start+end:]
}
