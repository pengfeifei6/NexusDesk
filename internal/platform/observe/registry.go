package observe

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var buckets = [...]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

type histogram struct {
	Count   uint64
	Sum     float64
	Buckets [len(buckets)]uint64
}

func (h *histogram) add(d time.Duration) {
	seconds := max(0, d.Seconds())
	h.Count++
	h.Sum += seconds
	for i, b := range buckets {
		if seconds <= b {
			h.Buckets[i]++
		}
	}
}

type httpKey struct{ Method, Route, Status string }
type sqlKey struct{ Operation, Result string }
type workerKey struct{ Phase, Result string }

// CacheStats is supplied by an in-memory callback; scraping never queries Redis.
type CacheStats struct{ Hits, Misses, Failures, Corrupt, Stores uint64 }

// Registry owns a fixed vocabulary of labels, at most 128 registered routes,
// and a maximum of 512 HTTP series (including one shared overflow series).
// Register routes before serving requests; unregistered inputs collapse to unmatched.
type Registry struct {
	mu       sync.Mutex
	routes   map[string]bool
	http     map[httpKey]*histogram
	sql      map[sqlKey]*histogram
	worker   map[workerKey]*histogram
	inFlight int64
	pool     *pgxpool.Pool
	cache    func() CacheStats
}

func NewRegistry() *Registry {
	return &Registry{routes: map[string]bool{}, http: map[httpKey]*histogram{}, sql: map[sqlKey]*histogram{}, worker: map[workerKey]*histogram{}}
}
func (r *Registry) RegisterRoutes(routes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, route := range routes {
		if len(r.routes) < 128 && len(route) <= 256 {
			r.routes[route] = true
		}
	}
}
func Method(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		return method
	}
	return "other"
}
func (r *Registry) Route(route string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.routes[route] {
		return route
	}
	return "unmatched"
}
func (r *Registry) HTTPStarted() { r.mu.Lock(); r.inFlight++; r.mu.Unlock() }
func (r *Registry) HTTPCompleted(method, route string, status int, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
	if !r.routes[route] {
		route = "unmatched"
	}
	if status < 100 || status > 599 {
		status = 0
	}
	k := httpKey{Method(method), route, strconv.Itoa(status)}
	// Reserve one shared overflow series, keeping memory bounded independently of
	// combinations of otherwise valid method/route/status labels.
	if r.http[k] == nil && len(r.http) >= 511 {
		k = httpKey{"other", "unmatched", "0"}
	}
	if r.http[k] == nil {
		r.http[k] = &histogram{}
	}
	r.http[k].add(d)
}
func sqlOperation(operation string) string {
	switch operation {
	case "select", "insert", "update", "delete", "begin", "commit", "rollback":
		return operation
	}
	return "other"
}
func result(err error) string {
	if err != nil {
		return "error"
	}
	return "nil"
}
func (r *Registry) RecordSQL(operation string, err error, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := sqlKey{sqlOperation(operation), result(err)}
	if r.sql[k] == nil {
		r.sql[k] = &histogram{}
	}
	r.sql[k].add(d)
}

// RecordWorker reports function return only, never the durable job business state.
func (r *Registry) RecordWorker(phase string, err error, d time.Duration) {
	switch phase {
	case "delivery_return", "dispatch_return":
	default:
		phase = "other"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := workerKey{phase, result(err)}
	if r.worker[k] == nil {
		r.worker[k] = &histogram{}
	}
	r.worker[k].add(d)
}
func (r *Registry) SetPool(pool *pgxpool.Pool)           { r.mu.Lock(); r.pool = pool; r.mu.Unlock() }
func (r *Registry) SetCacheStats(read func() CacheStats) { r.mu.Lock(); r.cache = read; r.mu.Unlock() }

func writeHistogram(w io.Writer, name, labels string, h histogram) {
	fmt.Fprintf(w, "%s_sum{%s} %g\n%s_count{%s} %d\n", name, labels, h.Sum, name, labels, h.Count)
	for i, b := range buckets {
		fmt.Fprintf(w, "%s_bucket{%s,le=%q} %d\n", name, labels, strconv.FormatFloat(b, 'g', -1, 64), h.Buckets[i])
	}
	fmt.Fprintf(w, "%s_bucket{%s,le=\"+Inf\"} %d\n", name, labels, h.Count)
}
func family(w io.Writer, name, kind, help string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

// WritePrometheus writes Prometheus text format 0.0.4 from local snapshots.
func (r *Registry) WritePrometheus(w io.Writer) {
	r.mu.Lock()
	httpRows := make(map[httpKey]histogram, len(r.http))
	for k, v := range r.http {
		httpRows[k] = *v
	}
	sqlRows := make(map[sqlKey]histogram, len(r.sql))
	for k, v := range r.sql {
		sqlRows[k] = *v
	}
	workerRows := make(map[workerKey]histogram, len(r.worker))
	for k, v := range r.worker {
		workerRows[k] = *v
	}
	inFlight, pool, cache := r.inFlight, r.pool, r.cache
	r.mu.Unlock()
	// Sort request counters for repeatable output while holding no registry lock.
	var lines []string
	family(w, "nexusdesk_http_requests_total", "counter", "Completed HTTP requests.")
	for k, h := range httpRows {
		lines = append(lines, fmt.Sprintf("nexusdesk_http_requests_total{method=%q,route=%q,status=%q} %d\n", k.Method, k.Route, k.Status, h.Count))
	}
	sort.Strings(lines)
	for _, line := range lines {
		io.WriteString(w, line)
	}
	family(w, "nexusdesk_http_request_duration_seconds", "histogram", "HTTP request duration.")
	for k, h := range httpRows {
		writeHistogram(w, "nexusdesk_http_request_duration_seconds", fmt.Sprintf("method=%q,route=%q,status=%q", k.Method, k.Route, k.Status), h)
	}
	family(w, "nexusdesk_http_in_flight", "gauge", "HTTP requests currently running.")
	fmt.Fprintf(w, "nexusdesk_http_in_flight %d\n", inFlight)
	family(w, "nexusdesk_sql_queries_total", "counter", "SQL query returns by fixed operation.")
	for k, h := range sqlRows {
		fmt.Fprintf(w, "nexusdesk_sql_queries_total{operation=%q,result=%q} %d\n", k.Operation, k.Result, h.Count)
	}
	family(w, "nexusdesk_sql_query_duration_seconds", "histogram", "SQL query duration.")
	for k, h := range sqlRows {
		writeHistogram(w, "nexusdesk_sql_query_duration_seconds", fmt.Sprintf("operation=%q,result=%q", k.Operation, k.Result), h)
	}
	family(w, "nexusdesk_worker_returns_total", "counter", "Worker function returns, not business outcomes.")
	for k, h := range workerRows {
		fmt.Fprintf(w, "nexusdesk_worker_returns_total{phase=%q,result=%q} %d\n", k.Phase, k.Result, h.Count)
	}
	family(w, "nexusdesk_worker_return_duration_seconds", "histogram", "Worker function duration.")
	for k, h := range workerRows {
		writeHistogram(w, "nexusdesk_worker_return_duration_seconds", fmt.Sprintf("phase=%q,result=%q", k.Phase, k.Result), h)
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	type gauge struct {
		name  string
		value float64
	}
	gauges := []gauge{{"runtime_goroutines", float64(runtime.NumGoroutine())}, {"runtime_heap_alloc_bytes", float64(mem.HeapAlloc)}, {"runtime_heap_inuse_bytes", float64(mem.HeapInuse)}, {"runtime_sys_bytes", float64(mem.Sys)}}
	if pool != nil {
		s := pool.Stat()
		gauges = append(gauges, gauge{"pg_pool_total_connections", float64(s.TotalConns())}, gauge{"pg_pool_idle_connections", float64(s.IdleConns())}, gauge{"pg_pool_acquired_connections", float64(s.AcquiredConns())}, gauge{"pg_pool_max_connections", float64(s.MaxConns())})
		writeCounter(w, "pg_pool_acquires_total", float64(s.AcquireCount()))
		writeCounter(w, "pg_pool_acquire_duration_seconds_total", s.AcquireDuration().Seconds())
		writeCounter(w, "pg_pool_canceled_acquires_total", float64(s.CanceledAcquireCount()))
		writeCounter(w, "pg_pool_empty_acquires_total", float64(s.EmptyAcquireCount()))
	}
	for _, g := range gauges {
		family(w, "nexusdesk_"+g.name, "gauge", "Process local snapshot.")
		fmt.Fprintf(w, "nexusdesk_%s %g\n", g.name, g.value)
	}
	writeCounter(w, "runtime_gc_cycles_total", float64(mem.NumGC))
	writeCounter(w, "runtime_gc_pause_seconds_total", float64(mem.PauseTotalNs)/1e9)
	writeCounter(w, "runtime_alloc_bytes_total", float64(mem.TotalAlloc))
	if cache != nil {
		s := cache()
		for _, v := range []struct {
			name  string
			value uint64
		}{{"hits", s.Hits}, {"misses", s.Misses}, {"failures", s.Failures}, {"corrupt", s.Corrupt}, {"stores", s.Stores}} {
			writeCounter(w, "cache_"+v.name+"_total", float64(v.value))
		}
	}
}
func writeCounter(w io.Writer, name string, value float64) {
	family(w, "nexusdesk_"+name, "counter", "Cumulative process local value.")
	fmt.Fprintf(w, "nexusdesk_%s %g\n", name, value)
}
