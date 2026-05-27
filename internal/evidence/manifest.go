package evidence

// manifest.go — ed25519 chain-of-custody manifest signing and verification.
//
// Each time a clip is exported, a compliance PDF is rendered, or an evidence
// share is created, the platform builds a Manifest, serialises it to
// canonical JSON, and signs the bytes with an ed25519 private key.  The
// resulting (signature, key_id) pair is stored alongside the manifest in the
// evidence_manifests append-only table.
//
// Anyone with the corresponding public key can verify a manifest completely
// offline — no database or network access required.  The offline verification
// story is what justifies ed25519 over the existing HMAC-SHA256 path (which
// requires the shared secret).
//
// Design decisions:
//   - Canonical JSON: fields are serialised in a deterministic order via a
//     custom MarshalJSON method so the byte sequence is reproducible across
//     Go versions.  This is critical — ed25519 verifies exact bytes.
//   - key_id: first 16 hex chars of SHA-256(publicKey bytes).  Lets a verifier
//     look up the right public key from a keyring after key rotation without
//     needing to try every key.
//   - No new dependencies: crypto/ed25519, crypto/sha256, encoding/hex,
//     encoding/json are all stdlib.
//
// The existing HMAC SignedZipWriter is untouched; this is an additive path.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Manifest is the canonical chain-of-custody record for one artifact.
// It is serialised to JSON, signed with ed25519, and stored in the DB.
//
// Field ordering in MarshalJSON is fixed so the byte sequence is
// deterministic regardless of Go struct field order.
type Manifest struct {
	// ManifestVersion identifies the schema version; currently always "1".
	ManifestVersion string `json:"manifest_version"`
	// ArtifactType is one of "clip_export", "compliance_report", "evidence_share".
	ArtifactType string `json:"artifact_type"`
	// ArtifactID is the primary identifier of the artifact (e.g. event_id,
	// report_id, share token).
	ArtifactID string `json:"artifact_id"`
	// OrganizationID is the tenant that owns the artifact.
	OrganizationID string `json:"organization_id"`
	// CreatedBy is the user_id (UUID string) of the operator who triggered
	// the export/report.
	CreatedBy string `json:"created_by"`
	// CreatedAt is the timestamp at which this manifest was built.
	CreatedAt time.Time `json:"created_at"`

	// CameraIDs lists every camera covered by the artifact (may be empty
	// for single-camera exports where camera info lives in ArtifactID).
	CameraIDs []string `json:"camera_ids"`
	// SegmentIDs lists the storage-segment identifiers included.
	SegmentIDs []string `json:"segment_ids"`
	// TimeRangeStart / TimeRangeEnd bound the temporal window of evidence.
	TimeRangeStart *time.Time `json:"time_range_start,omitempty"`
	TimeRangeEnd   *time.Time `json:"time_range_end,omitempty"`

	// SourceSegmentHashes maps segment_id → hex SHA-256 of the raw segment
	// file.  This is the "video is truth" anchor: once signed, any
	// alteration to a segment is detectable.  May be empty for artifact
	// types that don't reference raw segments (e.g. compliance_report).
	SourceSegmentHashes map[string]string `json:"source_segment_hashes"`

	// ArtifactSHA256 is the hex SHA-256 of the primary artifact bytes:
	//   clip_export:        SHA-256 of the event.json bytes inside the ZIP.
	//   compliance_report:  SHA-256 of the PDF bytes.
	//   evidence_share:     SHA-256 of the canonical share metadata JSON.
	ArtifactSHA256 string `json:"artifact_sha256"`

	// KeyID is populated by SignManifest; consumers SHOULD NOT set this
	// directly — it is derived from the public key.
	KeyID string `json:"key_id"`
	// SigAlgorithm is always "ed25519".
	SigAlgorithm string `json:"sig_algorithm"`
	// Signature is populated by SignManifest after the manifest bytes are
	// signed.  It is base64-encoded (standard encoding).
	Signature string `json:"signature"`
}

// manifestCanonical is the minimal struct marshalled during signing.
// It omits key_id, sig_algorithm, signature so those fields don't
// participate in the signed payload (they're metadata about the
// signature, not the evidence content).
//
// Field names MUST match the Manifest JSON tags for the same fields.
type manifestCanonical struct {
	ManifestVersion     string            `json:"manifest_version"`
	ArtifactType        string            `json:"artifact_type"`
	ArtifactID          string            `json:"artifact_id"`
	OrganizationID      string            `json:"organization_id"`
	CreatedBy           string            `json:"created_by"`
	CreatedAt           string            `json:"created_at"` // RFC3339Nano
	CameraIDs           []string          `json:"camera_ids"`
	SegmentIDs          []string          `json:"segment_ids"`
	TimeRangeStart      *string           `json:"time_range_start,omitempty"`
	TimeRangeEnd        *string           `json:"time_range_end,omitempty"`
	SourceSegmentHashes map[string]string `json:"source_segment_hashes"`
	ArtifactSHA256      string            `json:"artifact_sha256"`
}

// BuildManifest constructs and returns the canonical JSON bytes that are
// signed by SignManifest.  The output is deterministic: same inputs always
// produce the same bytes.
//
// cameraIDs and segmentIDs are sorted before serialisation.
// SourceSegmentHashes keys are sorted (json.Marshal sorts map keys).
func BuildManifest(m Manifest) ([]byte, error) {
	// Sort slices so the canonical form is deterministic.
	sortedCameras := sortedStringSlice(m.CameraIDs)
	sortedSegments := sortedStringSlice(m.SegmentIDs)

	var tsStart, tsEnd *string
	if m.TimeRangeStart != nil {
		s := m.TimeRangeStart.UTC().Format(time.RFC3339Nano)
		tsStart = &s
	}
	if m.TimeRangeEnd != nil {
		s := m.TimeRangeEnd.UTC().Format(time.RFC3339Nano)
		tsEnd = &s
	}

	hashes := m.SourceSegmentHashes
	if hashes == nil {
		hashes = map[string]string{}
	}

	canon := manifestCanonical{
		ManifestVersion:     "1",
		ArtifactType:        m.ArtifactType,
		ArtifactID:          m.ArtifactID,
		OrganizationID:      m.OrganizationID,
		CreatedBy:           m.CreatedBy,
		CreatedAt:           m.CreatedAt.UTC().Format(time.RFC3339Nano),
		CameraIDs:           sortedCameras,
		SegmentIDs:          sortedSegments,
		TimeRangeStart:      tsStart,
		TimeRangeEnd:        tsEnd,
		SourceSegmentHashes: hashes,
		ArtifactSHA256:      m.ArtifactSHA256,
	}

	b, err := json.Marshal(canon)
	if err != nil {
		return nil, fmt.Errorf("BuildManifest: marshal canonical: %w", err)
	}
	return b, nil
}

// ED25519PublicKeyFingerprint returns the first 16 hex chars of SHA-256
// of the raw public key bytes.  This is the key_id stored in each manifest
// so a verifier can select the right entry from the keyring.
func ED25519PublicKeyFingerprint(pubKey ed25519.PublicKey) string {
	if len(pubKey) == 0 {
		return ""
	}
	h := sha256.Sum256(pubKey)
	return hex.EncodeToString(h[:8]) // 16 hex chars = 8 bytes
}

// SignManifest serialises m into its canonical form, signs the bytes with
// privKey, and returns:
//   - sigB64: base64-encoded ed25519 signature over the canonical JSON,
//   - keyID:  public-key fingerprint (first 16 hex chars of SHA-256(pubkey)),
//   - canonicalJSON: the exact bytes that were signed.
//
// The caller should store sigB64, keyID, and canonicalJSON in the DB row so
// a later VerifyManifest call can re-derive and check everything.
func SignManifest(privKey ed25519.PrivateKey, m Manifest) (sigB64 string, keyID string, canonicalJSON []byte, err error) {
	if len(privKey) == 0 {
		return "", "", nil, fmt.Errorf("SignManifest: private key is empty")
	}

	canonicalJSON, err = BuildManifest(m)
	if err != nil {
		return "", "", nil, err
	}

	sig := ed25519.Sign(privKey, canonicalJSON)
	sigB64 = base64.StdEncoding.EncodeToString(sig)

	pub, ok := privKey.Public().(ed25519.PublicKey)
	if !ok {
		return "", "", nil, fmt.Errorf("SignManifest: could not extract public key")
	}
	keyID = ED25519PublicKeyFingerprint(pub)
	return sigB64, keyID, canonicalJSON, nil
}

// Keyring maps key_id → ed25519 public key.  Used by VerifyManifest to
// locate the right public key after a key rotation.  Callers should build
// this from EVIDENCE_SIGNING_KEYRING config and the current active key.
type Keyring map[string]ed25519.PublicKey

// VerifyManifest re-derives the canonical JSON from m, locates the public
// key in keyring via m.KeyID, and verifies the ed25519 signature.
//
// Returns (true, "") on success.  Returns (false, reason) with a
// human-readable reason on any failure so callers can surface useful
// error messages to operators.
func VerifyManifest(keyring Keyring, m Manifest) (ok bool, reason string) {
	if m.KeyID == "" {
		return false, "manifest has no key_id"
	}
	if m.SigAlgorithm != "ed25519" {
		return false, fmt.Sprintf("unsupported sig_algorithm %q", m.SigAlgorithm)
	}
	if m.Signature == "" {
		return false, "manifest has no signature"
	}

	pubKey, found := keyring[m.KeyID]
	if !found {
		return false, fmt.Sprintf("key_id %q not found in keyring", m.KeyID)
	}

	canonicalJSON, err := BuildManifest(m)
	if err != nil {
		return false, fmt.Sprintf("rebuild canonical JSON: %v", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return false, fmt.Sprintf("decode signature base64: %v", err)
	}

	if !ed25519.Verify(pubKey, canonicalJSON, sigBytes) {
		return false, "signature verification failed"
	}
	return true, ""
}

// ParseED25519PrivateKeyHex decodes a hex string into an ed25519.PrivateKey.
//
// Accepts:
//   - 64 hex chars (32 bytes) — treated as a seed; the full 64-byte key is derived.
//   - 128 hex chars (64 bytes) — already the full private key.
//
// This is the format stored in EVIDENCE_ED25519_PRIVATE_KEY.
// Generate a seed with: openssl rand -hex 32
func ParseED25519PrivateKeyHex(h string) (ed25519.PrivateKey, error) {
	raw, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("ParseED25519PrivateKeyHex: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize: // 32 bytes — derive full key from seed
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize: // 64 bytes — already full key
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("ParseED25519PrivateKeyHex: expected 32 or 64 bytes, got %d", len(raw))
	}
}

// ParseED25519PublicKeyHex decodes a 64-char hex string (32 bytes) into an
// ed25519.PublicKey.  Used for keyring entries (historical public keys stored
// in EVIDENCE_SIGNING_KEYRING).
func ParseED25519PublicKeyHex(h string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(h)
	if err != nil {
		return nil, fmt.Errorf("ParseED25519PublicKeyHex: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("ParseED25519PublicKeyHex: expected %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// sortedStringSlice returns a sorted copy of s (nil/empty returns empty slice).
func sortedStringSlice(s []string) []string {
	if len(s) == 0 {
		return []string{}
	}
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}
