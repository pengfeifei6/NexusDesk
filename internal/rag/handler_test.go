package rag

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
)

func testRouter(s *Service, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if authenticated {
		r.Use(func(c *gin.Context) { c.Set("nexusdesk.auth.user_id", int64(1)); c.Next() })
	}
	NewHandler(s).RegisterRoutes(r.Group("/api/v1"))
	return r
}
func request(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}
func TestHandlerRejectsAmbiguousQueryJSONBeforeModels(t *testing.T) {
	for _, body := range []string{`{}`, `{"query":"x","query":"y"}`, `{"Query":"x"}`, `{"query":"x","extra":1}`, `{"query":null}`, `{"query":""}`, `{"query":"x","top_k":null}`, `{"query":"x","top_k":0}`, `{"query":"x","top_k":1.0}`, `{"query":"x","top_k":"1"}`, `{"query":"x","top_k":11}`, `{"query":"\ud800"}`, `{"query":"x"}{}`, `{"query":"x\u0000"}`, `{"query":"` + strings.Repeat("字", 1001) + `"}`} {
		t.Run(body[:min(35, len(body))], func(t *testing.T) {
			s, _, _, e, g := fixture()
			res := request(testRouter(s, true), "POST", "/api/v1/organizations/2/knowledge/ask", body)
			if res.Code != 400 || e.calls != 0 || g.calls != 0 {
				t.Fatalf("status=%d body=%s calls=%d/%d", res.Code, res.Body, e.calls, g.calls)
			}
		})
	}
}
func TestHandlerAuthorizesBeforeMalformedBody(t *testing.T) {
	s, _, a, e, _ := fixture()
	a.err = knowledge.ErrNotFound
	res := request(testRouter(s, true), "POST", "/api/v1/organizations/2/knowledge/ask", "{")
	if res.Code != 404 || e.calls != 0 {
		t.Fatalf("%d %s", res.Code, res.Body)
	}
	res = request(testRouter(s, false), "POST", "/api/v1/organizations/2/knowledge/ask", "{")
	if res.Code != 401 || res.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("auth: %d", res.Code)
	}
}
func TestHandlerNoInputAndStrictIDs(t *testing.T) {
	for _, tc := range []struct{ method, path, body string }{{"POST", "/documents/3/index", "{}"}, {"GET", "/documents/3/index", " "}, {"POST", "/documents/3/index?x=1", ""}, {"GET", "/documents/0/index", ""}, {"GET", "/documents/+3/index", ""}, {"POST", "/search?x=1", `{"query":"x"}`}} {
		s, _, _, e, _ := fixture()
		res := request(testRouter(s, true), tc.method, "/api/v1/organizations/2/knowledge"+tc.path, tc.body)
		if res.Code != 400 || e.calls != 0 {
			t.Fatalf("%+v: %d %s", tc, res.Code, res.Body)
		}
	}
}
func TestHandlerSearchAndAbsentIndexEnvelopes(t *testing.T) {
	s, _, _, _, _ := fixture()
	r := testRouter(s, true)
	res := request(r, "POST", "/api/v1/organizations/2/knowledge/search", `{"query":" x ","top_k":2}`)
	if res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", res.Code, res.Body)
	}
	var body struct {
		Data SearchResponse `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body.Data.Items == nil || body.Data.EmbeddingModel != EmbeddingModel {
		t.Fatalf("envelope: %s", res.Body)
	}
	res = request(r, "GET", "/api/v1/organizations/2/knowledge/documents/3/index", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"ABSENT"`) || !strings.Contains(res.Body.String(), `"started_at":null`) {
		t.Fatalf("status: %s", res.Body)
	}
}
func TestHandlerErrorsNeverExposeStorageDetails(t *testing.T) {
	s, r, _, _, _ := fixture()
	r.claimErr = errors.New("password=secret SQL SELECT private body")
	res := request(testRouter(s, true), "POST", "/api/v1/organizations/2/knowledge/documents/3/index", "")
	if res.Code != 500 || strings.Contains(res.Body.String(), "secret") || !strings.Contains(res.Body.String(), "INTERNAL_ERROR") {
		t.Fatalf("leak: %d %s", res.Code, res.Body)
	}
}
