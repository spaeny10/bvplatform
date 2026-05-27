package recording

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ProbeRTSPStream verifies an RTSP URI actually serves a video stream by
// running ffprobe DESCRIBE against it. Returns nil if at least one video
// stream is found. The recording engine already shells out to ffprobe for
// codec detection, so this reuses the same binary discovery (ffprobeBin)
// and keeps the dependency surface unchanged.
//
// Used at camera-add time to detect when an ONVIF-reported StreamURI is
// wrong — either the path (Milesight MS-C4467 reports "/main" when the
// camera only serves "/channel1/main") or the port (NAT setups where the
// camera reports its INTERNAL port 554 but the external mapping uses
// 555/556/557/etc. per camera). Without this check the row lands in the
// DB and MediaMTX endlessly 404s on the stream source, leaving the
// operator with a "broken camera" they have no signal to diagnose.
//
// 453 "Not Enough Bandwidth" gets a single retry after a short wait —
// camera firmware caps concurrent RTSP clients at 4–6, and if the
// recorder is already pulling main+sub on adjacent cameras the probe
// can lose a slot to a transient. One quick re-attempt distinguishes
// the transient case from a steady-state saturation that no amount of
// retrying will fix. Other failures (404, connection refused, timeout)
// don't retry — those indicate a different fault that retrying won't
// resolve and would just stretch add-camera latency.
func ProbeRTSPStream(ctx context.Context, ffmpegPath, uri string) error {
	if uri == "" {
		return fmt.Errorf("rtsp probe: empty uri")
	}
	if ffmpegPath == "" {
		return fmt.Errorf("rtsp probe: ffmpeg path not configured")
	}

	err := probeRTSPStreamOnceFn(ctx, ffmpegPath, uri)
	if err == nil {
		return nil
	}
	if !isRTSPBandwidthExhausted(err) {
		return err
	}

	// Steady-state saturation will still 453 after the wait, but
	// transient overlap with the recording engine's connect+drop on a
	// neighboring camera typically clears in a couple of seconds. One
	// retry is plenty — the candidate matrix is already trying multiple
	// (port, path) variants so we don't need to hammer any single URI.
	log.Printf("[RTSP-PROBE] 453 bandwidth-exhausted on %s — retrying once after %s", redactURI(uri), rtspBandwidthRetryDelay)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(rtspBandwidthRetryDelay):
	}
	return probeRTSPStreamOnceFn(ctx, ffmpegPath, uri)
}

// probeRTSPStreamOnceFn is the package-level hook that ProbeRTSPStream
// calls to run a single DESCRIBE attempt. Defaults to the real
// probeRTSPStreamOnce; tests swap it out to drive the retry decision
// logic without invoking ffprobe. Kept as a var so the test override
// pattern (set, t.Cleanup-restore) stays type-checked.
var probeRTSPStreamOnceFn = probeRTSPStreamOnce

// rtspBandwidthRetryDelay is how long we wait between the first probe
// and the single retry when the camera responded 453. Long enough for
// a neighbor's connect-then-drop cycle to clear, short enough to keep
// the worst-case add-camera latency reasonable when stacked across the
// candidate matrix. Exposed as a package var (not a const) so tests
// can shorten it without sleeping for real seconds.
var rtspBandwidthRetryDelay = 3 * time.Second

// probeRTSPStreamOnce is the single-shot DESCRIBE used by both the
// happy path and the post-453 retry. Split out so the retry logic in
// ProbeRTSPStream stays readable and so unit tests can drive the
// retry-decision path without re-running the underlying ffprobe call.
func probeRTSPStreamOnce(ctx context.Context, ffmpegPath, uri string) error {
	// 4 s per-probe budget keeps the candidate matrix tractable. With up
	// to ~12 candidates per stream and 2 streams (main + sub) per camera,
	// a 4 s ceiling caps add-camera at ~96 s worst-case; typical first-
	// hit success is well under 2 s.
	pctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	// RTSP socket timeout is "-timeout <microseconds>" on ffmpeg 5.x+; the
	// older "-stimeout" flag was removed and ffprobe rejects it outright.
	// The wrapping context above is the real backstop — the in-process
	// timeout just keeps unresponsive cameras from chewing the full 4 s.
	cmd := exec.CommandContext(pctx, ffprobeBin(ffmpegPath),
		"-v", "error",
		"-rtsp_transport", "tcp",
		"-timeout", "3000000",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=nokey=1:noprint_wrappers=1",
		uri,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffprobe %s: %w (stderr: %s)", redactURI(uri), err, strings.TrimSpace(stderr.String()))
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return fmt.Errorf("ffprobe %s: no video stream", redactURI(uri))
	}
	return nil
}

// isRTSPBandwidthExhausted reports whether an ffprobe error is the
// specific "453 Not Enough Bandwidth" response — the signal that the
// camera has hit its concurrent-RTSP-clients cap and the only useful
// recovery is to wait for someone else to disconnect.
//
// ffprobe surfaces 453 via stderr (formatted by libavformat as
// "method DESCRIBE failed: 453 Not Enough Bandwidth" plus a second
// line "Server returned 4XX Client Error, but not one of 40{0,1,3,4}").
// We match on the "453" substring rather than the whole message
// because the exact phrasing changes between ffmpeg versions, but the
// numeric status stays put.
//
// 404 / 401 / 403 / connection-refused / timeout deliberately don't
// match — those are not bandwidth issues and retrying them just slows
// down add-camera without changing the outcome.
func isRTSPBandwidthExhausted(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "453")
}

// RTSPCandidateURIs returns a prioritized list of (port, path) variants to
// probe when an ONVIF-reported StreamURI fails or is suspected to point at
// the wrong device. Two real-world patterns drive this:
//
//  1. Wrong path — ONVIF reports a path the camera doesn't actually serve
//     (e.g., MS-C4467 returns "/main" when only "/channel1/main" works).
//  2. Wrong port (NAT) — each camera in a multi-cam NAT setup reports its
//     INTERNAL port 554, but the external mapping uses sequential ports
//     (ONVIF 8080→RTSP 554, 8081→555, 8082→556, ...). The camera firmware
//     has no way to know which external port its operator routed it to.
//
// Pass 1 of the matrix preserves the ONVIF-reported path and sweeps ports
// derived from the user-typed ONVIF address — this catches the NAT case
// without dropping back to wrong-camera streams that happen to also work
// on 554. Pass 2 sweeps alternative paths in case the path was the issue.
//
// streamKind ("main" or "sub") selects which path family to fall back
// through.
//
// Empty when the input URI is unparseable.
func RTSPCandidateURIs(originalURI, onvifAddress, streamKind string) []string {
	parsed, err := url.Parse(originalURI)
	if err != nil || parsed.Host == "" {
		return nil
	}

	var altPaths []string
	switch streamKind {
	case "sub":
		altPaths = []string{
			"/channel1/sub",
			"/channel01/sub",
			"/sub",
			"/Streaming/Channels/102",
			"/h264sub",
		}
	default:
		altPaths = []string{
			"/channel1/main",
			"/channel01/main",
			"/main",
			"/Streaming/Channels/101",
			"/h264",
		}
	}

	host := parsed.Hostname()
	origPort := portOf(parsed.Host, 554)
	onvifPort := portOf(onvifAddress, 0)

	// Build the candidate port set in priority order. Derived port goes
	// first when the user typed a non-default ONVIF port — that's the
	// NAT case, and getting it right BEFORE falling back to 554 is what
	// keeps us from latching onto whichever camera happens to live on
	// the default port.
	var ports []int
	add := func(p int) {
		if p <= 0 || p > 65535 {
			return
		}
		for _, existing := range ports {
			if existing == p {
				return
			}
		}
		ports = append(ports, p)
	}
	// NAT pattern: ONVIF 8080+N → RTSP 554+N (the offset is 7526).
	if onvifPort >= 7527 && onvifPort < 7527+65535 {
		add(onvifPort - 7526)
	}
	// Whatever the ONVIF response said.
	add(origPort)
	// HTTP-tunneling: RTSP on the same port as ONVIF.
	if onvifPort > 0 {
		add(onvifPort)
	}
	// 8554 is a common alternate default for RTSP-over-non-privileged
	// (e.g., docker port maps).
	add(8554)

	seen := map[string]struct{}{}
	var out []string
	emit := func(port int, path string) {
		u := *parsed
		u.Host = host + ":" + strconv.Itoa(port)
		u.Path = path
		u.RawQuery = ""
		s := u.String()
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	// Pass 1: original path × candidate ports (catches the NAT case).
	originalPath := parsed.Path
	if originalPath != "" {
		for _, p := range ports {
			emit(p, originalPath)
		}
	}
	// Pass 2: alternative paths × candidate ports (catches the wrong-path
	// case after we've already ruled out port-only mismatches).
	for _, path := range altPaths {
		if path == originalPath {
			continue
		}
		for _, p := range ports {
			emit(p, path)
		}
	}
	return out
}

// portOf parses "host:port" and returns the port as int. fallback is
// returned when the port is missing, unparseable, or out of range.
func portOf(hostport string, fallback int) int {
	// Handle bare "host" with no port.
	if !strings.Contains(hostport, ":") {
		return fallback
	}
	// Try url-style first to handle "[::1]:554"-shaped inputs.
	u, err := url.Parse("scheme://" + hostport)
	if err == nil && u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 && p < 65536 {
			return p
		}
	}
	// Fall back to last-colon split.
	idx := strings.LastIndex(hostport, ":")
	if idx < 0 {
		return fallback
	}
	if p, err := strconv.Atoi(hostport[idx+1:]); err == nil && p > 0 && p < 65536 {
		return p
	}
	return fallback
}

// redactURI strips userinfo so probe errors logged at INFO don't leak
// camera credentials into the journal.
func redactURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "<unparseable>"
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("***", "***")
	}
	return parsed.String()
}
