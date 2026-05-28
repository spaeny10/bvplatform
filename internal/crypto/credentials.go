// Package crypto provides at-rest encryption for camera credentials
// (and other small secrets the API persists). The format is a
// version-prefixed AES-256-GCM ciphertext so we can rotate algorithms
// or keys later without ambiguity about how a stored value was
// produced.
//
// Wire format of an encrypted value:
//
//	crypt:v1:<base64(nonce || ciphertext || authTag)>
//
// - "crypt:v1:" is a fixed plaintext discriminator. Callers detect
//   already-encrypted values by prefix match before re-encrypting, and
//   the at-rest sweeper uses it to find rows that still hold plaintext.
// - The nonce is 12 random bytes (GCM's default). Each encryption uses
//   a fresh nonce — never reused under the same key.
// - The ciphertext is the input bytes XOR-encrypted by the GCM keystream.
// - The 16-byte tag is GCM's authenticator. Decryption fails closed on
//   any tampering of the stored blob.
//
// The key is a 32-byte AES-256 key. Operators provide it via env
// (CAMERA_CREDENTIALS_KEY) in one of three forms: 64 hex chars, 44
// standard-base64 chars (32 bytes encoded), or a raw 32-byte UTF-8
// string. The parser accepts all three so a hand-typed long passphrase
// works just as well as `openssl rand -hex 32`.
//
// SECURITY: this package does NOT manage key storage or rotation. The
// operator is responsible for keeping CAMERA_CREDENTIALS_KEY out of
// version control and rotating it on the schedule the platform's
// secrets policy demands. Losing the key makes every encrypted row
// unrecoverable — there is no escrow.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// CredentialPrefix is the discriminator every encrypted value begins
// with. Stored as plaintext so the sweeper can identify already-
// encrypted rows without a decrypt attempt, and so a future v2 format
// can coexist with v1 ciphertext during a key rotation.
const CredentialPrefix = "crypt:v1:"

// ErrEmptyKey is returned when EncryptCredential / DecryptCredential
// is called without a configured key. The server binary refuses to
// start when CAMERA_CREDENTIALS_KEY is unset, but callers that route
// around that boot check still need to fail closed.
var ErrEmptyKey = errors.New("crypto: empty credentials key")

// ErrMalformed is returned when a stored value doesn't begin with
// CredentialPrefix or its base64 payload won't decode / is too short
// to contain a GCM nonce + tag. Treat as "this row isn't encrypted
// yet" by the sweeper, "tampered/corrupted" by the API.
var ErrMalformed = errors.New("crypto: malformed credential payload")

// ParseKey accepts a 32-byte AES-256 key in one of two unambiguous
// encodings operators use in the wild: 64-char hex, or 44-char
// standard-base64. The raw-32-bytes-of-UTF-8 form is deliberately
// rejected — a 32-char string is just as likely to be a 16-byte hex
// key (AES-128, too weak) accidentally pasted, and accepting it would
// silently downgrade the at-rest construction.
//
// Anything that doesn't decode to exactly 32 bytes is rejected so a
// short or oversized key fails loud at boot rather than at first
// camera-add.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrEmptyKey
	}
	// Hex: 64 chars → 32 bytes.
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	// Standard base64, padded (44 chars) or unpadded (43 chars) — both → 32 bytes.
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, fmt.Errorf("crypto: key must be 32 bytes; accepts 64-char hex or 44-char base64. Got input of length %d", len(s))
}

// EncryptCredential converts a plaintext secret (camera password,
// webhook token, etc.) into the version-prefixed wire format. Each
// call uses a fresh nonce, so two calls with the same plaintext and
// key produce different stored values — the at-rest representation
// reveals neither equality of secrets nor the underlying length beyond
// the cipher's natural granularity.
//
// Empty plaintext is preserved as empty (not encrypted). This matches
// the existing column convention where a NULL/empty password means
// "no auth set" on the camera; encrypting "" would lose that signal.
func EncryptCredential(plaintext string, key []byte) (string, error) {
	if len(key) == 0 {
		return "", ErrEmptyKey
	}
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := make([]byte, 0, len(nonce)+len(sealed))
	payload = append(payload, nonce...)
	payload = append(payload, sealed...)
	return CredentialPrefix + base64.StdEncoding.EncodeToString(payload), nil
}

// DecryptCredential reverses EncryptCredential. Returns the plaintext
// secret, or an error if the stored value is empty (treated as empty
// plaintext — round-trips the "no auth" case), malformed, or tampered.
//
// IsEncrypted should be checked first by callers that need to handle
// mixed plaintext/encrypted rows during a migration; this function
// returns ErrMalformed for plaintext input because the prefix check
// fails. That's intentional — once a column is supposed to be
// encrypted, an un-prefixed row IS a bug.
func DecryptCredential(stored string, key []byte) (string, error) {
	if stored == "" {
		return "", nil
	}
	if len(key) == 0 {
		return "", ErrEmptyKey
	}
	if !strings.HasPrefix(stored, CredentialPrefix) {
		return "", ErrMalformed
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, CredentialPrefix))
	if err != nil {
		return "", ErrMalformed
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}
	if len(payload) < gcm.NonceSize() {
		return "", ErrMalformed
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether a stored value has the credential
// prefix. Used by the migration sweeper to skip rows that have
// already been re-encrypted on a previous run, and by the database
// read path to detect legacy plaintext rows during the transition
// window.
func IsEncrypted(stored string) bool {
	return strings.HasPrefix(stored, CredentialPrefix)
}
