package crypto

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// hexKey is a known-good 32-byte AES-256 key used across tests.
// Hard-coded test value — never plumb into runtime config.
const hexKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func mustKey(t *testing.T, s string) []byte {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatalf("ParseKey(%q): %v", s, err)
	}
	return k
}

// TestParseKey_AllEncodings asserts that operators can supply the same
// 32-byte secret in either of the accepted encodings: 64-char hex or
// 44-char std-base64. The raw-32-byte path was dropped because a
// 32-char string is just as plausibly a half-typed 64-char hex key,
// and silently accepting it would let an AES-128-strength input slip
// into an AES-256 deployment.
func TestParseKey_AllEncodings(t *testing.T) {
	rawBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		t.Fatal(err)
	}
	rawStr := string(rawBytes) // 32 raw bytes — used to verify decoded equivalence
	b64 := base64.StdEncoding.EncodeToString(rawBytes)

	cases := []struct{ name, in string }{
		{"hex", hexKey},
		{"std-base64", b64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, err := ParseKey(tc.in)
			if err != nil {
				t.Fatalf("ParseKey: %v", err)
			}
			if len(k) != 32 {
				t.Errorf("len(key) = %d, want 32", len(k))
			}
			if string(k) != rawStr {
				t.Errorf("decoded key mismatch")
			}
		})
	}
}

// TestParseKey_RejectsShortKey: a 16-byte hex/base64 must NOT silently
// become an AES-128 key. The whole point of requiring exactly 32 bytes
// is to keep the at-rest construction at AES-256. Also covers the
// "raw 32-char string" rejection — that path used to be accepted but
// got dropped because it's indistinguishable from a half-typed
// 64-char hex key (16 bytes after hex-decode = AES-128 strength).
func TestParseKey_RejectsShortKey(t *testing.T) {
	cases := []string{
		"",                                 // empty
		"deadbeef",                         // 8 hex chars
		"00112233445566778899aabbccddeeff", // 16 bytes hex = 32 chars — was previously accepted as raw, now rejected
		strings.Repeat("a", 16),            // 16-byte raw
		strings.Repeat("z", 32),            // 32 chars but not valid hex AND not valid base64
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseKey(in); err == nil {
				t.Errorf("ParseKey(%q) = nil, want error", in)
			}
		})
	}
}

// TestEncryptDecrypt_RoundTrip is the happy path: plaintext → encrypted
// blob → original plaintext. Repeats with several payload sizes to
// confirm the GCM seal handles realistic password lengths. We don't
// assert "stored doesn't contain plaintext" because short plaintexts
// like "a" trivially appear in random base64 output — that property
// is already guaranteed by AES-GCM, and a substring check would be a
// false-positive minefield.
func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := mustKey(t, hexKey)
	cases := []string{
		"a",
		"admin",
		"J3tstr3am0!",
		strings.Repeat("x", 1024), // pathologically long
	}
	for _, plain := range cases {
		stored, err := EncryptCredential(plain, key)
		if err != nil {
			t.Fatalf("EncryptCredential(%q): %v", plain, err)
		}
		if !strings.HasPrefix(stored, CredentialPrefix) {
			t.Errorf("stored value missing prefix: %q", stored)
		}
		got, err := DecryptCredential(stored, key)
		if err != nil {
			t.Fatalf("DecryptCredential: %v", err)
		}
		if got != plain {
			t.Errorf("DecryptCredential = %q, want %q", got, plain)
		}
	}
}

// TestEncrypt_FreshNonce ensures two encryptions of the same plaintext
// don't produce identical stored values. Without this property an
// attacker with read-only DB access could tell when two cameras share
// a password.
func TestEncrypt_FreshNonce(t *testing.T) {
	key := mustKey(t, hexKey)
	a, _ := EncryptCredential("samepass", key)
	b, _ := EncryptCredential("samepass", key)
	if a == b {
		t.Errorf("two encryptions of same plaintext produced identical stored values — nonce reuse?")
	}
}

// TestEncrypt_EmptyPreserved keeps the "no auth set" sentinel intact.
// Cameras with no password should round-trip as empty strings, not
// as a non-empty ciphertext that decrypts to empty.
func TestEncrypt_EmptyPreserved(t *testing.T) {
	key := mustKey(t, hexKey)
	got, err := EncryptCredential("", key)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("EncryptCredential(\"\") = %q, want empty", got)
	}
	plain, err := DecryptCredential("", key)
	if err != nil || plain != "" {
		t.Errorf("DecryptCredential(\"\") = (%q, %v), want (\"\", nil)", plain, err)
	}
}

// TestDecrypt_TamperingRejected is the GCM authentication tag in
// action: flipping any bit of the stored payload must produce a hard
// error, never a corrupted plaintext.
func TestDecrypt_TamperingRejected(t *testing.T) {
	key := mustKey(t, hexKey)
	stored, err := EncryptCredential("J3tstr3am0!", key)
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.TrimPrefix(stored, CredentialPrefix)
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	// Flip one bit in the ciphertext region (past the 12-byte nonce).
	raw[len(raw)-1] ^= 0x01
	tampered := CredentialPrefix + base64.StdEncoding.EncodeToString(raw)
	if _, err := DecryptCredential(tampered, key); err == nil {
		t.Error("tampered ciphertext decrypted without error — GCM auth tag bypass?")
	}
}

// TestDecrypt_WrongKeyRejected: encrypting with key A and decrypting
// with key B must fail. Catches a startup regression where the
// operator reused the wrong env var.
func TestDecrypt_WrongKeyRejected(t *testing.T) {
	keyA := mustKey(t, hexKey)
	keyB := mustKey(t, strings.Repeat("ab", 32))
	stored, err := EncryptCredential("admin", keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptCredential(stored, keyB); err == nil {
		t.Error("wrong-key decrypt succeeded — key not actually authenticated")
	}
}

// TestDecrypt_MalformedRejected covers the "stored value isn't even an
// encrypted blob" path. The migration sweeper relies on this to flag
// plaintext rows during transition; the API uses it to refuse to
// connect to a camera whose row got corrupted.
func TestDecrypt_MalformedRejected(t *testing.T) {
	key := mustKey(t, hexKey)
	cases := []string{
		"plain-text",                                 // no prefix
		CredentialPrefix + "!!!invalid base64!!!",    // bad b64
		CredentialPrefix + base64.StdEncoding.EncodeToString([]byte("short")), // too short for nonce
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := DecryptCredential(in, key); err == nil {
				t.Errorf("DecryptCredential(%q) = nil error, want failure", in)
			}
		})
	}
}

// TestIsEncrypted is small but the sweeper depends on it producing
// correct truth values across the prefix / non-prefix split.
func TestIsEncrypted(t *testing.T) {
	if !IsEncrypted(CredentialPrefix+"abc") {
		t.Error("IsEncrypted missed prefixed value")
	}
	if IsEncrypted("admin") {
		t.Error("IsEncrypted false-positive on plaintext")
	}
	if IsEncrypted("") {
		t.Error("IsEncrypted false-positive on empty")
	}
}

// TestEncrypt_EmptyKeyRejected: the boot-time check in cmd/server
// gates this, but the function must fail closed too in case a future
// caller routes around the check.
func TestEncrypt_EmptyKeyRejected(t *testing.T) {
	if _, err := EncryptCredential("x", nil); err == nil {
		t.Error("EncryptCredential(_, nil) = nil error")
	}
}
