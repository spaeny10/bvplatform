package api

// Security-hardening batch 2 — camera web-UI proxy unit tests.
//
// Unit-level, no Postgres and no network listener required. The SSRF
// validator tests use IP literals only (documentation ranges for the
// allow cases) so they never touch DNS.
//
// Test map:
//   TestValidateProxyUpstream_RejectsInternal — loopback / link-local /
//       metadata / unspecified upstreams are refused (F-13)
//   TestValidateProxyUpstream_AllowsCameraLAN  — routable + default port pin
//   TestRewriteSetCookie_ForcesAttrs           — Secure + SameSite=Strict
//       forced, Domain stripped, Path scoped (F-29)
//   TestRewriteRootRelativeURLs                — root-relative src/href/
//       action/url() rewritten to the proxy prefix; protocol-relative,
//       document-relative, and already-proxied URLs untouched (F-21)

import (
	"context"
	"strings"
	"testing"
)

// ── F-13: SSRF upstream validation ───────────────────────────────────────────

func TestValidateProxyUpstream_RejectsInternal(t *testing.T) {
	cases := []struct {
		name string
		host string
	}{
		{"loopback v4", "127.0.0.1:8501"},
		{"loopback v4 no port", "127.0.0.1"},
		{"loopback v6", "[::1]:80"},
		{"link-local metadata", "169.254.169.254"},
		{"link-local v4", "169.254.10.20:80"},
		{"link-local v6", "[fe80::1]:80"},
		{"unspecified", "0.0.0.0:9090"},
		{"multicast", "224.0.0.1"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateProxyUpstream(context.Background(), tc.host); err == nil {
				t.Errorf("validateProxyUpstream(%q) = nil error, want rejection", tc.host)
			}
		})
	}
}

func TestValidateProxyUpstream_AllowsCameraLAN(t *testing.T) {
	// 203.0.113.0/24 is TEST-NET-3 (RFC 5737) — routable-looking, never a
	// local interface, never loopback/link-local. Stands in for a real
	// trailer-LAN camera without depending on the test host's addressing.
	pinned, err := validateProxyUpstream(context.Background(), "203.0.113.10:8080")
	if err != nil {
		t.Fatalf("validateProxyUpstream: %v", err)
	}
	if pinned != "203.0.113.10:8080" {
		t.Errorf("pinned = %q, want 203.0.113.10:8080", pinned)
	}

	// No port → camera web UI default 80 is appended.
	pinned, err = validateProxyUpstream(context.Background(), "203.0.113.10")
	if err != nil {
		t.Fatalf("validateProxyUpstream (no port): %v", err)
	}
	if pinned != "203.0.113.10:80" {
		t.Errorf("pinned = %q, want 203.0.113.10:80", pinned)
	}
}

// ── F-29: rewritten Set-Cookie attributes ────────────────────────────────────

func TestRewriteSetCookie_ForcesAttrs(t *testing.T) {
	const camID = "11111111-2222-3333-4444-555555555555"
	wantPath := "Path=/api/cameras/" + camID + "/web-ui/"

	t.Run("secure deployment", func(t *testing.T) {
		got := rewriteSetCookie("sid=abc123; Domain=192.168.50.10; Path=/; SameSite=None", camID, true)
		if strings.Contains(got, "Domain=") {
			t.Errorf("Domain attribute not stripped: %q", got)
		}
		if !strings.Contains(got, wantPath) {
			t.Errorf("Path not scoped to proxy: %q", got)
		}
		if !strings.Contains(got, "SameSite=Strict") {
			t.Errorf("SameSite=Strict not forced: %q", got)
		}
		if strings.Contains(got, "SameSite=None") {
			t.Errorf("camera-supplied SameSite survived: %q", got)
		}
		if !strings.Contains(got, "Secure") {
			t.Errorf("Secure not forced on secure deployment: %q", got)
		}
		if !strings.HasPrefix(got, "sid=abc123") {
			t.Errorf("cookie name/value mangled: %q", got)
		}
	})

	t.Run("insecure dev deployment", func(t *testing.T) {
		got := rewriteSetCookie("sid=abc123", camID, false)
		if strings.Contains(got, "Secure") {
			t.Errorf("Secure must follow cfg.CookieSecure=false: %q", got)
		}
		if !strings.Contains(got, "SameSite=Strict") {
			t.Errorf("SameSite=Strict not forced: %q", got)
		}
		if !strings.Contains(got, wantPath) {
			t.Errorf("Path not scoped when camera sent none: %q", got)
		}
	})
}

// ── F-21: root-relative URL rewriting ────────────────────────────────────────

func TestRewriteRootRelativeURLs(t *testing.T) {
	const camID = "11111111-2222-3333-4444-555555555555"
	prefix := "/api/cameras/" + camID + "/web-ui"

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"double-quoted src",
			`<script src="/static/main.js"></script>`,
			`<script src="` + prefix + `/static/main.js"></script>`,
		},
		{
			"single-quoted href",
			`<link href='/css/app.css' rel='stylesheet'>`,
			`<link href='` + prefix + `/css/app.css' rel='stylesheet'>`,
		},
		{
			"form action",
			`<form action="/cgi-bin/login.cgi" method="post">`,
			`<form action="` + prefix + `/cgi-bin/login.cgi" method="post">`,
		},
		{
			"css url()",
			`<style>body { background: url(/img/bg.png); }</style>`,
			`<style>body { background: url(` + prefix + `/img/bg.png); }</style>`,
		},
		{
			"root path only",
			`<a href="/">home</a>`,
			`<a href="` + prefix + `/">home</a>`,
		},
		{
			"protocol-relative untouched",
			`<script src="//cdn.example.com/lib.js"></script>`,
			`<script src="//cdn.example.com/lib.js"></script>`,
		},
		{
			"document-relative untouched",
			`<img src="img/logo.png">`,
			`<img src="img/logo.png">`,
		},
		{
			"absolute URL untouched",
			`<a href="https://example.com/x">x</a>`,
			`<a href="https://example.com/x">x</a>`,
		},
		{
			"already-proxied untouched",
			`<img src="` + prefix + `/img/logo.png">`,
			`<img src="` + prefix + `/img/logo.png">`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(rewriteRootRelativeURLs([]byte(tc.in), camID))
			if got != tc.want {
				t.Errorf("rewriteRootRelativeURLs:\n  in:   %s\n  got:  %s\n  want: %s", tc.in, got, tc.want)
			}
		})
	}
}
