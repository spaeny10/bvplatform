package onvif

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sync"
)

// BackchannelPlayer streams audio files to an ONVIF speaker via FFmpeg RTSP push.
type BackchannelPlayer struct {
	mu        sync.Mutex
	activeCmd *exec.Cmd
	activeCam string // speaker ID currently playing
}

// NewBackchannelPlayer creates a new player instance
func NewBackchannelPlayer() *BackchannelPlayer {
	return &BackchannelPlayer{}
}

// Play streams an audio file to the speaker's RTSP backchannel URI.
// The audio is transcoded to G.711 µ-law (the most common ONVIF speaker codec).
// If a message is already playing, it is stopped first.
func (bp *BackchannelPlayer) Play(ctx context.Context, ffmpegPath, audioFilePath, rtspUri, username, password string) error {
	bp.Stop()

	bp.mu.Lock()
	defer bp.mu.Unlock()

	// Build authenticated RTSP URI
	authRTSP := rtspUri
	if username != "" {
		// Insert credentials into rtsp://host:port/path
		// rtsp://user:pass@host:port/path
		if len(rtspUri) > 7 { // "rtsp://"
			authRTSP = fmt.Sprintf("rtsp://%s:%s@%s", username, password, rtspUri[7:])
		}
	}

	// FFmpeg command:
	// -re: read at native frame rate (real-time playback)
	// -i: input audio file
	// -c:a pcm_mulaw: G.711 µ-law (widely supported by ONVIF speakers)
	// -ar 8000: 8kHz sample rate (telephony standard)
	// -ac 1: mono
	// -f rtsp: output format
	// -rtsp_transport tcp: use TCP for reliable delivery
	args := []string{
		"-re",
		"-i", audioFilePath,
		"-c:a", "pcm_mulaw",
		"-ar", "8000",
		"-ac", "1",
		"-f", "rtsp",
		"-rtsp_transport", "tcp",
		authRTSP,
	}

	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	bp.activeCmd = cmd

	log.Printf("[SPEAKER] Playing %s to %s", audioFilePath, rtspUri)

	// Run async — the command will finish when the audio file ends
	go func() {
		output, err := cmd.CombinedOutput()
		bp.mu.Lock()
		bp.activeCmd = nil
		bp.mu.Unlock()
		if err != nil {
			log.Printf("[SPEAKER] Playback error: %v\nOutput: %s", err, string(output))
		} else {
			log.Printf("[SPEAKER] Playback complete for %s", audioFilePath)
		}
	}()

	return nil
}

// Stop terminates any currently playing audio
func (bp *BackchannelPlayer) Stop() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.activeCmd != nil && bp.activeCmd.Process != nil {
		log.Printf("[SPEAKER] Stopping active playback")
		bp.activeCmd.Process.Kill()
		bp.activeCmd = nil
	}
}

// IsPlaying returns true if audio is currently being streamed
func (bp *BackchannelPlayer) IsPlaying() bool {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.activeCmd != nil
}

// GetAudioOutputs queries the ONVIF device for audio output capabilities.
// This discovers backchannel URIs for speakers that support ONVIF Profile T.
func (c *Client) GetAudioOutputs(ctx context.Context) (string, error) {
	// Try to get a media profile with audio output configuration
	profiles, err := c.GetProfiles(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get profiles: %w", err)
	}

	// Use the first profile's stream URI as the backchannel URI base
	// Most ONVIF speakers use the same URI for backchannel with a specific path
	if len(profiles) > 0 {
		uri, err := c.GetStreamURI(ctx, profiles[0].Token)
		if err != nil {
			return "", fmt.Errorf("failed to get stream URI: %w", err)
		}
		return uri, nil
	}

	return "", fmt.Errorf("no profiles found on device")
}
