# Streaming Architecture

> Last updated: 2026-05-29 — P3-INFRA-06 gohlslib LL-HLS live-view replacement.

---

## Live View (P3-INFRA-06)

Live camera preview is delivered via **LL-HLS (Low-Latency HLS)** using `github.com/bluenviron/gohlslib/v2`, embedded directly in ironsight-api. WebRTC/WHEP was removed because mediamtx silently drops H.265 tracks from its WebRTC SDP answer, causing 3 of 4 trailer cameras (HEVC sub-streams) to fail with a WHEP 400.

### Path

```
Browser → POST /api/media/mint (kind=live-hls, ttl=60s)
       ← /media/v1/<token>
Browser → GET /media/v1/<token>          (master.m3u8)
       → HandleMediaServe (kind=live-hls)
       → LiveHLSManager.GetOrStart(cameraID)
       → LiveHLSMuxer: RTSP pull from mediamtx_sub → gohlslib → fMP4 LL-HLS
       ← chunked fMP4 segments / partial segments
```

### Token

- Kind: `live-hls`
- TTL: 60 seconds
- Frontend refreshes every 30 seconds via `createMediaRefresher`
- Path field: `"live"` (synthetic; no on-disk file)

### LiveHLSManager (`internal/streaming/livehls.go`)

- One `LiveHLSMuxer` per camera UUID, created lazily on first viewer request.
- Each muxer pulls from `rtsp://<MediaMTXRTSPAddr>/<cameraID>_sub` (sub-stream on mediamtx).
- Muxer idles out 30 seconds after the last `RecordViewer()` heartbeat.
- On unexpected RTSP disconnect, the muxer tears down; the next request creates a fresh one.
- `LiveHLSManager.StopAll()` is called on server graceful shutdown.

### gohlslib configuration

| Parameter            | Value     | Rationale                              |
|----------------------|-----------|----------------------------------------|
| Variant              | LowLatency | fMP4 partial-segment LL-HLS (~2-3s latency) |
| SegmentMinDuration   | 2 s       | Matches default 7-segment window       |
| PartMinDuration      | 200 ms    | gohlslib default for LL-HLS parts      |

### Browser compatibility

| Browser               | H.264 | H.265 | Notes                                       |
|-----------------------|-------|-------|---------------------------------------------|
| Safari (Mac/iOS)      | Yes   | Yes   | Native HLS + HEVC — hls.js not needed       |
| Chrome 107+ (hw HEVC) | Yes   | Yes   | hls.js + hardware H.265 decode              |
| Chrome (sw only)      | Yes   | No    | H.265 decode requires hardware              |
| Firefox               | Yes   | No    | No HEVC support in HLS; gets error overlay  |

Firefox users on H.265-only cameras see a "codec not supported" error from hls.js. A future PR will add a browser compatibility banner. Transcoding H.265→H.264 server-side is tracked as a Phase-5 item.

---

## Recorded Playback

Playback of archived recordings uses the existing on-disk HLS path (not gohlslib). Segments are pre-muxed by the recording engine (`internal/recording/engine.go`) and served via the `/media/v1/<token>` scheme with `kind=segment` or `kind=hls` tokens. See `docs/media-auth.md`.

---

## mediamtx Role (post P3-INFRA-06)

mediamtx is now an **RTSP relay only**. Its sole job is to maintain one RTSP pull from each camera and expose:

- `rtsp://<host>:18554/<cameraID>` — main stream (persistent, for recording engine)
- `rtsp://<host>:18554/<cameraID>_sub` — sub-stream (on-demand, for live-HLS muxer)

WebRTC is disabled in the mediamtx config (`webrtc: false`). The `/webrtc/*` proxy route in ironsight-api has been removed.

---

## NPM proxy note

LL-HLS relies on chunked transfer encoding. NPM already forwards `Cache-Control: no-store` correctly for `/media/v1/` URLs. If operators observe stalled live-view playlists, verify that NPM's `proxy_buffering` is off for the ironsight host — do not change NPM config from ironsight code.
