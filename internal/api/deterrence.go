package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/database"
	"ironsight/internal/onvif"
)

// DeterrenceRequest is the body for POST /api/cameras/{id}/deterrence. All
// fields except `action` are optional; `reason` is strongly encouraged (and
// recorded in the audit trail) for compliance.
type DeterrenceRequest struct {
	// action: "strobe" | "siren" | "both" | "alarm_out". Case-insensitive.
	Action string `json:"action"`
	// DurationSec is advisory — Milesight monostable relays auto-release
	// after their camera-configured DelayTime. We re-fire every 8 s up to
	// DurationSec to hold the output "on" for longer than one pulse.
	DurationSec int `json:"duration_sec,omitempty"`
	// Reason is the operator's justification, stored verbatim on the audit
	// row ("observed person with rifle", "gate forced open", etc). Optional
	// because some SOC deployments require it and some don't, but the
	// frontend should always prompt.
	Reason string `json:"reason,omitempty"`
	// AlarmID is the active_alarms row that prompted this action, when
	// launched from the alarm card. Purely audit context — lets us answer
	// "what alarm caused the strobe that fired at 2:31 AM".
	AlarmID string `json:"alarm_id,omitempty"`
}

// DeterrenceResponse is returned on success so the frontend can show a
// confirmation toast ("Strobe fired for 10 s").
type DeterrenceResponse struct {
	OK           bool   `json:"ok"`
	Action       string `json:"action"`
	CameraID     string `json:"camera_id"`
	CameraName   string `json:"camera_name"`
	DurationSec  int    `json:"duration_sec"`
	FiredAt      int64  `json:"fired_at"` // unix millis
	RelayTokens  []string `json:"relay_tokens"`
	Message      string `json:"message,omitempty"`
}

// HandleDeterrence fires one of the camera's relay outputs (strobe / siren /
// both / alarm_out). Requires an authenticated user with access to the
// camera (RBAC). Every call — successful or not — writes a row to
// deterrence_audits. This is a high-trust action; we err on the side of
// logging too much rather than too little.
//
// URL: POST /api/cameras/{id}/deterrence
func HandleDeterrence(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		cameraID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid camera ID", http.StatusBadRequest)
			return
		}

		// RBAC: only roles with camera access can fire deterrence on it.
		// Operators (soc_operator / soc_supervisor / admin) can act on any
		// camera; site_manager and customer are restricted to their sites.
		if ok, cErr := CanAccessCamera(r.Context(), db, claims, cameraID); cErr != nil {
			http.Error(w, cErr.Error(), http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Extra guard: customer-tier roles shouldn't be firing sirens from
		// their own portal. Only staff or site managers do active
		// deterrence. This is policy, not cryptography — the frontend
		// won't show the button to customers either.
		if claims.Role == "customer" || claims.Role == "viewer" {
			http.Error(w, "deterrence requires operator or site-manager role", http.StatusForbidden)
			return
		}

		var req DeterrenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		tokens, err := tokensForAction(action)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Load the camera so we have its ONVIF address + creds on hand.
		cam, err := db.GetCamera(r.Context(), cameraID)
		if err != nil {
			http.Error(w, "camera lookup failed", http.StatusInternalServerError)
			return
		}

		// Fire the relays. Collect errors per-token so "both" partial-success
		// (strobe fires, siren fails) still reports accurately.
		client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
		connectCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		if _, cErr := client.Connect(connectCtx); cErr != nil {
			cancel()
			writeDeterrenceAudit(db, claims, r, cameraID, cam.Name, action, req, false, cErr.Error())
			http.Error(w, fmt.Sprintf("camera unreachable: %v", cErr), http.StatusBadGateway)
			return
		}
		cancel()

		fireCtx, fireCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer fireCancel()

		var firstErr error
		for _, token := range tokens {
			if fErr := client.SetRelayOutputState(fireCtx, token, "active"); fErr != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", token, fErr)
				}
			}
		}

		success := firstErr == nil
		errMsg := ""
		if firstErr != nil {
			errMsg = firstErr.Error()
		}
		writeDeterrenceAudit(db, claims, r, cameraID, cam.Name, action, req, success, errMsg)

		if !success {
			http.Error(w, fmt.Sprintf("relay trigger failed: %v", firstErr), http.StatusBadGateway)
			return
		}

		// Optional sustain: re-fire every 8 s until DurationSec elapses.
		// Monostable relays auto-release after ~10 s so this gives a
		// smooth hold. Runs in a goroutine so the HTTP response returns
		// immediately — the browser doesn't wait for a 30 s siren.
		if req.DurationSec > 10 {
			go sustainDeterrence(cam, tokens, req.DurationSec)
		}

		dur := req.DurationSec
		if dur == 0 {
			dur = 10
		}
		writeJSON(w, DeterrenceResponse{
			OK:          true,
			Action:      action,
			CameraID:    cameraID.String(),
			CameraName:  cam.Name,
			DurationSec: dur,
			FiredAt:     time.Now().UnixMilli(),
			RelayTokens: tokens,
			Message:     fmt.Sprintf("Deterrence fired on %s (%s)", cam.Name, strings.Join(tokens, " + ")),
		})
	}
}

// tokensForAction maps the API's coarse action name to one or more relay
// tokens the ONVIF call targets.
func tokensForAction(action string) ([]string, error) {
	switch action {
	case "strobe":
		return []string{onvif.RelayTokenWarningLight}, nil
	case "siren":
		return []string{onvif.RelayTokenSounder}, nil
	case "both":
		// Fire in sequence: strobe first, then siren. Gives a visual cue
		// before the audio; better operator UX and slightly staggers the
		// electrical draw on cameras with weaker PSUs.
		return []string{onvif.RelayTokenWarningLight, onvif.RelayTokenSounder}, nil
	case "alarm_out":
		return []string{onvif.RelayTokenAlarmOut}, nil
	default:
		return nil, errors.New(`action must be one of: "strobe", "siren", "both", "alarm_out"`)
	}
}

// sustainDeterrence re-fires the requested relays every 8 seconds until
// the requested duration elapses. Runs in its own goroutine so the HTTP
// response can return immediately. Each re-fire is a fresh SetRelayOutputState
// call, taking advantage of the monostable relay's auto-reset.
func sustainDeterrence(cam *database.Camera, tokens []string, durationSec int) {
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	client := onvif.NewClient(cam.OnvifAddress, cam.Username, cam.Password)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, _ = client.Connect(ctx)
	cancel()

	for time.Now().Before(deadline) {
		<-ticker.C
		if time.Now().After(deadline) {
			return
		}
		fireCtx, fc := context.WithTimeout(context.Background(), 5*time.Second)
		for _, t := range tokens {
			_ = client.SetRelayOutputState(fireCtx, t, "active")
		}
		fc()
	}
}

// writeDeterrenceAudit persists one row to deterrence_audits. Best-effort:
// failures are logged but don't surface to the caller, because audit logging
// must never be the reason a legit SOC action gets rejected.
func writeDeterrenceAudit(
	db *database.DB, claims *auth.Claims, r *http.Request,
	cameraID uuid.UUID, cameraName, action string,
	req DeterrenceRequest, success bool, errMsg string,
) {
	userID, username, role := "", "", ""
	if claims != nil {
		userID, username, role = claims.UserID, claims.Username, claims.Role
	}
	duration := req.DurationSec
	if duration == 0 {
		duration = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO deterrence_audits
		  (user_id, username, role, camera_id, camera_name, action,
		   duration_sec, reason, alarm_id, success, error, ip)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		userID, username, role, cameraID, cameraName, action,
		duration, req.Reason, req.AlarmID, success, errMsg, clientIP(r))
	if err != nil {
		// Log but don't fail — audit failure must never block the action.
		fmt.Printf("[DETERRENCE] audit write failed: %v\n", err)
	}
}
