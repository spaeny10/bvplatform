package api

// evidence_manifest_handler.go — REST endpoints for chain-of-custody manifests.
//
// Routes (registered in router.go):
//
//   GET /api/evidence/manifests/{id}        — fetch a manifest (tenant-scoped)
//   GET /api/evidence/manifests/{id}/verify — re-derive canonical JSON, check
//                                             ed25519 signature; return {ok, reason}
//   GET /api/evidence/manifests             — list tenant's manifests (paginated)
//
// All three are GET requests; CSRF middleware exempts GET/HEAD/OPTIONS.
// Auth is via the standard RequireAuth middleware already on the /api group.

import (
	"crypto/ed25519"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/evidence"
)

// HandleGetManifest fetches a single chain-of-custody manifest by UUID,
// scoped to the caller's organization.
//
// Returns 404 when the manifest doesn't exist OR belongs to another org
// (cross-tenant isolation: same opaque 404 to avoid leaking existence).
func HandleGetManifest(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		idStr := chi.URLParam(r, "id")
		manifestID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid manifest id", http.StatusBadRequest)
			return
		}

		row, err := db.GetManifest(r.Context(), manifestID, claims.OrganizationID)
		if err != nil {
			// ErrManifestNotFound is the cross-tenant isolation 404.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		writeJSON(w, row)
	}
}

// HandleListManifests returns the tenant's manifests in reverse-chronological
// order.  Optional query params:
//   - type  filter to artifact_type (clip_export | compliance_report | evidence_share)
//   - limit (default 50, max 200)
//   - offset
func HandleListManifests(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		artifactType := r.URL.Query().Get("type")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 {
			limit = 50
		}

		rows, err := db.ListManifests(r.Context(), claims.OrganizationID, artifactType, limit, offset)
		if err != nil {
			slog.Error("ListManifests", "error", err, "org", claims.OrganizationID)
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}
		if rows == nil {
			rows = []database.EvidenceManifestRow{}
		}
		writeJSON(w, rows)
	}
}

// verifyResponse is the JSON shape returned by the /verify endpoint.
type verifyResponse struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// HandleVerifyManifest re-derives the canonical JSON from the stored manifest,
// looks up the public key from cfg's keyring, and verifies the ed25519
// signature.  Returns {"ok":true} on success or {"ok":false,"reason":"..."}.
//
// This is deliberately a GET so it can be called by monitoring scripts and
// does not mutate state.
func HandleVerifyManifest(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		idStr := chi.URLParam(r, "id")
		manifestID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid manifest id", http.StatusBadRequest)
			return
		}

		row, err := db.GetManifest(r.Context(), manifestID, claims.OrganizationID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Build the keyring from config: active key + historical entries.
		keyring, buildErr := buildKeyringFromConfig(cfg)
		if buildErr != nil || len(keyring) == 0 {
			// No keys configured — can't verify.
			writeJSON(w, verifyResponse{
				OK:     false,
				Reason: "no ed25519 keys configured; EVIDENCE_ED25519_PRIVATE_KEY must be set",
			})
			return
		}

		// Re-hydrate a Manifest value from the DB row so BuildManifest
		// produces the exact same canonical JSON that was signed.
		m := manifestRowToEvidence(row)
		ok, reason := evidence.VerifyManifest(keyring, m)
		writeJSON(w, verifyResponse{OK: ok, Reason: reason})
	}
}

// buildKeyringFromConfig parses EVIDENCE_ED25519_PRIVATE_KEY (active key)
// and EVIDENCE_SIGNING_KEYRING (JSON map of key_id → hex public key) into an
// evidence.Keyring.
//
// The active private key's public key is always added; entries in
// EVIDENCE_SIGNING_KEYRING supplement it for rotation support (old public keys
// so manifests signed with previous keys remain verifiable).
func buildKeyringFromConfig(cfg *config.Config) (evidence.Keyring, error) {
	kr := make(evidence.Keyring)

	// Active private key → derive public key → add to keyring.
	if cfg.EvidenceED25519PrivateKey != "" {
		priv, err := evidence.ParseED25519PrivateKeyHex(cfg.EvidenceED25519PrivateKey)
		if err == nil {
			pub := priv.Public().(ed25519.PublicKey)
			fp := evidence.ED25519PublicKeyFingerprint(pub)
			kr[fp] = pub
		}
	}

	// Historical public keys from keyring JSON.
	// Format: {"<key_id>": "<64-hex-public-key>"}
	// key_id is the fingerprint (16 hex chars); value is the raw 32-byte
	// public key encoded as 64 hex chars.
	if cfg.EvidenceSigningKeyring != "" {
		var raw map[string]string
		if err := json.Unmarshal([]byte(cfg.EvidenceSigningKeyring), &raw); err == nil {
			for keyID, hexPub := range raw {
				if len(keyID) == 0 {
					continue
				}
				pub, err := evidence.ParseED25519PublicKeyHex(hexPub)
				if err == nil {
					kr[keyID] = pub
				}
			}
		}
	}

	return kr, nil
}

// manifestRowToEvidence converts a DB row back into an evidence.Manifest
// for re-verification.  The key_id + signature fields are preserved so
// VerifyManifest can locate the right public key and verify.
func manifestRowToEvidence(row *database.EvidenceManifestRow) evidence.Manifest {
	m := evidence.Manifest{
		ManifestVersion:     "1",
		ArtifactType:        row.ArtifactType,
		ArtifactID:          row.ArtifactID,
		OrganizationID:      row.OrganizationID,
		CreatedBy:           row.CreatedBy.String(),
		CreatedAt:           row.CreatedAt.UTC(),
		CameraIDs:           row.CameraIDs,
		SegmentIDs:          row.SegmentIDs,
		TimeRangeStart:      row.TimeRangeStart,
		TimeRangeEnd:        row.TimeRangeEnd,
		SourceSegmentHashes: row.SourceSegmentHashes,
		ArtifactSHA256:      row.ArtifactSHA256,
		SigAlgorithm:        row.SigAlgorithm,
		Signature:           row.Signature,
		KeyID:               row.KeyID,
	}
	if m.CameraIDs == nil {
		m.CameraIDs = []string{}
	}
	if m.SegmentIDs == nil {
		m.SegmentIDs = []string{}
	}
	if m.SourceSegmentHashes == nil {
		m.SourceSegmentHashes = map[string]string{}
	}
	return m
}
