// Package reanalysis implements P4-SCHEMA-06 re-analysis logic.
//
// "Re-analysis" here means re-processing existing detection rows through the
// rule set encoded in a new model_versions.params JSONB field.  It does NOT
// mean re-running YOLO or Qwen against raw video frames.  No changes to
// services/yolo/ or services/qwen/ are required or intended.
//
// Rule evaluation is pure-Go (no DB, no I/O) so it is fully unit-testable
// without a running database.
package reanalysis

import (
	"encoding/json"
	"fmt"
	"math"
)

// ─────────────────────────────────────────────────────────────────────────────
// RuleSet — the model_versions.params payload that drives re-analysis
// ─────────────────────────────────────────────────────────────────────────────

// RuleSet is the structured form of model_versions.params used by the
// re-analysis engine.  All fields are optional; an empty RuleSet passes
// every detection through unchanged (no-op model).
//
// Suggested schema for the JSONB stored in model_versions.params:
//
//	{
//	  "confidence_threshold": 0.6,
//	  "class_remap": {"helmet_no": "ppe_violation", "person": null},
//	  "min_bbox_area":        1600,
//	  "rules": [{"class": "ppe_violation", "min_confidence": 0.7}]
//	}
type RuleSet struct {
	// ConfidenceThreshold is the global minimum confidence.  Detections below
	// this value are "dropped" (emitted as filtered_out).
	ConfidenceThreshold float32 `json:"confidence_threshold"`

	// ClassRemap maps old detection_class values to new ones.
	//   - Missing key:    class is kept as-is.
	//   - Non-null value: class is renamed.
	//   - Null value:     detection is dropped (emitted as filtered_out).
	ClassRemap map[string]*string `json:"class_remap"`

	// MinBBoxArea is the minimum bounding-box pixel area (width*height in
	// normalised 0-1 coordinates, so 1.0 means 100% of frame area).  Value
	// is compared as float32 area = (x2-x1)*(y2-y1).  Detections with a
	// smaller bbox are dropped.
	MinBBoxArea float32 `json:"min_bbox_area"`

	// Rules are per-class overrides applied after the global threshold and
	// class_remap.  Each rule matches by (possibly-remapped) class and can
	// require a higher minimum confidence than the global threshold.
	Rules []ClassRule `json:"rules"`
}

// ClassRule is a per-class confidence override inside RuleSet.Rules.
type ClassRule struct {
	// Class is the (possibly-remapped) detection class this rule applies to.
	Class string `json:"class"`

	// MinConfidence overrides the global ConfidenceThreshold for this class.
	// Only applied when MinConfidence > global ConfidenceThreshold.
	MinConfidence float32 `json:"min_confidence"`
}

// ParseRuleSet deserialises model_versions.params into a RuleSet.
// An empty or nil payload returns a zero RuleSet (pass-through).
func ParseRuleSet(params json.RawMessage) (RuleSet, error) {
	var rs RuleSet
	if len(params) == 0 || string(params) == "{}" || string(params) == "null" {
		return rs, nil
	}
	if err := json.Unmarshal(params, &rs); err != nil {
		return rs, fmt.Errorf("ParseRuleSet: %w", err)
	}
	return rs, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// BBox helper
// ─────────────────────────────────────────────────────────────────────────────

// BBox is the normalised bounding box from detection.bounding_box JSONB.
type BBox struct {
	X1 float32 `json:"x1"`
	Y1 float32 `json:"y1"`
	X2 float32 `json:"x2"`
	Y2 float32 `json:"y2"`
}

// Area returns the normalised bounding-box area ((x2-x1)*(y2-y1)).
// Returns 0 for degenerate boxes (collapsed or inverted).
func (b BBox) Area() float32 {
	w := b.X2 - b.X1
	h := b.Y2 - b.Y1
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// ParseBBox deserialises a raw JSONB bounding_box into BBox.
// An empty or null payload returns a zero BBox (area 0).
func ParseBBox(raw json.RawMessage) (BBox, error) {
	var bb BBox
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return bb, nil
	}
	if err := json.Unmarshal(raw, &bb); err != nil {
		return bb, fmt.Errorf("ParseBBox: %w", err)
	}
	return bb, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Outcome — result of applying a RuleSet to one detection
// ─────────────────────────────────────────────────────────────────────────────

// OutcomeKind indicates what the rule engine decided for a detection.
type OutcomeKind int

const (
	// OutcomeUnchanged means the new rule set produces an identical result
	// to the old detection.  No new row should be emitted.
	OutcomeUnchanged OutcomeKind = iota

	// OutcomeChanged means the new rule set produced a different class or
	// other change.  A new supersede row should be emitted.
	OutcomeChanged

	// OutcomeDropped means the detection no longer meets the new model's
	// criteria.  A supersede row with detection_class="filtered_out" should
	// be emitted to preserve the audit trail.
	OutcomeDropped
)

// Outcome is the result returned by ApplyRuleSet.
type Outcome struct {
	Kind  OutcomeKind
	Class string  // final class after remap (only meaningful for OutcomeChanged)
	Reason string // human-readable reason for drop or change (for logging)
}

// ─────────────────────────────────────────────────────────────────────────────
// ApplyRuleSet — the core pure function
// ─────────────────────────────────────────────────────────────────────────────

// ApplyRuleSet evaluates a single existing detection against the new model's
// RuleSet and returns an Outcome indicating what action the caller should take.
//
// Parameters:
//   - oldClass:      detection.detection_class of the existing row.
//   - confidence:    detection.confidence of the existing row.
//   - bboxRaw:       detection.bounding_box JSONB of the existing row.
//   - rs:            the new model_version's parsed RuleSet.
//
// Evaluation order (mirrors the spec in §3 of the P4-SCHEMA-06 task):
//  1. Global confidence threshold.
//  2. Class remap (may drop the detection if remap value is null).
//  3. Min bounding-box area (if MinBBoxArea is set and > 0).
//  4. Per-class rule overrides (ClassRule.MinConfidence).
//  5. If nothing changed, return OutcomeUnchanged.
func ApplyRuleSet(oldClass string, confidence float32, bboxRaw json.RawMessage, rs RuleSet) Outcome {
	// ── Step 1: global confidence threshold ──────────────────────────────────
	if rs.ConfidenceThreshold > 0 && confidence < rs.ConfidenceThreshold {
		return Outcome{
			Kind:   OutcomeDropped,
			Reason: fmt.Sprintf("confidence %.3f below global threshold %.3f", confidence, rs.ConfidenceThreshold),
		}
	}

	// ── Step 2: class remap ──────────────────────────────────────────────────
	newClass := oldClass
	classChanged := false
	if rs.ClassRemap != nil {
		if remapped, ok := rs.ClassRemap[oldClass]; ok {
			if remapped == nil {
				// Explicit null → drop.
				return Outcome{
					Kind:   OutcomeDropped,
					Reason: fmt.Sprintf("class %q mapped to null (drop) in class_remap", oldClass),
				}
			}
			if *remapped != oldClass {
				newClass = *remapped
				classChanged = true
			}
		}
	}

	// ── Step 3: min bounding-box area ───────────────────────────────────────
	if rs.MinBBoxArea > 0 {
		bb, err := ParseBBox(bboxRaw)
		if err != nil {
			// Malformed bbox — treat as zero area → drop.
			return Outcome{
				Kind:   OutcomeDropped,
				Reason: fmt.Sprintf("bounding_box parse error: %v", err),
			}
		}
		if bb.Area() < rs.MinBBoxArea {
			return Outcome{
				Kind:   OutcomeDropped,
				Reason: fmt.Sprintf("bbox area %.6f below min_bbox_area %.6f", bb.Area(), rs.MinBBoxArea),
			}
		}
	}

	// ── Step 4: per-class rule overrides ────────────────────────────────────
	for _, rule := range rs.Rules {
		if rule.Class != newClass {
			continue
		}
		effective := rule.MinConfidence
		if effective <= rs.ConfidenceThreshold {
			effective = rs.ConfidenceThreshold
		}
		if confidence < effective {
			return Outcome{
				Kind:   OutcomeDropped,
				Reason: fmt.Sprintf("confidence %.3f below per-class min %.3f for class %q", confidence, effective, newClass),
			}
		}
		// A per-class rule match that doesn't drop is informational only;
		// it does not by itself cause a "changed" outcome.
		break
	}

	// ── Step 5: unchanged? ──────────────────────────────────────────────────
	if !classChanged {
		return Outcome{Kind: OutcomeUnchanged}
	}
	return Outcome{
		Kind:   OutcomeChanged,
		Class:  newClass,
		Reason: fmt.Sprintf("class remapped %q → %q", oldClass, newClass),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// FalsePositiveStats
// ─────────────────────────────────────────────────────────────────────────────

// FalsePositiveStats holds false-positive-rate measurements for a model.
type FalsePositiveStats struct {
	// Rate is false_positives / total_reviewed.  NaN when no reviews exist.
	Rate float64

	// FalsePositiveCount is the number of reviewed detections marked false_positive.
	FalsePositiveCount int

	// TotalReviewed is the number of detections with at least one review verdict.
	TotalReviewed int
}

// IsAvailable returns true when ground-truth review data existed to compute the
// false-positive rate.
func (s FalsePositiveStats) IsAvailable() bool {
	return s.TotalReviewed > 0
}

// ComputeFPRate takes flat arrays of verdicts (one entry per reviewed detection,
// each string is the most recent verdict: "confirmed", "false_positive",
// "uncertain") and returns FalsePositiveStats.
//
// Only "false_positive" verdicts count against the numerator; "confirmed" and
// "uncertain" are counted in the denominator.
func ComputeFPRate(verdicts []string) FalsePositiveStats {
	if len(verdicts) == 0 {
		return FalsePositiveStats{Rate: math.NaN()}
	}
	var fps int
	for _, v := range verdicts {
		if v == "false_positive" {
			fps++
		}
	}
	return FalsePositiveStats{
		Rate:               float64(fps) / float64(len(verdicts)),
		FalsePositiveCount: fps,
		TotalReviewed:      len(verdicts),
	}
}
