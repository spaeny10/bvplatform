package streaming

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"ironsight/internal/config"
	"ironsight/internal/database"
)

// HLSServer manages HLS live streams and playback playlist generation
type HLSServer struct {
	cfg     *config.Config
	db      *database.DB
	streams map[uuid.UUID]*LiveStream
	mu      sync.RWMutex
}

// LiveStream manages a single camera's HLS live stream (FFmpeg RTSP → HLS)
type LiveStream struct {
	cameraID     uuid.UUID
	cameraName   string
	rtspURI      string
	subStreamURI string
	hlsDir       string
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	running      bool
	mu           sync.Mutex
	ffmpegPath   string
}

// NewHLSServer creates a new HLS server
func NewHLSServer(cfg *config.Config, db *database.DB) *HLSServer {
	return &HLSServer{
		cfg:     cfg,
		db:      db,
		streams: make(map[uuid.UUID]*LiveStream),
	}
}

// StartLiveStream begins HLS conversion from RTSP for a camera
func (h *HLSServer) StartLiveStream(cameraID uuid.UUID, cameraName, rtspURI, subStreamURI string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.streams[cameraID]; ok {
		return nil // Already streaming
	}

	// Create camera-specific HLS directory
	hlsDir := filepath.Join(h.cfg.HLSPath, cameraID.String())
	if err := os.MkdirAll(hlsDir, 0755); err != nil {
		return fmt.Errorf("create HLS dir: %w", err)
	}

	stream := &LiveStream{
		cameraID:     cameraID,
		cameraName:   cameraName,
		rtspURI:      rtspURI,
		subStreamURI: subStreamURI,
		hlsDir:       hlsDir,
		ffmpegPath:   h.cfg.FFmpegPath,
	}

	if err := stream.Start(); err != nil {
		return err
	}

	h.streams[cameraID] = stream
	log.Printf("[HLS] Started live stream for %s", cameraName)
	return nil
}

// StopLiveStream stops HLS conversion for a camera
func (h *HLSServer) StopLiveStream(cameraID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if stream, ok := h.streams[cameraID]; ok {
		stream.Stop()
		delete(h.streams, cameraID)
		log.Printf("[HLS] Stopped live stream for camera %s", cameraID)
	}
}

// StopAll stops all live streams
func (h *HLSServer) StopAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, stream := range h.streams {
		stream.Stop()
		delete(h.streams, id)
	}
	log.Println("[HLS] All live streams stopped")
}

// GetLivePlaylistPath returns the HLS playlist path for a camera
func (h *HLSServer) GetLivePlaylistPath(cameraID uuid.UUID) string {
	return filepath.Join(cameraID.String(), "live.m3u8")
}

// GeneratePlaybackPlaylist creates a dynamic HLS playlist from recorded segments
func (h *HLSServer) GeneratePlaybackPlaylist(ctx context.Context, cameraID uuid.UUID, start, end time.Time) (string, error) {
	segments, err := h.db.GetSegments(ctx, cameraID, start, end)
	if err != nil {
		return "", fmt.Errorf("get segments: %w", err)
	}

	if len(segments) == 0 {
		return "", fmt.Errorf("no recordings found for the specified time range")
	}

	// Generate M3U8 playlist from segments
	playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:61\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n"

	for _, seg := range segments {
		duration := float64(seg.DurationMs) / 1000.0
		if duration <= 0 {
			duration = 60.0
		}
		// Use relative path to the segment file served via /recordings/ endpoint
		playlist += fmt.Sprintf("#EXTINF:%.3f,\n/recordings/%s/%s\n", duration, cameraID.String(), filepath.Base(seg.FilePath))
	}

	playlist += "#EXT-X-ENDLIST\n"

	return playlist, nil
}

// Start begins the FFmpeg HLS conversion
func (ls *LiveStream) Start() error {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if ls.running {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel

	go ls.streamLoop(ctx)

	ls.running = true
	return nil
}

// Stop terminates the FFmpeg HLS process
func (ls *LiveStream) Stop() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	if !ls.running {
		return
	}

	if ls.cancel != nil {
		ls.cancel()
	}

	if ls.cmd != nil && ls.cmd.Process != nil {
		ls.cmd.Process.Kill()
	}

	// Clean up HLS files
	os.RemoveAll(ls.hlsDir)
	os.MkdirAll(ls.hlsDir, 0755)

	ls.running = false
}

// streamLoop runs FFmpeg for HLS with auto-restart
func (ls *LiveStream) streamLoop(ctx context.Context) {
	retryDelay := 3 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := ls.runFFmpeg(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[HLS] FFmpeg error for %s: %v (restarting in %v)", ls.cameraName, err, retryDelay)
			time.Sleep(retryDelay)
			continue
		}
	}
}

// runFFmpeg executes FFmpeg for RTSP → HLS conversion
func (ls *LiveStream) runFFmpeg(ctx context.Context) error {
	playlistPath := filepath.Join(ls.hlsDir, "live.m3u8")

	args := []string{
		"-rtsp_transport", "tcp",
		"-i", ls.rtspURI,
	}

	if ls.subStreamURI != "" {
		args = append(args, "-rtsp_transport", "tcp", "-i", ls.subStreamURI)
	}

	if ls.subStreamURI != "" {
		args = append(args,
			"-map", "0:v:0", // Main stream video
			"-map", "1:v:0", // Sub stream video
			"-c:v", "copy",
			"-f", "hls",
			"-hls_time", "2",
			"-hls_list_size", "10",
			"-hls_flags", "delete_segments+append_list+independent_segments",
			"-master_pl_name", "live.m3u8",
			"-var_stream_map", "v:0,name:main v:1,name:sub",
			"-hls_segment_filename", filepath.Join(ls.hlsDir, "%v_seg_%03d.ts"),
			"-y",
			filepath.Join(ls.hlsDir, "%v_live.m3u8"),
		)
	} else {
		args = append(args,
			"-c:v", "copy", // No transcoding
			"-c:a", "aac", // Transcode audio to AAC for browser compatibility
			"-b:a", "128k",
			"-f", "hls",
			"-hls_time", "2", // 2-second segments for low latency
			"-hls_list_size", "10", // Keep last 10 segments in playlist
			"-hls_flags", "delete_segments+append_list",
			"-hls_segment_filename", filepath.Join(ls.hlsDir, "seg_%03d.ts"),
			"-y",
			playlistPath,
		)
	}

	ls.cmd = exec.CommandContext(ctx, ls.ffmpegPath, args...)
	ls.cmd.Stdout = nil
	ls.cmd.Stderr = nil // Discard FFmpeg stderr to prevent log flooding

	if err := ls.cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg HLS: %w", err)
	}

	return ls.cmd.Wait()
}
