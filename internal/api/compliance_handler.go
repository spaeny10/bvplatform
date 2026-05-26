package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"ironsight/internal/database"
	"ironsight/internal/pdf"
)

// parsePeriod interprets the ?period, ?start_date, ?end_date query params and
// returns the start/end window, a human-readable label, and the date_trunc
// unit for bucketing ('hour' for today, 'day' for all other periods).
func parsePeriod(r *http.Request) (start, end time.Time, label, truncUnit string, err error) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}
	now := time.Now().UTC()
	truncUnit = "day"

	switch period {
	case "today":
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end = now
		label = "Today"
		truncUnit = "hour"
	case "week":
		end = now
		start = now.AddDate(0, 0, -7)
		label = "Last 7 days"
	case "month":
		end = now
		start = now.AddDate(0, 0, -30)
		label = "Last 30 days"
	case "90days":
		end = now
		start = now.AddDate(0, 0, -90)
		label = "Last 90 days"
	case "custom":
		startStr := r.URL.Query().Get("start_date")
		endStr := r.URL.Query().Get("end_date")
		if startStr == "" || endStr == "" {
			err = fmt.Errorf("period=custom requires start_date and end_date")
			return
		}
		start, err = time.Parse("2006-01-02", startStr)
		if err != nil {
			err = fmt.Errorf("invalid start_date: %w", err)
			return
		}
		end, err = time.Parse("2006-01-02", endStr)
		if err != nil {
			err = fmt.Errorf("invalid end_date: %w", err)
			return
		}
		end = end.Add(24*time.Hour - time.Second) // end of that day
		label = "Custom: " + startStr + " – " + endStr
	default:
		err = fmt.Errorf("invalid period %q; must be today|week|month|90days|custom", period)
		return
	}
	return
}

// resolveOrgID returns the org to query for. SOC roles may spectate via ?org=;
// all other roles are pinned to claims.OrganizationID.
func resolveOrgID(r *http.Request) string {
	claims := claimsFromRequest(r)
	if claims == nil {
		return ""
	}
	if globalViewRoles[claims.Role] && r.URL.Query().Get("org") != "" {
		return r.URL.Query().Get("org")
	}
	return claims.OrganizationID
}

// complianceSummaryResponse is the JSON shape for GET /api/v1/portal/compliance/summary.
type complianceSummaryResponse struct {
	Period struct {
		Label string `json:"label"`
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"period"`
	SiteID               *string                       `json:"site_id"`
	TotalViolations      int                           `json:"total_violations"`
	TotalReviewed        int                           `json:"total_reviewed"`
	PendingCount         int                           `json:"pending_count"`
	ComplianceRate       *float64                      `json:"compliance_rate"`
	PersonHours          *float64                      `json:"person_hours"`
	PersonHoursAvailable bool                          `json:"person_hours_available"`
	ViolationsOverTime   []database.ComplianceTimeBucket  `json:"violations_over_time"`
	TopCameras           []database.ComplianceCamera      `json:"top_cameras"`
	RecentFindings       []complianceFindingJSON          `json:"recent_findings"`
}

type complianceFindingJSON struct {
	ID             string   `json:"id"`
	CameraID       string   `json:"camera_id"`
	CameraName     string   `json:"camera_name"`
	SiteID         *string  `json:"site_id,omitempty"`
	SiteName       string   `json:"site_name,omitempty"`
	DetectionClass string   `json:"detection_class"`
	MissingLabel   string   `json:"missing_label"`
	Confidence     float64  `json:"confidence"`
	Status         string   `json:"status"`
	CreatedAt      string   `json:"created_at"`
}

// HandleComplianceSummary handles GET /api/v1/portal/compliance/summary.
// Returns tenant-scoped PPE compliance metrics for the requested period.
func HandleComplianceSummary(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		orgID := resolveOrgID(r)
		if orgID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}

		start, end, label, truncUnit, err := parsePeriod(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Optional site filter with cross-tenant ownership check.
		var siteID *string
		if s := r.URL.Query().Get("site_id"); s != "" {
			ok, err := db.VerifySiteOwnership(r.Context(), s, orgID)
			if err != nil {
				slog.Error("VerifySiteOwnership", "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "site not in scope", http.StatusForbidden)
				return
			}
			siteID = &s
		}

		f := database.ComplianceFilter{
			OrgID:     orgID,
			SiteID:    siteID,
			Start:     start,
			End:       end,
			TruncUnit: truncUnit,
		}

		// Run the four aggregation queries.
		headline, err := db.GetComplianceHeadline(r.Context(), f)
		if err != nil {
			slog.Error("GetComplianceHeadline", "error", err)
			http.Error(w, "summary query failed", http.StatusInternalServerError)
			return
		}

		timeSeries, err := db.GetComplianceViolationsOverTime(r.Context(), f)
		if err != nil {
			slog.Error("GetComplianceViolationsOverTime", "error", err)
			http.Error(w, "time series query failed", http.StatusInternalServerError)
			return
		}

		topCameras, err := db.GetComplianceTopCameras(r.Context(), f, 5)
		if err != nil {
			slog.Error("GetComplianceTopCameras", "error", err)
			http.Error(w, "cameras query failed", http.StatusInternalServerError)
			return
		}

		findings, err := db.GetComplianceRecentFindings(r.Context(), f, 20)
		if err != nil {
			slog.Error("GetComplianceRecentFindings", "error", err)
			http.Error(w, "findings query failed", http.StatusInternalServerError)
			return
		}

		// Occupancy query (gracefully absent when C-02 not migrated).
		_, personHours, err := db.GetComplianceOccupancy(r.Context(), f)
		if err != nil {
			slog.Error("GetComplianceOccupancy", "error", err)
			http.Error(w, "occupancy query failed", http.StatusInternalServerError)
			return
		}

		// Compute compliance rate (nil when no reviewed rows).
		var complianceRate *float64
		if headline.TotalReviewed > 0 {
			rate := float64(headline.TotalReviewed-headline.TotalViolations) / float64(headline.TotalReviewed) * 100
			complianceRate = &rate
		}

		// Build findings JSON (strip FramePath).
		findingsJSON := make([]complianceFindingJSON, 0, len(findings))
		for _, f := range findings {
			findingsJSON = append(findingsJSON, complianceFindingJSON{
				ID:             f.ID.String(),
				CameraID:       f.CameraID.String(),
				CameraName:     f.CameraName,
				SiteID:         f.SiteID,
				SiteName:       f.SiteName,
				DetectionClass: f.DetectionClass,
				MissingLabel:   f.MissingLabel,
				Confidence:     f.Confidence,
				Status:         f.Status,
				CreatedAt:      f.CreatedAt.UTC().Format(time.RFC3339),
			})
		}

		resp := complianceSummaryResponse{
			SiteID:               siteID,
			TotalViolations:      headline.TotalViolations,
			TotalReviewed:        headline.TotalReviewed,
			PendingCount:         headline.PendingCount,
			ComplianceRate:       complianceRate,
			PersonHours:          personHours,
			PersonHoursAvailable: personHours != nil,
			ViolationsOverTime:   timeSeries,
			TopCameras:           topCameras,
			RecentFindings:       findingsJSON,
		}
		resp.Period.Label = label
		resp.Period.Start = start.UTC().Format(time.RFC3339)
		resp.Period.End = end.UTC().Format(time.RFC3339)

		writeJSON(w, resp)
	}
}

// HandleComplianceReportPDF handles GET /api/v1/portal/compliance/report.pdf.
// Generates and streams a server-rendered compliance PDF. report_id is assigned
// server-side and returned in the X-Report-ID header for audit-trail linkage.
func HandleComplianceReportPDF(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		orgID := resolveOrgID(r)
		if orgID == "" {
			http.Error(w, "no organization scope", http.StatusBadRequest)
			return
		}

		start, end, label, truncUnit, err := parsePeriod(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var siteID *string
		siteName := "All Sites"
		if s := r.URL.Query().Get("site_id"); s != "" {
			ok, err := db.VerifySiteOwnership(r.Context(), s, orgID)
			if err != nil {
				slog.Error("VerifySiteOwnership (pdf)", "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "site not in scope", http.StatusForbidden)
				return
			}
			siteID = &s
		}

		includeFindings := r.URL.Query().Get("include_findings") != "false"

		f := database.ComplianceFilter{
			OrgID:     orgID,
			SiteID:    siteID,
			Start:     start,
			End:       end,
			TruncUnit: truncUnit,
		}

		headline, err := db.GetComplianceHeadline(r.Context(), f)
		if err != nil {
			slog.Error("GetComplianceHeadline (pdf)", "error", err)
			http.Error(w, `{"error":"report generation failed"}`, http.StatusInternalServerError)
			return
		}

		timeSeries, err := db.GetComplianceViolationsOverTime(r.Context(), f)
		if err != nil {
			slog.Error("GetComplianceViolationsOverTime (pdf)", "error", err)
			http.Error(w, `{"error":"report generation failed"}`, http.StatusInternalServerError)
			return
		}

		topCameras, err := db.GetComplianceTopCameras(r.Context(), f, 5)
		if err != nil {
			slog.Error("GetComplianceTopCameras (pdf)", "error", err)
			http.Error(w, `{"error":"report generation failed"}`, http.StatusInternalServerError)
			return
		}

		var findings []database.ComplianceFinding
		if includeFindings {
			findings, err = db.GetComplianceRecentFindings(r.Context(), f, 20)
			if err != nil {
				slog.Error("GetComplianceRecentFindings (pdf)", "error", err)
				http.Error(w, `{"error":"report generation failed"}`, http.StatusInternalServerError)
				return
			}
		}

		_, personHours, err := db.GetComplianceOccupancy(r.Context(), f)
		if err != nil {
			slog.Error("GetComplianceOccupancy (pdf)", "error", err)
			http.Error(w, `{"error":"report generation failed"}`, http.StatusInternalServerError)
			return
		}

		// Fetch org name.
		org, err := db.GetOrganizationByID(r.Context(), orgID)
		if err != nil {
			slog.Error("GetOrganizationByID (pdf)", "error", err)
		}
		orgName := orgID
		if org != nil {
			orgName = org.Name
		}

		// If a site is filtered, try to get its name.
		if siteID != nil {
			// site name is available via GetComplianceRecentFindings join; use a
			// quick direct query via one of the existing findings if available,
			// otherwise leave it as the site ID.
			if len(findings) > 0 && findings[0].SiteName != "" {
				siteName = findings[0].SiteName
			} else {
				siteName = *siteID
			}
		}

		var complianceRate *float64
		if headline.TotalReviewed > 0 {
			rate := float64(headline.TotalReviewed-headline.TotalViolations) / float64(headline.TotalReviewed) * 100
			complianceRate = &rate
		}

		// Convert DB types to PDF types.
		chartBuckets := make([]pdf.ComplianceTimeBucketPDF, 0, len(timeSeries))
		for _, b := range timeSeries {
			lbl := b.Bucket.Format("01/02")
			if truncUnit == "hour" {
				lbl = b.Bucket.Format("15:00")
			}
			chartBuckets = append(chartBuckets, pdf.ComplianceTimeBucketPDF{
				Label: lbl,
				Count: b.Count,
			})
		}

		cameras := make([]pdf.ComplianceCameraPDF, 0, len(topCameras))
		for _, c := range topCameras {
			cameras = append(cameras, pdf.ComplianceCameraPDF{
				CameraName:     c.CameraName,
				ViolationCount: c.ViolationCount,
				PctOfTotal:     c.PctOfTotal,
			})
		}

		pdfFindings := make([]pdf.ComplianceFindingPDF, 0, len(findings))
		for _, fi := range findings {
			pdfFindings = append(pdfFindings, pdf.ComplianceFindingPDF{
				CreatedAt:      fi.CreatedAt,
				CameraName:     fi.CameraName,
				MissingLabel:   fi.MissingLabel,
				DetectionClass: fi.DetectionClass,
				Confidence:     fi.Confidence,
				Status:         fi.Status,
			})
		}

		reportID := pdf.GenerateReportID()
		pdfData := pdf.ComplianceReportData{
			ReportID:        reportID,
			OrgName:         orgName,
			SiteName:        siteName,
			PeriodLabel:     label,
			PeriodStart:     start,
			PeriodEnd:       end,
			GeneratedBy:     claims.Username,
			GeneratedAt:     time.Now().UTC(),
			TotalViolations: headline.TotalViolations,
			TotalReviewed:   headline.TotalReviewed,
			PendingCount:    headline.PendingCount,
			ComplianceRate:  complianceRate,
			PersonHours:     personHours,
			ViolationsChart: chartBuckets,
			TopCameras:      cameras,
			RecentFindings:  pdfFindings,
			IncludeFindings: includeFindings,
		}

		pdfBytes, err := pdf.RenderComplianceReport(pdfData)
		if err != nil {
			slog.Error("RenderComplianceReport", "error", err)
			http.Error(w, `{"error":"report generation failed"}`, http.StatusInternalServerError)
			return
		}

		filename := fmt.Sprintf("ironsight-compliance-%s-%s-%s.pdf",
			r.URL.Query().Get("period"),
			start.Format("20060102"),
			end.Format("20060102"),
		)

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("X-Report-ID", reportID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdfBytes)
	}
}
