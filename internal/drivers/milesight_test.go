package drivers

import "testing"

// TestNormalizeRTSPURI_B14_PortGuard verifies the B-14 narrowing of the
// sub-1024 port guard: only port 0 and port 1 are treated as firmware bugs
// and collapsed to 554. Ports in the BigView NAT convention range (554–558)
// and other legitimate ports must NOT be overwritten.
func TestNormalizeRTSPURI_B14_PortGuard(t *testing.T) {
	drv := &MilesightDriver{}

	cases := []struct {
		name     string
		input    string
		wantPort string // expected port in the output URI
	}{
		// These are the NAT convention ports — must be PRESERVED.
		{
			name:     "NAT port 554 preserved",
			input:    "rtsp://admin:pass@527.bigview.ai:554/main",
			wantPort: "554",
		},
		{
			name:     "NAT port 555 preserved (slot 2)",
			input:    "rtsp://admin:pass@527.bigview.ai:555/main",
			wantPort: "555",
		},
		{
			name:     "NAT port 556 preserved (slot 3)",
			input:    "rtsp://admin:pass@527.bigview.ai:556/main",
			wantPort: "556",
		},
		{
			name:     "NAT port 557 preserved (slot 4)",
			input:    "rtsp://admin:pass@577.bigview.ai:557/channel1/main",
			wantPort: "557",
		},
		// Legitimate HTTP/HTTPS tunneling ports must be preserved.
		{
			name:     "HTTP tunneling port 80 preserved",
			input:    "rtsp://admin:pass@192.168.1.100:80/main",
			wantPort: "80",
		},
		{
			name:     "HTTPS tunneling port 443 preserved",
			input:    "rtsp://admin:pass@192.168.1.100:443/main",
			wantPort: "443",
		},
		// Missing port defaults to :554.
		{
			name:     "no port defaults to 554",
			input:    "rtsp://admin:pass@192.168.1.100/main",
			wantPort: "554",
		},
		// The real firmware bug values — must be fixed.
		{
			name:     "port 0 is firmware bug — fix to 554",
			input:    "rtsp://admin:pass@192.168.1.100:0/main",
			wantPort: "554",
		},
		{
			name:     "port 1 is firmware bug — fix to 554",
			input:    "rtsp://admin:pass@192.168.1.100:1/main",
			wantPort: "554",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := drv.NormalizeRTSPURI(tc.input, "admin", "pass")
			p := portSubstring(got)
			if p != tc.wantPort {
				t.Errorf("NormalizeRTSPURI(%q) port = %q, want %q (full result: %q)", tc.input, p, tc.wantPort, got)
			}
		})
	}
}

// portSubstring extracts the port from an RTSP URI for test assertions.
// rtsp://user:pass@host:PORT/path → "PORT"
func portSubstring(uri string) string {
	// Strip scheme.
	s := uri
	if len(s) > 7 && s[:7] == "rtsp://" {
		s = s[7:]
	}
	// Strip credentials.
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			s = s[i+1:]
			break
		}
	}
	// Find port after last colon before slash.
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			s = s[:i]
			break
		}
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return s[i+1:]
		}
	}
	return ""
}
