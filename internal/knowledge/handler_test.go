package knowledge

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

type verifier struct{}

func (verifier) Verify(string) (int64, error) { return 10, nil }

type finder struct{}

func (finder) FindUserByID(context.Context, int64) (auth.User, error) { return auth.User{ID: 10}, nil }
func multipartBody(t *testing.T, name, body string, extra bool) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, e := w.CreateFormFile("file", name)
	if e != nil {
		t.Fatal(e)
	}
	io.WriteString(part, body)
	if extra {
		w.WriteField("org_id", "999")
	}
	w.Close()
	return b.Bytes(), w.FormDataContentType()
}
func TestKnowledgeHTTPMultipartAndSafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name, filename, body string
		extra                bool
		status               int
	}{{"valid", "guide.md", "# hello", false, 201}, {"duplicate", "guide.md", "hi", true, 400}, {"traversal", "../../x.md", "hi", false, 400}, {"empty", "guide.txt", "", false, 400}, {"binary", "guide.md", "a\x00b", false, 415}, {"pdf", "guide.pdf", "%PDF-1.7\nfixture", false, 201}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _ := fixture(t)
			router := httpserver.NewRouter(func(g *gin.RouterGroup) {
				g.Use(auth.RequireUser(verifier{}, finder{}))
				NewHandler(s).RegisterRoutes(g)
			})
			b, ct := multipartBody(t, tc.filename, tc.body, tc.extra)
			req := httptest.NewRequest("POST", "/api/v1/organizations/20/knowledge/documents", bytes.NewReader(b))
			req.Header.Set("Authorization", "Bearer fixture")
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status %d want %d", rec.Code, tc.status)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cache header")
			}
			if strings.Contains(rec.Body.String(), "storage_key") {
				t.Fatal("key exposed")
			}
		})
	}
}
func TestKnowledgeHTTPRejectsOversizeTrailerAndInvalidQueries(t *testing.T) {
	s, repo, _, _ := fixture(t)
	s.limits.MaxFileBytes = 16
	s.limits.MaxRequestBytes = 1024
	router := httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(verifier{}, finder{}))
		NewHandler(s).RegisterRoutes(g)
	})
	for _, trailer := range []bool{false, true} {
		body := "hello"
		if !trailer {
			body = strings.Repeat("x", 17)
		}
		b, ct := multipartBody(t, "x.md", body, false)
		if trailer {
			b = append(b, bytes.Repeat([]byte{'x'}, 2048)...)
		}
		req := httptest.NewRequest("POST", "/api/v1/organizations/20/knowledge/documents", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer fixture")
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 413 || repo.creates != 0 {
			t.Fatal("size limit bypass", rec.Code)
		}
	}
	for _, suffix := range []string{"?page=0", "?page=1&page=2", "?page=9223372036854775807&page_size=100", "?status=unknown", "?actor=10"} {
		req := httptest.NewRequest("GET", "/api/v1/organizations/20/knowledge/documents"+suffix, nil)
		req.Header.Set("Authorization", "Bearer fixture")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatal(fmt.Sprint("query", suffix, rec.Code))
		}
	}
}
