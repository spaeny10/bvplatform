package recording

import (
	"context"
	"log"

	"ironsight/internal/database"
)

// SettingsForCamera resolves the recording policy a camera should run under.
//
// The policy lives on the camera's site (migrated out of per-camera columns
// in 2026-04). Falling back in order:
//
//   1. If the camera is assigned to a site and the site row loads: use the
//      site's retention/mode/buffers/triggers/schedule.
//   2. If the camera has no site, or the site lookup fails: return zero-value
//      RecordingSettings, which StartRecording treats as "engine defaults"
//      (continuous mode, 10/30 buffers, motion+object triggers, no schedule).
//
// The caller should treat this as best-effort — a missing site is not an
// error, just an unusual configuration (e.g., a camera in onboarding that
// hasn't been assigned yet). We log rather than fail so recording still
// starts with sane defaults.
func SettingsForCamera(ctx context.Context, db *database.DB, cam *database.Camera) RecordingSettings {
	if cam == nil || cam.SiteID == "" {
		return RecordingSettings{}
	}
	site, err := db.GetSite(ctx, cam.SiteID)
	if err != nil || site == nil {
		log.Printf("[REC] Camera %s: site %q lookup failed (%v) — using engine defaults", cam.Name, cam.SiteID, err)
		return RecordingSettings{}
	}
	return RecordingSettings{
		RecordingMode:     site.RecordingMode,
		PreBufferSec:      site.PreBufferSec,
		PostBufferSec:     site.PostBufferSec,
		RecordingTriggers: site.RecordingTriggers,
		Schedule:          site.RecordingSchedule,
	}
}

// RetentionDaysForCamera resolves the retention window for segment purge.
// Like SettingsForCamera, the value comes from the camera's site. Returns
// 0 when no site policy applies — the retention manager treats 0 as "fall
// through to the storage-location-level default".
func RetentionDaysForCamera(ctx context.Context, db *database.DB, cam *database.Camera) int {
	if cam == nil || cam.SiteID == "" {
		return 0
	}
	site, err := db.GetSite(ctx, cam.SiteID)
	if err != nil || site == nil {
		return 0
	}
	return site.RetentionDays
}
