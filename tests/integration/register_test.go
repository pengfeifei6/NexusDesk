package integration

import (
	"bytes"
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

	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/database"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func TestWeek02Registration(t *testing.T) {
	if os.Getenv("NEXUSDESK_TEST_W02_REGISTER") != "1" {
		t.Skip("requires --registration mode of scripts/week02-day2-smoke.py")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || !strings.HasPrefix(cfg.Database.Name, "nexusdesk_w02d2_test_") {
		t.Fatal("refusing registration tests outside the dedicated loopback test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := auth.NewPostgresRepository(pool)
	hasher := auth.NewPasswordHasher()
	router := httpserver.NewRouter(auth.NewHandler(auth.NewService(repo, hasher)).RegisterRoutes)
	const password = "SchemaFixture-LongRegistrationPassword"
	newEmail := func() string { return fmt.Sprintf("registration-%d@example.test", time.Now().UnixNano()) }
	requestRegistration := func(email string, target http.Handler) (*httptest.ResponseRecorder, error) {
		body, err := json.Marshal(map[string]string{"email": email, "display_name": " Alice 中文 ", "password": password})
		if err != nil {
			return nil, err
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		target.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), password) || strings.Contains(response.Body.String(), "$argon2id$") ||
			strings.Contains(response.Body.String(), cfg.Database.Password) {
			return nil, errors.New("registration response exposed a credential")
		}
		return response, nil
	}
	post := func(t *testing.T, email string, target http.Handler) *httptest.ResponseRecorder {
		t.Helper()
		response, err := requestRegistration(email, target)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	t.Run("register_persists_hash_without_organization_side_effect", func(t *testing.T) {
		var organizationsBefore, membersBefore int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM organizations), (SELECT count(*) FROM organization_members)").Scan(&organizationsBefore, &membersBefore); err != nil {
			t.Fatal(err)
		}
		email := newEmail()
		response := post(t, " "+strings.ToUpper(email)+" ", router)
		var body struct {
			Data struct {
				ID          int64  `json:"id"`
				Email       string `json:"email"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 201 || body.Data.ID <= 0 ||
			body.Data.Email != email || body.Data.DisplayName != "Alice 中文" {
			t.Fatalf("registration failed: HTTP %d", response.Code)
		}
		credential, err := repo.FindUserByEmail(ctx, strings.ToUpper(email))
		if err != nil || credential.User.ID != body.Data.ID || credential.PasswordHash == password {
			t.Fatal("registration did not persist a separate credential")
		}
		if ok, err := hasher.VerifyPassword(ctx, password, credential.PasswordHash); err != nil || !ok {
			t.Fatal("stored credential could not verify its original password")
		}
		serialized, err := json.Marshal(credential)
		if err != nil || bytes.Contains(serialized, []byte(credential.PasswordHash)) {
			t.Fatal("credential model exposed password hash to JSON")
		}
		user, err := repo.FindUserByID(ctx, body.Data.ID)
		if err != nil || user.Email != email || user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() {
			t.Fatal("user-by-ID lookup failed")
		}
		var organizationsAfter, membersAfter int
		if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM organizations), (SELECT count(*) FROM organization_members)").Scan(&organizationsAfter, &membersAfter); err != nil ||
			organizationsAfter != organizationsBefore || membersAfter != membersBefore {
			t.Fatal("registration created an organization or membership")
		}
		duplicate := post(t, strings.ToUpper(email), router)
		if duplicate.Code != 409 || !strings.Contains(duplicate.Body.String(), "EMAIL_ALREADY_EXISTS") {
			t.Fatal("duplicate normalized email was not mapped to HTTP 409")
		}
		otherEmail := newEmail()
		if post(t, otherEmail, router).Code != 201 {
			t.Fatal("second independent account was rejected")
		}
		other, err := repo.FindUserByEmail(ctx, otherEmail)
		if err != nil || other.PasswordHash == credential.PasswordHash {
			t.Fatal("same password did not receive independent random salts")
		}
	})
	t.Run("concurrent_registration_has_one_success_and_one_conflict", func(t *testing.T) {
		email := newEmail()
		start := make(chan struct{})
		type result struct {
			status int
			err    error
		}
		results := make(chan result, 2)
		for _, input := range []string{email, strings.ToUpper(email)} {
			go func(input string) {
				<-start
				response, err := requestRegistration(input, router)
				if err != nil {
					results <- result{err: err}
					return
				}
				results <- result{status: response.Code}
			}(input)
		}
		close(start)
		counts := map[int]int{}
		for i := 0; i < 2; i++ {
			select {
			case result := <-results:
				if result.err != nil {
					t.Fatal(result.err)
				}
				counts[result.status]++
			case <-time.After(10 * time.Second):
				t.Fatal("concurrent registration did not finish")
			}
		}
		var rows int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE lower(email)=lower($1)", email).Scan(&rows); err != nil || rows != 1 || counts[201] != 1 || counts[409] != 1 {
			t.Fatalf("concurrent registration violated uniqueness: statuses=%v, rows=%d", counts, rows)
		}
	})
	t.Run("display_name_nul_is_rejected_before_persistence", func(t *testing.T) {
		email := newEmail()
		body, err := json.Marshal(map[string]string{"email": email, "display_name": "A\x00B", "password": password})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "INVALID_REQUEST") {
			t.Fatal("unsupported display name reached database as an internal error")
		}
		if _, err := repo.FindUserByEmail(ctx, email); !errors.Is(err, auth.ErrNotFound) {
			t.Fatal("rejected display name created a user")
		}
	})
	t.Run("invalid_password_leaves_no_user", func(t *testing.T) {
		email := newEmail()
		body, err := json.Marshal(map[string]string{"email": email, "display_name": "Invalid password fixture", "password": "short"})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "INVALID_PASSWORD") {
			t.Fatal("invalid password was not rejected by the real registration chain")
		}
		if _, err := repo.FindUserByEmail(ctx, email); !errors.Is(err, auth.ErrNotFound) {
			t.Fatal("invalid password created a persisted user")
		}
	})
	t.Run("repository_missing_and_parameterized_lookups", func(t *testing.T) {
		if _, err := repo.FindUserByID(ctx, 9223372036854775807); !errors.Is(err, auth.ErrNotFound) {
			t.Fatal("missing user ID was not classified")
		}
		if _, err := repo.FindUserByEmail(ctx, "' OR true --"); !errors.Is(err, auth.ErrNotFound) {
			t.Fatal("email lookup was not an exact parameterized lookup")
		}
		if _, err := repo.FindUserByID(ctx, 0); !errors.Is(err, auth.ErrInvalidArgument) {
			t.Fatal("invalid user ID was accepted")
		}
		if _, err := repo.CreateUser(ctx, auth.CreateUserParams{}); !errors.Is(err, auth.ErrInvalidArgument) {
			t.Fatal("invalid create parameters were accepted")
		}
	})
	t.Run("repository_propagates_inflight_cancellation", func(t *testing.T) {
		encoded, err := hasher.HashPassword(ctx, password)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Rollback(context.Background())
		if _, err := lock.Exec(ctx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
			t.Fatal(err)
		}
		queryCtx, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		started := time.Now()
		_, err = repo.CreateUser(queryCtx, auth.CreateUserParams{Email: newEmail(), DisplayName: "Canceled fixture", PasswordHash: encoded})
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second {
			t.Fatalf("locked write ignored request deadline: %v", err)
		}
	})
	t.Run("database_failure_does_not_return_success", func(t *testing.T) {
		closed, err := database.Open(ctx, cfg.Database)
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		brokenRouter := httpserver.NewRouter(auth.NewHandler(auth.NewService(auth.NewPostgresRepository(closed), hasher)).RegisterRoutes)
		email := newEmail()
		response := post(t, email, brokenRouter)
		if response.Code != 500 || !strings.Contains(response.Body.String(), "INTERNAL_ERROR") || strings.Contains(response.Body.String(), "storage") {
			t.Fatal("database failure was reported as success or exposed internal details")
		}
		if _, err := repo.FindUserByEmail(ctx, email); !errors.Is(err, auth.ErrNotFound) {
			t.Fatal("failed database operation nevertheless created a user")
		}
	})
}
