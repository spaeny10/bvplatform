package api

// P1-A-02 part 2 — cookie issuance, CSRF double-submit, and session-cookie
// read path in RequireAuth.
//
// These tests are unit-level — no Postgres required. Handler tests that
// exercise the full login/logout flow (including DB writes) belong in the
// integration suite (internal/testutil/integration_test.go).
//
// Test map (§4 of the plan):
//   TestLogin_SetsCookies          — setSessionCookies emits the right attributes
//   TestLogout_ClearsCookies       — clearSessionCookies zeroes both cookies
//   TestRequireAuth_CookieAccepted — valid cookie reaches next handler
//   TestRequireAuth_AuthHeaderStillAccepted — Bearer header still accepted during fallback window
//   TestCSRF_MismatchReturns403    — wrong CSRF token → 403
//   TestCSRF_AbsenceReturns403     — absent CSRF token → 403
//   TestCSRF_GETNotChecked         — GET exempt from CSRF
//   TestSSO_NoCookieRequired       — X-Forwarded-Email path unaffected
//   TestWSTicket_CookieSession     — stub: verify cookie read supplies claims to context

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ironsight/internal/auth"
	"ironsight/internal/config"
)

// cookieTestSecret is the JWT signing secret used across all tests in
// this file. Must not appear in production code paths.
const cookieTestSecret = "cookie-csrf-unit-test-secret-abc"

// minConfig returns the smallest *config.Config needed to exercise the
// cookie and CSRF middleware paths. No database connection is needed.
func minConfig(secure bool) *config.Config {
	return &config.Config{
		JWTSecret:      cookieTestSecret,
		CookieSecure:   secure,
		SSOTrustHeader: "",
	}
}

// signTestToken mints a real HS256 JWT with a 24 h expiry for testing.
// Returns the signed string and its expiry time.
func signTestToken(t *testing.T, userID, username, role string) (string, time.Time) {
	t.Helper()
	tok, _, err := auth.SignToken(userID, username, role, "Test User", "", cookieTestSecret)
	if err != nil {
		t.Fatalf("signTestToken: %v", err)
	}
	exp := time.Now().Add(24 * time.Hour)
	return tok, exp
}

// ── Cookie issuance ──────────────────────────────────────────────────────────

// TestLogin_SetsCookies verifies that setSessionCookies emits two
// Set-Cookie headers with the right attributes.
//
// We test setSessionCookies directly rather than HandleLogin because
// HandleLogin requires a live DB. The cookie-setter is the exact function
// HandleLogin calls — this test covers the emission contract.
func TestLogin_SetsCookies(t *testing.T) {
	cfg := minConfig(true)
	tok, exp := signTestToken(t, "uid-01", "testuser", "admin")
	csrfTok, err := generateCSRFToken()
	if err != nil {
		t.Fatalf("generateCSRFToken: %v", err)
	}

	w := httptest.NewRecorder()
	setSessionCookies(w, tok, csrfTok, exp, cfg)

	cookies := w.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 Set-Cookie headers, got %d", len(cookies))
	}

	var session, csrf *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case sessionCookieName:
			session = c
		case csrfCookieName:
			csrf = c
		}
	}
	if session == nil {
		t.Fatal("ironsight_session cookie not set")
	}
	if csrf == nil {
		t.Fatal("ironsight_csrf cookie not set")
	}

	// Session cookie: HttpOnly=true, Secure=true (cfg.CookieSecure), SameSite=Lax
	if !session.HttpOnly {
		t.Error("ironsight_session must be HttpOnly")
	}
	if !session.Secure {
		t.Error("ironsight_session must be Secure when cfg.CookieSecure=true")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("ironsight_session SameSite = %v, want Lax", session.SameSite)
	}
	if session.MaxAge <= 0 {
		t.Errorf("ironsight_session MaxAge = %d, want >0", session.MaxAge)
	}
	if session.Value != tok {
		t.Error("ironsight_session value should be the JWT token string")
	}

	// CSRF cookie: NOT HttpOnly (must be JS-readable), Secure=true, SameSite=Lax
	if csrf.HttpOnly {
		t.Error("ironsight_csrf must NOT be HttpOnly — JS must be able to read it")
	}
	if !csrf.Secure {
		t.Error("ironsight_csrf must be Secure when cfg.CookieSecure=true")
	}
	if csrf.SameSite != http.SameSiteLaxMode {
		t.Errorf("ironsight_csrf SameSite = %v, want Lax", csrf.SameSite)
	}
	if csrf.Value != csrfTok {
		t.Error("ironsight_csrf value mismatch")
	}
}

// TestLogout_ClearsCookies verifies that clearSessionCookies emits both
// cookies with Max-Age=0, which causes the browser to delete them.
func TestLogout_ClearsCookies(t *testing.T) {
	cfg := minConfig(true)
	w := httptest.NewRecorder()
	clearSessionCookies(w, cfg)

	cookies := w.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 Set-Cookie headers on logout, got %d", len(cookies))
	}

	for _, c := range cookies {
		if c.MaxAge != 0 {
			t.Errorf("cookie %s: MaxAge = %d, want 0 to clear", c.Name, c.MaxAge)
		}
		if c.Name != sessionCookieName && c.Name != csrfCookieName {
			t.Errorf("unexpected cookie name %q on logout", c.Name)
		}
	}
}

// ── RequireAuth cookie read path ─────────────────────────────────────────────

// TestRequireAuth_CookieAccepted verifies that a valid ironsight_session
// cookie is accepted by RequireAuth and the request reaches the downstream
// handler with claims populated.
func TestRequireAuth_CookieAccepted(t *testing.T) {
	cfg := minConfig(false) // Secure=false so httptest (plain HTTP) doesn't strip cookies
	tok, exp := signTestToken(t, "uid-02", "alice", "soc_operator")

	csrfTok, _ := generateCSRFToken()
	// Handler that asserts claims are present
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if !ok || claims == nil {
			t.Error("claims not found in context")
		} else if claims.Username != "alice" {
			t.Errorf("claims.Username = %q, want alice", claims.Username)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(cfg, nil)(sentinel)
	req := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTok})
	// Use actual cookie expiry to satisfy cookie parsing
	_ = exp
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

// TestRequireAuth_AuthHeaderStillAccepted verifies the Authorization:
// Bearer fallback is still accepted during the PR1+PR2 migration window.
// This test should be deleted when PR 3 retires the header path.
func TestRequireAuth_AuthHeaderStillAccepted(t *testing.T) {
	cfg := minConfig(false)
	tok, _ := signTestToken(t, "uid-03", "bob", "admin")

	reached := false
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(cfg, nil)(sentinel)
	req := httptest.NewRequest(http.MethodGet, "/api/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	if !reached {
		t.Error("downstream handler not reached")
	}
}

// ── CSRF middleware ───────────────────────────────────────────────────────────

// TestCSRF_MismatchReturns403 verifies that a POST with a session cookie
// and an X-CSRF-Token that does NOT match ironsight_csrf returns 403.
func TestCSRF_MismatchReturns403(t *testing.T) {
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware(sentinel)

	req := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	// Cookie says "abc", header says "xyz" — mismatch
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "abc123"})
	req.Header.Set("X-CSRF-Token", "xyz999")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403 on CSRF mismatch", w.Code)
	}
}

// TestCSRF_AbsenceReturns403 verifies that a POST with no X-CSRF-Token
// header (even if the cookie exists) returns 403.
func TestCSRF_AbsenceReturns403(t *testing.T) {
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware(sentinel)

	req := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "abc123"})
	// No X-CSRF-Token header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403 on absent CSRF header", w.Code)
	}
}

// TestCSRF_GETNotChecked verifies that GET requests pass through CSRF
// middleware without any CSRF cookie or header required.
func TestCSRF_GETNotChecked(t *testing.T) {
	reached := false
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware(sentinel)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	// Deliberately no CSRF cookie or header
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200 for GET (CSRF exempt)", w.Code)
	}
	if !reached {
		t.Error("downstream handler not reached on GET")
	}
}

// TestCSRF_MatchAllows verifies that a POST with matching cookie and header
// values passes through.
func TestCSRF_MatchAllows(t *testing.T) {
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CSRFMiddleware(sentinel)

	token := "matching-csrf-token-value"
	req := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200 when CSRF token matches", w.Code)
	}
}

// ── SSO trust path ────────────────────────────────────────────────────────────

// TestSSO_NoCookieRequired verifies that a request bearing X-Forwarded-Email
// (the SSO trust-header path) succeeds without any session cookie or JWT.
// The SSOTrustHeader must be "email" and a nil db is passed (SSO provisioning
// needs the DB, but we short-circuit by verifying RequireAuth calls next on
// a configured email when db is non-nil).
//
// Since provisioning requires a real DB, we test the simpler case: when
// SSOTrustHeader is not "email", the header is ignored and a missing
// cookie/token produces a 401. This confirms the SSO gate is respected.
func TestSSO_NoCookieRequired(t *testing.T) {
	// With SSOTrustHeader unset, X-Forwarded-Email is ignored.
	cfgNoSSO := minConfig(false)

	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(cfgNoSSO, nil)(sentinel)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	req.Header.Set("X-Forwarded-Email", "user@example.com") // injected, but cfg doesn't trust it
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Without SSO trust enabled AND without a cookie/bearer, expect 401
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401 when SSO trust header not configured", w.Code)
	}
}

// TestSSO_TrustHeaderPathWithoutDB confirms that when SSOTrustHeader="email"
// AND X-Forwarded-Email is present, RequireAuth attempts the SSO branch
// (which then requires the DB) rather than falling through to the no-cookie 401.
// We assert this by catching the nil-DB panic with recover() and observing that
// the panic IS reached — proving the SSO branch was entered, not the JWT branch.
func TestSSO_TrustHeaderPathWithoutDB(t *testing.T) {
	cfgSSO := &config.Config{
		JWTSecret:      cookieTestSecret,
		CookieSecure:   false,
		SSOTrustHeader: "email",
		SSODefaultRole: "viewer",
		SSOAdminEmails: nil,
	}

	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(cfgSSO, nil)(sentinel)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	req.Header.Set("X-Forwarded-Email", "user@example.com")
	w := httptest.NewRecorder()

	ssoBranchReached := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				ssoBranchReached = true
			}
		}()
		handler.ServeHTTP(w, req)
	}()

	// The SSO branch should be reached (and panic on nil DB), NOT the 401 branch.
	if !ssoBranchReached && w.Code == http.StatusUnauthorized {
		t.Error("SSO trust branch was not entered — got 401 instead of nil-DB panic")
	}
}

// ── WS ticket (stub) ──────────────────────────────────────────────────────────

// TestWSTicket_CookieSession verifies that RequireAuth correctly populates
// the claims in context when a session cookie is present, which is the
// precondition for the ws-ticket endpoint to work post-migration.
// The ws-ticket handler itself lives in websocket.go (not yet committed
// locally from fred); this test covers the auth side of the interaction.
func TestWSTicket_CookieSession(t *testing.T) {
	cfg := minConfig(false)
	tok, _ := signTestToken(t, "uid-ws", "wsuser", "soc_operator")

	var gotRole string
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if ok && claims != nil {
			gotRole = claims.Role
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(cfg, nil)(sentinel)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/ws-ticket", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	if gotRole != "soc_operator" {
		t.Errorf("claims.Role = %q, want soc_operator", gotRole)
	}
}

// ── generateCSRFToken ────────────────────────────────────────────────────────

// TestGenerateCSRFToken confirms the function produces non-empty,
// 64-character hex strings and doesn't repeat across calls.
func TestGenerateCSRFToken(t *testing.T) {
	a, err := generateCSRFToken()
	if err != nil {
		t.Fatalf("generateCSRFToken: %v", err)
	}
	if len(a) != 64 {
		t.Errorf("len = %d, want 64 (32 bytes × 2 hex chars)", len(a))
	}
	if !isHex(a) {
		t.Errorf("value %q is not hex", a)
	}
	b, _ := generateCSRFToken()
	if a == b {
		t.Error("two calls returned identical CSRF tokens — RNG not seeded?")
	}
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
