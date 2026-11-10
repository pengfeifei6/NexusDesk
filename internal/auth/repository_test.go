package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUserStorageErrorsAreClassifiedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"email_unique", &pgconn.PgError{Code: "23505", ConstraintName: "users_email_ci_unique", Detail: "private-fixture-secret"}, ErrEmailExists},
		{"other_unique", &pgconn.PgError{Code: "23505", ConstraintName: "another_unique", Detail: "private-fixture-secret"}, ErrStorage},
		{"check", &pgconn.PgError{Code: "23514", Detail: "private-fixture-secret"}, ErrStorage},
		{"missing", pgx.ErrNoRows, ErrNotFound},
		{"unavailable", errors.New("postgres://private-fixture-secret"), ErrStorage},
		{"canceled", context.Canceled, context.Canceled},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mapUserError(context.Background(), tc.err)
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "private-fixture-secret") {
				t.Fatal("database failure was misclassified or leaked details")
			}
		})
	}
}
