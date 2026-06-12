package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/onvif"
)

// Milesight's VCA GETs return polygon coordinates in a pixel space whose
// dimensions are reported by the same response as maxWidth/maxHeight. The
// polygon itself is encoded as a colon-separated list of ints:
//
//	"polygonX": "128:320:280:180:-1:-1:-1:-1:-1:-1:"
//	"polygonY": "64:96:220:200:-1:-1:-1:-1:-1:-1:"
//
// -1 marks unused slots (the vendor pre-allocates 10 vertex slots per rule).
// We normalise to the 0.0–1.0 floats our DB already uses.

// VCAPullResult is returned to the frontend when the user clicks
// "Pull from camera". It contains the set of rules that exist on the
// device, plus a diff summary so the UI can explain what will change
// before the operator confirms overwriting our DB copy.
type VCAPullResult struct {
	CameraID   uuid.UUID             `json:"camera_id"`
	Rules      []database.VCARule    `json:"rules"` // normalised into our schema
	DBOnly     []database.VCARule    `json:"db_only"`
	CameraOnly []database.VCARule    `json:"camera_only"`
	Modified   []VCAPullModification `json:"modified"`
	Applied    bool                  `json:"applied"`
}

// VCAPullModification pairs the DB copy with the camera copy for rules
// that exist in both places but differ. The UI renders a diff against
// these pairs.
type VCAPullModification struct {
	Before database.VCARule `json:"before"`
	After  database.VCARule `json:"after"`
}

// HandleVCAPull reads the current VCA rule set from the camera and returns
// a diff against the DB. When the client supplies ?apply=1 we also commit
// the camera's state to the DB (replacing our copy), so camera-side edits
// made via the vendor web UI don't stay invisible.
//
// This closes the existing unidirectional push loop — without pull, any
// rule touched by the camera's native UI silently drifts out of sync.
//
// GET  /api/cameras/{id}/vca/pull            (preview only, no DB write)
// POST /api/cameras/{id}/vca/pull?apply=1    (replace DB rule set)
func HandleVCAPull(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cam, code, err := resolveMilesightCamera(r, db)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		apply := r.URL.Query().Get("apply") == "1" && r.Method == http.MethodPost

		// Writing is admin-gated, same as config writes.
		if apply {
			claims := claimsFromRequest(r)
			if claims == nil || (claims.Role != "admin" && claims.Role != "soc_supervisor") {
				http.Error(w, "forbidden: admin only for apply", http.StatusForbidden)
				return
			}
		}

		camID, _ := uuid.Parse(chi.URLParam(r, "id"))
		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		fromCam, err := pullCameraVCA(ctx, client, camID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		inDB, err := db.ListVCARules(r.Context(), camID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		out := VCAPullResult{
			CameraID: camID,
			// Initialise all the slice fields to non-nil so the JSON
			// output is `[]` instead of `null`. The frontend reads
			// `.length` on each (camera_only.length etc.) and a null
			// value crashes the VCA editor with
			// "Cannot read properties of null (reading 'length')".
			Rules:      []database.VCARule{},
			DBOnly:     []database.VCARule{},
			CameraOnly: []database.VCARule{},
			Modified:   []VCAPullModification{},
		}
		if fromCam != nil {
			out.Rules = fromCam
		}
		dbOnly, camOnly, mod := diffVCARules(inDB, fromCam)
		if dbOnly != nil {
			out.DBOnly = dbOnly
		}
		if camOnly != nil {
			out.CameraOnly = camOnly
		}
		if mod != nil {
			out.Modified = mod
		}

		if apply {
			if err := applyCameraVCAToDB(r.Context(), db, camID, fromCam); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			out.Applied = true
		}
		writeJSON(w, out)
	}
}

// pullCameraVCA reads all four VCA rule types from the camera and
// normalises each enabled region into a database.VCARule. Empty slots
// (no polygon, `regionEnable=0`) are dropped.
func pullCameraVCA(ctx context.Context, client *onvif.Client, camID uuid.UUID) ([]database.VCARule, error) {
	var out []database.VCARule

	// Intrusion
	if rules, err := pullIntrusion(ctx, client, camID); err == nil {
		out = append(out, rules...)
	} else {
		return nil, fmt.Errorf("intrusion: %w", err)
	}
	// Line crossing
	if rules, err := pullLineCross(ctx, client, camID); err == nil {
		out = append(out, rules...)
	} else {
		return nil, fmt.Errorf("linecross: %w", err)
	}
	// Region entrance
	if rules, err := pullRegionEntrance(ctx, client, camID); err == nil {
		out = append(out, rules...)
	} else {
		return nil, fmt.Errorf("regionentrance: %w", err)
	}
	// Loitering
	if rules, err := pullLoitering(ctx, client, camID); err == nil {
		out = append(out, rules...)
	} else {
		return nil, fmt.Errorf("loitering: %w", err)
	}
	return out, nil
}

// ── polygon / line parsing ──

type vcaEnvelope struct {
	MaxWidth  int `json:"maxWidth"`
	MaxHeight int `json:"maxHeight"`
}

func parsePolygon(polyX, polyY string, maxW, maxH int) []database.Point {
	xs := splitSlotList(polyX)
	ys := splitSlotList(polyY)
	n := len(xs)
	if len(ys) < n {
		n = len(ys)
	}
	if maxW <= 0 || maxH <= 0 {
		return nil
	}
	out := make([]database.Point, 0, n)
	for i := 0; i < n; i++ {
		// -1 = unused slot. Milesight pre-allocates 10 vertices per rule.
		if xs[i] < 0 || ys[i] < 0 {
			continue
		}
		out = append(out, database.Point{
			X: float64(xs[i]) / float64(maxW),
			Y: float64(ys[i]) / float64(maxH),
		})
	}
	return out
}

func splitSlotList(s string) []int {
	parts := strings.Split(strings.TrimSuffix(s, ":"), ":")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			out = append(out, -1)
			continue
		}
		out = append(out, n)
	}
	return out
}

// ── per-rule-type pullers ──

func pullIntrusion(ctx context.Context, client *onvif.Client, camID uuid.UUID) ([]database.VCARule, error) {
	raw, err := client.MilesightGet(ctx, "get.vca.intrusion")
	if err != nil {
		return nil, err
	}
	var payload struct {
		vcaEnvelope
		IntrusionDetectionSens int `json:"intrusionDetectionSens"`
		DetectionInfoList      []struct {
			RegionIndex  int    `json:"regionIndex"`
			RegionEnable int    `json:"regionEnable"`
			PolygonX     string `json:"polygonX"`
			PolygonY     string `json:"polygonY"`
		} `json:"detectionInfoList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []database.VCARule
	for _, info := range payload.DetectionInfoList {
		pts := parsePolygon(info.PolygonX, info.PolygonY, payload.MaxWidth, payload.MaxHeight)
		if len(pts) < 3 {
			continue
		}
		out = append(out, database.VCARule{
			CameraID:    camID,
			RuleType:    "intrusion",
			Name:        fmt.Sprintf("Intrusion Region %d", info.RegionIndex+1),
			Enabled:     info.RegionEnable == 1,
			Sensitivity: payload.IntrusionDetectionSens * 20, // 0..5 → 0..100
			Region:      pts,
			Actions:     []string{"record", "notify"},
			Synced:      true,
		})
	}
	return out, nil
}

func pullRegionEntrance(ctx context.Context, client *onvif.Client, camID uuid.UUID) ([]database.VCARule, error) {
	raw, err := client.MilesightGet(ctx, "get.vca.regionentrance")
	if err != nil {
		return nil, err
	}
	var payload struct {
		vcaEnvelope
		IntrusionEnterSens int `json:"intrusionEnterSens"`
		DetectionInfoList  []struct {
			RegionIndex  int    `json:"regionIndex"`
			RegionEnable int    `json:"regionEnable"`
			PolygonX     string `json:"polygonX"`
			PolygonY     string `json:"polygonY"`
		} `json:"detectionInfoList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []database.VCARule
	for _, info := range payload.DetectionInfoList {
		pts := parsePolygon(info.PolygonX, info.PolygonY, payload.MaxWidth, payload.MaxHeight)
		if len(pts) < 3 {
			continue
		}
		out = append(out, database.VCARule{
			CameraID:    camID,
			RuleType:    "regionentrance",
			Name:        fmt.Sprintf("Region Entrance %d", info.RegionIndex+1),
			Enabled:     info.RegionEnable == 1,
			Sensitivity: payload.IntrusionEnterSens * 20,
			Region:      pts,
			Actions:     []string{"record", "notify"},
			Synced:      true,
		})
	}
	return out, nil
}

func pullLoitering(ctx context.Context, client *onvif.Client, camID uuid.UUID) ([]database.VCARule, error) {
	raw, err := client.MilesightGet(ctx, "get.vca.loitering")
	if err != nil {
		return nil, err
	}
	var payload struct {
		vcaEnvelope
		DetectionInfoList []struct {
			RegionIndex      int    `json:"regionIndex"`
			LoiteringEnable  int    `json:"loiteringEnable"`
			MinLoiteringTime int    `json:"minLoiteringTime"`
			PolygonX         string `json:"polygonX"`
			PolygonY         string `json:"polygonY"`
		} `json:"detectionInfoList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []database.VCARule
	for _, info := range payload.DetectionInfoList {
		pts := parsePolygon(info.PolygonX, info.PolygonY, payload.MaxWidth, payload.MaxHeight)
		if len(pts) < 3 {
			continue
		}
		out = append(out, database.VCARule{
			CameraID:     camID,
			RuleType:     "loitering",
			Name:         fmt.Sprintf("Loitering Region %d", info.RegionIndex+1),
			Enabled:      info.LoiteringEnable == 1,
			ThresholdSec: info.MinLoiteringTime,
			Region:       pts,
			Actions:      []string{"record", "notify"},
			Synced:       true,
		})
	}
	return out, nil
}

// Line-cross rules are points, not polygons. Milesight encodes two
// endpoints per line using the same polygonX/Y format, but only the first
// two indices are meaningful.
func pullLineCross(ctx context.Context, client *onvif.Client, camID uuid.UUID) ([]database.VCARule, error) {
	raw, err := client.MilesightGet(ctx, "get.vca.alllinecrossing")
	if err != nil {
		return nil, err
	}
	var payload struct {
		vcaEnvelope
		LineCrossingSens  int `json:"lineCrossingSens"`
		DetectionInfoList []struct {
			LineIndex          int    `json:"lineIndex"`
			LineCrossingEnable int    `json:"lineCrossingEnable"`
			Direction          int    `json:"direction"`
			PolygonX           string `json:"polygonX"`
			PolygonY           string `json:"polygonY"`
		} `json:"detectionInfoList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var out []database.VCARule
	for _, info := range payload.DetectionInfoList {
		pts := parsePolygon(info.PolygonX, info.PolygonY, payload.MaxWidth, payload.MaxHeight)
		if len(pts) < 2 {
			continue
		}
		out = append(out, database.VCARule{
			CameraID:    camID,
			RuleType:    "linecross",
			Name:        fmt.Sprintf("Line %d", info.LineIndex+1),
			Enabled:     info.LineCrossingEnable == 1,
			Sensitivity: payload.LineCrossingSens * 20,
			Region:      pts[:2],
			Direction:   lineCrossDirection(info.Direction),
			Actions:     []string{"record", "notify"},
			Synced:      true,
		})
	}
	return out, nil
}

func lineCrossDirection(d int) string {
	switch d {
	case 0:
		return "both"
	case 1:
		return "left_to_right"
	case 2:
		return "right_to_left"
	default:
		return "both"
	}
}

// ── diff + apply ──

// diffVCARules compares the DB copy with the camera copy using
// (rule_type + region_index) as the identity key. Since our DB schema
// doesn't store the camera-side index, we match by rule_type + best
// polygon-overlap instead; for a first pass we keep it simple and use
// rule_type + name.
func diffVCARules(inDB, fromCam []database.VCARule) (dbOnly, camOnly []database.VCARule, modified []VCAPullModification) {
	dbByKey := make(map[string]database.VCARule, len(inDB))
	for _, r := range inDB {
		dbByKey[r.RuleType+"|"+r.Name] = r
	}
	seen := make(map[string]bool, len(fromCam))
	for _, c := range fromCam {
		key := c.RuleType + "|" + c.Name
		seen[key] = true
		if d, ok := dbByKey[key]; ok {
			if !rulesEquivalent(d, c) {
				modified = append(modified, VCAPullModification{Before: d, After: c})
			}
		} else {
			camOnly = append(camOnly, c)
		}
	}
	for key, d := range dbByKey {
		if !seen[key] {
			dbOnly = append(dbOnly, d)
		}
	}
	return
}

// rulesEquivalent compares the user-visible fields — coarse-grained on
// purpose, since polygon vertex ordering + floating-point round-trips
// will almost never match exactly even when "the same".
func rulesEquivalent(a, b database.VCARule) bool {
	if a.Enabled != b.Enabled {
		return false
	}
	if a.Sensitivity != b.Sensitivity {
		return false
	}
	if a.ThresholdSec != b.ThresholdSec {
		return false
	}
	if a.Direction != b.Direction {
		return false
	}
	if len(a.Region) != len(b.Region) {
		return false
	}
	for i := range a.Region {
		if absDelta(a.Region[i].X, b.Region[i].X) > 0.02 ||
			absDelta(a.Region[i].Y, b.Region[i].Y) > 0.02 {
			return false
		}
	}
	return true
}

func absDelta(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// applyCameraVCAToDB replaces the camera's rule set in our DB with what
// the camera reports — a full overwrite, not a merge, which matches what
// "pull" means to operators. Delete + re-create run in one transaction
// (ReplaceVCARules) so a mid-way failure can't leave the camera with a
// wiped or partial rule set.
func applyCameraVCAToDB(ctx context.Context, db *database.DB, camID uuid.UUID, rules []database.VCARule) error {
	creates := make([]*database.VCARuleCreate, 0, len(rules))
	for _, r := range rules {
		creates = append(creates, &database.VCARuleCreate{
			RuleType:     r.RuleType,
			Name:         r.Name,
			Enabled:      r.Enabled,
			Sensitivity:  r.Sensitivity,
			Region:       r.Region,
			Direction:    r.Direction,
			ThresholdSec: r.ThresholdSec,
			Schedule:     r.Schedule,
			Actions:      r.Actions,
		})
	}
	return db.ReplaceVCARules(ctx, camID, creates)
}

// HandleSyncZones imports the camera's current VCA zone configuration
// into vca_rules so the live overlay picks them up immediately.
//
// It mirrors POST /api/cameras/{id}/vca/pull?apply=1 but as a dedicated
// named endpoint that operators can invoke from the camera card without
// opening the full VCA editor. Required role: admin or soc_supervisor.
//
// POST /api/cameras/{id}/sync-zones
func HandleSyncZones(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" && claims.Role != "soc_supervisor" {
			http.Error(w, "forbidden: admin or supervisor required", http.StatusForbidden)
			return
		}

		camID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera id", http.StatusBadRequest)
			return
		}
		cam, err := db.GetCamera(r.Context(), camID)
		if err != nil {
			http.Error(w, "camera not found", http.StatusNotFound)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		rules, err := pullCameraVCA(ctx, client, camID)
		if err != nil {
			log.Printf("[VCA] sync-zones fetch failed for %s (%s): %v", cam.Name, cam.OnvifAddress, err)
			http.Error(w, "failed to read VCA config from camera: "+err.Error(), http.StatusBadGateway)
			return
		}

		if err := applyCameraVCAToDB(r.Context(), db, camID, rules); err != nil {
			http.Error(w, "failed to save zones to DB: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[VCA] sync-zones imported %d rules for camera %s", len(rules), cam.Name)

		// Return a summary so the frontend can update its rule list without a
		// separate GET /vca/rules round trip.
		type ruleInfo struct {
			RuleType   string `json:"rule_type"`
			Name       string `json:"name"`
			Enabled    bool   `json:"enabled"`
			PointCount int    `json:"point_count"`
		}
		summary := make([]ruleInfo, 0, len(rules))
		for _, r := range rules {
			summary = append(summary, ruleInfo{
				RuleType:   r.RuleType,
				Name:       r.Name,
				Enabled:    r.Enabled,
				PointCount: len(r.Region),
			})
		}
		writeJSON(w, map[string]interface{}{
			"camera_id": camID,
			"imported":  len(rules),
			"rules":     summary,
		})
	}
}

// TrySyncCameraVCAZones fires-and-forgets a VCA zone import for a camera.
// Safe to call from HandleCreateCamera on Milesight devices — failures are
// logged but don't fail the camera-create response. The goroutine is short
// (≤20 s) and uses a background context so the HTTP request can return
// while the sync is in flight.
func TrySyncCameraVCAZones(db *database.DB, camID uuid.UUID, onvifAddress, username, password, camName string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		client := onvif.NewClient(onvifAddress, username, password)
		rules, err := pullCameraVCA(ctx, client, camID)
		if err != nil {
			log.Printf("[VCA] auto-import failed for new camera %s (%s): %v", camName, onvifAddress, err)
			return
		}
		if err := applyCameraVCAToDB(ctx, db, camID, rules); err != nil {
			log.Printf("[VCA] auto-import DB write failed for %s: %v", camName, err)
			return
		}
		log.Printf("[VCA] auto-import: imported %d zones for new camera %s", len(rules), camName)
	}()
}
