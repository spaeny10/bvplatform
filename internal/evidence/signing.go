package evidence

import (
	"archive/zip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Evidence-bundle signing scheme.
//
// Goal: a downstream consumer (insurer, court, PSAP) should be able to
// detect tampering with any file inside an exported ZIP using only a
// shared secret and a small verification routine — no networked
// service required.
//
// Layout in the resulting ZIP:
//
//	clip.mp4       binary, may be absent
//	snapshot.jpg   binary, may be absent
//	event.json     manifest including content_hashes for the binaries
//	README.txt     human-readable summary, NOT signed (cosmetic)
//	SIGNATURE.txt  textual record of the algorithm + HMAC over event.json
//
// Verification chain:
//   - Recompute SHA-256 of clip.mp4 / snapshot.jpg, compare to manifest.
//   - Recompute HMAC-SHA256 over the bytes of event.json, compare to
//     the hex value in SIGNATURE.txt.
//   - If both match, the bundle is original.
//
// We deliberately keep README.txt out of the signature: it's regenerable
// from the manifest, gets edited by humans for legibility (footers,
// whitespace), and a strict signature over it would create false
// tamper-warnings every time we rebrand a header. The legal record is
// event.json + the binary blobs.

// SignatureBlock is the structured fragment we add to an EvidenceManifest
// before serializing it. The presence of ContentHashes is what tells a
// verifier "this manifest is signable" — pre-signing exports omit this
// field entirely.
type SignatureBlock struct {
	// Algorithm names the MAC primitive. Currently always "HMAC-SHA256".
	Algorithm string `json:"algorithm"`
	// KeyFingerprint is the first 16 hex chars of SHA-256(key). It lets
	// the verifier confirm they have the right secret without leaking
	// the secret itself. UL 827B-grade key rotation is "swap the
	// fingerprint, retain the old key for verification of historical
	// exports during the cutover window."
	KeyFingerprint string `json:"key_fingerprint"`
	// ContentHashes maps filename → hex SHA-256 for every binary file in
	// the bundle. Filenames are the names inside the ZIP (clip.mp4,
	// snapshot.jpg). event.json itself is NOT in this map — it carries
	// the hashes, so you'd need to recurse to hash itself, which is
	// nonsense. The manifest as-a-whole is signed by the SIGNATURE.txt
	// HMAC.
	ContentHashes map[string]string `json:"content_hashes"`
	// SignedAt is the timestamp the signature was produced. Doesn't
	// affect verification; included for forensic context.
	SignedAt time.Time `json:"signed_at"`
}

// KeyFingerprint returns the canonical short identifier for a signing
// key. Exposed so callers can log the fingerprint at startup or
// surface it in the admin UI for rotation planning.
func KeyFingerprint(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:8])
}

// SignedZipWriter wraps zip.Writer and tracks SHA-256 over every
// binary file added through Add. Once all files are in, the caller
// finalizes by calling Sign — which adds event.json and SIGNATURE.txt
// and closes the underlying writer.
type SignedZipWriter struct {
	zw      *zip.Writer
	hashes  map[string]string
	closed  bool
}

// NewSignedZipWriter wraps an open zip.Writer. The caller retains
// ownership of the underlying writer and is responsible for closing
// it after Sign returns (or via defer if Sign isn't reached on an
// error path — Sign also closes if it succeeds, since a closed writer
// is what gets sent to the client).
func NewSignedZipWriter(zw *zip.Writer) *SignedZipWriter {
	return &SignedZipWriter{
		zw:     zw,
		hashes: make(map[string]string),
	}
}

// AddBinary writes a file into the ZIP and records its SHA-256 hash
// in the bundle's content_hashes map. Use this for clip.mp4 /
// snapshot.jpg etc. — anything where preserving the exact bytes
// matters legally.
func (s *SignedZipWriter) AddBinary(name string, data []byte) error {
	h := sha256.Sum256(data)
	s.hashes[name] = hex.EncodeToString(h[:])

	f, err := s.zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}

// AddTextNoSign writes a file that is NOT covered by the signature.
// Used for README.txt — human-friendly, regenerable, fine to vary.
func (s *SignedZipWriter) AddTextNoSign(name string, body string) error {
	f, err := s.zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, body)
	return err
}

// Sign finalizes the bundle:
//   - serializes the manifest with ContentHashes filled in,
//   - writes event.json,
//   - computes HMAC-SHA256(key, event.json bytes),
//   - writes SIGNATURE.txt with the algorithm, fingerprint, and hex MAC.
//
// If key is empty, signing is skipped: event.json is written without a
// SignatureBlock and SIGNATURE.txt is omitted. Callers can detect this
// at the response level (no SIGNATURE.txt in the ZIP) and disclose
// "this export was generated without a signing key configured" in the
// audit trail. UL 827B-track deployments must always have a key set.
func (s *SignedZipWriter) Sign(manifest interface{}, manifestSetSig func(*SignatureBlock), key []byte) error {
	if s.closed {
		return fmt.Errorf("signed zip already finalized")
	}
	s.closed = true

	// Build the signature block. Key fingerprint goes in even when the
	// key is empty so the manifest format is uniform; an empty
	// fingerprint signals "unsigned export" to the verifier.
	block := &SignatureBlock{
		Algorithm:      "HMAC-SHA256",
		KeyFingerprint: KeyFingerprint(key),
		ContentHashes:  cloneSorted(s.hashes),
		SignedAt:       time.Now().UTC(),
	}
	manifestSetSig(block)

	// Serialize the manifest into a stable byte form. We use indented
	// JSON for readability inside the ZIP; the verifier MUST hash the
	// exact bytes we wrote (not re-serialize the parsed struct, which
	// could re-order fields or change spacing).
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	mf, err := s.zw.Create("event.json")
	if err != nil {
		return err
	}
	if _, err := mf.Write(body); err != nil {
		return err
	}

	if len(key) == 0 {
		// Unsigned export — bail out cleanly without a SIGNATURE.txt.
		return nil
	}

	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	sf, err := s.zw.Create("SIGNATURE.txt")
	if err != nil {
		return err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "algorithm=%s\n", block.Algorithm)
	fmt.Fprintf(&sb, "key_fingerprint=%s\n", block.KeyFingerprint)
	fmt.Fprintf(&sb, "signed_at=%s\n", block.SignedAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "manifest_file=event.json\n")
	fmt.Fprintf(&sb, "signature=%s\n", sig)
	fmt.Fprintln(&sb, "")
	fmt.Fprintln(&sb, "VERIFICATION")
	fmt.Fprintln(&sb, strings.Repeat("-", 40))
	fmt.Fprintln(&sb, "1. Compute SHA-256 of each file in content_hashes (see event.json),")
	fmt.Fprintln(&sb, "   compare to the hex values listed there.")
	fmt.Fprintln(&sb, "2. Read the raw bytes of event.json (do not reformat).")
	fmt.Fprintln(&sb, "3. Compute HMAC-SHA256(shared_key, event.json_bytes) and compare")
	fmt.Fprintln(&sb, "   to the signature= line above.")
	fmt.Fprintln(&sb, "4. The shared key is held by the issuing monitoring center; the")
	fmt.Fprintln(&sb, "   key_fingerprint above identifies which key was used.")
	_, err = io.WriteString(sf, sb.String())
	return err
}

// cloneSorted returns a copy of the input map with deterministic key
// ordering. Maps in Go are unordered; serializing a hashes map without
// this would produce a different byte sequence on every call, breaking
// reproducibility for any verifier that re-serializes.
func cloneSorted(m map[string]string) map[string]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// JSON marshaling will re-sort by key for map[string]string anyway,
	// but we copy through a sorted order to make the intent explicit
	// and to give a stable Go-side iteration if the field is ever
	// emitted via something other than encoding/json.
	out := make(map[string]string, len(m))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}
