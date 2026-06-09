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
//   - If the RTSP connection drops unexpectedly, the RTSP session is
//     re-established (with exponential backoff up to 30 s) while the
//     gohlslib Muxer is kept alive.  Only after the muxer is idle-stopped
//     or after repeated fatal failures is the muxer torn down.
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
	"context"
	"errors"
	"fmt"
	"log"
	"net"
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
	// 2 s instead of the HLS standard 6 s so the first segment is
	// available ~2 s after the muxer starts pulling RTSP instead of
	// 6 s — this is the dominant component of click-to-first-frame
	// latency for new viewers ("takes a long time to connect" in user
	// testing). H.265 keyframe jitter (~150 ms) is still invisible at a
	// 2 s boundary. Side-effect: more segments per minute in the muxer's
	// window, so memory + churn go up slightly, but for trailer
	// monitoring (4 cams × few viewers) that's a rounding error.
	liveHLSSegmentMinDuration = 2 * time.Second

	// liveHLSReconnectBaseDelay is the starting back-off between RTSP
	// reconnect attempts.  Doubles each failure up to liveHLSReconnectMax.
	liveHLSReconnectBaseDelay = 2 * time.Second

	// liveHLSReconnectMax caps the reconnect back-off.
	liveHLSReconnectMax = 30 * time.Second

	// liveHLSDescribeMaxAttempts is the number of times start() will retry
	// RTSP DESCRIBE before giving up.  mediamtx sourceOnDemand paths take
	// ~10 s to start the on-demand source; if the camera is slow to respond
	// mediamtx times out and closes the RTSP connection.  Retrying allows
	// the muxer to succeed on the second attempt once mediamtx has already
	// kicked off the camera connection.
	liveHLSDescribeMaxAttempts = 3

	// liveHLSDescribeRetryDelay is the pause between DESCRIBE retry attempts.
	liveHLSDescribeRetryDelay = 3 * time.Second
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
//
// If start() is still in-flight for this camera (rare concurrent case),
// GetOrStart blocks until start() finishes, then returns the muxer if
// it started successfully or an error if it failed.
func (m *LiveHLSManager) GetOrStart(cameraID uuid.UUID) (*LiveHLSMuxer, error) {
	m.mu.Lock()
	mux, ok := m.muxers[cameraID]
	if ok {
		if mux.isStopped() {
			// Stopped muxer in map — fall through to create a new one.
			delete(m.muxers, cameraID)
		} else {
			// Muxer exists and is not stopped.  Wait for start() to finish
			// before returning (handles the concurrent-request race where
			// start() was not yet done when this request arrived).
			readyCh := mux.readyCh
			m.mu.Unlock()
			<-readyCh // blocks until start() closes readyCh (or is already closed)
			if mux.isStopped() {
				// start() failed — the muxer was stopped immediately
				return nil, fmt.Errorf("livehls: stream not available for camera %s", cameraID)
			}
			log.Printf("[LIVEHLS] GetOrStart: existing muxer ready for camera %s", cameraID)
			return mux, nil
		}
	}

	// Try the sub-stream first (lower bitrate, on-demand in mediamtx,
	// designed for live monitoring).  If all sub-stream retries fail (e.g.
	// camera hasn't responded within mediamtx's readTimeout), fall back to
	// the main stream which the recording engine keeps alive continuously.
	subURL := fmt.Sprintf("rtsp://%s/%s_sub", m.mediamtxRTSPHost, cameraID)
	mainURL := fmt.Sprintf("rtsp://%s/%s", m.mediamtxRTSPHost, cameraID)

	// startURL creates, registers, starts a muxer and returns it.
	// The manager mutex must NOT be held when this is called.
	// On error the muxer is removed from the map and the error is returned.
	startURL := func(rtspURL string) (*LiveHLSMuxer, error) {
		// Declare candidate before the closure so the closure can capture
		// the variable reference (the value is assigned below, after the
		// closure is defined but before it can ever be called).
		var candidate *LiveHLSMuxer
		candidate = newLiveHLSMuxer(cameraID, rtspURL, func() {
			m.mu.Lock()
			if cur, exists := m.muxers[cameraID]; exists && cur == candidate {
				delete(m.muxers, cameraID)
			}
			m.mu.Unlock()
		})
		m.mu.Lock()
		m.muxers[cameraID] = candidate
		m.mu.Unlock()

		if err := candidate.start(); err != nil {
			m.mu.Lock()
			if cur, exists := m.muxers[cameraID]; exists && cur == candidate {
				delete(m.muxers, cameraID)
			}
			m.mu.Unlock()
			return nil, err
		}
		return candidate, nil
	}

	// Unlock the manager before attempting to start (start() does I/O).
	m.mu.Unlock()

	mux, err := startURL(subURL)
	if err != nil {
		log.Printf("[LIVEHLS] GetOrStart: sub-stream start() failed for camera %s (%v) — trying main stream", cameraID, err)
		mux, err = startURL(mainURL)
	}
	if err != nil {
		log.Printf("[LIVEHLS] GetOrStart: start() failed for camera %s: %v", cameraID, err)
		return nil, fmt.Errorf("livehls: start muxer for %s: %w", cameraID, err)
	}
	log.Printf("[LIVEHLS] GetOrStart: returning new muxer for camera %s (url=%s)", cameraID, mux.rtspURL)
	return mux, nil
}

// GetRunning returns the running muxer for cameraID, or nil if no muxer
// is active.  Unlike GetOrStart it never creates a new muxer — use it
// for segment/part requests where the muxer must already exist (the
// client's master playlist token already started it).
func (m *LiveHLSManager) GetRunning(cameraID uuid.UUID) *LiveHLSMuxer {
	m.mu.Lock()
	mux, ok := m.muxers[cameraID]
	m.mu.Unlock()
	if !ok || mux.isStopped() {
		return nil
	}
	return mux
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

// LiveHLSMuxer is the per-camera LL-HLS server.  It owns one gohlslib
// Muxer (long-lived) and one gortsplib Client at a time (replaced on
// reconnect).  HTTP playlist / segment requests are served via
// ServePlaylist / ServeSegment.
type LiveHLSMuxer struct {
	cameraID uuid.UUID
	rtspURL  string
	onStop   func() // called once when the muxer tears down

	mu       sync.Mutex
	hlsMuxer *gohlslib.Muxer
	rtspC    *gortsplib.Client
	_stopped bool

	// stopCh is closed by stop() to signal the reconnect loop to exit.
	stopCh chan struct{}

	// readyCh is closed by start() (on success or failure) to unblock
	// concurrent GetOrStart callers that arrived during initialization.
	// readyOnce ensures the channel is closed exactly once even if both
	// start() and stop() race to close it.
	readyCh   chan struct{}
	readyOnce sync.Once

	// viewers is an atomic heartbeat counter (incremented by
	// RecordViewer, decremented after liveHLSIdleTimeout).
	viewers   atomic.Int64
	idleTimer *time.Timer
}

func newLiveHLSMuxer(cameraID uuid.UUID, rtspURL string, onStop func()) *LiveHLSMuxer {
	return &LiveHLSMuxer{
		cameraID: cameraID,
		rtspURL:  rtspURL,
		onStop:   onStop,
		stopCh:   make(chan struct{}),
		readyCh:  make(chan struct{}),
	}
}

func (m *LiveHLSMuxer) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m._stopped
}

// start opens the first RTSP connection, discovers codec tracks, creates
// the gohlslib Muxer (once), and launches the reconnect loop goroutine.
func (m *LiveHLSMuxer) start() error {
	// Guarantee readyCh is always closed when start() returns, regardless
	// of the exit path.  Concurrent GetOrStart callers are waiting on it.
	defer m.readyOnce.Do(func() { close(m.readyCh) })

	m.mu.Lock()
	if m._stopped {
		m.mu.Unlock()
		return fmt.Errorf("muxer already stopped")
	}
	m.mu.Unlock()

	// Perform the initial RTSP describe to discover codec parameters.
	// We need these once to build the gohlslib Track list.
	//
	// Retry up to liveHLSDescribeMaxAttempts: mediamtx sourceOnDemand paths
	// start the camera connection asynchronously when the first reader
	// arrives.  If the camera is slow to respond, mediamtx's internal
	// readTimeout fires and it closes our RTSP connection before it can
	// relay the SDP.  A second attempt (after a brief pause) succeeds
	// because mediamtx is already dialing the camera.
	var (
		hlsTracks   []*gohlslib.Track
		wirings     []trackWiring
		desc        *description.Session
		firstClient *gortsplib.Client
		err         error
	)
	for attempt := 1; attempt <= liveHLSDescribeMaxAttempts; attempt++ {
		hlsTracks, wirings, desc, firstClient, err = m.describeAndBuildTracks()
		if err == nil {
			break
		}
		if attempt < liveHLSDescribeMaxAttempts {
			log.Printf("[LIVEHLS] DESCRIBE attempt %d/%d failed for camera %s: %v — retrying in %v",
				attempt, liveHLSDescribeMaxAttempts, m.cameraID, err, liveHLSDescribeRetryDelay)
			select {
			case <-m.stopCh:
				return fmt.Errorf("muxer stopped during DESCRIBE retry")
			case <-time.After(liveHLSDescribeRetryDelay):
			}
		}
	}
	if err != nil {
		return err
	}

	// Create the gohlslib Muxer once — it persists across RTSP reconnects.
	//
	// MuxerVariantFMP4 produces a plain HLS playlist with no LL-HLS tags
	// (#EXT-X-PART, #EXT-X-PART-INF, #EXT-X-SERVER-CONTROL, #EXT-X-PRELOAD-HINT).
	// LL-HLS turned out to be the wrong variant for our use case: even with
	// `lowLatencyMode: false` on hls.js, the manifest's LL scaffolding +
	// gohlslib's variable part durations (5 s / 1 s / 2.86 s — H.265 cellular
	// keyframe jitter spans 100 ms+ and gohlslib's partTargetDuration is a
	// running max, so PART-TARGET keeps moving) tripped hls.js's parser and
	// blew up playback with manifest-content dumps as error messages. Plain
	// HLS with 6 s segments has no part tracking at all, so the jitter is
	// invisible to the player. Latency goes from ~6 s to ~12-18 s — fine for
	// security trailer monitoring.
	//
	// The PR #39 "0-byte fMP4 segments" theory was misdiagnosis: 0-byte
	// responses happen for ANY segment that's rotated out of gohlslib's
	// sliding window, regardless of variant. hls.js fetches segments at
	// the live edge so it doesn't normally hit them. Verified by direct
	// curl: latest segment serves ~157 KB, rotated-out seg serves 0 B.
	//
	// PartMinDuration is dropped — only meaningful for LL-HLS variant.
	mux := &gohlslib.Muxer{
		Tracks:             hlsTracks,
		Variant:            gohlslib.MuxerVariantFMP4,
		SegmentMinDuration: liveHLSSegmentMinDuration,
		OnEncodeError: func(err error) {
			log.Printf("[LIVEHLS] encode error for camera %s: %v", m.cameraID, err)
		},
	}
	if err := mux.Start(); err != nil {
		firstClient.Close()
		return fmt.Errorf("gohlslib muxer start: %w", err)
	}

	// Wire callbacks and begin playing on the first client.
	if err := m.wireAndPlay(firstClient, desc, wirings, mux); err != nil {
		mux.Close()
		firstClient.Close()
		return fmt.Errorf("rtsp wire/play: %w", err)
	}

	m.mu.Lock()
	m.hlsMuxer = mux
	m.rtspC = firstClient
	m.mu.Unlock()

	log.Printf("[LIVEHLS] Started muxer for camera %s (RTSP: %s, %d track(s))",
		m.cameraID, m.rtspURL, len(hlsTracks))

	// Launch the reconnect supervisor.
	go m.reconnectLoop(mux, firstClient, hlsTracks, wirings)

	return nil
}

// trackWiring holds the association between a gohlslib Track and the
// gortsplib Media/format that feeds it.  It is reconstructed on every
// reconnect so the codec pointers stay fresh.
type trackWiring struct {
	hlsTrackIdx int // index into the gohlslib Tracks slice
	rtspMed     *description.Media
	codec       interface{} // *format.H264 or *format.H265
}

// newMediamtxPinnedClient creates and starts a gortsplib Client targeting
// mediamtxURL (e.g. "rtsp://mediamtx:18554/cameraID_sub").  The Client's
// DialContext is overridden so that every TCP connection it opens —
// including any it tries after following a mediamtx 301/302 redirect —
// is pinned to the mediamtx host:port.
//
// Without this, gortsplib follows the mediamtx redirect to the camera's
// direct RTSP URL, which is reachable at the TCP level from the api
// container but stalls because the camera limits concurrent RTSP sessions
// (the recording engine already holds one).  Pinning all dials to
// mediamtx keeps gortsplib on the relay; mediamtx proxies the stream.
//
// Returns the started Client and the parsed URL, or an error if URL
// parsing or Start() fails.  On error the Client is already closed.
func newMediamtxPinnedClient(mediamtxURL string) (*gortsplib.Client, *base.URL, error) {
	u, err := base.ParseURL(mediamtxURL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse rtsp url: %w", err)
	}

	// Extract bare host:port to use as the dial target.
	mediamtxAddr := u.Host

	proto := gortsplib.ProtocolTCP
	c := &gortsplib.Client{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Protocol: &proto,
		// Pin all dials to mediamtx regardless of the destination gortsplib
		// computes after following a redirect response.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != mediamtxAddr {
				log.Printf("[LIVEHLS] redirect dial intercepted: %s → %s (pinning to mediamtx)", address, mediamtxAddr)
			}
			return (&net.Dialer{}).DialContext(ctx, network, mediamtxAddr)
		},
	}
	if err := c.Start(); err != nil {
		return nil, nil, fmt.Errorf("rtsp client start: %w", err)
	}
	return c, u, nil
}

// describeAndBuildTracks runs RTSP DESCRIBE, parses the SDP for H264/H265,
// and returns the matching gohlslib Track list, wiring metadata, the full
// RTSP description, and the open (but not yet playing) gortsplib Client.
func (m *LiveHLSMuxer) describeAndBuildTracks() ([]*gohlslib.Track, []trackWiring, *description.Session, *gortsplib.Client, error) {
	log.Printf("[LIVEHLS] describeAndBuildTracks: starting DESCRIBE for camera %s url=%s", m.cameraID, m.rtspURL)
	c, u, err := newMediamtxPinnedClient(m.rtspURL)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	desc, _, err := c.Describe(u)
	log.Printf("[LIVEHLS] describeAndBuildTracks: DESCRIBE returned for camera %s err=%v", m.cameraID, err)
	if err != nil {
		c.Close()
		return nil, nil, nil, nil, fmt.Errorf("rtsp describe %s: %w", m.rtspURL, err)
	}

	log.Printf("[LIVEHLS] DESCRIBE %s: %d medias, baseURL=%s", m.rtspURL, len(desc.Medias), desc.BaseURL)
	for i, med := range desc.Medias {
		fmtNames := make([]string, 0, len(med.Formats))
		for _, f := range med.Formats {
			fmtNames = append(fmtNames, fmt.Sprintf("%T", f))
		}
		log.Printf("[LIVEHLS]   media[%d]: type=%s formats=%v", i, med.Type, fmtNames)
	}

	var hlsTracks []*gohlslib.Track
	var wirings []trackWiring

	for _, media := range desc.Medias {
		for _, f := range media.Formats {
			switch ft := f.(type) {
			case *format.H265:
				vps, sps, pps := ft.SafeParams()
				codec := &gohlscodecs.H265{VPS: vps, SPS: sps, PPS: pps}
				idx := len(hlsTracks)
				// ClockRate must be set explicitly — gohlslib Track does NOT
				// derive it from the Codec; zero causes divide-by-zero in fmp4WriteSample.
				hlsTracks = append(hlsTracks, &gohlslib.Track{Codec: codec, ClockRate: 90000})
				wirings = append(wirings, trackWiring{hlsTrackIdx: idx, rtspMed: media, codec: ft})
				log.Printf("[LIVEHLS] Found H265 track idx=%d for camera %s", idx, m.cameraID)
				goto nextMedia

			case *format.H264:
				sps, pps := ft.SafeParams()
				codec := &gohlscodecs.H264{SPS: sps, PPS: pps}
				idx := len(hlsTracks)
				// ClockRate must be set explicitly — gohlslib Track does NOT
				// derive it from the Codec; zero causes divide-by-zero in fmp4WriteSample.
				hlsTracks = append(hlsTracks, &gohlslib.Track{Codec: codec, ClockRate: 90000})
				wirings = append(wirings, trackWiring{hlsTrackIdx: idx, rtspMed: media, codec: ft})
				log.Printf("[LIVEHLS] Found H264 track idx=%d for camera %s", idx, m.cameraID)
				goto nextMedia
			}
		}
	nextMedia:
	}

	if len(hlsTracks) == 0 {
		c.Close()
		log.Printf("[LIVEHLS] No H264/H265 tracks in DESCRIBE response for %s (medias=%d)", m.rtspURL, len(desc.Medias))
		return nil, nil, nil, nil, fmt.Errorf("no H264/H265 video track found in RTSP stream %s", m.rtspURL)
	}

	return hlsTracks, wirings, desc, c, nil
}

// wireAndPlay calls SetupAll, registers OnPacketRTP callbacks, then calls
// Play.  The gortsplib v5 API requires SetupAll to be called before
// OnPacketRTP — SetupAll populates c.setuppedMedias which OnPacketRTP
// indexes into; calling OnPacketRTP first panics with a nil pointer.
// mux is the gohlslib Muxer to feed; wirings maps RTSP medias to
// gohlslib track indices.
func (m *LiveHLSMuxer) wireAndPlay(c *gortsplib.Client, desc *description.Session, wirings []trackWiring, mux *gohlslib.Muxer) error {
	hlsTracks := mux.Tracks

	// Step 1: Setup only the video medias we actually use.
	// Using SetupAll (all medias including non-video application tracks)
	// can delay or prevent RTCP timing alignment for the video track.
	// Mirror the recording engine's approach: Setup each video media individually.
	for _, w := range wirings {
		if _, err := c.Setup(desc.BaseURL, w.rtspMed, 0, 0); err != nil {
			return fmt.Errorf("rtsp setup media: %w", err)
		}
	}

	// Step 2: Register per-track RTP callbacks now that setup is done.
	// We use RTP-timestamp-based PTS rather than c.PacketPTS to avoid
	// stalling on RTCP SR timing: mediamtx-relayed streams may delay the
	// first RTCP SR by 30+ seconds, causing PacketPTS to return ok=false
	// for every packet until the SR arrives. Computing PTS from the first
	// RTP timestamp produces output immediately without needing RTCP.
	for _, w := range wirings {
		w := w // capture
		track := hlsTracks[w.hlsTrackIdx]
		var pktCount int64
		var firstRTPTS uint32
		var firstRTPSet bool

		// rtpToPTS converts a raw RTP timestamp (90 kHz clock) to an int64
		// PTS relative to the first packet on this track. gohlslib WriteH264/
		// WriteH265 accept raw clock ticks (int64) — not time.Duration — and
		// use the Track.ClockRate to compute the wall-clock presentation time.
		// We avoid c.PacketPTS because that requires an RTCP Sender Report to
		// initialise; mediamtx-relayed streams can delay the SR by 30+ seconds.
		rtpToPTS := func(rtpTS uint32) int64 {
			if !firstRTPSet {
				firstRTPTS = rtpTS
				firstRTPSet = true
			}
			// Handle 32-bit wraparound: signed delta in 90 kHz ticks.
			return int64(int32(rtpTS - firstRTPTS))
		}

		switch ft := w.codec.(type) {
		case *format.H265:
			dec, err := ft.CreateDecoder()
			if err != nil {
				return fmt.Errorf("h265 decoder: %w", err)
			}
			wMed := w.rtspMed
			c.OnPacketRTP(wMed, ft, func(pkt *rtp.Packet) {
				n := atomic.AddInt64(&pktCount, 1)
				if n == 1 {
					log.Printf("[LIVEHLS] First H265 RTP packet for camera %s", m.cameraID)
				}
				pts := rtpToPTS(pkt.Timestamp)
				au, err := dec.Decode(pkt)
				if err != nil {
					if err != rtph265.ErrNonStartingPacketAndNoPrevious &&
						err != rtph265.ErrMorePacketsNeeded {
						if n <= 5 {
							log.Printf("[LIVEHLS] H265 decode err #%d camera %s: %v", n, m.cameraID, err)
						}
					}
					return
				}
				if err := mux.WriteH265(track, time.Now(), pts, au); err != nil {
					log.Printf("[LIVEHLS] WriteH265 error for camera %s: %v", m.cameraID, err)
				} else if n == 1 {
					log.Printf("[LIVEHLS] First WriteH265 succeeded for camera %s", m.cameraID)
				}
			})

		case *format.H264:
			dec, err := ft.CreateDecoder()
			if err != nil {
				return fmt.Errorf("h264 decoder: %w", err)
			}
			wMed := w.rtspMed
			c.OnPacketRTP(wMed, ft, func(pkt *rtp.Packet) {
				n := atomic.AddInt64(&pktCount, 1)
				if n == 1 {
					log.Printf("[LIVEHLS] First H264 RTP packet for camera %s", m.cameraID)
				}
				pts := rtpToPTS(pkt.Timestamp)
				au, err := dec.Decode(pkt)
				if err != nil {
					if errors.Is(err, rtph264.ErrNonStartingPacketAndNoPrevious) ||
						errors.Is(err, rtph264.ErrMorePacketsNeeded) {
						return
					}
					log.Printf("[LIVEHLS] H264 decode error for camera %s: %v", m.cameraID, err)
					return
				}
				if err := mux.WriteH264(track, time.Now(), pts, au); err != nil {
					log.Printf("[LIVEHLS] WriteH264 error for camera %s: %v", m.cameraID, err)
				} else if n == 1 {
					log.Printf("[LIVEHLS] First WriteH264 succeeded for camera %s", m.cameraID)
				}
			})
		}
	}

	// Step 3: Begin playback.
	if _, err := c.Play(nil); err != nil {
		return fmt.Errorf("rtsp play: %w", err)
	}
	return nil
}

// reconnectLoop watches the current RTSP client.  When it disconnects
// (camera reboot, network blip), the loop tries to reconnect with
// exponential backoff.  It exits when stop() closes stopCh.
func (m *LiveHLSMuxer) reconnectLoop(mux *gohlslib.Muxer, firstClient *gortsplib.Client, hlsTracks []*gohlslib.Track, firstWirings []trackWiring) {
	currentClient := firstClient
	delay := liveHLSReconnectBaseDelay

	for {
		// Wait for the current client to disconnect.
		err := currentClient.Wait()
		if err != nil {
			log.Printf("[LIVEHLS] RTSP session ended for camera %s: %v", m.cameraID, err)
		} else {
			log.Printf("[LIVEHLS] RTSP session closed cleanly for camera %s", m.cameraID)
		}

		// Check if we've been stopped.
		select {
		case <-m.stopCh:
			log.Printf("[LIVEHLS] Reconnect loop exiting for camera %s (stopped)", m.cameraID)
			return
		default:
		}

		// Back off before reconnecting, but respect stopCh.
		log.Printf("[LIVEHLS] Reconnecting in %v for camera %s", delay, m.cameraID)
		select {
		case <-m.stopCh:
			log.Printf("[LIVEHLS] Reconnect loop exiting for camera %s (stopped during backoff)", m.cameraID)
			return
		case <-time.After(delay):
		}

		// Double the delay (capped).
		delay *= 2
		if delay > liveHLSReconnectMax {
			delay = liveHLSReconnectMax
		}

		// Attempt to re-describe and reconnect.
		newClient, u, clientErr := newMediamtxPinnedClient(m.rtspURL)
		if clientErr != nil {
			log.Printf("[LIVEHLS] Reconnect client init failed for camera %s: %v (will retry)", m.cameraID, clientErr)
			continue
		}
		desc, _, err := newClient.Describe(u)
		if err != nil {
			newClient.Close()
			log.Printf("[LIVEHLS] Reconnect describe failed for camera %s: %v (will retry)", m.cameraID, err)
			continue
		}

		// Re-use the same hlsTracks slice (same codec layout assumed).
		// Re-build wirings from the new description.
		var newWirings []trackWiring
		trackIdx := 0
		for _, media := range desc.Medias {
			for _, f := range media.Formats {
				switch ft := f.(type) {
				case *format.H265:
					if trackIdx < len(hlsTracks) {
						newWirings = append(newWirings, trackWiring{hlsTrackIdx: trackIdx, rtspMed: media, codec: ft})
						trackIdx++
					}
					goto nextMediaR
				case *format.H264:
					if trackIdx < len(hlsTracks) {
						newWirings = append(newWirings, trackWiring{hlsTrackIdx: trackIdx, rtspMed: media, codec: ft})
						trackIdx++
					}
					goto nextMediaR
				}
			}
		nextMediaR:
		}

		if len(newWirings) == 0 {
			newClient.Close()
			log.Printf("[LIVEHLS] Reconnect: no video tracks in re-described stream for camera %s (will retry)", m.cameraID)
			continue
		}

		if err := m.wireAndPlay(newClient, desc, newWirings, mux); err != nil {
			newClient.Close()
			log.Printf("[LIVEHLS] Reconnect wire/play failed for camera %s: %v (will retry)", m.cameraID, err)
			continue
		}

		// Success: swap the client under lock so stop() closes the right one.
		m.mu.Lock()
		if m._stopped {
			m.mu.Unlock()
			newClient.Close()
			log.Printf("[LIVEHLS] Reconnect loop exiting for camera %s (stopped after reconnect)", m.cameraID)
			return
		}
		m.rtspC = newClient
		m.mu.Unlock()

		currentClient = newClient
		delay = liveHLSReconnectBaseDelay // reset backoff on success
		log.Printf("[LIVEHLS] Reconnected RTSP for camera %s", m.cameraID)
	}
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

	// Signal the reconnect loop to exit before closing the client
	// (closing the client makes c.Wait() return, which the loop uses
	// as its wake-up; the stopCh check prevents a new reconnect).
	close(m.stopCh)

	// Unblock any GetOrStart callers waiting on readyCh in case stop()
	// is called before start() returns (e.g., shutdown race).
	m.readyOnce.Do(func() { close(m.readyCh) })

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

// ServePlaylist writes the gohlslib LL-HLS multivariant playlist to w.
// The request path is rewritten to "/index.m3u8" — the path gohlslib
// registers for the multivariant (master) playlist.  Handle() blocks until
// the first HLS segment is available, then returns the playlist.
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
	req.URL.Path = "/index.m3u8"
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
