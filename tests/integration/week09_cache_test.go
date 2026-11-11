package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/ai"
	"github.com/pengfeifei6/NexusDesk/internal/knowledge"
	"github.com/pengfeifei6/NexusDesk/internal/platform/cache"
	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
	"github.com/pengfeifei6/NexusDesk/internal/ticket"
	"github.com/redis/go-redis/v9"
)

func week09Cache(t *testing.T, ttl time.Duration) (*knowledgeFixture, *redis.Client, *cache.Cache, *ticket.Service) {
	t.Helper()
	if os.Getenv("NEXUSDESK_TEST_W09_CACHE") != "1" {
		t.Skip("requires isolated W09 harness (NEXUSDESK_TEST_W09_CACHE=1)")
	}
	f := week06Fixture(t)
	host, port, err := net.SplitHostPort(os.Getenv("REDIS_ADDR"))
	ip := net.ParseIP(host)
	// Preserve W07's loopback/DB/password guard and exclude both known daily ports.
	if err != nil || ip == nil || !ip.IsLoopback() || port == "6379" || port == "6380" || os.Getenv("REDIS_DB") != "6" || os.Getenv("REDIS_PASSWORD") != "" {
		t.Fatal("requires owned isolated loopback Redis DB 6 on a disposable port")
	}
	client := cache.NewClient(config.AsyncConfig{RedisAddr: os.Getenv("REDIS_ADDR"), RedisDB: 6}, 100*time.Millisecond)
	t.Cleanup(func() { client.Close() })
	if err := client.Ping(f.ctx).Err(); err != nil {
		t.Fatal(err)
	}
	c := cache.New(client, f.pool, cache.Options{TTL: ttl, Timeout: 100 * time.Millisecond})
	f.service.WithCache(c)
	return f, client, c, ticket.NewService(ticket.NewPostgresRepository(f.pool), f.members).WithCache(c)
}
func w09Tickets(t *testing.T, f *knowledgeFixture, s *ticket.Service, filter ticket.Filter) []ticket.Ticket {
	t.Helper()
	items, e := s.List(f.ctx, f.uploader, f.org, filter)
	if e != nil {
		t.Fatal(e)
	}
	return items
}
func w09Create(t *testing.T, f *knowledgeFixture, s *ticket.Service, title string, priority int16) ticket.Ticket {
	t.Helper()
	item, e := s.Create(f.ctx, f.uploader, f.org, ticket.CreateInput{Title: title, Body: "synthetic body", Priority: priority})
	if e != nil {
		t.Fatal(e)
	}
	return item
}
func w09Revision(t *testing.T, f *knowledgeFixture, c *cache.Cache, domain string) int64 {
	t.Helper()
	v, e := c.Revision(f.ctx, f.org, domain)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestWeek09CacheListsAuthorizationFiltersAndInvalidation(t *testing.T) {
	f, _, c, s := week09Cache(t, time.Second)
	filter := ticket.Filter{Page: ticket.Page{Number: 1, Size: 1}}
	low := w09Create(t, f, s, "low", 1)
	high := w09Create(t, f, s, "high", 3)
	if got := w09Tickets(t, f, s, filter); len(got) != 1 || got[0].ID != high.ID {
		t.Fatal("initial ticket page")
	}
	before := c.Stats()
	if got := w09Tickets(t, f, s, filter); len(got) != 1 || got[0].ID != high.ID || c.Stats().Hits != before.Hits+1 {
		t.Fatal("ticket cache missed")
	}
	next := filter
	next.Number = 2
	if got := w09Tickets(t, f, s, next); len(got) != 1 || got[0].ID != low.ID {
		t.Fatal("pagination key collision")
	}
	p := int16(1)
	byPriority := filter
	byPriority.Priority = &p
	if got := w09Tickets(t, f, s, byPriority); len(got) != 1 || got[0].ID != low.ID {
		t.Fatal("priority key collision")
	}
	closed := filter
	closed.Status = ticket.Closed
	if got := w09Tickets(t, f, s, closed); got == nil || len(got) != 0 {
		t.Fatal("empty result not canonical")
	}
	before = c.Stats()
	w09Tickets(t, f, s, closed)
	if c.Stats().Hits != before.Hits+1 {
		t.Fatal("empty list not cached")
	}
	foreign, e := s.List(f.ctx, f.foreign, f.otherOrg, filter)
	if e != nil || len(foreign) != 0 {
		t.Fatal("organization isolation", e)
	}
	// Both metadata statuses and page dimensions must be represented in keys.
	d := f.upload(t, f.uploadToken, f.org, "cache.md", []byte("synthetic cache document"))
	df := knowledge.Filter{Page: 1, PageSize: 1}
	docs, e := f.service.List(f.ctx, f.uploader, f.org, df)
	if e != nil || len(docs) != 1 || docs[0].ID != d.ID {
		t.Fatal("document fill", e)
	}
	before = c.Stats()
	docs, e = f.service.List(f.ctx, f.peer, f.org, df)
	if e != nil || len(docs) != 1 || c.Stats().Hits != before.Hits+1 {
		t.Fatal("active members share authorized cache", e)
	}
	df.Status = knowledge.Ready
	docs, e = f.service.List(f.ctx, f.uploader, f.org, df)
	if e != nil || len(docs) != 0 {
		t.Fatal("document status collision", e)
	}
	df.Status = ""
	df.Page = 2
	docs, e = f.service.List(f.ctx, f.uploader, f.org, df)
	if e != nil || len(docs) != 0 {
		t.Fatal("document page collision", e)
	}
	// A worker's independent SQL transaction must also invalidate metadata caches.
	old := w09Revision(t, f, c, "documents")
	f.exec(t, "UPDATE knowledge_documents SET status='READY',extracted_chars=24 WHERE org_id=$1 AND id=$2", f.org, d.ID)
	if w09Revision(t, f, c, "documents") <= old {
		t.Fatal("worker-style SQL missed revision")
	}
	df.Page = 1
	docs, e = f.service.List(f.ctx, f.uploader, f.org, df)
	if e != nil || len(docs) != 1 || docs[0].Status != knowledge.Ready {
		t.Fatal("worker update remained cached", e)
	}
	// Permission checks happen on every request, before reading Redis.
	before = c.Stats()
	f.exec(t, "UPDATE organization_members SET status='disabled' WHERE org_id=$1 AND user_id=$2", f.org, f.uploader)
	if _, e := s.List(f.ctx, f.uploader, f.org, filter); !errors.Is(e, ticket.ErrNotFound) {
		t.Fatal("revoked ticket access", e)
	}
	if _, e := f.service.List(f.ctx, f.uploader, f.org, df); !errors.Is(e, knowledge.ErrNotFound) {
		t.Fatal("revoked document access", e)
	}
	if c.Stats() != before {
		t.Fatal("revoked member accessed cache")
	}
}
func TestWeek09CacheCorruptionTTLAndOutage(t *testing.T) {
	f, client, c, s := week09Cache(t, 100*time.Millisecond)
	filter := ticket.Filter{Page: ticket.Page{Number: 1, Size: 10}}
	item := w09Create(t, f, s, "good", 2)
	w09Tickets(t, f, s, filter)
	key := cache.Key(f.org, "tickets", w09Revision(t, f, c, "tickets"), filter)
	for _, bad := range []string{`{`, `{"org_id":999,"data":[]}`, `{"org_id":` + strconv.FormatInt(f.org, 10) + `,"data":null}`, `{"org_id":` + strconv.FormatInt(f.org, 10) + `,"data":[{"OrgID":999,"Title":"foreign"}]}`, strings.Repeat("x", (1<<20)+1)} {
		if e := client.Set(f.ctx, key, bad, time.Second).Err(); e != nil {
			t.Fatal(e)
		}
		before := c.Stats()
		got := w09Tickets(t, f, s, filter)
		if len(got) != 1 || got[0].ID != item.ID || c.Stats().Corrupt != before.Corrupt+1 {
			t.Fatal("corrupt cache did not fall back")
		}
	}
	ttl, e := client.PTTL(f.ctx, key).Result()
	if e != nil || ttl <= 0 || ttl > 120*time.Millisecond {
		t.Fatal("bounded TTL missing", ttl, e)
	}
	time.Sleep(150 * time.Millisecond)
	before := c.Stats()
	w09Tickets(t, f, s, filter)
	if c.Stats().Misses != before.Misses+1 {
		t.Fatal("expired entry hit")
	}
	// A closed client models an outage without disturbing other tests' Redis.
	client.Close()
	before = c.Stats()
	start := time.Now()
	got := w09Tickets(t, f, s, filter)
	if len(got) != 1 || time.Since(start) > time.Second || c.Stats().Failures <= before.Failures {
		t.Fatal("outage did not fall back promptly")
	}
	// Commit while unavailable, then reconnect: old valid entries cannot revive.
	f.exec(t, "UPDATE tickets SET title='after outage' WHERE org_id=$1 AND id=$2", f.org, item.ID)
	recovery := cache.NewClient(config.AsyncConfig{RedisAddr: os.Getenv("REDIS_ADDR"), RedisDB: 6}, 100*time.Millisecond)
	defer recovery.Close()
	s.WithCache(cache.New(recovery, f.pool, cache.Options{TTL: time.Second}))
	if got = w09Tickets(t, f, s, filter); len(got) != 1 || got[0].Title != "after outage" {
		t.Fatal("recovery revived stale result")
	}
}
func TestWeek09CacheCommitRollbackAndPendingCommit(t *testing.T) {
	f, _, c, s := week09Cache(t, time.Second)
	filter := ticket.Filter{Page: ticket.Page{Number: 1, Size: 10}}
	item := w09Create(t, f, s, "before", 2)
	w09Tickets(t, f, s, filter)
	base := w09Revision(t, f, c, "tickets")
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(f.ctx, "UPDATE tickets SET title='pending' WHERE org_id=$1 AND id=$2", f.org, item.ID); e != nil {
		t.Fatal(e)
	}
	ch := make(chan []ticket.Ticket, 1)
	errs := make(chan error, 1)
	go func() { v, e := s.List(f.ctx, f.uploader, f.org, filter); ch <- v; errs <- e }()
	got := week06Await(t, ch)
	if e := week06Await(t, errs); e != nil || len(got) != 1 || got[0].Title != "before" {
		t.Fatal("uncommitted write leaked or blocked read", e)
	}
	if w09Revision(t, f, c, "tickets") != base {
		t.Fatal("pending revision visible")
	}
	if e = tx.Rollback(f.ctx); e != nil {
		t.Fatal(e)
	}
	if w09Revision(t, f, c, "tickets") != base {
		t.Fatal("rollback advanced revision")
	}
	tx, e = f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	// This uses the exact outer transaction entry point used by AI confirmation.
	if _, e = s.CreateInTransaction(f.ctx, tx, f.uploader, f.org, ticket.CreateInput{Title: "outer rollback", Body: "body", Priority: 2}, ""); e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(f.ctx); e != nil {
		t.Fatal(e)
	}
	if w09Revision(t, f, c, "tickets") != base {
		t.Fatal("outer rollback advanced revision")
	}
	f.exec(t, "UPDATE tickets SET title='committed' WHERE org_id=$1 AND id=$2", f.org, item.ID)
	got = w09Tickets(t, f, s, filter)
	if len(got) != 1 || got[0].Title != "committed" || w09Revision(t, f, c, "tickets") <= base {
		t.Fatal("committed write not visible")
	}
	// Run the actual AI confirmation, not a mock or equivalent ticket write.
	store := ai.NewStore(f.pool, s)
	actor := ai.Actor{UserID: f.uploader, OrgID: f.org}
	_, tr, proposal := w08Proposal(t, store, actor)
	if e = store.FinishTurn(f.ctx, actor, tr.Assistant.ID, "INTERRUPTED", "partial", nil, 0, 0); e != nil {
		t.Fatal(e)
	}
	base = w09Revision(t, f, c, "tickets")
	if _, e = store.Confirm(f.ctx, actor, proposal.ID); e != nil {
		t.Fatal(e)
	}
	got = w09Tickets(t, f, s, filter)
	if len(got) != 2 || w09Revision(t, f, c, "tickets") <= base {
		t.Fatal("AI confirm did not invalidate cache")
	}
}
func TestWeek09CacheDelayedFillCannotPoisonCommittedGeneration(t *testing.T) {
	f, client, c, s := week09Cache(t, time.Second)
	filter := ticket.Filter{Page: ticket.Page{Number: 1, Size: 10}}
	item := w09Create(t, f, s, "snapshot before commit", 2)
	start, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := cache.List(f.ctx, c, f.org, "tickets", filter, func() ([]ticket.Ticket, error) {
			result, e := ticket.NewPostgresRepository(f.pool).ListTickets(f.ctx, f.org, filter)
			close(start)
			<-release
			return result, e
		}, func(v ticket.Ticket) bool { return v.OrgID == f.org })
		done <- e
	}()
	week06Await(t, start)
	f.exec(t, "UPDATE tickets SET title='committed after snapshot' WHERE org_id=$1 AND id=$2", f.org, item.ID)
	close(release)
	if e := week06Await(t, done); e != nil {
		t.Fatal(e)
	}
	key := cache.Key(f.org, "tickets", w09Revision(t, f, c, "tickets"), filter)
	if n, e := client.Exists(f.ctx, key).Result(); e != nil || n != 0 {
		t.Fatal("old fill poisoned current key")
	}
	got := w09Tickets(t, f, s, filter)
	if len(got) != 1 || got[0].Title != "committed after snapshot" {
		t.Fatal("new generation served stale fill")
	}
	// Verify the stored envelope itself keeps a tenant boundary.
	raw, e := client.Get(f.ctx, key).Bytes()
	var value struct {
		OrgID int64 `json:"org_id"`
	}
	if e != nil || json.Unmarshal(raw, &value) != nil || value.OrgID != f.org {
		t.Fatal("cache envelope org boundary")
	}
}
