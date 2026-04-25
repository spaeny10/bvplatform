package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP implementation per RFC 6238. We deliberately implement this
// in-house rather than adding a third-party dependency:
//
//   - the algorithm is a small, stable, well-understood specification
//   - it is the only OTP variant we support (no HOTP, no SMS, etc.)
//   - third-party OTP libraries vary widely on default parameters,
//     and we want our defaults pinned (SHA-1, 30s period, 6 digits)
//     so authenticator apps interop without surprises.
//
// Production-grade TOTP implementations also typically allow ±1 step
// of clock drift; we honor that in VerifyTOTP.

const (
	// TOTPSecretBytes is the size of the random secret in bytes. RFC 4226
	// recommends 160 bits (20 bytes) as a minimum; we go with that.
	TOTPSecretBytes = 20

	// TOTPPeriodSec is the time step. 30s is what every popular
	// authenticator app expects when scanning a QR code; changing it
	// requires the user to also pick a non-default profile in their app,
	// which is a poor UX trade.
	TOTPPeriodSec = 30

	// TOTPDigits is the number of decimal digits in the generated code.
	TOTPDigits = 6

	// TOTPDriftSteps is the number of ±1 windows we accept around the
	// current time step to absorb minor clock skew between server and
	// authenticator. Larger values weaken the second factor; 1 is the
	// industry default.
	TOTPDriftSteps = 1
)

// GenerateTOTPSecret returns a fresh base32-encoded TOTP secret. The
// base32 form is what authenticator apps consume directly when a user
// types it manually (rather than scanning a QR).
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, TOTPSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	// NoPadding because authenticator apps reject the "=" padding
	// characters that base32 normally uses.
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// ProvisioningURL formats the otpauth:// URL that authenticator apps
// scan from a QR code. Frontend renders the QR; we just hand back the
// canonical URL form.
//
// The label is "{issuer}:{accountName}" by convention so the entry in
// the user's authenticator app shows both pieces of context.
func ProvisioningURL(secret, issuer, accountName string) string {
	label := url.PathEscape(issuer + ":" + accountName)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprintf("%d", TOTPDigits))
	q.Set("period", fmt.Sprintf("%d", TOTPPeriodSec))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// VerifyTOTP returns true if the supplied code matches the secret
// within the current ±TOTPDriftSteps window. The check uses a
// constant-time compare so success/failure paths don't differ on
// timing characteristics.
//
// Returns an error only when the secret can't be decoded; an invalid
// code returns (false, nil).
func VerifyTOTP(secret, code string) (bool, error) {
	code = strings.TrimSpace(code)
	if len(code) != TOTPDigits {
		return false, nil
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false, nil
		}
	}

	rawSecret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return false, fmt.Errorf("decode totp secret: %w", err)
	}

	now := time.Now().Unix()
	for offset := -TOTPDriftSteps; offset <= TOTPDriftSteps; offset++ {
		step := uint64(now/TOTPPeriodSec) + uint64(offset) //nolint:gosec
		expected := totpAt(rawSecret, step)
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true, nil
		}
	}
	return false, nil
}

// totpAt returns the canonical zero-padded code for a given time step.
// Split out for testability and reuse by the drift loop in VerifyTOTP.
func totpAt(secret []byte, step uint64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, step)

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	hashv := mac.Sum(nil)

	// Dynamic truncation per RFC 4226 §5.3: the last 4 bits of the hash
	// pick a starting byte offset; we read 4 bytes from there, mask the
	// high bit (to keep the result positive on signed-int platforms),
	// and reduce mod 10^digits.
	offset := hashv[len(hashv)-1] & 0x0F
	binary32 := uint32(hashv[offset]&0x7F)<<24 |
		uint32(hashv[offset+1])<<16 |
		uint32(hashv[offset+2])<<8 |
		uint32(hashv[offset+3])

	mod := uint32(1)
	for i := 0; i < TOTPDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", TOTPDigits, binary32%mod)
}

// GenerateRecoveryCodes returns N fresh recovery codes formatted as
// "xxxx-xxxx-xxxx" (12 alphanumeric characters with two dashes). The
// dash format is deliberately easy to read aloud and type without
// confusing 'l' for '1' — we exclude visually-ambiguous characters
// from the alphabet.
func GenerateRecoveryCodes(n int) ([]string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no l, i, o, 0, 1
	out := make([]string, n)
	for i := 0; i < n; i++ {
		raw := make([]byte, 12)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("generate recovery codes: %w", err)
		}
		var b strings.Builder
		for j, byteVal := range raw {
			if j == 4 || j == 8 {
				b.WriteByte('-')
			}
			b.WriteByte(alphabet[int(byteVal)%len(alphabet)])
		}
		out[i] = b.String()
	}
	return out, nil
}

// ErrMFARequired is returned by login flows when the user has MFA
// enabled and the request did not include a code (or the code was
// invalid). The handler maps this to HTTP 401 with a body that lets
// the frontend distinguish "show password form" from "show TOTP
// form" — we do NOT reveal the distinction in WWW-Authenticate
// headers, so a token-probing attacker can't tell which accounts
// are MFA-enrolled by response shape alone.
var ErrMFARequired = errors.New("mfa code required")
