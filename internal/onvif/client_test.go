package onvif

import (
	"strings"
	"testing"
)

// TestRewriteStreamHost_NATPortDerivation verifies the B-14 fix: when the
// camera sits behind a NAT and the ONVIF connect port implies an external
// RTSP port (onvifPort - 7526), the derived port is used instead of the
// camera's advertised :554.
//
// Ground truth from ffprobe-verified live BigView trailers:
//   ONVIF :8080 → RTSP :554  (offset = 7526)
//   ONVIF :8081 → RTSP :555
//   ONVIF :8082 → RTSP :556
//   ONVIF :8083 → RTSP :557
func TestRewriteStreamHost_NATPortDerivation(t *testing.T) {
	cases := []struct {
		name          string
		xAddr         string // what NewClient was dialled with (includes port)
		inputURI      string // ONVIF-reported RTSP URI (always :554 from camera)
		wantPort      string // expected external port in rewritten URI
		wantHostPart  string // expected host in rewritten URI
	}{
		{
			name:         "ONVIF:8080 → RTSP:554 (first camera slot)",
			xAddr:        "http://527.bigview.ai:8080/onvif/device_service",
			inputURI:     "rtsp://192.168.50.1:554/main",
			wantPort:     "554",
			wantHostPart: "527.bigview.ai",
		},
		{
			name:         "ONVIF:8081 → RTSP:555 (second camera slot)",
			xAddr:        "http://527.bigview.ai:8081/onvif/device_service",
			inputURI:     "rtsp://192.168.50.2:554/main",
			wantPort:     "555",
			wantHostPart: "527.bigview.ai",
		},
		{
			name:         "ONVIF:8082 → RTSP:556 (third camera slot)",
			xAddr:        "http://527.bigview.ai:8082/onvif/device_service",
			inputURI:     "rtsp://192.168.50.3:554/main",
			wantPort:     "556",
			wantHostPart: "527.bigview.ai",
		},
		{
			name:         "ONVIF:8083 → RTSP:557 (fourth camera slot)",
			xAddr:        "http://577.bigview.ai:8083/onvif/device_service",
			inputURI:     "rtsp://192.168.50.4:554/channel1/main",
			wantPort:     "557",
			wantHostPart: "577.bigview.ai",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{XAddr: tc.xAddr}
			got := c.rewriteStreamHost(tc.inputURI)
			if !strings.Contains(got, tc.wantHostPart) {
				t.Errorf("rewriteStreamHost(%q) = %q, want host %q", tc.inputURI, got, tc.wantHostPart)
			}
			if !strings.Contains(got, ":"+tc.wantPort+"/") {
				t.Errorf("rewriteStreamHost(%q) = %q, want port :%s", tc.inputURI, got, tc.wantPort)
			}
		})
	}
}

// TestRewriteStreamHost_LAN verifies that on-LAN deployments where both
// the advertised and connect addresses are private IPs, the URI is returned
// untouched — the NAT port-derivation path must not fire for LAN cameras.
func TestRewriteStreamHost_LAN(t *testing.T) {
	c := &Client{XAddr: "http://192.168.1.100:80/onvif/device_service"}
	input := "rtsp://192.168.1.100:554/main"
	got := c.rewriteStreamHost(input)
	// Both hosts are private — no rewrite should occur.
	if got != input {
		t.Errorf("rewriteStreamHost LAN: got %q, want unchanged %q", got, input)
	}
}

// TestRewriteStreamHost_PublicAlready verifies that a camera already
// reporting a public host in its RTSP URI is not touched.
func TestRewriteStreamHost_PublicAlready(t *testing.T) {
	c := &Client{XAddr: "http://203.0.113.50:8080/onvif/device_service"}
	input := "rtsp://203.0.113.50:554/main"
	got := c.rewriteStreamHost(input)
	if got != input {
		t.Errorf("rewriteStreamHost already-public: got %q, want unchanged %q", got, input)
	}
}

// TestRewriteStreamHost_NAT_PanoPath verifies that a panoramic camera's
// ONVIF-reported path (/channel1/main) is preserved through the NAT rewrite;
// the rewrite must not drop or mangle the path.
func TestRewriteStreamHost_NAT_PanoPath(t *testing.T) {
	c := &Client{XAddr: "http://577.bigview.ai:8081/onvif/device_service"}
	input := "rtsp://192.168.50.10:554/channel1/main"
	got := c.rewriteStreamHost(input)
	if !strings.Contains(got, "/channel1/main") {
		t.Errorf("rewriteStreamHost pano path: got %q — path /channel1/main not preserved", got)
	}
	if !strings.Contains(got, ":555/") {
		t.Errorf("rewriteStreamHost pano port: got %q — want port :555", got)
	}
}

// TestPortFromAddr covers the portFromAddr helper used by rewriteStreamHost.
func TestPortFromAddr(t *testing.T) {
	cases := []struct {
		addr string
		want int
	}{
		{"http://host:8082/onvif/device_service", 8082},
		{"http://host/onvif/device_service", 0},  // no port
		{"http://host:80/onvif/device_service", 80},
		{"not a url at all", 0},
		{"http://192.168.1.1:8081/", 8081},
	}
	for _, tc := range cases {
		got := portFromAddr(tc.addr)
		if got != tc.want {
			t.Errorf("portFromAddr(%q) = %d, want %d", tc.addr, got, tc.want)
		}
	}
}
