package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Kyrie-w8/aster-edge/internal/config"
	"github.com/Kyrie-w8/aster-edge/internal/core"
	"github.com/Kyrie-w8/aster-edge/internal/policy"
	"github.com/Kyrie-w8/aster-edge/internal/session"
	"github.com/Kyrie-w8/aster-edge/internal/tools"
)

type sequenceProvider struct{ calls int }

func (p *sequenceProvider) Complete(_ context.Context, req core.ProviderRequest) (core.ProviderResponse, error) {
	p.calls++
	if p.calls == 1 {
		return core.ProviderResponse{ToolCalls: []core.ToolCall{{Name: "echo", Arguments: map[string]any{"text": "ok"}}}}, nil
	}
	if len(req.Messages) < 3 {
		return core.ProviderResponse{}, nil
	}
	return core.ProviderResponse{Content: "done", Usage: map[string]any{"tokens": 3}}, nil
}

func TestAgentToolLoopPersistsCanonicalMessages(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.MaxSteps = 3
	store, _ := session.New(t.TempDir())
	registry := tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"echo"}}), nil, 1024)
	err := registry.Add(core.Tool{Definition: core.ToolDefinition{Name: "echo", Risk: "read", Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false}}, Handler: func(_ context.Context, args map[string]any) (any, error) { return args["text"], nil }})
	if err != nil {
		t.Fatal(err)
	}
	provider := &sequenceProvider{}
	engine := Engine{Config: cfg, Provider: provider, Tools: registry, Store: store, SystemPrompt: "test"}
	result, err := engine.Run(context.Background(), "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || result.Steps != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	messages, err := store.Messages(result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[2].Role != "tool" {
		t.Fatalf("unexpected trajectory: %+v", messages)
	}
	if messages[1].ToolCalls[0].ID == "" || messages[1].ToolCalls[0].ID != messages[2].ToolCallID {
		t.Fatalf("generated call id was not preserved: %+v", messages)
	}
}

func TestAgentEmitsOrderedLifecycleEvents(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agent.MaxSteps = 1
	store, _ := session.New(t.TempDir())
	registry := tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"*"}}), nil, 1024)
	engine := Engine{Config: cfg, Provider: &staticProvider{response: core.ProviderResponse{Content: "hello"}}, Tools: registry, Store: store}
	var events []core.AgentEvent
	result, err := engine.RunWithEvents(context.Background(), "", "hi", func(event core.AgentEvent) {
		events = append(events, event)
	})
	if err != nil || result.Content != "hello" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := []core.AgentEventType{core.EventTurnStarted, core.EventProviderStarted, core.EventTextDelta, core.EventTurnCompleted}
	if len(events) != len(want) {
		t.Fatalf("events=%+v", events)
	}
	for i, event := range events {
		if event.Type != want[i] || event.Sequence != uint64(i+1) || event.SessionID != result.SessionID || event.TurnID == "" {
			t.Fatalf("event[%d]=%+v", i, event)
		}
	}
}

type staticProvider struct {
	response    core.ProviderResponse
	lastRequest core.ProviderRequest
}

func (p *staticProvider) Complete(_ context.Context, request core.ProviderRequest) (core.ProviderResponse, error) {
	p.lastRequest = request
	return p.response, nil
}

func TestAgentRejectsSilentEmptyResponse(t *testing.T) {
	cfg := config.Defaults()
	store, _ := session.New(t.TempDir())
	engine := Engine{
		Config: cfg, Provider: &staticProvider{}, Store: store,
		Tools: tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"*"}}), nil, 1024),
	}
	_, err := engine.Run(context.Background(), "", "hi")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepareProviderContextMovesSummaryIntoSystemPrompt(t *testing.T) {
	systemPrompt, messages := prepareProviderContext("base", []core.Message{
		{Role: "context", Content: "preserved state"},
		{Role: "user", Content: "continue"},
	})
	if !strings.Contains(systemPrompt, "<session-context>\npreserved state\n</session-context>") {
		t.Fatalf("system prompt=%q", systemPrompt)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("messages=%+v", messages)
	}
}

func TestCompactReplacesActiveContextWithSummary(t *testing.T) {
	cfg := config.Defaults()
	store, err := session.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := session.NewID()
	if err := store.AppendEvent(id, string(core.EventTurnStarted), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(id, core.Message{Role: "user", Content: "inspect the board"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(id, core.Message{Role: "assistant", Content: "board is healthy"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(id, string(core.EventTurnCompleted), nil); err != nil {
		t.Fatal(err)
	}
	provider := &staticProvider{response: core.ProviderResponse{Content: "Board inspection completed; all checks passed."}}
	engine := Engine{
		Config: cfg, Provider: provider, Store: store, SystemPrompt: "test",
		Tools: tools.New(policy.New(config.SecurityConfig{AllowedTools: []string{"*"}}), nil, 1024),
	}
	summary, err := engine.Compact(context.Background(), id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "all checks passed") || len(provider.lastRequest.Tools) != 0 {
		t.Fatalf("summary=%q request=%+v", summary, provider.lastRequest)
	}
	messages, err := store.Messages(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "context" || messages[0].Content != summary {
		t.Fatalf("messages=%+v", messages)
	}
}
