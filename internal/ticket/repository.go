package ticket

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
)

const ticketColumns = "id,org_id,creator_id,assignee_id,title,body,priority,status,version,created_at,updated_at,deleted_at"

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(p *pgxpool.Pool) *PostgresRepository { return &PostgresRepository{pool: p} }
func storageError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return ErrStorage
}

type scanner interface{ Scan(...any) error }

func scanTicket(ctx context.Context, row scanner) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.OrgID, &t.CreatorID, &t.AssigneeID, &t.Title, &t.Body, &t.Priority, &t.Status, &t.Version, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt)
	if err != nil {
		return Ticket{}, storageError(ctx, err)
	}
	return t, nil
}
func (r *PostgresRepository) GetTicket(ctx context.Context, org, id int64) (Ticket, error) {
	if org <= 0 || id <= 0 {
		return Ticket{}, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return scanTicket(ctx, r.pool.QueryRow(ctx, "SELECT "+ticketColumns+" FROM tickets WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL", org, id))
}
func (r *PostgresRepository) ListTickets(ctx context.Context, org int64, f Filter) ([]Ticket, error) {
	offset, err := f.offset()
	if err != nil || org <= 0 {
		return nil, ErrInvalidRequest
	}
	if f.Status != "" && !validStatus(f.Status) || f.Priority != nil && !validPriority(*f.Priority) {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	query := "SELECT " + ticketColumns + " FROM tickets WHERE org_id=$1 AND deleted_at IS NULL"
	args := []any{org}
	if f.Status != "" {
		args = append(args, string(f.Status))
		query += " AND status=$" + strconv.Itoa(len(args))
	}
	if f.Priority != nil {
		args = append(args, *f.Priority)
		query += " AND priority=$" + strconv.Itoa(len(args))
	}
	args = append(args, f.Size, offset)
	query += " ORDER BY created_at DESC,id DESC LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	defer rows.Close()
	items := []Ticket{}
	for rows.Next() {
		item, err := scanTicket(ctx, rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError(ctx, err)
	}
	return items, nil
}
func (r *PostgresRepository) ListComments(ctx context.Context, org, id int64, p Page) ([]Comment, error) {
	offset, err := p.offset()
	if err != nil || org <= 0 || id <= 0 {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT c.id,c.org_id,c.ticket_id,c.author_id,c.content,c.created_at FROM ticket_comments c JOIN tickets t ON t.org_id=c.org_id AND t.id=c.ticket_id WHERE c.org_id=$1 AND c.ticket_id=$2 AND t.deleted_at IS NULL ORDER BY c.created_at ASC,c.id ASC LIMIT $3 OFFSET $4`, org, id, p.Size, offset)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	defer rows.Close()
	items := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.OrgID, &c.TicketID, &c.AuthorID, &c.Content, &c.CreatedAt); err != nil {
			return nil, storageError(ctx, err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError(ctx, err)
	}
	return items, nil
}
func (r *PostgresRepository) ListEvents(ctx context.Context, org, id int64, p Page) ([]Event, error) {
	offset, err := p.offset()
	if err != nil || org <= 0 || id <= 0 {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT e.id,e.org_id,e.ticket_id,e.actor_id,e.event_type,e.ticket_version,e.before_data,e.after_data,e.created_at FROM ticket_events e JOIN tickets t ON t.org_id=e.org_id AND t.id=e.ticket_id WHERE e.org_id=$1 AND e.ticket_id=$2 AND t.deleted_at IS NULL ORDER BY e.created_at ASC,e.id ASC LIMIT $3 OFFSET $4`, org, id, p.Size, offset)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OrgID, &e.TicketID, &e.ActorID, &e.Type, &e.TicketVersion, &e.Before, &e.After, &e.CreatedAt); err != nil {
			return nil, storageError(ctx, err)
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError(ctx, err)
	}
	return items, nil
}

type postgresTx struct{ tx pgx.Tx }

func (r *PostgresRepository) Begin(ctx context.Context) (Transaction, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	return &postgresTx{tx: tx}, nil
}
func (t *postgresTx) FindActiveMembership(ctx context.Context, org, user int64) (organization.Membership, error) {
	return organization.FindActiveMembershipWith(ctx, t.tx, org, user)
}
func (t *postgresTx) GetTicket(ctx context.Context, org, id int64) (Ticket, error) {
	if org <= 0 || id <= 0 {
		return Ticket{}, ErrInvalidRequest
	}
	return scanTicket(ctx, t.tx.QueryRow(ctx, "SELECT "+ticketColumns+" FROM tickets WHERE org_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE", org, id))
}
func (t *postgresTx) CreateTicket(ctx context.Context, row Ticket) (Ticket, error) {
	return scanTicket(ctx, t.tx.QueryRow(ctx, "INSERT INTO tickets(org_id,creator_id,title,body,priority,status,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'OPEN',0,clock_timestamp(),clock_timestamp()) RETURNING "+ticketColumns, row.OrgID, row.CreatorID, row.Title, row.Body, row.Priority))
}
func (t *postgresTx) UpdateTicket(ctx context.Context, row Ticket, expected int64, from Status) (Ticket, error) {
	result, err := scanTicket(ctx, t.tx.QueryRow(ctx, "UPDATE tickets SET title=$1,body=$2,priority=$3,assignee_id=$4,status=$5,deleted_at=$6,version=version+1,updated_at=clock_timestamp() WHERE org_id=$7 AND id=$8 AND version=$9 AND status=$10 AND deleted_at IS NULL RETURNING "+ticketColumns, row.Title, row.Body, row.Priority, row.AssigneeID, row.Status, row.DeletedAt, row.OrgID, row.ID, expected, from))
	if errors.Is(err, ErrNotFound) {
		return Ticket{}, ErrConflict
	}
	return result, err
}
func (t *postgresTx) ClaimTicket(ctx context.Context, org, id, actor, expected int64) (Ticket, error) {
	if org <= 0 || id <= 0 || actor <= 0 || expected < 0 {
		return Ticket{}, ErrInvalidRequest
	}
	row, err := scanTicket(ctx, t.tx.QueryRow(ctx, `UPDATE tickets
		SET assignee_id=$3, version=version+1, updated_at=clock_timestamp()
		WHERE org_id=$1 AND id=$2 AND status='OPEN' AND assignee_id IS NULL
		AND deleted_at IS NULL AND version=$4 AND version<9223372036854775807
		RETURNING `+ticketColumns, org, id, actor, expected))
	if errors.Is(err, ErrNotFound) {
		return Ticket{}, ErrConflict
	}
	return row, err
}
func (t *postgresTx) InsertComment(ctx context.Context, c Comment) (Comment, error) {
	err := t.tx.QueryRow(ctx, `INSERT INTO ticket_comments(org_id,ticket_id,author_id,content,created_at) VALUES($1,$2,$3,$4,clock_timestamp()) RETURNING id,created_at`, c.OrgID, c.TicketID, c.AuthorID, c.Content).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return Comment{}, storageError(ctx, err)
	}
	return c, nil
}
func (t *postgresTx) InsertEvent(ctx context.Context, e Event) error {
	_, err := t.tx.Exec(ctx, `INSERT INTO ticket_events(org_id,ticket_id,actor_id,event_type,ticket_version,before_data,after_data,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp())`, e.OrgID, e.TicketID, e.ActorID, e.Type, e.TicketVersion, e.Before, e.After)
	if err != nil {
		return storageError(ctx, err)
	}
	return nil
}
func (t *postgresTx) Commit(ctx context.Context) error {
	if err := t.tx.Commit(ctx); err != nil {
		return storageError(ctx, err)
	}
	return nil
}
func (t *postgresTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }
