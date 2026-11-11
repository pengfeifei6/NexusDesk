package llm

import (
	"context"
	"encoding/json"
	"github.com/pengfeifei6/NexusDesk/internal/rag"
	"net"
	"net/http"
	"strings"
	"time"
)

type deepSeekClient struct {
	*Client
	model string
}

// NewDeepSeek sends credentials only to the official HTTPS endpoint. It does
// not contact the provider during startup and never follows redirects/proxies.
func NewDeepSeek(apiKey, model string) (rag.Generator, error) {
	if len(apiKey) == 0 || len(apiKey) > 4096 || strings.IndexFunc(apiKey, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 || (model != "deepseek-flash" && model != "deepseek-v4-pro") {
		return nil, rag.NewError("INVALID_REQUEST")
	}
	transport := &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: chatTimeout,
		DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 2,
		IdleConnTimeout: 90 * time.Second, MaxResponseHeaderBytes: 16 * 1024,
	}
	return &deepSeekClient{Client: &Client{baseURL: "https://api.deepseek.com", apiKey: apiKey,
		http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, slots: make(chan struct{}, 2)}, model: model}, nil
}
func (c *deepSeekClient) Model() string { return c.model }
func (c *deepSeekClient) Generate(ctx context.Context, query string, sources []rag.Source) (rag.Generated, error) {
	messages, allowed, err := buildChatMessages(query, sources, systemInstructions+` Example JSON for insufficient evidence: {"answer":"","citation_ids":[],"insufficient_evidence":true}.`)
	if err != nil {
		return rag.Generated{}, err
	}
	payload := struct {
		Model          string            `json:"model"`
		Messages       []chatMessage     `json:"messages"`
		Stream         bool              `json:"stream"`
		MaxTokens      int               `json:"max_tokens"`
		Thinking       map[string]string `json:"thinking"`
		ResponseFormat map[string]string `json:"response_format"`
	}{Model: c.model, Messages: messages, MaxTokens: 1024, Thinking: map[string]string{"type": "disabled"}, ResponseFormat: map[string]string{"type": "json_object"}}
	body, err := c.post(ctx, "/chat/completions", payload, chatTimeout, chatResponseLimit)
	if err != nil {
		return rag.Generated{}, err
	}
	var response struct {
		Usage   json.RawMessage `json:"usage"`
		Object  string          `json:"object"`
		Model   string          `json:"model"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Index        *int   `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role         string          `json:"role"`
				Content      string          `json:"content"`
				ToolCalls    json.RawMessage `json:"tool_calls"`
				FunctionCall json.RawMessage `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := decodeJSON(body, &response); err != nil {
		return rag.Generated{}, err
	}
	if response.Object != "chat.completion" || response.Model != c.model || len(response.Error) != 0 || len(response.Choices) != 1 {
		return rag.Generated{}, rag.NewError("MODEL_INVALID_RESPONSE")
	}
	choice := response.Choices[0]
	if choice.Index == nil || *choice.Index != 0 || choice.FinishReason != "stop" || choice.Message.Role != "assistant" || !emptyCalls(choice.Message.ToolCalls) || !emptyCalls(choice.Message.FunctionCall) {
		return rag.Generated{}, rag.NewError("MODEL_INVALID_RESPONSE")
	}
	generated, err := parseAnswer(choice.Message.Content, allowed)
	if err != nil {
		return rag.Generated{}, err
	}
	generated.Model = response.Model
	generated.Usage = deepSeekUsage(response.Usage)
	return generated, nil
}
func emptyCalls(value json.RawMessage) bool {
	return len(value) == 0 || string(value) == "null" || string(value) == "[]"
}
