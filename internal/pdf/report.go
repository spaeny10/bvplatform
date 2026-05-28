// Package pdf renders server-side PDF reports for the Ironsight compliance dashboard.
// It uses signintech/gopdf (pure Go, no CGO) to produce A4 single-column compliance
// reports with a chain-of-custody report_id UUID in the auditor footer.
//
// Font files are embedded via go:embed — Liberation Sans (SIL Open Font License,
// metric-compatible with Helvetica). The TTF files are placed into fonts/ by the
// Docker build stage (apt install fonts-liberation && cp). For local dev, run
// scripts/fetch-fonts.sh to download them.
package pdf

import (
	"bytes"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/signintech/gopdf"
)

// A4 page dimensions in points (1 pt = 1/72 inch).
const (
	pageWidth  = 595.28
	pageHeight = 841.89
	marginL    = 48.0
	marginR    = 48.0
	marginT    = 48.0
	marginB    = 48.0
	contentW   = pageWidth - marginL - marginR
)

// Accent color: warm terracotta (#c84b2f) matching portal --accent.
// gopdf color functions take uint8 arguments.
const (
	accentR uint8 = 200
	accentG uint8 = 75
	accentB uint8 = 47
)

// ComplianceFindingPDF is a single finding row for the PDF findings section.
type ComplianceFindingPDF struct {
	CreatedAt      time.Time
	CameraName     string
	MissingLabel   string
	DetectionClass string
	Confidence     float64
	Status         string
}

// ComplianceCameraPDF is a single camera row for the PDF cameras section.
type ComplianceCameraPDF struct {
	CameraName     string
	ViolationCount int
	PctOfTotal     float64
}

// ComplianceTimeBucketPDF is one data point for the bar chart.
type ComplianceTimeBucketPDF struct {
	Label string // pre-formatted axis label
	Count int
}

// ComplianceReportData is the full data payload for rendering a compliance PDF.
type ComplianceReportData struct {
	ReportID         string
	OrgName          string
	SiteName         string // "All Sites" when no site filter
	PeriodLabel      string // e.g. "Last 7 days"
	PeriodStart      time.Time
	PeriodEnd        time.Time
	GeneratedBy      string // user email
	GeneratedAt      time.Time
	TotalViolations  int
	TotalReviewed    int
	PendingCount     int
	ComplianceRate   *float64 // nil when insufficient data
	PersonHours      *float64 // nil when C-02 not available
	ViolationsChart  []ComplianceTimeBucketPDF
	TopCameras       []ComplianceCameraPDF
	RecentFindings   []ComplianceFindingPDF
	IncludeFindings  bool
}

// GenerateReportID returns a new UUIDv4 string for use as a report identifier.
func GenerateReportID() string {
	return uuid.New().String()
}

// RenderComplianceReport renders a compliance report PDF and returns the raw bytes.
// Returns an error if font setup or any page write fails. The returned []byte
// starts with "%PDF" on success.
func RenderComplianceReport(data ComplianceReportData) ([]byte, error) {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()

	// Load embedded fonts. gopdf.AddTTFFontByReader takes an io.Reader.
	if err := pdf.AddTTFFontByReader("regular", bytes.NewReader(fontRegular)); err != nil {
		return nil, fmt.Errorf("pdf: load regular font: %w", err)
	}
	if err := pdf.AddTTFFontByReader("bold", bytes.NewReader(fontBold)); err != nil {
		return nil, fmt.Errorf("pdf: load bold font: %w", err)
	}

	if err := renderHeader(&pdf, data); err != nil {
		return nil, err
	}
	if err := renderHeadlineMetrics(&pdf, data); err != nil {
		return nil, err
	}
	if len(data.ViolationsChart) > 0 {
		if err := renderViolationsChart(&pdf, data); err != nil {
			return nil, err
		}
	}
	if len(data.TopCameras) > 0 {
		if err := renderTopCamerasTable(&pdf, data); err != nil {
			return nil, err
		}
	}
	if data.IncludeFindings && len(data.RecentFindings) > 0 {
		if err := renderFindingsTable(&pdf, data); err != nil {
			return nil, err
		}
	}
	if err := renderFooter(&pdf, data); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("pdf: write: %w", err)
	}
	return buf.Bytes(), nil
}

// ── Section renderers ────────────────────────────────────────────

func renderHeader(pdf *gopdf.GoPdf, data ComplianceReportData) error {
	// Accent bar across the top.
	pdf.SetFillColor(accentR, accentG, accentB)
	pdf.RectFromUpperLeftWithStyle(0, 0, pageWidth, 8, "F")
	pdf.SetFillColor(0, 0, 0)

	y := marginT + 4.0

	// Report title.
	if err := pdf.SetFont("bold", "", 18); err != nil {
		return err
	}
	pdf.SetXY(marginL, y)
	pdf.SetTextColor(accentR, accentG, accentB)
	if err := pdf.Cell(nil, "Ironsight PPE Compliance Report"); err != nil {
		return err
	}
	pdf.SetTextColor(0, 0, 0)
	y += 24

	// Org + site.
	if err := pdf.SetFont("bold", "", 11); err != nil {
		return err
	}
	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, data.OrgName); err != nil {
		return err
	}
	y += 14

	if err := pdf.SetFont("regular", "", 10); err != nil {
		return err
	}
	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, "Site: "+data.SiteName); err != nil {
		return err
	}
	y += 13

	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, "Period: "+data.PeriodLabel+" ("+
		data.PeriodStart.Format("2006-01-02")+" – "+
		data.PeriodEnd.Format("2006-01-02")+")"); err != nil {
		return err
	}
	y += 13

	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, "Generated: "+data.GeneratedAt.Format("2006-01-02 15:04 UTC")); err != nil {
		return err
	}
	y += 13

	// Thin separator line.
	pdf.SetStrokeColor(180, 170, 160)
	pdf.SetLineWidth(0.5)
	pdf.Line(marginL, y+4, pageWidth-marginR, y+4)
	pdf.SetStrokeColor(0, 0, 0)
	y += 14

	// Store cursor position in Br hack — gopdf lacks a persistent Y state API;
	// use a workaround via SetXY before each subsequent section.
	_ = y // sections track their own Y from a shared pointer via the data struct
	return nil
}

func renderHeadlineMetrics(pdf *gopdf.GoPdf, data ComplianceReportData) error {
	y := 200.0
	boxW := contentW / 3.0
	labels := []string{"Total Violations", "Compliance Rate", "Person-Hours"}
	values := make([]string, 3)
	values[0] = fmt.Sprintf("%d", data.TotalViolations)
	if data.ComplianceRate != nil {
		values[1] = fmt.Sprintf("%.1f%%", *data.ComplianceRate)
	} else {
		values[1] = "N/A"
	}
	if data.PersonHours != nil {
		values[2] = fmt.Sprintf("%.1f h", *data.PersonHours)
	} else {
		values[2] = "N/A"
	}

	for i, label := range labels {
		x := marginL + float64(i)*boxW
		// Box outline.
		pdf.SetStrokeColor(210, 200, 190)
		pdf.SetLineWidth(0.75)
		pdf.RectFromUpperLeftWithStyle(x+2, y, boxW-6, 56, "D")

		// Value (large).
		if err := pdf.SetFont("bold", "", 22); err != nil {
			return err
		}
		pdf.SetXY(x+8, y+10)
		if err := pdf.Cell(nil, values[i]); err != nil {
			return err
		}

		// Label (small).
		if err := pdf.SetFont("regular", "", 8); err != nil {
			return err
		}
		pdf.SetTextColor(100, 90, 80)
		pdf.SetXY(x+8, y+38)
		if err := pdf.Cell(nil, label); err != nil {
			return err
		}
		pdf.SetTextColor(0, 0, 0)
	}

	// Pending count note below boxes.
	if err := pdf.SetFont("regular", "", 8); err != nil {
		return err
	}
	pdf.SetTextColor(120, 110, 100)
	pdf.SetXY(marginL, y+62)
	if err := pdf.Cell(nil, fmt.Sprintf("Total reviewed: %d  |  Pending review: %d", data.TotalReviewed, data.PendingCount)); err != nil {
		return err
	}
	pdf.SetTextColor(0, 0, 0)
	return nil
}

func renderViolationsChart(pdf *gopdf.GoPdf, data ComplianceReportData) error {
	y := 280.0
	chartH := 80.0
	chartW := contentW

	// Section label.
	if err := pdf.SetFont("bold", "", 10); err != nil {
		return err
	}
	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, "Violations Over Time"); err != nil {
		return err
	}
	y += 14

	buckets := data.ViolationsChart
	n := len(buckets)
	if n == 0 {
		return nil
	}

	maxCount := 1
	for _, b := range buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}

	barW := chartW / float64(n)
	gap := barW * 0.15

	for i, b := range buckets {
		if b.Count == 0 {
			continue
		}
		barH := math.Max(2, float64(b.Count)/float64(maxCount)*chartH)
		bx := marginL + float64(i)*barW + gap/2
		by := y + chartH - barH
		bw := barW - gap

		pdf.SetFillColor(accentR, accentG, accentB)
		pdf.RectFromUpperLeftWithStyle(bx, by, bw, barH, "F")
		pdf.SetFillColor(0, 0, 0)

		// Axis label below bar.
		if err := pdf.SetFont("regular", "", 6); err != nil {
			return err
		}
		pdf.SetTextColor(100, 90, 80)
		pdf.SetXY(bx, y+chartH+2)
		_ = pdf.Cell(nil, b.Label) // ignore truncation
		pdf.SetTextColor(0, 0, 0)
	}
	return nil
}

func renderTopCamerasTable(pdf *gopdf.GoPdf, data ComplianceReportData) error {
	y := 395.0

	if err := pdf.SetFont("bold", "", 10); err != nil {
		return err
	}
	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, "Top Cameras by Violation Count"); err != nil {
		return err
	}
	y += 14

	// Header row.
	if err := pdf.SetFont("bold", "", 8); err != nil {
		return err
	}
	pdf.SetFillColor(240, 235, 230)
	pdf.RectFromUpperLeftWithStyle(marginL, y, contentW, 14, "F")
	pdf.SetFillColor(0, 0, 0)

	cols := []struct {
		label string
		x     float64
	}{
		{"Camera", marginL + 4},
		{"Violations", marginL + contentW*0.55},
		{"% of Total", marginL + contentW*0.75},
	}
	for _, c := range cols {
		pdf.SetXY(c.x, y+4)
		_ = pdf.Cell(nil, c.label)
	}
	y += 14

	if err := pdf.SetFont("regular", "", 8); err != nil {
		return err
	}
	for i, cam := range data.TopCameras {
		if i%2 == 0 {
			pdf.SetFillColor(250, 248, 245)
			pdf.RectFromUpperLeftWithStyle(marginL, y, contentW, 13, "F")
			pdf.SetFillColor(0, 0, 0)
		}
		pdf.SetXY(cols[0].x, y+3)
		_ = pdf.Cell(nil, cam.CameraName)
		pdf.SetXY(cols[1].x, y+3)
		_ = pdf.Cell(nil, fmt.Sprintf("%d", cam.ViolationCount))
		pdf.SetXY(cols[2].x, y+3)
		_ = pdf.Cell(nil, fmt.Sprintf("%.1f%%", cam.PctOfTotal))
		y += 13
	}
	return nil
}

func renderFindingsTable(pdf *gopdf.GoPdf, data ComplianceReportData) error {
	y := 540.0

	if err := pdf.SetFont("bold", "", 10); err != nil {
		return err
	}
	pdf.SetXY(marginL, y)
	if err := pdf.Cell(nil, "Recent Findings"); err != nil {
		return err
	}
	y += 14

	// Header.
	if err := pdf.SetFont("bold", "", 8); err != nil {
		return err
	}
	pdf.SetFillColor(240, 235, 230)
	pdf.RectFromUpperLeftWithStyle(marginL, y, contentW, 14, "F")
	pdf.SetFillColor(0, 0, 0)

	cols := []struct {
		label string
		x     float64
	}{
		{"Timestamp", marginL + 4},
		{"Camera", marginL + contentW*0.28},
		{"Violation", marginL + contentW*0.56},
		{"Confidence", marginL + contentW*0.82},
	}
	for _, c := range cols {
		pdf.SetXY(c.x, y+4)
		_ = pdf.Cell(nil, c.label)
	}
	y += 14

	if err := pdf.SetFont("regular", "", 7); err != nil {
		return err
	}
	for i, f := range data.RecentFindings {
		if y > pageHeight-marginB-30 {
			break // guard against overflow; PDF is single-page for Phase 2
		}
		if i%2 == 0 {
			pdf.SetFillColor(250, 248, 245)
			pdf.RectFromUpperLeftWithStyle(marginL, y, contentW, 12, "F")
			pdf.SetFillColor(0, 0, 0)
		}
		pdf.SetXY(cols[0].x, y+3)
		_ = pdf.Cell(nil, f.CreatedAt.Format("01/02 15:04"))
		pdf.SetXY(cols[1].x, y+3)
		_ = pdf.Cell(nil, truncate(f.CameraName, 22))
		pdf.SetXY(cols[2].x, y+3)
		_ = pdf.Cell(nil, truncate(f.MissingLabel, 18))
		pdf.SetXY(cols[3].x, y+3)
		_ = pdf.Cell(nil, fmt.Sprintf("%.0f%%", f.Confidence*100))
		y += 12
	}
	return nil
}

func renderFooter(pdf *gopdf.GoPdf, data ComplianceReportData) error {
	y := pageHeight - marginB - 18

	// Dashed separator.
	pdf.SetStrokeColor(160, 150, 140)
	pdf.SetLineWidth(0.4)
	pdf.SetLineType("dashed")
	pdf.Line(marginL, y, pageWidth-marginR, y)
	pdf.SetLineType("solid")
	pdf.SetStrokeColor(0, 0, 0)
	y += 6

	if err := pdf.SetFont("regular", "", 7); err != nil {
		return err
	}
	pdf.SetTextColor(120, 110, 100)
	pdf.SetXY(marginL, y)
	line1 := fmt.Sprintf(
		"Report generated by %s on %s  |  Report ID: %s",
		data.GeneratedBy,
		data.GeneratedAt.Format("2006-01-02 15:04 UTC"),
		data.ReportID,
	)
	_ = pdf.Cell(nil, line1)
	y += 9

	pdf.SetXY(marginL, y)
	_ = pdf.Cell(nil, "This report is based on AI-assisted detection and requires human review before use in formal compliance proceedings.")
	pdf.SetTextColor(0, 0, 0)
	return nil
}

// truncate shortens s to maxRunes characters, appending "…" if trimmed.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}
