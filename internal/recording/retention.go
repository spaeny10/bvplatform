package recording

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"ironsight/internal/database"
)

// RetentionManager periodically cleans up old recording segments based on:
//  1. Disk space caps (max_gb per storage location) — highest priority
//  2. Per-camera retention days
//  3. Storage location default retention days (fallback when camera's is 0)
//
// Scope: this manager touches recording-storage tables and a small
// set of explicitly-allowlisted operational tables.
//
//	cameras, segments, exports, thumbnails, hls, recordings on disk.
//	support_tickets + support_messages (closed-and-stale only).
//
// It MUST NOT touch:
//
//	audit_log, playback_audits, deterrence_audits, security_events,
//	incidents, evidence_share_opens, revoked_tokens.
//
// Audit and evidence tables are governed by UL 827B / TMA-AVS-01
// retention policy (see database.MinAuditRetentionDays — currently
// 365 days minimum, never automatically purged). The append-only
// triggers on the audit tables provide a runtime backstop in case a
// future code change accidentally adds a DELETE here, but the
// expectation is that this file's surface stays narrow on its own.
//
// US-only deployment scope. Per-customer data-deletion requests
// (rare in B2B and usually covered by end-of-contract terms) are a
// manual operation performed inside a documented signed-maintenance
// window — never an automated retention purge. See
// `Documents/USCompliance.md` for the full posture.
type RetentionManager struct {
	db     *database.DB
	stopCh chan struct{}
}

// NewRetentionManager creates a new retention manager
func NewRetentionManager(db *database.DB) *RetentionManager {
	return &RetentionManager{
		db:     db,
		stopCh: make(chan struct{}),
	}
}

// Start begins the retention cleanup loop (runs every hour)
func (rm *RetentionManager) Start(ctx context.Context) {
	log.Println("[RETENTION] Starting retention manager (cleanup interval: 1 hour)")

	// Run immediately on startup
	rm.cleanup(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			log.Println("[RETENTION] Stopped")
			return
		case <-ctx.Done():
			log.Println("[RETENTION] Context cancelled, stopping")
			return
		case <-ticker.C:
			rm.cleanup(ctx)
		}
	}
}

// Stop terminates the retention manager
func (rm *RetentionManager) Stop() {
	close(rm.stopCh)
}

// cleanup runs the full retention strategy.
//
// Pass order matters: explicit operator policy (storage caps) wins
// over age-based windows, and the capacity safety valve runs LAST so
// it only fires when the normal policies couldn't keep up — typically
// after a sudden volume spike or when an admin set retention longer
// than the disk can sustain. The valve never overrides audit/evidence
// retention; it only deletes recording segments.
func (rm *RetentionManager) cleanup(ctx context.Context) {
	log.Println("[RETENTION] Running cleanup...")

	totalDeleted := 0
	totalBytes := int64(0)

	// Pass 1: per-location max_gb caps (operator-configured hard limits).
	d, b := rm.enforceStorageCaps(ctx)
	totalDeleted += d
	totalBytes += b

	// Pass 2: per-camera / per-site retention windows.
	d, b = rm.enforceRetentionDays(ctx)
	totalDeleted += d
	totalBytes += b

	// Pass 3: capacity safety valve — fires only if disk usage exceeds
	// the threshold even after normal pruning.
	d, b = rm.enforceCapacitySafetyValve(ctx)
	totalDeleted += d
	totalBytes += b

	// Pass 4: closed support tickets older than the support retention window.
	rm.pruneSupportTickets(ctx)

	if totalDeleted > 0 {
		log.Printf("[RETENTION] Cleanup complete: %d segment(s) deleted, %.2f MB freed",
			totalDeleted, float64(totalBytes)/1024/1024)
	} else {
		log.Println("[RETENTION] Cleanup complete: no expired segments")
	}
}

// enforceCapacitySafetyValve protects the host from filling up. For
// each enabled recordings storage location it samples the actual
// filesystem utilization (statfs / GetDiskFreeSpaceEx). If usage is
// above CapacitySafetyThreshold (default 0.85), it deletes the oldest
// segments at that location until usage drops to
// CapacitySafetyTargetAfterPrune (0.80) — the gap exists so a single
// hour of new recording doesn't immediately re-trigger the valve.
//
// Why a fixed fraction instead of a per-location max_gb: customers
// share underlying disks (especially on the AI Workbench / WSL test
// rigs), so a per-volume cap doesn't reflect what the OS actually has
// available. A live filesystem reading is the only honest answer.
func (rm *RetentionManager) enforceCapacitySafetyValve(ctx context.Context) (int, int64) {
	locations, err := rm.db.ListStorageLocations(ctx)
	if err != nil {
		log.Printf("[RETENTION] Safety valve: failed to list storage locations: %v", err)
		return 0, 0
	}

	totalDeleted := 0
	totalBytes := int64(0)
	// Avoid double-pruning when several locations share a filesystem
	// (common on dev where everything is /home). Track FS by total
	// bytes — locations with the same total are almost certainly the
	// same filesystem; we only prune one.
	seenFS := make(map[uint64]bool)

	for _, loc := range locations {
		if !loc.Enabled {
			continue
		}
		if loc.Purpose != "recordings" && loc.Purpose != "all" {
			continue
		}

		total, used, ok := diskUsageForPath(loc.Path)
		if !ok || total == 0 {
			continue
		}
		if seenFS[total] {
			continue
		}
		seenFS[total] = true

		usedFraction := float64(used) / float64(total)
		if usedFraction <= CapacitySafetyThreshold {
			continue
		}

		targetUsed := uint64(float64(total) * CapacitySafetyTargetAfterPrune)
		mustFree := used - targetUsed
		log.Printf("[RETENTION] Safety valve TRIPPED on '%s': %.1f%% used (%.1f GB / %.1f GB) — pruning to %.0f%% (free %.1f GB)",
			loc.Label,
			usedFraction*100, float64(used)/1024/1024/1024, float64(total)/1024/1024/1024,
			CapacitySafetyTargetAfterPrune*100, float64(mustFree)/1024/1024/1024)

		// Normalize the path for matching segments.path in DB.
		pathPrefix := strings.ReplaceAll(loc.Path, "\\", "/")

		freedHere := int64(0)
		deletedHere := 0
		for freedHere < int64(mustFree) {
			paths, freed, err := rm.db.DeleteOldestSegmentsByPath(ctx, pathPrefix, 100)
			if err != nil {
				log.Printf("[RETENTION] Safety valve: delete failed on '%s': %v", loc.Label, err)
				break
			}
			if len(paths) == 0 {
				break
			}
			for _, p := range paths {
				if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
					log.Printf("[RETENTION] Safety valve: file rm %s: %v", p, err)
				}
			}
			freedHere += freed
			deletedHere += len(paths)
		}
		totalDeleted += deletedHere
		totalBytes += freedHere
		log.Printf("[RETENTION] Safety valve done on '%s': %d segment(s), %.2f GB freed",
			loc.Label, deletedHere, float64(freedHere)/1024/1024/1024)
	}

	return totalDeleted, totalBytes
}

// enforceStorageCaps checks each storage location's usage against its max_gb limit.
// If over the limit, it deletes the oldest segments until usage is below the cap.
func (rm *RetentionManager) enforceStorageCaps(ctx context.Context) (int, int64) {
	locations, err := rm.db.ListStorageLocations(ctx)
	if err != nil {
		log.Printf("[RETENTION] Failed to list storage locations: %v", err)
		return 0, 0
	}

	totalDeleted := 0
	totalBytes := int64(0)

	for _, loc := range locations {
		if !loc.Enabled || loc.MaxGB <= 0 {
			continue // No cap set or location disabled
		}

		maxBytes := int64(loc.MaxGB) * 1024 * 1024 * 1024

		// Normalize path for consistent matching (use forward slashes)
		pathPrefix := strings.ReplaceAll(loc.Path, "\\", "/")

		usage, err := rm.db.GetStorageUsageByPath(ctx, pathPrefix)
		if err != nil {
			log.Printf("[RETENTION] Failed to get usage for %s: %v", loc.Label, err)
			continue
		}

		if usage <= maxBytes {
			continue // Under the cap
		}

		overBy := usage - maxBytes
		log.Printf("[RETENTION] Storage '%s' over cap: %.2f GB / %d GB (%.2f GB over)",
			loc.Label, float64(usage)/1024/1024/1024, loc.MaxGB, float64(overBy)/1024/1024/1024)

		// Delete oldest segments in batches until under the cap
		for usage > maxBytes {
			paths, freed, err := rm.db.DeleteOldestSegmentsByPath(ctx, pathPrefix, 50)
			if err != nil {
				log.Printf("[RETENTION] Failed to delete oldest segments for %s: %v", loc.Label, err)
				break
			}
			if len(paths) == 0 {
				break // No more segments to delete
			}

			// Remove files from disk
			for _, path := range paths {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					log.Printf("[RETENTION] Failed to delete file %s: %v", path, err)
				}
			}

			totalDeleted += len(paths)
			totalBytes += freed
			usage -= freed

			log.Printf("[RETENTION] Storage '%s': deleted %d segment(s), freed %.2f MB",
				loc.Label, len(paths), float64(freed)/1024/1024)
		}
	}

	return totalDeleted, totalBytes
}

// enforceRetentionDays walks every camera, resolves its retention window
// from the camera's site policy (with storage-location default as a
// fallback), and deletes segments older than the cutoff.
//
// Retention resolution order (first non-zero wins):
//
//   1. Site policy: sites.retention_days for the camera's assigned site.
//   2. Storage location default: first enabled recordings-purpose location.
//   3. 0 → skip this camera (no policy; keep everything).
//
// Sites are cached for the duration of one pass so we don't query the DB
// N times for the common case of many cameras sharing a site.
func (rm *RetentionManager) enforceRetentionDays(ctx context.Context) (int, int64) {
	cameras, err := rm.db.ListCameras(ctx)
	if err != nil {
		log.Printf("[RETENTION] Failed to list cameras: %v", err)
		return 0, 0
	}

	// Load storage locations to use as fallback defaults
	locations, err := rm.db.ListStorageLocations(ctx)
	if err != nil {
		log.Printf("[RETENTION] Failed to list storage locations for fallback: %v", err)
		locations = nil
	}

	// Find the best fallback retention from storage locations (lowest priority = preferred)
	fallbackRetention := 0
	for _, loc := range locations {
		if loc.Enabled && loc.RetentionDays > 0 {
			if loc.Purpose == "recordings" || loc.Purpose == "all" {
				fallbackRetention = loc.RetentionDays
				break // Locations are already sorted by priority
			}
		}
	}

	// Per-pass site cache: avoids N round-trips for the common case where
	// several cameras share a site.
	siteRetention := make(map[string]int)

	totalDeleted := 0
	totalBytes := int64(0)

	for _, camera := range cameras {
		retentionDays := 0
		if camera.SiteID != "" {
			if cached, ok := siteRetention[camera.SiteID]; ok {
				retentionDays = cached
			} else {
				site, err := rm.db.GetSite(ctx, camera.SiteID)
				if err == nil && site != nil {
					retentionDays = site.RetentionDays
				}
				siteRetention[camera.SiteID] = retentionDays
			}
		}

		// Fallback: storage-location default if the site has no policy or
		// the camera isn't assigned to a site yet.
		if retentionDays <= 0 && fallbackRetention > 0 {
			retentionDays = fallbackRetention
		}

		if retentionDays <= 0 {
			continue // No retention policy at any level
		}

		cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

		paths, err := rm.db.DeleteOldSegments(ctx, camera.ID, cutoff)
		if err != nil {
			log.Printf("[RETENTION] Failed to delete segments for camera %s: %v", camera.Name, err)
			continue
		}

		for _, path := range paths {
			info, err := os.Stat(path)
			if err == nil {
				totalBytes += info.Size()
			}
			if err := os.Remove(path); err != nil {
				log.Printf("[RETENTION] Failed to delete file %s: %v", path, err)
			}
		}

		if len(paths) > 0 {
			totalDeleted += len(paths)
			log.Printf("[RETENTION] Camera %s (%d-day retention): deleted %d expired segment(s)",
				camera.Name, retentionDays, len(paths))
		}
	}

	return totalDeleted, totalBytes
}

// supportTicketRetentionDays — closed tickets older than this are
// purged. 180 days gives operators ample time to reference the
// thread for recurrence analysis without letting customer-content
// PII (names, gate codes, contact details that may appear in free
// text) sit indefinitely. Open and answered tickets are never
// purged regardless of age.
const supportTicketRetentionDays = 180

// pruneSupportTickets removes closed support tickets (and their
// messages, via ON DELETE CASCADE) older than the retention window.
// We only purge tickets in status='closed' — open and answered
// tickets are active state and stay regardless of age.
func (rm *RetentionManager) pruneSupportTickets(ctx context.Context) {
	cutoff := time.Now().Add(-time.Duration(supportTicketRetentionDays) * 24 * time.Hour)
	deleted, err := rm.db.PruneClosedSupportTickets(ctx, cutoff)
	if err != nil {
		log.Printf("[RETENTION] Failed to prune closed support tickets: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("[RETENTION] Pruned %d closed support ticket(s) older than %d days",
			deleted, supportTicketRetentionDays)
	}
}

