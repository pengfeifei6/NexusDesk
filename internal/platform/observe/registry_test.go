package observe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"sync"
	"testing"
	"time"
)

func metrics(r *Registry) string { var b bytes.Buffer; r.WritePrometheus(&b); return b.String() }
func TestRegistryBoundsLabelsAndCountsParallelCalls(t *testing.T) {
	r := NewRegistry()
	r.RegisterRoutes([]string{"/api/v1/tickets/:id"})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			r.HTTPStarted()
			r.HTTPCompleted("GET", "/api/v1/tickets/:id", 200, time.Millisecond)
			r.RecordWorker("delivery_return", nil, time.Millisecond)
		})
	}
	wg.Wait()
	for i := 0; i < 1000; i++ {
		r.HTTPStarted()
		r.HTTPCompleted(fmt.Sprint(i), fmt.Sprintf("/secret/%d", i), 999, time.Millisecond)
		r.RecordSQL(fmt.Sprint(i), errors.New("private SQL"), time.Millisecond)
	}
	out := metrics(r)
	for _, want := range []string{`nexusdesk_http_requests_total{method="GET",route="/api/v1/tickets/:id",status="200"} 100`, `nexusdesk_http_requests_total{method="other",route="unmatched",status="0"} 1000`, `nexusdesk_http_in_flight 0`, `nexusdesk_worker_returns_total{phase="delivery_return",result="nil"} 100`, `nexusdesk_sql_queries_total{operation="other",result="error"} 1000`, `nexusdesk_runtime_goroutines `, `nexusdesk_http_request_duration_seconds_bucket`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if strings.Contains(out, "/secret/") || strings.Contains(out, "private") || len(out) > 40000 {
		t.Fatalf("unbounded or private metrics: %s", out)
	}
}
func TestPGXTracerNeverExportsSQLOrArguments(t *testing.T) {
	r := NewRegistry()
	tracer := NewQueryTracer(r)
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 'private-query'", Args: []any{"private-argument"}})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("private-error")})
	out := metrics(r)
	if strings.Contains(out, "private") || !strings.Contains(out, `operation="select",result="error"} 1`) {
		t.Fatal(out)
	}
}
func TestRequestIDGeneratedAndContextScoped(t *testing.T) {
	ctx := WithRequestID(context.Background())
	id := RequestID(ctx)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || RequestID(context.Background()) != "" || RequestID(WithRequestID(context.Background())) == id {
		t.Fatal("request IDs must be random server identities")
	}
}

func TestPrometheusFamiliesRemainGrouped(t *testing.T) {
	r := NewRegistry()
	r.RecordSQL("select", nil, time.Millisecond)
	r.RecordSQL("update", nil, time.Millisecond)
	r.RecordWorker("delivery_return", nil, time.Millisecond)
	r.RecordWorker("dispatch_return", nil, time.Millisecond)
	out := metrics(r)
	for _, prefix := range []string{"sql_queries", "worker_returns"} {
		counter := "nexusdesk_" + prefix + "_total{"
		histogram := "nexusdesk_sql_query_duration_seconds_sum{"
		if prefix == "worker_returns" {
			histogram = "nexusdesk_worker_return_duration_seconds_sum{"
		}
		if strings.LastIndex(out, counter) > strings.Index(out, histogram) {
			t.Errorf("counter family split by histogram family: %s", out)
		}
	}
}

func TestScrapesRaceWithRecordersAndCacheSnapshot(t *testing.T) {
	r := NewRegistry()
	r.RegisterRoutes([]string{"/healthz"})
	r.SetCacheStats(func() CacheStats { return CacheStats{Hits: 7, Misses: 3, Failures: 2, Corrupt: 1, Stores: 4} })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 30; j++ {
				r.HTTPStarted()
				r.HTTPCompleted("GET", "/healthz", 200, time.Millisecond)
				r.RecordSQL("select", nil, time.Millisecond)
				r.RecordWorker("dispatch_return", nil, time.Millisecond)
				metrics(r)
			}
		})
	}
	wg.Wait()
	out := metrics(r)
	for _, want := range []string{`status="200"} 240`, `nexusdesk_cache_hits_total 7`, `nexusdesk_cache_misses_total 3`, `nexusdesk_cache_failures_total 2`, `nexusdesk_cache_corrupt_total 1`, `nexusdesk_cache_stores_total 4`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestHTTPSeriesHasAnExplicitBudget(t *testing.T) {
	r := NewRegistry()
	var routes []string
	for i := 0; i < 200; i++ {
		routes = append(routes, fmt.Sprintf("/route/%d", i))
	}
	r.RegisterRoutes(routes)
	for i := 0; i < 200; i++ {
		for status := 200; status < 210; status++ {
			r.HTTPStarted()
			r.HTTPCompleted("GET", fmt.Sprintf("/route/%d", i), status, time.Millisecond)
		}
	}
	out := metrics(r)
	if n := strings.Count(out, "\nnexusdesk_http_requests_total{"); n > 512 {
		t.Fatalf("HTTP series exceeded fixed budget: %d", n)
	}
	if !strings.Contains(out, `nexusdesk_http_requests_total{method="other",route="unmatched",status="0"}`) {
		t.Fatal("exhausted series did not collapse")
	}
}
