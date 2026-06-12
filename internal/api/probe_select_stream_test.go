package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// installProbeStub replaces probeStreamFn with a scripted stub and
// restores the original via t.Cleanup. The stub consumes errs[0],
// errs[1], … in order; once exhausted every subsequent call returns nil.
func installProbeStub(t *testing.T, errs ...error) *int32 {
	t.Helper()
	orig := probeStreamFn
	t.Cleanup(func() { probeStreamFn = orig })

	var calls int32
	probeStreamFn = func(_ context.Context, _ string, _ string) error {
		idx := atomic.AddInt32(&calls, 1) - 1
		if int(idx) >= len(errs) {
			return nil
		}
		return errs[idx]
	}
	return &calls
}

// TestProbeAndSelectStream_FirstCandidateFails_SecondSucceeds verifies
// that when the first candidate (convention-derived port :556) fails,
// ProbeAndSelectStream moves to the next candidate and returns that URI.
func TestProbeAndSelectStream_FirstCandidateFails_SecondSucceeds(t *testing.T) {
	calls := installProbeStub(t,
		errors.New("connection refused"), // first probe (convention :556/main) fails
		// second and subsequent succeed (nil from stub)
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gotMain, gotSub, err := ProbeAndSelectStream(
		ctx, "/usr/bin/ffmpeg",
		"rtsp://admin:pw@527.bigview.ai:554/main",
		"",
		"527.bigview.ai:8082",
	)
	if err != nil {
		t.Fatalf("ProbeAndSelectStream: unexpected error: %v", err)
	}
	if gotMain == "" {
		t.Fatal("ProbeAndSelectStream returned empty main URI on success")
	}
	if gotSub != "" {
		t.Errorf("expected empty sub (none provided), got %q", gotSub)
	}
	if c := atomic.LoadInt32(calls); c < 2 {
		t.Errorf("expected at least 2 probe calls (1 fail + 1 succeed), got %d", c)
	}
}

// TestProbeAndSelectStream_AllCandidatesFail verifies that when every
// candidate in the matrix fails, ProbeAndSelectStream returns a non-nil
// error and empty URIs.
func TestProbeAndSelectStream_AllCandidatesFail(t *testing.T) {
	connRefused := errors.New("connection refused")
	// Enough failures to exhaust the entire candidate matrix (~30 entries max).
	errs := make([]error, 40)
	for i := range errs {
		errs[i] = connRefused
	}
	installProbeStub(t, errs...)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gotMain, gotSub, err := ProbeAndSelectStream(
		ctx, "/usr/bin/ffmpeg",
		"rtsp://admin:pw@527.bigview.ai:554/main",
		"",
		"527.bigview.ai:8082",
	)
	if err == nil {
		t.Fatal("expected error when all candidates fail, got nil")
	}
	if gotMain != "" || gotSub != "" {
		t.Errorf("expected empty URIs on failure, got main=%q sub=%q", gotMain, gotSub)
	}
}

// TestProbeAndSelectStream_SubFails_MainSucceeds verifies that a failing
// sub-stream is non-fatal: the function returns the working main URI and
// an empty sub rather than propagating an error.
func TestProbeAndSelectStream_SubFails_MainSucceeds(t *testing.T) {
	var callCount int32
	orig := probeStreamFn
	t.Cleanup(func() { probeStreamFn = orig })
	probeStreamFn = func(_ context.Context, _ string, _ string) error {
		idx := atomic.AddInt32(&callCount, 1)
		if idx == 1 {
			return nil // first call (main, first candidate) succeeds immediately
		}
		return errors.New("404 not found") // all sub-stream candidates fail
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	gotMain, gotSub, err := ProbeAndSelectStream(
		ctx, "/usr/bin/ffmpeg",
		"rtsp://admin:pw@527.bigview.ai:554/main",
		"rtsp://admin:pw@527.bigview.ai:554/sub",
		"527.bigview.ai:8082",
	)
	if err != nil {
		t.Fatalf("expected nil error when main succeeds but sub fails, got: %v", err)
	}
	if gotMain == "" {
		t.Error("expected non-empty main URI")
	}
	if gotSub != "" {
		t.Errorf("expected empty sub URI (all sub probes failed), got %q", gotSub)
	}
}

// TestProbeAndSelectStream_EmptyFFmpegPath verifies that ProbeAndSelectStream
// propagates the empty-ffmpegPath error from ProbeRTSPStream. Handlers guard
// this with `if ffmpegPath != ""` before calling, but the underlying function
// should not silently succeed with an empty path.
func TestProbeAndSelectStream_EmptyFFmpegPath(t *testing.T) {
	// Do NOT install a stub — let the real probeStreamFn run, which calls
	// recording.ProbeRTSPStream and returns an error for empty ffmpegPath.
	ctx := context.Background()
	_, _, err := ProbeAndSelectStream(ctx, "", "rtsp://cam/main", "", "cam:8080")
	if err == nil {
		t.Fatal("expected error with empty ffmpegPath, got nil")
	}
}
