package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
)

func TestWeek04Claim(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W04_CLAIM") != "1" {
		t.Skip("requires scripts/week04-smoke.py")
	}
	guarded := ticketDB(t)
	cfg := guarded.Config()
	cfg.MaxConns = 24
	cfg.ConnConfig.RuntimeParams["application_name"] = "nexusdesk_w04_claim_test"
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("create claim test pool")
	}
	defer pool.Close()
	// Simultaneous leases prove this suite is not serialized by a one-connection pool.
	leases := make([]*pgxpool.Conn, 20)
	defer func() {
		for _, conn := range leases {
			if conn != nil {
				conn.Release()
			}
		}
	}()
	pids := map[uint32]bool{}
	for i := range leases {
		c, e := pool.Acquire(ctx)
		if e != nil {
			t.Fatal("warm concurrent connections")
		}
		leases[i] = c
		pids[c.Conn().PgConn().PID()] = true
	}
	for i, c := range leases {
		c.Release()
		leases[i] = nil
	}
	if len(pids) != 20 {
		t.Fatal("need twenty independent PostgreSQL connections")
	}
	members := organization.NewPostgresRepository(pool)
	repo := ticket.NewPostgresRepository(pool)
	svc := ticket.NewService(repo, members)
	manager, err := auth.NewTokenManager(auth.TokenConfig{Issuer: "claim-test", Audience: "api", TTL: 15 * time.Minute, Secret: []byte(strings.Repeat("C", 32))})
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(manager, auth.NewPostgresRepository(pool)))
		ticket.NewHandler(svc).RegisterRoutes(g)
	})
	users := make([]int64, 5)
	tokens := make([]string, 5)
	tag := fmt.Sprint(time.Now().UnixNano())
	for i := range users {
		if err := pool.QueryRow(ctx, "INSERT INTO users(email,display_name,password_hash) VALUES($1,$2,'not-a-credential') RETURNING id", fmt.Sprintf("claim-%d-%s@example.test", i, tag), fmt.Sprintf("Claim %d", i)).Scan(&users[i]); err != nil {
			t.Fatal("user fixture")
		}
		tok, err := manager.Issue(users[i])
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = tok.AccessToken
	}
	oa, err := members.CreateOrganizationWithAdmin(ctx, users[0], "Claim A "+tag)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := members.CreateOrganizationWithAdmin(ctx, users[3], "Claim B "+tag)
	if err != nil {
		t.Fatal(err)
	}
	a, b := oa.Organization.ID, ob.Organization.ID
	if _, err := pool.Exec(ctx, "INSERT INTO organization_members(org_id,user_id,role,status) VALUES($1,$2,'member','active'),($1,$3,'member','active'),($1,$4,'member','disabled')", a, users[1], users[2], users[4]); err != nil {
		t.Fatal("member fixture")
	}
	create := func(t *testing.T) ticket.Ticket {
		t.Helper()
		r, err := svc.Create(ctx, users[0], a, ticket.CreateInput{Title: "Claim fixture", Body: "Fictional test", Priority: 2})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	call := func(org, id int64, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/organizations/%d/tickets/%d/claim", org, id), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		r := httptest.NewRecorder()
		router.ServeHTTP(r, req)
		return r
	}
	expect := func(t *testing.T, r *httptest.ResponseRecorder, status int) {
		t.Helper()
		if r.Code != status {
			t.Fatalf("claim HTTP %d want %d", r.Code, status)
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		if status == 409 && !strings.Contains(r.Body.String(), `"code":"TICKET_CLAIM_CONFLICT"`) {
			t.Fatal("wrong conflict code")
		}
		if strings.Contains(r.Body.String(), "private claim fixture") {
			t.Fatal("raw database error leaked")
		}
	}
	assertRow := func(t *testing.T, id, version int64, assignee *int64, eventCount int) {
		t.Helper()
		r, err := svc.Get(ctx, users[0], a, id)
		if err != nil || r.Version != version || r.Status != ticket.Open || (r.AssigneeID == nil) != (assignee == nil) {
			t.Fatal("unexpected final row")
		}
		if assignee != nil && *r.AssigneeID != *assignee {
			t.Fatal("winner differs from final assignee")
		}
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM ticket_events WHERE org_id=$1 AND ticket_id=$2 AND event_type='TICKET_ASSIGNED'", a, id).Scan(&n); err != nil || n != eventCount {
			t.Fatalf("assignment event count %d want %d", n, eventCount)
		}
	}

	t.Run("C01_C09_C15_C16_member_claim_repeat_and_start", func(t *testing.T) {
		r := create(t)
		expect(t, call(a, r.ID, tokens[1], `{"expected_version":0}`), 200)
		assertRow(t, r.ID, 1, &users[1], 1)
		expect(t, call(a, r.ID, tokens[1], `{"expected_version":1}`), 409)
		assertRow(t, r.ID, 1, &users[1], 1)
		if _, err := svc.Transition(ctx, users[2], a, r.ID, ticket.TransitionInput{ExpectedVersion: 1, To: ticket.InProgress}); !errors.Is(err, ticket.ErrForbidden) {
			t.Fatal("other member may start")
		}
		if got, err := svc.Transition(ctx, users[1], a, r.ID, ticket.TransitionInput{ExpectedVersion: 1, To: ticket.InProgress}); err != nil || got.Status != ticket.InProgress || got.Version != 2 {
			t.Fatal("assignee start failed")
		}
		// W03 explicitly allows admin to act on behalf of an active assignee.
		r = create(t)
		expect(t, call(a, r.ID, tokens[0], `{"expected_version":0}`), 200)
		assertRow(t, r.ID, 1, &users[0], 1)
	})
	t.Run("C02_C03_C05_C08_isolation_identity_and_input", func(t *testing.T) {
		r := create(t)
		for _, tc := range []struct {
			org         int64
			token, body string
			status      int
		}{{a, tokens[3], `{"expected_version":0}`, 404}, {b, tokens[3], `{"expected_version":0}`, 404}, {a, tokens[4], `{"expected_version":0}`, 404}, {a, "", `{"expected_version":0}`, 401}, {a, tokens[1], `{"expected_version":0,"assignee_id":1}`, 400}} {
			expect(t, call(tc.org, r.ID, tc.token, tc.body), tc.status)
		}
		assertRow(t, r.ID, 0, nil, 0)
		if err := svc.Delete(ctx, users[0], a, r.ID); err != nil {
			t.Fatal(err)
		}
		expect(t, call(a, r.ID, tokens[1], `{"expected_version":1}`), 404)
		expect(t, call(a, 9223372036854775807, tokens[1], `{"expected_version":0}`), 404)
	})
	t.Run("C04_C06_C07_nonopen_assigned_stale_and_max", func(t *testing.T) {
		r := create(t)
		expect(t, call(a, r.ID, tokens[1], `{"expected_version":1}`), 409)
		assertRow(t, r.ID, 0, nil, 0)
		if _, err := svc.Patch(ctx, users[0], a, r.ID, ticket.PatchInput{ExpectedVersion: 0, AssigneeSet: true, AssigneeID: &users[2]}); err != nil {
			t.Fatal(err)
		}
		expect(t, call(a, r.ID, tokens[1], `{"expected_version":1}`), 409)
		version := int64(1)
		for _, status := range []ticket.Status{ticket.InProgress, ticket.Resolved, ticket.Closed} {
			if _, err := svc.Transition(ctx, users[0], a, r.ID, ticket.TransitionInput{ExpectedVersion: version, To: status}); err != nil {
				t.Fatal(err)
			}
			version++
			expect(t, call(a, r.ID, tokens[1], fmt.Sprintf(`{"expected_version":%d}`, version)), 409)
		}
		r = create(t)
		if _, err := pool.Exec(ctx, "UPDATE tickets SET version=9223372036854775807 WHERE org_id=$1 AND id=$2", a, r.ID); err != nil {
			t.Fatal("max version fixture")
		}
		expect(t, call(a, r.ID, tokens[1], `{"expected_version":9223372036854775807}`), 409)
		assertRow(t, r.ID, 9223372036854775807, nil, 0)
	})
	t.Run("C10_C11_C12_C13_simultaneous_claims", func(t *testing.T) {
		for round := 0; round < 21; round++ {
			n := 20
			if round == 0 {
				n = 2
			}
			r := create(t)
			// First 20-request round holds the row until multiple live backend PIDs
			// are observed waiting on database locks; no sleep-based winner assertion.
			lock, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				_ = lock.Rollback(cleanup)
			}()
			if round == 1 {
				if _, err = lock.Exec(ctx, "SELECT id FROM tickets WHERE org_id=$1 AND id=$2 FOR UPDATE", a, r.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = lock.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			results := make(chan struct {
				actor int64
				r     *httptest.ResponseRecorder
			}, n)
			var ready sync.WaitGroup
			ready.Add(n)
			for i := 0; i < n; i++ {
				actorIndex := 1 + (i+round)%2
				go func(index int) {
					ready.Done()
					<-start
					results <- struct {
						actor int64
						r     *httptest.ResponseRecorder
					}{users[index], call(a, r.ID, tokens[index], `{"expected_version":0}`)}
				}(actorIndex)
			}
			ready.Wait()
			close(start)
			if round == 1 {
				deadline := time.Now().Add(2 * time.Second)
				blocked := 0
				for time.Now().Before(deadline) {
					if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name='nexusdesk_w04_claim_test' AND wait_event_type='Lock' AND query LIKE 'UPDATE tickets%'").Scan(&blocked); err != nil {
						t.Fatal(err)
					}
					if blocked >= 2 {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if err := lock.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				if blocked < 2 {
					t.Fatal("did not observe concurrent database lock waiters")
				}
				t.Logf("observed %d concurrent PostgreSQL UPDATE lock waiters", blocked)
			}
			var winner int64
			success, conflicts := 0, 0
			for i := 0; i < n; i++ {
				select {
				case got := <-results:
					if got.r.Code == 200 {
						success++
						winner = got.actor
						var body struct {
							Data struct {
								Assignee int64  `json:"assignee_id"`
								Version  int64  `json:"version"`
								Status   string `json:"status"`
							}
						}
						if json.Unmarshal(got.r.Body.Bytes(), &body) != nil || body.Data.Assignee != winner || body.Data.Version != 1 || body.Data.Status != "OPEN" {
							t.Fatal("winner response incorrect")
						}
					} else {
						expect(t, got.r, 409)
						conflicts++
					}
				case <-ctx.Done():
					t.Fatal("concurrent request failed to finish")
				}
			}
			if success != 1 || conflicts != n-1 {
				t.Fatalf("round %d success=%d conflict=%d", round, success, conflicts)
			}
			assertRow(t, r.ID, 1, &winner, 1)
			var actor, version int64
			var before, after []byte
			if err := pool.QueryRow(ctx, "SELECT actor_id,ticket_version,before_data,after_data FROM ticket_events WHERE org_id=$1 AND ticket_id=$2 AND event_type='TICKET_ASSIGNED'", a, r.ID).Scan(&actor, &version, &before, &after); err != nil || actor != winner || version != 1 {
				t.Fatal("event does not match winner")
			}
			var bdata, adata map[string]any
			if json.Unmarshal(before, &bdata) != nil || json.Unmarshal(after, &adata) != nil || bdata["assignee_id"] != nil || bdata["version"] != float64(0) || adata["version"] != float64(1) || adata["assignee_id"] != float64(winner) {
				t.Fatal("event before/after incorrect")
			}
			t.Logf("round %02d: requests=%d success=1 conflicts=%d version=1 assignment_events=1", round, n, n-1)
		}
	})
	t.Run("C14_event_and_deferred_commit_failure_roll_back", func(t *testing.T) {
		for _, deferred := range []bool{false, true} {
			t.Run(fmt.Sprint("deferred=", deferred), func(t *testing.T) {
				r := create(t)
				if _, err := pool.Exec(ctx, `CREATE FUNCTION nexusdesk_w04_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private claim fixture'; END $$`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := pool.Exec(ctx, "DROP FUNCTION nexusdesk_w04_fail()"); err != nil {
						t.Error("drop fixture function")
					}
				}()
				ddl := `CREATE TRIGGER nexusdesk_w04_failure BEFORE INSERT ON ticket_events FOR EACH ROW EXECUTE FUNCTION nexusdesk_w04_fail()`
				if deferred {
					ddl = `CREATE CONSTRAINT TRIGGER nexusdesk_w04_failure AFTER INSERT ON ticket_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nexusdesk_w04_fail()`
				}
				if _, err := pool.Exec(ctx, ddl); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := pool.Exec(ctx, "DROP TRIGGER nexusdesk_w04_failure ON ticket_events"); err != nil {
						t.Error("drop fixture trigger")
					}
				}()
				expect(t, call(a, r.ID, tokens[1], `{"expected_version":0}`), 500)
				assertRow(t, r.ID, 0, nil, 0)
			})
		}
	})
	t.Run("lock_cancellation_leaves_no_assignment", func(t *testing.T) {
		r := create(t)
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Rollback(context.Background())
		if _, err := lock.Exec(ctx, "SELECT id FROM tickets WHERE org_id=$1 AND id=$2 FOR UPDATE", a, r.ID); err != nil {
			t.Fatal(err)
		}
		limited, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		if _, err := svc.Claim(limited, users[1], a, r.ID, 0); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation: %v", err)
		}
		if err := lock.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		assertRow(t, r.ID, 0, nil, 0)
	})
	t.Run("unsafe_read_then_unconditional_write_counterexample", func(t *testing.T) {
		r := create(t)
		tx1, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx1.Rollback(ctx)
		tx2, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx2.Rollback(ctx)
		var x, y *int64
		if tx1.QueryRow(ctx, "SELECT assignee_id FROM tickets WHERE org_id=$1 AND id=$2", a, r.ID).Scan(&x) != nil || tx2.QueryRow(ctx, "SELECT assignee_id FROM tickets WHERE org_id=$1 AND id=$2", a, r.ID).Scan(&y) != nil || x != nil || y != nil {
			t.Fatal("both transactions must first observe unassigned")
		}
		if _, err := tx1.Exec(ctx, "UPDATE tickets SET assignee_id=$3,version=version+1 WHERE org_id=$1 AND id=$2", a, r.ID, users[1]); err != nil {
			t.Fatal(err)
		}
		if tx1.Commit(ctx) != nil {
			t.Fatal("first unsafe commit")
		}
		if _, err := tx2.Exec(ctx, "UPDATE tickets SET assignee_id=$3,version=version+1 WHERE org_id=$1 AND id=$2", a, r.ID, users[2]); err != nil {
			t.Fatal(err)
		}
		if tx2.Commit(ctx) != nil {
			t.Fatal("second unsafe commit")
		}
		assertRow(t, r.ID, 2, &users[2], 0)
		t.Log("counterexample: both readers saw NULL, both unsafe writes committed; final assignee overwritten, version=2 (isolated experiment only)")
	})
}
