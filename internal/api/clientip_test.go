package api

// F-15 — clientIP must not trust the client-supplied (left-most)
// X-Forwarded-For hop for security-sensitive keys: the login rate
// limiter bucket and the failed-login / playback audit IP.
//
// Trusted-proxy model under test: NPM appends the real transport peer
// as the RIGHT-most XFF hop, so the right-most hop is the only one we
// honor. Rotating the left-most segment (the old bucket-reset attack)
// must not change the derived IP.
//
// Test map:
//   TestClientIP_RightMostXFFHop          — right-most hop wins, spoof ignored
//   TestClientIP_Fallbacks                — X-Real-IP then RemoteAddr
//   TestRateLimitLogin_XFFRotationNoReset — rotating left-most XFF can't
//       reset the 10/min login bucket

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP_RightMostXFFHop(t *testing.T) {
	cases := []struct {
		name string
		xff  string
		want string
	}{
		{"single hop", "203.0.113.7", "203.0.113.7"},
		{"spoofed left-most ignored", "6.6.6.6, 203.0.113.7", "203.0.113.7"},
		{"multi-hop takes right-most", "1.1.1.1, 2.2.2.2, 203.0.113.7", "203.0.113.7"},
		{"whitespace trimmed", "6.6.6.6 ,  203.0.113.7 ", "203.0.113.7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
			req.Header.Set("X-Forwarded-For", tc.xff)
			if got := clientIP(req); got != tc.want {
				t.Errorf("clientIP with XFF %q = %q, want %q", tc.xff, got, tc.want)
			}
		})
	}
}

func TestClientIP_Fallbacks(t *testing.T) {
	// No XFF → X-Real-IP (set by NPM).
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Real-IP", "203.0.113.9")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Errorf("clientIP X-Real-IP fallback = %q, want 203.0.113.9", got)
	}

	// No proxy headers at all → transport RemoteAddr.
	req = httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "192.0.2.4:51234"
	if got := clientIP(req); got != "192.0.2.4:51234" {
		t.Errorf("clientIP RemoteAddr fallback = %q, want 192.0.2.4:51234", got)
	}

	if got := clientIP(nil); got != "" {
		t.Errorf("clientIP(nil) = %q, want empty", got)
	}
}

// TestRateLimitLogin_XFFRotationNoReset reproduces the F-15 attack:
// an attacker rotating the left-most XFF segment per request. With the
// right-most-hop key, all requests from the same proxy-observed peer
// share one bucket, so the 11th request within the window must be 429.
func TestRateLimitLogin_XFFRotationNoReset(t *testing.T) {
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RateLimitLogin(10)(sentinel)

	for i := 0; i < 12; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		// Left-most hop rotates every request; right-most (the hop our
		// trusted proxy appended) stays constant.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("10.0.0.%d, 203.0.113.50", i))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		switch {
		case i < 10 && w.Code != http.StatusOK:
			t.Fatalf("request %d: got %d, want 200 (under limit)", i+1, w.Code)
		case i >= 10 && w.Code != http.StatusTooManyRequests:
			t.Fatalf("request %d: got %d, want 429 — rotating left-most XFF reset the bucket", i+1, w.Code)
		}
	}
}
