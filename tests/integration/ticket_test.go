package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
)

func TestWeek03Tickets(t *testing.T) {
	pool := ticketDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	users := auth.NewPostgresRepository(pool)
	members := organization.NewPostgresRepository(pool)
	repo := ticket.NewPostgresRepository(pool)
	svc := ticket.NewService(repo, members)
	tokens, err := auth.NewTokenManager(auth.TokenConfig{Issuer: cfg.JWT.Issuer, Audience: cfg.JWT.Audience, TTL: cfg.JWT.TTL, Secret: cfg.JWT.Secret})
	if err != nil {
		t.Fatal(err)
	}
	routerFor := func(s *ticket.Service) http.Handler {
		return httpserver.NewRouter(func(g *gin.RouterGroup) {
			g.Use(auth.RequireUser(tokens, users))
			ticket.NewHandler(s).RegisterRoutes(g)
		})
	}
	router := routerFor(svc)
	register := auth.NewService(users, auth.NewPasswordHasher())
	tag := strconv.FormatInt(time.Now().UnixNano(), 10)
	makeUser := func(name string) (auth.User, string) {
		u, err := register.Register(ctx, auth.RegisterInput{Email: "w03-" + name + tag + "@example.test", DisplayName: name, Password: "Only-W03-Test-LongPassword"})
		if err != nil {
			t.Fatal(err)
		}
		token, err := tokens.Issue(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return u, token.AccessToken
	}
	alice, at := makeUser("Alice")
	bob, bt := makeUser("Bob")
	carol, ct := makeUser("Carol")
	eve, et := makeUser("Eve")
	disabled, dt := makeUser("Disabled")
	oa, err := members.CreateOrganizationWithAdmin(ctx, alice.ID, "W03-A-"+tag)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := members.CreateOrganizationWithAdmin(ctx, carol.ID, "W03-B-"+tag)
	if err != nil {
		t.Fatal(err)
	}
	a, b := oa.Organization.ID, ob.Organization.ID
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO organization_members(org_id,user_id,role,status) VALUES($1,$2,'member','active'),($1,$3,'member','active'),($1,$4,'member','disabled')", a, bob.ID, eve.ID, disabled.ID)
	base := func(org int64) string { return fmt.Sprintf("/api/v1/organizations/%d/tickets", org) }
	call := func(r http.Handler, token, method, path string, input any) *httptest.ResponseRecorder {
		var body string
		if input != nil {
			data, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			body = string(data)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	expect := func(t *testing.T, rec *httptest.ResponseRecorder, status int) {
		t.Helper()
		if rec.Code != status || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("HTTP %d want %d", rec.Code, status)
		}
		if status >= 400 {
			var e map[string]map[string]any
			if json.Unmarshal(rec.Body.Bytes(), &e) != nil || e["error"]["code"] == nil || strings.Contains(rec.Body.String(), "private fixture") || strings.Contains(rec.Body.String(), cfg.Database.Password) {
				t.Fatal("unsafe error response")
			}
		}
	}
	data := func(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var x struct {
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &x) != nil {
			t.Fatal("invalid response")
		}
		return x.Data
	}
	create := func(t *testing.T, token string, org int64, title string, p int) int64 {
		rec := call(router, token, "POST", base(org), map[string]any{"title": title, "body": "test body", "priority": p})
		expect(t, rec, 201)
		d := data(t, rec)
		if d["status"] != "OPEN" || d["version"] != float64(0) || len(d) != 11 {
			t.Fatal("create defaults/DTO")
		}
		return int64(d["id"].(float64))
	}
	path := func(org, id int64) string { return base(org) + "/" + strconv.FormatInt(id, 10) }
	eventsCount := func(t *testing.T, id int64) int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM ticket_events WHERE org_id=$1 AND ticket_id=$2", a, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var id int64
	t.Run("T01_create_identity_and_initial_event", func(t *testing.T) {
		id = create(t, bt, a, "Bob ticket", 2)
		var creator, version int64
		var status string
		if err := pool.QueryRow(ctx, "SELECT creator_id,status,version FROM tickets WHERE org_id=$1 AND id=$2", a, id).Scan(&creator, &status, &version); err != nil || creator != bob.ID || version != 0 || status != "OPEN" || eventsCount(t, id) != 1 {
			t.Fatal("create not atomically persisted")
		}
	})
	t.Run("T02_T03_T04_input_membership_and_JWT", func(t *testing.T) {
		for _, body := range []map[string]any{{"title": "T", "body": "B", "creator_id": alice.ID}, {"title": "T", "body": "B", "org_id": b}, {"title": " ", "body": "B"}, {"title": "T", "body": "B", "priority": 4}} {
			expect(t, call(router, bt, "POST", base(a), body), 400)
		}
		expect(t, call(router, ct, "POST", base(a), map[string]any{"title": "forged", "body": "B"}), 404)
		expect(t, call(router, dt, "GET", path(a, id), nil), 404)
		expect(t, call(router, "", "GET", path(a, id), nil), 401)
		now := time.Now()
		expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: strconv.FormatInt(bob.ID, 10), Issuer: cfg.JWT.Issuer, Audience: jwt.ClaimStrings{cfg.JWT.Audience}, IssuedAt: jwt.NewNumericDate(now.Add(-16 * time.Minute)), ExpiresAt: jwt.NewNumericDate(now.Add(-time.Minute))}).SignedString(cfg.JWT.Secret)
		if err != nil {
			t.Fatal("expired fixture failed")
		}
		expect(t, call(router, expired, "GET", path(a, id), nil), 401)
	})
	t.Run("T05_T06_T07_T08_filters_and_scope", func(t *testing.T) {
		foreign := create(t, ct, b, "Foreign", 3)
		first := create(t, at, a, "list-first", 3)
		second := create(t, at, a, "list-second", 3)
		exec("UPDATE tickets SET created_at='2026-11-10T00:00:00Z' WHERE org_id=$1 AND id IN($2,$3)", a, first, second)
		for _, q := range []struct {
			page int
			want int64
		}{{1, second}, {2, first}} {
			rec := call(router, bt, "GET", base(a)+fmt.Sprintf("?page=%d&page_size=1&status=OPEN&priority=3", q.page), nil)
			expect(t, rec, 200)
			items := data(t, rec)["items"].([]any)
			if len(items) != 1 || items[0].(map[string]any)["id"] != float64(q.want) {
				t.Fatal("stable scoped filters/page")
			}
		}
		expect(t, call(router, ct, "GET", path(b, id), nil), 404)
		expect(t, call(router, ct, "GET", path(a, id), nil), 404)
		expect(t, call(router, bt, "GET", path(a, foreign), nil), 404)
		expect(t, call(router, bt, "GET", base(a)+"?status=BAD", nil), 400)
		expect(t, call(router, bt, "GET", base(a)+"?page=9223372036854775807&page_size=100", nil), 400)
		expect(t, call(router, et, "GET", path(a, id), nil), 200) // W03 all active members can read.
	})
	t.Run("T10_T11_T12_T13_T14_edit_assignment_versions", func(t *testing.T) {
		expect(t, call(router, bt, "PATCH", path(a, id), map[string]any{"expected_version": 0, "title": "Changed"}), 200)
		if d := data(t, call(router, bt, "GET", path(a, id), nil)); d["version"] != float64(1) {
			t.Fatal("version not incremented")
		}
		expect(t, call(router, et, "PATCH", path(a, id), map[string]any{"expected_version": 1, "title": "Other"}), 403)
		expect(t, call(router, bt, "PATCH", path(a, id), map[string]any{"expected_version": 1, "assignee_id": bob.ID}), 403)
		for _, target := range []int64{carol.ID, disabled.ID} {
			expect(t, call(router, at, "PATCH", path(a, id), map[string]any{"expected_version": 1, "assignee_id": target}), 400)
		}
		expect(t, call(router, bt, "PATCH", path(a, id), map[string]any{"expected_version": 0, "title": "Stale"}), 409)
		expect(t, call(router, at, "PATCH", path(a, id), map[string]any{"expected_version": 1, "status": "CLOSED"}), 400)
	})
	t.Run("T15_T16_T17_T18_T19_state_machine", func(t *testing.T) {
		move := func(token string, v int64, to string, want int) {
			rec := call(router, token, "POST", path(a, id)+"/transition", map[string]any{"expected_version": v, "to_status": to})
			expect(t, rec, want)
			if want == 200 && data(t, rec)["version"] != float64(v+1) {
				t.Fatal("transition version")
			}
		}
		move(bt, 1, "IN_PROGRESS", 409)
		move(at, 1, "CLOSED", 409)
		expect(t, call(router, at, "PATCH", path(a, id), map[string]any{"expected_version": 1, "assignee_id": bob.ID}), 200)
		move(et, 2, "IN_PROGRESS", 403)
		move(bt, 2, "IN_PROGRESS", 200)
		move(bt, 3, "RESOLVED", 200)
		move(bt, 4, "IN_PROGRESS", 403)
		move(at, 4, "IN_PROGRESS", 200)
		move(bt, 5, "RESOLVED", 200)
		move(bt, 6, "CLOSED", 403)
		move(at, 6, "CLOSED", 200)
		move(at, 7, "OPEN", 409)
		expect(t, call(router, at, "PATCH", path(a, id), map[string]any{"expected_version": 7, "title": "closed"}), 409)
		expect(t, call(router, bt, "POST", path(a, id)+"/comments", map[string]any{"content": "closed"}), 409)
	})
	t.Run("T21_T23_comments_events_scope_and_version", func(t *testing.T) {
		open := create(t, bt, a, "comment-parent", 2)
		rec := call(router, et, "POST", path(a, open)+"/comments", map[string]any{"content": "  comment body only  "})
		expect(t, rec, 201)
		if len(data(t, rec)) != 5 || data(t, rec)["author_id"] != float64(eve.ID) {
			t.Fatal("comment DTO")
		}
		if data(t, call(router, bt, "GET", path(a, open), nil))["version"] != float64(0) {
			t.Fatal("comment incremented ticket version")
		}
		for _, child := range []string{"/comments", "/events"} {
			expect(t, call(router, ct, "GET", path(b, open)+child, nil), 404)
			expect(t, call(router, ct, "GET", path(a, open)+child, nil), 404)
		}
		expect(t, call(router, ct, "POST", path(b, open)+"/comments", map[string]any{"content": "wrong"}), 404)
		comments := data(t, call(router, bt, "GET", path(a, open)+"/comments", nil))["items"].([]any)
		if len(comments) != 1 || comments[0].(map[string]any)["content"] != "comment body only" {
			t.Fatal("comment persistence")
		}
		ev := call(router, bt, "GET", path(a, open)+"/events", nil)
		expect(t, ev, 200)
		if strings.Contains(ev.Body.String(), "comment body only") || len(data(t, ev)["items"].([]any)) != 2 {
			t.Fatal("event leaked comment or missing audit")
		}
	})
	t.Run("T09_T20_T22_all_write_event_and_commit_failures_rollback", func(t *testing.T) {
		target := create(t, at, a, "rollback-parent", 2)
		expect(t, call(router, at, "PATCH", path(a, target), map[string]any{"expected_version": 0, "assignee_id": bob.ID}), 200)
		for _, deferred := range []bool{false, true} {
			exec(`CREATE FUNCTION nexusdesk_w03_event_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private fixture'; END $$`)
			ddl := `CREATE TRIGGER nexusdesk_w03_failure BEFORE INSERT ON ticket_events FOR EACH ROW EXECUTE FUNCTION nexusdesk_w03_event_fail()`
			if deferred {
				ddl = `CREATE CONSTRAINT TRIGGER nexusdesk_w03_failure AFTER INSERT ON ticket_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nexusdesk_w03_event_fail()`
			}
			exec(ddl)
			func() {
				defer exec(`DROP FUNCTION nexusdesk_w03_event_fail()`)
				defer exec(`DROP TRIGGER nexusdesk_w03_failure ON ticket_events`)
				var beforeTickets, beforeComments int
				pool.QueryRow(ctx, "SELECT count(*) FROM tickets").Scan(&beforeTickets)
				pool.QueryRow(ctx, "SELECT count(*) FROM ticket_comments").Scan(&beforeComments)
				for _, op := range []struct {
					method, url string
					body        any
				}{{"POST", base(a), map[string]any{"title": "rollback-new", "body": "B"}}, {"PATCH", path(a, target), map[string]any{"expected_version": 1, "priority": 3}}, {"POST", path(a, target) + "/transition", map[string]any{"expected_version": 1, "to_status": "IN_PROGRESS"}}, {"POST", path(a, target) + "/comments", map[string]any{"content": "rollback-comment"}}, {"DELETE", path(a, target), nil}} {
					expect(t, call(router, at, op.method, op.url, op.body), 500)
				}
				var afterTickets, afterComments int
				pool.QueryRow(ctx, "SELECT count(*) FROM tickets").Scan(&afterTickets)
				pool.QueryRow(ctx, "SELECT count(*) FROM ticket_comments").Scan(&afterComments)
				d := data(t, call(router, bt, "GET", path(a, target), nil))
				if afterTickets != beforeTickets || afterComments != beforeComments || d["version"] != float64(1) || d["status"] != "OPEN" || d["priority"] != float64(2) || eventsCount(t, target) != 2 {
					t.Fatal("business/audit partially committed")
				}
			}()
		}
	})
	t.Run("T24_T25_soft_delete_hides_every_child_and_write", func(t *testing.T) {
		target := create(t, bt, a, "delete-parent", 2)
		expect(t, call(router, bt, "DELETE", path(a, target), nil), 403)
		expect(t, call(router, at, "DELETE", path(a, target), nil), 204)
		for _, op := range []struct {
			method, suffix string
			body           any
		}{{"GET", "", nil}, {"PATCH", "", map[string]any{"expected_version": 1, "title": "x"}}, {"DELETE", "", nil}, {"POST", "/transition", map[string]any{"expected_version": 1, "to_status": "IN_PROGRESS"}}, {"POST", "/comments", map[string]any{"content": "x"}}, {"GET", "/comments", nil}, {"GET", "/events", nil}} {
			expect(t, call(router, at, op.method, path(a, target)+op.suffix, op.body), 404)
		}
		var retained int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM tickets WHERE org_id=$1 AND id=$2 AND deleted_at IS NOT NULL", a, target).Scan(&retained); err != nil || retained != 1 || eventsCount(t, target) != 2 {
			t.Fatal("soft delete destroyed audit")
		}
		for _, item := range data(t, call(router, at, "GET", base(a), nil))["items"].([]any) {
			if item.(map[string]any)["id"] == float64(target) {
				t.Fatal("deleted ticket in list")
			}
		}
	})
	t.Run("concurrent_old_version_only_one_update", func(t *testing.T) {
		target := create(t, at, a, "race-parent", 2)
		results := make(chan int, 2)
		var wg sync.WaitGroup
		for _, title := range []string{"race-one", "race-two"} {
			wg.Add(1)
			go func(title string) {
				defer wg.Done()
				results <- call(router, at, "PATCH", path(a, target), map[string]any{"expected_version": 0, "title": title}).Code
			}(title)
		}
		wg.Wait()
		close(results)
		codes := map[int]int{}
		for code := range results {
			codes[code]++
		}
		if codes[200] != 1 || codes[409] != 1 || eventsCount(t, target) != 2 {
			t.Fatal("stale concurrent update/audit bypassed")
		}
	})
	t.Run("assignee_disabled_unassign_and_version_overflow", func(t *testing.T) {
		target := create(t, at, a, "active-assignee", 2)
		expect(t, call(router, at, "PATCH", path(a, target), map[string]any{"expected_version": 0, "assignee_id": bob.ID}), 200)
		exec("UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", a, bob.ID)
		expect(t, call(router, at, "POST", path(a, target)+"/transition", map[string]any{"expected_version": 1, "to_status": "IN_PROGRESS"}), 409)
		exec("UPDATE organization_members SET status='active' WHERE org_id=$1 AND user_id=$2", a, bob.ID)
		expect(t, call(router, at, "PATCH", path(a, target), map[string]any{"expected_version": 1, "assignee_id": nil}), 200)
		if data(t, call(router, at, "GET", path(a, target), nil))["assignee_id"] != nil {
			t.Fatal("null did not unassign OPEN")
		}
		exec("UPDATE tickets SET version=9223372036854775807 WHERE org_id=$1 AND id=$2", a, target)
		expect(t, call(router, at, "PATCH", path(a, target), map[string]any{"expected_version": int64(9223372036854775807), "title": "overflow"}), 409)
	})
	t.Run("real_lock_cancellation_and_comment_close_order", func(t *testing.T) {
		target := create(t, at, a, "lock-parent", 2)
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Rollback(context.Background())
		if _, err := lock.Exec(ctx, "SELECT id FROM tickets WHERE org_id=$1 AND id=$2 FOR UPDATE", a, target); err != nil {
			t.Fatal(err)
		}
		short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		title := "blocked"
		_, err = svc.Patch(short, alice.ID, a, target, ticket.PatchInput{ExpectedVersion: 0, Title: &title})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("inflight lock did not honor cancellation")
		}
		if err := lock.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if data(t, call(router, at, "GET", path(a, target), nil))["version"] != float64(0) || eventsCount(t, target) != 1 {
			t.Fatal("canceled lock waiter changed data")
		}
		closing, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer closing.Rollback(context.Background())
		if _, err := closing.Exec(ctx, "UPDATE tickets SET status='CLOSED',assignee_id=$1,version=1 WHERE org_id=$2 AND id=$3", bob.ID, a, target); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { _, err := svc.Comment(ctx, bob.ID, a, target, "blocked comment"); result <- err }()
		// The held parent lock makes either arrival order observe CLOSED after commit.
		if err := closing.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, ticket.ErrConflict) {
			t.Fatal("comment did not recheck locked CLOSED parent")
		}
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM ticket_comments WHERE org_id=$1 AND ticket_id=$2", a, target).Scan(&count); err != nil || count != 0 {
			t.Fatal("comment persisted after closed parent")
		}
	})
	t.Run("older_transaction_writes_later_audit_time", func(t *testing.T) {
		target := create(t, at, a, "audit-order", 2)
		older, err := repo.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer older.Rollback(context.Background())
		if _, err := older.FindActiveMembership(ctx, a, alice.ID); err != nil {
			t.Fatal(err)
		}
		exec("SELECT pg_sleep(0.02)")
		title := "newer transaction first"
		newer, err := svc.Patch(ctx, alice.ID, a, target, ticket.PatchInput{ExpectedVersion: 0, Title: &title})
		if err != nil {
			t.Fatal(err)
		}
		first, err := svc.Comment(ctx, bob.ID, a, target, "first inserted comment")
		if err != nil {
			t.Fatal(err)
		}
		current, err := older.GetTicket(ctx, a, target)
		if err != nil {
			t.Fatal(err)
		}
		current.Title = "older transaction later write"
		later, err := older.UpdateTicket(ctx, current, 1, ticket.Open)
		if err != nil {
			t.Fatal(err)
		}
		second, err := older.InsertComment(ctx, ticket.Comment{OrgID: a, TicketID: target, AuthorID: alice.ID, Content: "second inserted comment"})
		if err != nil {
			t.Fatal(err)
		}
		if err := older.InsertEvent(ctx, ticket.Event{OrgID: a, TicketID: target, ActorID: alice.ID, Type: "TICKET_UPDATED", TicketVersion: 2, Before: json.RawMessage(`{"version":1}`), After: json.RawMessage(`{"version":2}`)}); err != nil {
			t.Fatal(err)
		}
		if err := older.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if later.UpdatedAt.Before(newer.UpdatedAt) || second.CreatedAt.Before(first.CreatedAt) {
			t.Fatal("older transaction made later write timestamps move backwards")
		}
		rows, err := svc.Comments(ctx, alice.ID, a, target, ticket.Page{Number: 1, Size: 20})
		if err != nil || len(rows) != 2 || rows[0].ID != first.ID || rows[1].ID != second.ID {
			t.Fatal("comments sorted before an earlier inserted comment")
		}
		events, err := svc.Events(ctx, alice.ID, a, target, ticket.Page{Number: 1, Size: 20})
		if err != nil || events[len(events)-1].TicketVersion != 2 {
			t.Fatal("audit order places newer version before previous write")
		}
	})
	t.Run("T26_closed_storage_safe_500", func(t *testing.T) {
		dead, err := pgxpool.NewWithConfig(ctx, pool.Config())
		if err != nil {
			t.Fatal(err)
		}
		dead.Close()
		r := routerFor(ticket.NewService(ticket.NewPostgresRepository(dead), members))
		for _, op := range []struct {
			method, url string
			body        any
		}{{"GET", path(a, id), nil}, {"GET", base(a), nil}, {"POST", base(a), map[string]any{"title": "T", "body": "B"}}} {
			expect(t, call(r, at, op.method, op.url, op.body), 500)
		}
	})
}
