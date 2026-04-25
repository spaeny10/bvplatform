package api

import (
	"archive/zip"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"onvif-tool/internal/avs"
	"onvif-tool/internal/config"
	"onvif-tool/internal/database"
	"onvif-tool/internal/evidence"
	"onvif-tool/internal/recording"
)

// EvidenceManifest is serialized as event.json inside the downloaded ZIP.
// It contains everything a police report or insurance claim would typically
// reference: the event, the camera / site it came from, the AI verdict, and
// the operator's disposition if recorded.
type EvidenceManifest struct {
	ExportedAt   time.Time              `json:"exported_at"`
	ExportedBy   string                 `json:"exported_by"`
	EventID      int64                  `json:"event_id"`
	CameraID     string                 `json:"camera_id"`
	CameraName   string                 `json:"camera_name"`
	SiteID       string                 `json:"site_id,omitempty"`
	EventType    string                 `json:"event_type"`
	EventTime    time.Time              `json:"event_time"`
	ClipOffsetS  float64                `json:"clip_offset_seconds"`
	ClipDuration float64                `json:"clip_duration_seconds"`
	Details      map[string]interface{} `json:"details"`

	// Optional AI enrichment — present when this event was promoted to an
	// alarm and the Qwen pipeline produced a description.
	AI *EvidenceAISection `json:"ai,omitempty"`

	// AVS section is populated when this event has a dispositioned
	// security_events row. Lets a recipient (PSAP, insurer, court) see
	// the same TMA-AVS-01 score the central station consumed when it
	// decided whether to dispatch.
	AVS *EvidenceAVSSection `json:"avs,omitempty"`

	// Signature block populated by the SignedZipWriter at export time.
	// Empty when the deployment hasn't configured EVIDENCE_SIGNING_KEY —
	// in that case SIGNATURE.txt is omitted from the bundle and the
	// audit trail records the export as "unsigned."
	Signature *evidence.SignatureBlock `json:"signature,omitempty"`
}

// EvidenceAVSSection summarizes the TMA-AVS-01 score and the operator
// attestations that produced it. Carried inside event.json so the
// score travels with the bundle for any downstream consumer; covered
// by SIGNATURE.txt's HMAC like the rest of the manifest.
type EvidenceAVSSection struct {
	Score          int         `json:"score"`
	Label          string      `json:"label"`
	RubricVersion  string      `json:"rubric_version"`
	Factors        avs.Factors `json:"factors"`
	DispatchEligible bool      `json:"dispatch_eligible"`
}

type EvidenceAISection struct {
	Description       string  `json:"description,omitempty"`
	ThreatLevel       string  `json:"threat_level,omitempty"`
	RecommendedAction string  `json:"recommended_action,omitempty"`
	FalsePositivePct  float64 `json:"false_positive_pct,omitempty"`
	OperatorAgreed    *bool   `json:"operator_agreed,omitempty"`
	WasCorrect        *bool   `json:"was_correct,omitempty"`
}

// HandleEvidenceExport bundles an event into a downloadable .zip containing
// the trimmed clip, a snapshot JPEG (if available), a machine-readable
// event.json, and a human-readable README.txt. RBAC-scoped: caller must have
// access to the event's camera.
//
// URL: GET /api/events/{id}/export
// Query params:
//   - pre  (optional, seconds before event_time, default 5, max 30)
//   - post (optional, seconds after event_time, default 10, max 60)
func HandleEvidenceExport(db *database.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ── Auth + parse ──
		claims := claimsFromRequest(r)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		eventID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid event id", http.StatusBadRequest)
			return
		}

		preSec := parseFloatParam(r, "pre", 5, 30)
		postSec := parseFloatParam(r, "post", 10, 60)

		// ── Load the event + joined context in one query ──
		var (
			cameraID    string
			cameraName  string
			siteID      *string
			eventType   string
			eventTime   time.Time
			detailsJSON []byte
			segID       *int64
			segPath     *string
			segStart    *time.Time
		)
		err = db.Pool.QueryRow(r.Context(), `
			SELECT e.camera_id::text, c.name, c.site_id, e.event_type, e.event_time,
			       e.details, e.segment_id, s.file_path, s.start_time
			FROM events e
			JOIN cameras c ON c.id = e.camera_id
			LEFT JOIN segments s ON s.id = e.segment_id
			WHERE e.id = $1`, eventID).
			Scan(&cameraID, &cameraName, &siteID, &eventType, &eventTime,
				&detailsJSON, &segID, &segPath, &segStart)
		if err != nil {
			http.Error(w, "event not found", http.StatusNotFound)
			return
		}

		// ── RBAC: restrict to caller's authorized cameras ──
		camUUID, _ := uuid.Parse(cameraID)
		if ok, cErr := CanAccessCamera(r.Context(), db, claims, camUUID); cErr != nil {
			http.Error(w, cErr.Error(), http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// ── Build the manifest ──
		details := map[string]interface{}{}
		_ = json.Unmarshal(detailsJSON, &details)

		offset := 0.0
		if segStart != nil {
			offset = eventTime.Sub(*segStart).Seconds()
		}
		manifest := EvidenceManifest{
			ExportedAt:   time.Now().UTC(),
			ExportedBy:   claims.Username,
			EventID:      eventID,
			CameraID:     cameraID,
			CameraName:   cameraName,
			EventType:    eventType,
			EventTime:    eventTime,
			ClipOffsetS:  offset,
			ClipDuration: preSec + postSec,
			Details:      details,
		}
		if siteID != nil {
			manifest.SiteID = *siteID
		}

		// Enrich with AI if an alarm row exists for this event (match by
		// camera + event time, since events and active_alarms aren't FK-linked).
		if ai := loadAIForEvent(r, db, cameraID, eventTime); ai != nil {
			manifest.AI = ai
		}

		// Enrich with TMA-AVS-01 score if this event was dispositioned
		// into a security_events row. Same camera/time-window heuristic
		// as the AI enrichment — security_events doesn't have a direct
		// FK to events for the same hypertable reason.
		if avsSection := loadAVSForEvent(r, db, cameraID, eventTime); avsSection != nil {
			manifest.AVS = avsSection
		}

		// ── Stream the ZIP back to the client ──
		zipName := fmt.Sprintf("evidence-event-%d.zip", eventID)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+zipName+`"`)
		w.Header().Set("Cache-Control", "no-store")

		zw := zip.NewWriter(w)
		defer zw.Close()

		// SignedZipWriter wraps zw and tracks per-file SHA-256 hashes.
		// All evidence binaries (clip, snapshot) go through AddBinary so
		// they end up in the manifest's content_hashes; README.txt goes
		// through AddTextNoSign because it's regenerable cosmetic text;
		// event.json + SIGNATURE.txt are emitted by Sign() at the end.
		sw := evidence.NewSignedZipWriter(zw)

		// 1) clip.mp4 — trimmed video around the event. If we have a segment,
		//    use our ffmpeg helper to cut the exact window; otherwise skip
		//    and note the omission in the README.
		clipIncluded := false
		if segPath != nil && segStart != nil {
			clipStart := offset - preSec
			if clipStart < 0 {
				clipStart = 0
			}
			// Stage to temp so we can stream it into the zip.
			tmp := filepath.Join(os.TempDir(), fmt.Sprintf("evidence_clip_%d_%d.mp4", eventID, time.Now().UnixNano()))
			if _, eerr := recording.ExtractClipFromSegment(cfg.FFmpegPath, *segPath, tmp, clipStart, preSec+postSec); eerr == nil {
				if data, rerr := os.ReadFile(tmp); rerr == nil && len(data) > 1000 {
					if werr := sw.AddBinary("clip.mp4", data); werr == nil {
						clipIncluded = true
					}
				}
				_ = os.Remove(tmp)
			}
		}

		// 2) snapshot.jpg — try the alarm snapshot directory first, then
		//    fall back to the base64 thumbnail stored on the event row.
		snapshotIncluded := false
		if cfg.StoragePath != "" {
			snapDir := filepath.Join(filepath.Dir(cfg.StoragePath), "snapshots", cameraID)
			if entries, dErr := os.ReadDir(snapDir); dErr == nil {
				// Pick the snapshot closest to event time by filename timestamp.
				bestPath := ""
				var bestDelta time.Duration = 10 * time.Minute
				for _, e := range entries {
					name := e.Name()
					if !strings.HasSuffix(strings.ToLower(name), ".jpg") || strings.Contains(name, ".clip.") {
						continue
					}
					info, iErr := e.Info()
					if iErr != nil {
						continue
					}
					diff := info.ModTime().Sub(eventTime)
					if diff < 0 {
						diff = -diff
					}
					if diff < bestDelta {
						bestDelta = diff
						bestPath = filepath.Join(snapDir, name)
					}
				}
				if bestPath != "" {
					if data, rerr := os.ReadFile(bestPath); rerr == nil {
						if werr := sw.AddBinary("snapshot.jpg", data); werr == nil {
							snapshotIncluded = true
						}
					}
				}
			}
		}
		// Thumbnail fallback: events.thumbnail is a base64-encoded JPEG
		// captured at event time by the thumbnail worker.
		if !snapshotIncluded {
			var thumb64 string
			_ = db.Pool.QueryRow(r.Context(),
				`SELECT COALESCE(thumbnail,'') FROM events WHERE id = $1`, eventID).Scan(&thumb64)
			if thumb64 != "" {
				// Strip data URL prefix if present.
				if i := strings.Index(thumb64, ","); i > 0 && strings.HasPrefix(thumb64, "data:") {
					thumb64 = thumb64[i+1:]
				}
				if raw, bErr := base64.StdEncoding.DecodeString(thumb64); bErr == nil && len(raw) > 0 {
					if werr := sw.AddBinary("snapshot.jpg", raw); werr == nil {
						snapshotIncluded = true
					}
				}
			}
		}

		// 3) README.txt — human-readable summary, intentionally NOT signed
		//    because it's regenerable from the manifest. We render it to
		//    a string and pass through AddTextNoSign.
		var readme strings.Builder
		writeEvidenceReadme(&readme, manifest, clipIncluded, snapshotIncluded, cfg.ProductName)
		_ = sw.AddTextNoSign("README.txt", readme.String())

		// 4) Sign + emit event.json + SIGNATURE.txt. The Sign call writes
		//    the manifest and (when a key is configured) a HMAC-SHA256
		//    signature alongside it. The closure lets us stash the
		//    SignatureBlock back into the manifest before serialization
		//    so verifiers see content_hashes inside event.json itself.
		var signingKey []byte
		if cfg.EvidenceSigningKey != "" {
			if k, err := hex.DecodeString(cfg.EvidenceSigningKey); err == nil && len(k) >= 16 {
				signingKey = k
			} else {
				log.Printf("[EVIDENCE] EVIDENCE_SIGNING_KEY invalid (need >=16 bytes hex); export will be unsigned")
			}
		}
		if err := sw.Sign(&manifest, func(b *evidence.SignatureBlock) { manifest.Signature = b }, signingKey); err != nil {
			log.Printf("[EVIDENCE] sign failed for event %d: %v", eventID, err)
		}

		// Audit trail: evidence export is a high-value action; log it.
		var segIDVal int64
		if segID != nil {
			segIDVal = *segID
		}
		auditPlayback(db, claims, r, "GET /api/events/{id}/export", camUUID, segIDVal, eventID)
	}
}

// writeEvidenceReadme generates the plain-text summary dropped into the ZIP
// so a non-technical recipient (adjuster, officer) can read the key facts
// without opening event.json.
//
// `productName` comes from cfg.ProductName (env var PRODUCT_NAME, default
// "Ironsight"). Rebranding the backend is a one-env-var change — the
// string flows through to the header + footer of every exported bundle.
// writeEvidenceReadme accepts anything that can absorb bytes (zip
// file writer, strings.Builder, bytes.Buffer). Switched from a custom
// inline interface to io.Writer so the SignedZipWriter path can pass
// a Builder for the textual content without per-write copies.
func writeEvidenceReadme(w io.Writer, m EvidenceManifest, clip, snap bool, productName string) {
	if productName == "" {
		productName = "Ironsight"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s EVIDENCE EXPORT\n", strings.ToUpper(productName))
	b.WriteString(strings.Repeat("=", 40) + "\n\n")
	fmt.Fprintf(&b, "Exported at : %s UTC\n", m.ExportedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Exported by : %s\n\n", m.ExportedBy)

	b.WriteString("EVENT\n")
	b.WriteString(strings.Repeat("-", 40) + "\n")
	fmt.Fprintf(&b, "Event ID   : %d\n", m.EventID)
	fmt.Fprintf(&b, "Event Type : %s\n", m.EventType)
	fmt.Fprintf(&b, "Event Time : %s\n", m.EventTime.Format(time.RFC3339))
	if m.SiteID != "" {
		fmt.Fprintf(&b, "Site       : %s\n", m.SiteID)
	}
	fmt.Fprintf(&b, "Camera     : %s (%s)\n\n", m.CameraName, m.CameraID)

	if m.AVS != nil {
		b.WriteString("TMA-AVS-01 ALARM VALIDATION SCORE\n")
		b.WriteString(strings.Repeat("-", 40) + "\n")
		fmt.Fprintf(&b, "Score          : %d (%s)\n", m.AVS.Score, m.AVS.Label)
		fmt.Fprintf(&b, "Rubric version : %s\n", m.AVS.RubricVersion)
		fmt.Fprintf(&b, "Dispatch ready : %v\n", m.AVS.DispatchEligible)
		b.WriteString("Factors:\n")
		writeFactorLine(&b, "video verified", m.AVS.Factors.VideoVerified)
		writeFactorLine(&b, "person detected", m.AVS.Factors.PersonDetected)
		writeFactorLine(&b, "suspicious behavior", m.AVS.Factors.SuspiciousBehavior)
		writeFactorLine(&b, "weapon observed", m.AVS.Factors.WeaponObserved)
		writeFactorLine(&b, "active crime", m.AVS.Factors.ActiveCrime)
		writeFactorLine(&b, "multi-camera evidence", m.AVS.Factors.MultiCameraEvidence)
		writeFactorLine(&b, "multi-sensor evidence", m.AVS.Factors.MultiSensorEvidence)
		writeFactorLine(&b, "audio verified", m.AVS.Factors.AudioVerified)
		writeFactorLine(&b, "talk-down ignored", m.AVS.Factors.TalkdownIgnored)
		writeFactorLine(&b, "auth failure", m.AVS.Factors.AuthFailure)
		writeFactorLine(&b, "AI corroborated", m.AVS.Factors.AICorroborated)
		b.WriteString("\n")
	}

	if m.AI != nil {
		b.WriteString("AI ASSESSMENT\n")
		b.WriteString(strings.Repeat("-", 40) + "\n")
		if m.AI.Description != "" {
			fmt.Fprintf(&b, "Description      : %s\n", m.AI.Description)
		}
		if m.AI.ThreatLevel != "" {
			fmt.Fprintf(&b, "Threat Level     : %s\n", m.AI.ThreatLevel)
		}
		if m.AI.RecommendedAction != "" {
			fmt.Fprintf(&b, "Recommended      : %s\n", m.AI.RecommendedAction)
		}
		if m.AI.FalsePositivePct > 0 {
			fmt.Fprintf(&b, "False-positive % : %.0f%%\n", m.AI.FalsePositivePct*100)
		}
		if m.AI.OperatorAgreed != nil {
			fmt.Fprintf(&b, "Operator agreed  : %v\n", *m.AI.OperatorAgreed)
		}
		b.WriteString("\n")
	}

	b.WriteString("PACKAGE CONTENTS\n")
	b.WriteString(strings.Repeat("-", 40) + "\n")
	if clip {
		fmt.Fprintf(&b, "clip.mp4       - Trimmed video (%.0fs total, event at %.1fs)\n",
			m.ClipDuration, m.ClipDuration/2)
	} else {
		b.WriteString("clip.mp4       - NOT AVAILABLE (no recording covered this moment)\n")
	}
	if snap {
		b.WriteString("snapshot.jpg   - Still frame captured near event time\n")
	} else {
		b.WriteString("snapshot.jpg   - NOT AVAILABLE\n")
	}
	b.WriteString("event.json     - Machine-readable event + AI metadata\n")
	b.WriteString("README.txt     - This file\n\n")

	fmt.Fprintf(&b, "Generated by %s Platform. This package is a faithful\n", productName)
	b.WriteString("copy of recorded data at the time of export.\n")

	_, _ = w.Write([]byte(b.String()))
}

// loadAIForEvent looks up AI enrichment tied to this event via the
// active_alarms table. We match by camera + timestamp because events and
// alarms aren't directly FK-linked today — a ±30s window is safe since
// alarms are debounced to one-per-event by allowAlarm.
func loadAIForEvent(r *http.Request, db *database.DB, cameraID string, eventTime time.Time) *EvidenceAISection {
	var (
		desc, threat, action string
		fpPct                float64
		opAgreed, wasCorrect *bool
	)
	err := db.Pool.QueryRow(r.Context(), `
		SELECT COALESCE(ai_description,''), COALESCE(ai_threat_level,''),
		       COALESCE(ai_recommended_action,''), COALESCE(ai_false_positive_pct, 0),
		       ai_operator_agreed, ai_was_correct
		FROM active_alarms
		WHERE camera_id = $1
		  AND ABS(EXTRACT(EPOCH FROM (to_timestamp(ts/1000) - $2))) < 30
		ORDER BY ts DESC
		LIMIT 1`,
		cameraID, eventTime).
		Scan(&desc, &threat, &action, &fpPct, &opAgreed, &wasCorrect)
	if err != nil {
		return nil
	}
	// If none of the AI fields are populated there's nothing meaningful to
	// report; skip the section rather than emit empty keys.
	if desc == "" && threat == "" && action == "" && opAgreed == nil && wasCorrect == nil {
		return nil
	}
	return &EvidenceAISection{
		Description:       desc,
		ThreatLevel:       threat,
		RecommendedAction: action,
		FalsePositivePct:  fpPct,
		OperatorAgreed:    opAgreed,
		WasCorrect:        wasCorrect,
	}
}

// writeFactorLine renders a single yes/no AVS factor as one line of
// the README. Boolean rendering uses ✓/· glyphs that survive plain-
// text recipients (versus emoji or true/false strings) and aligns to
// a fixed column so a reader scans down the column to spot truths.
func writeFactorLine(w io.Writer, label string, present bool) {
	mark := "·"
	if present {
		mark = "Y"
	}
	fmt.Fprintf(w, "  [%s] %s\n", mark, label)
}

// loadAVSForEvent fetches the TMA-AVS-01 score for the most recent
// security_events row that matches this event's camera and a tight
// time window around the event_time. Returns nil if the event hasn't
// been dispositioned yet (which is normal — exports can happen during
// investigation, before final disposition).
//
// The 30-second window matches loadAIForEvent's heuristic for the same
// reason: events and security_events aren't directly FK-linked, but
// the disposition flow guarantees they share camera + a near-identical
// timestamp.
func loadAVSForEvent(r *http.Request, db *database.DB, cameraID string, eventTime time.Time) *EvidenceAVSSection {
	var (
		factorsJSON   []byte
		score         int
		rubricVersion string
	)
	err := db.Pool.QueryRow(r.Context(), `
		SELECT COALESCE(avs_factors, '{}'::jsonb), avs_score, COALESCE(avs_rubric_version,'')
		FROM security_events
		WHERE camera_id = $1
		  AND ABS(EXTRACT(EPOCH FROM (to_timestamp(ts/1000) - $2))) < 30
		ORDER BY ts DESC
		LIMIT 1`,
		cameraID, eventTime).
		Scan(&factorsJSON, &score, &rubricVersion)
	if err != nil {
		return nil
	}
	if rubricVersion == "" && score == 0 && len(factorsJSON) <= 2 {
		// Empty disposition row (predates AVS rollout, or factors not
		// captured). Don't include the section — better to show "no
		// score" than to show a misleading 0 when the operator never
		// answered the questionnaire.
		return nil
	}
	var factors avs.Factors
	_ = json.Unmarshal(factorsJSON, &factors)
	s := avs.Score(score)
	return &EvidenceAVSSection{
		Score:            int(s),
		Label:            avs.ScoreLabel(s),
		RubricVersion:    rubricVersion,
		Factors:          factors,
		DispatchEligible: avs.DispatchEligible(s),
	}
}

// ── small parse helpers ──

func parseFloatParam(r *http.Request, key string, def, max float64) float64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return def
	}
	if f > max {
		return max
	}
	return f
}

