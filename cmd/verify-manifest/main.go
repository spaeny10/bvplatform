// cmd/verify-manifest — offline ed25519 manifest verifier.
//
// Usage:
//
//	verify-manifest -manifest manifest.json -pubkey <64-hex-chars>
//	verify-manifest -manifest manifest.json -pubkeyfile pubkey.hex
//
// The manifest JSON must be the canonical JSON payload stored in the
// manifest_json column (or exported from the /api/evidence/manifests/{id}
// endpoint as the "manifest_json" field).
//
// Exit codes:
//
//	0 — signature valid
//	1 — signature invalid or bad input
//
// This tool is the "third party verifies independently" proof that justifies
// ed25519 over the HMAC-SHA256 path: anyone with the public key can verify a
// manifest without needing the private key or database access.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"ironsight/internal/evidence"
)

func main() {
	var (
		manifestFile = flag.String("manifest", "", "path to manifest JSON file (from manifest_json column)")
		pubkeyHex    = flag.String("pubkey", "", "ed25519 public key as 64 hex chars")
		pubkeyFile   = flag.String("pubkeyfile", "", "file containing ed25519 public key as 64 hex chars")
	)
	flag.Parse()

	if *manifestFile == "" {
		fmt.Fprintln(os.Stderr, "error: -manifest is required")
		flag.Usage()
		os.Exit(1)
	}

	// Load public key.
	var pubHex string
	switch {
	case *pubkeyHex != "":
		pubHex = *pubkeyHex
	case *pubkeyFile != "":
		raw, err := os.ReadFile(*pubkeyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read pubkeyfile: %v\n", err)
			os.Exit(1)
		}
		pubHex = string(raw)
		// Trim whitespace / newline.
		for len(pubHex) > 0 && (pubHex[len(pubHex)-1] == '\n' || pubHex[len(pubHex)-1] == '\r' || pubHex[len(pubHex)-1] == ' ') {
			pubHex = pubHex[:len(pubHex)-1]
		}
	default:
		fmt.Fprintln(os.Stderr, "error: -pubkey or -pubkeyfile is required")
		flag.Usage()
		os.Exit(1)
	}

	pubBytes, err := hex.DecodeString(pubHex)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		fmt.Fprintf(os.Stderr, "error: invalid public key (want %d hex chars, got %d): %v\n",
			ed25519.PublicKeySize*2, len(pubHex), err)
		os.Exit(1)
	}
	pub := ed25519.PublicKey(pubBytes)

	// Load manifest JSON.
	manifestBytes, err := os.ReadFile(*manifestFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read manifest: %v\n", err)
		os.Exit(1)
	}

	// The manifest_json column contains the *canonical* JSON (produced by
	// evidence.BuildManifest) — NOT the full EvidenceManifestRow JSON.
	// We parse it into a canonicalManifest struct that matches BuildManifest's
	// manifestCanonical layout, then reconstruct an evidence.Manifest so
	// VerifyManifest can re-derive the canonical bytes and verify the sig.
	//
	// The manifest file passed here may be either:
	//   (a) the raw canonical JSON (manifest_json column value), or
	//   (b) the full manifest row JSON (EvidenceManifestRow), which includes
	//       a "manifest_json" sub-field.
	//
	// We try (b) first; fall back to (a).
	type rowShape struct {
		ManifestJSON json.RawMessage `json:"manifest_json"`
		Signature    string          `json:"signature"`
		KeyID        string          `json:"key_id"`
		SigAlgorithm string          `json:"sig_algorithm"`
	}
	var row rowShape
	var canonBytes []byte
	var signature, keyID, sigAlgorithm string

	if jsonErr := json.Unmarshal(manifestBytes, &row); jsonErr == nil && len(row.ManifestJSON) > 2 {
		// Shape (b): full row JSON.
		canonBytes = []byte(row.ManifestJSON)
		signature = row.Signature
		keyID = row.KeyID
		sigAlgorithm = row.SigAlgorithm
	} else {
		// Shape (a): raw canonical JSON — signature must come from a separate
		// command-line flag (not yet supported; print guidance).
		fmt.Fprintln(os.Stderr, "error: manifest file must be the full EvidenceManifestRow JSON "+
			"(as returned by GET /api/evidence/manifests/{id}), which includes the "+
			"\"manifest_json\", \"signature\", and \"key_id\" fields.")
		os.Exit(1)
	}

	if signature == "" || keyID == "" {
		fmt.Fprintln(os.Stderr, "error: manifest has no signature or key_id — was it signed?")
		os.Exit(1)
	}

	// Parse canonical bytes into the minimal struct we need for Manifest.
	type canonicalShape struct {
		ManifestVersion     string            `json:"manifest_version"`
		ArtifactType        string            `json:"artifact_type"`
		ArtifactID          string            `json:"artifact_id"`
		OrganizationID      string            `json:"organization_id"`
		CreatedBy           string            `json:"created_by"`
		CreatedAt           string            `json:"created_at"`
		CameraIDs           []string          `json:"camera_ids"`
		SegmentIDs          []string          `json:"segment_ids"`
		TimeRangeStart      *string           `json:"time_range_start,omitempty"`
		TimeRangeEnd        *string           `json:"time_range_end,omitempty"`
		SourceSegmentHashes map[string]string `json:"source_segment_hashes"`
		ArtifactSHA256      string            `json:"artifact_sha256"`
	}
	var canon canonicalShape
	if err := json.Unmarshal(canonBytes, &canon); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse canonical manifest JSON: %v\n", err)
		os.Exit(1)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, canon.CreatedAt)
	m := evidence.Manifest{
		ManifestVersion:     canon.ManifestVersion,
		ArtifactType:        canon.ArtifactType,
		ArtifactID:          canon.ArtifactID,
		OrganizationID:      canon.OrganizationID,
		CreatedBy:           canon.CreatedBy,
		CreatedAt:           createdAt,
		CameraIDs:           canon.CameraIDs,
		SegmentIDs:          canon.SegmentIDs,
		SourceSegmentHashes: canon.SourceSegmentHashes,
		ArtifactSHA256:      canon.ArtifactSHA256,
		SigAlgorithm:        sigAlgorithm,
		Signature:           signature,
		KeyID:               keyID,
	}
	if canon.TimeRangeStart != nil {
		t, _ := time.Parse(time.RFC3339Nano, *canon.TimeRangeStart)
		m.TimeRangeStart = &t
	}
	if canon.TimeRangeEnd != nil {
		t, _ := time.Parse(time.RFC3339Nano, *canon.TimeRangeEnd)
		m.TimeRangeEnd = &t
	}

	fp := evidence.ED25519PublicKeyFingerprint(pub)
	kr := evidence.Keyring{fp: pub}

	ok, reason := evidence.VerifyManifest(kr, m)
	if ok {
		fmt.Printf("OK  manifest %s artifact_type=%s artifact_id=%s key_id=%s\n",
			keyID, m.ArtifactType, m.ArtifactID, m.KeyID)
		os.Exit(0)
	}

	fmt.Printf("FAIL  %s\n", reason)
	os.Exit(1)
}
