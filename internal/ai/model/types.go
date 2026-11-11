// Package model is the provider-independent streaming contract. Events contain
// complete tool calls only; a provider must never expose argument fragments.
package model

import (
	"context"
	"encoding/json"
)

const MaxInputBytes = 24 * 1024
const MaxOutputBytes = 16 * 1024

type ChatModel interface {
	Model() string
	Stream(context.Context, ChatRequest, func(ModelEvent) error) error
}
type ChatRequest struct {
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function CallFunction `json:"function"`
}
type CallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
type EventKind string

const (
	TextDelta     EventKind = "text_delta"
	ToolCallReady EventKind = "tool_call_ready"
	UsageReady    EventKind = "usage"
	Completed     EventKind = "completed"
)

type ModelEvent struct {
	Kind  EventKind
	Delta string
	Call  ToolCall
	Usage Usage
}
