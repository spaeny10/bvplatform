package streaming

// TestH265InitSegmentArrayCompleteness verifies that the vendored patch to
// github.com/bluenviron/mediacommon/v2 correctly sets array_completeness=1
// on the VPS, SPS, and PPS NALU arrays inside the hvcC box of an H.265
// fMP4 init segment.
//
// ISO/IEC 14496-15 §8.4.1.1.1 mandates that when the sample-entry type is
// "hvc1", array_completeness MUST equal 1 for the VPS/SPS/PPS arrays.
// Chrome MSE rejects the source buffer when this bit is zero, producing
// "bufferAppendError / MANIFEST_INCOMPATIBLE_CODECS_ERROR" and causing the
// live feed to flash and die immediately.
//
// The bug was in mediacommon/v2@v2.8.3 internal/mp4/codec_boxes.go:729–754:
// HEVCNaluArray{...} structs left Completeness at Go's zero-value (false).
// The fix sets Completeness:true on each of the three arrays.
//
// Byte layout inside the marshalled hvcC box:
//
//	Each HEVCNaluArray is serialised as:
//	  [1 byte]  = Completeness(1b) | Reserved(1b) | NaluType(6b)
//	  [2 bytes] = NumNalus
//	  per-nalu: [2 bytes length] [N bytes NALUnit]
//
// VPS NaluType = 0x20  → with completeness bit (top bit set): 0xA0
// SPS NaluType = 0x21  →                                       0xA1
// PPS NaluType = 0x22  →                                       0xA2
//
// The scanner below locates the hvcC box by its four-byte FourCC signature
// ("hvcC" = 0x68 0x76 0x63 0x43), then reads the NALU-array-count field and
// checks the completeness bit on each array header. This is robust against
// shifts in the absolute offsets of the hvcC box.
import (
	"encoding/binary"
	"testing"

	fmp4 "github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
)

// realH265Params returns valid H.265 VPS/SPS/PPS bytes sourced from the
// mediacommon test suite (init_test.go "h265" case). The SPS is parseable
// by the mediacommon SPS parser, which is called during Init.Marshal.
func realH265Params() (vps, sps, pps []byte) {
	vps = []byte{0x01, 0x02, 0x03, 0x04}
	sps = []byte{
		0x42, 0x01, 0x01, 0x01, 0x60, 0x00, 0x00, 0x03,
		0x00, 0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03,
		0x00, 0x78, 0xa0, 0x03, 0xc0, 0x80, 0x10, 0xe5,
		0x96, 0x66, 0x69, 0x24, 0xca, 0xe0, 0x10, 0x00,
		0x00, 0x03, 0x00, 0x10, 0x00, 0x00, 0x03, 0x01,
		0xe0, 0x80,
	}
	pps = []byte{0x08}
	return
}

// hvcCNaluArrayHeader holds one parsed NALU-array header from the hvcC box.
type hvcCNaluArrayHeader struct {
	completeness bool
	naluType     byte
}

// parseHvcCNaluArrays locates the first "hvcC" box in data (by FourCC
// signature), then parses the NALU-array headers (completeness + naluType).
//
// hvcC box layout (after the 4-byte size and 4-byte FourCC):
//   offset  0: configurationVersion (1)
//   offsets 1–21: profile/tier/level + miscellaneous fields (21 bytes)
//   offset 22: numOfNaluArrays (1 byte)
//   offset 23+: array entries, each beginning with one byte:
//               Completeness(1) | Reserved(1) | NaluType(6)
//               then NumNalus (2) then the NALUs
//
// Because we only need the VPS/SPS/PPS completeness bits (all appear before
// the variable-length NALU data), we parse lazily: we walk array-by-array
// and skip over each NALU payload to reach the next header.
func parseHvcCNaluArrays(data []byte) ([]hvcCNaluArrayHeader, int, bool) {
	// Signature: "hvcC" = 0x68 0x76 0x63 0x43
	sig := []byte{'h', 'v', 'c', 'C'}
	boxStart := -1
	for i := 0; i <= len(data)-4; i++ {
		if data[i] == sig[0] && data[i+1] == sig[1] && data[i+2] == sig[2] && data[i+3] == sig[3] {
			boxStart = i
			break
		}
	}
	if boxStart < 0 {
		return nil, 0, false
	}

	// Skip past the FourCC (4 bytes) to reach configurationVersion.
	// The box content starts at boxStart+4.
	p := boxStart + 4

	// configurationVersion (1) + 21 bytes of profile/tier/level/misc = 22 bytes before numOfNaluArrays.
	if p+22 > len(data) {
		return nil, boxStart, false
	}
	p += 22

	numArrays := int(data[p])
	p++

	headers := make([]hvcCNaluArrayHeader, 0, numArrays)
	for i := 0; i < numArrays; i++ {
		if p >= len(data) {
			break
		}
		arrayByte := data[p]
		p++
		completeness := (arrayByte & 0x80) != 0
		naluType := arrayByte & 0x3F
		headers = append(headers, hvcCNaluArrayHeader{completeness: completeness, naluType: naluType})

		// Skip NumNalus (2) and the NALU payloads so we land on the next header.
		if p+2 > len(data) {
			break
		}
		numNalus := int(binary.BigEndian.Uint16(data[p : p+2]))
		p += 2
		for n := 0; n < numNalus; n++ {
			if p+2 > len(data) {
				break
			}
			naluLen := int(binary.BigEndian.Uint16(data[p : p+2]))
			p += 2 + naluLen
		}
	}

	return headers, boxStart, true
}

func TestH265InitSegmentArrayCompleteness(t *testing.T) {
	vps, sps, pps := realH265Params()

	init := fmp4.Init{
		Tracks: []*fmp4.InitTrack{
			{
				ID:        1,
				TimeScale: 90000,
				Codec: &codecs.H265{
					VPS: vps,
					SPS: sps,
					PPS: pps,
				},
			},
		},
	}

	var buf seekablebuffer.Buffer
	if err := init.Marshal(&buf); err != nil {
		t.Fatalf("Init.Marshal failed: %v", err)
	}

	data := buf.Bytes()
	t.Logf("init segment size: %d bytes", len(data))

	headers, hvcCOffset, found := parseHvcCNaluArrays(data)
	if !found {
		t.Fatal("hvcC box not found in marshalled init segment — unexpected format change?")
	}
	t.Logf("hvcC box found at offset 0x%03X, %d NALU arrays", hvcCOffset, len(headers))

	// Expected: 3 arrays — VPS (0x20), SPS (0x21), PPS (0x22) — each with completeness=true.
	if len(headers) != 3 {
		t.Errorf("expected 3 NALU arrays (VPS+SPS+PPS), got %d", len(headers))
	}

	names := []string{"VPS", "SPS", "PPS"}
	expectedTypes := []byte{0x20, 0x21, 0x22}

	for i, h := range headers {
		name := "array[unknown]"
		if i < len(names) {
			name = names[i]
		}
		if i < len(expectedTypes) && h.naluType != expectedTypes[i] {
			t.Errorf("%s: unexpected NaluType: got 0x%02X, want 0x%02X", name, h.naluType, expectedTypes[i])
		}
		if !h.completeness {
			t.Errorf("%s (NaluType=0x%02X): array_completeness=0 — "+
				"hvc1 spec violation (ISO/IEC 14496-15 §8.4.1.1.1); "+
				"Chrome MSE will reject the source buffer (bufferAppendError). "+
				"Ensure internal/vendored/mediacommon has Completeness:true in each HEVCNaluArray.",
				name, h.naluType)
		} else {
			t.Logf("OK: %s NaluType=0x%02X array_completeness=1", name, h.naluType)
		}
	}
}
