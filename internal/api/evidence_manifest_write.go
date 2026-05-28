package api

// evidence_manifest_write.go — helpers used by compliance_handler and
// evidence_export to build, sign, and persist chain-of-custody manifests.
//
// These helpers are intentionally thin: they call evidence.BuildManifest +
// evidence.SignManifest, then database.InsertManifest.  Signing errors are
// logged but do NOT abort the parent HTTP response — a missing key means
// manifests are inserted unsigned; the export/report still succeeds.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
	"ironsight/internal/evidence"
)


// SHA256Hex returns the lowercase hex SHA-256 of b.
func SHA256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// writeManifest builds, signs, and inserts one manifest row.
// It is a best-effort call: errors are logged but not propagated to the
// HTTP response so a signing-key misconfiguration doesn't break exports.
//
// Arguments:
//   - ctx, db, cfg: standard wiring
//   - artifactType: "clip_export" | "compliance_report" | "evidence_share"
//   - artifactID:   primary key of the artifact (event_id as string, report_id, etc.)
//   - orgID:        tenant organization_id
//   - createdByID:  user UUID who triggered the action
//   - cameraIDs:    cameras covered by the artifact (may be nil/empty)
//   - segmentIDs:   segment identifiers covered (may be nil/empty)
//   - trStart/End:  temporal window (may be nil)
//   - segHashes:    map of segmentID → hex SHA-256 (may be nil)
//   - artifactBytes: the primary artifact bytes to hash (event.json for exports,
//                    PDF bytes for reports)
//   - parentID:     optional parent manifest UUID (for chaining; pass uuid.Nil)
func writeManifest(
	ctx context.Context,
	db *database.DB,
	cfg *config.Config,
	artifactType string,
	artifactID string,
	orgID string,
	createdByID uuid.UUID,
	cameraIDs []string,
	segmentIDs []string,
	trStart, trEnd *time.Time,
	segHashes map[string]string,
	artifactBytes []byte,
	parentID uuid.UUID,
) {
	if db == nil {
		return
	}

	artifactSHA := SHA256Hex(artifactBytes)

	if cameraIDs == nil {
		cameraIDs = []string{}
	}
	if segmentIDs == nil {
		segmentIDs = []string{}
	}
	if segHashes == nil {
		segHashes = map[string]string{}
	}

	now := time.Now().UTC()
	m := evidence.Manifest{
		ManifestVersion:     "1",
		ArtifactType:        artifactType,
		ArtifactID:          artifactID,
		OrganizationID:      orgID,
		CreatedBy:           createdByID.String(),
		CreatedAt:           now,
		CameraIDs:           cameraIDs,
		SegmentIDs:          segmentIDs,
		TimeRangeStart:      trStart,
		TimeRangeEnd:        trEnd,
		SourceSegmentHashes: segHashes,
		ArtifactSHA256:      artifactSHA,
		SigAlgorithm:        "ed25519",
	}

	var sigB64, keyID string
	var canonicalJSON []byte

	if cfg != nil && cfg.EvidenceED25519PrivateKey != "" {
		priv, err := evidence.ParseED25519PrivateKeyHex(cfg.EvidenceED25519PrivateKey)
		if err != nil {
			slog.Warn("writeManifest: invalid EVIDENCE_ED25519_PRIVATE_KEY, manifest will be unsigned",
				"artifact_type", artifactType, "artifact_id", artifactID, "error", err)
		} else {
			sigB64, keyID, canonicalJSON, err = evidence.SignManifest(priv, m)
			if err != nil {
				slog.Warn("writeManifest: SignManifest error, manifest will be unsigned",
					"artifact_type", artifactType, "artifact_id", artifactID, "error", err)
				sigB64, keyID, canonicalJSON = "", "", nil
			}
		}
	}

	if canonicalJSON == nil {
		// Build canonical JSON even when not signed so the manifest_json
		// column is always populated.
		var buildErr error
		canonicalJSON, buildErr = evidence.BuildManifest(m)
		if buildErr != nil {
			slog.Error("writeManifest: BuildManifest failed",
				"artifact_type", artifactType, "artifact_id", artifactID, "error", buildErr)
			return
		}
	}

	var parentPtr *uuid.UUID
	if parentID != uuid.Nil {
		parentPtr = &parentID
	}

	in := database.InsertManifestInput{
		ArtifactType:        artifactType,
		ArtifactID:          artifactID,
		OrganizationID:      orgID,
		CreatedBy:           createdByID,
		CameraIDs:           cameraIDs,
		SegmentIDs:          segmentIDs,
		TimeRangeStart:      trStart,
		TimeRangeEnd:        trEnd,
		SourceSegmentHashes: segHashes,
		ArtifactSHA256:      artifactSHA,
		Signature:           sigB64,
		KeyID:               keyID,
		SigAlgorithm:        "ed25519",
		ParentManifestID:    parentPtr,
		ManifestJSON:        canonicalJSON,
	}

	manifestID, err := db.InsertManifest(ctx, in)
	if err != nil {
		slog.Error("writeManifest: InsertManifest failed",
			"artifact_type", artifactType, "artifact_id", artifactID, "error", err)
		return
	}

	slog.Info("writeManifest: manifest created",
		"manifest_id", manifestID,
		"artifact_type", artifactType,
		"artifact_id", artifactID,
		"signed", sigB64 != "",
		"key_id", fmt.Sprintf("%.8s", keyID),
	)
}
