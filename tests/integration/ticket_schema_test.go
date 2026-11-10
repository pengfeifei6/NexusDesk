package integration

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
)

func ticketDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W03_TICKETS") != "1" {
		t.Skip("requires scripts/week03-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Database.Name, "nexusdesk_w02d2_test_") {
		t.Fatal("refusing ticket tests outside dedicated loopback test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestWeek03Schema(t *testing.T) {
	pool := ticketDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('tickets','ticket_comments','ticket_events')").Scan(&n); err != nil || n != 3 {
		t.Fatal("W03 schema missing")
	}
	var a, b, u, v int64
	for _, p := range []struct {
		sql string
		out *int64
	}{{"INSERT INTO organizations(name) VALUES('schema-ticket-A') RETURNING id", &a}, {"INSERT INTO organizations(name) VALUES('schema-ticket-B') RETURNING id", &b}, {"INSERT INTO users(email,display_name,password_hash) VALUES('schema-ticket-a@example.test','A','not-a-credential') RETURNING id", &u}, {"INSERT INTO users(email,display_name,password_hash) VALUES('schema-ticket-b@example.test','B','not-a-credential') RETURNING id", &v}} {
		if err := pool.QueryRow(ctx, p.sql).Scan(p.out); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO organization_members(org_id,user_id,role) VALUES($1,$2,'admin'),($3,$4,'member')", a, u, b, v); err != nil {
		t.Fatal(err)
	}
	var ticket, version int64
	var status string
	var created time.Time
	if err := pool.QueryRow(ctx, "INSERT INTO tickets(org_id,creator_id,title,body) VALUES($1,$2,'valid','body') RETURNING id,status,version,created_at", a, u).Scan(&ticket, &status, &version, &created); err != nil || status != "OPEN" || version != 0 || created.IsZero() {
		t.Fatal("ticket defaults invalid")
	}
	bad := func(name, code, sql string, args ...any) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(ctx, sql, args...)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != code {
				t.Fatalf("expected SQLSTATE %s", code)
			}
		})
	}
	for _, p := range []struct{ name, expr string }{{"priority0", "priority=0"}, {"priority4", "priority=4"}, {"status", "status='BAD'"}, {"negative_version", "version=-1"}, {"blank_title", "title='  '"}, {"long_title", "title=repeat('中',201)"}, {"blank_body", "body=' '"}, {"status_requires_assignee", "status='IN_PROGRESS'"}} {
		code := "23514"
		if p.name == "long_title" {
			code = "22001"
		} // VARCHAR length fails before CHECK evaluation.
		bad(p.name, code, "UPDATE tickets SET "+p.expr+" WHERE org_id=$1 AND id=$2", a, ticket)
	}
	bad("cross_org_assignee", "23503", "UPDATE tickets SET assignee_id=$1 WHERE org_id=$2 AND id=$3", v, a, ticket)
	bad("missing_org", "23503", "INSERT INTO tickets(org_id,creator_id,title,body) VALUES(9223372036854775807,$1,'T','B')", u)
	bad("comment_parent_org", "23503", "INSERT INTO ticket_comments(org_id,ticket_id,author_id,content) VALUES($1,$2,$3,'text')", b, ticket, v)
	bad("comment_author_org", "23503", "INSERT INTO ticket_comments(org_id,ticket_id,author_id,content) VALUES($1,$2,$3,'text')", a, ticket, v)
	bad("comment_blank", "23514", "INSERT INTO ticket_comments(org_id,ticket_id,author_id,content) VALUES($1,$2,$3,' ')", a, ticket, u)
	bad("event_parent_org", "23503", "INSERT INTO ticket_events(org_id,ticket_id,actor_id,event_type,ticket_version) VALUES($1,$2,$3,'TICKET_CREATED',0)", b, ticket, v)
	bad("event_actor_org", "23503", "INSERT INTO ticket_events(org_id,ticket_id,actor_id,event_type,ticket_version) VALUES($1,$2,$3,'TICKET_CREATED',0)", a, ticket, v)
	bad("event_type", "23514", "INSERT INTO ticket_events(org_id,ticket_id,actor_id,event_type,ticket_version) VALUES($1,$2,$3,'CUSTOM',0)", a, ticket, u)
	bad("event_json_object", "23514", "INSERT INTO ticket_events(org_id,ticket_id,actor_id,event_type,ticket_version,before_data) VALUES($1,$2,$3,'TICKET_CREATED',0,'[]')", a, ticket, u)
	var indexes int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname IN ('tickets_org_created_idx','tickets_org_status_created_idx','ticket_comments_order_idx','ticket_events_order_idx')").Scan(&indexes); err != nil || indexes != 4 {
		t.Fatal("required query indexes missing")
	}
	if _, err := pool.Exec(ctx, "INSERT INTO ticket_comments(org_id,ticket_id,author_id,content) VALUES($1,$2,$3,'ok')", a, ticket, u); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO ticket_events(org_id,ticket_id,actor_id,event_type,ticket_version) VALUES($1,$2,$3,'TICKET_CREATED',0)", a, ticket, u); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE tickets SET assignee_id=$1 WHERE org_id=$2 AND id=$3", u, a, ticket); err != nil {
		t.Fatal("OPEN preassignment incorrectly prohibited")
	}
	var plan string
	if err := pool.QueryRow(ctx, "EXPLAIN (COSTS OFF) SELECT id FROM tickets WHERE org_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 20", a).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	t.Log("EXPLAIN first node: " + plan)
}
