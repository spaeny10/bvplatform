package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const defaultSecret = "onvif-tool-change-me-in-production"

// Claims is the JWT payload
type Claims struct {
	UserID         string `json:"uid"`
	Username       string `json:"username"`
	Role           string `json:"role"`
	DisplayName    string `json:"display_name"`
	OrganizationID string `json:"organization_id,omitempty"`
	jwt.RegisteredClaims
}

// HashPassword hashes a plaintext password with bcrypt
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword verifies a plaintext password against a bcrypt hash
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// SignToken generates a signed JWT for the given user attributes
func SignToken(userID, username, role, displayName, organizationID, secret string) (string, error) {
	if secret == "" {
		secret = defaultSecret
	}
	claims := Claims{
		UserID:         userID,
		Username:       username,
		Role:           role,
		DisplayName:    displayName,
		OrganizationID: organizationID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// ParseToken validates a JWT string and returns its claims
func ParseToken(tokenStr, secret string) (*Claims, error) {
	if secret == "" {
		secret = defaultSecret
	}
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid or expired token")
	}
	c, ok := tok.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return c, nil
}
