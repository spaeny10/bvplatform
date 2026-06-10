package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
	appmetrics "ironsight/internal/metrics"
)

// Engine manages FFmpeg recording processes for all cameras
type Engine struct {
	cfg           *config.Config
	db            *database.DB
	recorders     map[uuid.UUID]*Recorder
	gortRecorders map[uuid.UUID]*GortRecorder // pure-Go recorders (opt-in per camera)
	mu            sync.RWMutex
}

// Recorder manages a single camera's FFmpeg recording process
type Recorder struct {
	cameraID         uuid.UUID
	cameraName       string
	rtspURI          string
	subStreamURI     string
	outputDir        string
	hlsDir           string
	segmentDur       int
	mediaMTXRTSPAddr string // host:port for the local mediamtx RTSP relay; used to avoid pulling a 2nd RTSP session from the camera
	cmd              *exec.Cmd
	cancel           context.CancelFunc
	running          bool
	mu               sync.Mutex
	db               *database.DB
	ffmpegPath       string
	hasAudio         bool // set once at startup via ffprobe

	// Event-based recording
	recordingMode string // "continuous" or "event"
	ringDir       string // directory for ring buffer segments
	clipWriter    *ClipWriter
	schedule      string // JSON schedule string: {"days":[0,1,2,3,4,5,6],"start":"08:00","end":"18:00"}

	stderrPath string // path to current FFmpeg stderr capture file (truncated each run)
}

// NewEngine creates a new recording engine
func NewEngine(cfg *config.Config, db *database.DB) *Engine {
	return &Engine{
		cfg:           cfg,
		db:            db,
		recorders:     make(map[uuid.UUID]*Recorder),
		gortRecorders: make(map[uuid.UUID]*GortRecorder),
	}
}

// useGortRecorder returns true if the given camera should use the pure-Go
// recorder instead of FFmpeg. The set comes from cfg.GortCameras (env var
// GORT_CAMERAS, parsed at config-load time into a slice of full UUIDs or
// 8-char prefixes). Example env value:
//
//	GORT_CAMERAS=9ca4bcfd,6884a556
func useGortRecorder(gortCameras []string, cameraID uuid.UUID) bool {
	if len(gortCameras) == 0 {
		return false
	}
	id := cameraID.String()
	for _, tok := range gortCameras {
		if tok == "" {
			continue
		}
		if tok == id || strings.HasPrefix(id, tok) {
			return true
		}
	}
	return false
}

// RecordingSettings holds the settings for starting a recording
type RecordingSettings struct {
	RecordingMode     string
	PreBufferSec      int
	PostBufferSec     int
	RecordingTriggers string
	Schedule          string // JSON: {"days":[0-6],"start":"HH:MM","end":"HH:MM"}
}

// StartRecording begins recording for a camera
func (e *Engine) StartRecording(cameraID uuid.UUID, cameraName, rtspURI, subStreamURI string, settings ...RecordingSettings) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.recorders[cameraID]; ok {
		return fmt.Errorf("recording already active for camera %s", cameraID)
	}

	// Create camera-specific output directory
	outputDir := filepath.Join(e.cfg.StoragePath, cameraID.String())
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create recording dir: %w", err)
	}

	// Opt-in: use the pure-Go gortsplib recorder for cameras listed in GORT_CAMERAS.
	// Skips FFmpeg entirely for that camera (no HLS from this engine either).
	if useGortRecorder(e.cfg.GortCameras, cameraID) {
		gr := NewGortRecorder(cameraID, cameraName, rtspURI, outputDir, e.db, time.Duration(e.cfg.SegmentDuration)*time.Second)
		if err := gr.Start(); err != nil {
			return err
		}
		e.gortRecorders[cameraID] = gr
		log.Printf("[REC] Camera %s using Go recorder (gortsplib)", cameraName)
		// P1-C-03: update gauges after a Go recorder starts.
		appmetrics.SetActiveCameras(len(e.recorders) + len(e.gortRecorders))
		appmetrics.SetFFmpegSubprocesses(len(e.recorders))
		return nil
	}

	mode := "continuous"
	preBuffer := 10
	postBuffer := 30
	triggers := "motion,object"
	schedule := ""
	if len(settings) > 0 {
		s := settings[0]
		if s.RecordingMode != "" {
			mode = s.RecordingMode
		}
		if s.PreBufferSec > 0 {
			preBuffer = s.PreBufferSec
		}
		if s.PostBufferSec > 0 {
			postBuffer = s.PostBufferSec
		}
		if s.RecordingTriggers != "" {
			triggers = s.RecordingTriggers
		}
		if s.Schedule != "" {
			schedule = s.Schedule
		}
	}

	recorder := &Recorder{
		cameraID:         cameraID,
		cameraName:       cameraName,
		rtspURI:          rtspURI,
		subStreamURI:     subStreamURI,
		outputDir:        outputDir,
		hlsDir:           filepath.Join(e.cfg.HLSPath, cameraID.String()),
		segmentDur:       e.cfg.SegmentDuration,
		mediaMTXRTSPAddr: e.cfg.MediaMTXRTSPAddr,
		db:               e.db,
		ffmpegPath:       e.cfg.FFmpegPath,
		recordingMode:    mode,
		schedule:         schedule,
	}

	// Set up ring buffer directory and clip writer for event mode
	if mode == "event" {
		ringDir := filepath.Join(e.cfg.StoragePath, cameraID.String(), "ring")
		os.MkdirAll(ringDir, 0755)
		recorder.ringDir = ringDir
		recorder.clipWriter = NewClipWriter(cameraID, cameraName, ringDir, outputDir,
			preBuffer, postBuffer, triggers, e.db)
	}

	// Probe for audio before starting so segments are tagged correctly
	recorder.hasAudio = probeHasAudio(e.cfg.FFmpegPath, rtspURI)
	log.Printf("[REC] Camera %s audio detected: %v", cameraName, recorder.hasAudio)

	if err := recorder.Start(); err != nil {
		return err
	}

	e.recorders[cameraID] = recorder
	log.Printf("[REC] Started recording for camera %s (%s) mode=%s", cameraName, cameraID, mode)
	// P1-C-03: update Prometheus gauges after a new FFmpeg recorder starts.
	appmetrics.SetActiveCameras(len(e.recorders) + len(e.gortRecorders))
	appmetrics.SetFFmpegSubprocesses(len(e.recorders))
	return nil
}

// StopRecording stops recording for a camera
func (e *Engine) StopRecording(cameraID uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if gr, ok := e.gortRecorders[cameraID]; ok {
		gr.Stop()
		delete(e.gortRecorders, cameraID)
		log.Printf("[REC] Stopped Go recording for camera %s", cameraID)
		// P1-C-03: update Prometheus gauges after a Go recorder stops.
		appmetrics.SetActiveCameras(len(e.recorders) + len(e.gortRecorders))
		appmetrics.SetFFmpegSubprocesses(len(e.recorders))
		return nil
	}
	recorder, ok := e.recorders[cameraID]
	if !ok {
		return fmt.Errorf("no active recording for camera %s", cameraID)
	}

	recorder.Stop()
	delete(e.recorders, cameraID)
	log.Printf("[REC] Stopped recording for camera %s", cameraID)
	// P1-C-03: update Prometheus gauges after an FFmpeg recorder stops.
	appmetrics.SetActiveCameras(len(e.recorders) + len(e.gortRecorders))
	appmetrics.SetFFmpegSubprocesses(len(e.recorders))
	return nil
}

// StopAll stops all active recordings
func (e *Engine) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for id, recorder := range e.recorders {
		recorder.Stop()
		delete(e.recorders, id)
	}
	for id, gr := range e.gortRecorders {
		gr.Stop()
		delete(e.gortRecorders, id)
	}
	// P1-C-03: zero out Prometheus gauges — no cameras are recording.
	appmetrics.SetActiveCameras(0)
	appmetrics.SetFFmpegSubprocesses(0)
	log.Println("[REC] All recordings stopped")
}

// IsRecording checks if a camera is currently being recorded (either engine)
func (e *Engine) IsRecording(cameraID uuid.UUID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if _, ok := e.recorders[cameraID]; ok {
		return true
	}
	_, ok := e.gortRecorders[cameraID]
	return ok
}

// ActiveCount returns the number of currently active recording sessions
func (e *Engine) ActiveCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.recorders) + len(e.gortRecorders)
}

// TriggerEvent notifies the recording engine of an event for a camera.
// In event-based mode, this triggers clip creation.
func (e *Engine) TriggerEvent(cameraID uuid.UUID, eventType string) {
	e.mu.RLock()
	recorder, ok := e.recorders[cameraID]
	e.mu.RUnlock()

	if !ok || recorder.recordingMode != "event" || recorder.clipWriter == nil {
		return
	}

	if recorder.clipWriter.ShouldTrigger(eventType) {
		recorder.clipWriter.TriggerEvent(context.Background(), eventType, time.Now())
	}
}

// Start begins the FFmpeg recording process
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	go r.recordLoop(ctx)

	r.running = true
	return nil
}

// Stop terminates the FFmpeg recording process
func (r *Recorder) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}

	if r.cancel != nil {
		r.cancel()
	}

	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}

	r.running = false
}

// recordLoop runs FFmpeg and restarts on failure
func (r *Recorder) recordLoop(ctx context.Context) {
	retryDelay := 5 * time.Second
	fastCrashCount := 0
	// Track recent crash times in a sliding window so we can detect unreliable
	// cameras (cellular/5G) that fail after minutes of running — not just in
	// tight fast-crash bursts. Cellular cameras produce RTSP CSeq-desync errors
	// whose only practical mitigation is to reduce the number of RTSP sessions
	// FFmpeg opens, i.e. drop the sub stream.
	var recentCrashes []time.Time

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check recording schedule — pause if outside scheduled hours
		if r.schedule != "" && !isWithinSchedule(r.schedule) {
			log.Printf("[REC] %s: outside scheduled hours, pausing (checking every 30s)", r.cameraName)
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}

		startTime := time.Now()
		err := r.runFFmpeg(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return // context canceled, expected shutdown
			}
			// P1-C-04: emit a discrete app alert for each non-expected FFmpeg
			// crash so alertmanager can surface recurring subprocess failures
			// even when the process is auto-restarting. The label is bounded
			// (alert name is a constant string; camera name is not a label).
			appmetrics.SetCustomAlert("ffmpeg_subprocess_crash", "warning",
				"FFmpeg exited for camera "+r.cameraName+": "+err.Error())
			// Detect fast crashes (< 5s) — likely bandwidth/connection errors.
			// We keep counting them (and the crash alert above fires), but we
			// no longer drop the sub stream: the sub is now the RECORDED
			// stream and the sole ffmpeg input. Dropping it would fall back to
			// the heavy full-res main over cellular (worse), so the sub is
			// never dropped.
			if time.Since(startTime) < 5*time.Second {
				fastCrashCount++
			} else {
				fastCrashCount = 0
			}
			// Slow-crash bookkeeping: track recent crash times in a 15-minute
			// sliding window so the crash rate is observable. We used to drop
			// the sub stream after ≥3 crashes here, but the sub is now the
			// RECORDED stream and ffmpeg's only input — dropping it would
			// force a fallback to the heavy full-res main over cellular
			// (worse) or break input selection, so the sub is never dropped.
			// The crash alert above still fires; we just keep restarting on
			// the sub.
			now := time.Now()
			recentCrashes = append(recentCrashes, now)
			windowStart := now.Add(-15 * time.Minute)
			pruned := recentCrashes[:0]
			for _, t := range recentCrashes {
				if t.After(windowStart) {
					pruned = append(pruned, t)
				}
			}
			recentCrashes = pruned
			stderrTail := ""
			if r.stderrPath != "" {
				if data, rerr := os.ReadFile(r.stderrPath); rerr == nil {
					// Keep only the last ~4KB so the log stays readable but includes the error.
					if len(data) > 4096 {
						data = data[len(data)-4096:]
					}
					stderrTail = string(data)
				}
			}
			log.Printf("[REC] FFmpeg error for %s: %v (restarting in %v)\n[REC][stderr tail for %s]\n%s\n[REC][/stderr tail]", r.cameraName, err, retryDelay, r.cameraName, stderrTail)
			time.Sleep(retryDelay)
			continue
		}
	}
}

// scheduleWindow is one entry in a recording schedule. Same shape the
// Monitoring Schedule uses — deliberately, so a single visual editor
// drives both. Zero-value or disabled windows don't gate recording.
type scheduleWindow struct {
	Enabled   bool   `json:"enabled"`
	Days      []int  `json:"days"`       // 0=Sun … 6=Sat
	StartTime string `json:"start_time"` // "HH:MM"
	EndTime   string `json:"end_time"`   // "HH:MM"
}

// scheduleConfig is the legacy single-window format we accepted before
// 2026-04. Kept for back-compat: sites backfilled with the old format
// keep working until someone edits the schedule in the new UI.
type scheduleConfig struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// isWithinSchedule reports whether the current time is inside any active
// window of the given schedule JSON. Parsing tries three shapes in order:
//
//  1. Array of scheduleWindow — the current format. Any single enabled
//     window that covers "now" opens the recorder.
//  2. Single scheduleConfig — legacy format preserved from pre-2026-04
//     backfilled rows.
//  3. Anything else / empty — always record.
func isWithinSchedule(scheduleJSON string) bool {
	s := strings.TrimSpace(scheduleJSON)
	if s == "" {
		return true
	}
	now := time.Now()

	// New format: an array of windows.
	if s[0] == '[' {
		var windows []scheduleWindow
		if err := json.Unmarshal([]byte(s), &windows); err == nil {
			if len(windows) == 0 {
				return true
			}
			for _, w := range windows {
				if !w.Enabled {
					continue
				}
				if nowInWindow(now, w.Days, w.StartTime, w.EndTime) {
					return true
				}
			}
			return false
		}
		// Array-shaped but malformed — be permissive, don't silently stop.
		return true
	}

	// Legacy single-object format.
	var sched scheduleConfig
	if err := json.Unmarshal([]byte(s), &sched); err != nil {
		return true
	}
	if len(sched.Days) == 0 || sched.Start == "" || sched.End == "" {
		return true
	}
	return nowInWindow(now, sched.Days, sched.Start, sched.End)
}

// nowInWindow returns true when `now` falls inside an (HH:MM, HH:MM) range
// on one of the given days. Handles overnight ranges (e.g. 22:00 → 06:00)
// by checking the "spans midnight" branch.
func nowInWindow(now time.Time, days []int, start, end string) bool {
	today := int(now.Weekday())
	dayMatch := false
	for _, d := range days {
		if d == today {
			dayMatch = true
			break
		}
	}
	if !dayMatch {
		return false
	}

	startParts := strings.SplitN(start, ":", 2)
	endParts := strings.SplitN(end, ":", 2)
	if len(startParts) != 2 || len(endParts) != 2 {
		return true // malformed times — be permissive
	}
	var startH, startM, endH, endM int
	fmt.Sscanf(startParts[0], "%d", &startH)
	fmt.Sscanf(startParts[1], "%d", &startM)
	fmt.Sscanf(endParts[0], "%d", &endH)
	fmt.Sscanf(endParts[1], "%d", &endM)

	currentMinutes := now.Hour()*60 + now.Minute()
	startMinutes := startH*60 + startM
	endMinutes := endH*60 + endM

	if startMinutes <= endMinutes {
		return currentMinutes >= startMinutes && currentMinutes < endMinutes
	}
	// Overnight window (e.g. 22:00 → 06:00).
	return currentMinutes >= startMinutes || currentMinutes < endMinutes
}

// useLocalRelay used to choose between mediamtx relay vs direct camera
// for recording. It's intentionally retained as an unused helper for the
// gort recorder path; the FFmpeg recorder now always dials the camera
// directly (see runFFmpeg above for the rationale).
func (r *Recorder) useLocalRelay(_ context.Context, _ string) bool {
	_ = r.mediaMTXRTSPAddr
	return false
}

// runFFmpeg executes the FFmpeg process for segment-based recording
func (r *Recorder) runFFmpeg(ctx context.Context) error {
	os.MkdirAll(r.hlsDir, 0755)

	// Recording dials the camera DIRECTLY, not through the mediamtx relay.
	// The earlier "relay everything" approach (PR #48) backfired: mediamtx
	// is sensitive to damaged HEVC NAL fragmentation from cellular sources
	// and overflows its internal RTP reorder buffer ("buffer length
	// exceeds 64") roughly every 30s. When mediamtx tears down + reconnects
	// the upstream path, the recorder's ffmpeg sees a brief 404 window and
	// either dies on startup or stops receiving frames mid-session, then
	// the 180s stall watchdog kills it. The 0.3fps observation in stderr
	// is the average across one of those frozen sessions, not steady state.
	//
	// Bypassing mediamtx for the recorder eliminates that failure mode at
	// the cost of one extra RTSP session per camera against the camera
	// itself — which we previously feared would saturate the cellular link
	// but actually doesn't: mediamtx + 1 ffmpeg-recorder = 2 connections,
	// still well under what the DVR can serve. Live HLS continues to use
	// mediamtx via /api/live/ so the live path is unaffected.
	//
	// Record the SUB stream, not the full-res main. The main is H.265
	// full-res and is too heavy to pull+record continuously over the
	// cellular uplink: its ffmpeg frame counter freezes mid-session while
	// the sub keeps flowing, the stall watchdog kills ffmpeg, and the
	// in-progress segment is discarded ("moov atom not found") — ~50%
	// recording gaps. The sub is H.264/low-bitrate and stays gapless on
	// cellular, so we make it the recorder's SOLE input. Live view is
	// unaffected: mediamtx serves it independently from the camera sub.
	inputURI := r.rtspURI
	if r.subStreamURI != "" {
		inputURI = r.subStreamURI
	}

	args := []string{
		"-fflags", "+nobuffer+genpts",
		"-flags", "low_delay",
		"-probesize", "256000",
		"-analyzeduration", "500000",
		"-rtsp_transport", "tcp",
		// Allow only video+audio at the demuxer; drops the Milesight
		// "Generic" metadata track that ffmpeg can't decode and that
		// would otherwise allocate a stream context the rest of the
		// pipeline can't satisfy.
		"-allowed_media_types", "video+audio",
		"-i", inputURI,
	}

	// LOCAL-09: probe the source codec once so we can conditionally rewrite
	// the codec_tag from `hev1` (what Milesight emits) to `hvc1` (what Safari
	// recognises) for HEVC sources. MUST NOT pass `-tag:v hvc1` on H.264
	// streams — the mp4 box would say hvc1 over actual H.264 bytes, which
	// breaks playback in every browser. Empty/unknown codec → no override
	// (fail-safe: keep whatever ffmpeg picks by default, which is correct
	// for the source codec).
	hvc1Tag := []string{}
	switch probeStreamVideoCodec(r.ffmpegPath, inputURI) {
	case "hevc", "h265":
		hvc1Tag = []string{"-tag:v", "hvc1"}
	}

	// 1) HLS live stream output. Always single-variant from input 0, which
	// is now the sub stream (see inputURI selection above). The old
	// dual-variant var_stream_map (HD main + SD sub) is gone: the live HD/SD
	// toggle goes away, which is acceptable because live view is served by
	// mediamtx via /api/live/, and main-over-cellular HD stalls anyway.
	args = append(args,
		"-map", "0:v:0",
		"-c:v", "copy",
	)
	args = append(args, hvc1Tag...)
	args = append(args,
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "5",
		"-hls_flags", "delete_segments+append_list",
		"-hls_segment_filename", filepath.Join(r.hlsDir, "seg_%03d.ts"),
		"-y",
		filepath.Join(r.hlsDir, "live.m3u8"),
	)

	// Create a child context that lives only for this FFmpeg execution.
	// This ensures watchSegments and ringBufferCleanup goroutines are
	// terminated when FFmpeg exits (before recordLoop restarts it).
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	// Audio codec for MP4 containers: always transcode to AAC. MP4 cannot hold
	// pcm_mulaw/pcm_alaw (G.711), which is common on IP cameras — attempting
	// -c:a copy fails with AVERROR(EINVAL) before the first segment is written.
	// AAC transcoding adds negligible CPU overhead at 64 kbps and is safe even
	// when the source is already AAC (minor re-encoding loss is acceptable).
	// `-map 0:a:0?` makes the audio optional, so this flag is harmless when
	// the camera has no audio track.
	audioCodec := "aac"

	// 2) Recording output depends on mode (uses original stream with -c copy)
	if r.recordingMode == "event" {
		// Event mode: write short ring buffer segments (2s each)
		os.MkdirAll(r.ringDir, 0755)
		ringPattern := filepath.Join(r.ringDir, "ring_%Y%m%d_%H%M%S.mp4")
		args = append(args,
			"-map", "0:v:0",
			"-map", "0:a:0?",
			"-c:v", "copy",
		)
		args = append(args, hvc1Tag...)
		args = append(args,
			"-c:a", audioCodec,
			"-f", "segment",
			"-segment_time", "2",
			"-segment_format", "mp4",
			"-segment_atclocktime", "1",
			"-reset_timestamps", "1",
			"-strftime", "1",
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
			"-y",
			ringPattern,
		)
		// Start ring buffer cleanup goroutine (scoped to this FFmpeg run)
		go r.ringBufferCleanup(runCtx)
	} else {
		// Continuous mode: write normal long segments
		outputPattern := filepath.Join(r.outputDir, "seg_%Y%m%d_%H%M%S.mp4")
		args = append(args,
			"-map", "0:v:0",
			"-map", "0:a:0?",
			"-c:v", "copy",
		)
		args = append(args, hvc1Tag...)
		args = append(args,
			"-c:a", audioCodec,
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%d", r.segmentDur),
			"-segment_format", "mp4",
			"-segment_atclocktime", "1",
			"-reset_timestamps", "1",
			"-strftime", "1",
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
			"-y",
			outputPattern,
		)
	}

	cmd := exec.CommandContext(ctx, r.ffmpegPath, args...)
	// P1-C-05: force the ffmpeg subprocess to interpret strftime in UTC
	// so segment filenames (`seg_%Y%m%d_%H%M%S.mp4`, `ring_*` etc.) are
	// always in UTC regardless of the host timezone. Without TZ=UTC, a
	// host in CDT writes filenames in CDT, and parseSegmentTimestamp
	// (which now parses in UTC, also P1-C-05) would shift the segment
	// boundaries by 5 hours every spring/fall on DST flips. Existing
	// pre-fix filenames stay accessible because parseSegmentTimestamp's
	// behaviour for already-recorded files isn't going to be re-run.
	cmd.Env = append(os.Environ(), "TZ=UTC")
	cmd.Stdout = nil
	// Capture FFmpeg stderr to a per-camera file (truncated each run) so we can
	// surface the full startup output on crash. This replaces the discarded
	// stderr that was masking EINVAL failures.
	stderrPath := filepath.Join(r.outputDir, "ffmpeg_stderr.log")
	if f, ferr := os.Create(stderrPath); ferr == nil {
		cmd.Stderr = f
		r.stderrPath = stderrPath
		// Close the file when FFmpeg exits OR when we leave this function
		// for any other reason (Wait returns, stall watchdog kills FFmpeg,
		// panic). The previous fire-and-forget goroutine could leak the FD
		// across many crash/restart cycles before runCtx finally canceled.
		// sync.Once-style: defer and Done channel — whichever fires first.
		stderrFile := f
		defer func() {
			_ = stderrFile.Close()
		}()
	}

	// Manually write master playlist for dual-stream since FFmpeg stream-copy won't auto-generate it
	if r.subStreamURI != "" {
		masterPL := "#EXTM3U\n" +
			"#EXT-X-VERSION:3\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=1500000,RESOLUTION=1920x1080,NAME=\"Main\"\n" +
			"main_live.m3u8\n" +
			"#EXT-X-STREAM-INF:BANDWIDTH=500000,RESOLUTION=640x360,NAME=\"Sub\"\n" +
			"sub_live.m3u8\n"
		os.MkdirAll(r.hlsDir, 0755)
		os.WriteFile(filepath.Join(r.hlsDir, "live.m3u8"), []byte(masterPL), 0644)
	}

	log.Printf("[REC] Starting FFmpeg for %s", r.cameraName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	// Publish the cmd under the mutex AFTER Start so cmd.Process is set
	// and immutable by the time Stop() can see it: Stop() reads r.cmd
	// under r.mu to kill the active process, and the restart cycle
	// reassigns it on every run — the previous lock-free write was a
	// data race. A Stop() that fires in the window before publication
	// still kills this process via r.cancel() (same ctx given to
	// CommandContext). The local `cmd` stays the handle for this run
	// (Wait + watchdog) so the mutex is never held across Wait().
	r.mu.Lock()
	r.cmd = cmd
	r.mu.Unlock()

	// Start segment watcher (scoped to this FFmpeg run via runCtx)
	go r.watchSegments(runCtx)

	// Stall watchdog: kill FFmpeg if no segment file grows for 2× segment duration.
	// Cellular streams can drop silently without closing the TCP connection, causing
	// cmd.Wait() to block indefinitely. This detects the stall and forces a restart.
	go func() {
		// Panic guard: a malformed directory listing or os.Stat hiccup
		// must not crash the recording supervisor. Recover and return so
		// the next restart cycle takes over.
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[REC] PANIC in stall watchdog for %s: %v", r.cameraName, rec)
			}
		}()
		// Stall watchdog headroom. Now that the recorder pulls the SUB
		// stream (H.264/low-bitrate, reliable over cellular), the long
		// headroom we previously needed for slow HEVC keyframes on the
		// full-res main is no longer warranted. segmentDur*2 (120 s for
		// 60 s segments) means "two missed segments = stalled": 3× tighter
		// than the old 360 s, so we recover from a real stall fast, while
		// still tolerating one slow segment without a false kill.
		stallTimeout := time.Duration(r.segmentDur*2) * time.Second
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var lastTotalSize int64
		watchStart := time.Now()

		// Watch the directory ffmpeg actually writes to. In event mode
		// segments land in r.ringDir; r.outputDir only gains per-clip
		// subdirectories, so scanning it top-level would never see a
		// fresh .mp4 and the watchdog would be permanently blind.
		watchDir := r.outputDir
		if r.recordingMode == "event" {
			watchDir = r.ringDir
		}

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				entries, err := os.ReadDir(watchDir)
				if err != nil {
					continue
				}
				var totalSize int64
				var latestMod time.Time
				for _, e := range entries {
					if !strings.HasSuffix(e.Name(), ".mp4") {
						continue
					}
					if info, ferr := e.Info(); ferr == nil {
						totalSize += info.Size()
						if info.ModTime().After(latestMod) {
							latestMod = info.ModTime()
						}
					}
				}
				if totalSize > lastTotalSize {
					lastTotalSize = totalSize
					continue // progress detected, reset stall window
				}
				if latestMod.IsZero() {
					// No segment has ever appeared in this run (brand-new
					// camera, or event-mode ring already swept clean after
					// a stall). A camera that accepts the RTSP connection
					// but never delivers frames blocks cmd.Wait() just as
					// hard as one that stops mid-run — give it one full
					// stall window from process start, then kill.
					if time.Since(watchStart) <= stallTimeout {
						continue
					}
				} else if time.Since(latestMod) <= stallTimeout {
					continue
				}
				log.Printf("[REC] Stream stall detected for %s (no segment growth in %v) — killing FFmpeg", r.cameraName, stallTimeout)
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				return
			}
		}
	}()

	return cmd.Wait()
}

// watchSegments monitors the output directory for new segments and registers them in the DB
func (r *Recorder) watchSegments(ctx context.Context) {
	seen := make(map[string]bool)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	pruneCounter := 0
	startedAt := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneCounter++

			// Prune the seen map every ~100 ticks (~16 min) to prevent unbounded growth.
			// Remove entries for files that no longer exist on disk (already deleted by retention).
			if pruneCounter >= 100 {
				pruneCounter = 0
				for name := range seen {
					if _, err := os.Stat(filepath.Join(r.outputDir, name)); os.IsNotExist(err) {
						delete(seen, name)
					}
				}
			}

			entries, err := os.ReadDir(r.outputDir)
			if err != nil {
				continue
			}

			// Determine the newest segment file this pass. ffmpeg keeps the
			// most-recent segment OPEN (and unfinalized — no moov atom) until
			// it rotates to the next file, so the open segment can never be
			// probed and must never be registered yet. os.ReadDir returns
			// names sorted ascending and our segment names (seg_YYYYMMDD_
			// HHMMSS.mp4 continuous / ring_*.mp4 event) sort chronologically,
			// so the LAST entry matching our pattern is the currently-open
			// one. We skip it WITHOUT marking it seen, so a later scan picks
			// it up once a newer sibling exists (i.e. it has finalized).
			//
			// Background: the H.264 sub stream is gapless but low-bitrate, so
			// no new fragment lands in the open file during the 2s stability
			// check below — the size looks stable and the file would be
			// treated as finalized, probed before the moov is written, fail
			// ("moov atom not found") and previously get marked seen forever.
			// Excluding the open file removes that whole failure class.
			newestSegment := ""
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				n := e.Name()
				if !strings.HasSuffix(n, ".mp4") || (!strings.HasPrefix(n, "seg_") && !strings.HasPrefix(n, "ring_")) {
					continue
				}
				if n > newestSegment {
					newestSegment = n
				}
			}

			for _, entry := range entries {
				if entry.IsDir() || seen[entry.Name()] {
					continue
				}

				// Never probe/register the currently-open (newest) segment —
				// ffmpeg is still writing it and has not flushed the moov.
				// Do NOT mark it seen; a later scan handles it after rotation.
				if entry.Name() == newestSegment {
					continue
				}

				// LOCAL-10: only index files that match the segment-output
				// pattern this recorder is writing — seg_*.mp4 (continuous
				// mode) or ring_*.mp4 (event mode). The previous loop
				// also picked up `ffmpeg_stderr.log` (4-9 KB, passes the
				// size gate) and indexed it as a "segment". That row then
				// flows through the timeline endpoint and the player tries
				// to render the text log as video. Filter at the source.
				name := entry.Name()
				if !strings.HasSuffix(name, ".mp4") || (!strings.HasPrefix(name, "seg_") && !strings.HasPrefix(name, "ring_")) {
					continue
				}

				filePath := filepath.Join(r.outputDir, name)
				info, err := entry.Info()
				if err != nil || info.Size() < 1000 {
					continue // Skip tiny/incomplete files
				}

				// LOCAL-10: only process files this recorder run produced.
				// Files older than startedAt are already in the DB from
				// prior runs (the segments hypertable has no UNIQUE
				// constraint on (camera_id, file_path) yet, so duplicate
				// InsertSegment calls on every restart bloat the table by
				// ~9.5K rows per camera per startup — and on 27M-row
				// tables each InsertSegment is slow enough that the
				// rescan never finishes before the next ffmpeg stall +
				// restart, blocking fresh segments from being indexed
				// at all). Mark as seen and skip entirely.
				if info.ModTime().Before(startedAt) {
					seen[name] = true
					continue
				}

				// Only do the expensive 2-second stability check for files that
				// could still be actively written (modified recently). Old files
				// from previous sessions are already complete on disk — sleeping
				// 2s per old file was causing hours of delay on startup when
				// thousands of segments existed.
				if time.Since(info.ModTime()) < 30*time.Second {
					time.Sleep(2 * time.Second)
					info2, err := os.Stat(filePath)
					if err != nil || info2.Size() != info.Size() {
						continue // File still being written
					}
				}

				// Parse timestamp from filename (seg_YYYYMMDD_HHMMSS.mp4)
				startTime := parseSegmentTimestamp(entry.Name())
				if startTime.IsZero() {
					startTime = info.ModTime().Add(-time.Duration(r.segmentDur) * time.Second)
				}

				// Probe the codec so the recorded-playback serve handler can
				// decide pass-through vs transcode without ffprobing every
				// segment on every request.
				//
				// Scope-limited to segments produced by THIS recorder run
				// (mtime after startedAt). On startup, watchSegments re-
				// scans every existing file in the camera dir — for a
				// 9.5 K-file backlog at ~100 ms/probe that's a 16-minute
				// stall before any fresh segment gets indexed, which we
				// can't accept. Old segments inherit a NULL `video_codec`,
				// which playback treats as "unknown, playable": the
				// /media/v1 serve handler probes the file per-request
				// (maybeTranscodeForBrowser) to pick pass-through vs
				// transcode. Nothing writes the codec back to the DB.
				//
				// Probe codec AND actual file duration. Hardcoding
				// duration = r.segmentDur is wrong when ffmpeg restarted
				// mid-segment: the file ends up shorter (5-46s observed
				// on 2026-06-09) but the DB row says 60s, so the playback
				// player seeks past EOF and the decoder shows the last
				// frame with packet-loss artifacts ("garbled recent
				// recordings" bug).
				//
				// Codec-probe failure is the canary for "moov atom not
				// found" — i.e. ffmpeg was killed before it could
				// finalize the file. We DON'T register those rows in the
				// segments table: the player can't load them anyway and
				// they pollute the timeline with dead-end clicks. The
				// retention sweeper handles the on-disk cleanup later.
				var videoCodec string
				durMs := r.segmentDur * 1000 // safe fallback if probe fails
				if info.ModTime().After(startedAt) {
					vc, codecErr := ProbeVideoCodec(r.ffmpegPath, filePath)
					if codecErr != nil {
						// A probe failure ("moov atom not found") is usually
						// transient: ffmpeg writes the moov atom only when it
						// rotates the segment, so a just-rotated file can be
						// scanned in the brief window before the moov lands.
						// Retry on the next scan (do NOT mark seen). Only give
						// up when the file is genuinely stale — well past the
						// finalize window (segmentDur*2) and STILL unprobeable,
						// meaning it was truncated/killed mid-write.
						if time.Since(info.ModTime()) > time.Duration(r.segmentDur*2)*time.Second {
							log.Printf("[REC] giving up on segment %s — codec probe still failing %s after last write (truncated, no moov): %v", entry.Name(), time.Since(info.ModTime()).Round(time.Second), codecErr)
							seen[entry.Name()] = true
						}
						continue
					}
					videoCodec = vc
					if dur, derr := ProbeVideoDuration(r.ffmpegPath, filePath); derr == nil && dur > 0 {
						durMs = int(dur * 1000)
					} else if derr != nil {
						log.Printf("[REC] duration probe failed for %s (using fallback %ds): %v", entry.Name(), r.segmentDur, derr)
					}
				}

				segment := &database.Segment{
					CameraID:   r.cameraID,
					StartTime:  startTime,
					EndTime:    startTime.Add(time.Duration(durMs) * time.Millisecond),
					FilePath:   filePath,
					FileSize:   info.Size(),
					DurationMs: durMs,
					HasAudio:   r.hasAudio,
					VideoCodec: videoCodec,
				}

				if err := r.db.InsertSegment(ctx, segment); err != nil {
					// Silently skip duplicates (already registered from previous run)
					if !strings.Contains(err.Error(), "duplicate") {
						log.Printf("[REC] Failed to register segment %s: %v", entry.Name(), err)
					}
					seen[entry.Name()] = true
					continue
				}

				// P1-C-03: count segments written per camera for Prometheus.
				appmetrics.IncSegmentsWritten(r.cameraID.String())

				seen[entry.Name()] = true
				// Only log new segments (created after this recorder started) to reduce noise
				if info.ModTime().After(startedAt) {
					log.Printf("[REC] Registered segment: %s (%d bytes)", entry.Name(), info.Size())
				}
			}
		}
	}
}

// parseSegmentTimestamp extracts time from filename like seg_20260219_140530.mp4
//
// P1-C-05: parses in UTC. The ffmpeg subprocess that wrote the
// filename has `TZ=UTC` set in its env (see runFFmpeg), so the
// strftime fields are UTC. Parsing the same string in time.Local
// would silently shift segment boundaries by the host's UTC offset
// — fine on UTC hosts, broken on US/Central, ambiguous across DST
// boundaries. UTC everywhere removes the whole class of bugs.
//
// Filename-format change is intentionally only forward-looking:
// pre-P1-C-05 segments were written in the host's local timezone,
// so their filenames don't carry a TZ marker and a local-parse
// would have produced their original wall-clock time. Operators
// reviewing OLD segments by filename see a 5-hour offset until the
// segments age out of retention (typically 14-30 days). That's
// acceptable — the database stores start_time/end_time as
// timestamptz which preserves the absolute instant regardless of
// what timezone the filename was parsed in.
func parseSegmentTimestamp(filename string) time.Time {
	// Expected format: seg_YYYYMMDD_HHMMSS.mp4 (19 chars before the extension)
	name := filepath.Base(filename)
	// Strip extension first so we parse exactly the stem.
	stem := strings.TrimSuffix(name, filepath.Ext(name)) // "seg_20260219_140530"
	if len(stem) < 19 {
		return time.Time{}
	}
	t, err := time.ParseInLocation("seg_20060102_150405", stem[:19], time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ringBufferCleanup periodically cleans old ring buffer segments
func (r *Recorder) ringBufferCleanup(ctx context.Context) {
	if r.clipWriter == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.clipWriter.CleanRingBuffer()
		}
	}
}

// probeStreamVideoCodec runs ffprobe on the RTSP URI and returns the
// lowercase codec_name of the first video stream ("h264", "hevc", etc.).
// Empty string on any error (network timeout, missing video stream,
// ffprobe not available). Called once per FFmpeg run before constructing
// args so we can conditionally apply `-tag:v hvc1` for HEVC sources only
// — that tag MUST NOT be applied to H.264 streams (would write an mp4
// with codec_tag=hvc1 over actual H.264 data, breaking the file).
//
// LOCAL-09: Milesight cameras emit HEVC with codec_tag `hev1` in the
// RTSP stream, which `-c:v copy` carries through to the recorded mp4.
// Safari only renders HEVC mp4s tagged `hvc1`. This probe lets us
// rewrite the tag for HEVC sources without touching H.264 ones.
func probeStreamVideoCodec(ffmpegPath, rtspURI string) string {
	if rtspURI == "" {
		return ""
	}

	ffprobePath := "ffprobe"
	if ffmpegPath != "" && ffmpegPath != "ffmpeg" {
		dir := filepath.Dir(ffmpegPath)
		ffprobePath = filepath.Join(dir, "ffprobe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// `-show_entries stream=codec_name` keeps the output tiny + parse-free:
	// the entire stdout is the codec name (one per line if multiple video
	// streams; we take the first non-empty line). Pinning `-select_streams v:0`
	// to the first video stream matches what the recorder consumes via
	// `-map 0:v:0`.
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000", // 5s in microseconds
		rtspURI)

	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// Output is the codec name (possibly with trailing newline). Lower-case
	// for stable comparison; ffprobe emits lowercase by convention but the
	// docs don't strictly guarantee it.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "" {
			return line
		}
	}
	return ""
}

// probeHasAudio runs ffprobe on the RTSP URI to detect whether an audio stream is present.
// Returns false on any error (network timeout, no audio track, etc.).
func probeHasAudio(ffmpegPath, rtspURI string) bool {
	if rtspURI == "" {
		return false
	}

	// Use the ffprobe binary (same directory as ffmpeg, or on PATH)
	ffprobePath := "ffprobe"
	if ffmpegPath != "" && ffmpegPath != "ffmpeg" {
		// Swap the binary name: e.g. /usr/bin/ffmpeg → /usr/bin/ffprobe
		dir := filepath.Dir(ffmpegPath)
		ffprobePath = filepath.Join(dir, "ffprobe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// ffprobe -v quiet -show_streams -select_streams a -rtsp_transport tcp <uri>
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "quiet",
		"-show_streams",
		"-select_streams", "a",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000", // 5s in microseconds
		rtspURI)

	out, err := cmd.Output()
	if err != nil {
		return false
	}

	// If ffprobe emitted any stream info, an audio stream was found
	return len(strings.TrimSpace(string(out))) > 0
}
