package ticket

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/cache"
)

type MembershipFinder interface {
	FindActiveMembership(context.Context, int64, int64) (organization.Membership, error)
}
type Store interface {
	Begin(context.Context) (Transaction, error)
	GetTicket(context.Context, int64, int64) (Ticket, error)
	ListTickets(context.Context, int64, Filter) ([]Ticket, error)
	ListComments(context.Context, int64, int64, Page) ([]Comment, error)
	ListEvents(context.Context, int64, int64, Page) ([]Event, error)
}
type Transaction interface {
	MembershipFinder
	GetTicket(context.Context, int64, int64) (Ticket, error)
	CreateTicket(context.Context, Ticket) (Ticket, error)
	UpdateTicket(context.Context, Ticket, int64, Status) (Ticket, error)
	ClaimTicket(context.Context, int64, int64, int64, int64) (Ticket, error)
	InsertComment(context.Context, Comment) (Comment, error)
	InsertEvent(context.Context, Event) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
type Service struct {
	cache   *cache.Cache
	store   Store
	members MembershipFinder
}

func NewService(s Store, m MembershipFinder) *Service { return &Service{store: s, members: m} }
func member(ctx context.Context, f MembershipFinder, actor, org int64) (organization.Membership, error) {
	if err := ctx.Err(); err != nil {
		return organization.Membership{}, err
	}
	if actor <= 0 {
		return organization.Membership{}, ErrUnauthenticated
	}
	if org <= 0 {
		return organization.Membership{}, ErrInvalidRequest
	}
	m, err := f.FindActiveMembership(ctx, org, actor)
	if errors.Is(err, organization.ErrNotFound) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if m.OrgID != org || m.UserID != actor {
		return m, ErrStorage
	}
	if m.Status != organization.StatusActive {
		return m, ErrNotFound
	}
	if m.Role != organization.RoleAdmin && m.Role != organization.RoleMember {
		return m, ErrStorage
	}
	return m, nil
}

// WithCache optionally enables authorized list caching during service setup.
func (s *Service) WithCache(c *cache.Cache) *Service { s.cache = c; return s }
func (s *Service) Authorize(ctx context.Context, actor, org int64) error {
	_, err := member(ctx, s.members, actor, org)
	return err
}
func (s *Service) write(ctx context.Context, actor, org int64, work func(context.Context, Transaction, organization.Membership) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	m, err := member(ctx, tx, actor, org)
	if err != nil {
		return err
	}
	if err := work(ctx, tx, m); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func snapshot(t Ticket) map[string]any {
	return map[string]any{"title": t.Title, "priority": t.Priority, "assignee_id": t.AssigneeID, "status": t.Status, "version": t.Version}
}
func audit(ctx context.Context, tx Transaction, t Ticket, actor int64, kind string, before, after map[string]any) error {
	b, err := json.Marshal(before)
	if err != nil {
		return ErrStorage
	}
	a, err := json.Marshal(after)
	if err != nil {
		return ErrStorage
	}
	return tx.InsertEvent(ctx, Event{OrgID: t.OrgID, TicketID: t.ID, ActorID: actor, Type: kind, TicketVersion: t.Version, Before: b, After: a})
}
func (s *Service) Create(ctx context.Context, actor, org int64, in CreateInput) (Ticket, error) {
	var result Ticket
	err := s.write(ctx, actor, org, func(ctx context.Context, tx Transaction, _ organization.Membership) error {
		var err error
		result, err = s.createInTransaction(ctx, tx, actor, org, in, "")
		return err
	})
	if err != nil {
		return Ticket{}, err
	}
	return result, nil
}

// CreateInTransaction reuses ticket validation and audit without owning commit or rollback.
// The caller must keep authorization, the ticket, audit and its own state in this tx.
func (s *Service) CreateInTransaction(ctx context.Context, tx pgx.Tx, actor, org int64, in CreateInput, proposalID string) (Ticket, error) {
	if tx == nil {
		return Ticket{}, ErrStorage
	}
	wrapped := &postgresTx{tx: tx}
	if _, err := member(ctx, wrapped, actor, org); err != nil {
		return Ticket{}, err
	}
	return s.createInTransaction(ctx, wrapped, actor, org, in, proposalID)
}
func (s *Service) createInTransaction(ctx context.Context, tx Transaction, actor, org int64, in CreateInput, proposalID string) (Ticket, error) {
	title, err := cleanText(in.Title, 200)
	if err != nil {
		return Ticket{}, err
	}
	body, err := cleanText(in.Body, 10000)
	if err != nil {
		return Ticket{}, err
	}
	if !validPriority(in.Priority) {
		return Ticket{}, ErrInvalidRequest
	}
	result, err := tx.CreateTicket(ctx, Ticket{OrgID: org, CreatorID: actor, Title: title, Body: body, Priority: in.Priority, Status: Open})
	if err != nil {
		return Ticket{}, err
	}
	after := snapshot(result)
	if proposalID != "" {
		after["ai_proposal_id"] = proposalID
	}
	if err := audit(ctx, tx, result, actor, "TICKET_CREATED", map[string]any{}, after); err != nil {
		return Ticket{}, err
	}
	return result, nil
}
func (s *Service) Get(ctx context.Context, actor, org, id int64) (Ticket, error) {
	if err := s.Authorize(ctx, actor, org); err != nil {
		return Ticket{}, err
	}
	if id <= 0 {
		return Ticket{}, ErrInvalidRequest
	}
	return s.store.GetTicket(ctx, org, id)
}
func (s *Service) List(ctx context.Context, actor, org int64, f Filter) ([]Ticket, error) {
	if err := s.Authorize(ctx, actor, org); err != nil {
		return nil, err
	}
	if _, err := f.Page.offset(); err != nil {
		return nil, err
	}
	if f.Status != "" && !validStatus(f.Status) || f.Priority != nil && !validPriority(*f.Priority) {
		return nil, ErrInvalidRequest
	}
	items, err := cache.List(ctx, s.cache, org, "tickets", f, func() ([]Ticket, error) {
		return s.store.ListTickets(ctx, org, f)
	}, func(item Ticket) bool { return item.OrgID == org && item.ID > 0 && item.DeletedAt == nil })
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Ticket{}
	}
	return items, nil
}
func checkVersion(t Ticket, expected int64) error {
	if expected < 0 {
		return ErrInvalidRequest
	}
	if expected != t.Version || t.Version == math.MaxInt64 {
		return ErrConflict
	}
	return nil
}

// Claim relies on a single conditional UPDATE, rather than a read followed by
// an unconditional assignment. Authorization and the audit share its transaction.
func (s *Service) Claim(ctx context.Context, actor, org, id, expected int64) (Ticket, error) {
	var result Ticket
	err := s.write(ctx, actor, org, func(ctx context.Context, tx Transaction, _ organization.Membership) error {
		if id <= 0 || expected < 0 {
			return ErrInvalidRequest
		}
		var err error
		result, err = tx.ClaimTicket(ctx, org, id, actor, expected)
		if errors.Is(err, ErrConflict) {
			// Only disclose a conflict when the object is visible in this org.
			if _, lookupErr := tx.GetTicket(ctx, org, id); lookupErr != nil {
				return lookupErr
			}
			return ErrClaimConflict
		}
		if err != nil {
			return err
		}
		return audit(ctx, tx, result, actor, "TICKET_ASSIGNED",
			map[string]any{"assignee_id": nil, "version": expected},
			map[string]any{"assignee_id": result.AssigneeID, "version": result.Version})
	})
	if err != nil {
		return Ticket{}, err
	}
	return result, nil
}

func (s *Service) Patch(ctx context.Context, actor, org, id int64, in PatchInput) (Ticket, error) {
	var result Ticket
	err := s.write(ctx, actor, org, func(ctx context.Context, tx Transaction, m organization.Membership) error {
		old, err := tx.GetTicket(ctx, org, id)
		if err != nil {
			return err
		}
		if err := CanEdit(m, old, in.AssigneeSet); err != nil {
			return err
		}
		if err := checkVersion(old, in.ExpectedVersion); err != nil {
			return err
		}
		next := old
		if in.Title != nil {
			next.Title, err = cleanText(*in.Title, 200)
			if err != nil {
				return err
			}
		}
		if in.Body != nil {
			next.Body, err = cleanText(*in.Body, 10000)
			if err != nil {
				return err
			}
		}
		if in.Priority != nil {
			if !validPriority(*in.Priority) {
				return ErrInvalidRequest
			}
			next.Priority = *in.Priority
		}
		if in.AssigneeSet {
			if in.AssigneeID != nil {
				if *in.AssigneeID <= 0 {
					return ErrInvalidRequest
				}
				if _, err := member(ctx, tx, *in.AssigneeID, org); err != nil {
					if errors.Is(err, ErrNotFound) {
						return ErrInvalidAssignee
					}
					return err
				}
			}
			next.AssigneeID = in.AssigneeID
		}
		basic := next.Title != old.Title || next.Body != old.Body || next.Priority != old.Priority
		assigned := !sameID(next.AssigneeID, old.AssigneeID)
		if !basic && !assigned {
			return ErrInvalidRequest
		}
		result, err = tx.UpdateTicket(ctx, next, old.Version, old.Status)
		if err != nil {
			return err
		}
		if basic {
			b, a := snapshot(old), snapshot(result)
			if next.Body != old.Body {
				b["body_changed"] = false
				a["body_changed"] = true
			}
			if err := audit(ctx, tx, result, actor, "TICKET_UPDATED", b, a); err != nil {
				return err
			}
		}
		if assigned {
			return audit(ctx, tx, result, actor, "TICKET_ASSIGNED", map[string]any{"assignee_id": old.AssigneeID, "version": old.Version}, map[string]any{"assignee_id": result.AssigneeID, "version": result.Version})
		}
		return nil
	})
	if err != nil {
		return Ticket{}, err
	}
	return result, nil
}
func (s *Service) Transition(ctx context.Context, actor, org, id int64, in TransitionInput) (Ticket, error) {
	var result Ticket
	err := s.write(ctx, actor, org, func(ctx context.Context, tx Transaction, m organization.Membership) error {
		old, err := tx.GetTicket(ctx, org, id)
		if err != nil {
			return err
		}
		if err := checkVersion(old, in.ExpectedVersion); err != nil {
			return err
		}
		active := false
		if old.AssigneeID != nil && !(old.Status == Resolved && in.To == Closed) {
			_, err := member(ctx, tx, *old.AssigneeID, org)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			active = err == nil
		}
		if err := ValidateTransition(old.Status, in.To, m.Role, old.AssigneeID != nil && *old.AssigneeID == actor, active); err != nil {
			return err
		}
		next := old
		next.Status = in.To
		result, err = tx.UpdateTicket(ctx, next, old.Version, old.Status)
		if err != nil {
			return err
		}
		return audit(ctx, tx, result, actor, "TICKET_STATUS_CHANGED", map[string]any{"status": old.Status, "version": old.Version}, map[string]any{"status": result.Status, "version": result.Version})
	})
	if err != nil {
		return Ticket{}, err
	}
	return result, nil
}
func (s *Service) Delete(ctx context.Context, actor, org, id int64) error {
	return s.write(ctx, actor, org, func(ctx context.Context, tx Transaction, m organization.Membership) error {
		old, err := tx.GetTicket(ctx, org, id)
		if err != nil {
			return err
		}
		if m.Role != organization.RoleAdmin {
			return ErrForbidden
		}
		if err := checkVersion(old, old.Version); err != nil {
			return err
		}
		next := old
		now := time.Now().UTC()
		next.DeletedAt = &now
		result, err := tx.UpdateTicket(ctx, next, old.Version, old.Status)
		if err != nil {
			return err
		}
		return audit(ctx, tx, result, actor, "TICKET_DELETED", snapshot(old), map[string]any{"deleted": true, "version": result.Version})
	})
}
func (s *Service) Comment(ctx context.Context, actor, org, id int64, content string) (Comment, error) {
	var result Comment
	err := s.write(ctx, actor, org, func(ctx context.Context, tx Transaction, _ organization.Membership) error {
		parent, err := tx.GetTicket(ctx, org, id)
		if err != nil {
			return err
		}
		if parent.Status == Closed {
			return ErrConflict
		}
		content, err = cleanText(content, 5000)
		if err != nil {
			return err
		}
		result, err = tx.InsertComment(ctx, Comment{OrgID: org, TicketID: id, AuthorID: actor, Content: content})
		if err != nil {
			return err
		}
		return audit(ctx, tx, parent, actor, "TICKET_COMMENTED", map[string]any{}, map[string]any{"comment_id": result.ID})
	})
	if err != nil {
		return Comment{}, err
	}
	return result, nil
}
func (s *Service) Comments(ctx context.Context, actor, org, id int64, p Page) ([]Comment, error) {
	if _, err := s.Get(ctx, actor, org, id); err != nil {
		return nil, err
	}
	if _, err := p.offset(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListComments(ctx, org, id, p)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Comment{}
	}
	return rows, nil
}
func (s *Service) Events(ctx context.Context, actor, org, id int64, p Page) ([]Event, error) {
	if _, err := s.Get(ctx, actor, org, id); err != nil {
		return nil, err
	}
	if _, err := p.offset(); err != nil {
		return nil, err
	}
	rows, err := s.store.ListEvents(ctx, org, id, p)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Event{}
	}
	return rows, nil
}
