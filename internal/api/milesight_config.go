package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/database"
	"ironsight/internal/onvif"
)

// Milesight's web UI exposes ~30 configuration panels, each backed by a
// dot-notation CGI action (get.X.Y / set.X.Y). Rather than hand-wrap every
// panel in a typed Go struct, we route them through a single pair of
// allowlisted passthrough handlers and let the frontend hold the typed
// contract. The allowlist is the security boundary: no free-form action
// strings reach the camera, only the panels we've vetted.

// panelActions maps a stable panel slug (used in our URL path) to the
// vendor's get/set action pair and (rarely) a list-wrap rule.
//
// `wrapKey` applies to scene-indexed actions: the vendor splits image and
// day/night settings per "scene" and uses `{<wrapKey>: [{scene: N, ...body}]}`
// as the wire format for BOTH directions. When set:
//
//   - On GET, the handler unwraps to return just `sceneList[0]` to the
//     frontend, so the TS types stay flat.
//   - On SET, the handler re-wraps the flat body as
//     `{<wrapKey>: [{scene: 0, ...body}]}` before POSTing.
//
// Sending the flat body without wrapping causes the camera to close the
// TCP connection without a response — not a useful error.
type panelAction struct {
	getAction string
	setAction string // empty = read-only
	wrapKey   string // empty = pass body through verbatim
}

var panelActions = map[string]panelAction{
	"osd":             {getAction: "get.video.advanced", setAction: "set.video.advanced"},
	"streams":         {getAction: "get.video.general", setAction: "set.video.general"},
	"image":           {getAction: "get.multi.camera.setting", setAction: "set.multi.camera.setting", wrapKey: "sceneList"},
	"audio":           {getAction: "get.audio", setAction: "set.audio"},
	"datetime":        {getAction: "get.system.datetime", setAction: "set.system.datetime"},
	"network":         {getAction: "get.system.information", setAction: "set.system.information"},
	"privacyMask":     {getAction: "get.camera.mask", setAction: "set.camera.mask"},
	"autoReboot":      {getAction: "get.auto.reboot", setAction: "set.auto.reboot"},
	"ptzPresets":      {getAction: "get.ptz.preset", setAction: "set.ptz.basic"},
	"alarmInput":      {getAction: "get.event.input", setAction: "set.event.input"},
	"alarmOutput":     {getAction: "get.event.externoutput", setAction: "set.event.externoutput"},
	"dayNight":        {getAction: "get.multi.camera.daynight", setAction: "set.multi.camera.daynight", wrapKey: "dayNightList"},
	"imageCaps":       {getAction: "get.cap.image"},
	"system":          {getAction: "get.system.information"},
	"networkPlatform": {getAction: "get.network.platform", setAction: "set.network.platform"},
	"networkSnmp":     {getAction: "get.network.snmp", setAction: "set.network.snmp"},
}

// HandleMilesightGet reads one panel's state from the camera and returns
// the raw JSON the vendor CGI emits. The frontend unmarshalls this against
// its typed contract; we pass through without re-shaping to avoid the
// maintenance burden of keeping N Go structs in lockstep with firmware.
func HandleMilesightGet(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// GET still gates to admin/supervisor: the vendor CGI returns
		// network and credential fields that customer-side users at
		// the camera's site shouldn't see, even if they have read
		// access to the camera itself.
		if !requireAdminOrSupervisor(r) {
			http.Error(w, "forbidden: admin or supervisor required", http.StatusForbidden)
			return
		}
		cam, code, err := resolveMilesightCamera(r, db)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		panel := chi.URLParam(r, "panel")
		entry, ok := panelActions[panel]
		if !ok {
			http.Error(w, "unknown panel", http.StatusNotFound)
			return
		}

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		raw, err := client.MilesightGet(ctx, entry.getAction)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		// Scene-indexed panels: the camera returns
		// `{wrapKey: [{scene, ...fields}, ...], ...topLevel}`. The frontend
		// expects a flat shape, so we unwrap the scene-0 entry and merge
		// top-level fields in for round-tripping.
		if entry.wrapKey != "" {
			if flattened, ok := unwrapSceneList(raw, entry.wrapKey); ok {
				raw = flattened
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// unwrapSceneList pulls sceneList[scene==0] out of a scene-indexed response
// and returns it as a flat object. Top-level device-capability fields
// (irisType, aiispMode, iRLevelLimit, etc.) are dropped on purpose: the
// UI doesn't edit them, and stripping them from the flat view means we
// can't accidentally ship them back inside the sceneList wrapper on save.
//
// Returns (unwrapped, true) on a well-formed response; (original, false)
// when the shape doesn't match (e.g., the camera sent an error envelope).
func unwrapSceneList(raw []byte, wrapKey string) ([]byte, bool) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return raw, false
	}
	listVal, ok := top[wrapKey]
	if !ok {
		return raw, false
	}
	list, ok := listVal.([]any)
	if !ok || len(list) == 0 {
		return raw, false
	}
	// Prefer scene 0; fall back to first element if all scene keys differ.
	picked, _ := list[0].(map[string]any)
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			if s, ok := m["scene"].(float64); ok && s == 0 {
				picked = m
				break
			}
		}
	}
	if picked == nil {
		return raw, false
	}
	out, err := json.Marshal(picked)
	if err != nil {
		return raw, false
	}
	return out, true
}

// HandleMilesightSet writes one panel's state. Restricted to admin and
// soc_supervisor — silently reconfiguring a customer's camera is beyond
// what we want an operator-tier account to be able to do.
func HandleMilesightSet(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" && claims.Role != "soc_supervisor" {
			http.Error(w, "forbidden: admin only", http.StatusForbidden)
			return
		}
		cam, code, err := resolveMilesightCamera(r, db)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		panel := chi.URLParam(r, "panel")
		entry, ok := panelActions[panel]
		if !ok {
			http.Error(w, "unknown panel", http.StatusNotFound)
			return
		}
		if entry.setAction == "" {
			http.Error(w, "panel is read-only", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "body read: "+err.Error(), http.StatusBadRequest)
			return
		}
		var decoded any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				http.Error(w, "body must be JSON", http.StatusBadRequest)
				return
			}
		}

		// Scene-indexed panels need the body wrapped in a list keyed by
		// scene index. See panelAction.wrapKey — sending the flat body
		// causes a TCP hangup on this firmware rather than a helpful error.
		if entry.wrapKey != "" {
			obj, ok := decoded.(map[string]any)
			if !ok {
				http.Error(w, "body must be a JSON object for this panel", http.StatusBadRequest)
				return
			}
			obj["scene"] = 0
			decoded = map[string]any{entry.wrapKey: []any{obj}}
		}

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		raw, err := client.MilesightPost(ctx, entry.setAction, decoded)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := onvif.MilesightCheckSetState(raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// HandleMilesightReboot triggers an immediate camera reboot. Admin-only,
// since it drops every live stream and takes 90s-2min to recover.
func HandleMilesightReboot(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" {
			http.Error(w, "forbidden: admin only", http.StatusForbidden)
			return
		}
		cam, code, err := resolveMilesightCamera(r, db)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		raw, err := client.MilesightPost(ctx, "reboot.system.maintenance", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		// Reboot returns before the camera actually restarts — absence of
		// setState:"succeed" isn't necessarily a failure, so we don't call
		// MilesightCheckSetState here.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// HandlePTZPresetGoto calls the vendor preset-recall action. Separate
// from the generic set handler because "go to preset N" isn't a config
// write — it's a command. Body: {"preset": <int>}.
func HandlePTZPresetGoto(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cam, code, err := resolveMilesightCamera(r, db)
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		var req struct {
			Preset int `json:"preset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Preset < 1 || req.Preset > 255 {
			http.Error(w, "preset must be 1..255", http.StatusBadRequest)
			return
		}

		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Milesight's preset recall rides on set.ptz.basic with a control
		// code. 0x10 = goto-preset per the vendor PTZ protocol docs.
		raw, err := client.MilesightPost(ctx, "set.ptz.basic", map[string]any{
			"ptzControlType": 16,
			"presetIndex":    req.Preset,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := onvif.MilesightCheckSetState(raw); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// resolveMilesightCamera is shared boilerplate: parse the :id URL param,
// check access, look up the camera row, and confirm it's a Milesight
// device (the CGI action vocabulary is vendor-specific).
func resolveMilesightCamera(r *http.Request, db *database.DB) (*database.Camera, int, error) {
	claims := claimsFromRequest(r)
	if claims == nil {
		return nil, http.StatusUnauthorized, errors.New("unauthorized")
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, http.StatusBadRequest, errors.New("invalid camera id")
	}
	ok, cerr := CanAccessCamera(r.Context(), db, claims, id)
	if cerr != nil {
		return nil, http.StatusInternalServerError, cerr
	}
	if !ok {
		return nil, http.StatusNotFound, errors.New("not found")
	}
	cam, cerr := db.GetCamera(r.Context(), id)
	if cerr != nil {
		return nil, http.StatusInternalServerError, cerr
	}
	if !isMilesightCamera(cam) {
		return nil, http.StatusBadRequest, fmt.Errorf("camera %q is not a Milesight device", cam.Name)
	}
	return cam, 0, nil
}

// isMilesightCamera checks the manufacturer + model strings stored at
// camera onboarding. Matches the detection logic in internal/drivers/milesight.go
// so a row that the VCA path treats as Milesight also satisfies this check.
func isMilesightCamera(cam *database.Camera) bool {
	m := strings.ToLower(cam.Manufacturer)
	if strings.Contains(m, "milesight") {
		return true
	}
	mdl := strings.ToUpper(cam.Model)
	return strings.HasPrefix(mdl, "MS-C") || strings.HasPrefix(mdl, "MS-N")
}
