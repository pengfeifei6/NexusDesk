package organization

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

type fixtureUsers struct{}

func (fixtureUsers) FindUserByID(context.Context, int64) (auth.User, error) {
	return auth.User{ID: 42}, nil
}
func orgHTTP(t *testing.T, store *storeStub) (http.Handler, string) {
	t.Helper()
	tokens, err := auth.NewTokenManager(auth.TokenConfig{Issuer: "fixture", Audience: "api", TTL: 15 * time.Minute, Secret: []byte(strings.Repeat("Q", 32))})
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	return httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(tokens, fixtureUsers{}))
		NewHandler(NewService(store)).RegisterRoutes(g)
	}), token.AccessToken
}
func requestOrg(router http.Handler, token, method, target, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
func TestOrganizationHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, body, mime string
		want             int
	}{
		{"valid", `{"name":"  Team  "}`, "application/json", 201},
		{"unknown_owner", `{"name":"T","owner_id":99}`, "application/json", 400},
		{"unknown_user", `{"name":"T","user_id":99}`, "application/json", 400},
		{"unknown_admin", `{"name":"T","admin_user_id":99}`, "application/json", 400},
		{"role", `{"name":"T","role":"admin"}`, "application/json", 400},
		{"status", `{"name":"T","status":"active"}`, "application/json", 400},
		{"duplicate", `{"name":"T","name":"Other"}`, "application/json", 400},
		{"escaped_duplicate", `{"name":"T","na\u006de":"Other"}`, "application/json", 400},
		{"trailing", `{"name":"T"}{}`, "application/json", 400},
		{"null", `{"name":null}`, "application/json", 400},
		{"wrong_type", `{"name":42}`, "application/json", 400},
		{"missing", `{}`, "application/json", 400},
		{"empty", `{"name":""}`, "application/json", 400},
		{"whitespace", `{"name":"  "}`, "application/json", 400},
		{"nul", `{"name":"T\u0000"}`, "application/json", 400},
		{"surrogate", `{"name":"\ud800"}`, "application/json", 400},
		{"invalid_utf8", "{\"name\":\"" + string([]byte{255}) + "\"}", "application/json", 400},
		{"oversized", strings.Repeat(" ", 16385), "application/json", 413},
		{"mime", `{"name":"T"}`, "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &storeStub{}
			r, token := orgHTTP(t, store)
			rec := requestOrg(r, token, "POST", "/api/v1/organizations", tc.body, tc.mime)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d want %d", rec.Code, tc.want)
			}
			if tc.want == 201 {
				var b map[string]map[string]any
				if json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b["data"]) != 3 || b["data"]["my_role"] != "admin" || store.gotID != 42 || store.gotName != "Team" {
					t.Fatal("incorrect creation DTO/identity")
				}
			} else if store.created {
				t.Fatal("invalid input wrote organization")
			}
		})
	}
	store := &storeStub{}
	r, token := orgHTTP(t, store)
	for _, target := range []string{"/api/v1/organizations", "/api/v1/organizations?page=2&page_size=100"} {
		rec := requestOrg(r, token, "GET", target, "", "")
		var b struct {
			Data struct {
				Items []any `json:"items"`
				Page  int64 `json:"page"`
				Size  int64 `json:"page_size"`
			} `json:"data"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &b) != nil || b.Data.Items == nil || b.Data.Page < 1 {
			t.Fatal("incorrect list DTO")
		}
	}
	for _, suffix := range []string{"?user_id=99", "?org_id=1", "?page=1&page=2", "?page_size=1&page_size=2", "?page=0", "?page=-1", "?page=+1", "?page=1.5", "?page_size=101", "?page_size=0", "?page=9223372036854775807&page_size=100", "?page=9223372036854775808", "?page=%ff", "?page=1;page_size=2", "?page="} {
		store.listed = false
		rec := requestOrg(r, token, "GET", "/api/v1/organizations"+suffix, "", "")
		if rec.Code != 400 || store.listed {
			t.Fatalf("bad query accepted: %s", suffix)
		}
	}
	store.listed = false
	if rec := requestOrg(r, token, "GET", "/api/v1/organizations", "{}", ""); rec.Code != 400 || store.listed {
		t.Fatal("GET body accepted")
	}
	for _, method := range []string{"GET", "POST"} {
		rec := requestOrg(r, "", method, "/api/v1/organizations", `{"owner_id":99}`, "application/json")
		if rec.Code != 401 {
			t.Fatal("authentication must precede input")
		}
	}
	for _, err := range []error{ErrStorage, ErrUserNotFound} {
		store.err = err
		rec := requestOrg(r, token, "POST", "/api/v1/organizations", `{"name":"Team"}`, "application/json")
		want := 500
		if err == ErrUserNotFound {
			want = 401
		}
		if rec.Code != want {
			t.Fatal("creation fault classification")
		}
	}
}

func FuzzOrganizationJSON(f *testing.F) {
	for _, s := range []string{`{"name":"Team"}`, `{"name":"\ud800"}`, `{"name":"T","owner_id":1}`, `{"name":"T","name":"X"}`, `[]`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 16385 {
			t.Skip()
		}
		store := &storeStub{}
		r, token := orgHTTP(t, store)
		rec := requestOrg(r, token, "POST", "/api/v1/organizations", body, "application/json")
		if rec.Code == 201 {
			var decoded map[string]json.RawMessage
			if json.Unmarshal([]byte(body), &decoded) != nil || len(decoded) != 1 || decoded["name"] == nil || !store.created || store.gotID != 42 {
				t.Fatal("accepted invalid organization input")
			}
		} else if store.created {
			t.Fatal("rejected input wrote organization")
		}
	})
}
