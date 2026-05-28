package safety

// roi_crop.go — P2-C-05 ROI cropping for VLM input.
//
// CropToROI decodes a JPEG, crops to the detection bounding box (normalized
// coords) with configurable padding, and returns a re-encoded JPEG. The
// function is pure (no I/O, no side effects) and fully unit-testable.
//
// Integration point: vlm_worker.go calls CropToROI before sending the frame
// to Qwen. On any crop failure the worker falls back to the full frame so
// VLM validation always proceeds — crop quality is best-effort.
//
// Design notes:
//   - Go stdlib only (image/jpeg + image/draw). No new dependencies.
//   - Bboxes are normalized [0,1]; conversion uses int(coord * dim) with
//     x2/y2 clamped to dim (not dim-1) because image.Rect uses half-open
//     intervals and SubImage / Draw respect that correctly.
//   - JPEG re-encode at quality=90: minimal perceptual loss, 2–5× size
//     reduction vs. raw RGBA. Single-use inference artifact; no audit need.
//   - SubImage is called via the SubImager interface when the decoded image
//     supports it (image.YCbCr, which is the standard JPEG decode output).
//     A draw.Draw fallback handles exotic image types.

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"math"

	"ironsight/internal/ai"
)

// ErrDegenerateBBox is returned when the bounding box produces a zero-area
// crop after coordinate conversion and clamping.
var ErrDegenerateBBox = errors.New("degenerate bounding box: zero-area crop after clamping")

// subImager is satisfied by image types that support SubImage natively
// (image.YCbCr, image.NRGBA, image.RGBA, etc.).
type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// CropToROI decodes a JPEG frame, crops to the detection's bounding box
// (normalized coords) with padding, and returns a re-encoded JPEG.
//
// paddingFactor controls context surrounding the bbox. 0.25 pads by 25% of
// max(bboxWidth, bboxHeight) in each direction, clamped to frame bounds.
// Accepted range: 0.0–1.0; values outside are silently clamped.
//
// Returns (nil, ErrDegenerateBBox) when the bbox is degenerate (zero-area
// or inverted after coordinate conversion and clamping).
// Returns (nil, err) when JPEG decode or re-encode fails.
// On success returns the cropped JPEG bytes; jpegBytes is not modified.
func CropToROI(jpegBytes []byte, bbox ai.BBox, paddingFactor float64) ([]byte, error) {
	// Decode the full JPEG to get dimensions and pixel data.
	img, _, err := image.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	imgW := bounds.Max.X - bounds.Min.X
	imgH := bounds.Max.Y - bounds.Min.Y
	if imgW <= 0 || imgH <= 0 {
		return nil, ErrDegenerateBBox
	}

	// Clamp paddingFactor to [0.0, 1.0].
	if paddingFactor < 0.0 {
		paddingFactor = 0.0
	}
	if paddingFactor > 1.0 {
		paddingFactor = 1.0
	}

	// Convert normalized bbox to pixel coordinates.
	// int(coord * dim) gives the correct half-open interval bound for
	// image.Rect — x2=imgW is valid (points one past the last column).
	pixX1 := int(bbox.X1 * float64(imgW))
	pixY1 := int(bbox.Y1 * float64(imgH))
	pixX2 := int(bbox.X2 * float64(imgW))
	pixY2 := int(bbox.Y2 * float64(imgH))

	// Handle inverted bbox: swap so x1 < x2, y1 < y2 before padding.
	if pixX1 > pixX2 {
		pixX1, pixX2 = pixX2, pixX1
	}
	if pixY1 > pixY2 {
		pixY1, pixY2 = pixY2, pixY1
	}

	// Compute padding in pixels: pad = round(max(bboxW, bboxH) * paddingFactor).
	bboxW := pixX2 - pixX1
	bboxH := pixY2 - pixY1
	larger := bboxW
	if bboxH > larger {
		larger = bboxH
	}
	pad := int(math.Round(float64(larger) * paddingFactor))

	// Expand bbox by pad in all four directions, clamp to frame bounds.
	x1 := pixX1 - pad
	y1 := pixY1 - pad
	x2 := pixX2 + pad
	y2 := pixY2 + pad

	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 > imgW {
		x2 = imgW
	}
	if y2 > imgH {
		y2 = imgH
	}

	// Offset to absolute image coordinates (bounds.Min may not be (0,0) for
	// sub-images, though for a freshly decoded JPEG it always is).
	x1 += bounds.Min.X
	y1 += bounds.Min.Y
	x2 += bounds.Min.X
	y2 += bounds.Min.Y

	// Degenerate crop guard: must have positive area.
	if x2 <= x1 || y2 <= y1 {
		return nil, ErrDegenerateBBox
	}

	cropRect := image.Rect(x1, y1, x2, y2)

	// Crop — prefer SubImage (zero-copy view) when the image type supports it;
	// fall back to draw.Draw into a new RGBA for exotic image types.
	var cropped image.Image
	if si, ok := img.(subImager); ok {
		cropped = si.SubImage(cropRect)
	} else {
		dst := image.NewRGBA(image.Rect(0, 0, x2-x1, y2-y1))
		draw.Draw(dst, dst.Bounds(), img, cropRect.Min, draw.Src)
		cropped = dst
	}

	// Re-encode as JPEG at quality 90.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, cropped, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
