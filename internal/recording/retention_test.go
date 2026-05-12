package recording

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes content to path, creating parent dirs. Returns the
// file size. Test helper for retention scenarios that need real on-disk
// files (the helper stats them to compute "bytes freed").
func writeFile(t *testing.T, path string, content []byte) int64 {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

func TestRemoveSegmentAndCache_BothPresent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "seg_001.mp4")
	cache := CachedTranscodePath(src)

	srcSize := writeFile(t, src, []byte("sourcebytes____________"))
	cacheSize := writeFile(t, cache, []byte("cachebytes_______"))

	freed := removeSegmentAndCache(src)
	want := srcSize + cacheSize
	if freed != want {
		t.Errorf("freed = %d, want %d (src %d + cache %d)", freed, want, srcSize, cacheSize)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src not removed: stat err = %v", err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Errorf("cache not removed: stat err = %v", err)
	}
}

func TestRemoveSegmentAndCache_NoCacheFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "seg_002.mp4")
	srcSize := writeFile(t, src, []byte("sourcebytes____________"))

	freed := removeSegmentAndCache(src)
	if freed != srcSize {
		t.Errorf("freed = %d, want %d (source only)", freed, srcSize)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src not removed: stat err = %v", err)
	}
}

func TestRemoveSegmentAndCache_SourceMissing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "absent.mp4")
	// Source doesn't exist; cache does. Helper should still drop the cache
	// (the source-segments-table DELETE was already done by the caller).
	cache := CachedTranscodePath(src)
	cacheSize := writeFile(t, cache, []byte("cachebytes_______"))

	freed := removeSegmentAndCache(src)
	if freed != cacheSize {
		t.Errorf("freed = %d, want %d (cache only)", freed, cacheSize)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Errorf("cache not removed: stat err = %v", err)
	}
}

func TestRemoveSegmentAndCache_BothMissing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "never_existed.mp4")
	// Idempotent: missing-everything is a normal no-op for retention.
	if freed := removeSegmentAndCache(src); freed != 0 {
		t.Errorf("freed = %d, want 0 for missing files", freed)
	}
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
		got := CachedTranscodePath(c.in)
		if got != c.want {
			t.Errorf("CachedTranscodePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
