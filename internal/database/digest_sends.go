package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// DigestSend is one recorded weekly-digest send for one org + scope +
// ISO week. The row is written after a successful dispatcher call;
// queried at the start of each tick to prevent double-send.
type DigestSend struct {
	ID          int64
	OrgID       string
	Scope       string
	PeriodStart time.Time
	SentAt      time.Time
}

// GetLastDigestSend returns the most recent DigestSend row for the given
// (orgID, scope, periodStart) triple. Returns nil, nil when no row exists
// (i.e., the digest has not yet been sent for that week).
//
// periodStart MUST be the Monday 00:00:00 UTC of the ISO week being checked.
func (db *DB) GetLastDigestSend(ctx context.Context, orgID, scope string, periodStart time.Time) (*DigestSend, error) {
	var d DigestSend
	err := db.Pool.QueryRow(ctx, `
		SELECT id, org_id, scope, period_start, sent_at
		FROM digest_sends
		WHERE org_id = $1 AND scope = $2 AND period_start = $3
		LIMIT 1`,
		orgID, scope, periodStart,
	).Scan(&d.ID, &d.OrgID, &d.Scope, &d.PeriodStart, &d.SentAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// UpsertDigestSend records a successful weekly digest send. Uses INSERT
// ... ON CONFLICT DO NOTHING so a second call for the same
// (org, scope, period_start) is a no-op — safe against racing workers or
// a restart during the send window.
//
// Returns true if the row was newly inserted (first send), false if it
// was already present (duplicate / idempotent call).
func (db *DB) UpsertDigestSend(ctx context.Context, orgID, scope string, periodStart time.Time) (inserted bool, err error) {
	tag, err := db.Pool.Exec(ctx, `
		INSERT INTO digest_sends (org_id, scope, period_start)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, scope, period_start) DO NOTHING`,
		orgID, scope, periodStart,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
