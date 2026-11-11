// loadtest is a bounded closed-loop client for owned synthetic W11 fixtures.
// Configuration, including the ephemeral token, is read exclusively from stdin.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type config struct {
	Project        string  `json:"project"`
	Environment    string  `json:"environment"`
	Origin         string  `json:"origin"`
	Path           string  `json:"path"`
	Method         string  `json:"method"`
	Body           string  `json:"body"`
	Token          string  `json:"token"`
	Concurrency    int     `json:"concurrency"`
	Seconds        float64 `json:"seconds"`
	TimeoutSeconds float64 `json:"timeout_seconds"`
	MaxBytes       int64   `json:"max_bytes"`
}
type result struct {
	Requests       int            `json:"requests"`
	Successes      int            `json:"successes"`
	Failures       int            `json:"failures"`
	Connections    int64          `json:"connections"`
	ElapsedSeconds float64        `json:"elapsed_seconds"`
	RPS            float64        `json:"rps"`
	P50            float64        `json:"p50_ms"`
	P95            float64        `json:"p95_ms"`
	P99            float64        `json:"p99_ms"`
	Statuses       map[int]int    `json:"statuses"`
	Errors         map[string]int `json:"errors"`
	Latencies      []float64      `json:"latencies_ms"`
}

func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	v = append([]float64(nil), v...)
	sort.Float64s(v)
	return v[int(math.Ceil(p*float64(len(v))))-1]
}
func validate(c config) error {
	u, e := url.Parse(c.Origin)
	if e != nil {
		return fmt.Errorf("invalid origin")
	}
	ip := net.ParseIP(u.Hostname())
	_, portErr := net.LookupPort("tcp", u.Port())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.Port() == "" || portErr != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("requires literal loopback HTTP origin")
	}
	if c.Environment != "test" || !regexp.MustCompile(`^nexusdesk-week11-[a-f0-9]{12}$`).MatchString(c.Project) {
		return fmt.Errorf("requires owned test fixture")
	}
	if c.Concurrency < 1 || c.Concurrency > 32 || math.IsNaN(c.Seconds) || c.Seconds <= 0 || c.Seconds > 30 || math.IsNaN(c.TimeoutSeconds) || c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 5 || c.MaxBytes < 1 || c.MaxBytes > 1<<20 || len(c.Body) > 1<<16 || len(c.Token) > 8192 || strings.ContainsAny(c.Token, "\r\n") {
		return fmt.Errorf("invalid bounded workload")
	}
	_, pathErr := url.ParseRequestURI(c.Path)
	if pathErr != nil || !strings.HasPrefix(c.Path, "/api/v1/") || strings.ContainsAny(c.Path, "\r\n#") || (c.Method != "GET" && c.Method != "POST") {
		return fmt.Errorf("invalid API workload")
	}
	return nil
}
func run(c config) result {
	var connections atomic.Int64
	var attempted atomic.Int64
	var mu sync.Mutex
	out := result{Statuses: map[int]int{}, Errors: map[string]int{}}
	start := time.Now()
	deadline := start.Add(time.Duration(c.Seconds * float64(time.Second)))
	var wg sync.WaitGroup
	for range c.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transport := &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, DisableCompression: true, IdleConnTimeout: time.Minute, ResponseHeaderTimeout: time.Duration(c.TimeoutSeconds * float64(time.Second))}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Duration(c.TimeoutSeconds * float64(time.Second)), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			local := result{Statuses: map[int]int{}, Errors: map[string]int{}}
			for time.Now().Before(deadline) && attempted.Add(1) <= 1000000 {
				req, _ := http.NewRequest(c.Method, c.Origin+c.Path, bytes.NewBufferString(c.Body))
				if c.Token != "" {
					req.Header.Set("Authorization", "Bearer "+c.Token)
				}
				if c.Method == "POST" {
					req.Header.Set("Content-Type", "application/json")
				}
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(v httptrace.GotConnInfo) {
					if !v.Reused {
						connections.Add(1)
					}
				}}))
				begun := time.Now()
				response, err := client.Do(req)
				category := ""
				status := 0
				if err != nil {
					category = "transport_or_timeout"
				} else {
					status = response.StatusCode
					n, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, c.MaxBytes+1))
					response.Body.Close()
					if readErr != nil {
						category = "body_read"
					} else if n > c.MaxBytes {
						category = "response_too_large"
					} else if status < 200 || status >= 300 {
						category = "non_2xx"
					}
				}
				local.Latencies = append(local.Latencies, float64(time.Since(begun))/float64(time.Millisecond))
				local.Requests++
				if status != 0 {
					local.Statuses[status]++
				}
				if category == "" {
					local.Successes++
				} else {
					local.Failures++
					local.Errors[category]++
				}
			}
			mu.Lock()
			defer mu.Unlock()
			out.Requests += local.Requests
			out.Successes += local.Successes
			out.Failures += local.Failures
			out.Latencies = append(out.Latencies, local.Latencies...)
			for k, v := range local.Statuses {
				out.Statuses[k] += v
			}
			for k, v := range local.Errors {
				out.Errors[k] += v
			}
		}()
	}
	wg.Wait()
	out.Connections = connections.Load()
	out.ElapsedSeconds = time.Since(start).Seconds()
	out.RPS = float64(out.Requests) / out.ElapsedSeconds
	out.P50 = percentile(out.Latencies, .5)
	out.P95 = percentile(out.Latencies, .95)
	out.P99 = percentile(out.Latencies, .99)
	return out
}
func main() {
	var c config
	d := json.NewDecoder(io.LimitReader(os.Stdin, 1<<18))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || validate(c) != nil {
		fmt.Fprintln(os.Stderr, "invalid owned load configuration")
		os.Exit(2)
	}
	if json.NewEncoder(os.Stdout).Encode(run(c)) != nil {
		os.Exit(1)
	}
}
