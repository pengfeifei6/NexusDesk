package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNearestRank(t *testing.T) {
	for _, tc := range []struct {
		p    float64
		want float64
	}{{.5, 2}, {.95, 4}, {.99, 4}, {1, 4}} {
		if got := percentile([]float64{4, 1, 3, 2}, tc.p); got != tc.want {
			t.Fatalf("p=%v got=%v", tc.p, got)
		}
	}
	if percentile(nil, .5) != 0 {
		t.Fatal("empty")
	}
}
func validConfig() config {
	return config{Project: "nexusdesk-week11-123456abcdef", Environment: "test", Origin: "http://127.0.0.1:1234", Path: "/api/v1/test", Method: "GET", Concurrency: 1, Seconds: .1, TimeoutSeconds: .1, MaxBytes: 128}
}
func TestGuards(t *testing.T) {
	for _, change := range []func(*config){func(c *config) { c.Origin = "http://example.com" }, func(c *config) { c.Origin = "http://localhost:1234" }, func(c *config) { c.Origin = "http://127.0.0.1:1234/path" }, func(c *config) { c.Environment = "production" }, func(c *config) { c.Project = "daily" }, func(c *config) { c.Concurrency = 0 }, func(c *config) { c.Seconds = 31 }, func(c *config) { c.MaxBytes = 1 << 22 }, func(c *config) { c.Path = "//example.com" },
		func(c *config) { c.Path = "/api/v1/%zz" }, func(c *config) { c.Token = "secret\nvalue" }} {
		c := validConfig()
		change(&c)
		if validate(c) == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}
func TestKeepaliveAndAccounting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("missing token")
		}
		w.Write([]byte("{}"))
	}))
	defer server.Close()
	c := validConfig()
	c.Origin = server.URL
	c.Token = "synthetic"
	c.Concurrency = 2
	got := run(c)
	if got.Successes < 2 || got.Failures != 0 || got.Requests != got.Successes || got.Connections > 2 || got.ElapsedSeconds < .09 || got.RPS <= 0 {
		t.Fatalf("%+v", got)
	}
}
func TestBoundedResponsesAndTimeouts(t *testing.T) {
	for _, slow := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slow {
				time.Sleep(40 * time.Millisecond)
			}
			w.Write([]byte(strings.Repeat("x", 256)))
		}))
		c := validConfig()
		c.Origin = server.URL
		c.Seconds = .06
		c.TimeoutSeconds = .01
		got := run(c)
		server.Close()
		if got.Failures == 0 || got.Successes != 0 || got.Requests != got.Failures {
			t.Fatalf("%+v", got)
		}
	}
}
