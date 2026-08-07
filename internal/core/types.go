package core

import (
	"context"
	"encoding/json"
)

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Risk        string         `json:"risk,omitempty"`
}

type ProviderRequest struct {
	Model        string
	SystemPrompt string
	Messages     []Message
	Tools        []ToolDefinition
	Settings     map[string]any
}

type ProviderResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        map[string]any
}

type Provider interface {
	Complete(context.Context, ProviderRequest) (ProviderResponse, error)
}

type ToolHandler func(context.Context, map[string]any) (any, error)

type Tool struct {
	Definition ToolDefinition
	TimeoutSec int
	Handler    ToolHandler
}

type ToolExecution struct {
	CallID     string `json:"call_id"`
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Output     any    `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

func (e ToolExecution) JSON() string {
	b, _ := json.Marshal(e)
	return string(b)
}

type AgentResult struct {
	SessionID string         `json:"session_id"`
	Content   string         `json:"content"`
	Steps     int            `json:"steps"`
	Usage     map[string]any `json:"usage,omitempty"`
}
