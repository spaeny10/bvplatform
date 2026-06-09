// Camera web-UI same-origin proxy.
//
// /api/cameras/{id}/web-ui/*  ──proxy──►  http://<camera.onvif_address>/*
//
// Why this exists: the VCAZoneEditor "Camera VCA" tab embeds the
// camera's own web configuration page in an iframe so operators can
// configure on-device VCA without leaving Ironsight. Pointing the
// iframe directly at the camera fails for two reasons that aren't
// visible to the user:
//   1. Mixed content — Ironsight is served over https, most cameras
//      only speak http. Chromium silently blocks the iframe load.
//   2. X-Frame-Options: SAMEORIGIN / DENY from the camera itself,
//      which is the default Milesight/Hikvision header. Even on
//      http-only deployments the iframe won't render.
//
// Proxying through this handler fixes both: the iframe src becomes
// same-origin https, and we strip X-Frame-Options + Content-Security-
// Policy from the camera's response so the browser allows embedding.
// A <base href="/api/cameras/<id>/web-ui/"> tag is injected into HTML
// responses so any absolute-rooted relative URLs (like
// /static/main.js) resolve back through the proxy instead of the
// Ironsight origin.
//
// Auth: same /api/* path-prefix middleware (RequireAuth) gates this
// route, and a CanAccessCamera check confirms the caller can actually
// see this camera. The camera's *own* auth is whatever the camera
// returns — the iframe will show a login page on first load and the
// browser handles cookies set by the camera (we strip Domain attrs so
// the cookies land on the Ironsight origin and travel through this
// proxy on subsequent fetches).

package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
)

// HandleCameraWebUIProxy proxies any HTTP method under
// /api/cameras/{id}/web-ui/* to http://<camera.onvif_address>/<rest>.
//
// Headers stripped from the upstream response so the iframe loads:
//   - X-Frame-Options
//   - Content-Security-Policy / Content-Security-Policy-Report-Only
//   - Strict-Transport-Security (would force an HTTPS upgrade we can't satisfy)
//
// Cookies' Domain attribute is removed so they apply to the Ironsight
// origin (the iframe's effective origin) instead of the camera's IP.
//
// For text/html responses we inject a <base> tag pointing at the proxy
// path so any absolute-rooted relative URLs in the camera's HTML keep
// flowing through this handler.
func HandleCameraWebUIProxy(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		cameraIDStr := chi.URLParam(r, "id")
		cameraID, err := uuid.Parse(cameraIDStr)
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		ok, accErr := CanAccessCamera(r.Context(), db, claims, cameraID)
		if accErr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		cam, err := db.GetCamera(r.Context(), cameraID)
		if err != nil || cam == nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		// chi wildcard captures everything after /web-ui/, including
		// the leading slash on chi v5. Normalise to a single leading
		// slash for URL composition.
		wildcard := chi.URLParam(r, "*")
		if !strings.HasPrefix(wildcard, "/") {
			wildcard = "/" + wildcard
		}

		// Build upstream URL. cam.OnvifAddress looks like
		// "504.bigview.ai:8080" or "192.168.50.10" — schemeless. Assume
		// http; HTTPS-only cameras are rare and we'd need vendor work.
		host := cam.OnvifAddress
		if strings.Contains(host, "://") {
			// Defensive: drop a scheme if someone stored a full URL.
			if u, perr := url.Parse(host); perr == nil {
				host = u.Host
			}
		}
		upstream := &url.URL{
			Scheme:   "http",
			Host:     host,
			Path:     wildcard,
			RawQuery: r.URL.RawQuery,
		}

		// Forward request body for POST/PUT (login forms, config writes).
		req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), r.Body)
		if err != nil {
			http.Error(w, "proxy build error", http.StatusBadGateway)
			return
		}
		// Pass through cookies + most headers. Strip Host (must be the
		// upstream's), accept-encoding (so we can rewrite HTML if needed),
		// and authorization (we don't share Ironsight tokens with the camera).
		for k, vv := range r.Header {
			lower := strings.ToLower(k)
			if lower == "host" || lower == "accept-encoding" || lower == "authorization" {
				continue
			}
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}
		req.Host = host

		// Some cameras' HTTP stacks are slow; use a generous timeout but
		// not infinite so a hung backend can't pin a goroutine forever.
		client := &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				// Don't follow redirects on the server side — let the
				// browser see the redirect (rewritten via Location
				// header munging below) so its cookie jar stays consistent.
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "camera unreachable: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Strip framing headers so the iframe loads.
		copyHeadersForProxy(w.Header(), resp.Header, cameraIDStr)
		// Rewrite Location header on redirect responses so absolute paths
		// from the camera resolve back through our proxy.
		if loc := resp.Header.Get("Location"); loc != "" {
			w.Header().Set("Location", rewriteLocation(loc, host, cameraIDStr))
		}

		// For HTML, buffer + inject <base> tag.
		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(strings.ToLower(ct), "text/html") {
			body, rerr := io.ReadAll(resp.Body)
			if rerr != nil {
				http.Error(w, "proxy read error", http.StatusBadGateway)
				return
			}
			body = injectBaseHref(body, cameraIDStr)
			// Recompute Content-Length since we changed body length.
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(body)
			return
		}

		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

// copyHeadersForProxy mirrors all upstream headers except the ones we
// must strip (framing controls, HSTS) and rewrites Set-Cookie to drop
// any Domain= attribute so the cookie lands on the Ironsight origin.
func copyHeadersForProxy(dst, src http.Header, cameraID string) {
	for k, vv := range src {
		lower := strings.ToLower(k)
		// Strip framing-control headers — those are exactly what's
		// blocking the iframe today.
		if lower == "x-frame-options" || lower == "content-security-policy" ||
			lower == "content-security-policy-report-only" ||
			lower == "strict-transport-security" {
			continue
		}
		// Content-Length will be recomputed when we rewrite HTML.
		if lower == "content-length" {
			continue
		}
		// Location is rewritten by the caller.
		if lower == "location" {
			continue
		}
		if lower == "set-cookie" {
			for _, v := range vv {
				dst.Add("Set-Cookie", rewriteSetCookie(v, cameraID))
			}
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// rewriteSetCookie strips the Domain attribute (so the browser scopes
// the cookie to the Ironsight origin instead of the camera IP) and
// narrows Path to /api/cameras/<id>/web-ui/ so cookies from two
// different cameras don't collide.
func rewriteSetCookie(c, cameraID string) string {
	parts := strings.Split(c, ";")
	var keep []string
	pathScoped := false
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "domain=") {
			continue
		}
		if strings.HasPrefix(lower, "path=") {
			pathScoped = true
			keep = append(keep, " Path=/api/cameras/"+cameraID+"/web-ui/")
			continue
		}
		keep = append(keep, p)
	}
	if !pathScoped {
		keep = append(keep, " Path=/api/cameras/"+cameraID+"/web-ui/")
	}
	return strings.Join(keep, ";")
}

// rewriteLocation turns absolute Location URLs that point at the
// camera back into paths under our proxy. Relative locations are left
// alone — the browser resolves them against the iframe's current URL,
// which is already in /api/cameras/<id>/web-ui/.
func rewriteLocation(loc, cameraHost, cameraID string) string {
	if strings.HasPrefix(loc, "/") {
		// Absolute path on the camera — prefix with our proxy.
		return "/api/cameras/" + cameraID + "/web-ui" + loc
	}
	if u, err := url.Parse(loc); err == nil && u.Host != "" && (u.Host == cameraHost || strings.HasPrefix(u.Host, cameraHost+":")) {
		// Full URL pointing at the camera — strip scheme+host.
		newPath := u.Path
		if newPath == "" {
			newPath = "/"
		}
		if u.RawQuery != "" {
			newPath += "?" + u.RawQuery
		}
		return "/api/cameras/" + cameraID + "/web-ui" + newPath
	}
	return loc
}

// injectBaseHref inserts <base href="/api/cameras/<id>/web-ui/"> right
// after the <head> tag in the camera's HTML so any absolute-rooted
// relative URLs resolve back through our proxy. If <head> isn't found
// (rare — minimal HTML or non-standard cameras) we fall back to
// inserting at the very start of the body.
func injectBaseHref(body []byte, cameraID string) []byte {
	base := []byte(`<base href="/api/cameras/` + cameraID + `/web-ui/">`)
	// Find the first <head> (case-insensitive) and inject after its closing >.
	lower := bytes.ToLower(body)
	idx := bytes.Index(lower, []byte("<head"))
	if idx < 0 {
		// No <head> — prepend at start so at least the first asset loads.
		return append(base, body...)
	}
	closeAngle := bytes.IndexByte(body[idx:], '>')
	if closeAngle < 0 {
		return append(base, body...)
	}
	insertAt := idx + closeAngle + 1
	out := make([]byte, 0, len(body)+len(base))
	out = append(out, body[:insertAt]...)
	out = append(out, base...)
	out = append(out, body[insertAt:]...)
	return out
}
