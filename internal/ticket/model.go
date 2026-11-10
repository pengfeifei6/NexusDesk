package ticket

import (
	"encoding/json"
	"time"
)

type Status string

const (
	Open       Status = "OPEN"
	InProgress Status = "IN_PROGRESS"
	Resolved   Status = "RESOLVED"
	Closed     Status = "CLOSED"
)

type Ticket struct {
	ID, OrgID, CreatorID int64
	AssigneeID           *int64
	Title, Body          string
	Priority             int16
	Status               Status
	Version              int64
	CreatedAt, UpdatedAt time.Time
	DeletedAt            *time.Time
}
type Comment struct {
	ID, OrgID, TicketID, AuthorID int64
	Content                       string
	CreatedAt                     time.Time
}
type Event struct {
	ID, OrgID, TicketID, ActorID int64
	Type                         string
	TicketVersion                int64
	Before, After                json.RawMessage
	CreatedAt                    time.Time
}
type Page struct{ Number, Size int64 }
type Filter struct {
	Page
	Status   Status
	Priority *int16
}
type CreateInput struct {
	Title, Body string
	Priority    int16
}
type PatchInput struct {
	ExpectedVersion int64
	Title, Body     *string
	Priority        *int16
	AssigneeSet     bool
	AssigneeID      *int64
}
type TransitionInput struct {
	ExpectedVersion int64
	To              Status
}
