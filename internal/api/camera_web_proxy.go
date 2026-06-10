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
// HTML responses get two rewrites so the camera's assets keep flowing
// through the proxy:
//   - document-relative URLs ("img/logo.png"): a <base href> tag pinned
//     to the proxy root handles these (see injectBaseHref);
//   - root-relative URLs ("/static/main.js"): <base> CANNOT reroute
//     these (RFC 3986 §5.3 — a path-absolute reference replaces the
//     base path entirely), so rewriteRootRelativeURLs prefixes them
//     with the proxy path in the HTML body itself.
//
// Auth: same /api/* path-prefix middleware (RequireAuth) gates this
// route, and a CanAccessCamera check confirms the caller can actually
// see this camera. The camera's *own* auth is whatever the camera
// returns — the iframe will show a login page on first load and the
// browser handles cookies set by the camera (we strip Domain attrs so
// the cookies land on the Ironsight origin and travel through this
// proxy on subsequent fetches). Note that path-scoping camera cookies
// under /api/cameras/<id>/web-ui/ avoids name collisions between
// cameras but is NOT a security boundary — content served through this
// proxy is same-origin with the rest of the Ironsight app.

package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/config"
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
// For text/html responses we rewrite root-relative URLs to the proxy
// prefix and inject a <base> tag (for document-relative URLs) so the
// camera's assets keep flowing through this handler.
func HandleCameraWebUIProxy(cfg *config.Config, db *database.DB) http.HandlerFunc {
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

		// SSRF guard (F-13): onvif_address is admin/supervisor-controlled
		// (HandleCreateCamera / HandleUpdateCamera are role-gated), but a
		// bad row must still not be able to point this proxy at the API
		// host's own services (AI sidecars on 127.0.0.1:8501/:8502,
		// /metrics, mediamtx) or at link-local/metadata addresses. Resolve
		// the host once, reject disallowed ranges, and pin the validated
		// IP into the upstream URL so a DNS rebind between the check and
		// the dial can't redirect the request.
		pinnedHost, verr := validateProxyUpstream(r.Context(), host)
		if verr != nil {
			http.Error(w, "camera address not allowed: "+verr.Error(), http.StatusBadGateway)
			return
		}
		upstream := &url.URL{
			Scheme:   "http",
			Host:     pinnedHost,
			Path:     wildcard,
			RawQuery: r.URL.RawQuery,
		}

		// Forward request body for POST/PUT (login forms, config writes).
		req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), r.Body)
		if err != nil {
			http.Error(w, "proxy build error", http.StatusBadGateway)
			return
		}
		// Pass through most headers. Strip Host (must be the upstream's),
		// accept-encoding (so we can rewrite HTML if needed), authorization
		// (we don't share Ironsight tokens with the camera), and cookie —
		// the Cookie header is rebuilt below from camera-origin cookies
		// only, so Ironsight's own session JWT never leaves the platform.
		for k, vv := range r.Header {
			lower := strings.ToLower(k)
			if lower == "host" || lower == "accept-encoding" || lower == "authorization" || lower == "cookie" {
				continue
			}
			for _, v := range vv {
				req.Header.Add(k, v)
			}
		}
		// F-12: forward ONLY cookies the camera itself set (the ones
		// rewriteSetCookie path-scoped under this proxy). The iframe lives
		// under the session cookie's Path=/, so the browser attaches
		// ironsight_session (a valid 24 h JWT) and ironsight_csrf to every
		// proxied request — relaying those would hand an operator session
		// to whatever onvif_address points at, over cleartext HTTP. The
		// oauth2-proxy SSO cookie is stripped for the same reason.
		for _, c := range r.Cookies() {
			if c.Name == sessionCookieName || c.Name == csrfCookieName ||
				strings.HasPrefix(c.Name, "_oauth2_proxy") {
				continue
			}
			req.AddCookie(c)
		}
		req.Host = host

		// Some cameras' HTTP stacks are slow; use a generous timeout but
		// not infinite so a hung backend can't pin a goroutine and the
		// iframe on a spinner until the browser gives up (F-20).
		// Tradeoff: a single response (headers + body) slower than 30 s is
		// cut off — camera UI pages and assets are small, so in practice
		// only a truly wedged device hits this.
		client := &http.Client{
			Timeout: 30 * time.Second,
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
		copyHeadersForProxy(w.Header(), resp.Header, cameraIDStr, cfg.CookieSecure)
		// Rewrite Location header on redirect responses so absolute paths
		// from the camera resolve back through our proxy.
		if loc := resp.Header.Get("Location"); loc != "" {
			w.Header().Set("Location", rewriteLocation(loc, host, cameraIDStr))
		}

		// For HTML, buffer + rewrite root-relative URLs + inject <base> tag.
		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(strings.ToLower(ct), "text/html") {
			body, rerr := io.ReadAll(resp.Body)
			if rerr != nil {
				http.Error(w, "proxy read error", http.StatusBadGateway)
				return
			}
			body = rewriteRootRelativeURLs(body, cameraIDStr)
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

// validateProxyUpstream resolves a schemeless host[:port] and rejects
// upstreams the camera web-UI proxy must never reach. This is SSRF
// defense-in-depth (F-13): onvif_address is admin-controlled, but a
// mistyped or malicious row must not turn the proxy into a read
// channel to the API host itself. Denied after DNS resolution:
//
//   - loopback (127.0.0.0/8, ::1) — AI sidecars on :8501/:8502, the
//     API itself, anything bound to localhost
//   - link-local (169.254.0.0/16 incl. the 169.254.169.254 cloud
//     metadata endpoint, fe80::/10) and multicast/unspecified
//   - any of the host's own interface addresses — /metrics and other
//     services whose production model is network-trust
//
// RFC1918 ranges are deliberately ALLOWED: every security trailer's
// camera LAN is private (Peplink default 192.168.50.0/24), so a
// private-range deny would block all legitimate cameras.
//
// On success it returns "ip:port" with the first validated IP pinned,
// so the subsequent dial cannot be redirected by a DNS rebind between
// validation and connect. Callers keep req.Host = the original host so
// vhost-routing camera firmware still works.
func validateProxyUpstream(ctx context.Context, host string) (string, error) {
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		// No port — bare hostname/IP. Camera web UIs default to 80.
		hostname, port = host, "80"
	}
	if hostname == "" {
		return "", fmt.Errorf("empty host")
	}

	var ips []net.IP
	if ip := net.ParseIP(strings.Trim(hostname, "[]")); ip != nil {
		ips = []net.IP{ip}
	} else {
		addrs, lerr := net.DefaultResolver.LookupIPAddr(ctx, hostname)
		if lerr != nil {
			return "", fmt.Errorf("resolve %s: %w", hostname, lerr)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no addresses for %s", hostname)
	}

	local := localInterfaceIPs()
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", fmt.Errorf("%s resolves to a disallowed range", hostname)
		}
		if _, isLocal := local[ip.String()]; isLocal {
			return "", fmt.Errorf("%s resolves to this host", hostname)
		}
	}
	return net.JoinHostPort(ips[0].String(), port), nil
}

// localInterfaceIPs returns the set of IPs bound to this host's
// interfaces. Best-effort: an enumeration error yields an empty set
// (the loopback/link-local checks above still apply).
func localInterfaceIPs() map[string]struct{} {
	out := make(map[string]struct{})
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP != nil {
			out[ipn.IP.String()] = struct{}{}
		}
	}
	return out
}

// copyHeadersForProxy mirrors all upstream headers except the ones we
// must strip (framing controls, HSTS) and rewrites Set-Cookie to drop
// any Domain= attribute so the cookie lands on the Ironsight origin.
func copyHeadersForProxy(dst, src http.Header, cameraID string, secure bool) {
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
				dst.Add("Set-Cookie", rewriteSetCookie(v, cameraID, secure))
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
// different cameras don't collide. Path-scoping is a convenience, NOT
// an isolation boundary — these cookies live first-party on the
// Ironsight origin like everything else.
//
// F-29: cameras over plain HTTP never set Secure/SameSite themselves,
// so we force SameSite=Strict (the cookies are only ever needed by
// same-origin iframe fetches through this proxy) and Secure when the
// deployment runs HTTPS (cfg.CookieSecure, same switch the session
// cookies use). Any attributes the camera did send are replaced.
func rewriteSetCookie(c, cameraID string, secure bool) string {
	parts := strings.Split(c, ";")
	var keep []string
	pathScoped := false
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "domain=") {
			continue
		}
		// Drop camera-supplied Secure/SameSite — re-added below on our terms.
		if lower == "secure" || strings.HasPrefix(lower, "samesite=") {
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
	keep = append(keep, " SameSite=Strict")
	if secure {
		keep = append(keep, " Secure")
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

// rootRel*Re match root-relative URL references in camera HTML:
// src/href/action attributes and CSS url() values whose path starts
// with a single "/". Protocol-relative ("//host/…") and already-
// proxied paths are excluded in rewriteRootRelativeURLs itself (RE2
// has no lookahead). The capture-group layout puts the path in group
// 3 (attributes) / group 2 (CSS) — keep rewriteRootRelativeURLs in
// sync if these change.
var (
	rootRelAttrDQRe = regexp.MustCompile(`(?i)\b(src|href|action)(\s*=\s*")(/[^"]*)"`)
	rootRelAttrSQRe = regexp.MustCompile(`(?i)\b(src|href|action)(\s*=\s*')(/[^']*)'`)
	rootRelCSSRe    = regexp.MustCompile(`(?i)\burl\(\s*(["']?)(/[^"')]+)`)
)

// rewriteRootRelativeURLs prefixes root-relative URLs in camera HTML
// with the proxy path (F-21). The injected <base href> only affects
// document-relative references — per RFC 3986 §5.3 a path-absolute
// reference like /static/main.js takes only scheme+authority from the
// base and REPLACES its path, so without this rewrite such assets
// resolve against the Ironsight origin root and 404. Regex-based and
// best-effort: covers src=/href=/action= attributes and url(/…) in
// inline CSS; URLs constructed in JavaScript at runtime can't be
// caught here (those vendors get the "open in new tab" fallback).
func rewriteRootRelativeURLs(body []byte, cameraID string) []byte {
	prefix := []byte("/api/cameras/" + cameraID + "/web-ui")
	fix := func(re *regexp.Regexp, pathGroup int) {
		body = re.ReplaceAllFunc(body, func(m []byte) []byte {
			idx := re.FindSubmatchIndex(m)
			if idx == nil || idx[2*pathGroup] < 0 {
				return m
			}
			start := idx[2*pathGroup]
			path := m[start:idx[2*pathGroup+1]]
			// Leave protocol-relative URLs and paths already pointing at
			// the proxy untouched.
			if bytes.HasPrefix(path, []byte("//")) || bytes.HasPrefix(path, prefix) {
				return m
			}
			out := make([]byte, 0, len(m)+len(prefix))
			out = append(out, m[:start]...)
			out = append(out, prefix...)
			out = append(out, m[start:]...)
			return out
		})
	}
	fix(rootRelAttrDQRe, 3)
	fix(rootRelAttrSQRe, 3)
	fix(rootRelCSSRe, 2)
	return body
}

// injectBaseHref inserts <base href="/api/cameras/<id>/web-ui/"> right
// after the <head> tag in the camera's HTML so document-relative URLs
// (like "img/logo.png") resolve against the proxy root. NOTE: <base>
// does NOT affect root-relative URLs ("/static/main.js") — those are
// handled by rewriteRootRelativeURLs above. If <head> isn't found
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
