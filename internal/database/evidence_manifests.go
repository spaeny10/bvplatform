package database

// evidence_manifests.go — append-only DB operations for the chain-of-custody
// manifest table introduced in migration 0026.
//
// Design constraints enforced here (in addition to the DB-level trigger):
//
//   - No UPDATE or DELETE methods exist.  The append-only guarantee is
//     enforced at both the DB layer (trigger) and the code layer (absence
//     of mutation methods).
//   - All read queries are tenant-scoped via organization_id.
//   - GetManifest enforces tenant ownership: a caller from org B cannot fetch
//     org A's manifests (returns ErrManifestNotFound).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrManifestNotFound is returned when a manifest does not exist or is
// not owned by the requesting organization.
var ErrManifestNotFound = errors.New("evidence manifest not found")

// EvidenceManifestRow mirrors the evidence_manifests table columns.
type EvidenceManifestRow struct {
	ManifestID           uuid.UUID        `json:"manifest_id"`
	ArtifactType         string           `json:"artifact_type"`
	ArtifactID           string           `json:"artifact_id"`
	OrganizationID       string           `json:"organization_id"`
	CreatedBy            uuid.UUID        `json:"created_by"`
	CreatedAt            time.Time        `json:"created_at"`
	CameraIDs            []string         `json:"camera_ids"`
	SegmentIDs           []string         `json:"segment_ids"`
	TimeRangeStart       *time.Time       `json:"time_range_start,omitempty"`
	TimeRangeEnd         *time.Time       `json:"time_range_end,omitempty"`
	SourceSegmentHashes  map[string]string `json:"source_segment_hashes"`
	ArtifactSHA256       string           `json:"artifact_sha256"`
	Signature            string           `json:"signature"`
	KeyID                string           `json:"key_id"`
	SigAlgorithm         string           `json:"sig_algorithm"`
	ParentManifestID     *uuid.UUID       `json:"parent_manifest_id,omitempty"`
	ManifestJSON         json.RawMessage  `json:"manifest_json"`
}

// InsertManifestInput carries everything needed to write one manifest row.
type InsertManifestInput struct {
	ArtifactType         string
	ArtifactID           string
	OrganizationID       string
	CreatedBy            uuid.UUID
	CameraIDs            []string
	SegmentIDs           []string
	TimeRangeStart       *time.Time
	TimeRangeEnd         *time.Time
	SourceSegmentHashes  map[string]string
	ArtifactSHA256       string
	Signature            string
	KeyID                string
	SigAlgorithm         string
	ParentManifestID     *uuid.UUID
	// ManifestJSON is the canonical JSON bytes that were signed.
	ManifestJSON         []byte
}

// InsertManifest writes a new manifest row and returns the generated
// manifest_id.  This is the only write operation; there is no Update or
// Delete.
func (db *DB) InsertManifest(ctx context.Context, in InsertManifestInput) (uuid.UUID, error) {
	if in.SigAlgorithm == "" {
		in.SigAlgorithm = "ed25519"
	}

	cameraJSON, err := json.Marshal(in.CameraIDs)
	if err != nil {
		return uuid.Nil, fmt.Errorf("InsertManifest: marshal camera_ids: %w", err)
	}
	segJSON, err := json.Marshal(in.SegmentIDs)
	if err != nil {
		return uuid.Nil, fmt.Errorf("InsertManifest: marshal segment_ids: %w", err)
	}
	hashJSON, err := json.Marshal(in.SourceSegmentHashes)
	if err != nil {
		return uuid.Nil, fmt.Errorf("InsertManifest: marshal source_segment_hashes: %w", err)
	}

	const q = `
INSERT INTO evidence_manifests
    (artifact_type, artifact_id, organization_id, created_by,
     camera_ids, segment_ids,
     time_range_start, time_range_end,
     source_segment_hashes, artifact_sha256,
     signature, key_id, sig_algorithm,
     parent_manifest_id, manifest_json)
VALUES
    ($1, $2, $3, $4,
     $5, $6,
     $7, $8,
     $9, $10,
     $11, $12, $13,
     $14, $15)
RETURNING manifest_id`

	var id uuid.UUID
	err = db.Pool.QueryRow(ctx, q,
		in.ArtifactType,
		in.ArtifactID,
		in.OrganizationID,
		in.CreatedBy,
		cameraJSON,
		segJSON,
		in.TimeRangeStart,
		in.TimeRangeEnd,
		hashJSON,
		in.ArtifactSHA256,
		in.Signature,
		in.KeyID,
		in.SigAlgorithm,
		in.ParentManifestID,
		in.ManifestJSON,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("InsertManifest: %w", err)
	}
	return id, nil
}

// GetManifest fetches a manifest by ID, scoped to organizationID.
// Returns ErrManifestNotFound when the row doesn't exist or belongs to
// a different org (cross-tenant isolation).
func (db *DB) GetManifest(ctx context.Context, manifestID uuid.UUID, organizationID string) (*EvidenceManifestRow, error) {
	const q = `
SELECT manifest_id, artifact_type, artifact_id, organization_id, created_by,
       created_at, camera_ids, segment_ids,
       time_range_start, time_range_end,
       source_segment_hashes, artifact_sha256,
       signature, key_id, sig_algorithm,
       parent_manifest_id, manifest_json
FROM evidence_manifests
WHERE manifest_id = $1 AND organization_id = $2`

	row := db.Pool.QueryRow(ctx, q, manifestID, organizationID)
	return scanManifestRow(row)
}

// ListManifests returns the manifests for a tenant in reverse-chronological
// order.  limit is capped at 200; offset is for pagination.
func (db *DB) ListManifests(ctx context.Context, organizationID string, artifactType string, limit, offset int) ([]EvidenceManifestRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	const q = `
SELECT manifest_id, artifact_type, artifact_id, organization_id, created_by,
       created_at, camera_ids, segment_ids,
       time_range_start, time_range_end,
       source_segment_hashes, artifact_sha256,
       signature, key_id, sig_algorithm,
       parent_manifest_id, manifest_json
FROM evidence_manifests
WHERE organization_id = $1
  AND ($2 = '' OR artifact_type = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4`

	rows, err := db.Pool.Query(ctx, q, organizationID, artifactType, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("ListManifests: %w", err)
	}
	defer rows.Close()

	var out []EvidenceManifestRow
	for rows.Next() {
		r, err := scanManifestRow(rows)
		if err != nil {
			return nil, fmt.Errorf("ListManifests scan: %w", err)
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListManifests rows: %w", err)
	}
	return out, nil
}

// scanManifestRow reads one EvidenceManifestRow from a pgx.Row or pgx.Rows
// scan target.
func scanManifestRow(row interface {
	Scan(...any) error
}) (*EvidenceManifestRow, error) {
	var r EvidenceManifestRow
	var cameraJSON, segJSON, hashJSON, manifestJSON []byte

	err := row.Scan(
		&r.ManifestID,
		&r.ArtifactType,
		&r.ArtifactID,
		&r.OrganizationID,
		&r.CreatedBy,
		&r.CreatedAt,
		&cameraJSON,
		&segJSON,
		&r.TimeRangeStart,
		&r.TimeRangeEnd,
		&hashJSON,
		&r.ArtifactSHA256,
		&r.Signature,
		&r.KeyID,
		&r.SigAlgorithm,
		&r.ParentManifestID,
		&manifestJSON,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(cameraJSON, &r.CameraIDs); err != nil {
		r.CameraIDs = []string{}
	}
	if err := json.Unmarshal(segJSON, &r.SegmentIDs); err != nil {
		r.SegmentIDs = []string{}
	}
	if err := json.Unmarshal(hashJSON, &r.SourceSegmentHashes); err != nil {
		r.SourceSegmentHashes = map[string]string{}
	}
	r.ManifestJSON = json.RawMessage(manifestJSON)
	return &r, nil
}
