package database

import (
	"time"

	"github.com/google/uuid"

	"onvif-tool/internal/avs"
)

// ═══════════════════════════════════════════════════════════════
// Ironsight Platform Models
// Organizations, Sites, SOPs, Operators, Security Events
// ═══════════════════════════════════════════════════════════════

// Organization represents a customer company
type Organization struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Plan         string                 `json:"plan"`
	ContactName  string                 `json:"contact_name"`
	ContactEmail string                 `json:"contact_email"`
	LogoURL      string                 `json:"logo_url,omitempty"`
	Features     map[string]interface{} `json:"features,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

// MonitoringWindow is a recurring time window when the SOC monitors this site.
type MonitoringWindow struct {
	ID        string `json:"id"`
	Label     string `json:"label"`      // "Weeknight", "Weekend 24hr"
	Days      []int  `json:"days"`       // 0=Sun … 6=Sat
	StartTime string `json:"start_time"` // "18:00"
	EndTime   string `json:"end_time"`   // "06:00"
	Enabled   bool   `json:"enabled"`
}

// SiteSnooze records a customer-initiated disarm/snooze period.
type SiteSnooze struct {
	Active    bool   `json:"active"`
	Reason    string `json:"reason"`
	SnoozedBy string `json:"snoozed_by"`
	SnoozedAt string `json:"snoozed_at"` // ISO
	ExpiresAt string `json:"expires_at"` // ISO — auto-rearm
}

// Site represents a monitored construction site
type Site struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Address         string    `json:"address"`
	OrganizationID  string    `json:"company_id"` // frontend expects company_id
	Latitude        *float64  `json:"latitude,omitempty"`
	Longitude       *float64  `json:"longitude,omitempty"`
	Status          string    `json:"status"`
	MonitoringStart string    `json:"monitoring_start"`
	MonitoringEnd   string    `json:"monitoring_end"`
	SiteNotes       []string  `json:"site_notes"`
	// FeatureMode controls which product tier is active for this site.
	// "security_only"       → cameras, recordings, SOC events, security reports
	// "security_and_safety" → all of the above + PPE compliance, OSHA, vLM safety engine
	FeatureMode        string             `json:"feature_mode"`
	MonitoringSchedule []MonitoringWindow `json:"monitoring_schedule,omitempty"`
	Snooze             *SiteSnooze        `json:"snooze,omitempty"`
	// Recording policy is applied to every camera assigned to this site.
	// Previously these lived on the camera row; they moved to the site in
	// the 2026-04 migration so a single retention/schedule change applies
	// uniformly across the site's fleet. Cameras inherit at read time —
	// there's no per-camera override.
	RetentionDays      int                `json:"retention_days"`
	RecordingMode      string             `json:"recording_mode"`     // "continuous" | "event"
	PreBufferSec       int                `json:"pre_buffer_sec"`     // event-mode pre-roll
	PostBufferSec      int                `json:"post_buffer_sec"`    // event-mode tail
	RecordingTriggers  string             `json:"recording_triggers"` // comma-separated: "motion,object"
	RecordingSchedule  string             `json:"recording_schedule"` // JSON {"days":[0-6],"start":"HH:MM","end":"HH:MM"}
	CreatedAt          time.Time          `json:"created_at"`
	CamerasOnline      int                `json:"cameras_online"`
	CamerasTotal       int                `json:"cameras_total"`
	// Frontend SiteSummary fields — populated with defaults
	ComplianceScore  int    `json:"compliance_score"`
	OpenIncidents    int    `json:"open_incidents"`
	WorkersOnSite    int    `json:"workers_on_site"`
	LastActivity     string `json:"last_activity"`
	Trend            string `json:"trend"`
}

// SiteSOP represents a Standard Operating Procedure for a site
type SiteSOP struct {
	ID        string                   `json:"id"`
	SiteID    string                   `json:"site_id"`
	Title     string                   `json:"title"`
	Category  string                   `json:"category"`
	Priority  string                   `json:"priority"`
	Steps     []string                 `json:"steps"`
	Contacts  []map[string]interface{} `json:"contacts"`
	UpdatedAt time.Time                `json:"updated_at"`
	UpdatedBy string                   `json:"updated_by"`
}

// CompanyUser represents a customer-side user (site manager, executive)
type CompanyUser struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Email           string    `json:"email"`
	Phone           string    `json:"phone"`
	PasswordHash    string    `json:"-"`
	Role            string    `json:"role"`
	OrganizationID  string    `json:"organization_id"`
	AssignedSiteIDs []string  `json:"assigned_site_ids"`
	CreatedAt       time.Time `json:"created_at"`
}

// Operator represents a SOC security operator
type Operator struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Callsign      string `json:"callsign"`
	Email         string `json:"email,omitempty"`
	PasswordHash  string `json:"-"`
	Status        string `json:"status"`
	ActiveAlarmID string `json:"active_alarm_id,omitempty"`
	LastActive    int64  `json:"last_active"`
}

// SecurityEvent represents a dispositioned alarm visible on the customer portal
type SecurityEvent struct {
	ID               string                   `json:"id"`
	AlarmID          string                   `json:"alarm_id"`
	SiteID           string                   `json:"site_id"`
	CameraID         string                   `json:"camera_id"`
	Severity         string                   `json:"severity"`
	Type             string                   `json:"type"`
	Description      string                   `json:"description"`
	DispositionCode  string                   `json:"disposition_code"`
	DispositionLabel string                   `json:"disposition_label"`
	OperatorID       string                   `json:"operator_id"`
	OperatorCallsign string                   `json:"operator_callsign"`
	OperatorNotes    string                   `json:"operator_notes"`
	ActionLog        []map[string]interface{} `json:"action_log"`
	EscalationDepth  int                      `json:"escalation_depth"`
	ClipURL          string                   `json:"clip_url"`
	Ts               int64                    `json:"ts"`
	ResolvedAt       int64                    `json:"resolved_at"`
	ViewedByCustomer bool                     `json:"viewed_by_customer"`

	// Dual-operator verification. Set via POST /api/security-events/{id}/verify
	// by a supervisor or admin who is NOT the disposing operator. Required
	// before the event can be escalated to law enforcement or counted as
	// "video-verified" in TMA-AVS-01 scoring.
	DisposedByUserID   *uuid.UUID `json:"disposed_by_user_id,omitempty"`
	VerifiedByUserID   *uuid.UUID `json:"verified_by_user_id,omitempty"`
	VerifiedByCallsign string     `json:"verified_by_callsign,omitempty"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`

	// TMA-AVS-01 Alarm Validation Score. AVSFactors is the raw operator
	// attestation set; AVSScore is the 0–4 mapping computed at
	// disposition time by internal/avs.ComputeScore. AVSRubricVersion
	// pins the score to a specific algorithm release so auditors can
	// reproduce historical scores even after future rubric edits.
	AVSFactors       avs.Factors `json:"avs_factors,omitempty"`
	AVSScore         avs.Score   `json:"avs_score,omitempty"`
	AVSRubricVersion string      `json:"avs_rubric_version,omitempty"`
}

// IsVerificationRequired returns true if this event's severity demands
// a second-operator sign-off before downstream actions (dispatch,
// AVS-scored alarm transmission). Lives on the model so the rule is
// uniform across handlers and the frontend can display a verification
// badge consistently.
func (e *SecurityEvent) IsVerificationRequired() bool {
	return e.Severity == "critical" || e.Severity == "high"
}

// IsVerified is the boolean shortcut callers want — encapsulates the
// nullable VerifiedAt so business logic doesn't have to re-check the
// nil pointer everywhere.
func (e *SecurityEvent) IsVerified() bool {
	return e.VerifiedAt != nil
}

// EvidenceShare represents a shareable evidence link
type EvidenceShare struct {
	Token      string     `json:"token"`
	IncidentID string     `json:"incident_id"`
	CreatedBy  string     `json:"created_by"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Revoked    bool       `json:"revoked"`
	CreatedAt  time.Time  `json:"created_at"`
}

// PlatformCamera is a camera with its platform-layer site assignment info.
// OnvifAddress and Manufacturer are admin-only — never expose to site customers.
type PlatformCamera struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OnvifAddress string `json:"onvif_address"` // admin-only: physical identity
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Status       string `json:"status"`
	SiteID       string `json:"site_id,omitempty"`
	Location     string `json:"location"`  // site-scoped alias shown to operators/customers
	Recording    bool   `json:"recording"`
}

// PlatformSpeaker is a speaker with its platform-layer site assignment info.
type PlatformSpeaker struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OnvifAddress string `json:"onvif_address"` // admin-only
	Zone         string `json:"zone"`          // original zone name
	Location     string `json:"location"`      // site-scoped alias
	Status       string `json:"status"`
	SiteID       string `json:"site_id,omitempty"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
}

// DeviceAssignment records when a camera or speaker was at a site.
// assigned_at..removed_at defines the window; removed_at=NULL means currently assigned.
// Recording/event queries scoped to a site use this to prevent data leakage
// when a physical device is moved from one customer to another.
type DeviceAssignment struct {
	ID            int64      `json:"id"`
	DeviceType    string     `json:"device_type"` // "camera" | "speaker"
	DeviceID      string     `json:"device_id"`
	SiteID        string     `json:"site_id"`
	LocationLabel string     `json:"location_label"`
	AssignedAt    time.Time  `json:"assigned_at"`
	RemovedAt     *time.Time `json:"removed_at,omitempty"`
}

// Incident groups related alarms from the same site within a correlation window.
// When multiple cameras or repeated events fire at one site, they are collected
// into a single incident so the SOC operator sees one actionable item.
type Incident struct {
	ID            string   `json:"id"`
	SiteID        string   `json:"site_id"`
	SiteName      string   `json:"site_name"`
	Severity      string   `json:"severity"`       // max severity across child alarms
	Status        string   `json:"status"`          // active | acknowledged
	AlarmCount    int      `json:"alarm_count"`
	CameraIDs     []string `json:"camera_ids"`      // unique cameras involved
	CameraNames   []string `json:"camera_names"`    // display names, parallel to CameraIDs
	Types         []string `json:"types"`           // unique event types (intrusion, face, …)
	LatestType    string   `json:"latest_type"`     // most recent alarm type
	Description   string   `json:"description"`     // human-readable summary
	SnapshotURL   string   `json:"snapshot_url"`    // latest snapshot
	ClipURL       string   `json:"clip_url"`        // latest clip
	FirstAlarmTs  int64    `json:"first_alarm_ts"`
	LastAlarmTs   int64    `json:"last_alarm_ts"`
	SlaDeadlineMs int64    `json:"sla_deadline_ms"` // resets on each new alarm
	CreatedAt     int64    `json:"created_at"`
}

// ActiveAlarm is a live, unacknowledged alarm generated by the NVR detection pipeline.
// Created when a high-confidence detection fires; closed when the operator dispositions it.
type ActiveAlarm struct {
	ID              string `json:"id"`
	// AlarmCode is the phoneticizable short code (ALM-YYMMDD-NNNN) used
	// on radios and phone bridges; the ID stays as the URL/API key.
	AlarmCode       string `json:"alarm_code,omitempty"`
	// TriggeringEventID pins the alarm to the specific detection-pipeline
	// event that produced it, so forensic replay is one SELECT instead of
	// a lossy (camera_id, ts) join.
	TriggeringEventID *int64 `json:"triggering_event_id,omitempty"`
	IncidentID      string `json:"incident_id,omitempty"` // parent incident grouping
	SiteID          string `json:"site_id"`
	SiteName        string `json:"site_name"`
	CameraID        string `json:"camera_id"`
	CameraName      string `json:"camera_name"`
	Severity        string `json:"severity"`
	Type            string `json:"type"`
	Description     string `json:"description"`
	SnapshotURL     string `json:"snapshot_url"`
	ClipURL         string `json:"clip_url"`
	Ts              int64  `json:"ts"`
	Acknowledged    bool   `json:"acknowledged"`
	ClaimedBy       string `json:"claimed_by,omitempty"`
	EscalationLevel     int     `json:"escalation_level"`
	SlaDeadlineMs       int64   `json:"sla_deadline_ms"`
	AIDescription       string                   `json:"ai_description,omitempty"`
	AIThreatLevel       string                   `json:"ai_threat_level,omitempty"`
	AIRecommendedAction string                   `json:"ai_recommended_action,omitempty"`
	AIFalsePositivePct  float64                  `json:"ai_false_positive_pct,omitempty"`
	AIDetections        []map[string]interface{} `json:"ai_detections,omitempty"`
	AIPPEViolations     []map[string]interface{} `json:"ai_ppe_violations,omitempty"`
}

// ShiftHandoff records a handoff from one SOC operator to another at shift change.
type ShiftHandoff struct {
	ID                   int64      `json:"id"`
	FromOperatorID       string     `json:"from_operator_id"`
	FromOperatorCallsign string     `json:"from_operator_callsign"`
	ToOperatorID         string     `json:"to_operator_id"`
	ToOperatorCallsign   string     `json:"to_operator_callsign"`
	LockedSiteIDs        []string   `json:"locked_site_ids"`
	ActiveAlertIDs       []string   `json:"active_alert_ids"`
	Notes                string     `json:"notes"`
	Status               string     `json:"status"` // "pending" | "accepted" | "declined"
	CreatedAt            time.Time  `json:"created_at"`
	AcceptedAt           *time.Time `json:"accepted_at,omitempty"`
}

// IncidentDetail is the full portal-facing detail view of a security event.
// Returned by GET /api/v1/incidents/{id}.
type IncidentDetail struct {
	ID                 string                   `json:"id"`
	Severity           string                   `json:"severity"`
	Status             string                   `json:"status"`
	Title              string                   `json:"title"`
	SiteID             string                   `json:"site_id"`
	SiteName           string                   `json:"site_name"`
	CameraID           string                   `json:"camera_id"`
	CameraName         string                   `json:"camera_name"`
	Ts                 int64                    `json:"ts"`
	DurationMs         int64                    `json:"duration_ms"`
	WorkersIdentified  int                      `json:"workers_identified"`
	AIConfidence       float64                  `json:"ai_confidence"`
	AICaption          string                   `json:"ai_caption"`
	Findings           []map[string]interface{} `json:"findings"`
	Detections         []map[string]interface{} `json:"detections"`
	Workers            []map[string]interface{} `json:"workers"`
	Timeline           []map[string]interface{} `json:"timeline"`
	Notifications      []map[string]interface{} `json:"notifications"`
	Comments           []map[string]interface{} `json:"comments"`
	ClipURL            string                   `json:"clip_url"`
	Keyframes          []map[string]interface{} `json:"keyframes"`
	OSHAClassification string                   `json:"osha_classification"`
	RelatedIncidents   []string                 `json:"related_incidents"`
	OperatorCallsign   string                   `json:"operator_callsign"`
	OperatorNotes      string                   `json:"operator_notes"`
	DispositionCode    string                   `json:"disposition_code"`
	DispositionLabel   string                   `json:"disposition_label"`
}

// ShiftHandoffCreate is the DTO for creating a shift handoff.
type ShiftHandoffCreate struct {
	FromOperatorID       string   `json:"from_operator_id"`
	FromOperatorCallsign string   `json:"from_operator_callsign"`
	ToOperatorID         string   `json:"to_operator_id"`
	ToOperatorCallsign   string   `json:"to_operator_callsign"`
	LockedSiteIDs        []string `json:"locked_site_ids"`
	ActiveAlertIDs       []string `json:"active_alert_ids"`
	Notes                string   `json:"notes"`
}

// IncidentSummary is the portal-facing shape for security_events JOIN sites.
// Returned by GET /api/v1/incidents.
type IncidentSummary struct {
	ID                string `json:"id"`
	Severity          string `json:"severity"`
	Status            string `json:"status"`
	Title             string `json:"title"`
	SiteID            string `json:"site_id"`
	SiteName          string `json:"site_name"`
	CameraID          string `json:"camera_id"`
	Ts                int64  `json:"ts"`
	ResolvedAt        int64  `json:"resolved_at"`
	WorkersIdentified int    `json:"workers_identified"`
	Type              string `json:"type"`
	EscalationLevel   int    `json:"escalation_level"`
	DispositionCode   string `json:"disposition_code"`
	DispositionLabel  string `json:"disposition_label"`
	OperatorCallsign  string `json:"operator_callsign"`
	CameraName        string `json:"camera_name"`
}

// ── Create/Update DTOs ──

type OrganizationCreate struct {
	Name         string `json:"name"`
	Plan         string `json:"plan"`
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	LogoURL      string `json:"logo_url"`
}

type SiteCreate struct {
	Name        string   `json:"name"`
	Address     string   `json:"address"`
	CompanyID   string   `json:"company_id"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
	FeatureMode string   `json:"feature_mode"` // "security_only" | "security_and_safety"
}

type SOPCreate struct {
	SiteID    string                   `json:"site_id"`
	Title     string                   `json:"title"`
	Category  string                   `json:"category"`
	Priority  string                   `json:"priority"`
	Steps     []string                 `json:"steps"`
	Contacts  []map[string]interface{} `json:"contacts"`
	UpdatedBy string                   `json:"updated_by"`
}

type CompanyUserCreate struct {
	Name            string   `json:"name"`
	Email           string   `json:"email"`
	Phone           string   `json:"phone"`
	Role            string   `json:"role"`
	CompanyID       string   `json:"company_id"`
	Password        string   `json:"password"`
	AssignedSiteIDs []string `json:"assigned_site_ids"`
}

type SecurityEventCreate struct {
	AlarmID          string                   `json:"alarm_id"`
	SiteID           string                   `json:"site_id"`
	CameraID         string                   `json:"camera_id"`
	Severity         string                   `json:"severity"`
	Type             string                   `json:"type"`
	Description      string                   `json:"description"`
	DispositionCode  string                   `json:"disposition_code"`
	DispositionLabel string                   `json:"disposition_label"`
	OperatorCallsign string                   `json:"operator_callsign"`
	OperatorNotes    string                   `json:"operator_notes"`
	ActionLog        []map[string]interface{} `json:"action_log"`
	EscalationDepth  int                      `json:"escalation_depth"`
	ClipURL          string                   `json:"clip_url"`
	// DisposedByUserID is the authenticated operator's UUID, populated
	// from the JWT claims at the API layer. It's NOT a JSON field — the
	// client cannot supply this value, otherwise it could be spoofed
	// against the dual-operator self-verification check.
	DisposedByUserID uuid.UUID `json:"-"`

	// AVSFactors is the structured TMA-AVS-01 attestation set the
	// operator captured during disposition. The score is computed
	// server-side from these factors — clients never supply the score
	// directly, so a malicious client can't claim a higher score
	// than its factors warrant.
	AVSFactors avs.Factors `json:"avs_factors,omitempty"`
}
