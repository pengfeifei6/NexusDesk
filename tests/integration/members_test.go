package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func TestWeek02Members(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W02_MEMBERS") != "1" {
		t.Skip("requires --members mode of scripts/week02-day2-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Database.Name, "nexusdesk_w02d2_test_") {
		t.Fatal("refusing member tests outside dedicated loopback test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	users := auth.NewPostgresRepository(pool)
	passwords := auth.NewPasswordHasher()
	register := auth.NewService(users, passwords)
	tokens, err := auth.NewTokenManager(auth.TokenConfig{Issuer: cfg.JWT.Issuer, Audience: cfg.JWT.Audience, TTL: cfg.JWT.TTL, Secret: cfg.JWT.Secret})
	if err != nil {
		t.Fatal(err)
	}
	repo := organization.NewPostgresRepository(pool)
	routerFor := func(target *organization.PostgresRepository) http.Handler {
		return httpserver.NewRouter(func(g *gin.RouterGroup) {
			g.Use(auth.RequireUser(tokens, users))
			organization.NewMemberHandler(organization.NewMemberService(target)).RegisterRoutes(g)
		})
	}
	router := routerFor(repo)
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	password := "Member-Fixture-Only-LongPassword"
	makeUser := func(name string) (auth.User, string) {
		user, err := register.Register(ctx, auth.RegisterInput{Email: name + tag + "@example.test", DisplayName: name, Password: password})
		if err != nil {
			t.Fatal("member fixture registration failed")
		}
		token, err := tokens.Issue(user.ID)
		if err != nil {
			t.Fatal(err)
		}
		return user, token.AccessToken
	}
	alice, aliceToken := makeUser("Alice")
	bob, bobToken := makeUser("Bob")
	carol, carolToken := makeUser("Carol")
	dave, daveToken := makeUser("Dave")
	eve, _ := makeUser("Eve")
	frank, _ := makeUser("Frank")
	orgA, err := repo.CreateOrganizationWithAdmin(ctx, alice.ID, "A-"+tag)
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := repo.CreateOrganizationWithAdmin(ctx, bob.ID, "B-"+tag)
	if err != nil {
		t.Fatal(err)
	}
	a, b := orgA.Organization.ID, orgB.Organization.ID
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO organization_members(org_id,user_id,role,status) VALUES($1,$2,'member','active'),($1,$3,'member','disabled')", a, bob.ID, carol.ID)
	call := func(target http.Handler, token, method string, org int64, suffix, body string) *httptest.ResponseRecorder {
		path := fmt.Sprintf("/api/v1/organizations/%d/members%s", org, suffix)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		target.ServeHTTP(rec, req)
		return rec
	}
	addBody := func(id int64, role string) string { return fmt.Sprintf(`{"user_id":%d,"role":%q}`, id, role) }
	assertError := func(rec *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		var body map[string]map[string]any
		if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"]["code"] != code || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status/code %d want %d/%s", rec.Code, status, code)
		}
		if strings.Contains(rec.Body.String(), password) || strings.Contains(rec.Body.String(), "$argon2id$") || strings.Contains(rec.Body.String(), "private fixture") {
			t.Fatal("member error leaked details")
		}
	}
	count := func(org, id int64) int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM organization_members WHERE org_id=$1 AND user_id=$2", org, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("non_member_disabled_missing_org_are_identical_and_precede_input", func(t *testing.T) {
		baseline := call(router, daveToken, "GET", a, "", "")
		assertError(baseline, 404, "NOT_FOUND")
		seen := map[string]bool{}
		baselineBody, err := comparableCorrelatedError(baseline, seen)
		if err != nil {
			t.Fatal("baseline member error has invalid request identity or unsafe response shape")
		}
		for _, rec := range []*httptest.ResponseRecorder{call(router, carolToken, "GET", a, "", ""), call(router, aliceToken, "GET", b, "", ""), call(router, aliceToken, "GET", 9223372036854775807, "", ""), call(router, daveToken, "GET", a, "?page=bad", ""), call(router, carolToken, "POST", a, "", "invalid"), call(router, aliceToken, "POST", b, "", addBody(dave.ID, "member"))} {
			assertError(rec, 404, "NOT_FOUND")
			body, err := comparableCorrelatedError(rec, seen)
			if err != nil {
				t.Fatal("member error has invalid, uncorrelated or reused request identity or unsafe response shape")
			}
			if !reflect.DeepEqual(body, baselineBody) {
				t.Fatal("404 disclosed membership/resource state")
			}
		}
		if count(b, dave.ID) != 0 {
			t.Fatal("cross-organization addition wrote")
		}
	})
	t.Run("active_member_reads_only_current_org_and_safe_DTO", func(t *testing.T) {
		rec := call(router, bobToken, "GET", a, "", "")
		if rec.Code != 200 {
			t.Fatal("active member cannot read")
		}
		var body struct {
			Data struct {
				Items []map[string]any `json:"items"`
				Page  int              `json:"page"`
				Size  int              `json:"page_size"`
			} `json:"data"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Data.Items) != 3 || body.Data.Page != 1 || body.Data.Size != 20 {
			t.Fatal("member list envelope")
		}
		ids := []int64{alice.ID, bob.ID, carol.ID}
		for i, item := range body.Data.Items {
			if len(item) != 5 || item["user_id"] != float64(ids[i]) || item["display_name"] == nil || item["role"] == nil || item["status"] == nil {
				t.Fatal("member fields/order/org scope")
			}
			if _, err := time.Parse(time.RFC3339, item["created_at"].(string)); err != nil {
				t.Fatal("member timestamp")
			}
		}
		if body.Data.Items[2]["status"] != "disabled" {
			t.Fatal("authorized list hid disabled relationship")
		}
		page := call(router, aliceToken, "GET", a, "?page=2&page_size=1", "")
		var result struct {
			Data struct {
				Items []struct {
					ID int64 `json:"user_id"`
				} `json:"items"`
			} `json:"data"`
		}
		if page.Code != 200 || json.Unmarshal(page.Body.Bytes(), &result) != nil || len(result.Data.Items) != 1 || result.Data.Items[0].ID != bob.ID {
			t.Fatal("member paging")
		}
	})
	t.Run("member_cannot_probe_targets_or_forge_admin", func(t *testing.T) {
		for _, body := range []string{addBody(dave.ID, "member"), addBody(9223372036854775807, "member"), `{"user_id":1,"role":"admin","actor_id":1}`, "invalid"} {
			assertError(call(router, bobToken, "POST", a, "", body), 403, "FORBIDDEN")
		}
		if count(a, dave.ID) != 0 {
			t.Fatal("member added target")
		}
	})
	t.Run("admin_adds_registered_member_or_admin_and_duplicate_is_409", func(t *testing.T) {
		for _, target := range []struct {
			id   int64
			role string
		}{{dave.ID, "member"}, {eve.ID, "admin"}} {
			rec := call(router, aliceToken, "POST", a, "", addBody(target.id, target.role))
			var result map[string]map[string]any
			if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result["data"]) != 5 || result["data"]["user_id"] != float64(target.id) || result["data"]["role"] != target.role || result["data"]["status"] != "active" {
				t.Fatal("admin add response")
			}
			if count(a, target.id) != 1 {
				t.Fatal("addition not persisted")
			}
		}
		assertError(call(router, aliceToken, "POST", a, "", addBody(dave.ID, "admin")), 409, "MEMBER_ALREADY_EXISTS")
		assertError(call(router, aliceToken, "POST", a, "", addBody(alice.ID, "member")), 409, "MEMBER_ALREADY_EXISTS")
		assertError(call(router, aliceToken, "POST", a, "", addBody(carol.ID, "admin")), 409, "MEMBER_ALREADY_EXISTS")
		var role, status string
		if err := pool.QueryRow(ctx, "SELECT role,status FROM organization_members WHERE org_id=$1 AND user_id=$2", a, carol.ID).Scan(&role, &status); err != nil || role != "member" || status != "disabled" {
			t.Fatal("duplicate restored or changed disabled relationship")
		}
		assertError(call(router, aliceToken, "POST", a, "", addBody(9223372036854775807, "member")), 404, "USER_NOT_FOUND")
	})
	t.Run("concurrent_add_has_one_success_one_conflict", func(t *testing.T) {
		eveToken, err := tokens.Issue(eve.ID)
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan int, 2)
		var wg sync.WaitGroup
		for _, credential := range []string{aliceToken, eveToken.AccessToken} {
			wg.Add(1)
			go func(token string) {
				defer wg.Done()
				results <- call(router, token, "POST", a, "", addBody(frank.ID, "member")).Code
			}(credential)
		}
		wg.Wait()
		close(results)
		got := map[int]int{}
		for code := range results {
			got[code]++
		}
		if got[201] != 1 || got[409] != 1 || count(a, frank.ID) != 1 {
			t.Fatal("concurrent addition bypassed unique constraint")
		}
	})
	t.Run("old_tokens_observe_current_role_and_disabled_state", func(t *testing.T) {
		exec("UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", a, bob.ID)
		for _, method := range []string{"GET", "POST"} {
			assertError(call(router, bobToken, method, a, "", addBody(eve.ID, "member")), 404, "NOT_FOUND")
		}
		if call(router, bobToken, "GET", b, "", "").Code != 200 {
			t.Fatal("disabled relation revoked another organization")
		}
		exec("UPDATE organization_members SET role='member' WHERE org_id=$1 AND user_id=$2", a, alice.ID)
		assertError(call(router, aliceToken, "POST", a, "", addBody(eve.ID, "member")), 403, "FORBIDDEN")
		if call(router, aliceToken, "GET", a, "", "").Code != 200 {
			t.Fatal("demoted active member cannot read")
		}
		exec("UPDATE organization_members SET role='admin' WHERE org_id=$1 AND user_id=$2", a, alice.ID)
	})
	t.Run("invalid_input_and_missing_auth_never_write", func(t *testing.T) {
		for _, body := range []string{fmt.Sprintf(`{"user_id":%d,"role":"member","status":"active"}`, bob.ID), fmt.Sprintf(`{"user_id":%d,"role":"member","org_id":%d}`, bob.ID, b), `{"user_id":99.0,"role":"member"}`, `{"user_id":99,"role":"support"}`} {
			assertError(call(router, aliceToken, "POST", a, "", body), 400, "INVALID_REQUEST")
		}
		for _, method := range []string{"GET", "POST"} {
			assertError(call(router, "", method, a, "", ""), 401, "UNAUTHENTICATED")
		}
	})
	t.Run("transaction_commit_failure_and_cancel_do_not_add_relationship", func(t *testing.T) {
		target, _ := makeUser("Rollback")
		exec(`CREATE FUNCTION nexusdesk_d6_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private fixture detail'; END $$`)
		exec(`CREATE CONSTRAINT TRIGGER nexusdesk_d6_failure AFTER INSERT ON organization_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nexusdesk_d6_fail()`)
		func() {
			defer exec(`DROP FUNCTION nexusdesk_d6_fail()`)
			defer exec(`DROP TRIGGER nexusdesk_d6_failure ON organization_members`)
			result, err := repo.AddMember(ctx, a, target.ID, organization.RoleMember)
			if !errors.Is(err, organization.ErrStorage) || result.UserID != 0 || count(a, target.ID) != 0 {
				t.Fatal("failed commit returned member or persisted")
			}
			assertError(call(router, aliceToken, "POST", a, "", addBody(target.ID, "member")), 500, "INTERNAL_ERROR")
		}()
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Rollback(context.Background())
		if _, err := lock.Exec(ctx, "LOCK TABLE organization_members IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatal(err)
		}
		short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		_, err = repo.AddMember(short, a, target.ID, organization.RoleMember)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("add cancellation not preserved")
		}
		if err := lock.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if count(a, target.ID) != 0 {
			t.Fatal("canceled add persisted")
		}
		if _, err := repo.AddMember(ctx, 9223372036854775807, target.ID, organization.RoleMember); !errors.Is(err, organization.ErrNotFound) {
			t.Fatal("missing organization FK mapping")
		}
	})
	t.Run("database_errors_are_500_and_not_404", func(t *testing.T) {
		closed, err := database.Open(ctx, cfg.Database)
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		dead := routerFor(organization.NewPostgresRepository(closed))
		for _, method := range []string{"GET", "POST"} {
			assertError(call(dead, aliceToken, method, a, "", addBody(frank.ID, "member")), 500, "INTERNAL_ERROR")
		}
	})
}
