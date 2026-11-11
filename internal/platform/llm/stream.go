package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/pengfeifei6/NexusDesk/internal/ai/model"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
)

type streamClient struct {
	client   *Client
	name     string
	deepseek bool
}

func (c *Client) StreamModel() model.ChatModel { return &streamClient{client: c, name: rag.ChatModel} }
func NewDeepSeekStream(key, name string) (model.ChatModel, error) {
	g, err := NewDeepSeek(key, name)
	if err != nil {
		return nil, err
	}
	return &streamClient{client: g.(*deepSeekClient).Client, name: name, deepseek: true}, nil
}
func (c *streamClient) Model() string { return c.name }

// DecodeToolArguments applies the same duplicate/Unicode checks as provider
// responses, then rejects unknown fields. JSON Schema alone is not a validator.
func DecodeToolArguments(raw string, target any) error {
	if len(raw) > 16*1024 || strings.TrimSpace(raw) == "null" {
		return rag.NewError("INVALID_REQUEST")
	}
	var object map[string]json.RawMessage
	if err := decodeJSON([]byte(raw), &object); err != nil || object == nil {
		return rag.NewError("INVALID_REQUEST")
	}
	// encoding/json matches struct fields case-insensitively even with
	// DisallowUnknownFields. Check exact schema keys before typed decoding.
	typ := reflect.TypeOf(target)
	if typ == nil || typ.Kind() != reflect.Pointer {
		return rag.NewError("INVALID_REQUEST")
	}
	if typ = typ.Elem(); typ.Kind() == reflect.Struct {
		allowed := make(map[string]bool, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if tag != "" && tag != "-" {
				allowed[tag] = true
			}
		}
		for key := range object {
			if !allowed[key] {
				return rag.NewError("INVALID_REQUEST")
			}
		}
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return rag.NewError("INVALID_REQUEST")
	}
	for _, value := range object {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return rag.NewError("INVALID_REQUEST")
		}
	}
	return nil
}

func (c *streamClient) Stream(parent context.Context, input model.ChatRequest, emit func(model.ModelEvent) error) error {
	if err := parent.Err(); err != nil {
		return err
	}
	if emit == nil || len(input.Messages) == 0 || len(input.Tools) > 3 {
		return rag.NewError("INVALID_REQUEST")
	}
	budget, err := json.Marshal(input)
	if err != nil || len(budget) > model.MaxInputBytes {
		return rag.NewError("INVALID_REQUEST")
	}
	select {
	case c.client.slots <- struct{}{}:
		defer func() { <-c.client.slots }()
	default:
		return rag.NewError("RAG_BUSY")
	}
	ctx, cancel := context.WithTimeout(parent, chatTimeout)
	defer cancel()
	payload := map[string]any{"model": c.name, "messages": input.Messages, "tools": input.Tools, "stream": true}
	path := "/chat/completions"
	if c.deepseek {
		messages := append([]model.Message(nil), input.Messages...)
		for i := range messages {
			messages[i].ToolName = ""
		} // Ollama-only field.
		payload["messages"] = messages
		payload["max_tokens"] = 2048
		payload["thinking"] = map[string]string{"type": "disabled"}
		payload["stream_options"] = map[string]bool{"include_usage": true}
	} else {
		path = "/api/chat"
		payload["think"] = false
		payload["options"] = map[string]int{"num_ctx": 16384, "num_predict": 2048, "temperature": 0}
		// Native Ollama accepts an object for function arguments, unlike DeepSeek.
		messages := make([]map[string]any, 0, len(input.Messages))
		for _, m := range input.Messages {
			v := map[string]any{"role": m.Role, "content": m.Content}
			if m.ToolName != "" {
				v["tool_name"] = m.ToolName
			}
			if len(m.ToolCalls) > 0 {
				calls := []any{}
				for _, call := range m.ToolCalls {
					var args map[string]any
					if err := DecodeToolArguments(call.Function.Arguments, &args); err != nil {
						return err
					}
					calls = append(calls, map[string]any{"function": map[string]any{"name": call.Function.Name, "arguments": args}})
				}
				v["tool_calls"] = calls
			}
			messages = append(messages, v)
		}
		payload["messages"] = messages
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return rag.NewError("INVALID_REQUEST")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return rag.NewError("INVALID_REQUEST")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/x-ndjson")
	if c.client.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.client.apiKey)
	}
	resp, err := c.client.http.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		return requestError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		switch resp.StatusCode {
		case 429:
			return rag.NewError("MODEL_RATE_LIMITED")
		case 408, 504:
			return rag.NewError("MODEL_TIMEOUT")
		default:
			return rag.NewError("MODEL_UNAVAILABLE")
		}
	}
	if c.deepseek && !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return rag.NewError("MODEL_INVALID_RESPONSE")
	}
	err = c.consume(ctx, resp.Body, emit)
	if parent.Err() != nil {
		return parent.Err()
	}
	if ctx.Err() != nil {
		return rag.NewError("MODEL_TIMEOUT")
	}
	return err
}

type streamState struct {
	calls     []model.ToolCall
	usage     model.Usage
	textBytes int
	finish    string
}

func invalidStream() error { return rag.NewError("MODEL_INVALID_RESPONSE") }
func (s *streamState) text(value string, emit func(model.ModelEvent) error) error {
	s.textBytes += len(value)
	if s.textBytes > model.MaxOutputBytes || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return invalidStream()
	}
	if value != "" {
		return emit(model.ModelEvent{Kind: model.TextDelta, Delta: value})
	}
	return nil
}
func validCall(call model.ToolCall) bool {
	if call.Type != "function" || len(call.ID) == 0 || len(call.ID) > 128 || len(call.Function.Name) == 0 || len(call.Function.Name) > 128 || len(call.Function.Arguments) > 16*1024 {
		return false
	}
	for _, v := range []string{call.ID, call.Function.Name} {
		for _, ch := range v {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
				return false
			}
		}
	}
	var args map[string]json.RawMessage
	return decodeJSON([]byte(call.Function.Arguments), &args) == nil && args != nil
}
func (s *streamState) complete(emit func(model.ModelEvent) error) error {
	if s.finish != "stop" && s.finish != "tool_calls" || s.finish == "tool_calls" && len(s.calls) == 0 || s.finish == "stop" && len(s.calls) > 0 {
		return invalidStream()
	}
	seen := map[string]bool{}
	for _, call := range s.calls {
		if !validCall(call) || seen[call.ID] {
			return invalidStream()
		}
		seen[call.ID] = true
	}
	for _, call := range s.calls {
		if err := emit(model.ModelEvent{Kind: model.ToolCallReady, Call: call}); err != nil {
			return err
		}
	}
	if s.usage.InputTokens != 0 || s.usage.OutputTokens != 0 {
		if err := emit(model.ModelEvent{Kind: model.UsageReady, Usage: s.usage}); err != nil {
			return err
		}
	}
	return emit(model.ModelEvent{Kind: model.Completed})
}
func (c *streamClient) consume(ctx context.Context, reader io.Reader, emit func(model.ModelEvent) error) error {
	limited := &io.LimitedReader{R: reader, N: 1024*1024 + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	state := &streamState{}
	var data strings.Builder
	events := 0
	process := func(raw string) (bool, error) {
		events++
		if events > 8192 {
			return false, invalidStream()
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if raw == "[DONE]" {
			return true, state.complete(emit)
		}
		if c.deepseek {
			return false, c.deepseekChunk([]byte(raw), state, emit)
		}
		return c.ollamaChunk([]byte(raw), state, emit)
	}
	for scanner.Scan() {
		if limited.N <= 0 {
			return invalidStream()
		}
		line := scanner.Text()
		if !c.deepseek {
			done, err := process(line)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			continue
		}
		if line == "" {
			if data.Len() == 0 {
				continue
			}
			raw := strings.TrimSuffix(data.String(), "\n")
			data.Reset()
			done, err := process(raw)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			if data.Len()+len(value) > 64*1024 {
				return invalidStream()
			}
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return invalidStream() // EOF, oversize lines and truncated final event all fail closed.
}

func (c *streamClient) deepseekChunk(raw []byte, s *streamState, emit func(model.ModelEvent) error) error {
	var chunk struct {
		Model   string          `json:"model"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Index  *int    `json:"index"`
			Finish *string `json:"finish_reason"`
			Delta  struct {
				Content string `json:"content"`
				Calls   []struct {
					Index    *int               `json:"index"`
					ID       string             `json:"id"`
					Type     string             `json:"type"`
					Function model.CallFunction `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if decodeJSON(raw, &chunk) != nil || chunk.Model != c.name || len(chunk.Error) > 0 || len(chunk.Choices) > 1 {
		return invalidStream()
	}
	if chunk.Usage != nil {
		if chunk.Usage.Input < 0 || chunk.Usage.Output < 0 || chunk.Usage.Input > 1e9 || chunk.Usage.Output > 1e9 {
			return invalidStream()
		}
		s.usage = model.Usage{InputTokens: chunk.Usage.Input, OutputTokens: chunk.Usage.Output}
	}
	if len(chunk.Choices) == 0 {
		if chunk.Usage == nil {
			return invalidStream()
		}
		return nil
	}
	v := chunk.Choices[0]
	if v.Index == nil || *v.Index != 0 {
		return invalidStream()
	}
	if s.finish != "" {
		return invalidStream()
	}
	if err := s.text(v.Delta.Content, emit); err != nil {
		return err
	}
	for _, part := range v.Delta.Calls {
		if part.Index == nil || *part.Index < 0 || *part.Index > 3 || *part.Index > len(s.calls) {
			return invalidStream()
		}
		if *part.Index == len(s.calls) {
			s.calls = append(s.calls, model.ToolCall{})
		}
		call := &s.calls[*part.Index]
		if part.ID != "" {
			if call.ID != "" {
				return invalidStream()
			}
			call.ID = part.ID
		}
		if part.Type != "" {
			if call.Type != "" && call.Type != part.Type {
				return invalidStream()
			}
			call.Type = part.Type
		}
		call.Function.Name += part.Function.Name
		call.Function.Arguments += part.Function.Arguments
		if len(call.Function.Name) > 128 || len(call.Function.Arguments) > 16*1024 {
			return invalidStream()
		}
	}
	if v.Finish != nil {
		s.finish = *v.Finish
		if s.finish != "stop" && s.finish != "tool_calls" {
			return invalidStream()
		}
	}
	return nil
}
func (c *streamClient) ollamaChunk(raw []byte, s *streamState, emit func(model.ModelEvent) error) (bool, error) {
	var chunk struct {
		Model   string          `json:"model"`
		Error   json.RawMessage `json:"error"`
		Done    bool            `json:"done"`
		Reason  string          `json:"done_reason"`
		Input   int64           `json:"prompt_eval_count"`
		Output  int64           `json:"eval_count"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Calls   []struct {
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	}
	if decodeJSON(raw, &chunk) != nil || chunk.Model != c.name || len(chunk.Error) > 0 || chunk.Message.Role != "assistant" {
		return false, invalidStream()
	}
	if err := s.text(chunk.Message.Content, emit); err != nil {
		return false, err
	}
	for _, call := range chunk.Message.Calls {
		s.calls = append(s.calls, model.ToolCall{ID: "call_" + uuid.NewString(), Type: "function", Function: model.CallFunction{Name: call.Function.Name, Arguments: string(call.Function.Arguments)}})
		if len(s.calls) > 4 {
			return false, invalidStream()
		}
	}
	if chunk.Done {
		if chunk.Reason != "stop" || chunk.Input < 0 || chunk.Output < 0 || chunk.Input > 1e9 || chunk.Output > 1e9 {
			return false, invalidStream()
		}
		s.finish = "stop"
		if len(s.calls) > 0 {
			s.finish = "tool_calls"
		}
		s.usage = model.Usage{InputTokens: chunk.Input, OutputTokens: chunk.Output}
		return true, s.complete(emit)
	}
	return false, nil
}
