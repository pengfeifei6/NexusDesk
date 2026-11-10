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
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func TestWeek02Organizations(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W02_ORGANIZATIONS") != "1" {
		t.Skip("requires --organizations mode of scripts/week02-day2-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Database.Name, "nexusdesk_w02d2_test_") {
		t.Fatal("refusing organization tests outside dedicated loopback test database")
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
	login, err := auth.NewLoginService(ctx, users, passwords, tokens)
	if err != nil {
		t.Fatal(err)
	}
	repo := organization.NewPostgresRepository(pool)
	router := httpserver.NewRouter(auth.NewHandler(register).RegisterRoutes, auth.NewLoginHandler(login).RegisterRoutes, func(g *gin.RouterGroup) {
		p := g.Group("")
		p.Use(auth.RequireUser(tokens, users))
		auth.NewMeHandler(users).RegisterRoutes(p)
		organization.NewHandler(organization.NewService(repo)).RegisterRoutes(p)
	})
	fixturePassword := "Only-Organization-Fixture-LongPassword"
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	makeUser := func(name string) auth.User {
		u, err := register.Register(ctx, auth.RegisterInput{Email: name + tag + "@example.test", DisplayName: name, Password: fixturePassword})
		if err != nil {
			t.Fatal("fixture user creation failed")
		}
		return u
	}
	alice, bob := makeUser("alice"), makeUser("bob")
	call := func(target http.Handler, token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		target.ServeHTTP(rec, req)
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable protected response")
		}
		if strings.Contains(rec.Body.String(), "$argon2id$") || strings.Contains(rec.Body.String(), fixturePassword) {
			t.Fatal("response leaked credential")
		}
		return rec
	}
	loginAs := func(u auth.User) string {
		payload, _ := json.Marshal(map[string]string{"email": u.Email, "password": fixturePassword})
		rec := call(router, "", "POST", "/api/v1/auth/login", string(payload))
		var result struct {
			Data auth.AccessToken `json:"data"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Data.AccessToken == "" {
			t.Fatal("real fixture login failed")
		}
		return result.Data.AccessToken
	}
	aliceToken, bobToken := loginAs(alice), loginAs(bob)
	create := func(token, name string) int64 {
		payload, _ := json.Marshal(map[string]string{"name": name})
		rec := call(router, token, "POST", "/api/v1/organizations", string(payload))
		var result struct {
			Data struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Role string `json:"my_role"`
			} `json:"data"`
		}
		if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Data.Role != "admin" || result.Data.ID <= 0 || result.Data.Name != strings.TrimSpace(name) {
			t.Fatal("organization creation response invalid")
		}
		return result.Data.ID
	}
	type listResult struct {
		Data struct {
			Items []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Role string `json:"my_role"`
			} `json:"items"`
			Page int64 `json:"page"`
			Size int64 `json:"page_size"`
		} `json:"data"`
	}
	list := func(token, path string) listResult {
		rec := call(router, token, "GET", path, "")
		var result listResult
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || result.Data.Items == nil {
			t.Fatal("invalid organizations list")
		}
		return result
	}
	exec := func(statement string, args ...any) {
		if _, err := pool.Exec(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	countName := func(name string) int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM organizations WHERE name=$1", name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("registered_user_has_no_organization_and_me_is_public_DTO", func(t *testing.T) {
		if len(list(aliceToken, "/api/v1/organizations").Data.Items) != 0 {
			t.Fatal("registration created membership")
		}
		rec := call(router, aliceToken, "GET", "/api/v1/me", "")
		var b map[string]map[string]any
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b["data"]) != 3 || b["data"]["id"] != float64(alice.ID) {
			t.Fatal("wrong current user")
		}
	})
	bobOrg := create(bobToken, "Bob "+tag)
	aliceOrg := create(aliceToken, "  Alice "+tag+"  ")
	aliceOrg2 := create(aliceToken, "Other "+tag)
	t.Run("creator_is_admin_and_only_current_active_relations_are_paginated", func(t *testing.T) {
		var role, status string
		if err := pool.QueryRow(ctx, "SELECT role,status FROM organization_members WHERE org_id=$1 AND user_id=$2", aliceOrg, alice.ID).Scan(&role, &status); err != nil || role != "admin" || status != "active" {
			t.Fatal("creator membership not committed")
		}
		exec("INSERT INTO organization_members(org_id,user_id,role,status) VALUES($1,$2,'member','disabled')", bobOrg, alice.ID)
		exec("UPDATE organization_members SET role='member' WHERE org_id=$1 AND user_id=$2", aliceOrg2, alice.ID)
		a := list(aliceToken, "/api/v1/organizations?page=1&page_size=1")
		b := list(aliceToken, "/api/v1/organizations?page=2&page_size=1")
		if len(a.Data.Items) != 1 || a.Data.Items[0].ID != aliceOrg || len(b.Data.Items) != 1 || b.Data.Items[0].ID != aliceOrg2 || b.Data.Items[0].Role != "member" {
			t.Fatal("filter before paging or ordering failed")
		}
		onlyBob := list(bobToken, "/api/v1/organizations")
		if len(onlyBob.Data.Items) != 1 || onlyBob.Data.Items[0].ID != bobOrg {
			t.Fatal("other user's organizations exposed")
		}
		exec("UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", aliceOrg, alice.ID)
		a = list(aliceToken, "/api/v1/organizations?page=1&page_size=1")
		if len(a.Data.Items) != 1 || a.Data.Items[0].ID != aliceOrg2 {
			t.Fatal("disabled relation consumed a page slot")
		}
		if call(router, aliceToken, "GET", "/api/v1/me", "").Code != 200 {
			t.Fatal("organization disabled revoked global identity")
		}
		if len(list(aliceToken, "/api/v1/organizations?page=9223372036854775807&page_size=1").Data.Items) != 0 {
			t.Fatal("large valid empty page")
		}
	})
	t.Run("forged_identity_and_malformed_inputs_never_write", func(t *testing.T) {
		name := "forged-" + tag
		for _, field := range []string{"owner_id", "admin_user_id", "user_id", "role", "status"} {
			body := fmt.Sprintf(`{"name":%q,%q:99}`, name, field)
			if call(router, aliceToken, "POST", "/api/v1/organizations", body).Code != 400 {
				t.Fatal("forged field accepted")
			}
		}
		if countName(name) != 0 {
			t.Fatal("forged input wrote organization")
		}
		for _, path := range []string{"/api/v1/me?user_id=99", "/api/v1/organizations?user_id=99", "/api/v1/organizations?page=9223372036854775807&page_size=100"} {
			if call(router, aliceToken, "GET", path, "").Code != 400 {
				t.Fatal("invalid GET accepted")
			}
		}
	})
	t.Run("second_insert_and_deferred_commit_failure_roll_back", func(t *testing.T) {
		exec(`CREATE FUNCTION nexusdesk_d5_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private fixture detail'; END $$`)
		defer exec(`DROP FUNCTION nexusdesk_d5_fail()`)
		for _, deferred := range []bool{false, true} {
			trigger := `CREATE TRIGGER nexusdesk_d5_failure BEFORE INSERT ON organization_members FOR EACH ROW EXECUTE FUNCTION nexusdesk_d5_fail()`
			if deferred {
				trigger = `CREATE CONSTRAINT TRIGGER nexusdesk_d5_failure AFTER INSERT ON organization_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION nexusdesk_d5_fail()`
			}
			exec(trigger)
			func() {
				defer exec(`DROP TRIGGER nexusdesk_d5_failure ON organization_members`)
				name := fmt.Sprintf("rollback-%t-%s", deferred, tag)
				result, err := repo.CreateOrganizationWithAdmin(ctx, alice.ID, name)
				if !errors.Is(err, organization.ErrStorage) || result.Organization.ID != 0 || countName(name) != 0 {
					t.Fatal("failed transaction left a partial result")
				}
				rec := call(router, aliceToken, "POST", "/api/v1/organizations", fmt.Sprintf(`{"name":%q}`, name))
				if rec.Code != 500 || strings.Contains(rec.Body.String(), "private") || countName(name) != 0 {
					t.Fatal("failed transaction returned success/leaked detail/left orphan")
				}
			}()
		}
	})
	t.Run("missing_creator_rolls_back_and_is_unauthenticated", func(t *testing.T) {
		name := "missing-creator-" + tag
		_, err := repo.CreateOrganizationWithAdmin(ctx, 9223372036854775807, name)
		if !errors.Is(err, organization.ErrUserNotFound) || countName(name) != 0 {
			t.Fatal("missing creator did not roll back")
		}
		removed := makeUser("removed")
		token := loginAs(removed)
		exec("DELETE FROM users WHERE id=$1", removed.ID)
		for _, path := range []string{"/api/v1/me", "/api/v1/organizations"} {
			rec := call(router, token, "GET", path, "")
			if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("deleted user token accepted")
			}
		}
	})
	t.Run("invalid_or_missing_token_rejected_on_all_protected_routes", func(t *testing.T) {
		for _, token := range []string{"", aliceToken + "x"} {
			for _, route := range [][2]string{{"GET", "/api/v1/me"}, {"GET", "/api/v1/organizations"}, {"POST", "/api/v1/organizations"}} {
				rec := call(router, token, route[0], route[1], "")
				if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatal("unprotected endpoint")
				}
			}
		}
	})
	t.Run("canceled_second_insert_rolls_back_and_releases_connection", func(t *testing.T) {
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
		name := "cancel-" + tag
		started := time.Now()
		_, err = repo.CreateOrganizationWithAdmin(short, alice.ID, name)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 4*time.Second {
			t.Fatal("in-flight transaction cancellation failed")
		}
		if err := lock.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if countName(name) != 0 {
			t.Fatal("canceled transaction left orphan")
		}
		if _, err := repo.ListOrganizationsByUser(ctx, alice.ID, organization.PageQuery{Limit: 20}); err != nil {
			t.Fatal("connection unusable after rollback")
		}
	})
	t.Run("database_fault_is_safe_500", func(t *testing.T) {
		closed, err := database.Open(ctx, cfg.Database)
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		deadUsers := auth.NewPostgresRepository(closed)
		deadRepo := organization.NewPostgresRepository(closed)
		dead := httpserver.NewRouter(func(g *gin.RouterGroup) {
			g.Use(auth.RequireUser(tokens, deadUsers))
			auth.NewMeHandler(deadUsers).RegisterRoutes(g)
		})
		if call(dead, aliceToken, "GET", "/api/v1/me", "").Code != 500 {
			t.Fatal("user storage fault became401")
		}
		deadOrg := httpserver.NewRouter(func(g *gin.RouterGroup) {
			g.Use(auth.RequireUser(tokens, users))
			organization.NewHandler(organization.NewService(deadRepo)).RegisterRoutes(g)
		})
		for _, method := range []string{"GET", "POST"} {
			rec := call(deadOrg, aliceToken, method, "/api/v1/organizations", map[bool]string{true: `{"name":"Fail"}`, false: ""}[method == "POST"])
			if rec.Code != 500 {
				t.Fatal("organization storage fault returned success")
			}
		}
	})
}
