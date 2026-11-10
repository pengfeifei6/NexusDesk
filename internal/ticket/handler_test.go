package ticket

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

type ticketUser struct{}

func (ticketUser) FindUserByID(context.Context, int64) (auth.User, error) {
	return auth.User{ID: 42}, nil
}
func ticketRouter(t *testing.T, s *testStore) (http.Handler, string) {
	t.Helper()
	manager, err := auth.NewTokenManager(auth.TokenConfig{Issuer: "test", Audience: "api", TTL: 15 * time.Minute, Secret: []byte(strings.Repeat("W", 32))})
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	return httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(manager, ticketUser{}))
		NewHandler(NewService(s, s)).RegisterRoutes(g)
	}), token.AccessToken
}
func ticketRequest(r http.Handler, token, method, path, body, mime string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/organizations/7/tickets"+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", mime)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTicketHTTPStrictFieldsAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, mime string
		want                           int
	}{
		{"create", "POST", "", `{"title":"T","body":"B"}`, "application/json", 201},
		{"optional_priority", "POST", "", `{"title":"T","body":"B","priority":3}`, "application/json", 201},
		{"big_allowed_body", "POST", "", `{"title":"T","body":"` + strings.Repeat("中", 6000) + `"}`, "application/json", 201},
		{"fake_creator", "POST", "", `{"title":"T","body":"B","creator_id":99}`, "application/json", 400},
		{"fake_org", "POST", "", `{"title":"T","body":"B","org_id":8}`, "application/json", 400},
		{"duplicate", "POST", "", `{"title":"T","title":"X","body":"B"}`, "application/json", 400},
		{"surrogate", "POST", "", `{"title":"\ud800","body":"B"}`, "application/json", 400},
		{"nul", "POST", "", `{"title":"T\u0000","body":"B"}`, "application/json", 400},
		{"null", "POST", "", `{"title":null,"body":"B"}`, "application/json", 400},
		{"fraction", "POST", "", `{"title":"T","body":"B","priority":2.0}`, "application/json", 400},
		{"exponent", "POST", "", `{"title":"T","body":"B","priority":2e0}`, "application/json", 400},
		{"overflow", "PATCH", "/11", `{"expected_version":9223372036854775808,"title":"X"}`, "application/json", 400},
		{"patch_status", "PATCH", "/11", `{"expected_version":0,"status":"CLOSED"}`, "application/json", 400},
		{"patch_without_version", "PATCH", "/11", `{"title":"X"}`, "application/json", 400},
		{"patch_no_fields", "PATCH", "/11", `{"expected_version":0}`, "application/json", 400},
		{"patch", "PATCH", "/11", `{"expected_version":0,"title":"Changed"}`, "application/json", 200},
		{"comment", "POST", "/11/comments", `{"content":"hello"}`, "application/json", 201},
		{"comment_forge", "POST", "/11/comments", `{"content":"hello","author_id":99}`, "application/json", 400},
		{"skip_state", "POST", "/11/transition", `{"expected_version":0,"to_status":"CLOSED"}`, "application/json", 409},
		{"oversize", "POST", "", strings.Repeat(" ", 65537), "application/json", 413},
		{"media", "POST", "", `{}`, "text/plain", 415},
		{"write_query", "POST", "?org_id=8", `{"title":"T","body":"B"}`, "application/json", 400},
		{"list_body", "GET", "", `{}`, "", 400}, {"detail_query", "GET", "/11?user_id=99", "", "", 400},
		{"list_bad_page", "GET", "?page=9223372036854775807&page_size=100", "", "", 400},
		{"list_bad_status", "GET", "?status=INVALID", "", "", 400}, {"list_priority", "GET", "?priority=4", "", "", 400},
		{"duplicate_query", "GET", "?page=1&page=2", "", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := freshStore()
			r, token := ticketRouter(t, s)
			rec := ticketRequest(r, token, tc.method, tc.path, tc.body, tc.mime)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s status %d want %d", tc.name, rec.Code, tc.want)
			}
			if tc.want >= 400 && (len(s.events) != 0 || s.row.Version != 0) {
				t.Fatal("rejected request modified data")
			}
		})
	}
	s := freshStore()
	r, token := ticketRouter(t, s)
	for _, path := range []string{"/0", "/-1", "/9223372036854775808", "?unknown=1"} {
		if ticketRequest(r, token, "GET", path, "", "").Code != 400 {
			t.Fatal("invalid path/query accepted")
		}
	}
	for _, path := range []string{"", "/11", "/11/comments", "/11/events"} {
		if ticketRequest(r, "", "GET", path, "", "").Code != 401 {
			t.Fatal("unprotected read")
		}
	}
	rec := ticketRequest(r, token, "GET", "/11", "", "")
	var b map[string]map[string]any
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &b) != nil || len(b["data"]) != 11 || b["data"]["deleted_at"] != nil {
		t.Fatal("ticket DTO outside whitelist")
	}
	for _, path := range []string{"/11/comments", "/11/events"} {
		rec := ticketRequest(r, token, "GET", path, "", "")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items":[]`) {
			t.Fatal("empty child list")
		}
	}
	if ticketRequest(r, token, "DELETE", "/11", "", "").Code != 204 {
		t.Fatal("delete HTTP")
	}
}

func FuzzTicketJSON(f *testing.F) {
	for _, s := range []string{`{"title":"T","body":"B"}`, `{"title":"T","body":"B","status":"CLOSED"}`, `{"title":"\ud800","body":"B"}`, `{}`, `[]`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 65537 {
			t.Skip()
		}
		s := freshStore()
		r, token := ticketRouter(t, s)
		rec := ticketRequest(r, token, "POST", "", body, "application/json")
		if rec.Code == 201 {
			var fields map[string]json.RawMessage
			if json.Unmarshal([]byte(body), &fields) != nil || fields["title"] == nil || fields["body"] == nil || len(fields) > 3 || len(s.events) != 1 || s.row.CreatorID != 42 || s.row.OrgID != 7 || s.row.Status != Open {
				t.Fatal("unsafe accepted create")
			}
		} else if len(s.events) != 0 {
			t.Fatal("rejected create emitted event")
		}
	})
}
