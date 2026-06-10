package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ironsight/internal/auth"
	"ironsight/internal/config"
)

// live2Request builds a request for /api/live2/{cameraID}/ws with the chi
// URL param populated and optional claims in context. We don't actually
// perform a WebSocket upgrade here — the auth gate runs (and rejects)
// before any upgrade, which is exactly what these tests assert.
func live2Request(cameraID string, claims *auth.Claims) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/live2/"+cameraID+"/ws", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("cameraID", cameraID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	if claims != nil {
		r = r.WithContext(context.WithValue(r.Context(), ContextKeyClaims, claims))
	}
	return r
}

// TestHandleLive2Proxy_Unauthed verifies that a request with no JWT claims
// gets 401 — BEFORE any DB access or WS upgrade. A nil *database.DB is safe
// here precisely because the claims check short-circuits first (mirrors the
// HandleLiveProxy auth ordering this handler copies).
func TestHandleLive2Proxy_Unauthed(t *testing.T) {
	cfg := &config.Config{Go2RTCAddr: "127.0.0.1:1984", MediaMTXRTSPAddr: "127.0.0.1:18554"}
	handler := HandleLive2Proxy(cfg, nil)

	w := httptest.NewRecorder()
	r := live2Request("11111111-1111-1111-1111-111111111111", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated request: want 401, got %d", w.Code)
	}
}

// TestHandleLive2Proxy_BadCameraID verifies that an authenticated request
// with a non-UUID cameraID gets 404 (not found), again before any DB or WS
// work. claims are present so we pass the auth gate and reach the UUID parse.
func TestHandleLive2Proxy_BadCameraID(t *testing.T) {
	cfg := &config.Config{Go2RTCAddr: "127.0.0.1:1984", MediaMTXRTSPAddr: "127.0.0.1:18554"}
	handler := HandleLive2Proxy(cfg, nil)

	claims := &auth.Claims{UserID: "u", Role: "admin"}
	w := httptest.NewRecorder()
	r := live2Request("not-a-uuid", claims)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("bad cameraID: want 404, got %d", w.Code)
	}
}

// TestLowLatencyLiveFlagDefaultOff verifies the new feature flag ships dark:
// it must exist in DefaultFeatureFlags and default to false, matching the
// frontend default in lib/feature-flags.ts.
func TestLowLatencyLiveFlagDefaultOff(t *testing.T) {
	val, ok := DefaultFeatureFlags["lowlatency_live"]
	if !ok {
		t.Fatal("lowlatency_live missing from DefaultFeatureFlags")
	}
	if val {
		t.Error("lowlatency_live must default to false (ships dark)")
	}
}
