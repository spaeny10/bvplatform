package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"onvif-tool/internal/database"
)

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
