package cache

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
)

// A dropped key dimension could return another tenant/filter/page's list.
func TestKeySeparatesTenantDomainRevisionAndFilters(t *testing.T) {
	base := Key(7, "tickets", 2, struct {
		Page     int
		Status   string
		Priority int
	}{1, "OPEN", 2})
	if !strings.HasPrefix(base, "nexusdesk:cache:v1:7:tickets:2:active-members-all:created_at-desc-id-desc:") {
		t.Fatal(base)
	}
	for _, key := range []string{
		Key(8, "tickets", 2, struct {
			Page     int
			Status   string
			Priority int
		}{1, "OPEN", 2}),
		Key(7, "documents", 2, struct {
			Page     int
			Status   string
			Priority int
		}{1, "OPEN", 2}),
		Key(7, "tickets", 3, struct {
			Page     int
			Status   string
			Priority int
		}{1, "OPEN", 2}),
		Key(7, "tickets", 2, struct {
			Page     int
			Status   string
			Priority int
		}{2, "OPEN", 2}),
		Key(7, "tickets", 2, struct {
			Page     int
			Status   string
			Priority int
		}{1, "CLOSED", 2}),
		Key(7, "tickets", 2, struct {
			Page     int
			Status   string
			Priority int
		}{1, "OPEN", 3}),
	} {
		if key == base {
			t.Fatal("key dimension omitted")
		}
	}
}
func TestTTLHasBoundedPositiveJitter(t *testing.T) {
	c := New(nil, nil, Options{TTL: time.Second})
	seen := map[time.Duration]bool{}
	for range 100 {
		v := c.ttl()
		if v < time.Second || v > 1200*time.Millisecond {
			t.Fatal(v)
		}
		seen[v] = true
	}
	if len(seen) < 2 {
		t.Fatal("expiry jitter missing")
	}
}

// A silent server must not hold an API request past its cache budget.
func TestClientSilentServerIsBoundedAndDoesNotRetry(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer conn.Close(); <-done }()
		}
	}()
	c := NewClient(config.AsyncConfig{RedisAddr: l.Addr().String()}, 30*time.Millisecond)
	defer c.Close()
	start := time.Now()
	e = c.Get(context.Background(), "synthetic").Err()
	if e == nil || time.Since(start) > 300*time.Millisecond {
		t.Fatalf("unbounded Redis: %v elapsed=%v", e, time.Since(start))
	}
	if c.Options().MaxRetries != 0 || !c.Options().ContextTimeoutEnabled {
		t.Fatal("timeouts/retries incorrectly configured")
	}
}
