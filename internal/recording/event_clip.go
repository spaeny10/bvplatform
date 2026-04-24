package recording

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CaptureEventClip grabs a short MP4 clip from an RTSP stream.
// Returns the relative URL path to the clip file (serveable via /recordings/).
// Duration is in seconds (typically 10-15s).
func CaptureEventClip(ffmpegPath, storagePath, cameraID, rtspURI string, durationSec int) (string, error) {
	if rtspURI == "" {
		return "", fmt.Errorf("no RTSP URI")
	}

	// Create clips directory
	clipsDir := filepath.Join(storagePath, cameraID, "clips")
	os.MkdirAll(clipsDir, 0755)

	// Generate filename with timestamp
	ts := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("event_%s.mp4", ts)
	outputPath := filepath.Join(clipsDir, filename)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(durationSec+10)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-rtsp_transport", "tcp",
		"-i", rtspURI,
		"-t", fmt.Sprintf("%d", durationSec),
		"-c:v", "copy",      // no re-encoding — fast
		"-an",                // skip audio for speed
		"-movflags", "+faststart", // web-playable MP4
		"-y",                 // overwrite
		outputPath,
	)
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg clip capture failed: %w", err)
	}

	// Verify file was created and has content
	info, err := os.Stat(outputPath)
	if err != nil || info.Size() == 0 {
		return "", fmt.Errorf("clip file empty or missing")
	}

	log.Printf("[CLIP] Captured %ds event clip for camera %s (%d bytes): %s",
		durationSec, cameraID, info.Size(), filename)

	// Return the URL path that the HTTP server can serve
	return fmt.Sprintf("/recordings/%s/clips/%s", cameraID, filename), nil
}
