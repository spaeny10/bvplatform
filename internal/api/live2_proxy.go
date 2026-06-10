// Phase 1a low-latency live view (low-latency-live-view-go2rtc.md):
// MSE-over-WebSocket proxy to a go2rtc sidecar.
//
// /api/live2/{cameraID}/ws upgrades the caller's WebSocket and bridges it
// to go2rtc's MSE WebSocket (ws://<Go2RTCAddr>/api/ws?src=<cameraID>_sub).
// go2rtc reads its source from mediamtx's RTSP relay (the same
// {cameraID}_sub path the HLS proxy serves), so this adds NO second
// cellular pull from the camera — it fans out the stream mediamtx already
// has. Sub-second glass-to-glass vs ~20 s for the HLS path.
//
// Dynamic cameras (no go2rtc restart): cameras are added at RUNTIME, so we
// do NOT pre-enumerate streams in go2rtc.yaml. Instead, just before the WS
// upgrade, this handler idempotently registers the camera's stream with the
// running go2rtc via its HTTP control API —
//   PUT /api/streams?name=<cameraID>_sub&src=rtsp://<mediamtx>/<cameraID>_sub
// — exactly mirroring how internal/streaming/mediamtx.go pushes paths via
// apiAddPath. go2rtc applies this in-memory with no restart, and a repeat
// PUT for an existing stream is a no-op. go2rtc's /api/ws requires the
// stream to exist (GET returns 404 for an unknown src), which is why we
// register first rather than passing a raw RTSP URL as src.
//
// Why proxy instead of pointing the browser straight at go2rtc:
//   - Auth: we reuse the exact session-cookie + CanAccessCamera gate from
//     live_proxy.go. go2rtc has no concept of our RBAC; the browser must
//     never reach it directly. 404-on-denial mirrors HandleLiveProxy so a
//     denied camera is indistinguishable from a missing one.
//   - Same-origin: the WS rides the same NPM-fronted HTTPS origin the HLS
//     path uses today — no UDP/ICE/TURN, works behind any viewer NAT.
//
// Transport detail: this is a dumb byte-pump. go2rtc's MSE protocol speaks
// a JSON control handshake then binary fMP4 segments; we forward both
// directions verbatim (text + binary frames) without interpreting them.
// The fMP4 init segment and hvcC HEVC concern are handled client-side in
// frontend/src/lib/mse-player.ts, mirroring patchHVCCCompleteness from
// live_proxy.go — see the TODO(bench) there.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// live2Dialer dials go2rtc's MSE WebSocket. Default timeouts are fine —
// go2rtc is a backplane sibling, not an arbitrary internet host. Kept as a
// package var (not the gorilla DefaultDialer) so the handshake timeout is
// explicit and a future bench can tune it without touching call sites.
var live2Dialer = websocket.DefaultDialer

// go2rtcHTTPClient is the client used for the runtime stream-registration
// PUT against go2rtc's control API. Short timeout: it's a backplane sibling
// and registration must not stall the WS upgrade for long.
var go2rtcHTTPClient = &http.Client{Timeout: 5 * time.Second}

// ensureGo2RTCStream idempotently registers the camera's sub-stream with the
// running go2rtc via PUT /api/streams, then CONFIRMS via GET. The source is
// mediamtx's RTSP relay — the same {cameraID}_sub path the recorder/HLS
// already use — so no extra cellular pull is created.
//
// Why we don't trust the PUT status code: go2rtc 1.9.14's PUT /api/streams
// handler unconditionally parses the request body as YAML and returns
// 400 ("yaml: line 1: did not find expected key") on an empty body — even
// though the query-param registration (name + src) succeeds and the stream
// appears in GET /api/streams immediately. Bench-confirmed on bob 2026-06-10:
// every PUT 400s yet every stream registers. So we ignore the PUT status
// (beyond transport errors) and verify the stream is actually present with a
// follow-up GET; only an absent stream or a transport failure is an error.
// A repeat registration for an existing stream is a no-op.
func ensureGo2RTCStream(ctx context.Context, cfg *config.Config, cameraIDStr string) error {
	name := cameraIDStr + "_sub"
	rtspSrc := fmt.Sprintf("rtsp://%s/%s", cfg.MediaMTXRTSPAddr, name)

	base := url.URL{Scheme: "http", Host: cfg.Go2RTCAddr, Path: "/api/streams"}

	putURL := base
	q := url.Values{}
	q.Set("name", name)
	q.Set("src", rtspSrc)
	putURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL.String(), nil)
	if err != nil {
		return err
	}
	resp, err := go2rtcHTTPClient.Do(req)
	if err != nil {
		return err
	}
	// Status is deliberately not checked — see the doc comment above.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Confirm the stream is registered. go2rtc returns the full stream map
	// keyed by name; the PUT is synchronous (in-memory), so the key is
	// present immediately on success.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return err
	}
	getResp, err := go2rtcHTTPClient.Do(getReq)
	if err != nil {
		return err
	}
	defer getResp.Body.Close()
	body, err := io.ReadAll(getResp.Body)
	if err != nil {
		return err
	}
	var streams map[string]json.RawMessage
	if err := json.Unmarshal(body, &streams); err != nil {
		return fmt.Errorf("go2rtc GET /api/streams: %w", err)
	}
	if _, ok := streams[name]; !ok {
		return fmt.Errorf("go2rtc stream %q not registered after PUT", name)
	}
	return nil
}

// HandleLive2Proxy proxies /api/live2/{cameraID}/ws to the go2rtc MSE
// WebSocket. Auth is checked BEFORE the upgrade (copying HandleLiveProxy
// and HandleWebSocket): an unauthenticated or unauthorized caller gets a
// plain HTTP status, never an upgraded socket.
//
// Auth: route is inside the /api group which already runs RequireAuth +
// CSRFMiddleware. The WS upgrade is a GET, which CSRFMiddleware exempts,
// so no X-CSRF-Token header is needed — same as the /api/live/* GETs.
func HandleLive2Proxy(cfg *config.Config, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		cameraIDStr := chi.URLParam(r, "cameraID")
		cameraID, err := uuid.Parse(cameraIDStr)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		ok, cErr := CanAccessCamera(r.Context(), db, claims, cameraID)
		if cErr != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			// 404 (not 403) so a denied camera is indistinguishable from a
			// non-existent one — same posture as HandleLiveProxy.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Register the stream with go2rtc at runtime (idempotent, no restart).
		// Source is mediamtx's RTSP relay — not a fresh camera pull. If this
		// fails go2rtc has no such stream and /api/ws would 404, so bail with
		// a 502 rather than upgrade into a dead socket.
		if err := ensureGo2RTCStream(r.Context(), cfg, cameraIDStr); err != nil {
			log.Printf("[LIVE2-PROXY] go2rtc stream register cam=%s: %v", cameraIDStr, err)
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}

		// go2rtc source name == the mediamtx sub path name registered above.
		src := cameraIDStr + "_sub"
		upstream := url.URL{
			Scheme:   "ws",
			Host:     cfg.Go2RTCAddr,
			Path:     "/api/ws",
			RawQuery: "src=" + url.QueryEscape(src),
		}

		// Dial go2rtc BEFORE upgrading the client. If go2rtc is down or the
		// stream isn't ready, the client still has a plain HTTP response and
		// gets a 502 — not a confusing immediately-closed WebSocket.
		upConn, upResp, err := live2Dialer.Dial(upstream.String(), nil)
		if err != nil {
			if upResp != nil {
				log.Printf("[LIVE2-PROXY] go2rtc dial cam=%s status=%d: %v", cameraIDStr, upResp.StatusCode, err)
			} else {
				log.Printf("[LIVE2-PROXY] go2rtc dial cam=%s: %v", cameraIDStr, err)
			}
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		defer upConn.Close()

		// Upgrade the client connection. upgrader is the shared one from
		// websocket.go (CheckOrigin permissive — auth already happened above
		// via the session cookie, so origin isn't the security boundary).
		clientConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			// Upgrade already wrote an error response on failure.
			log.Printf("[LIVE2-PROXY] client upgrade cam=%s: %v", cameraIDStr, err)
			return
		}
		defer clientConn.Close()

		proxyWebSocket(clientConn, upConn)
	}
}

// proxyWebSocket pumps frames in both directions between the browser client
// and go2rtc until either side closes or errors. go2rtc→client carries the
// fMP4 media (binary) and JSON control frames; client→go2rtc carries the
// MSE handshake and any seek/keyframe requests. We forward frame type +
// payload verbatim; neither side's protocol is interpreted here.
//
// Each direction runs in its own goroutine. When either copy loop ends we
// close both connections so the surviving goroutine's blocking ReadMessage
// unblocks with an error and returns — no leak.
func proxyWebSocket(client, upstream *websocket.Conn) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)

	copyFrames := func(dst, srcConn *websocket.Conn) {
		defer wg.Done()
		defer closeBoth()
		for {
			mt, payload, err := srcConn.ReadMessage()
			if err != nil {
				return
			}
			if err := dst.WriteMessage(mt, payload); err != nil {
				return
			}
		}
	}

	go copyFrames(upstream, client) // client → go2rtc
	go copyFrames(client, upstream) // go2rtc → client
	wg.Wait()
}
