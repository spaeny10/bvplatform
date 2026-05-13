package database

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"ironsight/internal/avs"
)

// scanSiteRows is the shared row → []Site reader used by both the
// unscoped ListSites and the RBAC-scoped ListSitesScoped. The column
// list in the SELECT must match this function exactly; refactoring
// either side of that pair means refactoring both.
func scanSiteRows(rows pgx.Rows) ([]Site, error) {
	var sites []Site
	for rows.Next() {
		var s Site
		var notesJSON []byte
		if err := rows.Scan(&s.ID, &s.Name, &s.Address, &s.OrganizationID, &s.Latitude, &s.Longitude, &s.Status, &s.MonitoringStart, &s.MonitoringEnd, &notesJSON, &s.FeatureMode,
			&s.RetentionDays, &s.RecordingMode, &s.PreBufferSec, &s.PostBufferSec, &s.RecordingTriggers, &s.RecordingSchedule,
			&s.CreatedAt, &s.CamerasOnline, &s.CamerasTotal); err != nil {
			return nil, err
		}
		json.Unmarshal(notesJSON, &s.SiteNotes)
		if s.SiteNotes == nil {
			s.SiteNotes = []string{}
		}
		s.ComplianceScore = 85
		s.Trend = "flat"
		s.LastActivity = time.Now().UTC().Format(time.RFC3339)
		sites = append(sites, s)
	}
	if sites == nil {
		sites = []Site{}
	}
	return sites, nil
}

// ═══════════════════════════════════════════════════════════════
// Organization CRUD
// ═══════════════════════════════════════════════════════════════

func (db *DB) ListOrganizations(ctx context.Context) ([]Organization, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, plan, contact_name, contact_email, logo_url, created_at FROM organizations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Plan, &o.ContactName, &o.ContactEmail, &o.LogoURL, &o.CreatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	if orgs == nil {
		orgs = []Organization{}
	}
	return orgs, nil
}

func (db *DB) CreateOrganization(ctx context.Context, c *OrganizationCreate) (*Organization, error) {
	id := fmt.Sprintf("co-%s", uuid.New().String()[:8])
	plan := c.Plan
	if plan == "" {
		plan = "professional"
	}
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO organizations (id, name, plan, contact_name, contact_email, logo_url) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, c.Name, plan, c.ContactName, c.ContactEmail, c.LogoURL,
	)
	if err != nil {
		return nil, err
	}
	return &Organization{ID: id, Name: c.Name, Plan: plan, ContactName: c.ContactName, ContactEmail: c.ContactEmail, CreatedAt: time.Now()}, nil
}

func (db *DB) UpdateOrganization(ctx context.Context, id string, c *OrganizationCreate) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE organizations SET name=$2, plan=$3, contact_name=$4, contact_email=$5 WHERE id=$1`,
		id, c.Name, c.Plan, c.ContactName, c.ContactEmail,
	)
	return err
}

func (db *DB) DeleteOrganization(ctx context.Context, id string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, id)
	return err
}

// ═══════════════════════════════════════════════════════════════
// Site CRUD
// ═══════════════════════════════════════════════════════════════

// CustomerContact is a single entry in a site's customer-facing
// contact list. Distinct from the SOC's SOP call tree — these are
// the names the customer wants their notification channel to know
// about, plus the on-site operations contacts the SOC asks for
// during disposition. notify_on_alarm hints whether this person
// should receive an SMS when an alarm fires (separate from the
// authenticated-user notification preferences in
// notification_subscriptions; this flag drives the to-be-built
// "non-account contact" SMS path).
type CustomerContact struct {
	Name           string `json:"name"`
	Role           string `json:"role"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	NotifyOnAlarm  bool   `json:"notify_on_alarm"`
	Notes          string `json:"notes,omitempty"`
}

// GetCustomerContacts returns the contact list stored on the site
// row. Empty array if never edited. Caller is responsible for site-
// scope authorization — this method just performs the read.
func (db *DB) GetCustomerContacts(ctx context.Context, siteID string) ([]CustomerContact, error) {
	var raw []byte
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(customer_contacts, '[]'::jsonb) FROM sites WHERE id=$1`, siteID,
	).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var out []CustomerContact
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []CustomerContact{}
	}
	return out, nil
}

// SetCustomerContacts overwrites the entire contact list. We
// deliberately don't support per-contact upsert: the list is small
// (rarely more than a dozen entries) and full replacement keeps the
// audit trail clean — one row in audit_log per save instead of one
// per contact change.
func (db *DB) SetCustomerContacts(ctx context.Context, siteID string, contacts []CustomerContact) error {
	if contacts == nil {
		contacts = []CustomerContact{}
	}
	raw, err := json.Marshal(contacts)
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx, `UPDATE sites SET customer_contacts=$2 WHERE id=$1`, siteID, raw)
	return err
}

// CallerScope describes how a list query should be filtered for the
// authenticated user. Built from JWT claims at the handler layer:
//
//   - Role: from claims.Role
//   - OrganizationID: from claims.OrganizationID (set for customer/
//     site_manager accounts that are tied to a customer org)
//   - AssignedSiteIDs: from users.assigned_site_ids (loaded fresh
//     from the DB at request time so a site assignment change takes
//     effect on the next request, not on the next login)
//
// IsUnscoped returns true for SOC-internal roles that should see
// everything (admin, soc_supervisor, soc_operator). Anyone else is
// constrained to their org's sites or their explicit assignment.
type CallerScope struct {
	Role            string
	OrganizationID  string
	AssignedSiteIDs []string
}

// IsUnscoped reports whether the caller should see global data.
// Centralized so any handler can ask the same question and get the
// same answer.
func (c CallerScope) IsUnscoped() bool {
	switch c.Role {
	case "admin", "soc_supervisor", "soc_operator":
		return true
	}
	return false
}

// ListSitesScoped returns sites the caller is allowed to see. Replaces
// ListSites in handler call paths to enforce row-level RBAC at the DB
// layer rather than relying on the frontend to filter — a UL 827B
// reviewer (and any reasonable security review) wants the boundary
// checked on every read, not assumed.
func (db *DB) ListSitesScoped(ctx context.Context, scope CallerScope) ([]Site, error) {
	base := `
		SELECT s.id, s.name, s.address, s.organization_id, s.latitude, s.longitude,
		       s.status, s.monitoring_start, s.monitoring_end, s.site_notes,
		       COALESCE(s.feature_mode, 'security_and_safety') AS feature_mode,
		       COALESCE(s.retention_days, 30),
		       COALESCE(s.recording_mode, 'continuous'),
		       COALESCE(s.pre_buffer_sec, 10),
		       COALESCE(s.post_buffer_sec, 30),
		       COALESCE(s.recording_triggers, 'motion,object'),
		       COALESCE(s.recording_schedule, ''),
		       s.created_at,
		       COUNT(c.id) FILTER (WHERE c.status = 'online') AS cameras_online,
		       COUNT(c.id) AS cameras_total
		FROM sites s
		LEFT JOIN cameras c ON c.site_id = s.id`

	var rows pgx.Rows
	var err error
	switch {
	case scope.IsUnscoped():
		rows, err = db.Pool.Query(ctx, base+` GROUP BY s.id ORDER BY s.name`)
	case scope.OrganizationID != "":
		// Customer / site_manager: scoped to their organization's
		// sites. Plus any explicitly-assigned sites in case the user
		// has a cross-org assignment (rare but supported).
		rows, err = db.Pool.Query(ctx, base+`
			WHERE s.organization_id = $1
			   OR ($2::text[] IS NOT NULL AND s.id = ANY($2::text[]))
			GROUP BY s.id ORDER BY s.name`,
			scope.OrganizationID, scope.AssignedSiteIDs)
	case len(scope.AssignedSiteIDs) > 0:
		rows, err = db.Pool.Query(ctx, base+`
			WHERE s.id = ANY($1::text[])
			GROUP BY s.id ORDER BY s.name`,
			scope.AssignedSiteIDs)
	default:
		// No org, no assignments — caller has access to nothing. Return
		// empty rather than 403 so the UI can render a "no sites yet"
		// state cleanly. The router still rejects unauthenticated
		// callers upstream of this method.
		return []Site{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSiteRows(rows)
}

func (db *DB) ListSites(ctx context.Context) ([]Site, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT s.id, s.name, s.address, s.organization_id, s.latitude, s.longitude,
		       s.status, s.monitoring_start, s.monitoring_end, s.site_notes,
		       COALESCE(s.feature_mode, 'security_and_safety') AS feature_mode,
		       COALESCE(s.retention_days, 30),
		       COALESCE(s.recording_mode, 'continuous'),
		       COALESCE(s.pre_buffer_sec, 10),
		       COALESCE(s.post_buffer_sec, 30),
		       COALESCE(s.recording_triggers, 'motion,object'),
		       COALESCE(s.recording_schedule, ''),
		       s.created_at,
		       COUNT(c.id) FILTER (WHERE c.status = 'online') AS cameras_online,
		       COUNT(c.id) AS cameras_total
		FROM sites s
		LEFT JOIN cameras c ON c.site_id = s.id
		GROUP BY s.id
		ORDER BY s.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []Site
	for rows.Next() {
		var s Site
		var notesJSON []byte
		if err := rows.Scan(&s.ID, &s.Name, &s.Address, &s.OrganizationID, &s.Latitude, &s.Longitude, &s.Status, &s.MonitoringStart, &s.MonitoringEnd, &notesJSON, &s.FeatureMode,
			&s.RetentionDays, &s.RecordingMode, &s.PreBufferSec, &s.PostBufferSec, &s.RecordingTriggers, &s.RecordingSchedule,
			&s.CreatedAt, &s.CamerasOnline, &s.CamerasTotal); err != nil {
			return nil, err
		}
		json.Unmarshal(notesJSON, &s.SiteNotes)
		if s.SiteNotes == nil {
			s.SiteNotes = []string{}
		}
		s.ComplianceScore = 85
		s.Trend = "flat"
		s.LastActivity = time.Now().UTC().Format(time.RFC3339)
		sites = append(sites, s)
	}
	if sites == nil {
		sites = []Site{}
	}
	return sites, nil
}

func (db *DB) GetSite(ctx context.Context, id string) (*Site, error) {
	var s Site
	var notesJSON []byte
	err := db.Pool.QueryRow(ctx, `
		SELECT id, name, address, organization_id, latitude, longitude, status,
		       monitoring_start, monitoring_end, site_notes,
		       COALESCE(feature_mode, 'security_and_safety'),
		       COALESCE(retention_days, 30),
		       COALESCE(recording_mode, 'continuous'),
		       COALESCE(pre_buffer_sec, 10),
		       COALESCE(post_buffer_sec, 30),
		       COALESCE(recording_triggers, 'motion,object'),
		       COALESCE(recording_schedule, ''),
		       created_at
		FROM sites WHERE id=$1`, id,
	).Scan(&s.ID, &s.Name, &s.Address, &s.OrganizationID, &s.Latitude, &s.Longitude, &s.Status,
		&s.MonitoringStart, &s.MonitoringEnd, &notesJSON, &s.FeatureMode,
		&s.RetentionDays, &s.RecordingMode, &s.PreBufferSec, &s.PostBufferSec, &s.RecordingTriggers, &s.RecordingSchedule,
		&s.CreatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(notesJSON, &s.SiteNotes)
	return &s, nil
}

// UpdateSiteRecording writes the per-site recording/retention policy. Separate
// from UpdateSite because the policy has stricter semantics (every camera on
// the site will adopt the new values on its next recording restart) and we
// want this to be a focused admin action, not bundled into a generic detail
// edit. The recording engine re-reads on its next schedule check.
func (db *DB) UpdateSiteRecording(ctx context.Context, id string, retentionDays, preBufferSec, postBufferSec int, mode, triggers, schedule string) error {
	if mode == "" {
		mode = "continuous"
	}
	if triggers == "" {
		triggers = "motion,object"
	}
	_, err := db.Pool.Exec(ctx, `
		UPDATE sites
		   SET retention_days      = $2,
		       recording_mode      = $3,
		       pre_buffer_sec      = $4,
		       post_buffer_sec     = $5,
		       recording_triggers  = $6,
		       recording_schedule  = $7,
		       recording_backfilled = true
		 WHERE id = $1`,
		id, retentionDays, mode, preBufferSec, postBufferSec, triggers, schedule)
	return err
}

func (db *DB) CreateSite(ctx context.Context, c *SiteCreate) (*Site, error) {
	initials := ""
	for _, w := range strings.Fields(c.Name) {
		if len(w) > 0 {
			initials += string(w[0])
		}
	}
	id := fmt.Sprintf("%s-%d", strings.ToUpper(initials), 100+rand.Intn(900))

	mode := c.FeatureMode
	if mode == "" {
		mode = "security_and_safety"
	}
	notesJSON, _ := json.Marshal([]string{})
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO sites (id, name, address, organization_id, latitude, longitude, status, site_notes, feature_mode) VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8)`,
		id, c.Name, c.Address, c.CompanyID, c.Latitude, c.Longitude, notesJSON, mode,
	)
	if err != nil {
		return nil, err
	}
	return &Site{
		ID: id, Name: c.Name, Address: c.Address, OrganizationID: c.CompanyID,
		Status: "active", FeatureMode: mode, CreatedAt: time.Now(),
		SiteNotes: []string{}, MonitoringStart: "18:00", MonitoringEnd: "06:00",
		ComplianceScore: 85, Trend: "flat", LastActivity: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (db *DB) UpdateSite(ctx context.Context, id string, c *SiteCreate) error {
	mode := c.FeatureMode
	if mode == "" {
		mode = "security_and_safety"
	}
	_, err := db.Pool.Exec(ctx,
		`UPDATE sites SET name=$2, address=$3, organization_id=$4, latitude=$5, longitude=$6, feature_mode=$7 WHERE id=$1`,
		id, c.Name, c.Address, c.CompanyID, c.Latitude, c.Longitude, mode,
	)
	return err
}

func (db *DB) DeleteSite(ctx context.Context, id string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM sites WHERE id=$1`, id)
	return err
}

// ═══════════════════════════════════════════════════════════════
// Site SOPs
// ═══════════════════════════════════════════════════════════════

func (db *DB) ListSiteSOPs(ctx context.Context, siteID string) ([]SiteSOP, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, site_id, title, category, priority, steps, contacts, updated_at, updated_by FROM site_sops WHERE site_id=$1`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sops []SiteSOP
	for rows.Next() {
		var s SiteSOP
		var stepsJSON, contactsJSON []byte
		if err := rows.Scan(&s.ID, &s.SiteID, &s.Title, &s.Category, &s.Priority, &stepsJSON, &contactsJSON, &s.UpdatedAt, &s.UpdatedBy); err != nil {
			return nil, err
		}
		json.Unmarshal(stepsJSON, &s.Steps)
		json.Unmarshal(contactsJSON, &s.Contacts)
		if s.Steps == nil { s.Steps = []string{} }
		if s.Contacts == nil { s.Contacts = []map[string]interface{}{} }
		sops = append(sops, s)
	}
	if sops == nil {
		sops = []SiteSOP{}
	}
	return sops, nil
}

func (db *DB) CreateSiteSOP(ctx context.Context, c *SOPCreate) (*SiteSOP, error) {
	id := fmt.Sprintf("sop-%s", uuid.New().String()[:8])
	stepsJSON, _ := json.Marshal(c.Steps)
	contactsJSON, _ := json.Marshal(c.Contacts)
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO site_sops (id, site_id, title, category, priority, steps, contacts, updated_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, c.SiteID, c.Title, c.Category, c.Priority, stepsJSON, contactsJSON, c.UpdatedBy,
	)
	if err != nil {
		return nil, err
	}
	return &SiteSOP{ID: id, SiteID: c.SiteID, Title: c.Title, Category: c.Category, Priority: c.Priority, Steps: c.Steps, Contacts: c.Contacts, UpdatedBy: c.UpdatedBy, UpdatedAt: time.Now()}, nil
}

func (db *DB) UpdateSiteSOP(ctx context.Context, id string, c *SOPCreate) error {
	stepsJSON, _ := json.Marshal(c.Steps)
	contactsJSON, _ := json.Marshal(c.Contacts)
	_, err := db.Pool.Exec(ctx,
		`UPDATE site_sops SET title=$2, category=$3, priority=$4, steps=$5, contacts=$6, updated_by=$7, updated_at=NOW() WHERE id=$1`,
		id, c.Title, c.Category, c.Priority, stepsJSON, contactsJSON, c.UpdatedBy,
	)
	return err
}

func (db *DB) DeleteSiteSOP(ctx context.Context, id string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM site_sops WHERE id=$1`, id)
	return err
}

// ═══════════════════════════════════════════════════════════════
// Company Users
// ═══════════════════════════════════════════════════════════════

func (db *DB) ListCompanyUsers(ctx context.Context, orgID string) ([]CompanyUser, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, email, phone, role, organization_id, assigned_site_ids, created_at FROM company_users WHERE organization_id=$1`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []CompanyUser
	for rows.Next() {
		var u CompanyUser
		var siteIDsJSON []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Role, &u.OrganizationID, &siteIDsJSON, &u.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(siteIDsJSON, &u.AssignedSiteIDs)
		if u.AssignedSiteIDs == nil { u.AssignedSiteIDs = []string{} }
		users = append(users, u)
	}
	if users == nil {
		users = []CompanyUser{}
	}
	return users, nil
}

func (db *DB) CreateCompanyUser(ctx context.Context, c *CompanyUserCreate) (*CompanyUser, error) {
	id := uuid.New().String()[:8]
	siteIDsJSON, _ := json.Marshal(c.AssignedSiteIDs)
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO company_users (id, name, email, phone, password_hash, role, organization_id, assigned_site_ids) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, c.Name, c.Email, c.Phone, c.Password, c.Role, c.CompanyID, siteIDsJSON,
	)
	if err != nil {
		return nil, err
	}
	return &CompanyUser{ID: id, Name: c.Name, Email: c.Email, Phone: c.Phone, Role: c.Role, OrganizationID: c.CompanyID, AssignedSiteIDs: c.AssignedSiteIDs, CreatedAt: time.Now()}, nil
}

func (db *DB) DeleteCompanyUser(ctx context.Context, id string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM company_users WHERE id=$1`, id)
	return err
}

// ═══════════════════════════════════════════════════════════════
// Operators
// ═══════════════════════════════════════════════════════════════

func (db *DB) ListOperators(ctx context.Context) ([]Operator, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, callsign, email, status, active_alarm_id, last_active FROM operators ORDER BY callsign`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []Operator
	for rows.Next() {
		var o Operator
		var activeAlarm *string
		if err := rows.Scan(&o.ID, &o.Name, &o.Callsign, &o.Email, &o.Status, &activeAlarm, &o.LastActive); err != nil {
			return nil, err
		}
		if activeAlarm != nil {
			o.ActiveAlarmID = *activeAlarm
		}
		ops = append(ops, o)
	}
	if ops == nil {
		ops = []Operator{}
	}
	return ops, nil
}

func (db *DB) CreateOperator(ctx context.Context, name, callsign, email string, userID *string) (*Operator, error) {
	id := "op-" + uuid.New().String()[:8]
	now := time.Now().UnixMilli()
	var err error
	if userID != nil {
		_, err = db.Pool.Exec(ctx,
			`INSERT INTO operators (id, name, callsign, email, status, last_active, user_id) VALUES ($1, $2, $3, $4, 'available', $5, $6)`,
			id, name, callsign, email, now, *userID,
		)
	} else {
		_, err = db.Pool.Exec(ctx,
			`INSERT INTO operators (id, name, callsign, email, status, last_active) VALUES ($1, $2, $3, $4, 'available', $5)`,
			id, name, callsign, email, now,
		)
	}
	if err != nil {
		return nil, err
	}
	return &Operator{ID: id, Name: name, Callsign: callsign, Email: email, Status: "available", LastActive: now}, nil
}

func (db *DB) GetCurrentOperator(ctx context.Context) (*Operator, error) {
	var o Operator
	var activeAlarm *string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, callsign, email, status, active_alarm_id, last_active FROM operators WHERE status != 'away' LIMIT 1`,
	).Scan(&o.ID, &o.Name, &o.Callsign, &o.Email, &o.Status, &activeAlarm, &o.LastActive)
	if err != nil {
		return nil, err
	}
	if activeAlarm != nil {
		o.ActiveAlarmID = *activeAlarm
	}
	return &o, nil
}

func (db *DB) GetOperatorByUserID(ctx context.Context, userID string) (*Operator, error) {
	var o Operator
	var activeAlarm *string
	err := db.Pool.QueryRow(ctx,
		`SELECT id, name, callsign, email, status, active_alarm_id, last_active FROM operators WHERE user_id = $1`,
		userID,
	).Scan(&o.ID, &o.Name, &o.Callsign, &o.Email, &o.Status, &activeAlarm, &o.LastActive)
	if err != nil {
		return nil, err
	}
	if activeAlarm != nil {
		o.ActiveAlarmID = *activeAlarm
	}
	return &o, nil
}

// ═══════════════════════════════════════════════════════════════
// Security Events
// ═══════════════════════════════════════════════════════════════

func (db *DB) CreateSecurityEvent(ctx context.Context, c *SecurityEventCreate) (*SecurityEvent, error) {
	id := fmt.Sprintf("EVT-%d-%04d", time.Now().Year(), rand.Intn(9999))
	now := time.Now().UnixMilli()
	actionLogJSON, _ := json.Marshal(c.ActionLog)

	severity := c.Severity
	if severity == "" {
		severity = "medium"
	}

	// Compute AVS server-side from the operator-supplied factors. The
	// client never sends a score — it sends what they observed; we
	// derive what that means. avs.RubricVersion is stored on the row
	// so an audit replay can reproduce the same score even after we
	// later publish a v2 rubric.
	avsScore := avs.ComputeScore(c.AVSFactors)
	avsFactorsJSON, _ := json.Marshal(c.AVSFactors)

	_, err := db.Pool.Exec(ctx,
		`INSERT INTO security_events
		 (id, alarm_id, site_id, camera_id, severity, type, description,
		  disposition_code, disposition_label, operator_callsign, operator_notes,
		  action_log, escalation_depth, clip_url, ts, resolved_at,
		  disposed_by_user_id,
		  avs_factors, avs_score, avs_rubric_version)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		id, c.AlarmID, c.SiteID, c.CameraID, severity, c.Type, c.Description,
		c.DispositionCode, c.DispositionLabel, c.OperatorCallsign, c.OperatorNotes,
		actionLogJSON, c.EscalationDepth, c.ClipURL, now, now,
		nullableUUID(c.DisposedByUserID),
		avsFactorsJSON, int(avsScore), avs.RubricVersion,
	)
	if err != nil {
		return nil, err
	}
	// Close the active alarm if one exists. SecurityEvent creation
	// already implies the operator who dispositioned this is the same
	// person who's "ack'ing" the alarm — record their callsign for the
	// SLA report. No user_id available at this layer; the API-level
	// HandleCreateSecurityEvent has already called AcknowledgeAlarm
	// with full attribution before reaching here, so this fallback only
	// fires for direct DB callers (tests, migration tools).
	db.Pool.Exec(ctx, `UPDATE active_alarms
	                   SET acknowledged             = true,
	                       acknowledged_at          = COALESCE(acknowledged_at, NOW()),
	                       acknowledged_by_callsign = CASE
	                           WHEN acknowledged_by_callsign = '' THEN $2
	                           ELSE acknowledged_by_callsign
	                       END
	                   WHERE id = $1`, c.AlarmID, c.OperatorCallsign)
	return &SecurityEvent{ID: id, AlarmID: c.AlarmID, SiteID: c.SiteID, Ts: now, ResolvedAt: now}, nil
}

// ErrSelfVerification is returned by VerifySecurityEvent when the
// supervisor attempting to verify is the same person who originally
// dispositioned the event. UL 827B's four-eyes rule explicitly
// forbids this — a single operator can't both make and approve the
// call. The handler converts this to HTTP 409 Conflict.
var ErrSelfVerification = fmt.Errorf("verifier must be a different operator from the disposing operator")

// ErrAlreadyVerified is returned when the event already has a
// non-null verified_at. We don't allow re-verification — the first
// signature stands, otherwise an attacker with a compromised
// supervisor account could rewrite history.
var ErrAlreadyVerified = fmt.Errorf("event already verified")

// VerifySecurityEvent records a second-operator sign-off on a
// dispositioned event. Returns ErrSelfVerification if the supplied
// verifier is the same user who disposed of the event. Atomic via a
// single conditional UPDATE — the WHERE clause encodes both the
// "not yet verified" and "not the same operator" rules so two
// supervisors racing each other can't both succeed.
func (db *DB) VerifySecurityEvent(ctx context.Context, eventID string, verifierID uuid.UUID, verifierCallsign string) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE security_events
		SET verified_by_user_id  = $2,
		    verified_by_callsign = $3,
		    verified_at          = NOW()
		WHERE id = $1
		  AND verified_at IS NULL
		  AND (disposed_by_user_id IS NULL OR disposed_by_user_id <> $2)`,
		eventID, verifierID, verifierCallsign,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		// Disambiguate between "already verified" and "self-verification
		// attempt" so the API layer can return the right status code.
		var existingVerifiedAt *time.Time
		var disposedBy *uuid.UUID
		errFetch := db.Pool.QueryRow(ctx,
			`SELECT verified_at, disposed_by_user_id FROM security_events WHERE id = $1`,
			eventID,
		).Scan(&existingVerifiedAt, &disposedBy)
		if errFetch != nil {
			return fmt.Errorf("event %q not found", eventID)
		}
		if existingVerifiedAt != nil {
			return ErrAlreadyVerified
		}
		if disposedBy != nil && *disposedBy == verifierID {
			return ErrSelfVerification
		}
		return fmt.Errorf("verification update affected 0 rows")
	}
	return nil
}

func (db *DB) ListSecurityEvents(ctx context.Context, siteID string, viewedOnly *bool) ([]SecurityEvent, error) {
	query := `SELECT id, alarm_id, site_id, camera_id, severity,
	       COALESCE(type,''), COALESCE(description,''),
	       disposition_code, disposition_label, COALESCE(operator_callsign,''), operator_notes,
	       action_log, escalation_depth, COALESCE(clip_url,''), ts, resolved_at, viewed_by_customer,
	       disposed_by_user_id, verified_by_user_id, COALESCE(verified_by_callsign,''), verified_at,
	       COALESCE(avs_factors, '{}'::jsonb), avs_score, COALESCE(avs_rubric_version,'')
	       FROM security_events`
	var args []interface{}
	var conditions []string

	if siteID != "" {
		conditions = append(conditions, fmt.Sprintf("site_id=$%d", len(args)+1))
		args = append(args, siteID)
	}
	if viewedOnly != nil {
		conditions = append(conditions, fmt.Sprintf("viewed_by_customer=$%d", len(args)+1))
		args = append(args, *viewedOnly)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY resolved_at DESC"

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SecurityEvent
	for rows.Next() {
		var e SecurityEvent
		var actionLogJSON, avsFactorsJSON []byte
		var avsScoreInt int
		if err := rows.Scan(&e.ID, &e.AlarmID, &e.SiteID, &e.CameraID, &e.Severity,
			&e.Type, &e.Description,
			&e.DispositionCode, &e.DispositionLabel, &e.OperatorCallsign, &e.OperatorNotes,
			&actionLogJSON, &e.EscalationDepth, &e.ClipURL, &e.Ts, &e.ResolvedAt, &e.ViewedByCustomer,
			&e.DisposedByUserID, &e.VerifiedByUserID, &e.VerifiedByCallsign, &e.VerifiedAt,
			&avsFactorsJSON, &avsScoreInt, &e.AVSRubricVersion); err != nil {
			return nil, err
		}
		json.Unmarshal(actionLogJSON, &e.ActionLog)
		json.Unmarshal(avsFactorsJSON, &e.AVSFactors)
		e.AVSScore = avs.Score(avsScoreInt)
		events = append(events, e)
	}
	if events == nil {
		events = []SecurityEvent{}
	}
	return events, nil
}

func (db *DB) ListIncidents(ctx context.Context, siteID, severity string, limit int) ([]IncidentSummary, error) {
	query := `
		SELECT e.id,
		       COALESCE(NULLIF(e.severity,''), 'medium'),
		       'resolved',
		       COALESCE(NULLIF(e.description,''), NULLIF(e.disposition_label,''), NULLIF(e.type,''), 'Security Event'),
		       e.site_id, COALESCE(s.name, e.site_id),
		       COALESCE(e.camera_id,''), e.ts, COALESCE(e.resolved_at, e.ts), 0,
		       COALESCE(NULLIF(e.type,''), NULLIF(e.disposition_code,''), ''),
		       e.escalation_depth,
		       COALESCE(e.disposition_code,''), COALESCE(e.disposition_label,''),
		       COALESCE(e.operator_callsign,''),
		       COALESCE(c.name, '')
		FROM security_events e
		LEFT JOIN sites s ON s.id = e.site_id
		LEFT JOIN cameras c ON c.id::text = e.camera_id`
	var args []interface{}
	var conds []string
	if siteID != "" {
		conds = append(conds, fmt.Sprintf("e.site_id=$%d", len(args)+1))
		args = append(args, siteID)
	}
	if severity != "" {
		conds = append(conds, fmt.Sprintf("e.severity=$%d", len(args)+1))
		args = append(args, severity)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY e.ts DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []IncidentSummary
	for rows.Next() {
		var inc IncidentSummary
		if err := rows.Scan(&inc.ID, &inc.Severity, &inc.Status, &inc.Title, &inc.SiteID, &inc.SiteName, &inc.CameraID, &inc.Ts, &inc.ResolvedAt, &inc.WorkersIdentified, &inc.Type, &inc.EscalationLevel, &inc.DispositionCode, &inc.DispositionLabel, &inc.OperatorCallsign, &inc.CameraName); err != nil {
			return nil, err
		}
		incidents = append(incidents, inc)
	}
	if incidents == nil {
		incidents = []IncidentSummary{}
	}
	return incidents, nil
}

// ═══════════════════════════════════════════════════════════════
// Active Alarms (live queue from detection pipeline)
// ═══════════════════════════════════════════════════════════════

// GetCameraWithSite looks up a camera's name and its current site assignment.
// Returns empty siteID if the camera is unassigned.
func (db *DB) GetCameraWithSite(ctx context.Context, cameraID string) (cameraName, siteID, siteName string, err error) {
	err = db.Pool.QueryRow(ctx, `
		SELECT c.name, COALESCE(c.site_id,''), COALESCE(s.name,'')
		FROM cameras c
		LEFT JOIN sites s ON s.id = c.site_id
		WHERE c.id::text = $1`, cameraID,
	).Scan(&cameraName, &siteID, &siteName)
	return
}

// ── Incident management ──

// IncidentCorrelationWindow is how far back (in milliseconds) we look for an
// open incident at the same site before creating a new one.
const IncidentCorrelationWindow = 5 * 60 * 1000 // 5 minutes

// FindOpenIncident returns an active incident at the given site whose last alarm
// is within the correlation window, or nil if none exists.
func (db *DB) FindOpenIncident(ctx context.Context, siteID string, nowMs int64) (*Incident, error) {
	cutoff := nowMs - IncidentCorrelationWindow
	var inc Incident
	err := db.Pool.QueryRow(ctx, `
		SELECT id, site_id, site_name, severity, status, alarm_count,
		       camera_ids, camera_names, types, latest_type, description,
		       snapshot_url, clip_url, first_alarm_ts, last_alarm_ts, sla_deadline_ms
		FROM incidents
		WHERE site_id = $1 AND status = 'active' AND last_alarm_ts >= $2
		ORDER BY last_alarm_ts DESC LIMIT 1`, siteID, cutoff,
	).Scan(&inc.ID, &inc.SiteID, &inc.SiteName, &inc.Severity, &inc.Status, &inc.AlarmCount,
		&inc.CameraIDs, &inc.CameraNames, &inc.Types, &inc.LatestType, &inc.Description,
		&inc.SnapshotURL, &inc.ClipURL, &inc.FirstAlarmTs, &inc.LastAlarmTs, &inc.SlaDeadlineMs)
	if err != nil {
		return nil, nil // no open incident found (or error — treat as "none")
	}
	return &inc, nil
}

// CreateIncident inserts a new incident. Returns the created incident.
//
// If inc.ID is empty or doesn't start with "INC-", a fresh INC-YYYY-NNNN
// identifier is assigned before insert. Caller-supplied ids (migration
// replays, tests, explicit re-imports) are accepted verbatim — the
// INC- prefix check is what distinguishes "generated" from "manual".
// See NextIncidentID for the sequencing rationale.
func (db *DB) CreateIncident(ctx context.Context, inc *Incident) error {
	if inc.ID == "" || !strings.HasPrefix(inc.ID, "INC-") {
		id, err := db.NextIncidentID(ctx)
		if err != nil {
			return fmt.Errorf("assign incident id: %w", err)
		}
		inc.ID = id
	}
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO incidents
		  (id, site_id, site_name, severity, status, alarm_count,
		   camera_ids, camera_names, types, latest_type, description,
		   snapshot_url, clip_url, first_alarm_ts, last_alarm_ts, sla_deadline_ms)
		VALUES ($1,$2,$3,$4,'active',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		inc.ID, inc.SiteID, inc.SiteName, inc.Severity, inc.AlarmCount,
		inc.CameraIDs, inc.CameraNames, inc.Types, inc.LatestType, inc.Description,
		inc.SnapshotURL, inc.ClipURL, inc.FirstAlarmTs, inc.LastAlarmTs, inc.SlaDeadlineMs,
	)
	return err
}

// AttachAlarmToIncident adds a new alarm's data to an existing incident:
// increments alarm_count, appends camera/type if new, updates severity/timestamps/snapshot.
func (db *DB) AttachAlarmToIncident(ctx context.Context, incidentID, cameraID, cameraName, eventType, severity, description, snapshotURL, clipURL string, ts, slaDeadlineMs int64) error {
	_, err := db.Pool.Exec(ctx, `
		UPDATE incidents SET
			alarm_count = alarm_count + 1,
			camera_ids = CASE WHEN $2 = ANY(camera_ids) THEN camera_ids
			             ELSE array_append(camera_ids, $2) END,
			camera_names = CASE WHEN $3 = ANY(camera_names) THEN camera_names
			               ELSE array_append(camera_names, $3) END,
			types = CASE WHEN $4 = ANY(types) THEN types
			        ELSE array_append(types, $4) END,
			latest_type = $4,
			description = $5,
			severity = CASE
				WHEN $6 = 'critical' THEN 'critical'
				WHEN $6 = 'high' AND severity NOT IN ('critical') THEN 'high'
				WHEN $6 = 'medium' AND severity NOT IN ('critical','high') THEN 'medium'
				ELSE severity END,
			snapshot_url = CASE WHEN $7 != '' THEN $7 ELSE snapshot_url END,
			clip_url = CASE WHEN $8 != '' THEN $8 ELSE clip_url END,
			last_alarm_ts = $9,
			sla_deadline_ms = $10
		WHERE id = $1`, incidentID, cameraID, cameraName, eventType, description,
		severity, snapshotURL, clipURL, ts, slaDeadlineMs)
	return err
}

// ListActiveIncidents returns all active (unacknowledged) incidents, newest first.
func (db *DB) ListActiveIncidents(ctx context.Context) ([]Incident, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, site_id, site_name, severity, status, alarm_count,
		       camera_ids, camera_names, types, latest_type, description,
		       snapshot_url, clip_url, first_alarm_ts, last_alarm_ts, sla_deadline_ms
		FROM incidents WHERE status = 'active'
		ORDER BY last_alarm_ts DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.SiteID, &inc.SiteName, &inc.Severity, &inc.Status,
			&inc.AlarmCount, &inc.CameraIDs, &inc.CameraNames, &inc.Types, &inc.LatestType,
			&inc.Description, &inc.SnapshotURL, &inc.ClipURL,
			&inc.FirstAlarmTs, &inc.LastAlarmTs, &inc.SlaDeadlineMs); err != nil {
			return nil, err
		}
		incidents = append(incidents, inc)
	}
	return incidents, nil
}

// GetIncidentWithAlarms fetches a single incident by ID along with all its child alarms.
func (db *DB) GetIncidentWithAlarms(ctx context.Context, incidentID string) (*Incident, []ActiveAlarm, error) {
	var inc Incident
	err := db.Pool.QueryRow(ctx, `
		SELECT id, site_id, site_name, severity, status, alarm_count,
		       camera_ids, camera_names, types, latest_type, description,
		       snapshot_url, clip_url, first_alarm_ts, last_alarm_ts, sla_deadline_ms
		FROM incidents WHERE id = $1`, incidentID,
	).Scan(&inc.ID, &inc.SiteID, &inc.SiteName, &inc.Severity, &inc.Status, &inc.AlarmCount,
		&inc.CameraIDs, &inc.CameraNames, &inc.Types, &inc.LatestType, &inc.Description,
		&inc.SnapshotURL, &inc.ClipURL, &inc.FirstAlarmTs, &inc.LastAlarmTs, &inc.SlaDeadlineMs)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT id, COALESCE(incident_id,''), site_id, site_name, camera_id, camera_name, severity, type,
		       description, snapshot_url, clip_url, ts, acknowledged,
		       COALESCE(claimed_by,''), escalation_level, COALESCE(sla_deadline_ms,0),
		       COALESCE(ai_description,''), COALESCE(ai_threat_level,''),
		       COALESCE(ai_recommended_action,''), COALESCE(ai_false_positive_pct,0),
		       COALESCE(ai_detections,'[]'::jsonb),
		       COALESCE(ai_ppe_violations,'[]'::jsonb)
		FROM active_alarms WHERE incident_id = $1
		ORDER BY ts ASC`, incidentID)
	if err != nil {
		return &inc, nil, nil
	}
	defer rows.Close()

	var alarms []ActiveAlarm
	for rows.Next() {
		var a ActiveAlarm
		var aiDetJSON, aiPPEJSON []byte
		if err := rows.Scan(&a.ID, &a.IncidentID, &a.SiteID, &a.SiteName, &a.CameraID, &a.CameraName,
			&a.Severity, &a.Type, &a.Description, &a.SnapshotURL, &a.ClipURL,
			&a.Ts, &a.Acknowledged, &a.ClaimedBy, &a.EscalationLevel, &a.SlaDeadlineMs,
			&a.AIDescription, &a.AIThreatLevel, &a.AIRecommendedAction, &a.AIFalsePositivePct,
			&aiDetJSON, &aiPPEJSON); err != nil {
			return &inc, alarms, nil
		}
		json.Unmarshal(aiDetJSON, &a.AIDetections)
		json.Unmarshal(aiPPEJSON, &a.AIPPEViolations)
		alarms = append(alarms, a)
	}
	if alarms == nil {
		alarms = []ActiveAlarm{}
	}
	return &inc, alarms, nil
}

// AcknowledgeIncident marks an incident and all its child alarms as acknowledged.
func (db *DB) AcknowledgeIncident(ctx context.Context, incidentID string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE incidents SET status = 'acknowledged' WHERE id = $1`, incidentID)
	if err != nil {
		return err
	}
	_, err = db.Pool.Exec(ctx,
		`UPDATE active_alarms SET acknowledged = true WHERE incident_id = $1`, incidentID)
	return err
}

// UpdateIncidentSnapshot sets the snapshot/clip on an incident after async capture.
func (db *DB) UpdateIncidentSnapshot(ctx context.Context, incidentID, clipURL, snapshotURL string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE incidents SET snapshot_url=$2, clip_url=$3 WHERE id=$1`,
		incidentID, snapshotURL, clipURL)
	return err
}

// ── Active alarm operations ──

// CreateActiveAlarm inserts a new alarm. Returns true if newly inserted (false = already exists).
//
// If AlarmCode is empty, the helper assigns one (ALM-YYMMDD-NNNN). A
// caller that already has a code (e.g., mid-retry) can pre-populate the
// field and it will be used verbatim. TriggeringEventID is best-effort:
// callers that don't know the event id (manual test inserts, legacy
// paths) leave it nil and the column stays NULL.
func (db *DB) CreateActiveAlarm(ctx context.Context, a *ActiveAlarm) (bool, error) {
	if a.AlarmCode == "" {
		code, err := db.NextAlarmCode(ctx)
		if err != nil {
			return false, fmt.Errorf("assign alarm code: %w", err)
		}
		a.AlarmCode = code
	}

	tag, err := db.Pool.Exec(ctx, `
		INSERT INTO active_alarms
		  (id, alarm_code, triggering_event_id, incident_id, site_id, site_name,
		   camera_id, camera_name, severity, type, description,
		   snapshot_url, clip_url, ts, sla_deadline_ms)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO NOTHING`,
		a.ID, a.AlarmCode, a.TriggeringEventID, a.IncidentID,
		a.SiteID, a.SiteName, a.CameraID, a.CameraName,
		a.Severity, a.Type, a.Description,
		a.SnapshotURL, a.ClipURL, a.Ts, a.SlaDeadlineMs,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AcknowledgeAlarm marks an alarm as acknowledged (archived) so it no longer
// appears in the SOC dispatch queue.
//
// Records the operator who acknowledged it and when, along with their
// callsign-at-ack-time (denormalized so a future callsign rename
// doesn't rewrite the audit narrative). Pass uuid.Nil and "" for
// system-initiated acks (e.g., dispose-by-incident-close); the SLA
// report filters those out.
//
// Idempotent on the timestamp side — re-acking an already-acked alarm
// preserves the original acknowledged_at, so the SLA metric still
// reflects the first action, not the latest. Achieved via the
// COALESCE on acknowledged_at.
func (db *DB) AcknowledgeAlarm(ctx context.Context, alarmID string, userID uuid.UUID, callsign string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE active_alarms
		 SET acknowledged             = true,
		     acknowledged_at          = COALESCE(acknowledged_at, NOW()),
		     acknowledged_by_user_id  = COALESCE(acknowledged_by_user_id, $2),
		     acknowledged_by_callsign = CASE
		         WHEN acknowledged_by_callsign = '' THEN $3
		         ELSE acknowledged_by_callsign
		     END
		 WHERE id = $1`,
		alarmID, nullableUUID(userID), callsign)
	return err
}

// nullableUUID converts a zero-UUID to a database NULL. The
// acknowledged_by_user_id column is nullable, and inserting the
// 00000000-... sentinel makes "system-initiated ack" indistinguishable
// from "Mr. Zero ack'd it." Using NULL keeps the report cleaner.
func nullableUUID(id uuid.UUID) interface{} {
	if id == (uuid.UUID{}) {
		return nil
	}
	return id
}

// SLAReportRow is one slice of the response-time report — typically
// per-operator or per-day, depending on the grouping. Counts split by
// whether the alarm was ack'd within the SLA deadline.
type SLAReportRow struct {
	Bucket          string  `json:"bucket"` // operator callsign or YYYY-MM-DD
	TotalAlarms     int     `json:"total_alarms"`
	AckedAlarms     int     `json:"acked_alarms"`
	WithinSLA       int     `json:"within_sla"`
	OverSLA         int     `json:"over_sla"`
	AvgAckSec       float64 `json:"avg_ack_sec"`
	P50AckSec       float64 `json:"p50_ack_sec"`
	P95AckSec       float64 `json:"p95_ack_sec"`
}

// GetSLAReport aggregates response-time stats for alarms acknowledged
// within [from, to). Group is either "operator" (rows per
// acknowledged_by_callsign) or "day" (rows per UTC date). Bucketing is
// done in SQL so a year-long report stays in one round trip.
//
// "Within SLA" is computed as acknowledged_at - ts <= sla_deadline_ms,
// where ts and sla_deadline_ms are both already on the alarm row at
// creation time. p50/p95 use Postgres's percentile_cont aggregator.
func (db *DB) GetSLAReport(ctx context.Context, from, to time.Time, group string) ([]SLAReportRow, error) {
	if group != "operator" && group != "day" {
		group = "day"
	}

	bucketExpr := "TO_CHAR(acknowledged_at AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	if group == "operator" {
		bucketExpr = "COALESCE(NULLIF(acknowledged_by_callsign, ''), '<unattributed>')"
	}

	q := `
		SELECT ` + bucketExpr + ` AS bucket,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE acknowledged_at IS NOT NULL) AS acked,
		       COUNT(*) FILTER (
		           WHERE acknowledged_at IS NOT NULL
		             AND EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0))) * 1000 <= sla_deadline_ms
		       ) AS within_sla,
		       COUNT(*) FILTER (
		           WHERE acknowledged_at IS NOT NULL
		             AND EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0))) * 1000 > sla_deadline_ms
		       ) AS over_sla,
		       COALESCE(AVG(EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0))))
		           FILTER (WHERE acknowledged_at IS NOT NULL), 0) AS avg_ack,
		       COALESCE(percentile_cont(0.5) WITHIN GROUP (
		           ORDER BY EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0)))
		       ) FILTER (WHERE acknowledged_at IS NOT NULL), 0) AS p50,
		       COALESCE(percentile_cont(0.95) WITHIN GROUP (
		           ORDER BY EXTRACT(EPOCH FROM (acknowledged_at - to_timestamp(ts/1000.0)))
		       ) FILTER (WHERE acknowledged_at IS NOT NULL), 0) AS p95
		FROM active_alarms
		WHERE ts >= $1 AND ts < $2
		GROUP BY 1
		ORDER BY 1`

	rows, err := db.Pool.Query(ctx, q, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("sla report query: %w", err)
	}
	defer rows.Close()

	var out []SLAReportRow
	for rows.Next() {
		var r SLAReportRow
		if err := rows.Scan(&r.Bucket, &r.TotalAlarms, &r.AckedAlarms,
			&r.WithinSLA, &r.OverSLA, &r.AvgAckSec, &r.P50AckSec, &r.P95AckSec); err != nil {
			return nil, fmt.Errorf("sla report scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListActiveAlarms returns all unacknowledged alarms, newest first.
func (db *DB) ListActiveAlarms(ctx context.Context) ([]ActiveAlarm, error) {
	rows, err := db.Pool.Query(ctx,
		`SELECT id, COALESCE(alarm_code,''), triggering_event_id,
		        COALESCE(incident_id,''), site_id, site_name, camera_id, camera_name, severity, type,
		        description, snapshot_url, clip_url, ts, acknowledged,
		        COALESCE(claimed_by,''), escalation_level, COALESCE(sla_deadline_ms,0),
		        COALESCE(ai_description,''), COALESCE(ai_threat_level,''),
		        COALESCE(ai_recommended_action,''), COALESCE(ai_false_positive_pct,0),
		       COALESCE(ai_detections,'[]'::jsonb),
		       COALESCE(ai_ppe_violations,'[]'::jsonb)
		 FROM active_alarms WHERE acknowledged = false
		 ORDER BY ts DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alarms []ActiveAlarm
	for rows.Next() {
		var a ActiveAlarm
		var aiDetJSON, aiPPEJSON []byte
		if err := rows.Scan(&a.ID, &a.AlarmCode, &a.TriggeringEventID,
			&a.IncidentID, &a.SiteID, &a.SiteName, &a.CameraID, &a.CameraName,
			&a.Severity, &a.Type, &a.Description, &a.SnapshotURL, &a.ClipURL,
			&a.Ts, &a.Acknowledged, &a.ClaimedBy, &a.EscalationLevel, &a.SlaDeadlineMs,
			&a.AIDescription, &a.AIThreatLevel, &a.AIRecommendedAction, &a.AIFalsePositivePct,
			&aiDetJSON, &aiPPEJSON); err != nil {
			return nil, err
		}
		json.Unmarshal(aiDetJSON, &a.AIDetections)
		json.Unmarshal(aiPPEJSON, &a.AIPPEViolations)
		alarms = append(alarms, a)
	}
	return alarms, nil
}

// GetActiveAlarmsCount returns the number of unacknowledged alarms and the timestamp of the oldest one.
func (db *DB) GetActiveAlarmsCount(ctx context.Context) (count int, oldestTs int64, err error) {
	err = db.Pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(MIN(ts), 0) FROM active_alarms WHERE acknowledged = false`,
	).Scan(&count, &oldestTs)
	return
}

// UpdateActiveAlarmClip sets the clip_url on an alarm after async clip capture.
func (db *DB) UpdateActiveAlarmClip(ctx context.Context, alarmID, clipURL, snapshotURL string) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE active_alarms SET clip_url=$2, snapshot_url=$3 WHERE id=$1`,
		alarmID, clipURL, snapshotURL)
	return err
}

// UpdateAlarmAI persists AI pipeline results (YOLO + Qwen) on an active alarm.
func (db *DB) UpdateAlarmAI(ctx context.Context, alarmID, description, threatLevel, recommendedAction string, fpPct float64, detectionsJSON, ppeViolationsJSON []byte) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE active_alarms SET ai_description=$2, ai_threat_level=$3, ai_recommended_action=$4, ai_false_positive_pct=$5, ai_detections=$6, ai_ppe_violations=$7 WHERE id=$1`,
		alarmID, description, threatLevel, recommendedAction, fpPct, detectionsJSON, ppeViolationsJSON)
	return err
}

// SetAlarmAIFeedback records the operator's explicit feedback on the AI assessment.
func (db *DB) SetAlarmAIFeedback(ctx context.Context, alarmID string, agreed bool) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE active_alarms SET ai_operator_agreed=$2 WHERE id=$1`,
		alarmID, agreed)
	return err
}

// ComputeAICorrectness compares the AI threat assessment against the operator's
// disposition and stores the result. Called when the alarm is resolved.
// Logic: AI said high/critical + disposition is verified_* → AI was correct
//        AI said high/critical + disposition is false_positive_* → AI was wrong
//        AI said low/none + disposition is verified_* → AI was wrong (missed threat)
//        AI said low/none + disposition is false_positive_* → AI was correct
func (db *DB) ComputeAICorrectness(ctx context.Context, alarmID, dispositionCode string) error {
	var aiThreatLevel string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(ai_threat_level,'') FROM active_alarms WHERE id=$1`, alarmID,
	).Scan(&aiThreatLevel)
	if err != nil || aiThreatLevel == "" {
		return nil // no AI assessment, nothing to compute
	}

	aiSaidThreat := aiThreatLevel == "critical" || aiThreatLevel == "high"
	isFalsePositive := len(dispositionCode) > 0 && dispositionCode[:5] == "false"
	aiWasCorrect := (aiSaidThreat && !isFalsePositive) || (!aiSaidThreat && isFalsePositive)

	_, err = db.Pool.Exec(ctx,
		`UPDATE active_alarms SET ai_was_correct=$2 WHERE id=$1`,
		alarmID, aiWasCorrect)
	return err
}

// EscalateActiveAlarm bumps the escalation_level on an active alarm.
func (db *DB) EscalateActiveAlarm(ctx context.Context, alarmID string, level int) error {
	_, err := db.Pool.Exec(ctx,
		`UPDATE active_alarms SET escalation_level=$2 WHERE id=$1`, alarmID, level)
	return err
}

// GetSecurityEventByID fetches one security_event by ID and maps it to IncidentDetail.
func (db *DB) GetSecurityEventByID(ctx context.Context, id string) (*IncidentDetail, error) {
	var (
		evtID, alarmID, siteID, cameraID                        string
		severity, evtType, description                          string
		dispositionCode, dispositionLabel                       string
		operatorCallsign, operatorNotes                         string
		actionLogJSON                                           []byte
		clipURL                                                 string
		ts, resolvedAt                                          int64
	)
	err := db.Pool.QueryRow(ctx, `
		SELECT id, COALESCE(alarm_id,''), site_id, COALESCE(camera_id,''),
		       COALESCE(severity,'medium'), COALESCE(type,''), COALESCE(description,''),
		       COALESCE(disposition_code,''), COALESCE(disposition_label,''),
		       COALESCE(operator_callsign,''), COALESCE(operator_notes,''),
		       COALESCE(action_log,'[]'::jsonb), COALESCE(clip_url,''), ts, resolved_at
		FROM security_events WHERE id=$1`, id,
	).Scan(&evtID, &alarmID, &siteID, &cameraID,
		&severity, &evtType, &description,
		&dispositionCode, &dispositionLabel,
		&operatorCallsign, &operatorNotes,
		&actionLogJSON, &clipURL, &ts, &resolvedAt)
	if err != nil {
		return nil, err
	}

	// Look up site and camera names
	var siteName, cameraName string
	db.Pool.QueryRow(ctx, `SELECT COALESCE(name,'') FROM sites WHERE id=$1`, siteID).Scan(&siteName)
	if cameraID != "" {
		db.Pool.QueryRow(ctx, `SELECT COALESCE(name,'') FROM cameras WHERE id::text=$1`, cameraID).Scan(&cameraName)
	}

	// Map action_log → timeline
	var rawLog []map[string]interface{}
	json.Unmarshal(actionLogJSON, &rawLog)
	timeline := make([]map[string]interface{}, 0, len(rawLog))
	for _, entry := range rawLog {
		timeline = append(timeline, map[string]interface{}{
			"ts":    entry["ts"],
			"label": entry["text"],
			"type":  "action",
		})
	}

	title := description
	if title == "" {
		title = dispositionLabel
	}
	if title == "" {
		title = evtType
	}
	if title == "" {
		title = "Security Event"
	}

	return &IncidentDetail{
		ID:                 evtID,
		Severity:           severity,
		Status:             "resolved",
		Title:              title,
		SiteID:             siteID,
		SiteName:           siteName,
		CameraID:           cameraID,
		CameraName:         cameraName,
		Ts:                 ts,
		DurationMs:         resolvedAt - ts,
		WorkersIdentified:  0,
		AIConfidence:       0,
		AICaption:          description,
		Findings:           []map[string]interface{}{},
		Detections:         []map[string]interface{}{},
		Workers:            []map[string]interface{}{},
		Timeline:           timeline,
		Notifications:      []map[string]interface{}{},
		Comments:           []map[string]interface{}{},
		ClipURL:            clipURL,
		Keyframes:          []map[string]interface{}{},
		OSHAClassification: "",
		RelatedIncidents:   []string{},
		OperatorCallsign:   operatorCallsign,
		OperatorNotes:      operatorNotes,
		DispositionCode:    dispositionCode,
		DispositionLabel:   dispositionLabel,
	}, nil
}

// ═══════════════════════════════════════════════════════════════
// Shift Handoffs
// ═══════════════════════════════════════════════════════════════

func (db *DB) CreateHandoff(ctx context.Context, h *ShiftHandoffCreate) (*ShiftHandoff, error) {
	siteLocks, _ := json.Marshal(h.LockedSiteIDs)
	activeAlerts, _ := json.Marshal(h.ActiveAlertIDs)
	var id int64
	var createdAt time.Time
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO shift_handoffs
		  (from_operator_id, from_operator_callsign, to_operator_id, to_operator_callsign, notes, site_locks, pending_alarms)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at`,
		h.FromOperatorID, h.FromOperatorCallsign, h.ToOperatorID, h.ToOperatorCallsign,
		h.Notes, siteLocks, activeAlerts,
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, err
	}
	return &ShiftHandoff{
		ID: id, FromOperatorID: h.FromOperatorID, FromOperatorCallsign: h.FromOperatorCallsign,
		ToOperatorID: h.ToOperatorID, ToOperatorCallsign: h.ToOperatorCallsign,
		LockedSiteIDs: h.LockedSiteIDs, ActiveAlertIDs: h.ActiveAlertIDs,
		Notes: h.Notes, Status: "pending", CreatedAt: createdAt,
	}, nil
}

func (db *DB) ListHandoffs(ctx context.Context, toOperatorID string) ([]ShiftHandoff, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, from_operator_id, from_operator_callsign, to_operator_id, to_operator_callsign,
		       notes, site_locks, pending_alarms, status, created_at, accepted_at
		FROM shift_handoffs
		WHERE to_operator_id=$1
		ORDER BY created_at DESC LIMIT 20`, toOperatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var handoffs []ShiftHandoff
	for rows.Next() {
		var h ShiftHandoff
		var siteLocks, pendingAlarms []byte
		if err := rows.Scan(&h.ID, &h.FromOperatorID, &h.FromOperatorCallsign, &h.ToOperatorID, &h.ToOperatorCallsign,
			&h.Notes, &siteLocks, &pendingAlarms, &h.Status, &h.CreatedAt, &h.AcceptedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(siteLocks, &h.LockedSiteIDs)
		json.Unmarshal(pendingAlarms, &h.ActiveAlertIDs)
		if h.LockedSiteIDs == nil {
			h.LockedSiteIDs = []string{}
		}
		if h.ActiveAlertIDs == nil {
			h.ActiveAlertIDs = []string{}
		}
		handoffs = append(handoffs, h)
	}
	if handoffs == nil {
		handoffs = []ShiftHandoff{}
	}
	return handoffs, nil
}

// ═══════════════════════════════════════════════════════════════
// Camera site assignment
// ═══════════════════════════════════════════════════════════════

func (db *DB) AssignCameraToSite(ctx context.Context, cameraID, siteID, location string) error {
	// Close any open assignment record for this device
	db.Pool.Exec(ctx, `UPDATE device_assignments SET removed_at=NOW()
		WHERE device_type='camera' AND device_id=$1 AND removed_at IS NULL`, cameraID)
	_, err := db.Pool.Exec(ctx, `UPDATE cameras SET site_id=$2, location=$3 WHERE id=$1`, cameraID, siteID, location)
	if err != nil {
		return err
	}
	// Open a new assignment record
	db.Pool.Exec(ctx, `INSERT INTO device_assignments (device_type, device_id, site_id, location_label)
		VALUES ('camera', $1, $2, $3)`, cameraID, siteID, location)
	return nil
}

func (db *DB) UnassignCameraFromSite(ctx context.Context, cameraID string) error {
	db.Pool.Exec(ctx, `UPDATE device_assignments SET removed_at=NOW()
		WHERE device_type='camera' AND device_id=$1 AND removed_at IS NULL`, cameraID)
	_, err := db.Pool.Exec(ctx, `UPDATE cameras SET site_id=NULL, location='' WHERE id=$1`, cameraID)
	return err
}

func (db *DB) GetSiteCameras(ctx context.Context, siteID string) ([]Camera, error) {
	rows, err := db.Pool.Query(ctx, `SELECT id, name, onvif_address, status, manufacturer, model, rtsp_uri, recording, COALESCE(site_id, ''), COALESCE(location, '') FROM cameras WHERE site_id=$1`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var c Camera
		var siteID, location string
		if err := rows.Scan(&c.ID, &c.Name, &c.OnvifAddress, &c.Status, &c.Manufacturer, &c.Model, &c.RTSPUri, &c.Recording, &siteID, &location); err != nil {
			return nil, err
		}
		c.CameraGroup = location // reuse CameraGroup field for location
		cameras = append(cameras, c)
	}
	if cameras == nil {
		cameras = []Camera{}
	}
	return cameras, nil
}

// ═══════════════════════════════════════════════════════════════
// Platform Camera Registry (all cameras with site assignment info)
// ═══════════════════════════════════════════════════════════════

func (db *DB) ListAllPlatformCameras(ctx context.Context) ([]PlatformCamera, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id::text, name, COALESCE(onvif_address,''), COALESCE(manufacturer,''),
		       COALESCE(model,''), status, COALESCE(site_id,''), COALESCE(location,''), recording
		FROM cameras ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cameras []PlatformCamera
	for rows.Next() {
		var c PlatformCamera
		if err := rows.Scan(&c.ID, &c.Name, &c.OnvifAddress, &c.Manufacturer,
			&c.Model, &c.Status, &c.SiteID, &c.Location, &c.Recording); err != nil {
			return nil, err
		}
		cameras = append(cameras, c)
	}
	if cameras == nil {
		cameras = []PlatformCamera{}
	}
	return cameras, nil
}

// ═══════════════════════════════════════════════════════════════
// Speaker site assignment
// ═══════════════════════════════════════════════════════════════

func (db *DB) ListAllPlatformSpeakers(ctx context.Context) ([]PlatformSpeaker, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id::text, name, COALESCE(onvif_address,''), COALESCE(zone,''),
		       COALESCE(location,''), status, COALESCE(site_id,''),
		       COALESCE(manufacturer,''), COALESCE(model,'')
		FROM speakers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var speakers []PlatformSpeaker
	for rows.Next() {
		var s PlatformSpeaker
		if err := rows.Scan(&s.ID, &s.Name, &s.OnvifAddress, &s.Zone,
			&s.Location, &s.Status, &s.SiteID, &s.Manufacturer, &s.Model); err != nil {
			return nil, err
		}
		speakers = append(speakers, s)
	}
	if speakers == nil {
		speakers = []PlatformSpeaker{}
	}
	return speakers, nil
}

func (db *DB) AssignSpeakerToSite(ctx context.Context, speakerID, siteID, location string) error {
	db.Pool.Exec(ctx, `UPDATE device_assignments SET removed_at=NOW()
		WHERE device_type='speaker' AND device_id=$1 AND removed_at IS NULL`, speakerID)
	_, err := db.Pool.Exec(ctx,
		`UPDATE speakers SET site_id=$2, location=$3 WHERE id=$1::uuid`, speakerID, siteID, location)
	if err != nil {
		return err
	}
	db.Pool.Exec(ctx, `INSERT INTO device_assignments (device_type, device_id, site_id, location_label)
		VALUES ('speaker', $1, $2, $3)`, speakerID, siteID, location)
	return nil
}

func (db *DB) UnassignSpeakerFromSite(ctx context.Context, speakerID string) error {
	db.Pool.Exec(ctx, `UPDATE device_assignments SET removed_at=NOW()
		WHERE device_type='speaker' AND device_id=$1 AND removed_at IS NULL`, speakerID)
	_, err := db.Pool.Exec(ctx,
		`UPDATE speakers SET site_id=NULL, location='' WHERE id=$1::uuid`, speakerID)
	return err
}

func (db *DB) GetDeviceHistory(ctx context.Context, deviceType, deviceID string) ([]DeviceAssignment, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, device_type, device_id, site_id, location_label, assigned_at, removed_at
		FROM device_assignments
		WHERE device_type=$1 AND device_id=$2
		ORDER BY assigned_at DESC`, deviceType, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []DeviceAssignment
	for rows.Next() {
		var d DeviceAssignment
		if err := rows.Scan(&d.ID, &d.DeviceType, &d.DeviceID, &d.SiteID,
			&d.LocationLabel, &d.AssignedAt, &d.RemovedAt); err != nil {
			return nil, err
		}
		history = append(history, d)
	}
	if history == nil {
		history = []DeviceAssignment{}
	}
	return history, nil
}
