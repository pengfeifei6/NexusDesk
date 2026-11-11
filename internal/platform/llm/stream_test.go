package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pengfeifei6/NexusDesk/internal/ai/model"
)

func streamFixture(t *testing.T, body string) *streamClient {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(s.Close)
	c, _ := New(s.URL)
	return &streamClient{client: c, name: "deepseek-flash", deepseek: true}
}
func streamRequest() model.ChatRequest {
	return model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hi"}}}
}
func delta(content string) string {
	b, _ := json.Marshal(map[string]any{"model": "deepseek-flash", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil}}})
	return "data: " + string(b) + "\n\n"
}

const finish = "data: {\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"

func TestStreamTextAndUsage(t *testing.T) {
	c := streamFixture(t, ": ping\n\n"+delta("你好")+delta("。")+finish)
	var events []model.ModelEvent
	if e := c.Stream(context.Background(), streamRequest(), func(e model.ModelEvent) error { events = append(events, e); return nil }); e != nil {
		t.Fatal(e)
	}
	if len(events) != 4 || events[0].Delta != "你好" || events[2].Usage.InputTokens != 10 || events[3].Kind != model.Completed {
		t.Fatalf("unexpected normalized events: %+v", events)
	}
}
func TestStreamArgumentsOnlyAfterComplete(t *testing.T) {
	b := `data: {"model":"deepseek-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_ticket","arguments":"{\"ticket_"}}]},"finish_reason":null}]}

data: {"model":"deepseek-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"id\":\"1\"}"}}]},"finish_reason":null}]}

data: {"model":"deepseek-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	c := streamFixture(t, b)
	var got []model.ModelEvent
	if e := c.Stream(context.Background(), streamRequest(), func(e model.ModelEvent) error { got = append(got, e); return nil }); e != nil {
		t.Fatal(e)
	}
	if len(got) != 2 || got[0].Call.Function.Arguments != `{"ticket_id":"1"}` || got[0].Kind != model.ToolCallReady {
		t.Fatalf("bad tool event %+v", got)
	}
}
func TestStreamRejectsBrokenResponses(t *testing.T) {
	for name, b := range map[string]string{"eof": delta("partial"), "missing_done": strings.Split(finish, "data: [DONE]")[0], "missing_finish": "data: [DONE]\n\n", "unknown_finish": strings.ReplaceAll(finish, `"stop"`, `"length"`), "invalid_json": "data: {\n\n", "duplicate": `data: {"model":"deepseek-flash","model":"evil"}` + "\n\n", "invalid_unicode": "data: {\"x\":\"\\ud800\"}\n\n", "too_big": delta(strings.Repeat("x", model.MaxOutputBytes+1)) + finish} {
		t.Run(name, func(t *testing.T) {
			var complete bool
			e := streamFixture(t, b).Stream(context.Background(), streamRequest(), func(e model.ModelEvent) error { complete = e.Kind == model.Completed; return nil })
			if e == nil || complete {
				t.Fatal("accepted malformed stream")
			}
		})
	}
}
func TestStreamCancellationAndEmitterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := streamFixture(t, finish).Stream(ctx, streamRequest(), func(model.ModelEvent) error { return nil }); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel = %v", e)
	}
	sentinel := errors.New("writer stopped")
	if e := streamFixture(t, delta("x")+finish).Stream(context.Background(), streamRequest(), func(model.ModelEvent) error { return sentinel }); !errors.Is(e, sentinel) {
		t.Fatalf("writer failure lost: %v", e)
	}
}
func TestStreamTimeoutCancelsUpstream(t *testing.T) {
	done := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(done)
	}))
	defer s.Close()
	c, _ := New(s.URL)
	sc := &streamClient{client: c, name: "deepseek-flash", deepseek: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if e := sc.Stream(ctx, streamRequest(), func(model.ModelEvent) error { return nil }); e == nil {
		t.Fatal("expected timeout")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("upstream not canceled")
	}
}

func TestOllamaStreamToolsAndNativeRequest(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role     string `json:"role"`
				ToolName string `json:"tool_name"`
				Calls    []struct {
					Function struct {
						Arguments map[string]any `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&payload); e != nil {
			t.Error(e)
		}
		if r.URL.Path != "/api/chat" || len(payload.Messages[0].Calls) != 1 || payload.Messages[0].Calls[0].Function.Arguments["ticket_id"] != "1" || payload.Messages[1].ToolName != "get_ticket" {
			t.Error("incorrect native tool messages")
		}
		fmt.Fprintln(w, `{"model":"qwen3.5:35b","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_ticket","arguments":{"ticket_id":"2"}}}]},"done":false}`)
		fmt.Fprintln(w, `{"model":"qwen3.5:35b","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":12,"eval_count":3}`)
	}))
	defer s.Close()
	c, _ := New(s.URL)
	input := model.ChatRequest{Messages: []model.Message{{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "one", Type: "function", Function: model.CallFunction{Name: "get_ticket", Arguments: `{"ticket_id":"1"}`}}}}, {Role: "tool", Content: `{"id":"1"}`, ToolCallID: "one", ToolName: "get_ticket"}}}
	var got []model.ModelEvent
	e := c.StreamModel().Stream(context.Background(), input, func(e model.ModelEvent) error { got = append(got, e); return nil })
	if e != nil || len(got) != 3 || got[0].Call.Function.Name != "get_ticket" || got[1].Usage.InputTokens != 12 || got[2].Kind != model.Completed {
		t.Fatal("native stream failed", e, got)
	}
}
func TestToolArgumentsRejectInvalidJSONAndTypes(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"id":"1","id":"2"}`, `{"id":"1","ID":"2"}`, `{"ID":"1"}`, `{"id":"\ud800"}`, `{"id":null}`, `{"id":1}`, `{"id":"1","org_id":2}`, `{"id":"1"} {}`, strings.Repeat(" ", 16385)} {
		var args struct {
			ID string `json:"id"`
		}
		if DecodeToolArguments(raw, &args) == nil {
			t.Errorf("accepted invalid arguments %q", raw)
		}
	}
}
