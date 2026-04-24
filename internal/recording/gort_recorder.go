package recording

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/google/uuid"
	"github.com/pion/rtp"

	"onvif-tool/internal/database"
)

// GortRecorder is a pure-Go RTSP recorder built on gortsplib. It replaces the
// FFmpeg-based Recorder for cameras where FFmpeg's RTSP client is unreliable —
// notably cellular (5G) cameras that produce CSeq desync errors under packet
// loss. Because there's no separate process, we have direct control over the
// RTSP session and can reconnect cleanly on transient failures.
//
// Scope of this recorder:
//   - H.264 video only (no audio, no sub stream, no HLS)
//   - Writes fMP4 segments to match the existing FFmpeg filename layout
//     (seg_YYYYMMDD_HHMMSS.mp4) so FindEventClip and the /recordings/ static
//     server keep working unchanged.
//   - Rotates segments on minute boundaries, always at a keyframe.
//
// HLS live playback for cameras using this recorder is out of scope and will
// fall back to the existing FFmpeg HLS path (which can be started as a
// separate, stateless process against the same camera if needed).
type GortRecorder struct {
	cameraID   uuid.UUID
	cameraName string
	rtspURI    string
	outputDir  string
	db         *database.DB
	segmentDur time.Duration

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// NewGortRecorder constructs a recorder but does not start it.
func NewGortRecorder(cameraID uuid.UUID, cameraName, rtspURI, outputDir string,
	db *database.DB, segmentDur time.Duration) *GortRecorder {
	if segmentDur <= 0 {
		segmentDur = 60 * time.Second
	}
	return &GortRecorder{
		cameraID:   cameraID,
		cameraName: cameraName,
		rtspURI:    rtspURI,
		outputDir:  outputDir,
		db:         db,
		segmentDur: segmentDur,
	}
}

// Start spawns the record loop. Returns immediately; errors are logged and
// retried internally so transient network failures don't require restart.
func (r *GortRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	if err := os.MkdirAll(r.outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	go r.recordLoop(ctx)
	log.Printf("[GORT] Started recording for %s (%s)", r.cameraName, r.cameraID)
	return nil
}

// Stop signals the record loop to exit. It returns after cancellation is
// requested; the loop may take a moment to actually close the RTSP session.
func (r *GortRecorder) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.running = false
	log.Printf("[GORT] Stopped recording for %s", r.cameraName)
}

// recordLoop connects and records, restarting on failure with backoff.
func (r *GortRecorder) recordLoop(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := r.runOnce(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		log.Printf("[GORT] %s: session ended: %v (reconnecting in %v)", r.cameraName, err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runOnce opens one RTSP session, streams until it fails, then returns the error.
func (r *GortRecorder) runOnce(ctx context.Context) error {
	u, err := base.ParseURL(r.rtspURI)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}

	c := &gortsplib.Client{
		Scheme: u.Scheme,
		Host:   u.Host,
	}
	if err := c.Start(); err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer c.Close()

	desc, _, err := c.Describe(u)
	if err != nil {
		return fmt.Errorf("describe: %w", err)
	}

	// Find the H.264 media/format — the only codec we handle here.
	var forma *format.H264
	medi := desc.FindFormat(&forma)
	if medi == nil {
		return errors.New("no H.264 media found in stream")
	}

	rtpDec, err := forma.CreateDecoder()
	if err != nil {
		return fmt.Errorf("create H264 decoder: %w", err)
	}

	if _, err := c.Setup(desc.BaseURL, medi, 0, 0); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// Build the segment writer. SPS/PPS come from the SDP initially and can be
	// refreshed when they appear inline in the stream.
	sw := &segmentWriter{
		outputDir:  r.outputDir,
		cameraID:   r.cameraID,
		db:         r.db,
		segmentDur: r.segmentDur,
		sps:        forma.SPS,
		pps:        forma.PPS,
	}
	defer sw.close()

	// Capture packet-flow errors from the RTP callback so the outer loop sees them.
	var (
		runErrMu sync.Mutex
		runErr   error
	)
	setErr := func(e error) {
		runErrMu.Lock()
		if runErr == nil {
			runErr = e
		}
		runErrMu.Unlock()
	}

	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		pts, ok := c.PacketPTS(medi, pkt)
		if !ok {
			return // still waiting for the RTCP sender report → timestamp alignment
		}

		au, err := rtpDec.Decode(pkt)
		if err != nil {
			// Intermediate-packet states (waiting for remainder) aren't real errors.
			if errors.Is(err, rtph264.ErrNonStartingPacketAndNoPrevious) ||
				errors.Is(err, rtph264.ErrMorePacketsNeeded) {
				return
			}
			// On flaky cellular links the decoder emits "discarding frame since a
			// RTP packet is missing" and "invalid FU-A packet (non-starting)" for
			// every dropped packet or mid-fragment join. These are normal lossy-
			// network artifacts — not actionable — so we drop them silently. The
			// next keyframe resynchronizes the decoder.
			msg := err.Error()
			if strings.Contains(msg, "invalid FU-A packet") ||
				strings.Contains(msg, "discarding frame since a RTP packet is missing") {
				return
			}
			log.Printf("[GORT] %s: rtp decode: %v", r.cameraName, err)
			return
		}

		if wErr := sw.writeAccessUnit(au, pts, int(forma.ClockRate())); wErr != nil {
			setErr(fmt.Errorf("segment write: %w", wErr))
		}
	})

	if _, err := c.Play(nil); err != nil {
		return fmt.Errorf("play: %w", err)
	}

	// Wait until the connection ends or we're cancelled.
	waitErrCh := make(chan error, 1)
	go func() { waitErrCh <- c.Wait() }()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-waitErrCh:
		runErrMu.Lock()
		preferred := runErr
		runErrMu.Unlock()
		if preferred != nil {
			return preferred
		}
		return err
	}
}

// segmentWriter owns the state for rolling fMP4 segments for one RTSP session.
// It holds the current segment file, the H.264 DTS extractor (which needs to
// see every AU in order), and the SPS/PPS needed to emit a fresh Init box at
// each segment start.
type segmentWriter struct {
	outputDir  string
	cameraID   uuid.UUID
	db         *database.DB
	segmentDur time.Duration

	sps []byte
	pps []byte

	// Current segment state — reset on every rotation.
	curFile      *os.File
	curName      string
	curAbsPath   string
	curStartWall time.Time
	curStartDTS  int64 // DTS of this segment's first AU, in 90kHz
	curSeq       uint32
	dtsExtractor *h264.DTSExtractor

	// pending holds one access unit that has been decoded but not yet written,
	// because its duration equals the DTS delta to the *next* AU. We flush it
	// on arrival of the next AU (or on segment close using lastDelta).
	pending   *pendingAU
	lastDelta int64 // most recently observed DTS delta, used as fallback
}

// pendingAU is one access unit waiting for the next AU to arrive so we can
// compute its sample duration. Holding the AVCC-encoded payload avoids re-
// marshaling on flush.
type pendingAU struct {
	avcc       []byte
	dts        int64
	pts        int64
	idrPresent bool
}

// writeAccessUnit appends one access unit to the current segment, rotating
// first if needed. pts is in the RTP media clock (typically 90 kHz); we
// convert to the 90 kHz MP4 timebase and let DTSExtractor produce DTS.
func (sw *segmentWriter) writeAccessUnit(au [][]byte, pts int64, clockRate int) error {
	if clockRate == 0 {
		clockRate = 90000
	}

	// Split out SPS/PPS and access-unit delimiters. Keep them current so that
	// rotated segments can emit a valid Init box even if the SDP-provided ones
	// are stale. Also detect whether this AU is random-access (contains IDR).
	var kept [][]byte
	idrPresent := false
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		typ := h264.NALUType(nalu[0] & 0x1F)
		switch typ {
		case h264.NALUTypeSPS:
			sw.sps = append(sw.sps[:0], nalu...)
			continue
		case h264.NALUTypePPS:
			sw.pps = append(sw.pps[:0], nalu...)
			continue
		case h264.NALUTypeAccessUnitDelimiter:
			continue
		case h264.NALUTypeIDR:
			idrPresent = true
		}
		kept = append(kept, nalu)
	}
	if len(kept) == 0 {
		return nil
	}
	if idrPresent {
		// fMP4 init tracks need SPS/PPS; prepending them to keyframes also lets
		// browser seek to any segment start without state from earlier ones.
		if sw.sps != nil && sw.pps != nil {
			kept = append([][]byte{sw.sps, sw.pps}, kept...)
		}
	}

	// DTS extractor must be primed with an IDR before it will produce DTS values.
	if sw.dtsExtractor == nil {
		if !idrPresent {
			return nil // skip non-keyframe prelude until the first IDR
		}
		sw.dtsExtractor = &h264.DTSExtractor{}
		sw.dtsExtractor.Initialize()
	}
	dts, err := sw.dtsExtractor.Extract(kept, pts)
	if err != nil {
		return fmt.Errorf("dts extract: %w", err)
	}

	// Encode this AU once, upfront — we'll stash it as the new pending AU.
	avcc, err := h264.AVCC(kept).Marshal()
	if err != nil {
		return fmt.Errorf("avcc marshal: %w", err)
	}
	newAU := &pendingAU{
		avcc:       avcc,
		dts:        dts,
		pts:        pts,
		idrPresent: idrPresent,
	}

	// Rotation: close the current file and open a new one when the segment is
	// past its duration AND this AU is a keyframe (so the new segment is
	// self-contained / seekable). Before rotating, we have to flush the pending
	// AU to the *current* segment with a meaningful duration — the arriving
	// AU's DTS gives us that delta.
	now := time.Now()
	needRotate := sw.curFile == nil ||
		(idrPresent && !sw.curStartWall.IsZero() &&
			now.Sub(sw.curStartWall) >= sw.segmentDur)

	if needRotate {
		// Flush the previous segment's trailing AU with a real duration.
		if sw.pending != nil && sw.curFile != nil {
			if err := sw.flushPending(newAU.dts); err != nil {
				return err
			}
		}
		if err := sw.rotate(now, dts, clockRate); err != nil {
			return err
		}
		// New segment: first AU must be a keyframe. If somehow it isn't we skip.
		if !idrPresent {
			return nil
		}
		sw.pending = newAU
		return nil
	}

	// Not rotating — flush the pending AU with its true duration (delta to this one).
	if sw.pending != nil {
		if err := sw.flushPending(newAU.dts); err != nil {
			return err
		}
	}
	sw.pending = newAU
	return nil
}

// flushPending writes the currently-buffered access unit to the current
// segment, using nextDTS to compute its real sample duration. Clears sw.pending
// on success.
func (sw *segmentWriter) flushPending(nextDTS int64) error {
	p := sw.pending
	sw.pending = nil
	if p == nil {
		return nil
	}

	dur := nextDTS - p.dts
	if dur <= 0 {
		// DTS out of order (shouldn't happen with H264 but be defensive) —
		// fall back to the most recent good delta, or 3000 ticks (~30fps at
		// 90kHz) if we don't have one yet.
		dur = sw.lastDelta
		if dur <= 0 {
			dur = 3000
		}
	} else {
		sw.lastDelta = dur
	}

	sw.curSeq++
	part := &fmp4.Part{
		SequenceNumber: sw.curSeq,
		Tracks: []*fmp4.PartTrack{
			{
				ID:       1,
				BaseTime: uint64(p.dts - sw.curStartDTS),
				Samples: []*fmp4.PartSample{
					{
						Duration:        uint32(dur),
						PTSOffset:       int32(p.pts - p.dts),
						IsNonSyncSample: !p.idrPresent,
						Payload:         p.avcc,
					},
				},
			},
		},
	}
	return part.Marshal(sw.curFile)
}

// rotate closes the current segment (if any) and opens a fresh one. It must
// be called with an IDR-containing AU so the new segment starts seekable.
func (sw *segmentWriter) rotate(now time.Time, startDTS int64, clockRate int) error {
	if sw.curFile != nil {
		sw.finalizeCurrent()
	}

	filename := fmt.Sprintf("seg_%s.mp4", now.Format("20060102_150405"))
	absPath := filepath.Join(sw.outputDir, filename)
	f, err := os.Create(absPath)
	if err != nil {
		return fmt.Errorf("create segment: %w", err)
	}

	// Write the fMP4 init (ftyp + moov) describing a single H.264 track at
	// 90 kHz. Each segment is self-contained so it can be served or extracted
	// independently, matching what FFmpeg produces with
	// frag_keyframe+empty_moov+default_base_moof.
	init := fmp4.Init{
		Tracks: []*fmp4.InitTrack{
			{
				ID:        1,
				TimeScale: uint32(clockRate),
				Codec: &fmp4.CodecH264{
					SPS: sw.sps,
					PPS: sw.pps,
				},
			},
		},
	}
	if err := init.Marshal(f); err != nil {
		_ = f.Close()
		_ = os.Remove(absPath)
		return fmt.Errorf("init marshal: %w", err)
	}
	// Flush to disk so os.Stat reports a non-zero size immediately. Without
	// this, the Windows file-size metadata can lag by seconds, causing
	// listSegments to skip this still-open segment when an event fires right
	// after rotation.
	_ = f.Sync()

	sw.curFile = f
	sw.curName = filename
	sw.curAbsPath = absPath
	sw.curStartWall = now
	sw.curStartDTS = startDTS
	sw.curSeq = 0
	log.Printf("[GORT] %s: opened segment %s", shortID(sw.cameraID), filename)
	return nil
}

// finalizeCurrent closes the current file and registers it in the DB so the
// existing /recordings/ and clip-finding paths can see it.
func (sw *segmentWriter) finalizeCurrent() {
	if sw.curFile == nil {
		return
	}
	// Flush the trailing pending AU with a best-guess duration (we have no
	// next AU to compute a delta from). Using lastDelta keeps the trailing
	// sample in step with the stream's frame rate; 3000 is a 30fps fallback
	// when we never recorded a delta (segment had <2 AUs).
	if sw.pending != nil {
		dur := sw.lastDelta
		if dur <= 0 {
			dur = 3000
		}
		_ = sw.flushPending(sw.pending.dts + dur)
	}

	_ = sw.curFile.Sync()
	_ = sw.curFile.Close()

	if info, err := os.Stat(sw.curAbsPath); err == nil && sw.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		endTime := time.Now()
		seg := &database.Segment{
			CameraID:   sw.cameraID,
			StartTime:  sw.curStartWall,
			EndTime:    endTime,
			FilePath:   sw.curAbsPath,
			FileSize:   info.Size(),
			DurationMs: int(endTime.Sub(sw.curStartWall).Milliseconds()),
		}
		// Best-effort registration — missing DB rows don't break clip finding
		// because FindEventClip scans the filesystem directly.
		if err := sw.db.InsertSegment(ctx, seg); err != nil {
			log.Printf("[GORT] %s: InsertSegment failed for %s: %v", shortID(sw.cameraID), sw.curName, err)
		}
	}
	log.Printf("[GORT] %s: closed segment %s", shortID(sw.cameraID), sw.curName)
	sw.curFile = nil
}

func (sw *segmentWriter) close() {
	sw.finalizeCurrent()
}

// shortID trims a UUID for readable log output.
func shortID(u uuid.UUID) string {
	s := u.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

// Silence "unused" warnings for imports we may yet need as we expand the
// recorder (encoding/binary for length prefixes, etc.).
var _ = binary.BigEndian
