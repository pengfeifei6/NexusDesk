package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func mapUserError(ctx context.Context, err error) error {
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_ci_unique" {
		return ErrEmailExists
	}
	// PostgreSQL Detail can contain email/password_hash. Do not wrap or expose it.
	return ErrStorage
}

func (r *PostgresRepository) CreateUser(ctx context.Context, p CreateUserParams) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if strings.TrimSpace(p.Email) == "" || strings.TrimSpace(p.DisplayName) == "" || strings.TrimSpace(p.PasswordHash) == "" {
		return User{}, ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var user User
	err := r.pool.QueryRow(ctx, `INSERT INTO users (email, display_name, password_hash)
        VALUES ($1, $2, $3) RETURNING id, email, display_name, created_at, updated_at`,
		p.Email, p.DisplayName, p.PasswordHash).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, mapUserError(ctx, err)
	}
	return user, nil
}

func (r *PostgresRepository) FindUserByEmail(ctx context.Context, normalizedEmail string) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if strings.TrimSpace(normalizedEmail) == "" {
		return Credential{}, ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var credential Credential
	err := r.pool.QueryRow(ctx, `SELECT id, email, display_name, password_hash, created_at, updated_at
        FROM users WHERE lower(email) = lower($1)`, normalizedEmail).Scan(
		&credential.User.ID, &credential.User.Email, &credential.User.DisplayName, &credential.PasswordHash,
		&credential.User.CreatedAt, &credential.User.UpdatedAt)
	if err != nil {
		return Credential{}, mapUserError(ctx, err)
	}
	return credential, nil
}

func (r *PostgresRepository) FindUserByID(ctx context.Context, userID int64) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if userID <= 0 {
		return User{}, ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var user User
	err := r.pool.QueryRow(ctx, `SELECT id, email, display_name, created_at, updated_at
        FROM users WHERE id=$1`, userID).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, mapUserError(ctx, err)
	}
	return user, nil
}
