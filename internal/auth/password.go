package auth

import (
	"errors"
	"strings"
	"unicode"
)

// MinPasswordLength is the floor enforced on every password create or
// change. 12 is the NIST SP 800-63B minimum for memorized secrets in a
// service context and the number most UL 827B reviewers expect to see
// when they inspect auth controls. Raise this later if policy demands.
const MinPasswordLength = 12

// ErrPasswordTooShort etc. are returned by ValidatePassword so the
// handler can surface a specific, actionable message to the caller
// instead of a generic "invalid password" that forces users to guess.
var (
	ErrPasswordTooShort  = errors.New("password must be at least 12 characters")
	ErrPasswordNoVariety = errors.New("password must contain letters and at least one digit or symbol")
	ErrPasswordCommon    = errors.New("password is too common; choose something else")
)

// commonPasswords is a short blocklist of the most frequently seen
// passwords in breach corpora. It is deliberately tiny — an offline
// dictionary check belongs at account-creation time where we can do a
// full SecLists pass, not at the hot login path. The goal here is to
// catch the "Password123!" class of low-effort picks and route the
// user back to a stronger choice. Case-insensitive comparison.
var commonPasswords = map[string]struct{}{
	"password":      {},
	"password1":     {},
	"password123":   {},
	"password1234":  {},
	"password12345": {},
	"password!":     {},
	"welcome1":      {},
	"welcome123":    {},
	"qwerty":        {},
	"qwerty123":     {},
	"qwertyuiop":    {},
	"letmein":       {},
	"letmein1":      {},
	"letmein123":    {},
	"admin":         {},
	"admin123":      {},
	"administrator": {},
	"changeme":      {},
	"changeme123":   {},
	"iloveyou":      {},
	"abc123":        {},
	"abcd1234":      {},
	"123456789012":  {},
	"qwerty1234":    {},
	"1qaz2wsx3edc":  {},
	"monkey123":     {},
	"football":      {},
	"superman":      {},
	"ironsight":     {},
	"ironsight1":    {},
	"ironsight123":  {},
}

// ValidatePassword enforces the server-side policy for new or changed
// passwords. It intentionally returns a typed error so callers can emit
// a helpful response without leaking which rule fired — the API layer
// can decide whether to be specific (better UX) or opaque (slightly
// better anti-enumeration posture). UL 827B reviewers have generally
// been fine with specific messages because failed attempts are
// rate-limited and audited anyway.
//
// The rule set is intentionally modest:
//   - length ≥ 12
//   - mixture: letters plus at least one digit or symbol
//   - not on the common-password blocklist (case-insensitive)
//
// We deliberately do NOT require character-class combinations beyond
// "letters plus one other class." NIST SP 800-63B rev 3 recommends
// against the "must have 1 upper, 1 lower, 1 digit, 1 symbol" pattern
// — it encourages predictable substitutions and weakens real-world
// entropy. Length + uniqueness do more.
func ValidatePassword(plain string) error {
	if len(plain) < MinPasswordLength {
		return ErrPasswordTooShort
	}

	hasLetter := false
	hasNonLetter := false
	for _, r := range plain {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r), unicode.IsPunct(r), unicode.IsSymbol(r), unicode.IsSpace(r):
			hasNonLetter = true
		}
	}
	if !hasLetter || !hasNonLetter {
		return ErrPasswordNoVariety
	}

	if _, common := commonPasswords[strings.ToLower(plain)]; common {
		return ErrPasswordCommon
	}

	return nil
}
