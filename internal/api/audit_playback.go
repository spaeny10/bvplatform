package api

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"onvif-tool/internal/auth"
	"onvif-tool/internal/database"
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

// clientIP extracts the best-effort source address of a request, preferring
// proxy-populated headers when present (ironsight runs behind nginx in prod).
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// Left-most is the original client.
		for i := 0; i < len(v); i++ {
			if v[i] == ',' {
				return trimSpace(v[:i])
			}
		}
		return trimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	return r.RemoteAddr
}
