package recording

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestIsRTSPBandwidthExhausted_PositiveCases covers the ffprobe error
// strings we've actually seen surface 453 in the journal. ffprobe
// produces slightly different wording depending on libavformat
// version, but the literal "453" always appears — the matcher anchors
// on the digit string for that reason.
func TestIsRTSPBandwidthExhausted_PositiveCases(t *testing.T) {
	cases := []string{
		"[rtsp @ 0x...] method DESCRIBE failed: 453 Not Enough Bandwidth",
		"rtsp://cam:554/main: Server returned 4XX Client Error, but not one of 40{0,1,3,4} (453)",
		"ffprobe rtsp://...: exit status 1 (stderr: 453 Not Enough Bandwidth)",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !isRTSPBandwidthExhausted(errors.New(msg)) {
				t.Errorf("isRTSPBandwidthExhausted(%q) = false, want true", msg)
			}
		})
	}
}

// TestIsRTSPBandwidthExhausted_NegativeCases keeps the matcher from
// drifting into false positives. 404 / 401 / connection refused / nil
// must all return false so the retry doesn't fire on errors retry
// can't fix.
func TestIsRTSPBandwidthExhausted_NegativeCases(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil", nil},
		{"404 not found", errors.New("method DESCRIBE failed: 404 Stream Not Found")},
		{"401 unauthorized", errors.New("method DESCRIBE failed: 401 Unauthorized")},
		{"connection refused", errors.New("dial tcp 5001.bigview.ai:554: connect: connection refused")},
		{"timeout", errors.New("rtsp probe: context deadline exceeded")},
		{"empty uri sentinel", errors.New("rtsp probe: empty uri")},
		{"random number lookalike (no 453)", errors.New("ffprobe exit status 1: error 41-7654-xy")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isRTSPBandwidthExhausted(tc.err) {
				t.Errorf("isRTSPBandwidthExhausted(%v) = true, want false", tc.err)
			}
		})
	}
}

// fakeFFprobe is a counter-tracked stub used in the integration-shaped
// retry test below. Replaces the ffmpeg/ffprobe shell-out by hooking
// the probe-one entry point via a package-level injector.
type fakeFFprobe struct {
	attempts int32
	// errs is consumed left-to-right; once exhausted, every subsequent
	// call returns nil (success). Allows tests to express scripts like
	// "453 then 453 then succeed" or "453 then succeed".
	errs []error
}

func (f *fakeFFprobe) probe(_ context.Context, _ string, _ string) error {
	idx := atomic.AddInt32(&f.attempts, 1) - 1
	if int(idx) >= len(f.errs) {
		return nil
	}
	return f.errs[idx]
}

// TestProbeRTSPStream_RetryOn453 swaps in a hook for the inner probe
// call and asserts the retry-on-453 behavior end-to-end:
//
//  1. First call returns 453 → retry should fire
//  2. Sleep window completes
//  3. Second call returns nil → overall return is nil
//
// We can't drive the actual exec.CommandContext path in a unit test
// without ffprobe installed, but we can verify the retry decision and
// the attempts counter — which is what the regression actually
// targets.
func TestProbeRTSPStream_RetryOn453(t *testing.T) {
	originalDelay := rtspBandwidthRetryDelay
	rtspBandwidthRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { rtspBandwidthRetryDelay = originalDelay })

	fake := &fakeFFprobe{
		errs: []error{
			errors.New("ffprobe rtsp://x: exit status 1 (stderr: method DESCRIBE failed: 453 Not Enough Bandwidth)"),
			// second call: nil (success)
		},
	}
	originalProbe := probeRTSPStreamOnceFn
	probeRTSPStreamOnceFn = fake.probe
	t.Cleanup(func() { probeRTSPStreamOnceFn = originalProbe })

	if err := ProbeRTSPStream(context.Background(), "/usr/bin/ffmpeg", "rtsp://cam/main"); err != nil {
		t.Fatalf("ProbeRTSPStream after 453+success: %v", err)
	}
	if got := atomic.LoadInt32(&fake.attempts); got != 2 {
		t.Errorf("attempt count = %d, want 2 (initial + 1 retry)", got)
	}
}

// TestProbeRTSPStream_NoRetryOn404 is the inverse safety net:
// non-453 errors must propagate immediately, with no second probe
// call. A retry on 404 would just stretch add-camera latency for a
// fault that's never going to clear.
func TestProbeRTSPStream_NoRetryOn404(t *testing.T) {
	originalDelay := rtspBandwidthRetryDelay
	rtspBandwidthRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { rtspBandwidthRetryDelay = originalDelay })

	notFound := errors.New("ffprobe rtsp://x: exit status 1 (stderr: method DESCRIBE failed: 404 Stream Not Found)")
	fake := &fakeFFprobe{errs: []error{notFound, nil}} // second slot would be a retry; we expect it NOT to fire
	originalProbe := probeRTSPStreamOnceFn
	probeRTSPStreamOnceFn = fake.probe
	t.Cleanup(func() { probeRTSPStreamOnceFn = originalProbe })

	err := ProbeRTSPStream(context.Background(), "/usr/bin/ffmpeg", "rtsp://cam/main")
	if err == nil {
		t.Fatal("ProbeRTSPStream on 404: nil error, want failure")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want 404 substring", err)
	}
	if got := atomic.LoadInt32(&fake.attempts); got != 1 {
		t.Errorf("attempt count = %d, want 1 (no retry on 404)", got)
	}
}

// TestProbeRTSPStream_PersistentBandwidthSaturation: when 453 still
// comes back on the retry, the function fails with the underlying
// error rather than papering over a permanent saturation. The
// candidate-matrix caller observes the failure and moves to the next
// (port, path) variant.
func TestProbeRTSPStream_PersistentBandwidthSaturation(t *testing.T) {
	originalDelay := rtspBandwidthRetryDelay
	rtspBandwidthRetryDelay = 10 * time.Millisecond
	t.Cleanup(func() { rtspBandwidthRetryDelay = originalDelay })

	bw := errors.New("ffprobe rtsp://x: exit status 1 (stderr: 453 Not Enough Bandwidth)")
	fake := &fakeFFprobe{errs: []error{bw, bw}}
	originalProbe := probeRTSPStreamOnceFn
	probeRTSPStreamOnceFn = fake.probe
	t.Cleanup(func() { probeRTSPStreamOnceFn = originalProbe })

	err := ProbeRTSPStream(context.Background(), "/usr/bin/ffmpeg", "rtsp://cam/main")
	if err == nil {
		t.Fatal("ProbeRTSPStream with persistent 453: nil error, want failure")
	}
	if got := atomic.LoadInt32(&fake.attempts); got != 2 {
		t.Errorf("attempt count = %d, want 2 (initial + 1 retry, no more)", got)
	}
}

// TestProbeRTSPStream_ContextCancellationDuringBackoff: if the caller
// cancels mid-wait, the probe must return ctx.Err() rather than
// completing the retry — otherwise the add-camera 60 s budget would
// not actually bound the worst case.
func TestProbeRTSPStream_ContextCancellationDuringBackoff(t *testing.T) {
	originalDelay := rtspBandwidthRetryDelay
	rtspBandwidthRetryDelay = 2 * time.Second // long enough we can cancel inside it
	t.Cleanup(func() { rtspBandwidthRetryDelay = originalDelay })

	fake := &fakeFFprobe{
		errs: []error{errors.New("ffprobe ...: 453 Not Enough Bandwidth")},
	}
	originalProbe := probeRTSPStreamOnceFn
	probeRTSPStreamOnceFn = fake.probe
	t.Cleanup(func() { probeRTSPStreamOnceFn = originalProbe })

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first probe has run but before the backoff completes.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := ProbeRTSPStream(ctx, "/usr/bin/ffmpeg", "rtsp://cam/main")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ProbeRTSPStream after cancellation: nil error, want context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if elapsed > rtspBandwidthRetryDelay {
		t.Errorf("returned after %s, longer than the full backoff %s — cancellation didn't short-circuit",
			elapsed, rtspBandwidthRetryDelay)
	}
	if got := atomic.LoadInt32(&fake.attempts); got != 1 {
		t.Errorf("attempt count = %d, want 1 (canceled before retry)", got)
	}
}

// ensure fmt is used (kept for adding richer assertions later) — silence unused-import lint.
var _ = fmt.Sprintf

// ── B-16: ClassifyProbeError ──────────────────────────────────────────────────

// TestClassifyProbeError_Success: a nil error is ProbeSuccess.
func TestClassifyProbeError_Success(t *testing.T) {
	if got := ClassifyProbeError(nil); got != ProbeSuccess {
		t.Errorf("ClassifyProbeError(nil) = %v, want ProbeSuccess", got)
	}
}

// TestClassifyProbeError_DefinitiveFailures: hard server rejections must map to
// ProbeDefinitiveFailure so block-add (HandleCreateCamera) returns 400.
func TestClassifyProbeError_DefinitiveFailures(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"404 not found", "ffprobe rtsp://...: exit status 1 (stderr: method DESCRIBE failed: 404 Stream Not Found)"},
		{"404 bare", "Server returned 404"},
		{"connection refused", "dial tcp 5001.bigview.ai:554: connect: connection refused"},
		{"no route to host", "connect: no route to host"},
		{"401 unauthorized", "method DESCRIBE failed: 401 Unauthorized"},
		{"401 bare", "Server returned 401"},
		{"invalid data", "Invalid data found when processing input"},
		{"could not find codec", "could not find codec parameters"},
		{"stream not found", "Stream Not Found"},
		{"ffmpeg path not configured", "rtsp probe: ffmpeg path not configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyProbeError(errors.New(tc.msg))
			if got != ProbeDefinitiveFailure {
				t.Errorf("ClassifyProbeError(%q) = %v, want ProbeDefinitiveFailure", tc.msg, got)
			}
		})
	}
}

// TestClassifyProbeError_Inconclusive: timeout/kill class must map to
// ProbeInconclusive so wide/cellular cameras are allowed optimistically.
func TestClassifyProbeError_Inconclusive(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"signal killed", "ffprobe rtsp://5001.bigview.ai:554/channel1/main: signal: killed"},
		{"context deadline exceeded", "ffprobe rtsp://...: context deadline exceeded"},
		{"context canceled", "ffprobe rtsp://...: context canceled"},
		// Generic connect timeout — no server fragment → Inconclusive by default.
		{"generic timeout no fragment", "ffprobe rtsp://...: exit status 1 (stderr: Connection timed out)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyProbeError(errors.New(tc.msg))
			if got != ProbeInconclusive {
				t.Errorf("ClassifyProbeError(%q) = %v, want ProbeInconclusive", tc.msg, got)
			}
		})
	}
}

// TestClassifyProbeError_UnknownDefaultsToInconclusive: unrecognised error
// strings fall back to Inconclusive so novel transient conditions don't
// permanently block camera adds.
func TestClassifyProbeError_UnknownDefaultsToInconclusive(t *testing.T) {
	msg := "ffprobe: some novel error we've never seen before"
	got := ClassifyProbeError(errors.New(msg))
	if got != ProbeInconclusive {
		t.Errorf("ClassifyProbeError(%q) = %v, want ProbeInconclusive (unknown defaults to inconclusive)", msg, got)
	}
}
