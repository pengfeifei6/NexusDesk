package organization

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMemberConstraintErrorsAreSpecificAndSafe(t *testing.T) {
	for _, tc := range []struct {
		code, name string
		want       error
	}{
		{"23505", "organization_members_pkey", ErrMemberExists}, {"23505", "another_unique", ErrStorage},
		{"23503", "organization_members_user_id_fkey", ErrUserNotFound}, {"23503", "organization_members_org_id_fkey", ErrNotFound}, {"23503", "another_fk", ErrStorage},
		{"23514", "organization_members_role_check", ErrStorage},
	} {
		raw := &pgconn.PgError{Code: tc.code, ConstraintName: tc.name, Message: "private fixture", Detail: "password or SQL"}
		err := memberWriteError(context.Background(), raw)
		if !errors.Is(err, tc.want) || err.Error() == raw.Error() {
			t.Fatal("database error misclassified or exposed")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := memberWriteError(ctx, &pgconn.PgError{Code: "23505", ConstraintName: "organization_members_pkey"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost to conflict")
	}
}
