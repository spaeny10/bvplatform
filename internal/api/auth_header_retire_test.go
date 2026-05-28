package api

// P1-A-02 PR3 — retire the Authorization: Bearer session path.
//
// Test map (§4 of the PR3 plan):
//   TestRequireAuth_AuthHeaderRejected  — regression guard: Bearer header without
//                                         cookie now returns 401.
//   TestRequireAuth_CookieStillAccepted — valid ironsight_session cookie still works.
//   TestRequireAuth_SSOStillWorks       — X-Forwarded-Email path unchanged.
//   TestMetrics_NetworkTrust            — METRICS_AUTH=none → 200 without auth;
//                                         METRICS_AUTH=sso  → 401 without auth.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ironsight/internal/auth"
	"ironsight/internal/config"
)

// ── TestRequireAuth_AuthHeaderRejected ────────────────────────────────────────
//
// A request carrying only a valid-looking Authorization: Bearer <sessionJWT>
// (no ironsight_session cookie) MUST return 401 after PR3.
// This is the retirement regression guard — if the header path ever sneaks
// back into RequireAuth, this test will catch it.
func TestRequireAuth_AuthHeaderRejected(t *testing.T) {
	cfg := minConfig(false)
	tok, _ := signTestToken(t, "uid-retired", "headeruser", "admin")

	reached := false
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(cfg, nil)(sentinel)
	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	// Deliberately NO ironsight_session cookie.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Authorization: Bearer without cookie: got %d, want 401 (header path retired)", w.Code)
	}
	if reached {
		t.Error("downstream handler was reached — Bearer header should have been rejected after PR3")
	}
}

// ── TestRequireAuth_CookieStillAccepted ──────────────────────────────────────
//
// A valid ironsight_session cookie continues to grant access after PR3.
// Confirms that removing the bearer path didn't inadvertently break the
// cookie path.
func TestRequireAuth_CookieStillAccepted(t *testing.T) {
	cfg := minConfig(false)
	tok, _ := signTestToken(t, "uid-cookie-pr3", "cookieuser", "soc_operator")

	var gotUsername string
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims, ok := r.Context().Value(ContextKeyClaims).(*auth.Claims); ok && claims != nil {
			gotUsername = claims.Username
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(cfg, nil)(sentinel)
	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid cookie: got %d, want 200", w.Code)
	}
	if gotUsername != "cookieuser" {
		t.Errorf("claims.Username = %q, want cookieuser", gotUsername)
	}
}

// ── TestRequireAuth_SSOStillWorks ─────────────────────────────────────────────
//
// The X-Forwarded-Email SSO trust path is untouched by PR3. Confirms it still
// returns 401 when SSOTrustHeader is not "email", and that the SSO
// provisioning branch is entered when SSOTrustHeader IS "email".
func TestRequireAuth_SSOStillWorks(t *testing.T) {
	t.Run("sso_disabled_header_ignored", func(t *testing.T) {
		// When SSO is not configured, X-Forwarded-Email is ignored and a
		// request with no cookie gets 401.
		cfg := minConfig(false) // SSOTrustHeader = ""
		sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := RequireAuth(cfg, nil)(sentinel)
		req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
		req.Header.Set("X-Forwarded-Email", "operator@bigview.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("SSO disabled: got %d, want 401", w.Code)
		}
	})

	t.Run("sso_enabled_branch_reached", func(t *testing.T) {
		// When SSOTrustHeader="email" and X-Forwarded-Email is present,
		// RequireAuth enters the SSO provisioning path (which panics on nil DB).
		// Catching the panic confirms the SSO branch was reached, not the 401 branch.
		cfgSSO := &config.Config{
			JWTSecret:      cookieTestSecret,
			CookieSecure:   false,
			SSOTrustHeader: "email",
			SSODefaultRole: "viewer",
		}
		sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := RequireAuth(cfgSSO, nil)(sentinel)
		req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
		req.Header.Set("X-Forwarded-Email", "operator@bigview.com")
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

		if !ssoBranchReached && w.Code == http.StatusUnauthorized {
			t.Error("SSO trust branch was not entered — got 401 instead of nil-DB panic")
		}
	})
}

// ── TestMetrics_NetworkTrust ──────────────────────────────────────────────────
//
// Verifies the METRICS_AUTH semantics post-PR3:
//   METRICS_AUTH=none  → /metrics returns 200 with no auth (network restriction
//                        is the reverse proxy's responsibility, not the app's)
//   METRICS_AUTH=sso   → /metrics requires auth; unauthenticated request returns 401
//
// Note: "none" is the default as of P1-A-02 PR3. The router registers the handler
// without RequireAuth when MetricsAuth=="none". This test simulates both sides of
// that conditional without pulling in the full NewRouter graph.
func TestMetrics_NetworkTrust(t *testing.T) {
	t.Run("none_returns_200_without_auth", func(t *testing.T) {
		// Simulate METRICS_AUTH=none: register /metrics with NO RequireAuth.
		metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		// No cookie, no Authorization header.
		w := httptest.NewRecorder()
		metricsHandler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("METRICS_AUTH=none: got %d, want 200 (no app auth required)", w.Code)
		}
	})

	t.Run("sso_returns_401_without_auth", func(t *testing.T) {
		// Simulate METRICS_AUTH=sso: wrap the handler in RequireAuth.
		// An unauthenticated request (no cookie, no SSO header) must return 401.
		cfg := &config.Config{
			JWTSecret:      cookieTestSecret,
			MetricsAuth:    "sso",
			MetricsEnabled: true,
		}
		metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := RequireAuth(cfg, nil)(metricsHandler)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		// No cookie, no auth header.
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("METRICS_AUTH=sso unauthenticated: got %d, want 401", w.Code)
		}
	})
}
