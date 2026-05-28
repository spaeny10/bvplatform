package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// allowedBucketMinutes is the set of bucket granularities the handler accepts.
// Only 5-minute buckets are written today; the others are reserved for future
// aggregator granularities. Rejecting unknown values prevents silent empty
// results when a caller passes an unsupported granularity.
var allowedBucketMinutes = map[int]bool{
	5: true, 15: true, 60: true,
}

// trackBucketResponse is the wire shape for one bucket in the response.
type trackBucketResponse struct {
	CameraID        string  `json:"camera_id"`
	CameraName      string  `json:"camera_name"`
	SiteID          *string `json:"site_id,omitempty"`
	SiteName        string  `json:"site_name,omitempty"`
	BucketStart     string  `json:"bucket_start"`
	BucketMinutes   int     `json:"bucket_minutes"`
	PersonMinutes   float64 `json:"person_minutes"`
	PeakPersonCount int     `json:"peak_person_count"`
	FrameCount      int     `json:"frame_count"`
	ViolationCount  int     `json:"violation_count"`
}

// trackBucketsEnvelope is the top-level response JSON.
type trackBucketsEnvelope struct {
	Buckets            []trackBucketResponse `json:"buckets"`
	TotalPersonMinutes float64               `json:"total_person_minutes"`
	TotalViolations    int                   `json:"total_violation_count"`
	PeriodStart        string                `json:"period_start"`
	PeriodEnd          string                `json:"period_end"`
}

// HandleGetPersonTracks handles GET /api/v1/portal/person-tracks.
//
// Auth: RequireAuth (JWT). All rows are scoped to claims.OrganizationID.
//
// Query params:
//
//	camera_id      UUID filter (optional)
//	site_id        TEXT filter (optional)
//	start          RFC3339; defaults to 7 days ago
//	end            RFC3339; defaults to now
//	bucket_minutes int; must be in allowedBucketMinutes; default 5
//	limit          int; max 2000; default 500
func HandleGetPersonTracks(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.OrganizationID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}

		q := r.URL.Query()

		// ── bucket_minutes ────────────────────────────────────────────
		bucketMinutes := 5
		if bm := q.Get("bucket_minutes"); bm != "" {
			n, err := strconv.Atoi(bm)
			if err != nil || !allowedBucketMinutes[n] {
				http.Error(w, "bucket_minutes must be one of: 5, 15, 60",
					http.StatusUnprocessableEntity)
				return
			}
			bucketMinutes = n
		}

		// ── time range ────────────────────────────────────────────────
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -7)
		end := now

		if s := q.Get("start"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				http.Error(w, "start: invalid RFC3339 timestamp", http.StatusBadRequest)
				return
			}
			start = t.UTC()
		}
		if e := q.Get("end"); e != "" {
			t, err := time.Parse(time.RFC3339, e)
			if err != nil {
				http.Error(w, "end: invalid RFC3339 timestamp", http.StatusBadRequest)
				return
			}
			end = t.UTC()
		}
		if !start.Before(end) {
			http.Error(w, "start must be before end", http.StatusBadRequest)
			return
		}

		// ── limit ─────────────────────────────────────────────────────
		limit := 500
		if l := q.Get("limit"); l != "" {
			n, err := strconv.Atoi(l)
			if err != nil || n <= 0 {
				http.Error(w, "limit: must be a positive integer", http.StatusBadRequest)
				return
			}
			limit = n
		}

		// ── optional filters ─────────────────────────────────────────
		f := database.TrackBucketFilter{
			OrganizationID: claims.OrganizationID,
			Start:          start,
			End:            end,
			BucketMinutes:  bucketMinutes,
			Limit:          limit,
		}

		if camStr := q.Get("camera_id"); camStr != "" {
			camID, err := uuid.Parse(camStr)
			if err != nil {
				http.Error(w, "camera_id: invalid UUID", http.StatusBadRequest)
				return
			}
			f.CameraID = &camID
		}
		if siteStr := q.Get("site_id"); siteStr != "" {
			s := siteStr
			f.SiteID = &s
		}

		// ── DB query ──────────────────────────────────────────────────
		buckets, err := db.ListTrackBuckets(r.Context(), f)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if buckets == nil {
			buckets = []database.PersonTrackBucket{}
		}

		// ── shape response ────────────────────────────────────────────
		resp := make([]trackBucketResponse, 0, len(buckets))
		var totalPersonMinutes float64
		var totalViolations int

		for _, b := range buckets {
			totalPersonMinutes += b.PersonMinutes
			totalViolations += b.ViolationCount

			row := trackBucketResponse{
				CameraID:        b.CameraID.String(),
				CameraName:      b.CameraName,
				SiteID:          b.SiteID,
				SiteName:        b.SiteName,
				BucketStart:     b.BucketStart.UTC().Format(time.RFC3339),
				BucketMinutes:   b.BucketMinutes,
				PersonMinutes:   b.PersonMinutes,
				PeakPersonCount: b.PeakPersonCount,
				FrameCount:      b.FrameCount,
				ViolationCount:  b.ViolationCount,
			}
			resp = append(resp, row)
		}

		writeJSON(w, trackBucketsEnvelope{
			Buckets:            resp,
			TotalPersonMinutes: totalPersonMinutes,
			TotalViolations:    totalViolations,
			PeriodStart:        start.Format(time.RFC3339),
			PeriodEnd:          end.Format(time.RFC3339),
		})
	}
}
