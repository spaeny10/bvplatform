// P3-INFRA-06: gohlslib-backed LL-HLS live-view muxer.
//
// Architecture:
//
//	Frontend requests live token → /api/media/mint (kind=live-hls)
//	→ /media/v1/<token> → HandleMediaServeLiveHLS
//	→ LiveHLSManager.GetOrStart(cameraID) → *LiveHLSMuxer
//	→ muxer.ServePlaylist / muxer.ServeSegment
//
// Each muxer pulls RTSP from mediamtx's local relay at
// rtsp://<mediamtxRTSPHost>/<cameraID>, feeds RTP packets through
// format-specific decoders (H264 / H265), and writes access units into
// a gohlslib Muxer configured for LL-HLS (fMP4 CMAF).  The gohlslib
// Muxer's Handle method serves playlists and fMP4 segments inline from
// the /media/v1/ handler.
//
// Lifecycle:
//   - Lazy-started on the first viewer playlist request.
//   - Each playlist request calls RecordViewer(); that arms a 30-second
//     idle timer.  If no new playlist request arrives before the timer
//     fires, the muxer tears down the RTSP client and the gohlslib Muxer
//     and removes itself from the manager map.
//   - If the RTSP connection drops unexpectedly, the muxer stops itself
//     and the next incoming request creates a fresh one.
//
// Browser compatibility:
//   - Safari: native HLS + HEVC — works.
//   - Chrome 107+ with hardware H.265 decode: works.
//   - Firefox: HLS playlist served but H.265 decode not supported.
//     A future PR will add a Firefox/H.264-fallback banner in the UI.
//
// NPM buffering note: LL-HLS relies on chunked transfer encoding.  NPM
// passes Cache-Control: no-store through for /media/v1/ URLs correctly.
// If operators observe stalled playlists, verify NPM's proxy_buffering
// is off for the ironsight host.  Do NOT change NPM config from this
// code — that is prod infra.
package streaming

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gohlslib "github.com/bluenviron/gohlslib/v2"
	gohlscodecs "github.com/bluenviron/gohlslib/v2/pkg/codecs"
	gortsplib "github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph265"
	"github.com/pion/rtp"

	"github.com/google/uuid"
)

const (
	// liveHLSIdleTimeout is how long a muxer waits at zero viewers
	// before tearing down the RTSP connection.  Short enough to free
	// resources quickly; long enough to survive a page refresh.
	liveHLSIdleTimeout = 30 * time.Second

	// liveHLSSegmentMinDuration is the gohlslib target segment length.
	// 2 seconds × default 7-segment window.  LL-HLS effective latency
	// with partial segments is ~2-3 s end-to-end.
	liveHLSSegmentMinDuration = 2 * time.Second

	// liveHLSPartMinDuration is the LL-HLS partial-segment duration.
	// 200 ms matches the gohlslib default.
	liveHLSPartMinDuration = 200 * time.Millisecond
)

// ──────────────────────────────────────────────────────────────────────
// Manager
// ──────────────────────────────────────────────────────────────────────

// LiveHLSManager manages per-camera gohlslib muxers.  Each muxer is
// lazy-started on the first viewer request and idle-stopped
// liveHLSIdleTimeout after the last viewer heartbeat.
// Safe for concurrent use.
type LiveHLSManager struct {
	// mediamtxRTSPHost is "host:port" of the local RTSP relay.
	// Derived from config.MediaMTXRTSPAddr (default "127.0.0.1:18554").
	mediamtxRTSPHost string

	mu     sync.Mutex
	muxers map[uuid.UUID]*LiveHLSMuxer
}

// NewLiveHLSManager returns a manager that connects to the mediamtx RTSP
// relay at rtspAddr (e.g. "127.0.0.1:18554" or "rtsp://127.0.0.1:18554").
func NewLiveHLSManager(rtspAddr string) *LiveHLSManager {
	if i := strings.Index(rtspAddr, "://"); i >= 0 {
		rtspAddr = rtspAddr[i+3:]
	}
	if rtspAddr == "" {
		rtspAddr = "127.0.0.1:18554"
	}
	return &LiveHLSManager{
		mediamtxRTSPHost: rtspAddr,
		muxers:           make(map[uuid.UUID]*LiveHLSMuxer),
	}
}

// GetOrStart returns the live muxer for cameraID, creating one if
// necessary.  Every successful call should be followed by
// muxer.RecordViewer() so the idle-stop timer is refreshed.
func (m *LiveHLSManager) GetOrStart(cameraID uuid.UUID) (*LiveHLSMuxer, error) {
	m.mu.Lock()
	mux, ok := m.muxers[cameraID]
	if ok && !mux.isStopped() {
		m.mu.Unlock()
		return mux, nil
	}

	// Use the sub-stream path — it is on-demand in mediamtx (lower bitrate,
	// designed for live monitoring). The main stream is reserved for the
	// recording engine, which needs the full-resolution footage.
	rtspURL := fmt.Sprintf("rtsp://%s/%s_sub", m.mediamtxRTSPHost, cameraID)
	mux = newLiveHLSMuxer(cameraID, rtspURL, func() {
		m.mu.Lock()
		if cur, exists := m.muxers[cameraID]; exists && cur == mux {
			delete(m.muxers, cameraID)
		}
		m.mu.Unlock()
	})
	m.muxers[cameraID] = mux
	m.mu.Unlock()

	if err := mux.start(); err != nil {
		m.mu.Lock()
		if cur, exists := m.muxers[cameraID]; exists && cur == mux {
			delete(m.muxers, cameraID)
		}
		m.mu.Unlock()
		return nil, fmt.Errorf("livehls: start muxer for %s: %w", cameraID, err)
	}
	return mux, nil
}

// StopAll tears down every active muxer.  Call on graceful shutdown.
func (m *LiveHLSManager) StopAll() {
	m.mu.Lock()
	toStop := make([]*LiveHLSMuxer, 0, len(m.muxers))
	for _, mux := range m.muxers {
		toStop = append(toStop, mux)
	}
	m.muxers = make(map[uuid.UUID]*LiveHLSMuxer)
	m.mu.Unlock()

	for _, mux := range toStop {
		mux.stop()
	}
	log.Printf("[LIVEHLS] All muxers stopped")
}

// ──────────────────────────────────────────────────────────────────────
// Muxer
// ──────────────────────────────────────────────────────────────────────

// LiveHLSMuxer is the per-camera LL-HLS server.  It owns one RTSP
// gortsplib client and one gohlslib Muxer.  HTTP playlist / segment
// requests are served via ServePlaylist / ServeSegment.
type LiveHLSMuxer struct {
	cameraID uuid.UUID
	rtspURL  string
	onStop   func() // called once when the muxer tears down

	mu        sync.Mutex
	hlsMuxer  *gohlslib.Muxer
	rtspC     *gortsplib.Client
	_stopped  bool

	// viewers is an atomic heartbeat counter (incremented by
	// RecordViewer, decremented after liveHLSIdleTimeout).
	viewers   atomic.Int64
	idleTimer *time.Timer
}

func newLiveHLSMuxer(cameraID uuid.UUID, rtspURL string, onStop func()) *LiveHLSMuxer {
	return &LiveHLSMuxer{cameraID: cameraID, rtspURL: rtspURL, onStop: onStop}
}

func (m *LiveHLSMuxer) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m._stopped
}

// start opens the RTSP connection, discovers tracks, configures the
// gohlslib Muxer with matching codec descriptors, and begins the
// packet-relay loop.
func (m *LiveHLSMuxer) start() error {
	m.mu.Lock()
	if m._stopped {
		m.mu.Unlock()
		return fmt.Errorf("muxer already stopped")
	}
	m.mu.Unlock()

	u, err := base.ParseURL(m.rtspURL)
	if err != nil {
		return fmt.Errorf("parse rtsp url: %w", err)
	}

	c := &gortsplib.Client{}
	desc, _, err := c.Describe(u)
	if err != nil {
		return fmt.Errorf("rtsp describe %s: %w", m.rtspURL, err)
	}

	// ---- build gohlslib Track list from the RTSP description ----
	var hlsTracks []*gohlslib.Track
	type trackWiring struct {
		hlsTrack *gohlslib.Track
		rtspMed  *description.Media
		codec    interface{} // *format.H264 or *format.H265
	}
	var wirings []trackWiring

	for _, media := range desc.Medias {
		for _, f := range media.Formats {
			switch ft := f.(type) {
			case *format.H265:
				// SafeParams returns (vps []byte, sps []byte, pps []byte) — plain byte slices.
				vps, sps, pps := ft.SafeParams()
				codec := &gohlscodecs.H265{
					VPS: vps,
					SPS: sps,
					PPS: pps,
				}
				t := &gohlslib.Track{Codec: codec}
				hlsTracks = append(hlsTracks, t)
				wirings = append(wirings, trackWiring{hlsTrack: t, rtspMed: media, codec: ft})
				goto nextMedia

			case *format.H264:
				// SafeParams returns (sps []byte, pps []byte) — plain byte slices.
				sps, pps := ft.SafeParams()
				codec := &gohlscodecs.H264{
					SPS: sps,
					PPS: pps,
				}
				t := &gohlslib.Track{Codec: codec}
				hlsTracks = append(hlsTracks, t)
				wirings = append(wirings, trackWiring{hlsTrack: t, rtspMed: media, codec: ft})
				goto nextMedia
			}
		}
	nextMedia:
	}

	if len(hlsTracks) == 0 {
		c.Close()
		return fmt.Errorf("no H264/H265 video track found in RTSP stream %s", m.rtspURL)
	}

	// ---- create and start the gohlslib Muxer ----
	mux := &gohlslib.Muxer{
		Tracks:             hlsTracks,
		Variant:            gohlslib.MuxerVariantLowLatency,
		SegmentMinDuration: liveHLSSegmentMinDuration,
		PartMinDuration:    liveHLSPartMinDuration,
		OnEncodeError: func(err error) {
			log.Printf("[LIVEHLS] encode error for camera %s: %v", m.cameraID, err)
		},
	}
	if err := mux.Start(); err != nil {
		c.Close()
		return fmt.Errorf("gohlslib muxer start: %w", err)
	}

	// ---- wire per-track RTP callbacks ----
	for _, w := range wirings {
		w := w // capture

		switch ft := w.codec.(type) {
		case *format.H265:
			dec, err := ft.CreateDecoder()
			if err != nil {
				mux.Close()
				c.Close()
				return fmt.Errorf("h265 decoder: %w", err)
			}
			wMed := w.rtspMed // capture for closure
			c.OnPacketRTP(w.rtspMed, ft, func(pkt *rtp.Packet) {
				// PacketPTS returns the presentation timestamp in 90kHz clock units
				// (same unit that gohlslib's WriteH265 expects).
				pts, ok := c.PacketPTS(wMed, pkt)
				if !ok {
					return // clock not yet established; skip
				}
				au, err := dec.Decode(pkt)
				if err != nil {
					if err != rtph265.ErrNonStartingPacketAndNoPrevious &&
						err != rtph265.ErrMorePacketsNeeded {
						// benign fragmentation; skip logging
					}
					return
				}
				if err := mux.WriteH265(w.hlsTrack, time.Now(), pts, au); err != nil {
					log.Printf("[LIVEHLS] WriteH265 error for camera %s: %v", m.cameraID, err)
				}
			})

		case *format.H264:
			dec, err := ft.CreateDecoder()
			if err != nil {
				mux.Close()
				c.Close()
				return fmt.Errorf("h264 decoder: %w", err)
			}
			wMed := w.rtspMed // capture for closure
			c.OnPacketRTP(w.rtspMed, ft, func(pkt *rtp.Packet) {
				// PacketPTS returns the presentation timestamp in 90kHz clock units.
				pts, ok := c.PacketPTS(wMed, pkt)
				if !ok {
					return // clock not yet established; skip
				}
				au, err := dec.Decode(pkt)
				if err != nil {
					if err != rtph264.ErrNonStartingPacketAndNoPrevious &&
						err != rtph264.ErrMorePacketsNeeded {
						// benign fragmentation; skip logging
					}
					return
				}
				if err := mux.WriteH264(w.hlsTrack, time.Now(), pts, au); err != nil {
					log.Printf("[LIVEHLS] WriteH264 error for camera %s: %v", m.cameraID, err)
				}
			})
		}
	}

	// ---- setup and play ----
	// desc.BaseURL is the RTSP base URL from the DESCRIBE response; it may
	// differ from the stream URL u when the server uses a different base path.
	if err := c.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		mux.Close()
		c.Close()
		return fmt.Errorf("rtsp setup %s: %w", m.rtspURL, err)
	}
	if _, err := c.Play(nil); err != nil {
		mux.Close()
		c.Close()
		return fmt.Errorf("rtsp play %s: %w", m.rtspURL, err)
	}

	m.mu.Lock()
	m.hlsMuxer = mux
	m.rtspC = c
	m.mu.Unlock()

	log.Printf("[LIVEHLS] Started muxer for camera %s (RTSP: %s, %d track(s))",
		m.cameraID, m.rtspURL, len(hlsTracks))

	// Watch for RTSP disconnect and tear down on unexpected exit.
	go func() {
		err := c.Wait()
		if err != nil {
			log.Printf("[LIVEHLS] RTSP client for %s exited: %v", m.cameraID, err)
		}
		m.stop()
	}()

	return nil
}

// stop tears down the muxer and fires onStop once.
func (m *LiveHLSMuxer) stop() {
	m.mu.Lock()
	if m._stopped {
		m.mu.Unlock()
		return
	}
	m._stopped = true
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	rtspC := m.rtspC
	hlsMux := m.hlsMuxer
	m.rtspC = nil
	m.hlsMuxer = nil
	m.mu.Unlock()

	if rtspC != nil {
		rtspC.Close()
	}
	if hlsMux != nil {
		hlsMux.Close()
	}
	log.Printf("[LIVEHLS] Stopped muxer for camera %s", m.cameraID)
	if m.onStop != nil {
		m.onStop()
	}
}

// RecordViewer records one playlist request as a viewer heartbeat.
// After liveHLSIdleTimeout elapses with the viewer count at zero, the
// muxer tears itself down.  Each call arms the countdown — callers
// should call this on every successful playlist response.
func (m *LiveHLSMuxer) RecordViewer() {
	m.viewers.Add(1)
	// Reset any pending idle timer — a new viewer just arrived.
	m.mu.Lock()
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	m.mu.Unlock()

	// After liveHLSIdleTimeout, decrement the counter.  If the counter
	// reaches zero we start a final idle timer.
	go func() {
		time.Sleep(liveHLSIdleTimeout)
		remaining := m.viewers.Add(-1)
		if remaining <= 0 {
			m.mu.Lock()
			if !m._stopped && m.idleTimer == nil {
				m.idleTimer = time.AfterFunc(liveHLSIdleTimeout, func() {
					if m.viewers.Load() <= 0 {
						log.Printf("[LIVEHLS] Idle timeout for camera %s — stopping", m.cameraID)
						m.stop()
					}
				})
			}
			m.mu.Unlock()
		}
	}()
}

// ServePlaylist writes the gohlslib LL-HLS master playlist to w.
// The request path is rewritten to "/master.m3u8" so gohlslib returns
// the master playlist regardless of the original signed URL path.
func (m *LiveHLSMuxer) ServePlaylist(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	mux := m.hlsMuxer
	stopped := m._stopped
	m.mu.Unlock()

	if stopped || mux == nil {
		http.Error(w, "stream not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	req := r.Clone(r.Context())
	req.URL.Path = "/master.m3u8"
	req.URL.RawQuery = ""
	mux.Handle(w, req)
}

// ServeSegment proxies an fMP4 segment or partial-segment request to
// the gohlslib Muxer.  segmentName is the bare filename from the URL
// (e.g. "seg0.mp4", "part0_seg0.mp4").
func (m *LiveHLSMuxer) ServeSegment(w http.ResponseWriter, r *http.Request, segmentName string) {
	m.mu.Lock()
	mux := m.hlsMuxer
	stopped := m._stopped
	m.mu.Unlock()

	if stopped || mux == nil {
		http.Error(w, "stream not available", http.StatusNotFound)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	req := r.Clone(r.Context())
	req.URL.Path = "/" + segmentName
	req.URL.RawQuery = r.URL.RawQuery // LL-HLS parts use ?_HLS_msn=&_HLS_part=
	mux.Handle(w, req)
}
