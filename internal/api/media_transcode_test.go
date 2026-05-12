package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"ironsight/internal/auth"
	"ironsight/internal/config"
	"ironsight/internal/recording"
)

// genFixtureMP4 writes a 1-second test video at the requested codec into a
// temp dir and returns the path. The api package can't import the
// recording package's test helper (Go tests don't expose helpers across
// packages), so we duplicate the minimum here. lavfi testsrc is used so
// no external file dependency is required.
func genFixtureMP4(t *testing.T, ffmpegPath, codecArg string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "fixture.mp4")
	cmd := exec.Command(ffmpegPath,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi",
		"-i", "testsrc=duration=1:size=320x240:rate=10",
		"-c:v", codecArg,
		"-pix_fmt", "yuv420p",
		"-y",
		out,
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate fixture (%s): %v", codecArg, err)
	}
	return out
}

func ffmpegOrSkip(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH; skipping transcode tests")
	}
	return p
}

func hasEncoder(t *testing.T, ffmpegPath, encoder string) bool {
	t.Helper()
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil {
		return false
	}
	// Crude word-boundary check sufficient for ffmpeg's encoder listing.
	s := string(out)
	for i := 0; i+len(encoder) <= len(s); i++ {
		if s[i:i+len(encoder)] != encoder {
			continue
		}
		ok := true
		if i > 0 && isAlnum(s[i-1]) {
			ok = false
		}
		if i+len(encoder) < len(s) && isAlnum(s[i+len(encoder)]) {
			ok = false
		}
		if ok {
			return true
		}
	}
	return false
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func TestCachedTranscodePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			in:   filepath.FromSlash("/data/recordings/abc/seg_001.mp4"),
			want: filepath.FromSlash("/data/recordings/abc/.h264-cache/seg_001.mp4"),
		},
		{
			in:   "seg.mp4",
			want: filepath.Join(".h264-cache", "seg.mp4"),
		},
	}
	for _, c := range cases {
		got := recording.CachedTranscodePath(c.in)
		if got != c.want {
			t.Errorf("CachedTranscodePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTranscodeToH264_HEVCInput(t *testing.T) {
	ffmpeg := ffmpegOrSkip(t)
	if !hasEncoder(t, ffmpeg, "libx265") {
		t.Skip("ffmpeg lacks libx265; skipping HEVC transcode test")
	}
	src := genFixtureMP4(t, ffmpeg, "libx265")
	dst := filepath.Join(t.TempDir(), ".h264-cache", "out.mp4")

	if err := transcodeToH264(ffmpeg, src, dst); err != nil {
		t.Fatalf("transcodeToH264: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output empty")
	}
	// Verify it's actually H.264 now via a probe in the recording package.
	// We import only api here, so check by ffprobe directly.
	out, err := exec.Command(ffprobeFromFfmpeg(ffmpeg),
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=nokey=1:noprint_wrappers=1",
		dst,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe transcoded file: %v", err)
	}
	if codec := trim(string(out)); codec != "h264" {
		t.Fatalf("transcoded codec = %q, want h264", codec)
	}
	// Tmp file should have been cleaned up.
	if _, err := os.Stat(dst + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp not cleaned up: stat err=%v", err)
	}
}

func ffprobeFromFfmpeg(ffmpeg string) string {
	dir := filepath.Dir(ffmpeg)
	base := filepath.Base(ffmpeg)
	ext := filepath.Ext(base)
	return filepath.Join(dir, "ffprobe"+ext)
}

func trim(s string) string {
	// strings.TrimSpace without the import — tests already have many
	// helpers and the dep would be the only use.
	out := []byte(s)
	for len(out) > 0 && (out[0] == ' ' || out[0] == '\n' || out[0] == '\r' || out[0] == '\t') {
		out = out[1:]
	}
	for len(out) > 0 && (out[len(out)-1] == ' ' || out[len(out)-1] == '\n' || out[len(out)-1] == '\r' || out[len(out)-1] == '\t') {
		out = out[:len(out)-1]
	}
	return string(out)
}

func TestTranscodeToH264_RejectsEmptyFfmpegPath(t *testing.T) {
	if err := transcodeToH264("", "src.mp4", "dst.mp4"); err == nil {
		t.Fatal("want error for empty ffmpegPath")
	}
}

func TestMaybeTranscode_PassThroughForH264(t *testing.T) {
	ffmpeg := ffmpegOrSkip(t)
	src := genFixtureMP4(t, ffmpeg, "libx264")
	reg := newTranscodeRegistry()
	cfg := &config.Config{FFmpegPath: ffmpeg}
	claims := &auth.MediaClaims{Kind: auth.MediaKindSegment, Path: filepath.Base(src)}

	got, err := reg.maybeTranscodeForBrowser(cfg, claims, src)
	if err != nil {
		t.Fatalf("maybeTranscodeForBrowser: %v", err)
	}
	if got != src {
		t.Fatalf("H.264 source should pass through; got %q, want %q", got, src)
	}
	// No cache file should have been created.
	if _, err := os.Stat(recording.CachedTranscodePath(src)); !os.IsNotExist(err) {
		t.Errorf("unexpected cache file at %s", recording.CachedTranscodePath(src))
	}
}

func TestMaybeTranscode_TranscodesHEVC(t *testing.T) {
	ffmpeg := ffmpegOrSkip(t)
	if !hasEncoder(t, ffmpeg, "libx265") {
		t.Skip("ffmpeg lacks libx265; skipping HEVC transcode test")
	}
	src := genFixtureMP4(t, ffmpeg, "libx265")
	reg := newTranscodeRegistry()
	cfg := &config.Config{FFmpegPath: ffmpeg}
	claims := &auth.MediaClaims{Kind: auth.MediaKindSegment, Path: filepath.Base(src)}

	got, err := reg.maybeTranscodeForBrowser(cfg, claims, src)
	if err != nil {
		t.Fatalf("maybeTranscodeForBrowser: %v", err)
	}
	want := recording.CachedTranscodePath(src)
	if got != want {
		t.Fatalf("HEVC source should return cache path; got %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("cache file missing after transcode: %v", err)
	}

	// Second call should hit the cache, not retranscode. We can't directly
	// observe "ffmpeg was not called" but we can verify the mtime is stable.
	info1, _ := os.Stat(want)
	got2, err := reg.maybeTranscodeForBrowser(cfg, claims, src)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got2 != want {
		t.Fatalf("second call returned %q, want %q", got2, want)
	}
	info2, _ := os.Stat(want)
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Errorf("cache file was rewritten on second call (mtime changed): %v → %v", info1.ModTime(), info2.ModTime())
	}
}

func TestMaybeTranscode_PassThroughForNonSegment(t *testing.T) {
	ffmpeg := ffmpegOrSkip(t)
	src := genFixtureMP4(t, ffmpeg, "libx264")
	reg := newTranscodeRegistry()
	cfg := &config.Config{FFmpegPath: ffmpeg}
	for _, kind := range []auth.MediaKind{auth.MediaKindHLS, auth.MediaKindSnapshot} {
		claims := &auth.MediaClaims{Kind: kind, Path: filepath.Base(src)}
		got, err := reg.maybeTranscodeForBrowser(cfg, claims, src)
		if err != nil {
			t.Fatalf("kind=%s: %v", kind, err)
		}
		if got != src {
			t.Errorf("kind=%s: should pass through; got %q, want %q", kind, got, src)
		}
	}
}

// TestMaybeTranscode_ConcurrentDedup verifies that two simultaneous calls
// for the same HEVC source only run ffmpeg once — the second caller waits
// for the first and then sees the cache hit.
func TestMaybeTranscode_ConcurrentDedup(t *testing.T) {
	ffmpeg := ffmpegOrSkip(t)
	if !hasEncoder(t, ffmpeg, "libx265") {
		t.Skip("ffmpeg lacks libx265; skipping HEVC concurrent test")
	}
	src := genFixtureMP4(t, ffmpeg, "libx265")
	reg := newTranscodeRegistry()
	cfg := &config.Config{FFmpegPath: ffmpeg}
	claims := &auth.MediaClaims{Kind: auth.MediaKindSegment, Path: filepath.Base(src)}

	const N = 4
	var wg sync.WaitGroup
	var errs atomic.Int32
	results := make([]string, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			got, err := reg.maybeTranscodeForBrowser(cfg, claims, src)
			if err != nil {
				errs.Add(1)
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			results[idx] = got
		}(i)
	}
	wg.Wait()
	if errs.Load() > 0 {
		t.Fatalf("%d goroutines reported errors", errs.Load())
	}
	for i, r := range results {
		if r != recording.CachedTranscodePath(src) {
			t.Errorf("result[%d] = %q, want %q", i, r, recording.CachedTranscodePath(src))
		}
	}
	// After all dust settles, the registry should be empty.
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.inflight) != 0 {
		t.Errorf("inflight map not empty after concurrent calls: %d entries", len(reg.inflight))
	}
}
