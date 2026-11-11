package jobs

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFiveRoutesRequireIdentityAndUseSafeEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct{ method, path string }{{"POST", "/api/v1/organizations/1/knowledge/documents/2/jobs"}, {"GET", "/api/v1/organizations/1/knowledge/documents/2/jobs"}, {"GET", "/api/v1/organizations/1/jobs/3"}, {"POST", "/api/v1/organizations/1/jobs/3/retry"}, {"POST", "/api/v1/organizations/1/jobs/3/cancel"}}
	r := gin.New()
	NewHandler(nil).RegisterRoutes(r.Group("/api/v1"))
	if len(r.Routes()) != 5 {
		t.Fatal("route count")
	}
	for _, c := range cases {
		t.Run(c.method+c.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(c.method, c.path+"?unexpected=true", strings.NewReader(`{"actor":7}`)))
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("authentication contract: %d %s", w.Code, w.Body)
			}
			var body struct {
				Error struct {
					Code, Message string
					RequestID     string `json:"request_id"`
				}
			}
			if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil || body.Error.Code != "UNAUTHENTICATED" || body.Error.Message != "authentication required" {
				t.Fatalf("safe envelope %s", w.Body)
			}
		})
	}
}
func TestRouteIDsRejectNoncanonicalBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("nexusdesk.auth.user_id", int64(1)) })
	NewHandler(nil).RegisterRoutes(r.Group("/api/v1"))
	for _, id := range []string{"0", "-1", "01", "1e0", "9223372036854775808"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/organizations/"+id+"/jobs/2", nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d", id, w.Code)
		}
	}
}
func TestJobDTOExcludesOwnershipAndPreservesNumericIDs(t *testing.T) {
	b, e := json.Marshal(Job{ID: 9223372036854775807, RunVersion: 3})
	if e != nil {
		t.Fatal(e)
	}
	var obj map[string]json.RawMessage
	if e = json.Unmarshal(b, &obj); e != nil {
		t.Fatal(e)
	}
	if string(obj["id"]) != "9223372036854775807" {
		t.Fatal("lossy or string id")
	}
	if len(obj) != 22 {
		t.Fatalf("DTO fields changed: %d %s", len(obj), b)
	}
	for _, key := range []string{"lease_token", "lease_until", "dispatch_seq", "storage_key", "embedding_dimension", "extractor_version"} {
		if _, ok := obj[key]; ok {
			t.Fatal("private field exposed", key)
		}
	}
}
func TestSafeErrorsNeverExposeUnknownCode(t *testing.T) {
	for _, code := range []string{"INVALID_REQUEST", "UNAUTHENTICATED", "FORBIDDEN", "NOT_FOUND", "JOB_STATE_CONFLICT", "DOCUMENT_STATE_CONFLICT", "ASYNC_DISABLED", "INTERNAL_ERROR", "DELIVERY_RETRY"} {
		e := newError(code)
		if Code(e) != code || e.Error() == "" {
			t.Fatal(code)
		}
	}
	if Code(&Error{Code: "redis://secret"}) != "INTERNAL_ERROR" || strings.Contains((&Error{Code: "redis://secret"}).Error(), "secret") {
		t.Fatal("unsafe error")
	}
}
