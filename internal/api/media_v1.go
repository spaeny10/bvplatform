// P1-A-03: authenticated, tenant-scoped media serving.
//
// Before this file existed, /recordings/*, /hls/*, and /snapshots/* were
// served by bare http.FileServer handlers — anyone who could guess (or
// otherwise obtain) a camera UUID + filename could fetch the bytes
// directly without authentication, and even authenticated users could
// fetch any other org's media because the file-server didn't consult
// the DB at all.
//
// The new scheme:
//
//   1. The frontend hits POST /api/media/mint (behind the existing JWT
//      auth middleware) with {camera_id, kind, path, ttl_seconds?}.
//   2. The mint handler runs CanAccessCamera against the caller's
//      claims. On success it signs a short-lived JWT
//      (kind=segment|hls|snapshot, path=leaf-only, exp=5 min default)
//      and returns /media/v1/<token>.
//   3. The frontend uses that URL directly in <video src=...>, <img
//      src=...>, etc. Browser → /media/v1/<token> hits HandleMediaServe.
//   4. HandleMediaServe parses + validates the token, re-runs
//      CanAccessCamera (defence against role changes between mint &
//      serve), resolves the kind to a base directory, and streams the
//      file. For .m3u8 playlists it rewrites every segment-URI line
//      to its own freshly-minted /media/v1/<sub-token>.
//   5. Every successful serve gets enqueued into an in-memory ring
//      buffer that flushes 100-row batches into audit_log every 5 sec
//      (or sooner when the ring fills). Synchronous DB-write per serve
//      would bottleneck the hot path (4 cams × 30 segments/min × N
//      operators).
//
// See docs/media-auth.md for the full design rationale + 8 decisions.

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/database"
)

// ──────────────────── Constants ────────────────────

const (
	// DefaultMediaTTL is the standard token lifetime when the mint
	// caller didn't request anything else.
	DefaultMediaTTL = 5 * time.Minute

	// MaxMediaTTL is the operator-facing ceiling for ttl_seconds on the
	// mint endpoint. Evidence-export uses up to 1 h so a downloaded
	// archive can reference media that's still fetchable when the
	// recipient opens the bundle. Beyond 1 h, callers must re-mint.
	MaxMediaTTL = time.Hour

	// AuditRingSize is how many serve events we hold before forcing a
	// flush. Sized for ~30 serves/sec sustained burst with a 5-sec
	// flush interval — well above expected steady state.
	AuditRingSize = 1024

	// AuditFlushInterval bounds how long any audit row sits in memory
	// before reaching the DB. A power-loss bug here is acceptable —
	// the playback_audits / segment-write trails are the courtroom
	// record; this audit_log subset is the operator-facing "who
	// downloaded what" view, and short-window loss has no compliance
	// impact.
	AuditFlushInterval = 5 * time.Second

	// AuditFlushBatch is the row-count of one INSERT. INSERT ... VALUES
	// with multi-row tuples is dramatically faster than N single-row
	// inserts and stays well under Postgres's 65535-arg-per-statement
	// limit (we use 7 args per row × 100 rows = 700 args).
	AuditFlushBatch = 100
)

// ──────────────────── Audit ring buffer ────────────────────

// mediaAuditRow is one in-flight serve event. Captured before the
// response writer is touched so even an aborted connection still gets
// logged.
type mediaAuditRow struct {
	when       time.Time
	userID     uuid.UUID
	username   string
	cameraID   string
	path       string
	kind       string
	ip         string
}

// mediaAuditor is the goroutine-safe queue that batches serve events
// into the audit_log table. Created once at router-construction time;
// any handler can call enqueue(...) and is guaranteed not to block on
// the DB. Start() must be called exactly once; Stop() drains and exits.
type mediaAuditor struct {
	db    *database.DB
	rows  chan mediaAuditRow
	stop  chan struct{}
	wg    sync.WaitGroup
}

func newMediaAuditor(db *database.DB) *mediaAuditor {
	return &mediaAuditor{
		db:   db,
		rows: make(chan mediaAuditRow, AuditRingSize),
		stop: make(chan struct{}),
	}
}

// Start launches the background flush loop. Call exactly once.
func (a *mediaAuditor) Start() {
	a.wg.Add(1)
	go a.run()
}

// Stop signals the flush loop to drain the ring once more and exit.
// Blocks until the loop has returned.
func (a *mediaAuditor) Stop() {
	close(a.stop)
	a.wg.Wait()
}

// enqueue is non-blocking: if the ring is full we drop the event and
// log a warning. A dropped audit row is preferable to a wedged HTTP
// handler — the failure mode is "missing entry in audit log", not
// "service unavailable".
func (a *mediaAuditor) enqueue(row mediaAuditRow) {
	select {
	case a.rows <- row:
	default:
		log.Printf("[MEDIA-AUDIT] ring full, dropping row for cam=%s path=%s", row.cameraID, row.path)
	}
}

func (a *mediaAuditor) run() {
	defer a.wg.Done()
	tick := time.NewTicker(AuditFlushInterval)
	defer tick.Stop()
	pending := make([]mediaAuditRow, 0, AuditFlushBatch)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		a.flushBatch(pending)
		pending = pending[:0]
	}

	for {
		select {
		case <-a.stop:
			// Drain remaining rows before exit.
			for {
				select {
				case row := <-a.rows:
					pending = append(pending, row)
					if len(pending) >= AuditFlushBatch {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case row := <-a.rows:
			pending = append(pending, row)
			if len(pending) >= AuditFlushBatch {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

// flushBatch issues one multi-row INSERT against audit_log. Uses the
// same column shape as the existing single-row InsertAuditEntry helper:
// (user_id, username, action, target_type, target_id, details, ip_address).
// One row per serve. action="media_serve", target_type="camera",
// target_id=<cameraID>, details=JSON `{"kind":"...","path":"..."}` so
// the polymorphic-target index already in place keeps these rows
// queryable by camera.
func (a *mediaAuditor) flushBatch(rows []mediaAuditRow) {
	if a.db == nil || len(rows) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var sb strings.Builder
	sb.WriteString("INSERT INTO audit_log (user_id, username, action, target_type, target_id, details, ip_address, created_at) VALUES ")
	args := make([]interface{}, 0, len(rows)*8)
	argIdx := 1
	for i, r := range rows {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("($")
		writeIdx(&sb, argIdx)
		sb.WriteString(",$")
		writeIdx(&sb, argIdx+1)
		sb.WriteString(",'media_serve','camera',$")
		writeIdx(&sb, argIdx+2)
		sb.WriteString(",$")
		writeIdx(&sb, argIdx+3)
		sb.WriteString(",$")
		writeIdx(&sb, argIdx+4)
		sb.WriteString(",$")
		writeIdx(&sb, argIdx+5)
		sb.WriteString(")")
		details := mediaDetailsJSON(r.kind, r.path)
		var userArg interface{}
		if r.userID != (uuid.UUID{}) {
			userArg = r.userID
		}
		args = append(args, userArg, r.username, r.cameraID, details, r.ip, r.when)
		argIdx += 6
	}
	if _, err := a.db.Pool.Exec(ctx, sb.String(), args...); err != nil {
		log.Printf("[MEDIA-AUDIT] flush failed (%d rows): %v", len(rows), err)
	}
}

// writeIdx writes an int as decimal into sb without pulling in
// strconv on this hot path. Always positive small numbers.
func writeIdx(sb *strings.Builder, n int) {
	if n < 10 {
		sb.WriteByte(byte('0' + n))
		return
	}
	// up to 6 digits is plenty (max 7 args × 100 rows = 700 < 1e4).
	digits := [6]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	sb.Write(digits[i:])
}

// mediaDetailsJSON is a tiny, allocation-light JSON encoder for the
// two-field details payload. Done by hand rather than encoding/json to
// keep the hot path predictable; the inputs are already strictly
// validated (kind ∈ enum, path ∈ [a-zA-Z0-9._-]).
func mediaDetailsJSON(kind, path string) string {
	var sb strings.Builder
	sb.Grow(len(kind) + len(path) + 24)
	sb.WriteString(`{"kind":"`)
	sb.WriteString(kind)
	sb.WriteString(`","path":"`)
	sb.WriteString(path)
	sb.WriteString(`"}`)
	return sb.String()
}

// ──────────────────── Handlers ────────────────────

// MediaMintRequest is the body shape for POST /api/media/mint. CameraID
// is the only required field beyond the implicit caller (taken from
// the JWT-validated session). Path + Kind together identify the leaf
// file. TTLSeconds is optional; if omitted defaults to 5 min, capped at
// 3600 (1 h).
type MediaMintRequest struct {
	CameraID   string `json:"camera_id"`
	Kind       string `json:"kind"`        // segment | hls | snapshot
	Path       string `json:"path"`        // leaf filename only
	TTLSeconds int    `json:"ttl_seconds"` // optional; 0 = default
}

// MediaMintResponse carries the signed URL the frontend should use.
// We return the absolute path so the caller doesn't have to reconstruct
// /media/v1/ on the client side; only the token portion is opaque.
type MediaMintResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// HandleMediaMint issues a signed URL for one specific media file. The
// caller must be authenticated (mounted behind RequireAuth) and have
// access to the camera (CanAccessCamera). Returns 400 on malformed
// input, 404 on access-denied or non-existent camera (don't distinguish
// — decision 4 from the design doc: hide cross-tenant existence).
func HandleMediaMint(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req MediaMintRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		camUUID, err := uuid.Parse(req.CameraID)
		if err != nil {
			http.Error(w, "invalid camera_id", http.StatusBadRequest)
			return
		}
		kind := auth.MediaKind(req.Kind)
		if !kind.IsValid() {
			http.Error(w, "invalid kind", http.StatusBadRequest)
			return
		}

		// Tenant scope check (same helper everyone else uses). 404 on
		// denial — design decision 4: never confirm whether a camera
		// exists in another tenant.
		ok, cErr := CanAccessCamera(r.Context(), db, claims, camUUID)
		if cErr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		ttl := DefaultMediaTTL
		if req.TTLSeconds > 0 {
			ttl = time.Duration(req.TTLSeconds) * time.Second
			if ttl > MaxMediaTTL {
				ttl = MaxMediaTTL
			}
		}

		token, err := auth.SignMediaToken(claims.UserID, camUUID.String(), kind, req.Path, cfg.JWTSecret, ttl)
		if err != nil {
			// Path or kind invalid — surface as 400. Don't leak the
			// internal error string (could echo back tampered input).
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		resp := MediaMintResponse{
			URL:       "/media/v1/" + token,
			ExpiresAt: time.Now().Add(ttl).UTC(),
		}
		writeJSON(w, resp)
	}
}

// HandleMediaServe validates the token in the URL, re-checks tenant
// scope, and streams the file. Public (no auth middleware) — the token
// IS the authorisation.
//
// Failure modes:
//
//	bad token signature / expired / wrong issuer → 401 Unauthorized
//	tenant scope check fails (camera deleted, user demoted, etc.) → 404
//	file missing on disk → 404 (same response as cross-tenant: don't leak)
//	path traversal somehow survives → 400 (should be unreachable post-parse)
func HandleMediaServe(cfg *config.Config, db *database.DB, auditor *mediaAuditor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := chi.URLParam(r, "token")
		if tokenStr == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := auth.ParseMediaToken(tokenStr, cfg.JWTSecret)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Re-verify tenant scope on every serve (design decision 5).
		// Tokens have a 5-min TTL but role changes (admin demotes a
		// customer mid-session, customer is removed from a site) must
		// take effect immediately — we can't trust the mint-time
		// authorisation alone. Cost: one indexed point lookup.
		camUUID, err := uuid.Parse(claims.CameraID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		userUUID, _ := uuid.Parse(claims.UserID)
		fakeClaims, err := loadClaimsForUser(r.Context(), db, userUUID)
		if err != nil || fakeClaims == nil {
			// User no longer exists / DB error → 404 (don't reveal).
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		ok, cErr := CanAccessCamera(r.Context(), db, fakeClaims, camUUID)
		if cErr != nil || !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Resolve kind → on-disk path. The path inside the token is
		// already proven leaf-only by ParseMediaToken's validMediaPath
		// check; we still pass it through filepath.Base as
		// defence-in-depth so a future bug in the validator can't
		// escape the camera directory.
		absPath, ok := resolveMediaPath(cfg, claims)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.IsDir() {
			// Same 404 for "file missing" and "directory traversal" —
			// no information leak about the on-disk layout.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Audit: enqueue before we touch the body. A successful
		// auditor.enqueue does not guarantee the row hits the DB
		// before the response completes, but it does guarantee we
		// won't drop it just because the response failed.
		auditor.enqueue(mediaAuditRow{
			when:     time.Now().UTC(),
			userID:   userUUID,
			username: fakeClaims.Username,
			cameraID: camUUID.String(),
			path:     claims.Path,
			kind:     string(claims.Kind),
			ip:       clientIP(r),
		})

		// HLS playlists require rewriting — every segment URI in the
		// playlist needs its own signed token so the browser's
		// follow-up GETs are authorised. Anything else streams
		// straight off disk.
		if claims.Kind == auth.MediaKindHLS && strings.HasSuffix(strings.ToLower(claims.Path), ".m3u8") {
			serveRewrittenM3U8(w, r, cfg, claims, absPath)
			return
		}

		// Content-type hint for the common cases. http.ServeContent
		// would sniff the first 512 bytes otherwise, which is fine for
		// MP4 but wrong for .ts (which sniffs as application/octet-
		// stream and trips some browsers). Set explicitly here.
		switch strings.ToLower(filepath.Ext(claims.Path)) {
		case ".mp4":
			w.Header().Set("Content-Type", "video/mp4")
		case ".ts":
			w.Header().Set("Content-Type", "video/MP2T")
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		}
		// Block browser caching on the token-bound URL. Each token is
		// 5-min-TTL and the URL itself is single-use-shaped; a CDN that
		// caches the response could leak data after the token expires.
		w.Header().Set("Cache-Control", "no-store, private")

		// ServeContent honours Range requests, which the HTML5 video
		// element relies on for scrubbing. The displayed filename is the
		// leaf-only Path so downloads land with a useful name.
		http.ServeContent(w, r, claims.Path, info.ModTime(), mustOpen(absPath))
	}
}

// resolveMediaPath maps a parsed MediaClaims into an absolute on-disk
// path. Returns ok=false if the kind is unhandled or the configured
// base path is empty (storage not configured).
func resolveMediaPath(cfg *config.Config, c *auth.MediaClaims) (string, bool) {
	// filepath.Base is the second-line defence. ParseMediaToken's
	// validMediaPath already rejected `/`, `\`, and `..` — but we run
	// Base anyway so a regression in the validator can't reach disk.
	leaf := filepath.Base(c.Path)
	if leaf != c.Path {
		return "", false
	}
	switch c.Kind {
	case auth.MediaKindSegment:
		if cfg.StoragePath == "" {
			return "", false
		}
		return filepath.Join(cfg.StoragePath, c.CameraID, leaf), true
	case auth.MediaKindHLS:
		if cfg.HLSPath == "" {
			return "", false
		}
		return filepath.Join(cfg.HLSPath, c.CameraID, leaf), true
	case auth.MediaKindSnapshot:
		if cfg.StoragePath == "" {
			return "", false
		}
		// Snapshots dir is a sibling of the storage dir, per the layout
		// established in cmd/server/main.go + router.go.
		base := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots")
		return filepath.Join(base, c.CameraID, leaf), true
	}
	return "", false
}

// mustOpen returns a *os.File or panics. The serve handler only calls
// this after a successful os.Stat, so a failure here means a race with
// retention or a permissions issue — let it surface as 500.
func mustOpen(p string) *os.File {
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	return f
}

// ──────────────────── M3U8 rewriting ────────────────────

// serveRewrittenM3U8 reads the on-disk playlist and re-emits it with
// every media URI replaced by a freshly-minted /media/v1/<token>. All
// comment lines (#EXTM3U, #EXTINF, #EXT-X-*) and blank lines pass
// through byte-identical — players are picky about exact formatting,
// even trailing spaces and CRLF vs LF.
//
// Each minted child token reuses the same UserID / CameraID / Kind as
// the parent playlist; only the Path differs. Their TTL matches the
// remaining lifetime of the parent token (capped at 5 min) so a
// long-lived evidence-export playlist token doesn't unlock arbitrary-
// future segment fetches.
func serveRewrittenM3U8(w http.ResponseWriter, r *http.Request, cfg *config.Config, parent *auth.MediaClaims, absPath string) {
	f, err := os.Open(absPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	// Compute remaining TTL for child tokens. Clamp to 5 min so even
	// a 1-h evidence-export parent doesn't issue 1-h segment tokens.
	childTTL := DefaultMediaTTL
	if parent.ExpiresAt != nil {
		remaining := time.Until(parent.ExpiresAt.Time)
		if remaining < childTTL {
			childTTL = remaining
		}
		if childTTL < time.Second {
			childTTL = time.Second
		}
	}

	var out bytes.Buffer
	out.Grow(8192)

	scanner := bufio.NewScanner(f)
	// Default Scanner buffer is 64 KiB which is plenty for a live HLS
	// playlist, but a long-running VOD might be larger — raise to 1 MiB
	// to be safe.
	buf := make([]byte, 0, 65536)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		rewritten, didRewrite := rewriteM3U8Line(line, cfg, parent, childTTL)
		_ = didRewrite
		out.WriteString(rewritten)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		http.Error(w, "playlist read failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store, private")
	w.Write(out.Bytes())
}

// rewriteM3U8Line returns (rewritten-line, did-rewrite). Lines starting
// with `#` (HLS tags) or empty lines pass through. Anything else is
// treated as a media URI — we mint a child token with kind=hls and
// path=that-line and replace the line with /media/v1/<token>.
//
// Some HLS tags carry inline URI attributes — most notably
// #EXT-X-MAP:URI="seg.mp4" — but in our setup (FFmpeg-generated live
// playlists) those URIs are filenames within the same camera dir, and
// the HLS.js player follows the *segment* URI from the EXTINF line
// directly. We *also* rewrite the URI inside EXT-X-MAP for HLS.js
// fMP4 init-segment requests — see rewriteAttributeURI for the parse.
func rewriteM3U8Line(line string, cfg *config.Config, parent *auth.MediaClaims, ttl time.Duration) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line, false
	}
	if strings.HasPrefix(trimmed, "#") {
		// Inline URI rewrite for tags that embed a URI="..." attr.
		// EXT-X-MAP is the common case we hit in our FFmpeg output;
		// EXT-X-KEY and EXT-X-MEDIA can also carry URI=, so the
		// helper looks for the literal `URI="...".
		if strings.Contains(trimmed, "URI=\"") {
			return rewriteAttributeURI(line, cfg, parent, ttl), true
		}
		return line, false
	}
	// Bare URI line. Mint a child token using this line as the path
	// (leaf only — abort the rewrite if it isn't a leaf).
	tok, err := auth.SignMediaToken(parent.UserID, parent.CameraID, auth.MediaKindHLS, trimmed, cfg.JWTSecret, ttl)
	if err != nil {
		// Path didn't pass validMediaPath — leave the original line
		// untouched. A real player will fail the GET; an attacker has
		// no win since the file-server it would have hit is also gone.
		return line, false
	}
	return "/media/v1/" + tok, true
}

// rewriteAttributeURI rewrites the URI="..." attribute embedded in an
// HLS tag line. Returns the line unchanged if the attribute can't be
// parsed (defence against malformed playlists — better to pass through
// than to corrupt).
func rewriteAttributeURI(line string, cfg *config.Config, parent *auth.MediaClaims, ttl time.Duration) string {
	const marker = `URI="`
	idx := strings.Index(line, marker)
	if idx < 0 {
		return line
	}
	start := idx + len(marker)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return line
	}
	uri := line[start : start+end]
	tok, err := auth.SignMediaToken(parent.UserID, parent.CameraID, auth.MediaKindHLS, uri, cfg.JWTSecret, ttl)
	if err != nil {
		return line
	}
	return line[:start] + "/media/v1/" + tok + line[start+end:]
}

// ──────────────────── Tenant re-verify helper ────────────────────

// loadClaimsForUser rebuilds a minimal *auth.Claims from the user_id
// embedded in a media token. We don't have the original session JWT in
// hand on the serve path — only the media token's `sub` (user_id) — so
// we hit the DB once to recover the user's current role and assigned
// sites. This is the lookup that powers the re-verify-on-every-serve
// rule (design decision 5).
//
// Returns nil, nil if the user no longer exists. Callers MUST treat
// nil as "deny with 404".
func loadClaimsForUser(ctx context.Context, db *database.DB, userID uuid.UUID) (*auth.Claims, error) {
	if userID == (uuid.UUID{}) {
		return nil, nil
	}
	u, err := db.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	return &auth.Claims{
		UserID:         u.ID.String(),
		Username:       u.Username,
		Role:           u.Role,
		DisplayName:    u.DisplayName,
		OrganizationID: u.OrganizationID,
	}, nil
}

// ──────────────────── Errors ────────────────────

// ErrMediaServeDenied is returned by helpers when access should be
// denied without leaking which kind of denial fired. Currently unused
// outside tests; kept here so future helpers stay aligned.
var ErrMediaServeDenied = errors.New("media: access denied")

// ──────────────────── Helpers for other handlers ────────────────────

// MintSegmentPlaybackURL returns a signed /media/v1/<token>#t=<seek> URL
// for the given camera + on-disk segment path + event time. The handler
// callers (search, events listing, evidence-search) use this to attach
// playable URLs to API responses. Returns "" on any failure (bad path,
// empty secret) so a caller can just check for empty and skip writing
// the URL field — no row needs to fail just because one segment file
// has an oddly-named filename.
func MintSegmentPlaybackURL(cfg *config.Config, userID, cameraID, segFilePath string, segStart, eventTime time.Time) string {
	if segFilePath == "" {
		return ""
	}
	leaf := filepath.Base(segFilePath)
	// FilePath may contain Windows separators in the DB (dev/seed data).
	// Strip any leftover backslash segments.
	if i := strings.LastIndexAny(leaf, `\/`); i >= 0 {
		leaf = leaf[i+1:]
	}
	tok, err := auth.SignMediaToken(userID, cameraID, auth.MediaKindSegment, leaf, cfg.JWTSecret, DefaultMediaTTL)
	if err != nil {
		return ""
	}
	url := "/media/v1/" + tok
	offset := eventTime.Sub(segStart).Seconds()
	if offset > 0 && offset < 7200 {
		// Match the original buildPlaybackURL format: append a fragment
		// that the HTML5 video element honours as an initial-seek hint.
		var sb strings.Builder
		sb.Grow(len(url) + 16)
		sb.WriteString(url)
		sb.WriteString("#t=")
		// We avoid fmt.Sprintf on a hot path; manual one-decimal format.
		whole := int(offset)
		tenths := int((offset - float64(whole)) * 10)
		writeIdx(&sb, whole)
		sb.WriteByte('.')
		sb.WriteByte(byte('0' + tenths))
		return sb.String()
	}
	return url
}
