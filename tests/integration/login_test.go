package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func TestWeek02Login(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W02_LOGIN") != "1" {
		t.Skip("requires --login mode of scripts/week02-day2-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Database.Name, "nexusdesk_w02d2_test_") {
		t.Fatal("refusing login tests outside the dedicated loopback test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo, passwords := auth.NewPostgresRepository(pool), auth.NewPasswordHasher()
	tokens, err := auth.NewTokenManager(auth.TokenConfig{Issuer: cfg.JWT.Issuer, Audience: cfg.JWT.Audience, TTL: cfg.JWT.TTL, Secret: cfg.JWT.Secret})
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.NewLoginService(ctx, repo, passwords, tokens)
	if err != nil {
		t.Fatal(err)
	}
	registration := auth.NewService(repo, passwords)
	router := httpserver.NewRouter(auth.NewHandler(registration).RegisterRoutes, auth.NewLoginHandler(login).RegisterRoutes)
	email := fmt.Sprintf("login-%d@example.test", time.Now().UnixNano())
	password := "  Login-Fixture-\x00-中文-Long-Password  "
	user, err := registration.Register(ctx, auth.RegisterInput{Email: email, DisplayName: "Login fixture", Password: password})
	if err != nil {
		t.Fatal("login fixture registration failed")
	}
	var usersBefore, orgsBefore, membersBefore int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM organizations), (SELECT count(*) FROM organization_members)").Scan(&usersBefore, &orgsBefore, &membersBefore); err != nil {
		t.Fatal(err)
	}
	post := func(t *testing.T, target http.Handler, input any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		target.ServeHTTP(response, request)
		if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "$argon2id$") || strings.Contains(response.Body.String(), cfg.Database.Password) || strings.Contains(response.Body.String(), base64.StdEncoding.EncodeToString(cfg.JWT.Secret)) {
			t.Fatal("login response was cacheable or exposed non-token credentials")
		}
		return response
	}
	t.Run("registered_user_logs_in_with_original_password", func(t *testing.T) {
		response := post(t, router, map[string]string{"email": " " + strings.ToUpper(email) + " ", "password": password})
		var result struct {
			Data auth.AccessToken `json:"data"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Data.TokenType != "Bearer" || result.Data.ExpiresIn != 900 {
			t.Fatal("real login failed")
		}
		if id, err := tokens.Verify(result.Data.AccessToken); err != nil || id != user.ID {
			t.Fatal("login did not issue a valid token for the registered user")
		}
		var body map[string]map[string]json.RawMessage
		if json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body) != 1 || len(body["data"]) != 3 {
			t.Fatal("login exposed fields outside the token DTO")
		}
	})
	t.Run("wrong_password_and_missing_user_are_indistinguishable", func(t *testing.T) {
		wrong := post(t, router, map[string]string{"email": email, "password": "Wrong-Long-Login-Fixture-Password"})
		missing := post(t, router, map[string]string{"email": "missing-" + email, "password": "Wrong-Long-Login-Fixture-Password"})
		dummyMatch := post(t, router, map[string]string{"email": "missing-" + email, "password": "Dummy-Login-Fixture-Never-An-Account-Password"})
		if wrong.Code != 401 || missing.Code != 401 || dummyMatch.Code != 401 || !bytes.Equal(wrong.Body.Bytes(), missing.Body.Bytes()) || !bytes.Equal(wrong.Body.Bytes(), dummyMatch.Body.Bytes()) {
			t.Fatal("login errors disclosed account state or dummy credential authenticated")
		}
		var body map[string]map[string]any
		if json.Unmarshal(wrong.Body.Bytes(), &body) != nil || body["error"]["code"] != "INVALID_CREDENTIALS" || body["error"]["message"] != "invalid email or password" {
			t.Fatal("wrong generic login failure")
		}
	})
	t.Run("empty_password_is_input_error", func(t *testing.T) {
		response := post(t, router, map[string]string{"email": email, "password": ""})
		if response.Code != 400 || !strings.Contains(response.Body.String(), "INVALID_REQUEST") {
			t.Fatal("empty password was not rejected")
		}
	})
	t.Run("corrupt_hash_is_safe_internal_error", func(t *testing.T) {
		stored, err := repo.FindUserByID(ctx, user.ID)
		if err != nil || stored.ID != user.ID {
			t.Fatal("fixture user missing")
		}
		credential, err := repo.FindUserByEmail(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, "UPDATE users SET password_hash='broken-login-fixture' WHERE id=$1", user.ID); err != nil {
			t.Fatal("could not prepare corrupt hash fixture")
		}
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if _, err := pool.Exec(cleanupCtx, "UPDATE users SET password_hash=$1 WHERE id=$2", credential.PasswordHash, user.ID); err != nil {
				t.Error("could not restore fixture hash")
			}
		})
		response := post(t, router, map[string]string{"email": email, "password": password})
		if response.Code != 500 || !strings.Contains(response.Body.String(), "INTERNAL_ERROR") || strings.Contains(response.Body.String(), "broken-login-fixture") {
			t.Fatal("corrupt hash was accepted or disclosed")
		}
	})
	t.Run("database_unavailable_is_not_credential_failure", func(t *testing.T) {
		closed, err := database.Open(ctx, cfg.Database)
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		failed, err := auth.NewLoginService(ctx, auth.NewPostgresRepository(closed), passwords, tokens)
		if err != nil {
			t.Fatal(err)
		}
		failedRouter := httpserver.NewRouter(auth.NewLoginHandler(failed).RegisterRoutes)
		response := post(t, failedRouter, map[string]string{"email": email, "password": password})
		if response.Code != 500 || !strings.Contains(response.Body.String(), "INTERNAL_ERROR") {
			t.Fatal("database error masqueraded as login success or wrong credentials")
		}
	})
	t.Run("lookup_propagates_request_deadline", func(t *testing.T) {
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			_ = lock.Rollback(cleanupCtx)
		}()
		if _, err := lock.Exec(ctx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatal(err)
		}
		queryCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		started := time.Now()
		result, err := login.Login(queryCtx, auth.LoginInput{Email: email, Password: password})
		if !errors.Is(err, context.DeadlineExceeded) || result.AccessToken != "" || time.Since(started) > 2*time.Second {
			t.Fatal("login lookup ignored request cancellation")
		}
	})
	t.Run("login_has_no_database_write_side_effects", func(t *testing.T) {
		var usersAfter, orgsAfter, membersAfter int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM organizations), (SELECT count(*) FROM organization_members)").Scan(&usersAfter, &orgsAfter, &membersAfter); err != nil || usersAfter != usersBefore || orgsAfter != orgsBefore || membersAfter != membersBefore {
			t.Fatal("login created a user, organization or membership")
		}
	})
}
