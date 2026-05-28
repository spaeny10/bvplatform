package reanalysis_test

import (
	"encoding/json"
	"math"
	"testing"

	"ironsight/internal/reanalysis"
)

// ─────────────────────────────────────────────────────────────────────────────
// ParseRuleSet
// ─────────────────────────────────────────────────────────────────────────────

func TestParseRuleSet_Empty(t *testing.T) {
	for _, raw := range []string{"", "{}", "null"} {
		rs, err := reanalysis.ParseRuleSet(json.RawMessage(raw))
		if err != nil {
			t.Errorf("ParseRuleSet(%q) unexpected error: %v", raw, err)
		}
		if rs.ConfidenceThreshold != 0 {
			t.Errorf("want zero RuleSet, got non-zero ConfidenceThreshold")
		}
	}
}

func TestParseRuleSet_Full(t *testing.T) {
	raw := `{
		"confidence_threshold": 0.6,
		"class_remap": {"helmet_no": "ppe_violation", "person": null},
		"min_bbox_area": 0.01,
		"rules": [{"class": "ppe_violation", "min_confidence": 0.7}]
	}`
	rs, err := reanalysis.ParseRuleSet(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("ParseRuleSet: %v", err)
	}
	if rs.ConfidenceThreshold != 0.6 {
		t.Errorf("ConfidenceThreshold: want 0.6, got %v", rs.ConfidenceThreshold)
	}
	if len(rs.ClassRemap) != 2 {
		t.Errorf("ClassRemap len: want 2, got %d", len(rs.ClassRemap))
	}
	remapped := rs.ClassRemap["helmet_no"]
	if remapped == nil || *remapped != "ppe_violation" {
		t.Errorf("ClassRemap[helmet_no]: want 'ppe_violation', got %v", remapped)
	}
	if rs.ClassRemap["person"] != nil {
		t.Errorf("ClassRemap[person]: want nil, got %v", rs.ClassRemap["person"])
	}
	if rs.MinBBoxArea != 0.01 {
		t.Errorf("MinBBoxArea: want 0.01, got %v", rs.MinBBoxArea)
	}
	if len(rs.Rules) != 1 || rs.Rules[0].MinConfidence != 0.7 {
		t.Errorf("Rules: unexpected %+v", rs.Rules)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// BBox helpers
// ─────────────────────────────────────────────────────────────────────────────

func TestBBoxArea(t *testing.T) {
	cases := []struct {
		bb   reanalysis.BBox
		want float32
	}{
		{reanalysis.BBox{X1: 0, Y1: 0, X2: 0.5, Y2: 0.5}, 0.25},
		{reanalysis.BBox{X1: 0.1, Y1: 0.2, X2: 0.4, Y2: 0.6}, 0.12},
		{reanalysis.BBox{X1: 0.5, Y1: 0.5, X2: 0.3, Y2: 0.3}, 0}, // inverted
	}
	for _, tc := range cases {
		got := tc.bb.Area()
		// Use approximate compare for float32.
		if math.Abs(float64(got-tc.want)) > 1e-5 {
			t.Errorf("BBox%+v.Area() = %v, want %v", tc.bb, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ApplyRuleSet — core rule engine
// ─────────────────────────────────────────────────────────────────────────────

func noopBBox() json.RawMessage {
	return json.RawMessage(`{"x1":0.1,"y1":0.1,"x2":0.5,"y2":0.5}`)
}

func mustRS(t *testing.T, raw string) reanalysis.RuleSet {
	t.Helper()
	rs, err := reanalysis.ParseRuleSet(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("mustRS: %v", err)
	}
	return rs
}

func TestApplyRuleSet_Unchanged(t *testing.T) {
	// Empty ruleset → no changes.
	rs := reanalysis.RuleSet{}
	out := reanalysis.ApplyRuleSet("no-hardhat", 0.85, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeUnchanged {
		t.Errorf("want Unchanged, got %v (reason: %q)", out.Kind, out.Reason)
	}
}

func TestApplyRuleSet_DropByConfidence(t *testing.T) {
	rs := mustRS(t, `{"confidence_threshold": 0.7}`)
	out := reanalysis.ApplyRuleSet("no-hardhat", 0.65, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeDropped {
		t.Errorf("want Dropped, got %v", out.Kind)
	}
}

func TestApplyRuleSet_PassConfidence(t *testing.T) {
	rs := mustRS(t, `{"confidence_threshold": 0.7}`)
	out := reanalysis.ApplyRuleSet("no-hardhat", 0.75, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeUnchanged {
		t.Errorf("want Unchanged (passes global threshold), got %v", out.Kind)
	}
}

func TestApplyRuleSet_ClassRemap_Rename(t *testing.T) {
	rs := mustRS(t, `{"class_remap": {"helmet_no": "ppe_violation"}}`)
	out := reanalysis.ApplyRuleSet("helmet_no", 0.9, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeChanged {
		t.Errorf("want Changed, got %v", out.Kind)
	}
	if out.Class != "ppe_violation" {
		t.Errorf("want class 'ppe_violation', got %q", out.Class)
	}
}

func TestApplyRuleSet_ClassRemap_Drop(t *testing.T) {
	rs := mustRS(t, `{"class_remap": {"person": null}}`)
	out := reanalysis.ApplyRuleSet("person", 0.9, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeDropped {
		t.Errorf("want Dropped (null remap), got %v", out.Kind)
	}
}

func TestApplyRuleSet_ClassRemap_Unknown(t *testing.T) {
	// Class not in remap map → unchanged.
	rs := mustRS(t, `{"class_remap": {"helmet_no": "ppe_violation"}}`)
	out := reanalysis.ApplyRuleSet("no-vest", 0.9, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeUnchanged {
		t.Errorf("want Unchanged (class not in remap), got %v", out.Kind)
	}
}

func TestApplyRuleSet_MinBBoxArea_Drop(t *testing.T) {
	// Bbox area = (0.5-0.1)*(0.5-0.1) = 0.16; require 0.3 → drop.
	rs := mustRS(t, `{"min_bbox_area": 0.3}`)
	out := reanalysis.ApplyRuleSet("no-hardhat", 0.9, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeDropped {
		t.Errorf("want Dropped (small bbox), got %v", out.Kind)
	}
}

func TestApplyRuleSet_MinBBoxArea_Pass(t *testing.T) {
	// Bbox area 0.16; require 0.1 → pass.
	rs := mustRS(t, `{"min_bbox_area": 0.1}`)
	out := reanalysis.ApplyRuleSet("no-hardhat", 0.9, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeUnchanged {
		t.Errorf("want Unchanged (bbox large enough), got %v", out.Kind)
	}
}

func TestApplyRuleSet_PerClassRule_Drop(t *testing.T) {
	// Global threshold 0.6, per-class rule requires 0.8 for ppe_violation.
	rs := mustRS(t, `{
		"confidence_threshold": 0.6,
		"class_remap": {"helmet_no": "ppe_violation"},
		"rules": [{"class": "ppe_violation", "min_confidence": 0.8}]
	}`)
	// confidence 0.75 passes global (0.6) but fails per-class (0.8).
	out := reanalysis.ApplyRuleSet("helmet_no", 0.75, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeDropped {
		t.Errorf("want Dropped by per-class rule, got %v (reason: %q)", out.Kind, out.Reason)
	}
}

func TestApplyRuleSet_PerClassRule_Pass(t *testing.T) {
	rs := mustRS(t, `{
		"confidence_threshold": 0.6,
		"class_remap": {"helmet_no": "ppe_violation"},
		"rules": [{"class": "ppe_violation", "min_confidence": 0.8}]
	}`)
	// confidence 0.85 passes both global and per-class.
	out := reanalysis.ApplyRuleSet("helmet_no", 0.85, noopBBox(), rs)
	if out.Kind != reanalysis.OutcomeChanged {
		t.Errorf("want Changed (class remapped, confidence ok), got %v", out.Kind)
	}
}

func TestApplyRuleSet_EmptyBBox_WithMinArea(t *testing.T) {
	// Empty bbox → area 0 → drop when min_bbox_area > 0.
	rs := mustRS(t, `{"min_bbox_area": 0.01}`)
	out := reanalysis.ApplyRuleSet("no-vest", 0.9, json.RawMessage(`{}`), rs)
	if out.Kind != reanalysis.OutcomeDropped {
		t.Errorf("want Dropped (empty bbox), got %v", out.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ComputeFPRate
// ─────────────────────────────────────────────────────────────────────────────

func TestComputeFPRate_NoReviews(t *testing.T) {
	s := reanalysis.ComputeFPRate(nil)
	if s.IsAvailable() {
		t.Error("want not available for empty reviews")
	}
	if !math.IsNaN(s.Rate) {
		t.Errorf("want NaN rate for empty reviews, got %v", s.Rate)
	}
}

func TestComputeFPRate_Mixed(t *testing.T) {
	verdicts := []string{
		"false_positive", "false_positive", "confirmed",
		"uncertain", "confirmed",
	}
	s := reanalysis.ComputeFPRate(verdicts)
	if !s.IsAvailable() {
		t.Error("want available")
	}
	if s.TotalReviewed != 5 {
		t.Errorf("TotalReviewed: want 5, got %d", s.TotalReviewed)
	}
	if s.FalsePositiveCount != 2 {
		t.Errorf("FalsePositiveCount: want 2, got %d", s.FalsePositiveCount)
	}
	wantRate := 2.0 / 5.0
	if math.Abs(s.Rate-wantRate) > 1e-9 {
		t.Errorf("Rate: want %v, got %v", wantRate, s.Rate)
	}
}

func TestComputeFPRate_AllConfirmed(t *testing.T) {
	s := reanalysis.ComputeFPRate([]string{"confirmed", "confirmed"})
	if s.Rate != 0.0 {
		t.Errorf("want 0.0 rate, got %v", s.Rate)
	}
}
