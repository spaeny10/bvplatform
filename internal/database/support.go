package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SupportTicket is one customer-to-SOC conversation thread. Subject
// + first message create it; subsequent messages append. Status
// transitions follow the simple state machine documented in the
// schema migration.
type SupportTicket struct {
	ID              int64     `json:"id"`
	OrganizationID  string    `json:"organization_id"`
	SiteID          string    `json:"site_id,omitempty"`
	CreatedBy       uuid.UUID `json:"created_by"`
	CreatedByName   string    `json:"created_by_name"`
	Subject         string    `json:"subject"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastMessageAt   time.Time `json:"last_message_at"`
	LastMessageBy   string    `json:"last_message_by"` // "customer" or "soc"
	MessageCount    int       `json:"message_count"`
}

type SupportMessage struct {
	ID         int64     `json:"id"`
	TicketID   int64     `json:"ticket_id"`
	AuthorID   uuid.UUID `json:"author_id"`
	AuthorName string    `json:"author_name"`
	AuthorRole string    `json:"author_role"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// CreateSupportTicket opens a new ticket and writes the first message
// in one transaction so a partial create can never leave a ticket
// with no messages. Returns the populated ticket with the message
// already counted.
func (db *DB) CreateSupportTicket(ctx context.Context, orgID, siteID, subject, body string, authorID uuid.UUID, authorRole string) (*SupportTicket, error) {
	if subject == "" || body == "" {
		return nil, fmt.Errorf("subject and body required")
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	t := &SupportTicket{
		OrganizationID: orgID,
		SiteID:         siteID,
		CreatedBy:      authorID,
		Subject:        subject,
		Status:         "open",
		LastMessageBy:  classifyRole(authorRole),
	}
	var siteParam interface{}
	if siteID != "" {
		siteParam = siteID
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO support_tickets
		  (organization_id, site_id, created_by, subject, status, last_message_by)
		VALUES ($1, $2, $3, $4, 'open', $5)
		RETURNING id, created_at, updated_at, last_message_at`,
		orgID, siteParam, authorID, subject, t.LastMessageBy,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.LastMessageAt)
	if err != nil {
		return nil, fmt.Errorf("insert ticket: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO support_messages (ticket_id, author_id, author_role, body)
		VALUES ($1, $2, $3, $4)`,
		t.ID, authorID, authorRole, body,
	)
	if err != nil {
		return nil, fmt.Errorf("insert first message: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	t.MessageCount = 1
	return t, nil
}

// AddSupportMessage appends a message to an existing ticket and
// updates the ticket's last_message metadata + flips status based on
// who replied. Customer reply on an answered ticket re-opens it.
// SOC reply on an open ticket marks it answered.
func (db *DB) AddSupportMessage(ctx context.Context, ticketID int64, authorID uuid.UUID, authorRole, body string) (*SupportMessage, error) {
	if body == "" {
		return nil, fmt.Errorf("body required")
	}
	side := classifyRole(authorRole)
	newStatus := "open"
	if side == "soc" {
		newStatus = "answered"
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	m := &SupportMessage{
		TicketID:   ticketID,
		AuthorID:   authorID,
		AuthorRole: authorRole,
		Body:       body,
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO support_messages (ticket_id, author_id, author_role, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		ticketID, authorID, authorRole, body,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE support_tickets
		SET last_message_at = NOW(),
		    last_message_by = $2,
		    status          = CASE
		        WHEN status = 'closed' THEN 'closed'
		        ELSE $3
		    END,
		    updated_at      = NOW()
		WHERE id = $1`,
		ticketID, side, newStatus,
	)
	if err != nil {
		return nil, fmt.Errorf("update ticket meta: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// classifyRole reduces the four user roles to the binary
// customer-side / soc-side distinction the ticket state machine
// cares about. site_manager is treated as customer-side because
// they're a customer-tier user, just one with elevated permissions
// inside their org.
func classifyRole(role string) string {
	switch role {
	case "soc_supervisor", "admin":
		return "soc"
	default:
		return "customer"
	}
}

// ListSupportTicketsForOrg returns the ticket list for one
// organization. Sorted by last_message_at desc (most-recently-
// active first) so a returning customer sees their pending replies
// at the top. MessageCount is denormalized in via subquery rather
// than join + count to keep the query plan simple.
func (db *DB) ListSupportTicketsForOrg(ctx context.Context, orgID, statusFilter string) ([]SupportTicket, error) {
	q := `
		SELECT t.id, t.organization_id, COALESCE(t.site_id, ''),
		       t.created_by, COALESCE(u.display_name, u.username),
		       t.subject, t.status, t.created_at, t.updated_at,
		       t.last_message_at, t.last_message_by,
		       (SELECT COUNT(*) FROM support_messages m WHERE m.ticket_id = t.id) AS msg_count
		FROM support_tickets t
		JOIN users u ON u.id = t.created_by
		WHERE t.organization_id = $1`
	args := []interface{}{orgID}
	if statusFilter != "" && statusFilter != "all" {
		q += " AND t.status = $2"
		args = append(args, statusFilter)
	}
	q += " ORDER BY t.last_message_at DESC"

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupportTicket
	for rows.Next() {
		var t SupportTicket
		if err := rows.Scan(&t.ID, &t.OrganizationID, &t.SiteID,
			&t.CreatedBy, &t.CreatedByName,
			&t.Subject, &t.Status, &t.CreatedAt, &t.UpdatedAt,
			&t.LastMessageAt, &t.LastMessageBy, &t.MessageCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListAllSupportTickets is the SOC-side view across all
// organizations. Used by the supervisor dashboard's Support tab.
// Same shape as the org-scoped variant; deliberately returns
// organization_id so the UI can group/filter.
func (db *DB) ListAllSupportTickets(ctx context.Context, statusFilter string) ([]SupportTicket, error) {
	q := `
		SELECT t.id, t.organization_id, COALESCE(t.site_id, ''),
		       t.created_by, COALESCE(u.display_name, u.username),
		       t.subject, t.status, t.created_at, t.updated_at,
		       t.last_message_at, t.last_message_by,
		       (SELECT COUNT(*) FROM support_messages m WHERE m.ticket_id = t.id) AS msg_count
		FROM support_tickets t
		JOIN users u ON u.id = t.created_by`
	args := []interface{}{}
	if statusFilter != "" && statusFilter != "all" {
		q += " WHERE t.status = $1"
		args = append(args, statusFilter)
	}
	q += " ORDER BY t.last_message_at DESC LIMIT 200"

	rows, err := db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SupportTicket
	for rows.Next() {
		var t SupportTicket
		if err := rows.Scan(&t.ID, &t.OrganizationID, &t.SiteID,
			&t.CreatedBy, &t.CreatedByName,
			&t.Subject, &t.Status, &t.CreatedAt, &t.UpdatedAt,
			&t.LastMessageAt, &t.LastMessageBy, &t.MessageCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetSupportTicketWithMessages loads one ticket and its full message
// thread. Two queries (ticket header, then messages); separate so the
// ticket lookup can reject early on access-denied without paying the
// message-fetch cost.
func (db *DB) GetSupportTicketWithMessages(ctx context.Context, ticketID int64) (*SupportTicket, []SupportMessage, error) {
	var t SupportTicket
	err := db.Pool.QueryRow(ctx, `
		SELECT t.id, t.organization_id, COALESCE(t.site_id, ''),
		       t.created_by, COALESCE(u.display_name, u.username),
		       t.subject, t.status, t.created_at, t.updated_at,
		       t.last_message_at, t.last_message_by
		FROM support_tickets t
		JOIN users u ON u.id = t.created_by
		WHERE t.id = $1`,
		ticketID,
	).Scan(&t.ID, &t.OrganizationID, &t.SiteID,
		&t.CreatedBy, &t.CreatedByName,
		&t.Subject, &t.Status, &t.CreatedAt, &t.UpdatedAt,
		&t.LastMessageAt, &t.LastMessageBy)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT m.id, m.ticket_id, m.author_id,
		       COALESCE(u.display_name, u.username),
		       m.author_role, m.body, m.created_at
		FROM support_messages m
		JOIN users u ON u.id = m.author_id
		WHERE m.ticket_id = $1
		ORDER BY m.created_at`,
		ticketID,
	)
	if err != nil {
		return &t, nil, err
	}
	defer rows.Close()

	var msgs []SupportMessage
	for rows.Next() {
		var m SupportMessage
		if err := rows.Scan(&m.ID, &m.TicketID, &m.AuthorID,
			&m.AuthorName, &m.AuthorRole, &m.Body, &m.CreatedAt); err != nil {
			return &t, nil, err
		}
		msgs = append(msgs, m)
	}
	t.MessageCount = len(msgs)
	return &t, msgs, nil
}

// SetSupportTicketStatus is the explicit close/reopen path. The
// state machine inside AddSupportMessage handles open↔answered
// flipping automatically; this method is only invoked when a user
// (either side) wants to close out a thread.
func (db *DB) SetSupportTicketStatus(ctx context.Context, ticketID int64, status string) error {
	switch status {
	case "open", "answered", "closed":
	default:
		return fmt.Errorf("invalid status %q", status)
	}
	_, err := db.Pool.Exec(ctx,
		`UPDATE support_tickets SET status=$2, updated_at=NOW() WHERE id=$1`,
		ticketID, status,
	)
	return err
}

// ListSupervisorEmails returns email addresses of every soc_supervisor
// or admin user with an email on file. Used by the support-ticket
// notifier so a new customer message reaches the SOC team without
// per-user opt-in.
func (db *DB) ListSupervisorEmails(ctx context.Context) ([]string, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT email FROM users
		WHERE role IN ('soc_supervisor', 'admin')
		  AND COALESCE(email,'') <> ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err == nil && e != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// GetUserEmailByID returns the email of a single user by id. Used by
// the ticket notifier to email the customer who created a ticket
// when the SOC replies.
func (db *DB) GetUserEmailByID(ctx context.Context, id uuid.UUID) (string, error) {
	var email string
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(email,'') FROM users WHERE id=$1`, id,
	).Scan(&email)
	return email, err
}

// PruneClosedSupportTickets deletes closed tickets last touched
// before the cutoff. Open and answered tickets are never purged
// regardless of age — they represent live conversations. Messages
// are removed via ON DELETE CASCADE on the FK from
// support_messages.ticket_id.
//
// Called by the retention worker (see internal/recording/retention.go,
// pass 4). Returns the number of tickets removed.
func (db *DB) PruneClosedSupportTickets(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := db.Pool.Exec(ctx, `
		DELETE FROM support_tickets
		WHERE status = 'closed'
		  AND last_message_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
