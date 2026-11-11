package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/platform/llm"
)

func clientFor(t *testing.T, handler http.HandlerFunc) *llm.Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c, err := llm.New(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func vector() []float32 { v := make([]float32, 1024); v[0] = 1; return v }

func embedResponse(w http.ResponseWriter, vectors [][]float32) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"model": "bge-m3:567m", "embeddings": vectors, "total_duration": 1234, "load_duration": 12, "prompt_eval_count": 5})
}

func TestNewOnlyAllowsLiteralLoopbackHTTP(t *testing.T) {
	for _, base := range []string{"", "http://localhost:11434", "http://example.com", "http://169.254.169.254", "http://0.0.0.0:11434", "http://127.0.0.1.example.test", "https://127.0.0.1:11434", "http://user:secret@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1?secret=value", "http://127.0.0.1?", "http://127.0.0.1#secret", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://[::ffff:192.0.2.1]:11434", "http://[::1%25lo]:11434"} {
		t.Run(base, func(t *testing.T) { _, err := llm.New(base); requireCode(t, err, "INVALID_REQUEST") })
	}
	for _, base := range []string{"http://127.0.0.1:11434", "http://127.0.0.2:11434/", "http://[::1]:11434"} {
		if _, err := llm.New(base); err != nil {
			t.Fatalf("valid loopback rejected: %v", err)
		}
	}
}

func TestEmbeddingHTTPContract(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" || r.URL.RawQuery != "" {
			t.Errorf("request: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected headers")
		}
		var body struct {
			Model    string   `json:"model"`
			Input    []string `json:"input"`
			Truncate *bool    `json:"truncate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "bge-m3:567m" || body.Truncate == nil || *body.Truncate || len(body.Input) != 2 || body.Input[0] != "文档甲" || body.Input[1] != "data" {
			t.Errorf("embedding request = %+v", body)
		}
		embedResponse(w, [][]float32{vector(), vector()})
	})
	e := c.Embedder()
	got, err := e.Embed(context.Background(), []string{"文档甲", "data"})
	if err != nil || len(got) != 2 || got[1][0] != 1 || e.Model() != "bge-m3:567m" || e.Dimension() != 1024 {
		t.Fatalf("embed result len=%d, err=%v", len(got), err)
	}
}

func TestEmbeddingInputLimitsNeverSend(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); embedResponse(w, [][]float32{vector()}) })
	for _, input := range [][]string{nil, {}, {""}, {"  "}, {string([]byte{0xff})}, {strings.Repeat("x", 4001)}, {"1", "2", "3", "4", "5", "6", "7", "8", "9"}} {
		_, err := c.Embedder().Embed(context.Background(), input)
		requireCode(t, err, "INVALID_REQUEST")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input reached upstream")
	}
}

func TestEmbeddingMaximumBatchAccepted(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Input) != 8 || len(body.Input[7]) != 4000 {
			t.Error("maximum batch altered")
		}
		vs := make([][]float32, 8)
		for i := range vs {
			vs[i] = vector()
		}
		embedResponse(w, vs)
	})
	texts := make([]string, 8)
	for i := range texts {
		texts[i] = strings.Repeat("x", 4000)
	}
	if _, err := c.Embedder().Embed(context.Background(), texts); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddingNormalizesExtremeFiniteVectors(t *testing.T) {
	for _, value := range []float32{math.MaxFloat32, math.SmallestNonzeroFloat32, 3} {
		c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
			v := make([]float32, 1024)
			v[0], v[1] = value, value
			embedResponse(w, [][]float32{v})
		})
		got, err := c.Embedder().Embed(context.Background(), []string{"sample"})
		if err != nil {
			t.Fatal(err)
		}
		var norm float64
		for _, v := range got[0] {
			norm += float64(v) * float64(v)
		}
		if math.Abs(norm-1) > 1e-6 {
			t.Fatalf("vector norm squared = %g, want 1", norm)
		}
		if got[0][0] <= 0 || got[0][1] <= 0 {
			t.Fatal("normalization lost direction")
		}
	}
}

func TestEmbeddingRejectsUntrustedResponses(t *testing.T) {
	valid, _ := json.Marshal(map[string]any{"model": "bge-m3:567m", "embeddings": [][]float32{vector()}})
	for _, tc := range []struct{ name, body, code string }{
		{"wrong model", strings.Replace(string(valid), "bge-m3:567m", "bge-m3:latest", 1), "MODEL_INVALID_RESPONSE"},
		{"missing model", `{"embeddings":[[1]]}`, "MODEL_INVALID_RESPONSE"},
		{"count", `{"model":"bge-m3:567m","embeddings":[]}`, "EMBEDDING_COUNT_MISMATCH"},
		{"dimension", `{"model":"bge-m3:567m","embeddings":[[1]]}`, "EMBEDDING_DIMENSION_MISMATCH"},
		{"zero", strings.Replace(string(valid), "[1,", "[0,", 1), "EMBEDDING_INVALID_VECTOR"},
		{"nan", `{"model":"bge-m3:567m","embeddings":[[NaN]]}`, "MODEL_INVALID_RESPONSE"},
		{"overflow", `{"model":"bge-m3:567m","embeddings":[[1e1000]]}`, "MODEL_INVALID_RESPONSE"},
		{"null scalar", strings.Replace(string(valid), "[1,", "[null,", 1), "MODEL_INVALID_RESPONSE"},
		{"duplicate model", `{"model":"private","model":"bge-m3:567m","embeddings":[]}`, "MODEL_INVALID_RESPONSE"},
		{"error body", `{"error":"private server content"}`, "MODEL_INVALID_RESPONSE"},
		{"error alongside result", strings.TrimSuffix(string(valid), "}") + `,"error":"private server content"}`, "MODEL_INVALID_RESPONSE"},
		{"trailing json", string(valid) + `{}`, "MODEL_INVALID_RESPONSE"},
		{"oversize", strings.Repeat(" ", 2*1024*1024) + string(valid), "MODEL_INVALID_RESPONSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) })
			_, err := c.Embedder().Embed(context.Background(), []string{"sample"})
			requireCode(t, err, tc.code)
			if strings.Contains(err.Error(), "private") {
				t.Fatal("upstream metadata leaked")
			}
		})
	}
}

func TestHTTPFailuresUseFixedErrorsAndNeverRetry(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, "MODEL_UPSTREAM_ERROR"}, {404, "MODEL_UNAVAILABLE"}, {429, "MODEL_RATE_LIMITED"}, {500, "MODEL_UPSTREAM_ERROR"}, {502, "MODEL_UPSTREAM_ERROR"}, {503, "MODEL_UNAVAILABLE"}, {504, "MODEL_TIMEOUT"}} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			var calls atomic.Int32
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":"private credentials upstream"}`)
			})
			_, err := c.Embedder().Embed(context.Background(), []string{"sample"})
			requireCode(t, err, tc.code)
			if calls.Load() != 1 || strings.Contains(err.Error(), "private") {
				t.Fatal("retry or raw upstream error")
			}
		})
	}
}

func TestConnectionFailureIsSafe(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	base := s.URL
	s.Close()
	c, err := llm.New(base)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Embedder().Embed(context.Background(), []string{"sample"})
	requireCode(t, err, "MODEL_UNAVAILABLE")
	if strings.Contains(err.Error(), base) {
		t.Fatal("transport URL leaked")
	}
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); embedResponse(w, [][]float32{vector()}) }))
	defer target.Close()
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, err := c.Embedder().Embed(context.Background(), []string{"sample"})
	requireCode(t, err, "MODEL_UPSTREAM_ERROR")
	if calls.Load() != 0 {
		t.Fatal("redirect reached second host")
	}
}

func TestProxyEnvironmentCannotCaptureRequests(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer proxy.Close()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { embedResponse(w, [][]float32{vector()}) })
	if _, err := c.Embedder().Embed(context.Background(), []string{"sample"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("request reached environment proxy")
	}
}

func TestModelAdmissionSharedAndCancellationReleasesSlot(t *testing.T) {
	entered := make(chan string, 4)
	release := make(chan struct{})
	defer close(release)
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- r.URL.Path
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := make(chan error, 2)
	go func() { _, err := c.Embedder().Embed(ctx, []string{"sample"}); errors <- err }()
	go func() { _, err := c.Generator().Generate(ctx, "question", sources()); errors <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("request did not start")
		}
	}
	_, err := c.Embedder().Embed(context.Background(), []string{"sample"})
	requireCode(t, err, "RAG_BUSY")
	_, err = c.Generator().Generate(context.Background(), "question", sources())
	requireCode(t, err, "RAG_BUSY")
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-errors:
			requireCode(t, err, "MODEL_TIMEOUT")
		case <-time.After(2 * time.Second):
			t.Fatal("cancellation did not release request")
		}
	}
	deadline, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	_, err = c.Embedder().Embed(deadline, []string{"sample"})
	requireCode(t, err, "MODEL_TIMEOUT")
	select {
	case <-entered:
	default:
		t.Fatal("cancellation did not release admission")
	}
}

func TestAlreadyCanceledContextNeverCallsUpstream(t *testing.T) {
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); embedResponse(w, [][]float32{vector()}) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Embedder().Embed(ctx, []string{"sample"})
	requireCode(t, err, "MODEL_TIMEOUT")
	if calls.Load() != 0 {
		t.Fatal("canceled context reached upstream")
	}
}
