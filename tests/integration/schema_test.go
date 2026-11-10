package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
)

// TestWeek02Schema exercises the deployed migrations, not a copy of their SQL.
func TestWeek02Schema(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W02_SCHEMA") != "1" {
		t.Skip("requires the disposable database from scripts/week02-day2-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Database.Name, "nexusdesk_w02d2_test_") {
		t.Fatal("refusing schema tests outside the dedicated loopback test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var ready bool
	err = pool.QueryRow(ctx, "SELECT to_regclass('public.users') IS NOT NULL AND to_regclass('public.organization_members') IS NOT NULL").Scan(&ready)
	if err != nil || !ready {
		t.Fatalf("W02 schema missing users or organization_members: ready=%v, error=%v", ready, err)
	}

	transaction := func(t *testing.T) pgx.Tx {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("rollback test fixture: %v", err)
			}
		})
		return tx
	}
	user := func(t *testing.T, tx pgx.Tx, email string) int64 {
		t.Helper()
		var id int64
		err := tx.QueryRow(ctx, "INSERT INTO users (email, display_name, password_hash) VALUES ($1, 'Schema fixture', 'schema-only-opaque-hash') RETURNING id", email).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	organization := func(t *testing.T, tx pgx.Tx) int64 {
		t.Helper()
		var id int64
		if err := tx.QueryRow(ctx, "INSERT INTO organizations (name) VALUES ('Schema fixture') RETURNING id").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	expectState := func(t *testing.T, tx pgx.Tx, state, statement string, args ...any) {
		t.Helper()
		// A savepoint isolates the expected failure from the rest of the fixture.
		savepoint, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, queryErr := savepoint.Exec(ctx, statement, args...)
		if err := savepoint.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var pgErr *pgconn.PgError
		if !errors.As(queryErr, &pgErr) || pgErr.Code != state {
			t.Fatalf("expected SQLSTATE %s, got %v", state, queryErr)
		}
	}

	t.Run("columns_and_types", func(t *testing.T) {
		type column struct {
			table, name, dataType string
			length                int
			nullable, identity    string
		}
		want := []column{
			{"organization_members", "org_id", "bigint", 0, "NO", "NO"},
			{"organization_members", "user_id", "bigint", 0, "NO", "NO"},
			{"organization_members", "role", "character varying", 16, "NO", "NO"},
			{"organization_members", "status", "character varying", 16, "NO", "NO"},
			{"organization_members", "created_at", "timestamp with time zone", 0, "NO", "NO"},
			{"users", "id", "bigint", 0, "NO", "YES"},
			{"users", "email", "character varying", 254, "NO", "NO"},
			{"users", "display_name", "character varying", 80, "NO", "NO"},
			{"users", "password_hash", "text", 0, "NO", "NO"},
			{"users", "created_at", "timestamp with time zone", 0, "NO", "NO"},
			{"users", "updated_at", "timestamp with time zone", 0, "NO", "NO"},
		}
		rows, err := pool.Query(ctx, `SELECT table_name, column_name, data_type,
            COALESCE(character_maximum_length, 0), is_nullable, is_identity
            FROM information_schema.columns WHERE table_schema = 'public'
            AND table_name IN ('users', 'organization_members') ORDER BY table_name, ordinal_position`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []column
		for rows.Next() {
			var c column
			if err := rows.Scan(&c.table, &c.name, &c.dataType, &c.length, &c.nullable, &c.identity); err != nil {
				t.Fatal(err)
			}
			got = append(got, c)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("schema columns = %#v, want %#v", got, want)
		}
	})
	t.Run("identity_timestamp_and_active_defaults", func(t *testing.T) {
		tx := transaction(t)
		var id int64
		var created, updated, joined time.Time
		err := tx.QueryRow(ctx, "INSERT INTO users (email, display_name, password_hash) VALUES ('defaults@example.test', '默认用户', 'schema-only-opaque-hash') RETURNING id, created_at, updated_at").Scan(&id, &created, &updated)
		if err != nil || id <= 0 || created.IsZero() || updated.IsZero() {
			t.Fatalf("user defaults failed: %v", err)
		}
		org := organization(t, tx)
		var status string
		err = tx.QueryRow(ctx, "INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'admin') RETURNING status, created_at", org, id).Scan(&status, &joined)
		if err != nil || status != "active" || joined.IsZero() {
			t.Fatalf("membership defaults failed: status=%s, error=%v", status, err)
		}
	})
	t.Run("case_insensitive_email_unique", func(t *testing.T) {
		tx := transaction(t)
		user(t, tx, "Alice@example.test")
		expectState(t, tx, "23505", "INSERT INTO users (email, display_name, password_hash) VALUES ('alice@example.test', 'Another', 'schema-only-opaque-hash')")
	})
	t.Run("user_required_fields_and_lengths", func(t *testing.T) {
		for _, tc := range []struct {
			name, state, sql string
		}{
			{"email_null", "23502", "INSERT INTO users (display_name, password_hash) VALUES ('Fixture', 'opaque')"},
			{"display_name_null", "23502", "INSERT INTO users (email, password_hash) VALUES ('null@example.test', 'opaque')"},
			{"hash_null", "23502", "INSERT INTO users (email, display_name) VALUES ('null@example.test', 'Fixture')"},
			{"email_empty", "23514", "INSERT INTO users (email, display_name, password_hash) VALUES (' ', 'Fixture', 'opaque')"},
			{"display_name_empty", "23514", "INSERT INTO users (email, display_name, password_hash) VALUES ('blank@example.test', ' ', 'opaque')"},
			{"hash_empty", "23514", "INSERT INTO users (email, display_name, password_hash) VALUES ('blank@example.test', 'Fixture', ' ')"},
			{"email_too_long", "22001", "INSERT INTO users (email, display_name, password_hash) VALUES (repeat('x',255), 'Fixture', 'opaque')"},
			{"display_name_too_long", "22001", "INSERT INTO users (email, display_name, password_hash) VALUES ('long@example.test', repeat('中',81), 'opaque')"},
		} {
			t.Run(tc.name, func(t *testing.T) { expectState(t, transaction(t), tc.state, tc.sql) })
		}
	})
	t.Run("membership_constraints_and_multi_organization", func(t *testing.T) {
		tx := transaction(t)
		uid := user(t, tx, "member@example.test")
		orgA, orgB := organization(t, tx), organization(t, tx)
		_, err := tx.Exec(ctx, "INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $3, 'admin'), ($2, $3, 'member')", orgA, orgB, uid)
		if err != nil {
			t.Fatal(err)
		}
		expectState(t, tx, "23505", "INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'member')", orgA, uid)
		for _, role := range []string{"support", "ADMIN", "unknown"} {
			expectState(t, tx, "23514", "UPDATE organization_members SET role=$1 WHERE org_id=$2 AND user_id=$3", role, orgA, uid)
		}
		for _, status := range []string{"ACTIVE", "inactive", "unknown"} {
			expectState(t, tx, "23514", "UPDATE organization_members SET status=$1 WHERE org_id=$2 AND user_id=$3", status, orgA, uid)
		}
		expectState(t, tx, "23503", "INSERT INTO organization_members (org_id, user_id, role) VALUES (-1, $1, 'member')", uid)
		expectState(t, tx, "23503", "INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, -1, 'member')", orgA)
		expectState(t, tx, "23502", "INSERT INTO organization_members (org_id, user_id) VALUES ($1, $2)", organization(t, tx), uid)
		expectState(t, tx, "23502", "UPDATE organization_members SET status=NULL WHERE org_id=$1 AND user_id=$2", orgA, uid)
		if _, err := tx.Exec(ctx, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", orgA, uid); err != nil {
			t.Fatal(err)
		}
		expectState(t, tx, "23505", "INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'admin')", orgA, uid)
		expectState(t, tx, "23503", "DELETE FROM users WHERE id=$1", uid)
		expectState(t, tx, "23503", "DELETE FROM organizations WHERE id=$1", orgA)
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM organization_members WHERE user_id=$1", uid).Scan(&count); err != nil || count != 2 {
			t.Fatalf("expected two retained memberships, got %d: %v", count, err)
		}
	})
	t.Run("user_organization_lookup_index", func(t *testing.T) {
		var exists bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (
            SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
            WHERE i.indrelid='public.organization_members'::regclass
            AND i.indisvalid AND i.indisready AND i.indpred IS NULL
            AND pg_get_indexdef(c.oid, 1, true)='user_id'
            AND pg_get_indexdef(c.oid, 2, true)='org_id')`).Scan(&exists)
		if err != nil || !exists {
			t.Fatalf("missing usable user_id/org_id membership index: %v", err)
		}
	})
	t.Run("concurrent_email_unique", func(t *testing.T) {
		email := fmt.Sprintf("race-%d@example.test", time.Now().UnixNano())
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, input := range []string{email, strings.ToUpper(email)} {
			workers.Add(1)
			go func(email string) {
				defer workers.Done()
				<-start
				_, err := pool.Exec(ctx, "INSERT INTO users (email, display_name, password_hash) VALUES ($1, 'Race fixture', 'schema-only-opaque-hash')", email)
				results <- err
			}(input)
		}
		close(start)
		workers.Wait()
		close(results)
		passed, duplicates := 0, 0
		for err := range results {
			var pgErr *pgconn.PgError
			if err == nil {
				passed++
			} else if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				duplicates++
			} else {
				t.Fatalf("unexpected concurrent insert result: %v", err)
			}
		}
		if passed != 1 || duplicates != 1 {
			t.Fatalf("success=%d duplicate=%d, want 1 each", passed, duplicates)
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE lower(email)=lower($1)", email).Scan(&count); err != nil || count != 1 {
			t.Fatalf("duplicate rows after concurrent inserts: count=%d, err=%v", count, err)
		}
	})
	t.Run("concurrent_membership_unique", func(t *testing.T) {
		tx := transaction(t)
		uid := user(t, tx, fmt.Sprintf("membership-race-%d@example.test", time.Now().UnixNano()))
		org := organization(t, tx)
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				<-start
				_, err := pool.Exec(ctx, "INSERT INTO organization_members (org_id, user_id, role) VALUES ($1, $2, 'member')", org, uid)
				results <- err
			}()
		}
		close(start)
		successes, duplicates := 0, 0
		for i := 0; i < 2; i++ {
			err := <-results
			var pgErr *pgconn.PgError
			if err == nil {
				successes++
			} else if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				duplicates++
			} else {
				t.Fatalf("unexpected concurrent membership result: %v", err)
			}
		}
		var count int
		err := pool.QueryRow(ctx, "SELECT count(*) FROM organization_members WHERE org_id=$1 AND user_id=$2 AND role='member' AND status='active'", org, uid).Scan(&count)
		if err != nil || successes != 1 || duplicates != 1 || count != 1 {
			t.Fatalf("membership success=%d duplicate=%d rows=%d: %v", successes, duplicates, count, err)
		}
	})
}
