package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
)

// HandleQueryEvents returns filtered events for a time range. Enforces
// per-user site-based visibility: admins / SOC roles see all cameras,
// customer-side roles only see events from their assigned cameras.
func HandleQueryEvents(db *database.DB) http.HandlerFunc {
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

		q := parseEventQuery(r)
		if restricted {
			// If the request explicitly asked for a camera_id, ensure it's in
			// the allowed set; otherwise clear it and fall back to the ANY()
			// whitelist below. A non-allowed explicit camera → zero rows.
			if q.CameraID != nil {
				if !containsUUID(allowed, *q.CameraID) {
					writeJSON(w, []database.Event{})
					return
				}
			} else {
				q.CameraIDs = allowed
				q.CameraIDsNonNil = true
			}
		}

		events, err := db.QueryEvents(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []database.Event{}
		}
		writeJSON(w, events)
	}
}

// HandleGetTimeline returns bucketed event counts for the timeline scrubber
// UI, filtered by the caller's authorized cameras.
func HandleGetTimeline(db *database.DB) http.HandlerFunc {
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

		start, end := parseTimeRange(r)

		var cameraIDs []uuid.UUID

		// Support comma-separated camera_ids param
		if idsStr := r.URL.Query().Get("camera_ids"); idsStr != "" {
			for _, s := range strings.Split(idsStr, ",") {
				s = strings.TrimSpace(s)
				if cid, err := uuid.Parse(s); err == nil {
					cameraIDs = append(cameraIDs, cid)
				}
			}
		} else if cidStr := r.URL.Query().Get("camera_id"); cidStr != "" {
			// Backwards compat: single camera_id
			if cid, err := uuid.Parse(cidStr); err == nil {
				cameraIDs = append(cameraIDs, cid)
			}
		}

		// Narrow requested cameras to the caller's authorized set.
		if restricted {
			if len(cameraIDs) == 0 {
				cameraIDs = allowed
			} else {
				cameraIDs = intersectUUIDs(cameraIDs, allowed)
			}
			if len(cameraIDs) == 0 {
				writeJSON(w, []database.TimelineBucket{})
				return
			}
		}

		intervalStr := r.URL.Query().Get("interval")
		interval := 1 // default 1 minute
		if intervalStr != "" {
			if v, err := strconv.Atoi(intervalStr); err == nil && v > 0 {
				interval = v
			}
		}

		buckets, err := db.GetTimelineBuckets(r.Context(), cameraIDs, start, end, interval)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if buckets == nil {
			buckets = []database.TimelineBucket{}
		}
		writeJSON(w, buckets)
	}
}

// HandleGetRecordings returns available recording segments for a camera in a
// time range, after verifying the caller has access to that camera.
func HandleGetRecordings(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ok, err := CanAccessCamera(r.Context(), db, claims, cameraID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			// 404 rather than 403 so unauthorized callers can't probe for
			// camera-UUID existence via status-code differences.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		start, end := parseTimeRange(r)

		segments, err := db.GetSegments(r.Context(), cameraID, start, end)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if segments == nil {
			segments = []database.Segment{}
		}
		// Audit: operator/customer viewed the list of recordings for this
		// camera. No specific segment yet — they pick from the list next.
		auditPlayback(db, claims, r, "GET /api/cameras/{id}/recordings", cameraID, 0, 0)
		writeJSON(w, segments)
	}
}

// SearchEventsResponse wraps HandleSearchEvents's output with meta useful for
// the SOC / customer history UI. Each event already has PlaybackURL populated
// by QueryEvents's JOIN; the meta fields let the caller paginate and know
// how many total events match without a second query.
type SearchEventsResponse struct {
	Events     []database.Event `json:"events"`
	NextOffset int              `json:"next_offset"`
	HasMore    bool             `json:"has_more"`
	// AuthorizedCameras surfaces the caller's accessible camera UUIDs so the
	// frontend can render a cameras filter without an extra roundtrip. Empty
	// for callers with global view (admin / SOC) — they can see everything.
	AuthorizedCameras []uuid.UUID `json:"authorized_cameras,omitempty"`
	Restricted        bool        `json:"restricted"`
}

// HandleSearchEvents is the unified historical-search endpoint. It runs the
// same query as /api/events but wraps the output so a single frontend fetch
// yields playable rows: each event record carries the segment path + seek
// offset. RBAC matches /api/events — customer-side roles only see events on
// their assigned cameras.
func HandleSearchEvents(db *database.DB) http.HandlerFunc {
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

		q := parseEventQuery(r)
		if restricted {
			if q.CameraID != nil {
				if !containsUUID(allowed, *q.CameraID) {
					writeJSON(w, SearchEventsResponse{
						Events:            []database.Event{},
						Restricted:        true,
						AuthorizedCameras: allowed,
					})
					return
				}
			} else {
				q.CameraIDs = allowed
				q.CameraIDsNonNil = true
			}
		}

		// Request one extra row so we can report has_more without a COUNT.
		requested := q.Limit
		if requested <= 0 || requested > 500 {
			requested = 50
		}
		q.Limit = requested + 1

		events, err := db.QueryEvents(r.Context(), q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hasMore := len(events) > requested
		if hasMore {
			events = events[:requested]
		}
		if events == nil {
			events = []database.Event{}
		}

		resp := SearchEventsResponse{
			Events:     events,
			NextOffset: q.Offset + len(events),
			HasMore:    hasMore,
			Restricted: restricted,
		}
		if restricted {
			resp.AuthorizedCameras = allowed
		}
		writeJSON(w, resp)
	}
}

// containsUUID reports whether id is present in haystack. Used for single-ID
// authorization checks.
func containsUUID(haystack []uuid.UUID, id uuid.UUID) bool {
	for _, h := range haystack {
		if h == id {
			return true
		}
	}
	return false
}

// intersectUUIDs returns the UUIDs present in both slices, preserving the
// order of the first input.
func intersectUUIDs(a, b []uuid.UUID) []uuid.UUID {
	set := make(map[uuid.UUID]struct{}, len(b))
	for _, x := range b {
		set[x] = struct{}{}
	}
	out := make([]uuid.UUID, 0, len(a))
	for _, x := range a {
		if _, ok := set[x]; ok {
			out = append(out, x)
		}
	}
	return out
}

func parseEventQuery(r *http.Request) database.EventQuery {
	start, end := parseTimeRange(r)

	q := database.EventQuery{
		StartTime: start,
		EndTime:   end,
		Search:    r.URL.Query().Get("search"),
	}

	if cidStr := r.URL.Query().Get("camera_id"); cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			q.CameraID = &cid
		}
	}

	if types := r.URL.Query().Get("types"); types != "" {
		q.EventTypes = strings.Split(types, ",")
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			q.Limit = l
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			q.Offset = o
		}
	}

	return q
}

func parseTimeRange(r *http.Request) (time.Time, time.Time) {
	end := time.Now()
	start := end.Add(-1 * time.Hour) // default: last hour

	if s := r.URL.Query().Get("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			start = t
		}
	}
	if e := r.URL.Query().Get("end"); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			end = t
		}
	}
	return start, end
}

// HandleCreateExport starts a new video export job
func HandleCreateExport(db *database.DB) http.HandlerFunc {
	type exportRequest struct {
		CameraID  string `json:"camera_id"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req exportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		cameraID, err := uuid.Parse(req.CameraID)
		if err != nil {
			http.Error(w, "invalid camera_id", http.StatusBadRequest)
			return
		}

		startTime, err := time.Parse(time.RFC3339, req.StartTime)
		if err != nil {
			http.Error(w, "invalid start_time (use RFC3339)", http.StatusBadRequest)
			return
		}

		endTime, err := time.Parse(time.RFC3339, req.EndTime)
		if err != nil {
			http.Error(w, "invalid end_time (use RFC3339)", http.StatusBadRequest)
			return
		}

		export := &database.Export{
			CameraID:  cameraID,
			StartTime: startTime,
			EndTime:   endTime,
		}

		if err := db.CreateExport(r.Context(), export); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, export)
	}
}

// HandleListExports returns recent export jobs
func HandleListExports(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		exports, err := db.ListExports(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if exports == nil {
			exports = []database.Export{}
		}
		writeJSON(w, exports)
	}
}
