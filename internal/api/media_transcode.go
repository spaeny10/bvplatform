package api

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/recording"
)

// LOCAL-11: HEVC recorded-playback transcode-on-demand.
//
// BigView's trailer fleet records H.265 (HEVC) by design — the bandwidth /
// storage cost of H.264 fleet-wide would be unworkable on LTE-uplinked
// trailers. Chrome / Firefox cannot decode HEVC in a <video> element,
// however, so the recorded-playback serve handler has to bridge the gap
// without giving up the codec choice at recording time.
//
// The approach is transcode-on-demand with a disk cache:
//
//   * On every GET /media/v1/<token> for a segment kind, we look up the
//     codec.  Fast path: the source is H.264 already — serve as-is.
//   * If the source is HEVC (or any non-browser-friendly codec), we check
//     for a cached transcode at <camera-dir>/.h264-cache/<leaf>. Hit → swap
//     the served path to the cache; miss → run ffmpeg synchronously into
//     the cache, then serve.
//   * Concurrent requests for the same source path are serialised through
//     a per-path channel so we never spawn parallel ffmpeg processes for
//     the same segment.
//   * The cache file is written atomically: ffmpeg writes to a sibling
//     .tmp file and we rename into place after success. A torn cache file
//     can therefore never be observed by a reader.
//
// Storage / CPU footprint:
//
//   * Cache only fills when someone actually scrubs — in practice <1% of
//     recorded footage is ever watched, so the disk overhead is bounded.
//   * Cache entries are GC'd alongside the source segment by the existing
//     retention sweeper (extended for this purpose in session 3 of
//     LOCAL-11; see ironsight/backlog/phase-1.md).
//   * Synchronous transcode adds ~2-5 s of latency to the FIRST playback of
//     a previously-untranscoded HEVC segment. Repeat plays serve from
//     cache with no transcode cost.
//
// Cache directory naming uses a leading dot (`.h264-cache`) so the
// recorder's own segment indexer never picks the directory up as a
// segment file (see LOCAL-10 for the indexer dedup work that strengthens
// this).

// transcodeTimeout caps any single ffmpeg invocation. A 60-second segment
// on fred CPU (`libx264 -preset veryfast`) finishes in 2-5 s for the
// trailer-570 5K HEVC inputs. 90 s is well above the worst case and
// prevents a wedged process from blocking the response.
const transcodeTimeout = 90 * time.Second

// The cache directory name and path helper live in the recording package
// (recording.H264CacheDir / recording.CachedTranscodePath) so the
// retention sweeper can clean cache files without circular imports.

// transcodeRegistry serialises concurrent transcode requests for the same
// cache path. Without this, two simultaneous GETs for an un-cached HEVC
// segment would both spawn ffmpeg, race on the temp file, and (best case)
// waste CPU on duplicate work. A second caller waits on a channel until
// the first finishes; on success it sees the cache hit, on failure it
// reports the error.
type transcodeRegistry struct {
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

func newTranscodeRegistry() *transcodeRegistry {
	return &transcodeRegistry{inflight: make(map[string]chan struct{})}
}

// maybeTranscodeForBrowser decides what path to serve for a given media
// request and ensures a browser-playable file exists at that path. Three
// outcomes:
//
//   - source kind is not a recorded segment (HLS, snapshot, anything
//     non-mp4) → return srcPath unchanged
//   - source is already H.264 → return srcPath unchanged
//   - source is HEVC / non-browser-playable → return the cached H.264
//     copy path, transcoding into the cache first if needed
//
// On transcode failure returns srcPath + error; the caller should treat
// that as 500 (the source is unplayable in the browser, but we can't
// produce a working alternative either). On success the returned path is
// guaranteed to exist and be complete on disk.
//
// Concurrent calls for the same source serialise on a per-path channel
// inside the registry so parallel ffmpeg invocations don't burn fred CPU
// on duplicate work.
func (reg *transcodeRegistry) maybeTranscodeForBrowser(cfg *config.Config, claims *auth.MediaClaims, srcPath string) (string, error) {
	// Only segments need this treatment — HLS chunks are MPEG-TS (a
	// separate format altogether), and snapshots are JPEG.
	if claims.Kind != auth.MediaKindSegment {
		return srcPath, nil
	}
	if !strings.EqualFold(filepath.Ext(srcPath), ".mp4") {
		return srcPath, nil
	}

	codec, err := recording.ProbeVideoCodec(cfg.FFmpegPath, srcPath)
	if err != nil {
		// Probe failed (corrupt file, ffprobe unavailable). Best we can
		// do is hand the source path back and let the browser try. The
		// player may render it (Safari on macOS) or fail; either way
		// we've not made it worse.
		log.Printf("[MEDIA-TRANSCODE] probe failed for %s: %v", srcPath, err)
		return srcPath, nil
	}
	if recording.BrowserPlayableCodec(codec) {
		return srcPath, nil
	}

	cachePath := recording.CachedTranscodePath(srcPath)
	if info, statErr := os.Stat(cachePath); statErr == nil && !info.IsDir() && info.Size() > 0 {
		return cachePath, nil
	}

	// Cache miss — serialise concurrent transcodes for the same target.
	reg.mu.Lock()
	if waitCh, busy := reg.inflight[cachePath]; busy {
		reg.mu.Unlock()
		<-waitCh
		// Prior transcode finished. Re-check the cache: success leaves
		// a file in place, failure leaves nothing.
		if info, statErr := os.Stat(cachePath); statErr == nil && !info.IsDir() && info.Size() > 0 {
			return cachePath, nil
		}
		return srcPath, fmt.Errorf("media-transcode: prior in-flight transcode failed for %s", srcPath)
	}
	done := make(chan struct{})
	reg.inflight[cachePath] = done
	reg.mu.Unlock()
	defer func() {
		reg.mu.Lock()
		delete(reg.inflight, cachePath)
		close(done)
		reg.mu.Unlock()
	}()

	if err := transcodeToH264(cfg.FFmpegPath, srcPath, cachePath); err != nil {
		return srcPath, fmt.Errorf("media-transcode: %w", err)
	}
	return cachePath, nil
}

// transcodeToH264 produces a browser-playable mp4 at destPath from
// srcPath. Strategy:
//
//   * libx264 video at CRF 23 with the veryfast preset / fastdecode tune.
//     Roughly 1.5-2× the H.265 file size at equivalent visual quality.
//     CPU budget: ~3 s for a 60 s 5K segment on a fred core.
//   * Audio re-encoded to AAC. Cheap and side-steps codec compatibility
//     gotchas — `-c:a copy` would propagate PCM mulaw from some Milesight
//     models, which mp4 muxers refuse.
//   * +faststart relocates the moov atom to the head of the file so the
//     browser can begin playback / range-seek before the full file
//     downloads. Essential for the <video> scrub UX.
//   * Output goes to a sibling .tmp file and is renamed into place only
//     on full success. A torn temp file is removed in the deferred
//     cleanup so a future call retries cleanly rather than serving
//     half-bytes.
func transcodeToH264(ffmpegPath, srcPath, destPath string) error {
	if ffmpegPath == "" {
		return fmt.Errorf("ffmpeg path not configured")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir cache dir: %w", err)
	}
	tmp := destPath + ".tmp"
	_ = os.Remove(tmp) // wipe any leftover from a previous crashed attempt

	ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "warning",
		"-i", srcPath,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-tune", "fastdecode",
		"-crf", "23",
		"-c:a", "aac",
		"-movflags", "+faststart",
		// Pass the source's frame timestamps through unchanged. Milesight
		// cameras advertise a nominal 60 fps in their RTSP SDP but only
		// deliver ~9-10 fps in practice. Without this, ffmpeg's default
		// CFR output duplicates each real frame ~6× to hit the nominal
		// rate — playback looks chunky / "distorted" because every real
		// motion frame is held for 6 output frames then jumps. With
		// passthrough the H.264 output matches the source's actual frame
		// cadence and motion is smooth. Cost: ~2× larger cache file (the
		// encoder can no longer cheat with tiny P-frames for duplicates)
		// — acceptable given the cache only fills on demand.
		"-fps_mode", "passthrough",
		// ffmpeg picks output format from the filename extension; the
		// .tmp suffix on our temp file confuses it ("Unable to find a
		// suitable output format"), so force the muxer explicitly.
		"-f", "mp4",
		"-y",
		tmp,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg %s → %s: %w (stderr: %s)",
			srcPath, destPath, err, strings.TrimSpace(stderr.String()))
	}
	if err := os.Rename(tmp, destPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, destPath, err)
	}
	return nil
}
