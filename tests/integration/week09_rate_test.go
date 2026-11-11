package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pengfeifei6/NexusDesk/internal/ai"
	"github.com/pengfeifei6/NexusDesk/internal/ai/model"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
	"github.com/pengfeifei6/NexusDesk/internal/platform/cache"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/platform/httpserver"
	"github.com/pengfeifei6/NexusDesk/internal/platform/ratelimit"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
	"github.com/redis/go-redis/v9"
)

func week09RateClient(t *testing.T) *redis.Client {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W09_RATE") != "1" {
		t.Skip("requires isolated W09 rate harness")
	}
	host, port, err := net.SplitHostPort(os.Getenv("REDIS_ADDR"))
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || port == "6379" || port == "6380" || os.Getenv("REDIS_DB") != "6" || os.Getenv("REDIS_PASSWORD") != "" {
		t.Fatal("requires owned disposable loopback Redis DB6")
	}
	c := cache.NewClient(config.AsyncConfig{RedisAddr: os.Getenv("REDIS_ADDR"), RedisDB: 6}, 100*time.Millisecond)
	t.Cleanup(func() { c.Close() })
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return c
}
func rateKey(org, user int64) string {
	return fmt.Sprintf("nexusdesk:ratelimit:v1:ai:%d:%d", org, user)
}
func TestWeek09RateAtomicConcurrencyIsolationAndExpiry(t *testing.T) {
	c := week09RateClient(t)
	f, _, a := week08Store(t)
	l := ratelimit.New(c, 7, 600*time.Millisecond, 100*time.Millisecond)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, retry, err := l.Check(f.ctx, a.OrgID, a.UserID)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			} else if retry <= 0 || retry > time.Second {
				t.Error("invalid retry duration", retry)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 7 {
		t.Fatal("atomic limit", accepted.Load())
	}
	if n, err := c.Get(f.ctx, rateKey(a.OrgID, a.UserID)).Int(); err != nil || n != 7 {
		t.Fatal("counter must remain bounded", n, err)
	}
	for _, ids := range [][2]int64{{a.OrgID, f.peer}, {f.otherOrg, a.UserID}} {
		if ok, _, err := l.Check(f.ctx, ids[0], ids[1]); !ok || err != nil {
			t.Fatal("scope isolation", err)
		}
	}
	before := c.PTTL(f.ctx, rateKey(a.OrgID, a.UserID)).Val()
	time.Sleep(70 * time.Millisecond)
	if ok, _, err := l.Check(f.ctx, a.OrgID, a.UserID); ok || err != nil {
		t.Fatal("excess request", err)
	}
	if after := c.PTTL(f.ctx, rateKey(a.OrgID, a.UserID)).Val(); after >= before {
		t.Fatal("excess reset TTL")
	}
	time.Sleep(650 * time.Millisecond)
	if ok, _, err := l.Check(f.ctx, a.OrgID, a.UserID); !ok || err != nil {
		t.Fatal("window failed to expire", err)
	}
}
func TestWeek09RateHTTPPreflightPrivacyAndUnavailable(t *testing.T) {
	client := week09RateClient(t)
	f, s, a := week08Store(t)
	conv, err := s.CreateConversation(f.ctx, a, "rate fixture")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	m := week08Model(func(ctx context.Context, r model.ChatRequest, emit func(model.ModelEvent) error) error {
		calls.Add(1)
		if err := emit(model.ModelEvent{Kind: model.TextDelta, Delta: "safe"}); err != nil {
			return err
		}
		return emit(model.ModelEvent{Kind: model.Completed})
	})
	tickets := ticket.NewService(ticket.NewPostgresRepository(f.pool), f.members)
	runner := ai.NewRunner(m, week06Service(f, &week06Embedder{}, &week06Generator{}), tickets, s)
	service := ai.NewChatService(s, runner)
	router := httpserver.NewRouter(func(g *gin.RouterGroup) {
		g.Use(auth.RequireUser(f.tokens, f.users))
		ai.NewHandler(service).WithLimiter(ratelimit.New(client, 1, time.Minute, 100*time.Millisecond)).RegisterRoutes(g)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	send := func(token, path, body string) (int, http.Header, string) {
		t.Helper()
		req, _ := http.NewRequest("POST", server.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, resp.Header, string(b)
	}
	path := w08Path(a, conv.ID)
	for _, tc := range []struct {
		token, path, body string
		status            int
	}{{f.peerToken, path, w08Message("private"), 404}, {f.adminToken, path, w08Message("private"), 404}, {f.foreignToken, path, w08Message("private"), 404}, {f.uploadToken, w08Path(a, uuid.NewString()), w08Message("missing"), 404}, {f.uploadToken, w08Path(a, "bad"), w08Message("bad"), 400}, {f.uploadToken, path, `{"content":"q","client_request_id":"bad"}`, 400}, {f.uploadToken, path, w08Message("  "), 400}, {f.uploadToken, path, w08Message(strings.Repeat("x", 4001)), 400}} {
		status, _, _ := send(tc.token, tc.path, tc.body)
		if status != tc.status {
			t.Fatalf("preflight got %d want %d", status, tc.status)
		}
	}
	if n := client.Exists(f.ctx, rateKey(a.OrgID, a.UserID), rateKey(a.OrgID, f.peer), rateKey(a.OrgID, f.admin)).Val(); n != 0 {
		t.Fatal("invalid/nonowner request consumed quota")
	}
	if status, _, body := send(f.uploadToken, path, w08Message("accepted")); status != 200 || !strings.Contains(body, "event: message_end") {
		t.Fatal("first send", status, body)
	}
	status, headers, body := send(f.uploadToken, path, w08Message("limited"))
	if status != 429 || headers.Get("Retry-After") == "" || strings.Contains(headers.Get("Content-Type"), "text/event-stream") || !strings.Contains(body, "RATE_LIMITED") {
		t.Fatal("quota response", status, headers, body)
	}
	history, err := s.History(f.ctx, a, conv.ID)
	if err != nil || len(history.Messages) != 2 || calls.Load() != 1 {
		t.Fatal("429 mutated turn", len(history.Messages), calls.Load(), err)
	}
	client.Close()
	start := time.Now()
	status, headers, body = send(f.uploadToken, path, w08Message("unavailable"))
	if status != 503 || !strings.Contains(body, "RATE_LIMIT_UNAVAILABLE") || strings.Contains(headers.Get("Content-Type"), "text/event-stream") || time.Since(start) > time.Second {
		t.Fatal("outage did not fail closed", status, body)
	}
	history, err = s.History(f.ctx, a, conv.ID)
	if err != nil || len(history.Messages) != 2 || calls.Load() != 1 {
		t.Fatal("503 mutated turn", len(history.Messages), calls.Load(), err)
	}
}

// Separate Redis clients and HTTP servers must still enforce one shared quota.
func TestWeek09RateConcurrentHTTPAcrossInstances(t *testing.T) {
	c1, c2 := week09RateClient(t), week09RateClient(t)
	f, s, a := week08Store(t)
	conv, err := s.CreateConversation(f.ctx, a, "parallel rate fixture")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	m := week08Model(func(ctx context.Context, r model.ChatRequest, emit func(model.ModelEvent) error) error {
		calls.Add(1)
		if err := emit(model.ModelEvent{Kind: model.TextDelta, Delta: "safe"}); err != nil {
			return err
		}
		return emit(model.ModelEvent{Kind: model.Completed})
	})
	tickets := ticket.NewService(ticket.NewPostgresRepository(f.pool), f.members)
	makeServer := func(c *redis.Client) *httptest.Server {
		runner := ai.NewRunner(m, week06Service(f, &week06Embedder{}, &week06Generator{}), tickets, s)
		router := httpserver.NewRouter(func(g *gin.RouterGroup) {
			g.Use(auth.RequireUser(f.tokens, f.users))
			ai.NewHandler(ai.NewChatService(s, runner)).WithLimiter(ratelimit.New(c, 1, time.Minute, 100*time.Millisecond)).RegisterRoutes(g)
		})
		return httptest.NewServer(router)
	}
	first, second := makeServer(c1), makeServer(c2)
	defer first.Close()
	defer second.Close()
	var allowed, denied atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			server := first
			if i%2 == 1 {
				server = second
			}
			req, _ := http.NewRequest("POST", server.URL+w08Path(a, conv.ID), strings.NewReader(w08Message("concurrent request")))
			req.Header.Set("Authorization", "Bearer "+f.uploadToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Error(err)
				return
			}
			switch resp.StatusCode {
			case 200:
				if !strings.Contains(string(body), "event: message_end") {
					t.Error("accepted request did not finish")
				}
				allowed.Add(1)
			case 429:
				if !strings.Contains(string(body), "RATE_LIMITED") || resp.Header.Get("Retry-After") == "" {
					t.Error("unexpected rejection", string(body))
				}
				denied.Add(1)
			default:
				t.Error("unexpected status", resp.StatusCode, string(body))
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if allowed.Load() != 1 || denied.Load() != 15 || calls.Load() != 1 {
		t.Fatal("shared HTTP quota", allowed.Load(), denied.Load(), calls.Load())
	}
	history, err := s.History(f.ctx, a, conv.ID)
	if err != nil || len(history.Messages) != 2 {
		t.Fatal("concurrent denials wrote turns", len(history.Messages), err)
	}
}
