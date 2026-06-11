package milesight

import (
	"encoding/binary"
	"log"
)

// Binary /webstream/track frame format (panoramic / multi-sensor Milesight
// models — 504 front/back, 5001 front/back). Reverse-engineered from live
// captures cross-referenced against the JSON path (504 left-ptz) on 2026-06-11.
//
// Wire layout (all little-endian):
//
//	HEADER (80 bytes)
//	  off 0   [8] magic           33 22 11 00 37 C8 33 01
//	  off 40  u32 headerSize      always 80
//	  off 48  u32 payloadBytes    == trackNum*76 (records follow the header)
//	  off 52  u32 flags           0x1000 bitfield (0x20 toggles when a track is "showing")
//	  off 56  u32 trackNum        number of 76-byte track records that follow
//	  off 64  u32 timeUsec        frame timestamp (monotonic, microseconds)
//	  off 72  u32 timeHd          timestamp high word
//
//	TRACK RECORD (76 bytes, repeated trackNum times) — 19× int32 LE
//	  rec[0]  off 0   trackID
//	  rec[1]  off 4   x      } analytics-frame coordinates (320×180 space — see
//	  rec[2]  off 8   y      } msAnalyticsFrameW/H below), same semantics as the
//	  rec[3]  off 12  w      } JSON path's x/y/w/h.
//	  rec[4]  off 16  h      }
//	  rec[5]  off 20  Class  1=human, 2=vehicle, 3=face
//	  rec[6..17]      VCA rule flag slots (intrusion/linecross/loitering/…).
//	                  All observed as 0 in captures — these cameras signal basic
//	                  motion via track presence + showEvent, not a per-flag motion
//	                  bit, so the exact per-rule offsets could not be positively
//	                  pinned (no rule fired during capture). Decoded conservatively.
//	  rec[18] off 72  showEvent (0/1) — the track is actively reported this frame.
const (
	msHeaderSize    = 80
	msTrackRecSize  = 76
	msTrackRecInts  = 19 // 19 × int32 = 76 bytes
	msTrackNumOff   = 56 // u32 trackNum within the header

	// Field offsets within a 76-byte track record (int32 indices).
	msRecTrackID   = 0
	msRecX         = 1
	msRecY         = 2
	msRecW         = 3
	msRecH         = 4
	msRecClass     = 5
	msRecShowEvent = 18

	// Analytics-frame resolution the track x/y/w/h coordinates live in. The
	// Milesight VCA engine runs at a fixed 320×180 grid regardless of the
	// camera's capture resolution (the JSON 504 left-ptz reaches x=319, and the
	// binary cameras' max x+w≈273 / y+h≈128 fit the same space). This is NOT the
	// stream pixel size (the 5001/504 panoramics encode 5120×1520). The frontend
	// must normalize the bbox by these dims, not by the 0-10000 region grid
	// (see O-08) and not by the thumbnail pixel size.
	msAnalyticsFrameW = 320
	msAnalyticsFrameH = 180
)

// msMagic is the 5-byte tag every binary /webstream/track frame starts with.
var msMagic = []byte{0x33, 0x22, 0x11, 0x00, 0x37}

// isBinaryTrackFrame reports whether data is a Milesight binary track frame:
// it carries the magic header and a header + N whole 76-byte records.
func isBinaryTrackFrame(data []byte) bool {
	if len(data) < msHeaderSize || len(data) < len(msMagic) {
		return false
	}
	for i, b := range msMagic {
		if data[i] != b {
			return false
		}
	}
	return (len(data)-msHeaderSize)%msTrackRecSize == 0
}

// decodeBinaryTrack parses a binary /webstream/track frame into the shared
// msTrack representation. It returns the decoded tracks. The number of records
// is derived from the frame length and cross-checked against the header
// trackNum; if they disagree the smaller is used (defensive against a partial
// or padded frame).
func decodeBinaryTrack(data []byte) []msTrack {
	if !isBinaryTrackFrame(data) {
		return nil
	}

	byLen := (len(data) - msHeaderSize) / msTrackRecSize
	trackNum := int(binary.LittleEndian.Uint32(data[msTrackNumOff : msTrackNumOff+4]))
	n := byLen
	if trackNum >= 0 && trackNum < n {
		n = trackNum
	}

	tracks := make([]msTrack, 0, n)
	for i := 0; i < n; i++ {
		base := msHeaderSize + i*msTrackRecSize
		rec := data[base : base+msTrackRecSize]
		i32 := func(idx int) int {
			return int(int32(binary.LittleEndian.Uint32(rec[idx*4 : idx*4+4])))
		}

		t := msTrack{
			TrackID: i32(msRecTrackID),
			X:       i32(msRecX),
			Y:       i32(msRecY),
			W:       i32(msRecW),
			H:       i32(msRecH),
			Class:   i32(msRecClass),
		}

		// These panoramic models do not set a per-track vcaAdvancedMotion bit in
		// the binary frame (all VCA flag slots are 0); a track that is actively
		// reported (showEvent=1) IS the camera's basic-motion signal — the binary
		// equivalent of the JSON path's vcaAdvancedMotion:1. Map showEvent → basic
		// motion so these cameras deliver motion events (the JSON path's flag set
		// otherwise leaves them silent, the root of O-07).
		if i32(msRecShowEvent) != 0 {
			t.VcaAdvancedMotion = 1
		}

		tracks = append(tracks, t)
	}
	return tracks
}

// parseBinaryTrack decodes a binary /webstream/track frame and emits events
// through the same flag→event mapping + edge-detection as the JSON path.
func (es *EventStream) parseBinaryTrack(data []byte) {
	tracks := decodeBinaryTrack(data)
	if tracks == nil {
		log.Printf("[MILESIGHT] %s: dropped malformed binary track frame len=%d", es.label, len(data))
		return
	}
	es.emitTrackEvents(tracks, msAnalyticsFrameW, msAnalyticsFrameH)
}
