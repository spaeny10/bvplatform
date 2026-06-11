package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ironsight/internal/auth"
)

// timeline_handler_test.go — regression coverage for the cross-camera
// timeline leak at the HTTP layer.
//
// HandleGetTimeline used to silently drop camera_ids values that did not
// parse as UUIDs. A request like `?camera_ids=5001` (a non-UUID) left an
// empty cameraIDs slice, which the DB layer then treated as "all cameras."
// The handler now returns 400 when a camera filter is supplied but nothing
// parses as a valid UUID, so the client bug is loud instead of leaking
// every camera's events.

func timelineRequest(url string, claims *auth.Claims) *http.Request {
	r := httptest.NewRequest(http.MethodGet, url, nil)
	if claims != nil {
		ctx := context.WithValue(r.Context(), ContextKeyClaims, claims)
		r = r.WithContext(ctx)
	}
	return r
}

// admin role is in globalViewRoles, so AuthorizedCameraIDs returns
// (nil, false, nil) without touching the DB — the 400 branch is reached
// before any db.* call, so a nil DB is safe here.
func timelineAdminClaims() *auth.Claims {
	return &auth.Claims{UserID: "admin-test", Role: "admin", Username: "admin@test"}
}

// TestHandleGetTimeline_Unauthed: no claims → 401.
func TestHandleGetTimeline_Unauthed(t *testing.T) {
	handler := HandleGetTimeline(nil)
	w := httptest.NewRecorder()
	r := timelineRequest("/api/timeline?camera_ids=5001", nil)
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

// TestHandleGetTimeline_BadCameraIDsRejected: a camera_ids param that
// contains only non-UUID values must yield 400 — NOT a silent empty filter
// that the DB would treat as "all cameras" (the cross-camera leak).
func TestHandleGetTimeline_BadCameraIDsRejected(t *testing.T) {
	handler := HandleGetTimeline(nil)
	claims := timelineAdminClaims()

	for _, url := range []string{
		"/api/timeline?camera_ids=5001",                 // single bad id (the live-repro case)
		"/api/timeline?camera_ids=5001,504,not-a-uuid",  // several bad ids
		"/api/timeline?camera_id=5001",                  // legacy single-id param, bad value
	} {
		w := httptest.NewRecorder()
		r := timelineRequest(url, claims)

		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("%s: unexpected panic (should 400 before any DB call): %v", url, rec)
			}
		}()

		handler.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400 for all-unparseable camera ids, got %d", url, w.Code)
		}
	}
}
