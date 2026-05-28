package recording

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// resolveFfmpeg finds an ffmpeg binary on PATH or skips the test cleanly.
// The recording package is meaningless without ffmpeg installed; CI must
// have it but local dev shells (Windows) might not.
func resolveFfmpeg(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH; skipping codec_probe tests")
	}
	return p
}

// genFixture writes a 1-second test video using the named codec into a
// temp dir and returns the path. Uses ffmpeg's lavfi testsrc generator so
// the test has no external file dependencies.
func genFixture(t *testing.T, ffmpegPath, codecArg string) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "fixture.mp4")
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

func TestProbeVideoCodec_H264(t *testing.T) {
	ffmpeg := resolveFfmpeg(t)
	path := genFixture(t, ffmpeg, "libx264")
	codec, err := ProbeVideoCodec(ffmpeg, path)
	if err != nil {
		t.Fatalf("ProbeVideoCodec: %v", err)
	}
	if codec != "h264" {
		t.Fatalf("want h264, got %q", codec)
	}
}

func TestProbeVideoCodec_HEVC(t *testing.T) {
	ffmpeg := resolveFfmpeg(t)
	// libx265 is built into most ffmpeg packages but not all. Skip if
	// this particular ffmpeg can't encode HEVC — the test isn't asserting
	// build-system shape, just probe correctness when HEVC is available.
	probe := exec.Command(ffmpeg, "-hide_banner", "-encoders")
	out, err := probe.CombinedOutput()
	if err != nil || !containsWord(string(out), "libx265") {
		t.Skip("ffmpeg lacks libx265; skipping HEVC probe test")
	}
	path := genFixture(t, ffmpeg, "libx265")
	codec, err := ProbeVideoCodec(ffmpeg, path)
	if err != nil {
		t.Fatalf("ProbeVideoCodec: %v", err)
	}
	if codec != "hevc" {
		t.Fatalf("want hevc, got %q", codec)
	}
}

func TestProbeVideoCodec_RejectsEmptyArgs(t *testing.T) {
	if _, err := ProbeVideoCodec("", "anything"); err == nil {
		t.Fatal("expected error for empty ffmpegPath")
	}
	if _, err := ProbeVideoCodec("/usr/bin/ffmpeg", ""); err == nil {
		t.Fatal("expected error for empty filePath")
	}
}

func TestProbeVideoCodec_MissingFile(t *testing.T) {
	ffmpeg := resolveFfmpeg(t)
	_, err := ProbeVideoCodec(ffmpeg, filepath.Join(t.TempDir(), "nope.mp4"))
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestBrowserPlayableCodec(t *testing.T) {
	playable := []string{"h264", "H264", "avc1", " avc "}
	for _, c := range playable {
		if !BrowserPlayableCodec(c) {
			t.Errorf("BrowserPlayableCodec(%q) = false, want true", c)
		}
	}
	notPlayable := []string{"hevc", "h265", "vp9", "av1", "mpeg4", "", "unknown"}
	for _, c := range notPlayable {
		if BrowserPlayableCodec(c) {
			t.Errorf("BrowserPlayableCodec(%q) = true, want false", c)
		}
	}
}

func TestFfprobeBin(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/usr/bin/ffmpeg", filepath.Join("/usr/bin", "ffprobe")},
		{"ffmpeg", "ffprobe"},
		{filepath.FromSlash("C:/bin/ffmpeg.exe"), filepath.Join(filepath.FromSlash("C:/bin"), "ffprobe.exe")},
	}
	for _, c := range cases {
		got := ffprobeBin(c.in)
		if got != c.want {
			t.Errorf("ffprobeBin(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// containsWord reports whether s contains target as a whole word (so
// "libx265" matches but "libx265dec" wouldn't). Cheap stand-in for a
// regexp; only used by the HEVC support probe.
func containsWord(s, target string) bool {
	for i := 0; i+len(target) <= len(s); i++ {
		if s[i:i+len(target)] != target {
			continue
		}
		// Boundary check on either side.
		if i > 0 {
			c := s[i-1]
			if isWordChar(c) {
				continue
			}
		}
		if i+len(target) < len(s) {
			c := s[i+len(target)]
			if isWordChar(c) {
				continue
			}
		}
		return true
	}
	return false
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
