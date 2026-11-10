package ticket

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/organization"
)

type testStore struct {
	row              Ticket
	events           []Event
	comments         []Comment
	fail             string
	role             organization.Role
	disabledAssignee bool
	rolled           bool
	cancelOnEvent    context.CancelFunc
}

func freshStore() *testStore {
	return &testStore{row: Ticket{ID: 11, OrgID: 7, CreatorID: 42, Title: "Before", Body: "Body", Priority: 2, Status: Open}, role: organization.RoleAdmin}
}
func (s *testStore) FindActiveMembership(_ context.Context, org, user int64) (organization.Membership, error) {
	if org != 7 || (user != 42 && user != 99) || (user == 99 && s.disabledAssignee) {
		return organization.Membership{}, organization.ErrNotFound
	}
	return organization.Membership{OrgID: org, UserID: user, Role: s.role, Status: organization.StatusActive}, nil
}
func (s *testStore) Begin(context.Context) (Transaction, error) {
	if s.fail == "begin" {
		return nil, ErrStorage
	}
	return &testTx{s: s, row: s.row}, nil
}
func (s *testStore) GetTicket(_ context.Context, org, id int64) (Ticket, error) {
	if org != 7 || id != s.row.ID || s.row.DeletedAt != nil {
		return Ticket{}, ErrNotFound
	}
	return s.row, nil
}
func (s *testStore) ListTickets(context.Context, int64, Filter) ([]Ticket, error) {
	return []Ticket{s.row}, nil
}
func (s *testStore) ListComments(context.Context, int64, int64, Page) ([]Comment, error) {
	return s.comments, nil
}
func (s *testStore) ListEvents(context.Context, int64, int64, Page) ([]Event, error) {
	return s.events, nil
}

type testTx struct {
	s        *testStore
	row      Ticket
	events   []Event
	comments []Comment
}

func (tx *testTx) FindActiveMembership(ctx context.Context, org, user int64) (organization.Membership, error) {
	return tx.s.FindActiveMembership(ctx, org, user)
}
func (tx *testTx) GetTicket(ctx context.Context, org, id int64) (Ticket, error) {
	if tx.s.fail == "get" {
		return Ticket{}, ErrStorage
	}
	return tx.s.GetTicket(ctx, org, id)
}
func (tx *testTx) CreateTicket(_ context.Context, row Ticket) (Ticket, error) {
	if tx.s.fail == "write" {
		return Ticket{}, ErrStorage
	}
	row.ID = 12
	tx.row = row
	return row, nil
}
func (tx *testTx) UpdateTicket(_ context.Context, row Ticket, expected int64, from Status) (Ticket, error) {
	if tx.s.fail == "write" {
		return Ticket{}, ErrStorage
	}
	if expected != tx.row.Version || from != tx.row.Status {
		return Ticket{}, ErrConflict
	}
	row.Version = expected + 1
	tx.row = row
	return row, nil
}
func (tx *testTx) InsertComment(_ context.Context, c Comment) (Comment, error) {
	if tx.s.fail == "write" {
		return Comment{}, ErrStorage
	}
	c.ID = 5
	tx.comments = append(tx.comments, c)
	return c, nil
}
func (tx *testTx) InsertEvent(_ context.Context, e Event) error {
	if tx.s.fail == "event" {
		return ErrStorage
	}
	tx.events = append(tx.events, e)
	if tx.s.cancelOnEvent != nil {
		tx.s.cancelOnEvent()
	}
	return nil
}
func (tx *testTx) Commit(ctx context.Context) error {
	if tx.s.fail == "commit" {
		return ErrStorage
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx.s.row = tx.row
	tx.s.events = append(tx.s.events, tx.events...)
	tx.s.comments = append(tx.s.comments, tx.comments...)
	return nil
}
func (tx *testTx) Rollback(ctx context.Context) error {
	if ctx.Err() != nil {
		panic("rollback reused canceled context")
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 3*time.Second {
		panic("rollback has no bounded deadline")
	}
	tx.s.rolled = true
	return nil
}

func TestServiceTransactionsAndVersions(t *testing.T) {
	ctx := context.Background()
	store := freshStore()
	service := NewService(store, store)
	made, err := service.Create(ctx, 42, 7, CreateInput{Title: "  New  ", Body: "Body", Priority: 2})
	if err != nil || made.CreatorID != 42 || made.OrgID != 7 || made.Version != 0 || made.Status != Open || made.Title != "New" || len(store.events) != 1 || store.events[0].Type != "TICKET_CREATED" {
		t.Fatal("create identity/default/event contract")
	}
	store = freshStore()
	service = NewService(store, store)
	title := "After"
	assignee := int64(99)
	patched, err := service.Patch(ctx, 42, 7, 11, PatchInput{ExpectedVersion: 0, Title: &title, AssigneeSet: true, AssigneeID: &assignee})
	if err != nil || patched.Version != 1 || patched.AssigneeID == nil || *patched.AssigneeID != 99 || len(store.events) != 2 || store.events[0].Type != "TICKET_UPDATED" || store.events[1].Type != "TICKET_ASSIGNED" {
		t.Fatal("combined patch versions/events")
	}
	if _, err := service.Patch(ctx, 42, 7, 11, PatchInput{ExpectedVersion: 0, Title: &title}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale version overwritten")
	}
	if _, err := service.Comment(ctx, 42, 7, 11, "comment body only"); err != nil || store.row.Version != 1 || len(store.comments) != 1 || strings.Contains(string(store.events[2].After), "comment body only") {
		t.Fatal("comment changed version or duplicated content in event")
	}
	moved, err := service.Transition(ctx, 42, 7, 11, TransitionInput{ExpectedVersion: 1, To: InProgress})
	if err != nil || moved.Version != 2 || moved.Status != InProgress {
		t.Fatal("transition failed")
	}
	if err := service.Delete(ctx, 42, 7, 11); err != nil || store.row.DeletedAt == nil || store.row.Version != 3 || store.events[len(store.events)-1].Type != "TICKET_DELETED" {
		t.Fatal("delete not audited")
	}
}

func TestServiceFailureNeverCommitsPartialWrite(t *testing.T) {
	for _, action := range []string{"create", "patch", "transition", "comment", "delete"} {
		for _, stage := range []string{"begin", "event", "commit"} {
			t.Run(action+"/"+stage, func(t *testing.T) {
				s := freshStore()
				s.fail = stage
				id := int64(99)
				s.row.AssigneeID = &id
				service := NewService(s, s)
				ctx := context.Background()
				var err error
				switch action {
				case "create":
					_, err = service.Create(ctx, 42, 7, CreateInput{Title: "T", Body: "B", Priority: 2})
				case "patch":
					title := "changed"
					_, err = service.Patch(ctx, 42, 7, 11, PatchInput{ExpectedVersion: 0, Title: &title})
				case "transition":
					_, err = service.Transition(ctx, 42, 7, 11, TransitionInput{ExpectedVersion: 0, To: InProgress})
				case "comment":
					_, err = service.Comment(ctx, 42, 7, 11, "comment")
				case "delete":
					err = service.Delete(ctx, 42, 7, 11)
				}
				if !errors.Is(err, ErrStorage) || s.row.Version != 0 || s.row.DeletedAt != nil || len(s.events) != 0 || len(s.comments) != 0 {
					t.Fatal("failed atomic operation persisted partial state")
				}
				if stage != "begin" && !s.rolled {
					t.Fatal("failure did not release transaction")
				}
			})
		}
	}
	s := freshStore()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelOnEvent = cancel
	_, err := NewService(s, s).Create(ctx, 42, 7, CreateInput{Title: "T", Body: "B", Priority: 2})
	if !errors.Is(err, context.Canceled) || !s.rolled || len(s.events) != 0 {
		t.Fatal("canceled operation committed or cleanup failed")
	}
}

func TestServiceValidationAndObjectPermissions(t *testing.T) {
	ctx := context.Background()
	s := freshStore()
	service := NewService(s, s)
	for _, in := range []CreateInput{{Title: " ", Body: "B", Priority: 2}, {Title: "T", Body: "\x00", Priority: 2}, {Title: "T", Body: "B", Priority: 0}, {Title: strings.Repeat("中", 201), Body: "B", Priority: 1}, {Title: "T", Body: strings.Repeat("a", 10001), Priority: 2}} {
		if _, err := service.Create(ctx, 42, 7, in); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("invalid ticket accepted")
		}
	}
	if _, err := service.Get(ctx, 42, 8, 11); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross org visible")
	}
	s.disabledAssignee = true
	id := int64(99)
	if _, err := service.Patch(ctx, 42, 7, 11, PatchInput{AssigneeSet: true, AssigneeID: &id}); !errors.Is(err, ErrInvalidAssignee) {
		t.Fatal("disabled assignee accepted")
	}
	s.role = organization.RoleMember
	s.row.CreatorID = 99
	title := "T"
	if _, err := service.Patch(ctx, 42, 7, 11, PatchInput{Title: &title}); !errors.Is(err, ErrForbidden) {
		t.Fatal("member changed another creator")
	}
	if err := service.Delete(ctx, 42, 7, 11); !errors.Is(err, ErrForbidden) {
		t.Fatal("member deleted")
	}
	s.row.Status = Closed
	if _, err := service.Comment(ctx, 42, 7, 11, "text"); !errors.Is(err, ErrConflict) {
		t.Fatal("closed comment accepted")
	}
	if _, err := service.List(ctx, 42, 7, Filter{Page: Page{1, 101}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("bad page")
	}
	if _, err := service.Patch(ctx, 42, 7, 11, PatchInput{}); err == nil {
		t.Fatal("empty patch accepted")
	}
}
