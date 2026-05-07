package streaming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"onvif-tool/internal/config"
)

// writeConfigForTest builds an MediaMTXServer with the given config, redirects
// its configPath into a temp dir, and returns the on-disk YAML it writes.
func writeConfigForTest(t *testing.T, cfg *config.Config) string {
	t.Helper()
	dir := t.TempDir()
	srv := NewMediaMTXServer(cfg)
	// NewMediaMTXServer derives configPath from cwd; redirect to the temp dir
	// so we don't pollute the working tree.
	srv.configPath = filepath.Join(dir, "mediamtx_runtime.yml")
	if err := srv.writeConfig(); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	b, err := os.ReadFile(srv.configPath)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	return string(b)
}

func TestWriteConfig_OmitsAdditionalHostsWhenEmpty(t *testing.T) {
	out := writeConfigForTest(t, &config.Config{
		MediaMTXAPIAddr:    "mediamtx:9997",
		MediaMTXRTSPAddr:   "mediamtx:18554",
		MediaMTXWebRTCAddr: "mediamtx:8889",
	})
	if strings.Contains(out, "webrtcAdditionalHosts") {
		t.Fatalf("config should omit webrtcAdditionalHosts when empty; got:\n%s", out)
	}
}

func TestWriteConfig_EmitsAdditionalHostsWhenSet(t *testing.T) {
	out := writeConfigForTest(t, &config.Config{
		MediaMTXAPIAddr:       "mediamtx:9997",
		MediaMTXRTSPAddr:      "mediamtx:18554",
		MediaMTXWebRTCAddr:    "mediamtx:8889",
		WebRTCAdditionalHosts: []string{"192.168.103.49", "ironsight.bigview.ai"},
	})
	if !strings.Contains(out, "webrtcAdditionalHosts:") {
		t.Fatalf("config missing webrtcAdditionalHosts key; got:\n%s", out)
	}
	if !strings.Contains(out, "192.168.103.49") {
		t.Fatalf("config missing 192.168.103.49; got:\n%s", out)
	}
	if !strings.Contains(out, "ironsight.bigview.ai") {
		t.Fatalf("config missing ironsight.bigview.ai; got:\n%s", out)
	}
}
