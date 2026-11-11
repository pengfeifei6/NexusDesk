package observe

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsDisabledAndRejectsUnsafeAddresses(t *testing.T) {
	d, err := StartDiagnostics(context.Background(), "", NewRegistry())
	if err != nil || d.Addr() != "" {
		t.Fatal(d, err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"localhost:6060", ":6060", "0.0.0.0:6060", "127.0.0.1:-1", "127.0.0.1:65536", "127.0.0.1:+1", " 127.0.0.1:6060"} {
		if _, err := StartDiagnostics(context.Background(), addr, NewRegistry()); err == nil {
			t.Fatalf("accepted %q", addr)
		}
	}
}
func TestDiagnosticsAllowlistBoundsCaptureAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d, err := StartDiagnostics(ctx, "127.0.0.1:0", NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	base := "http://" + d.Addr()
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/metrics", 200}, {"POST", "/metrics", 405}, {"GET", "/debug/pprof/", 404}, {"GET", "/debug/pprof/cmdline", 404}, {"GET", "/debug/pprof/symbol", 404}, {"GET", "/debug/pprof/heap?debug=2", 400}, {"POST", "/debug/pprof/trace", 405}, {"GET", "/debug/pprof/profile?seconds=0", 400}, {"GET", "/debug/pprof/profile?seconds=31", 400}, {"GET", "/debug/pprof/profile?seconds=1.0", 400}, {"GET", "/debug/pprof/profile?seconds=1&seconds=2", 400}, {"GET", "/debug/pprof/trace?private=x", 400}} {
		req, _ := http.NewRequest(tc.method, base+tc.path, nil)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("%s: %d want %d", tc.path, res.StatusCode, tc.status)
		}
	}
	// A live trace owns the same slot as a CPU capture, with no overlap.
	firstDone := make(chan *http.Response, 1)
	firstError := make(chan error, 1)
	go func() {
		response, err := client.Get(base + "/debug/pprof/trace?seconds=1")
		if err != nil {
			firstError <- err
			return
		}
		firstDone <- response
	}()
	deadline := time.Now().Add(time.Second)
	for len(captureSlot) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(captureSlot) == 0 {
		t.Fatal("capture never started")
	}
	second, err := client.Get(base + "/debug/pprof/profile?seconds=1")
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != 409 {
		t.Fatalf("concurrent capture: %d", second.StatusCode)
	}
	var first *http.Response
	select {
	case first = <-firstDone:
	case err := <-firstError:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("capture timed out")
	}
	data, _ := io.ReadAll(first.Body)
	first.Body.Close()
	if !strings.HasPrefix(string(data), "go 1.") {
		t.Fatalf("not a runtime trace: %q", data[:min(32, len(data))])
	}
	cancel()
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	if res, err := client.Get(base + "/metrics"); err == nil {
		res.Body.Close()
		t.Fatal("listener still available after close")
	}
}
func TestCaptureSecondsDefaultAndBounds(t *testing.T) {
	for _, tc := range []struct {
		query   string
		seconds int
		valid   bool
	}{{"", 5, true}, {"seconds=1", 1, true}, {"seconds=30", 30, true}, {"seconds=", 0, false}, {"seconds=%2B1", 0, false}, {"seconds=1&x=2", 0, false}} {
		req, _ := http.NewRequest("GET", "http://localhost/?"+tc.query, nil)
		n, err := captureSeconds(req)
		if (err == nil) != tc.valid || (err == nil && n != tc.seconds) {
			t.Errorf("%s: %d %v", tc.query, n, err)
		}
	}
}

func TestDiagnosticsBindingFailureAndImmediateClose(t *testing.T) {
	first, err := StartDiagnostics(context.Background(), "127.0.0.1:0", NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := StartDiagnostics(context.Background(), first.Addr(), NewRegistry()); err == nil {
		second.Close()
		t.Fatal("duplicate listener accepted")
	}
	for i := 0; i < 20; i++ {
		d, err := StartDiagnostics(context.Background(), "127.0.0.1:0", NewRegistry())
		if err != nil {
			t.Fatal(err)
		}
		if err = d.Close(); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp", d.Addr())
		if err != nil {
			t.Fatal("close did not release listener", err)
		}
		listener.Close()
	}
}
func TestDiagnosticsLimitsConcurrentScrapes(t *testing.T) {
	r := NewRegistry()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	r.SetCacheStats(func() CacheStats { entered <- struct{}{}; <-release; return CacheStats{} })
	d, err := StartDiagnostics(context.Background(), "127.0.0.1:0", r)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	client := &http.Client{Timeout: 3 * time.Second}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			res, err := client.Get("http://" + d.Addr() + "/metrics")
			if err == nil {
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
			}
			done <- err
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("scrape did not enter callback")
		}
	}
	res, err := client.Get("http://" + d.Addr() + "/metrics")
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatal("unlimited scrapes", res.StatusCode)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestShutdownCancelsActiveCapture(t *testing.T) {
	d, err := StartDiagnostics(context.Background(), "127.0.0.1:0", NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	done := make(chan error, 1)
	go func() {
		res, err := (&http.Client{Timeout: 4 * time.Second}).Get("http://" + d.Addr() + "/debug/pprof/trace?seconds=30")
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for len(captureSlot) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(captureSlot) == 0 {
		t.Fatal("capture did not start")
	}
	started := time.Now()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second || len(captureSlot) != 0 {
		t.Fatal("capture was not canceled")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("capture client still blocked")
	}
}
