package api

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// RecordingHealth is one row of the /api/recording/health response. Values are
// computed over a rolling 24-hour window so the dashboard shows "yesterday vs
// today"-style trends rather than since-install totals.
type RecordingHealth struct {
	CameraID       uuid.UUID  `json:"camera_id"`
	CameraName     string     `json:"camera_name"`
	SiteID         string     `json:"site_id,omitempty"`
	Recording      bool       `json:"recording"`
	RecorderType   string     `json:"recorder_type"`   // "ffmpeg" | "gort" | "off"
	Segments24h    int        `json:"segments_24h"`
	Bytes24h       int64      `json:"bytes_24h"`
	LastSegmentAt  *time.Time `json:"last_segment_at,omitempty"`
	LastGapSeconds float64    `json:"last_gap_seconds"` // seconds since last segment end; 0 if actively writing
	LongestGap24h  float64    `json:"longest_gap_seconds_24h"`
	Status         string     `json:"status"` // "healthy" | "degraded" | "stale" | "off"
}

// HandleRecordingHealth returns a per-camera recording health snapshot so
// operators and customers can spot silent failures (no segments in the last
// hour, long gaps, wrong recorder engine) without reading server logs.
//
// RBAC: same rules as the rest of the history endpoints — admins + SOC roles
// see all cameras, customer-side roles only their assigned ones.
func HandleRecordingHealth(db *database.DB) http.HandlerFunc {
	// Build the set of cameras routed to the Go recorder from env once; the
	// engine doesn't currently expose the mapping, and this env var is the
	// authoritative source at startup time.
	gortSet := parseGortSet(os.Getenv("GORT_CAMERAS"))

	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		allowed, restricted, err := AuthorizedCameraIDs(r.Context(), db, claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Fetch cameras. Customer-side callers only see their allowed set;
		// staff see everything (fleet view).
		cameras, err := db.ListCameras(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if restricted {
			allowSet := make(map[uuid.UUID]struct{}, len(allowed))
			for _, id := range allowed {
				allowSet[id] = struct{}{}
			}
			filtered := cameras[:0]
			for _, c := range cameras {
				if _, ok := allowSet[c.ID]; ok {
					filtered = append(filtered, c)
				}
			}
			cameras = filtered
		}

		windowStart := time.Now().Add(-24 * time.Hour)
		out := make([]RecordingHealth, 0, len(cameras))

		for _, c := range cameras {
			h := RecordingHealth{
				CameraID:   c.ID,
				CameraName: c.Name,
				Recording:  c.Recording,
			}

			// Recorder type: gort if the camera is in the opt-in env var,
			// ffmpeg otherwise, "off" if recording is disabled in settings.
			switch {
			case !c.Recording:
				h.RecorderType = "off"
			case gortMatches(gortSet, c.ID.String()):
				h.RecorderType = "gort"
			default:
				h.RecorderType = "ffmpeg"
			}

			// Aggregate segments in the last 24h for this camera in one round
			// trip. COALESCE the SUM to avoid NULL when there are no rows.
			var (
				count       int
				bytes       int64
				lastEnd     *time.Time
				longestGap  float64
			)
			err := db.Pool.QueryRow(r.Context(), `
				WITH seg AS (
					SELECT start_time, end_time, file_size,
					       LAG(end_time) OVER (ORDER BY start_time) AS prev_end
					FROM segments
					WHERE camera_id = $1 AND start_time >= $2
				)
				SELECT COUNT(*)::int,
				       COALESCE(SUM(file_size), 0)::bigint,
				       MAX(end_time),
				       COALESCE(MAX(EXTRACT(EPOCH FROM (start_time - prev_end))), 0)
				FROM seg`,
				c.ID, windowStart).
				Scan(&count, &bytes, &lastEnd, &longestGap)
			if err != nil {
				// Single-camera failure shouldn't break the whole response.
				h.Status = "unknown"
				out = append(out, h)
				continue
			}

			h.Segments24h = count
			h.Bytes24h = bytes
			h.LastSegmentAt = lastEnd
			h.LongestGap24h = longestGap
			if lastEnd != nil {
				h.LastGapSeconds = time.Since(*lastEnd).Seconds()
			} else {
				// No segments in the last 24h at all — treat as "forever ago".
				h.LastGapSeconds = 86400
			}

			// Traffic-light status. Tuned for 60s-nominal segments:
			//   healthy  = actively writing (last segment < 2 min ago)
			//   degraded = lagging (last segment 2-10 min ago) or gap > 2 min
			//   stale    = last segment > 10 min ago or zero segments
			//   off      = recording not enabled
			switch {
			case h.RecorderType == "off":
				h.Status = "off"
			case h.LastSegmentAt == nil:
				h.Status = "stale"
			case h.LastGapSeconds > 600:
				h.Status = "stale"
			case h.LastGapSeconds > 120 || h.LongestGap24h > 120:
				h.Status = "degraded"
			default:
				h.Status = "healthy"
			}

			out = append(out, h)
		}

		writeJSON(w, out)
	}
}

// parseGortSet splits the GORT_CAMERAS env var into a set of tokens. Tokens
// can be full UUIDs or 8-char prefixes; the caller checks via `hasPrefix`.
func parseGortSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out[tok] = struct{}{}
		}
	}
	return out
}

// gortMatches reports whether the camera UUID is in the GORT_CAMERAS env
// set, either as an exact match or as a prefix (matches how Engine.Start
// parses the same env var in internal/recording).
func gortMatches(set map[string]struct{}, id string) bool {
	if _, ok := set[id]; ok {
		return true
	}
	for k := range set {
		if len(k) < len(id) && strings.HasPrefix(id, k) {
			return true
		}
	}
	return false
}
