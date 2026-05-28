// Package dualwrite implements P4-SCHEMA-02 parallel-write mode.
//
// Every legacy detection-equivalent write path (PPE queue, ONVIF events,
// sense webhook events, alarm AI PPE violations, VLM verdict writes) calls
// DualWrite after its primary legacy INSERT succeeds.  If the detections
// insert fails, the failure is logged and counted but the legacy write is
// NOT rolled back — the legacy table remains authoritative during the
// P4-SCHEMA-02/03/04 migration window.
//
// # Detection-class mapping
//
// Legacy sources use a variety of string labels.  This package translates
// them to canonical detection_class values written into detections:
//
//   - PPE worker: YOLO class → normalised PPE class ("no-hardhat" etc.)
//   - ONVIF / sense events: event_type → security class ("intrusion" etc.)
//   - Alarm AI PPE violations: violation_type → PPE class
//   - VLM verdicts: write the detection_class forwarded from the PRQ row
//
// # Model version cache
//
// InsertModelVersion and InsertAnalysisRun carry cost on every call.
// The package holds an in-process cache of model_version_id keyed by
// (organization_id, model_name, version_tag) with a negligible footprint
// (at fleet scale: 90 orgs × 3 model types = <300 entries).  Cache is
// populated lazily; eviction is never needed because model versions are
// append-only.
//
// # Analysis runs
//
// Each distinct pipeline invocation gets one analysis_run row.  A
// "pipeline invocation" is defined per-source:
//
//   - PPE worker: one run per DualWritePPEViolation call (one camera
//     snapshot).  Multiple violation detections from the same snapshot
//     share a single run.
//   - ONVIF / sense events: one run per InsertEvent call.
//   - Alarm AI PPE violations: one run per UpdateAlarmAI call.
//   - VLM worker: one run per VLM verdict write.
//
// Callers pass an *AnalysisRunHandle that is created once before the loop
// and lazily materialised on first use.  The handle is not safe for
// concurrent use from multiple goroutines.
package dualwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"ironsight/internal/database"
	appmetrics "ironsight/internal/metrics"
)

// ── Prometheus counter ────────────────────────────────────────────────────────

// DualWriteFailuresTotal counts detections dual-write failures by source.
// Registered into the global metrics.Registry at init time so the scrape
// endpoint surfaces it without any extra wiring.
var DualWriteFailuresTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ironsight_detections_dual_write_failures_total",
		Help: "Count of detections dual-write failures by legacy source.",
	},
	[]string{"source"},
)

func init() {
	appmetrics.Registry.MustRegister(DualWriteFailuresTotal)
}

// ── Model-version in-process cache ───────────────────────────────────────────

type mvCacheKey struct {
	orgID     string
	modelName string
	versionTag string
}

var (
	mvCacheMu sync.RWMutex
	mvCache   = map[mvCacheKey]uuid.UUID{}
)

// lookupOrCreateModelVersion returns the UUID for (orgID, modelName, versionTag),
// creating it via InsertModelVersion if not yet present.  The result is cached
// for the process lifetime.
func lookupOrCreateModelVersion(
	ctx context.Context,
	db *database.DB,
	orgID, modelName, versionTag, weightsHash, domain string,
) (uuid.UUID, error) {
	key := mvCacheKey{orgID, modelName, versionTag}

	mvCacheMu.RLock()
	if id, ok := mvCache[key]; ok {
		mvCacheMu.RUnlock()
		return id, nil
	}
	mvCacheMu.RUnlock()

	// Not cached — check DB first (ON CONFLICT DO NOTHING via the unique
	// index on (org_id, model_name, version_tag)); then fetch back if it
	// already existed.
	mv, err := db.InsertModelVersion(ctx, database.ModelVersionInsert{
		OrganizationID: orgID,
		ModelName:      modelName,
		VersionTag:     versionTag,
		WeightsHash:    weightsHash,
		ModelDomain:    domain,
	})
	if err != nil {
		// The unique index fires when the version already exists.  Fall back
		// to a SELECT.
		var id uuid.UUID
		qErr := db.Pool.QueryRow(ctx,
			`SELECT id FROM model_versions
			 WHERE organization_id=$1 AND model_name=$2 AND version_tag=$3`,
			orgID, modelName, versionTag,
		).Scan(&id)
		if qErr != nil {
			return uuid.Nil, fmt.Errorf("lookupOrCreateModelVersion fallback: %w (original: %v)", qErr, err)
		}
		mv = &database.ModelVersion{ID: id}
	}

	mvCacheMu.Lock()
	mvCache[key] = mv.ID
	mvCacheMu.Unlock()
	return mv.ID, nil
}

// ── Analysis-run handle ───────────────────────────────────────────────────────

// AnalysisRunHandle lazily materialises one analysis_run row on first use.
// Create one per pipeline invocation (one PPE snapshot, one alarm event, etc.).
// Not goroutine-safe.
type AnalysisRunHandle struct {
	orgID         string
	modelVersionID uuid.UUID
	id            uuid.UUID // zero until materialised
	db            *database.DB
}

// NewRunHandle returns a handle that will create one analysis_run on first
// use.  orgID and modelVersionID must be non-empty/non-nil.
func NewRunHandle(db *database.DB, orgID string, mvID uuid.UUID) *AnalysisRunHandle {
	return &AnalysisRunHandle{db: db, orgID: orgID, modelVersionID: mvID}
}

// ID returns the analysis_run UUID, creating the DB row if needed.
func (h *AnalysisRunHandle) ID(ctx context.Context) (uuid.UUID, error) {
	if h.id != uuid.Nil {
		return h.id, nil
	}
	ar, err := h.db.InsertAnalysisRun(ctx, database.AnalysisRunInsert{
		OrganizationID: h.orgID,
		ModelVersionID: h.modelVersionID,
		RunType:        "live_ingest",
		StartedAt:      time.Now().UTC(),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("InsertAnalysisRun: %w", err)
	}
	h.id = ar.ID
	return h.id, nil
}

// ── Detection-class mapping ───────────────────────────────────────────────────

// MappedDetection is the normalized payload passed to InsertDetection.
// Callers populate only the fields specific to their source; DualWrite
// fills the common provenance fields.
type MappedDetection struct {
	// Temporal / spatial
	DetectedAt time.Time
	SiteID     *string

	// Classification
	DetectionClass  string  // canonical class string (see mapping tables below)
	DetectionDomain string  // "ppe" | "security" | "vlm_validation"
	Confidence      float32

	// Spatial — one bbox per row (DECISION-C)
	BoundingBox json.RawMessage // {"x1":…,"y1":…,"x2":…,"y2":…} or {} for VLM

	// Zone linkage — mutually exclusive; DECISION-B
	ZoneID    *uuid.UUID // PPE zone → set for PPE source
	VCARuleID *uuid.UUID // VCA rule → set for VCA/ONVIF security source

	// Evidence anchor
	SegmentID     *int64
	FrameOffsetMs *int64

	// Catch-all for source-specific extras
	Details json.RawMessage
}

// normaliseEventTypeToSecurityClass maps ONVIF/sense event_type strings to
// canonical detections detection_class values for the "security" domain.
//
// Mapping rationale:
//   - ONVIF topology fire many related topic types for the same physical event
//     (linecross + object + human); the canonical class collapses them.
//   - "object" is a superset; map to "person" only when the detection does
//     not carry a more specific type.  Here we default to "object_detected"
//     so the mapping is lossless.
//   - Unknown / vendor-specific strings fall through as-is; the detection_class
//     column has no CHECK constraint.
//
// Source: event_type values observed in cmd/server/main.go alarm generation.
func NormaliseEventTypeToSecurityClass(eventType string) string {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "intrusion":
		return "intrusion"
	case "linecross":
		return "linecross"
	case "regionentrance":
		return "regionentrance"
	case "loitering":
		return "loitering"
	case "human", "person_detected":
		return "person"
	case "vehicle", "car", "vehicle_detected":
		return "vehicle"
	case "face":
		return "face"
	case "lpr":
		return "lpr"
	case "object":
		return "object_detected"
	case "motion":
		return "motion"
	case "no_go_intrusion":
		return "no_go_intrusion"
	default:
		if eventType == "" {
			return "motion"
		}
		return eventType
	}
}

// normalisePPEClassToDomain maps YOLO PPE violation class names to the
// canonical detection_class values used in the "ppe" domain.
//
// All known YOLO PPE-model class variants are collapsed to a dash-separated
// canonical form ("no-hardhat", "no-vest", etc.).  Unknown classes are
// forwarded as-is.
func normalisePPEClass(cls string) string {
	// Normalise: lowercase, replace underscores with dashes.
	c := strings.ToLower(strings.ReplaceAll(cls, "_", "-"))
	switch c {
	case "nohat", "no-hat", "no-hardhat", "no-helmet":
		return "no-hardhat"
	case "novest", "no-vest", "no-safety-vest":
		return "no-vest"
	case "no-mask":
		return "no-mask"
	case "no-glove", "no-gloves":
		return "no-gloves"
	case "no-goggles":
		return "no-goggles"
	case "no-shoes":
		return "no-shoes"
	default:
		return c
	}
}

// NormalisePPEClass is the exported form used by callers outside this package.
func NormalisePPEClass(cls string) string { return normalisePPEClass(cls) }

// ── Core dual-write function ──────────────────────────────────────────────────

// Write inserts one detection row using the provided AnalysisRunHandle and
// model-version information.  On failure it logs, increments the failure
// counter, and returns — it does NOT propagate the error to the caller so
// the legacy write is never rolled back.
//
// source is a short label for the Prometheus counter label (e.g. "ppe",
// "events", "alarms", "vlm").
func Write(
	ctx context.Context,
	db *database.DB,
	source string,
	orgID string,
	camID uuid.UUID,
	run *AnalysisRunHandle,
	d MappedDetection,
) {
	runID, err := run.ID(ctx)
	if err != nil {
		log.Printf("[DUALWRITE:%s] InsertAnalysisRun: %v", source, err)
		DualWriteFailuresTotal.WithLabelValues(source).Inc()
		return
	}

	bbox := d.BoundingBox
	if len(bbox) == 0 {
		bbox = json.RawMessage(`{}`)
	}
	details := d.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}

	_, err = db.InsertDetection(ctx, database.DetectionInsert{
		OrganizationID:  orgID,
		SiteID:          d.SiteID,
		CameraID:        camID,
		DetectedAt:      d.DetectedAt,
		DetectionClass:  d.DetectionClass,
		DetectionDomain: d.DetectionDomain,
		Confidence:      d.Confidence,
		BoundingBox:     bbox,
		ZoneID:          d.ZoneID,
		VCARuleID:       d.VCARuleID,
		ModelVersionID:  run.modelVersionID,
		AnalysisRunID:   runID,
		SegmentID:       d.SegmentID,
		FrameOffsetMs:   d.FrameOffsetMs,
		Source:          "live",
		Supersedes:      nil, // always nil on first write; reanalysis sets this (P4-SCHEMA-06)
		Details:         details,
	})
	if err != nil {
		log.Printf("[DUALWRITE:%s] InsertDetection org=%s cam=%s class=%s: %v",
			source, orgID, camID, d.DetectionClass, err)
		DualWriteFailuresTotal.WithLabelValues(source).Inc()
	}
}

// LookupOrCreateModelVersion is the exported entry point for callers that
// need the model version UUID before constructing the AnalysisRunHandle.
func LookupOrCreateModelVersion(
	ctx context.Context,
	db *database.DB,
	orgID, modelName, versionTag, weightsHash, domain string,
) (uuid.UUID, error) {
	return lookupOrCreateModelVersion(ctx, db, orgID, modelName, versionTag, weightsHash, domain)
}

// BBoxFromX1Y1X2Y2 marshals a bounding box from x1/y1/x2/y2 normalised floats
// into the JSON representation used by the detections table.
func BBoxFromX1Y1X2Y2(x1, y1, x2, y2 float64) json.RawMessage {
	b, _ := json.Marshal(map[string]float64{
		"x1": x1, "y1": y1, "x2": x2, "y2": y2,
	})
	return b
}
