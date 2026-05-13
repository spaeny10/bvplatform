package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ironsight/internal/auth"
	"ironsight/internal/database"
	"ironsight/internal/notify"
)

// Customer ↔ SOC support ticket handlers.
//
// Five endpoints:
//   POST /api/v1/support/tickets             customer creates a thread
//   GET  /api/v1/support/tickets             list (org-scoped or SOC-wide)
//   GET  /api/v1/support/tickets/{id}        full thread + messages
//   POST /api/v1/support/tickets/{id}/messages add a reply
//   PATCH /api/v1/support/tickets/{id}       status transition (close/reopen)
//
// Scope rules:
//   - Customer / site_manager: see only their organization's
//     tickets. Creating a ticket auto-fills organization_id from
//     their JWT — they cannot specify a different org.
//   - soc_supervisor / admin: see all tickets across all orgs;
//     can reply to any.
//   - soc_operator / viewer: no access (route guard rejects
//     anyway, but the handlers also enforce).

type createTicketRequest struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	SiteID  string `json:"site_id,omitempty"`
}

func HandleCreateSupportTicket(db *database.DB, dispatcher *notify.Dispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.OrganizationID == "" && classifyForSupport(claims.Role) == "customer" {
			// A customer-side user without an org has nowhere to send
			// the ticket. SOC roles bypass this — they don't open
			// tickets on the customer side anyway.
			http.Error(w, "your account is not associated with an organization", http.StatusForbidden)
			return
		}

		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}

		var req createTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if len(req.Subject) > 200 {
			req.Subject = req.Subject[:200]
		}
		if len(req.Body) > 8000 {
			req.Body = req.Body[:8000]
		}

		// Org for the ticket is taken from the JWT for customer-side
		// users; SOC roles can specify any org via a hidden internal
		// field, but for now we enforce JWT-only.
		orgID := claims.OrganizationID

		ticket, err := db.CreateSupportTicket(r.Context(), orgID, req.SiteID, req.Subject, req.Body, userID, claims.Role)
		if err != nil {
			http.Error(w, "create failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Email all SOC supervisors so a new ticket doesn't sit
		// unread until someone happens to look at /reports.
		go notifyNewTicket(db, dispatcher, ticket, req.Body, claims.Username)

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, ticket)
	}
}

func HandleListSupportTickets(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		statusFilter := r.URL.Query().Get("status")

		var (
			tickets []database.SupportTicket
			err     error
		)
		switch claims.Role {
		case "admin", "soc_supervisor":
			tickets, err = db.ListAllSupportTickets(r.Context(), statusFilter)
		case "soc_operator", "viewer":
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		default:
			if claims.OrganizationID == "" {
				writeJSON(w, []interface{}{})
				return
			}
			tickets, err = db.ListSupportTicketsForOrg(r.Context(), claims.OrganizationID, statusFilter)
		}
		if err != nil {
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}
		if tickets == nil {
			tickets = []database.SupportTicket{}
		}
		writeJSON(w, tickets)
	}
}

func HandleGetSupportTicket(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		ticket, msgs, err := db.GetSupportTicketWithMessages(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !canAccessTicket(claims, ticket) {
			// 404 not 403 — don't leak existence cross-tenant.
			http.NotFound(w, r)
			return
		}
		if msgs == nil {
			msgs = []database.SupportMessage{}
		}
		writeJSON(w, map[string]interface{}{
			"ticket":   ticket,
			"messages": msgs,
		})
	}
}

type replyRequest struct {
	Body string `json:"body"`
}

func HandleSupportTicketReply(db *database.DB, dispatcher *notify.Dispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		ticket, _, err := db.GetSupportTicketWithMessages(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !canAccessTicket(claims, ticket) {
			http.NotFound(w, r)
			return
		}
		userID, err := uuid.Parse(claims.UserID)
		if err != nil {
			http.Error(w, "invalid user id in token", http.StatusBadRequest)
			return
		}

		var req replyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
			http.Error(w, "body required", http.StatusBadRequest)
			return
		}
		if len(req.Body) > 8000 {
			req.Body = req.Body[:8000]
		}

		msg, err := db.AddSupportMessage(r.Context(), id, userID, claims.Role, req.Body)
		if err != nil {
			http.Error(w, "reply failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		go notifyTicketReply(db, dispatcher, ticket, msg, claims.Username, claims.Role)

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, msg)
	}
}

type updateTicketRequest struct {
	Status string `json:"status"`
}

func HandleUpdateSupportTicket(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := r.Context().Value(ContextKeyClaims).(*auth.Claims)
		if claims == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		ticket, _, err := db.GetSupportTicketWithMessages(r.Context(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !canAccessTicket(claims, ticket) {
			http.NotFound(w, r)
			return
		}
		var req updateTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := db.SetSupportTicketStatus(r.Context(), id, req.Status); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// canAccessTicket gates read/write on the cross-tenant boundary. SOC
// roles see everything; customer-side users see only their org's
// tickets. Returns false for unknown roles so a misconfigured user
// account fails closed.
func canAccessTicket(claims *auth.Claims, t *database.SupportTicket) bool {
	switch claims.Role {
	case "admin", "soc_supervisor":
		return true
	case "customer", "site_manager":
		return claims.OrganizationID != "" && t.OrganizationID == claims.OrganizationID
	default:
		return false
	}
}

func classifyForSupport(role string) string {
	switch role {
	case "admin", "soc_supervisor":
		return "soc"
	default:
		return "customer"
	}
}

// ── notification fan-out ─────────────────────────────────────────

func notifyNewTicket(db *database.DB, dispatcher *notify.Dispatcher, t *database.SupportTicket, body, fromUsername string) {
	if dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	emails, err := db.ListSupervisorEmails(ctx)
	if err != nil || len(emails) == 0 {
		if err != nil {
			log.Printf("[SUPPORT] supervisor lookup failed: %v", err)
		}
		return
	}
	recipients := make([]notify.Recipient, 0, len(emails))
	for _, e := range emails {
		recipients = append(recipients, notify.Recipient{Email: e})
	}
	dispatcher.SupportTicketEvent(ctx, notify.SupportTicketEvent{
		Kind:        "new",
		TicketID:    t.ID,
		Subject:     t.Subject,
		Body:        body,
		FromName:    fromUsername,
		Org:         t.OrganizationID,
		PortalPath:  fmt.Sprintf("/reports?support=%d", t.ID),
	}, recipients)
}

func notifyTicketReply(db *database.DB, dispatcher *notify.Dispatcher, t *database.SupportTicket, m *database.SupportMessage, fromUsername, fromRole string) {
	if dispatcher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	side := classifyForSupport(fromRole)
	var recipients []notify.Recipient
	var portalPath string

	if side == "soc" {
		// SOC replied → email the customer who opened the ticket.
		email, err := db.GetUserEmailByID(ctx, t.CreatedBy)
		if err != nil || email == "" {
			return
		}
		recipients = []notify.Recipient{{Email: email}}
		portalPath = fmt.Sprintf("/portal?support=%d", t.ID)
	} else {
		// Customer replied → email all supervisors.
		emails, err := db.ListSupervisorEmails(ctx)
		if err != nil || len(emails) == 0 {
			return
		}
		recipients = make([]notify.Recipient, 0, len(emails))
		for _, e := range emails {
			recipients = append(recipients, notify.Recipient{Email: e})
		}
		portalPath = fmt.Sprintf("/reports?support=%d", t.ID)
	}

	dispatcher.SupportTicketEvent(ctx, notify.SupportTicketEvent{
		Kind:       "reply",
		TicketID:   t.ID,
		Subject:    t.Subject,
		Body:       m.Body,
		FromName:   fromUsername,
		Org:        t.OrganizationID,
		PortalPath: portalPath,
	}, recipients)
}
