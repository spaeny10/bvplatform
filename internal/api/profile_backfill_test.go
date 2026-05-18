package api

import "testing"

func TestStripCreds(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// No creds — pass-through.
		{"rtsp://192.168.50.10:554/path", "rtsp://192.168.50.10:554/path"},
		// Standard user:pass@host shape (camera-add handler injects this).
		{"rtsp://admin:secret123@192.168.50.10:554/path", "rtsp://192.168.50.10:554/path"},
		// Username only (no password) — still ends in @.
		{"rtsp://admin@192.168.50.10:554/path", "rtsp://192.168.50.10:554/path"},
		// Empty password but colon present (rare but possible).
		{"rtsp://admin:@192.168.50.10:554/path", "rtsp://192.168.50.10:554/path"},
		// @ inside the path (not in the userinfo) — preserved.
		// Our stripCreds only strips @ that appears before the first /,
		// so an @ in the path is left alone.
		{"rtsp://192.168.50.10:554/path@with@ats", "rtsp://192.168.50.10:554/path@with@ats"},
		// Both — user:pass@ AND @ in path. Userinfo stripped, path-@ kept.
		{"rtsp://admin:pw@192.168.50.10:554/path@suffix", "rtsp://192.168.50.10:554/path@suffix"},
		// Not a URI at all — pass-through.
		{"", ""},
		{"not a url", "not a url"},
		// HTTP-style URI (e.g., from an ONVIF GetSnapshotUri).
		{"http://admin:pw@192.168.1.5/snapshot", "http://192.168.1.5/snapshot"},
	}
	for _, c := range cases {
		got := stripCreds(c.in)
		if got != c.want {
			t.Errorf("stripCreds(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
