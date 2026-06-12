package recording

import (
	"strings"
	"testing"
)

// TestRTSPCandidateURIs_BigViewNATConvention verifies the port-derivation
// math for the BigView multi-camera trailer NAT pattern:
//   ONVIF_port - 7526 = external_RTSP_port
//   (8080 → 554, 8081 → 555, 8082 → 556, 8083 → 557, …)
//
// The convention port must appear FIRST in the candidate list — that's the
// entire point of having it: we want to probe the right port before falling
// back to the camera's bogus self-reported :554.
func TestRTSPCandidateURIs_BigViewNATConvention(t *testing.T) {
	cases := []struct {
		name          string
		originalURI   string
		onvifAddress  string
		wantFirstPort string // must be the FIRST candidate's port
	}{
		{
			name:          "ONVIF:8082 → RTSP:556 first (third slot)",
			originalURI:   "rtsp://admin:pw@527.bigview.ai:554/main",
			onvifAddress:  "527.bigview.ai:8082",
			wantFirstPort: "556",
		},
		{
			name:          "ONVIF:8083 → RTSP:557 first (fourth slot)",
			originalURI:   "rtsp://admin:pw@577.bigview.ai:554/channel1/main",
			onvifAddress:  "577.bigview.ai:8083",
			wantFirstPort: "557",
		},
		{
			name:          "ONVIF:8081 → RTSP:555 first (second slot)",
			originalURI:   "rtsp://admin:pw@5001.bigview.ai:554/main",
			onvifAddress:  "5001.bigview.ai:8081",
			wantFirstPort: "555",
		},
		{
			name:          "ONVIF:8080 → RTSP:554 first (first slot, convention == default)",
			originalURI:   "rtsp://admin:pw@527.bigview.ai:554/main",
			onvifAddress:  "527.bigview.ai:8080",
			wantFirstPort: "554",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidates := RTSPCandidateURIs(tc.originalURI, tc.onvifAddress, "main")
			if len(candidates) == 0 {
				t.Fatal("RTSPCandidateURIs returned empty slice")
			}
			first := candidates[0]
			// Extract the port from the first candidate.
			gotPort := portOf(first, 0)
			wantPort := portOf("scheme://host:"+tc.wantFirstPort+"/", 0)
			if gotPort != wantPort {
				t.Errorf("first candidate = %q, port = %d; want port %s (derived from onvif port)",
					first, gotPort, tc.wantFirstPort)
			}
		})
	}
}

// TestRTSPCandidateURIs_PathPreserved ensures that the ONVIF-reported path
// is preserved in Pass-1 candidates (the NAT port sweep uses the original
// path, not a guessed fallback path).
func TestRTSPCandidateURIs_PathPreserved(t *testing.T) {
	// Panoramic camera reports /channel1/main — that path must appear in
	// the first (convention-derived-port) candidate.
	candidates := RTSPCandidateURIs(
		"rtsp://admin:pw@577.bigview.ai:554/channel1/main",
		"577.bigview.ai:8082",
		"main",
	)
	if len(candidates) == 0 {
		t.Fatal("empty candidate list")
	}
	if !strings.Contains(candidates[0], "/channel1/main") {
		t.Errorf("first candidate %q does not preserve /channel1/main path", candidates[0])
	}
}

// TestRTSPCandidateURIs_LAN verifies that when the ONVIF address has no
// port (or port 0), the convention formula doesn't produce a bogus port and
// the original URI port appears in the candidate set.
func TestRTSPCandidateURIs_LAN(t *testing.T) {
	// On-LAN: ONVIF address has no port → onvifPort = 0 → no convention
	// candidate. The original :554 port should appear in the list.
	candidates := RTSPCandidateURIs(
		"rtsp://192.168.1.100:554/main",
		"192.168.1.100",  // no port
		"main",
	)
	found554 := false
	for _, c := range candidates {
		if strings.Contains(c, ":554/") {
			found554 = true
			break
		}
	}
	if !found554 {
		t.Errorf("LAN candidates %v: expected :554 to appear somewhere but did not", candidates)
	}
}

// TestRTSPCandidateURIs_NoDuplicates verifies that the candidate list has no
// duplicate URIs even when the convention-derived port equals the advertised
// port (the ONVIF:8080 / RTSP:554 first-slot case).
func TestRTSPCandidateURIs_NoDuplicates(t *testing.T) {
	candidates := RTSPCandidateURIs(
		"rtsp://admin:pw@527.bigview.ai:554/main",
		"527.bigview.ai:8080", // derived port == advertised port == 554
		"main",
	)
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if _, dup := seen[c]; dup {
			t.Errorf("duplicate candidate URI: %s", c)
		}
		seen[c] = struct{}{}
	}
}

// TestRTSPCandidateURIs_SubStream verifies that the sub-stream kind selects
// the correct alternative path family (/channel1/sub, /sub, etc.).
func TestRTSPCandidateURIs_SubStream(t *testing.T) {
	candidates := RTSPCandidateURIs(
		"rtsp://admin:pw@527.bigview.ai:554/sub",
		"527.bigview.ai:8082",
		"sub",
	)
	hasSubPath := false
	for _, c := range candidates {
		if strings.Contains(c, "/sub") {
			hasSubPath = true
			break
		}
	}
	if !hasSubPath {
		t.Errorf("sub-stream candidates %v: expected a /sub path variant but none found", candidates)
	}
}
