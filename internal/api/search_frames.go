package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/database"
)

// SearchFiltersRequest matches the shape the /search page POSTs. Kept
// permissive — any field may be missing — so the unified-search endpoint
// can serve both safety filters (violation_types) and security filters
// (no violation filter = all alarms) without separate endpoints.
type SearchFiltersRequest struct {
	Query          string     `json:"query"`
	ViolationTypes []string   `json:"violation_types,omitempty"`
	SiteIDs        []string   `json:"site_ids,omitempty"`
	DateRange      *DateRange `json:"date_range,omitempty"`
	ConfidenceMin  float64    `json:"confidence_min,omitempty"`
	TimeOfDay      *TimeOfDay `json:"time_of_day,omitempty"`
	Model          string     `json:"model,omitempty"` // "hybrid" | "visual" | "caption"
}

type DateRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type TimeOfDay struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// SearchFrameResult matches the frontend's SearchResult type exactly so the
// existing /search page renders the response with no mapping layer.
type SearchFrameResult struct {
	FrameID        string                  `json:"frame_id"`
	SiteID         string                  `json:"site_id"`
	SiteName       string                  `json:"site_name"`
	CameraID       string                  `json:"camera_id"`
	CameraName     string                  `json:"camera_name"`
	Ts             int64                   `json:"ts"` // unix millis — frontend uses number
	RelevanceScore float64                 `json:"relevance_score"`
	Caption        string                  `json:"caption"`
	ThumbnailURL   string                  `json:"thumbnail_url"`
	ClipURL        string                  `json:"clip_url"`
	Detections     []SearchFrameDetection  `json:"detections"`
	ViolationFlags map[string]bool         `json:"violation_flags"`
	TokenMatches   []SearchFrameTokenMatch `json:"token_matches"`
	Source         string                  `json:"source"` // "vlm_segment" | "security_alarm" | "safety_alarm"
}

type SearchFrameDetection struct {
	// JSON key is `class` (not `label`) to match the frontend's shared
	// `Detection` type. Go field name stays as Label for readability.
	Label     string  `json:"class"`
	BBox      [4]int  `json:"bbox"` // [x1, y1, x2, y2]
	Violation bool    `json:"violation"`
	Conf      float64 `json:"confidence"`
}

type SearchFrameTokenMatch struct {
	Token  string  `json:"token"`
	Score  float64 `json:"score"`
	Source string  `json:"source"` // "visual" | "caption"
}

// ppeViolationLabelMap translates the YOLO PPE class names the indexer stores
// (e.g. "no-hardhat", "nohat", "novest") to the stable UI keys the /search
// page uses for its checkbox filters. Matches the frontend enum.
var ppeViolationLabelMap = map[string]string{
	"nohat": "no_hard_hat", "no-hat": "no_hard_hat", "no_hat": "no_hard_hat",
	"no-hardhat": "no_hard_hat", "no_hardhat": "no_hard_hat", "no-hard-hat": "no_hard_hat",
	"novest": "no_hi_vis", "no-vest": "no_hi_vis", "no_vest": "no_hi_vis",
	"no-hivis": "no_hi_vis", "no_hivis": "no_hi_vis", "no-hi-vis": "no_hi_vis",
	"noharness": "no_harness", "no-harness": "no_harness", "no_harness": "no_harness",
}

// HandleSearchFrames is the unified safety + security search used by the
// /search page. It UNIONs two sources:
//
//  1. VLM-described recording segments (segment_descriptions) — covers every
//     indexed minute of recorded footage, searchable by natural language
//     ("white van", "ladder", "person with backpack").
//  2. SOC alarms (active_alarms) — every triggered alarm with AI enrichment,
//     including PPE violations. Allows structured filters (violation_types)
//     that FTS alone can't cleanly express.
//
// RBAC-scoped identical to the rest of the history endpoints.
func HandleSearchFrames(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req SearchFiltersRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		q := strings.TrimSpace(req.Query)
		if q == "" && len(req.ViolationTypes) == 0 {
			// No query + no structured filter = nothing to rank. Empty array,
			// not error — matches frontend expectation of empty results view.
			writeJSON(w, []SearchFrameResult{})
			return
		}

		allowed, restricted, err := AuthorizedCameraIDs(r.Context(), db, claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if restricted && len(allowed) == 0 {
			writeJSON(w, []SearchFrameResult{})
			return
		}

		start, end := parseDateRange(req.DateRange)

		// ── Part 1: VLM segment descriptions (FTS rank) ──
		vlmResults, err := searchVLMSegments(r, db, q, req, allowed, restricted, start, end)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// ── Part 2: SOC alarms (safety + security) ──
		alarmResults, err := searchAlarms(r, db, q, req, allowed, restricted, start, end)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Merge VLM + alarm hits.
		all := append(vlmResults, alarmResults...)

		// Min-confidence filter. Applied as an either-or gate:
		//   (a) relevance_score ≥ min (FTS / AI-certainty threshold), OR
		//   (b) at least one detection has confidence ≥ min (visual certainty).
		// A row passes if EITHER bar clears the threshold — so the filter
		// catches "strong textual match" AND "clear visual detection" without
		// discarding rows that only qualify on one axis.
		if req.ConfidenceMin > 0 {
			kept := all[:0]
			for _, r := range all {
				if r.RelevanceScore >= req.ConfidenceMin {
					kept = append(kept, r)
					continue
				}
				for _, d := range r.Detections {
					if d.Conf >= req.ConfidenceMin {
						kept = append(kept, r)
						break
					}
				}
			}
			all = kept
		}

		// Sort by relevance_score DESC, cap at 100.
		for i := range all {
			for j := i + 1; j < len(all); j++ {
				if all[j].RelevanceScore > all[i].RelevanceScore {
					all[i], all[j] = all[j], all[i]
				}
			}
		}
		if len(all) > 100 {
			all = all[:100]
		}
		if all == nil {
			all = []SearchFrameResult{}
		}
		writeJSON(w, all)
	}
}

// searchVLMSegments ranks segment_descriptions rows by FTS over description
// plus tag-overlap bonus. Returns zero rows if q is empty (tags-only filter
// would need a different path — not today's scope).
func searchVLMSegments(
	r *http.Request, db *database.DB,
	q string, req SearchFiltersRequest,
	allowed []uuid.UUID, restricted bool,
	start, end time.Time,
) ([]SearchFrameResult, error) {
	if q == "" {
		return nil, nil
	}

	// Search model gates which match path contributes:
	//   hybrid  (default) — description FTS OR tag overlap
	//   caption           — description FTS only (no YOLO/VLM tag shortcut)
	//   visual            — tag overlap only (no natural-language matching)
	var matchClause string
	switch req.Model {
	case "caption":
		matchClause = `(to_tsvector('english', d.description) @@ websearch_to_tsquery('english', $1))`
	case "visual":
		matchClause = `(d.tags && string_to_array(lower($1), ' '))`
	default:
		matchClause = `(to_tsvector('english', d.description) @@ websearch_to_tsquery('english', $1)
		               OR d.tags && string_to_array(lower($1), ' '))`
	}
	where := []string{matchClause}
	args := []interface{}{q}
	argN := 2

	if !start.IsZero() {
		where = append(where, fmt.Sprintf("s.start_time >= $%d", argN))
		args = append(args, start)
		argN++
	}
	if !end.IsZero() {
		where = append(where, fmt.Sprintf("s.end_time <= $%d", argN))
		args = append(args, end)
		argN++
	}
	// Time-of-day post-filter: SQL EXTRACT(hour) over start_time. Handles
	// both normal ranges ("08:00 → 17:00") and overnight ranges that wrap
	// across midnight ("20:00 → 06:00") by OR-ing the two halves.
	if req.TimeOfDay != nil {
		sh, eh, ok := parseTimeOfDay(req.TimeOfDay)
		if ok {
			if sh <= eh {
				where = append(where, fmt.Sprintf(
					"EXTRACT(hour FROM s.start_time AT TIME ZONE 'UTC') BETWEEN $%d AND $%d", argN, argN+1))
				args = append(args, sh, eh)
				argN += 2
			} else {
				where = append(where, fmt.Sprintf(
					"(EXTRACT(hour FROM s.start_time AT TIME ZONE 'UTC') >= $%d OR EXTRACT(hour FROM s.start_time AT TIME ZONE 'UTC') <= $%d)",
					argN, argN+1))
				args = append(args, sh, eh)
				argN += 2
			}
		}
	}
	// Site filter: match cameras.site_id against the ID list the UI sent.
	// When the dropdown has stale mock IDs that don't match real data, this
	// correctly returns zero rows — a visible failure the operator can see
	// and report instead of silently ignoring their choice.
	if len(req.SiteIDs) > 0 {
		where = append(where, fmt.Sprintf("c.site_id = ANY($%d)", argN))
		args = append(args, req.SiteIDs)
		argN++
	}
	if restricted {
		where = append(where, fmt.Sprintf("d.camera_id = ANY($%d)", argN))
		args = append(args, allowed)
		argN++
	}

	query := fmt.Sprintf(`
		SELECT d.segment_id, d.camera_id, c.name AS camera_name,
		       COALESCE(c.site_id, '') AS site_id, s.start_time, s.end_time,
		       d.description, d.tags, d.detections, s.file_path,
		       ts_rank(to_tsvector('english', d.description),
		               websearch_to_tsquery('english', $1)) AS rank
		FROM segment_descriptions d
		JOIN segments s ON s.id = d.segment_id
		JOIN cameras c ON c.id = d.camera_id
		WHERE %s
		ORDER BY rank DESC, s.start_time DESC
		LIMIT 50`, strings.Join(where, " AND "))

	rows, err := db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchFrameResult
	for rows.Next() {
		var (
			segID       int64
			cameraID    uuid.UUID
			cameraName  string
			siteID      string
			startTime   time.Time
			endTime     time.Time
			description string
			tags        []string
			detectJSON  []byte
			filePath    string
			rank        float64
		)
		if err := rows.Scan(&segID, &cameraID, &cameraName, &siteID,
			&startTime, &endTime, &description, &tags, &detectJSON, &filePath, &rank); err != nil {
			return nil, err
		}

		// ts_rank is unbounded; normalize with a simple squash so it sits
		// roughly in [0, 1] for the UI's relevance-score bar.
		rankN := rank / (rank + 0.5)
		if rankN < 0.15 {
			rankN = 0.15
		}

		// Seek the clip to the moment the VLM actually described. The
		// indexer samples its YOLO-gate frame and its 8s describe window
		// centered on mid-segment, so mid-segment is what the tags and
		// description refer to. Without #t=, users would see frame 0 of
		// a 60s segment — frequently a different scene than the one the
		// tags match. Clamp to 0 for safety on zero-duration rows.
		offset := endTime.Sub(startTime).Seconds() / 2
		if offset < 0 {
			offset = 0
		}
		clipURL := "/recordings/" + cameraID.String() + "/" + filepath.Base(filePath)
		if offset > 0 {
			clipURL = fmt.Sprintf("%s#t=%.1f", clipURL, offset)
		}

		result := SearchFrameResult{
			FrameID:        fmt.Sprintf("seg-%d", segID),
			SiteID:         siteID,
			SiteName:       siteID, // no site_name lookup today; same string is fine
			CameraID:       cameraID.String(),
			CameraName:     cameraName,
			Ts:             startTime.Add(time.Duration(offset * float64(time.Second))).UnixMilli(),
			RelevanceScore: rankN,
			Caption:        description,
			ClipURL:        clipURL,
			Detections:     parseYOLODetections(detectJSON),
			ViolationFlags: tagsToViolationFlags(tags),
			TokenMatches:   tokensMatchingQuery(q, tags),
			Source:         "vlm_segment",
		}
		// Apply the violation-type checkbox filter post-query. Cheap because
		// we've already capped to 50 rows at the DB layer.
		if !matchesViolationFilter(result.ViolationFlags, req.ViolationTypes) {
			continue
		}
		out = append(out, result)
	}
	return out, nil
}

// searchAlarms ranks active_alarms rows by FTS on ai_description and by PPE
// violation overlap. This is the bridge between keyword search and the SOC
// queue — the same events that fire alarms should be findable by keyword.
func searchAlarms(
	r *http.Request, db *database.DB,
	q string, req SearchFiltersRequest,
	allowed []uuid.UUID, restricted bool,
	start, end time.Time,
) ([]SearchFrameResult, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}
	argN := 1

	// Query filter: match against AI description + alarm type + description
	// fields. Use ILIKE to avoid tsvector setup; alarms have lower volume
	// than segment descriptions so full-scan is fine.
	if q != "" {
		where = append(where, fmt.Sprintf(
			"(ai_description ILIKE $%d OR description ILIKE $%d OR type ILIKE $%d)",
			argN, argN, argN))
		args = append(args, "%"+q+"%")
		argN++
	}
	if !start.IsZero() {
		where = append(where, fmt.Sprintf("to_timestamp(ts/1000) >= $%d", argN))
		args = append(args, start)
		argN++
	}
	if !end.IsZero() {
		where = append(where, fmt.Sprintf("to_timestamp(ts/1000) <= $%d", argN))
		args = append(args, end)
		argN++
	}
	// Time-of-day filter against the alarm's firing timestamp.
	if req.TimeOfDay != nil {
		sh, eh, ok := parseTimeOfDay(req.TimeOfDay)
		if ok {
			if sh <= eh {
				where = append(where, fmt.Sprintf(
					"EXTRACT(hour FROM to_timestamp(ts/1000)) BETWEEN $%d AND $%d", argN, argN+1))
				args = append(args, sh, eh)
				argN += 2
			} else {
				where = append(where, fmt.Sprintf(
					"(EXTRACT(hour FROM to_timestamp(ts/1000)) >= $%d OR EXTRACT(hour FROM to_timestamp(ts/1000)) <= $%d)",
					argN, argN+1))
				args = append(args, sh, eh)
				argN += 2
			}
		}
	}
	if len(req.SiteIDs) > 0 {
		where = append(where, fmt.Sprintf("site_id = ANY($%d)", argN))
		args = append(args, req.SiteIDs)
		argN++
	}
	if restricted {
		// camera_id is stored as TEXT in active_alarms, so cast to match.
		where = append(where, fmt.Sprintf("camera_id::uuid = ANY($%d)", argN))
		args = append(args, allowed)
		argN++
	}

	query := fmt.Sprintf(`
		SELECT id, camera_id, camera_name, site_id, site_name, ts, type,
		       description, COALESCE(ai_description, ''),
		       COALESCE(ai_ppe_violations, '[]'::jsonb),
		       COALESCE(ai_detections, '[]'::jsonb),
		       COALESCE(snapshot_url, ''), COALESCE(clip_url, ''),
		       COALESCE(ai_false_positive_pct, 0)
		FROM active_alarms
		WHERE %s
		ORDER BY ts DESC
		LIMIT 50`, strings.Join(where, " AND "))

	rows, err := db.Pool.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchFrameResult
	for rows.Next() {
		var (
			id, cameraID, cameraName, siteID, siteName string
			ts                                          int64
			alarmType, description, aiDesc              string
			ppeJSON, detJSON                            []byte
			snapshotURL, clipURL                        string
			fpPct                                       float64
		)
		if err := rows.Scan(&id, &cameraID, &cameraName, &siteID, &siteName,
			&ts, &alarmType, &description, &aiDesc, &ppeJSON, &detJSON,
			&snapshotURL, &clipURL, &fpPct); err != nil {
			return nil, err
		}

		caption := aiDesc
		if caption == "" {
			caption = description
		}

		violationFlags := ppeFromJSON(ppeJSON)
		if !matchesViolationFilter(violationFlags, req.ViolationTypes) {
			continue
		}

		source := "security_alarm"
		if len(violationFlags) > 0 {
			source = "safety_alarm"
		}

		// Relevance: invert false-positive probability. An alarm with fp=0%
		// is highly relevant; fp=50% is marginal. Query-matching alarms get
		// a small bonus so keyword hits outrank generic-relevance.
		relevance := 1.0 - fpPct
		if relevance < 0.3 {
			relevance = 0.3
		}
		if q != "" {
			relevance = relevance*0.6 + 0.4 // stronger when user-queried
		}

		out = append(out, SearchFrameResult{
			FrameID:        "alarm-" + id,
			SiteID:         siteID,
			SiteName:       siteName,
			CameraID:       cameraID,
			CameraName:     cameraName,
			Ts:             ts,
			RelevanceScore: relevance,
			Caption:        caption,
			ThumbnailURL:   snapshotURL,
			ClipURL:        clipURL,
			Detections:     parseYOLODetections(detJSON),
			ViolationFlags: violationFlags,
			TokenMatches:   alarmTokenMatches(q, alarmType, violationFlags),
			Source:         source,
		})
	}
	return out, nil
}

// ── helpers ──

// parseTimeOfDay turns the "HH:MM" inputs from the time-of-day picker into
// two integer hours (0-23). Returns ok=false if both fields are blank or
// unparseable so the caller can skip the filter cleanly.
func parseTimeOfDay(t *TimeOfDay) (int, int, bool) {
	if t == nil {
		return 0, 0, false
	}
	hour := func(s string) (int, bool) {
		if len(s) < 2 {
			return 0, false
		}
		var h int
		_, err := fmt.Sscanf(s, "%d", &h)
		if err != nil || h < 0 || h > 23 {
			return 0, false
		}
		return h, true
	}
	sh, sok := hour(t.Start)
	eh, eok := hour(t.End)
	if !sok && !eok {
		return 0, 0, false
	}
	if !sok {
		sh = 0
	}
	if !eok {
		eh = 23
	}
	return sh, eh, true
}

func parseDateRange(dr *DateRange) (time.Time, time.Time) {
	var start, end time.Time
	if dr == nil {
		return start, end
	}
	if dr.Start != "" {
		if t, err := time.Parse("2006-01-02", dr.Start); err == nil {
			start = t
		} else if t, err := time.Parse(time.RFC3339, dr.Start); err == nil {
			start = t
		}
	}
	if dr.End != "" {
		if t, err := time.Parse("2006-01-02", dr.End); err == nil {
			// End-of-day for a date-only input; users expect "Oct 5" to
			// include Oct 5 23:59.
			end = t.Add(24 * time.Hour)
		} else if t, err := time.Parse(time.RFC3339, dr.End); err == nil {
			end = t
		}
	}
	return start, end
}

// parseYOLODetections converts the JSONB-stored YOLO detection list into the
// frontend's Detection shape (which expects [x1, y1, x2, y2] + label + conf).
// The storage format from internal/ai differs slightly per source; we probe
// the common keys and skip anything malformed.
func parseYOLODetections(raw []byte) []SearchFrameDetection {
	if len(raw) == 0 {
		return nil
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	out := make([]SearchFrameDetection, 0, len(arr))
	for _, d := range arr {
		label, _ := d["class"].(string)
		if label == "" {
			if s, ok := d["label"].(string); ok {
				label = s
			}
		}
		conf, _ := d["confidence"].(float64)
		var box [4]int
		// Prefer bbox_normalized (fractional 0-1) and scale to the frontend's
		// assumed 1920x1080 coord space. YOLO's pixel-space `bbox` is in the
		// source frame's native resolution, which varies per camera (often
		// 2592x1944 on 5MP models) — dividing by 1920/1080 in the browser
		// would shift boxes off-screen. Normalized is resolution-independent.
		if bn, ok := d["bbox_normalized"].(map[string]interface{}); ok {
			box[0] = int(toFloat(bn["x1"]) * 1920)
			box[1] = int(toFloat(bn["y1"]) * 1080)
			box[2] = int(toFloat(bn["x2"]) * 1920)
			box[3] = int(toFloat(bn["y2"]) * 1080)
		} else if b, ok := d["bbox"].(map[string]interface{}); ok {
			// Fallback to pixel bbox only when normalized isn't present.
			box[0] = toInt(b["x1"])
			box[1] = toInt(b["y1"])
			box[2] = toInt(b["x2"])
			box[3] = toInt(b["y2"])
		} else if b, ok := d["bbox"].([]interface{}); ok && len(b) == 4 {
			for i := 0; i < 4; i++ {
				box[i] = toInt(b[i])
			}
		}
		if label == "" {
			continue
		}
		// A PPE "violation" class marks the box red in the UI.
		_, isViolation := ppeViolationLabelMap[strings.ToLower(label)]
		out = append(out, SearchFrameDetection{
			Label:     label,
			BBox:      box,
			Violation: isViolation,
			Conf:      conf,
		})
	}
	return out
}

func toInt(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	}
	return 0
}

// toFloat coerces a JSONB-decoded numeric (always float64 from encoding/json)
// to a Go float64. Explicit helper keeps the normalized-bbox math readable.
func toFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

// ppeFromJSON translates the JSONB ai_ppe_violations list into the
// violation_flags map the frontend's filter uses (no_hard_hat / no_hi_vis / no_harness).
func ppeFromJSON(raw []byte) map[string]bool {
	out := map[string]bool{}
	if len(raw) == 0 {
		return out
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return out
	}
	for _, v := range arr {
		cls, _ := v["class"].(string)
		if key, ok := ppeViolationLabelMap[strings.ToLower(cls)]; ok {
			out[key] = true
		}
	}
	return out
}

// tagsToViolationFlags checks VLM-produced tags for PPE-related language.
// Lets a keyword search for "missing hard hat" still set the violation flag
// even if the row came from the VLM indexer rather than a PPE YOLO detection.
func tagsToViolationFlags(tags []string) map[string]bool {
	out := map[string]bool{}
	joined := strings.Join(tags, " ")
	lc := strings.ToLower(joined)
	if strings.Contains(lc, "no hard hat") || strings.Contains(lc, "no hardhat") || strings.Contains(lc, "missing hard hat") {
		out["no_hard_hat"] = true
	}
	if strings.Contains(lc, "no vest") || strings.Contains(lc, "no hi-vis") || strings.Contains(lc, "no high-vis") || strings.Contains(lc, "missing vest") {
		out["no_hi_vis"] = true
	}
	if strings.Contains(lc, "no harness") || strings.Contains(lc, "missing harness") {
		out["no_harness"] = true
	}
	return out
}

func matchesViolationFilter(flags map[string]bool, required []string) bool {
	if len(required) == 0 {
		return true
	}
	// AND-match: every checkbox the user ticked must be present.
	for _, req := range required {
		if !flags[req] {
			return false
		}
	}
	return true
}

// tokensMatchingQuery extracts tokens from the tag set that appear in the
// user's query. Powers the "Token Matches" sidebar in the preview pane.
func tokensMatchingQuery(q string, tags []string) []SearchFrameTokenMatch {
	if q == "" {
		return nil
	}
	qLower := strings.ToLower(q)
	out := []SearchFrameTokenMatch{}
	for _, tag := range tags {
		tl := strings.ToLower(tag)
		if strings.Contains(qLower, tl) || strings.Contains(tl, qLower) {
			out = append(out, SearchFrameTokenMatch{
				Token:  tag,
				Score:  0.85,
				Source: "caption",
			})
		}
	}
	return out
}

func alarmTokenMatches(q, alarmType string, flags map[string]bool) []SearchFrameTokenMatch {
	out := []SearchFrameTokenMatch{}
	if q != "" && alarmType != "" && strings.Contains(strings.ToLower(q), strings.ToLower(alarmType)) {
		out = append(out, SearchFrameTokenMatch{Token: alarmType, Score: 0.95, Source: "caption"})
	}
	for flag := range flags {
		out = append(out, SearchFrameTokenMatch{Token: flag, Score: 0.9, Source: "visual"})
	}
	return out
}
