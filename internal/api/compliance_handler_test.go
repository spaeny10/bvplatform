package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ironsight/internal/auth"
	"ironsight/internal/database"
	"ironsight/internal/testutil"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func complianceClaimsFor(orgID, role string) *auth.Claims {
	return &auth.Claims{
		OrganizationID: orgID,
		UserID:         "user-uuid-test",
		Role:           role,
		Username:       "testuser@example.com",
	}
}

func complianceRequest(method, url string, claims *auth.Claims) *http.Request {
	r := httptest.NewRequest(method, url, nil)
	if claims != nil {
		ctx := context.WithValue(r.Context(), ContextKeyClaims, claims)
		r = r.WithContext(ctx)
	}
	return r
}

// ── Handler unit tests (input-validation, nil DB) ─────────────────────────────

// TestHandleComplianceSummary_Unauthed verifies that a request with no JWT
// claims returns 401.
func TestHandleComplianceSummary_Unauthed(t *testing.T) {
	db := mustOpenTestDB(t)
	handler := HandleComplianceSummary(db)

	w := httptest.NewRecorder()
	r := complianceRequest(http.MethodGet, "/api/v1/portal/compliance/summary", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

// TestHandleComplianceSummary_InvalidPeriod verifies that an unknown period
// value returns 400.
func TestHandleComplianceSummary_InvalidPeriod(t *testing.T) {
	db := mustOpenTestDB(t)
	handler := HandleComplianceSummary(db)

	claims := complianceClaimsFor("org-a", "customer")
	w := httptest.NewRecorder()
	r := complianceRequest(http.MethodGet, "/api/v1/portal/compliance/summary?period=yesterday", claims)

	defer func() {
		if rec := recover(); rec != nil {
			// nil-DB panic is expected only when we reach the DB call.
			// If the panic happened after we expected a 400, that is a bug.
			t.Errorf("unexpected panic (should have rejected before DB call): %v", rec)
		}
	}()

	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid period, got %d", w.Code)
	}
}

// TestHandleComplianceSummary_CustomPeriodMissingDates verifies that
// period=custom without start_date/end_date returns 400.
func TestHandleComplianceSummary_CustomPeriodMissingDates(t *testing.T) {
	db := mustOpenTestDB(t)
	handler := HandleComplianceSummary(db)

	claims := complianceClaimsFor("org-a", "customer")
	w := httptest.NewRecorder()
	r := complianceRequest(http.MethodGet, "/api/v1/portal/compliance/summary?period=custom", claims)

	defer func() {
		if rec := recover(); rec != nil {
			t.Errorf("unexpected panic: %v", rec)
		}
	}()

	handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for custom period without dates, got %d", w.Code)
	}
}

// ── Integration tests (real DB) ───────────────────────────────────────────────

// TestHandleComplianceSummary_Authed does a happy-path request against the real
// DB and verifies the response is 200 with the expected JSON shape.
func TestHandleComplianceSummary_Authed(t *testing.T) {
	realDB := testutil.IntegrationDB(t)

	// Find a real org+camera to use.
	ctx := context.Background()
	orgID, _, _ := findDBTestCamera(t, realDB, ctx)

	handler := HandleComplianceSummary(realDB)
	claims := complianceClaimsFor(orgID, "customer")
	w := httptest.NewRecorder()
	r := complianceRequest(http.MethodGet, "/api/v1/portal/compliance/summary?period=week", claims)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	// Verify key shape fields are present.
	for _, key := range []string{"period", "total_violations", "total_reviewed", "compliance_rate", "violations_over_time", "top_cameras", "recent_findings"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing field %q", key)
		}
	}
}

// TestHandleComplianceSummary_CrossTenant verifies that passing a site_id
// belonging to a different org returns 403.
func TestHandleComplianceSummary_CrossTenant(t *testing.T) {
	realDB := testutil.IntegrationDB(t)
	ctx := context.Background()

	// Get real org A site.
	orgA, _, siteID := findDBTestCamera(t, realDB, ctx)
	// Use a completely different fake org.
	orgB := "fake-org-b-" + siteID[:8]

	handler := HandleComplianceSummary(realDB)
	// Caller is org B but requests org A's site.
	claims := complianceClaimsFor(orgB, "customer")
	w := httptest.NewRecorder()
	r := complianceRequest(http.MethodGet,
		"/api/v1/portal/compliance/summary?period=week&site_id="+siteID,
		claims)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("cross-tenant site access: want 403, got %d; body: %s", w.Code, w.Body.String())
	}
	_ = orgA
}

// TestHandleComplianceReportPDF_Smoke verifies that the PDF endpoint returns
// 200 with Content-Type: application/pdf and valid PDF magic bytes, and
// that the X-Report-ID header is a UUID.
func TestHandleComplianceReportPDF_Smoke(t *testing.T) {
	realDB := testutil.IntegrationDB(t)
	ctx := context.Background()

	orgID, _, _ := findDBTestCamera(t, realDB, ctx)

	handler := HandleComplianceReportPDF(realDB, nil) // nil cfg: manifest signing skipped in unit tests
	claims := complianceClaimsFor(orgID, "customer")
	w := httptest.NewRecorder()
	r := complianceRequest(http.MethodGet, "/api/v1/portal/compliance/report.pdf?period=week", claims)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/pdf" {
		t.Errorf("Content-Type: want application/pdf, got %q", ct)
	}

	body := w.Body.Bytes()
	if len(body) < 4 || string(body[:4]) != "%PDF" {
		t.Errorf("expected PDF magic bytes, got: %q", body[:minInt(8, len(body))])
	}

	reportID := w.Header().Get("X-Report-ID")
	if len(reportID) != 36 || strings.Count(reportID, "-") != 4 {
		t.Errorf("X-Report-ID is not a UUID: %q", reportID)
	}
}

// findDBTestCamera wraps findTestCamera for tests in package api.
// It delegates to the same DB query as database_test.findTestCamera.
func findDBTestCamera(t *testing.T, db *database.DB, ctx context.Context) (orgID string, camID string, siteID string) {
	t.Helper()
	row := db.Pool.QueryRow(ctx, `
		SELECT s.organization_id, c.id::text, s.id
		FROM cameras c
		JOIN sites s ON s.id = c.site_id
		LIMIT 1`)
	if err := row.Scan(&orgID, &camID, &siteID); err != nil {
		t.Skip("no camera with site assignment found; skipping compliance integration test")
	}
	if orgID == "" || siteID == "" {
		t.Skip("empty org or site ID")
	}
	return
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
