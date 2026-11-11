package httpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/ai"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/jobs"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/organization"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObservationGeneratesIDAndErrorCorrelation(t *testing.T) {
	var logs bytes.Buffer
	registry := observe.NewRegistry()
	router := NewRouterWithObserver(registry, slog.New(slog.NewJSONHandler(&logs, nil)), auth.NewHandler(nil).RegisterRoutes)
	req := httptest.NewRequest("POST", "/api/v1/auth/register?private-query=secret", strings.NewReader("bad-json"))
	req.Header.Set("X-Request-ID", "attacker")
	req.Header.Set("Authorization", "private-token")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	var body struct {
		Error struct {
			RequestID string `json:"request_id"`
		}
	}
	json.Unmarshal(response.Body.Bytes(), &body)
	id := response.Header().Get("X-Request-ID")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || body.Error.RequestID != id {
		t.Fatalf("uncorrelated response: %s %s", id, response.Body.String())
	}
	if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "attacker") || !strings.Contains(logs.String(), id) {
		t.Fatal(logs.String())
	}
}
func TestSafePanicDoesNotExposePayloadOrAppendAfterFlush(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouterWithObserver(observe.NewRegistry(), slog.New(slog.NewJSONHandler(&logs, nil)), func(g *gin.RouterGroup) {
		g.GET("/panic", func(c *gin.Context) { panic("private-panic") })
		g.GET("/stream", func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.Writer.WriteString("data: safe\n\n")
			c.Writer.Flush()
			panic("private-panic")
		})
	})
	for _, path := range []string{"panic", "stream"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/"+path, nil))
		if path == "panic" && (response.Code != 500 || !strings.Contains(response.Body.String(), response.Header().Get("X-Request-ID"))) {
			t.Fatal(response.Body.String())
		}
		if path == "stream" && response.Body.String() != "data: safe\n\n" {
			t.Fatal(response.Body.String())
		}
	}
	if strings.Contains(logs.String(), "private-panic") {
		t.Fatal(logs.String())
	}
}

func TestAllDomainErrorEnvelopesUseServerIdentity(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouterWithObserver(observe.NewRegistry(), slog.New(slog.NewJSONHandler(&logs, nil)), auth.NewHandler(nil).RegisterRoutes, organization.NewHandler(nil).RegisterRoutes, ticket.NewHandler(nil).RegisterRoutes, knowledge.NewHandler(nil).RegisterRoutes, rag.NewHandler(nil).RegisterRoutes, jobs.NewHandler(nil).RegisterRoutes, ai.NewHandler(nil).RegisterRoutes)
	for _, path := range []string{"/auth/register", "/organizations", "/organizations/1/tickets", "/organizations/1/knowledge/documents", "/organizations/1/knowledge/ask", "/organizations/1/knowledge/documents/1/jobs", "/organizations/1/ai/conversations"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1"+path, strings.NewReader("invalid")))
		var body struct {
			Error struct {
				RequestID string `json:"request_id"`
			}
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(path, err)
		}
		id := response.Header().Get("X-Request-ID")
		if response.Code < 400 || len(id) != 32 || body.Error.RequestID != id {
			t.Fatalf("%s: uncorrelated error %s", path, response.Body.String())
		}
	}
}
func TestUnknownHTTPInputsCollapseWithoutLeakingPath(t *testing.T) {
	var logs, metrics bytes.Buffer
	r := observe.NewRegistry()
	router := NewRouterWithObserver(r, slog.New(slog.NewJSONHandler(&logs, nil)))
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("PRIVATE-METHOD", fmt.Sprintf("/private-path/%d?secret=value", i), nil)
		router.ServeHTTP(httptest.NewRecorder(), req)
	}
	r.WritePrometheus(&metrics)
	if !strings.Contains(metrics.String(), `nexusdesk_http_requests_total{method="other",route="unmatched",status="404"} 50`) || strings.Contains(metrics.String(), "private-path") || strings.Contains(logs.String(), "private-path") || strings.Contains(logs.String(), "PRIVATE-METHOD") || strings.Contains(logs.String(), "secret") {
		t.Fatal(logs.String(), metrics.String())
	}
}

func TestTrailingSlashRedirectsHaveExactlyOneObservation(t *testing.T) {
	for _, tc := range []struct {
		method, path, location, route string
		status                        int
	}{
		{"GET", "/healthz/?private-query=secret", "/healthz?private-query=secret", "/healthz", 301},
		{"POST", "/api/v1/organizations/123/tickets/?private-query=secret", "/api/v1/organizations/123/tickets?private-query=secret", "/api/v1/organizations/:orgID/tickets", 307},
		{"GET", "/api/v1/slashed", "/api/v1/slashed/", "/api/v1/slashed/", 301},
		{"GET", "/api/v1/files", "/api/v1/files/", "/api/v1/files/*path", 301},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			var logs, metrics bytes.Buffer
			r := observe.NewRegistry()
			calls := 0
			router := NewRouterWithObserver(r, slog.New(slog.NewJSONHandler(&logs, nil)), func(g *gin.RouterGroup) {
				business := func(c *gin.Context) { calls++; c.Status(204) }
				g.POST("/organizations/:orgID/tickets", business)
				g.GET("/slashed/", business)
				g.GET("/files/*path", business)
			})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Request-ID", "attacker")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status || response.Header().Get("Location") != tc.location || calls != 0 {
				t.Fatalf("redirect changed: status=%d location=%q business calls=%d", response.Code, response.Header().Get("Location"), calls)
			}
			id := response.Header().Get("X-Request-ID")
			if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
				t.Fatalf("redirect has no generated ID: %q", id)
			}
			var entry struct {
				Message   string `json:"msg"`
				RequestID string `json:"request_id"`
				Route     string `json:"route"`
				Status    int    `json:"status"`
			}
			if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
				t.Fatalf("expected exactly one JSON completion event: %s", logs.String())
			}
			if entry.Message != "HTTP request completed" || entry.RequestID != id || entry.Route != tc.route || entry.Status != tc.status || strings.Contains(logs.String(), "private-query") || strings.Contains(logs.String(), "attacker") {
				t.Fatal(logs.String())
			}
			r.WritePrometheus(&metrics)
			want := fmt.Sprintf("nexusdesk_http_requests_total{method=%q,route=%q,status=%q} 1", tc.method, tc.route, fmt.Sprint(tc.status))
			if !strings.Contains(metrics.String(), want) || strings.Count(metrics.String(), "\nnexusdesk_http_requests_total{") != 1 || !strings.Contains(metrics.String(), "nexusdesk_http_in_flight 0\n") {
				t.Fatal(metrics.String())
			}
		})
	}
}
func TestSlashPairsAndConnectKeepTheirExistingHandlers(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouterWithObserver(observe.NewRegistry(), slog.New(slog.NewJSONHandler(&logs, nil)), func(g *gin.RouterGroup) {
		g.GET("/pair", func(c *gin.Context) { c.Status(201) })
		g.GET("/pair/", func(c *gin.Context) { c.Status(202) })
		g.Handle("CONNECT", "/probe", func(c *gin.Context) { c.Status(204) })
	})
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/v1/pair", 201}, {"GET", "/api/v1/pair/", 202}, {"CONNECT", "/api/v1/probe/", 404}, {"GET", "/healthz//", 404}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status || response.Header().Get("Location") != "" {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, response.Code, response.Header().Get("Location"))
		}
	}
}

func TestStaticSlashRedirectKeepsGinPrecedenceOverParameterRoute(t *testing.T) {
	var logs, metrics bytes.Buffer
	r := observe.NewRegistry()
	router := NewRouterWithObserver(r, slog.New(slog.NewJSONHandler(&logs, nil)), func(g *gin.RouterGroup) {
		g.GET("/items/:id", func(c *gin.Context) { c.String(201, c.Param("id")) })
		g.GET("/items/new/", func(c *gin.Context) { c.Status(202) })
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/items/new", nil))
	if response.Code != 301 || response.Header().Get("Location") != "/api/v1/items/new/" {
		t.Fatalf("Gin static redirect precedence changed: %d %s", response.Code, response.Body.String())
	}
	r.WritePrometheus(&metrics)
	if !strings.Contains(metrics.String(), `nexusdesk_http_requests_total{method="GET",route="/api/v1/items/new/",status="301"} 1`) {
		t.Fatal(metrics.String())
	}
}
func TestRedirectAliasLabelsAreSpecificToMethod(t *testing.T) {
	var logs, metrics bytes.Buffer
	r := observe.NewRegistry()
	router := NewRouterWithObserver(r, slog.New(slog.NewJSONHandler(&logs, nil)), func(g *gin.RouterGroup) {
		g.GET("/files/*path", func(c *gin.Context) { c.Status(201) })
		g.POST("/files/", func(c *gin.Context) { c.Status(202) })
	})
	for _, method := range []string{"GET", "POST"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/files", nil))
		want := 301
		if method == "POST" {
			want = 307
		}
		if response.Code != want {
			t.Fatal(method, response.Code)
		}
	}
	r.WritePrometheus(&metrics)
	for _, want := range []string{`method="GET",route="/api/v1/files/*path",status="301"`, `method="POST",route="/api/v1/files/",status="307"`} {
		if !strings.Contains(metrics.String(), want) {
			t.Fatal(metrics.String())
		}
	}
}

func TestSlashRedirectLocationsRemainCompatibleWithSelectedGin(t *testing.T) {
	for _, tc := range []struct{ requestPath, prefix string }{{"/api/v1/target/?q=a%20b", ""}, {"/api/v1/%74arget/?q=a%20b", ""}, {"/api/v1/target/", "/edge//path/../safe!"}, {"/api/v1/target/", "/"}} {
		t.Run(tc.requestPath+tc.prefix, func(t *testing.T) {
			baseline := gin.New()
			baseline.GET("/api/v1/target", func(c *gin.Context) { c.Status(204) })
			var logs bytes.Buffer
			router := NewRouterWithObserver(observe.NewRegistry(), slog.New(slog.NewJSONHandler(&logs, nil)), func(g *gin.RouterGroup) { g.GET("/target", func(c *gin.Context) { c.Status(204) }) })
			original := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tc.requestPath, nil)
			req.Header.Set("X-Forwarded-Prefix", tc.prefix)
			baseline.ServeHTTP(original, req)
			observed := httptest.NewRecorder()
			req = httptest.NewRequest("GET", tc.requestPath, nil)
			req.Header.Set("X-Forwarded-Prefix", tc.prefix)
			router.ServeHTTP(observed, req)
			if original.Code != observed.Code || original.Header().Get("Location") != observed.Header().Get("Location") || original.Body.String() != observed.Body.String() {
				t.Fatalf("redirect compatibility: baseline=%d %q observed=%d %q", original.Code, original.Header().Get("Location"), observed.Code, observed.Header().Get("Location"))
			}
		})
	}
}
