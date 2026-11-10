package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

type userFinderFunc func(context.Context, int64) (User, error)

func (f userFinderFunc) FindUserByID(c context.Context, id int64) (User, error) { return f(c, id) }

func TestRequireUser(t *testing.T) {
	manager, err := NewTokenManager(TokenConfig{Issuer: "fixture", Audience: "api", TTL: 15 * time.Minute, Secret: []byte(strings.Repeat("X", 32))})
	if err != nil {
		t.Fatal(err)
	}
	good, _ := manager.Issue(42)
	expired := *manager
	expired.now = func() time.Time { return time.Now().Add(-time.Hour) }
	old, _ := expired.Issue(42)
	other, _ := NewTokenManager(TokenConfig{Issuer: "other", Audience: "api", TTL: 15 * time.Minute, Secret: []byte(strings.Repeat("X", 32))})
	wrong, _ := other.Issue(42)
	for _, tc := range []struct {
		name      string
		headers   []string
		lookupErr error
		missingID bool
		cancel    bool
		want      int
	}{
		{name: "valid", headers: []string{"Bearer " + good.AccessToken}, want: 204},
		{name: "case_insensitive", headers: []string{"bEaReR " + good.AccessToken}, want: 204},
		{name: "absent", want: 401}, {name: "empty", headers: []string{"Bearer "}, want: 401},
		{name: "basic", headers: []string{"Basic " + good.AccessToken}, want: 401},
		{name: "duplicate", headers: []string{"Bearer " + good.AccessToken, "Bearer " + good.AccessToken}, want: 401},
		{name: "comma", headers: []string{"Bearer " + good.AccessToken + ",Bearer " + good.AccessToken}, want: 401},
		{name: "double_space", headers: []string{"Bearer  " + good.AccessToken}, want: 401},
		{name: "tab", headers: []string{"Bearer\t" + good.AccessToken}, want: 401},
		{name: "expired", headers: []string{"Bearer " + old.AccessToken}, want: 401},
		{name: "wrong_issuer", headers: []string{"Bearer " + wrong.AccessToken}, want: 401},
		{name: "tampered", headers: []string{"Bearer " + good.AccessToken + "x"}, want: 401},
		{name: "oversized", headers: []string{"Bearer " + strings.Repeat("x", 8193)}, want: 401},
		{name: "deleted_user", headers: []string{"Bearer " + good.AccessToken}, lookupErr: ErrNotFound, want: 401},
		{name: "storage_fault", headers: []string{"Bearer " + good.AccessToken}, lookupErr: errors.New("sensitive database details"), want: 500},
		{name: "invalid_record", headers: []string{"Bearer " + good.AccessToken}, missingID: true, want: 500},
		{name: "canceled", headers: []string{"Bearer " + good.AccessToken}, cancel: true, want: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queried := false
			users := userFinderFunc(func(ctx context.Context, id int64) (User, error) {
				queried = true
				if id != 42 {
					t.Fatal("untrusted user ID")
				}
				if ctx.Err() != nil {
					return User{}, ctx.Err()
				}
				if tc.missingID {
					return User{ID: 99}, nil
				}
				return User{ID: 42}, tc.lookupErr
			})
			router := httpserver.NewRouter(func(g *gin.RouterGroup) {
				g.GET("/protected", RequireUser(manager, users), func(c *gin.Context) {
					id, ok := CurrentUserID(c)
					if !ok || id != 42 {
						t.Fatal("missing trusted identity")
					}
					if len(c.Keys) != 1 {
						t.Fatal("context stored more than identity")
					}
					c.Status(204)
				})
			})
			req := httptest.NewRequest("GET", "/api/v1/protected?user_id=99", nil)
			for _, v := range tc.headers {
				req.Header.Add("Authorization", v)
			}
			if tc.cancel {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status %d want %d", rec.Code, tc.want)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable authentication")
			}
			if tc.want == 401 && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("missing challenge")
			}
			if strings.Contains(rec.Body.String(), "sensitive") || strings.Contains(rec.Body.String(), good.AccessToken) {
				t.Fatal("error leaked credentials/details")
			}
			if tc.want == 401 && tc.lookupErr == nil && queried {
				t.Fatal("invalid credential queried user")
			}
		})
	}
}

func TestMeUsesTrustedIdentityAndStrictGET(t *testing.T) {
	calls := 0
	users := userFinderFunc(func(_ context.Context, id int64) (User, error) {
		calls++
		if id != 42 {
			t.Fatal("wrong identity")
		}
		return User{ID: 42, Email: "alice@example.test", DisplayName: "Alice"}, nil
	})
	router := httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(func(c *gin.Context) { c.Set(currentUserIDKey, int64(42)) })
		NewMeHandler(users).RegisterRoutes(g)
	})
	for _, tc := range []struct {
		target, body string
		want         int
	}{{"/api/v1/me", "", 200}, {"/api/v1/me?user_id=99", "", 400}, {"/api/v1/me", "{}", 400}} {
		before := calls
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", tc.target, strings.NewReader(tc.body)))
		if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected me status %d", rec.Code)
		}
		if tc.want == 200 {
			var result map[string]map[string]any
			if json.Unmarshal(rec.Body.Bytes(), &result) != nil || len(result["data"]) != 3 || result["data"]["id"] != float64(42) {
				t.Fatal("unsafe me DTO")
			}
		} else if calls != before {
			t.Fatal("bad GET queried user")
		}
	}
	for _, err := range []error{ErrNotFound, ErrStorage} {
		r := httpserver.NewRouter(func(g *gin.RouterGroup) {
			g.Use(func(c *gin.Context) { c.Set(currentUserIDKey, int64(42)) })
			NewMeHandler(userFinderFunc(func(context.Context, int64) (User, error) { return User{}, err })).RegisterRoutes(g)
		})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))
		want := 500
		if err == ErrNotFound {
			want = 401
		}
		if rec.Code != want {
			t.Fatal("me error classification")
		}
	}
	r := httpserver.NewRouter(NewMeHandler(users).RegisterRoutes)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/me", nil))
	if rec.Code != 401 {
		t.Fatal("unguarded me accepted identity")
	}
}
