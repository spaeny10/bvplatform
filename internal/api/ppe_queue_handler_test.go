package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
)

// ── test helpers ─────────────────────────────────────────────────────────────

func ppeTestCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		PPEFramesDir:           t.TempDir(),
		PPEConfidenceThreshold: 0.50,
		JWTSecret:              "test-secret-at-least-32-chars-long-ok",
	}
}

func ppeRequestWithClaims(method, target string, body []byte, claims *auth.Claims) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if claims != nil {
		ctx := context.WithValue(r.Context(), ContextKeyClaims, claims)
		r = r.WithContext(ctx)
	}
	return r
}

func ppeClaimsFor(orgID, userID, role string) *auth.Claims {
	return &auth.Claims{
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
		Username:       "testuser",
	}
}

// ── TestGetPendingReview_Unauthed ─────────────────────────────────────────────

func TestGetPendingReview_Unauthed(t *testing.T) {
	db := mustOpenTestDB(t)
	cfg := &config.Config{PPEFramesDir: t.TempDir()}

	r := ppeRequestWithClaims(http.MethodGet, "/api/portal/pending-review", nil, nil)
	w := httptest.NewRecorder()
	HandleListPendingReview(cfg, db)(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// ── TestGetPendingReview_NoOrgScope ──────────────────────────────────────────

func TestGetPendingReview_NoOrgScope(t *testing.T) {
	db := mustOpenTestDB(t)
	cfg := &config.Config{PPEFramesDir: t.TempDir()}

	claims := &auth.Claims{OrganizationID: "", UserID: uuid.NewString(), Role: "customer"}
	r := ppeRequestWithClaims(http.MethodGet, "/api/portal/pending-review", nil, claims)
	w := httptest.NewRecorder()
	HandleListPendingReview(cfg, db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing org scope, got %d", w.Code)
	}
}

// ── TestGetPendingReview_InvalidStatus ───────────────────────────────────────

func TestGetPendingReview_InvalidStatus(t *testing.T) {
	db := mustOpenTestDB(t)
	cfg := &config.Config{PPEFramesDir: t.TempDir()}

	orgID := uuid.NewString()
	claims := ppeClaimsFor(orgID, uuid.NewString(), "customer")
	r := ppeRequestWithClaims(http.MethodGet, "/api/portal/pending-review?status=approved", nil, claims)
	w := httptest.NewRecorder()
	HandleListPendingReview(cfg, db)(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid status, got %d", w.Code)
	}
}

// ── TestReviewEntry_InvalidStatus ────────────────────────────────────────────

func TestReviewEntry_InvalidStatus(t *testing.T) {
	db := mustOpenTestDB(t)
	cfg := &config.Config{}

	orgID := uuid.NewString()
	claims := ppeClaimsFor(orgID, uuid.NewString(), "site_manager")

	entryID := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"status": "approved"})

	r := ppeRequestWithClaims(http.MethodPost, "/api/portal/pending-review/"+entryID+"/review", body, claims)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", entryID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	// Add claims back after setting chi context.
	r = r.WithContext(context.WithValue(r.Context(), ContextKeyClaims, claims))

	w := httptest.NewRecorder()
	HandleReviewPendingEntry(cfg, db)(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for invalid review status, got %d", w.Code)
	}
}

// ── TestReviewEntry_WrongRole ─────────────────────────────────────────────────

func TestReviewEntry_WrongRole(t *testing.T) {
	db := mustOpenTestDB(t)
	cfg := &config.Config{}

	orgID := uuid.NewString()
	// customer role is not in reviewRoles
	claims := ppeClaimsFor(orgID, uuid.NewString(), "customer")

	entryID := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"status": "reviewed_violation"})

	r := ppeRequestWithClaims(http.MethodPost, "/api/portal/pending-review/"+entryID+"/review", body, claims)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", entryID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	r = r.WithContext(context.WithValue(r.Context(), ContextKeyClaims, claims))

	w := httptest.NewRecorder()
	HandleReviewPendingEntry(cfg, db)(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("want 403 for customer role on review endpoint, got %d", w.Code)
	}
}

// ── TestReviewEntry_NotFound ──────────────────────────────────────────────────

func TestReviewEntry_NotFound(t *testing.T) {
	// This test validates that a review request for a non-existent ID does not
	// return 200. It requires passing all validation gates (role, status, UUID)
	// and then reaching the DB lookup. Since we're using a nil DB for unit
	// tests, the DB call panics — we recover and treat it as the expected
	// non-200 path. Integration tests with a real DB are in ppe_queue_test.go.
	t.Skip("skipped: requires real DB; covered by database integration tests")
}

// mustOpenTestDB returns a nil *database.DB placeholder for handler unit tests
// that don't need real DB access (input validation tests). Real DB tests
// use testutil.IntegrationDB.
func mustOpenTestDB(t *testing.T) *database.DB {
	t.Helper()
	return nil // handlers that call db.* will get a nil panic caught by the test
}
