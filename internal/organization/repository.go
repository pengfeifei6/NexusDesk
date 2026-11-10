package organization

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "organization_members_user_id_fkey" {
		return ErrUserNotFound
	}
	return ErrStorage
}
func (r *PostgresRepository) CreateOrganizationWithAdmin(ctx context.Context, id int64, name string) (OrganizationView, error) {
	if err := ctx.Err(); err != nil {
		return OrganizationView{}, err
	}
	if id <= 0 || name != strings.TrimSpace(name) || !validName(name) {
		return OrganizationView{}, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return OrganizationView{}, storageError(ctx, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var org Organization
	err = tx.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id,name,created_at`, name).Scan(&org.ID, &org.Name, &org.CreatedAt)
	if err != nil {
		return OrganizationView{}, storageError(ctx, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO organization_members (org_id,user_id,role,status) VALUES ($1,$2,'admin','active')`, org.ID, id)
	if err != nil {
		return OrganizationView{}, storageError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return OrganizationView{}, storageError(ctx, err)
	}
	return OrganizationView{Organization: org, MyRole: RoleAdmin}, nil
}
func (r *PostgresRepository) ListOrganizationsByUser(ctx context.Context, id int64, p PageQuery) ([]OrganizationView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id <= 0 || p.Limit < 1 || p.Limit > 100 || p.Offset < 0 {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT o.id,o.name,o.created_at,m.role FROM organizations o JOIN organization_members m ON m.org_id=o.id WHERE m.user_id=$1 AND m.status='active' ORDER BY o.id ASC LIMIT $2 OFFSET $3`, id, p.Limit, p.Offset)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	defer rows.Close()
	items := make([]OrganizationView, 0)
	for rows.Next() {
		var item OrganizationView
		if err := rows.Scan(&item.Organization.ID, &item.Organization.Name, &item.Organization.CreatedAt, &item.MyRole); err != nil {
			return nil, storageError(ctx, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError(ctx, err)
	}
	return items, nil
}
