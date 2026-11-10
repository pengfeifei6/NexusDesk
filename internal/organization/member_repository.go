package organization

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func memberWriteError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && pgErr.ConstraintName == "organization_members_pkey" {
			return ErrMemberExists
		}
		if pgErr.Code == "23503" && pgErr.ConstraintName == "organization_members_org_id_fkey" {
			return ErrNotFound
		}
	}
	return storageError(ctx, err)
}

func (r *PostgresRepository) FindActiveMembership(ctx context.Context, org, user int64) (Membership, error) {
	if err := ctx.Err(); err != nil {
		return Membership{}, err
	}
	if org <= 0 || user <= 0 {
		return Membership{}, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var m Membership
	err := r.pool.QueryRow(ctx, `SELECT org_id,user_id,role,status,created_at FROM organization_members WHERE org_id=$1 AND user_id=$2 AND status='active'`, org, user).Scan(&m.OrgID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt)
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, pgx.ErrNoRows) {
			return Membership{}, ErrNotFound
		}
		return Membership{}, storageError(ctx, err)
	}
	return m, nil
}

func (r *PostgresRepository) ListMembers(ctx context.Context, org int64, p PageQuery) ([]Member, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if org <= 0 || p.Limit < 1 || p.Limit > 100 || p.Offset < 0 {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT m.org_id,m.user_id,m.role,m.status,m.created_at,u.display_name FROM organization_members m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1 ORDER BY m.user_id ASC LIMIT $2 OFFSET $3`, org, p.Limit, p.Offset)
	if err != nil {
		return nil, storageError(ctx, err)
	}
	defer rows.Close()
	items := make([]Member, 0)
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.OrgID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt, &m.DisplayName); err != nil {
			return nil, storageError(ctx, err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError(ctx, err)
	}
	return items, nil
}

// AddMember requires the service's active-admin authorization. It never updates existing relations.
func (r *PostgresRepository) AddMember(ctx context.Context, org, target int64, role Role) (Member, error) {
	if err := ctx.Err(); err != nil {
		return Member{}, err
	}
	if org <= 0 || target <= 0 || (role != RoleMember && role != RoleAdmin) {
		return Member{}, ErrInvalidRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Member{}, storageError(ctx, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var m Member
	if err := tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, target).Scan(&m.DisplayName); err != nil {
		return Member{}, memberWriteError(ctx, err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO organization_members(org_id,user_id,role,status) VALUES($1,$2,$3,'active') RETURNING org_id,user_id,role,status,created_at`, org, target, role).Scan(&m.OrgID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt)
	if err != nil {
		return Member{}, memberWriteError(ctx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Member{}, memberWriteError(ctx, err)
	}
	return m, nil
}
