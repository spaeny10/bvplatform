package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrEmptySecret is returned when SignToken/ParseToken is called with an
// empty signing secret. Production callers must plumb a non-empty secret
// from internal/config.Config.JWTSecret, which itself fails fast at boot
// if JWT_SECRET is unset (see internal/config/config.go:requireSecret).
// We refuse the empty-secret path explicitly so a future caller that
// forgets to plumb the secret fails loudly instead of silently signing
// with a known constant.
var ErrEmptySecret = errors.New("auth: empty signing secret")

// Claims is the JWT payload. jwt.RegisteredClaims provides the
// standard `jti` (JWT ID), `exp`, and `iat` fields — UL 827B's
// server-side revocation story relies on `jti` being unique per
// token so we can blocklist a single session without rotating the
// shared signing secret.
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

// SignToken generates a signed JWT for the given user attributes.
// Each call mints a fresh `jti` (JWT ID) so revocation can target a
// specific session without invalidating every other live token. The
// jti is also returned to the caller so the login handler can record
// it alongside the user-facing audit row if needed.
func SignToken(userID, username, role, displayName, organizationID, secret string) (token string, jti string, err error) {
	if secret == "" {
		return "", "", ErrEmptySecret
	}
	jti = uuid.NewString()
	claims := Claims{
		UserID:         userID,
		Username:       username,
		Role:           role,
		DisplayName:    displayName,
		OrganizationID: organizationID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	return signed, jti, err
}

// ParseToken validates a JWT string and returns its claims
func ParseToken(tokenStr, secret string) (*Claims, error) {
	if secret == "" {
		return nil, ErrEmptySecret
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

// ──────────────────── Media tokens (P1-A-03) ────────────────────
//
// Media tokens are a SEPARATE class of JWT from the session tokens above.
// They authorize read-access to a single file (one segment, one snapshot,
// or one HLS playlist) for a short window — default 5 min, capped at 1 h.
// The split is enforced by the `iss` (issuer) claim: session tokens use
// the default issuer (i.e. empty / not set by SignToken); media tokens
// use MediaTokenIssuer. ParseMediaToken rejects any token whose `iss`
// is not exactly MediaTokenIssuer, so a stolen session token can never
// be replayed as a media token (and vice versa).
//
// The signing secret is shared with the session-token path on purpose:
// the audience split via `iss` is what isolates them. Sharing the secret
// keeps the boot-time `JWT_SECRET` check the single source of truth for
// auth-grade key material; we don't need a separate MEDIA_JWT_SECRET
// that could drift or get rotated independently.
//
// Design rationale lives in docs/media-auth.md.

// MediaTokenIssuer is the `iss` value every media JWT carries. Any token
// presented to ParseMediaToken without exactly this issuer is rejected.
const MediaTokenIssuer = "ironsight-media-v1"

// MediaKind identifies which on-disk media tree a token grants access to.
// The serve handler in internal/api/media_v1.go maps each kind to a
// configured base directory (recordings / hls / snapshots) — the token
// alone cannot escape that mapping.
type MediaKind string

const (
	MediaKindSegment  MediaKind = "segment"   // long-form recording MP4 in StoragePath
	MediaKindHLS      MediaKind = "hls"       // live HLS playlist or .ts segment in HLSPath
	MediaKindSnapshot MediaKind = "snapshot"  // alarm-time JPEG in snapshots dir
	// MediaKindPPEFrame is the audience for PPE violation frame thumbnails
	// written by the PPE worker (P2-C-01). Distinct from "snapshot" so a
	// token minted for a recording cannot be replayed against the PPE path.
	MediaKindPPEFrame MediaKind = "ppe-frame" // PPE evidence JPEG in PPEFramesDir
	// MediaKindLiveHLS is the audience for gohlslib-backed LL-HLS live-view
	// tokens (P3-INFRA-06). Distinct from "hls" (which refers to legacy
	// FFmpeg-written playlists on disk) so a token for a recorded playlist
	// cannot be replayed to trigger an RTSP pull from mediamtx.
	// Live-HLS tokens use a 60-second TTL; the frontend refreshes every 30s.
	MediaKindLiveHLS MediaKind = "live-hls"
)

// IsValid reports whether k is one of the recognized media kinds. Anything
// else must be rejected at parse time so a forged-but-typo'd token can't
// silently fall through to a "default" code path.
func (k MediaKind) IsValid() bool {
	switch k {
	case MediaKindSegment, MediaKindHLS, MediaKindSnapshot, MediaKindPPEFrame, MediaKindLiveHLS:
		return true
	}
	return false
}

// MediaClaims is the payload of a media token. It carries enough state
// for the serve handler to reconstruct the on-disk path, re-verify the
// caller's tenant scope against the live DB at serve-time (see CLAUDE v2
// "tenant scope on every customer-data query"), and write a faithful
// audit row even after the token's lifetime has expired.
//
// `Path` is the leaf filename only — no directory components, no `..`,
// no slashes. SignMediaToken enforces this at mint time so a tampered
// token also fails at parse time (path-traversal defense in depth).
type MediaClaims struct {
	UserID   string    `json:"sub"`  // user_id of the caller who minted
	CameraID string    `json:"cam"`  // camera UUID the file belongs to
	Path     string    `json:"path"` // leaf filename, no directory traversal
	Kind     MediaKind `json:"kind"` // segment | hls | snapshot
	jwt.RegisteredClaims
}

// validMediaPath enforces a strict allow-list on the leaf filename.
// Reject anything containing path separators (`/`, `\`), parent
// references (`..`), or characters outside [a-zA-Z0-9._-]. Empty is
// rejected too. Mirror this check on the serve side as defense in
// depth — a forged token that survives signature verification would
// otherwise be the only line of defense against traversal.
func validMediaPath(p string) bool {
	if p == "" || len(p) > 255 {
		return false
	}
	if p == "." || p == ".." {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return false
		}
	}
	// Disallow a leading dot (would let `..hidden` style names slip),
	// and `..` anywhere as a token boundary is already excluded by the
	// character allow-list (no `/`), but be paranoid:
	if p[0] == '.' {
		return false
	}
	return true
}

// SignMediaToken mints a short-lived media-access JWT. The caller is
// responsible for having already verified that `userID` may access
// `cameraID` (e.g. via api.CanAccessCamera) — this function does not
// re-check authorization, it only signs. The serve handler also
// re-checks at every request to defend against role changes between
// mint and serve.
//
// ttl is clamped: minimum 1 second, maximum 1 hour. The caller passes
// in the operator-requested value (default 5 min via the mint handler);
// the clamp here is the floor.
func SignMediaToken(userID, cameraID string, kind MediaKind, path, secret string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	if userID == "" || cameraID == "" {
		return "", errors.New("auth: empty user or camera id")
	}
	if !kind.IsValid() {
		return "", errors.New("auth: invalid media kind")
	}
	if !validMediaPath(path) {
		return "", errors.New("auth: invalid media path")
	}
	if ttl < time.Second {
		ttl = time.Second
	}
	if ttl > time.Hour {
		ttl = time.Hour
	}
	claims := MediaClaims{
		UserID:   userID,
		CameraID: cameraID,
		Path:     path,
		Kind:     kind,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    MediaTokenIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString([]byte(secret))
}

// ParseMediaToken validates a media JWT and returns its claims. Returns
// an error if the signature is bad, the token is expired, the issuer is
// not exactly MediaTokenIssuer, or the path/kind fields fail the same
// allow-list applied at mint time. Callers MUST treat any non-nil error
// as a hard 401/404 — never log claim contents, never let the request
// fall through to a "default" code path.
func ParseMediaToken(tokenStr, secret string) (*MediaClaims, error) {
	if secret == "" {
		return nil, ErrEmptySecret
	}
	tok, err := jwt.ParseWithClaims(tokenStr, &MediaClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid or expired media token")
	}
	c, ok := tok.Claims.(*MediaClaims)
	if !ok {
		return nil, errors.New("invalid media claims")
	}
	// Issuer enforcement is what makes this a separate audience from the
	// session-JWT path. Without this check a stolen Authorization-header
	// token could be replayed in a media URL (and vice versa). The
	// session SignToken does NOT set Issuer, so its default empty value
	// trips this check naturally.
	if c.Issuer != MediaTokenIssuer {
		return nil, errors.New("invalid media token issuer")
	}
	if !c.Kind.IsValid() {
		return nil, errors.New("invalid media kind")
	}
	if !validMediaPath(c.Path) {
		return nil, errors.New("invalid media path")
	}
	if c.UserID == "" || c.CameraID == "" {
		return nil, errors.New("invalid media subject")
	}
	return c, nil
}

// ──────────────────── WebSocket tickets (P1-A-02) ────────────────────
//
// WS tickets are a THIRD class of JWT, separate from session tokens and
// media tokens. They authorize a single WebSocket UPGRADE for a short
// window (default 5 min). Audience-split by `iss` enforces that:
//
//	* A stolen session JWT can't be replayed as a WS ticket.
//	* A leaked WS ticket can't be replayed as a session token.
//	* A media token can't be replayed against /ws.
//
// Why a separate ticket class at all (vs. just passing the session JWT
// on the WS upgrade — which is what the connect-time check did before
// P1-A-02): the session JWT lives 24 h, but browsers can only attach it
// to a WS upgrade via the query string (no Authorization header on the
// WebSocket handshake). Logging a 24h-bearer credential in webserver
// access logs, NPM logs, browser history, etc. is a large blast radius.
// A 5-min single-purpose ticket reduces that risk to a tiny window.
//
// The signing secret is shared with the session-token path on purpose:
// the audience split via `iss` is what isolates them, mirroring the
// MediaTokenIssuer design.

// WSTicketIssuer is the `iss` value every WebSocket ticket carries.
// Any token presented to ParseWSTicket without exactly this issuer is
// rejected.
const WSTicketIssuer = "ironsight-ws-v1"

// DefaultWSTicketTTL is the lifetime of a newly-minted ticket. Short
// enough to make replay attacks impractical, long enough to survive a
// brief network blip between the mint request and the WS upgrade.
const DefaultWSTicketTTL = 5 * time.Minute

// WSClaims is the payload of a WS ticket. We carry the same identity
// fields that ParseToken would have surfaced from the session JWT —
// the WS upgrade handler doesn't need to re-fetch the user record to
// build the wsClient, just to apply RBAC.
type WSClaims struct {
	UserID         string `json:"sub"`
	Username       string `json:"username"`
	Role           string `json:"role"`
	DisplayName    string `json:"display_name"`
	OrganizationID string `json:"organization_id,omitempty"`
	jwt.RegisteredClaims
}

// SignWSTicket mints a short-lived WebSocket-upgrade ticket for the
// given session claims. The session JWT is the authority that already
// validated the caller; this function just re-signs the identity into
// a different audience for the upgrade path.
//
// ttl is clamped: minimum 1 second, maximum 1 hour. Callers should
// pass DefaultWSTicketTTL; the bounds are defense in depth.
func SignWSTicket(c *Claims, secret string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	if c == nil || c.UserID == "" {
		return "", errors.New("auth: empty session claims")
	}
	if ttl < time.Second {
		ttl = time.Second
	}
	if ttl > time.Hour {
		ttl = time.Hour
	}
	ticket := WSClaims{
		UserID:         c.UserID,
		Username:       c.Username,
		Role:           c.Role,
		DisplayName:    c.DisplayName,
		OrganizationID: c.OrganizationID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Issuer:    WSTicketIssuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, ticket).SignedString([]byte(secret))
}

// ParseWSTicket validates a WebSocket ticket and returns its claims.
// Returns an error if the signature is bad, the token is expired, the
// issuer is not WSTicketIssuer, or the subject is empty. Callers MUST
// treat any non-nil error as a hard 401 close on the upgrade.
//
// Returns the *Claims shape (not *WSClaims) so the WS upgrade handler
// can build the wsClient with the same identity model the rest of the
// API uses — auth.Claims is the project's canonical identity type.
func ParseWSTicket(tokenStr, secret string) (*Claims, error) {
	if secret == "" {
		return nil, ErrEmptySecret
	}
	tok, err := jwt.ParseWithClaims(tokenStr, &WSClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid or expired ws ticket")
	}
	c, ok := tok.Claims.(*WSClaims)
	if !ok {
		return nil, errors.New("invalid ws claims")
	}
	// Issuer enforcement is what makes this a separate audience from
	// the session-JWT and media-token paths. The session SignToken does
	// NOT set Issuer, and SignMediaToken sets MediaTokenIssuer — both
	// trip this check naturally.
	if c.Issuer != WSTicketIssuer {
		return nil, errors.New("invalid ws ticket issuer")
	}
	if c.UserID == "" {
		return nil, errors.New("invalid ws ticket subject")
	}
	return &Claims{
		UserID:         c.UserID,
		Username:       c.Username,
		Role:           c.Role,
		DisplayName:    c.DisplayName,
		OrganizationID: c.OrganizationID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID: c.ID,
		},
	}, nil
}
