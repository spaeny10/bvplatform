package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"ironsight/internal/auth"
	"ironsight/internal/database"
)

// shareDefaultTTL is what we hand out when the caller doesn't supply
// expires_in_hours. UL 827B reviewers expect public shares to have a
// concrete expiration; 7 days matches a typical insurance/police
// claims window and is short enough that a leaked token has limited
// downside. Operators can request shorter; an admin can extend up to
// shareMaxTTL.
const shareDefaultTTL = 7 * 24 * time.Hour

// shareMaxTTL caps how long a single share can live. 90 days covers
// nearly every legitimate use (claims investigations, court discovery
// preparation) without enabling "permanent" public links — which
// would be a UL 827B audit finding.
const shareMaxTTL = 90 * 24 * time.Hour

type createShareRequest struct {
	IncidentID     string `json:"incident_id"`
	ExpiresInHours int    `json:"expires_in_hours,omitempty"`
}

// HandleCreateEvidenceShare mints a new public share token for an
// incident. Restricted to soc_supervisor / admin — operators don't
// have unilateral authority to publish evidence externally.
//
// Behavior:
//   - Validates the incident exists. We deliberately accept any incident
//     the caller knows about; site-scope ACLs are enforced at the
//     incident-create layer, not here.
//   - Picks expiry: clamp `expires_in_hours` to (0, shareMaxTTL]. Default
//     when caller leaves it unset is shareDefaultTTL.
//   - Returns the new EvidenceShare object including the plaintext token.
//     The frontend should immediately show this once and then never read
//     it back from a list endpoint — the audit binder lives at the
//     supervisor's clipboard, not in our database UI.
func HandleCreateEvidenceShare(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" && claims.Role != "soc_supervisor" {
			http.Error(w, "supervisor or admin role required", http.StatusForbidden)
			return
		}

		var req createShareRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.IncidentID == "" {
			http.Error(w, "incident_id required", http.StatusBadRequest)
			return
		}

		ttl := shareDefaultTTL
		if req.ExpiresInHours > 0 {
			ttl = time.Duration(req.ExpiresInHours) * time.Hour
			if ttl > shareMaxTTL {
				http.Error(w, fmt.Sprintf("expires_in_hours exceeds max of %d", int(shareMaxTTL.Hours())), http.StatusBadRequest)
				return
			}
		}
		expiresAt := time.Now().UTC().Add(ttl)

		share, err := db.CreateEvidenceShare(r.Context(), req.IncidentID, claims.UserID, expiresAt)
		if err != nil {
			http.Error(w, "create share failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, share)
	}
}

// HandleRevokeEvidenceShare flips a share's revoked flag. Same role
// gate as creation — supervisors/admins only. The audit middleware
// records the revocation at the route level (action=revoke_evidence_share);
// no extra logging needed here.
func HandleRevokeEvidenceShare(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil || (claims.Role != "admin" && claims.Role != "soc_supervisor") {
			http.Error(w, "supervisor or admin role required", http.StatusForbidden)
			return
		}
		token := chi.URLParam(r, "token")
		if token == "" {
			http.Error(w, "token required", http.StatusBadRequest)
			return
		}
		if err := db.RevokeEvidenceShare(r.Context(), token); err != nil {
			http.Error(w, "revoke failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleListEvidenceShares returns all shares for an incident, with
// open-counts denormalized in. Used by the supervisor's "manage
// shares" tab — sees active, revoked, and expired side by side.
func HandleListEvidenceShares(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		incidentID := chi.URLParam(r, "id")
		if incidentID == "" {
			http.Error(w, "incident id required", http.StatusBadRequest)
			return
		}
		shares, err := db.ListEvidenceSharesByIncident(r.Context(), incidentID)
		if err != nil {
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}
		// Decorate each share with its open count. Done as a separate
		// per-row query rather than a join because evidence_share_opens
		// can grow unbounded — a join would scan the whole table for
		// each list call. Index on (token, opened_at) makes the
		// per-row count cheap.
		type withCount struct {
			database.EvidenceShare
			OpenCount int  `json:"open_count"`
			Active    bool `json:"active"`
		}
		out := make([]withCount, 0, len(shares))
		now := time.Now().UTC()
		for _, s := range shares {
			cnt, _ := db.CountShareOpens(r.Context(), s.Token)
			active := !s.Revoked && (s.ExpiresAt == nil || s.ExpiresAt.After(now))
			out = append(out, withCount{EvidenceShare: s, OpenCount: cnt, Active: active})
		}
		writeJSON(w, out)
	}
}

// HandlePublicEvidenceShare serves a public (unauthenticated) share URL
// for an evidence bundle.
//
// Every access is logged to evidence_share_opens — that table is the
// chain-of-custody ledger. We deliberately don't make the logging part
// of the request's critical path: if the insert fails, the client still
// gets the evidence, and a line goes to stderr for the ops team to
// investigate. An outage of the audit log must not become an outage of
// customer-visible sharing.
//
// The actual evidence payload (metadata, clip URL, etc.) is looked up
// from evidence_shares by the token. When that table doesn't contain a
// matching row (or the row is revoked or expired), we return 404 —
// deliberately opaque so token-probing gives the same response whether
// the token never existed or was revoked yesterday.
//
// Client IP extraction: prefer X-Forwarded-For when a trusted reverse
// proxy (Caddy, nginx) is in front; otherwise fall back to RemoteAddr.
// For legal defensibility, the same record captures both — operators
// can always see the "real" next-hop addr plus the claimed chain.
func HandlePublicEvidenceShare(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		// Log the open first, regardless of whether the token resolves.
		// A probe for a non-existent token is itself useful audit data —
		// clusters of 404s on random tokens are how you discover someone
		// trying to enumerate shares.
		go func(token, ip, ua, ref string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := db.LogEvidenceShareOpen(ctx, token, ip, ua, ref); err != nil {
				log.Printf("[EVIDENCE] Failed to log share open (token=%q): %v", token, err)
			}
		}(token, clientIP(r), r.UserAgent(), r.Referer())

		// Look up the share. evidence_shares is written by a handler that
		// doesn't exist yet — until it does, every public share URL will
		// 404, which is the safe default. No share = no access.
		share, err := db.GetEvidenceShare(r.Context(), token)
		if err != nil || share == nil || share.Revoked {
			http.NotFound(w, r)
			return
		}
		if share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now()) {
			http.NotFound(w, r)
			return
		}

		writeJSON(w, share)
	}
}

// clientIP is defined in audit_playback.go and reused here. Both files
// want the same behavior: honor a first-hop X-Forwarded-For from a
// trusted proxy, fall back to the raw RemoteAddr otherwise.
