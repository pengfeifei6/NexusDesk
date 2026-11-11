package observe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/platform/config"
)

// Profiling is process-global, so all diagnostic servers share one capture slot.
var captureSlot = make(chan struct{}, 1)

type Diagnostics struct {
	server   *http.Server
	listener net.Listener
	addr     string
	cancel   context.CancelFunc
	once     sync.Once
	closeErr error
}

func (d *Diagnostics) Addr() string { return d.addr }
func (d *Diagnostics) Close() error {
	d.once.Do(func() {
		if d.server == nil {
			return
		}
		d.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		d.closeErr = d.server.Shutdown(ctx)
		_ = d.listener.Close()
		if d.closeErr != nil {
			_ = d.server.Close()
		}
	})
	return d.closeErr
}

// StartDiagnostics starts a private mux on an optional literal loopback address.
// Empty means disabled. The returned lifecycle must be closed before resources.
func StartDiagnostics(ctx context.Context, addr string, r *Registry) (*Diagnostics, error) {
	if err := config.ValidateObservabilityAddress(addr); err != nil {
		return nil, err
	}
	d := &Diagnostics{}
	if addr == "" {
		return d, nil
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, errors.New("listen diagnostics: address unavailable")
	}
	runCtx, cancel := context.WithCancel(ctx)
	d.listener = listener
	d.addr = listener.Addr().String()
	d.cancel = cancel
	slots := make(chan struct{}, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		var profile string
		switch req.URL.Path {
		case "/metrics":
			profile = "metrics"
		case "/debug/pprof/profile":
			profile = "profile"
		case "/debug/pprof/trace":
			profile = "trace"
		case "/debug/pprof/heap":
			profile = "heap"
		case "/debug/pprof/allocs":
			profile = "allocs"
		case "/debug/pprof/goroutine":
			profile = "goroutine"
		default:
			http.NotFound(w, req)
			return
		}
		if req.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		if profile == "profile" || profile == "trace" {
			seconds, err := captureSeconds(req)
			if err != nil {
				http.Error(w, "seconds must be an integer 1..30; no other parameters allowed", 400)
				return
			}
			select {
			case captureSlot <- struct{}{}:
				defer func() { <-captureSlot }()
			default:
				http.Error(w, "capture already running", http.StatusConflict)
				return
			}
			// Use the validated/default duration instead of pprof's different defaults.
			q := req.URL.Query()
			q.Set("seconds", strconv.Itoa(seconds))
			req.URL.RawQuery = q.Encode()
			if profile == "profile" {
				pprof.Profile(w, req)
			} else {
				pprof.Trace(w, req)
			}
			return
		}
		if req.URL.RawQuery != "" {
			http.Error(w, "query parameters not allowed", 400)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "diagnostics busy", http.StatusServiceUnavailable)
			return
		}
		if profile == "metrics" {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			r.WritePrometheus(w)
			return
		}
		pprof.Handler(profile).ServeHTTP(w, req)
	})
	d.server = &http.Server{Handler: handler, BaseContext: func(net.Listener) context.Context { return runCtx }, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = d.server.Serve(listener) }()
	go func() { <-runCtx.Done(); _ = d.Close() }()
	return d, nil
}
func captureSeconds(req *http.Request) (int, error) {
	q, err := parseCaptureQuery(req)
	if err != nil {
		return 0, err
	}
	if q == "" {
		return 5, nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 || n > 30 {
		return 0, errors.New("invalid capture duration")
	}
	for _, c := range q {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid capture duration")
		}
	}
	return n, nil
}
func parseCaptureQuery(req *http.Request) (string, error) {
	q, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return "", err
	}
	if len(q) == 0 {
		return "", nil
	}
	if len(q) != 1 || len(q["seconds"]) != 1 || q.Get("seconds") == "" {
		return "", errors.New("invalid capture query")
	}
	return q.Get("seconds"), nil
}
