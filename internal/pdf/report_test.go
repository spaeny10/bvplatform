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

func TestRenderComplianceReport_ContainsSiteName(t *testing.T) {
	data := makeTestData("Southgate Power")
	b, err := pdf.RenderComplianceReport(data)
	if err != nil {
		t.Fatalf("RenderComplianceReport: %v", err)
	}
	if !bytes.Contains(b, []byte("Southgate Power")) {
		t.Error("PDF bytes do not contain site name 'Southgate Power'")
	}
}

func TestRenderComplianceReport_ContainsReportID(t *testing.T) {
	data := makeTestData("Test Site")
	data.ReportID = "aaaabbbb-cccc-dddd-eeee-ffff00001111"
	b, err := pdf.RenderComplianceReport(data)
	if err != nil {
		t.Fatalf("RenderComplianceReport: %v", err)
	}
	if !bytes.Contains(b, []byte(data.ReportID)) {
		t.Error("PDF bytes do not contain the report_id UUID")
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
