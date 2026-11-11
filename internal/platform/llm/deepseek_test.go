package llm

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type deepSeekTestTransport struct {
	target *url.URL
	base   http.RoundTripper
	t      *testing.T
}

func (tr deepSeekTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != "https://api.deepseek.com/chat/completions" {
		tr.t.Error("request left fixed official endpoint")
	}
	r = r.Clone(r.Context())
	u := *r.URL
	r.URL = &u
	r.URL.Scheme = tr.target.Scheme
	r.URL.Host = tr.target.Host
	return tr.base.RoundTrip(r)
}
func deepSeekFor(t *testing.T, h http.HandlerFunc) *deepSeekClient {
	t.Helper()
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	g, err := NewDeepSeek("test-only-key", "deepseek-flash")
	if err != nil {
		t.Fatal(err)
	}
	c := g.(*deepSeekClient)
	target, _ := url.Parse(server.URL)
	c.http.Transport = deepSeekTestTransport{target, server.Client().Transport, t}
	return c
}
func cloudSources() []rag.Source {
	return []rag.Source{{ChunkID: 9007199254740993, SourceName: "public.txt", Excerpt: "数据库使用5432端口。"}}
}
func cloudBody(content string) string {
	b, _ := json.Marshal(map[string]any{"object": "chat.completion", "model": "deepseek-flash", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}}})
	return string(b)
}

const cloudAnswer = `{"answer":"5432","citation_ids":["9007199254740993"],"insufficient_evidence":false}`

func cloudCode(t *testing.T, err error, want string) {
	t.Helper()
	var e *rag.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}
func TestDeepSeekHTTPContract(t *testing.T) {
	c := deepSeekFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-only-key" {
			t.Error("wrong HTTP request")
		}
		var b struct {
			Model          string
			Stream         bool
			MaxTokens      int `json:"max_tokens"`
			Thinking       struct{ Type string }
			ResponseFormat struct{ Type string } `json:"response_format"`
			Messages       []chatMessage
			Tools          json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		if b.Model != "deepseek-flash" || b.Stream || b.MaxTokens != 1024 || b.Thinking.Type != "disabled" || b.ResponseFormat.Type != "json_object" || len(b.Tools) != 0 {
			t.Error("wrong JSON contract")
		}
		if len(b.Messages) != 2 || b.Messages[0].Role != "system" || b.Messages[1].Role != "user" || strings.Contains(b.Messages[0].Content, "untrusted-query") {
			t.Error("bad prompt boundary")
		}
		io.WriteString(w, cloudBody(cloudAnswer))
	})
	got, err := c.Generate(context.Background(), "untrusted-query", cloudSources())
	if err != nil || got.Answer != "5432" || len(got.CitationIDs) != 1 || got.CitationIDs[0] != 9007199254740993 {
		t.Fatalf("bad answer %+v %v", got, err)
	}
}
func TestDeepSeekRejectsMalformedAndTruncatedResponses(t *testing.T) {
	good := cloudBody(cloudAnswer)
	for _, body := range []string{cloudBody(" "), cloudBody(strings.Replace(cloudAnswer, "9007199254740993", "999", 1)), strings.Replace(good, `"stop"`, `"length"`, 1), strings.Replace(good, `"assistant"`, `"tool"`, 1), strings.Replace(good, `"role":"assistant"`, `"role":"assistant","tool_calls":[{}]`, 1), strings.Replace(good, `"deepseek-flash"`, `"other"`, 1), strings.Replace(good, `"index":0`, `"index":1`, 1), good + "{}", strings.Repeat(" ", 65537), cloudBody(`{"answer":"a","answer":"b","citation_ids":[],"insufficient_evidence":true}`)} {
		c := deepSeekFor(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, err := c.Generate(context.Background(), "question", cloudSources())
		cloudCode(t, err, "MODEL_INVALID_RESPONSE")
	}
}
func TestDeepSeekErrorsDoNotLeakOrFollowRedirect(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{401, "MODEL_UPSTREAM_ERROR"}, {402, "MODEL_UPSTREAM_ERROR"}, {429, "MODEL_RATE_LIMITED"}, {503, "MODEL_UNAVAILABLE"}, {504, "MODEL_TIMEOUT"}, {302, "MODEL_UPSTREAM_ERROR"}} {
		var calls atomic.Int32
		c := deepSeekFor(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", "https://api.deepseek.com/other")
			w.WriteHeader(tc.status)
			io.WriteString(w, "private upstream test-only-key")
		})
		_, err := c.Generate(context.Background(), "question", cloudSources())
		cloudCode(t, err, tc.code)
		if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "test-only-key") || calls.Load() != 1 {
			t.Fatal("leaked or retried error")
		}
	}
}
func TestDeepSeekInputBudgetAndCancellation(t *testing.T) {
	var calls atomic.Int32
	c := deepSeekFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var b struct{ Messages []chatMessage }
		json.NewDecoder(r.Body).Decode(&b)
		raw, _ := json.Marshal(b.Messages)
		if len(raw) > 12000 {
			t.Error("exceeded budget")
		}
		io.WriteString(w, cloudBody(`{"answer":"ok","citation_ids":["1"],"insufficient_evidence":false}`))
	})
	_, err := c.Generate(context.Background(), "", cloudSources())
	cloudCode(t, err, "INVALID_REQUEST")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Generate(ctx, "question", cloudSources())
	cloudCode(t, err, "MODEL_TIMEOUT")
	if calls.Load() != 0 {
		t.Fatal("invalid or canceled request sent")
	}
	sources := make([]rag.Source, 10)
	for i := range sources {
		sources[i] = rag.Source{ChunkID: int64(i + 1), Excerpt: strings.Repeat("😀", 500)}
	}
	_, err = c.Generate(context.Background(), strings.Repeat("😀", 1000), sources)
	if err != nil {
		t.Fatal(err)
	}
}
func TestDeepSeekConcurrencyAndDeadline(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	c := deepSeekFor(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		io.WriteString(w, cloudBody(cloudAnswer))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, e := c.Generate(ctx, "question", cloudSources()); results <- e }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("no upstream call")
		}
	}
	_, err := c.Generate(context.Background(), "question", cloudSources())
	cloudCode(t, err, "RAG_BUSY")
	for i := 0; i < 2; i++ {
		cloudCode(t, <-results, "MODEL_TIMEOUT")
	}
}
func TestDeepSeekConstructorRestrictsConfiguration(t *testing.T) {
	for _, key := range []string{"", " ", "x\r\nInjected: y", strings.Repeat("x", 4097)} {
		if _, err := NewDeepSeek(key, "deepseek-flash"); err == nil {
			t.Fatal("accepted invalid key")
		}
	}
	if _, err := NewDeepSeek("test-key", "unknown"); err == nil {
		t.Fatal("accepted unknown model")
	}
	g, err := NewDeepSeek("test-key", "deepseek-v4-pro")
	if err != nil {
		t.Fatal(err)
	}
	c := g.(*deepSeekClient)
	if c.http.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("proxy can receive credential")
	}
}
