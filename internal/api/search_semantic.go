package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// SemanticMatch is one row of /api/search/semantic output — a segment whose
// VLM-generated description / tags matched the query, plus enough context
// to play it directly (reusing the same /recordings/<cam>/<seg>#t= pattern
// the rest of the history UI uses).
type SemanticMatch struct {
	SegmentID     int64     `json:"segment_id"`
	CameraID      uuid.UUID `json:"camera_id"`
	CameraName    string    `json:"camera_name"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	Description   string    `json:"description"`
	Tags          []string  `json:"tags"`
	ActivityLevel string    `json:"activity_level"`
	PlaybackURL   string    `json:"playback_url"`
	Rank          float64   `json:"rank"`
}

// SemanticSearchResponse wraps a page of matches plus the total count so a
// client can paginate without an extra COUNT query.
type SemanticSearchResponse struct {
	Query             string          `json:"query"`
	Results           []SemanticMatch `json:"results"`
	Total             int             `json:"total"`
	NextOffset        int             `json:"next_offset"`
	HasMore           bool            `json:"has_more"`
	Restricted        bool            `json:"restricted"`
	AuthorizedCameras []uuid.UUID     `json:"authorized_cameras,omitempty"`
}

// HandleSemanticSearch runs a full-text query against segment_descriptions
// populated by the background indexer. Supports:
//   - q      : free-text query (required). Uses websearch_to_tsquery so users
//              can type natural phrases like "person with red jacket".
//   - start, end : optional ISO8601 time bounds.
//   - camera_id  : optional single camera filter.
//   - activity   : optional min activity level (low|moderate|high).
//   - limit, offset : pagination.
//
// RBAC: restricted roles only see matches on their authorized cameras.
// Empty-query responses return a helpful error rather than all rows.
func HandleSemanticSearch(db *database.DB) http.HandlerFunc {
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

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			http.Error(w, `query "q" required`, http.StatusBadRequest)
			return
		}

		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		offset := 0
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		// Build a parameterized WHERE. We stack clauses into a slice so each
		// optional filter only contributes a predicate when set.
		where := []string{
			// websearch_to_tsquery accepts natural-language input including
			// quoted phrases ("red jacket") and OR operators. More forgiving
			// than plainto_tsquery and more powerful than to_tsquery.
			`(to_tsvector('english', d.description) @@ websearch_to_tsquery('english', $1)
			  OR d.tags && string_to_array(lower($1), ' '))`,
		}
		args := []interface{}{q}
		argN := 2

		if s := r.URL.Query().Get("start"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				where = append(where, fmt.Sprintf("s.start_time >= $%d", argN))
				args = append(args, t)
				argN++
			}
		}
		if e := r.URL.Query().Get("end"); e != "" {
			if t, err := time.Parse(time.RFC3339, e); err == nil {
				where = append(where, fmt.Sprintf("s.end_time <= $%d", argN))
				args = append(args, t)
				argN++
			}
		}
		if cid := r.URL.Query().Get("camera_id"); cid != "" {
			if id, err := uuid.Parse(cid); err == nil {
				if restricted && !containsUUID(allowed, id) {
					writeJSON(w, SemanticSearchResponse{Query: q, Restricted: true, AuthorizedCameras: allowed})
					return
				}
				where = append(where, fmt.Sprintf("d.camera_id = $%d", argN))
				args = append(args, id)
				argN++
			}
		}
		if activity := r.URL.Query().Get("activity"); activity != "" {
			// Maps low→all levels ≥ low, moderate→mod+high, high→high only.
			switch activity {
			case "low":
				where = append(where, "d.activity_level IN ('low','moderate','high')")
			case "moderate":
				where = append(where, "d.activity_level IN ('moderate','high')")
			case "high":
				where = append(where, "d.activity_level = 'high'")
			}
		}

		// RBAC whitelist. Like /api/events, an empty allowlist for a
		// restricted caller means "return nothing" — not "no filter".
		if restricted {
			if len(allowed) == 0 {
				writeJSON(w, SemanticSearchResponse{Query: q, Restricted: true})
				return
			}
			where = append(where, fmt.Sprintf("d.camera_id = ANY($%d)", argN))
			args = append(args, allowed)
			argN++
		}

		// Request one extra row so the client can flag has_more without a
		// second COUNT(*) query.
		args = append(args, limit+1, offset)
		limitArg := argN
		offsetArg := argN + 1

		query := fmt.Sprintf(`
			SELECT d.segment_id, d.camera_id, c.name, s.start_time, s.end_time,
			       d.description, d.tags, d.activity_level,
			       ts_rank(to_tsvector('english', d.description),
			               websearch_to_tsquery('english', $1)) AS rank,
			       s.file_path
			FROM segment_descriptions d
			JOIN segments s ON s.id = d.segment_id
			JOIN cameras c ON c.id = d.camera_id
			WHERE %s
			ORDER BY rank DESC, s.start_time DESC
			LIMIT $%d OFFSET $%d`,
			strings.Join(where, " AND "), limitArg, offsetArg)

		rows, err := db.Pool.Query(r.Context(), query, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		results := make([]SemanticMatch, 0, limit)
		for rows.Next() {
			var m SemanticMatch
			var filePath string
			if err := rows.Scan(&m.SegmentID, &m.CameraID, &m.CameraName,
				&m.StartTime, &m.EndTime, &m.Description, &m.Tags,
				&m.ActivityLevel, &m.Rank, &filePath); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Playback URL: open the segment at its beginning. The caller
			// typically refines with the segment timestamp range.
			m.PlaybackURL = buildSegmentURL(m.CameraID.String(), filePath)
			results = append(results, m)
		}

		hasMore := len(results) > limit
		if hasMore {
			results = results[:limit]
		}

		resp := SemanticSearchResponse{
			Query:      q,
			Results:    results,
			Total:      len(results),
			NextOffset: offset + len(results),
			HasMore:    hasMore,
			Restricted: restricted,
		}
		if restricted {
			resp.AuthorizedCameras = allowed
		}
		writeJSON(w, resp)
	}
}

// buildSegmentURL trims the absolute file path to the /recordings/<cam>/<file>
// form the static handler serves. No seek offset; the player starts at 0 and
// the user can scrub.
func buildSegmentURL(cameraID, filePath string) string {
	base := filePath
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' || base[i] == '\\' {
			base = base[i+1:]
			break
		}
	}
	return "/recordings/" + cameraID + "/" + base
}
