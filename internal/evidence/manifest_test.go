package evidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// fixedTime is a stable timestamp used in tests so canonical JSON is
// deterministic across runs.
var fixedTime = time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

func testManifest() Manifest {
	return Manifest{
		ManifestVersion: "1",
		ArtifactType:    "clip_export",
		ArtifactID:      "event-123",
		OrganizationID:  "org-abc",
		CreatedBy:       "user-456",
		CreatedAt:       fixedTime,
		CameraIDs:       []string{"cam-b", "cam-a"},
		SegmentIDs:      []string{"seg-2", "seg-1"},
		SourceSegmentHashes: map[string]string{
			"seg-1": "aabbcc",
			"seg-2": "ddeeff",
		},
		ArtifactSHA256: "deadbeef",
		SigAlgorithm:   "ed25519",
	}
}

// TestBuildManifest_Deterministic verifies that the same input always
// produces the same canonical JSON bytes.
func TestBuildManifest_Deterministic(t *testing.T) {
	m := testManifest()
	b1, err := BuildManifest(m)
	if err != nil {
		t.Fatalf("BuildManifest error: %v", err)
	}
	b2, err := BuildManifest(m)
	if err != nil {
		t.Fatalf("BuildManifest error (2nd call): %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("BuildManifest not deterministic:\n%s\n!=\n%s", b1, b2)
	}
}

// TestBuildManifest_CameraIDsSorted verifies that CameraIDs are sorted in
// the canonical JSON regardless of input order.
func TestBuildManifest_CameraIDsSorted(t *testing.T) {
	m1 := testManifest()
	m1.CameraIDs = []string{"cam-z", "cam-a", "cam-m"}

	m2 := testManifest()
	m2.CameraIDs = []string{"cam-a", "cam-m", "cam-z"}

	b1, _ := BuildManifest(m1)
	b2, _ := BuildManifest(m2)

	if !bytes.Equal(b1, b2) {
		t.Fatalf("canonical JSON differs for different CameraID orderings:\n%s\n!=\n%s", b1, b2)
	}
}

// TestSignVerify_RoundTrip checks the happy path: sign then verify succeeds.
func TestSignVerify_RoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := testManifest()

	sigB64, keyID, _, err := SignManifest(priv, m)
	if err != nil {
		t.Fatalf("SignManifest: %v", err)
	}
	if sigB64 == "" || keyID == "" {
		t.Fatal("SignManifest returned empty sig or keyID")
	}

	m.Signature = sigB64
	m.KeyID = keyID
	m.SigAlgorithm = "ed25519"

	kr := Keyring{keyID: pub}
	ok, reason := VerifyManifest(kr, m)
	if !ok {
		t.Fatalf("VerifyManifest failed: %s", reason)
	}
}

// TestVerify_TamperedArtifactSHA256 checks that changing ArtifactSHA256
// after signing causes verification to fail.
func TestVerify_TamperedArtifactSHA256(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := testManifest()
	sigB64, keyID, _, err := SignManifest(priv, m)
	if err != nil {
		t.Fatal(err)
	}

	m.Signature = sigB64
	m.KeyID = keyID
	m.SigAlgorithm = "ed25519"
	// Tamper the artifact hash.
	m.ArtifactSHA256 = "00000000tampered"

	kr := Keyring{keyID: pub}
	ok, reason := VerifyManifest(kr, m)
	if ok {
		t.Fatal("expected VerifyManifest to fail after tampering ArtifactSHA256, but it succeeded")
	}
	if !strings.Contains(reason, "verification failed") {
		t.Fatalf("unexpected failure reason: %s", reason)
	}
}

// TestVerify_TamperedSignature checks that corrupting the signature bytes
// causes verification to fail.
func TestVerify_TamperedSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := testManifest()
	sigB64, keyID, _, err := SignManifest(priv, m)
	if err != nil {
		t.Fatal(err)
	}

	// Flip the first byte of the decoded signature.
	sigBytes, _ := base64.StdEncoding.DecodeString(sigB64)
	sigBytes[0] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(sigBytes)

	m.Signature = tampered
	m.KeyID = keyID
	m.SigAlgorithm = "ed25519"

	kr := Keyring{keyID: pub}
	ok, reason := VerifyManifest(kr, m)
	if ok {
		t.Fatal("expected failure with tampered signature bytes")
	}
	if !strings.Contains(reason, "verification failed") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

// TestVerify_WrongKey ensures that verifying with a different keypair fails.
func TestVerify_WrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)

	m := testManifest()
	sigB64, keyID, _, err := SignManifest(priv, m)
	if err != nil {
		t.Fatal(err)
	}

	m.Signature = sigB64
	m.KeyID = keyID
	m.SigAlgorithm = "ed25519"

	// Keyring maps the same key_id to a *different* public key.
	kr := Keyring{keyID: wrongPub}
	ok, reason := VerifyManifest(kr, m)
	if ok {
		t.Fatal("expected failure with wrong public key")
	}
	if !strings.Contains(reason, "verification failed") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

// TestVerify_KeyRotation verifies that a manifest signed with key A still
// verifies after key B is added to the keyring (key_id resolves to the
// right entry).
func TestVerify_KeyRotation(t *testing.T) {
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)

	m := testManifest()
	sigB64, keyIDA, _, err := SignManifest(privA, m)
	if err != nil {
		t.Fatal(err)
	}

	m.Signature = sigB64
	m.KeyID = keyIDA
	m.SigAlgorithm = "ed25519"

	// Keyring has both A and B.
	keyIDB := ED25519PublicKeyFingerprint(pubB)
	kr := Keyring{
		keyIDA: pubA,
		keyIDB: pubB,
	}
	ok, reason := VerifyManifest(kr, m)
	if !ok {
		t.Fatalf("expected success with keyring containing both keys, got: %s", reason)
	}
}

// TestVerify_MissingKeyID checks that a manifest without a key_id fails.
func TestVerify_MissingKeyID(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	m := testManifest()
	m.SigAlgorithm = "ed25519"
	m.Signature = "dummysig"
	// KeyID deliberately empty.

	kr := Keyring{"some-id": pub}
	ok, reason := VerifyManifest(kr, m)
	if ok {
		t.Fatal("expected failure for missing key_id")
	}
	if !strings.Contains(reason, "no key_id") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

// TestVerify_KeyNotInKeyring checks the "key_id not found" path.
func TestVerify_KeyNotInKeyring(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	m := testManifest()
	sigB64, keyID, _, _ := SignManifest(priv, m)
	m.Signature = sigB64
	m.KeyID = keyID
	m.SigAlgorithm = "ed25519"

	// Keyring maps to a different key_id.
	otherFP := ED25519PublicKeyFingerprint(pub) + "xx" // corrupt fingerprint
	kr := Keyring{otherFP: pub}
	ok, reason := VerifyManifest(kr, m)
	if ok {
		t.Fatal("expected failure when key_id not in keyring")
	}
	if !strings.Contains(reason, "not found in keyring") {
		t.Fatalf("unexpected reason: %s", reason)
	}
}

// TestParseED25519PrivateKeyHex checks round-trip of seed and full-key parsing.
func TestParseED25519PrivateKeyHex(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	// 64-byte full key.
	hexStr := make([]byte, ed25519.PrivateKeySize*2)
	encodeHex(hexStr, priv)
	parsed, err := ParseED25519PrivateKeyHex(string(hexStr))
	if err != nil {
		t.Fatalf("ParseED25519PrivateKeyHex (full key): %v", err)
	}
	// Verify round-trip: sign + verify.
	testMsg := []byte("hello world")
	sig := ed25519.Sign(parsed, testMsg)
	if !ed25519.Verify(pub, testMsg, sig) {
		t.Fatal("parsed full key produces invalid signature")
	}

	// 32-byte seed.
	seedHex := make([]byte, ed25519.SeedSize*2)
	encodeHex(seedHex, priv.Seed())
	parsedSeed, err := ParseED25519PrivateKeyHex(string(seedHex))
	if err != nil {
		t.Fatalf("ParseED25519PrivateKeyHex (seed): %v", err)
	}
	sig2 := ed25519.Sign(parsedSeed, testMsg)
	// Seed-derived key should have the same public key as the original.
	derivedPub := parsedSeed.Public().(ed25519.PublicKey)
	if !ed25519.Verify(derivedPub, testMsg, sig2) {
		t.Fatal("parsed seed key produces invalid signature")
	}
}

// encodeHex is a tiny helper so we don't import encoding/hex in test helpers.
func encodeHex(dst, src []byte) {
	const hextable = "0123456789abcdef"
	for i, v := range src {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
}
