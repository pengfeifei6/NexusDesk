package organization

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
)

func membersHTTP(t *testing.T, store *memberStoreStub) (http.Handler, string) {
	t.Helper()
	manager, err := auth.NewTokenManager(auth.TokenConfig{Issuer: "fixture", Audience: "api", TTL: 15 * time.Minute, Secret: []byte(strings.Repeat("Z", 32))})
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	router := httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(manager, fixtureUsers{}))
		NewMemberHandler(NewMemberService(store)).RegisterRoutes(g)
	})
	return router, token.AccessToken
}

func TestMemberHTTPInputAndDTO(t *testing.T) {
	for _, tc := range []struct {
		name, body, mime string
		want             int
	}{
		{"valid", `{"user_id":99,"role":"member"}`, "application/json", 201}, {"admin", `{"user_id":99,"role":"admin"}`, "application/json", 201},
		{"numeric_string", `{"user_id":"99","role":"member"}`, "application/json", 400}, {"fraction", `{"user_id":99.0,"role":"member"}`, "application/json", 400}, {"exponent", `{"user_id":9e1,"role":"member"}`, "application/json", 400},
		{"overflow", `{"user_id":9223372036854775808,"role":"member"}`, "application/json", 400}, {"negative", `{"user_id":-1,"role":"member"}`, "application/json", 400}, {"zero", `{"user_id":0,"role":"member"}`, "application/json", 400},
		{"null", `{"user_id":null,"role":"member"}`, "application/json", 400}, {"missing", `{"user_id":99}`, "application/json", 400}, {"support", `{"user_id":99,"role":"support"}`, "application/json", 400},
		{"role_case", `{"user_id":99,"role":"ADMIN"}`, "application/json", 400}, {"role_null", `{"user_id":99,"role":null}`, "application/json", 400}, {"surrogate", `{"user_id":99,"role":"\ud800"}`, "application/json", 400},
		{"duplicate", `{"user_id":99,"user_id":100,"role":"member"}`, "application/json", 400}, {"escaped_duplicate", `{"user_id":99,"user\u005fid":100,"role":"member"}`, "application/json", 400},
		{"status", `{"user_id":99,"role":"member","status":"active"}`, "application/json", 400}, {"owner", `{"user_id":99,"role":"member","owner_id":42}`, "application/json", 400},
		{"org", `{"user_id":99,"role":"member","org_id":8}`, "application/json", 400}, {"actor", `{"user_id":99,"role":"member","actor_id":42}`, "application/json", 400},
		{"trailing", `{"user_id":99,"role":"member"}{}`, "application/json", 400}, {"oversized", strings.Repeat(" ", 16385), "application/json", 413}, {"mime", `{"user_id":99,"role":"member"}`, "text/plain", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memberStoreStub{role: RoleAdmin, status: StatusActive}
			r, token := membersHTTP(t, store)
			rec := requestOrg(r, token, "POST", "/api/v1/organizations/7/members", tc.body, tc.mime)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status %d want %d", rec.Code, tc.want)
			}
			if tc.want == 201 {
				var b map[string]map[string]any
				if json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b["data"]) != 5 || b["data"]["user_id"] != float64(99) || b["data"]["created_at"] != "2026-11-10T02:00:00Z" || store.orgID != 7 || store.targetID != 99 {
					t.Fatal("unsafe member DTO or wrong identity")
				}
			} else if store.actions != 0 {
				t.Fatal("invalid body wrote membership")
			}
		})
	}
	for _, tc := range []struct {
		role   Role
		status MembershipStatus
		err    error
		want   int
		code   string
	}{{RoleMember, StatusActive, nil, 403, "FORBIDDEN"}, {RoleAdmin, StatusDisabled, nil, 404, "NOT_FOUND"}, {"", "", ErrNotFound, 404, "NOT_FOUND"}, {"", "", ErrStorage, 500, "INTERNAL_ERROR"}} {
		store := &memberStoreStub{role: tc.role, status: tc.status, lookupErr: tc.err}
		r, token := membersHTTP(t, store)
		rec := requestOrg(r, token, "POST", "/api/v1/organizations/7/members", "invalid", "text/plain")
		var b map[string]map[string]any
		if rec.Code != tc.want || json.Unmarshal(rec.Body.Bytes(), &b) != nil || b["error"]["code"] != tc.code || store.actions != 0 {
			t.Fatal("permission did not precede body parsing")
		}
	}
	store := &memberStoreStub{role: RoleAdmin, status: StatusActive}
	r, token := membersHTTP(t, store)
	for _, path := range []string{"0", "-1", "1.0", "1e2", "9223372036854775808", "x", "+7"} {
		if requestOrg(r, token, "GET", "/api/v1/organizations/"+path+"/members", "", "").Code != 400 {
			t.Fatal("invalid orgID accepted")
		}
	}
	rec := requestOrg(r, token, "GET", "/api/v1/organizations/7/members?page=2&page_size=10", "", "")
	var result map[string]map[string]json.RawMessage
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &result) != nil || string(result["data"]["items"]) != "[]" || store.listPage.Offset != 10 {
		t.Fatal("member list envelope")
	}
	for _, suffix := range []string{"?user_id=99", "?page=1&page=2", "?page_size=101", "?page=9223372036854775807&page_size=100"} {
		if requestOrg(r, token, "GET", "/api/v1/organizations/7/members"+suffix, "", "").Code != 400 {
			t.Fatal("invalid member page")
		}
	}
	if requestOrg(r, token, "GET", "/api/v1/organizations/7/members", "{}", "").Code != 400 {
		t.Fatal("member GET body accepted")
	}
	for _, tc := range []struct {
		err  error
		want int
		code string
	}{{ErrMemberExists, 409, "MEMBER_ALREADY_EXISTS"}, {ErrUserNotFound, 404, "USER_NOT_FOUND"}, {ErrNotFound, 404, "NOT_FOUND"}, {ErrStorage, 500, "INTERNAL_ERROR"}} {
		store.errorOnAction = tc.err
		rec := requestOrg(r, token, "POST", "/api/v1/organizations/7/members", `{"user_id":99,"role":"member"}`, "application/json")
		var b map[string]map[string]any
		if rec.Code != tc.want || json.Unmarshal(rec.Body.Bytes(), &b) != nil || b["error"]["code"] != tc.code {
			t.Fatal("member error mapping")
		}
	}
	for _, method := range []string{"GET", "POST"} {
		if requestOrg(r, "", method, "/api/v1/organizations/7/members", "", "").Code != 401 {
			t.Fatal("missing token accepted")
		}
	}
}

func FuzzAddMemberJSON(f *testing.F) {
	for _, s := range []string{`{"user_id":99,"role":"member"}`, `{"user_id":99.0,"role":"member"}`, `{"user_id":99,"role":"\ud800"}`, `{"user_id":99,"role":"member","status":"active"}`, `[]`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 16385 {
			t.Skip()
		}
		store := &memberStoreStub{role: RoleAdmin, status: StatusActive}
		r, token := membersHTTP(t, store)
		rec := requestOrg(r, token, "POST", "/api/v1/organizations/7/members", body, "application/json")
		if rec.Code == 201 {
			var m map[string]json.RawMessage
			if json.Unmarshal([]byte(body), &m) != nil || len(m) != 2 || m["user_id"] == nil || m["role"] == nil || store.targetID <= 0 || (store.addedRole != RoleMember && store.addedRole != RoleAdmin) || store.orgID != 7 {
				t.Fatal("invalid member addition accepted")
			}
		} else if store.actions != 0 {
			t.Fatal("rejected request changed membership")
		}
	})
}
