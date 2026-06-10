package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/database"
)

// auditPlayback writes one row to playback_audits describing a user's access
// to a recording resource. Runs in a goroutine so it never blocks the actual
// playback response; DB errors are swallowed intentionally — audit logging is
// a best-effort compliance trail, not a hard dependency.
//
// cameraID may be uuid.Nil, segmentID/eventID may be 0 when not applicable to
// the particular endpoint (e.g. coverage-range list has a camera but no
// specific segment or event).
func auditPlayback(
	db *database.DB,
	claims *auth.Claims,
	r *http.Request,
	endpoint string,
	cameraID uuid.UUID,
	segmentID int64,
	eventID int64,
) {
	if db == nil || claims == nil {
		return
	}
	// Snapshot fields before the goroutine so request lifecycle doesn't race.
	userID := claims.UserID
	username := claims.Username
	role := claims.Role
	ip := clientIP(r)

	var camArg any
	if cameraID != uuid.Nil {
		camArg = cameraID
	}
	var segArg any
	if segmentID != 0 {
		segArg = segmentID
	}
	var evArg any
	if eventID != 0 {
		evArg = eventID
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = db.Pool.Exec(ctx, `
			INSERT INTO playback_audits
			  (user_id, username, role, camera_id, segment_id, event_id, endpoint, ip)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			userID, username, role, camArg, segArg, evArg, endpoint, ip)
	}()
}

// clientIP extracts the source address of a request for security-sensitive
// uses (the login rate-limit key, failed-login audit rows, playback audits).
//
// F-15: NEVER trust the left-most X-Forwarded-For hop — that segment is
// client-supplied and arrives verbatim even behind our reverse proxy
// (NPM *appends* the transport peer, leaving any spoofed value left-most;
// chi's middleware.RealIP also copies the left-most hop into RemoteAddr
// without stripping the header). Keying the login rate limiter on it let
// an attacker reset the 10/min bucket by rotating XFF per request, and
// poisoned failed-login audit rows with forged source IPs.
//
// Trusted-proxy assumption (documented, deliberately simple): Ironsight's
// only production ingress is NPM, which appends the real transport peer
// as the RIGHT-most XFF hop. So:
//   - take the right-most XFF hop when the header is present;
//   - else X-Real-IP (set only by NPM; absent on direct connections);
//   - else the raw RemoteAddr (direct connection, dev/local).
//
// A client connecting directly to the API port can still forge these
// headers about itself, but it can no longer impersonate *other* clients
// going through the proxy, and rotating the spoofable left-most hop no
// longer changes the value we key on.
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// Right-most hop = the one appended by our own trusted proxy.
		if idx := strings.LastIndexByte(v, ','); idx >= 0 {
			v = v[idx+1:]
		}
		return trimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	return r.RemoteAddr
}
