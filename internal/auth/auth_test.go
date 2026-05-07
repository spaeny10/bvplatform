package auth

import (
	"errors"
	"strings"
	"testing"
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
