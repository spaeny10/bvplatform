package safety

import (
	"testing"

	"github.com/google/uuid"

	"ironsight/internal/ai"
	"ironsight/internal/database"
)

// helper zone builders

var (
	zoneID1 = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	zoneID2 = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ruleID1 = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	ruleID2 = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

// unitSquare is the polygon [(0,0),(1,0),(1,1),(0,1)].
var unitSquare = []database.Point{
	{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
}

// topHalf is the polygon for y in [0, 0.5].
var topHalf = []database.Point{
	{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 0.5}, {X: 0, Y: 0.5},
}

func ppeZone(id uuid.UUID, ztype string, region []database.Point) database.PPEZone {
	return database.PPEZone{
		ID:       id,
		ZoneType: ztype,
		Name:     "Test Zone " + id.String()[:4],
		Region:   region,
		Enabled:  true,
	}
}

func ppeRule(id, zoneID uuid.UUID, rtype string, classes []string) database.ComplianceRule {
	return database.ComplianceRule{
		ID:         id,
		ZoneID:     zoneID,
		RuleType:   rtype,
		PPEClasses: classes,
		Enabled:    true,
	}
}

func detection(class string, x1, y1, x2, y2 float64) ai.Detection {
	return ai.Detection{
		Class:      class,
		Confidence: 0.90,
		BBoxNorm:   ai.BBox{X1: x1, Y1: y1, X2: x2, Y2: y2},
	}
}

// centroidDetection creates a detection whose bounding box centroid is at (cx, cy).
func centroidDetection(class string, cx, cy float64) ai.Detection {
	// small 10x10% box centred at (cx, cy)
	return detection(class, cx-0.05, cy-0.05, cx+0.05, cy+0.05)
}

// TestEvaluateCompliance_CentroidInsideZone: detection centroid inside the
// zone + ppe_required rule requires the class → violation returned.
func TestEvaluateCompliance_CentroidInsideZone(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", unitSquare)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	dets := []ai.Detection{centroidDetection("no-hat", 0.5, 0.5)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(got))
	}
	if got[0].ZoneID != zoneID1.String() {
		t.Errorf("wrong zone ID: %s", got[0].ZoneID)
	}
	if got[0].RuleType != "ppe_required" {
		t.Errorf("wrong rule type: %s", got[0].RuleType)
	}
}

// TestEvaluateCompliance_CentroidOutsideZone: centroid outside zone → no violation.
func TestEvaluateCompliance_CentroidOutsideZone(t *testing.T) {
	// Zone is top half (y < 0.5); detection centroid at y=0.75 is outside.
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", topHalf)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	dets := []ai.Detection{centroidDetection("no-hat", 0.5, 0.75)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 0 {
		t.Errorf("expected 0 violations, got %d", len(got))
	}
}

// TestEvaluateCompliance_MultipleZones: two zones, detection only inside zone 1
// → only zone-1 violation returned.
func TestEvaluateCompliance_MultipleZones(t *testing.T) {
	zones := []database.PPEZone{
		ppeZone(zoneID1, "ppe_required", topHalf),                           // y in [0, 0.5]
		ppeZone(zoneID2, "ppe_required", []database.Point{                   // y in [0.6, 1.0]
			{X: 0, Y: 0.6}, {X: 1, Y: 0.6}, {X: 1, Y: 1.0}, {X: 0, Y: 1.0},
		}),
	}
	rules := []database.ComplianceRule{
		ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"}),
		ppeRule(ruleID2, zoneID2, "ppe_required", []string{"no-vest"}),
	}
	// Detection centroid at y=0.3 → inside topHalf, outside bottom zone.
	dets := []ai.Detection{centroidDetection("no-hat", 0.5, 0.3)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(got))
	}
	if got[0].ZoneID != zoneID1.String() {
		t.Errorf("expected zone1, got %s", got[0].ZoneID)
	}
}

// TestEvaluateCompliance_NoGoZone: detection inside no-go zone → violation
// regardless of PPE class.
func TestEvaluateCompliance_NoGoZone(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "no_go", unitSquare)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "no_go", nil)}
	// Detection class doesn't matter for no_go.
	dets := []ai.Detection{centroidDetection("person", 0.5, 0.5)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(got))
	}
	if got[0].RuleType != "no_go" {
		t.Errorf("wrong rule type: %s", got[0].RuleType)
	}
	if len(got[0].MissingClasses) != 0 {
		t.Errorf("no_go violation should have no MissingClasses")
	}
}

// TestEvaluateCompliance_DisabledRule: disabled rule must be skipped.
func TestEvaluateCompliance_DisabledRule(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", unitSquare)}
	rule := ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})
	rule.Enabled = false
	rules := []database.ComplianceRule{rule}
	dets := []ai.Detection{centroidDetection("no-hat", 0.5, 0.5)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 0 {
		t.Errorf("disabled rule should produce 0 violations, got %d", len(got))
	}
}

// TestEvaluateCompliance_EmptyZones: no zones/rules → no violations, no panic.
func TestEvaluateCompliance_EmptyZones(t *testing.T) {
	got := EvaluateCompliance(
		[]ai.Detection{centroidDetection("no-hat", 0.5, 0.5)},
		nil,
		nil,
	)
	if len(got) != 0 {
		t.Errorf("empty zones: expected 0 violations, got %d", len(got))
	}
}

// TestEvaluateCompliance_EmptyDetections: no detections → no violations, no panic.
func TestEvaluateCompliance_EmptyDetections(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", unitSquare)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	got := EvaluateCompliance(nil, zones, rules)
	if len(got) != 0 {
		t.Errorf("empty detections: expected 0 violations, got %d", len(got))
	}
}

// TestEvaluateCompliance_PointOnBoundary: centroid exactly on polygon edge
// must not panic; result is deterministic (whichever direction the ray-cast
// chooses is acceptable).
func TestEvaluateCompliance_PointOnBoundary(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", unitSquare)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	// Centroid exactly on the top edge of the unit square (y = 0, x = 0.5).
	dets := []ai.Detection{centroidDetection("no-hat", 0.5, 0.0)}

	// Must not panic; result may be true or false — we just check it runs.
	got := EvaluateCompliance(dets, zones, rules)
	_ = got // result is deterministic but we don't assert true/false here
}

// TestEvaluateCompliance_ClassMismatch: detection class not in rule's PPEClasses
// → no violation, even if centroid is inside the zone.
func TestEvaluateCompliance_ClassMismatch(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", unitSquare)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	// Detection class is "no-vest" — not in rule PPEClasses.
	dets := []ai.Detection{centroidDetection("no-vest", 0.5, 0.5)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 0 {
		t.Errorf("class mismatch: expected 0 violations, got %d", len(got))
	}
}

// TestEvaluateCompliance_TwoDetectionsOneInside: two detections, one inside
// and one outside → only one violation.
func TestEvaluateCompliance_TwoDetectionsOneInside(t *testing.T) {
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", topHalf)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	dets := []ai.Detection{
		centroidDetection("no-hat", 0.5, 0.2), // inside topHalf
		centroidDetection("no-hat", 0.5, 0.8), // outside topHalf
	}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 1 {
		t.Errorf("expected 1 violation, got %d", len(got))
	}
}

// TestEvaluateCompliance_TooFewZonePoints: a zone with fewer than 3 points
// must be skipped (cannot form a valid polygon).
func TestEvaluateCompliance_TooFewZonePoints(t *testing.T) {
	twoPoints := []database.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}
	zones := []database.PPEZone{ppeZone(zoneID1, "ppe_required", twoPoints)}
	rules := []database.ComplianceRule{ppeRule(ruleID1, zoneID1, "ppe_required", []string{"no-hat"})}
	dets := []ai.Detection{centroidDetection("no-hat", 0.5, 0.5)}

	got := EvaluateCompliance(dets, zones, rules)
	if len(got) != 0 {
		t.Errorf("zone with 2 points should produce 0 violations, got %d", len(got))
	}
}

// ── pointInPolygon tests ──────────────────────────────────────────────────────

func TestPointInPolygon_InsideSquare(t *testing.T) {
	poly := unitSquare
	if !pointInPolygon(0.5, 0.5, poly) {
		t.Error("(0.5, 0.5) should be inside unit square")
	}
}

func TestPointInPolygon_OutsideSquare(t *testing.T) {
	poly := unitSquare
	if pointInPolygon(1.5, 0.5, poly) {
		t.Error("(1.5, 0.5) should be outside unit square")
	}
}

func TestPointInPolygon_Triangle(t *testing.T) {
	// Right-angled triangle at (0,0), (1,0), (0,1)
	poly := []database.Point{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	if !pointInPolygon(0.1, 0.1, poly) {
		t.Error("(0.1, 0.1) should be inside triangle")
	}
	if pointInPolygon(0.9, 0.9, poly) {
		t.Error("(0.9, 0.9) should be outside triangle")
	}
}

func TestPointInPolygon_EmptyPolygon(t *testing.T) {
	if pointInPolygon(0.5, 0.5, nil) {
		t.Error("empty polygon should always return false")
	}
}
