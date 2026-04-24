package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
)

// Engine manages FFmpeg recording processes for all cameras
type Engine struct {
	cfg            *config.Config
	db             *database.DB
	recorders      map[uuid.UUID]*Recorder
	gortRecorders  map[uuid.UUID]*GortRecorder // pure-Go recorders (opt-in per camera)
	mu             sync.RWMutex
}

// Recorder manages a single camera's FFmpeg recording process
type Recorder struct {
	cameraID     uuid.UUID
	cameraName   string
	rtspURI      string
	subStreamURI string
	outputDir    string
	hlsDir       string
	segmentDur   int
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	running      bool
	mu           sync.Mutex
	db           *database.DB
	ffmpegPath   string
	hasAudio     bool // set once at startup via ffprobe

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
// recorder instead of FFmpeg. Set via env var GORT_CAMERAS as a comma-separated
// list of full UUIDs or 8-char prefixes. Example:
//
//	GORT_CAMERAS=9ca4bcfd,6884a556
func useGortRecorder(cameraID uuid.UUID) bool {
	env := os.Getenv("GORT_CAMERAS")
	if env == "" {
		return false
	}
	id := cameraID.String()
	for _, raw := range strings.Split(env, ",") {
		tok := strings.TrimSpace(raw)
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
	if useGortRecorder(cameraID) {
		gr := NewGortRecorder(cameraID, cameraName, rtspURI, outputDir, e.db, time.Duration(e.cfg.SegmentDuration)*time.Second)
		if err := gr.Start(); err != nil {
			return err
		}
		e.gortRecorders[cameraID] = gr
		log.Printf("[REC] Camera %s using Go recorder (gortsplib)", cameraName)
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
		cameraID:      cameraID,
		cameraName:    cameraName,
		rtspURI:       rtspURI,
		subStreamURI:  subStreamURI,
		outputDir:     outputDir,
		hlsDir:        filepath.Join(e.cfg.HLSPath, cameraID.String()),
		segmentDur:    e.cfg.SegmentDuration,
		db:            e.db,
		ffmpegPath:    e.cfg.FFmpegPath,
		recordingMode: mode,
		schedule:      schedule,
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
		return nil
	}
	recorder, ok := e.recorders[cameraID]
	if !ok {
		return fmt.Errorf("no active recording for camera %s", cameraID)
	}

	recorder.Stop()
	delete(e.recorders, cameraID)
	log.Printf("[REC] Stopped recording for camera %s", cameraID)
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
				return // context cancelled, expected shutdown
			}
			// Detect fast crashes (< 5s) — likely bandwidth/connection errors.
			// After 3 fast crashes, drop sub stream to reduce RTSP connections.
			if time.Since(startTime) < 5*time.Second {
				fastCrashCount++
				if fastCrashCount >= 3 && r.subStreamURI != "" {
					log.Printf("[REC] %s: %d fast crashes — dropping sub stream (bandwidth limit?)", r.cameraName, fastCrashCount)
					r.subStreamURI = ""
					fastCrashCount = 0
				}
			} else {
				fastCrashCount = 0
			}
			// Slow-crash mitigation: if we've crashed ≥3 times in the last 15 minutes
			// (regardless of how long each run lasted), drop the sub stream. Cellular
			// cameras stay up for several minutes and then die from CSeq desync —
			// each RTSP connection we hold open is another chance for the session to
			// corrupt. One stream is more stable than two.
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
			if len(recentCrashes) >= 3 && r.subStreamURI != "" {
				log.Printf("[REC] %s: %d crashes in last 15min — dropping sub stream (cellular/flaky link?)", r.cameraName, len(recentCrashes))
				r.subStreamURI = ""
				recentCrashes = nil
			}
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

// useLocalRelay checks if the MediaMTX local RTSP relay is available by
// probing the TCP port. Returns true if the relay is reachable.
func (r *Recorder) useLocalRelay(_ context.Context, relayURI string) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:18554", 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	_ = relayURI
	return true
}

// runFFmpeg executes the FFmpeg process for segment-based recording
func (r *Recorder) runFFmpeg(ctx context.Context) error {
	os.MkdirAll(r.hlsDir, 0755)

	// Prefer local MediaMTX relay (rtsp://127.0.0.1:18554/<cameraID>) to avoid
	// consuming an RTSP slot on the camera. Bandwidth-limited cameras (cellular/5G)
	// cannot handle parallel RTSP connections from MediaMTX + recording + HLS.
	inputURI := r.rtspURI
	localRelay := fmt.Sprintf("rtsp://127.0.0.1:18554/%s", r.cameraID.String())
	if r.useLocalRelay(ctx, localRelay) {
		inputURI = localRelay
		log.Printf("[REC] %s: using MediaMTX local relay for recording", r.cameraName)
	}

	args := []string{
		"-fflags", "+nobuffer+genpts",
		"-flags", "low_delay",
		"-probesize", "256000",
		"-analyzeduration", "500000",
		"-rtsp_transport", "tcp",
		"-i", inputURI,
	}

	if r.subStreamURI != "" {
		subRelay := fmt.Sprintf("rtsp://127.0.0.1:18554/%s_sub", r.cameraID.String())
		if r.useLocalRelay(ctx, subRelay) {
			args = append(args, "-rtsp_transport", "tcp", "-i", subRelay)
		} else {
			args = append(args, "-rtsp_transport", "tcp", "-i", r.subStreamURI)
		}
	}

	// 1) HLS live stream output
	if r.subStreamURI != "" {
		// Dual stream HLS
		args = append(args,
			"-map", "0:v:0", // Main stream video
			"-map", "1:v:0", // Sub stream video
			"-c:v", "copy",
			"-f", "hls",
			"-hls_time", "1",
			"-hls_list_size", "5",
			"-hls_flags", "delete_segments+append_list+independent_segments",
			"-var_stream_map", "v:0,name:main v:1,name:sub",
			"-hls_segment_filename", filepath.Join(r.hlsDir, "%v_seg_%03d.ts"),
			"-y",
			filepath.Join(r.hlsDir, "%v_live.m3u8"),
		)
	} else {
		// Single stream HLS
		args = append(args,
			"-map", "0:v:0",
			"-c:v", "copy",
			"-f", "hls",
			"-hls_time", "1",
			"-hls_list_size", "5",
			"-hls_flags", "delete_segments+append_list",
			"-hls_segment_filename", filepath.Join(r.hlsDir, "seg_%03d.ts"),
			"-y",
			filepath.Join(r.hlsDir, "live.m3u8"),
		)
	}

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

	r.cmd = exec.CommandContext(ctx, r.ffmpegPath, args...)
	r.cmd.Stdout = nil
	// Capture FFmpeg stderr to a per-camera file (truncated each run) so we can
	// surface the full startup output on crash. This replaces the discarded
	// stderr that was masking EINVAL failures.
	stderrPath := filepath.Join(r.outputDir, "ffmpeg_stderr.log")
	if f, ferr := os.Create(stderrPath); ferr == nil {
		r.cmd.Stderr = f
		r.stderrPath = stderrPath
		// Close the file when FFmpeg exits.
		go func() {
			<-runCtx.Done()
			_ = f.Close()
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
	if err := r.cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// Start segment watcher (scoped to this FFmpeg run via runCtx)
	go r.watchSegments(runCtx)

	// Stall watchdog: kill FFmpeg if no segment file grows for 2× segment duration.
	// Cellular streams can drop silently without closing the TCP connection, causing
	// cmd.Wait() to block indefinitely. This detects the stall and forces a restart.
	go func() {
		stallTimeout := time.Duration(r.segmentDur*2+60) * time.Second
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var lastTotalSize int64

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				entries, err := os.ReadDir(r.outputDir)
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
				if !latestMod.IsZero() && time.Since(latestMod) > stallTimeout {
					log.Printf("[REC] Stream stall detected for %s (no segment growth in %v) — killing FFmpeg", r.cameraName, stallTimeout)
					if r.cmd != nil && r.cmd.Process != nil {
						r.cmd.Process.Kill()
					}
					return
				}
			}
		}
	}()

	return r.cmd.Wait()
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

			for _, entry := range entries {
				if entry.IsDir() || seen[entry.Name()] {
					continue
				}

				filePath := filepath.Join(r.outputDir, entry.Name())
				info, err := entry.Info()
				if err != nil || info.Size() < 1000 {
					continue // Skip tiny/incomplete files
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

				segment := &database.Segment{
					CameraID:   r.cameraID,
					StartTime:  startTime,
					EndTime:    startTime.Add(time.Duration(r.segmentDur) * time.Second),
					FilePath:   filePath,
					FileSize:   info.Size(),
					DurationMs: r.segmentDur * 1000,
					HasAudio:   r.hasAudio,
				}

				if err := r.db.InsertSegment(ctx, segment); err != nil {
					// Silently skip duplicates (already registered from previous run)
					if !strings.Contains(err.Error(), "duplicate") {
						log.Printf("[REC] Failed to register segment %s: %v", entry.Name(), err)
					}
					seen[entry.Name()] = true
					continue
				}

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
func parseSegmentTimestamp(filename string) time.Time {
	// Expected format: seg_YYYYMMDD_HHMMSS.mp4 (19 chars before the extension)
	name := filepath.Base(filename)
	// Strip extension first so we parse exactly the stem.
	stem := strings.TrimSuffix(name, filepath.Ext(name)) // "seg_20260219_140530"
	if len(stem) < 19 {
		return time.Time{}
	}
	// Extract date/time parts using the system's local timezone (since FFmpeg strftime uses local time)
	t, err := time.ParseInLocation("seg_20060102_150405", stem[:19], time.Local)
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
