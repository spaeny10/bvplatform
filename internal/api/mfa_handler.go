package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
)

// HandleMFAEnroll generates a fresh TOTP secret and 10 recovery codes
// for the authenticated user, writes them to the database with
// mfa_enabled still false, and returns the otpauth:// URL plus the
// plaintext recovery codes.
//
// The recovery codes are only ever shown to the user this once — we
// store bcrypt hashes server-side. If the user loses them and their
// authenticator app, an admin must reset their MFA. That asymmetry is
// deliberate: it keeps a database leak from being a "skeleton key."
//
// Calling this endpoint while MFA is already enabled is a no-op
// (returns 409). If the user wants to re-enroll, they must disable
// first — the explicit two-step prevents accidental loss of access.
func HandleMFAEnroll(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}

		state, err := db.GetMFAState(r.Context(), userID)
		if err != nil {
			http.Error(w, "fetch mfa state failed", http.StatusInternalServerError)
			return
		}
		if state.Enabled {
			http.Error(w, "mfa already enabled; disable first to re-enroll", http.StatusConflict)
			return
		}

		secret, err := auth.GenerateTOTPSecret()
		if err != nil {
			http.Error(w, "generate secret failed", http.StatusInternalServerError)
			return
		}
		recoveryCodes, err := auth.GenerateRecoveryCodes(10)
		if err != nil {
			http.Error(w, "generate recovery codes failed", http.StatusInternalServerError)
			return
		}

		// Hash recovery codes before storage. bcrypt cost stays at the
		// package default — recovery codes are high-entropy already, so
		// the cost factor is about resisting offline cracking attempts
		// rather than padding low-entropy human passwords.
		hashes := make([]string, 0, len(recoveryCodes))
		for _, code := range recoveryCodes {
			h, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
			if err != nil {
				http.Error(w, "hash recovery codes failed", http.StatusInternalServerError)
				return
			}
			hashes = append(hashes, string(h))
		}

		if err := db.SetMFAEnrollment(r.Context(), userID, secret, hashes); err != nil {
			http.Error(w, "store enrollment failed", http.StatusInternalServerError)
			return
		}

		issuer := cfg.ProductName
		if issuer == "" {
			issuer = "Ironsight"
		}
		writeJSON(w, map[string]interface{}{
			"secret":            secret,
			"provisioning_url":  auth.ProvisioningURL(secret, issuer, claims.Username),
			"recovery_codes":    recoveryCodes,
			"recovery_count":    len(recoveryCodes),
			"requires_confirm":  true,
			"confirm_endpoint":  "/api/auth/mfa/confirm",
		})
	}
}

// HandleMFAConfirm completes enrollment: takes a TOTP code from the
// just-generated secret, verifies it, and flips mfa_enabled true. If
// the code doesn't validate the secret stays in place but disabled —
// the user can retry.
func HandleMFAConfirm(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}

		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
			http.Error(w, "code required", http.StatusBadRequest)
			return
		}

		state, err := db.GetMFAState(r.Context(), userID)
		if err != nil || state.Secret == "" {
			http.Error(w, "no enrollment in progress; call /api/auth/mfa/enroll first", http.StatusBadRequest)
			return
		}
		if state.Enabled {
			http.Error(w, "mfa already enabled", http.StatusConflict)
			return
		}

		ok, err := auth.VerifyTOTP(state.Secret, req.Code)
		if err != nil {
			http.Error(w, "verify failed", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "invalid code", http.StatusUnauthorized)
			return
		}

		if err := db.EnableMFA(r.Context(), userID); err != nil {
			http.Error(w, "enable failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleMFADisable wipes MFA for the authenticated user. Requires a
// current TOTP code (or recovery code) to prove the caller actually
// has the second factor — otherwise a stolen primary-credential token
// could disable MFA and re-enable account takeover.
//
// Admin/supervisor flag is honored: an admin can call
// /api/users/{id}/mfa/disable to wipe MFA on another account without
// the second-factor proof. That endpoint lives separately so the audit
// row distinguishes self-disable (action=disable_mfa) from admin
// override (action=admin_reset_mfa).
func HandleMFADisable(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}

		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
			http.Error(w, "code required to disable mfa", http.StatusBadRequest)
			return
		}

		state, err := db.GetMFAState(r.Context(), userID)
		if err != nil {
			http.Error(w, "fetch state failed", http.StatusInternalServerError)
			return
		}
		if !state.Enabled {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if !verifyMFACode(r.Context(), state, req.Code, db, userID) {
			http.Error(w, "invalid code", http.StatusUnauthorized)
			return
		}

		if err := db.DisableMFA(r.Context(), userID); err != nil {
			http.Error(w, "disable failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleAdminMFAReset lets an admin or soc_supervisor wipe MFA for
// any user without needing the user's TOTP code. The caller only needs
// an admin-level JWT. Used when a user loses their authenticator and
// has no recovery codes.
//
// POST /api/v1/users/{id}/mfa/reset
func HandleAdminMFAReset(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" && claims.Role != "soc_supervisor" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		targetID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		if err := db.DisableMFA(r.Context(), targetID); err != nil {
			http.Error(w, "reset failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// verifyMFACode is shared by login and disable. It checks the code
// against the live TOTP secret first, then against each recovery
// hash. A matched recovery code is consumed (one-time use).
//
// Returns true on either path. Returns false on no match. Errors on
// the database side are treated as no-match — the handler surfaces a
// generic 401 either way so a probing attacker can't distinguish
// "wrong code" from "DB hiccup."
//
// Takes userID explicitly rather than fishing it out of context.
// The login flow calls this BEFORE issuing a JWT, so there are no
// claims to read; the disable flow already parses the userID from
// the bearer token. Both paths pass it in the same way.
func verifyMFACode(ctx context.Context, state *database.MFAState, code string, db *database.DB, userID uuid.UUID) bool {
	// Try TOTP first — this is the common path.
	ok, _ := auth.VerifyTOTP(state.Secret, code)
	if ok {
		return true
	}

	// Recovery code path. Iterate hashes in stored order; on match,
	// consume that index so the code becomes one-time use. bcrypt's
	// CompareHashAndPassword runs in time proportional to the cost
	// factor regardless of input correctness.
	for i, h := range state.RecoveryHashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(code)) == nil {
			if userID != (uuid.UUID{}) {
				_, _ = db.ConsumeRecoveryCode(ctx, userID, i)
			}
			return true
		}
	}
	return false
}
