// Package safety implements the Ironsight compliance rules engine.
//
// EvaluateCompliance is a pure function — no DB I/O, no side effects. The
// PPE worker calls it after fetching zones and rules from the DB, and before
// deciding whether to persist a violation finding.
//
// Spatial test: bounding-box centroid (single point) vs. zone polygon.
// Using centroid rather than full-polygon intersection is the industry
// convention for zone-trigger logic: it is fast, deterministic at zone edges,
// and avoids ambiguous partial-overlap results. Documented here so a future
// reviewer doesn't re-litigate the choice without context.
//
// C-03 (VLM validation) will expand this package. Do not move or rename
// EvaluateCompliance — C-03 imports the same function signature.
package safety

import (
	"ironsight/internal/ai"
	"ironsight/internal/database"
)

// Violation represents a single PPE compliance failure or no-go intrusion.
type Violation struct {
	Detection      ai.Detection
	ZoneID         string // uuid as string
	ZoneName       string
	MissingClasses []string // e.g. ["no-vest", "no-hat"] — empty for no_go violations
	RuleType       string   // "ppe_required" | "no_go"
}

// EvaluateCompliance filters YOLO PPE violation detections against the active
// compliance rules for a camera. It returns only the violations that are
// spatially inside a zone that has a matching compliance rule.
//
// Rules:
//   - A ppe_required rule fires when: detection centroid is inside the zone AND
//     the detection.Class matches one of rule.PPEClasses (or rule.PPEClasses
//     contains the class family). Missing means "this PPE item is absent."
//   - A no_go rule fires when: detection centroid is inside the zone, regardless
//     of PPE class — the person's presence itself is the violation.
//   - Disabled rules (enabled=false) are skipped. The caller (worker) already
//     filters disabled rules in ListZonesAndRulesForCamera, but we re-check here
//     for correctness when the slice is built in tests without the DB filter.
//   - A detection can match multiple rules (e.g. inside two overlapping zones).
//
// detections should be yolo.PPEViolations (persons with missing PPE) for
// ppe_required rules. For no_go rules, pass yolo.Detections (all detections
// including class=="person") — the caller decides which slice to use.
// In the worker, we pass PPEViolations for both rule types because the
// no_go rule fires on presence of any person; a PPEViolation where
// class=="person" would still satisfy the centroid-inside-zone check.
// The worker passes yolo.PPEViolations augmented by any "person" detections
// for no_go zones — see worker.go for the fan-out logic.
func EvaluateCompliance(
	detections []ai.Detection,
	zones []database.PPEZone,
	rules []database.ComplianceRule,
) []Violation {
	if len(rules) == 0 || len(zones) == 0 || len(detections) == 0 {
		return nil
	}

	// Build a quick lookup from zone ID to zone polygon.
	zoneByID := make(map[string]database.PPEZone, len(zones))
	for _, z := range zones {
		zoneByID[z.ID.String()] = z
	}

	var violations []Violation

	for _, detection := range detections {
		// Centroid of the detection bounding box in normalized coords.
		cx := detection.BBoxNorm.X1 + (detection.BBoxNorm.X2-detection.BBoxNorm.X1)/2
		cy := detection.BBoxNorm.Y1 + (detection.BBoxNorm.Y2-detection.BBoxNorm.Y1)/2

		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}

			zone, ok := zoneByID[rule.ZoneID.String()]
			if !ok {
				continue
			}
			if !zone.Enabled {
				continue
			}

			if len(zone.Region) < 3 {
				// A polygon needs at least 3 points to form a valid area.
				continue
			}

			if !pointInPolygon(cx, cy, zone.Region) {
				continue
			}

			// Detection centroid is inside the zone.
			switch rule.RuleType {
			case "ppe_required":
				// Fire when the detection class matches any of the required PPE
				// classes in the rule (the YOLO class represents a missing PPE item).
				if classMatchesAny(detection.Class, rule.PPEClasses) {
					violations = append(violations, Violation{
						Detection:      detection,
						ZoneID:         zone.ID.String(),
						ZoneName:       zone.Name,
						MissingClasses: rule.PPEClasses,
						RuleType:       "ppe_required",
					})
				}
			case "no_go":
				// Any person inside a no-go zone is a violation.
				violations = append(violations, Violation{
					Detection:      detection,
					ZoneID:         zone.ID.String(),
					ZoneName:       zone.Name,
					MissingClasses: nil,
					RuleType:       "no_go",
				})
			}
		}
	}

	return violations
}

// pointInPolygon implements the ray-casting algorithm for a point vs. a
// closed polygon. Returns true when (px, py) lies inside. Coordinates are
// normalized floats (0.0-1.0). Matches the TypeScript implementation in
// frontend/src/lib/vca-zones.ts for consistency.
//
// The result is deterministic for a point exactly on the boundary — it may
// return true or false depending on the winding; this is acceptable because
// the centroid of a well-sized detection bounding box is unlikely to land
// exactly on a zone edge in practice.
func pointInPolygon(px, py float64, polygon []database.Point) bool {
	inside := false
	n := len(polygon)
	j := n - 1
	for i := 0; i < n; i++ {
		xi, yi := polygon[i].X, polygon[i].Y
		xj, yj := polygon[j].X, polygon[j].Y
		intersect := (yi > py) != (yj > py) &&
			px < (xj-xi)*(py-yi)/(yj-yi)+xi
		if intersect {
			inside = !inside
		}
		j = i
	}
	return inside
}

// classMatchesAny returns true when cls matches any string in the required
// slice. Matching is exact-string equality on the full YOLO class name (e.g.
// "no-hat"). The ppe_classes field in compliance rules uses the same class
// names as the YOLO model output.
func classMatchesAny(cls string, required []string) bool {
	for _, r := range required {
		if cls == r {
			return true
		}
	}
	return false
}
