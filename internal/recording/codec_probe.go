package recording

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ProbeVideoCodec reads a video file's primary stream codec name via ffprobe.
// Returns a lowercase codec identifier matching ffprobe's stream.codec_name —
// "h264", "hevc", "av1", "vp9", "mpeg4", etc. The recorded-playback serve
// handler uses this to decide whether the segment can be served as-is or
// needs transcoding for browser compatibility.
//
// ffprobe is assumed to live next to ffmpeg in the same directory (the
// standard install layout on every platform we support); ffmpegPath is the
// resolved ffmpeg binary already plumbed through config.FFmpegPath.
//
// Errors propagate as-is from the underlying ffprobe call so the caller can
// log them. Callers that don't have an opinion on transient probe failures
// should treat an error the same as an unknown codec — i.e. defer the
// playback decision rather than crashing the recorder.
func ProbeVideoCodec(ffmpegPath, filePath string) (string, error) {
	if ffmpegPath == "" {
		return "", fmt.Errorf("ffmpeg path not configured")
	}
	if filePath == "" {
		return "", fmt.Errorf("file path empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobeBin(ffmpegPath),
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=nokey=1:noprint_wrappers=1",
		filePath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffprobe %s: %w (stderr: %s)", filePath, err, strings.TrimSpace(stderr.String()))
	}
	codec := strings.ToLower(strings.TrimSpace(stdout.String()))
	if codec == "" {
		return "", fmt.Errorf("ffprobe %s: empty codec_name", filePath)
	}
	return codec, nil
}

// ffprobeBin derives the ffprobe binary path from the ffmpeg binary path,
// preserving the platform-specific extension if any. `/usr/bin/ffmpeg` →
// `/usr/bin/ffprobe`, `C:\bin\ffmpeg.exe` → `C:\bin\ffprobe.exe`.
func ffprobeBin(ffmpegPath string) string {
	dir := filepath.Dir(ffmpegPath)
	base := filepath.Base(ffmpegPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	// If the operator passed something like "ffmpeg" with no path, base==stem
	// and dir==".". Joining "." with "ffprobe" yields "ffprobe", which exec
	// will resolve via PATH the same way "ffmpeg" did.
	switch {
	case strings.EqualFold(stem, "ffmpeg"):
		return filepath.Join(dir, "ffprobe"+ext)
	default:
		// Unusual: ffmpegPath doesn't end in "ffmpeg". Fall back to assuming
		// ffprobe lives next to whatever they configured, with the same ext.
		return filepath.Join(dir, "ffprobe"+ext)
	}
}

// H264CacheDir is the leaf-only name of the per-camera sibling directory
// holding LOCAL-11 transcode-on-demand H.264 copies of HEVC segments.
// Lives next to each camera's recording dir so the retention sweeper
// finds it alongside the source files. Leading dot keeps the recording
// engine's segment indexer from picking it up as a segment.
const H264CacheDir = ".h264-cache"

// CachedTranscodePath returns the on-disk path of the cached H.264 copy
// corresponding to a recorded segment at srcPath. Used by both the api
// package (writes/reads the cache) and the retention sweeper (deletes
// the cache alongside the source). Shared here so the two stay locked.
func CachedTranscodePath(srcPath string) string {
	return filepath.Join(filepath.Dir(srcPath), H264CacheDir, filepath.Base(srcPath))
}

// BrowserPlayableCodec reports whether a codec name (as returned by
// ProbeVideoCodec) is something modern browsers decode natively in a
// <video> element without server-side help. Used by the serve handler to
// short-circuit the transcode path for already-browser-friendly codecs.
//
// Conservative on purpose: only codecs supported by Chrome/Firefox/Safari
// natively make the cut. VP9 and AV1 are emerging but not universal in
// the contexts our operators run on, so we don't include them yet.
func BrowserPlayableCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1":
		return true
	}
	return false
}
