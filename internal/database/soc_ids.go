package database

import (
	"context"
	"fmt"
	"time"
)

// MinAuditRetentionDays is the policy floor for how long audit-trail
// rows must remain readable in the database. UL 827B reviewers
// consistently expect 12 months minimum; some state regulators
// (notably California and New York monitoring statutes) push this to
// 24 months. We default to 365 here and let an operator extend it via
// configuration if their jurisdiction demands more.
//
// The append-only triggers on audit_log / playback_audits /
// deterrence_audits enforce this implicitly — without a DELETE path,
// "minimum retention" is "forever, less manual intervention." The
// constant is exported so dashboards, status endpoints, and audit-
// package documentation can quote one canonical number rather than
// drift apart over time.
const MinAuditRetentionDays = 365

// Short, phoneticizable identifiers for SOC operations.
//
// Why bother: the UUID primary keys work fine inside the app and over
// URLs, but they're unusable on a radio or phone bridge — by the time
// an operator has read "alarm-a7b3d91e-1709876543211" to a supervisor
// they've lost the situation. These helpers hand out codes humans can
// say once: ALM-250415-0042, INC-2026-0147.
//
// Uniqueness strategy is deliberately simple: read MAX(seq) for the
// current period, increment, insert. Collision on insert (another api
// replica raced ahead) is handled one level up by the caller retrying.
// That's fine at Ironsight's scale — we'll never touch the daily 10k
// alarm ceiling, and multi-replica races are vanishingly rare.

// NextAlarmCode returns a new ALM-YYMMDD-NNNN code unique within the
// current calendar day (UTC). Zero-padded four-digit sequence — 10k
// alarms per site per day is orders of magnitude beyond anything we
// expect, and the fixed width keeps the code readable aloud.
func (db *DB) NextAlarmCode(ctx context.Context) (string, error) {
	today := time.Now().UTC()
	prefix := fmt.Sprintf("ALM-%s-", today.Format("060102"))

	var nextSeq int
	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(
			CAST(SUBSTRING(alarm_code FROM LENGTH($1) + 1) AS INT)
		), 0) + 1
		FROM active_alarms
		WHERE alarm_code LIKE $1 || '%'
	`, prefix).Scan(&nextSeq)
	if err != nil {
		return "", fmt.Errorf("alarm code: read next seq: %w", err)
	}
	return fmt.Sprintf("%s%04d", prefix, nextSeq), nil
}

// NextIncidentID returns a new INC-YYYY-NNNN identifier unique within
// the current calendar year (UTC). Four-digit sequence caps at 10k
// incidents per year per deployment — if we're ever hitting that we
// have much bigger problems than id formatting. Year rollover is
// implicit: the LIKE filter scopes to the current year, so 2027's
// sequence starts fresh at 0001.
func (db *DB) NextIncidentID(ctx context.Context) (string, error) {
	year := time.Now().UTC().Year()
	prefix := fmt.Sprintf("INC-%d-", year)

	var nextSeq int
	err := db.Pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(
			CAST(SUBSTRING(id FROM LENGTH($1) + 1) AS INT)
		), 0) + 1
		FROM incidents
		WHERE id LIKE $1 || '%'
	`, prefix).Scan(&nextSeq)
	if err != nil {
		return "", fmt.Errorf("incident id: read next seq: %w", err)
	}
	return fmt.Sprintf("%s%04d", prefix, nextSeq), nil
}

// GetEvidenceShare returns the share metadata for a token, or nil if
// the token is unknown. The handler is responsible for interpreting
// revoked/expired states — we return the row as-stored so future
// audit tools can inspect revoked shares without special APIs.
func (db *DB) GetEvidenceShare(ctx context.Context, token string) (*EvidenceShare, error) {
	var s EvidenceShare
	err := db.Pool.QueryRow(ctx, `
		SELECT token,
		       COALESCE(incident_id, ''),
		       COALESCE(created_by, ''),
		       expires_at,
		       COALESCE(revoked, false),
		       created_at
		FROM evidence_shares
		WHERE token = $1`, token).Scan(
		&s.Token, &s.IncidentID, &s.CreatedBy, &s.ExpiresAt, &s.Revoked, &s.CreatedAt,
	)
	if err != nil {
		// pgx returns ErrNoRows equivalent; caller treats any error as "not found"
		// for the public endpoint, but logs it at debug level for diagnostics.
		return nil, err
	}
	return &s, nil
}

// LogEvidenceShareOpen records one access of a public share URL. This
// is the chain-of-custody primitive a court cares about — not who
// created the share, but every IP + user-agent that actually opened
// it, timestamped by the database (append-only).
//
// Fire-and-forget: errors are logged by the caller but never propagate
// to the HTTP response. A failed audit write must not block access to
// the evidence itself — that would create a different kind of incident.
func (db *DB) LogEvidenceShareOpen(ctx context.Context, token, ip, userAgent, referrer string) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO evidence_share_opens (token, ip, user_agent, referrer)
		VALUES ($1, $2, $3, $4)`,
		token, ip, userAgent, referrer,
	)
	return err
}
