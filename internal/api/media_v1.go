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
//      CanAccessCamera (defense against role changes between mint &
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
	"log/slog"
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
	"ironsight/internal/logging"
	"ironsight/internal/streaming"
)

// ──────────────────── Constants ────────────────────

const (
	// DefaultMediaTTL is the standard token lifetime when the mint
	// caller didn't request anything else.
	DefaultMediaTTL = 5 * time.Minute

	// LiveHLSMediaTTL is the token lifetime for kind=live-hls.
	// Originally 60s with 30s refresh on the frontend, but live HLS
	// playlists carry the same segment across multiple manifest fetches
	// and hls.js validates that segment URLs are identical between
	// playlist refreshes (it diffs by URL at the same media-sequence
	// number and fires levelParsingError "media sequence mismatch N" on
	// any change). Bumping to 5 min so the cached child tokens (see
	// liveTokenCache below) outlive a segment's lifetime in the muxer's
	// sliding window (~7 segs × 6s = ~42s). Combined with the cache, the
	// same segment now produces the same URL every time it's listed.
	LiveHLSMediaTTL = 5 * time.Minute

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
	when     time.Time
	userID   uuid.UUID
	username string
	cameraID string
	path     string
	kind     string
	ip       string
}

// mediaAuditor is the goroutine-safe queue that batches serve events
// into the audit_log table. Created once at router-construction time;
// any handler can call enqueue(...) and is guaranteed not to block on
// the DB. Start() must be called exactly once; Stop() drains and exits.
type mediaAuditor struct {
	db   *database.DB
	rows chan mediaAuditRow
	stop chan struct{}
	wg   sync.WaitGroup
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
		// P3-INFRA-06: live-hls tokens use a fixed 60s TTL regardless of
		// the caller's ttl_seconds — the frontend refreshes every 30s and
		// there's no on-disk file to hold open beyond that window.
		if kind == auth.MediaKindLiveHLS {
			ttl = LiveHLSMediaTTL
		} else if req.TTLSeconds > 0 {
			ttl = time.Duration(req.TTLSeconds) * time.Second
			if ttl > MaxMediaTTL {
				ttl = MaxMediaTTL
			}
		}

		// live-hls tokens have no on-disk path. Use the synthetic "live"
		// leaf so validMediaPath passes. The serve handler ignores this
		// field for live-hls — it uses claims.CameraID to look up the
		// gohlslib muxer instead.
		path := req.Path
		if kind == auth.MediaKindLiveHLS {
			path = "live"
		}

		token, err := auth.SignMediaToken(claims.UserID, camUUID.String(), kind, path, cfg.JWTSecret, ttl)
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
// IS the authorization.
//
// Failure modes:
//
//	bad token signature / expired / wrong issuer → 401 Unauthorized
//	tenant scope check fails (camera deleted, user demoted, etc.) → 404
//	file missing on disk → 404 (same response as cross-tenant: don't leak)
//	path traversal somehow survives → 400 (should be unreachable post-parse)
//
// P3-INFRA-06: kind=live-hls short-circuits before any disk I/O and
// proxies the request to the per-camera gohlslib LL-HLS muxer instead.
func HandleMediaServe(cfg *config.Config, db *database.DB, auditor *mediaAuditor, transcoder *transcodeRegistry, liveHLS *streaming.LiveHLSManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := chi.URLParam(r, "token")
		if tokenStr == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Strip optional segment extension so HLS demuxers that validate
		// URL by filename suffix (ffmpeg, hls.js) accept the rewritten
		// segment URLs. JWT tokens never legally contain these suffixes
		// because the base64url alphabet doesn't produce trailing literal
		// ".mp4"/".m4s"/".m3u8". The mint-side rewriter appends the
		// extension from the original resource name (see rewriteLiveHLSPlaylist).
		for _, ext := range []string{".mp4", ".m4s", ".m3u8", ".m4v", ".m4a", ".ts"} {
			if strings.HasSuffix(tokenStr, ext) {
				tokenStr = strings.TrimSuffix(tokenStr, ext)
				break
			}
		}
		claims, err := auth.ParseMediaToken(tokenStr, cfg.JWTSecret)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Re-verify tenant scope on every serve (design decision 5).
		// Tokens have a short TTL but role changes (admin demotes a
		// customer mid-session, camera deleted) must take effect
		// immediately. Cost: one indexed point lookup.
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

		// P3-INFRA-06: live-hls — route to the gohlslib LL-HLS muxer.
		// No on-disk path to resolve; the muxer is looked up by cameraID.
		// The request path after the token suffix tells us whether the
		// client wants the master playlist or a segment/part:
		//   /media/v1/<token>           → master playlist
		//   /media/v1/<token>/seg0.mp4  → fMP4 segment
		if claims.Kind == auth.MediaKindLiveHLS {
			if liveHLS == nil {
				http.Error(w, "live view not available", http.StatusServiceUnavailable)
				return
			}
			// Audit before touching the body.
			auditor.enqueue(mediaAuditRow{
				when:     time.Now().UTC(),
				userID:   userUUID,
				username: fakeClaims.Username,
				cameraID: camUUID.String(),
				path:     claims.Path,
				kind:     string(claims.Kind),
				ip:       clientIP(r),
			})

			// Token path semantics:
			//   claims.Path == "live" -> multivariant playlist fetch.
			//     Capture gohlslib output and rewrite relative URIs to signed
			//     /media/v1/<child-token> URLs so the browser can fetch each
			//     resource through the same auth layer.
			//   claims.Path != "live" -> media playlist or segment/part.
			//     The path in the token IS the gohlslib resource name
			//     (e.g. "video1_stream.m3u8", "seg0.mp4", "init.mp4").
			//     Look up the already-running muxer and proxy.
			if claims.Path != "live" {
				runningMuxer := liveHLS.GetRunning(camUUID)
				if runningMuxer == nil {
					http.Error(w, "stream not available", http.StatusServiceUnavailable)
					return
				}
				// Media playlists (.m3u8) contain relative segment URIs that
				// also need to be rewritten to /media/v1/<token> form.
				// Binary segments (.mp4) are proxied directly.
				if strings.HasSuffix(claims.Path, ".m3u8") {
					rec := &liveHLSResponseRecorder{header: make(http.Header)}
					runningMuxer.ServeSegment(rec, r, claims.Path)
					if rec.status != 0 && rec.status != http.StatusOK {
						w.WriteHeader(rec.status)
						_, _ = w.Write(rec.body.Bytes())
						return
					}
					rewritten := rewriteLiveHLSPlaylist(rec.body.Bytes(), cfg, claims, LiveHLSMediaTTL)
					for k, vv := range rec.header {
						for _, v := range vv {
							w.Header().Add(k, v)
						}
					}
					w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
					_, _ = w.Write(rewritten)
				} else {
					runningMuxer.ServeSegment(w, r, claims.Path)
				}
				return
			}

			// Initial multivariant playlist: GetOrStart, capture, rewrite, serve.
			muxer, mErr := liveHLS.GetOrStart(camUUID)
			if mErr != nil {
				log.Printf("[MEDIA-SERVE] live-hls GetOrStart camera %s: %v", camUUID, mErr)
				http.Error(w, "stream not available", http.StatusServiceUnavailable)
				return
			}
			muxer.RecordViewer()
			rec := &liveHLSResponseRecorder{header: make(http.Header)}
			muxer.ServePlaylist(rec, r)
			if rec.status != 0 && rec.status != http.StatusOK {
				w.WriteHeader(rec.status)
				_, _ = w.Write(rec.body.Bytes())
				return
			}
			rewritten := rewriteLiveHLSPlaylist(rec.body.Bytes(), cfg, claims, LiveHLSMediaTTL)
			for k, vv := range rec.header {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write(rewritten)
			return
		}

		// Resolve kind → on-disk path. The path inside the token is
		// already proven leaf-only by ParseMediaToken's validMediaPath
		// check; we still pass it through filepath.Base as
		// defense-in-depth so a future bug in the validator can't
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
		// follow-up GETs are authorized. Anything else streams
		// straight off disk.
		if claims.Kind == auth.MediaKindHLS && strings.HasSuffix(strings.ToLower(claims.Path), ".m3u8") {
			serveRewrittenM3U8(w, r, cfg, claims, absPath)
			return
		}

		// LOCAL-11: HEVC recorded-playback bridge. If the source is a
		// non-browser-playable codec (HEVC), produce or reuse a cached
		// H.264 copy and serve that instead. Pass-through for H.264.
		// See ironsight/feature-requests/hevc-recorded-playback.md.
		if transcoder != nil {
			servePath, terr := transcoder.maybeTranscodeForBrowser(cfg, claims, absPath)
			if terr != nil {
				logging.FromContext(r.Context()).LogAttrs(r.Context(), slog.LevelError, "media_transcode_failed",
					slog.String("camera_id", claims.CameraID),
					slog.String("path", claims.Path),
					slog.String("error", terr.Error()),
				)
				http.Error(w, "media unavailable", http.StatusInternalServerError)
				return
			}
			if servePath != absPath {
				// Refresh stat / info from the cache file so http.ServeContent
				// reports the correct size + ModTime for Range requests.
				cInfo, cErr := os.Stat(servePath)
				if cErr == nil && !cInfo.IsDir() {
					absPath = servePath
					info = cInfo
				}
			}
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

		// ServeContent honors Range requests, which the HTML5 video
		// element relies on for scrubbing. The displayed filename is the
		// leaf-only Path so downloads land with a useful name.
		http.ServeContent(w, r, claims.Path, info.ModTime(), mustOpen(absPath))
	}
}

// resolveMediaPath maps a parsed MediaClaims into an absolute on-disk
// path. Returns ok=false if the kind is unhandled or the configured
// base path is empty (storage not configured).
func resolveMediaPath(cfg *config.Config, c *auth.MediaClaims) (string, bool) {
	// filepath.Base is the second-line defense. ParseMediaToken's
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
// parsed (defense against malformed playlists — better to pass through
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
		// that the HTML5 video element honors as an initial-seek hint.
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

// liveHLSResponseRecorder captures a gohlslib HTTP response into memory
// so that the handler can rewrite the playlist body before sending it to
// the browser.  It implements http.ResponseWriter.
type liveHLSResponseRecorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *liveHLSResponseRecorder) Header() http.Header         { return r.header }
func (r *liveHLSResponseRecorder) WriteHeader(code int)        { r.status = code }
func (r *liveHLSResponseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }

// ──────────────────── Live-HLS child token cache ────────────────────
//
// hls.js diffs segment URLs at the same media-sequence number across
// playlist refreshes and fires "media sequence mismatch N — levelParsingError"
// on any change. Without caching, every rewriteLiveHLSPlaylist call
// mints a fresh JWT (with new iat/jti/exp) for the same segment, so the
// URLs differ between refreshes and hls.js kills the stream.
//
// Cache key includes userID so two operators viewing the same camera
// never share tokens — preserves per-user audit and tenant scope. TTL is
// LiveHLSMediaTTL (5 min); we reuse a cached token until it has <10s
// left, then re-sign. The reuse window comfortably outlives the muxer's
// 7-segment sliding window (~42s), so a segment's URL stays stable for
// its entire lifetime in the manifest.
type liveTokenKey struct {
	userID   uuid.UUID
	cameraID uuid.UUID
	path     string
}

type liveTokenEntry struct {
	token  string
	expiry time.Time
}

var liveTokenCache = struct {
	mu      sync.Mutex
	entries map[liveTokenKey]liveTokenEntry
}{entries: make(map[liveTokenKey]liveTokenEntry)}

// signLiveChildToken returns a child token for (userID, cameraID, path),
// reusing a cached one if it has >10s remaining. Prunes expired entries
// opportunistically when the map grows beyond 200 entries.
func signLiveChildToken(userID, cameraID uuid.UUID, path, secret string, ttl time.Duration) (string, error) {
	key := liveTokenKey{userID: userID, cameraID: cameraID, path: path}
	liveTokenCache.mu.Lock()
	defer liveTokenCache.mu.Unlock()
	now := time.Now()
	if entry, ok := liveTokenCache.entries[key]; ok && entry.expiry.Sub(now) > 10*time.Second {
		return entry.token, nil
	}
	if len(liveTokenCache.entries) > 200 {
		for k, v := range liveTokenCache.entries {
			if v.expiry.Before(now) {
				delete(liveTokenCache.entries, k)
			}
		}
	}
	tok, err := auth.SignMediaToken(userID.String(), cameraID.String(), auth.MediaKindLiveHLS, path, secret, ttl)
	if err != nil {
		return "", err
	}
	liveTokenCache.entries[key] = liveTokenEntry{token: tok, expiry: now.Add(ttl)}
	return tok, nil
}

// rewriteLiveHLSPlaylist walks the gohlslib-generated M3U8 line by line
// and replaces every bare URI (non-comment, non-blank) with a freshly
// signed /media/v1/<child-token> URL.  Comment lines that embed a URI=
// attribute (e.g. EXT-X-MAP) are also rewritten.
//
// Child tokens carry kind=live-hls and path=<gohlslib-resource-name>.
// HandleMediaServe routes those tokens to muxer.ServeSegment.
// Tokens are cached per (userID, cameraID, path) so URLs are stable
// across playlist refreshes — see liveTokenCache above.
func rewriteLiveHLSPlaylist(body []byte, cfg *config.Config, parent *auth.MediaClaims, ttl time.Duration) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			// Pass comment lines through; rewrite any embedded URI= attribute.
			if strings.Contains(trimmed, `URI="`) {
				out.WriteString(rewriteLiveHLSAttributeURI(line, cfg, parent, ttl))
			} else {
				out.WriteString(line)
			}
		} else {
			// Bare URI line — get-or-mint a child token with path=<resource-name>.
			// Cached per (user, camera, path) so URLs are deterministic across
			// playlist refreshes (hls.js compares segment URLs at the same
			// media-sequence number and dies on any mismatch).
			userID, errU := uuid.Parse(parent.UserID)
			cameraID, errC := uuid.Parse(parent.CameraID)
			var tok string
			var err error
			if errU == nil && errC == nil {
				tok, err = signLiveChildToken(userID, cameraID, trimmed, cfg.JWTSecret, ttl)
			} else {
				// Defensive fallback — should never hit since parent token already validated.
				tok, err = auth.SignMediaToken(parent.UserID, parent.CameraID, auth.MediaKindLiveHLS, trimmed, cfg.JWTSecret, ttl)
			}
			if err != nil {
				// validMediaPath rejected the name — leave it as-is.
				out.WriteString(line)
			} else {
				// Append the original resource's extension so HLS
				// demuxers (ffmpeg, hls.js) that validate by URL
				// filename suffix accept the rewritten URL. The
				// serve handler strips it before JWT parsing.
				out.WriteString("/media/v1/" + tok + filepath.Ext(trimmed))
			}
		}
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// rewriteLiveHLSAttributeURI rewrites the URI="..." attribute in an
// HLS tag line (typically EXT-X-MAP) to a signed live-hls child token.
func rewriteLiveHLSAttributeURI(line string, cfg *config.Config, parent *auth.MediaClaims, ttl time.Duration) string {
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
	// Cached signer; URLs deterministic across playlist refreshes.
	userID, errU := uuid.Parse(parent.UserID)
	cameraID, errC := uuid.Parse(parent.CameraID)
	var tok string
	var err error
	if errU == nil && errC == nil {
		tok, err = signLiveChildToken(userID, cameraID, uri, cfg.JWTSecret, ttl)
	} else {
		tok, err = auth.SignMediaToken(parent.UserID, parent.CameraID, auth.MediaKindLiveHLS, uri, cfg.JWTSecret, ttl)
	}
	if err != nil {
		return line
	}
	// Preserve the original extension on the URL so HLS demuxers accept it.
	return line[:start] + "/media/v1/" + tok + filepath.Ext(uri) + line[start+end:]
}
