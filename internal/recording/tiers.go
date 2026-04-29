package recording

// Recording retention tiers offered to customers as upgrade options.
// The tier list is the source of truth — site-config UIs render this
// as a dropdown and the platform-side validator (see ValidateRetentionTier)
// rejects any value outside it. Adding a new tier here surfaces it
// everywhere; we don't want freeform integers in customer-visible
// config because they create per-site sprawl in the SLA / billing
// matrix and make storage capacity planning unpredictable.
//
// 3 days is the included default. 7/14/30/60/90 are paid upgrades.
var RetentionTiers = []int{3, 7, 14, 30, 60, 90}

// DefaultRetentionDays is the included tier — every site that hasn't
// been explicitly upgraded inherits this.
const DefaultRetentionDays = 3

// ValidateRetentionTier returns true if `days` matches one of the
// supported tiers. Used by API handlers to reject arbitrary values
// from request bodies.
func ValidateRetentionTier(days int) bool {
	for _, t := range RetentionTiers {
		if t == days {
			return true
		}
	}
	return false
}

// CapacitySafetyThreshold is the disk-utilization fraction at which the
// retention manager force-prunes the oldest segments regardless of
// configured retention windows. 0.85 leaves the system 15% headroom
// for in-flight HLS chunks, snapshot capture, evidence exports, and
// a Postgres WAL spike — anything tighter risks the recording engine
// failing mid-write because the filesystem went read-only.
const CapacitySafetyThreshold = 0.85

// CapacitySafetyTargetAfterPrune is the fraction we prune DOWN TO once
// the safety valve fires. We don't stop right at 0.85 because a single
// hour of new recordings would push us back over and we'd thrash; 0.80
// gives ~5% breathing room before the next cycle.
const CapacitySafetyTargetAfterPrune = 0.80
