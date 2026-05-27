package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ── PPE Zone CRUD ─────────────────────────────────────────────────────────────

// ListPPEZones returns all live zones for a camera, scoped to orgID.
// Returns an empty (non-nil) slice when none exist.
func (db *DB) ListPPEZones(ctx context.Context, cameraID uuid.UUID, orgID string) ([]PPEZone, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, organization_id, camera_id, site_id,
		       zone_type, name, region, enabled, notes,
		       created_by, created_at, updated_at
		FROM ppe_zones_active
		WHERE camera_id = $1 AND organization_id = $2
		ORDER BY created_at`,
		cameraID, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListPPEZones: %w", err)
	}
	defer rows.Close()

	out := []PPEZone{}
	for rows.Next() {
		z, err := scanPPEZone(rows)
		if err != nil {
			return nil, fmt.Errorf("ListPPEZones scan: %w", err)
		}
		out = append(out, z)
	}
	return out, nil
}

// GetPPEZone fetches a single live zone by ID, scoped to orgID.
// Returns (nil, nil) when not found, wrong org, or soft-deleted.
func (db *DB) GetPPEZone(ctx context.Context, id uuid.UUID, orgID string) (*PPEZone, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, organization_id, camera_id, site_id,
		       zone_type, name, region, enabled, notes,
		       created_by, created_at, updated_at
		FROM ppe_zones_active
		WHERE id = $1 AND organization_id = $2`,
		id, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("GetPPEZone: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	z, err := scanPPEZone(rows)
	if err != nil {
		return nil, fmt.Errorf("GetPPEZone scan: %w", err)
	}
	return &z, nil
}

// ListPPEZonesIncludeDeleted returns all zones for a camera (including soft-deleted),
// scoped to orgID. Admin-only path.
func (db *DB) ListPPEZonesIncludeDeleted(ctx context.Context, cameraID uuid.UUID, orgID string) ([]PPEZone, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT id, organization_id, camera_id, site_id,
		       zone_type, name, region, enabled, notes,
		       created_by, created_at, updated_at
		FROM ppe_zones
		WHERE camera_id = $1 AND organization_id = $2
		ORDER BY created_at`,
		cameraID, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListPPEZonesIncludeDeleted: %w", err)
	}
	defer rows.Close()

	out := []PPEZone{}
	for rows.Next() {
		z, err := scanPPEZone(rows)
		if err != nil {
			return nil, fmt.Errorf("ListPPEZonesIncludeDeleted scan: %w", err)
		}
		out = append(out, z)
	}
	return out, nil
}

// CreatePPEZone inserts a new zone. Returns the created row.
func (db *DB) CreatePPEZone(ctx context.Context, cameraID uuid.UUID, orgID string, siteID *string, createdBy *uuid.UUID, inp PPEZoneCreate) (PPEZone, error) {
	regionJSON, err := json.Marshal(inp.Region)
	if err != nil {
		return PPEZone{}, fmt.Errorf("CreatePPEZone marshal region: %w", err)
	}

	rows, err := db.Pool.Query(ctx, `
		INSERT INTO ppe_zones
		    (organization_id, camera_id, site_id, zone_type, name, region, enabled, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, organization_id, camera_id, site_id,
		          zone_type, name, region, enabled, notes,
		          created_by, created_at, updated_at`,
		orgID, cameraID, siteID, inp.ZoneType, inp.Name, regionJSON, inp.Enabled, inp.Notes, createdBy,
	)
	if err != nil {
		return PPEZone{}, fmt.Errorf("CreatePPEZone: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return PPEZone{}, fmt.Errorf("CreatePPEZone: no row returned")
	}
	z, err := scanPPEZone(rows)
	if err != nil {
		return PPEZone{}, fmt.Errorf("CreatePPEZone scan: %w", err)
	}
	return z, nil
}

// UpdatePPEZone updates an existing zone. Returns rows affected (0 = not found/wrong org).
func (db *DB) UpdatePPEZone(ctx context.Context, id uuid.UUID, orgID string, inp PPEZoneCreate) (int64, error) {
	regionJSON, err := json.Marshal(inp.Region)
	if err != nil {
		return 0, fmt.Errorf("UpdatePPEZone marshal region: %w", err)
	}
	tag, err := db.Pool.Exec(ctx, `
		UPDATE ppe_zones
		SET zone_type=$1, name=$2, region=$3, enabled=$4, notes=$5, updated_at=NOW()
		WHERE id=$6 AND organization_id=$7`,
		inp.ZoneType, inp.Name, regionJSON, inp.Enabled, inp.Notes, id, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("UpdatePPEZone: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeletePPEZone removes a zone. Returns rows affected.
func (db *DB) DeletePPEZone(ctx context.Context, id uuid.UUID, orgID string) (int64, error) {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM ppe_zones WHERE id=$1 AND organization_id=$2`,
		id, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("DeletePPEZone: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SoftDeletePPEZone marks a zone and its compliance rules as deleted.
// The 409 guard (CountComplianceRulesForZone) is removed from the handler
// when using this function — zones can always be soft-deleted; their rules
// go with them.
func (db *DB) SoftDeletePPEZone(ctx context.Context, id uuid.UUID, orgID string) (int64, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("SoftDeletePPEZone begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()

	// Cascade: soft-delete compliance rules referencing this zone.
	_, err = tx.Exec(ctx,
		`UPDATE compliance_rules SET deleted_at=$1
		 WHERE zone_id=$2 AND organization_id=$3 AND deleted_at IS NULL`,
		now, id, orgID)
	if err != nil {
		return 0, fmt.Errorf("SoftDeletePPEZone cascade compliance_rules: %w", err)
	}

	// Soft-delete the zone itself.
	tag, err := tx.Exec(ctx,
		`UPDATE ppe_zones SET deleted_at=$1
		 WHERE id=$2 AND organization_id=$3 AND deleted_at IS NULL`,
		now, id, orgID)
	if err != nil {
		return 0, fmt.Errorf("SoftDeletePPEZone: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("SoftDeletePPEZone commit: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountComplianceRulesForZone returns the number of live compliance rules referencing
// the given zone. Uses the _active view so soft-deleted rules are not counted.
// Retained for any caller that needs the count; the DELETE handler no longer
// uses it as a 409 guard (SoftDeletePPEZone cascades rules).
func (db *DB) CountComplianceRulesForZone(ctx context.Context, zoneID uuid.UUID, orgID string) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM compliance_rules_active WHERE zone_id=$1 AND organization_id=$2`,
		zoneID, orgID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountComplianceRulesForZone: %w", err)
	}
	return n, nil
}

// ── Compliance Rule CRUD ──────────────────────────────────────────────────────

// ListComplianceRules returns all live rules for a camera (including site-wide rules
// where camera_id IS NULL and site_id matches), scoped to orgID.
// Zone name + type are joined in from ppe_zones_active.
func (db *DB) ListComplianceRules(ctx context.Context, cameraID uuid.UUID, orgID string) ([]ComplianceRule, error) {
	// Fetch the camera's site_id for the site-wide rule join.
	var siteID string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(site_id,'') FROM cameras_active WHERE id=$1`, cameraID,
	).Scan(&siteID)
	if err != nil {
		return nil, fmt.Errorf("ListComplianceRules get site_id: %w", err)
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT r.id, r.organization_id, r.site_id, r.camera_id, r.zone_id,
		       r.rule_type, r.ppe_classes, r.enabled, r.notes, r.created_by, r.created_at,
		       COALESCE(z.name,'') AS zone_name,
		       COALESCE(z.zone_type,'') AS zone_type
		FROM compliance_rules_active r
		LEFT JOIN ppe_zones_active z ON z.id = r.zone_id
		WHERE r.organization_id = $1
		  AND (r.camera_id = $2 OR (r.camera_id IS NULL AND r.site_id = $3))
		ORDER BY r.created_at`,
		orgID, cameraID, siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListComplianceRules: %w", err)
	}
	defer rows.Close()

	out := []ComplianceRule{}
	for rows.Next() {
		r, err := scanComplianceRule(rows)
		if err != nil {
			return nil, fmt.Errorf("ListComplianceRules scan: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// GetComplianceRule fetches a single live rule by ID, scoped to orgID.
// Returns (nil, nil) when not found, wrong org, or soft-deleted.
func (db *DB) GetComplianceRule(ctx context.Context, id uuid.UUID, orgID string) (*ComplianceRule, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT r.id, r.organization_id, r.site_id, r.camera_id, r.zone_id,
		       r.rule_type, r.ppe_classes, r.enabled, r.notes, r.created_by, r.created_at,
		       COALESCE(z.name,'') AS zone_name,
		       COALESCE(z.zone_type,'') AS zone_type
		FROM compliance_rules_active r
		LEFT JOIN ppe_zones_active z ON z.id = r.zone_id
		WHERE r.id=$1 AND r.organization_id=$2`,
		id, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("GetComplianceRule: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	r, err := scanComplianceRule(rows)
	if err != nil {
		return nil, fmt.Errorf("GetComplianceRule scan: %w", err)
	}
	return &r, nil
}

// ListComplianceRulesIncludeDeleted returns all rules for a camera (including
// soft-deleted), scoped to orgID. Admin-only path.
func (db *DB) ListComplianceRulesIncludeDeleted(ctx context.Context, cameraID uuid.UUID, orgID string) ([]ComplianceRule, error) {
	var siteID string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(site_id,'') FROM cameras WHERE id=$1`, cameraID,
	).Scan(&siteID)
	if err != nil {
		return nil, fmt.Errorf("ListComplianceRulesIncludeDeleted get site_id: %w", err)
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT r.id, r.organization_id, r.site_id, r.camera_id, r.zone_id,
		       r.rule_type, r.ppe_classes, r.enabled, r.notes, r.created_by, r.created_at,
		       COALESCE(z.name,'') AS zone_name,
		       COALESCE(z.zone_type,'') AS zone_type
		FROM compliance_rules r
		LEFT JOIN ppe_zones z ON z.id = r.zone_id
		WHERE r.organization_id = $1
		  AND (r.camera_id = $2 OR (r.camera_id IS NULL AND r.site_id = $3))
		ORDER BY r.created_at`,
		orgID, cameraID, siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListComplianceRulesIncludeDeleted: %w", err)
	}
	defer rows.Close()

	out := []ComplianceRule{}
	for rows.Next() {
		r, err := scanComplianceRule(rows)
		if err != nil {
			return nil, fmt.Errorf("ListComplianceRulesIncludeDeleted scan: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// CreateComplianceRule inserts a new compliance rule. Returns the new UUID.
func (db *DB) CreateComplianceRule(ctx context.Context, cameraID uuid.UUID, orgID string, siteID *string, createdBy *uuid.UUID, inp ComplianceRuleCreate) (uuid.UUID, error) {
	classJSON, err := json.Marshal(inp.PPEClasses)
	if err != nil {
		return uuid.Nil, fmt.Errorf("CreateComplianceRule marshal ppe_classes: %w", err)
	}

	// site-wide: store camera_id as NULL.
	var camIDPtr *uuid.UUID
	if !inp.SiteWide {
		camIDPtr = &cameraID
	}

	var newID uuid.UUID
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO compliance_rules
		    (organization_id, site_id, camera_id, zone_id, rule_type, ppe_classes, enabled, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		orgID, siteID, camIDPtr, inp.ZoneID, inp.RuleType, classJSON, inp.Enabled, inp.Notes, createdBy,
	).Scan(&newID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("CreateComplianceRule: %w", err)
	}
	return newID, nil
}

// UpdateComplianceRule updates an existing rule's mutable fields.
func (db *DB) UpdateComplianceRule(ctx context.Context, id uuid.UUID, orgID string, cameraID uuid.UUID, inp ComplianceRuleCreate) (int64, error) {
	classJSON, err := json.Marshal(inp.PPEClasses)
	if err != nil {
		return 0, fmt.Errorf("UpdateComplianceRule marshal ppe_classes: %w", err)
	}
	var camIDPtr *uuid.UUID
	if !inp.SiteWide {
		camIDPtr = &cameraID
	}
	tag, err := db.Pool.Exec(ctx, `
		UPDATE compliance_rules
		SET zone_id=$1, rule_type=$2, ppe_classes=$3, enabled=$4, notes=$5, camera_id=$6, updated_at=NOW()
		WHERE id=$7 AND organization_id=$8`,
		inp.ZoneID, inp.RuleType, classJSON, inp.Enabled, inp.Notes, camIDPtr, id, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("UpdateComplianceRule: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteComplianceRule removes a compliance rule (hard delete — retained for
// internal/admin use; API handlers use SoftDeleteComplianceRule).
func (db *DB) DeleteComplianceRule(ctx context.Context, id uuid.UUID, orgID string) (int64, error) {
	tag, err := db.Pool.Exec(ctx,
		`DELETE FROM compliance_rules WHERE id=$1 AND organization_id=$2`,
		id, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("DeleteComplianceRule: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SoftDeleteComplianceRule marks a single compliance rule as deleted.
// Returns rows affected (0 = not found, wrong org, or already deleted).
func (db *DB) SoftDeleteComplianceRule(ctx context.Context, id uuid.UUID, orgID string) (int64, error) {
	tag, err := db.Pool.Exec(ctx,
		`UPDATE compliance_rules SET deleted_at=$1
		 WHERE id=$2 AND organization_id=$3 AND deleted_at IS NULL`,
		time.Now().UTC(), id, orgID,
	)
	if err != nil {
		return 0, fmt.Errorf("SoftDeleteComplianceRule: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ── Worker query ──────────────────────────────────────────────────────────────

// ListZonesAndRulesForCamera returns all live, enabled compliance rules for the given
// camera (including site-wide rules where camera_id IS NULL) with zone polygons
// joined in. This is the single JOIN query called by the PPE worker once per
// camera poll cycle — no N+1. Both _active views filter deleted_at IS NULL.
func (db *DB) ListZonesAndRulesForCamera(ctx context.Context, cameraID uuid.UUID, siteID string, orgID string) ([]ComplianceRuleWithZone, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT r.id, r.organization_id, r.site_id, r.camera_id, r.zone_id,
		       r.rule_type, r.ppe_classes, r.enabled, r.notes, r.created_by, r.created_at,
		       z.id, z.organization_id, z.camera_id, z.site_id,
		       z.zone_type, z.name, z.region, z.enabled, z.notes,
		       z.created_by, z.created_at, z.updated_at
		FROM compliance_rules_active r
		JOIN ppe_zones_active z ON z.id = r.zone_id
		WHERE r.organization_id = $1
		  AND r.enabled = TRUE
		  AND z.enabled = TRUE
		  AND (r.camera_id = $2 OR (r.camera_id IS NULL AND r.site_id = $3))`,
		orgID, cameraID, siteID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListZonesAndRulesForCamera: %w", err)
	}
	defer rows.Close()

	var out []ComplianceRuleWithZone
	for rows.Next() {
		var r ComplianceRule
		var rSiteID *string
		var rCameraID *uuid.UUID
		var rCreatedBy *uuid.UUID
		var ppeClassesRaw []byte

		var z PPEZone
		var zSiteID *string
		var zCreatedBy *uuid.UUID
		var zRegionRaw []byte
		var zNotes *string

		if err := rows.Scan(
			&r.ID, &r.OrganizationID, &rSiteID, &rCameraID, &r.ZoneID,
			&r.RuleType, &ppeClassesRaw, &r.Enabled, &r.Notes, &rCreatedBy, &r.CreatedAt,
			&z.ID, &z.OrganizationID, &z.CameraID, &zSiteID,
			&z.ZoneType, &z.Name, &zRegionRaw, &z.Enabled, &zNotes,
			&zCreatedBy, &z.CreatedAt, &z.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListZonesAndRulesForCamera scan: %w", err)
		}
		r.SiteID = rSiteID
		r.CameraID = rCameraID
		r.CreatedBy = rCreatedBy
		r.SiteWide = rCameraID == nil

		if ppeClassesRaw != nil {
			if err := json.Unmarshal(ppeClassesRaw, &r.PPEClasses); err != nil {
				r.PPEClasses = nil
			}
		}
		z.SiteID = zSiteID
		z.Notes = zNotes
		z.CreatedBy = zCreatedBy

		if zRegionRaw != nil {
			if err := json.Unmarshal(zRegionRaw, &z.Region); err != nil {
				z.Region = nil
			}
		}

		out = append(out, ComplianceRuleWithZone{ComplianceRule: r, Zone: z})
	}
	return out, nil
}

// ── row scanners ─────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanPPEZone(row scanner) (PPEZone, error) {
	var z PPEZone
	var siteID *string
	var createdBy *uuid.UUID
	var regionRaw []byte

	if err := row.Scan(
		&z.ID, &z.OrganizationID, &z.CameraID, &siteID,
		&z.ZoneType, &z.Name, &regionRaw, &z.Enabled, &z.Notes,
		&createdBy, &z.CreatedAt, &z.UpdatedAt,
	); err != nil {
		return PPEZone{}, err
	}
	z.SiteID = siteID
	z.CreatedBy = createdBy
	if regionRaw != nil {
		if err := json.Unmarshal(regionRaw, &z.Region); err != nil {
			z.Region = []Point{}
		}
	} else {
		z.Region = []Point{}
	}
	return z, nil
}

func scanComplianceRule(row scanner) (ComplianceRule, error) {
	var r ComplianceRule
	var siteID *string
	var camID *uuid.UUID
	var createdBy *uuid.UUID
	var ppeClassesRaw []byte

	if err := row.Scan(
		&r.ID, &r.OrganizationID, &siteID, &camID, &r.ZoneID,
		&r.RuleType, &ppeClassesRaw, &r.Enabled, &r.Notes, &createdBy, &r.CreatedAt,
		&r.ZoneName, &r.ZoneType,
	); err != nil {
		return ComplianceRule{}, err
	}
	r.SiteID = siteID
	r.CameraID = camID
	r.CreatedBy = createdBy
	r.SiteWide = camID == nil

	if ppeClassesRaw != nil {
		if err := json.Unmarshal(ppeClassesRaw, &r.PPEClasses); err != nil {
			r.PPEClasses = []string{}
		}
	} else {
		r.PPEClasses = []string{}
	}
	return r, nil
}
