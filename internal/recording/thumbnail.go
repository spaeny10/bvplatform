package recording

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// captureJPEG is the shared FFmpeg frame-grab logic. Returns raw JPEG bytes.
func captureJPEG(ffmpegPath, rtspUri string, timeoutSec int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-rtsp_transport", "tcp",
		"-i", rtspUri,
		"-vframes", "1",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"-q:v", "3", // good quality for SOC evidence
		"pipe:1",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg capture failed: %w (stderr: %s)", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced no output")
	}
	return stdout.Bytes(), nil
}

// CaptureFrame grabs a single JPEG frame and returns a base64 data URI.
// Used for event thumbnails stored in the DB.
func CaptureFrame(ffmpegPath, rtspUri string, timeoutSec int) (string, error) {
	data, err := captureJPEG(ffmpegPath, rtspUri, timeoutSec)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// CaptureFrameToFile grabs a JPEG frame and writes it to destPath.
// The parent directory is created if it does not exist.
// Returns the number of bytes written.
func CaptureFrameToFile(ffmpegPath, rtspUri, destPath string, timeoutSec int) (int, error) {
	data, err := captureJPEG(ffmpegPath, rtspUri, timeoutSec)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return 0, fmt.Errorf("write snapshot: %w", err)
	}
	return len(data), nil
}

// extractSegmentJPEG is the shared seek-and-grab core. It seeks to offsetSec
// within a recording segment file and decodes a single JPEG frame, returning
// the raw bytes. Works with both complete and still-open fragmented MP4
// segments (frag_keyframe+empty_moov) — no network, 8s timeout.
func extractSegmentJPEG(ffmpegPath, segmentPath string, offsetSec float64) ([]byte, error) {
	if offsetSec < 0 {
		offsetSec = 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-ss", fmt.Sprintf("%.3f", offsetSec),
		"-i", segmentPath,
		"-vframes", "1",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"-q:v", "3",
		"pipe:1",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg segment seek failed at %.1fs: %w (stderr: %s)", offsetSec, err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg produced no output seeking to %.1fs in %s", offsetSec, segmentPath)
	}
	return stdout.Bytes(), nil
}

// ExtractFrameFromSegment seeks to offsetSec within a recording segment file and
// extracts a single JPEG frame. Works with both complete and still-open fragmented
// MP4 segments (frag_keyframe+empty_moov). Writes the result to destPath.
// Returns the number of bytes written.
func ExtractFrameFromSegment(ffmpegPath, segmentPath, destPath string, offsetSec float64) (int, error) {
	data, err := extractSegmentJPEG(ffmpegPath, segmentPath, offsetSec)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return 0, fmt.Errorf("write snapshot: %w", err)
	}
	return len(data), nil
}

// ExtractFrameFromSegmentBytes seeks to offsetSec within a recording segment and
// returns a base64 "data:image/jpeg;base64,…" data URI, ready to store on the
// event row and broadcast over the WS patch — no temp-file dance required.
func ExtractFrameFromSegmentBytes(ffmpegPath, segmentPath string, offsetSec float64) (string, error) {
	data, err := extractSegmentJPEG(ffmpegPath, segmentPath, offsetSec)
	if err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// CaptureEventThumbnail produces a base64 JPEG data URI for an event by reading
// the LOCAL recording segment that covers eventTime — a reliable, network-free
// seek that works even when the live RTSP stream is slow (cellular trailers) or
// the high-res HEVC main stream chokes a fresh grab.
//
// Resolution order:
//  1. If a DB segment hint is supplied (segHintPath non-empty), seek that file
//     at eventTime - segHintStart. This is the events.segment_id link populated
//     at insert time.
//  2. Otherwise (or if the hint fails), scan the camera's on-disk recording dir
//     via FindEventClipFull — picks the segment whose [start,end) window holds
//     the event, including the still-open fMP4 being written right now.
//  3. Fallback: a fresh RTSP grab from the SUB stream (H.264, fast) with a
//     longer timeout. Only used when no segment is found on disk (recording gap)
//     or every segment seek failed. The main stream is never used here — on a
//     cellular link the high-res HEVC pull reliably hits the timeout → empty
//     thumbnail, which is the original bug.
//
// Returns the data URI and a short label ("segment" / "segment-hint" /
// "rtsp-sub-fallback") describing which path produced it, for logging.
func CaptureEventThumbnail(ffmpegPath, storagePath, cameraID string, eventTime time.Time, segHintPath string, segHintStart time.Time, subStreamUri string, fallbackTimeoutSec int) (dataURI, via string, err error) {
	// 1) DB segment hint (events.segment_id → file_path + start_time).
	if segHintPath != "" {
		if _, statErr := os.Stat(segHintPath); statErr == nil {
			offset := eventTime.Sub(segHintStart).Seconds()
			if uri, gErr := ExtractFrameFromSegmentBytes(ffmpegPath, segHintPath, offset); gErr == nil {
				return uri, "segment-hint", nil
			} else {
				log.Printf("[THUMB] segment-hint seek failed (%s @ %.1fs): %v — trying disk scan", filepath.Base(segHintPath), offset, gErr)
			}
		}
	}

	// 2) Filesystem scan for the covering (or closest) segment.
	if storagePath != "" {
		absPath, _, segStart := FindEventClipFull(storagePath, cameraID, eventTime)
		if absPath != "" {
			offset := eventTime.Sub(segStart).Seconds()
			if uri, gErr := ExtractFrameFromSegmentBytes(ffmpegPath, absPath, offset); gErr == nil {
				return uri, "segment", nil
			} else {
				log.Printf("[THUMB] segment seek failed (%s @ %.1fs): %v — falling back to RTSP sub", filepath.Base(absPath), offset, gErr)
			}
		}
	}

	// 3) RTSP sub-stream fallback (H.264, fast) — recording gap or seek failures.
	if subStreamUri != "" {
		if fallbackTimeoutSec <= 0 {
			fallbackTimeoutSec = 12
		}
		if uri, gErr := CaptureFrame(ffmpegPath, subStreamUri, fallbackTimeoutSec); gErr == nil {
			return uri, "rtsp-sub-fallback", nil
		} else {
			return "", "", fmt.Errorf("rtsp-sub fallback failed: %w", gErr)
		}
	}

	return "", "", fmt.Errorf("no local segment found for camera %s at %s and no sub-stream URI for fallback", cameraID, eventTime.Format(time.RFC3339))
}

// ExtractClipFromSegment pulls a short video clip out of a recording segment,
// centered on an event. startOffsetSec is where to begin reading inside the
// segment, durationSec is how long the clip runs. The output is a fragmented
// MP4 suitable for uploading to the Qwen /analyze_video endpoint.
//
// We re-encode to H.264 (libx264) rather than stream-copying because stream
// copy snaps start to the nearest keyframe — on cameras with long GOPs that
// can shift the clip by several seconds from the requested start. Re-encoding
// is ~1-2s of CPU on 4s of 1080p input, which is dwarfed by Qwen inference.
func ExtractClipFromSegment(ffmpegPath, segmentPath, destPath string, startOffsetSec, durationSec float64) (int, error) {
	if startOffsetSec < 0 {
		startOffsetSec = 0
	}
	if durationSec <= 0 {
		durationSec = 4
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return 0, fmt.Errorf("mkdir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-ss", fmt.Sprintf("%.3f", startOffsetSec),
		"-i", segmentPath,
		"-t", fmt.Sprintf("%.3f", durationSec),
		"-an",             // strip audio — Qwen only needs vision
		"-c:v", "libx264", // re-encode for frame-accurate start
		"-preset", "ultrafast",
		"-crf", "28", // modest quality: the VLM downsamples anyway
		"-pix_fmt", "yuv420p",
		"-r", "10", // force constant 10 fps so qwen-vl-utils can read video_fps
		"-movflags", "+faststart",
		"-y",
		destPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg clip extract failed [%.1fs +%.1fs]: %w (stderr: %s)",
			startOffsetSec, durationSec, err, stderr.String())
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return 0, fmt.Errorf("stat clip: %w", err)
	}
	if info.Size() == 0 {
		return 0, fmt.Errorf("ffmpeg produced empty clip at %.1fs in %s", startOffsetSec, segmentPath)
	}
	return int(info.Size()), nil
}
