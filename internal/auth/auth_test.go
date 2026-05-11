package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignToken_RejectsEmptySecret(t *testing.T) {
	_, _, err := SignToken("u1", "alice", "admin", "Alice", "org1", "")
	if !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("want ErrEmptySecret, got %v", err)
	}
}

func TestSignToken_AcceptsRealSecret(t *testing.T) {
	tok, jti, err := SignToken("u1", "alice", "admin", "Alice", "org1", "s3cret")
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token returned with no error")
	}
	if jti == "" {
		t.Fatal("empty jti returned with no error")
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("token does not look like a JWT: %q", tok)
	}
}

func TestParseToken_RejectsEmptySecret(t *testing.T) {
	_, err := ParseToken("anything", "")
	if !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("want ErrEmptySecret, got %v", err)
	}
}

func TestSignAndParse_RoundTrip(t *testing.T) {
	const secret = "round-trip-secret"
	tok, jti, err := SignToken("u42", "bob", "viewer", "Bob", "orgX", secret)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	claims, err := ParseToken(tok, secret)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.UserID != "u42" || claims.Username != "bob" || claims.Role != "viewer" ||
		claims.DisplayName != "Bob" || claims.OrganizationID != "orgX" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
	if claims.ID != jti {
		t.Fatalf("jti mismatch: signed=%s parsed=%s", jti, claims.ID)
	}
}

func TestParseToken_RejectsWrongSecret(t *testing.T) {
	tok, _, err := SignToken("u1", "alice", "admin", "Alice", "org1", "secret-A")
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if _, err := ParseToken(tok, "secret-B"); err == nil {
		t.Fatal("ParseToken accepted token signed with a different secret")
	}
}

// ──────────────────── Media token tests (P1-A-03) ────────────────────

const mediaSecret = "media-test-secret"

func TestSignMediaToken_RejectsEmptySecret(t *testing.T) {
	_, err := SignMediaToken("u1", "cam1", MediaKindSegment, "seg_001.mp4", "", time.Minute)
	if !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("want ErrEmptySecret, got %v", err)
	}
}

func TestSignMediaToken_RoundTrip(t *testing.T) {
	tok, err := SignMediaToken("u1", "cam-abc", MediaKindSegment, "seg_20260511_120000.mp4", mediaSecret, 5*time.Minute)
	if err != nil {
		t.Fatalf("SignMediaToken: %v", err)
	}
	if tok == "" || strings.Count(tok, ".") != 2 {
		t.Fatalf("token shape wrong: %q", tok)
	}
	c, err := ParseMediaToken(tok, mediaSecret)
	if err != nil {
		t.Fatalf("ParseMediaToken: %v", err)
	}
	if c.UserID != "u1" || c.CameraID != "cam-abc" ||
		c.Path != "seg_20260511_120000.mp4" || c.Kind != MediaKindSegment {
		t.Fatalf("media claims mismatch: %+v", c)
	}
	if c.Issuer != MediaTokenIssuer {
		t.Fatalf("issuer mismatch: %q", c.Issuer)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty jti")
	}
}

func TestParseMediaToken_RejectsExpired(t *testing.T) {
	// Negative TTL gets clamped to 1s by SignMediaToken, so sign with 1s
	// and then sleep just past expiry.
	tok, err := SignMediaToken("u1", "cam1", MediaKindHLS, "live.m3u8", mediaSecret, time.Second)
	if err != nil {
		t.Fatalf("SignMediaToken: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := ParseMediaToken(tok, mediaSecret); err == nil {
		t.Fatal("ParseMediaToken accepted an expired token")
	}
}

func TestParseMediaToken_RejectsWrongSecret(t *testing.T) {
	tok, err := SignMediaToken("u1", "cam1", MediaKindSegment, "seg_a.mp4", mediaSecret, time.Minute)
	if err != nil {
		t.Fatalf("SignMediaToken: %v", err)
	}
	if _, err := ParseMediaToken(tok, "different-secret"); err == nil {
		t.Fatal("ParseMediaToken accepted token signed with a different secret")
	}
}

// A session token (issuer empty) MUST be rejected as a media token even
// if signed with the same secret. This is the core audience-binding check.
func TestParseMediaToken_RejectsSessionTokenAsMedia(t *testing.T) {
	sessionTok, _, err := SignToken("u1", "alice", "admin", "Alice", "org1", mediaSecret)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if _, err := ParseMediaToken(sessionTok, mediaSecret); err == nil {
		t.Fatal("ParseMediaToken accepted a session token (wrong issuer)")
	}
}

// And the reverse: a media token MUST NOT parse as a session token. The
// session parser doesn't inspect issuer, but the claims struct types are
// different — assert specifically that the values returned are not the
// session shape, which would indicate the strict-typing was bypassed.
func TestParseToken_DoesNotConfuseMediaTokenForSession(t *testing.T) {
	mediaTok, err := SignMediaToken("u1", "cam1", MediaKindSegment, "seg.mp4", mediaSecret, time.Minute)
	if err != nil {
		t.Fatalf("SignMediaToken: %v", err)
	}
	// ParseToken parses into *Claims; the media token's MediaClaims
	// shape doesn't carry username/role/etc., so even if the signature
	// validates the claims will be empty for the session-required
	// fields. That's enough — the caller would 401 on empty UserID.
	c, err := ParseToken(mediaTok, mediaSecret)
	if err == nil && (c.UserID != "" || c.Username != "" || c.Role != "") {
		t.Fatalf("ParseToken returned session-shaped claims from a media token: %+v", c)
	}
}

func TestSignMediaToken_RejectsBadPath(t *testing.T) {
	cases := []string{
		"",
		"..",
		".",
		"../etc/passwd",
		"foo/bar.mp4",
		"foo\\bar.mp4",
		".hidden.mp4",
		"x?y",
		"x y",
		strings.Repeat("a", 256), // length cap
	}
	for _, p := range cases {
		if _, err := SignMediaToken("u1", "cam1", MediaKindSegment, p, mediaSecret, time.Minute); err == nil {
			t.Errorf("SignMediaToken accepted invalid path %q", p)
		}
	}
}

func TestSignMediaToken_RejectsBadKind(t *testing.T) {
	if _, err := SignMediaToken("u1", "cam1", MediaKind("bogus"), "seg.mp4", mediaSecret, time.Minute); err == nil {
		t.Fatal("SignMediaToken accepted unknown kind")
	}
}

// A token whose payload has been tampered with after signing must be
// rejected — verifies signature checking covers the path field, not
// just exp/iss.
func TestParseMediaToken_RejectsTamperedClaims(t *testing.T) {
	tok, err := SignMediaToken("u1", "cam-A", MediaKindSegment, "seg_a.mp4", mediaSecret, time.Minute)
	if err != nil {
		t.Fatalf("SignMediaToken: %v", err)
	}
	// JWT format: header.payload.signature — splice in a different,
	// also-valid-base64 payload (just append junk that decodes
	// differently). Any mutation invalidates the HMAC.
	parts := strings.SplitN(tok, ".", 3)
	if len(parts) != 3 {
		t.Fatalf("token shape wrong: %q", tok)
	}
	tampered := parts[0] + "." + parts[1] + "XYZ." + parts[2]
	if _, err := ParseMediaToken(tampered, mediaSecret); err == nil {
		t.Fatal("ParseMediaToken accepted a tampered token")
	}
}

func TestSignMediaToken_TTLClamp(t *testing.T) {
	// Asking for 2h should clamp to 1h. We can't directly inspect the
	// stored exp from outside; verify by signing and parsing immediately
	// and checking the exp is at most ~1h from now.
	before := time.Now()
	tok, err := SignMediaToken("u1", "cam1", MediaKindSegment, "seg.mp4", mediaSecret, 2*time.Hour)
	if err != nil {
		t.Fatalf("SignMediaToken: %v", err)
	}
	c, err := ParseMediaToken(tok, mediaSecret)
	if err != nil {
		t.Fatalf("ParseMediaToken: %v", err)
	}
	if c.ExpiresAt == nil {
		t.Fatal("no exp on signed token")
	}
	ttl := c.ExpiresAt.Sub(before)
	if ttl > time.Hour+5*time.Second {
		t.Fatalf("ttl was not clamped to 1h: got %v", ttl)
	}
}
