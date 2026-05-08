package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// HandleGetCoverage serves GET /api/timeline/coverage?start=&end=&camera_ids=id1,id2,...
// Returns lightweight segment spans so the frontend can draw recording coverage bars.
func HandleGetCoverage(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		start, err := time.Parse(time.RFC3339, q.Get("start"))
		if err != nil {
			http.Error(w, "invalid start", http.StatusBadRequest)
			return
		}
		end, err := time.Parse(time.RFC3339, q.Get("end"))
		if err != nil {
			http.Error(w, "invalid end", http.StatusBadRequest)
			return
		}

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

		var cameraIDs []uuid.UUID
		for _, raw := range splitComma(q.Get("camera_ids")) {
			id, err := uuid.Parse(raw)
			if err == nil {
				cameraIDs = append(cameraIDs, id)
			}
		}

		// Narrow to the caller's authorized cameras. If they asked for
		// specific ones, intersect; otherwise use their full allowed set.
		if restricted {
			if len(cameraIDs) == 0 {
				cameraIDs = allowed
			} else {
				cameraIDs = intersectUUIDs(cameraIDs, allowed)
			}
		}

		// If no camera_ids specified (and the caller has global view), return
		// empty — caller must be explicit to avoid dumping every camera.
		if len(cameraIDs) == 0 {
			writeJSON(w, []struct{}{})
			return
		}

		// Guard against absurdly large windows (max 7 days)
		if end.Sub(start) > 7*24*time.Hour {
			http.Error(w, "time range too large (max 7 days)", http.StatusBadRequest)
			return
		}

		spans, err := db.GetSegmentCoverage(r.Context(), cameraIDs, start, end)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		// Merge adjacent/overlapping spans per camera to reduce payload size.
		// Without this, a 1-hour window with 4 cameras can return 1MB+ of JSON.
		merged := mergeSpans(spans)

		if merged == nil {
			merged = []database.SegmentCoverage{} // return [] not null
		}
		writeJSON(w, merged)
	}
}

// mergeSpans coalesces adjacent or overlapping coverage spans per camera.
// Input must be sorted by camera_id, start_time (as from the DB query).
func mergeSpans(spans []database.SegmentCoverage) []database.SegmentCoverage {
	if len(spans) == 0 {
		return nil
	}

	var merged []database.SegmentCoverage
	current := spans[0]

	for i := 1; i < len(spans); i++ {
		s := spans[i]
		// Different camera — flush current, start new
		if s.CameraID != current.CameraID {
			merged = append(merged, current)
			current = s
			continue
		}
		// Same camera: merge if overlapping or adjacent (within 60s gap tolerance)
		// Segments are typically consecutive, so a small gap is normal
		if s.StartTime <= current.EndTime || timeDiffSeconds(current.EndTime, s.StartTime) < 60 {
			// Extend end time
			if s.EndTime > current.EndTime {
				current.EndTime = s.EndTime
			}
			current.HasAudio = current.HasAudio || s.HasAudio
		} else {
			// Gap too large — flush and start new
			merged = append(merged, current)
			current = s
		}
	}
	merged = append(merged, current)
	return merged
}

// timeDiffSeconds returns absolute seconds between two RFC3339 time strings.
func timeDiffSeconds(a, b string) float64 {
	ta, err1 := time.Parse(time.RFC3339, a)
	tb, err2 := time.Parse(time.RFC3339, b)
	if err1 != nil || err2 != nil {
		return 9999 // treat parse errors as large gap
	}
	diff := tb.Sub(ta).Seconds()
	if diff < 0 {
		return -diff
	}
	return diff
}

// splitComma splits a comma-separated string, trimming whitespace.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range splitString(s, ',') {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitString(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, trimSpace(s[start:]))
	return parts
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
