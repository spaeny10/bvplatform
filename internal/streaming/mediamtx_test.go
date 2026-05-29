package streaming

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ironsight/internal/config"
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

// TestWriteConfig_WebRTCDisabled verifies that P3-INFRA-06 removed WebRTC from
// the mediamtx config. webrtc must always be false regardless of what the
// Config struct carries, and webrtcAdditionalHosts must never appear.
func TestWriteConfig_WebRTCDisabled(t *testing.T) {
	out := writeConfigForTest(t, &config.Config{
		MediaMTXAPIAddr:    "mediamtx:9997",
		MediaMTXRTSPAddr:   "mediamtx:18554",
		MediaMTXWebRTCAddr: "mediamtx:8889",
	})
	if !strings.Contains(out, "webrtc: false") {
		t.Fatalf("config must have webrtc: false (P3-INFRA-06); got:\n%s", out)
	}
	if strings.Contains(out, "webrtcAdditionalHosts") {
		t.Fatalf("config must not contain webrtcAdditionalHosts (P3-INFRA-06 removed WebRTC); got:\n%s", out)
	}
}

// TestWriteConfig_WebRTCDisabledEvenWithAdditionalHosts verifies that setting
// WebRTCAdditionalHosts in Config does NOT cause them to appear in the mediamtx
// YAML. P3-INFRA-06 removed WebRTC entirely; the field is kept in Config only
// to avoid a breaking schema change in deployments that still set the env var.
func TestWriteConfig_WebRTCDisabledEvenWithAdditionalHosts(t *testing.T) {
	out := writeConfigForTest(t, &config.Config{
		MediaMTXAPIAddr:       "mediamtx:9997",
		MediaMTXRTSPAddr:      "mediamtx:18554",
		MediaMTXWebRTCAddr:    "mediamtx:8889",
		WebRTCAdditionalHosts: []string{"192.168.103.49", "ironsight.bigview.ai"},
	})
	if !strings.Contains(out, "webrtc: false") {
		t.Fatalf("config must have webrtc: false even when WebRTCAdditionalHosts is set; got:\n%s", out)
	}
	if strings.Contains(out, "webrtcAdditionalHosts") {
		t.Fatalf("config must not emit webrtcAdditionalHosts (P3-INFRA-06 removed WebRTC); got:\n%s", out)
	}
	if strings.Contains(out, "192.168.103.49") {
		t.Fatalf("config must not contain 192.168.103.49 (WebRTC is disabled); got:\n%s", out)
	}
}
