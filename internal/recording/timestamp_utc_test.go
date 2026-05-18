package recording

import (
	"testing"
	"time"
)

// P1-C-05: parseSegmentTimestamp + parseRingTimestamp must parse in
// UTC regardless of the host timezone. Pre-fix they parsed in
// time.Local / time.Now().Location() which produced wrong absolute
// instants on non-UTC hosts and ambiguous instants on DST-boundary
// days (1:30am on a fall-back day matches two different UTC instants).
//
// The tests below run with t.Setenv("TZ", ...) to simulate different
// host timezones. The expected results are independent of TZ because
// the parsers now pin to UTC.

func TestParseSegmentTimestamp_UTCIndependent(t *testing.T) {
	cases := []struct {
		name     string
		hostTZ   string
		filename string
		// wantUTC is the UTC instant the filename should resolve to.
		// Format: y, m, d, h, min, s.
		wantUTC time.Time
	}{
		{
			name:     "UTC host",
			hostTZ:   "UTC",
			filename: "seg_20260219_140530.mp4",
			wantUTC:  time.Date(2026, 2, 19, 14, 5, 30, 0, time.UTC),
		},
		{
			name:     "Central host — same UTC instant despite local tz",
			hostTZ:   "America/Chicago",
			filename: "seg_20260219_140530.mp4",
			wantUTC:  time.Date(2026, 2, 19, 14, 5, 30, 0, time.UTC),
		},
		{
			name:     "Pacific host — same UTC instant",
			hostTZ:   "America/Los_Angeles",
			filename: "seg_20260219_140530.mp4",
			wantUTC:  time.Date(2026, 2, 19, 14, 5, 30, 0, time.UTC),
		},
		{
			name:     "Tokyo host — same UTC instant",
			hostTZ:   "Asia/Tokyo",
			filename: "seg_20260219_140530.mp4",
			wantUTC:  time.Date(2026, 2, 19, 14, 5, 30, 0, time.UTC),
		},
		// DST boundary: 2026-03-08 02:30 doesn't exist in America/Chicago
		// (clocks spring forward from 02:00 to 03:00). Parsing it as
		// Local would either error or pick one of two ambiguous
		// interpretations. UTC parsing returns the unambiguous instant.
		{
			name:     "DST spring-forward boundary parses unambiguously",
			hostTZ:   "America/Chicago",
			filename: "seg_20260308_023000.mp4",
			wantUTC:  time.Date(2026, 3, 8, 2, 30, 0, 0, time.UTC),
		},
		// Fall back: 2026-11-01 01:30 occurs twice in America/Chicago
		// (clocks fall back from 02:00 to 01:00). Local parse picks one
		// of the two (Go pre-1.21 picks the first; post-1.21 picks
		// based on whichever is valid). UTC parse is unambiguous.
		{
			name:     "DST fall-back boundary parses unambiguously",
			hostTZ:   "America/Chicago",
			filename: "seg_20261101_013000.mp4",
			wantUTC:  time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC),
		},
		{
			name:     "stem-only path (no dir) works",
			hostTZ:   "UTC",
			filename: "seg_20260101_000000.mp4",
			wantUTC:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("TZ", c.hostTZ)
			got := parseSegmentTimestamp(c.filename)
			if !got.Equal(c.wantUTC) {
				t.Errorf("parseSegmentTimestamp(%q) under TZ=%s = %s; want %s",
					c.filename, c.hostTZ, got.UTC(), c.wantUTC)
			}
			// Verify the result is actually in UTC (not just equivalent).
			if got.Location() != time.UTC {
				t.Errorf("parseSegmentTimestamp result not in UTC: location=%v", got.Location())
			}
		})
	}
}

func TestParseSegmentTimestamp_InvalidInputs(t *testing.T) {
	cases := []string{
		"",
		"too_short.mp4",
		"seg_notatimestamp.mp4",
		"seg_20269999_xxxxxx.mp4", // bad date
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := parseSegmentTimestamp(in)
			if !got.IsZero() {
				t.Errorf("parseSegmentTimestamp(%q) = %v; want zero", in, got)
			}
		})
	}
}

func TestParseRingTimestamp_UTCIndependent(t *testing.T) {
	cases := []struct {
		hostTZ   string
		filename string
		wantUTC  time.Time
	}{
		{
			hostTZ:   "America/Chicago",
			filename: "ring_20260219_140530.mp4",
			wantUTC:  time.Date(2026, 2, 19, 14, 5, 30, 0, time.UTC),
		},
		{
			hostTZ:   "Asia/Tokyo",
			filename: "seg_20260219_140530.mp4",
			wantUTC:  time.Date(2026, 2, 19, 14, 5, 30, 0, time.UTC),
		},
	}
	for _, c := range cases {
		t.Run(c.hostTZ+"_"+c.filename, func(t *testing.T) {
			t.Setenv("TZ", c.hostTZ)
			got, err := parseRingTimestamp(c.filename)
			if err != nil {
				t.Fatalf("parseRingTimestamp(%q): %v", c.filename, err)
			}
			if !got.Equal(c.wantUTC) {
				t.Errorf("parseRingTimestamp(%q) under TZ=%s = %s; want %s",
					c.filename, c.hostTZ, got.UTC(), c.wantUTC)
			}
		})
	}
}
