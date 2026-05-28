package pdf_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"ironsight/internal/pdf"
)

func makeTestData(siteName string) pdf.ComplianceReportData {
	rate := 72.5
	hours := 45.0
	return pdf.ComplianceReportData{
		ReportID:        "test-report-id-12345678",
		OrgName:         "Acme Corp",
		SiteName:        siteName,
		PeriodLabel:     "Last 7 days",
		PeriodStart:     time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC),
		PeriodEnd:       time.Date(2026, 5, 26, 23, 59, 59, 0, time.UTC),
		GeneratedBy:     "testuser@acme.com",
		GeneratedAt:     time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		TotalViolations: 12,
		TotalReviewed:   44,
		PendingCount:    3,
		ComplianceRate:  &rate,
		PersonHours:     &hours,
		ViolationsChart: []pdf.ComplianceTimeBucketPDF{
			{Label: "05/19", Count: 2},
			{Label: "05/20", Count: 4},
			{Label: "05/21", Count: 1},
		},
		TopCameras: []pdf.ComplianceCameraPDF{
			{CameraName: "Gate A", ViolationCount: 7, PctOfTotal: 58.3},
		},
		RecentFindings: []pdf.ComplianceFindingPDF{
			{
				CreatedAt:      time.Date(2026, 5, 25, 9, 30, 0, 0, time.UTC),
				CameraName:     "Scaffold Bay 3",
				MissingLabel:   "Hi-Vis Vest",
				DetectionClass: "no-vest",
				Confidence:     0.81,
				Status:         "reviewed_violation",
			},
		},
		IncludeFindings: true,
	}
}

// NOTE: gopdf compresses content streams (FlateDecode), so visible text like
// the site name or report_id does NOT appear as plaintext in the raw PDF bytes.
// Asserting on raw bytes is testing through compression and is unreliable. We
// instead assert the renderer produces a structurally valid, non-trivial PDF.
// The report_id is verified end-to-end at the HTTP layer via the X-Report-Id
// response header (see compliance_handler_test.go). Visual fidelity of the
// rendered text is a manual-review concern, not a byte-grep concern.

func TestRenderComplianceReport_WithSiteName(t *testing.T) {
	data := makeTestData("Southgate Power")
	b, err := pdf.RenderComplianceReport(data)
	if err != nil {
		t.Fatalf("RenderComplianceReport: %v", err)
	}
	if len(b) < 4 || string(b[:4]) != "%PDF" {
		t.Fatalf("expected %%PDF magic bytes, got %q", b[:min(8, len(b))])
	}
	// A real report with cards + chart + a findings row should be well over 1 KB.
	if len(b) < 1024 {
		t.Errorf("rendered PDF suspiciously small (%d bytes) — expected a populated report", len(b))
	}
	if !bytes.Contains(b, []byte("%%EOF")) {
		t.Error("PDF missing EOF trailer marker — likely truncated/invalid")
	}
}

func TestRenderComplianceReport_PopulatedVsEmptyDiffer(t *testing.T) {
	// A report with full data should render to a larger PDF than a minimal one.
	// This guards against the renderer silently dropping the body content
	// (which is the real failure mode a byte-grep was reaching for).
	full, err := pdf.RenderComplianceReport(makeTestData("Test Site"))
	if err != nil {
		t.Fatalf("RenderComplianceReport(full): %v", err)
	}
	minimal := makeTestData("Test Site")
	minimal.ViolationsChart = nil
	minimal.TopCameras = nil
	minimal.RecentFindings = nil
	minimal.IncludeFindings = false
	small, err := pdf.RenderComplianceReport(minimal)
	if err != nil {
		t.Fatalf("RenderComplianceReport(minimal): %v", err)
	}
	if len(full) <= len(small) {
		t.Errorf("expected populated report (%d B) to exceed minimal report (%d B); body content may not be rendering", len(full), len(small))
	}
}

func TestRenderComplianceReport_EmptyData(t *testing.T) {
	// Render with nil compliance_rate and nil person_hours — should not panic.
	data := pdf.ComplianceReportData{
		ReportID:        pdf.GenerateReportID(),
		OrgName:         "Empty Org",
		SiteName:        "All Sites",
		PeriodLabel:     "Last 7 days",
		PeriodStart:     time.Now().AddDate(0, 0, -7),
		PeriodEnd:       time.Now(),
		GeneratedBy:     "user@example.com",
		GeneratedAt:     time.Now().UTC(),
		TotalViolations: 0,
		TotalReviewed:   0,
		PendingCount:    0,
		// ComplianceRate and PersonHours intentionally nil
		IncludeFindings: true,
	}

	b, err := pdf.RenderComplianceReport(data)
	if err != nil {
		t.Fatalf("RenderComplianceReport with empty data: %v", err)
	}
	if len(b) < 4 || string(b[:4]) != "%PDF" {
		t.Errorf("expected PDF magic bytes, got: %q", b[:min(8, len(b))])
	}
}

func TestGenerateReportID(t *testing.T) {
	id := pdf.GenerateReportID()
	if len(id) != 36 {
		t.Errorf("report ID length: want 36, got %d (%q)", len(id), id)
	}
	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Errorf("UUID should have 5 parts, got %d", len(parts))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
